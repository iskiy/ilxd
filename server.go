// Copyright (c) 2022 Project Illium
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	badger "github.com/ipfs/go-ds-badger"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/project-illium/ilxd/blockchain"
	"github.com/project-illium/ilxd/blockchain/indexers"
	"github.com/project-illium/ilxd/consensus"
	"github.com/project-illium/ilxd/mempool"
	"github.com/project-illium/ilxd/net"
	params "github.com/project-illium/ilxd/params"
	"github.com/project-illium/ilxd/repo"
	"github.com/project-illium/ilxd/sync"
	"github.com/project-illium/ilxd/types"
	"github.com/project-illium/ilxd/types/blocks"
	"go.uber.org/zap"
	"sort"
	stdsync "sync"
	"time"
)

var log = zap.S()

type orphanBlock struct {
	blk          *blocks.Block
	relayingPeer peer.ID
	firstSeen    time.Time
}

// Server is the main class that brings all the constituent parts together
// into a full node.
type Server struct {
	cancelFunc   context.CancelFunc
	ctx          context.Context
	config       *repo.Config
	params       *params.NetworkParams
	ds           repo.Datastore
	network      *net.Network
	blockchain   *blockchain.Blockchain
	mempool      *mempool.Mempool
	engine       *consensus.ConsensusEngine
	chainService *sync.ChainService

	orphanBlocks map[types.ID]*orphanBlock
	orphanLock   stdsync.RWMutex

	activeInventory map[types.ID]*blocks.Block
	inventoryLock   stdsync.RWMutex

	inflightRequests map[types.ID]bool
	inflightLock     stdsync.RWMutex
	policy           *Policy
}

// BuildServer is the constructor for the server. We pass in the config file here
// and use it to configure all the various parts of the Server.
func BuildServer(config *repo.Config) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())

	s := Server{}

	// Logging
	if err := setupLogging(config.LogDir, config.LogLevel, config.Testnet); err != nil {
		return nil, err
	}

	// Policy
	policy := &Policy{
		MinFeePerByte:      types.Amount(config.MinFeePerByte),
		MinStake:           types.Amount(config.MinStake),
		BlocksizeSoftLimit: config.BlocksizeSoftLimit,
		MaxMessageSize:     config.MaxMessageSize,
	}
	for _, id := range config.TreasuryWhitelist {
		w, err := types.NewIDFromString(id)
		if err != nil {
			return nil, err
		}
		s.policy.TreasuryWhitelist = append(s.policy.TreasuryWhitelist, w)
	}

	// Parameter selection
	var netParams *params.NetworkParams
	if config.Testnet {
		netParams = &params.Testnet1Params
	} else if config.Regest {
		netParams = &params.RegestParams
	} else {
		netParams = &params.MainnetParams
	}

	// Setup up badger datastore
	ds, err := badger.NewDatastore(config.DataDir, &badger.DefaultOptions)
	if err != nil {
		return nil, err
	}

	// Load or create the private key for the node
	var privKey crypto.PrivKey
	has, err := repo.HasNetworkKey(ds)
	if err != nil {
		return nil, err
	}
	if has {
		privKey, err = repo.LoadNetworkKey(ds)
		if err != nil {
			return nil, err
		}
	} else {
		privKey, _, err = repo.GenerateNetworkKeypair()
		if err != nil {
			return nil, err
		}
		if err := repo.PutNetworkKey(ds, privKey); err != nil {
			return nil, err
		}
	}

	// Select seed addresses
	var seedAddrs []string
	if config.SeedAddrs != nil {
		seedAddrs = config.SeedAddrs
	} else {
		seedAddrs = netParams.SeedAddrs
	}

	// Select listen addresses
	var listenAddrs []string
	if config.ListenAddrs != nil {
		listenAddrs = config.ListenAddrs
	} else {
		listenAddrs = netParams.ListenAddrs
	}

	// Create the blockchain
	sigCache := blockchain.NewSigCache(blockchain.DefaultSigCacheSize)
	proofCache := blockchain.NewProofCache(blockchain.DefaultProofCacheSize)

	blockchainOpts := []blockchain.Option{
		blockchain.Params(netParams),
		blockchain.Datastore(ds),
		blockchain.MaxNullifiers(blockchain.DefaultMaxNullifiers),
		blockchain.MaxTxoRoots(blockchain.DefaultMaxTxoRoots),
		blockchain.SignatureCache(sigCache),
		blockchain.SnarkProofCache(proofCache),
	}

	if config.DropTxIndex {
		if err := indexers.DropTxIndex(ds); err != nil {
			return nil, err
		}
	}

	if !config.NoTxIndex {
		blockchainOpts = append(blockchainOpts, blockchain.Indexers([]indexers.Indexer{indexers.NewTxIndex()}))

	}
	chain, err := blockchain.NewBlockchain(blockchainOpts...)
	if err != nil {
		return nil, err
	}

	// Mempool
	mempoolOpts := []mempool.Option{
		mempool.SignatureCache(sigCache),
		mempool.ProofCache(proofCache),
		mempool.Params(netParams),
		mempool.BlockchainView(chain),
		mempool.MinStake(policy.MinStake),
		mempool.FeePerByte(policy.MinFeePerByte),
	}

	mpool, err := mempool.NewMempool(mempoolOpts...)
	if err != nil {
		return nil, err
	}

	// Network
	networkOpts := []net.Option{
		net.Datastore(ds),
		net.SeedAddrs(seedAddrs),
		net.ListenAddrs(listenAddrs),
		net.UserAgent(config.UserAgent),
		net.PrivateKey(privKey),
		net.Params(netParams),
		net.BlockValidator(s.handleIncomingBlock),
		net.MempoolValidator(mpool.ProcessTransaction),
		net.MaxBanscore(config.MaxBanscore),
		net.BanDuration(config.BanDuration),
	}
	if config.DisableNATPortMap {
		networkOpts = append(networkOpts, net.DisableNatPortMap())
	}

	network, err := net.NewNetwork(ctx, networkOpts...)
	if err != nil {
		return nil, err
	}

	engine, err := consensus.NewConsensusEngine(ctx, netParams, network, chain, s.requestBlock)
	if err != nil {
		return nil, err
	}

	s.ctx = ctx
	s.cancelFunc = cancel
	s.config = config
	s.params = netParams
	s.ds = ds
	s.network = network
	s.blockchain = chain
	s.mempool = mpool
	s.engine = engine
	s.chainService = sync.NewChainService(ctx, s.fetchBlock, network, netParams)
	s.orphanBlocks = make(map[types.ID]*orphanBlock)
	s.activeInventory = make(map[types.ID]*blocks.Block)
	s.inflightRequests = make(map[types.ID]bool)
	s.orphanLock = stdsync.RWMutex{}
	s.inventoryLock = stdsync.RWMutex{}
	s.inflightLock = stdsync.RWMutex{}
	s.policy = policy

	s.printListenAddrs()

	return &s, nil
}

func (s *Server) handleIncomingBlock(xThinnerBlk *blocks.XThinnerBlock, p peer.ID) error {
	// Try to decode the block. This should succeed most of the time unless
	// the merkle root is invalid.
	blk, err := s.decodeXthinner(xThinnerBlk, p)
	if err != nil {
		return err
	}

	return s.processBlock(blk, p, false)
}

func (s *Server) processBlock(blk *blocks.Block, relayingPeer peer.ID, recheck bool) error {
	err := s.blockchain.CheckConnectBlock(blk)
	switch err.(type) {
	case blockchain.OrphanBlockError:
		// An orphan is a block's whose height is greater than
		// our current blockchain tip. It might be valid, but
		// we can't validate it until we connect the parent.
		// We'll store it in memory and will circle back after
		// we connect the next block.
		s.orphanLock.Lock()
		s.orphanBlocks[blk.ID()] = &orphanBlock{
			blk:          blk,
			firstSeen:    time.Now(),
			relayingPeer: relayingPeer,
		}
		s.orphanLock.Unlock()
		return err
	case blockchain.RuleError:
		if recheck {
			s.network.IncreaseBanscore(relayingPeer, 34, 0)
			return err
		}
		// If the merkle root is invalid it either means we had a collision in the
		// mempool or this block is genuinely invalid.
		//
		// Let's download the txid list from the peer and figure out which it is.
		if blockchain.ErrorIs(err, blockchain.ErrInvalidTxRoot) {
			blk, err := s.fetchBlockTxids(blk, relayingPeer)
			if err != nil {
				s.network.IncreaseBanscore(relayingPeer, 34, 0)

				for _, pid := range s.network.Host().Network().Peers() {
					blk, err = s.fetchBlockTxids(blk, pid)
					if err == nil {
						return s.processBlock(blk, relayingPeer, true)
					}
				}
			} else {
				return s.processBlock(blk, relayingPeer, true)
			}

		} else if blockchain.ErrorIs(err, blockchain.ErrDoesNotConnect) {
			// Small chance of a race condition where we receive a block
			// right after we finalize a block at the same height. We'll
			// only lightly increase the penalty for this to prevent banning
			// nodes for innocent behavior.
			s.network.IncreaseBanscore(relayingPeer, 0, 10)
		} else {
			// Ban nodes that send us invalid blocks.
			s.network.IncreaseBanscore(relayingPeer, 101, 0)
		}
		return err
	}
	if err != nil {
		return err
	}

	callback := make(chan consensus.Status)
	// TODO: set initial preference correctly
	startTime := time.Now()

	s.inventoryLock.Lock()
	s.activeInventory[blk.ID()] = blk
	s.inventoryLock.Unlock()

	s.orphanLock.Lock()
	delete(s.orphanBlocks, blk.ID())
	s.orphanLock.Unlock()

	s.engine.NewBlock(blk.ID(), true, callback)

	go func(b *blocks.Block, t time.Time) {
		select {
		case status := <-callback:
			switch status {
			case consensus.StatusFinalized:
				blockID := blk.ID()
				log.Debugf("Block %s finalized in %s milliseconds", blockID, time.Since(t).Milliseconds())
				if err := s.blockchain.ConnectBlock(b, blockchain.BFNone); err != nil {
					log.Warnf("Connect block error: block %s: %s", blockID, err)
				} else {
					log.Infof("New block: %s, (height: %d, transactions: %d)", blockID, blk.Header.Height, len(b.Transactions))
				}
			case consensus.StatusRejected:
				log.Debugf("Block %s rejected by consensus", b.ID())
			}

			s.inventoryLock.Lock()
			delete(s.activeInventory, blk.ID())
			s.inventoryLock.Unlock()

			s.orphanLock.Lock()
			for _, orphan := range s.orphanBlocks {
				if orphan.blk.Header.Height == blk.Header.Height+1 {
					// FIXME: delete old orphans
					s.processBlock(orphan.blk, orphan.relayingPeer, false)
					break
				}
			}
			s.orphanLock.Unlock()
		case <-s.ctx.Done():
			return
		}
	}(blk, startTime)
	return nil
}

func (s *Server) decodeXthinner(xThinnerBlk *blocks.XThinnerBlock, relayingPeer peer.ID) (*blocks.Block, error) {
	blk, missing := s.mempool.DecodeXthinner(xThinnerBlk)
	if len(missing) > 0 {
		txs, err := s.chainService.GetBlockTxs(relayingPeer, xThinnerBlk.ID(), missing)
		if err == nil {
			for i, tx := range txs {
				blk.Transactions[missing[i]] = tx
			}
			return blk, nil
		} else {
			s.network.IncreaseBanscore(relayingPeer, 34, 0)
		}

		for _, pid := range s.network.Host().Network().Peers() {
			txs, err := s.chainService.GetBlockTxs(pid, xThinnerBlk.ID(), missing)
			if err == nil {
				for i, tx := range txs {
					blk.Transactions[missing[i]] = tx
				}
				return blk, nil
			}
			// We won't increase the ban score for these peers as they didn't send
			// us the block. If the block is invalid they may not be able to legitimately
			// respond to our request.
		}
	}
	return blk, nil
}

func (s *Server) fetchBlockTxids(blk *blocks.Block, p peer.ID) (*blocks.Block, error) {
	txids, err := s.chainService.GetBlockTxids(p, blk.ID())
	if err != nil {
		return nil, err
	}
	if len(txids) != len(blk.Transactions) {
		return nil, errors.New("getblocktxids: peer returned unexpected  number of IDs")
	}
	missing := make([]uint32, 0, len(blk.Transactions))
	for i, tx := range blk.Transactions {
		if tx.ID() != txids[i] {
			missing = append(missing, uint32(i))
		}
	}
	if len(missing) == 0 {
		return nil, errors.New("block invalid")
	}
	txs, err := s.chainService.GetBlockTxs(p, blk.ID(), missing)
	if err != nil {
		return nil, err
	}
	for i, tx := range txs {
		blk.Transactions[missing[i]] = tx
	}
	return blk, nil
}

func (s *Server) fetchBlock(blockID types.ID) (*blocks.Block, error) {
	s.inventoryLock.RLock()
	defer s.inventoryLock.RUnlock()

	if blk, ok := s.activeInventory[blockID]; ok {
		return blk, nil
	}

	return s.blockchain.GetBlockByID(blockID)
}

func (s *Server) requestBlock(blockID types.ID, remotePeer peer.ID) {
	s.inflightLock.RLock()
	if _, ok := s.inflightRequests[blockID]; ok {
		s.inflightLock.RUnlock()
		return
	}
	s.inflightLock.RLock()

	s.inflightLock.Lock()
	s.inflightRequests[blockID] = true
	s.inflightLock.Unlock()

	blk, err := s.chainService.GetBlock(remotePeer, blockID)
	if err != nil {
		s.inflightLock.Lock()
		delete(s.inflightRequests, blockID)
		s.inflightLock.Unlock()
		return
	}

	s.processBlock(blk, remotePeer, false)

	time.AfterFunc(time.Minute*5, func() {
		s.inflightLock.Lock()
		delete(s.inflightRequests, blockID)
		s.inflightLock.Unlock()
	})

}

// Close shuts down all the parts of the server and blocks until
// they finish closing.
func (s *Server) Close() error {
	s.cancelFunc()
	if err := s.network.Close(); err != nil {
		return err
	}
	if err := s.ds.Close(); err != nil {
		return err
	}
	if err := s.blockchain.Close(); err != nil {
		return err
	}
	s.mempool.Close()
	return nil
}

func (s *Server) printListenAddrs() {
	log.Infof("PeerID: %s", s.network.Host().ID().String())
	var lisAddrs []string
	ifaceAddrs := s.network.Host().Addrs()
	for _, addr := range ifaceAddrs {
		lisAddrs = append(lisAddrs, addr.String())
	}
	sort.Strings(lisAddrs)
	for _, addr := range lisAddrs {
		log.Infof("Listening on %s", addr)
	}
}
