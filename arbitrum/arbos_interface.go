package arbitrum

import (
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
)

type ArbosWrapper interface {
	BuildBlock(force bool) (*types.Block, types.Receipts, *state.StateDB)

	//l1header is header is nil if message doesn't yet appear in l1
	EnqueueSequencerTx(tx *types.Transaction, l1header *types.Header) error
}
