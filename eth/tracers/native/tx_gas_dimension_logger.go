package native

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/params"
)

// initializer for the tracer
func init() {
	tracers.DefaultDirectory.Register("txGasDimensionLogger", NewTxGasDimensionLogger, false)
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
	ChildExecutionCost    uint64    `json:"callExecutionCost"`
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

// TxGasDimensionLogger struct
type TxGasDimensionLogger struct {
	BaseGasDimensionTracer
	logs []DimensionLog
}

// gasDimensionTracer returns a new tracer that traces gas
// usage for each opcode against the dimension of that opcode
// takes a context, and json input for configuration parameters
func NewTxGasDimensionLogger(
	_ *tracers.Context,
	_ json.RawMessage,
	chainConfig *params.ChainConfig,
) (*tracers.Tracer, error) {

	t := &TxGasDimensionLogger{
		BaseGasDimensionTracer: NewBaseGasDimensionTracer(chainConfig),
		logs:                   make([]DimensionLog, 0),
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
func (t *TxGasDimensionLogger) OnOpcode(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) {
	interrupted, gasesByDimension, callStackInfo, opcode := t.onOpcodeStart(pc, op, gas, cost, scope, rData, depth, err)
	// if an error occured, it was stored in the tracer's reason field
	// and we should return immediately
	if interrupted {
		return
	}

	t.logs = append(t.logs, DimensionLog{
		Pc:                    pc,
		Op:                    opcode,
		Depth:                 depth,
		OneDimensionalGasCost: gasesByDimension.OneDimensionalGasCost,
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
	if WasCallOrCreate(opcode) && err == nil {
		t.handleCallStackPush(callStackInfo)
	} else {
		// track the execution gas of all opcodes (but not the opcodes that do calls)
		t.AddToExecutionGasAccumulated(gasesByDimension.OneDimensionalGasCost)
		if depth < t.depth {
			interrupted, gasUsedByCall, stackInfo, finishGasesByDimension := t.callFinishFunction(pc, depth, gas)
			if interrupted {
				return
			}
			// track the execution gas of all opcodes that do calls
			t.AddToExecutionGasAccumulated(finishGasesByDimension.OneDimensionalGasCost)
			callDimensionLog := t.logs[stackInfo.DimensionLogPosition]
			callDimensionLog.OneDimensionalGasCost = finishGasesByDimension.OneDimensionalGasCost
			callDimensionLog.Computation = finishGasesByDimension.Computation
			callDimensionLog.StateAccess = finishGasesByDimension.StateAccess
			callDimensionLog.StateGrowth = finishGasesByDimension.StateGrowth
			callDimensionLog.HistoryGrowth = finishGasesByDimension.HistoryGrowth
			callDimensionLog.StateGrowthRefund = finishGasesByDimension.StateGrowthRefund
			callDimensionLog.CallRealGas = gasUsedByCall
			callDimensionLog.ChildExecutionCost = finishGasesByDimension.ChildExecutionCost
			callDimensionLog.CallMemoryExpansion = stackInfo.GasDimensionInfo.MemoryExpansionCost
			callDimensionLog.CreateInitCodeCost = stackInfo.GasDimensionInfo.InitCodeCost
			callDimensionLog.Create2HashCost = stackInfo.GasDimensionInfo.HashCost
			t.logs[stackInfo.DimensionLogPosition] = callDimensionLog

			t.depth -= 1
		}

		t.updateExecutionCost(gasesByDimension.OneDimensionalGasCost)
	}
	addresses, slots := t.env.StateDB.GetAccessList()
	t.updatePrevAccessList(addresses, slots)
}

// save the relevant information for the call stack when a call or create
// is made that increases the stack depth
func (t *TxGasDimensionLogger) handleCallStackPush(callStackInfo *CallGasDimensionInfo) {
	opcodeLogIndex := len(t.logs) - 1 // minus 1 because we've already appended the log
	t.callStack.Push(
		CallGasDimensionStackInfo{
			GasDimensionInfo:     *callStackInfo,
			DimensionLogPosition: opcodeLogIndex,
			ExecutionCost:        0,
		})
	t.depth += 1
}

// ############################################################################
//                        JSON OUTPUT PRODUCTION
// ############################################################################

// DimensionLogs returns the captured log entries.
func (t *TxGasDimensionLogger) DimensionLogs() []DimensionLog { return t.logs }

// ExecutionResult groups all dimension logs emitted by the EVM
// while replaying a transaction in debug mode as well as transaction
// execution status, the amount of gas used and the return value
type ExecutionResult struct {
	BaseExecutionResult
	DimensionLogs []DimensionLogRes `json:"dim"`
}

// produce json result for output from tracer
// this is what the end-user actually gets from the RPC endpoint
func (t *TxGasDimensionLogger) GetResult() (json.RawMessage, error) {
	baseResult, err := t.GetBaseExecutionResult()
	if err != nil {
		return nil, err
	}
	return json.Marshal(&ExecutionResult{
		BaseExecutionResult: baseResult,
		DimensionLogs:       formatLogs(t.DimensionLogs()),
	})
}

// formatted logs for json output
// as opposed to DimensionLog which has real non-string types
// keep json field names as short as possible to save on bandwidth bytes
type DimensionLogRes struct {
	Pc                    uint64 `json:"pc"`
	Op                    string `json:"op"`
	Depth                 int    `json:"depth"`
	OneDimensionalGasCost uint64 `json:"cost"`
	Computation           uint64 `json:"cpu,omitempty"`
	StateAccess           uint64 `json:"rw,omitempty"`
	StateGrowth           uint64 `json:"growth,omitempty"`
	HistoryGrowth         uint64 `json:"history,omitempty"`
	StateGrowthRefund     int64  `json:"refund,omitempty"`
	CallRealGas           uint64 `json:"callRealGas,omitempty"`
	ChildExecutionCost    uint64 `json:"childExecutionCost,omitempty"`
	CallMemoryExpansion   uint64 `json:"callMemoryExpansion,omitempty"`
	CreateInitCodeCost    uint64 `json:"createInitCodeCost,omitempty"`
	Create2HashCost       uint64 `json:"create2HashCost,omitempty"`
	Err                   error  `json:"err,omitempty"`
}

// DebugString returns a string
// representation of the DimensionLogRes for debugging
func (d *DimensionLogRes) DebugString() string {
	return fmt.Sprintf(
		"{Pc: %d, Op: %s, Depth: %d, OneDimensionalGasCost: %d, Computation: %d, StateAccess: %d, StateGrowth: %d, HistoryGrowth: %d, StateGrowthRefund: %d, CallRealGas: %d, CallExecutionCost: %d, CallMemoryExpansion: %d, CreateInitCodeCost: %d, Create2HashCost: %d, Err: %v}",
		d.Pc,
		d.Op,
		d.Depth,
		d.OneDimensionalGasCost,
		d.Computation,
		d.StateAccess,
		d.StateGrowth,
		d.HistoryGrowth,
		d.StateGrowthRefund,
		d.CallRealGas,
		d.ChildExecutionCost,
		d.CallMemoryExpansion,
		d.CreateInitCodeCost,
		d.Create2HashCost,
		d.Err,
	)
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
			ChildExecutionCost:    trace.ChildExecutionCost,
			CallMemoryExpansion:   trace.CallMemoryExpansion,
			CreateInitCodeCost:    trace.CreateInitCodeCost,
			Create2HashCost:       trace.Create2HashCost,
			Err:                   trace.Err,
		}
	}
	return formatted
}
