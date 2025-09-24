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

func TestOpCallsMultiGas(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	blockCtx := BlockContext{
		CanTransfer: func(db StateDB, addr common.Address, amount *uint256.Int) bool {
			return true
		},
		Transfer: func(db StateDB, from, to common.Address, amount *uint256.Int) {},
		GetHash:  func(uint64) common.Hash { return common.Hash{} },
	}
	evm := NewEVM(blockCtx, statedb, params.TestChainConfig, Config{})

	var (
		contractAddr = common.HexToAddress("0xc0de000000000000000000000000000000000000")
		caller       = common.HexToAddress("0xc0ffee0000000000000000000000000000000000")
		gasLimit     = uint64(100_000)
	)

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
				stack.push(uint256.NewInt(gasLimit))
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
				stack.push(uint256.NewInt(gasLimit))
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
				stack.push(uint256.NewInt(gasLimit))
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
				stack.push(uint256.NewInt(gasLimit))
			},
			opFn: opStaticCall,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := &ScopeContext{
				Contract: NewContract(caller, contractAddr, uint256.NewInt(0), gasLimit, nil),
				Stack:    newstack(),
				Memory:   NewMemory(),
			}
			tt.pushStack(scope.Stack)

			pc := uint64(0)
			ret, err := tt.opFn(&pc, evm, scope)
			require.NoError(t, err)

			// Stack top must be 1 (success)
			require.Equal(t, uint64(1), scope.Stack.peek().Uint64())

			// Multigas: assertaions
			// - callGasTemp should be fully retained (no gas refund on call)
			// - no used multigas for empty callee
			// - net multigas should be zero
			require.Equal(t, evm.callGasTemp,
				scope.Contract.RetainedMultiGas.Get(multigas.ResourceKindComputation),
				"expected retained callGasTemp")
			require.True(t, scope.Contract.UsedMultiGas.IsZero(),
				"expected no used multigas for empty callee")
			require.True(t, scope.Contract.GetTotalUsedMultiGas().IsZero(),
				"expected net multigas to be zero")

			// Return data should be empty
			require.Len(t, ret, 0)
		})
	}
}
