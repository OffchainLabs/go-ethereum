package multigas

import "testing"

func TestMultiGas(t *testing.T) {
	gas := new(MultiGas).Add(ComputationGas(10), HistoryGrowthGas(20))
	gas = gas.Add(gas, StorageAccessGas(30))
	gas = gas.Add(gas, StorageGrowthGas(40))
	if got, want := gas.Get(ResourceKindComputation), uint64(10); got != want {
		t.Errorf("unexpected computation gas: got %v, want %v", got, want)
	}
	if got, want := gas.Get(ResourceKindHistoryGrowth), uint64(20); got != want {
		t.Errorf("unexpected history growth gas: got %v, want %v", got, want)
	}
	if got, want := gas.Get(ResourceKindStorageAccess), uint64(30); got != want {
		t.Errorf("unexpected storage access gas: got %v, want %v", got, want)
	}
	if got, want := gas.Get(ResourceKindStorageGrowth), uint64(40); got != want {
		t.Errorf("unexpected storage growth gas: got %v, want %v", got, want)
	}
	gas = gas.Sub(gas, StorageGrowthGas(40))
	if got, want := gas.Get(ResourceKindStorageGrowth), uint64(0); got != want {
		t.Errorf("unexpected storage growth gas: got %v, want %v", got, want)
	}
}
