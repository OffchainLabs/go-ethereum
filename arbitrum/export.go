package arbitrum

import (
	"context"

	"github.com/paxosglobal/go-ethereum-arbitrum/common/hexutil"
	"github.com/paxosglobal/go-ethereum-arbitrum/core"
	"github.com/paxosglobal/go-ethereum-arbitrum/internal/ethapi"
	"github.com/paxosglobal/go-ethereum-arbitrum/rpc"
)

type TransactionArgs = ethapi.TransactionArgs

func EstimateGas(ctx context.Context, b ethapi.Backend, args TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, overrides *ethapi.StateOverride, gasCap uint64) (hexutil.Uint64, error) {
	return ethapi.DoEstimateGas(ctx, b, args, blockNrOrHash, overrides, gasCap)
}

func NewRevertReason(result *core.ExecutionResult) error {
	return ethapi.NewRevertError(result)
}
