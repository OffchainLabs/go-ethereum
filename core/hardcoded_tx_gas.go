package core

import (
	"github.com/ethereum/go-ethereum/common"
)

// HardcodedTxGasUsed maps specific transaction hashes to the amount of GAS used by that
// specific transaction alone.
var HardcodedTxGasUsed = map[common.Hash]uint64{
	common.HexToHash("0x58df300a7f04fe31d41d24672786cbe1c58b4f3d8329d0d74392d814dd9f7e40"): 45606,
}
