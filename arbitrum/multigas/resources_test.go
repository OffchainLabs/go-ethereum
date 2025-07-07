package multigas

import (
	"math"
	"testing"
)

func TestMultiGas(t *testing.T) {
	// Test SafeAdd
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

	// Test SafeAdd checks for overflow
	_, overflow = new(MultiGas).SafeAdd(ComputationGas(math.MaxUint64), ComputationGas(1))
	if !overflow {
		t.Errorf("unexpected overflow: got %v, want %v", overflow, true)
	}

	// Test SafeIncrement
	overflow = gas.SafeIncrement(ResourceKindComputation, 11)
	if overflow {
		t.Errorf("unexpected overflow: got %v, want %v", overflow, false)
	}
	if got, want := gas.Get(ResourceKindComputation), uint64(21); got != want {
		t.Errorf("unexpected computation gas: got %v, want %v", got, want)
	}

	// Test SafeIncrement checks for overflow
	overflow = gas.SafeIncrement(ResourceKindComputation, math.MaxUint64)
	if !overflow {
		t.Errorf("unexpected overflow: got %v, want %v", overflow, true)
	}
}
