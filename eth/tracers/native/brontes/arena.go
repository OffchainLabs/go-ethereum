package brontes

type CallTraceArena struct {
	Arena []CallTraceNode
}

func NewCallTraceArena() *CallTraceArena {
	return &CallTraceArena{
		Arena: []CallTraceNode{{}},
	}
}

// PushTrace pushes a new trace into the arena, returning the trace ID.
// It will attach the trace to its parent if kind.IsAttachToParent() returns true.
func (cta *CallTraceArena) PushTrace(entry int, kind PushTraceKind, newTrace CallTrace) int {
	for {
		// If newTrace is the entry/root node, update the root and return 0.
		if newTrace.Depth == 0 {
			cta.Arena[0].Trace = newTrace
			return 0
		}

		// If we found the parent node (its depth is one less than newTrace's depth):
		if cta.Arena[entry].Trace.Depth == newTrace.Depth-1 {
			id := len(cta.Arena)
			// Create a new node; other fields will have their zero value.
			node := CallTraceNode{
				Parent: &entry, // assuming Parent is a pointer to int
				Trace:  newTrace,
				Idx:    id,
			}
			cta.Arena = append(cta.Arena, node)

			// If we need to attach the new trace to its parent's children list:
			if kind.IsAttachToParent() {
				parent := &cta.Arena[entry]
				traceLocation := len(parent.Children)
				// Append a LogCallOrder value. Here we assume NewLogCallOrderCall is defined elsewhere.
				parent.Ordering = append(parent.Ordering, NewLogCallOrderCall(traceLocation))
				parent.Children = append(parent.Children, id)
			}
			return id
		}

		// Otherwise, we haven't found the proper parent; go deeper by selecting the last child.
		parentNode := cta.Arena[entry]
		if len(parentNode.Children) == 0 {
			panic("Disconnected trace")
		}
		entry = parentNode.Children[len(parentNode.Children)-1]
	}
}

// Nodes returns a slice of the trace nodes.
func (cta *CallTraceArena) Nodes() []CallTraceNode {
	return cta.Arena
}

// Clear removes all nodes from the arena (the underlying capacity is unchanged).
func (cta *CallTraceArena) Clear() {
	cta.Arena = cta.Arena[:0]
}

// PushTraceKind specifies how to push a trace into the arena.
type PushTraceKind int

const (
	// PushTraceKindPushOnly only pushes the trace into the arena.
	PushTraceKindPushOnly PushTraceKind = iota
	// PushTraceKindPushAndAttachToParent pushes the trace and also attaches it as a child to its parent.
	PushTraceKindPushAndAttachToParent
)

// IsAttachToParent returns true if the kind indicates that the trace should be attached to its parent.
func (ptk PushTraceKind) IsAttachToParent() bool {
	return ptk == PushTraceKindPushAndAttachToParent
}
