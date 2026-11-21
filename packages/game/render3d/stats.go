package render3d

import "fmt"

// RenderStats captures per-frame culling, batching, state-switch, geometry,
// and GPU memory telemetry for a [RenderPlan] (#288).
type RenderStats struct {
	TotalItems       int
	CulledItems      int
	VisibleItems     int
	OpaqueDraws      int
	TransparentDraws int
	InstancedBatches int
	TotalDrawCalls   int
	Triangles        uint64
	Vertices         uint64
	MaterialSwitches int
	PipelineSwitches int
	TextureBytes     uint64
	BufferBytes      uint64
}

// Add combines s and other, useful when aggregating multiple render passes
// (for example shadow pass + main pass).
func (s RenderStats) Add(other RenderStats) RenderStats {
	return RenderStats{
		TotalItems:       s.TotalItems + other.TotalItems,
		CulledItems:      s.CulledItems + other.CulledItems,
		VisibleItems:     s.VisibleItems + other.VisibleItems,
		OpaqueDraws:      s.OpaqueDraws + other.OpaqueDraws,
		TransparentDraws: s.TransparentDraws + other.TransparentDraws,
		InstancedBatches: s.InstancedBatches + other.InstancedBatches,
		TotalDrawCalls:   s.TotalDrawCalls + other.TotalDrawCalls,
		Triangles:        s.Triangles + other.Triangles,
		Vertices:         s.Vertices + other.Vertices,
		MaterialSwitches: s.MaterialSwitches + other.MaterialSwitches,
		PipelineSwitches: s.PipelineSwitches + other.PipelineSwitches,
		TextureBytes:     s.TextureBytes + other.TextureBytes,
		BufferBytes:      s.BufferBytes + other.BufferBytes,
	}
}

// Summary returns a concise human-readable diagnostic line for overlays or logs.
func (s RenderStats) Summary() string {
	return fmt.Sprintf(
		"draws=%d (batches=%d transparent=%d) visible=%d/%d tris=%d verts=%d switches=pipe:%d/mat:%d mem=tex:%dB/buf:%dB",
		s.TotalDrawCalls,
		s.InstancedBatches,
		s.TransparentDraws,
		s.VisibleItems,
		s.TotalItems,
		s.Triangles,
		s.Vertices,
		s.PipelineSwitches,
		s.MaterialSwitches,
		s.TextureBytes,
		s.BufferBytes,
	)
}

// Budget defines upper limits on per-frame draw submission and GPU memory
// consumption (#289). Any field set to 0 disables checking for that metric.
type Budget struct {
	MaxDrawCalls        int
	MaxTriangles        uint64
	MaxVertices         uint64
	MaxMaterialSwitches int
	MaxPipelineSwitches int
	MaxTextureBytes     uint64
	MaxBufferBytes      uint64
}

const (
	mib = 1024 * 1024
	gib = 1024 * mib
)

// MobileBudget returns a conservative 60 fps mobile GPU budget preset
// (<= 150 draw calls, <= 150k triangles, <= 300k vertices, <= 60 material
// switches, <= 32 pipeline switches, <= 128 MiB textures, <= 64 MiB buffers).
func MobileBudget() Budget {
	return Budget{
		MaxDrawCalls:        150,
		MaxTriangles:        150_000,
		MaxVertices:         300_000,
		MaxMaterialSwitches: 60,
		MaxPipelineSwitches: 32,
		MaxTextureBytes:     128 * mib,
		MaxBufferBytes:      64 * mib,
	}
}

// DesktopBudget returns a desktop GPU budget preset suitable for mid-range
// discrete and integrated GPUs.
func DesktopBudget() Budget {
	return Budget{
		MaxDrawCalls:        2_000,
		MaxTriangles:        2_000_000,
		MaxVertices:         4_000_000,
		MaxMaterialSwitches: 500,
		MaxPipelineSwitches: 200,
		MaxTextureBytes:     1 * gib,
		MaxBufferBytes:      512 * mib,
	}
}

// BudgetViolation reports a single [Budget] threshold exceeded by [RenderStats].
type BudgetViolation struct {
	Metric string
	Actual uint64
	Limit  uint64
}

// String formats v as a human-readable budget overrun message.
func (v BudgetViolation) String() string {
	return fmt.Sprintf("%s: %d exceeds budget %d", v.Metric, v.Actual, v.Limit)
}

// CheckBudget compares stats against every non-zero limit in b and returns all
// violations in deterministic order. It returns nil when stats stays within b.
func CheckBudget(stats RenderStats, b Budget) []BudgetViolation {
	var violations []BudgetViolation
	if b.MaxDrawCalls > 0 && stats.TotalDrawCalls > b.MaxDrawCalls {
		violations = append(violations, BudgetViolation{
			Metric: "draw_calls",
			Actual: uint64(stats.TotalDrawCalls),
			Limit:  uint64(b.MaxDrawCalls),
		})
	}
	if b.MaxTriangles > 0 && stats.Triangles > b.MaxTriangles {
		violations = append(violations, BudgetViolation{
			Metric: "triangles",
			Actual: stats.Triangles,
			Limit:  b.MaxTriangles,
		})
	}
	if b.MaxVertices > 0 && stats.Vertices > b.MaxVertices {
		violations = append(violations, BudgetViolation{
			Metric: "vertices",
			Actual: stats.Vertices,
			Limit:  b.MaxVertices,
		})
	}
	if b.MaxMaterialSwitches > 0 && stats.MaterialSwitches > b.MaxMaterialSwitches {
		violations = append(violations, BudgetViolation{
			Metric: "material_switches",
			Actual: uint64(stats.MaterialSwitches),
			Limit:  uint64(b.MaxMaterialSwitches),
		})
	}
	if b.MaxPipelineSwitches > 0 && stats.PipelineSwitches > b.MaxPipelineSwitches {
		violations = append(violations, BudgetViolation{
			Metric: "pipeline_switches",
			Actual: uint64(stats.PipelineSwitches),
			Limit:  uint64(b.MaxPipelineSwitches),
		})
	}
	if b.MaxTextureBytes > 0 && stats.TextureBytes > b.MaxTextureBytes {
		violations = append(violations, BudgetViolation{
			Metric: "texture_bytes",
			Actual: stats.TextureBytes,
			Limit:  b.MaxTextureBytes,
		})
	}
	if b.MaxBufferBytes > 0 && stats.BufferBytes > b.MaxBufferBytes {
		violations = append(violations, BudgetViolation{
			Metric: "buffer_bytes",
			Actual: stats.BufferBytes,
			Limit:  b.MaxBufferBytes,
		})
	}
	return violations
}
