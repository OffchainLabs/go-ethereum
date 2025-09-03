package multigas

import (
	"math/bits"
)

// ResourceKind represents a dimension for the multi-dimensional gas.
type ResourceKind uint8

const (
	ResourceKindUnknown ResourceKind = iota
	ResourceKindComputation
	ResourceKindHistoryGrowth
	ResourceKindStorageAccess
	ResourceKindStorageGrowth
	ResourceKindL1Calldata
	ResourceKindL2Calldata
	ResourceKindWasmComputation
	NumResourceKind
)

// MultiGas tracks gas usage across multiple resource kinds, while also
// maintaining a single-dimensional total gas sum and refund amount.
type MultiGas struct {
	gas    [NumResourceKind]uint64
	total  uint64
	refund uint64
}

// Pair represents a single resource kind and its associated gas amount.
type Pair struct {
	Kind   ResourceKind
	Amount uint64
}

// ZeroGas creates a MultiGas value with all fields set to zero.
func ZeroGas() MultiGas {
	return MultiGas{}
}

// NewMultiGas creates a new MultiGas with the given resource kind initialized to `amount`.
// All other kinds are zero. The total is also set to `amount`.
func NewMultiGas(kind ResourceKind, amount uint64) MultiGas {
	var mg MultiGas
	mg.gas[kind] = amount
	mg.total = amount
	return mg
}

// MultiGasFromPairs creates a new MultiGas from resource–amount pairs.
// Intended for constant-like construction; panics on overflow.
func MultiGasFromPairs(pairs ...Pair) MultiGas {
	var mg MultiGas
	for _, p := range pairs {
		newTotal, c := bits.Add64(mg.total, p.Amount, 0)
		if c != 0 {
			panic("multigas overflow")
		}
		mg.gas[p.Kind] = p.Amount
		mg.total = newTotal
	}
	return mg
}

// UnknownGas returns a MultiGas initialized with unknown gas.
func UnknownGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindUnknown, amount)
}

// ComputationGas returns a MultiGas initialized with computation gas.
func ComputationGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindComputation, amount)
}

// HistoryGrowthGas returns a MultiGas initialized with history growth gas.
func HistoryGrowthGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindHistoryGrowth, amount)
}

// StorageAccessGas returns a MultiGas initialized with storage access gas.
func StorageAccessGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindStorageAccess, amount)
}

// StorageGrowthGas returns a MultiGas initialized with storage growth gas.
func StorageGrowthGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindStorageGrowth, amount)
}

// L1CalldataGas returns a MultiGas initialized with L1 calldata gas.
func L1CalldataGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindL1Calldata, amount)
}

// L2CalldataGas returns a MultiGas initialized with L2 calldata gas.
func L2CalldataGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindL2Calldata, amount)
}

// WasmComputationGas returns a MultiGas initialized with computation gas used for WASM (Stylus contracts).
func WasmComputationGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindWasmComputation, amount)
}

// Get returns the gas amount for the specified resource kind.
func (z MultiGas) Get(kind ResourceKind) uint64 {
	return z.gas[kind]
}

// With returns a copy of z with the given resource kind set to amount.
// The total is adjusted accordingly. It returns the updated value and true if an overflow occurred.
func (z MultiGas) With(kind ResourceKind, amount uint64) (MultiGas, bool) {
	res := z
	newTotal, c := bits.Add64(z.total-z.gas[kind], amount, 0)
	if c != 0 {
		return z, true
	}
	res.gas[kind] = amount
	res.total = newTotal
	return res, false
}

// GetRefund gets the SSTORE refund computed at the end of the transaction.
func (z MultiGas) GetRefund() uint64 {
	return z.refund
}

// WithRefund returns a copy of z with its refund set to amount.
func (z MultiGas) WithRefund(amount uint64) MultiGas {
	res := z
	res.refund = amount
	return res
}

// SafeAdd returns a copy of z with the per-kind, total, and refund gas
// added to the values from x. It returns the updated value and true if
// an overflow occurred.
func (z MultiGas) SafeAdd(x MultiGas) (MultiGas, bool) {
	res := z

	for i := 0; i < int(NumResourceKind); i++ {
		v, c := bits.Add64(res.gas[i], x.gas[i], 0)
		if c != 0 {
			return z, true
		}
		res.gas[i] = v
	}

	t, c := bits.Add64(res.total, x.total, 0)
	if c != 0 {
		return z, true
	}
	res.total = t

	r, c := bits.Add64(res.refund, x.refund, 0)
	if c != 0 {
		return z, true
	}
	res.refund = r

	return res, false
}

// SaturatingAdd returns a copy of z with the per-kind, total, and refund gas
// added to the values from x. On overflow, the affected field(s) are clamped
// to MaxUint64.
func (z MultiGas) SaturatingAdd(x MultiGas) MultiGas {
	res := z

	for i := 0; i < int(NumResourceKind); i++ {
		if v, c := bits.Add64(res.gas[i], x.gas[i], 0); c != 0 {
			res.gas[i] = ^uint64(0) // clamp
		} else {
			res.gas[i] = v
		}
	}

	if t, c := bits.Add64(res.total, x.total, 0); c != 0 {
		res.total = ^uint64(0) // clamp
	} else {
		res.total = t
	}

	if r, c := bits.Add64(res.refund, x.refund, 0); c != 0 {
		res.refund = ^uint64(0) // clamp
	} else {
		res.refund = r
	}

	return res
}

// SaturatingAddInto adds x into z in place (per kind, total, and refund).
// On overflow, the affected field(s) are clamped to MaxUint64.
// This is a hot-path helper; the public immutable API remains preferred elsewhere.
func (z *MultiGas) SaturatingAddInto(x MultiGas) {
	for i := 0; i < int(NumResourceKind); i++ {
		if v, c := bits.Add64(z.gas[i], x.gas[i], 0); c != 0 {
			z.gas[i] = ^uint64(0) // clamp
		} else {
			z.gas[i] = v
		}
	}
	if t, c := bits.Add64(z.total, x.total, 0); c != 0 {
		z.total = ^uint64(0) // clamp
	} else {
		z.total = t
	}
	if r, c := bits.Add64(z.refund, x.refund, 0); c != 0 {
		z.refund = ^uint64(0) // clamp
	} else {
		z.refund = r
	}
}

// SafeIncrement returns a copy of z with the given resource kind
// and the total incremented by gas. It returns the updated value and true if
// an overflow occurred.
func (z MultiGas) SafeIncrement(kind ResourceKind, gas uint64) (MultiGas, bool) {
	res := z

	newValue, c := bits.Add64(z.gas[kind], gas, 0)
	if c != 0 {
		return res, true
	}

	newTotal, c := bits.Add64(z.total, gas, 0)
	if c != 0 {
		return res, true
	}

	res.gas[kind] = newValue
	res.total = newTotal
	return res, false
}

// SaturatingIncrement returns a copy of z with the given resource kind
// and the total incremented by gas. On overflow, the field(s) are clamped to MaxUint64.
func (z MultiGas) SaturatingIncrement(kind ResourceKind, gas uint64) MultiGas {
	res := z

	if v, c := bits.Add64(res.gas[kind], gas, 0); c != 0 {
		res.gas[kind] = ^uint64(0) // clamp
	} else {
		res.gas[kind] = v
	}

	if t, c := bits.Add64(res.total, gas, 0); c != 0 {
		res.total = ^uint64(0) // clamp
	} else {
		res.total = t
	}

	return res
}

// SaturatingIncrementInto increments the given resource kind and the total
// in place by gas. On overflow, the affected field(s) are clamped to MaxUint64.
// Unlike SaturatingIncrement, this method mutates the receiver directly and
// is intended for VM hot paths where avoiding value copies is critical.
func (z *MultiGas) SaturatingIncrementInto(kind ResourceKind, gas uint64) {
	if v, c := bits.Add64(z.gas[kind], gas, 0); c != 0 {
		z.gas[kind] = ^uint64(0)
	} else {
		z.gas[kind] = v
	}
	if t, c := bits.Add64(z.total, gas, 0); c != 0 {
		z.total = ^uint64(0)
	} else {
		z.total = t
	}
}

// SingleGas returns the single-dimensional total gas.
func (z MultiGas) SingleGas() uint64 {
	return z.total
}
