package vm

import (
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

type GasSStoreFuncTestCase struct {
	name             string             // descriptive name for the test case
	slotInAccessList bool               // whether the slot is in the access list
	originalValue    common.Hash        // committed state value
	currentValue     common.Hash        // current state value (may differ from original)
	newValue         common.Hash        // value to set
	refundValue      uint64             // initial refund value to add (if any)
	expectedMultiGas *multigas.MultiGas // expected multi gas after the operation
	expectedRefund   uint64             // expected refund after the operation, if any
}

func testGasSStoreFuncFuncWithCases(t *testing.T, config *params.ChainConfig, gasSStoreFunc gasFunc, testCases []GasSStoreFuncTestCase) {
	t.Helper()

	slotKey := common.HexToHash("0x01")
	contractGas := uint64(100000)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stateDb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			blockCtx := BlockContext{
				BlockNumber: big.NewInt(0), // ensures block 0 is passed into Rules()
			}

			evm := NewEVM(blockCtx, stateDb, config, Config{})

			caller := common.Address{}
			contractAddr := common.Address{1}
			contract := NewContract(caller, contractAddr, new(uint256.Int), contractGas, nil)

			stack := newstack()
			mem := NewMemory()

			if tc.slotInAccessList {
				stateDb.AddSlotToAccessList(contractAddr, slotKey)
			}

			// Set up state and stack
			if tc.originalValue != (common.Hash{}) {
				stateDb.SetState(contractAddr, slotKey, tc.originalValue)
				stateDb.Commit(0, false, false)
			}

			stateDb.SetState(contractAddr, slotKey, tc.currentValue)

			stack.push(new(uint256.Int).SetBytes(tc.newValue.Bytes())) // y (value)
			stack.push(new(uint256.Int).SetBytes(slotKey.Bytes()))     // x (slot)

			if tc.refundValue > 0 {
				stateDb.AddRefund(tc.refundValue)
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

			if stateDb.GetRefund() != tc.expectedRefund {
				t.Errorf("Expected refund %d, got %d for test case: %s",
					tc.expectedRefund, stateDb.GetRefund(), tc.name)
			}
		})
	}
}

// Modern (Berlin, EIP-2929) SSTORE gas function test with access list
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
			expectedMultiGas: multigas.StorageAccessGas(params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929),
			expectedRefund:   params.SstoreClearsScheduleRefundEIP2200,
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
			refundValue:      params.SstoreClearsScheduleRefundEIP2200,
			expectedMultiGas: multigas.StorageAccessGas(params.WarmStorageReadCostEIP2929),
		},
		{
			name:             "dirty update - delete slot - warm access",
			slotInAccessList: true,
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"), // was changed in current tx
			newValue:         common.Hash{},              // delete
			expectedMultiGas: multigas.StorageAccessGas(params.WarmStorageReadCostEIP2929),
			expectedRefund:   params.SstoreClearsScheduleRefundEIP2200,
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
			expectedMultiGas: multigas.StorageAccessGas(params.WarmStorageReadCostEIP2929),
			expectedRefund:   params.SstoreSetGasEIP2200 - params.WarmStorageReadCostEIP2929,
		},
		{
			name:             "reset to original existing slot - warm access",
			slotInAccessList: true,
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"), // was changed in current tx
			newValue:         common.HexToHash("0x1234"), // back to original value
			expectedMultiGas: multigas.StorageAccessGas(params.WarmStorageReadCostEIP2929),
			expectedRefund:   (params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929) - params.WarmStorageReadCostEIP2929,
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

// Istanbul (EIP-2200) SSTORE gas function test
func TestGasSStoreFuncEip2200(t *testing.T) {
	testCases := []GasSStoreFuncTestCase{
		// (1) NOOP
		{
			name:             "noop (1)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.HexToHash("0x1234"),
			expectedMultiGas: multigas.StorageAccessGas(params.SloadGasEIP2200),
		},
		// (2.1.1) Create slot from 0
		{
			name:             "create slot (2.1.1)",
			originalValue:    common.Hash{},
			currentValue:     common.Hash{},
			newValue:         common.HexToHash("0x1234"),
			expectedMultiGas: multigas.StorageGrowthGas(params.SstoreSetGasEIP2200),
		},
		// (2.1.2b) Delete existing slot (refund)
		{
			name:             "delete slot (2.1.2b)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.Hash{},
			expectedMultiGas: multigas.StorageAccessGas(params.SstoreResetGasEIP2200),
			expectedRefund:   params.SstoreClearsScheduleRefundEIP2200,
		},
		// (2.1.2) Write existing value (no refund)
		{
			name:             "write existing slot (2.1.2)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.HexToHash("0x5678"),
			expectedMultiGas: multigas.StorageAccessGas(params.SstoreResetGasEIP2200),
		},
		// (2.2.1.2) Delete dirty slot
		{
			name:             "delete slot (2.2.1.2)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"),
			newValue:         common.Hash{},
			expectedMultiGas: multigas.StorageAccessGas(params.SloadGasEIP2200),
			expectedRefund:   params.SstoreClearsScheduleRefundEIP2200,
		},
		// (2.2.1.1) Recreate dirty slot
		{
			name:             "recreate slot (2.2.1.1)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.Hash{},
			newValue:         common.HexToHash("0x5678"),
			refundValue:      params.SstoreClearsScheduleRefundEIP2200,
			expectedMultiGas: multigas.StorageAccessGas(params.SloadGasEIP2200),
		},
		// (2.2.2.1) Reset to original empty slot
		{
			name:             "reset to original inexistent slot (2.2.2.1)",
			originalValue:    common.Hash{},
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.Hash{},
			expectedMultiGas: multigas.StorageAccessGas(params.SloadGasEIP2200),
			expectedRefund:   params.SstoreSetGasEIP2200 - params.SloadGasEIP2200,
		},
		// (2.2.2.2) Reset to original value
		{
			name:             "reset to original existing slot (2.2.2.2)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"),
			newValue:         common.HexToHash("0x1234"),
			expectedMultiGas: multigas.StorageAccessGas(params.SloadGasEIP2200),
			expectedRefund:   params.SstoreResetGasEIP2200 - params.SloadGasEIP2200,
		},
		// (2.2) Generic dirty update
		{
			name:             "dirty update (2.2)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"),
			newValue:         common.HexToHash("0x9abc"),
			expectedMultiGas: multigas.StorageAccessGas(params.SloadGasEIP2200),
		},
	}

	testGasSStoreFuncFuncWithCases(t, params.TestChainConfig, gasSStoreEIP2200, testCases)
}

// Constantinople (EIP-1283) SSTORE gas function test
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
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreCleanGas),
			expectedRefund:   params.NetSstoreClearRefund,
		},
		{
			name:             "write existing slot (2.1.2)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x1234"),
			newValue:         common.HexToHash("0x5678"),
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreCleanGas),
		},
		// Dirty update cases (original != current)
		{
			name:             "recreate slot (2.2.1.1)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.Hash{}, // was deleted in current tx
			newValue:         common.HexToHash("0x5678"),
			refundValue:      params.SstoreClearsScheduleRefundEIP2200, // simulate refund from deletion
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreDirtyGas),
		},
		{
			name:             "delete slot (2.2.1.2)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"), // was changed in current tx
			newValue:         common.Hash{},              // delete
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreDirtyGas),
			expectedRefund:   params.NetSstoreClearRefund,
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
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreDirtyGas),
			expectedRefund:   params.NetSstoreResetClearRefund,
		},
		{
			name:             "reset to original existing slot (2.2.2.2)",
			originalValue:    common.HexToHash("0x1234"),
			currentValue:     common.HexToHash("0x5678"), // was changed in current tx
			newValue:         common.HexToHash("0x1234"), // back to original value
			expectedMultiGas: multigas.StorageAccessGas(params.NetSstoreDirtyGas),
			expectedRefund:   params.NetSstoreResetRefund,
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

// Legacy SSTORE gas function test, then we are in Petersburg (removal of EIP-1283) OR Constantinople is not active
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
			expectedMultiGas: multigas.StorageAccessGas(params.SstoreClearGas),
			expectedRefund:   params.SstoreRefundGas,
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

// Statelessness mode (EIP-4762) SSTORE gas function test
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

// CALL gas function test
func TestGasCall(t *testing.T) {
	testCases := []struct {
		name             string
		transfersValue   bool
		valueTransferGas uint64
		targetExists     bool
		targetEmpty      bool
		isEIP158         bool
		isEIP4762        bool
		isSystemCall     bool
		memorySize       uint64
	}{
		{
			name:             "No value transfer, no memory expansion",
			transfersValue:   false,
			valueTransferGas: 80000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        false,
			isSystemCall:     false,
			memorySize:       500,
		},
		{
			name:             "Value transfer to existing account",
			transfersValue:   true,
			valueTransferGas: 70000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        false,
			isSystemCall:     false,
			memorySize:       0,
		},
		{
			name:             "Value transfer to empty account (EIP158)",
			transfersValue:   true,
			valueTransferGas: 60000,
			targetExists:     true,
			targetEmpty:      true,
			isEIP158:         true,
			isEIP4762:        false,
			isSystemCall:     false,
			memorySize:       0,
		},
		{
			name:             "Non-existent account (pre-EIP158)",
			transfersValue:   false,
			valueTransferGas: 55000,
			targetExists:     false,
			targetEmpty:      false,
			isEIP158:         false,
			isEIP4762:        false,
			isSystemCall:     false,
			memorySize:       0,
		},
		{
			name:             "EIP4762 value transfer (not system call)",
			transfersValue:   true,
			valueTransferGas: 45000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        true,
			isSystemCall:     false,
			memorySize:       0,
		},
		{
			name:             "EIP4762 value transfer (system call)",
			transfersValue:   true,
			valueTransferGas: 40000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        true,
			isSystemCall:     true,
			memorySize:       0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stateDb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			stack := newstack()

			// Configure chain rules
			config := *params.TestChainConfig
			if tc.isEIP4762 {
				verkleTime := uint64(0)
				config.VerkleTime = &verkleTime
			}

			blockCtx := BlockContext{
				BlockNumber: big.NewInt(1),
			}
			evm := NewEVM(blockCtx, stateDb, &config, Config{})

			// Set chain rules based on test case
			evm.chainRules.IsEIP158 = tc.isEIP158
			evm.chainRules.IsEIP4762 = tc.isEIP4762

			// Setup target address
			caller := common.Address{1}
			targetAddr := common.Address{2}

			// Setup caller account
			stateDb.CreateAccount(caller)
			stateDb.SetBalance(caller, uint256.NewInt(1000000), tracing.BalanceChangeUnspecified)

			if tc.targetExists {
				if tc.targetEmpty {
					stateDb.CreateAccount(targetAddr)
				} else {
					stateDb.CreateAccount(targetAddr)
					stateDb.SetBalance(targetAddr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
				}
			}

			// Setup contract
			contractGas := uint64(100000)
			contract := NewContract(caller, caller, new(uint256.Int), contractGas, nil)
			contract.IsSystemCall = tc.isSystemCall
			contract.Gas = contractGas

			// Setup stack: [value, address, gas] (bottom to top)
			if tc.transfersValue {
				stack.push(new(uint256.Int).SetUint64(1)) // value
			} else {
				stack.push(new(uint256.Int).SetUint64(0)) // value
			}
			stack.push(new(uint256.Int).SetBytes(targetAddr.Bytes()))   // address
			stack.push(new(uint256.Int).SetUint64(tc.valueTransferGas)) // gas

			// Setup two instances of memory to avoid side effects in `memoryGasCost`
			mem := NewMemory()
			memForExpected := NewMemory()

			// Mock AccessEvents for EIP4762
			if tc.isEIP4762 {
				accessEvents := state.NewAccessEvents(stateDb.PointCache())
				evm.AccessEvents = accessEvents
				// For testing, we'll need to ensure the access events returns the expected gas
				// This might require setting up the state properly or mocking the behavior
			}

			// Initialize expected multi gas with transfer gas value
			expectedMultiGas := multigas.ComputationGas(tc.valueTransferGas)

			// Apply cahin rules
			if tc.transfersValue && !tc.isEIP4762 {
				expectedMultiGas.SafeIncrement(multigas.ResourceKindComputation, params.CallValueTransferGas)
			}

			if tc.isEIP158 {
				if tc.transfersValue && tc.targetEmpty {
					expectedMultiGas.SafeIncrement(multigas.ResourceKindComputation, params.CallNewAccountGas)
				}
			} else if !tc.targetExists {
				expectedMultiGas.SafeIncrement(multigas.ResourceKindComputation, params.CallNewAccountGas)
			}

			// Call `memoryGasCost` to get the memory gas cost
			memoryMultiGas, _, err := memoryGasCost(memForExpected, tc.memorySize)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			expectedMultiGas, _ = expectedMultiGas.SafeAdd(expectedMultiGas, memoryMultiGas)

			if tc.isEIP4762 && tc.transfersValue && !tc.isSystemCall {
				valueTransferGas := evm.AccessEvents.ValueTransferGas(contract.Address(), caller)
				overflow := expectedMultiGas.SafeIncrement(multigas.ResourceKindStorageAccess, valueTransferGas)
				if overflow {
					t.Fatalf("Expected multi gas overflow for test case %s", tc.name)
				}
			}

			// Call the function
			multiGas, _, err := gasCall(evm, contract, stack, mem, tc.memorySize)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if *multiGas != *expectedMultiGas {
				t.Errorf("Expected multi gas %d, got %d", expectedMultiGas, multiGas)
			}
		})
	}
}
