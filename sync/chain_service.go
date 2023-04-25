// Copyright (c) 2022 The illium developers
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.

package sync

import (
	"context"
	"fmt"
	"github.com/google/martian/log"
	ctxio "github.com/jbenet/go-context/io"
	inet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-msgio"
	"github.com/project-illium/ilxd/net"
	"github.com/project-illium/ilxd/params"
	"github.com/project-illium/ilxd/types"
	"github.com/project-illium/ilxd/types/blocks"
	"github.com/project-illium/ilxd/types/transactions"
	"github.com/project-illium/ilxd/types/wire"
	"google.golang.org/protobuf/proto"
)

const (
	ChainServiceProtocol = "chainservice"
)

type FetchBlockFunc func(blockID types.ID) (*blocks.Block, error)

type ChainService struct {
	ctx        context.Context
	network    *net.Network
	params     *params.NetworkParams
	fetchBlock FetchBlockFunc
	ms         net.MessageSender
}

func NewChainService(ctx context.Context, fetchBlock FetchBlockFunc, network *net.Network, params *params.NetworkParams) *ChainService {
	cs := &ChainService{
		ctx:        ctx,
		network:    network,
		fetchBlock: fetchBlock,
		params:     params,
		ms:         net.NewMessageSender(network.Host(), params.ProtocolPrefix+ChainServiceProtocol),
	}
	cs.network.Host().SetStreamHandler(cs.params.ProtocolPrefix+ChainServiceProtocol, cs.HandleNewStream)
	return cs
}

func (cs *ChainService) HandleNewStream(s inet.Stream) {
	go cs.handleNewMessage(s)
}

func (cs *ChainService) handleNewMessage(s inet.Stream) {
	defer s.Close()
	contextReader := ctxio.NewReader(cs.ctx, s)
	reader := msgio.NewVarintReaderSize(contextReader, 1<<23)
	remotePeer := s.Conn().RemotePeer()
	defer reader.Close()

	for {
		select {
		case <-cs.ctx.Done():
			return
		default:
		}

		req := new(wire.MsgChainServiceRequest)
		if err := net.ReadMsg(cs.ctx, reader, req); err != nil {
			log.Debugf("Error reading from block service stream: peer: %s, error: %s", remotePeer, err.Error())
			return
		}

		var (
			resp proto.Message
			err  error
		)
		switch m := req.Msg.(type) {
		case *wire.MsgChainServiceRequest_GetBlockTxs:
			resp, err = cs.handleGetBlockTxs(m.GetBlockTxs)
		case *wire.MsgChainServiceRequest_GetBlockTxids:
			resp, err = cs.handleGetBlockTxids(m.GetBlockTxids)
		case *wire.MsgChainServiceRequest_GetBlock:
			resp, err = cs.handleGetBlock(m.GetBlock)
		}
		if err != nil {
			log.Errorf("Error handing block service message to peer: %s, error: %s", remotePeer, err.Error())
			continue
		}

		if resp != nil {
			if err := net.WriteMsg(s, resp); err != nil {
				log.Errorf("Error writing block service response to peer: %s, error: %s", remotePeer, err.Error())
				s.Reset()
			}
		}
	}
}

func (cs *ChainService) GetBlockTxs(p peer.ID, blockID types.ID, txIndexes []uint32) ([]*transactions.Transaction, error) {
	var (
		req = &wire.MsgChainServiceRequest{
			Msg: &wire.MsgChainServiceRequest_GetBlockTxs{
				GetBlockTxs: &wire.GetBlockTxsReq{
					BlockID:   blockID[:],
					TxIndexes: txIndexes,
				},
			},
		}
		resp = new(wire.MsgBlockTxsResp)
	)
	err := cs.ms.SendRequest(cs.ctx, p, req, resp)
	if err != nil {
		return nil, err
	}
	if resp.Error != wire.ErrorResponse_None {
		return nil, fmt.Errorf("error response from peer: %s", resp.GetError().String())
	}

	if len(resp.Transactions) != len(txIndexes) {
		cs.network.IncreaseBanscore(p, 50, 0)
		return nil, fmt.Errorf("peer %s did not return all requested txs", p.String())
	}

	return resp.Transactions, nil
}

func (cs *ChainService) handleGetBlockTxs(req *wire.GetBlockTxsReq) (*wire.MsgBlockTxsResp, error) {
	// FIXME: this will only serve txs from blocks that have passed consensus and
	// have been connected to the chain. We should also check the inventory of the
	// consensus engine. Blocks in the consensus engine have passed the block
	// validation rules and thus are safe to serve in response to this.
	//
	// This is needed since nodes will call this RPC when decoding xthinner blocks
	// and before they've validated them, let alone finalized them.
	blk, err := cs.fetchBlock(types.NewID(req.BlockID))
	if err != nil {
		return &wire.MsgBlockTxsResp{Error: wire.ErrorResponse_NotFound}, nil
	}

	resp := &wire.MsgBlockTxsResp{
		Transactions: make([]*transactions.Transaction, len(req.TxIndexes)),
	}

	for _, idx := range req.TxIndexes {
		if idx > uint32(len(blk.Transactions))-1 {
			return &wire.MsgBlockTxsResp{Error: wire.ErrorResponse_BadRequest}, nil
		}
		resp.Transactions[idx] = blk.Transactions[idx]
	}

	return resp, nil
}

func (cs *ChainService) GetBlockTxids(p peer.ID, blockID types.ID) ([]types.ID, error) {
	var (
		req = &wire.MsgChainServiceRequest{
			Msg: &wire.MsgChainServiceRequest_GetBlockTxids{
				GetBlockTxids: &wire.GetBlockTxidsReq{
					BlockID: blockID[:],
				},
			},
		}
		resp = new(wire.MsgBlockTxidsResp)
	)
	err := cs.ms.SendRequest(cs.ctx, p, req, resp)
	if err != nil {
		return nil, err
	}
	if resp.Error != wire.ErrorResponse_None {
		return nil, fmt.Errorf("error response from peer: %s", resp.GetError().String())
	}

	txids := make([]types.ID, 0, len(resp.Txids))
	for _, txid := range resp.Txids {
		txids = append(txids, types.NewID(txid))
	}

	return txids, nil
}

func (cs *ChainService) handleGetBlockTxids(req *wire.GetBlockTxidsReq) (*wire.MsgBlockTxidsResp, error) {
	blk, err := cs.fetchBlock(types.NewID(req.BlockID))
	if err != nil {
		return &wire.MsgBlockTxidsResp{Error: wire.ErrorResponse_NotFound}, nil
	}

	txids := make([][]byte, 0, len(blk.Transactions))
	for _, tx := range blk.Transactions {
		id := tx.ID()
		txids = append(txids, id[:])
	}

	resp := &wire.MsgBlockTxidsResp{
		Txids: txids,
	}

	return resp, nil
}

func (cs *ChainService) GetBlock(p peer.ID, blockID types.ID) (*blocks.Block, error) {
	var (
		req = &wire.MsgChainServiceRequest{
			Msg: &wire.MsgChainServiceRequest_GetBlock{
				GetBlock: &wire.GetBlockReq{
					BlockID: blockID[:],
				},
			},
		}
		resp = new(wire.MsgBlockResp)
	)
	err := cs.ms.SendRequest(cs.ctx, p, req, resp)
	if err != nil {
		return nil, err
	}
	if resp.Error != wire.ErrorResponse_None {
		return nil, fmt.Errorf("error response from peer: %s", resp.GetError().String())
	}

	return resp.Block, nil
}

func (cs *ChainService) handleGetBlock(req *wire.GetBlockReq) (*wire.MsgBlockResp, error) {
	blk, err := cs.fetchBlock(types.NewID(req.BlockID))
	if err != nil {
		return &wire.MsgBlockResp{Error: wire.ErrorResponse_NotFound}, nil
	}

	resp := &wire.MsgBlockResp{
		Block: blk,
	}

	return resp, nil
}
