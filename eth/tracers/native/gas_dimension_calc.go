package native

import (
	"github.com/ethereum/go-ethereum/common/math"

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// GasesByDimension is a type that represents the gas consumption for each dimension
// for a given opcode.
// The dimensions in order of 0 - 3 are:
// 0: Computation
// 1: Storage Access (Read/Write)
// 2: State Growth (Expanding the size of the state)
// 3: History Growth (Expanding the size of the history, especially on archive nodes)
type GasesByDimension [5]uint64
type GasDimension = uint8

const (
	Computation       GasDimension = 0
	StateAccess       GasDimension = 1
	StateGrowth       GasDimension = 2
	HistoryGrowth     GasDimension = 3
	StateGrowthRefund GasDimension = 4
)

// calcGasDimensionFunc defines a type signature that takes the opcode
// tracing data for an opcode and return the gas consumption for each dimension
// for that given opcode.
//
// INVARIANT: the sum of the gas consumption for each dimension
// equals the input `gas` to this function
type calcGasDimensionFunc func(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) GasesByDimension

// getCalcGasDimensionFunc is a massive case switch
// statement that returns which function to call
// based on which opcode is being traced/executed
func getCalcGasDimensionFunc(op vm.OpCode) calcGasDimensionFunc {
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

// calcSimpleSingleDimensionGas returns the gas used for the
// simplest of transactions, that only use the computation dimension
func calcSimpleSingleDimensionGas(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) GasesByDimension {
	return GasesByDimension{
		Computation:       cost,
		StateAccess:       0,
		StateGrowth:       0,
		HistoryGrowth:     0,
		StateGrowthRefund: 0,
	}
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
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) GasesByDimension {
	// We do not have access to StateDb.AddressInAccessList and StateDb.SlotInAccessList
	// to check cold storage access directly.
	// Additionally, cold storage access for these address opcodes are handled differently
	// than for other operations like SSTORE or SLOAD.
	// for these opcodes, cold access cost is handled directly in the gas calculation
	// through gasEip2929AccountCheck. This function adds the address to the access list
	// and charges the cold access cost upfront as part of the initial gas calculation,
	// rather than as a separate gas change event, so no OnGasChange event is fired.
	//
	// Therefore, for these opcodes, we do a simple check based on the raw value
	// and we can deduce the dimensions directly from that value.

	if cost == params.ColdAccountAccessCostEIP2929 {
		return GasesByDimension{
			Computation:       params.WarmStorageReadCostEIP2929,
			StateAccess:       params.ColdAccountAccessCostEIP2929 - params.WarmStorageReadCostEIP2929,
			StateGrowth:       0,
			HistoryGrowth:     0,
			StateGrowthRefund: 0,
		}
	}
	return GasesByDimension{
		Computation:       cost,
		StateAccess:       0,
		StateGrowth:       0,
		HistoryGrowth:     0,
		StateGrowthRefund: 0,
	}
}

// calcSLOADGas returns the gas used for the `SLOAD` opcode
// SLOAD reads a slot from the state. It cannot expand the state
func calcSLOADGas(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) GasesByDimension {
	// we don't have access to StateDb.SlotInAccessList
	// so we have to infer whether the slot was cold or warm based on the absolute cost
	// and then deduce the dimensions from that
	if cost == params.ColdSloadCostEIP2929 {
		accessCost := params.ColdSloadCostEIP2929 - params.WarmStorageReadCostEIP2929
		leftOver := cost - accessCost
		return GasesByDimension{
			Computation:       leftOver,
			StateAccess:       accessCost,
			StateGrowth:       0,
			HistoryGrowth:     0,
			StateGrowthRefund: 0,
		}
	}
	return GasesByDimension{
		Computation:       cost,
		StateAccess:       0,
		StateGrowth:       0,
		HistoryGrowth:     0,
		StateGrowthRefund: 0,
	}
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

// calcExtCodeCopyGas returns the gas used
// for the `EXTCODECOPY` opcode, which reads from
// the code of an external contract.
// Hence only state read implications
func calcExtCodeCopyGas(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) GasesByDimension {
	// extcodecody has three components to its gas cost:
	// 1. minimum_word_size = (size + 31) / 32
	// 2. memory_expansion_cost
	// 3. address_access_cost - the access set.
	// gas for extcodecopy is 3 * minimum_word_size + memory_expansion_cost + address_access_cost
	//
	// at time of opcode trace, we know the state of the memory, and stack
	// and we know the total cost of the opcode.
	// therefore, we calculate minimum_word_size, and memory_expansion_cost
	// and observe if the subtraction of cost - memory_expansion_cost - minimum_word_size = 100 or 2600
	// 3*minimum_word_size is always state access
	// if it is 2600, then have 2500 for state access.
	// rest is computation.

	stack := scope.StackData()
	lenStack := len(stack)
	size := stack[lenStack-4].Uint64()   // size in stack position 4
	offset := stack[lenStack-2].Uint64() // destination offset in stack position 2

	// computing the memory gas cost requires knowing a "previous" price
	var emptyMem []byte = make([]byte, 0)
	memoryDataBeforeExtCodeCopyApplied := scope.MemoryData()
	_, lastGasCost, memErr := memoryGasCost(emptyMem, 0, uint64(len(memoryDataBeforeExtCodeCopyApplied)))
	if memErr != nil {
		return GasesByDimension{}
	}
	memoryExpansionCost, _, memErr := memoryGasCost(memoryDataBeforeExtCodeCopyApplied, lastGasCost, size+offset)
	if memErr != nil {
		return GasesByDimension{}
	}
	minimumWordSizeCost := (size + 31) / 32 * 3
	leftOver := cost - memoryExpansionCost - minimumWordSizeCost
	stateAccess := minimumWordSizeCost
	// check if the access set was hot or cold
	if leftOver == params.ColdAccountAccessCostEIP2929 {
		stateAccess += params.ColdAccountAccessCostEIP2929 - params.WarmStorageReadCostEIP2929
	}
	computation := cost - stateAccess
	return GasesByDimension{
		Computation:       computation,
		StateAccess:       stateAccess,
		StateGrowth:       0,
		HistoryGrowth:     0,
		StateGrowthRefund: 0,
	}
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
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) GasesByDimension {

	// todo: implement
	return GasesByDimension{}
}

// calcLogGas returns the gas used for the `LOG0, LOG1, LOG2, LOG3, LOG4` opcodes
// which only grow the history tree.  History does not need to be referenced with
// every block since it does not produce the block hashes. So the cost implications
// of growing it and the prunability of it are not as important as state growth.
// The relevant opcodes here are:
// `LOG0, LOG1, LOG2, LOG3, LOG4`
func calcLogGas(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) GasesByDimension {
	// todo: implement
	return GasesByDimension{}
}

// calcCreateGas returns the gas used for the CREATE set of opcodes
// which do storage growth when the store the newly created contract code.
// the relevant opcodes here are:
// `CREATE, CREATE2`
func calcCreateGas(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) GasesByDimension {
	// todo: implement
	return GasesByDimension{}
}

// calcReadAndStoreCallGas returns the gas used for the `CALL, CALLCODE` opcodes
// which both read from the state to figure out where to jump to and write to the state
// in limited cases, e.g. when CALLing an empty address, which is the equivalent of an
// ether transfer.
// the relevant opcodes here are:
// `CALL, CALLCODE`
func calcReadAndStoreCallGas(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) GasesByDimension {
	// todo: implement
	return GasesByDimension{}
}

// calcSStoreGas returns the gas used for the `SSTORE` opcode
// which writes to the state. There is a whole lot of complexity around
// gas refunds based on the state of the storage slot before and whether
// refunds happened before in this transaction,
// which manipulate the gas cost of this specific opcode.
func calcSStoreGas(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) GasesByDimension {
	// todo: implement
	return GasesByDimension{}
}

// calcSelfDestructGas returns the gas used for the `SELFDESTRUCT` opcode
// which deletes a contract which is a write to the state
func calcSelfDestructGas(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) GasesByDimension {
	// todo: implement
	return GasesByDimension{}
}
