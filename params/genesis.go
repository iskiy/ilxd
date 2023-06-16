// Copyright (c) 2022 Project Illium
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.

package params

import (
	"encoding/hex"
	"github.com/project-illium/ilxd/types/blocks"
	"github.com/project-illium/ilxd/types/transactions"
	"time"
)

// MainnetGenesisBlock is the genesis block for the mainnet.
//
// Technically this is not a valid block and would not pass the normal validation
// rules. The reason for this is because the genesis block needs to do two things
// ― create new coins and stake them. Without at least one validator created in the
// genesis block the chain cannot move forward. Normally however, a stake transaction
// cannot stake a coinbase created in the same block since the stake's zk-snark proof
// must make use of the block's txoRoot which isn't known until after the block is
// connected.
var MainnetGenesisBlock = &blocks.Block{
	Header: &blocks.BlockHeader{
		Producer_ID: []byte{0x00}, //TODO
		Height:      0,
		Timestamp:   time.Unix(0, 0).Unix(), //TODO
		Parent:      make([]byte, 32),
		Version:     1,
		TxRoot:      []byte{0x00}, //TODO
		Signature:   []byte{0x00}, //TODO
	},
	Transactions: []*transactions.Transaction{
		{
			Tx: &transactions.Transaction_CoinbaseTransaction{
				CoinbaseTransaction: &transactions.CoinbaseTransaction{
					Validator_ID: []byte{0x00}, //TODO
					NewCoins:     1 << 60 / 5,
					Outputs: []*transactions.Output{
						{
							Commitment: []byte{0x00}, //TODO
							Ciphertext: []byte{0x00}, //TODO
						},
					},
					Signature: []byte{0x00}, //TODO
					Proof:     []byte{0x00}, //TODO
				},
			},
		},
		{
			Tx: &transactions.Transaction_StakeTransaction{
				StakeTransaction: &transactions.StakeTransaction{
					Validator_ID: []byte{0x00}, //TODO
					Amount:       0,
					Nullifier:    []byte{0xff},
					Signature:    nil,
					Proof:        nil,
				},
			},
		},
	},
}

var RegtestMnemonicSeed = "machine owner oval voyage hero pride index rack doll planet route unaware survey canyon search million embrace power thumb goat design rich grab rhythm"

var RegtestGenesisKey = []byte{
	0x08, 0x01, 0x12, 0x40, 0xdc, 0xd8, 0xb1, 0x9d,
	0x2c, 0xc6, 0x6f, 0x0e, 0xc6, 0x13, 0xd4, 0xb0,
	0x8b, 0x7d, 0x73, 0x68, 0x2e, 0x2e, 0x11, 0x12,
	0x2c, 0x09, 0x95, 0x9a, 0x2c, 0xc0, 0x00, 0xb9,
	0x9a, 0x52, 0x5a, 0xcb, 0xb5, 0x62, 0xe4, 0x8c,
	0xa1, 0x18, 0xdb, 0x0f, 0x24, 0xa5, 0x3c, 0xfb,
	0xae, 0x9f, 0x6a, 0x3a, 0x67, 0xf8, 0x63, 0xe6,
	0x03, 0x15, 0x95, 0xd6, 0x43, 0xb7, 0xd8, 0x91,
	0x62, 0x1a, 0xc2, 0x80,
}

var RegtestGenesisBlock = &blocks.Block{
	Header: &blocks.BlockHeader{
		Version:     1,
		Height:      0,
		Parent:      hexToBytes("0000000000000000000000000000000000000000000000000000000000000000"),
		Timestamp:   1686874710,
		TxRoot:      hexToBytes("40917da3f861af8d07ef1867f5ab50a18a4b7e4547dd678d896115c81a5196a8"),
		Producer_ID: nil,
		Signature:   nil,
	},
	Transactions: []*transactions.Transaction{
		{
			Tx: &transactions.Transaction_CoinbaseTransaction{
				CoinbaseTransaction: &transactions.CoinbaseTransaction{
					Validator_ID: hexToBytes("002408011220b562e48ca118db0f24a53cfbae9f6a3a67f863e6031595d643b7d891621ac280"),
					NewCoins:     230584300921369395,
					Outputs: []*transactions.Output{
						{
							Commitment: hexToBytes("8ad4cc17bcf1f0a3f3eda0754a92cb29e183b9cea74a9bb760e168ffab5d48a0"),
							Ciphertext: hexToBytes("ceb4f6c9d440ec9da793a42823801b30d54a6c1a7e01bb8ea3a98c9997542ec6cf5c7cc8a9af3e028535abf0deb2da438e25a24a1264e83acd8a35ee8bf2811aec988502bac07a4bc55f71e0b0fac1e4d67e4916eb6ba2b4ecaa13702aed3d56ab81c1ec35a22896adc97c89ecb35bf53ad5eceac39a2354f02528d9b35a138abc0b789d2e4838d7de297a99288339f0b8ca3e7eb171a1054b81616736642cbf2adb3046691c4c8d0f9b0be914a4f953297fc2b4c55458d6962ac05258d1611215454d220f4df619b5ca02cb2fb17dde2946baaa7669c4e605f109ba678b975f0925bfd4d50b6a4c8dfd7afad957c1dafc21be3917f66da7573de1bcc1bd5712e3d93c8f42707784ad03681e3dd5f212a19a5416a49464739ea6ee663983a195a4200f595cd9ed8952160d0c3ec5c6cc\n849e5f844b5e0ff9faae2fb9427d4a65dc6149f4a829b1d63e883156cde13a5499630218adbf280a2f1e355744d7af0b51e852087e6c6c373baa1a256853daaf6baacc3565cc175616339062826a0e03494ed94071ad5a9233b25d1a6ca61d48ab4d7f9d851a828f3e8"),
						},
						{
							Commitment: hexToBytes("484a548038929251daeda8be545ad912260fc1e6a9ec19d92d99368c48bd3dfd"),
							Ciphertext: hexToBytes("a6769539fb2e9bed5842d0e20a97e3c51ffc8f897b9a4d3102ea09d68940d9c8cd70bd7ff5f7ba7bbb5d4dfc61a78b25806dcbe788c8307b7f1d0adf14473459d20e67ccad6ccaf7ae24f96545a561afa77a95688d0e89396d2db7f5274803d4a1d5f828820862d27305a86353cff586e575a03084f2b51d4e90cdc08de484fcd9d2406e2a13b6022fcfa57610353b3adc1b2d995188ead93c968c82cd5729fd5f9388e6b51a1da83960614ee65769de60745221333ba28e8d340456feaa8fbce735c18ff9f16c18aad2a7330a3b8e7040c59c85c2a409433d1c2b5f7307ae83b09e33f329eebee1acef9ec92eb6e9581e843989347ee0e64b151d71616c5bb8d6085211e95394e3cffcf370c0156934c05b1c596bb23d4fb53007bea1e03d1069bc2674a6a86c7a1eb86ac4f9084814\n849e5f844b5e0ff9faae2fb9427d4a65dc6149f4a829b1d63e883156cde13a5499630218adbf280a2f1e355744d7af0b51e852087e6c6c373baa1a256853daaf6baacc3565cc175616339062826a0e03494ed94071ad5a9233b25d1a6ca61d48ab4d7f9d851a828f3e8"),
						},
					},
					Signature: hexToBytes("638631ac50cd16c8ffbd13742b3a5a82c2b4a99600f7e8e80a0c3d518e1f97b8d883931732ef4ee4f529e7f207c90e3c0c0dce97b2b28cc18058b25bf041d907"),
					Proof:     nil,
				},
			},
		},
		{
			Tx: &transactions.Transaction_StakeTransaction{
				StakeTransaction: &transactions.StakeTransaction{
					Validator_ID: hexToBytes("002408011220b562e48ca118db0f24a53cfbae9f6a3a67f863e6031595d643b7d891621ac280"),
					Amount:       115292150460684697,
					Nullifier:    hexToBytes("931bf9e829315491463feed621c3b964663a12e05ac964c938b2e70bf75f14d0"),
					TxoRoot:      hexToBytes("144512edae5997c684c1cb9a775ff64895974b66842f5470365727f548077d8b"),
					Locktime:     0,
					Signature:    hexToBytes("8342cd818c8651806b51b9283156af0cc8e05b9593684b0247612ba4fef47bc85d7f7381941c14ed849e038041626ff92f8b44656c03bf8621863ce01a3ff701"),
					Proof:        nil,
				},
			},
		},
	},
}

func hexToBytes(s string) []byte {
	ret, _ := hex.DecodeString(s)
	return ret
}
