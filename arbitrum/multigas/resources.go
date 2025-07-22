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

// MultiGas tracks gas for each resource separately.
type MultiGas struct {
	gas    [NumResourceKind]uint64
	total  uint64
	refund uint64
}

func ZeroGas() *MultiGas {
	return &MultiGas{}
}

func NewMultiGas(kind ResourceKind, amount uint64) *MultiGas {
	mg := ZeroGas()
	mg.gas[kind] = amount
	mg.total = amount
	return mg
}

func ComputationGas(amount uint64) *MultiGas {
	return NewMultiGas(ResourceKindComputation, amount)
}

func HistoryGrowthGas(amount uint64) *MultiGas {
	return NewMultiGas(ResourceKindHistoryGrowth, amount)
}

func StorageAccessGas(amount uint64) *MultiGas {
	return NewMultiGas(ResourceKindStorageAccess, amount)
}

func StorageGrowthGas(amount uint64) *MultiGas {
	return NewMultiGas(ResourceKindStorageGrowth, amount)
}

func (z *MultiGas) Get(kind ResourceKind) uint64 {
	return z.gas[kind]
}

func (z *MultiGas) Set(kind ResourceKind, gas uint64) (*MultiGas, bool) {
	oldValue := z.gas[kind]
	newTotal := z.total - oldValue

	finalTotal, overflow := math.SafeAdd(newTotal, gas)
	if overflow {
		return z, true
	}

	z.gas[kind] = gas
	z.total = finalTotal
	return z, false
}

// GetRefund gets the SSTORE refund computed at the end of the transaction.
func (z *MultiGas) GetRefund() uint64 {
	return z.refund
}

// SetRefund sets the SSTORE refund computed at the end of the transaction.
func (z *MultiGas) SetRefund(amount uint64) *MultiGas {
	z.refund = amount
	return z
}

// SafeAdd sets z to the sum x+y and returns z and checks for overflow.
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

// SafeIncrement increments the given resource kind by the amount of gas and checks for overflow.
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
