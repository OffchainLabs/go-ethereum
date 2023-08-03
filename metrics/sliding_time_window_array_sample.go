package metrics

import (
	"sync"
	"time"
)

// SlidingTimeWindowArraySample is ported from Coda Hale's dropwizard library
// <https://github.com/dropwizard/metrics/pull/1139>
// A reservoir implementation backed by a sliding window that stores only the
// measurements made in the last given window of time
type SlidingTimeWindowArraySample struct {
	startTick    int64
	measurements *ChunkedAssociativeArray
	window       int64
	count        int64
	lastTick     int64
	mutex        sync.Mutex
}

const (
	// SlidingTimeWindowCollisionBuffer allow this many duplicate ticks
	// before overwriting measurements
	SlidingTimeWindowCollisionBuffer = 256

	// SlidingTimeWindowTrimThreshold is number of updates between trimming data
	SlidingTimeWindowTrimThreshold = 256

	// SlidingTimeWindowClearBufferTicks is the number of ticks to keep past the
	// requested trim
	SlidingTimeWindowClearBufferTicks = int64(time.Hour/time.Nanosecond) *
		SlidingTimeWindowCollisionBuffer
)

// NewSlidingTimeWindowArraySample creates new object with given window of time
func NewSlidingTimeWindowArraySample(window time.Duration) Sample {
	if !Enabled {
		return NilSample{}
	}
	return &SlidingTimeWindowArraySample{
		startTick:    time.Now().UnixNano(),
		measurements: NewChunkedAssociativeArray(ChunkedAssociativeArrayDefaultChunkSize),
		window:       window.Nanoseconds() * SlidingTimeWindowCollisionBuffer,
	}
}

// Clear clears all samples.
func (s *SlidingTimeWindowArraySample) Clear() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.count = 0
	s.measurements.Clear()
}

// Count returns the number of samples recorded, which may exceed the
// reservoir size.
func (s *SlidingTimeWindowArraySample) Count() int64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.count
}

// Max returns the maximum value in the sample, which may not be the maximum
// value ever to be part of the sample.
func (s *SlidingTimeWindowArraySample) Max() int64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.trim()
	return SampleMax(s.measurements.Values())
}

// Mean returns the mean of the values in the sample.
func (s *SlidingTimeWindowArraySample) Mean() float64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.trim()
	return SampleMean(s.measurements.Values())
}

// Min returns the minimum value in the sample, which may not be the minimum
// value ever to be part of the sample.
func (s *SlidingTimeWindowArraySample) Min() int64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.trim()
	return SampleMin(s.measurements.Values())
}

// Percentile returns an arbitrary percentile of values in the sample.
func (s *SlidingTimeWindowArraySample) Percentile(p float64) float64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.trim()
	return SamplePercentile(s.measurements.Values(), p)
}

// Percentiles returns a slice of arbitrary percentiles of values in the
// sample.
func (s *SlidingTimeWindowArraySample) Percentiles(ps []float64) []float64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.trim()
	return SamplePercentiles(s.measurements.Values(), ps)
}

// Size returns the size of the sample, which is at most the reservoir size.
func (s *SlidingTimeWindowArraySample) Size() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.trim()
	return s.measurements.Size()
}

// trim requires s.mutex to already be acquired
func (s *SlidingTimeWindowArraySample) trim() {
	now := s.getTick()
	windowStart := now - s.window
	windowEnd := now + SlidingTimeWindowClearBufferTicks
	if windowStart < windowEnd {
		s.measurements.Trim(windowStart, windowEnd)
	} else {
		// long overflow handling that can only happen 1 year after class loading
		s.measurements.Clear()
	}
}

// getTick requires s.mutex to already be acquired
func (s *SlidingTimeWindowArraySample) getTick() int64 {
	oldTick := s.lastTick
	tick := (time.Now().UnixNano() - s.startTick) * SlidingTimeWindowCollisionBuffer
	var newTick int64
	if tick-oldTick > 0 {
		newTick = tick
	} else {
		newTick = oldTick + 1
	}
	s.lastTick = newTick
	return newTick
}

// Snapshot returns a read-only copy of the sample.
func (s *SlidingTimeWindowArraySample) Snapshot() Sample {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.trim()
	origValues := s.measurements.Values()
	values := make([]int64, len(origValues))
	copy(values, origValues)
	return &SampleSnapshot{
		count:  s.count,
		values: values,
	}
}

// StdDev returns the standard deviation of the values in the sample.
func (s *SlidingTimeWindowArraySample) StdDev() float64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.trim()
	return SampleStdDev(s.measurements.Values())
}

// Sum returns the sum of the values in the sample.
func (s *SlidingTimeWindowArraySample) Sum() int64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.trim()
	return SampleSum(s.measurements.Values())
}

// Update samples a new value.
func (s *SlidingTimeWindowArraySample) Update(v int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var newTick int64
	s.count += 1
	if s.count%SlidingTimeWindowTrimThreshold == 0 {
		s.trim()
	}
	newTick = s.getTick()
	longOverflow := newTick < s.lastTick
	if longOverflow {
		s.measurements.Clear()
	}
	s.measurements.Put(newTick, v)
}

// Values returns a copy of the values in the sample.
func (s *SlidingTimeWindowArraySample) Values() []int64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.trim()
	origValues := s.measurements.Values()
	values := make([]int64, len(origValues))
	copy(values, origValues)
	return values
}

// Variance returns the variance of the values in the sample.
func (s *SlidingTimeWindowArraySample) Variance() float64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.trim()
	return SampleVariance(s.measurements.Values())
}
