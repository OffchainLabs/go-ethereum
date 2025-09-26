package vm

import (
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

func TestOpCreatesMultiGas(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	blockCtx := BlockContext{
		CanTransfer: func(db StateDB, addr common.Address, amount *uint256.Int) bool { return true },
		Transfer:    func(db StateDB, from, to common.Address, amount *uint256.Int) {},
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
	}

	var (
		caller     = common.HexToAddress("0xc0ffee0000000000000000000000000000000000")
		initialGas = uint64(100_000)
	)

	evm := NewEVM(blockCtx, statedb, params.TestChainConfig, Config{})

	tests := []struct {
		name      string
		pushStack func(stack *Stack)
		opFn      func(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error)
	}{
		{
			name: "opCreate",
			pushStack: func(stack *Stack) {
				// value, offset, size
				stack.push(uint256.NewInt(0)) // endowment
				stack.push(uint256.NewInt(0)) // offset
				stack.push(uint256.NewInt(0)) // size
			},
			opFn: opCreate,
		},
		{
			name: "opCreate2",
			pushStack: func(stack *Stack) {
				// value, offset, size, salt
				stack.push(uint256.NewInt(0)) // endowment
				stack.push(uint256.NewInt(0)) // offset
				stack.push(uint256.NewInt(0)) // size
				stack.push(uint256.NewInt(0)) // salt
			},
			opFn: opCreate2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := &ScopeContext{
				Contract: NewContract(caller, common.Address{}, uint256.NewInt(0), initialGas, nil),
				Stack:    newstack(),
				Memory:   NewMemory(),
			}
			tt.pushStack(scope.Stack)

			pc := uint64(0)
			_, err := tt.opFn(&pc, evm, scope)
			require.NoError(t, err)

			// Top of stack must be nonzero (addr) or 0 on failure.
			require.NotNil(t, scope.Stack.peek())

			require.True(t, scope.Contract.RetainedMultiGas.IsZero())
			total := scope.Contract.GetTotalUsedMultiGas()
			require.Equal(t, total, scope.Contract.UsedMultiGas)

			require.LessOrEqual(t, total.SingleGas(), initialGas)
		})
	}
}

func TestOpCallsMultiGas(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	blockCtx := BlockContext{
		CanTransfer: func(db StateDB, addr common.Address, amount *uint256.Int) bool {
			return true
		},
		Transfer: func(db StateDB, from, to common.Address, amount *uint256.Int) {},
		GetHash:  func(uint64) common.Hash { return common.Hash{} },
	}

	var (
		contractAddr = common.HexToAddress("0xc0de000000000000000000000000000000000000")
		caller       = common.HexToAddress("0xc0ffee0000000000000000000000000000000000")
		initialGas   = uint64(100_000)
		callGasTemp  = uint64(55_000)
	)

	evm := NewEVM(blockCtx, statedb, params.TestChainConfig, Config{})
	evm.callGasTemp = callGasTemp

	tests := []struct {
		name      string
		pushStack func(stack *Stack)
		opFn      func(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error)
	}{
		{
			name: "opCall",
			pushStack: func(stack *Stack) {
				// retSize, retOffset, inSize, inOffset, value, addr, gas
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(new(uint256.Int).SetBytes(contractAddr.Bytes()))
				stack.push(uint256.NewInt(initialGas))
			},
			opFn: opCall,
		},
		{
			name: "opCallCode",
			pushStack: func(stack *Stack) {
				// retSize, retOffset, inSize, inOffset, value, addr, gas
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(new(uint256.Int).SetBytes(contractAddr.Bytes()))
				stack.push(uint256.NewInt(initialGas))
			},
			opFn: opCallCode,
		},
		{
			name: "opDelegateCall",
			pushStack: func(stack *Stack) {
				// retSize, retOffset, inSize, inOffset, addr, gas
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(new(uint256.Int).SetBytes(contractAddr.Bytes()))
				stack.push(uint256.NewInt(initialGas))
			},
			opFn: opDelegateCall,
		},
		{
			name: "opStaticCall",
			pushStack: func(stack *Stack) {
				// retSize, retOffset, inSize, inOffset, addr, gas
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(uint256.NewInt(0))
				stack.push(new(uint256.Int).SetBytes(contractAddr.Bytes()))
				stack.push(uint256.NewInt(initialGas))
			},
			opFn: opStaticCall,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := &ScopeContext{
				Contract: NewContract(caller, contractAddr, uint256.NewInt(0), initialGas, nil),
				Stack:    newstack(),
				Memory:   NewMemory(),
			}
			tt.pushStack(scope.Stack)

			// Preset like in gas table
			scope.Contract.UsedMultiGas.SaturatingIncrementInto(multigas.ResourceKindComputation, callGasTemp)

			pc := uint64(0)
			ret, err := tt.opFn(&pc, evm, scope)
			require.NoError(t, err)

			// Stack top must be 1 (success)
			require.Equal(t, uint64(1), scope.Stack.peek().Uint64())
			require.Len(t, ret, 0)

			// Expect retain
			require.Equal(t, callGasTemp, scope.Contract.RetainedMultiGas.Get(multigas.ResourceKindComputation))

			// Total multigas should be annihilated
			totalMultiGas := scope.Contract.GetTotalUsedMultiGas()
			require.Equal(t, scope.Contract.Gas-callGasTemp, initialGas-totalMultiGas.SingleGas()) // No gas used
			require.Equal(t, uint64(0), totalMultiGas.SingleGas())
		})
	}
}
