package native

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/eth/tracers/native/proto"

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/params"
	protobuf "google.golang.org/protobuf/proto"
)

// initializer for the tracer
func init() {
	tracers.DefaultDirectory.Register("txGasDimensionByOpcode", NewTxGasDimensionByOpcodeLogger, false)
}

// gasDimensionTracer struct
type TxGasDimensionByOpcodeTracer struct {
	BaseGasDimensionTracer
	OpcodeToDimensions map[vm.OpCode]GasesByDimension
}

// gasDimensionTracer returns a new tracer that traces gas
// usage for each opcode against the dimension of that opcode
// takes a context, and json input for configuration parameters
func NewTxGasDimensionByOpcodeLogger(
	_ *tracers.Context,
	_ json.RawMessage,
	_ *params.ChainConfig,
) (*tracers.Tracer, error) {

	t := &TxGasDimensionByOpcodeTracer{
		BaseGasDimensionTracer: NewBaseGasDimensionTracer(),
		OpcodeToDimensions:     make(map[vm.OpCode]GasesByDimension),
	}

	return &tracers.Tracer{
		Hooks: &tracing.Hooks{
			OnOpcode:  t.OnOpcode,
			OnTxStart: t.OnTxStart,
			OnTxEnd:   t.OnTxEnd,
		},
		GetResult: t.GetResult,
		Stop:      t.Stop,
	}, nil
}

// ############################################################################
//                                    HOOKS
// ############################################################################

// hook into each opcode execution
func (t *TxGasDimensionByOpcodeTracer) OnOpcode(
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

		// update the aggregrate map for this opcode
		accumulatedDimensions := t.OpcodeToDimensions[opcode]

		accumulatedDimensions.OneDimensionalGasCost += gasesByDimension.OneDimensionalGasCost
		accumulatedDimensions.Computation += gasesByDimension.Computation
		accumulatedDimensions.StateAccess += gasesByDimension.StateAccess
		accumulatedDimensions.StateGrowth += gasesByDimension.StateGrowth
		accumulatedDimensions.HistoryGrowth += gasesByDimension.HistoryGrowth
		accumulatedDimensions.StateGrowthRefund += gasesByDimension.StateGrowthRefund

		t.OpcodeToDimensions[opcode] = accumulatedDimensions

		// if the opcode returns from the call stack depth, or
		// if this is an opcode immediately after a call that did not increase the stack depth
		// because it called an empty account or contract or wrong function signature,
		// call the appropriate finishX function to write the gas dimensions
		// for the call that increased the stack depth in the past
		if depth < t.depth {
			stackInfo, ok := t.callStack.Pop()
			// base case, stack is empty, do nothing
			if !ok {
				t.interrupt.Store(true)
				t.reason = fmt.Errorf("call stack is unexpectedly empty %d %d %d", pc, depth, t.depth)
				return
			}
			finishFunction := GetFinishCalcGasDimensionFunc(stackInfo.GasDimensionInfo.Op)
			if finishFunction == nil {
				t.interrupt.Store(true)
				t.reason = fmt.Errorf(
					"no finish function found for opcode %s, call stack is messed up %d",
					stackInfo.GasDimensionInfo.Op.String(),
					pc,
				)
				return
			}
			// IMPORTANT NOTE: for some reason the only reliable way to actually get the gas cost of the call
			// is to subtract gas at time of call from gas at opcode AFTER return
			// you can't trust the `gas` field on the call itself. I wonder if the gas field is an estimation
			gasUsedByCall := stackInfo.GasDimensionInfo.GasCounterAtTimeOfCall - gas
			finishGasesByDimension := finishFunction(gasUsedByCall, stackInfo.ExecutionCost, stackInfo.GasDimensionInfo)
			accumulatedDimensionsCall := t.OpcodeToDimensions[stackInfo.GasDimensionInfo.Op]

			accumulatedDimensionsCall.OneDimensionalGasCost += finishGasesByDimension.OneDimensionalGasCost
			accumulatedDimensionsCall.Computation += finishGasesByDimension.Computation
			accumulatedDimensionsCall.StateAccess += finishGasesByDimension.StateAccess
			accumulatedDimensionsCall.StateGrowth += finishGasesByDimension.StateGrowth
			accumulatedDimensionsCall.HistoryGrowth += finishGasesByDimension.HistoryGrowth
			accumulatedDimensionsCall.StateGrowthRefund += finishGasesByDimension.StateGrowthRefund
			t.OpcodeToDimensions[stackInfo.GasDimensionInfo.Op] = accumulatedDimensionsCall

			t.depth -= 1
		}
		t.updateExecutionCost(cost)
	}
}

// ############################################################################
//                        JSON OUTPUT PRODUCTION
// ############################################################################

// Error returns the VM error captured by the trace.
func (t *TxGasDimensionByOpcodeTracer) Error() error { return t.err }

// ExecutionResult groups all dimension logs emitted by the EVM
// while replaying a transaction in debug mode as well as transaction
// execution status, the amount of gas used and the return value
type TxGasDimensionByOpcodeExecutionResult struct {
	BaseExecutionResult
	Dimensions map[string]GasesByDimension `json:"dim"`
}

// produce json result for output from tracer
// this is what the end-user actually gets from the RPC endpoint
func (t *TxGasDimensionByOpcodeTracer) GetResult() (json.RawMessage, error) {
	baseExecutionResult, err := t.GetBaseExecutionResult()
	if err != nil {
		return nil, err
	}

	return json.Marshal(&TxGasDimensionByOpcodeExecutionResult{
		BaseExecutionResult: baseExecutionResult,
		Dimensions:          t.GetOpcodeDimensionSummary(),
	})
}

// produce protobuf serialized result
// for storing to file in compact format
func (t *TxGasDimensionByOpcodeTracer) GetProtobufResult() ([]byte, error) {
	baseExecutionResult, err := t.GetBaseExecutionResult()
	if err != nil {
		return nil, err
	}

	executionResult := &proto.TxGasDimensionByOpcodeExecutionResult{
		Gas:            baseExecutionResult.Gas,
		Failed:         baseExecutionResult.Failed,
		Dimensions:     make(map[uint32]*proto.GasesByDimension),
		TxHash:         baseExecutionResult.TxHash,
		BlockTimestamp: baseExecutionResult.BlockTimestamp,
		BlockNumber:    baseExecutionResult.BlockNumber.String(),
	}

	for opcode, dimensions := range t.OpcodeToDimensions {
		executionResult.Dimensions[uint32(opcode)] = &proto.GasesByDimension{
			OneDimensionalGasCost: dimensions.OneDimensionalGasCost,
			Computation:           dimensions.Computation,
			StateAccess:           dimensions.StateAccess,
			StateGrowth:           dimensions.StateGrowth,
			HistoryGrowth:         dimensions.HistoryGrowth,
			StateGrowthRefund:     dimensions.StateGrowthRefund,
		}
	}

	return protobuf.Marshal(executionResult)
}

// stringify opcodes for dimension log output
func (t *TxGasDimensionByOpcodeTracer) GetOpcodeDimensionSummary() map[string]GasesByDimension {
	summary := make(map[string]GasesByDimension)
	for opcode, dimensions := range t.OpcodeToDimensions {
		summary[opcode.String()] = dimensions
	}
	return summary
}
