package vm

import (
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

type GasSStoreFuncTestCase struct {
	name             string
	slotInAccessList bool
	originalValue    common.Hash // committed state value
	currentValue     common.Hash // current state value (may differ from original)
	newValue         common.Hash // value to set
	refund           bool        // for recreate slot test, we need to simulate that a refund was added when it was deleted
	expectedMultiGas *multigas.MultiGas
}

func testGasSStoreFuncFuncWithCases(t *testing.T, config *params.ChainConfig, gasSStoreFunc gasFunc, testCases []GasSStoreFuncTestCase) {
	slotKey := common.HexToHash("0x01")
	contractGas := uint64(100000)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			blockCtx := BlockContext{
				BlockNumber: big.NewInt(0), // ensures block 0 is passed into Rules()
			}

			evm := NewEVM(blockCtx, statedb, config, Config{})

			caller := common.Address{}
			contractAddr := common.Address{1}
			contract := NewContract(caller, contractAddr, new(uint256.Int), contractGas, nil)

			stack := newstack()
			mem := NewMemory()

			if tc.slotInAccessList {
				statedb.AddSlotToAccessList(contractAddr, slotKey)
			}

			// Set up state and stack
			if tc.originalValue != (common.Hash{}) {
				statedb.SetState(contractAddr, slotKey, tc.originalValue)
				statedb.Commit(0, false, false)
			}

			statedb.SetState(contractAddr, slotKey, tc.currentValue)

			stack.push(new(uint256.Int).SetBytes(tc.newValue.Bytes())) // y (value)
			stack.push(new(uint256.Int).SetBytes(slotKey.Bytes()))     // x (slot)

			if tc.refund {
				statedb.AddRefund(params.SstoreClearsScheduleRefundEIP2200)
			}

			multiGas, singleGas, err := gasSStoreFunc(evm, contract, stack, mem, 0)

			if err != nil {
				t.Fatalf("Unexpected error for test case %s: %v", tc.name, err)
			}

			if *multiGas != *tc.expectedMultiGas {
				t.Errorf("Expected multi gas %d, got %d for test case: %s",
					tc.expectedMultiGas, multiGas, tc.name)
			}

			expectedSingleGas, overflow := tc.expectedMultiGas.SingleGas()
			if overflow {
				t.Fatalf("Expected single gas overflow for test case %s", tc.name)
			}

			if singleGas != expectedSingleGas {
				t.Errorf("Expected signle gas %d, got %d for test case: %s",
					expectedSingleGas, singleGas, tc.name)
			}
		})
	}
}

func TestMakeGasSStoreFunc(t *testing.T) {
	testCases := []GasSStoreFuncTestCase{
		// NOOP cases (current == value)
		{
			name:             "noop - cold slot access",
			slotInAccessList: false,
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.HexToHash("0x1234"),
			expectedMultiGas: multigas.StorageAccessGas(params.ColdSloadCostEIP2929 + params.WarmStorageReadCostEIP2929),
		},
		{
			name:             "noop - warm slot access",
			slotInAccessList: true,
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.HexToHash("0x1234"),
			expectedMultiGas: multigas.StorageAccessGas(params.WarmStorageReadCostEIP2929),
		},
		// Cases where original == current
		{
			name:             "create slot - warm slot access",
			slotInAccessList: true,
			originalValue:    common.Hash{},
			currentValue:     common.Hash{},
			newValue:         common.HexToHash("0x1234"),
			expectedMultiGas: multigas.StorageGrowthGas(params.SstoreSetGasEIP2200),
		},
		{
			name:             "delete slot - warm access",
			slotInAccessList: true,
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.Hash{},
			expectedMultiGas: multigas.StorageAccessGas(params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929).SetRefund(params.SstoreClearsScheduleRefundEIP2200),
		},
		{
			name:             "update slot - warm access",
			slotInAccessList: true,
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.HexToHash("0x5678"),
			expectedMultiGas: multigas.StorageAccessGas(params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929),
		},
		// Dirty update cases (original != current)
		{
			name:             "dirty update - recreate slot - warm access",
			slotInAccessList: true,
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.Hash{}, // was deleted in current tx
			newValue:         common.HexToHash("0x5678"),
			refund:           true,
			expectedMultiGas: multigas.StorageAccessGas(params.WarmStorageReadCostEIP2929).SetRefund(params.SstoreClearsScheduleRefundEIP2200),
		},
		{
			name:             "dirty update - delete slot - warm access",
			slotInAccessList: true,
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"), // was changed in current tx
			newValue:         common.Hash{},              // delete
			expectedMultiGas: multigas.StorageAccessGas(params.WarmStorageReadCostEIP2929).SetRefund(params.SstoreClearsScheduleRefundEIP2200),
		},
		{
			name:             "dirty update - change non-zero to different non-zero - warm access",
			slotInAccessList: true,
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"),
			newValue:         common.HexToHash("0x9abc"),
			expectedMultiGas: multigas.StorageAccessGas(params.WarmStorageReadCostEIP2929),
		},
		// Reset to original cases (original == value but original != current)
		{
			name:             "reset to original empty slot - warm access",
			slotInAccessList: true,
			originalValue:    common.Hash{},
			currentValue:     common.HexToHash("0x1234"), // was created in current tx
			newValue:         common.Hash{},              // back to original empty
			expectedMultiGas: multigas.StorageAccessGas(params.WarmStorageReadCostEIP2929).SetRefund(params.SstoreSetGasEIP2200 - params.WarmStorageReadCostEIP2929),
		},
		{
			name:             "reset to original existing slot - warm access",
			slotInAccessList: true,
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"), // was changed in current tx
			newValue:         common.HexToHash("0x1234"), // back to original value
			expectedMultiGas: multigas.StorageAccessGas(params.WarmStorageReadCostEIP2929).SetRefund((params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929) - params.WarmStorageReadCostEIP2929),
		},
		{
			name:             "dirty update - create from nothing - warm access",
			slotInAccessList: true,
			originalValue:    common.Hash{},
			currentValue:     common.HexToHash("0x1234"), // was created in current tx
			newValue:         common.HexToHash("0x5678"), // change to different value
			expectedMultiGas: multigas.StorageAccessGas(params.WarmStorageReadCostEIP2929),
		},
	}

	gasSStoreFunc := makeGasSStoreFunc(params.SstoreClearsScheduleRefundEIP2200)
	testGasSStoreFuncFuncWithCases(t, params.TestChainConfig, gasSStoreFunc, testCases)
}

func TestGasSStoreFuncLegacy(t *testing.T) {
	testCases := []GasSStoreFuncTestCase{
		{
			name:             "legacy: create slot 0 => non-zero",
			originalValue:    common.Hash{},
			currentValue:     common.Hash{},
			newValue:         common.HexToHash("0x1234"),
			expectedMultiGas: multigas.StorageGrowthGas(params.SstoreSetGas),
		},
		{
			name:             "legacy: delete slot non-zero => 0",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.Hash{},
			expectedMultiGas: multigas.StorageAccessGas(params.SstoreClearGas).SetRefund(params.SstoreRefundGas),
		},
		{
			name:             "legacy: modify slot non-zero => non-zero",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.HexToHash("0x5678"),
			expectedMultiGas: multigas.StorageAccessGas(params.SstoreResetGas),
		},
	}

	config := *params.TestChainConfig
	config.PetersburgBlock = big.NewInt(0)     // enable legacy path
	config.ConstantinopleBlock = big.NewInt(0) // optional, but realistic

	testGasSStoreFuncFuncWithCases(t, &config, gasSStore, testCases)
}

func TestGasSStoreFuncEip1283(t *testing.T) {
	testCases := []GasSStoreFuncTestCase{
		// NOOP cases (current == value)
		{
			name:             "noop (1)",
			slotInAccessList: false, // No access list in EIP-1283
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.HexToHash("0x1234"),
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreNoopGas),
		},
		// Cases where original == current
		{
			name:             "create slot (2.1.1)",
			originalValue:    common.Hash{},
			currentValue:     common.Hash{},
			newValue:         common.HexToHash("0x1234"),
			expectedMultiGas: multigas.StorageGrowthGas(params.NetSstoreInitGas),
		},
		{
			name:             "delete slot (2.1.2b)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.Hash{},
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreCleanGas).SetRefund(params.NetSstoreClearRefund),
		},
		{
			name:             "write existing slot (2.1.2)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.HexToHash("0x5678"),
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreCleanGas),
		},
		// Dirty update cases (original != current)
		// { // FIXME(NIT-3484): this test case causing negative refund, see https://github.com/OffchainLabs/go-ethereum/pull/484#discussion_r2194774493
		// 	name:             "recreate slot (2.2.1.1)",
		// 	originalValue:    common.HexToHash("0x1234"),
		// 	currentValue:     common.Hash{}, // was deleted in current tx
		// 	newValue:         common.HexToHash("0x5678"),
		// 	refund:           true, // simulate refund from deletion
		// 	expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreDirtyGas).SetRefund(-params.NetSstoreClearRefund),
		// },
		{
			name:             "delete slot (2.2.1.2)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"), // was changed in current tx
			newValue:         common.Hash{},              // delete
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreDirtyGas).SetRefund(params.NetSstoreClearRefund),
		},
		{
			name:             "dirty update (2.2)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"),
			newValue:         common.HexToHash("0x9abc"),
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreDirtyGas),
		},
		// Reset to original cases (original == value but original != current)
		{
			name:             "reset to original inexistent slot (2.2.2.1)",
			originalValue:    common.Hash{},
			currentValue:     common.HexToHash("0x1234"), // was created in current tx
			newValue:         common.Hash{},              // back to original empty
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreDirtyGas).SetRefund(params.NetSstoreResetClearRefund),
		},
		{
			name:             "reset to original existing slot (2.2.2.2)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"), // was changed in current tx
			newValue:         common.HexToHash("0x1234"), // back to original value
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreDirtyGas).SetRefund(params.NetSstoreResetRefund),
		},
		{
			name:             "dirty update - create from nothing (2.2)",
			originalValue:    common.Hash{},
			currentValue:     common.HexToHash("0x1234"), // was created in current tx
			newValue:         common.HexToHash("0x5678"), // change to different value
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreDirtyGas),
		},
	}

	// Modify the chain config to test EIP-1283 path
	config := *params.TestChainConfig
	config.PetersburgBlock = big.NewInt(0).SetUint64(math.MaxUint64) // never activated
	config.ConstantinopleBlock = big.NewInt(0)                       // activates at block 0

	testGasSStoreFuncFuncWithCases(t, &config, gasSStore, testCases)
}

func TestGasSStore4762(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	evm := NewEVM(BlockContext{}, statedb, params.TestChainConfig, Config{})

	caller := common.Address{}
	contractAddr := common.Address{1}
	contractGas := uint64(100000)
	contract := NewContract(caller, contractAddr, new(uint256.Int), contractGas, nil)

	stack := newstack()
	mem := NewMemory()

	// Setup access list and stack
	accessList := state.NewAccessEvents(evm.StateDB.PointCache())
	accessList.AddAccount(caller, false)
	evm.AccessEvents = accessList

	slotKey := common.HexToHash("0xdeadbeef") // any dummy key
	stack.push(new(uint256.Int).SetBytes(slotKey.Bytes()))

	expectedSingleGas := params.WitnessBranchReadCost + params.WitnessChunkReadCost +
		params.WitnessBranchWriteCost + params.WitnessChunkWriteCost
	expectedMultiGas := multigas.StorageAccessGas(expectedSingleGas)

	multiGas, singleGas, err := gasSStore4762(evm, contract, stack, mem, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if *multiGas != *expectedMultiGas {
		t.Errorf("Expected multi gas %d, got %d", expectedMultiGas, multiGas)
	}
	if singleGas != expectedSingleGas {
		t.Errorf("Expected single gas %d, got %d", expectedSingleGas, singleGas)
	}
}
