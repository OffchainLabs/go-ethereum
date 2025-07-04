package multigas

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
	gas [NumResourceKind]uint64
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

// Add sets z to the sum x+y and returns z.
func (z *MultiGas) Add(x *MultiGas, y *MultiGas) *MultiGas {
	for i := ResourceKindUnknown; i < NumResourceKind; i++ {
		z.gas[i] = x.gas[i] + y.gas[i]
	}
	return z
}

// Sub sets z to the difference x-y and returns z.
func (z *MultiGas) Sub(x *MultiGas, y *MultiGas) *MultiGas {
	for i := ResourceKindUnknown; i < NumResourceKind; i++ {
		z.gas[i] = x.gas[i] - y.gas[i]
	}
	return z
}
