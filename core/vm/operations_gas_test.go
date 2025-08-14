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

			multiGas, err := gasSStoreFunc(evm, contract, stack, mem, 0)

			if err != nil {
				t.Fatalf("Unexpected error for test case %s: %v", tc.name, err)
			}

			if *multiGas != *tc.expectedMultiGas {
				t.Errorf("Expected multi gas %d, got %d for test case: %s",
					tc.expectedMultiGas, multiGas, tc.name)
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

	expectedStorageAccessGas := params.WitnessBranchReadCost + params.WitnessChunkReadCost +
		params.WitnessBranchWriteCost + params.WitnessChunkWriteCost
	expectedMultiGas := multigas.StorageAccessGas(expectedStorageAccessGas)

	multiGas, err := gasSStore4762(evm, contract, stack, mem, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if *multiGas != *expectedMultiGas {
		t.Errorf("Expected multi gas %d, got %d", expectedMultiGas, multiGas)
	}
}

type GasCallFuncTestCase struct {
	name             string // descriptive name for the test case
	slotInAccessList bool   // whether the slot is in the access list
	transfersValue   bool   // whether the call transfers value
	valueTransferGas uint64 // gas argument passed on stack
	targetExists     bool   // whether the target account exists
	targetEmpty      bool   // whether the target account is empty (no balance/code)
	isEIP158         bool   // whether EIP-158 rules apply (empty account handling)
	isEIP4762        bool   // whether EIP-4762 rules apply (Verkle trees)
	addWitnessGas    bool   // whether to add witness gas for EIP-4762
	isEIP2929        bool   // whether EIP-2929 rules apply (access lists)
	isSystemCall     bool   // whether this is a system call (bypasses some gas costs)
	memorySize       uint64 // memory size for the operation (triggers memory expansion)
}

func testGasCallFuncFuncWithCases(t *testing.T, config *params.ChainConfig, gasCallFunc gasFunc, testCases []GasCallFuncTestCase, isCallCode bool) {
	t.Helper()

	contractGas := uint64(100000)
	slotKey := common.HexToHash("0xdeadbeef") // any dummy key

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stateDb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			stack := newstack()

			// Configure chain rules
			if tc.isEIP4762 {
				verkleTime := uint64(0)
				config.VerkleTime = &verkleTime
			}

			blockCtx := BlockContext{
				BlockNumber: big.NewInt(1),
			}
			evm := NewEVM(blockCtx, stateDb, config, Config{})

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

			if tc.slotInAccessList {
				stateDb.AddSlotToAccessList(targetAddr, slotKey)
			}

			// Setup contract
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
			}

			// Initialize expected multi gas with transfer gas value
			expectedMultiGas := multigas.ComputationGas(tc.valueTransferGas)

			// For EIP-2929 (access lists)
			wasColdAccess := !tc.isEIP2929 || !evm.StateDB.AddressInAccessList(targetAddr)
			if tc.isEIP2929 && wasColdAccess && !tc.slotInAccessList {
				expectedMultiGas.SafeIncrement(multigas.ResourceKindStorageAccess, params.ColdAccountAccessCostEIP2929-params.WarmStorageReadCostEIP2929)
			}

			// Apply cahin rules
			if tc.transfersValue && !tc.isEIP4762 {
				expectedMultiGas.SafeIncrement(multigas.ResourceKindComputation, params.CallValueTransferGas)
			}

			// Account creation gas only applies to gasCall, not gasCallCode
			if !isCallCode && wasColdAccess {
				if tc.isEIP158 {
					if tc.transfersValue && tc.targetEmpty {
						expectedMultiGas.SafeIncrement(multigas.ResourceKindStorageGrowth, params.CallNewAccountGas)
					}
				} else if !tc.targetExists {
					expectedMultiGas.SafeIncrement(multigas.ResourceKindStorageGrowth, params.CallNewAccountGas)
				}
			}

			// Call `memoryGasCost` to get the memory gas cost
			memoryMultiGas, err := memoryGasCost(memForExpected, tc.memorySize)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			expectedMultiGas, _ = expectedMultiGas.SafeAdd(expectedMultiGas, memoryMultiGas)

			// EIP4762 storage access gas for value transfers
			if tc.isEIP4762 && tc.transfersValue && !tc.isSystemCall {
				valueTransferGas := evm.AccessEvents.ValueTransferGas(contract.Address(), caller)
				if overflow := expectedMultiGas.SafeIncrement(multigas.ResourceKindStorageAccess, valueTransferGas); overflow {
					t.Fatalf("Expected multi gas overflow for test case %s", tc.name)
				}
			}

			// For EIP-4762 (Witnesses gas)
			if tc.addWitnessGas && !contract.IsSystemCall {
				// Calculated in `touchAddressAndChargeGas` WitnessBranchReadCost + WitnessChunkReadCost = 2100
				expectedMultiGas.SafeIncrement(multigas.ResourceKindStorageAccess, 2100)
			}

			// Call the function
			multiGas, err := gasCallFunc(evm, contract, stack, mem, tc.memorySize)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if *multiGas != *expectedMultiGas {
				t.Errorf("Expected multi gas %d, got %d", expectedMultiGas, multiGas)
			}
		})
	}
}

// CALL gas function test
func TestGasCall(t *testing.T) {
	testCases := []GasCallFuncTestCase{
		{
			name:             "No value transfer with memory expansion",
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
		{
			name:             "EIP4762 no value transfer with memory",
			transfersValue:   false,
			valueTransferGas: 35000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        true,
			isSystemCall:     false,
			memorySize:       256,
		},
	}
	testGasCallFuncFuncWithCases(t, params.TestChainConfig, gasCall, testCases, false)
}

// CALLCODE gas function test
func TestGasCallCode(t *testing.T) {
	// NOTE: targetExists/targetEmpty fields don't affect gasCallCode but kept for consistency
	testCases := []GasCallFuncTestCase{
		{
			name:             "No value transfer with memory expansion",
			transfersValue:   false,
			valueTransferGas: 80000,
			memorySize:       500,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        false,
			isSystemCall:     false,
		},
		{
			name:             "Value transfer (pre-EIP4762)",
			transfersValue:   true,
			valueTransferGas: 70000,
			memorySize:       0,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        false,
			isSystemCall:     false,
		},
		{
			name:             "Value transfer with memory expansion",
			transfersValue:   true,
			valueTransferGas: 65000,
			memorySize:       256,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        false,
			isSystemCall:     false,
		},
		{
			name:             "EIP4762 value transfer (not system call)",
			transfersValue:   true,
			valueTransferGas: 45000,
			memorySize:       0,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        true,
			isSystemCall:     false,
		},
		{
			name:             "EIP4762 value transfer (system call)",
			transfersValue:   true,
			valueTransferGas: 40000,
			memorySize:       0,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        true,
			isSystemCall:     true,
		},
		{
			name:             "EIP4762 no value transfer with memory",
			transfersValue:   false,
			valueTransferGas: 50000,
			memorySize:       128,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        true,
			isSystemCall:     false,
		},
		{
			name:             "Pre-EIP158 with value transfer",
			transfersValue:   true,
			valueTransferGas: 55000,
			memorySize:       0,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         false,
			isEIP4762:        false,
			isSystemCall:     false,
		},
	}

	testGasCallFuncFuncWithCases(t, params.TestChainConfig, gasCallCode, testCases, true)
}

// CALL decorated with makeCallVariantGasCallEIP2929 gas function test
func TestCallVariantGasCallEIP2929(t *testing.T) {
	testCases := []GasCallFuncTestCase{
		{
			name:             "Cold access to existing account",
			slotInAccessList: false,
			transfersValue:   false,
			valueTransferGas: 80000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        false,
			isEIP2929:        true,
			isSystemCall:     false,
			memorySize:       0,
		},
		{
			name:             "Warm access (repeated)",
			slotInAccessList: true,
			transfersValue:   false,
			valueTransferGas: 80000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        false,
			isEIP2929:        true,
			isSystemCall:     false,
			memorySize:       0,
		},
	}

	gasCallEIP2929 = makeCallVariantGasCallEIP2929(gasCall, 1)
	testGasCallFuncFuncWithCases(t, params.TestChainConfig, gasCallEIP2929, testCases, false)
}

func TestVariantGasEIP4762(t *testing.T) {
	testCases := []GasCallFuncTestCase{
		{
			name:             "EIP4762 non-system call with witness cost",
			transfersValue:   false,
			valueTransferGas: 50000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        true,
			addWitnessGas:    true,
			isSystemCall:     false,
			memorySize:       64,
		},
		{
			name:             "EIP4762 system call skips witness cost",
			transfersValue:   false,
			valueTransferGas: 50000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP4762:        true,
			addWitnessGas:    true,
			isSystemCall:     true,
			memorySize:       64,
		},
	}

	gasCallEIP4762 = makeCallVariantGasEIP4762(gasCallCode)
	testGasCallFuncFuncWithCases(t, params.TestChainConfig, gasCallEIP4762, testCases, true)
}

func TestCallVariantGasCallEIP7702(t *testing.T) {
	testCases := []GasCallFuncTestCase{
		{
			name:             "7702 cold access, non-delegate, no value",
			slotInAccessList: false,
			transfersValue:   false,
			valueTransferGas: 50000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP2929:        true,
			isSystemCall:     false,
			memorySize:       128,
		},
		{
			name:             "7702 warm access, with value transfer",
			slotInAccessList: true,
			transfersValue:   true,
			valueTransferGas: 60000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP2929:        true,
			isSystemCall:     false,
			memorySize:       64,
		},
		{
			name:             "7702 cold access, empty account, value transfer",
			slotInAccessList: false,
			transfersValue:   true,
			valueTransferGas: 55000,
			targetExists:     true,
			targetEmpty:      true,
			isEIP158:         true,
			isEIP2929:        true,
			isSystemCall:     false,
			memorySize:       0,
		},
		{
			name:             "7702 no value, warm slot",
			slotInAccessList: true,
			transfersValue:   false,
			valueTransferGas: 45000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP2929:        true,
			isSystemCall:     false,
			memorySize:       32,
		},
		{
			name:             "7702 system call skips access gas",
			slotInAccessList: false,
			transfersValue:   false,
			valueTransferGas: 40000,
			targetExists:     true,
			targetEmpty:      false,
			isEIP158:         true,
			isEIP2929:        true,
			isSystemCall:     true,
			memorySize:       16,
		},
	}

	wrapped := makeCallVariantGasCallEIP7702(gasCall)
	testGasCallFuncFuncWithCases(t, params.TestChainConfig, wrapped, testCases, false)
}

func testGasDelegateOrStaticCall(t *testing.T, gasImplFunc gasFunc) {
	t.Helper()

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	evm := NewEVM(BlockContext{}, statedb, params.TestChainConfig, Config{})

	caller := common.Address{}
	contractAddr := common.Address{1}
	contractGas := uint64(100000)
	contract := NewContract(caller, contractAddr, new(uint256.Int), contractGas, nil)

	stack := newstack()
	mem := NewMemory()
	memForExpected := NewMemory()

	stack.push(new(uint256.Int).SetUint64(50000))
	memorySize := uint64(64)

	expectedMultiGas, err := memoryGasCost(memForExpected, memorySize)
	if err != nil {
		t.Fatalf("Failed memoryGasCost: %v", err)
	}

	callGas, err := callGas(evm.chainRules.IsEIP150, contractGas, expectedMultiGas.SingleGas(), stack.Back(0))
	if err != nil {
		t.Fatalf("Failed callGas: %v", err)
	}

	expectedMultiGas.SafeIncrement(multigas.ResourceKindComputation, callGas)

	// Call gasImplFunc
	multiGas, err := gasImplFunc(evm, contract, stack, mem, memorySize)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if *multiGas != *expectedMultiGas {
		t.Errorf("Expected multi gas %d, got %d", expectedMultiGas, multiGas)
	}
}

func TestGasDelegateCall(t *testing.T) {
	testGasDelegateOrStaticCall(t, gasDelegateCall)
}

func TestGasStaticCall(t *testing.T) {
	testGasDelegateOrStaticCall(t, gasStaticCall)
}

func testGasCreateFunc(t *testing.T, gasImplFunc gasFunc, includeHashCost bool, enableEip3860 bool) {
	t.Helper()

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	chainConfig := params.TestChainConfig
	if enableEip3860 {
		chainConfig.ShanghaiTime = new(uint64) // Enable EIP-3860
		*chainConfig.ShanghaiTime = 0
	}

	evm := NewEVM(BlockContext{}, statedb, chainConfig, Config{})

	caller := common.Address{}
	contractAddr := common.Address{1}
	contractGas := uint64(100000)
	contract := NewContract(caller, contractAddr, new(uint256.Int), contractGas, nil)

	stack := newstack()
	mem := NewMemory()
	memForExpected := NewMemory()

	// Set up test cases based on EIP-3860 requirement
	var testCases []struct {
		name       string
		initSize   uint64
		shouldFail bool
	}

	if enableEip3860 {
		// Test with different init code sizes (within limit) and boundary conditions
		maxInitCodeSize := evm.chainConfig.MaxInitCodeSize()
		testCases = []struct {
			name       string
			initSize   uint64
			shouldFail bool
		}{
			{"small init code", 32, false},
			{"medium init code", 1024, false},
			{"max init code", maxInitCodeSize, false},
			{"over max init code", maxInitCodeSize + 1, true},
		}
	} else {
		// Test with different init code sizes (no limit)
		testCases = []struct {
			name       string
			initSize   uint64
			shouldFail bool
		}{
			{"small init code", 32, false},
			{"medium init code", 1024, false},
			{"large init code", 32768, false},
		}
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup stack
			stack = newstack()
			stack.push(new(uint256.Int).SetUint64(tc.initSize)) // size (stack position 2)
			stack.push(new(uint256.Int).SetUint64(0))           // offset (stack position 1)
			stack.push(new(uint256.Int).SetUint64(0))           // value (stack position 0)

			memorySize := uint64(64)

			// Call gasImplFunc
			multiGas, err := gasImplFunc(evm, contract, stack, mem, memorySize)

			if tc.shouldFail {
				if err == nil {
					t.Fatalf("Expected error for init code size %d, but got none", tc.initSize)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Calculate expected multi-dimensional gas
			expectedMultiGas, err := memoryGasCost(memForExpected, memorySize)
			if err != nil {
				t.Fatalf("Failed memoryGasCost: %v", err)
			}
			// Calculate expected multi-dimensional gas

			wordSize := (tc.initSize + 31) / 32
			var totalComputationCost uint64
			if enableEip3860 {
				// EIP-3860 functions calculate init code cost
				initCodeCost := params.InitCodeWordGas * wordSize
				totalComputationCost = initCodeCost
				if includeHashCost {
					hashCost := params.Keccak256WordGas * wordSize
					totalComputationCost += hashCost
				}
			} else {
				// Regular gasCreate2 only calculates hash cost
				if includeHashCost {
					hashCost := params.Keccak256WordGas * wordSize
					totalComputationCost = hashCost
				}
			}

			expectedMultiGas.SafeIncrement(multigas.ResourceKindComputation, totalComputationCost)

			if *multiGas != *expectedMultiGas {
				t.Errorf("Expected multi gas %+v, got %+v", expectedMultiGas, multiGas)
			}
		})
	}
}

func TestGasCreate2(t *testing.T) {
	testGasCreateFunc(t, gasCreate2, true, false)
}

func TestGasCreateEip3860(t *testing.T) {
	testGasCreateFunc(t, gasCreateEip3860, false, true)
}

func TestGasCreate2Eip3860(t *testing.T) {
	testGasCreateFunc(t, gasCreate2Eip3860, true, true)
}

type GasSelfdestructFuncTestCase struct {
	name              string             // descriptive name for the test case
	isEIP150          bool               // whether the test is for EIP-150
	isEIP158          bool               // whether the test is for EIP-158
	beneficiaryExists bool               // whether beneficiary account exists
	slotInAccessList  bool               // whether the slot is in the access list
	hasBeenDestructed bool               // whether the contract has been destructed before
	expectedMultiGas  *multigas.MultiGas // expected multi gas after the operation
	expectedRefund    uint64             // expected refund after the operation, if any
}

func testGasSelfdestructFuncWithCases(t *testing.T, config *params.ChainConfig, gasSelfdestructFunc gasFunc, testCases []GasSelfdestructFuncTestCase) {
	t.Helper()

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
			beneficiaryAddr := common.Address{2}
			contract := NewContract(caller, contractAddr, new(uint256.Int), contractGas, nil)

			stack := newstack()
			mem := NewMemory()

			// Set chain rules based on test case
			evm.chainRules.IsEIP150 = tc.isEIP150
			evm.chainRules.IsEIP158 = tc.isEIP158

			if tc.beneficiaryExists {
				stateDb.CreateAccount(beneficiaryAddr)
			}

			stateDb.CreateAccount(contractAddr)
			if tc.hasBeenDestructed {
				stateDb.SelfDestruct(contractAddr)
			} else {
				stateDb.SetBalance(contractAddr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
			}

			stack.push(new(uint256.Int).SetBytes(beneficiaryAddr.Bytes()))

			multiGas, err := gasSelfdestructFunc(evm, contract, stack, mem, 0)
			if err != nil {
				t.Fatalf("Unexpected error for test case %s: %v", tc.name, err)
			}

			if *multiGas != *tc.expectedMultiGas {
				t.Errorf("Expected multi gas %d, got %d for test case: %s",
					tc.expectedMultiGas, multiGas, tc.name)
			}

			if stateDb.GetRefund() != tc.expectedRefund {
				t.Errorf("Expected refund %d, got %d for test case: %s",
					tc.expectedRefund, stateDb.GetRefund(), tc.name)
			}
		})
	}
}

// Base SELFDESTRUCT gas function test
func TestGasSelfdestruct(t *testing.T) {
	testCases := []GasSelfdestructFuncTestCase{
		{
			name:             "idle selfdestruct with refund",
			expectedMultiGas: multigas.ZeroGas(),
			expectedRefund:   params.SelfdestructRefundGas,
		},
		{
			name:              "idle selfdestruct without refund",
			expectedMultiGas:  multigas.ZeroGas(),
			beneficiaryExists: true,
			hasBeenDestructed: true,
		},
		{
			name:              "selfdestruct exisit with EIP-150 with refund",
			isEIP150:          true,
			expectedMultiGas:  multigas.StorageAccessGas(params.SelfdestructGasEIP150),
			beneficiaryExists: true,
			expectedRefund:    params.SelfdestructRefundGas,
		},
		{
			name:              "selfdestruct not-exisit with EIP-150 and EIP-158 without refund",
			isEIP150:          true,
			isEIP158:          true,
			expectedMultiGas:  multigas.StorageAccessGas(params.SelfdestructGasEIP150),
			hasBeenDestructed: true,
		},
		{
			name:              "selfdestruct exisit with EIP-150 and EIP-158 with refund",
			beneficiaryExists: false,
			isEIP150:          true,
			isEIP158:          true,
			expectedMultiGas: func() *multigas.MultiGas {
				mg, _ := multigas.StorageAccessGas(params.SelfdestructGasEIP150).Set(multigas.ResourceKindStorageGrowth, params.CreateBySelfdestructGas)
				return mg
			}(),
			expectedRefund: params.SelfdestructRefundGas,
		},
	}

	testGasSelfdestructFuncWithCases(t, params.TestChainConfig, gasSelfdestruct, testCases)
}

// Modern (EIP-2929) SELFDESTRUCT gas function test with access list
func TestMakeSelfdestructGasFn(t *testing.T) {
	testCases := []GasSelfdestructFuncTestCase{
		{
			name: "selfdestruct - no access list - with refund",
			expectedMultiGas: func() *multigas.MultiGas {
				mg, _ := multigas.StorageAccessGas(params.ColdAccountAccessCostEIP2929).Set(multigas.ResourceKindStorageGrowth, params.CreateBySelfdestructGas)
				return mg
			}(),
			expectedRefund: params.SelfdestructRefundGas,
		},
		{
			name:              "has been destructed - no access list - no refund",
			expectedMultiGas:  multigas.StorageAccessGas(params.ColdAccountAccessCostEIP2929),
			hasBeenDestructed: true,
		},
		{
			name: "selfdestruct - in access list - with refund",
			expectedMultiGas: func() *multigas.MultiGas {
				mg, _ := multigas.StorageAccessGas(params.ColdAccountAccessCostEIP2929).Set(multigas.ResourceKindStorageGrowth, params.CreateBySelfdestructGas)
				return mg
			}(),
			expectedRefund:   params.SelfdestructRefundGas,
			slotInAccessList: true,
		},
		{
			name:              "has been destructed - in access list - no refund",
			expectedMultiGas:  multigas.StorageAccessGas(params.ColdAccountAccessCostEIP2929),
			hasBeenDestructed: true,
			slotInAccessList:  true,
		},
	}

	testGasSelfdestructFuncWithCases(t, params.TestChainConfig, makeSelfdestructGasFn(true), testCases)
}

// Statelessness mode (EIP-4762) SELFDESTRUCT gas function test
func TestGasSelfdestructEIP4762(t *testing.T) {
	stateDb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	evm := NewEVM(BlockContext{}, stateDb, params.TestChainConfig, Config{})

	caller := common.Address{}
	contractAddr := common.Address{1}
	beneficiaryAddr := common.Address{2}
	contractGas := uint64(100000)
	contract := NewContract(caller, contractAddr, new(uint256.Int), contractGas, nil)

	stack := newstack()
	mem := NewMemory()

	// Setup access list
	accessList := state.NewAccessEvents(evm.StateDB.PointCache())
	accessListForExpected := state.NewAccessEvents(evm.StateDB.PointCache())
	evm.AccessEvents = accessList

	stateDb.CreateAccount(beneficiaryAddr)

	stateDb.CreateAccount(contractAddr)
	stateDb.SetBalance(contractAddr, uint256.NewInt(0), tracing.BalanceChangeUnspecified)

	stack.push(new(uint256.Int).SetBytes(beneficiaryAddr.Bytes()))

	expectedStorageAccessGas := accessListForExpected.BasicDataGas(contractAddr, false) + accessListForExpected.BasicDataGas(beneficiaryAddr, false)
	expectedMultiGas := multigas.StorageAccessGas(expectedStorageAccessGas)

	multiGas, err := gasSelfdestructEIP4762(evm, contract, stack, mem, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if *multiGas != *expectedMultiGas {
		t.Errorf("Expected multi gas %d, got %d", expectedMultiGas, multiGas)
	}
}

// Base LOG0-LOG4 gas function test
func TestMakeGasLog(t *testing.T) {
	for n := uint64(0); n <= 4; n++ {
		statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		evm := NewEVM(BlockContext{}, statedb, params.TestChainConfig, Config{})

		caller := common.Address{}
		contractAddr := common.Address{1}
		contractGas := uint64(100000)
		contract := NewContract(caller, contractAddr, new(uint256.Int), contractGas, nil)

		stack := newstack()
		mem := NewMemory()
		memForExpected := NewMemory()

		// Set up stack for LOG operation
		requestedSize := uint64(32)                           // requestedSize = 32 bytes for data
		stack.push(new(uint256.Int).SetUint64(requestedSize)) // stack.Back(1)
		stack.push(new(uint256.Int))                          // dummy top (stack.Back(0))

		// memorySize is arbitrary, only affects memory cost component
		memorySize := uint64(64)

		expectedMultiGas, err := memoryGasCost(memForExpected, memorySize)
		if err != nil {
			t.Fatalf("Failed memoryGasCost: %v", err)
		}

		// Base LOG op → computation
		expectedMultiGas.SafeIncrement(multigas.ResourceKindComputation, params.LogGas)

		// Per-topic split
		const topicBytes = uint64(32)
		topicHistPer := topicBytes * params.LogDataGas // e.g., 256
		if params.LogTopicGas < topicHistPer {
			t.Fatalf("invalid params: LogTopicGas < topicHistPer")
		}
		topicCompPer := params.LogTopicGas - topicHistPer

		expectedMultiGas.SafeIncrement(multigas.ResourceKindHistoryGrowth, n*topicHistPer)
		expectedMultiGas.SafeIncrement(multigas.ResourceKindComputation, n*topicCompPer)

		// Data bytes → history growth
		expectedMultiGas.SafeIncrement(multigas.ResourceKindHistoryGrowth, requestedSize*params.LogDataGas)

		multiGas, err := makeGasLog(n)(evm, contract, stack, mem, memorySize)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if *multiGas != *expectedMultiGas {
			t.Errorf("Expected multi gas %d, got %d", expectedMultiGas, multiGas)
		}
	}
}
