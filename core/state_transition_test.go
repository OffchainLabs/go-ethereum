package core

import (
	"testing"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

func TestApplyMessageReturnsMultiGas(t *testing.T) {
	contract := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	code := []byte{
		byte(vm.PUSH4), 0xde, 0xad, 0xbe, 0xef, // value
		byte(vm.PUSH1), 0, // key
		byte(vm.SSTORE),
		byte(vm.STOP),
	}

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.SetCode(contract, code)
	statedb.Finalise(true)

	header := &types.Header{
		Difficulty: common.Big0,
		Number:     common.Big0,
		BaseFee:    common.Big0,
	}
	blockContext := NewEVMBlockContext(header, nil, &common.Address{})
	evm := vm.NewEVM(blockContext, statedb, params.TestChainConfig, vm.Config{})

	msg := &Message{
		From:      params.SystemAddress,
		Value:     common.Big0,
		GasLimit:  30_000_000,
		GasPrice:  common.Big0,
		GasFeeCap: common.Big0,
		GasTipCap: common.Big0,
		To:        &contract,
	}

	gp := new(GasPool)
	gp.SetGas(30_000_000)

	res, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		t.Fatalf("failed to apply tx: %v", err)
	}

	expectedMultigas := multigas.MultiGasFromMap(map[multigas.ResourceKind]uint64{
		multigas.ResourceKindComputation:   3 + 3, // PUSH4+PUSH1
		multigas.ResourceKindStorageAccess: 2100,  // SSTORE
		multigas.ResourceKindStorageGrowth: 20000, // SSTORE
	})
	if got, want := *res.UsedMultiGas, *expectedMultigas; got != want {
		t.Errorf("unexpected multi gas: got %v, want %v", got, want)
	}
}
