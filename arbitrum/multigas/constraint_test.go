package multigas

import (
	"testing"
	"time"
)

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
	if len(rc[ResourceKindComputation]) != 2 {
		t.Fatalf("expected 2 computation constraints")
	}
	if rc[ResourceKindComputation][minuteSecs].period != time.Duration(minuteSecs)*time.Second {
		t.Errorf("wrong constraint period")
	}
	if rc[ResourceKindComputation][minuteSecs].target != 5_000_000 {
		t.Errorf("wrong constraint target")
	}
	if rc[ResourceKindComputation][weekSecs].period != time.Duration(weekSecs)*time.Second {
		t.Errorf("wrong constraint period")
	}
	if rc[ResourceKindComputation][weekSecs].target != 3_000_000 {
		t.Errorf("wrong constraint target")
	}
	if len(rc[ResourceKindHistoryGrowth]) != 1 {
		t.Fatalf("Expected 1 history growth constraint")
	}
	if rc[ResourceKindHistoryGrowth][monthSecs].period != time.Duration(monthSecs)*time.Second {
		t.Errorf("wrong constraint period")
	}
	if rc[ResourceKindHistoryGrowth][monthSecs].target != 1_000_000 {
		t.Errorf("wrong constraint target")
	}
	if len(rc[ResourceKindStorageAccess]) != 0 {
		t.Fatalf("expected 0 storage access constraints")
	}
	if len(rc[ResourceKindStorageGrowth]) != 0 {
		t.Fatalf("expected 0 storage growth constraints")
	}

	// Updates a constraint
	rc.SetConstraint(ResourceKindHistoryGrowth, monthSecs, 500_000*monthSecs)
	if len(rc[ResourceKindHistoryGrowth]) != 1 {
		t.Fatalf("Expected 1 history growth constraints")
	}
	if rc[ResourceKindHistoryGrowth][monthSecs].target != 500_000 {
		t.Errorf("wrong constraint target")
	}

	// Clear constraints
	rc.ClearConstraint(ResourceKindComputation, minuteSecs)
	rc.ClearConstraint(ResourceKindComputation, weekSecs)
	rc.ClearConstraint(ResourceKindHistoryGrowth, monthSecs)
	if len(rc[ResourceKindComputation]) != 0 {
		t.Fatalf("Expected 0 computation constraints")
	}
	if len(rc[ResourceKindHistoryGrowth]) != 0 {
		t.Fatalf("Expected 0 history growth constraints")
	}
}
