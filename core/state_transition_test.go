// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"bytes"
	"errors"
	"strings"
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

func TestFloorDataGas(t *testing.T) {
	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	key1 := common.HexToHash("0xaa")
	key2 := common.HexToHash("0xbb")

	tests := []struct {
		name       string
		amsterdam  bool
		data       []byte
		accessList types.AccessList
		want       uint64
	}{
		{
			name: "pre-amsterdam/empty",
			want: params.TxGas,
		},
		{
			name: "pre-amsterdam/zero-bytes-only",
			data: bytes.Repeat([]byte{0x00}, 100),
			// 100 zero tokens * 10 cost = 1000
			want: params.TxGas + 100*params.TxCostFloorPerToken,
		},
		{
			name: "pre-amsterdam/non-zero-bytes-only",
			data: bytes.Repeat([]byte{0xff}, 100),
			// 100 nz * 4 tokens * 10 cost = 4000
			want: params.TxGas + 100*params.TxTokenPerNonZeroByte*params.TxCostFloorPerToken,
		},
		{
			name: "pre-amsterdam/mixed",
			data: append(bytes.Repeat([]byte{0x00}, 50), bytes.Repeat([]byte{0xff}, 50)...),
			// 50 zero + 50*4 nz = 250 tokens * 10 = 2500
			want: params.TxGas + (50+50*params.TxTokenPerNonZeroByte)*params.TxCostFloorPerToken,
		},
		{
			name: "pre-amsterdam/access-list-ignored",
			data: bytes.Repeat([]byte{0xff}, 10),
			accessList: types.AccessList{
				{Address: addr1, StorageKeys: []common.Hash{key1, key2}},
			},
			// pre-amsterdam: floor calculation does not include access list
			want: params.TxGas + 10*params.TxTokenPerNonZeroByte*params.TxCostFloorPerToken,
		},
		{
			name:      "amsterdam/empty",
			amsterdam: true,
			want:      params.TxGas,
		},
		{
			name:      "amsterdam/data-only",
			amsterdam: true,
			data:      bytes.Repeat([]byte{0x00}, 1024),
			// post-amsterdam: every byte = 4 tokens regardless of value
			want: params.TxGas + 1024*params.TxTokenPerNonZeroByte*params.TxCostFloorPerToken7976,
		},
		{
			name:      "amsterdam/data-non-zero",
			amsterdam: true,
			data:      bytes.Repeat([]byte{0xff}, 1024),
			// same as zero data post-amsterdam
			want: params.TxGas + 1024*params.TxTokenPerNonZeroByte*params.TxCostFloorPerToken7976,
		},
		{
			name:      "amsterdam/access-list-addresses-only",
			amsterdam: true,
			accessList: types.AccessList{
				{Address: addr1},
				{Address: addr2},
			},
			// 2 * 20 bytes * 4 tokens/byte * 16 cost/token
			want: params.TxGas + 2*common.AddressLength*params.TxTokenPerNonZeroByte*params.TxCostFloorPerToken7976,
		},
		{
			name:      "amsterdam/access-list-with-storage-keys",
			amsterdam: true,
			accessList: types.AccessList{
				{Address: addr1, StorageKeys: []common.Hash{key1, key2}},
			},
			// 1 addr * 20 * 4 + 2 keys * 32 * 4 = 80 + 256 = 336 tokens * 16
			want: params.TxGas + (1*common.AddressLength+2*common.HashLength)*params.TxTokenPerNonZeroByte*params.TxCostFloorPerToken7976,
		},
		{
			name:      "amsterdam/mixed",
			amsterdam: true,
			data:      bytes.Repeat([]byte{0xff}, 100),
			accessList: types.AccessList{
				{Address: addr1, StorageKeys: []common.Hash{key1}},
				{Address: addr2, StorageKeys: []common.Hash{key1, key2}},
			},
			// data: 100*4 = 400; addrs: 2*20*4 = 160; keys: 3*32*4 = 384; total = 944 * 16
			want: params.TxGas + (100*params.TxTokenPerNonZeroByte+2*common.AddressLength*params.TxTokenPerNonZeroByte+3*common.HashLength*params.TxTokenPerNonZeroByte)*params.TxCostFloorPerToken7976,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := params.Rules{IsAmsterdam: tt.amsterdam}
			got, err := FloorDataGas(rules, tt.data, tt.accessList)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("gas mismatch: got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIntrinsicGas(t *testing.T) {
	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	key1 := common.HexToHash("0xaa")
	key2 := common.HexToHash("0xbb")

	const (
		amsterdamAddressCost    = uint64(common.AddressLength) * params.TxCostFloorPerToken7976 * params.TxTokenPerNonZeroByte // 1280
		amsterdamStorageKeyCost = uint64(common.HashLength) * params.TxCostFloorPerToken7976 * params.TxTokenPerNonZeroByte    // 2048
	)

	tests := []struct {
		name        string
		data        []byte
		accessList  types.AccessList
		authList    []types.SetCodeAuthorization
		creation    bool
		isHomestead bool
		isEIP2028   bool
		isEIP3860   bool
		isAmsterdam bool
		want        vm.GasCosts
	}{
		{
			name: "frontier/empty-call",
			want: vm.GasCosts{RegularGas: params.TxGas},
		},
		{
			name:        "frontier/contract-creation-pre-homestead",
			creation:    true,
			isHomestead: false,
			// pre-homestead, contract creation still uses TxGas
			want: vm.GasCosts{RegularGas: params.TxGas},
		},
		{
			name:        "homestead/contract-creation",
			creation:    true,
			isHomestead: true,
			want:        vm.GasCosts{RegularGas: params.TxGasContractCreation},
		},
		{
			name: "frontier/non-zero-data",
			data: bytes.Repeat([]byte{0xff}, 100),
			// 100 nz bytes * 68 (frontier)
			want: vm.GasCosts{RegularGas: params.TxGas + 100*params.TxDataNonZeroGasFrontier},
		},
		{
			name:      "istanbul/non-zero-data",
			data:      bytes.Repeat([]byte{0xff}, 100),
			isEIP2028: true,
			// 100 nz bytes * 16 (post-EIP2028)
			want: vm.GasCosts{RegularGas: params.TxGas + 100*params.TxDataNonZeroGasEIP2028},
		},
		{
			name:      "istanbul/zero-data",
			data:      bytes.Repeat([]byte{0x00}, 100),
			isEIP2028: true,
			// 100 zero bytes * 4
			want: vm.GasCosts{RegularGas: params.TxGas + 100*params.TxDataZeroGas},
		},
		{
			name:      "istanbul/mixed-data",
			data:      append(bytes.Repeat([]byte{0x00}, 50), bytes.Repeat([]byte{0xff}, 50)...),
			isEIP2028: true,
			want:      vm.GasCosts{RegularGas: params.TxGas + 50*params.TxDataZeroGas + 50*params.TxDataNonZeroGasEIP2028},
		},
		{
			name:        "shanghai/init-code-word-gas",
			data:        bytes.Repeat([]byte{0x00}, 64), // 2 words
			creation:    true,
			isHomestead: true,
			isEIP2028:   true,
			isEIP3860:   true,
			// TxGasContractCreation + 64 zero bytes * 4 + 2 words * 2
			want: vm.GasCosts{RegularGas: params.TxGasContractCreation + 64*params.TxDataZeroGas + 2*params.InitCodeWordGas},
		},
		{
			name:        "shanghai/init-code-non-multiple-of-32",
			data:        bytes.Repeat([]byte{0x00}, 33), // 2 words (rounded up)
			creation:    true,
			isHomestead: true,
			isEIP2028:   true,
			isEIP3860:   true,
			want:        vm.GasCosts{RegularGas: params.TxGasContractCreation + 33*params.TxDataZeroGas + 2*params.InitCodeWordGas},
		},
		{
			name: "berlin/access-list",
			accessList: types.AccessList{
				{Address: addr1, StorageKeys: []common.Hash{key1, key2}},
				{Address: addr2, StorageKeys: []common.Hash{key1}},
			},
			isEIP2028: true,
			// 2 addrs * 2400 + 3 keys * 1900
			want: vm.GasCosts{RegularGas: params.TxGas + 2*params.TxAccessListAddressGas + 3*params.TxAccessListStorageKeyGas},
		},
		{
			name: "amsterdam/access-list-extra-cost",
			accessList: types.AccessList{
				{Address: addr1, StorageKeys: []common.Hash{key1, key2}},
				{Address: addr2, StorageKeys: []common.Hash{key1}},
			},
			isEIP2028:   true,
			isAmsterdam: true,
			// base access-list charge + EIP-7981 extra
			want: vm.GasCosts{RegularGas: params.TxGas +
				2*params.TxAccessListAddressGas + 3*params.TxAccessListStorageKeyGas +
				2*amsterdamAddressCost + 3*amsterdamStorageKeyCost},
		},
		{
			name: "prague/auth-list",
			authList: []types.SetCodeAuthorization{
				{Address: addr1},
				{Address: addr2},
				{Address: addr1},
			},
			isEIP2028: true,
			// 3 auths * 25000 (pre-Amsterdam: CallNewAccountGas per auth tuple)
			want: vm.GasCosts{RegularGas: params.TxGas + 3*params.CallNewAccountGas},
		},
		{
			name:        "amsterdam/contract-creation-empty",
			creation:    true,
			isHomestead: true,
			isEIP2028:   true,
			isAmsterdam: true,
			// EIP-8037: creation regular gas is TxGas + CreateGasAmsterdam (not TxGasContractCreation),
			// and account-creation cost is moved to state gas.
			want: vm.GasCosts{
				RegularGas: params.TxGas + params.CreateGasAmsterdam,
				StateGas:   params.AccountCreationSize * params.CostPerStateByte,
			},
		},
		{
			name:        "amsterdam/contract-creation-init-code",
			data:        bytes.Repeat([]byte{0x00}, 64), // 2 words of init code
			creation:    true,
			isHomestead: true,
			isEIP2028:   true,
			isEIP3860:   true, // Shanghai gates init-code word gas
			isAmsterdam: true,
			want: vm.GasCosts{
				RegularGas: params.TxGas + params.CreateGasAmsterdam +
					64*params.TxDataZeroGas + 2*params.InitCodeWordGas,
				StateGas: params.AccountCreationSize * params.CostPerStateByte,
			},
		},
		{
			name: "amsterdam/contract-creation-with-access-list",
			data: bytes.Repeat([]byte{0xff}, 32), // 1 word of non-zero init code
			accessList: types.AccessList{
				{Address: addr1, StorageKeys: []common.Hash{key1}},
			},
			creation:    true,
			isHomestead: true,
			isEIP2028:   true,
			isEIP3860:   true,
			isAmsterdam: true,
			want: vm.GasCosts{
				RegularGas: params.TxGas + params.CreateGasAmsterdam +
					32*params.TxDataNonZeroGasEIP2028 + 1*params.InitCodeWordGas +
					1*params.TxAccessListAddressGas + 1*params.TxAccessListStorageKeyGas +
					1*amsterdamAddressCost + 1*amsterdamStorageKeyCost,
				StateGas: params.AccountCreationSize * params.CostPerStateByte,
			},
		},
		{
			name: "amsterdam/combined",
			data: bytes.Repeat([]byte{0xff}, 100),
			accessList: types.AccessList{
				{Address: addr1, StorageKeys: []common.Hash{key1}},
			},
			authList: []types.SetCodeAuthorization{
				{Address: addr2},
			},
			isEIP2028:   true,
			isAmsterdam: true,
			// EIP-8037 splits the auth-tuple charge into regular + state gas:
			//   regular: TxAuthTupleRegularGas (7500) per auth
			//   state:   (AuthorizationCreationSize + AccountCreationSize) * CostPerStateByte per auth
			want: vm.GasCosts{
				RegularGas: params.TxGas +
					100*params.TxDataNonZeroGasEIP2028 +
					1*params.TxAccessListAddressGas + 1*params.TxAccessListStorageKeyGas +
					1*amsterdamAddressCost + 1*amsterdamStorageKeyCost +
					1*params.TxAuthTupleRegularGas,
				StateGas: 1 * (params.AuthorizationCreationSize + params.AccountCreationSize) * params.CostPerStateByte,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := params.Rules{
				IsHomestead: tt.isHomestead,
				IsIstanbul:  tt.isEIP2028,
				IsShanghai:  tt.isEIP3860,
				IsAmsterdam: tt.isAmsterdam,
			}
			got, err := IntrinsicGas(tt.data, tt.accessList, tt.authList,
				tt.creation, rules, params.CostPerStateByte)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("gas mismatch: got %+v, want %+v", got, tt.want)
			}
		})
	}
}

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
		Value:     common.U2560,
		GasLimit:  30_000_000,
		GasPrice:  common.U2560,
		GasFeeCap: common.U2560,
		GasTipCap: common.U2560,
		To:        &contract,
	}

	gp := NewGasPool(30_000_000)

	res, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		t.Fatalf("failed to apply tx: %v", err)
	}

	expectedMultigas := multigas.MultiGasFromPairs(
		multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.TxGas + 3 + 3}, // IntrinsicGas+PUSH4+PUSH1
		multigas.Pair{Kind: multigas.ResourceKindStorageAccessRead, Amount: 2100},           // SSTORE cold access
		multigas.Pair{Kind: multigas.ResourceKindStorageGrowth, Amount: 20000},              // SSTORE new slot
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

	// shallow copy the struct, ArbitrumChainParams is copied as a value field,
	// so we can modify it safely without touching the global config instance
	chainConfig := *params.TestChainConfig
	chainConfig.ArbitrumChainParams = params.ArbitrumChainParams{EnableArbOS: true, InitialArbOSVersion: params.ArbosVersion_40}

	evm := vm.NewEVM(blockContext, statedb, &chainConfig, vm.Config{})

	data := bytes.Repeat([]byte{1, 0, 1}, 1000)
	msg := &Message{
		From:      params.SystemAddress,
		Data:      data,
		Value:     common.U2560,
		GasLimit:  30_000_000,
		GasPrice:  common.U2560,
		GasFeeCap: common.U2560,
		GasTipCap: common.U2560,
		To:        &contract,
	}

	gp := NewGasPool(30_000_000)

	res, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		t.Fatalf("failed to apply tx: %v", err)
	}

	rules := chainConfig.Rules(header.Number, true, header.Time, params.ArbosVersion_40)
	floorDataGas, err := FloorDataGas(rules, data, msg.AccessList)
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
		vm.NewGasBudget(gasLimit, 0),
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
		multigas.Pair{Kind: multigas.ResourceKindStorageAccessRead, Amount: params.ColdSloadCostEIP2929},
		multigas.Pair{Kind: multigas.ResourceKindStorageGrowth, Amount: params.SstoreSetGasEIP2200},
	)

	if got, want := usedMultigas, expectedMultigas; got != want {
		t.Errorf("unexpected multigas:\n  got:  %v\n  want: %v", got, want)
	}

	if len(ret) != 0 {
		t.Errorf("expected no return from initcode, got: %x", ret)
	}
	if leftoverGas.RegularGas >= gasLimit {
		t.Errorf("no gas consumed: leftover %d >= limit %d", leftoverGas.RegularGas, gasLimit)
	}
}

func TestIntrinsicMultiGas(t *testing.T) {
	tests := []struct {
		name               string
		data               []byte
		accessList         types.AccessList
		authList           []types.SetCodeAuthorization
		isContractCreation bool
		isHomestead        bool
		isEIP2028          bool
		isEIP3860          bool
		isAmsterdam        bool
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
				multigas.Pair{Kind: multigas.ResourceKindStorageAccessRead, Amount: params.TxAccessListAddressGas + 2*params.TxAccessListStorageKeyGas},
			),
		},
		{
			name:        "AccessListAmsterdam",
			data:        []byte{},
			accessList:  types.AccessList{{Address: common.Address{1}, StorageKeys: []common.Hash{{2}, {3}}}},
			isAmsterdam: true,
			// EIP-7981 charges access-list data on top of the base charge.
			want: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.TxGas},
				multigas.Pair{
					Kind: multigas.ResourceKindStorageAccessRead,
					Amount: params.TxAccessListAddressGas + 2*params.TxAccessListStorageKeyGas +
						(common.AddressLength+2*common.HashLength)*params.TxCostFloorPerToken7976*params.TxTokenPerNonZeroByte,
				},
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
			rules := params.Rules{
				IsHomestead: tt.isHomestead,
				IsIstanbul:  tt.isEIP2028,
				IsShanghai:  tt.isEIP3860,
				IsAmsterdam: tt.isAmsterdam,
			}
			got, err := IntrinsicMultiGas(tt.data, tt.accessList, tt.authList, tt.isContractCreation, rules, params.CostPerStateByte)
			if err != nil {
				t.Fatalf("unexpected IntrinsicMultiGas() error: got %v", err)
			}
			if got != tt.want {
				t.Errorf("wrong IntrinsicMultiGas() result: got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCallVariantsMultiGas(t *testing.T) {
	const gasLimit = 100_000

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
		name              string
		callFn            func() (ret []byte, leftOver vm.GasBudget, mg multigas.MultiGas, err error)
		expectErr         error
		expectErrContains string
		expectedMultiGas  multigas.MultiGas
	}{
		{
			name: "callcode_success",
			callFn: func() ([]byte, vm.GasBudget, multigas.MultiGas, error) {
				return evm.CallCode(caller, contractAddr, nil, vm.NewGasBudget(gasLimit, 0), uint256.NewInt(0))
			},
			expectErr: nil,
			expectedMultiGas: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: vm.GasFastestStep * 2},
				multigas.Pair{Kind: multigas.ResourceKindStorageAccessRead, Amount: params.ColdSloadCostEIP2929},
				multigas.Pair{Kind: multigas.ResourceKindStorageGrowth, Amount: params.SstoreSetGasEIP2200},
			),
		},
		{
			name: "delegatecall_success_same_caller",
			callFn: func() ([]byte, vm.GasBudget, multigas.MultiGas, error) {
				return evm.DelegateCall(caller, caller, contractAddr, nil, vm.NewGasBudget(gasLimit, 0), uint256.NewInt(0))
			},
			expectErr: nil,
			expectedMultiGas: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: params.WarmStorageReadCostEIP2929 + 6},
			),
		},
		{
			name: "delegatecall_success_delegate_caller",
			callFn: func() ([]byte, vm.GasBudget, multigas.MultiGas, error) {
				return evm.DelegateCall(caller, delegate, contractAddr, nil, vm.NewGasBudget(gasLimit, 0), uint256.NewInt(0))
			},
			expectErr: nil,
			expectedMultiGas: multigas.MultiGasFromPairs(
				multigas.Pair{Kind: multigas.ResourceKindComputation, Amount: vm.GasFastestStep * 2},
				multigas.Pair{Kind: multigas.ResourceKindStorageAccessRead, Amount: params.ColdSloadCostEIP2929},
				multigas.Pair{Kind: multigas.ResourceKindStorageGrowth, Amount: params.SstoreSetGasEIP2200},
			),
		},
		{
			name: "staticcall_err_write_protection",
			callFn: func() ([]byte, vm.GasBudget, multigas.MultiGas, error) {
				return evm.StaticCall(caller, contractAddr, nil, vm.NewGasBudget(gasLimit, 0))
			},
			expectErr:         vm.ErrOutOfGas,
			expectErrContains: vm.ErrWriteProtection.Error(),
			// Failed calls consume all remaining gas as computation.
			expectedMultiGas: multigas.ComputationGas(gasLimit),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret, leftover, usedMultiGas, err := tt.callFn()

			if tt.expectErr != nil {
				if err == nil {
					t.Fatalf("unexpected error, got: nil, want: %v", tt.expectErr)
				}
				if !errors.Is(err, tt.expectErr) {
					t.Fatalf("unexpected error, got: %v, want: %v", err, tt.expectErr)
				}
				if tt.expectErrContains != "" && !strings.Contains(err.Error(), tt.expectErrContains) {
					t.Fatalf("unexpected error, got: %v, want it to contain: %v", err, tt.expectErrContains)
				}
			} else if err != nil {
				t.Fatalf("unexpected error, got: %v, want: nil", err)
			}

			if gasLimit-leftover.RegularGas != usedMultiGas.SingleGas() {
				t.Errorf("mismatch single gas and leftover gas:\n got:  %v\n want: %v", (gasLimit - leftover.RegularGas), usedMultiGas.SingleGas())
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

// TestMessageRunContextPredicates pins down the predicate matrix for every
// MessageRunContext constructor. The split between messageSequencingMode
// (filterable) and messageDelayedSequencingMode (not filterable) is the whole
// point of this type, so a regression that e.g. makes IsSequencing return true
// for a delayed context would silently break censorship-resistance for Stylus
// page-limit filtering in arbos/programs/api.go.
func TestMessageRunContextPredicates(t *testing.T) {
	type predicates struct {
		isCommitMode              bool
		isSequencing              bool
		isDelayedSequencing       bool
		isExecutedOnChain         bool
		isGasEstimation           bool
		isEthcall                 bool
		isRecording               bool
		isNonMutating             bool
		expectedMetric            string
		expectNonEmptyWasmTargets bool
		expectedCacheTag          uint32
	}

	tests := []struct {
		name string
		ctx  *MessageRunContext
		want predicates
	}{
		{
			name: "commit",
			ctx:  NewMessageCommitContext(nil),
			want: predicates{
				isCommitMode:              true,
				isExecutedOnChain:         true,
				expectedMetric:            "commit_runmode",
				expectNonEmptyWasmTargets: true,
				expectedCacheTag:          1,
			},
		},
		{
			name: "sequencing",
			ctx:  NewMessageSequencingContext(nil),
			want: predicates{
				isCommitMode:              true,
				isSequencing:              true,
				isExecutedOnChain:         true,
				expectedMetric:            "sequencing_runmode",
				expectNonEmptyWasmTargets: true,
				expectedCacheTag:          1,
			},
		},
		{
			name: "delayed_sequencing",
			ctx:  NewMessageDelayedSequencingContext(nil),
			want: predicates{
				isCommitMode:              true,
				isDelayedSequencing:       true,
				isExecutedOnChain:         true,
				expectedMetric:            "delayed_sequencing_runmode",
				expectNonEmptyWasmTargets: true,
				expectedCacheTag:          1,
			},
		},
		{
			name: "replay",
			ctx:  NewMessageReplayContext(),
			want: predicates{
				isExecutedOnChain:         true,
				expectedMetric:            "replay_runmode",
				expectNonEmptyWasmTargets: true,
			},
		},
		{
			name: "recording",
			ctx:  NewMessageRecordingContext(nil),
			want: predicates{
				isExecutedOnChain:         true,
				isRecording:               true,
				expectedMetric:            "recording_runmode",
				expectNonEmptyWasmTargets: true,
			},
		},
		{
			name: "ethcall",
			ctx:  NewMessageEthcallContext(),
			want: predicates{
				isEthcall:                 true,
				isNonMutating:             true,
				expectedMetric:            "eth_call_runmode",
				expectNonEmptyWasmTargets: true,
			},
		},
		{
			name: "gas_estimation",
			ctx:  NewMessageGasEstimationContext(),
			want: predicates{
				isGasEstimation:           true,
				isNonMutating:             true,
				expectedMetric:            "gas_estimation_runmode",
				expectNonEmptyWasmTargets: true,
			},
		},
	}

	metricNames := make(map[string]struct{}, len(tests))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ctx.IsCommitMode(); got != tt.want.isCommitMode {
				t.Errorf("IsCommitMode() = %v, want %v", got, tt.want.isCommitMode)
			}
			if got := tt.ctx.IsSequencing(); got != tt.want.isSequencing {
				t.Errorf("IsSequencing() = %v, want %v", got, tt.want.isSequencing)
			}
			if got := tt.ctx.IsDelayedSequencing(); got != tt.want.isDelayedSequencing {
				t.Errorf("IsDelayedSequencing() = %v, want %v", got, tt.want.isDelayedSequencing)
			}
			if got := tt.ctx.IsExecutedOnChain(); got != tt.want.isExecutedOnChain {
				t.Errorf("IsExecutedOnChain() = %v, want %v", got, tt.want.isExecutedOnChain)
			}
			if got := tt.ctx.IsGasEstimation(); got != tt.want.isGasEstimation {
				t.Errorf("IsGasEstimation() = %v, want %v", got, tt.want.isGasEstimation)
			}
			if got := tt.ctx.IsEthcall(); got != tt.want.isEthcall {
				t.Errorf("IsEthcall() = %v, want %v", got, tt.want.isEthcall)
			}
			if got := tt.ctx.IsRecording(); got != tt.want.isRecording {
				t.Errorf("IsRecording() = %v, want %v", got, tt.want.isRecording)
			}
			if got := tt.ctx.IsNonMutating(); got != tt.want.isNonMutating {
				t.Errorf("IsNonMutating() = %v, want %v", got, tt.want.isNonMutating)
			}
			if got := tt.ctx.RunModeMetricName(); got != tt.want.expectedMetric {
				t.Errorf("RunModeMetricName() = %q, want %q", got, tt.want.expectedMetric)
			}
			if got := tt.ctx.WasmCacheTag(); got != tt.want.expectedCacheTag {
				t.Errorf("WasmCacheTag() = %d, want %d", got, tt.want.expectedCacheTag)
			}
			if gotNonEmpty := len(tt.ctx.WasmTargets()) > 0; gotNonEmpty != tt.want.expectNonEmptyWasmTargets {
				t.Errorf("len(WasmTargets()) non-empty = %v, want %v", gotNonEmpty, tt.want.expectNonEmptyWasmTargets)
			}

			if _, dup := metricNames[tt.want.expectedMetric]; dup {
				t.Errorf("duplicate RunModeMetricName: %q", tt.want.expectedMetric)
			}
			metricNames[tt.want.expectedMetric] = struct{}{}
		})
	}

	// Mutual exclusion: a sequencing context must not also be a delayed-sequencing
	// context, and vice versa. This is the invariant that api.go's FilterTx guard
	// relies on.
	seq := NewMessageSequencingContext(nil)
	del := NewMessageDelayedSequencingContext(nil)
	if seq.IsDelayedSequencing() {
		t.Error("NewMessageSequencingContext must not satisfy IsDelayedSequencing")
	}
	if del.IsSequencing() {
		t.Error("NewMessageDelayedSequencingContext must not satisfy IsSequencing")
	}
}

// endTxNowHook ends the state transition immediately from StartTxHook while
// reporting nonzero gas, mimicking Arbitrum's submit-retryable path.
type endTxNowHook struct {
	vm.DefaultTxProcessor
	gas uint64
}

func (h endTxNowHook) StartTxHook() (bool, multigas.MultiGas, error, []byte) {
	return true, multigas.SingleDimGas(h.gas), nil, nil
}

// TestEndTxNowGasPoolAccounting checks that gas reported by an endTxNow
// transaction (e.g. an Arbitrum retryable submission) is accounted for
// consistently in the block gas pool: gp.Used() feeds ProcessResult.GasUsed
// (validated against header.GasUsed in ValidateState) and gp.CumulativeUsed()
// feeds receipt.CumulativeGasUsed, so both must include the hook's gas.
func TestEndTxNowGasPoolAccounting(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	header := &types.Header{
		Difficulty: common.Big0,
		Number:     common.Big0,
		BaseFee:    common.Big0,
	}
	evm := vm.NewEVM(NewEVMBlockContext(header, nil, &common.Address{}), statedb, params.TestChainConfig, vm.Config{})

	const hookGas = 50_000
	evm.ProcessingHook = endTxNowHook{gas: hookGas}

	to := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	msg := &Message{
		From:      params.SystemAddress,
		To:        &to,
		Value:     common.U2560,
		GasLimit:  1_000_000,
		GasPrice:  common.U2560,
		GasFeeCap: common.U2560,
		GasTipCap: common.U2560,
	}
	gp := NewGasPool(30_000_000)

	res, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		t.Fatalf("failed to apply message: %v", err)
	}
	if res.UsedGas != hookGas {
		t.Errorf("result.UsedGas = %v, want %v", res.UsedGas, hookGas)
	}
	if gp.CumulativeUsed() != hookGas {
		t.Errorf("gp.CumulativeUsed() = %v, want %v", gp.CumulativeUsed(), hookGas)
	}
	if gp.Used() != hookGas {
		t.Errorf("gp.Used() = %v, want %v (endTxNow gas missing from pool accounting)", gp.Used(), hookGas)
	}
}
