package arbitrum

import (
	"context"

	"github.com/curtis0505/arbitrum/core"
	"github.com/curtis0505/arbitrum/core/types"
)

type ArbInterface interface {
	PublishTransaction(ctx context.Context, tx *types.Transaction) error
	BlockChain() *core.BlockChain
	ArbNode() interface{}
}
