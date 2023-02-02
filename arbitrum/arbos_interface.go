package arbitrum

import (
	"context"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
)

type ArbInterface interface {
	PublishTransaction(ctx context.Context, tx *types.Transaction, options *ConditionalOptions) error
	BlockChain() *core.BlockChain
	ArbNode() interface{}
}
