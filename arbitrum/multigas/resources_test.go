package multigas

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/rlp"
)

func TestZeroGas(t *testing.T) {
	zero := ZeroGas()
	if zero.SingleGas() != 0 {
		t.Errorf("ZeroGas total should be 0, got %d", zero.SingleGas())
	}
	if zero.IsZero() != true {
		t.Errorf("ZeroGas should be zero, got %v", zero.IsZero())
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
	if comp.IsZero() != false {
		t.Errorf("ComputationGas should not be zero, got %v", comp.IsZero())
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
		Pair{ResourceKindWasmComputation, 16},
	)

	if got := fromPairs.SingleGas(); got != 91 {
		t.Errorf("MultiGasFromPairs: expected SingleGas() == 91, got %d", got)
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
	if got := fromPairs.Get(ResourceKindWasmComputation); got != 16 {
		t.Errorf("MultiGasFromPairs: expected Get(ResourceKindWasmComputation) == 16, got %d", got)
	}
}

func TestSafeAdd(t *testing.T) {
	gas, overflow := ComputationGas(10).SafeAdd(HistoryGrowthGas(20))
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
	if got, want := gas.Get(ResourceKindWasmComputation), uint64(0); got != want {
		t.Errorf("unexpected WASM computation gas: got %v, want %v", got, want)
	}
	if got, want := gas.SingleGas(), uint64(30); got != want {
		t.Errorf("unexpected single gas: got %v, want %v", got, want)
	}
}

func TestSafeAddChecksOneDimensionalOverflow(t *testing.T) {
	_, overflow := ComputationGas(math.MaxUint64).SafeAdd(ComputationGas(1))
	if !overflow {
		t.Errorf("expected overflow: got %v, want %v", overflow, true)
	}
}

func TestSafeAddChecksTotalOverflow(t *testing.T) {
	_, overflow := ComputationGas(math.MaxUint64).SafeAdd(HistoryGrowthGas(1))
	if !overflow {
		t.Errorf("expected overflow: got %v, want %v", overflow, true)
	}
}

func TestSaturatingAdd(t *testing.T) {
	a := ComputationGas(10)
	b := ComputationGas(20)
	res := a.SaturatingAdd(b)

	if got, want := res.Get(ResourceKindComputation), uint64(30); got != want {
		t.Errorf("unexpected computation gas: got %v, want %v", got, want)
	}
	if got, want := res.SingleGas(), uint64(30); got != want {
		t.Errorf("unexpected total gas: got %v, want %v", got, want)
	}
}

func TestSaturatingAddClampsOnOverflow(t *testing.T) {
	a := ComputationGas(math.MaxUint64)
	b := ComputationGas(1)
	res := a.SaturatingAdd(b)

	if got, want := res.Get(ResourceKindComputation), uint64(math.MaxUint64); got != want {
		t.Errorf("expected computation gas to clamp: got %v, want %v", got, want)
	}
	if got, want := res.SingleGas(), uint64(math.MaxUint64); got != want {
		t.Errorf("expected total gas to clamp: got %v, want %v", got, want)
	}
}

func TestSaturatingAddInto_AddsKindsTotalRefund(t *testing.T) {
	// z: comp=5, sa=2, total=7
	z := MultiGasFromPairs(
		Pair{ResourceKindComputation, 5},
		Pair{ResourceKindStorageAccess, 2},
	)
	// x: l2=3, refund=4, total=3
	x := MultiGasFromPairs(
		Pair{ResourceKindL2Calldata, 3},
	)
	x = x.WithRefund(4)

	z.SaturatingAddInto(x)

	if got, want := z.Get(ResourceKindComputation), uint64(5); got != want {
		t.Errorf("unexpected computation: got %v, want %v", got, want)
	}
	if got, want := z.Get(ResourceKindStorageAccess), uint64(2); got != want {
		t.Errorf("unexpected storage access: got %v, want %v", got, want)
	}
	if got, want := z.Get(ResourceKindL2Calldata), uint64(3); got != want {
		t.Errorf("unexpected l2 calldata: got %v, want %v", got, want)
	}
	if got, want := z.GetRefund(), uint64(4); got != want {
		t.Errorf("unexpected refund: got %v, want %v", got, want)
	}
	if got, want := z.SingleGas(), uint64(10); got != want { // 7 + 3
		t.Errorf("unexpected total: got %v, want %v", got, want)
	}
}

func TestSafeIncrement(t *testing.T) {
	gas := ComputationGas(10)
	gas, overflow := gas.SafeIncrement(ResourceKindComputation, 11)
	if overflow {
		t.Errorf("unexpected overflow: got %v, want %v", overflow, false)
	}
	if got, want := gas.Get(ResourceKindComputation), uint64(21); got != want {
		t.Errorf("unexpected computation gas: got %v, want %v", got, want)
	}
}

func TestSafeIncrementChecksOverflow(t *testing.T) {
	gas := ComputationGas(10)
	_, overflow := gas.SafeIncrement(ResourceKindComputation, math.MaxUint64)
	if !overflow {
		t.Errorf("expected overflow: got %v, want %v", overflow, true)
	}
}

func TestSingleGas(t *testing.T) {
	gas := MultiGasFromPairs(
		Pair{ResourceKindComputation, 21},
		Pair{ResourceKindHistoryGrowth, 15},
		Pair{ResourceKindStorageAccess, 5},
		Pair{ResourceKindStorageGrowth, 6},
		Pair{ResourceKindL1Calldata, 7},
		Pair{ResourceKindL2Calldata, 8},
		Pair{ResourceKindWasmComputation, 9},
	)
	singleGas := gas.SingleGas()
	if want := uint64(71); singleGas != want {
		t.Errorf("unexpected storage growth gas: got %v, want %v", singleGas, want)
	}
}

func TestSaturatingIncrement(t *testing.T) {
	// normal increment
	gas := ComputationGas(10)
	newGas := gas.SaturatingIncrement(ResourceKindComputation, 5)
	if got, want := newGas.Get(ResourceKindComputation), uint64(15); got != want {
		t.Errorf("unexpected computation gas: got %v, want %v", got, want)
	}
	if got, want := newGas.SingleGas(), uint64(15); got != want {
		t.Errorf("unexpected single gas: got %v, want %v", got, want)
	}

	// saturating increment on kind
	gas = ComputationGas(math.MaxUint64)
	newGas = gas.SaturatingIncrement(ResourceKindComputation, 1)
	if got, want := newGas.Get(ResourceKindComputation), uint64(math.MaxUint64); got != want {
		t.Errorf("expected computation gas to saturate: got %v, want %v", got, want)
	}
	if got, want := newGas.SingleGas(), uint64(math.MaxUint64); got != want {
		t.Errorf("expected total to saturate: got %v, want %v", got, want)
	}

	// saturating increment on total only
	gas = MultiGasFromPairs(
		Pair{ResourceKindComputation, math.MaxUint64},
		Pair{ResourceKindHistoryGrowth, 0},
	)
	// bump history growth so total overflows
	newGas = gas.SaturatingIncrement(ResourceKindHistoryGrowth, 1)
	if got, want := newGas.Get(ResourceKindHistoryGrowth), uint64(1); got != want {
		t.Errorf("unexpected history growth gas: got %v, want %v", got, want)
	}
	if got, want := newGas.SingleGas(), uint64(math.MaxUint64); got != want {
		t.Errorf("expected total to saturate: got %v, want %v", got, want)
	}
}

func TestSaturatingIncrementIntoClampsOnOverflow(t *testing.T) {
	// total is at Max, so any increment must clamp total; the kind may or may not clamp.
	g := ComputationGas(math.MaxUint64)

	// Increment a different kind by 1: kind won't overflow, total will clamp.
	g.SaturatingIncrementInto(ResourceKindStorageAccess, 1)
	if got, want := g.Get(ResourceKindStorageAccess), uint64(1); got != want {
		t.Errorf("unexpected storage-access gas: got %v, want %v", got, want)
	}
	if got, want := g.SingleGas(), uint64(math.MaxUint64); got != want {
		t.Errorf("expected total to clamp: got %v, want %v", got, want)
	}

	// Now force kind overflow too: computation already Max, +1 should clamp kind as well.
	g.SaturatingIncrementInto(ResourceKindComputation, 1)
	if got, want := g.Get(ResourceKindComputation), uint64(math.MaxUint64); got != want {
		t.Errorf("expected computation to remain clamped: got %v, want %v", got, want)
	}
	if got, want := g.SingleGas(), uint64(math.MaxUint64); got != want {
		t.Errorf("expected total to remain clamped: got %v, want %v", got, want)
	}
}

func TestMultiGasSingleGasTracking(t *testing.T) {
	g := ZeroGas()
	if got := g.SingleGas(); got != 0 {
		t.Fatalf("initial total: got %v, want 0", got)
	}

	var overflow bool
	g, overflow = g.With(ResourceKindComputation, 5)
	if overflow {
		t.Fatalf("unexpected overflow in With")
	}
	if got, want := g.SingleGas(), uint64(5); got != want {
		t.Fatalf("after With: got total %v, want %v", got, want)
	}

	g, overflow = g.SafeIncrement(ResourceKindComputation, 7)
	if overflow {
		t.Fatalf("unexpected overflow in SafeIncrement")
	}
	if got, want := g.SingleGas(), uint64(12); got != want {
		t.Fatalf("after SafeIncrement: got total %v, want %v", got, want)
	}

	other := StorageAccessGas(8)
	g, overflow = g.SafeAdd(other)
	if overflow {
		t.Fatalf("unexpected overflow in SafeAdd")
	}
	if got, want := g.SingleGas(), uint64(20); got != want {
		t.Fatalf("after SafeAdd: got total %v, want %v", got, want)
	}

	overflowing := L1CalldataGas(math.MaxUint64)
	g = g.SaturatingAdd(overflowing)

	if got := g.SingleGas(); got != math.MaxUint64 {
		t.Fatalf("after SaturatingAdd: got total %v, want MaxUint64", got)
	}
}

func TestMultiGasRLPRoundTrip(t *testing.T) {
	mgs := []MultiGas{
		ZeroGas(),
		ComputationGas(100),
		L1CalldataGas(50).WithRefund(20),

		MultiGasFromPairs(
			Pair{ResourceKindUnknown, 1},
			Pair{ResourceKindComputation, 10},
			Pair{ResourceKindHistoryGrowth, 11},
			Pair{ResourceKindStorageGrowth, 13},
		),
		MultiGasFromPairs(
			Pair{ResourceKindComputation, 10},
			Pair{ResourceKindHistoryGrowth, 11},
			Pair{ResourceKindStorageAccess, 12},
			Pair{ResourceKindStorageGrowth, 13},
			Pair{ResourceKindL1Calldata, 14},
			Pair{ResourceKindL2Calldata, 15},
			Pair{ResourceKindWasmComputation, 16},
		).WithRefund(7),
	}

	for _, mg := range mgs {
		b, err := rlp.EncodeToBytes(&mg)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}

		var out MultiGas
		if err := rlp.DecodeBytes(b, &out); err != nil {
			t.Fatalf("decode: %v", err)
		}

		for i := range int(NumResourceKind) {
			require.Equal(t, mg.Get(ResourceKind(i)), out.Get(ResourceKind(i)))
		}
	}
}
