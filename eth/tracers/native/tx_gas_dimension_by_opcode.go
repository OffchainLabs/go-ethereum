package native

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/eth/tracers/native/proto"

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/params"
	protobuf "google.golang.org/protobuf/proto"
)

// initializer for the tracer
func init() {
	tracers.DefaultDirectory.Register("txGasDimensionByOpcode", NewTxGasDimensionByOpcodeTracer, false)
}

// gasDimensionTracer struct
type TxGasDimensionByOpcodeTracer struct {
	BaseGasDimensionTracer
	OpcodeToDimensions map[vm.OpCode]GasesByDimension
}

// gasDimensionTracer returns a new tracer that traces gas
// usage for each opcode against the dimension of that opcode
// takes a context, and json input for configuration parameters
func NewTxGasDimensionByOpcodeTracer(
	_ *tracers.Context,
	_ json.RawMessage,
	chainConfig *params.ChainConfig,
) (*tracers.Tracer, error) {
	t := &TxGasDimensionByOpcodeTracer{
		BaseGasDimensionTracer: NewBaseGasDimensionTracer(chainConfig),
		OpcodeToDimensions:     make(map[vm.OpCode]GasesByDimension),
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
		// track the execution gas of all opcodes (but not the opcodes that do calls)
		t.AddToRootExecutionGasAccumulated(gasesByDimension.OneDimensionalGasCost)

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
			interrupted, _, stackInfo, finishGasesByDimension := t.callFinishFunction(pc, depth, gas)
			if interrupted {
				return
			}

			// track the execution gas of all opcodes that do calls
			t.AddToRootExecutionGasAccumulated(finishGasesByDimension.OneDimensionalGasCost)

			accumulatedDimensionsCall := t.OpcodeToDimensions[stackInfo.GasDimensionInfo.Op]

			accumulatedDimensionsCall.OneDimensionalGasCost += finishGasesByDimension.OneDimensionalGasCost
			accumulatedDimensionsCall.Computation += finishGasesByDimension.Computation
			accumulatedDimensionsCall.StateAccess += finishGasesByDimension.StateAccess
			accumulatedDimensionsCall.StateGrowth += finishGasesByDimension.StateGrowth
			accumulatedDimensionsCall.HistoryGrowth += finishGasesByDimension.HistoryGrowth
			accumulatedDimensionsCall.StateGrowthRefund += finishGasesByDimension.StateGrowthRefund
			t.OpcodeToDimensions[stackInfo.GasDimensionInfo.Op] = accumulatedDimensionsCall

			t.depth -= 1
			t.updateCallChildExecutionCost(finishGasesByDimension.OneDimensionalGasCost)
		}
		t.updateCallChildExecutionCost(gasesByDimension.OneDimensionalGasCost)
	}
	addresses, slots := t.env.StateDB.GetAccessList()
	t.updatePrevAccessList(addresses, slots)
}

// if there is an error in the evm, e.g. invalid jump,
// out of gas, max call depth exceeded, etc, this hook is called
func (t *TxGasDimensionByOpcodeTracer) OnFault(
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
		opcode := vm.OpCode(op)
		accumulatedDimensions := t.OpcodeToDimensions[opcode]
		gasAfterOpcode := gas - cost // don't double charge the cost of the opcode itself
		accumulatedDimensions.OneDimensionalGasCost += gasAfterOpcode
		accumulatedDimensions.Computation += gasAfterOpcode
		t.OpcodeToDimensions[opcode] = accumulatedDimensions
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
	Dimensions map[string]GasesByDimension `json:"dimensions"`
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

	executionResult := &proto.TxGasDimensionByOpcodeExecutionResult{
		GasUsed:                    baseExecutionResult.GasUsed,
		GasUsedL1:                  baseExecutionResult.GasUsedForL1,
		GasUsedL2:                  baseExecutionResult.GasUsedForL2,
		IntrinsicGas:               baseExecutionResult.IntrinsicGas,
		AdjustedRefund:             adjustedRefund,
		RootIsPrecompileAdjustment: rootIsPrecompileAdjustment,
		RootIsStylusAdjustment:     rootIsStylusAdjustment,
		Failed:                     failed,
		TransactionReverted:        transactionReverted,
		Dimensions:                 make(map[uint32]*proto.GasesByDimension),
		TxHash:                     baseExecutionResult.TxHash,
		BlockTimestamp:             baseExecutionResult.BlockTimestamp,
		BlockNumber:                baseExecutionResult.BlockNumber.String(),
	}

	for opcode, dimensions := range t.OpcodeToDimensions {
		executionResult.Dimensions[uint32(opcode)] = &proto.GasesByDimension{
			OneDimensionalGasCost: dimensions.OneDimensionalGasCost,
			Computation:           dimensions.Computation,
			StateAccess:           dimensions.StateAccess,
			StateGrowth:           dimensions.StateGrowth,
			HistoryGrowth:         dimensions.HistoryGrowth,
			StateGrowthRefund:     dimensions.StateGrowthRefund,
			ChildExecutionCost:    dimensions.ChildExecutionCost,
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
