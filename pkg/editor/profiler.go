package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nsgo-dev/game/internal/gotoolchain"
	"github.com/nsgo-dev/game/pkg/debug"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/nsgo/automation"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// defaultProfilerPollInterval is how often the editor polls game_perf_readout and game_perf_zones while connected to a running game.
	defaultProfilerPollInterval = 250 * time.Millisecond
	// maxProfilerFrames bounds recorded frame history to 60 seconds at 60 fps.
	maxProfilerFrames = 3600
	// profilerStripBars is the number of frame bars shown in the Play-mode 60 s frame time strip (matching PlayMode.dc.html).
	profilerStripBars = 150
	// profilerOverThresholdMs is the half-budget warning line (8.3 ms at 60 fps, matching PlayMode.dc.html).
	profilerOverThresholdMs = 8.3
	// profilerMissedThresholdMs is the missed-frame line (16.7 ms at 60 fps, matching PlayMode.dc.html).
	profilerMissedThresholdMs = 16.7
)

var (
	// ErrProfilerNotConnected is returned by PollProfiler when no running game profiler client is connected.
	ErrProfilerNotConnected = errors.New("editor: profiler is not connected to a running game")
	// ErrNoProfilerFrames is returned by ExportProfilerTrace when no frames have been recorded yet.
	ErrNoProfilerFrames = errors.New("editor: no profiler frames recorded to export")
)

// ProfilerMetric selects which time-series chart is shown in the full Profiler panel.
type ProfilerMetric int

const (
	// ProfilerMetricFrame charts total frame, update, and draw times in milliseconds.
	ProfilerMetricFrame ProfilerMetric = iota
	// ProfilerMetricFPS charts frames per second over time.
	ProfilerMetricFPS
	// ProfilerMetricAllocsGC charts allocations per frame, live heap bytes, and GC hitches over time.
	ProfilerMetricAllocsGC
	// ProfilerMetricMemory charts tracked asset memory against budgets over time.
	ProfilerMetricMemory
)

// String returns the human-readable label for m.
func (m ProfilerMetric) String() string {
	switch m {
	case ProfilerMetricFrame:
		return "Frame"
	case ProfilerMetricFPS:
		return "FPS"
	case ProfilerMetricAllocsGC:
		return "Allocs & GC"
	case ProfilerMetricMemory:
		return "Memory"
	default:
		return "Unknown"
	}
}

// ProfilerClient invokes a running game's MCP performance tools (game_perf_readout, game_perf_zones, game_perf_reset).
// Implementations must be safe to Close more than once.
type ProfilerClient interface {
	CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)
	Close() error
}

// ProfilerFrame is one chronological sample recorded by the editor's profiler from game_perf_readout and game_perf_zones.
type ProfilerFrame struct {
	Timestamp          time.Time
	FrameMs            float64
	UpdateMs           float64
	DrawMs             float64
	FPS                float64
	AllocsPerFrame     float64
	AllocBytesPerFrame float64
	HeapBytes          uint64
	GCCount            uint64
	GCLastPauseMs      float64
	GCMaxPauseMs       float64
	GCRecentHitches    int
	GCHitch            bool
	AssetMemoryBytes   int64
	AssetMemoryBudget  int64
	AssetOverBudget    bool
	Cause              string
	Zones              []mcp.PerfZoneInfo
}

// ProfilerSpike describes the most prominent frame-time spike in the 60 s strip window.
type ProfilerSpike struct {
	// FrameIndex is the index into ProfilerSnapshot.Frames.
	FrameIndex int
	// BarIndex is the index within the visible strip bars (0 .. profilerStripBars-1).
	BarIndex int
	// PositionFrac is the horizontal fraction [0, 1] of the spike bar across the strip chart.
	PositionFrac float64
	// FrameMs is the spike frame duration in milliseconds.
	FrameMs float64
	// Cause is the game-supplied spike explanation or "GC pause" when known, or empty.
	Cause string
	// Label is the formatted callout pill text, such as "18.2 ms · first use of torch_glow compiled its shader".
	Label string
}

// ProfilerSnapshot is an immutable snapshot of the editor's profiler state for UI rendering and tests.
type ProfilerSnapshot struct {
	Connected      bool
	InEditorPlay   bool
	SourceLabel    string
	Open           bool
	Metric         ProfilerMetric
	HasReadout     bool
	Readout        mcp.PerfReadoutResult
	Zones          []mcp.PerfZoneInfo
	Recording      bool
	RecordedFrames int
	Frames         []ProfilerFrame
	Spike          *ProfilerSpike
	LastError      string
}

type scopeBoundProfilerClient interface {
	boundScope() *core.Scope
}

type mcpProfilerClient struct {
	mu      sync.Mutex
	session *sdkmcp.ClientSession
	label   string
}

// ConnectProfilerMCP connects to a running game's MCP server over transport using the official MCP Go SDK.
// Use it with &sdkmcp.CommandTransport{Command: ...}, &sdkmcp.IOTransport{...}, or sdkmcp.NewInMemoryTransports().
func ConnectProfilerMCP(ctx context.Context, transport sdkmcp.Transport) (ProfilerClient, error) {
	if transport == nil {
		return nil, errors.New("editor: nil MCP transport")
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "nsgo-game-editor-profiler",
		Version: "1.0.0",
	}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("editor: connect profiler MCP client: %w", err)
	}
	return &mcpProfilerClient{
		session: session,
		label:   "Connected · MCP",
	}, nil
}

// ConnectProfilerCommand connects to the running game in dir by launching `go tool nsgo mcp` (or custom args when provided) over an MCP stdio CommandTransport.
func ConnectProfilerCommand(ctx context.Context, dir string, args ...string) (ProfilerClient, error) {
	cmdArgs := args
	if len(cmdArgs) == 0 {
		cmdArgs = []string{"tool", "nsgo", "mcp"}
	}
	cmd := gotoolchain.Command(cmdArgs...)
	if dir != "" {
		cmd.Dir = dir
	}
	return ConnectProfilerMCP(ctx, &sdkmcp.CommandTransport{Command: cmd})
}

func (c *mcpProfilerClient) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	c.mu.Lock()
	sess := c.session
	c.mu.Unlock()
	if sess == nil {
		return nil, ErrProfilerNotConnected
	}
	var argVal any
	trimmed := bytes.TrimSpace(args)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		if err := json.Unmarshal(trimmed, &argVal); err != nil {
			return nil, fmt.Errorf("editor: encode MCP args for %s: %w", name, err)
		}
	}
	res, err := sess.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: argVal,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("editor: empty MCP result for %s", name)
	}
	if res.IsError {
		return nil, errors.New(mcpResultText(res))
	}
	if res.StructuredContent != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err == nil && len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
			return raw, nil
		}
	}
	text := strings.TrimSpace(mcpResultText(res))
	if text == "" {
		return json.RawMessage("{}"), nil
	}
	return json.RawMessage(text), nil
}

func (c *mcpProfilerClient) Close() error {
	c.mu.Lock()
	sess := c.session
	c.session = nil
	c.mu.Unlock()
	if sess == nil {
		return nil
	}
	return sess.Close()
}

func mcpResultText(res *sdkmcp.CallToolResult) string {
	var parts []string
	for _, item := range res.Content {
		if tc, ok := item.(*sdkmcp.TextContent); ok && tc.Text != "" {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

type scopeProfilerClient struct {
	cx *core.Scope
}

// ConnectProfilerScope returns a ProfilerClient that calls game_perf_* tools registered on cx via github.com/nsgo-dev/nsgo/automation.
// Call PollProfiler on cx's graph goroutine when invoking manually; background polling dispatches automatically onto cx.
func ConnectProfilerScope(cx *core.Scope) ProfilerClient {
	if cx == nil {
		return nil
	}
	return &scopeProfilerClient{cx: cx}
}

func (c *scopeProfilerClient) boundScope() *core.Scope {
	if c == nil {
		return nil
	}
	return c.cx
}

func (c *scopeProfilerClient) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	if c == nil || c.cx == nil || c.cx.Disposed() {
		return nil, ErrProfilerNotConnected
	}
	return automation.Call(c.cx, ctx, name, args)
}

func (c *scopeProfilerClient) Close() error {
	return nil
}

type bridgeProfilerClient struct {
	handlers map[string]func(context.Context, json.RawMessage) (any, error)
}

// NewBridgeProfilerClient returns a ProfilerClient backed directly by b's tool handlers without registering tools on a core.Scope.
func NewBridgeProfilerClient(b *mcp.Bridge) ProfilerClient {
	if b == nil {
		return nil
	}
	tools := b.Tools()
	handlers := make(map[string]func(context.Context, json.RawMessage) (any, error), len(tools))
	for _, tool := range tools {
		handlers[tool.Name] = tool.Handler
	}
	return &bridgeProfilerClient{handlers: handlers}
}

func (c *bridgeProfilerClient) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	if c == nil {
		return nil, ErrProfilerNotConnected
	}
	h, ok := c.handlers[name]
	if !ok {
		return nil, fmt.Errorf("editor: tool %q is not available on profiler bridge", name)
	}
	val, err := h(ctx, args)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func (c *bridgeProfilerClient) Close() error {
	return nil
}

type profilerState struct {
	mu           sync.Mutex
	seq          uint64
	generation   uint64
	open         bool
	metric       ProfilerMetric
	client       ProfilerClient
	inEditorPlay bool
	sourceLabel  string
	scope        *core.Scope
	pollStop     chan struct{}
	pollDone     chan struct{}
	pollCancel   context.CancelFunc

	hasReadout      bool
	readout         mcp.PerfReadoutResult
	zones           []mcp.PerfZoneInfo
	recording       bool
	recordedFrames  int
	frames          []ProfilerFrame
	lastSampleCount int
	lastHistory     []mcp.PerfFrameSample
	lastErr         string
}

func (p *profilerState) setScope(cx *core.Scope) {
	p.mu.Lock()
	p.scope = cx
	p.mu.Unlock()
}

func (e *Editor) isPlayProfilerActive() bool {
	if e == nil {
		return false
	}
	e.profiler.mu.Lock()
	defer e.profiler.mu.Unlock()
	return e.profiler.client != nil && e.profiler.inEditorPlay
}

func (e *Editor) profilerSeq() uint64 {
	if e == nil {
		return 0
	}
	e.profiler.mu.Lock()
	defer e.profiler.mu.Unlock()
	return e.profiler.seq
}

// ProfilerOpen reports whether the full Profiler panel is currently visible below the viewport.
func (e *Editor) ProfilerOpen() bool {
	if e == nil {
		return false
	}
	e.profiler.mu.Lock()
	defer e.profiler.mu.Unlock()
	return e.profiler.open
}

// SetProfilerOpen opens or closes the full Profiler panel below the viewport.
func (e *Editor) SetProfilerOpen(open bool) {
	if e == nil {
		return
	}
	e.profiler.mu.Lock()
	changed := e.profiler.open != open
	if changed {
		e.profiler.open = open
		e.profiler.seq++
	}
	e.profiler.mu.Unlock()
	if changed {
		e.notify()
	}
}

// ProfilerConnected reports whether the editor's profiler is currently connected to a running game (remote MCP client or active Play-in-editor session).
func (e *Editor) ProfilerConnected() bool {
	if e == nil {
		return false
	}
	e.profiler.mu.Lock()
	defer e.profiler.mu.Unlock()
	return e.profiler.client != nil
}

// ProfilerMetric returns the currently active chart metric in the full Profiler panel.
func (e *Editor) ProfilerMetric() ProfilerMetric {
	if e == nil {
		return ProfilerMetricFrame
	}
	e.profiler.mu.Lock()
	defer e.profiler.mu.Unlock()
	return e.profiler.metric
}

// SetProfilerMetric selects the active time-series chart in the full Profiler panel.
func (e *Editor) SetProfilerMetric(metric ProfilerMetric) {
	if e == nil {
		return
	}
	if metric < ProfilerMetricFrame || metric > ProfilerMetricMemory {
		metric = ProfilerMetricFrame
	}
	e.profiler.mu.Lock()
	changed := e.profiler.metric != metric
	if changed {
		e.profiler.metric = metric
		e.profiler.seq++
	}
	e.profiler.mu.Unlock()
	if changed {
		e.notify()
	}
}

// ConnectProfiler connects the editor's profiler to client, polling game_perf_readout and game_perf_zones while connected.
// Any previously connected profiler client is closed first. Passing nil disconnects the profiler.
func (e *Editor) ConnectProfiler(client ProfilerClient) {
	if e == nil {
		return
	}
	if client == nil {
		_ = e.DisconnectProfiler()
		return
	}
	label := "Connected · live"
	if mc, ok := client.(*mcpProfilerClient); ok && mc.label != "" {
		label = mc.label
	}
	e.attachProfilerClient(client, false, label)
}

func (e *Editor) attachProfilerClient(client ProfilerClient, inEditorPlay bool, label string) {
	e.stopProfilerPoller()

	e.profiler.mu.Lock()
	prev := e.profiler.client
	e.profiler.generation++
	e.profiler.seq++
	e.profiler.client = client
	e.profiler.inEditorPlay = inEditorPlay
	e.profiler.sourceLabel = label
	e.profiler.lastErr = ""
	e.profiler.lastSampleCount = 0
	e.profiler.lastHistory = nil
	e.profiler.frames = nil
	e.profiler.zones = nil
	e.profiler.hasReadout = false
	e.profiler.readout = mcp.PerfReadoutResult{}
	e.profiler.recording = false
	e.profiler.recordedFrames = 0
	hasScope := e.profiler.scope != nil
	e.profiler.mu.Unlock()

	if prev != nil && prev != client {
		_ = prev.Close()
	}

	// Play-in-editor drives PollProfiler on the UI goroutine inside game.Loop.Draw so local
	// debug.Overlay and debug.GCMonitor are never accessed concurrently from a background goroutine.
	if hasScope && !inEditorPlay {
		e.startProfilerPoller()
	}
	e.notify()
}

// DisconnectProfiler stops live polling and closes the connected profiler client, preserving recorded frames so the user can inspect or export them.
func (e *Editor) DisconnectProfiler() error {
	if e == nil {
		return nil
	}
	e.stopProfilerPoller()

	e.profiler.mu.Lock()
	prev := e.profiler.client
	if prev != nil || e.profiler.inEditorPlay || e.profiler.sourceLabel != "" {
		e.profiler.generation++
		e.profiler.seq++
	}
	e.profiler.client = nil
	e.profiler.inEditorPlay = false
	e.profiler.sourceLabel = ""
	e.profiler.mu.Unlock()

	var closeErr error
	if prev != nil {
		closeErr = prev.Close()
	}
	e.postOrNotifyProfiler()
	return closeErr
}

func (e *Editor) detachPlayProfiler() {
	if e == nil {
		return
	}
	e.profiler.mu.Lock()
	isPlay := e.profiler.inEditorPlay
	e.profiler.mu.Unlock()
	if !isPlay {
		return
	}
	_ = e.DisconnectProfiler()
}

func (e *Editor) startProfilerPoller() {
	if e == nil {
		return
	}
	interval := e.ProfilerPollInterval
	if interval <= 0 {
		interval = defaultProfilerPollInterval
	}

	e.profiler.mu.Lock()
	if e.profiler.client == nil || e.profiler.inEditorPlay || e.profiler.pollStop != nil {
		e.profiler.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	pollCtx, pollCancel := context.WithCancel(context.Background())
	e.profiler.pollStop = stopCh
	e.profiler.pollDone = doneCh
	e.profiler.pollCancel = pollCancel
	client := e.profiler.client
	e.profiler.mu.Unlock()

	go func() {
		defer close(doneCh)
		defer pollCancel()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		pollOnce := func() {
			if bound, ok := client.(scopeBoundProfilerClient); ok {
				if targetScope := bound.boundScope(); targetScope != nil && !targetScope.Disposed() {
					done := make(chan struct{})
					targetScope.Post(func() {
						ctx, cancel := context.WithTimeout(pollCtx, 2*time.Second)
						_ = e.PollProfiler(ctx)
						cancel()
						close(done)
					})
					select {
					case <-done:
					case <-stopCh:
					}
					return
				}
			}
			ctx, cancel := context.WithTimeout(pollCtx, 2*time.Second)
			_ = e.PollProfiler(ctx)
			cancel()
		}

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				pollOnce()
			}
		}
	}()
}

func (e *Editor) stopProfilerPoller() {
	if e == nil {
		return
	}
	e.profiler.mu.Lock()
	stopCh := e.profiler.pollStop
	doneCh := e.profiler.pollDone
	cancel := e.profiler.pollCancel
	e.profiler.pollStop = nil
	e.profiler.pollDone = nil
	e.profiler.pollCancel = nil
	e.profiler.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if stopCh != nil {
		close(stopCh)
	}
	if doneCh != nil {
		<-doneCh
	}
}

func (e *Editor) postOrNotifyProfiler() {
	if e == nil {
		return
	}
	e.profiler.mu.Lock()
	scope := e.profiler.scope
	e.profiler.mu.Unlock()
	if scope != nil && !scope.Disposed() {
		scope.Post(func() {
			e.notify()
		})
		return
	}
	e.notify()
}

// PollProfiler queries game_perf_readout (including frame history) and game_perf_zones from the connected game, updates the recorded frame history and zone table, and refreshes the UI.
func (e *Editor) PollProfiler(ctx context.Context) error {
	if e == nil {
		return ErrProfilerNotConnected
	}
	if ctx == nil {
		ctx = context.Background()
	}

	e.profiler.mu.Lock()
	client := e.profiler.client
	gen := e.profiler.generation
	e.profiler.mu.Unlock()
	if client == nil {
		return ErrProfilerNotConnected
	}

	readoutRaw, err := client.CallTool(ctx, mcp.ToolPerfReadout, json.RawMessage(`{"include_history":true,"include_zones":true}`))
	if err != nil {
		e.profiler.mu.Lock()
		stale := e.profiler.generation != gen || e.profiler.client != client
		if !stale {
			e.profiler.lastErr = err.Error()
			e.profiler.seq++
		}
		e.profiler.mu.Unlock()
		if !stale {
			e.postOrNotifyProfiler()
		}
		return err
	}

	var readout mcp.PerfReadoutResult
	if err := json.Unmarshal(readoutRaw, &readout); err != nil {
		e.profiler.mu.Lock()
		stale := e.profiler.generation != gen || e.profiler.client != client
		if !stale {
			e.profiler.lastErr = err.Error()
			e.profiler.seq++
		}
		e.profiler.mu.Unlock()
		if !stale {
			e.postOrNotifyProfiler()
		}
		return fmt.Errorf("editor: decode %s result: %w", mcp.ToolPerfReadout, err)
	}
	sanitizePerfReadout(&readout)

	zones := readout.Zones
	var recording bool
	var recordedFrames int
	if readout.Sources.Profiler {
		if zonesRaw, zErr := client.CallTool(ctx, mcp.ToolPerfZones, json.RawMessage(`{}`)); zErr == nil {
			var zonesRes mcp.PerfZonesResult
			if json.Unmarshal(zonesRaw, &zonesRes) == nil {
				zones = zonesRes.Zones
				recording = zonesRes.Recording
				recordedFrames = zonesRes.RecordedFrames
			}
		}
	}

	now := time.Now()
	e.profiler.mu.Lock()
	if e.profiler.generation != gen || e.profiler.client != client {
		e.profiler.mu.Unlock()
		return nil
	}
	e.profiler.hasReadout = true
	e.profiler.readout = readout
	e.profiler.zones = sortProfilerZones(zones)
	e.profiler.recording = recording
	e.profiler.recordedFrames = recordedFrames
	e.profiler.lastErr = ""
	e.mergeProfilerHistoryLocked(now, readout, e.profiler.zones)
	e.profiler.seq++
	e.profiler.mu.Unlock()

	e.postOrNotifyProfiler()
	return nil
}

func (e *Editor) mergeProfilerHistoryLocked(now time.Time, readout mcp.PerfReadoutResult, zones []mcp.PerfZoneInfo) {
	var assetBytes, assetBudget int64
	var assetOver bool
	if readout.AssetMemory != nil {
		assetBytes = readout.AssetMemory.TotalBytes
		assetBudget = readout.AssetMemory.TotalBudget
		assetOver = readout.AssetMemory.OverBudget
	}

	makeFrame := func(sample mcp.PerfFrameSample, withZones bool) ProfilerFrame {
		fps := readout.FPS
		if sample.FrameMs > 0 {
			fps = 1000.0 / sample.FrameMs
		}
		var frameZones []mcp.PerfZoneInfo
		if withZones && len(zones) > 0 {
			frameZones = slices.Clone(zones)
		}
		return ProfilerFrame{
			Timestamp:          now,
			FrameMs:            sample.FrameMs,
			UpdateMs:           sample.UpdateMs,
			DrawMs:             sample.DrawMs,
			FPS:                fps,
			AllocsPerFrame:     readout.AllocsPerFrame,
			AllocBytesPerFrame: readout.AllocBytesPerFrame,
			HeapBytes:          readout.HeapBytes,
			GCCount:            readout.GCCount,
			GCLastPauseMs:      readout.GCLastPauseMs,
			GCMaxPauseMs:       readout.GCMaxPauseMs,
			GCRecentHitches:    readout.GCRecentHitches,
			GCHitch:            sample.GCHitch,
			AssetMemoryBytes:   assetBytes,
			AssetMemoryBudget:  assetBudget,
			AssetOverBudget:    assetOver,
			Cause:              sample.Cause,
			Zones:              frameZones,
		}
	}

	if len(readout.History) > 0 {
		newCount := profilerHistoryDelta(e.profiler.lastHistory, e.profiler.lastSampleCount, readout.History, readout.SampleCount)
		start := len(readout.History) - newCount
		for i := start; i < len(readout.History); i++ {
			isLast := i == len(readout.History)-1
			e.profiler.frames = append(e.profiler.frames, makeFrame(readout.History[i], isLast))
		}
		if newCount == 0 && len(e.profiler.frames) > 0 && len(zones) > 0 {
			last := &e.profiler.frames[len(e.profiler.frames)-1]
			if len(last.Zones) == 0 {
				last.Zones = slices.Clone(zones)
			}
		}
		e.profiler.lastHistory = slices.Clone(readout.History)
		e.profiler.lastSampleCount = readout.SampleCount
	} else if readout.LatestFrameMs > 0 || readout.FrameMs > 0 || len(zones) > 0 {
		frameMs := readout.LatestFrameMs
		if frameMs <= 0 {
			frameMs = readout.FrameMs
		}
		if frameMs <= 0 {
			for _, z := range zones {
				frameMs += z.LastMs
			}
		}
		sample := mcp.PerfFrameSample{
			FrameMs:  frameMs,
			UpdateMs: readout.LatestUpdateMs,
			DrawMs:   readout.LatestDrawMs,
		}
		if sample.UpdateMs <= 0 {
			sample.UpdateMs = readout.UpdateMs
		}
		if sample.DrawMs <= 0 {
			sample.DrawMs = readout.DrawMs
		}
		e.profiler.frames = append(e.profiler.frames, makeFrame(sample, true))
	}

	if len(e.profiler.frames) > maxProfilerFrames {
		drop := len(e.profiler.frames) - maxProfilerFrames
		e.profiler.frames = slices.Clone(e.profiler.frames[drop:])
	}
}

func profilerHistoryDelta(prev []mcp.PerfFrameSample, prevCount int, cur []mcp.PerfFrameSample, curCount int) int {
	if len(cur) == 0 {
		return 0
	}
	if len(prev) == 0 || prevCount == 0 || curCount < prevCount {
		return len(cur)
	}
	if curCount > prevCount && curCount < debug.FrameHistory {
		delta := curCount - prevCount
		if delta > len(cur) {
			return len(cur)
		}
		return delta
	}
	// Ring buffer is full or sample count unchanged: find the longest suffix of prev that matches a prefix of cur.
	maxOverlap := min(len(prev), len(cur))
	for k := maxOverlap; k >= 1; k-- {
		if slices.Equal(prev[len(prev)-k:], cur[:k]) {
			return len(cur) - k
		}
	}
	return len(cur)
}

func sanitizeNonNegFloat64(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}

func sanitizePerfReadout(r *mcp.PerfReadoutResult) {
	if r == nil {
		return
	}
	r.FrameMs = sanitizeNonNegFloat64(r.FrameMs)
	r.UpdateMs = sanitizeNonNegFloat64(r.UpdateMs)
	r.DrawMs = sanitizeNonNegFloat64(r.DrawMs)
	r.LatestFrameMs = sanitizeNonNegFloat64(r.LatestFrameMs)
	r.LatestUpdateMs = sanitizeNonNegFloat64(r.LatestUpdateMs)
	r.LatestDrawMs = sanitizeNonNegFloat64(r.LatestDrawMs)
	r.P99FrameMs = sanitizeNonNegFloat64(r.P99FrameMs)
	r.FPS = sanitizeNonNegFloat64(r.FPS)
	r.AllocsPerFrame = sanitizeNonNegFloat64(r.AllocsPerFrame)
	r.AllocBytesPerFrame = sanitizeNonNegFloat64(r.AllocBytesPerFrame)
	r.GCLastPauseMs = sanitizeNonNegFloat64(r.GCLastPauseMs)
	r.GCMaxPauseMs = sanitizeNonNegFloat64(r.GCMaxPauseMs)
	for i := range r.History {
		r.History[i].FrameMs = sanitizeNonNegFloat64(r.History[i].FrameMs)
		r.History[i].UpdateMs = sanitizeNonNegFloat64(r.History[i].UpdateMs)
		r.History[i].DrawMs = sanitizeNonNegFloat64(r.History[i].DrawMs)
	}
}

func sortProfilerZones(zones []mcp.PerfZoneInfo) []mcp.PerfZoneInfo {
	if len(zones) == 0 {
		return nil
	}
	out := slices.Clone(zones)
	for i := range out {
		out[i].LastMs = sanitizeNonNegFloat64(out[i].LastMs)
		out[i].AvgMs = sanitizeNonNegFloat64(out[i].AvgMs)
		out[i].MaxMs = sanitizeNonNegFloat64(out[i].MaxMs)
	}
	slices.SortFunc(out, func(a, b mcp.PerfZoneInfo) int {
		if a.LastMs != b.LastMs {
			if a.LastMs > b.LastMs {
				return -1
			}
			return 1
		}
		if a.AvgMs != b.AvgMs {
			if a.AvgMs > b.AvgMs {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// ResetProfiler resets accumulated performance counters in the connected game via game_perf_reset (when connected) and clears the editor's recorded profiler timeline and zone table.
func (e *Editor) ResetProfiler(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	e.profiler.mu.Lock()
	client := e.profiler.client
	e.profiler.mu.Unlock()

	var resetErr error
	if client != nil {
		if _, err := client.CallTool(ctx, mcp.ToolPerfReset, json.RawMessage(`{}`)); err != nil {
			resetErr = err
		}
	}

	e.profiler.mu.Lock()
	e.profiler.generation++
	e.profiler.seq++
	e.profiler.hasReadout = false
	e.profiler.readout = mcp.PerfReadoutResult{}
	e.profiler.zones = nil
	e.profiler.frames = nil
	e.profiler.lastSampleCount = 0
	e.profiler.lastHistory = nil
	e.profiler.recordedFrames = 0
	if resetErr != nil {
		e.profiler.lastErr = resetErr.Error()
	} else {
		e.profiler.lastErr = ""
	}
	e.profiler.mu.Unlock()

	e.postOrNotifyProfiler()
	return resetErr
}

func (e *Editor) resetProfilerFromUI() {
	if e == nil {
		return
	}
	e.profiler.mu.Lock()
	client := e.profiler.client
	isLocal := client == nil || e.profiler.inEditorPlay
	if !isLocal {
		_, isLocal = client.(scopeBoundProfilerClient)
	}
	if !isLocal {
		_, isLocal = client.(*bridgeProfilerClient)
	}
	e.profiler.mu.Unlock()

	if isLocal {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := e.ResetProfiler(ctx)
		cancel()
		e.report(err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := e.ResetProfiler(ctx)
		cancel()
		if err != nil {
			e.profiler.mu.Lock()
			scope := e.profiler.scope
			e.profiler.mu.Unlock()
			if scope != nil && !scope.Disposed() {
				scope.Post(func() { e.report(err) })
			}
		}
	}()
}

// ProfilerSnapshot returns an immutable snapshot of the editor's current profiler readout, recorded frames, sorted zones, and highlighted spike. It is safe to call from any goroutine.
func (e *Editor) ProfilerSnapshot() ProfilerSnapshot {
	if e == nil {
		return ProfilerSnapshot{}
	}
	e.profiler.mu.Lock()
	defer e.profiler.mu.Unlock()

	frames := slices.Clone(e.profiler.frames)
	for i := range frames {
		if len(frames[i].Zones) > 0 {
			frames[i].Zones = slices.Clone(frames[i].Zones)
		}
	}
	zones := slices.Clone(e.profiler.zones)

	readout := e.profiler.readout
	if readout.AssetMemory != nil {
		memCopy := *readout.AssetMemory
		memCopy.Groups = slices.Clone(readout.AssetMemory.Groups)
		readout.AssetMemory = &memCopy
	}
	if readout.Render3D != nil {
		rCopy := *readout.Render3D
		readout.Render3D = &rCopy
	}

	source := e.profiler.sourceLabel
	if source == "" {
		if e.profiler.client != nil {
			source = "Connected · live"
		} else {
			source = "Disconnected"
		}
	}

	return ProfilerSnapshot{
		Connected:      e.profiler.client != nil,
		InEditorPlay:   e.profiler.inEditorPlay,
		SourceLabel:    source,
		Open:           e.profiler.open,
		Metric:         e.profiler.metric,
		HasReadout:     e.profiler.hasReadout,
		Readout:        readout,
		Zones:          zones,
		Recording:      e.profiler.recording,
		RecordedFrames: e.profiler.recordedFrames,
		Frames:         frames,
		Spike:          detectProfilerSpike(frames),
		LastError:      e.profiler.lastErr,
	}
}

func detectProfilerSpike(frames []ProfilerFrame) *ProfilerSpike {
	if len(frames) == 0 {
		return nil
	}
	visibleStart := 0
	if len(frames) > profilerStripBars {
		visibleStart = len(frames) - profilerStripBars
	}
	visible := frames[visibleStart:]
	slotOffset := profilerStripBars - len(visible)

	bestIdx := -1
	bestHasCause := false
	bestMs := 0.0
	for i, f := range visible {
		cause := spikeCauseFor(f)
		hasCause := cause != ""
		isSpike := f.FrameMs > profilerMissedThresholdMs || hasCause
		if !isSpike {
			continue
		}
		if bestIdx < 0 ||
			(hasCause && !bestHasCause) ||
			(hasCause == bestHasCause && f.FrameMs > bestMs) {
			bestIdx = i
			bestHasCause = hasCause
			bestMs = f.FrameMs
		}
	}
	if bestIdx < 0 {
		return nil
	}
	f := visible[bestIdx]
	cause := spikeCauseFor(f)
	label := fmt.Sprintf("%.1f ms", f.FrameMs)
	if cause != "" {
		label = fmt.Sprintf("%.1f ms · %s", f.FrameMs, cause)
	}
	barIndex := slotOffset + bestIdx
	return &ProfilerSpike{
		FrameIndex:   visibleStart + bestIdx,
		BarIndex:     barIndex,
		PositionFrac: (float64(barIndex) + 0.5) / float64(profilerStripBars),
		FrameMs:      f.FrameMs,
		Cause:        cause,
		Label:        label,
	}
}

func spikeCauseFor(f ProfilerFrame) string {
	if s := strings.TrimSpace(f.Cause); s != "" {
		return s
	}
	if f.GCHitch {
		return "GC pause"
	}
	return ""
}

// ExportProfilerTrace exports all recorded frames (and their zones) as Chrome/Perfetto Trace Event Format JSON through debug.Profiler.WriteChromeTrace.
func (e *Editor) ExportProfilerTrace(w io.Writer) error {
	if e == nil {
		return ErrNoProfilerFrames
	}
	if w == nil {
		return errors.New("editor: nil trace writer")
	}

	snap := e.ProfilerSnapshot()
	if len(snap.Frames) == 0 {
		return ErrNoProfilerFrames
	}

	prof := debug.NewProfiler(len(snap.Frames))
	clock := time.Unix(0, 0).UTC()
	prof.SetNow(func() time.Time { return clock })
	prof.StartRecording(len(snap.Frames))

	for _, f := range snap.Frames {
		frameStart := clock
		frameDur := msToDuration(f.FrameMs)
		updDur := msToDuration(f.UpdateMs)
		drawDur := msToDuration(f.DrawMs)
		if frameDur <= 0 {
			frameDur = updDur + drawDur
		}
		if frameDur <= 0 {
			for _, z := range f.Zones {
				frameDur += msToDuration(z.LastMs)
			}
		}
		if frameDur <= 0 {
			frameDur = time.Microsecond
		}

		prof.BeginFrame()
		frameTok := prof.Begin("frame")

		if updDur > 0 {
			uTok := prof.Begin("update")
			clock = clock.Add(updDur)
			prof.End(uTok)
		}
		if drawDur > 0 {
			dTok := prof.Begin("draw")
			clock = clock.Add(drawDur)
			prof.End(dTok)
		}

		frameEnd := frameStart.Add(frameDur)
		if clock.Before(frameEnd) {
			clock = frameEnd
		}
		prof.End(frameTok)

		// Record each named timing zone from the game on its own Perfetto track aligned to the frame start.
		zoneCursor := frameStart
		for _, z := range f.Zones {
			if z.Name == "" || z.Name == "frame" || z.Name == "update" || z.Name == "draw" {
				continue
			}
			zDur := msToDuration(z.LastMs)
			if zDur <= 0 {
				zDur = msToDuration(z.AvgMs)
			}
			if zDur <= 0 {
				continue
			}
			clock = zoneCursor
			zTok := prof.Begin(z.Name)
			zoneCursor = zoneCursor.Add(zDur)
			clock = zoneCursor
			prof.End(zTok)
		}

		if clock.Before(frameEnd) {
			clock = frameEnd
		}
		prof.EndFrame()
	}

	return prof.WriteChromeTrace(w)
}

// ExportProfilerTraceFile writes the recorded profiler frames as Chrome/Perfetto Trace Event Format JSON to path.
func (e *Editor) ExportProfilerTraceFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("editor: empty trace export path")
	}
	var buf bytes.Buffer
	if err := e.ExportProfilerTrace(&buf); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return err
	}
	e.Status = "Exported profiler trace: " + path
	e.notify()
	return nil
}

func (e *Editor) exportProfilerTraceFromUI() {
	if e == nil {
		return
	}
	var buf bytes.Buffer
	if err := e.ExportProfilerTrace(&buf); err != nil {
		e.reportStatusError("Profiler export failed: " + err.Error())
		return
	}
	data := buf.Bytes()
	if e.OnExportProfilerTrace != nil {
		if err := e.OnExportProfilerTrace(data); err != nil {
			e.reportStatusError("Profiler export failed: " + err.Error())
			return
		}
		e.Status = "Exported profiler trace"
		e.notify()
		return
	}
	if e.scope != nil && !e.scope.Disposed() {
		defaultName := "profiler-trace.json"
		if e.Project != nil && e.Project.Name != "" {
			defaultName = strings.ToLower(strings.ReplaceAll(e.Project.Name, " ", "-")) + "-trace.json"
		}
		e.scope.SaveFile(core.SaveOptions{
			Name: defaultName,
			MIME: "application/json",
			Data: data,
		}, func(saved bool, path string, err error) {
			if err != nil {
				e.reportStatusError("Profiler export failed: " + err.Error())
				return
			}
			if !saved {
				return
			}
			if path != "" {
				e.Status = "Exported profiler trace: " + path
			} else {
				e.Status = "Exported profiler trace"
			}
			e.notify()
		})
		return
	}
	e.reportStatusError("Profiler export failed: no export destination configured")
}

func msToDuration(ms float64) time.Duration {
	if ms <= 0 || math.IsNaN(ms) || math.IsInf(ms, 0) {
		return 0
	}
	const maxDurationMs = float64(math.MaxInt64/time.Millisecond) - 1
	if ms >= maxDurationMs {
		return time.Duration(math.MaxInt64 - int64(time.Millisecond))
	}
	return time.Duration(math.Round(ms * float64(time.Millisecond)))
}

func formatProfilerBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	return formatProfilerUintBytes(uint64(n))
}

func formatProfilerUintBytes(n uint64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)
	switch {
	case n >= gib:
		return fmt.Sprintf("%.2f GiB", float64(n)/float64(gib))
	case n >= mib:
		return fmt.Sprintf("%.2f MiB", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(kib))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
