package native

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/eth/tracers/native/proto"

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/params"
)

// initializer for the tracer
func init() {
	tracers.DefaultDirectory.Register("txGasDimension", NewTxGasDimensionTracer, false)
}

// gasDimensionTracer struct
type TxGasDimensionTracer struct {
	*BaseGasDimensionTracer
	Dimensions GasesByDimension
}

// gasDimensionTracer returns a new tracer that traces gas
// usage for each opcode against the dimension of that opcode
// takes a context, and json input for configuration parameters
func NewTxGasDimensionTracer(
	_ *tracers.Context,
	cfg json.RawMessage,
	chainConfig *params.ChainConfig,
) (*tracers.Tracer, error) {
	baseGasDimensionTracer, err := NewBaseGasDimensionTracer(cfg, chainConfig)
	if err != nil {
		return nil, err
	}
	t := &TxGasDimensionTracer{
		BaseGasDimensionTracer: baseGasDimensionTracer,
		Dimensions:             ZeroGasesByDimension(),
	}

	return &tracers.Tracer{
		Hooks: &tracing.Hooks{
			OnOpcode:  t.OnOpcode,
			OnTxStart: t.OnTxStart,
			OnTxEnd:   t.OnTxEnd,
			OnFault:   t.OnFault,
		},
		GetResult: t.GetResult,
		Stop:      t.Stop,
	}, nil
}

// ############################################################################
//                                    HOOKS
// ############################################################################

// hook into each opcode execution
func (t *TxGasDimensionTracer) OnOpcode(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) {
	interrupted, gasesByDimension, callStackInfo, opcode := t.onOpcodeStart(pc, op, gas, cost, scope, rData, depth, err)
	if interrupted {
		return
	}

	// if callStackInfo is not nil then we need to take a note of the index of the
	// DimensionLog that represents this opcode and save the callStackInfo
	// to call finishX after the call has returned
	if WasCallOrCreate(opcode) && err == nil {
		t.handleCallStackPush(callStackInfo)
	} else {
		// track the execution gas of all opcodes (but not the opcodes that do calls)
		t.AddToRootExecutionGasAccumulated(gasesByDimension.OneDimensionalGasCost)

		// update the dimensions for this opcode
		t.Dimensions.OneDimensionalGasCost += gasesByDimension.OneDimensionalGasCost
		t.Dimensions.Computation += gasesByDimension.Computation
		t.Dimensions.StateAccess += gasesByDimension.StateAccess
		t.Dimensions.StateGrowth += gasesByDimension.StateGrowth
		t.Dimensions.HistoryGrowth += gasesByDimension.HistoryGrowth
		t.Dimensions.StateGrowthRefund += gasesByDimension.StateGrowthRefund

		// if the opcode returns from the call stack depth, or
		// if this is an opcode immediately after a call that did not increase the stack depth
		// because it called an empty account or contract or wrong function signature,
		// call the appropriate finishX function to write the gas dimensions
		// for the call that increased the stack depth in the past
		if depth < t.depth {
			interrupted, totalGasUsedByCall, _, finishGasesByDimension := t.callFinishFunction(pc, depth, gas)
			if interrupted {
				return
			}

			// track the execution gas of all opcodes that do calls
			if depth == 1 {
				t.AddToRootExecutionGasAccumulated(totalGasUsedByCall)
			}

			t.Dimensions.OneDimensionalGasCost += finishGasesByDimension.OneDimensionalGasCost
			t.Dimensions.Computation += finishGasesByDimension.Computation
			t.Dimensions.StateAccess += finishGasesByDimension.StateAccess
			t.Dimensions.StateGrowth += finishGasesByDimension.StateGrowth
			t.Dimensions.HistoryGrowth += finishGasesByDimension.HistoryGrowth
			t.Dimensions.StateGrowthRefund += finishGasesByDimension.StateGrowthRefund

			t.depth -= 1
			t.updateCallChildExecutionCost(totalGasUsedByCall)
		}
		t.updateCallChildExecutionCost(gasesByDimension.OneDimensionalGasCost)
	}
	// update the access list for this opcode AFTER all the other logic is done
	t.accessListTracer.OnOpcode(pc, op, gas, cost, scope, rData, depth, err)
}

// if there is an error in the evm, e.g. invalid jump,
// out of gas, max call depth exceeded, etc, this hook is called
func (t *TxGasDimensionTracer) OnFault(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	depth int,
	err error,
) {
	// if opcode is a revert, do not modify the gas dimension
	// they are already right since reverts return gas to the caller
	if vm.OpCode(op) == vm.REVERT {
		return
	}
	// if there was an error, go get the opcode that caused the error
	// Then since errors consume all gas, add the gas
	// to the one dimensional and computation gas cost of the affected opcode
	// note we only do this for the last depth in the call stack
	// because if an error happens inside a call, the gas for the call opcode
	//  will capture the excess gas consumed by the error/revert
	if depth == 1 {
		gasAfterOpcode := gas - cost // don't double charge the cost of the opcode itself
		t.Dimensions.OneDimensionalGasCost += gasAfterOpcode
		t.Dimensions.Computation += gasAfterOpcode
	}
}

// ############################################################################
//                        JSON OUTPUT PRODUCTION
// ############################################################################

// Error returns the VM error captured by the trace.
func (t *TxGasDimensionTracer) Error() error { return t.err }

// ExecutionResult groups all dimension logs emitted by the EVM
// while replaying a transaction in debug mode as well as transaction
// execution status, the amount of gas used and the return value
type TxGasDimensionExecutionResult struct {
	BaseExecutionResult
	Dimensions GasesByDimension `json:"dimensions"`
}

// produce json result for output from tracer
// this is what the end-user actually gets from the RPC endpoint
func (t *TxGasDimensionTracer) GetResult() (json.RawMessage, error) {
	baseExecutionResult, err := t.GetBaseExecutionResult()
	if err != nil {
		return nil, err
	}

	return json.Marshal(&TxGasDimensionExecutionResult{
		BaseExecutionResult: baseExecutionResult,
		Dimensions:          t.Dimensions,
	})
}

// produce protobuf serialized result
// for storing to file in compact format
func (t *TxGasDimensionTracer) GetProtobufResult() (*proto.TxGasDimensionResult, error) {
	baseExecutionResult, err := t.GetBaseExecutionResult()
	if err != nil {
		return nil, err
	}

	// handle optional fields, set to nil
	// for "not present" values, such as zero or false
	var adjustedRefund *uint64 = nil
	var rootIsPrecompileAdjustment *uint64 = nil
	var rootIsStylusAdjustment *uint64 = nil
	var failed *bool = nil
	var transactionReverted *bool = nil

	if baseExecutionResult.AdjustedRefund != 0 {
		adjustedRefund = &baseExecutionResult.AdjustedRefund
	}
	if baseExecutionResult.RootIsPrecompile {
		if baseExecutionResult.RootIsPrecompileAdjustment != 0 {
			rootIsPrecompileAdjustment = &baseExecutionResult.RootIsPrecompileAdjustment
		}
	}
	if baseExecutionResult.RootIsStylus {
		if baseExecutionResult.RootIsStylusAdjustment != 0 {
			rootIsStylusAdjustment = &baseExecutionResult.RootIsStylusAdjustment
		}
	}
	if baseExecutionResult.Failed {
		failed = &baseExecutionResult.Failed
	}
	if baseExecutionResult.Status != 0 {
		var trueBool bool = true
		transactionReverted = &trueBool
	}

	executionResult := &proto.TxGasDimensionResult{
		GasUsed:                    baseExecutionResult.GasUsed,
		GasUsedL1:                  baseExecutionResult.GasUsedForL1,
		GasUsedL2:                  baseExecutionResult.GasUsedForL2,
		IntrinsicGas:               baseExecutionResult.IntrinsicGas,
		AdjustedRefund:             adjustedRefund,
		RootIsPrecompileAdjustment: rootIsPrecompileAdjustment,
		RootIsStylusAdjustment:     rootIsStylusAdjustment,
		Failed:                     failed,
		TransactionReverted:        transactionReverted,
		Dimensions: &proto.GasesByDimension{
			OneDimensionalGasCost: t.Dimensions.OneDimensionalGasCost,
			Computation:           t.Dimensions.Computation,
			StateAccess:           t.Dimensions.StateAccess,
			StateGrowth:           t.Dimensions.StateGrowth,
			HistoryGrowth:         t.Dimensions.HistoryGrowth,
			StateGrowthRefund:     t.Dimensions.StateGrowthRefund,
		},
		TxHash:      baseExecutionResult.TxHash,
		BlockNumber: baseExecutionResult.BlockNumber.Uint64(),
	}
	return executionResult, nil
}
