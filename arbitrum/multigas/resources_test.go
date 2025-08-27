package multigas

import (
	"math"
	"testing"
)

func TestZeroGas(t *testing.T) {
	zero := ZeroGas()
	if zero.SingleGas() != 0 {
		t.Errorf("ZeroGas total should be 0, got %d", zero.SingleGas())
	}
}

func TestComputationGas(t *testing.T) {
	comp := ComputationGas(100)
	if comp.Get(ResourceKindComputation) != 100 {
		t.Errorf("ComputationGas: expected Get(ResourceKindComputation) == 100, got %d", comp.Get(ResourceKindComputation))
	}
	if comp.SingleGas() != 100 {
		t.Errorf("ComputationGas: expected SingleGas() == 100, got %d", comp.SingleGas())
	}
}

func TestStorageAccessGas(t *testing.T) {
	storage := StorageAccessGas(200)
	if storage.Get(ResourceKindStorageAccess) != 200 {
		t.Errorf("StorageAccessGas: expected Get(ResourceKindStorageAccess) == 200, got %d", storage.Get(ResourceKindStorageAccess))
	}
	if storage.SingleGas() != 200 {
		t.Errorf("StorageAccessGas: expected SingleGas() == 200, got %d", storage.SingleGas())
	}
}

func TestMultiGasFromPairs(t *testing.T) {
	fromPairs := MultiGasFromPairs(
		Pair{ResourceKindComputation, 10},
		Pair{ResourceKindHistoryGrowth, 11},
		Pair{ResourceKindStorageAccess, 12},
		Pair{ResourceKindStorageGrowth, 13},
		Pair{ResourceKindL1Calldata, 14},
		Pair{ResourceKindL2Calldata, 15},
	)

	if got := fromPairs.SingleGas(); got != 75 {
		t.Errorf("MultiGasFromPairs: expected SingleGas() == 75, got %d", got)
	}
	if got := fromPairs.Get(ResourceKindComputation); got != 10 {
		t.Errorf("MultiGasFromPairs: expected Get(ResourceKindComputation) == 10, got %d", got)
	}
	if got := fromPairs.Get(ResourceKindHistoryGrowth); got != 11 {
		t.Errorf("MultiGasFromPairs: expected Get(ResourceKindHistoryGrowth) == 11, got %d", got)
	}
	if got := fromPairs.Get(ResourceKindStorageAccess); got != 12 {
		t.Errorf("MultiGasFromPairs: expected Get(ResourceKindStorageAccess) == 12, got %d", got)
	}
	if got := fromPairs.Get(ResourceKindStorageGrowth); got != 13 {
		t.Errorf("MultiGasFromPairs: expected Get(ResourceKindStorageGrowth) == 13, got %d", got)
	}
	if got := fromPairs.Get(ResourceKindL1Calldata); got != 14 {
		t.Errorf("MultiGasFromPairs: expected Get(ResourceKindL1Calldata) == 14, got %d", got)
	}
	if got := fromPairs.Get(ResourceKindL2Calldata); got != 15 {
		t.Errorf("MultiGasFromPairs: expected Get(ResourceKindL2Calldata) == 15, got %d", got)
	}
}

func TestSafeAdd(t *testing.T) {
	computation := ComputationGas(10)
	historyGrowth := HistoryGrowthGas(20)
	gas, overflow := new(MultiGas).SafeAdd(&computation, &historyGrowth)
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
	if got, want := gas.Get(ResourceKindL1Calldata), uint64(0); got != want {
		t.Errorf("unexpected L1 calldata gas: got %v, want %v", got, want)
	}
	if got, want := gas.Get(ResourceKindL2Calldata), uint64(0); got != want {
		t.Errorf("unexpected L2 calldata gas: got %v, want %v", got, want)
	}
	if got, want := gas.SingleGas(), uint64(30); got != want {
		t.Errorf("unexpected single gas: got %v, want %v", got, want)
	}
}

func TestSafeAddChecksOneDimensionalOverflow(t *testing.T) {
	x := ComputationGas(math.MaxUint64)
	y := ComputationGas(1)
	_, overflow := new(MultiGas).SafeAdd(&x, &y)
	if !overflow {
		t.Errorf("expected overflow: got %v, want %v", overflow, true)
	}
}

func TestSafeAddChecksTotalOverflow(t *testing.T) {
	x := ComputationGas(math.MaxUint64)
	y := HistoryGrowthGas(1)
	_, overflow := new(MultiGas).SafeAdd(&x, &y)
	if !overflow {
		t.Errorf("expected overflow: got %v, want %v", overflow, true)
	}
}

func TestSafeIncrement(t *testing.T) {
	gas := ComputationGas(10)
	overflow := gas.SafeIncrement(ResourceKindComputation, 11)
	if overflow {
		t.Errorf("unexpected overflow: got %v, want %v", overflow, false)
	}
	if got, want := gas.Get(ResourceKindComputation), uint64(21); got != want {
		t.Errorf("unexpected computation gas: got %v, want %v", got, want)
	}
}

func TestSafeIncrementChecksOverflow(t *testing.T) {
	gas := ComputationGas(10)
	overflow := gas.SafeIncrement(ResourceKindComputation, math.MaxUint64)
	if !overflow {
		t.Errorf("expected overflow: got %v, want %v", overflow, true)
	}
}

func TestSingleGas(t *testing.T) {
	gas := MultiGasFromPairs(
		Pair{ResourceKindComputation, 21},
		Pair{ResourceKindHistoryGrowth, 15},
		Pair{ResourceKindStorageAccess, 5},
	)
	singleGas := gas.SingleGas()
	if want := uint64(41); singleGas != want {
		t.Errorf("unexpected storage growth gas: got %v, want %v", singleGas, want)
	}
}
