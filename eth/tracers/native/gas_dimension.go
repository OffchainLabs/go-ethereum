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
	env     *tracing.VMContext
	txHash  common.Hash
	logs    []DimensionLog
	err     error
	usedGas uint64

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

	t := &GasDimensionTracer{}

	return &tracers.Tracer{
		Hooks: &tracing.Hooks{
			OnOpcode:    t.OnOpcode,
			OnTxStart:   t.OnTxStart,
			OnTxEnd:     t.OnTxEnd,
			OnGasChange: t.OnGasChange,
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

	f := getCalcGasDimensionFunc(vm.OpCode(op))
	gasesByDimension := f(pc, op, gas, cost, scope, rData, depth, err, t.env.StateDB)

	t.logs = append(t.logs, DimensionLog{
		Pc:                    pc,
		Op:                    vm.OpCode(op),
		Depth:                 depth,
		OneDimensionalGasCost: cost,
		Computation:           gasesByDimension[Computation],
		StateAccess:           gasesByDimension[StateAccess],
		StateGrowth:           gasesByDimension[StateGrowth],
		HistoryGrowth:         gasesByDimension[HistoryGrowth],
		StateGrowthRefund:     gasesByDimension[StateGrowthRefund],
		Err:                   err,
	})
}

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
			Err:                   trace.Err,
		}
	}
	return formatted
}
