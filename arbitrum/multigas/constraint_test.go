package multigas

import (
	"testing"
	"time"
)

func assertEqual[T comparable](t *testing.T, description string, got T, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("unexpected %v: want %v, got %v", description, want, got)
	}
}

func TestResourceConstraints(t *testing.T) {
	rc := NewResourceConstraints()

	const (
		minuteSecs = 60
		daySecs    = 24 * 60 * 60
		weekSecs   = 7 * daySecs
		monthSecs  = 30 * daySecs
	)

	// Adds a few constraints
	rc.SetConstraint(ResourceKindComputation, minuteSecs, 5_000_000*minuteSecs)
	rc.SetConstraint(ResourceKindComputation, weekSecs, 3_000_000*weekSecs)
	rc.SetConstraint(ResourceKindHistoryGrowth, monthSecs, 1_000_000*monthSecs)
	assertEqual(t, "number of computation constraints", len(rc[ResourceKindComputation]), 2)
	assertEqual(t, "constraint period", rc[ResourceKindComputation][minuteSecs].period, time.Duration(minuteSecs)*time.Second)
	assertEqual(t, "constraint target", rc[ResourceKindComputation][minuteSecs].target, 5_000_000)
	assertEqual(t, "constraint period", rc[ResourceKindComputation][weekSecs].period, time.Duration(weekSecs)*time.Second)
	assertEqual(t, "constraint target", rc[ResourceKindComputation][weekSecs].target, 3_000_000)
	assertEqual(t, "number of history growth constraints", len(rc[ResourceKindHistoryGrowth]), 1)
	assertEqual(t, "constraint period", rc[ResourceKindHistoryGrowth][monthSecs].period, time.Duration(monthSecs)*time.Second)
	assertEqual(t, "constraint target", rc[ResourceKindHistoryGrowth][monthSecs].target, 1_000_000)
	assertEqual(t, "number of storage access constraints", len(rc[ResourceKindStorageAccess]), 0)
	assertEqual(t, "number of storage growth constraints", len(rc[ResourceKindStorageGrowth]), 0)

	// Updates a constraint
	rc.SetConstraint(ResourceKindHistoryGrowth, monthSecs, 500_000*monthSecs)
	assertEqual(t, "number of history growth constraints", len(rc[ResourceKindHistoryGrowth]), 1)
	assertEqual(t, "constraint target", rc[ResourceKindHistoryGrowth][monthSecs].target, 500_000)

	// Clear constraints
	rc.ClearConstraint(ResourceKindComputation, minuteSecs)
	rc.ClearConstraint(ResourceKindComputation, weekSecs)
	rc.ClearConstraint(ResourceKindHistoryGrowth, monthSecs)
	assertEqual(t, "number of computation constraints", len(rc[ResourceKindComputation]), 0)
	assertEqual(t, "number of history growth constraints", len(rc[ResourceKindHistoryGrowth]), 0)
	assertEqual(t, "number of storage access constraints", len(rc[ResourceKindStorageAccess]), 0)
	assertEqual(t, "number of storage growth constraints", len(rc[ResourceKindStorageGrowth]), 0)
}
