package native

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
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
	StateGrowthRefund     uint64    `json:"stateGrowthRefund"`
	CallRealGas           uint64    `json:"callRealGas"`
	CallExecutionCost     uint64    `json:"callExecutionCost"`
	CallMemoryExpansion   uint64    `json:"callMemoryExpansion"`
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
	env            *tracing.VMContext
	txHash         common.Hash
	logs           []DimensionLog
	err            error
	usedGas        uint64
	callStack      CallGasDimensionStack
	depth          int
	previousOpcode vm.OpCode

	interrupt atomic.Bool // Atomic flag to signal execution interruption
	reason    error       // Textual reason for the interruption
}

// gasDimensionTracer returns a new tracer that traces gas
// usage for each opcode against the dimension of that opcode
// takes a context, and json input for configuration parameters
func NewGasDimensionTracer(
	ctx *tracers.Context,
	_ json.RawMessage,
) (*tracers.Tracer, error) {

	t := &GasDimensionTracer{
		depth: 1,
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

	if depth > t.depth {
		t.depth = depth
	}

	f := getCalcGasDimensionFunc(vm.OpCode(op))
	gasesByDimension, callStackInfo := f(pc, op, gas, cost, scope, rData, depth, err)
	// if callStackInfo is not nil then we need to take a note of the index of the
	// DimensionLog that represents this opcode and save the callStackInfo
	// to call finishX after the call has returned
	if callStackInfo != nil {
		opcodeLogIndex := len(t.logs)
		t.callStack.Push(
			CallGasDimensionStackInfo{
				gasDimensionInfo:     *callStackInfo,
				dimensionLogPosition: opcodeLogIndex,
				executionCost:        0,
			})
	}
	opcode := vm.OpCode(op)

	t.logs = append(t.logs, DimensionLog{
		Pc:                    pc,
		Op:                    opcode,
		Depth:                 depth,
		OneDimensionalGasCost: cost,
		Computation:           gasesByDimension[Computation],
		StateAccess:           gasesByDimension[StateAccess],
		StateGrowth:           gasesByDimension[StateGrowth],
		HistoryGrowth:         gasesByDimension[HistoryGrowth],
		StateGrowthRefund:     gasesByDimension[StateGrowthRefund],
		Err:                   err,
	})

	// if the opcode returns from the call stack depth, or
	// if this is an opcode immediately after a call that did not increase the stack depth
	// because it called an empty account or contract or wrong function signature,
	// call the appropriate finishX function to write the gas dimensions
	// for the call that increased the stack depth in the past
	if depth < t.depth || (t.depth == depth && wasCall(t.previousOpcode)) {
		stackInfo, ok := t.callStack.Pop()
		// base case, stack is empty, do nothing
		if !ok {
			// I am not sure if we should consider an empty stack an error
			// theoretically the top-level of the stack could be empty if
			// the transaction was not inside a call and one of the four key opcodes
			// was fired. That would halt execution of the transaction so theoretically
			// doing nothing should be the correct behavior - i thiiiiiiink
			//t.interrupt.Store(true)
			//t.reason = fmt.Errorf("call stack depth is empty, top level should now halt execution")
			return
		}
		finishFunction := getFinishCalcGasDimensionFunc(stackInfo.gasDimensionInfo.op)
		if finishFunction == nil {
			t.interrupt.Store(true)
			t.reason = fmt.Errorf("no finish function found for RETURN opcode, call stack is messed up")
			return
		}
		// IMPORTANT NOTE: for some reason the only reliable way to actually get the gas cost of the call
		// is to subtract gas at time of call from gas at opcode AFTER return
		gasUsedByCall := stackInfo.gasDimensionInfo.gasCounterAtTimeOfCall - gas
		gasesByDimension := finishFunction(gasUsedByCall, stackInfo.executionCost, stackInfo.gasDimensionInfo)
		callDimensionLog := t.logs[stackInfo.dimensionLogPosition]
		callDimensionLog.Computation = gasesByDimension[Computation]
		callDimensionLog.StateAccess = gasesByDimension[StateAccess]
		callDimensionLog.StateGrowth = gasesByDimension[StateGrowth]
		callDimensionLog.HistoryGrowth = gasesByDimension[HistoryGrowth]
		callDimensionLog.StateGrowthRefund = gasesByDimension[StateGrowthRefund]
		callDimensionLog.CallRealGas = gasUsedByCall
		callDimensionLog.CallExecutionCost = stackInfo.executionCost
		callDimensionLog.CallMemoryExpansion = stackInfo.gasDimensionInfo.memoryExpansionCost
		t.logs[stackInfo.dimensionLogPosition] = callDimensionLog
		t.depth = depth
	}

	// if we are in a call stack depth greater than 0, then we need to track the execution gas
	// of our own code so that when the call returns, we can write the gas dimensions for the call opcode itself
	// but we shouldn't do this if we are the call ourselves!
	if len(t.callStack) > 0 && callStackInfo == nil {
		t.callStack.UpdateExecutionCost(cost)
	}
	t.previousOpcode = opcode
}

/*

This code is garbage left over from when I was trying to figure out how to directly
compute state access costs. It doesn't work but I'm keeping it around for when
its time to do sload or sstore which will fire this onGasChange event.

// hook into gas changes
// used to observe Cold Storage Accesses
// We do not have access to StateDb.AddressInAccessList and StateDb.SlotInAccessList
// to check cold storage access directly. So instead what we do here is we
// assign all of the gas to the CPU dimension, which is what it would be if the
// state access was warm. If the state access is cold, then immediately after
// the onOpcode event is fired, we should observe an OnGasChange event
// that will indicate the GasChangeReason is a GasChangeCallStorageColdAccess
// and then we modify the logs in that hook.
func (t *GasDimensionTracer) OnGasChange(old, new uint64, reason tracing.GasChangeReason) {
	fmt.Println("OnGasChange", old, new, reason)
	if reason == tracing.GasChangeCallStorageColdAccess {
		lastLog := t.logs[len(t.logs)-1]
		fmt.Println("lastLog", lastLog)
		if canHaveColdStorageAccess(lastLog.Op) {
			coldCost := new - old
			fmt.Println("coldCost", coldCost)
			lastLog.StateAccess += coldCost
			lastLog.Computation -= coldCost
			// replace the last log with the corrected log
			t.logs[len(t.logs)-1] = lastLog
			fmt.Println("correctedLog", lastLog)
		} else {
			lastLog.Err = fmt.Errorf("cold storage access on opcode that is unsupported??? %s", lastLog.Op.String())
			t.interrupt.Store(true)
			t.reason = lastLog.Err
			return
		}
	}
}
*/

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
// todo: does CREATE and CREATE2 count?
func wasCall(opcode vm.OpCode) bool {
	return opcode == vm.CALL || opcode == vm.CALLCODE || opcode == vm.DELEGATECALL || opcode == vm.STATICCALL
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
	StateGrowthRefund     uint64 `json:"refund,omitempty"`
	CallRealGas           uint64 `json:"callRealGas,omitempty"`
	CallExecutionCost     uint64 `json:"callExecutionCost,omitempty"`
	CallMemoryExpansion   uint64 `json:"callMemoryExpansion,omitempty"`
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
			Err:                   trace.Err,
		}
	}
	return formatted
}
