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
type MultiGas [NumResourceKind]uint64

// NewEmptyMultiGas returns an empty MultiGas array
func NewEmptyMultiGas() MultiGas {
	return MultiGas{}
}
