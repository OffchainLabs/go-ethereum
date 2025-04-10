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
	tracers.DefaultDirectory.Register("gasDimension", NewGasDimensionTracer, false)
}

// DimensionLog emitted to the EVM each cycle and lists information about each opcode
// and its gas dimensions prior to the execution of the statement.
type DimensionLog struct {
	Pc                    uint64    `json:"pc"`
	Op                    vm.OpCode `json:"op"`
	Depth                 int       `json:"depth"`
	OneDimensionalGasCost uint64    `json:"oneDimensionalGasCost"`
	Computation           uint64    `json:"computation"`
	StateAccess           uint64    `json:"stateAccess"`
	StateGrowth           uint64    `json:"stateGrowth"`
	HistoryGrowth         uint64    `json:"historyGrowth"`
	StateGrowthRefund     int64     `json:"stateGrowthRefund"`
	CallRealGas           uint64    `json:"callRealGas"`
	CallExecutionCost     uint64    `json:"callExecutionCost"`
	CallMemoryExpansion   uint64    `json:"callMemoryExpansion"`
	CreateInitCodeCost    uint64    `json:"createInitCodeCost"`
	Create2HashCost       uint64    `json:"create2HashCost"`
	Err                   error     `json:"-"`
}

// ErrorString formats the log's error as a string.
func (d *DimensionLog) ErrorString() string {
	if d.Err != nil {
		return d.Err.Error()
	}
	return ""
}

// gasDimensionTracer struct
type GasDimensionTracer struct {
	env               *tracing.VMContext
	txHash            common.Hash
	logs              []DimensionLog
	err               error
	usedGas           uint64
	callStack         CallGasDimensionStack
	depth             int
	refundAccumulated uint64

	interrupt atomic.Bool // Atomic flag to signal execution interruption
	reason    error       // Textual reason for the interruption
}

// gasDimensionTracer returns a new tracer that traces gas
// usage for each opcode against the dimension of that opcode
// takes a context, and json input for configuration parameters
func NewGasDimensionTracer(
	ctx *tracers.Context,
	_ json.RawMessage,
	_ *params.ChainConfig,
) (*tracers.Tracer, error) {

	t := &GasDimensionTracer{
		depth:             1,
		refundAccumulated: 0,
	}

	return &tracers.Tracer{
		Hooks: &tracing.Hooks{
			OnOpcode:  t.OnOpcode,
			OnTxStart: t.OnTxStart,
			OnTxEnd:   t.OnTxEnd,
			//OnGasChange: t.OnGasChange,
		},
		GetResult: t.GetResult,
		Stop:      t.Stop,
	}, nil
}

// ############################################################################
//                                    HOOKS
// ############################################################################

// hook into each opcode execution
func (t *GasDimensionTracer) OnOpcode(
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

	t.logs = append(t.logs, DimensionLog{
		Pc:                    pc,
		Op:                    opcode,
		Depth:                 depth,
		OneDimensionalGasCost: cost,
		Computation:           gasesByDimension.Computation,
		StateAccess:           gasesByDimension.StateAccess,
		StateGrowth:           gasesByDimension.StateGrowth,
		HistoryGrowth:         gasesByDimension.HistoryGrowth,
		StateGrowthRefund:     gasesByDimension.StateGrowthRefund,
		Err:                   err,
	})

	// if callStackInfo is not nil then we need to take a note of the index of the
	// DimensionLog that represents this opcode and save the callStackInfo
	// to call finishX after the call has returned
	if wasCallOrCreate(opcode) {
		opcodeLogIndex := len(t.logs) - 1 // minus 1 because we've already appended the log
		t.callStack.Push(
			CallGasDimensionStackInfo{
				gasDimensionInfo:     *callStackInfo,
				dimensionLogPosition: opcodeLogIndex,
				executionCost:        0,
			})
		t.depth += 1
	} else {
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
			gasesByDimension := finishFunction(gasUsedByCall, stackInfo.executionCost, stackInfo.gasDimensionInfo)
			callDimensionLog := t.logs[stackInfo.dimensionLogPosition]
			callDimensionLog.Computation = gasesByDimension.Computation
			callDimensionLog.StateAccess = gasesByDimension.StateAccess
			callDimensionLog.StateGrowth = gasesByDimension.StateGrowth
			callDimensionLog.HistoryGrowth = gasesByDimension.HistoryGrowth
			callDimensionLog.StateGrowthRefund = gasesByDimension.StateGrowthRefund
			callDimensionLog.CallRealGas = gasUsedByCall
			callDimensionLog.CallExecutionCost = stackInfo.executionCost
			callDimensionLog.CallMemoryExpansion = stackInfo.gasDimensionInfo.memoryExpansionCost
			callDimensionLog.CreateInitCodeCost = stackInfo.gasDimensionInfo.initCodeCost
			callDimensionLog.Create2HashCost = stackInfo.gasDimensionInfo.hashCost
			t.logs[stackInfo.dimensionLogPosition] = callDimensionLog
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

func (t *GasDimensionTracer) OnTxStart(env *tracing.VMContext, tx *types.Transaction, from common.Address) {
	t.env = env
}

func (t *GasDimensionTracer) OnTxEnd(receipt *types.Receipt, err error) {
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
func (t *GasDimensionTracer) Stop(err error) {
	t.reason = err
	t.interrupt.Store(true)
}

// ############################################################################
//                        JSON OUTPUT PRODUCTION
// ############################################################################

// wasCall returns true if the opcode is a type of opcode that makes calls increasing the stack depth
func wasCallOrCreate(opcode vm.OpCode) bool {
	return opcode == vm.CALL || opcode == vm.CALLCODE || opcode == vm.DELEGATECALL || opcode == vm.STATICCALL || opcode == vm.CREATE || opcode == vm.CREATE2
}

// DimensionLogs returns the captured log entries.
func (t *GasDimensionTracer) DimensionLogs() []DimensionLog { return t.logs }

// Error returns the VM error captured by the trace.
func (t *GasDimensionTracer) Error() error { return t.err }

// ExecutionResult groups all dimension logs emitted by the EVM
// while replaying a transaction in debug mode as well as transaction
// execution status, the amount of gas used and the return value
type ExecutionResult struct {
	Gas           uint64            `json:"gas"`
	Failed        bool              `json:"failed"`
	DimensionLogs []DimensionLogRes `json:"dimensionLogs"`
	TxHash        string            `json:"txHash"`
	BlockTimetamp uint64            `json:"blockTimestamp"`
	BlockNumber   *big.Int          `json:"blockNumber"`
}

// produce json result for output from tracer
// this is what the end-user actually gets from the RPC endpoint
func (t *GasDimensionTracer) GetResult() (json.RawMessage, error) {
	// Tracing aborted
	if t.reason != nil {
		return nil, t.reason
	}
	failed := t.err != nil

	return json.Marshal(&ExecutionResult{
		Gas:           t.usedGas,
		Failed:        failed,
		DimensionLogs: formatLogs(t.DimensionLogs()),
		TxHash:        t.txHash.Hex(),
		BlockTimetamp: t.env.Time,
		BlockNumber:   t.env.BlockNumber,
	})
}

// formatted logs for json output
// as opposed to DimensionLog which has real non-string types
// keep json field names as short as possible to save on bandwidth bytes
type DimensionLogRes struct {
	Pc                    uint64 `json:"pc"`
	Op                    string `json:"op"`
	Depth                 int    `json:"depth"`
	OneDimensionalGasCost uint64 `json:"gasCost"`
	Computation           uint64 `json:"cpu"`
	StateAccess           uint64 `json:"access,omitempty"`
	StateGrowth           uint64 `json:"growth,omitempty"`
	HistoryGrowth         uint64 `json:"hist,omitempty"`
	StateGrowthRefund     int64  `json:"refund,omitempty"`
	CallRealGas           uint64 `json:"callRealGas,omitempty"`
	CallExecutionCost     uint64 `json:"callExecutionCost,omitempty"`
	CallMemoryExpansion   uint64 `json:"callMemoryExpansion,omitempty"`
	CreateInitCodeCost    uint64 `json:"createInitCodeCost,omitempty"`
	Create2HashCost       uint64 `json:"create2HashCost,omitempty"`
	Err                   error  `json:"error,omitempty"`
}

// formatLogs formats EVM returned structured logs for json output
func formatLogs(logs []DimensionLog) []DimensionLogRes {
	formatted := make([]DimensionLogRes, len(logs))
	for index, trace := range logs {
		formatted[index] = DimensionLogRes{
			Pc:                    trace.Pc,
			Op:                    trace.Op.String(),
			Depth:                 trace.Depth,
			OneDimensionalGasCost: trace.OneDimensionalGasCost,
			Computation:           trace.Computation,
			StateAccess:           trace.StateAccess,
			StateGrowth:           trace.StateGrowth,
			HistoryGrowth:         trace.HistoryGrowth,
			StateGrowthRefund:     trace.StateGrowthRefund,
			CallRealGas:           trace.CallRealGas,
			CallExecutionCost:     trace.CallExecutionCost,
			CallMemoryExpansion:   trace.CallMemoryExpansion,
			CreateInitCodeCost:    trace.CreateInitCodeCost,
			Create2HashCost:       trace.Create2HashCost,
			Err:                   trace.Err,
		}
	}
	return formatted
}
