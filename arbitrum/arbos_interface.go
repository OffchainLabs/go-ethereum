package arbitrum

import (
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
)

type ArbosWrapper interface {
	BuildBlock(force bool) (*types.Block, types.Receipts, *state.StateDB)

	EnqueueSequencerTx(tx *types.Transaction) error
}
