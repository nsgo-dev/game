// Package session records release-safe frame pacing and runtime memory metrics.
package session

import (
	"runtime"
	"runtime/metrics"
	"sort"
	"time"
)

type Summary struct {
	Frames                 uint64
	P50, P95, P99, Longest time.Duration
	Jank2x                 uint64
	HeapPeak, TotalMemory  uint64
	GCCycles               uint64
	MaxPause               time.Duration
}
type Recorder struct {
	interval                 time.Duration
	samples                  [4096]time.Duration
	n                        uint64
	jank                     uint64
	longest                  time.Duration
	heap, mem, gc, basePause uint64
	maxPause                 time.Duration
	metric                   [6]metrics.Sample
}

func New(interval time.Duration) *Recorder {
	if interval <= 0 {
		interval = 16 * time.Millisecond
	}
	r := &Recorder{interval: interval}
	r.metric[0].Name = "/memory/classes/heap/objects:bytes"
	r.metric[1].Name = "/memory/classes/total:bytes"
	r.metric[2].Name = "/gc/cycles/total:gc-cycles"
	r.metric[3].Name = "/gc/pauses:seconds"
	r.metric[4].Name = "/gc/heap/allocs:bytes"
	r.metric[5].Name = "/sched/pauses/total/gc:seconds"
	return r
}
func (r *Recorder) Observe(dt time.Duration) {
	if r == nil || dt <= 0 {
		return
	}
	r.samples[r.n%uint64(len(r.samples))] = dt
	r.n++
	if dt > r.longest {
		r.longest = dt
	}
	if dt >= 2*r.interval {
		r.jank++
	}
	metrics.Read(r.metric[:])
	if v := r.metric[0].Value.Uint64(); v > r.heap {
		r.heap = v
	}
	if v := r.metric[1].Value.Uint64(); v > r.mem {
		r.mem = v
	}
	if v := r.metric[2].Value.Uint64(); v > r.gc {
		r.gc = v
	}
	if r.metric[5].Value.Kind() == metrics.KindFloat64Histogram {
		h := r.metric[5].Value.Float64Histogram()
		for i, n := range h.Counts {
			if n > 0 && h.Buckets[i+1] > r.maxPause.Seconds() {
				r.maxPause = time.Duration(h.Buckets[i+1] * float64(time.Second))
			}
		}
	}
}
func (r *Recorder) Summary() Summary {
	if r == nil {
		return Summary{}
	}
	n := r.n
	if n > uint64(len(r.samples)) {
		n = uint64(len(r.samples))
	}
	vals := make([]time.Duration, n)
	for i := range vals {
		vals[i] = r.samples[(r.n-n+uint64(i))%uint64(len(r.samples))]
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	pick := func(q float64) time.Duration {
		if len(vals) == 0 {
			return 0
		}
		i := int(q * float64(len(vals)-1))
		return vals[i]
	}
	return Summary{Frames: r.n, P50: pick(.5), P95: pick(.95), P99: pick(.99), Longest: r.longest, Jank2x: r.jank, HeapPeak: r.heap, TotalMemory: r.mem, GCCycles: r.gc, MaxPause: r.maxPause}
}
func (_ Summary) MarshalJSON() ([]byte, error) { return nil, nil }

var _ = runtime.MemStats{}
