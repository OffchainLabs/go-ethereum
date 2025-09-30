package core

import (
	"bytes"
	"errors"
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
	statedb.SetCode(contract, code, tracing.CodeChangeUnspecified)
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
		multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.TxGas + 3 + 3}, // IntrinsicGas+PUSH4+PUSH1
		multigas.Pair{Kind: multigas.ResourceKindStorageAccess, Amount: 2100},               // SSTORE
		multigas.Pair{Kind: multigas.ResourceKindStorageGrowth, Amount: 20000},              // SSTORE
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

	floorDataGas, err := FloorDataGas(data)
	if err != nil {
		t.Fatalf("failed to calculate gas: %v", err)
	}
	expectedMultigas := multigas.MultiGasFromPairs(
		multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.TxGas}, // IntrinsicGas
		multigas.Pair{Kind: multigas.ResourceKindL2Calldata, Amount: floorDataGas - params.TxGas},
	)
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
		multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: vm.GasFastestStep * 2},
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

func TestIntrinsicGas(t *testing.T) {
	tests := []struct {
		name               string
		data               []byte
		accessList         types.AccessList
		authList           []types.SetCodeAuthorization
		isContractCreation bool
		isHomestead        bool
		isEIP2028          bool
		isEIP3860          bool
		want               multigas.MultiGas
	}{
		{
			name: "NoData",
			data: []byte{},
			want: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.TxGas},
			),
		},
		{
			name: "NonZeroData",
			data: []byte{1, 2, 3, 4, 5},
			want: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.TxGas},
				multigas.Pair{Kind: multigas.ResourceKindL2Calldata, Amount: 5 * params.TxDataNonZeroGasFrontier},
			),
		},
		{
			name: "ZeroAndNonZeroData",
			data: []byte{0, 1, 0, 2, 0, 3},
			want: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.TxGas},
				multigas.Pair{Kind: multigas.ResourceKindL2Calldata, Amount: 3*params.TxDataZeroGas + 3*params.TxDataNonZeroGasFrontier},
			),
		},
		{
			name:               "ContractCreation",
			data:               []byte{},
			isContractCreation: true,
			isHomestead:        true,
			want: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.TxGasContractCreation},
			),
		},
		{
			name:       "AccessList",
			data:       []byte{},
			accessList: types.AccessList{{Address: common.Address{1}, StorageKeys: []common.Hash{{2}, {3}}}},
			want: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.TxGas},
				multigas.Pair{Kind: multigas.ResourceKindStorageAccess, Amount: params.TxAccessListAddressGas + 2*params.TxAccessListStorageKeyGas},
			),
		},
		{
			name:     "AuthList",
			data:     []byte{},
			authList: []types.SetCodeAuthorization{{}},
			want: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.TxGas},
				multigas.Pair{Kind: multigas.ResourceKindStorageGrowth, Amount: params.CallNewAccountGas},
			),
		},
		{
			name:      "EIP2028",
			data:      []byte{1, 2, 3, 4, 5},
			isEIP2028: true,
			want: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.TxGas},
				multigas.Pair{Kind: multigas.ResourceKindL2Calldata, Amount: 5 * params.TxDataNonZeroGasEIP2028},
			),
		},
		{
			name:               "EIP3860",
			data:               []byte{1, 2, 3, 4, 5},
			isContractCreation: true,
			isEIP3860:          true,
			want: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.TxGas + toWordSize(5)*params.InitCodeWordGas},
				multigas.Pair{Kind: multigas.ResourceKindL2Calldata, Amount: 5 * params.TxDataNonZeroGasFrontier},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IntrinsicMultiGas(tt.data, tt.accessList, tt.authList, tt.isContractCreation, tt.isHomestead, tt.isEIP2028, tt.isEIP3860)
			if err != nil {
				t.Fatalf("unexpected IntrinsicMultiGas() error: got %v", err)
			}
			if got != tt.want {
				t.Errorf("wrong IntrinsicGas() result: got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCallVariantsMultiGas(t *testing.T) {
	const gasLimit = 100_000
	const refinedComputationGas = gasLimit - params.ColdSloadCostEIP2929 - params.SstoreSetGasEIP2200

	// PUSH1 0x01, PUSH1 0x00, SSTORE, STOP
	code := []byte{
		byte(vm.PUSH1), 0x01,
		byte(vm.PUSH1), 0x00,
		byte(vm.SSTORE),
		byte(vm.STOP),
	}

	caller := common.HexToAddress("0xc0ffee0000000000000000000000000000000000")
	delegate := common.HexToAddress("0xdeadbeef00000000000000000000000000000000")
	contractAddr := common.HexToAddress("0xc0de000000000000000000000000000000000000")

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.CreateAccount(caller)
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code, tracing.CodeChangeUnspecified)
	statedb.SetNonce(caller, 1, tracing.NonceChangeUnspecified)
	statedb.Finalise(true)

	header := &types.Header{
		Number:     common.Big0,
		BaseFee:    common.Big0,
		Difficulty: common.Big0,
	}
	evm := vm.NewEVM(NewEVMBlockContext(header, nil, &caller), statedb, params.TestChainConfig, vm.Config{})

	tests := []struct {
		name             string
		callFn           func() (ret []byte, leftOver uint64, mg multigas.MultiGas, err error)
		expectErr        error
		expectedMultiGas multigas.MultiGas
	}{
		{
			name: "callcode_success",
			callFn: func() ([]byte, uint64, multigas.MultiGas, error) {
				return evm.CallCode(caller, contractAddr, nil, gasLimit, uint256.NewInt(0))
			},
			expectErr: nil,
			expectedMultiGas: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: vm.GasFastestStep * 2},
				multigas.Pair{Kind: multigas.ResourceKindStorageAccess, Amount: params.ColdSloadCostEIP2929},
				multigas.Pair{Kind: multigas.ResourceKindStorageGrowth, Amount: params.SstoreSetGasEIP2200},
			),
		},
		{
			name: "delegatecall_success_same_caller",
			callFn: func() ([]byte, uint64, multigas.MultiGas, error) {
				return evm.DelegateCall(caller, caller, contractAddr, nil, gasLimit, uint256.NewInt(0))
			},
			expectErr: nil,
			expectedMultiGas: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.WarmStorageReadCostEIP2929 + 6},
			),
		},
		{
			name: "delegatecall_success_delegate_caller",
			callFn: func() ([]byte, uint64, multigas.MultiGas, error) {
				return evm.DelegateCall(caller, delegate, contractAddr, nil, gasLimit, uint256.NewInt(0))
			},
			expectErr: nil,
			expectedMultiGas: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: vm.GasFastestStep * 2},
				multigas.Pair{Kind: multigas.ResourceKindStorageAccess, Amount: params.ColdSloadCostEIP2929},
				multigas.Pair{Kind: multigas.ResourceKindStorageGrowth, Amount: params.SstoreSetGasEIP2200},
			),
		},
		{
			name: "staticcall_err_write_protection",
			callFn: func() ([]byte, uint64, multigas.MultiGas, error) {
				return evm.StaticCall(caller, contractAddr, nil, gasLimit)
			},
			expectErr: vm.ErrWriteProtection,
			expectedMultiGas: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: refinedComputationGas},
				multigas.Pair{Kind: multigas.ResourceKindStorageAccess, Amount: params.ColdSloadCostEIP2929},
				multigas.Pair{Kind: multigas.ResourceKindStorageGrowth, Amount: params.SstoreSetGasEIP2200},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret, leftover, usedMultiGas, err := tt.callFn()

			if !errors.Is(err, tt.expectErr) {
				t.Fatalf("unexpected error: got %v, want %v", err, tt.expectErr)
			}

			if gasLimit-leftover != usedMultiGas.SingleGas() {
				t.Errorf("mismatch single gas and leftover gas:\n got:  %v\n want: %v", (gasLimit - leftover), usedMultiGas.SingleGas())
			}

			if usedMultiGas != tt.expectedMultiGas {
				t.Errorf("unexpected multigas:\n got:  %v\n want: %v", usedMultiGas, tt.expectedMultiGas)
			}

			if tt.expectErr == nil && len(ret) != 0 {
				t.Errorf("expected empty return data, got: %x", ret)
			}
		})
	}
}
