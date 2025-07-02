package multigas

// ResourceKind represents a dimension for the multi-dimensional gas.
type ResourceKind uint8

const (
	ResourceKindComputation ResourceKind = iota
	ResourceKindHistoryGrowth
	ResourceKindStorageAccess
	ResourceKindStorageGrowth
	NumResourceKind
)

// MultiGas tracks gas for each resource separately.
type MultiGas [NumResourceKind]uint64
