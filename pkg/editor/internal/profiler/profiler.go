// Package profiler is the editor's live profiler: the MCP clients that read a running game's game_perf_* tools, the recorded frame timeline and zone table, Chrome/Perfetto trace export, and the Play-mode strip and docked Profiler panel views. It never imports the editor; the editor builds a Host of callbacks and forwards its public Profiler methods to a State.
package profiler

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
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
	"github.com/nsgo-dev/nsgo/automation"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// defaultPollInterval is how often the editor polls game_perf_readout and game_perf_zones while connected to a running game.
	defaultPollInterval = 250 * time.Millisecond
	// maxFrames bounds recorded frame history to 60 seconds at 60 fps.
	maxFrames = 3600
	// stripBars is the number of frame bars shown in the Play-mode 60 s frame time strip (matching PlayMode.dc.html).
	stripBars = 150
	// overThresholdMs is the half-budget warning line (8.3 ms at 60 fps, matching PlayMode.dc.html).
	overThresholdMs = 8.3
	// missedThresholdMs is the missed-frame line (16.7 ms at 60 fps, matching PlayMode.dc.html).
	missedThresholdMs = 16.7
)

var (
	// ErrNotConnected is returned by Poll when no running game profiler client is connected.
	ErrNotConnected = errors.New("editor: profiler is not connected to a running game")
	// ErrNoFrames is returned by ExportTrace when no frames have been recorded yet.
	ErrNoFrames = errors.New("editor: no profiler frames recorded to export")
)

// Metric selects which time-series chart is shown in the full Profiler panel.
type Metric int

const (
	// MetricFrame charts total frame, update, and draw times in milliseconds.
	MetricFrame Metric = iota
	// MetricFPS charts frames per second over time.
	MetricFPS
	// MetricAllocsGC charts allocations per frame, live heap bytes, and GC hitches over time.
	MetricAllocsGC
	// MetricMemory charts tracked asset memory against budgets over time.
	MetricMemory
)

// String returns the human-readable label for m.
func (m Metric) String() string {
	switch m {
	case MetricFrame:
		return "Frame"
	case MetricFPS:
		return "FPS"
	case MetricAllocsGC:
		return "Allocs & GC"
	case MetricMemory:
		return "Memory"
	default:
		return "Unknown"
	}
}

// Client invokes a running game's MCP performance tools (game_perf_readout, game_perf_zones, game_perf_reset).
// Implementations must be safe to Close more than once.
type Client interface {
	CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)
	Close() error
}

// Frame is one chronological sample recorded by the editor's profiler from game_perf_readout and game_perf_zones.
type Frame struct {
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

// Spike describes the most prominent frame-time spike in the 60 s strip window.
type Spike struct {
	// FrameIndex is the index into Snapshot.Frames.
	FrameIndex int
	// BarIndex is the index within the visible strip bars (0 .. stripBars-1).
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

// Snapshot is an immutable snapshot of the editor's profiler state for UI rendering and tests.
type Snapshot struct {
	Connected      bool
	InEditorPlay   bool
	SourceLabel    string
	Open           bool
	Metric         Metric
	HasReadout     bool
	Readout        mcp.PerfReadoutResult
	Zones          []mcp.PerfZoneInfo
	Recording      bool
	RecordedFrames int
	Frames         []Frame
	Spike          *Spike
	LastError      string
}

type scopeBoundClient interface {
	boundScope() *core.Scope
}

type mcpClient struct {
	mu      sync.Mutex
	session *sdkmcp.ClientSession
	label   string
}

// ConnectMCP connects to a running game's MCP server over transport using the official MCP Go SDK.
// Use it with &sdkmcp.CommandTransport{Command: ...}, &sdkmcp.IOTransport{...}, or sdkmcp.NewInMemoryTransports().
func ConnectMCP(ctx context.Context, transport sdkmcp.Transport) (Client, error) {
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
	return &mcpClient{
		session: session,
		label:   "Connected · MCP",
	}, nil
}

// ConnectCommand connects to the running game in dir by launching `go tool nsgo mcp` (or custom args when provided) over an MCP stdio CommandTransport.
func ConnectCommand(ctx context.Context, dir string, args ...string) (Client, error) {
	cmdArgs := args
	if len(cmdArgs) == 0 {
		cmdArgs = []string{"tool", "nsgo", "mcp"}
	}
	cmd := gotoolchain.Command(cmdArgs...)
	if dir != "" {
		cmd.Dir = dir
	}
	return ConnectMCP(ctx, &sdkmcp.CommandTransport{Command: cmd})
}

func (c *mcpClient) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	c.mu.Lock()
	sess := c.session
	c.mu.Unlock()
	if sess == nil {
		return nil, ErrNotConnected
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

func (c *mcpClient) Close() error {
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

type scopeClient struct {
	cx *core.Scope
}

// ConnectScope returns a Client that calls game_perf_* tools registered on cx via github.com/nsgo-dev/nsgo/automation.
// Call Poll on cx's graph goroutine when invoking manually; background polling dispatches automatically onto cx.
func ConnectScope(cx *core.Scope) Client {
	if cx == nil {
		return nil
	}
	return &scopeClient{cx: cx}
}

func (c *scopeClient) boundScope() *core.Scope {
	if c == nil {
		return nil
	}
	return c.cx
}

func (c *scopeClient) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	if c == nil || c.cx == nil || c.cx.Disposed() {
		return nil, ErrNotConnected
	}
	return automation.Call(c.cx, ctx, name, args)
}

func (c *scopeClient) Close() error {
	return nil
}

type bridgeClient struct {
	handlers map[string]func(context.Context, json.RawMessage) (any, error)
}

// NewBridgeClient returns a Client backed directly by b's tool handlers without registering tools on a core.Scope.
func NewBridgeClient(b *mcp.Bridge) Client {
	if b == nil {
		return nil
	}
	tools := b.Tools()
	handlers := make(map[string]func(context.Context, json.RawMessage) (any, error), len(tools))
	for _, tool := range tools {
		handlers[tool.Name] = tool.Handler
	}
	return &bridgeClient{handlers: handlers}
}

func (c *bridgeClient) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	if c == nil {
		return nil, ErrNotConnected
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

func (c *bridgeClient) Close() error {
	return nil
}

// Host is what the profiler needs from the editor that owns it. Every func must be set; New does not fill in defaults.
type Host struct {
	// Notify refreshes the editor UI after a profiler change; it runs on the UI goroutine.
	Notify func()
	// Report shows err in the status bar and the Output panel; nil is ignored.
	Report func(err error)
	// ReportStatusError shows msg as an error in the status bar and the Output panel.
	ReportStatusError func(msg string)
	// SetStatus sets the status bar text without notifying; callers call Notify afterwards.
	SetStatus func(status string)
	// Playing reports whether Play mode is running in the editor.
	Playing func() bool
	// MarkFrames returns the simulation frame of each Play mark, in mark order.
	MarkFrames func() []int
	// CurrentPlayFrameAndRate returns the current simulation frame and the fixed step rate of the game in Play mode or the connected running game.
	CurrentPlayFrameAndRate func() (frame int, rate int)
	// ReplayWindowSeconds is the length in seconds of the rolling replay window the strip's mark pins are placed in.
	ReplayWindowSeconds int
	// ProjectName returns the open project's name, or "" when no project is open.
	ProjectName func() string
	// Palette returns the editor's current design tokens for cx.
	Palette func(cx *core.Scope) theme.Palette
	// DockedStyle returns fallback adjusted to fill its dock slot when cx renders inside the editor's dock area.
	DockedStyle func(cx *core.Scope, fallback core.Style) core.Style
	// OnExportTrace returns the callback the Export Trace button hands the trace JSON to, or nil to open a Save File dialog. It is read on every export.
	OnExportTrace func() func(data []byte) error
	// T translates key into the editor's current language, filling in args, as Editor.T does.
	T func(key string, args ...l10n.Arg) string
	// PollInterval returns how often to poll a connected running game; zero or less means 250 ms. It is read each time polling starts.
	PollInterval func() time.Duration
}

// State is the profiler's connection, recorded frame timeline, zone table and panel visibility. Its methods are safe on a nil *State, which acts as a disconnected profiler that records nothing.
type State struct {
	host Host

	mu           sync.Mutex
	seq          uint64
	generation   uint64
	open         bool
	metric       Metric
	client       Client
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
	frames          []Frame
	lastSampleCount int
	lastHistory     []mcp.PerfFrameSample
	lastErr         string
}

// New returns a disconnected profiler that calls back into host.
func New(host Host) *State {
	return &State{host: host}
}

// SetScope binds the editor's UI scope, onto which background polls post their UI refreshes and from which the Save File dialog of a trace export opens.
func (s *State) SetScope(cx *core.Scope) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.scope = cx
	s.mu.Unlock()
}

// PlayActive reports whether the profiler is attached to the editor's own Play-mode session.
func (s *State) PlayActive() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client != nil && s.inEditorPlay
}

// Seq returns a counter that changes whenever the profiler's state does, for views to memoize on.
func (s *State) Seq() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq
}

// Open reports whether the full Profiler panel is currently visible below the viewport.
func (s *State) Open() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.open
}

// SetOpen opens or closes the full Profiler panel below the viewport.
func (s *State) SetOpen(open bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	changed := s.open != open
	if changed {
		s.open = open
		s.seq++
	}
	s.mu.Unlock()
	if changed {
		s.host.Notify()
	}
}

// Connected reports whether the editor's profiler is currently connected to a running game (remote MCP client or active Play-in-editor session).
func (s *State) Connected() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client != nil
}

// Metric returns the currently active chart metric in the full Profiler panel.
func (s *State) Metric() Metric {
	if s == nil {
		return MetricFrame
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metric
}

// SetMetric selects the active time-series chart in the full Profiler panel.
func (s *State) SetMetric(metric Metric) {
	if s == nil {
		return
	}
	if metric < MetricFrame || metric > MetricMemory {
		metric = MetricFrame
	}
	s.mu.Lock()
	changed := s.metric != metric
	if changed {
		s.metric = metric
		s.seq++
	}
	s.mu.Unlock()
	if changed {
		s.host.Notify()
	}
}

// Connect connects the editor's profiler to client, polling game_perf_readout and game_perf_zones while connected.
// Any previously connected profiler client is closed first. Passing nil disconnects the profiler.
func (s *State) Connect(client Client) {
	if s == nil {
		return
	}
	if client == nil {
		_ = s.Disconnect()
		return
	}
	label := "Connected · live"
	if mc, ok := client.(*mcpClient); ok && mc.label != "" {
		label = mc.label
	}
	s.Attach(client, false, label)
}

// Attach connects client under the source label label, clearing the recorded frames. inEditorPlay marks the editor's own Play-mode session, which the editor polls from its game loop instead of a background poller.
func (s *State) Attach(client Client, inEditorPlay bool, label string) {
	if s == nil {
		return
	}
	s.StopPoller()

	s.mu.Lock()
	prev := s.client
	s.generation++
	s.seq++
	s.client = client
	s.inEditorPlay = inEditorPlay
	s.sourceLabel = label
	s.lastErr = ""
	s.lastSampleCount = 0
	s.lastHistory = nil
	s.frames = nil
	s.zones = nil
	s.hasReadout = false
	s.readout = mcp.PerfReadoutResult{}
	s.recording = false
	s.recordedFrames = 0
	hasScope := s.scope != nil
	s.mu.Unlock()

	if prev != nil && prev != client {
		_ = prev.Close()
	}

	// Play-in-editor drives Poll on the UI goroutine inside game.Loop.Draw so local
	// gamedebug.Overlay and gamedebug.GCMonitor are never accessed concurrently from a background goroutine.
	if hasScope && !inEditorPlay {
		s.StartPoller()
	}
	s.host.Notify()
}

// Disconnect stops live polling and closes the connected profiler client, preserving recorded frames so the user can inspect or export them.
func (s *State) Disconnect() error {
	if s == nil {
		return nil
	}
	s.StopPoller()

	s.mu.Lock()
	prev := s.client
	if prev != nil || s.inEditorPlay || s.sourceLabel != "" {
		s.generation++
		s.seq++
	}
	s.client = nil
	s.inEditorPlay = false
	s.sourceLabel = ""
	s.mu.Unlock()

	var closeErr error
	if prev != nil {
		closeErr = prev.Close()
	}
	s.postOrNotify()
	return closeErr
}

// DetachPlay disconnects the profiler when it is attached to the editor's Play-mode session and leaves a remote connection alone.
func (s *State) DetachPlay() {
	if s == nil {
		return
	}
	s.mu.Lock()
	isPlay := s.inEditorPlay
	s.mu.Unlock()
	if !isPlay {
		return
	}
	_ = s.Disconnect()
}

// StartPoller starts polling a connected remote game in the background, unless polling already runs or the profiler is attached to Play mode.
func (s *State) StartPoller() {
	if s == nil {
		return
	}
	interval := s.host.PollInterval()
	if interval <= 0 {
		interval = defaultPollInterval
	}

	s.mu.Lock()
	if s.client == nil || s.inEditorPlay || s.pollStop != nil {
		s.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	pollCtx, pollCancel := context.WithCancel(context.Background())
	s.pollStop = stopCh
	s.pollDone = doneCh
	s.pollCancel = pollCancel
	client := s.client
	s.mu.Unlock()

	go func() {
		defer close(doneCh)
		defer pollCancel()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		pollOnce := func() {
			if bound, ok := client.(scopeBoundClient); ok {
				if targetScope := bound.boundScope(); targetScope != nil && !targetScope.Disposed() {
					done := make(chan struct{})
					targetScope.Post(func() {
						ctx, cancel := context.WithTimeout(pollCtx, 2*time.Second)
						_ = s.Poll(ctx)
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
			_ = s.Poll(ctx)
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

// StopPoller stops background polling and waits for an in-flight poll to finish.
func (s *State) StopPoller() {
	if s == nil {
		return
	}
	s.mu.Lock()
	stopCh := s.pollStop
	doneCh := s.pollDone
	cancel := s.pollCancel
	s.pollStop = nil
	s.pollDone = nil
	s.pollCancel = nil
	s.mu.Unlock()

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

func (s *State) postOrNotify() {
	if s == nil {
		return
	}
	s.mu.Lock()
	scope := s.scope
	s.mu.Unlock()
	if scope != nil && !scope.Disposed() {
		scope.Post(func() {
			s.host.Notify()
		})
		return
	}
	s.host.Notify()
}

// Poll queries game_perf_readout (including frame history) and game_perf_zones from the connected game, updates the recorded frame history and zone table, and refreshes the UI.
func (s *State) Poll(ctx context.Context) error {
	if s == nil {
		return ErrNotConnected
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	client := s.client
	gen := s.generation
	s.mu.Unlock()
	if client == nil {
		return ErrNotConnected
	}

	readoutRaw, err := client.CallTool(ctx, mcp.ToolPerfReadout, json.RawMessage(`{"include_history":true,"include_zones":true}`))
	if err != nil {
		s.mu.Lock()
		stale := s.generation != gen || s.client != client
		if !stale {
			s.lastErr = err.Error()
			s.seq++
		}
		s.mu.Unlock()
		if !stale {
			s.postOrNotify()
		}
		return err
	}

	var readout mcp.PerfReadoutResult
	if err := json.Unmarshal(readoutRaw, &readout); err != nil {
		s.mu.Lock()
		stale := s.generation != gen || s.client != client
		if !stale {
			s.lastErr = err.Error()
			s.seq++
		}
		s.mu.Unlock()
		if !stale {
			s.postOrNotify()
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
	s.mu.Lock()
	if s.generation != gen || s.client != client {
		s.mu.Unlock()
		return nil
	}
	s.hasReadout = true
	s.readout = readout
	s.zones = sortZones(zones)
	s.recording = recording
	s.recordedFrames = recordedFrames
	s.lastErr = ""
	s.mergeHistoryLocked(now, readout, s.zones)
	s.seq++
	s.mu.Unlock()

	s.postOrNotify()
	return nil
}

func (s *State) mergeHistoryLocked(now time.Time, readout mcp.PerfReadoutResult, zones []mcp.PerfZoneInfo) {
	var assetBytes, assetBudget int64
	var assetOver bool
	if readout.AssetMemory != nil {
		assetBytes = readout.AssetMemory.TotalBytes
		assetBudget = readout.AssetMemory.TotalBudget
		assetOver = readout.AssetMemory.OverBudget
	}

	makeFrame := func(sample mcp.PerfFrameSample, withZones bool) Frame {
		fps := readout.FPS
		if sample.FrameMs > 0 {
			fps = 1000.0 / sample.FrameMs
		}
		var frameZones []mcp.PerfZoneInfo
		if withZones && len(zones) > 0 {
			frameZones = slices.Clone(zones)
		}
		return Frame{
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
		newCount := historyDelta(s.lastHistory, s.lastSampleCount, readout.History, readout.SampleCount)
		start := len(readout.History) - newCount
		for i := start; i < len(readout.History); i++ {
			isLast := i == len(readout.History)-1
			s.frames = append(s.frames, makeFrame(readout.History[i], isLast))
		}
		if newCount == 0 && len(s.frames) > 0 && len(zones) > 0 {
			last := &s.frames[len(s.frames)-1]
			if len(last.Zones) == 0 {
				last.Zones = slices.Clone(zones)
			}
		}
		s.lastHistory = slices.Clone(readout.History)
		s.lastSampleCount = readout.SampleCount
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
		s.frames = append(s.frames, makeFrame(sample, true))
	}

	if len(s.frames) > maxFrames {
		drop := len(s.frames) - maxFrames
		s.frames = slices.Clone(s.frames[drop:])
	}
}

func historyDelta(prev []mcp.PerfFrameSample, prevCount int, cur []mcp.PerfFrameSample, curCount int) int {
	if len(cur) == 0 {
		return 0
	}
	if len(prev) == 0 || prevCount == 0 || curCount < prevCount {
		return len(cur)
	}
	if curCount > prevCount && curCount < gamedebug.FrameHistory {
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

func sortZones(zones []mcp.PerfZoneInfo) []mcp.PerfZoneInfo {
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

// Reset resets accumulated performance counters in the connected game via game_perf_reset (when connected) and clears the editor's recorded profiler timeline and zone table.
func (s *State) Reset(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	var resetErr error
	if client != nil {
		if _, err := client.CallTool(ctx, mcp.ToolPerfReset, json.RawMessage(`{}`)); err != nil {
			resetErr = err
		}
	}

	s.mu.Lock()
	s.generation++
	s.seq++
	s.hasReadout = false
	s.readout = mcp.PerfReadoutResult{}
	s.zones = nil
	s.frames = nil
	s.lastSampleCount = 0
	s.lastHistory = nil
	s.recordedFrames = 0
	if resetErr != nil {
		s.lastErr = resetErr.Error()
	} else {
		s.lastErr = ""
	}
	s.mu.Unlock()

	s.postOrNotify()
	return resetErr
}

// ResetFromUI resets the profiler for the panel's Reset button, off the UI goroutine when the client is a remote game, and reports a failure in the status bar.
func (s *State) ResetFromUI() {
	if s == nil {
		return
	}
	s.mu.Lock()
	client := s.client
	isLocal := client == nil || s.inEditorPlay
	if !isLocal {
		_, isLocal = client.(scopeBoundClient)
	}
	if !isLocal {
		_, isLocal = client.(*bridgeClient)
	}
	s.mu.Unlock()

	if isLocal {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := s.Reset(ctx)
		cancel()
		s.host.Report(err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := s.Reset(ctx)
		cancel()
		if err != nil {
			s.mu.Lock()
			scope := s.scope
			s.mu.Unlock()
			if scope != nil && !scope.Disposed() {
				scope.Post(func() { s.host.Report(err) })
			}
		}
	}()
}

// Snapshot returns an immutable snapshot of the editor's current profiler readout, recorded frames, sorted zones, and highlighted spike. It is safe to call from any goroutine.
func (s *State) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	frames := slices.Clone(s.frames)
	for i := range frames {
		if len(frames[i].Zones) > 0 {
			frames[i].Zones = slices.Clone(frames[i].Zones)
		}
	}
	zones := slices.Clone(s.zones)

	readout := s.readout
	if readout.AssetMemory != nil {
		memCopy := *readout.AssetMemory
		memCopy.Groups = slices.Clone(readout.AssetMemory.Groups)
		readout.AssetMemory = &memCopy
	}
	if readout.Render3D != nil {
		rCopy := *readout.Render3D
		readout.Render3D = &rCopy
	}

	source := s.sourceLabel
	if source == "" {
		if s.client != nil {
			source = s.host.T("profiler.connectedLive")
		} else {
			source = s.host.T("profiler.disconnected")
		}
	}

	return Snapshot{
		Connected:      s.client != nil,
		InEditorPlay:   s.inEditorPlay,
		SourceLabel:    source,
		Open:           s.open,
		Metric:         s.metric,
		HasReadout:     s.hasReadout,
		Readout:        readout,
		Zones:          zones,
		Recording:      s.recording,
		RecordedFrames: s.recordedFrames,
		Frames:         frames,
		Spike:          detectSpike(frames),
		LastError:      s.lastErr,
	}
}

func detectSpike(frames []Frame) *Spike {
	if len(frames) == 0 {
		return nil
	}
	visibleStart := 0
	if len(frames) > stripBars {
		visibleStart = len(frames) - stripBars
	}
	visible := frames[visibleStart:]
	slotOffset := stripBars - len(visible)

	bestIdx := -1
	bestHasCause := false
	bestMs := 0.0
	for i, f := range visible {
		cause := spikeCauseFor(f)
		hasCause := cause != ""
		isSpike := f.FrameMs > missedThresholdMs || hasCause
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
	return &Spike{
		FrameIndex:   visibleStart + bestIdx,
		BarIndex:     barIndex,
		PositionFrac: (float64(barIndex) + 0.5) / float64(stripBars),
		FrameMs:      f.FrameMs,
		Cause:        cause,
		Label:        label,
	}
}

func spikeCauseFor(f Frame) string {
	if s := strings.TrimSpace(f.Cause); s != "" {
		return s
	}
	if f.GCHitch {
		return "GC pause"
	}
	return ""
}

// ExportTrace exports all recorded frames (and their zones) as Chrome/Perfetto Trace Event Format JSON through gamedebug.Profiler.WriteChromeTrace.
func (s *State) ExportTrace(w io.Writer) error {
	if s == nil {
		return ErrNoFrames
	}
	if w == nil {
		return errors.New("editor: nil trace writer")
	}

	snap := s.Snapshot()
	if len(snap.Frames) == 0 {
		return ErrNoFrames
	}

	prof := gamedebug.NewProfiler(len(snap.Frames))
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

// ExportTraceFile writes the recorded profiler frames as Chrome/Perfetto Trace Event Format JSON to path.
func (s *State) ExportTraceFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("editor: empty trace export path")
	}
	var buf bytes.Buffer
	if err := s.ExportTrace(&buf); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return err
	}
	s.host.SetStatus("Exported profiler trace: " + path)
	s.host.Notify()
	return nil
}

// ExportTraceFromUI exports the recorded frames for the panel's Export Trace button: to Host.OnExportTrace when it returns a callback, otherwise through a Save File dialog on the bound scope.
func (s *State) ExportTraceFromUI() {
	if s == nil {
		return
	}
	var buf bytes.Buffer
	if err := s.ExportTrace(&buf); err != nil {
		s.host.ReportStatusError("Profiler export failed: " + err.Error())
		return
	}
	data := buf.Bytes()
	if onExport := s.host.OnExportTrace(); onExport != nil {
		if err := onExport(data); err != nil {
			s.host.ReportStatusError("Profiler export failed: " + err.Error())
			return
		}
		s.host.SetStatus("Exported profiler trace")
		s.host.Notify()
		return
	}
	s.mu.Lock()
	scope := s.scope
	s.mu.Unlock()
	if scope != nil && !scope.Disposed() {
		defaultName := "profiler-trace.json"
		if name := s.host.ProjectName(); name != "" {
			defaultName = strings.ToLower(strings.ReplaceAll(name, " ", "-")) + "-trace.json"
		}
		scope.SaveFile(core.SaveOptions{
			Name: defaultName,
			MIME: "application/json",
			Data: data,
		}, func(saved bool, path string, err error) {
			if err != nil {
				s.host.ReportStatusError("Profiler export failed: " + err.Error())
				return
			}
			if !saved {
				return
			}
			if path != "" {
				s.host.SetStatus("Exported profiler trace: " + path)
			} else {
				s.host.SetStatus("Exported profiler trace")
			}
			s.host.Notify()
		})
		return
	}
	s.host.ReportStatusError("Profiler export failed: no export destination configured")
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

func formatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	return formatUintBytes(uint64(n))
}

func formatUintBytes(n uint64) string {
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
