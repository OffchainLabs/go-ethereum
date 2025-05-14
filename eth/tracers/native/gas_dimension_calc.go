package native

import (
	"fmt"
	"math"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// GasesByDimension is a type that represents the gas consumption for each dimension
// for a given opcode.
type GasesByDimension struct {
	OneDimensionalGasCost uint64 `json:"gas1d"`
	Computation           uint64 `json:"cpu"`
	StateAccess           uint64 `json:"rw,omitempty"`
	StateGrowth           uint64 `json:"growth,omitempty"`
	HistoryGrowth         uint64 `json:"hist,omitempty"`
	StateGrowthRefund     int64  `json:"refund,omitempty"`
	ChildExecutionCost    uint64 `json:"childcost,omitempty"`
}

// in the case of opcodes like CALL, STATICCALL, DELEGATECALL, etc,
// in order to calculate the gas dimensions we need to allow the call to complete
// and then look at the data from that completion after the call has returned.
// CallGasDimensionInfo retains the relevant information that needs to be remembered
// from the start of the call to compute the gas dimensions after the call has returned.
type CallGasDimensionInfo struct {
	Pc                        uint64
	Op                        vm.OpCode
	GasCounterAtTimeOfCall    uint64
	MemoryExpansionCost       uint64
	AccessListComputationCost uint64
	AccessListStateAccessCost uint64
	IsValueSentWithCall       bool
	InitCodeCost              uint64
	HashCost                  uint64
}

// CallGasDimensionStackInfo is a struct that contains the gas dimension info
// and the position of the dimension log in the dimension logs array
// so that the finish functions can directly write into the dimension logs
type CallGasDimensionStackInfo struct {
	GasDimensionInfo     CallGasDimensionInfo
	DimensionLogPosition int
	ExecutionCost        uint64
}

// CallGasDimensionStack is a stack of CallGasDimensionStackInfo
// so that RETURN opcodes can pop the appropriate gas dimension info
// and then write the gas dimensions into the dimension logs
type CallGasDimensionStack []CallGasDimensionStackInfo

// Push a new CallGasDimensionStackInfo onto the stack
func (c *CallGasDimensionStack) Push(info CallGasDimensionStackInfo) { // gasDimensionInfo CallGasDimensionInfo, dimensionLogPosition int, executionCost uint64) {
	*c = append(*c, info)
}

// Pop a CallGasDimensionStackInfo from the stack, returning false if the stack is empty
func (c *CallGasDimensionStack) Pop() (CallGasDimensionStackInfo, bool) {
	if len(*c) == 0 {
		return zeroCallGasDimensionStackInfo(), false
	}
	last := (*c)[len(*c)-1]
	*c = (*c)[:len(*c)-1]
	return last, true
}

// UpdateExecutionCost updates the execution cost for the top layer of the call stack
// so that the call knows how much gas was consumed by child opcodes in that call depth
func (c *CallGasDimensionStack) UpdateExecutionCost(executionCost uint64) {
	stackLen := len(*c)
	if stackLen == 0 {
		return
	}
	top := (*c)[stackLen-1]
	top.ExecutionCost += executionCost
	(*c)[stackLen-1] = top
}

// define interface for a dimension tracer
// that provides the minimum necessary methods
// to make the calcSstore function work
type DimensionTracer interface {
	GetOpRefund() uint64
	GetRefundAccumulated() uint64
	SetRefundAccumulated(uint64)
	GetPrevAccessList() (addresses map[common.Address]int, slots []map[common.Hash]struct{})
}

// calcGasDimensionFunc defines a type signature that takes the opcode
// tracing data for an opcode and return the gas consumption for each dimension
// for that given opcode.
//
// INVARIANT (for non-call opcodes): the sum of the gas consumption for each dimension
// equals the input `gas` to this function
type CalcGasDimensionFunc func(
	t DimensionTracer,
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) (GasesByDimension, *CallGasDimensionInfo, error)

// FinishCalcGasDimensionFunc defines a type signature that takes the
// code execution cost of the call and the callGasDimensionInfo
// and returns the gas dimensions for the call opcode itself
// THIS EXPLICITLY BREAKS THE ABOVE INVARIANT FOR THE NON-CALL OPCODES
// as call opcodes only contain the dimensions for the call itself,
// and the dimensions of their children are computed as their children are
// seen/traced.
type FinishCalcGasDimensionFunc func(
	totalGasUsed uint64,
	codeExecutionCost uint64,
	callGasDimensionInfo CallGasDimensionInfo,
) (GasesByDimension, error)

// getCalcGasDimensionFunc is a massive case switch
// statement that returns which function to call
// based on which opcode is being traced/executed
func GetCalcGasDimensionFunc(op vm.OpCode) CalcGasDimensionFunc {
	switch op {
	//  Opcodes that Only Operate on Storage Read/Write (storage access in the short run)
	// `BALANCE, EXTCODESIZE, EXTCODEHASH,`
	// `SLOAD`
	// `EXTCODECOPY`
	// `DELEGATECALL, STATICCALL`
	case vm.BALANCE, vm.EXTCODESIZE, vm.EXTCODEHASH:
		return calcSimpleAddressAccessSetGas
	case vm.SLOAD:
		return calcSLOADGas
	case vm.EXTCODECOPY:
		return calcExtCodeCopyGas
	case vm.DELEGATECALL, vm.STATICCALL:
		return calcStateReadCallGas
	// Opcodes that only grow the history
	// all of the LOG opcodes: `LOG0, LOG1, LOG2, LOG3, LOG4`
	case vm.LOG0, vm.LOG1, vm.LOG2, vm.LOG3, vm.LOG4:
		return calcLogGas
	// Opcodes that grow state without reading existing state
	// `CREATE, CREATE2`
	case vm.CREATE, vm.CREATE2:
		return calcCreateGas
	// Opcodes that Operate on Both R/W and State Growth
	// `CALL, CALLCODE`
	// `SSTORE`
	// `SELFDESTRUCT`
	case vm.CALL, vm.CALLCODE:
		return calcReadAndStoreCallGas
	case vm.SSTORE:
		return calcSStoreGas
	case vm.SELFDESTRUCT:
		return calcSelfDestructGas
	// Everything else is all CPU!
	// ADD, PUSH, etc etc etc
	default:
		return calcSimpleSingleDimensionGas
	}
}

// for any opcode that increases the depth of the stack,
// we have to call a finish function after the call has returned
// to know the code_execution_cost of the call
// and then use that to compute the gas dimensions
// for the call opcode itself.
func GetFinishCalcGasDimensionFunc(op vm.OpCode) FinishCalcGasDimensionFunc {
	switch op {
	case vm.DELEGATECALL, vm.STATICCALL:
		return finishCalcStateReadCallGas
	case vm.CALL, vm.CALLCODE:
		return finishCalcStateReadAndStoreCallGas
	case vm.CREATE, vm.CREATE2:
		return finishCalcCreateGas
	default:
		return nil
	}
}

// calcSimpleSingleDimensionGas returns the gas used for the
// simplest of transactions, that only use the computation dimension
func calcSimpleSingleDimensionGas(
	t DimensionTracer,
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) (GasesByDimension, *CallGasDimensionInfo, error) {
	if err != nil {
		return outOfGas(gas)
	}
	return GasesByDimension{
		OneDimensionalGasCost: cost,
		Computation:           cost,
		StateAccess:           0,
		StateGrowth:           0,
		HistoryGrowth:         0,
		StateGrowthRefund:     0,
		ChildExecutionCost:    0,
	}, nil, nil
}

// calcSimpleAddressAccessSetGas returns the gas used
// for relatively simple state access (read/write)
// operations. These opcodes only read addresses
//
//	from the state and do not expand it
//
// this includes:
// `BALANCE, EXTCODESIZE, EXTCODEHASH
func calcSimpleAddressAccessSetGas(
	t DimensionTracer,
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) (GasesByDimension, *CallGasDimensionInfo, error) {
	if err != nil {
		return outOfGas(gas)
	}
	// for all these opcodes the address being checked is in stack position 0
	addressAsInt := scope.StackData()[len(scope.StackData())-1]
	address := common.Address(addressAsInt.Bytes20())
	computationCost, stateAccessCost := addressAccessListCost(t, address)
	ret := GasesByDimension{
		OneDimensionalGasCost: cost,
		Computation:           computationCost,
		StateAccess:           stateAccessCost,
		StateGrowth:           0,
		HistoryGrowth:         0,
		StateGrowthRefund:     0,
		ChildExecutionCost:    0,
	}
	if err := checkGasDimensionsEqualOneDimensionalGas(pc, op, depth, gas, cost, ret); err != nil {
		return GasesByDimension{}, nil, err
	}
	return ret, nil, nil
}

// calcSLOADGas returns the gas used for the `SLOAD` opcode
// SLOAD reads a slot from the state. It cannot expand the state
func calcSLOADGas(
	t DimensionTracer,
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) (GasesByDimension, *CallGasDimensionInfo, error) {
	if err != nil {
		return outOfGas(gas)
	}
	stackData := scope.StackData()
	stackLen := len(stackData)
	slot := common.Hash(stackData[stackLen-1].Bytes32())
	computationCost, stateAccessCost := storageSlotAccessListCost(t, scope.Address(), slot)
	ret := GasesByDimension{
		OneDimensionalGasCost: cost,
		Computation:           computationCost,
		StateAccess:           stateAccessCost,
		StateGrowth:           0,
		HistoryGrowth:         0,
		StateGrowthRefund:     0,
		ChildExecutionCost:    0,
	}
	if err := checkGasDimensionsEqualOneDimensionalGas(pc, op, depth, gas, cost, ret); err != nil {
		return GasesByDimension{}, nil, err
	}
	return ret, nil, nil
}

// calcExtCodeCopyGas returns the gas used
// for the `EXTCODECOPY` opcode, which reads from
// the code of an external contract.
// Hence only state read implications
func calcExtCodeCopyGas(
	t DimensionTracer,
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) (GasesByDimension, *CallGasDimensionInfo, error) {
	// extcodecody has three components to its gas cost:
	// 1. minimum_word_size = (size + 31) / 32
	// 2. memory_expansion_cost
	// 3. address_access_cost - the access set.
	// gas for extcodecopy is 3 * minimum_word_size + memory_expansion_cost + address_access_cost
	// 3*minimum_word_size is always state access
	// if state access is 2600, then have 2500 for state access
	// rest is computation.
	if err != nil {
		return outOfGas(gas)
	}
	stack := scope.StackData()
	lenStack := len(stack)
	size := stack[lenStack-4].Uint64()     // size in stack position 4
	offset := stack[lenStack-2].Uint64()   // destination offset in stack position 2
	address := stack[lenStack-1].Bytes20() // address in stack position 1

	accessListComputationCost, accessListStateAccessCost := addressAccessListCost(t, common.Address(address))

	memoryExpansionCost, memErr := memoryExpansionCost(scope.MemoryData(), offset, size)
	if memErr != nil {
		return GasesByDimension{}, nil, memErr
	}
	minimumWordSizeCost := 3 * ((size + 31) / 32)
	stateAccess := minimumWordSizeCost + accessListStateAccessCost
	computation := memoryExpansionCost + accessListComputationCost
	ret := GasesByDimension{
		OneDimensionalGasCost: cost,
		Computation:           computation,
		StateAccess:           stateAccess,
		StateGrowth:           0,
		HistoryGrowth:         0,
		StateGrowthRefund:     0,
		ChildExecutionCost:    0,
	}
	if err := checkGasDimensionsEqualOneDimensionalGas(pc, op, depth, gas, cost, ret); err != nil {
		return GasesByDimension{}, nil, err
	}
	return ret, nil, nil
}

// calcStateReadCallGas returns the gas used
// for opcodes that read from the state but do not write to it
// this includes:
// `DELEGATECALL, STATICCALL`
// even though delegatecalls can modify state, they modify the local
// state of the calling contract, not the state of the called contract
// therefore, the state writes happen inside the calling context and their gas implications
// are accounted for on those opcodes (e.g. SSTORE), which means the delegatecall opcode itself
// only has state read implications. Staticcall is the same but even more read-only.
func calcStateReadCallGas(
	t DimensionTracer,
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) (GasesByDimension, *CallGasDimensionInfo, error) {
	if err != nil {
		return outOfGas(gas)
	}
	stack := scope.StackData()
	lenStack := len(stack)
	// argsOffset in stack position 3 (1-indexed)
	// argsSize in stack position 4
	argsOffset := stack[lenStack-3].Uint64()
	argsSize := stack[lenStack-4].Uint64()
	// address in stack position 2
	address := stack[lenStack-2].Bytes20()
	// Note that opcodes with a byte size parameter of 0 will not trigger memory expansion, regardless of their offset parameters
	if argsSize == 0 {
		argsOffset = 0
	}
	// return data offset in stack position 5
	// return data size in stack position 6
	returnDataOffset := stack[lenStack-5].Uint64()
	returnDataSize := stack[lenStack-6].Uint64()
	// Note that opcodes with a byte size parameter of 0 will not trigger memory expansion, regardless of their offset parameters
	if returnDataSize == 0 {
		returnDataOffset = 0
	}

	// to figure out memory expansion cost, take the bigger of the two memory writes
	// which will determine how big memory is expanded to
	var memExpansionOffset uint64 = argsOffset
	var memExpansionSize uint64 = argsSize
	if returnDataOffset+returnDataSize > argsOffset+argsSize {
		memExpansionOffset = returnDataOffset
		memExpansionSize = returnDataSize
	}

	var memExpansionCost uint64 = 0
	if memExpansionOffset+memExpansionSize != 0 {
		var memErr error
		memExpansionCost, memErr = memoryExpansionCost(scope.MemoryData(), memExpansionOffset, memExpansionSize)
		if memErr != nil {
			return GasesByDimension{}, nil, memErr
		}
	}

	accessListComputationCost, accessListStateAccessCost := addressAccessListCost(t, address)

	// at a minimum, the cost is 100 for the warm access set
	// and the memory expansion cost
	computation := memExpansionCost + accessListComputationCost
	// see finishCalcStateReadCallGas for more details
	return GasesByDimension{
			OneDimensionalGasCost: cost,
			Computation:           computation,
			StateAccess:           0,
			StateGrowth:           0,
			HistoryGrowth:         0,
			StateGrowthRefund:     0,
			ChildExecutionCost:    0,
		}, &CallGasDimensionInfo{
			Pc:                        pc,
			Op:                        vm.OpCode(op),
			GasCounterAtTimeOfCall:    gas,
			MemoryExpansionCost:       memExpansionCost,
			AccessListComputationCost: accessListComputationCost,
			AccessListStateAccessCost: accessListStateAccessCost,
			IsValueSentWithCall:       false,
			InitCodeCost:              0,
			HashCost:                  0,
		}, nil
}

// In order to calculate the gas dimensions for opcodes that
// increase the stack depth, we need to know
// the computed gas consumption of the code executed in the call
// AFAIK, this is only computable after the call has returned
// the caller is responsible for maintaining the state of the CallGasDimensionInfo
// when the call is first seen, and then calling finishX after the call has returned.
// this function finishes the DELEGATECALL and STATICCALL opcodes
func finishCalcStateReadCallGas(
	totalGasUsed uint64,
	codeExecutionCost uint64,
	callGasDimensionInfo CallGasDimensionInfo,
) (GasesByDimension, error) {
	oneDimensionalGas := totalGasUsed - codeExecutionCost
	computation := callGasDimensionInfo.AccessListComputationCost + callGasDimensionInfo.MemoryExpansionCost
	stateAccess := callGasDimensionInfo.AccessListStateAccessCost
	ret := GasesByDimension{
		OneDimensionalGasCost: oneDimensionalGas,
		Computation:           computation,
		StateAccess:           stateAccess,
		StateGrowth:           0,
		HistoryGrowth:         0,
		StateGrowthRefund:     0,
		ChildExecutionCost:    codeExecutionCost,
	}
	err := checkGasDimensionsEqualCallGas(
		callGasDimensionInfo.Pc,
		byte(callGasDimensionInfo.Op),
		codeExecutionCost,
		ret,
	)
	return ret, err
}

// calcLogGas returns the gas used for the `LOG0, LOG1, LOG2, LOG3, LOG4` opcodes
// which only grow the history tree.  History does not need to be referenced with
// every block since it does not produce the block hashes. So the cost implications
// of growing it and the prunability of it are not as important as state growth.
// The relevant opcodes here are:
// `LOG0, LOG1, LOG2, LOG3, LOG4`
func calcLogGas(
	t DimensionTracer,
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) (GasesByDimension, *CallGasDimensionInfo, error) {
	// log gas = 375 + 375 * topic_count + 8 * size + memory_expansion_cost
	// 8 * size is always history growth
	// the size is charged 8 gas per byte, and 32 bytes per topic are
	// stored in the bloom filter in the history so at 8 gas per byte,
	// 32 bytes per topic is 256 gas per topic.
	// rest is computation (for the bloom filter computation, memory expansion, etc)
	if err != nil {
		return outOfGas(gas)
	}
	numTopics := uint64(0)
	switch vm.OpCode(op) {
	case vm.LOG0:
		numTopics = 0
	case vm.LOG1:
		numTopics = 1
	case vm.LOG2:
		numTopics = 2
	case vm.LOG3:
		numTopics = 3
	case vm.LOG4:
		numTopics = 4
	default:
		numTopics = 0
	}
	bloomHistoryGrowthCost := 256 * numTopics
	// size is on stack position 2
	stackData := scope.StackData()
	size := stackData[len(stackData)-2].Uint64()
	sizeHistoryGrowthCost := 8 * size
	historyGrowthCost := sizeHistoryGrowthCost + bloomHistoryGrowthCost
	computationCost := cost - historyGrowthCost

	return GasesByDimension{
		OneDimensionalGasCost: cost,
		Computation:           computationCost,
		StateAccess:           0,
		StateGrowth:           0,
		HistoryGrowth:         historyGrowthCost,
		StateGrowthRefund:     0,
		ChildExecutionCost:    0,
	}, nil, nil
}

// calcCreateGas returns the gas used for the CREATE and CREATE2 opcodes
// which do storage growth when the store the newly created contract code.
// the relevant opcodes here are:
// `CREATE, CREATE2`
func calcCreateGas(
	t DimensionTracer,
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) (GasesByDimension, *CallGasDimensionInfo, error) {
	// Create costs
	// minimum_word_size = (size + 31) / 32
	// init_code_cost = 2 * minimum_word_size
	// code_deposit_cost = 200 * deployed_code_size
	// static_gas = 32000
	// dynamic_gas = init_code_cost + memory_expansion_cost + deployment_code_execution_cost + code_deposit_cost
	if err != nil {
		return outOfGas(gas)
	}
	stack := scope.StackData()
	lenStack := len(stack)
	// size is on stack position 3 (1-indexed)
	size := stack[lenStack-3].Uint64()
	// offset is on stack position 2 (1-indexed)
	offset := stack[lenStack-2].Uint64()
	minimumWordSize := toWordSize(size)
	initCodeCost := 2 * minimumWordSize
	// if create2, then additionally we have hash_cost = 6*minimum_word_size
	var hashCost uint64 = 0
	if vm.OpCode(op) == vm.CREATE2 {
		hashCost = 6 * minimumWordSize
	}

	memExpansionCost, memErr := memoryExpansionCost(scope.MemoryData(), offset, size)
	if memErr != nil {
		return GasesByDimension{}, nil, memErr
	}
	// at this point we know everything except deployment_code_execution_cost and code_deposit_cost
	// so we can get those from the finishCalcCreateGas function
	return GasesByDimension{
			OneDimensionalGasCost: cost,
			Computation:           initCodeCost + memExpansionCost + params.CreateGas + hashCost,
			StateAccess:           0,
			StateGrowth:           0,
			HistoryGrowth:         0,
			StateGrowthRefund:     0,
			ChildExecutionCost:    0,
		}, &CallGasDimensionInfo{
			Pc:                        pc,
			Op:                        vm.OpCode(op),
			GasCounterAtTimeOfCall:    gas,
			MemoryExpansionCost:       memExpansionCost,
			AccessListComputationCost: 0,
			AccessListStateAccessCost: 0,
			IsValueSentWithCall:       false,
			InitCodeCost:              initCodeCost,
			HashCost:                  hashCost,
		}, nil
}

// finishCalcCreateGas returns the gas used for the CREATE and CREATE2 opcodes
// after finding out the deployment_code_execution_cost
func finishCalcCreateGas(
	totalGasUsed uint64,
	codeExecutionCost uint64,
	callGasDimensionInfo CallGasDimensionInfo,
) (GasesByDimension, error) {
	oneDimensionalGas := totalGasUsed - codeExecutionCost
	// totalGasUsed = init_code_cost + memory_expansion_cost + deployment_code_execution_cost + code_deposit_cost
	codeDepositCost := totalGasUsed - params.CreateGas - callGasDimensionInfo.InitCodeCost -
		callGasDimensionInfo.MemoryExpansionCost - callGasDimensionInfo.HashCost - codeExecutionCost
	// CALL costs 25000 for write to an empty account,
	// so of the 32000 static cost of CREATE and CREATE2 give 25000 to storage growth,
	// and then cut the last 7000 in half for compute and state growth to
	// manage the cost of intializing new accounts
	staticNonNewAccountCost := params.CreateGas - params.CallNewAccountGas
	computeNonNewAccountCost := staticNonNewAccountCost / 2
	growthNonNewAccountCost := staticNonNewAccountCost - computeNonNewAccountCost
	ret := GasesByDimension{
		OneDimensionalGasCost: oneDimensionalGas,
		Computation:           callGasDimensionInfo.InitCodeCost + callGasDimensionInfo.MemoryExpansionCost + callGasDimensionInfo.HashCost + computeNonNewAccountCost,
		StateAccess:           0,
		StateGrowth:           growthNonNewAccountCost + params.CallNewAccountGas + codeDepositCost,
		HistoryGrowth:         0,
		StateGrowthRefund:     0,
		ChildExecutionCost:    codeExecutionCost,
	}
	err := checkGasDimensionsEqualCallGas(
		callGasDimensionInfo.Pc,
		byte(callGasDimensionInfo.Op),
		codeExecutionCost,
		ret,
	)
	return ret, err
}

// calcReadAndStoreCallGas returns the gas used for the `CALL, CALLCODE` opcodes
// which both read from the state to figure out where to jump to and write to the state
// in limited cases, e.g. when CALLing an empty address, which is the equivalent of an
// ether transfer.
// the relevant opcodes here are:
// `CALL, CALLCODE`
func calcReadAndStoreCallGas(
	t DimensionTracer,
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) (GasesByDimension, *CallGasDimensionInfo, error) {
	if err != nil {
		return outOfGas(gas)
	}
	stack := scope.StackData()
	lenStack := len(stack)
	// value is in stack position 3
	valueSentWithCall := stack[lenStack-3].Uint64()
	// argsOffset in stack position 4 (1-indexed)
	// argsSize in stack position 5
	argsOffset := stack[lenStack-4].Uint64()
	argsSize := stack[lenStack-5].Uint64()
	// address in stack position 2
	address := stack[lenStack-2].Bytes20()
	// Note that opcodes with a byte size parameter of 0 will not trigger memory expansion, regardless of their offset parameters
	if argsSize == 0 {
		argsOffset = 0
	}
	// return data offset in stack position 6
	// return data size in stack position 7
	returnDataOffset := stack[lenStack-6].Uint64()
	returnDataSize := stack[lenStack-7].Uint64()
	// Note that opcodes with a byte size parameter of 0 will not trigger memory expansion, regardless of their offset parameters
	if returnDataSize == 0 {
		returnDataOffset = 0
	}

	// to figure out memory expansion cost, take the bigger of the two memory writes
	// which will determine how big memory is expanded to
	var memExpansionOffset uint64 = argsOffset
	var memExpansionSize uint64 = argsSize
	if returnDataOffset+returnDataSize > argsOffset+argsSize {
		memExpansionOffset = returnDataOffset
		memExpansionSize = returnDataSize
	}

	var memExpansionCost uint64 = 0
	if memExpansionOffset+memExpansionSize != 0 {
		var memErr error
		memExpansionCost, memErr = memoryExpansionCost(scope.MemoryData(), memExpansionOffset, memExpansionSize)
		if memErr != nil {
			return GasesByDimension{}, nil, memErr
		}
	}

	accessListComputationCost, accessListStateAccessCost := addressAccessListCost(t, address)

	// at a minimum, the cost is 100 for the warm access set
	// and the memory expansion cost
	computation := memExpansionCost + accessListComputationCost
	// see finishCalcStateReadCallGas for more details
	return GasesByDimension{
			OneDimensionalGasCost: cost,
			Computation:           computation,
			StateAccess:           accessListStateAccessCost,
			StateGrowth:           0,
			HistoryGrowth:         0,
			StateGrowthRefund:     0,
			ChildExecutionCost:    0,
		}, &CallGasDimensionInfo{
			Pc:                        pc,
			Op:                        vm.OpCode(op),
			GasCounterAtTimeOfCall:    gas,
			AccessListComputationCost: accessListComputationCost,
			AccessListStateAccessCost: accessListStateAccessCost,
			MemoryExpansionCost:       memExpansionCost,
			IsValueSentWithCall:       valueSentWithCall > 0,
			InitCodeCost:              0,
			HashCost:                  0,
		}, nil
}

// In order to calculate the gas dimensions for opcodes that
// increase the stack depth, we need to know
// the computed gas consumption of the code executed in the call
// AFAIK, this is only computable after the call has returned
// the caller is responsible for maintaining the state of the CallGasDimensionInfo
// when the call is first seen, and then calling finishX after the call has returned.
// this function finishes the CALL and CALLCODE opcodes
func finishCalcStateReadAndStoreCallGas(
	totalGasUsed uint64,
	codeExecutionCost uint64,
	callGasDimensionInfo CallGasDimensionInfo,
) (GasesByDimension, error) {
	oneDimensionalGas := totalGasUsed - codeExecutionCost
	// the stipend is 2300 and it is not charged to the call itself but used in the execution cost
	var positiveValueCostLessStipend uint64 = 0
	if callGasDimensionInfo.IsValueSentWithCall {
		positiveValueCostLessStipend = params.CallValueTransferGas - params.CallStipend
	}
	// the formula for call is:
	// dynamic_gas = memory_expansion_cost + code_execution_cost + address_access_cost + positive_value_cost + value_to_empty_account_cost
	// now with leftOver, we are left with address_access_cost + value_to_empty_account_cost
	leftOver := totalGasUsed - callGasDimensionInfo.MemoryExpansionCost - codeExecutionCost - positiveValueCostLessStipend
	// the maximum address_access_cost can ever be is 2600. Meanwhile value_to_empty_account_cost is at minimum 25000
	// so if leftOver is greater than 2600 then we know that the value_to_empty_account_cost was 25000
	// and whatever was left over after that was address_access_cost
	// callcode is the same as call except does not have value_to_empty_account_cost,
	// so this code properly handles it coincidentally, too
	ret := GasesByDimension{
		OneDimensionalGasCost: oneDimensionalGas,
		Computation:           callGasDimensionInfo.MemoryExpansionCost + params.WarmStorageReadCostEIP2929,
		StateAccess:           positiveValueCostLessStipend,
		StateGrowth:           0,
		HistoryGrowth:         0,
		StateGrowthRefund:     0,
		ChildExecutionCost:    codeExecutionCost,
	}
	if leftOver > params.ColdAccountAccessCostEIP2929 { // there is a value being sent to an empty account
		var coldCost uint64 = 0
		if leftOver-params.CallNewAccountGas == params.ColdAccountAccessCostEIP2929 {
			coldCost = params.ColdAccountAccessCostEIP2929 - params.WarmStorageReadCostEIP2929
		}
		ret = GasesByDimension{
			OneDimensionalGasCost: oneDimensionalGas,
			Computation:           callGasDimensionInfo.MemoryExpansionCost + params.WarmStorageReadCostEIP2929,
			StateAccess:           coldCost + positiveValueCostLessStipend,
			StateGrowth:           params.CallNewAccountGas,
			HistoryGrowth:         0,
			StateGrowthRefund:     0,
			ChildExecutionCost:    codeExecutionCost,
		}
	} else if leftOver == params.ColdAccountAccessCostEIP2929 {
		var coldCost uint64 = params.ColdAccountAccessCostEIP2929 - params.WarmStorageReadCostEIP2929
		ret = GasesByDimension{
			OneDimensionalGasCost: oneDimensionalGas,
			Computation:           callGasDimensionInfo.MemoryExpansionCost + params.WarmStorageReadCostEIP2929,
			StateAccess:           coldCost + positiveValueCostLessStipend,
			StateGrowth:           0,
			HistoryGrowth:         0,
			StateGrowthRefund:     0,
			ChildExecutionCost:    codeExecutionCost,
		}
	}
	err := checkGasDimensionsEqualCallGas(
		callGasDimensionInfo.Pc,
		byte(callGasDimensionInfo.Op),
		codeExecutionCost,
		ret,
	)
	return ret, err
}

// calcSStoreGas returns the gas used for the `SSTORE` opcode
func calcSStoreGas(
	t DimensionTracer,
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) (GasesByDimension, *CallGasDimensionInfo, error) {
	// if value == current_value
	//     base_dynamic_gas = 100
	// else if current_value == original_value
	//     if original_value == 0
	//         base_dynamic_gas = 20000
	//     else
	//         base_dynamic_gas = 2900
	// else
	//     base_dynamic_gas = 100
	// plus cost of access set cold vs warm
	// basically sstore is always state access unless the value is 0
	// in which case it is state growth

	// you have the following cases:
	// gas = 100 // warm, value == current_value,
	// or value != current_value and current_value != original_value
	// in which case its all compute you're just modifying memory really
	// gas = 20000 // warm, original_value == 0
	// state_access = 19900, 100 for compute
	// gas = 22100 // cold, original_value == 0
	// state_access = 22000, 100 for compute
	// gas = 2900 // warm, value != current_value, current_value == original_value, original_value != 0
	// this 2900 is state access, give warm 100 for compute
	// gas = 5000 // cold, value != current_value, current_value == original_value, original_value != 0
	// state access for the 2900 and also for the cold access set, give warm 100 for compute

	// REFUND LOGIC
	// refunds are tracked in the statedb
	// to find per-step changes, we track the accumulated refund
	// and compare it to the current refund
	if err != nil {
		return outOfGas(gas)
	}
	currentRefund := t.GetOpRefund()
	accumulatedRefund := t.GetRefundAccumulated()
	var diff int64 = 0
	if accumulatedRefund != currentRefund {
		if accumulatedRefund < currentRefund {
			diff = int64(currentRefund - accumulatedRefund)
		} else {
			diff = -1 * int64(accumulatedRefund-currentRefund)
		}
		t.SetRefundAccumulated(currentRefund)
	}
	ret := zeroGasesByDimension()
	ret.OneDimensionalGasCost = cost
	if cost >= params.SstoreSetGas { // 22100 case and 20000 case
		accessCost := cost - params.SstoreSetGas
		ret = GasesByDimension{
			OneDimensionalGasCost: cost,
			Computation:           0,
			StateAccess:           accessCost,
			StateGrowth:           params.SstoreSetGas,
			HistoryGrowth:         0,
			StateGrowthRefund:     diff,
			ChildExecutionCost:    0,
		}
	} else if cost > 0 {
		ret = GasesByDimension{
			OneDimensionalGasCost: cost,
			Computation:           0,
			StateAccess:           cost,
			StateGrowth:           0,
			HistoryGrowth:         0,
			StateGrowthRefund:     diff,
			ChildExecutionCost:    0,
		}
	}
	if err := checkGasDimensionsEqualOneDimensionalGas(pc, op, depth, gas, cost, ret); err != nil {
		return GasesByDimension{}, nil, err
	}
	return ret, nil, nil
}

// calcSelfDestructGas returns the gas used for the `SELFDESTRUCT` opcode
// which deletes a contract which is a write to the state
func calcSelfDestructGas(
	t DimensionTracer,
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) (GasesByDimension, *CallGasDimensionInfo, error) {
	// reverse engineer the gas dimensions from the cost
	// two things we care about:
	// address being cold or warm for the access set
	// account being empty or not for the target of the selfdestruct.
	// basically there are only 4 possible cases for the cost:
	// 5000  (warm, target for funds is not empty),
	// 7600  (cold, target for funds is not empty),
	// 30000 (warm, target for funds is empty),
	// 32600 (cold, target for funds is empty)
	// we consider the static cost of 5000 as a state read/write because selfdestruct,
	// excepting 100 for the warm access set
	// doesn't actually delete anything from disk, it just marks it as deleted.
	if err != nil {
		return outOfGas(gas)
	}
	if cost == params.CreateBySelfdestructGas+params.SelfdestructGasEIP150 {
		// warm but funds target empty
		// 30000 gas total
		// 100 for warm cost (computation)
		// 25000 for the selfdestruct (state growth)
		// 4900 for read/write (deleting the contract)
		return GasesByDimension{
			OneDimensionalGasCost: cost,
			Computation:           params.WarmStorageReadCostEIP2929,
			StateAccess:           params.SelfdestructGasEIP150 - params.WarmStorageReadCostEIP2929,
			StateGrowth:           params.CreateBySelfdestructGas,
			HistoryGrowth:         0,
			StateGrowthRefund:     0,
			ChildExecutionCost:    0,
		}, nil, nil
	} else if cost == params.CreateBySelfdestructGas+params.SelfdestructGasEIP150+params.ColdAccountAccessCostEIP2929 {
		// cold and funds target empty
		// 32600 gas total
		// 100 for warm cost (computation)
		// 25000 for the selfdestruct (state growth)
		// 2500 + 5000 for read/write (deleting the contract)
		return GasesByDimension{
			OneDimensionalGasCost: cost,
			Computation:           params.WarmStorageReadCostEIP2929,
			StateAccess:           params.ColdAccountAccessCostEIP2929 - params.WarmStorageReadCostEIP2929 + params.SelfdestructGasEIP150,
			StateGrowth:           params.CreateBySelfdestructGas,
			HistoryGrowth:         0,
			StateGrowthRefund:     0,
			ChildExecutionCost:    0,
		}, nil, nil
	} else if cost == params.SelfdestructGasEIP150+params.ColdAccountAccessCostEIP2929 {
		// address lookup was cold but funds target has money already. Cost is 7600
		// 100 for warm cost (computation)
		// 2500 to access the address cold (access)
		// 5000 for the selfdestruct (access)
		return GasesByDimension{
			OneDimensionalGasCost: cost,
			Computation:           params.WarmStorageReadCostEIP2929,
			StateAccess:           params.ColdAccountAccessCostEIP2929 - params.WarmStorageReadCostEIP2929 + params.SelfdestructGasEIP150,
			StateGrowth:           0,
			HistoryGrowth:         0,
			StateGrowthRefund:     0,
			ChildExecutionCost:    0,
		}, nil, nil
	}
	// if you reach here, then the cost was 5000
	// in which case give 100 for a warm access read
	// and 4900 for the state access (deleting the contract)
	return GasesByDimension{
		OneDimensionalGasCost: cost,
		Computation:           params.WarmStorageReadCostEIP2929,
		StateAccess:           params.SelfdestructGasEIP150 - params.WarmStorageReadCostEIP2929,
		StateGrowth:           0,
		HistoryGrowth:         0,
		StateGrowthRefund:     0,
		ChildExecutionCost:    0,
	}, nil, nil
}

// ############################################################################
//                        HELPER FUNCTIONS
// ############################################################################

// Calculating the memory expansion cost requires calling `memoryGasCost` twice
// because computing the memory gas cost expects to know a "previous" price
func memoryExpansionCost(memoryBefore []byte, offset uint64, size uint64) (memoryExpansionCost uint64, err error) {
	// calculating the "lastGasCost" requires working around uint64 overflow
	var lastGasCost uint64 = calculateLastGasCost(toWordSize(uint64(len(memoryBefore))))
	memoryExpansionCost, _, err = memoryGasCost(memoryBefore, lastGasCost, size+offset)
	return memoryExpansionCost, err
}

func calculateLastGasCost(targetWords uint64) uint64 {
	// Start from 0 words and build up
	var currentWords uint64 = 0
	var totalGas uint64 = 0

	// Use a reasonable step size that won't cause overflow
	// We'll increase by 2^20 words (about 1M words) at a time maximum
	const maxStep uint64 = 1 << 20

	for currentWords < targetWords {
		var nextWords uint64
		if targetWords-currentWords > maxStep {
			nextWords = currentWords + maxStep
		} else {
			nextWords = targetWords
		}
		// Calculate incremental cost from currentWords to nextWords
		// Using the formula: (newWords - oldWords) * (3 + (newWords + oldWords) / 512)
		wordsDiff := nextWords - currentWords
		wordsSum := nextWords + currentWords

		// Use 64-bit math carefully to avoid overflow
		// First part: 3 * wordsDiff
		linearCost := 3 * wordsDiff

		// Second part: wordsDiff * wordsSum / 512
		// We do this carefully to avoid overflow:
		// 1. Calculate wordsDiff * (wordsSum / 512)
		// 2. Add any remainder: wordsDiff * (wordsSum % 512) / 512
		quadraticCost := wordsDiff * (wordsSum / 512)
		remainder := wordsDiff * (wordsSum % 512) / 512

		incrementalCost := linearCost + quadraticCost + remainder

		// Add to total cost
		totalGas += incrementalCost

		// Move to next chunk
		currentWords = nextWords
	}
	return totalGas
}

// copied from go-ethereum/core/vm/gas_table.go because not exported there
// toWordSize returns the ceiled word size required for memory expansion.
func toWordSize(size uint64) uint64 {
	if size > math.MaxUint64-31 {
		return math.MaxUint64/32 + 1
	}

	return (size + 31) / 32
}

// This code is copied and edited from go-ethereum/core/vm/gas_table.go
// because the code there is not exported.
// memoryGasCost calculates the quadratic gas for memory expansion. It does so
// only for the memory region that is expanded, not the total memory.
func memoryGasCost(mem []byte, lastGasCost uint64, newMemSize uint64) (fee uint64, newTotalFee uint64, err error) {
	if newMemSize == 0 {
		return 0, 0, nil
	}
	// The maximum that will fit in a uint64 is max_word_count - 1. Anything above
	// that will result in an overflow. Additionally, a newMemSize which results in
	// a newMemSizeWords larger than 0xFFFFFFFF will cause the square operation to
	// overflow. The constant 0x1FFFFFFFE0 is the highest number that can be used
	// without overflowing the gas calculation.
	if newMemSize > 0x1FFFFFFFE0 {
		return 0, 0, vm.ErrGasUintOverflow
	}
	newMemSizeWords := toWordSize(newMemSize)
	newMemSize = newMemSizeWords * 32

	if newMemSize > uint64(len(mem)) {
		square := newMemSizeWords * newMemSizeWords
		linCoef := newMemSizeWords * params.MemoryGas
		quadCoef := square / params.QuadCoeffDiv
		newTotalFee = linCoef + quadCoef

		fee = newTotalFee - lastGasCost

		return fee, newTotalFee, nil
	}
	return 0, 0, nil
}

// helper function that calculates the gas dimensions for an access list access for an address
func addressAccessListCost(t DimensionTracer, address common.Address) (computationGasCost uint64, stateAccessGasCost uint64) {
	accessListAddresses, _ := t.GetPrevAccessList()
	_, addressInAccessList := accessListAddresses[address]
	if !addressInAccessList {
		computationGasCost = params.WarmStorageReadCostEIP2929
		stateAccessGasCost = params.ColdAccountAccessCostEIP2929 - params.WarmStorageReadCostEIP2929
	} else {
		computationGasCost = params.WarmStorageReadCostEIP2929
		stateAccessGasCost = 0
	}
	return
}

// helper function that calculates the gas dimensions for an access list access for a storage slot
func storageSlotAccessListCost(t DimensionTracer, address common.Address, slot common.Hash) (computationGasCost uint64, stateAccessGasCost uint64) {
	accessListAddresses, accessListSlots := t.GetPrevAccessList()
	idx, ok := accessListAddresses[address]
	if !ok {
		// no such address (and hence zero slots)
		return params.WarmStorageReadCostEIP2929, params.ColdSloadCostEIP2929 - params.WarmStorageReadCostEIP2929
	}
	if idx == -1 {
		// address yes, but no slots
		return params.WarmStorageReadCostEIP2929, params.ColdSloadCostEIP2929 - params.WarmStorageReadCostEIP2929
	}
	_, slotPresent := accessListSlots[idx][slot]
	if !slotPresent {
		return params.WarmStorageReadCostEIP2929, params.ColdSloadCostEIP2929 - params.WarmStorageReadCostEIP2929
	}
	return params.WarmStorageReadCostEIP2929, 0
}

// wherever it's possible, check that the gas dimensions are sane
func checkGasDimensionsEqualOneDimensionalGas(
	pc uint64,
	op byte,
	depth int,
	gas uint64,
	cost uint64,
	dim GasesByDimension,
) error {
	if cost != dim.Computation+dim.StateAccess+dim.StateGrowth+dim.HistoryGrowth {
		return fmt.Errorf(
			"unexpected gas cost mismatch: pc %d, op %d, depth %d, gas %d, cost %d != %v",
			pc,
			op,
			depth,
			gas,
			cost,
			dim,
		)
	}
	return nil
}

// for calls and other opcodes that increase stack depth,
// use this function to check that the total computed gas
// is consistent with the expected gas
func checkGasDimensionsEqualCallGas(
	pc uint64,
	op byte,
	codeExecutionCost uint64,
	dim GasesByDimension,
) error {
	if dim.OneDimensionalGasCost != dim.Computation+dim.StateAccess+dim.StateGrowth+dim.HistoryGrowth {
		return fmt.Errorf(
			"unexpected gas cost mismatch: pc %d, op %d, with codeExecutionCost %d, expected %d == %v",
			pc,
			op,
			codeExecutionCost,
			dim.OneDimensionalGasCost,
			dim,
		)
	}
	return nil
}

// helper function that purely makes the golang prettier for the out of gas case
func outOfGas(gas uint64) (GasesByDimension, *CallGasDimensionInfo, error) {
	return GasesByDimension{
		OneDimensionalGasCost: gas,
		Computation:           gas,
		StateAccess:           0,
		StateGrowth:           0,
		HistoryGrowth:         0,
		StateGrowthRefund:     0,
		ChildExecutionCost:    0,
	}, nil, nil
}
