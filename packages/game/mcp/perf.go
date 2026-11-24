package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/nsgo/automation"
)

// Performance readout tool names registered by Bridge with github.com/nsgo-dev/nsgo/automation.
const (
	// ToolPerfReadout reads live frame timing, draw statistics, allocation/GC telemetry, and optional frame history or profiler zones.
	ToolPerfReadout = "game_perf_readout"
	// ToolPerfZones lists named profiler timing zones sorted by recent cost.
	ToolPerfZones = "game_perf_zones"
	// ToolPerfReset resets accumulated overlay frame stats, profiler zones, and GC monitor history.
	ToolPerfReset = "game_perf_reset"
)

// PerfSources reports which telemetry providers contributed to a performance readout.
type PerfSources struct {
	Overlay   bool `json:"overlay"`
	Profiler  bool `json:"profiler"`
	GC        bool `json:"gc"`
	Memory    bool `json:"memory"`
	DrawStats bool `json:"draw_stats"`
	Render3D  bool `json:"render3d"`
}

// PerfFrameSample is one chronological frame timing entry from the debug overlay's ring buffer.
type PerfFrameSample struct {
	FrameMs  float64 `json:"frame_ms"`
	UpdateMs float64 `json:"update_ms"`
	DrawMs   float64 `json:"draw_ms"`
	GCHitch  bool    `json:"gc_hitch,omitempty"`
}

// PerfZoneInfo summarizes one named timing scope from debug.Profiler.
type PerfZoneInfo struct {
	Name   string  `json:"name"`
	LastMs float64 `json:"last_ms"`
	AvgMs  float64 `json:"avg_ms"`
	MaxMs  float64 `json:"max_ms"`
	Calls  int     `json:"calls"`
}

// PerfRender3DStats mirrors render3d.RenderStats in snake_case JSON form.
type PerfRender3DStats struct {
	TotalItems       int    `json:"total_items"`
	CulledItems      int    `json:"culled_items"`
	Occluded         int    `json:"occluded"`
	VisibleItems     int    `json:"visible_items"`
	OpaqueDraws      int    `json:"opaque_draws"`
	TransparentDraws int    `json:"transparent_draws"`
	InstancedBatches int    `json:"instanced_batches"`
	TotalDrawCalls   int    `json:"total_draw_calls"`
	Triangles        uint64 `json:"triangles"`
	Vertices         uint64 `json:"vertices"`
	MaterialSwitches int    `json:"material_switches"`
	PipelineSwitches int    `json:"pipeline_switches"`
	TextureBytes     uint64 `json:"texture_bytes"`
	BufferBytes      uint64 `json:"buffer_bytes"`
}

// PerfMemoryGroup summarizes tracked asset memory for one debug.MemoryTracker group.
type PerfMemoryGroup struct {
	Group      string           `json:"group"`
	TotalBytes int64            `json:"total_bytes"`
	Budget     int64            `json:"budget,omitempty"`
	OverBudget bool             `json:"over_budget,omitempty"`
	Count      int              `json:"count"`
	ByKind     map[string]int64 `json:"by_kind,omitempty"`
}

// PerfMemorySummary summarizes tracked asset memory from a debug.MemoryTracker.
type PerfMemorySummary struct {
	TotalBytes  int64             `json:"total_bytes"`
	TotalBudget int64             `json:"total_budget,omitempty"`
	OverBudget  bool              `json:"over_budget,omitempty"`
	Groups      []PerfMemoryGroup `json:"groups"`
}

// PerfReadoutResult is the JSON result of game_perf_readout.
type PerfReadoutResult struct {
	FPS            float64 `json:"fps"`
	FrameMs        float64 `json:"frame_ms"`
	UpdateMs       float64 `json:"update_ms"`
	DrawMs         float64 `json:"draw_ms"`
	SampleCount    int     `json:"sample_count"`
	LatestFrameMs  float64 `json:"latest_frame_ms"`
	LatestUpdateMs float64 `json:"latest_update_ms"`
	LatestDrawMs   float64 `json:"latest_draw_ms"`
	MinFrameMs     float64 `json:"min_frame_ms"`
	MaxFrameMs     float64 `json:"max_frame_ms"`
	P50FrameMs     float64 `json:"p50_frame_ms"`
	P95FrameMs     float64 `json:"p95_frame_ms"`
	P99FrameMs     float64 `json:"p99_frame_ms"`

	DrawCalls int    `json:"draw_calls"`
	Sprites   int    `json:"sprites"`
	Batches   int    `json:"batches"`
	Triangles uint64 `json:"triangles"`
	Vertices  uint64 `json:"vertices"`

	AllocsPerFrame      float64 `json:"allocs_per_frame"`
	AllocBytesPerFrame  float64 `json:"alloc_bytes_per_frame"`
	LastFrameAllocs     uint64  `json:"last_frame_allocs"`
	LastFrameAllocBytes uint64  `json:"last_frame_alloc_bytes"`
	HeapBytes           uint64  `json:"heap_bytes"`
	GCCount             uint64  `json:"gc_count"`
	GCLastPauseMs       float64 `json:"gc_last_pause_ms"`
	GCMaxPauseMs        float64 `json:"gc_max_pause_ms"`
	GCRecentHitches     int     `json:"gc_recent_hitches"`

	Render3D    *PerfRender3DStats `json:"render3d,omitempty"`
	AssetMemory *PerfMemorySummary `json:"asset_memory,omitempty"`
	History     []PerfFrameSample  `json:"history,omitempty"`
	Zones       []PerfZoneInfo     `json:"zones,omitempty"`
	Sources     PerfSources        `json:"sources"`
}

// PerfZonesResult is the JSON result of game_perf_zones.
type PerfZonesResult struct {
	Recording      bool           `json:"recording"`
	RecordedFrames int            `json:"recorded_frames"`
	Total          int            `json:"total"`
	Zones          []PerfZoneInfo `json:"zones"`
}

// PerfResetResult is the JSON result of game_perf_reset.
type PerfResetResult struct {
	ResetFrameStats bool `json:"reset_frame_stats"`
	ResetProfiler   bool `json:"reset_profiler"`
	ResetGC         bool `json:"reset_gc"`
	ResetDrawStats  bool `json:"reset_draw_stats"`
}

var (
	schemaPerfReadout = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"include_history": {
				"type": "boolean",
				"description": "Include chronological per-frame timing samples from the debug overlay ring buffer (default false)."
			},
			"history_limit": {
				"type": "integer",
				"minimum": 0,
				"description": "Maximum number of most recent frame samples to return when include_history is true (omit or 0 for all retained samples)."
			},
			"include_zones": {
				"type": "boolean",
				"description": "Include per-zone timing summaries from the debug profiler if configured (default false)."
			},
			"zones_limit": {
				"type": "integer",
				"minimum": 0,
				"description": "Maximum number of top timing zones to return when include_zones is true (omit or 0 for all zones)."
			}
		}
	}`)

	schemaPerfZones = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"limit": {
				"type": "integer",
				"minimum": 0,
				"description": "Maximum number of top zones to return, sorted by recent frame cost (omit or 0 for all zones)."
			}
		}
	}`)

	schemaPerfReset = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {}
	}`)
)

func (b *Bridge) perfTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolPerfReadout,
			Description: "Read live performance metrics matching the debug overlay: FPS, frame/update/draw durations and percentiles, draw statistics, allocations per frame, heap/GC counters, asset memory, and optional frame history or profiler zones.",
			InputSchema: schemaPerfReadout,
			Handler:     b.handlePerfReadout,
		},
		{
			Name:        ToolPerfZones,
			Description: "List named profiler timing zones sorted by recent per-frame cost (last, rolling average, peak milliseconds and call count).",
			InputSchema: schemaPerfZones,
			Handler:     b.handlePerfZones,
		},
		{
			Name:        ToolPerfReset,
			Description: "Reset accumulated debug overlay frame timings, pushed draw statistics, profiler zone timings and GC monitor history. An active profiler trace recording keeps running.",
			InputSchema: schemaPerfReset,
			Handler:     b.handlePerfReset,
		},
	}
}

func (b *Bridge) activeProfiler() *debug.Profiler {
	if b == nil {
		return nil
	}
	if b.cfg.Profiler != nil {
		return b.cfg.Profiler
	}
	if b.cfg.Overlay != nil {
		return b.cfg.Overlay.Profiler()
	}
	return nil
}

func (b *Bridge) activeGC() *debug.GCMonitor {
	if b == nil {
		return nil
	}
	if b.cfg.GC != nil {
		return b.cfg.GC
	}
	if b.cfg.Overlay != nil {
		return b.cfg.Overlay.GC()
	}
	return nil
}

func (b *Bridge) activeMemory() *debug.MemoryTracker {
	if b == nil {
		return nil
	}
	if b.cfg.Memory != nil {
		return b.cfg.Memory
	}
	if b.cfg.Overlay != nil {
		return b.cfg.Overlay.Memory()
	}
	return nil
}

func (b *Bridge) hasPerfSource() bool {
	if b == nil {
		return false
	}
	return b.cfg.Overlay != nil ||
		b.cfg.Profiler != nil ||
		b.cfg.GC != nil ||
		b.cfg.Memory != nil ||
		b.cfg.DrawStatsFunc != nil ||
		b.cfg.RenderStatsFunc != nil
}

type perfReadoutArgs struct {
	IncludeHistory bool `json:"include_history"`
	HistoryLimit   int  `json:"history_limit"`
	IncludeZones   bool `json:"include_zones"`
	ZonesLimit     int  `json:"zones_limit"`
}

func (b *Bridge) handlePerfReadout(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[perfReadoutArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.HistoryLimit < 0 {
		return nil, fmt.Errorf("mcp: history_limit must be >= 0 (got %d)", args.HistoryLimit)
	}
	if args.ZonesLimit < 0 {
		return nil, fmt.Errorf("mcp: zones_limit must be >= 0 (got %d)", args.ZonesLimit)
	}
	if !b.hasPerfSource() {
		return nil, errors.New("mcp: no performance telemetry configured")
	}

	var res PerfReadoutResult

	if ov := b.cfg.Overlay; ov != nil {
		res.Sources.Overlay = true
		stats := ov.Stats()
		if stats != nil {
			n := stats.Len()
			res.SampleCount = n
			res.FPS = stats.FPS()
			avg := stats.Average()
			res.FrameMs = durationMs(avg.Total)
			res.UpdateMs = durationMs(avg.Update)
			res.DrawMs = durationMs(avg.Draw)

			latest := stats.Latest()
			res.LatestFrameMs = durationMs(latest.Total)
			res.LatestUpdateMs = durationMs(latest.Update)
			res.LatestDrawMs = durationMs(latest.Draw)

			if n > 0 {
				samples := stats.Samples(make([]debug.FrameSample, 0, n))
				totals := make([]time.Duration, len(samples))
				for i, s := range samples {
					totals[i] = s.Total
				}
				slices.Sort(totals)
				res.MinFrameMs = durationMs(totals[0])
				res.MaxFrameMs = durationMs(totals[len(totals)-1])
				res.P50FrameMs = durationMs(percentileDuration(totals, 50))
				res.P95FrameMs = durationMs(percentileDuration(totals, 95))
				res.P99FrameMs = durationMs(percentileDuration(totals, 99))

				if args.IncludeHistory {
					start := 0
					if args.HistoryLimit > 0 && args.HistoryLimit < len(samples) {
						start = len(samples) - args.HistoryLimit
					}
					hist := make([]PerfFrameSample, 0, len(samples)-start)
					for _, s := range samples[start:] {
						hist = append(hist, PerfFrameSample{
							FrameMs:  durationMs(s.Total),
							UpdateMs: durationMs(s.Update),
							DrawMs:   durationMs(s.Draw),
							GCHitch:  s.GCHitch,
						})
					}
					res.History = hist
				}
			}
		}
	}

	if b.cfg.DrawStatsFunc != nil {
		ds := b.cfg.DrawStatsFunc()
		res.Sources.DrawStats = true
		res.DrawCalls = ds.DrawCalls
		res.Sprites = ds.Sprites
		res.Batches = ds.Batches
		res.Triangles = ds.Triangles
		res.Vertices = ds.Vertices
	} else if b.cfg.Overlay != nil && b.cfg.Overlay.HasDrawStats() {
		ds := b.cfg.Overlay.DrawStats()
		res.Sources.DrawStats = true
		res.DrawCalls = ds.DrawCalls
		res.Sprites = ds.Sprites
		res.Batches = ds.Batches
		res.Triangles = ds.Triangles
		res.Vertices = ds.Vertices
	}

	if b.cfg.RenderStatsFunc != nil {
		rs := b.cfg.RenderStatsFunc()
		res.Sources.Render3D = true
		res.Render3D = render3DStatsFrom(rs)
		if !res.Sources.DrawStats {
			res.DrawCalls = rs.TotalDrawCalls
			res.Batches = rs.InstancedBatches
			res.Triangles = rs.Triangles
			res.Vertices = rs.Vertices
		}
	}

	if gc := b.activeGC(); gc != nil {
		res.Sources.GC = true
		res.AllocsPerFrame = gc.AllocsPerFrame()
		res.AllocBytesPerFrame = gc.AllocBytesPerFrame()
		latestGC := gc.Latest()
		res.LastFrameAllocs = latestGC.AllocsDelta
		res.LastFrameAllocBytes = latestGC.AllocBytesDelta
		res.HeapBytes = gc.HeapObjectsBytes()
		res.GCCount = gc.TotalCycles()
		res.GCLastPauseMs = durationMs(gc.LastPause())
		res.GCMaxPauseMs = durationMs(gc.MaxPause())
		res.GCRecentHitches = gc.RecentHitches()
	}

	if mem := b.activeMemory(); mem != nil {
		res.Sources.Memory = true
		res.AssetMemory = memorySummaryFrom(mem)
	}

	if p := b.activeProfiler(); p != nil {
		res.Sources.Profiler = true
		if args.IncludeZones {
			res.Zones, _ = collectZones(p, args.ZonesLimit)
		}
	}

	return res, nil
}

type perfZonesArgs struct {
	Limit int `json:"limit"`
}

func (b *Bridge) handlePerfZones(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[perfZonesArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.Limit < 0 {
		return nil, fmt.Errorf("mcp: limit must be >= 0 (got %d)", args.Limit)
	}
	p := b.activeProfiler()
	if p == nil {
		return nil, errors.New("mcp: no profiler configured")
	}
	zones, total := collectZones(p, args.Limit)
	return PerfZonesResult{
		Recording:      p.Recording(),
		RecordedFrames: p.RecordedFrames(),
		Total:          total,
		Zones:          zones,
	}, nil
}

func (b *Bridge) handlePerfReset(_ context.Context, raw json.RawMessage) (any, error) {
	if _, err := decodeArgs[struct{}](raw); err != nil {
		return nil, err
	}
	if !b.hasPerfSource() {
		return nil, errors.New("mcp: no performance telemetry configured")
	}

	var res PerfResetResult
	if ov := b.cfg.Overlay; ov != nil {
		hadDraw := ov.HasDrawStats()
		ov.ResetStats()
		res.ResetFrameStats = true
		if hadDraw && !ov.HasDrawStats() {
			res.ResetDrawStats = true
		}
	}
	if p := b.activeProfiler(); p != nil {
		p.Reset()
		res.ResetProfiler = true
	}
	if gc := b.activeGC(); gc != nil {
		gc.Reset()
		res.ResetGC = true
	}
	return res, nil
}

func collectZones(p *debug.Profiler, limit int) ([]PerfZoneInfo, int) {
	all := p.TopScopes(0)
	total := len(all)
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	zones := make([]PerfZoneInfo, 0, len(all))
	for _, sc := range all {
		zones = append(zones, PerfZoneInfo{
			Name:   sc.Name,
			LastMs: durationMs(sc.Total),
			AvgMs:  durationMs(sc.Avg),
			MaxMs:  durationMs(sc.Max),
			Calls:  sc.Calls,
		})
	}
	return zones, total
}

func render3DStatsFrom(rs render3d.RenderStats) *PerfRender3DStats {
	return &PerfRender3DStats{
		TotalItems:       rs.TotalItems,
		CulledItems:      rs.CulledItems,
		Occluded:         rs.Occluded,
		VisibleItems:     rs.VisibleItems,
		OpaqueDraws:      rs.OpaqueDraws,
		TransparentDraws: rs.TransparentDraws,
		InstancedBatches: rs.InstancedBatches,
		TotalDrawCalls:   rs.TotalDrawCalls,
		Triangles:        rs.Triangles,
		Vertices:         rs.Vertices,
		MaterialSwitches: rs.MaterialSwitches,
		PipelineSwitches: rs.PipelineSwitches,
		TextureBytes:     rs.TextureBytes,
		BufferBytes:      rs.BufferBytes,
	}
}

func memorySummaryFrom(mem *debug.MemoryTracker) *PerfMemorySummary {
	total := mem.TotalBytes()
	budget := mem.TotalBudget()
	rawGroups := mem.Groups()
	groups := make([]PerfMemoryGroup, 0, len(rawGroups))
	for _, g := range rawGroups {
		var byKind map[string]int64
		if len(g.ByKind) > 0 {
			byKind = make(map[string]int64, len(g.ByKind))
			for kind, bytes := range g.ByKind {
				byKind[kind.String()] = bytes
			}
		}
		groups = append(groups, PerfMemoryGroup{
			Group:      g.Group,
			TotalBytes: g.TotalBytes,
			Budget:     g.Budget,
			OverBudget: g.OverBudget,
			Count:      g.Count,
			ByKind:     byKind,
		})
	}
	return &PerfMemorySummary{
		TotalBytes:  total,
		TotalBudget: budget,
		OverBudget:  budget > 0 && total > budget,
		Groups:      groups,
	}
}

func durationMs(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func percentileDuration(sorted []time.Duration, pct int) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := (pct*(n-1) + 99) / 100
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}
