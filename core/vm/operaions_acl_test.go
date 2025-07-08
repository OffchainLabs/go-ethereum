package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func TestMakeGasSStoreFunc(t *testing.T) {
	testCases := []struct {
		name             string
		slotInAccessList bool
		originalValue    common.Hash // committed state value
		currentValue     common.Hash // current state value (may differ from original)
		newValue         common.Hash // value to set
		refund           bool        // for recreate slot test, we need to simulate that a refund was added when it was deleted
		expectedMultiGas *multigas.MultiGas
	}{
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

	slotKey := common.HexToHash("0x01")
	gasSStoreFunc := makeGasSStoreFunc(params.SstoreClearsScheduleRefundEIP2200)
	contractGas := uint64(100000)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			evm := NewEVM(BlockContext{}, statedb, params.TestChainConfig, Config{})

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
