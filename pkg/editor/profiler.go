package editor

import (
	"context"
	"io"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nsgo-dev/game/pkg/editor/internal/profiler"
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/nsgo/core"
)

var (
	// ErrProfilerNotConnected is returned by PollProfiler when no running game profiler client is connected.
	ErrProfilerNotConnected = profiler.ErrNotConnected
	// ErrNoProfilerFrames is returned by ExportProfilerTrace when no frames have been recorded yet.
	ErrNoProfilerFrames = profiler.ErrNoFrames
)

type (
	// ProfilerMetric selects which time-series chart is shown in the full Profiler panel.
	ProfilerMetric = profiler.Metric
	// ProfilerClient invokes a running game's MCP performance tools (game_perf_readout, game_perf_zones, game_perf_reset). Implementations must be safe to Close more than once.
	ProfilerClient = profiler.Client
	// ProfilerFrame is one chronological sample recorded by the editor's profiler from game_perf_readout and game_perf_zones.
	ProfilerFrame = profiler.Frame
	// ProfilerSpike describes the most prominent frame-time spike in the 60 s strip window.
	ProfilerSpike = profiler.Spike
	// ProfilerSnapshot is an immutable snapshot of the editor's profiler state for UI rendering and tests.
	ProfilerSnapshot = profiler.Snapshot
)

const (
	// ProfilerMetricFrame charts total frame, update, and draw times in milliseconds.
	ProfilerMetricFrame = profiler.MetricFrame
	// ProfilerMetricFPS charts frames per second over time.
	ProfilerMetricFPS = profiler.MetricFPS
	// ProfilerMetricAllocsGC charts allocations per frame, live heap bytes, and GC hitches over time.
	ProfilerMetricAllocsGC = profiler.MetricAllocsGC
	// ProfilerMetricMemory charts tracked asset memory against budgets over time.
	ProfilerMetricMemory = profiler.MetricMemory
)

// ConnectProfilerMCP connects to a running game's MCP server over transport using the official MCP Go SDK.
// Use it with &sdkmcp.CommandTransport{Command: ...}, &sdkmcp.IOTransport{...}, or sdkmcp.NewInMemoryTransports().
func ConnectProfilerMCP(ctx context.Context, transport sdkmcp.Transport) (ProfilerClient, error) {
	return profiler.ConnectMCP(ctx, transport)
}

// ConnectProfilerCommand connects to the running game in dir by launching `go tool nsgo mcp` (or custom args when provided) over an MCP stdio CommandTransport.
func ConnectProfilerCommand(ctx context.Context, dir string, args ...string) (ProfilerClient, error) {
	return profiler.ConnectCommand(ctx, dir, args...)
}

// ConnectProfilerScope returns a ProfilerClient that calls game_perf_* tools registered on cx via github.com/nsgo-dev/nsgo/automation.
// Call PollProfiler on cx's graph goroutine when invoking manually; background polling dispatches automatically onto cx.
func ConnectProfilerScope(cx *core.Scope) ProfilerClient {
	return profiler.ConnectScope(cx)
}

// NewBridgeProfilerClient returns a ProfilerClient backed directly by b's tool handlers without registering tools on a core.Scope.
func NewBridgeProfilerClient(b *mcp.Bridge) ProfilerClient {
	return profiler.NewBridgeClient(b)
}

// ProfilerStripView renders the Play-mode "FRAME TIME · LAST 60 S" strip directly below the game view while Play mode is active or a running game is connected to the profiler, matching design/editor/PlayMode.dc.html. Pressing the strip's Profiler button opens the full Profiler panel.
func ProfilerStripView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return profiler.StripView(cx, ed.profiler, rev)
}

// ProfilerPanelView renders the full docked Profiler panel below the viewport when Editor.ProfilerOpen reports true.
// It charts frame, update and draw time, FPS, allocations per frame, GC and asset memory over time, lists profiler zones from game_perf_zones sorted by cost, and provides Reset (game_perf_reset) and Chrome/Perfetto trace export through gamedebug.Profiler.WriteChromeTrace.
func ProfilerPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return profiler.PanelView(cx, ed.profiler, rev)
}

// newProfilerHost returns the callbacks the profiler reads the editor through.
func (e *Editor) newProfilerHost() profiler.Host {
	return profiler.Host{
		Notify:            e.notify,
		Report:            e.report,
		ReportStatusError: e.reportStatusError,
		SetStatus:         func(status string) { e.Status = status },
		Playing:           e.Playing,
		MarkFrames: func() []int {
			marks := e.PlayMarks()
			frames := make([]int, len(marks))
			for i, m := range marks {
				frames[i] = m.Frame
			}
			return frames
		},
		CurrentPlayFrameAndRate: e.currentPlayFrameAndRate,
		ReplayWindowSeconds:     rollingReplayWindowSeconds,
		ProjectName: func() string {
			if e.Project == nil {
				return ""
			}
			return e.Project.Name
		},
		Palette:       func(cx *core.Scope) theme.Palette { return e.Palette(cx) },
		T:             e.T,
		DockedStyle:   dockedStyle,
		OnExportTrace: func() func(data []byte) error { return e.OnExportProfilerTrace },
		PollInterval:  func() time.Duration { return e.ProfilerPollInterval },
	}
}

// prof returns the editor's profiler, nil for a nil editor or one not made by New, which the profiler treats as disconnected.
func (e *Editor) prof() *profiler.State {
	if e == nil {
		return nil
	}
	return e.profiler
}

// ProfilerOpen reports whether the full Profiler panel is currently visible below the viewport.
func (e *Editor) ProfilerOpen() bool { return e.prof().Open() }

// SetProfilerOpen opens or closes the full Profiler panel below the viewport.
func (e *Editor) SetProfilerOpen(open bool) { e.prof().SetOpen(open) }

// ProfilerConnected reports whether the editor's profiler is currently connected to a running game (remote MCP client or active Play-in-editor session).
func (e *Editor) ProfilerConnected() bool { return e.prof().Connected() }

// ProfilerMetric returns the currently active chart metric in the full Profiler panel.
func (e *Editor) ProfilerMetric() ProfilerMetric { return e.prof().Metric() }

// SetProfilerMetric selects the active time-series chart in the full Profiler panel.
func (e *Editor) SetProfilerMetric(metric ProfilerMetric) { e.prof().SetMetric(metric) }

// ConnectProfiler connects the editor's profiler to client, polling game_perf_readout and game_perf_zones while connected.
// Any previously connected profiler client is closed first. Passing nil disconnects the profiler.
func (e *Editor) ConnectProfiler(client ProfilerClient) { e.prof().Connect(client) }

// DisconnectProfiler stops live polling and closes the connected profiler client, preserving recorded frames so the user can inspect or export them.
func (e *Editor) DisconnectProfiler() error { return e.prof().Disconnect() }

// PollProfiler queries game_perf_readout (including frame history) and game_perf_zones from the connected game, updates the recorded frame history and zone table, and refreshes the UI.
func (e *Editor) PollProfiler(ctx context.Context) error { return e.prof().Poll(ctx) }

// ResetProfiler resets accumulated performance counters in the connected game via game_perf_reset (when connected) and clears the editor's recorded profiler timeline and zone table.
func (e *Editor) ResetProfiler(ctx context.Context) error { return e.prof().Reset(ctx) }

// ProfilerSnapshot returns an immutable snapshot of the editor's current profiler readout, recorded frames, sorted zones, and highlighted spike. It is safe to call from any goroutine.
func (e *Editor) ProfilerSnapshot() ProfilerSnapshot { return e.prof().Snapshot() }

// ExportProfilerTrace exports all recorded frames (and their zones) as Chrome/Perfetto Trace Event Format JSON through gamedebug.Profiler.WriteChromeTrace.
func (e *Editor) ExportProfilerTrace(w io.Writer) error { return e.prof().ExportTrace(w) }

// ExportProfilerTraceFile writes the recorded profiler frames as Chrome/Perfetto Trace Event Format JSON to path.
func (e *Editor) ExportProfilerTraceFile(path string) error { return e.prof().ExportTraceFile(path) }
