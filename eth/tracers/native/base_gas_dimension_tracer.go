package native

import (
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/params"
)

// BaseGasDimensionTracer contains the shared functionality between different gas dimension tracers
type BaseGasDimensionTracer struct {
	// whether the tracer itself is being debugged
	debug bool
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
	// whether the root of the call stack is a precompile
	rootIsPrecompile bool
	// an adjustment must be made to the gas value of the transaction
	// if the root is a precompile
	rootIsPrecompileAdjustment uint64
	// whether the root of the call stack is a stylus contract
	rootIsStylus bool
	// an adjustment must be made to the gas value of the transaction
	// if the root is a stylus contract
	rootIsStylusAdjustment uint64
	// maintain an access list tracer to check previous access list statuses.
	accessListTracer *logger.AccessListTracer
	// the amount of refund accumulated at the current step of execution
	refundAccumulated uint64
	// in order to calculate the refund adjusted, we need to know the total execution gas
	// of just the opcodes of the transaction with no refunds
	rootExecutionGasAccumulated uint64
	// the amount of refund allowed at the end of the transaction, adjusted by EIP-3529
	refundAdjusted uint64
	// whether the transaction should be rejected for not following the rules of the VM
	err error
	// the transactions status, for a valid transaction that followed the rules,
	// but could have still failed for reasons inside the rules, like reverts, out of gas, etc.
	status uint64
	// whether the tracer itself was interrupted
	interrupt atomic.Bool
	// reason or error for the interruption in the tracer itself (as opposed to the transaction)
	reason error
	// cached chain config for use in hooks
	chainConfig *params.ChainConfig
	// for debugging it's sometimes useful to know the order in which opcodes were seen / count
	opCount uint64
}

// BaseGasDimensionTracerConfig is the configuration for the base gas dimension tracer
type BaseGasDimensionTracerConfig struct {
	Debug bool
}

// create a new base gas dimension tracer
func NewBaseGasDimensionTracer(cfg json.RawMessage, chainConfig *params.ChainConfig) (*BaseGasDimensionTracer, error) {
	debug := false
	if cfg != nil {
		var config BaseGasDimensionTracerConfig
		if err := json.Unmarshal(cfg, &config); err != nil {
			return nil, err
		}
		debug = config.Debug
	}
	return &BaseGasDimensionTracer{
		debug:                       debug,
		chainConfig:                 chainConfig,
		depth:                       1,
		refundAccumulated:           0,
		accessListTracer:            nil,
		env:                         nil,
		txHash:                      common.Hash{},
		gasUsed:                     0,
		gasUsedForL1:                0,
		gasUsedForL2:                0,
		intrinsicGas:                0,
		callStack:                   CallGasDimensionStack{},
		rootIsPrecompile:            false,
		rootIsPrecompileAdjustment:  0,
		rootIsStylus:                false,
		rootIsStylusAdjustment:      0,
		rootExecutionGasAccumulated: 0,
		refundAdjusted:              0,
		err:                         nil,
		status:                      0,
		interrupt:                   atomic.Bool{},
		reason:                      nil,
		opCount:                     0,
	}, nil
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
	t.opCount++
	// First check if tracer has been interrupted
	if t.interrupt.Load() {
		return true, ZeroGasesByDimension(), nil, vm.OpCode(op)
	}

	// Depth validation - if depth doesn't match expectations, this is a tracer error
	if depth != t.depth && depth != t.depth-1 {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf(
			"expected depth fell out of sync with actual depth: %d %d != %d, callStack: %v",
			pc,
			t.depth,
			depth,
			t.callStack,
		)
		return true, ZeroGasesByDimension(), nil, vm.OpCode(op)
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
		return true, ZeroGasesByDimension(), nil, vm.OpCode(op)
	}

	// Get the gas dimension function
	// If it's not a call, directly calculate the gas dimensions for the opcode
	f := GetCalcGasDimensionFunc(vm.OpCode(op))
	var fErr error
	gasesByDimension, callStackInfo, fErr = f(t, pc, op, gas, cost, scope, rData, depth, err)

	// If there's a problem with our dimension calculation function, this is a tracer error
	if fErr != nil {
		t.interrupt.Store(true)
		t.reason = fErr
		return true, ZeroGasesByDimension(), nil, vm.OpCode(op)
	}
	opcode = vm.OpCode(op)

	// Logic validation check - another potential tracer error
	if WasCallOrCreate(opcode) && callStackInfo == nil && err == nil || !WasCallOrCreate(opcode) && callStackInfo != nil {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf(
			"logic bug, calls/creates should always be accompanied by callStackInfo and non-calls should not have callStackInfo %d %s %v",
			pc,
			opcode.String(),
			callStackInfo,
		)
		return true, ZeroGasesByDimension(), nil, vm.OpCode(op)
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
	totalGasUsedByCall uint64,
	stackInfo CallGasDimensionStackInfo,
	finishGasesByDimension GasesByDimension,
) {
	stackInfo, ok := t.callStack.Pop()
	// base case, stack is empty, do nothing
	if !ok {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf("call stack is unexpectedly empty %d %d %d", pc, depth, t.depth)
		return true, 0, zeroCallGasDimensionStackInfo(), ZeroGasesByDimension()
	}
	finishFunction := GetFinishCalcGasDimensionFunc(stackInfo.GasDimensionInfo.Op)
	if finishFunction == nil {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf(
			"no finish function found for opcode %s, call stack is messed up %d",
			stackInfo.GasDimensionInfo.Op.String(),
			pc,
		)
		return true, 0, zeroCallGasDimensionStackInfo(), ZeroGasesByDimension()
	}
	// IMPORTANT NOTE: for some reason the only reliable way to actually get the gas cost of the call
	// is to subtract gas at time of call from gas at opcode AFTER return
	// you can't trust the `gas` field on the call itself. I wonder if the gas field is an estimation
	totalGasUsedByCall = stackInfo.GasDimensionInfo.GasCounterAtTimeOfCall - gas
	var finishErr error

	finishGasesByDimension, finishErr = finishFunction(totalGasUsedByCall, stackInfo.ExecutionCost, stackInfo.GasDimensionInfo)
	if finishErr != nil {
		t.interrupt.Store(true)
		t.reason = finishErr
		return true, 0, zeroCallGasDimensionStackInfo(), ZeroGasesByDimension()
	}
	return false, totalGasUsedByCall, stackInfo, finishGasesByDimension
}

// if we are in a call stack depth greater than 0,
// then we need to track the execution gas
// of our own code so that when the call returns,
// we can write the gas dimensions for the call opcode itself
func (t *BaseGasDimensionTracer) updateCallChildExecutionCost(cost uint64) {
	if len(t.callStack) > 0 {
		t.callStack.UpdateExecutionCost(cost)
	}
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
	t.rootIsPrecompile = false
	t.rootIsPrecompileAdjustment = 0
	t.rootIsStylus = false
	t.rootIsStylusAdjustment = 0
	precompileAddressList := t.GetPrecompileAddressList()
	if tx.To() != nil {
		t.rootIsPrecompile = slices.Contains(precompileAddressList, *tx.To())
		t.rootIsStylus = isStylusContract(t, *tx.To())
	}
	// addressesToExclude is supposed to contain sender, receiver, precompiles and valid authorizations
	addressesToExclude := map[common.Address]struct{}{}
	for _, addr := range precompileAddressList {
		addressesToExclude[addr] = struct{}{}
	}
	addressesToExclude[from] = struct{}{}
	if tx.To() != nil {
		addressesToExclude[*tx.To()] = struct{}{}
	}
	t.accessListTracer = logger.NewAccessListTracer(tx.AccessList(), addressesToExclude)
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
	t.status = receipt.Status
	t.refundAdjusted = t.adjustRefund(t.rootExecutionGasAccumulated+t.intrinsicGas, t.GetRefundAccumulated())
	// if the root is a precompile then we need to separately account for the gas used by the
	// precompile itself, which is not exposed to the opcode tracing
	if t.rootIsPrecompile {
		t.rootIsPrecompileAdjustment = t.gasUsedForL2 - (t.rootExecutionGasAccumulated + t.intrinsicGas)
	}
	if t.rootIsStylus {
		t.rootIsStylusAdjustment = t.gasUsedForL2 - (t.rootExecutionGasAccumulated + t.intrinsicGas)
	}
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

// GetCallStack returns the call stack
func (t *BaseGasDimensionTracer) GetCallStack() CallGasDimensionStack {
	return t.callStack
}

// GetOpCount returns the op count
func (t *BaseGasDimensionTracer) GetOpCount() uint64 {
	return t.opCount
}

// GetRootIsPrecompile returns whether the root of the call stack is a precompile
func (t *BaseGasDimensionTracer) GetRootIsPrecompile() bool {
	return t.rootIsPrecompile
}

// GetRootIsStylus returns whether the root of the call stack is a stylus contract
func (t *BaseGasDimensionTracer) GetRootIsStylus() bool {
	return t.rootIsStylus
}

// GetAccessListTracer returns the access list tracer
func (t *BaseGasDimensionTracer) GetAccessListTracer() *logger.AccessListTracer {
	return t.accessListTracer
}

// GetPrecompileAddressList returns the list of precompile addresses
func (t *BaseGasDimensionTracer) GetPrecompileAddressList() []common.Address {
	rules := t.chainConfig.Rules(t.env.BlockNumber, t.env.Random != nil, t.env.Time, t.env.ArbOSVersion)
	return vm.ActivePrecompiles(rules)
}

// Error returns the EVM execution error captured by the trace
func (t *BaseGasDimensionTracer) Error() error { return t.err }

// Status returns the status of the transaction, e.g. 0 for failure, 1 for success
// a transaction can revert, fail, and still be mined and included in a block
func (t *BaseGasDimensionTracer) Status() uint64 { return t.status }

// Reason returns any errors that occurred in the tracer itself
func (t *BaseGasDimensionTracer) Reason() error { return t.reason }

// Add to the execution gas accumulated, for tracking adjusted refund
func (t *BaseGasDimensionTracer) AddToRootExecutionGasAccumulated(gas uint64) {
	t.rootExecutionGasAccumulated += gas
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

// ZeroGasesByDimension returns a GasesByDimension struct with all fields set to zero
func ZeroGasesByDimension() GasesByDimension {
	return GasesByDimension{
		OneDimensionalGasCost: 0,
		Computation:           0,
		StateAccess:           0,
		StateGrowth:           0,
		HistoryGrowth:         0,
		StateGrowthRefund:     0,
		ChildExecutionCost:    0,
	}
}

// zeroCallGasDimensionInfo returns a CallGasDimensionInfo struct with all fields set to zero
func zeroCallGasDimensionInfo() CallGasDimensionInfo {
	return CallGasDimensionInfo{
		Pc:                        0,
		Op:                        0,
		DepthAtTimeOfCall:         0,
		GasCounterAtTimeOfCall:    0,
		MemoryExpansionCost:       0,
		AccessListComputationCost: 0,
		AccessListStateAccessCost: 0,
		IsValueSentWithCall:       false,
		InitCodeCost:              0,
		HashCost:                  0,
		isTargetPrecompile:        false,
		isTargetStylusContract:    false,
		inPrecompile:              false,
	}
}

// zeroCallGasDimensionStackInfo returns a CallGasDimensionStackInfo struct with all fields set to zero
func zeroCallGasDimensionStackInfo() CallGasDimensionStackInfo {
	return CallGasDimensionStackInfo{
		GasDimensionInfo:     zeroCallGasDimensionInfo(),
		ExecutionCost:        0,
		DimensionLogPosition: 0,
	}
}

// ############################################################################
//                                OUTPUTS
// ############################################################################

// BaseExecutionResult has shared fields for execution results
type BaseExecutionResult struct {
	GasUsed                    uint64   `json:"gasUsed"`
	GasUsedForL1               uint64   `json:"gasUsedForL1"`
	GasUsedForL2               uint64   `json:"gasUsedForL2"`
	IntrinsicGas               uint64   `json:"intrinsicGas"`
	AdjustedRefund             uint64   `json:"adjustedRefund"`
	RootIsPrecompile           bool     `json:"rootIsPrecompile"`
	RootIsPrecompileAdjustment uint64   `json:"rootIsPrecompileAdjustment"`
	RootIsStylus               bool     `json:"rootIsStylus"`
	RootIsStylusAdjustment     uint64   `json:"rootIsStylusAdjustment"`
	Failed                     bool     `json:"failed"`
	TxHash                     string   `json:"txHash"`
	BlockTimestamp             uint64   `json:"blockTimestamp"`
	BlockNumber                *big.Int `json:"blockNumber"`
	Status                     uint64   `json:"status"`
}

// get the result of the transaction execution that we will hand to the json output
func (t *BaseGasDimensionTracer) GetBaseExecutionResult() (BaseExecutionResult, error) {
	failed := t.err != nil
	ret := BaseExecutionResult{
		GasUsed:                    t.gasUsed,
		GasUsedForL1:               t.gasUsedForL1,
		GasUsedForL2:               t.gasUsedForL2,
		IntrinsicGas:               t.intrinsicGas,
		AdjustedRefund:             t.refundAdjusted,
		RootIsPrecompile:           t.rootIsPrecompile,
		RootIsPrecompileAdjustment: t.rootIsPrecompileAdjustment,
		RootIsStylus:               t.rootIsStylus,
		RootIsStylusAdjustment:     t.rootIsStylusAdjustment,
		Failed:                     failed,
		TxHash:                     t.txHash.Hex(),
		BlockTimestamp:             t.env.Time,
		BlockNumber:                t.env.BlockNumber,
		Status:                     t.status,
	}
	return ret, t.reason
}
