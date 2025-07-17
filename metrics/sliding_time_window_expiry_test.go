package metrics

import (
	"testing"
	"time"
)

// TestSlidingTimeWindowExpiry verifies that values expire to 0 after the time window
func TestSlidingTimeWindowExpiry(t *testing.T) {
	// Create a sample with a 50ms window
	window := 50 * time.Millisecond
	sample := NewSlidingTimeWindowArraySample(window)

	// Add some values
	sample.Update(100)
	sample.Update(200)
	sample.Update(300)

	// Verify initial values
	snapshot1 := sample.Snapshot()
	if snapshot1.Count() != 3 {
		t.Errorf("Expected initial count of 3, got %d", snapshot1.Count())
	}
	if snapshot1.Mean() != 200 {
		t.Errorf("Expected initial mean of 200, got %f", snapshot1.Mean())
	}

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	// Take snapshot after expiry
	snapshot2 := sample.Snapshot()

	// Should have count of 3 (from the implementation returning the actual count)
	// but all values should be 0
	percentiles := snapshot2.Percentiles([]float64{0.5, 0.95, 0.99})
	for i, p := range percentiles {
		if p != 0 {
			t.Errorf("Expected percentile[%d] to be 0 after expiry, got %f", i, p)
		}
	}

	if snapshot2.Min() != 0 {
		t.Errorf("Expected min to be 0 after expiry, got %d", snapshot2.Min())
	}

	if snapshot2.Max() != 0 {
		t.Errorf("Expected max to be 0 after expiry, got %d", snapshot2.Max())
	}

	if snapshot2.Mean() != 0 {
		t.Errorf("Expected mean to be 0 after expiry, got %f", snapshot2.Mean())
	}
}

// TestChunkedAssociativeArrayTrimAll verifies the fix for trimming all chunks
func TestChunkedAssociativeArrayTrimAll(t *testing.T) {
	array := NewChunkedAssociativeArray(3)

	// Add values with small keys
	array.Put(1, 100)
	array.Put(2, 200)
	array.Put(3, 300)
	array.Put(4, 400)

	// Verify values exist
	values := array.Values()
	if len(values) != 4 {
		t.Errorf("Expected 4 values, got %d", len(values))
	}

	// Trim with startKey greater than all existing keys
	// This should remove all chunks
	array.Trim(1000, 2000)

	// Should be empty now
	values = array.Values()
	if len(values) != 1 || values[0] != 0 {
		t.Errorf("Expected [0] after trimming all, got %v", values)
	}
}

// TestBoundedHistogramSampleExpiry verifies histogram expiry behavior
func TestBoundedHistogramSampleExpiry(t *testing.T) {
	// Create a histogram with 100ms window for faster testing
	sample := NewSlidingTimeWindowArraySample(100 * time.Millisecond)
	histogram := NewHistogram(sample)

	// Add values
	histogram.Update(1000)
	histogram.Update(2000)
	histogram.Update(3000)

	// Check initial state
	snapshot1 := histogram.Snapshot()
	if snapshot1.Count() != 3 {
		t.Errorf("Expected count of 3, got %d", snapshot1.Count())
	}

	// Wait for expiry
	time.Sleep(110 * time.Millisecond)

	// Check after expiry
	snapshot2 := histogram.Snapshot()
	percentiles := snapshot2.Percentiles([]float64{0.5, 0.99})

	for i, p := range percentiles {
		if p != 0 {
			t.Errorf("Expected percentile[%d] to be 0 after expiry, got %f", i, p)
		}
	}

	if snapshot2.Min() != 0 {
		t.Errorf("Expected min to be 0 after expiry, got %d", snapshot2.Min())
	}

	if snapshot2.Max() != 0 {
		t.Errorf("Expected max to be 0 after expiry, got %d", snapshot2.Max())
	}
}
