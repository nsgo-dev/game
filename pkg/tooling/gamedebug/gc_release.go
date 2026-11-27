//go:build release

package gamedebug

import (
	"testing"
	"time"
)

var releaseGCMonitor GCMonitor

// GCMonitor is a zero-cost stub in release builds.
type GCMonitor struct{}

// NewGCMonitor returns a no-op GCMonitor in release builds.
func NewGCMonitor(int) *GCMonitor { return &releaseGCMonitor }

// SampleFrame returns the zero GCFrameSample in release builds.
func (*GCMonitor) SampleFrame(time.Duration) GCFrameSample { return GCFrameSample{} }

// TotalCycles always returns 0 in release builds.
func (*GCMonitor) TotalCycles() uint64 { return 0 }

// MaxPause always returns 0 in release builds.
func (*GCMonitor) MaxPause() time.Duration { return 0 }

// LastPause always returns 0 in release builds.
func (*GCMonitor) LastPause() time.Duration { return 0 }

// RecentHitches always returns 0 in release builds.
func (*GCMonitor) RecentHitches() int { return 0 }

// HeapObjectsBytes always returns 0 in release builds.
func (*GCMonitor) HeapObjectsBytes() uint64 { return 0 }

// AllocsPerFrame always returns 0 in release builds.
func (*GCMonitor) AllocsPerFrame() float64 { return 0 }

// AllocBytesPerFrame always returns 0 in release builds.
func (*GCMonitor) AllocBytesPerFrame() float64 { return 0 }

// Reset is a no-op in release builds.
func (*GCMonitor) Reset() {}

// Len always returns 0 in release builds.
func (*GCMonitor) Len() int { return 0 }

// At always returns the zero GCFrameSample in release builds.
func (*GCMonitor) At(int) GCFrameSample { return GCFrameSample{} }

// Latest always returns the zero GCFrameSample in release builds.
func (*GCMonitor) Latest() GCFrameSample { return GCFrameSample{} }

// MeasureAllocs always returns 0 in release builds.
func MeasureAllocs(int, func()) float64 { return 0 }

// AssertZeroAllocs is a no-op in release builds.
func AssertZeroAllocs(testing.TB, int, func()) {}
