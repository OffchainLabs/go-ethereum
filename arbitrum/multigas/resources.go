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
	refund uint64
}

func ZeroGas() *MultiGas {
	return &MultiGas{}
}

func NewMultiGas(kind ResourceKind, amount uint64) *MultiGas {
	mg := ZeroGas()
	mg.gas[kind] = amount
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

func (z *MultiGas) Set(kind ResourceKind, gas uint64) {
	z.gas[kind] = gas
}

func (z *MultiGas) GetRefund() uint64 {
	return z.refund
}

func (z *MultiGas) SetRefund(amount uint64) {
	z.refund = amount
}

// SafeAdd sets z to the sum x+y and returns z and checks for overflow.
func (z *MultiGas) SafeAdd(x *MultiGas, y *MultiGas) (*MultiGas, bool) {
	for i := ResourceKindUnknown; i < NumResourceKind; i++ {
		var overflow bool
		z.gas[i], overflow = math.SafeAdd(x.gas[i], y.gas[i])
		if overflow {
			return z, overflow
		}
	}
	return z, false
}

// SafeIncrement increments the given resource kind by the amount of gas and checks for overflow.
func (z *MultiGas) SafeIncrement(kind ResourceKind, gas uint64) bool {
	result, overflow := math.SafeAdd(z.gas[kind], gas)
	if overflow {
		return overflow
	}
	z.gas[kind] = result
	return false
}
