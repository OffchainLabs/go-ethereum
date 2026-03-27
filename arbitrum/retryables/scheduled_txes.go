package retryables

import (
	"context"
	"math"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
)

// RunScheduledTxes executes scheduled transactions (retryable redeems) including
// cascading redeems. Touches addresses for filtering via the TouchTxAddresses hook.
func RunScheduledTxes(ctx context.Context, b core.NodeInterfaceBackendAPI, statedb *state.StateDB, header *types.Header, blockCtx vm.BlockContext, runCtx *core.MessageRunContext, result *core.ExecutionResult) (*core.ExecutionResult, error) {
	scheduled := result.ScheduledTxes
	for runCtx.IsGasEstimation() && len(scheduled) > 0 {
		msg, err := core.TransactionToMessage(scheduled[0], types.NewArbitrumSigner(nil), header.BaseFee, runCtx)
		if err != nil {
			return nil, err
		}

		core.TouchTxAddresses(statedb, scheduled[0], msg.From)

		if result.UsedGas >= msg.GasLimit {
			result.UsedGas -= msg.GasLimit
		} else {
			log.Warn("Scheduling tx used less gas than scheduled tx has available", "usedGas", result.UsedGas, "scheduledGas", msg.GasLimit)
			result.UsedGas = 0
		}

		evm := b.GetEVM(ctx, statedb, header, &vm.Config{NoBaseFee: true}, &blockCtx)
		go func() {
			<-ctx.Done()
			evm.Cancel()
		}()

		scheduledTxResult, err := core.ApplyMessage(evm, msg, new(core.GasPool).AddGas(math.MaxUint64))
		if err != nil {
			return nil, err
		}
		if vmerr := statedb.Error(); vmerr != nil {
			return nil, vmerr
		}
		if scheduledTxResult.Failed() {
			return scheduledTxResult, nil
		}
		result.UsedGas += scheduledTxResult.UsedGas
		scheduled = append(scheduled[1:], scheduledTxResult.ScheduledTxes...)
	}
	return result, nil
}
