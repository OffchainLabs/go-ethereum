package native

import (
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
	stateDB tracing.StateDB,
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

// canHaveColdStorageAccess returns true if the opcode can have cold storage access
func canHaveColdStorageAccess(op vm.OpCode) bool {
	// todo make this list fully complete
	switch op {
	case vm.BALANCE, vm.EXTCODESIZE, vm.EXTCODEHASH:
		return true
	case vm.SLOAD:
		return true
	default:
		return false
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
	stateDB tracing.StateDB,
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
	stateDB tracing.StateDB,
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
	stateDB tracing.StateDB,
) GasesByDimension {
	// need access to StateDb.AddressInAccessList and StateDb.SlotInAccessList
	return GasesByDimension{}
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
	stateDB tracing.StateDB,
) GasesByDimension {
	// todo: implement
	return GasesByDimension{}
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
	stateDB tracing.StateDB,
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
	stateDB tracing.StateDB,
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
	stateDB tracing.StateDB,
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
	stateDB tracing.StateDB,
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
	stateDB tracing.StateDB,
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
	stateDB tracing.StateDB,
) GasesByDimension {
	// todo: implement
	return GasesByDimension{}
}
