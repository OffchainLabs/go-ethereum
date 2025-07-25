package multigas

import (
	"math"
	"testing"
)

func TestMultiGas(t *testing.T) {
	t.Run("TestZeroGas", func(t *testing.T) {
		zero := ZeroGas()
		if zero.SingleGas() != 0 {
			t.Errorf("ZeroGas total should be 0, got %d", zero.SingleGas())
		}
	})

	t.Run("TestComputationGas", func(t *testing.T) {
		comp := ComputationGas(100)
		if comp.Get(ResourceKindComputation) != 100 {
			t.Errorf("ComputationGas: expected Get(ResourceKindComputation) == 100, got %d", comp.Get(ResourceKindComputation))
		}
		if comp.SingleGas() != 100 {
			t.Errorf("ComputationGas: expected SingleGas() == 100, got %d", comp.SingleGas())
		}
	})

	t.Run("TestStorageAccessGas", func(t *testing.T) {
		storage := StorageAccessGas(200)
		if storage.Get(ResourceKindStorageAccess) != 200 {
			t.Errorf("StorageAccessGas: expected Get(ResourceKindStorageAccess) == 200, got %d", storage.Get(ResourceKindStorageAccess))
		}
		if storage.SingleGas() != 200 {
			t.Errorf("StorageAccessGas: expected SingleGas() == 200, got %d", storage.SingleGas())
		}
	})

	t.Run("TestMultiGasFromMap", func(t *testing.T) {
		fromMap := MultiGasFromMap(map[ResourceKind]uint64{
			ResourceKindComputation:   10,
			ResourceKindHistoryGrowth: 11,
			ResourceKindStorageAccess: 12,
			ResourceKindStorageGrowth: 13,
		})
		if got := fromMap.SingleGas(); got != 46 {
			t.Errorf("MultiGasFromMap: expected SingleGas() == 46, got %d", got)
		}
		if got := fromMap.Get(ResourceKindComputation); got != 10 {
			t.Errorf("MultiGasFromMap: expected Get(ResourceKindComputation) == 10, got %d", got)
		}
		if got := fromMap.Get(ResourceKindHistoryGrowth); got != 11 {
			t.Errorf("MultiGasFromMap: expected Get(ResourceKindHistoryGrowth) == 11, got %d", got)
		}
		if got := fromMap.Get(ResourceKindStorageAccess); got != 12 {
			t.Errorf("MultiGasFromMap: expected Get(ResourceKindStorageAccess) == 12, got %d", got)
		}
		if got := fromMap.Get(ResourceKindStorageGrowth); got != 13 {
			t.Errorf("MultiGasFromMap: expected Get(ResourceKindStorageGrowth) == 13, got %d", got)
		}
	})

	t.Run("TestSafeAdd", func(t *testing.T) {
		gas, overflow := new(MultiGas).SafeAdd(ComputationGas(10), HistoryGrowthGas(20))
		if overflow {
			t.Errorf("unexpected overflow: got %v, want %v", overflow, false)
		}
		if got, want := gas.Get(ResourceKindComputation), uint64(10); got != want {
			t.Errorf("unexpected computation gas: got %v, want %v", got, want)
		}
		if got, want := gas.Get(ResourceKindHistoryGrowth), uint64(20); got != want {
			t.Errorf("unexpected history growth gas: got %v, want %v", got, want)
		}
		if got, want := gas.Get(ResourceKindStorageAccess), uint64(0); got != want {
			t.Errorf("unexpected storage access gas: got %v, want %v", got, want)
		}
		if got, want := gas.Get(ResourceKindStorageGrowth), uint64(0); got != want {
			t.Errorf("unexpected storage growth gas: got %v, want %v", got, want)
		}
		if got, want := gas.SingleGas(), uint64(30); got != want {
			t.Errorf("unexpected single gas: got %v, want %v", got, want)
		}
	})

	t.Run("TestSafeAddChecksOneDimensionalOverflow", func(t *testing.T) {
		_, overflow := new(MultiGas).SafeAdd(ComputationGas(math.MaxUint64), ComputationGas(1))
		if !overflow {
			t.Errorf("expected overflow: got %v, want %v", overflow, true)
		}
	})

	t.Run("TestSafeAddChecksTotalOverflow", func(t *testing.T) {
		_, overflow := new(MultiGas).SafeAdd(ComputationGas(math.MaxUint64), HistoryGrowthGas(1))
		if !overflow {
			t.Errorf("expected overflow: got %v, want %v", overflow, true)
		}
	})

	t.Run("TestSafeIncrement", func(t *testing.T) {
		gas := ComputationGas(10)
		overflow := gas.SafeIncrement(ResourceKindComputation, 11)
		if overflow {
			t.Errorf("unexpected overflow: got %v, want %v", overflow, false)
		}
		if got, want := gas.Get(ResourceKindComputation), uint64(21); got != want {
			t.Errorf("unexpected computation gas: got %v, want %v", got, want)
		}
	})

	t.Run("TestSafeIncrementChecksOverflow", func(t *testing.T) {
		gas := ComputationGas(10)
		overflow := gas.SafeIncrement(ResourceKindComputation, math.MaxUint64)
		if !overflow {
			t.Errorf("expected overflow: got %v, want %v", overflow, true)
		}
	})

	t.Run("TestSingleGas", func(t *testing.T) {
		gas := MultiGasFromMap(map[ResourceKind]uint64{
			ResourceKindComputation:   21,
			ResourceKindHistoryGrowth: 15,
			ResourceKindStorageAccess: 5,
		})
		singleGas := gas.SingleGas()
		if want := uint64(41); singleGas != want {
			t.Errorf("unexpected storage growth gas: got %v, want %v", singleGas, want)
		}
	})
}
