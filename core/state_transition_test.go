package core

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
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

	expectedMultigas := multigas.MultiGasFromPairs(
		multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: 3 + 3},   // PUSH4+PUSH1
		multigas.Pair{Kind: multigas.ResourceKindStorageAccess, Amount: 2100},  // SSTORE
		multigas.Pair{Kind: multigas.ResourceKindStorageGrowth, Amount: 20000}, // SSTORE
	)
	if got, want := res.UsedMultiGas, expectedMultigas; got != want {
		t.Errorf("unexpected multi gas: got %v, want %v", got, want)
	}
}

func TestApplyMessageCalldataReturnsMultiGas(t *testing.T) {
	contract := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	header := &types.Header{
		Difficulty: common.Big1,
		Number:     common.Big0,
		BaseFee:    common.Big0,
	}
	types.HeaderInfo{ArbOSFormatVersion: params.ArbosVersion_40}.UpdateHeaderWithInfo(header)

	blockContext := NewEVMBlockContext(header, nil, &common.Address{})

	chainConfig := params.TestChainConfig
	chainConfig.ArbitrumChainParams = params.ArbitrumChainParams{EnableArbOS: true, InitialArbOSVersion: params.ArbosVersion_40}

	evm := vm.NewEVM(blockContext, statedb, chainConfig, vm.Config{})

	data := bytes.Repeat([]byte{1, 0, 1}, 1000)
	msg := &Message{
		From:      params.SystemAddress,
		Data:      data,
		Value:     common.Big0,
		GasLimit:  30_000_000,
		GasPrice:  common.Big0,
		GasFeeCap: common.Big0,
		GasTipCap: common.Big0,
		To:        &contract,
	}

	gp := new(GasPool).AddGas(30_000_000)

	res, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		t.Fatalf("failed to apply tx: %v", err)
	}

	gas, err := FloorDataGas(data)
	if err != nil {
		t.Fatalf("failed to calculate gas: %v", err)
	}
	expectedMultigas := multigas.L2CalldataGas(gas)
	if got, want := res.UsedMultiGas, expectedMultigas; got != want {
		t.Errorf("unexpected multi gas: got %v, want %v", got, want)
	}
}

func TestCreateReturnsMultiGas(t *testing.T) {
	creator := common.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	initcode := []byte{
		byte(vm.PUSH1), 0x01,
		byte(vm.PUSH1), 0x00,
		byte(vm.SSTORE),
		byte(vm.STOP),
	}

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(creator)
	statedb.SetNonce(creator, 1, tracing.NonceChangeUnspecified)

	header := &types.Header{
		Number:     common.Big0,
		BaseFee:    common.Big0,
		Difficulty: common.Big0,
	}
	evm := vm.NewEVM(NewEVMBlockContext(header, nil, &creator), statedb, params.TestChainConfig, vm.Config{})

	gasLimit := uint64(500_000)
	value := uint256.NewInt(0)

	ret, _, leftoverGas, usedMultigas, err := evm.Create(
		creator,
		initcode,
		gasLimit,
		value,
	)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Expected gas dimensions:
	// - PUSH1 (x2): 2 * 3 = 6 computation
	// - SSTORE (new slot): 2100 access + 20000 growth
	expectedMultigas := multigas.MultiGasFromPairs(
		multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: 6},
		multigas.Pair{Kind: multigas.ResourceKindStorageAccess, Amount: params.ColdSloadCostEIP2929},
		multigas.Pair{Kind: multigas.ResourceKindStorageGrowth, Amount: params.SstoreSetGasEIP2200},
	)

	if got, want := usedMultigas, expectedMultigas; got != want {
		t.Errorf("unexpected multigas:\n  got:  %v\n  want: %v", got, want)
	}

	if len(ret) != 0 {
		t.Errorf("expected no return from initcode, got: %x", ret)
	}
	if leftoverGas >= gasLimit {
		t.Errorf("no gas consumed: leftover %d >= limit %d", leftoverGas, gasLimit)
	}
}
