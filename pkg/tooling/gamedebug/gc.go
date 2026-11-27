//go:build !release

package gamedebug

import (
	"math"
	"runtime/metrics"
	"testing"
	"time"
)

// GCMonitor samples Go runtime/metrics GC counters, heap allocations and pause histograms once per frame with zero steady-state allocations, flagging frames hitched by garbage collection.
type GCMonitor struct {
	history           []GCFrameSample
	head              int
	count             int
	recentHitches     int
	recentAllocs      uint64
	recentAllocBytes  uint64
	samples           [5]metrics.Sample
	prevPauseCounts   []uint64
	prevCycles        uint64
	totalCycles       uint64
	prevAllocsObjects uint64
	prevAllocsBytes   uint64
	heapBytes         uint64
	lastPause         time.Duration
	maxPause          time.Duration
}

// NewGCMonitor returns a GCMonitor retaining up to historyLen recent frame samples (defaulting to FrameHistory when <= 0).
func NewGCMonitor(historyLen int) *GCMonitor {
	if historyLen <= 0 {
		historyLen = FrameHistory
	}
	m := &GCMonitor{
		history: make([]GCFrameSample, historyLen),
		samples: [5]metrics.Sample{
			{Name: "/gc/pauses:seconds"},
			{Name: "/gc/cycles/total:gc-cycles"},
			{Name: "/memory/classes/heap/objects:bytes"},
			{Name: "/gc/heap/allocs:objects"},
			{Name: "/gc/heap/allocs:bytes"},
		},
	}
	metrics.Read(m.samples[:])
	if m.samples[0].Value.Kind() == metrics.KindFloat64Histogram {
		if h := m.samples[0].Value.Float64Histogram(); h != nil && len(h.Counts) > 0 {
			m.prevPauseCounts = make([]uint64, len(h.Counts))
			copy(m.prevPauseCounts, h.Counts)
		}
	}
	if m.samples[1].Value.Kind() == metrics.KindUint64 {
		c := m.samples[1].Value.Uint64()
		m.prevCycles = c
		m.totalCycles = c
	}
	if m.samples[2].Value.Kind() == metrics.KindUint64 {
		m.heapBytes = m.samples[2].Value.Uint64()
	}
	if m.samples[3].Value.Kind() == metrics.KindUint64 {
		m.prevAllocsObjects = m.samples[3].Value.Uint64()
	}
	if m.samples[4].Value.Kind() == metrics.KindUint64 {
		m.prevAllocsBytes = m.samples[4].Value.Uint64()
	}
	return m
}

// SampleFrame reads runtime/metrics for the completed frame and returns its GCFrameSample without heap allocations in steady state.
func (m *GCMonitor) SampleFrame(frameDur time.Duration) GCFrameSample {
	if m == nil {
		return GCFrameSample{}
	}
	if frameDur < 0 {
		frameDur = 0
	}

	metrics.Read(m.samples[:])

	var pauseSec float64
	var pauseObserved bool
	if m.samples[0].Value.Kind() == metrics.KindFloat64Histogram {
		h := m.samples[0].Value.Float64Histogram()
		if h != nil && len(h.Counts) == len(m.prevPauseCounts) && len(h.Buckets) == len(h.Counts)+1 {
			for i, count := range h.Counts {
				prev := m.prevPauseCounts[i]
				if count > prev {
					delta := count - prev
					m.prevPauseCounts[i] = count
					pauseObserved = true
					pauseSec += estimateBucketSeconds(h.Buckets[i], h.Buckets[i+1]) * float64(delta)
				}
			}
		}
	}

	var cyclesDelta uint64
	if m.samples[1].Value.Kind() == metrics.KindUint64 {
		cycles := m.samples[1].Value.Uint64()
		if cycles > m.prevCycles {
			cyclesDelta = cycles - m.prevCycles
		}
		m.prevCycles = cycles
		m.totalCycles = cycles
	}

	var heapBytes uint64
	if m.samples[2].Value.Kind() == metrics.KindUint64 {
		heapBytes = m.samples[2].Value.Uint64()
		m.heapBytes = heapBytes
	}

	var allocsDelta uint64
	if m.samples[3].Value.Kind() == metrics.KindUint64 {
		cur := m.samples[3].Value.Uint64()
		if cur > m.prevAllocsObjects {
			allocsDelta = cur - m.prevAllocsObjects
		}
		m.prevAllocsObjects = cur
	}

	var allocBytesDelta uint64
	if m.samples[4].Value.Kind() == metrics.KindUint64 {
		cur := m.samples[4].Value.Uint64()
		if cur > m.prevAllocsBytes {
			allocBytesDelta = cur - m.prevAllocsBytes
		}
		m.prevAllocsBytes = cur
	}

	pause := time.Duration(pauseSec * float64(time.Second))
	if pause < 0 {
		pause = 0
	}
	if pause > 0 {
		m.lastPause = pause
		if pause > m.maxPause {
			m.maxPause = pause
		}
	}

	hitched := cyclesDelta > 0 || pauseObserved || pause > 0
	sample := GCFrameSample{
		FrameDuration:    frameDur,
		CyclesDelta:      cyclesDelta,
		Pause:            pause,
		HeapObjectsBytes: heapBytes,
		AllocsDelta:      allocsDelta,
		AllocBytesDelta:  allocBytesDelta,
		Hitched:          hitched,
	}

	if len(m.history) > 0 {
		if m.count == len(m.history) {
			old := m.history[m.head]
			if old.Hitched {
				m.recentHitches--
			}
			m.recentAllocs -= old.AllocsDelta
			m.recentAllocBytes -= old.AllocBytesDelta
		}
		m.history[m.head] = sample
		if sample.Hitched {
			m.recentHitches++
		}
		m.recentAllocs += sample.AllocsDelta
		m.recentAllocBytes += sample.AllocBytesDelta
		m.head = (m.head + 1) % len(m.history)
		if m.count < len(m.history) {
			m.count++
		}
	}

	return sample
}

// TotalCycles returns the cumulative GC cycle count reported by /gc/cycles/total:gc-cycles.
func (m *GCMonitor) TotalCycles() uint64 {
	if m == nil {
		return 0
	}
	return m.totalCycles
}

// MaxPause returns the longest per-frame GC pause observed by this monitor.
func (m *GCMonitor) MaxPause() time.Duration {
	if m == nil {
		return 0
	}
	return m.maxPause
}

// LastPause returns the most recent non-zero GC pause observed by this monitor.
func (m *GCMonitor) LastPause() time.Duration {
	if m == nil {
		return 0
	}
	return m.lastPause
}

// RecentHitches returns how many frames in the retained history ring buffer experienced a GC pause.
func (m *GCMonitor) RecentHitches() int {
	if m == nil {
		return 0
	}
	return m.recentHitches
}

// HeapObjectsBytes returns the most recently sampled /memory/classes/heap/objects:bytes value.
func (m *GCMonitor) HeapObjectsBytes() uint64 {
	if m == nil {
		return 0
	}
	return m.heapBytes
}

// AllocsPerFrame returns the average number of heap objects allocated per frame across the retained history window, or 0 when empty.
func (m *GCMonitor) AllocsPerFrame() float64 {
	if m == nil || m.count == 0 {
		return 0
	}
	return float64(m.recentAllocs) / float64(m.count)
}

// AllocBytesPerFrame returns the average number of heap bytes allocated per frame across the retained history window, or 0 when empty.
func (m *GCMonitor) AllocBytesPerFrame() float64 {
	if m == nil || m.count == 0 {
		return 0
	}
	return float64(m.recentAllocBytes) / float64(m.count)
}

// Reset clears retained GC frame history, hitch counts and peak pause timings, and re-baselines runtime/metrics counters to the current point in time.
func (m *GCMonitor) Reset() {
	if m == nil {
		return
	}
	m.head = 0
	m.count = 0
	m.recentHitches = 0
	m.recentAllocs = 0
	m.recentAllocBytes = 0
	m.lastPause = 0
	m.maxPause = 0

	metrics.Read(m.samples[:])
	if m.samples[0].Value.Kind() == metrics.KindFloat64Histogram {
		if h := m.samples[0].Value.Float64Histogram(); h != nil && len(h.Counts) == len(m.prevPauseCounts) {
			copy(m.prevPauseCounts, h.Counts)
		}
	}
	if m.samples[1].Value.Kind() == metrics.KindUint64 {
		c := m.samples[1].Value.Uint64()
		m.prevCycles = c
		m.totalCycles = c
	}
	if m.samples[2].Value.Kind() == metrics.KindUint64 {
		m.heapBytes = m.samples[2].Value.Uint64()
	}
	if m.samples[3].Value.Kind() == metrics.KindUint64 {
		m.prevAllocsObjects = m.samples[3].Value.Uint64()
	}
	if m.samples[4].Value.Kind() == metrics.KindUint64 {
		m.prevAllocsBytes = m.samples[4].Value.Uint64()
	}
}

// Len returns how many frame samples are currently retained in history.
func (m *GCMonitor) Len() int {
	if m == nil {
		return 0
	}
	return m.count
}

// At returns sample i in chronological order (0 is oldest, Len()-1 is newest).
func (m *GCMonitor) At(i int) GCFrameSample {
	if m == nil || i < 0 || i >= m.count || len(m.history) == 0 {
		return GCFrameSample{}
	}
	n := len(m.history)
	start := (m.head - m.count + n) % n
	return m.history[(start+i)%n]
}

// Latest returns the most recently sampled GCFrameSample, or the zero value when empty.
func (m *GCMonitor) Latest() GCFrameSample {
	if m == nil || m.count == 0 || len(m.history) == 0 {
		return GCFrameSample{}
	}
	n := len(m.history)
	return m.history[(m.head-1+n)%n]
}

func estimateBucketSeconds(lo, hi float64) float64 {
	if math.IsInf(lo, -1) || lo < 0 {
		lo = 0
	}
	if math.IsInf(hi, 1) {
		return lo
	}
	if hi <= lo {
		return lo
	}
	return 0.5 * (lo + hi)
}

// MeasureAllocs wraps testing.AllocsPerRun and returns the average heap allocations per execution of fn over runs iterations (defaulting to 100 when runs <= 0).
func MeasureAllocs(runs int, fn func()) float64 {
	if fn == nil {
		return 0
	}
	if runs <= 0 {
		runs = 100
	}
	return testing.AllocsPerRun(runs, fn)
}

// AssertZeroAllocs fails t if fn allocates on the heap during runs iterations, for use in CI hot-path regression tests.
func AssertZeroAllocs(t testing.TB, runs int, fn func()) {
	t.Helper()
	if runs <= 0 {
		runs = 100
	}
	if allocs := MeasureAllocs(runs, fn); allocs != 0 {
		t.Fatalf("gamedebug: expected 0 allocs/run over %d runs, got %.2f", runs, allocs)
	}
}
