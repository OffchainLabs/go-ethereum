package native

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
)

type PrevOpcodeState struct {
	MemoryData []byte
	StackData  []uint256.Int
	Caller     common.Address
	Address    common.Address
	CallValue  *uint256.Int
	CallInput  []byte
}

// Deep copies an opcontext into a PrevOpcodeState
// so you can introspect state changes on a per-opcode level
func DeepCopyOpcodeState(scope tracing.OpContext) *PrevOpcodeState {
	// Create new slices and copy memory data
	memCopy := make([]byte, len(scope.MemoryData()))
	copy(memCopy, scope.MemoryData())

	// Create new slice and copy stack data
	stackData := scope.StackData()
	stackCopy := make([]uint256.Int, len(stackData))
	for i := range stackData {
		stackCopy[i].Set(&stackData[i]) // Assuming uint256.Int has a Set method
	}

	// Create new slice and copy call input
	inputCopy := make([]byte, len(scope.CallInput()))
	copy(inputCopy, scope.CallInput())

	// Deep copy the call value
	var callValueCopy uint256.Int
	if scope.CallValue() != nil {
		callValueCopy.Set(scope.CallValue())
	}

	return &PrevOpcodeState{
		MemoryData: memCopy,
		StackData:  stackCopy,
		Caller:     scope.Caller(),  // Value type, copy is automatic
		Address:    scope.Address(), // Value type, copy is automatic
		CallValue:  &callValueCopy,
		CallInput:  inputCopy,
	}
}

// freePrevOpcodeState helps manage memory by clearing references to allow GC
// to reclaim memory more aggressively. This is particularly useful when we know
// the lifecycle of the data and want to ensure memory is freed as soon as possible.
func freePrevOpcodeState(state *PrevOpcodeState) {
	if state == nil {
		return
	}

	// Clear slice references to allow GC
	state.MemoryData = nil
	state.StackData = nil
	state.CallInput = nil

	// Clear pointer reference
	state.CallValue = nil

	// Address and Caller are value types, no need to clear
}
