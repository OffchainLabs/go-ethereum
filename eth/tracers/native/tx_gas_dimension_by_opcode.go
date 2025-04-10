package native

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/params"
)

// initializer for the tracer
func init() {
	tracers.DefaultDirectory.Register("txGasDimensionByOpcode", NewTxGasDimensionByOpcodeLogger, false)
}

// gasDimensionTracer struct
type TxGasDimensionByOpcodeTracer struct {
	env                *tracing.VMContext
	txHash             common.Hash
	opcodeToDimensions map[vm.OpCode]GasesByDimension
	err                error
	usedGas            uint64
	callStack          CallGasDimensionStack
	depth              int
	refundAccumulated  uint64

	interrupt atomic.Bool // Atomic flag to signal execution interruption
	reason    error       // Textual reason for the interruption
}

// gasDimensionTracer returns a new tracer that traces gas
// usage for each opcode against the dimension of that opcode
// takes a context, and json input for configuration parameters
func NewTxGasDimensionByOpcodeLogger(
	ctx *tracers.Context,
	_ json.RawMessage,
	_ *params.ChainConfig,
) (*tracers.Tracer, error) {

	t := &TxGasDimensionByOpcodeTracer{
		depth:              1,
		refundAccumulated:  0,
		opcodeToDimensions: make(map[vm.OpCode]GasesByDimension),
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
	if t.interrupt.Load() {
		return
	}
	if depth != t.depth && depth != t.depth-1 {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf(
			"expected depth fell out of sync with actual depth: %d %d != %d, callStack: %v",
			pc,
			t.depth,
			depth,
			t.callStack,
		)
		return
	}
	if t.depth != len(t.callStack)+1 {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf(
			"depth fell out of sync with callStack: %d %d != %d, callStack: %v",
			pc,
			t.depth,
			len(t.callStack),
			t.callStack,
		)
	}

	// get the gas dimension function
	// if it's not a call, directly calculate the gas dimensions for the opcode
	f := getCalcGasDimensionFunc(vm.OpCode(op))
	gasesByDimension, callStackInfo, err := f(t, pc, op, gas, cost, scope, rData, depth, err)
	if err != nil {
		t.interrupt.Store(true)
		t.reason = err
		return
	}
	opcode := vm.OpCode(op)

	if wasCallOrCreate(opcode) && callStackInfo == nil || !wasCallOrCreate(opcode) && callStackInfo != nil {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf(
			"logic bug, calls/creates should always be accompanied by callStackInfo and non-calls should not have callStackInfo %d %s %v",
			pc,
			opcode.String(),
			callStackInfo,
		)
		return
	}

	// if callStackInfo is not nil then we need to take a note of the index of the
	// DimensionLog that represents this opcode and save the callStackInfo
	// to call finishX after the call has returned
	if wasCallOrCreate(opcode) {
		t.callStack.Push(
			CallGasDimensionStackInfo{
				gasDimensionInfo:     *callStackInfo,
				dimensionLogPosition: 0, //unused in this tracer
				executionCost:        0,
			})
		t.depth += 1
	} else {

		// update the aggregrate map for this opcode
		accumulatedDimensions := t.opcodeToDimensions[opcode]

		accumulatedDimensions.OneDimensionalGasCost += gasesByDimension.OneDimensionalGasCost
		accumulatedDimensions.Computation += gasesByDimension.Computation
		accumulatedDimensions.StateAccess += gasesByDimension.StateAccess
		accumulatedDimensions.StateGrowth += gasesByDimension.StateGrowth
		accumulatedDimensions.HistoryGrowth += gasesByDimension.HistoryGrowth
		accumulatedDimensions.StateGrowthRefund += gasesByDimension.StateGrowthRefund

		t.opcodeToDimensions[opcode] = accumulatedDimensions

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
			finishFunction := getFinishCalcGasDimensionFunc(stackInfo.gasDimensionInfo.op)
			if finishFunction == nil {
				t.interrupt.Store(true)
				t.reason = fmt.Errorf(
					"no finish function found for opcode %s, call stack is messed up %d",
					stackInfo.gasDimensionInfo.op.String(),
					pc,
				)
				return
			}
			// IMPORTANT NOTE: for some reason the only reliable way to actually get the gas cost of the call
			// is to subtract gas at time of call from gas at opcode AFTER return
			// you can't trust the `gas` field on the call itself. I wonder if the gas field is an estimation
			gasUsedByCall := stackInfo.gasDimensionInfo.gasCounterAtTimeOfCall - gas
			gasesByDimensionCall := finishFunction(gasUsedByCall, stackInfo.executionCost, stackInfo.gasDimensionInfo)
			accumulatedDimensionsCall := t.opcodeToDimensions[stackInfo.gasDimensionInfo.op]

			accumulatedDimensionsCall.OneDimensionalGasCost += gasesByDimensionCall.OneDimensionalGasCost
			accumulatedDimensionsCall.Computation += gasesByDimensionCall.Computation
			accumulatedDimensionsCall.StateAccess += gasesByDimensionCall.StateAccess
			accumulatedDimensionsCall.StateGrowth += gasesByDimensionCall.StateGrowth
			accumulatedDimensionsCall.HistoryGrowth += gasesByDimensionCall.HistoryGrowth
			accumulatedDimensionsCall.StateGrowthRefund += gasesByDimensionCall.StateGrowthRefund

			t.opcodeToDimensions[stackInfo.gasDimensionInfo.op] = accumulatedDimensionsCall
			t.depth -= 1
		}

		// if we are in a call stack depth greater than 0,
		// then we need to track the execution gas
		// of our own code so that when the call returns,
		// we can write the gas dimensions for the call opcode itself
		if len(t.callStack) > 0 {
			t.callStack.UpdateExecutionCost(cost)
		}
	}
}

func (t *TxGasDimensionByOpcodeTracer) OnTxStart(env *tracing.VMContext, tx *types.Transaction, from common.Address) {
	t.env = env
}

func (t *TxGasDimensionByOpcodeTracer) OnTxEnd(receipt *types.Receipt, err error) {
	if err != nil {
		// Don't override vm error
		if t.err == nil {
			t.err = err
		}
		return
	}
	t.usedGas = receipt.GasUsed
	t.txHash = receipt.TxHash
}

// signal the tracer to stop tracing, e.g. on timeout
func (t *TxGasDimensionByOpcodeTracer) Stop(err error) {
	t.reason = err
	t.interrupt.Store(true)
}

// ############################################################################
//                                HELPERS
// ############################################################################

func (t *TxGasDimensionByOpcodeTracer) GetOpRefund() uint64 {
	return t.env.StateDB.GetRefund()
}

func (t *TxGasDimensionByOpcodeTracer) GetRefundAccumulated() uint64 {
	return t.refundAccumulated
}

func (t *TxGasDimensionByOpcodeTracer) SetRefundAccumulated(refund uint64) {
	t.refundAccumulated = refund
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
	Gas           uint64                      `json:"gas"`
	Failed        bool                        `json:"failed"`
	Dimensions    map[string]GasesByDimension `json:"dimensions"`
	TxHash        string                      `json:"txHash"`
	BlockTimetamp uint64                      `json:"blockTimestamp"`
	BlockNumber   *big.Int                    `json:"blockNumber"`
}

// produce json result for output from tracer
// this is what the end-user actually gets from the RPC endpoint
func (t *TxGasDimensionByOpcodeTracer) GetResult() (json.RawMessage, error) {
	// Tracing aborted
	if t.reason != nil {
		return nil, t.reason
	}
	failed := t.err != nil

	return json.Marshal(&TxGasDimensionByOpcodeExecutionResult{
		Gas:           t.usedGas,
		Failed:        failed,
		Dimensions:    t.GetOpcodeDimensionSummary(),
		TxHash:        t.txHash.Hex(),
		BlockTimetamp: t.env.Time,
		BlockNumber:   t.env.BlockNumber,
	})
}

// stringify opcodes for dimension log output
func (t *TxGasDimensionByOpcodeTracer) GetOpcodeDimensionSummary() map[string]GasesByDimension {
	summary := make(map[string]GasesByDimension)
	for opcode, dimensions := range t.opcodeToDimensions {
		summary[opcode.String()] = dimensions
	}
	return summary
}
