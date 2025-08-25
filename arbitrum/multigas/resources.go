package multigas

import "github.com/ethereum/go-ethereum/common/math"

// ResourceKind represents a dimension for the multi-dimensional gas.
type ResourceKind uint8

const (
	ResourceKindUnknown ResourceKind = iota
	ResourceKindComputation
	ResourceKindHistoryGrowth
	ResourceKindStorageAccess
	ResourceKindStorageGrowth
	NumResourceKind
)

const ResourceKindL2Calldata = ResourceKindComputation

// MultiGas tracks gas usage across multiple resource kinds, while also
// maintaining a single-dimensional total gas sum and refund amount.
type MultiGas struct {
	gas    [NumResourceKind]uint64
	total  uint64
	refund uint64
}

// ZeroGas creates a MultiGas value with all fields set to zero.
func ZeroGas() *MultiGas {
	return &MultiGas{}
}

// NewMultiGas creates a new MultiGas with the given resource kind initialized to `amount`.
// All other kinds are zero. The total is also set to `amount`.
func NewMultiGas(kind ResourceKind, amount uint64) *MultiGas {
	mg := ZeroGas()
	mg.gas[kind] = amount
	mg.total = amount
	return mg
}

// MultiGasFromMap creates a new MultiGas that contains multiple resources.
// This is meant to be called with constant values and will panic if there is an overflow.
func MultiGasFromMap(gasMap map[ResourceKind]uint64) *MultiGas {
	mg := ZeroGas()
	for kind, gas := range gasMap {
		var overflow bool
		mg.total, overflow = math.SafeAdd(mg.total, gas)
		if overflow {
			panic("multigas overflow")
		}
		mg.gas[kind] = gas
	}
	return mg
}

// UnknownGas returns a MultiGas initialized with unknown gas.
func UnknownGas(amount uint64) *MultiGas {
	return NewMultiGas(ResourceKindUnknown, amount)
}

// ComputationGas returns a MultiGas initialized with computation gas.
func ComputationGas(amount uint64) *MultiGas {
	return NewMultiGas(ResourceKindComputation, amount)
}

// L2CalldataGas returns a MultiGas initialized with calldata gas.
func L2CalldataGas(amount uint64) *MultiGas {
	return NewMultiGas(ResourceKindL2Calldata, amount)
}

// HistoryGrowthGas returns a MultiGas initialized with history growth gas.
func HistoryGrowthGas(amount uint64) *MultiGas {
	return NewMultiGas(ResourceKindHistoryGrowth, amount)
}

// StorageAccessGas returns a MultiGas initialized with storage access gas.
func StorageAccessGas(amount uint64) *MultiGas {
	return NewMultiGas(ResourceKindStorageAccess, amount)
}

// StorageGrowthGas returns a MultiGas initialized with storage growth gas.
func StorageGrowthGas(amount uint64) *MultiGas {
	return NewMultiGas(ResourceKindStorageGrowth, amount)
}

// Get returns the gas amount for the specified resource kind.
func (z *MultiGas) Get(kind ResourceKind) uint64 {
	return z.gas[kind]
}

// Set sets the gas for a given resource kind to `gas`, adjusting the total accordingly.
// Returns the same MultiGas and a boolean indicating if an overflow occurred when updating the total.
func (z *MultiGas) Set(kind ResourceKind, gas uint64) (*MultiGas, bool) {
	newTotal, overflow := math.SafeAdd(z.total-z.gas[kind], gas)
	if overflow {
		return z, true
	}

	z.gas[kind] = gas
	z.total = newTotal
	return z, false
}

// GetRefund gets the SSTORE refund computed at the end of the transaction.
func (z *MultiGas) GetRefund() uint64 {
	return z.refund
}

// SetRefund sets the SSTORE refund computed at the end of the transaction and returns the modified MultiGas.
func (z *MultiGas) SetRefund(amount uint64) *MultiGas {
	z.refund = amount
	return z
}

// SafeAdd sets z to the sum of x and y, per resource kind and total.
// Returns the modified MultiGas and a boolean indicating if an overflow occurred in either the kind-specific or total value.
func (z *MultiGas) SafeAdd(x *MultiGas, y *MultiGas) (*MultiGas, bool) {
	for i := range z.gas {
		newValue, overflow := math.SafeAdd(x.gas[i], y.gas[i])
		if overflow {
			return z, true
		}
		z.gas[i] = newValue
	}

	newTotal, overflow := math.SafeAdd(x.total, y.total)
	if overflow {
		return z, true
	}
	z.total = newTotal
	return z, false
}

// SafeIncrement increments the given resource kind by the amount of gas and to the total.
// Returns true if an overflow occurred in either the kind-specific or total value.
func (z *MultiGas) SafeIncrement(kind ResourceKind, gas uint64) bool {
	newValue, overflow := math.SafeAdd(z.gas[kind], gas)
	if overflow {
		return true
	}

	newTotal, overflow := math.SafeAdd(z.total, gas)
	if overflow {
		return true
	}

	z.gas[kind] = newValue
	z.total = newTotal
	return false
}

// SingleGas returns single-dimensional gas sum.
func (z *MultiGas) SingleGas() uint64 {
	return z.total
}
