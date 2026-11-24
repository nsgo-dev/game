//go:build !release

package debug

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime/trace"
	"sort"
	"sync"
	"time"
)

const defaultMaxRecordedFrames = 300

// ScopeToken is returned by Profiler.Begin or Profiler.Scope and passed to End to close a timing scope without heap allocations.
type ScopeToken struct {
	profiler *Profiler
	scopeID  int
	startNs  int64
	depth    int
	region   *trace.Region
	active   bool
}

// End closes the timing scope associated with t. It is safe to call on a zero ScopeToken.
func (t ScopeToken) End() {
	if t.profiler != nil && t.active {
		t.profiler.End(t)
	}
}

type scopeRecord struct {
	name      string
	curTotal  int64
	curMax    int64
	curCalls  int
	lastTotal int64
	lastMax   int64
	lastCalls int

	ring      [FrameHistory]int64
	ringHead  int
	ringCount int
}

type recordedEvent struct {
	scopeID int
	startNs int64
	durNs   int64
	depth   int
	frame   int
}

// Profiler records named timing scopes per frame with zero steady-state allocations, produces top-scope summaries for Overlay, emits optional runtime/trace regions, and exports Chrome/Perfetto Trace Event Format JSON.
type Profiler struct {
	mu sync.Mutex

	epoch    time.Time
	nowFn    func() time.Time
	traceCtx context.Context

	scopeIndex map[string]int
	scopes     []scopeRecord

	inFrame         bool
	completedFrames int
	depth           int

	maxRecordedFrames int
	recording         bool
	recordTarget      int
	recordedFrames    int
	recordStartNs     int64
	events            []recordedEvent
}

// NewProfiler returns a Profiler capable of recording up to maxRecordedFrames frames (defaulting to 300 when <= 0).
func NewProfiler(maxRecordedFrames int) *Profiler {
	if maxRecordedFrames <= 0 {
		maxRecordedFrames = defaultMaxRecordedFrames
	}
	return &Profiler{
		epoch:             time.Now(),
		traceCtx:          context.Background(),
		scopeIndex:        make(map[string]int, 32),
		scopes:            make([]scopeRecord, 0, 32),
		maxRecordedFrames: maxRecordedFrames,
	}
}

// SetNow overrides the clock used to timestamp scopes, primarily for deterministic tests. Passing nil restores time.Now.
func (p *Profiler) SetNow(fn func() time.Time) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.nowFn = fn
	if fn != nil {
		p.epoch = fn()
	} else {
		p.epoch = time.Now()
	}
	p.mu.Unlock()
}

// SetTraceContext sets the context passed to runtime/trace.StartRegion when Go execution tracing is active.
func (p *Profiler) SetTraceContext(ctx context.Context) {
	if p == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	p.traceCtx = ctx
	p.mu.Unlock()
}

func (p *Profiler) nowNs() int64 {
	if p.nowFn != nil {
		return p.nowFn().Sub(p.epoch).Nanoseconds()
	}
	return time.Since(p.epoch).Nanoseconds()
}

// BeginFrame resets per-frame scope accumulators for the next frame.
func (p *Profiler) BeginFrame() {
	if p == nil {
		return
	}
	p.mu.Lock()
	for i := range p.scopes {
		p.scopes[i].curTotal = 0
		p.scopes[i].curMax = 0
		p.scopes[i].curCalls = 0
	}
	p.inFrame = true
	p.depth = 0
	p.mu.Unlock()
}

// EndFrame finalizes the current frame's scope timings and advances active recording if enabled.
func (p *Profiler) EndFrame() {
	if p == nil {
		return
	}
	p.mu.Lock()
	for i := range p.scopes {
		sc := &p.scopes[i]
		sc.lastTotal = sc.curTotal
		sc.lastMax = sc.curMax
		sc.lastCalls = sc.curCalls
		sc.ring[sc.ringHead] = sc.curTotal
		sc.ringHead = (sc.ringHead + 1) % FrameHistory
		if sc.ringCount < FrameHistory {
			sc.ringCount++
		}
	}
	p.inFrame = false
	p.completedFrames++
	if p.recording {
		p.recordedFrames++
		if p.recordedFrames >= p.recordTarget {
			p.recording = false
		}
	}
	p.mu.Unlock()
}

// Begin opens a named timing scope and returns a ScopeToken to pass to End.
func (p *Profiler) Begin(name string) ScopeToken {
	if p == nil || name == "" {
		return ScopeToken{}
	}
	var region *trace.Region
	traceOn := trace.IsEnabled()

	p.mu.Lock()
	id, ok := p.scopeIndex[name]
	if !ok {
		id = len(p.scopes)
		p.scopeIndex[name] = id
		p.scopes = append(p.scopes, scopeRecord{name: name})
	}
	depth := p.depth
	p.depth++
	startNs := p.nowNs()
	ctx := p.traceCtx
	p.mu.Unlock()

	if traceOn {
		if ctx == nil {
			ctx = context.Background()
		}
		region = trace.StartRegion(ctx, name)
	}

	return ScopeToken{
		profiler: p,
		scopeID:  id,
		startNs:  startNs,
		depth:    depth,
		region:   region,
		active:   true,
	}
}

// Scope opens a named timing scope for use with defer: `defer p.Scope("physics").End()`.
func (p *Profiler) Scope(name string) ScopeToken {
	return p.Begin(name)
}

// End closes a timing scope started by Begin or Scope.
func (p *Profiler) End(tok ScopeToken) {
	if p == nil || !tok.active {
		return
	}
	if tok.region != nil {
		tok.region.End()
	}

	p.mu.Lock()
	endNs := p.nowNs()
	if tok.scopeID >= 0 && tok.scopeID < len(p.scopes) {
		durNs := endNs - tok.startNs
		if durNs < 0 {
			durNs = 0
		}
		sc := &p.scopes[tok.scopeID]
		sc.curTotal += durNs
		sc.curCalls++
		if durNs > sc.curMax {
			sc.curMax = durNs
		}
		if p.recording {
			p.events = append(p.events, recordedEvent{
				scopeID: tok.scopeID,
				startNs: tok.startNs,
				durNs:   durNs,
				depth:   tok.depth,
				frame:   p.recordedFrames + 1,
			})
		}
	}
	if p.depth > 0 {
		p.depth--
	}
	p.mu.Unlock()
}

// StartRecording begins recording scope events for up to frames frames (capped by maxRecordedFrames).
func (p *Profiler) StartRecording(frames int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if frames <= 0 || frames > p.maxRecordedFrames {
		frames = p.maxRecordedFrames
	}
	p.recordTarget = frames
	p.recordedFrames = 0
	p.recording = true
	p.recordStartNs = p.nowNs()
	wantCap := frames * 16
	if cap(p.events) < wantCap {
		p.events = make([]recordedEvent, 0, wantCap)
	} else {
		p.events = p.events[:0]
	}
	p.mu.Unlock()
}

// StopRecording ends an active trace recording early, keeping already recorded frames.
func (p *Profiler) StopRecording() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.recording = false
	p.mu.Unlock()
}

// Recording reports whether trace recording is currently active.
func (p *Profiler) Recording() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	r := p.recording
	p.mu.Unlock()
	return r
}

// RecordedFrames returns the number of completed frames captured in the current or most recent recording.
func (p *Profiler) RecordedFrames() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	n := p.recordedFrames
	p.mu.Unlock()
	return n
}

// Reset clears every scope's accumulated timings and frame history, so TopScopes reports nothing until scopes run again. Scope names keep their IDs, so a ScopeToken opened before Reset and ended after it is still charged to its own scope, and an active or finished trace recording (StartRecording, WriteChromeTrace) is left as it is.
func (p *Profiler) Reset() {
	if p == nil {
		return
	}
	p.mu.Lock()
	for i := range p.scopes {
		p.scopes[i] = scopeRecord{name: p.scopes[i].name}
	}
	p.mu.Unlock()
}

// TopScopes returns up to limit scope summaries sorted descending by per-frame Total duration (then Avg, then Name). If limit <= 0, all scopes are returned.
func (p *Profiler) TopScopes(limit int) []ScopeSummary {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if len(p.scopes) == 0 {
		p.mu.Unlock()
		return nil
	}
	out := make([]ScopeSummary, 0, len(p.scopes))
	for i := range p.scopes {
		sc := &p.scopes[i]
		total := sc.lastTotal
		calls := sc.lastCalls
		var avg, peak int64
		if sc.ringCount > 0 {
			var sum int64
			for j := range sc.ringCount {
				v := sc.ring[j]
				sum += v
				if v > peak {
					peak = v
				}
			}
			avg = sum / int64(sc.ringCount)
		} else if sc.curCalls > 0 {
			// Allow inspecting scopes before the first EndFrame completes.
			total = sc.curTotal
			calls = sc.curCalls
			avg = sc.curTotal
			peak = sc.curTotal
		} else {
			continue
		}
		out = append(out, ScopeSummary{
			Name:  sc.name,
			Total: time.Duration(total),
			Avg:   time.Duration(avg),
			Max:   time.Duration(peak),
			Calls: calls,
		})
	}
	p.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		if out[i].Avg != out[j].Avg {
			return out[i].Avg > out[j].Avg
		}
		return out[i].Name < out[j].Name
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

type chromeTraceDocument struct {
	TraceEvents []any `json:"traceEvents"`
}

type chromeMetaEvent struct {
	Name string         `json:"name"`
	Ph   string         `json:"ph"`
	PID  int            `json:"pid"`
	TID  int            `json:"tid"`
	Args map[string]any `json:"args"`
}

type chromeCompleteEvent struct {
	Name string         `json:"name"`
	Cat  string         `json:"cat"`
	Ph   string         `json:"ph"`
	Ts   float64        `json:"ts"`
	Dur  float64        `json:"dur"`
	PID  int            `json:"pid"`
	TID  int            `json:"tid"`
	Args map[string]any `json:"args,omitempty"`
}

// WriteChromeTrace writes recorded scope events in Chrome/Perfetto Trace Event Format JSON to w. Each distinct scope name is assigned its own tid track and thread_name metadata event so Perfetto displays one track per scope.
func (p *Profiler) WriteChromeTrace(w io.Writer) error {
	if p == nil {
		return nil
	}
	if w == nil {
		return errors.New("debug: nil trace writer")
	}

	p.mu.Lock()
	names := make([]string, len(p.scopes))
	for i := range p.scopes {
		names[i] = p.scopes[i].name
	}
	events := make([]recordedEvent, len(p.events))
	copy(events, p.events)
	baseNs := p.recordStartNs
	p.mu.Unlock()

	for _, ev := range events {
		if ev.startNs < baseNs {
			baseNs = ev.startNs
		}
	}

	usedScopes := make([]bool, len(names))
	for _, ev := range events {
		if ev.scopeID >= 0 && ev.scopeID < len(usedScopes) {
			usedScopes[ev.scopeID] = true
		}
	}

	traceEvents := make([]any, 0, len(names)+len(events))
	const pid = 1
	for id, used := range usedScopes {
		if !used {
			continue
		}
		traceEvents = append(traceEvents, chromeMetaEvent{
			Name: "thread_name",
			Ph:   "M",
			PID:  pid,
			TID:  id + 1,
			Args: map[string]any{"name": names[id]},
		})
	}
	for _, ev := range events {
		if ev.scopeID < 0 || ev.scopeID >= len(names) {
			continue
		}
		tsUs := float64(ev.startNs-baseNs) / 1000.0
		if tsUs < 0 {
			tsUs = 0
		}
		durUs := float64(ev.durNs) / 1000.0
		if durUs < 0 {
			durUs = 0
		}
		traceEvents = append(traceEvents, chromeCompleteEvent{
			Name: names[ev.scopeID],
			Cat:  "scope",
			Ph:   "X",
			Ts:   tsUs,
			Dur:  durUs,
			PID:  pid,
			TID:  ev.scopeID + 1,
			Args: map[string]any{
				"frame": ev.frame,
				"depth": ev.depth,
			},
		})
	}

	enc := json.NewEncoder(w)
	return enc.Encode(chromeTraceDocument{TraceEvents: traceEvents})
}
