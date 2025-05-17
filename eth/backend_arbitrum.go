package eth

import (
	"github.com/curtis0505/arbitrum/core"
	"github.com/curtis0505/arbitrum/core/state"
	"github.com/curtis0505/arbitrum/core/types"
	"github.com/curtis0505/arbitrum/core/vm"
	"github.com/curtis0505/arbitrum/ethdb"
)

func NewArbEthereum(
	blockchain *core.BlockChain,
	chainDb ethdb.Database,
) *Ethereum {
	return &Ethereum{
		blockchain: blockchain,
		chainDb:    chainDb,
	}
}

func (eth *Ethereum) StateAtTransaction(block *types.Block, txIndex int, reexec uint64) (core.Message, vm.BlockContext, *state.StateDB, error) {
	return eth.stateAtTransaction(block, txIndex, reexec)
}
