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

// TracerErrorWithDimLogs is a custom error type that includes dimension logs
// in its string representation for debugging purposes
type TracerErrorWithDimLogs struct {
	BaseError error
	Logs      []DimensionLog
}

// Error implements the error interface
func (e *TracerErrorWithDimLogs) Error() string {
	if e.BaseError == nil {
		return "Dimension tracer error with logs"
	}

	logsStr := formatLogsDebugString(e.Logs)
	return fmt.Sprintf("%s%s", e.BaseError.Error(), logsStr)
}

// Unwrap returns the underlying error for error wrapping
func (e *TracerErrorWithDimLogs) Unwrap() error {
	return e.BaseError
}

// TxGasDimensionLogger struct
type TxGasDimensionLogger struct {
	*BaseGasDimensionTracer
	logs []DimensionLog
}

// gasDimensionTracer returns a new tracer that traces gas
// usage for each opcode against the dimension of that opcode
// takes a context, and json input for configuration parameters
func NewTxGasDimensionLogger(
	_ *tracers.Context,
	cfg json.RawMessage,
	chainConfig *params.ChainConfig,
) (*tracers.Tracer, error) {
	baseGasDimensionTracer, err := NewBaseGasDimensionTracer(cfg, chainConfig)
	if err != nil {
		return nil, err
	}
	t := &TxGasDimensionLogger{
		BaseGasDimensionTracer: baseGasDimensionTracer,
		logs:                   make([]DimensionLog, 0),
	}

	return &tracers.Tracer{
		Hooks: &tracing.Hooks{
			OnOpcode:  t.OnOpcode,
			OnTxStart: t.OnTxStart,
			OnFault:   t.OnFault,
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
	// If an error occurred in the tracer itself (not in the transaction),
	// it was stored in the tracer's reason field (t.reason)
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
		ChildExecutionCost:    gasesByDimension.ChildExecutionCost,
		// the following are considered unknown at this point in the tracer lifecycle
		// and are only filled in after the finish function is called
		CallRealGas:         0,
		CallMemoryExpansion: 0,
		CreateInitCodeCost:  0,
		Create2HashCost:     0,
		// VM err is stored in each log as the Err field
		Err: err,
	})

	// if callStackInfo is not nil then we need to take a note of the index of the
	// DimensionLog that represents this opcode and save the callStackInfo
	// to call finishX after the call has returned
	if WasCallOrCreate(opcode) && err == nil {
		t.handleCallStackPush(callStackInfo)
	} else {
		// track the execution gas of all opcodes (but not the opcodes that do calls)
		t.AddToRootExecutionGasAccumulated(gasesByDimension.OneDimensionalGasCost)
		if depth < t.depth {
			interrupted, totalGasUsedByCall, stackInfo, finishGasesByDimension := t.callFinishFunction(pc, depth, gas)
			if interrupted {
				return
			}
			// track the execution gas of all opcodes that do calls
			if depth == 1 {
				t.AddToRootExecutionGasAccumulated(totalGasUsedByCall)
			}
			callDimensionLog := t.logs[stackInfo.DimensionLogPosition]
			callDimensionLog.OneDimensionalGasCost = finishGasesByDimension.OneDimensionalGasCost
			callDimensionLog.Computation = finishGasesByDimension.Computation
			callDimensionLog.StateAccess = finishGasesByDimension.StateAccess
			callDimensionLog.StateGrowth = finishGasesByDimension.StateGrowth
			callDimensionLog.HistoryGrowth = finishGasesByDimension.HistoryGrowth
			callDimensionLog.StateGrowthRefund = finishGasesByDimension.StateGrowthRefund
			callDimensionLog.CallRealGas = totalGasUsedByCall
			callDimensionLog.ChildExecutionCost = finishGasesByDimension.ChildExecutionCost
			callDimensionLog.CallMemoryExpansion = stackInfo.GasDimensionInfo.MemoryExpansionCost
			callDimensionLog.CreateInitCodeCost = stackInfo.GasDimensionInfo.InitCodeCost
			callDimensionLog.Create2HashCost = stackInfo.GasDimensionInfo.HashCost
			t.logs[stackInfo.DimensionLogPosition] = callDimensionLog

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
func (t *TxGasDimensionLogger) OnFault(
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
	// if there was an error, go backwards through the logs until we find the log
	// with the affected opcode. Then since errors consume all gas, add the gas
	// to the one dimensional and computation gas cost of the affected opcode
	// note we only do this for the last depth in the call stack
	// because if an error happens inside a call, the gas for the call opcode
	//  will capture the excess gas consumed by the error/revert
	if depth == 1 {
		for i := len(t.logs) - 1; i >= 0; i-- {
			if t.logs[i].Pc == pc {
				gasLeft := gas - cost
				t.logs[i].OneDimensionalGasCost += gasLeft
				t.logs[i].Computation += gasLeft
			}
		}
	}
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
	baseResult, tracerError := t.GetBaseExecutionResult()
	// If there's a tracer error and debugging is on,
	// wrap it with dimension logs for additional help
	if tracerError != nil {
		if t.debug {
			return nil, &TracerErrorWithDimLogs{
				BaseError: tracerError,
				Logs:      t.DimensionLogs(),
			}
		} else {
			return nil, tracerError
		}
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

// copyDimLogToRes copies fields of a DimensionLog to a DimensionLogRes
func copyDimLogToRes(dimLog DimensionLog) DimensionLogRes {
	return DimensionLogRes{
		Pc:                    dimLog.Pc,
		Op:                    dimLog.Op.String(),
		Depth:                 dimLog.Depth,
		OneDimensionalGasCost: dimLog.OneDimensionalGasCost,
		Computation:           dimLog.Computation,
		StateAccess:           dimLog.StateAccess,
		StateGrowth:           dimLog.StateGrowth,
		HistoryGrowth:         dimLog.HistoryGrowth,
		StateGrowthRefund:     dimLog.StateGrowthRefund,
		CallRealGas:           dimLog.CallRealGas,
		ChildExecutionCost:    dimLog.ChildExecutionCost,
		CallMemoryExpansion:   dimLog.CallMemoryExpansion,
		CreateInitCodeCost:    dimLog.CreateInitCodeCost,
		Create2HashCost:       dimLog.Create2HashCost,
		Err:                   dimLog.Err,
	}
}

// formatLogs formats EVM returned structured logs for json output
func formatLogs(logs []DimensionLog) []DimensionLogRes {
	formatted := make([]DimensionLogRes, len(logs))
	for index, trace := range logs {
		formatted[index] = copyDimLogToRes(trace)
	}
	return formatted
}

func formatLogsDebugString(logs []DimensionLog) string {
	ret := "\n"
	for _, trace := range logs {
		dimLogRes := copyDimLogToRes(trace)
		ret += dimLogRes.DebugString() + "\n"
	}
	return ret
}
