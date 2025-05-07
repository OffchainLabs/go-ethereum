package native

import (
	"fmt"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// BaseGasDimensionTracer contains the shared functionality between different gas dimension tracers
type BaseGasDimensionTracer struct {
	// hold on to the context
	env *tracing.VMContext
	// the hash of the transactionh
	txHash common.Hash
	// the amount of gas used in the transaction
	gasUsed uint64
	// the amount of gas used for L1 in the transaction
	gasUsedForL1 uint64
	// the amount of gas used for L2 in the transaction
	gasUsedForL2 uint64
	// the intrinsic gas of the transaction, the static cost + calldata bytes cost
	intrinsicGas uint64
	// the call stack for the transaction
	callStack CallGasDimensionStack
	// the depth at the current step of execution of the call stack
	depth int
	// maintain an access list tracer to check previous access list statuses.
	prevAccessListAddresses map[common.Address]int
	prevAccessListSlots     []map[common.Hash]struct{}
	// the amount of refund accumulated at the current step of execution
	refundAccumulated uint64
	// in order to calculate the refund adjusted, we need to know the total execution gas
	// of just the opcodes of the transaction with no refunds
	executionGasAccumulated uint64
	// the amount of refund allowed at the end of the transaction, adjusted by EIP-3529
	refundAdjusted uint64
	// whether the transaction had an error, like out of gas
	err error
	// whether the tracer itself was interrupted
	interrupt atomic.Bool
	// reason or error for the interruption in the tracer itself (as opposed to the transaction)
	reason error
	// cached chain config for use in hooks
	chainConfig *params.ChainConfig
}

func NewBaseGasDimensionTracer(chainConfig *params.ChainConfig) BaseGasDimensionTracer {
	return BaseGasDimensionTracer{
		chainConfig:             chainConfig,
		depth:                   1,
		refundAccumulated:       0,
		prevAccessListAddresses: map[common.Address]int{},
		prevAccessListSlots:     []map[common.Hash]struct{}{},
	}
}

// OnOpcode handles the shared opcode execution logic
// since this is so sensitive, we supply helper methods for
// different parts of the logic but expect child tracers
// to implement their own specific logic in their own
// OnOpcode method
func (t *BaseGasDimensionTracer) OnOpcode(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) error {
	return fmt.Errorf("OnOpcode not implemented")
}

// onOpcodeStart is a helper function that
// implements the shared logic for the start of an OnOpcode function
// between all of the gas dimension tracers
func (t *BaseGasDimensionTracer) onOpcodeStart(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) (
	interrupted bool,
	gasesByDimension GasesByDimension,
	callStackInfo *CallGasDimensionInfo,
	opcode vm.OpCode,
) {
	if t.interrupt.Load() {
		return true, GasesByDimension{}, nil, vm.OpCode(op)
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
		return true, GasesByDimension{}, nil, vm.OpCode(op)
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
		return true, GasesByDimension{}, nil, vm.OpCode(op)
	}

	// get the gas dimension function
	// if it's not a call, directly calculate the gas dimensions for the opcode
	f := GetCalcGasDimensionFunc(vm.OpCode(op))
	var fErr error = nil
	gasesByDimension, callStackInfo, fErr = f(t, pc, op, gas, cost, scope, rData, depth, err)
	if fErr != nil {
		t.interrupt.Store(true)
		t.reason = fErr
		return true, GasesByDimension{}, nil, vm.OpCode(op)
	}
	opcode = vm.OpCode(op)

	if WasCallOrCreate(opcode) && callStackInfo == nil && err == nil || !WasCallOrCreate(opcode) && callStackInfo != nil {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf(
			"logic bug, calls/creates should always be accompanied by callStackInfo and non-calls should not have callStackInfo %d %s %v",
			pc,
			opcode.String(),
			callStackInfo,
		)
		return true, GasesByDimension{}, nil, vm.OpCode(op)
	}
	return false, gasesByDimension, callStackInfo, opcode
}

// handleCallStackPush is a helper function that
// implements the shared logic for the push of a call stack
// between all of the gas dimension tracers
// some of the tracers will need to implement their own
// because DimensionLogPosition is not used in all tracers
func (t *BaseGasDimensionTracer) handleCallStackPush(callStackInfo *CallGasDimensionInfo) {
	t.callStack.Push(
		CallGasDimensionStackInfo{
			GasDimensionInfo:     *callStackInfo,
			DimensionLogPosition: 0, // unused in opcode tracer, other tracers should implement their own.
			ExecutionCost:        0,
		})
	t.depth += 1
}

// if the opcode returns from the call stack depth, or
// if this is an opcode immediately after a call that did not increase the stack depth
// because it called an empty account or contract or wrong function signature,
// call the appropriate finishX function to write the gas dimensions
// for the call that increased the stack depth in the past
func (t *BaseGasDimensionTracer) callFinishFunction(
	pc uint64,
	depth int,
	gas uint64,
) (
	interrupted bool,
	gasUsedByCall uint64,
	stackInfo CallGasDimensionStackInfo,
	finishGasesByDimension GasesByDimension,
) {
	stackInfo, ok := t.callStack.Pop()
	// base case, stack is empty, do nothing
	if !ok {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf("call stack is unexpectedly empty %d %d %d", pc, depth, t.depth)
		return true, 0, CallGasDimensionStackInfo{}, GasesByDimension{}
	}
	finishFunction := GetFinishCalcGasDimensionFunc(stackInfo.GasDimensionInfo.Op)
	if finishFunction == nil {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf(
			"no finish function found for opcode %s, call stack is messed up %d",
			stackInfo.GasDimensionInfo.Op.String(),
			pc,
		)
		return true, 0, CallGasDimensionStackInfo{}, GasesByDimension{}
	}
	// IMPORTANT NOTE: for some reason the only reliable way to actually get the gas cost of the call
	// is to subtract gas at time of call from gas at opcode AFTER return
	// you can't trust the `gas` field on the call itself. I wonder if the gas field is an estimation
	gasUsedByCall = stackInfo.GasDimensionInfo.GasCounterAtTimeOfCall - gas
	var finishErr error = nil
	finishGasesByDimension, finishErr = finishFunction(gasUsedByCall, stackInfo.ExecutionCost, stackInfo.GasDimensionInfo)
	if finishErr != nil {
		t.interrupt.Store(true)
		t.reason = finishErr
		return true, 0, CallGasDimensionStackInfo{}, GasesByDimension{}
	}
	return false, gasUsedByCall, stackInfo, finishGasesByDimension
}

// if we are in a call stack depth greater than 0,
// then we need to track the execution gas
// of our own code so that when the call returns,
// we can write the gas dimensions for the call opcode itself
func (t *BaseGasDimensionTracer) updateExecutionCost(cost uint64) {
	if len(t.callStack) > 0 {
		t.callStack.UpdateExecutionCost(cost)
	}
}

// at the end of each OnOpcode, update the previous access list, so that we can
// use it should the next opcode need to check if an address is in the access list
func (t *BaseGasDimensionTracer) updatePrevAccessList(addresses map[common.Address]int, slots []map[common.Hash]struct{}) {
	if addresses == nil {
		t.prevAccessListAddresses = map[common.Address]int{}
		t.prevAccessListSlots = []map[common.Hash]struct{}{}
		return
	}
	t.prevAccessListAddresses = addresses
	t.prevAccessListSlots = slots
}

// OnTxStart handles transaction start
func (t *BaseGasDimensionTracer) OnTxStart(env *tracing.VMContext, tx *types.Transaction, from common.Address) {
	t.env = env
	isContractCreation := tx.To() == nil
	rules := t.chainConfig.Rules(env.BlockNumber, env.Random != nil, env.Time, env.ArbOSVersion)
	intrinsicGas, _ := core.IntrinsicGas(
		tx.Data(),
		tx.AccessList(),
		tx.SetCodeAuthorizations(),
		isContractCreation,
		rules.IsHomestead,
		rules.IsIstanbul,
		rules.IsShanghai,
	)
	t.intrinsicGas = intrinsicGas
}

// OnTxEnd handles transaction end
func (t *BaseGasDimensionTracer) OnTxEnd(receipt *types.Receipt, err error) {
	if err != nil {
		// Don't override vm error
		if t.err == nil {
			t.err = err
		}
		return
	}
	t.gasUsed = receipt.GasUsed
	t.gasUsedForL1 = receipt.GasUsedForL1
	t.gasUsedForL2 = receipt.GasUsedForL2()
	t.txHash = receipt.TxHash
	t.refundAdjusted = t.adjustRefund(t.executionGasAccumulated+t.intrinsicGas, t.GetRefundAccumulated())
}

// Stop signals the tracer to stop tracing
func (t *BaseGasDimensionTracer) Stop(err error) {
	t.reason = err
	t.interrupt.Store(true)
}

// ############################################################################
//                                HELPERS
// ############################################################################

// WasCallOrCreate returns true if the opcode is a type of opcode that makes calls increasing the stack depth
func WasCallOrCreate(opcode vm.OpCode) bool {
	return opcode == vm.CALL || opcode == vm.CALLCODE || opcode == vm.DELEGATECALL ||
		opcode == vm.STATICCALL || opcode == vm.CREATE || opcode == vm.CREATE2
}

// GetOpRefund returns the current op refund
func (t *BaseGasDimensionTracer) GetOpRefund() uint64 {
	return t.env.StateDB.GetRefund()
}

// GetRefundAccumulated returns the accumulated refund
func (t *BaseGasDimensionTracer) GetRefundAccumulated() uint64 {
	return t.refundAccumulated
}

// SetRefundAccumulated sets the accumulated refund
func (t *BaseGasDimensionTracer) SetRefundAccumulated(refund uint64) {
	t.refundAccumulated = refund
}

// GetStateDB returns the state database
func (t *BaseGasDimensionTracer) GetStateDB() tracing.StateDB {
	return t.env.StateDB
}

// GetPrevAccessList returns the previous access list
func (t *BaseGasDimensionTracer) GetPrevAccessList() (addresses map[common.Address]int, slots []map[common.Hash]struct{}) {
	return t.prevAccessListAddresses, t.prevAccessListSlots
}

// Error returns the VM error captured by the trace
func (t *BaseGasDimensionTracer) Error() error { return t.err }

// Add to the execution gas accumulated, for tracking adjusted refund
func (t *BaseGasDimensionTracer) AddToExecutionGasAccumulated(gas uint64) {
	t.executionGasAccumulated += gas
}

// this function implements the EIP-3529 refund adjustment
// this is a copy of the logic in the state transition function
func (t *BaseGasDimensionTracer) adjustRefund(gasUsedByL2BeforeRefunds, refund uint64) uint64 {
	var refundAdjusted uint64
	if !t.chainConfig.IsLondon(t.env.BlockNumber) {
		refundAdjusted = gasUsedByL2BeforeRefunds / params.RefundQuotient
	} else {
		refundAdjusted = gasUsedByL2BeforeRefunds / params.RefundQuotientEIP3529
	}
	if refundAdjusted > refund {
		return refund
	}
	return refundAdjusted
}

// ############################################################################
//                                OUTPUTS
// ############################################################################

// BaseExecutionResult has shared fields for execution results
type BaseExecutionResult struct {
	GasUsed        uint64   `json:"gasUsed"`
	GasUsedForL1   uint64   `json:"gasUsedForL1"`
	GasUsedForL2   uint64   `json:"gasUsedForL2"`
	IntrinsicGas   uint64   `json:"intrinsicGas"`
	AdjustedRefund uint64   `json:"adjustedRefund"`
	Failed         bool     `json:"failed"`
	TxHash         string   `json:"txHash"`
	BlockTimestamp uint64   `json:"blockTimestamp"`
	BlockNumber    *big.Int `json:"blockNumber"`
}

// get the result of the transaction execution that we will hand to the json output
func (t *BaseGasDimensionTracer) GetBaseExecutionResult() (BaseExecutionResult, error) {
	// Tracing aborted
	if t.reason != nil {
		return BaseExecutionResult{}, t.reason
	}
	failed := t.err != nil

	return BaseExecutionResult{
		GasUsed:        t.gasUsed,
		GasUsedForL1:   t.gasUsedForL1,
		GasUsedForL2:   t.gasUsedForL2,
		IntrinsicGas:   t.intrinsicGas,
		AdjustedRefund: t.refundAdjusted,
		Failed:         failed,
		TxHash:         t.txHash.Hex(),
		BlockTimestamp: t.env.Time,
		BlockNumber:    t.env.BlockNumber,
	}, nil
}
