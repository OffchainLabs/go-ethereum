package arbitrum

import (
	"context"

	"github.com/curtis0505/arbitrum/common/hexutil"
	"github.com/curtis0505/arbitrum/core"
	"github.com/curtis0505/arbitrum/internal/ethapi"
	"github.com/curtis0505/arbitrum/rpc"
)

type TransactionArgs = ethapi.TransactionArgs

func EstimateGas(ctx context.Context, b ethapi.Backend, args TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, gasCap uint64) (hexutil.Uint64, error) {
	return ethapi.DoEstimateGas(ctx, b, args, blockNrOrHash, gasCap)
}

func NewRevertReason(result *core.ExecutionResult) error {
	return ethapi.NewRevertError(result)
}
