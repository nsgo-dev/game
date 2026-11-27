package navbake3d

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

// Default bake settings, in world units unless noted, as Recast's sample uses them for a human-sized agent.
const (
	// DefaultCellSize is the default width and depth of one heightfield cell.
	DefaultCellSize float32 = 0.3
	// DefaultCellHeight is the default height of one heightfield cell.
	DefaultCellHeight float32 = 0.2
	// DefaultMinRegionArea is the default smallest area of an island of walkable surface that is kept.
	DefaultMinRegionArea float32 = 1
	// DefaultMaxEdgeLength is the default longest a polygon edge along a wall may be before it is split.
	DefaultMaxEdgeLength float32 = 12
	// DefaultMaxSimplificationError is the default farthest a simplified wall outline may stray from the walkable area's cell outline.
	DefaultMaxSimplificationError float32 = 0.4
	// DefaultMaxVertsPerPoly is the default largest number of vertices in one convex polygon.
	DefaultMaxVertsPerPoly = 6
	// DefaultDetailSampleDistance is the default spacing of the height samples the detail mesh is built from.
	DefaultDetailSampleDistance float32 = 1.8
	// DefaultDetailSampleMaxError is the default farthest the detail mesh may stray from the sampled height of the surface.
	DefaultDetailSampleMaxError float32 = 0.2
)

// Defaults of an agent type whose fields are zero.
const (
	// DefaultAgentRadius is the default radius of an agent.
	DefaultAgentRadius float32 = 0.6
	// DefaultAgentHeight is the default height of an agent.
	DefaultAgentHeight float32 = 2
	// DefaultAgentMaxSlope is the default steepest walkable slope in degrees.
	DefaultAgentMaxSlope float32 = 45
	// DefaultAgentMaxClimb is the default highest step an agent climbs.
	DefaultAgentMaxClimb float32 = 0.9
)

// MaxVertsPerPolyLimit is the largest MaxVertsPerPoly a bake accepts.
const MaxVertsPerPolyLimit = 12

// MaxCells is the most heightfield cells (columns across times rows deep) one bake allocates; a larger level needs a larger CellSize.
const MaxCells = 1 << 24

// maxDetailVerts is the most height samples added to the detail mesh of one polygon.
const maxDetailVerts = 64

// maxEdgeSamples is the most segments one polygon edge is divided into for height detail.
const maxEdgeSamples = 32

// AgentType is a named kind of agent a navigation mesh is baked for, such as a humanoid or a large creature. A level bakes one mesh per agent type, as Unity's agent types and Godot's per-agent navigation meshes do.
type AgentType struct {
	// Name uniquely identifies the agent type, such as "humanoid".
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Radius is how far the mesh keeps from walls and ledges; 0 uses DefaultAgentRadius.
	Radius float32 `yaml:"radius" schema:"minimum=0"`
	// Height is the clearance the agent needs; spaces under ceilings lower than it are left out. 0 uses DefaultAgentHeight.
	Height float32 `yaml:"height" schema:"minimum=0"`
	// MaxSlope is the steepest surface the agent walks on, in degrees from horizontal, below 90; 0 uses DefaultAgentMaxSlope.
	MaxSlope float32 `yaml:"maxSlope" schema:"minimum=0,exclusiveMaximum=90"`
	// MaxClimb is the highest step, such as a stair's riser, the agent walks up or down; 0 uses DefaultAgentMaxClimb.
	MaxClimb float32 `yaml:"maxClimb" schema:"minimum=0"`
}

// withDefaults returns a with each zero field replaced by its default.
func (a AgentType) withDefaults() AgentType {
	if a.Radius == 0 {
		a.Radius = DefaultAgentRadius
	}
	if a.Height == 0 {
		a.Height = DefaultAgentHeight
	}
	if a.MaxSlope == 0 {
		a.MaxSlope = DefaultAgentMaxSlope
	}
	if a.MaxClimb == 0 {
		a.MaxClimb = DefaultAgentMaxClimb
	}
	return a
}

// Settings are the voxel resolution and polygon building parameters of a bake, shared by every agent type. A zero field uses its default, and a baked File records every value it was baked with.
type Settings struct {
	// CellSize is the width and depth of one heightfield cell; smaller cells follow the level more closely and take longer to bake. 0 uses DefaultCellSize.
	CellSize float32 `yaml:"cellSize" schema:"minimum=0"`
	// CellHeight is the height of one heightfield cell, the precision of heights, clearances and step heights. 0 uses DefaultCellHeight.
	CellHeight float32 `yaml:"cellHeight" schema:"minimum=0"`
	// MinRegionArea is the smallest area of a connected island of walkable surface that is kept, which drops specks on tables and ledges. 0 uses DefaultMinRegionArea.
	MinRegionArea float32 `yaml:"minRegionArea" schema:"minimum=0"`
	// MaxEdgeLength is the longest a polygon edge along a wall may be before it is split. 0 uses DefaultMaxEdgeLength.
	MaxEdgeLength float32 `yaml:"maxEdgeLength" schema:"minimum=0"`
	// MaxSimplificationError is the farthest a simplified wall outline may stray from the walkable area's cell outline. 0 uses DefaultMaxSimplificationError.
	MaxSimplificationError float32 `yaml:"maxSimplificationError" schema:"minimum=0"`
	// MaxVertsPerPoly is the largest number of vertices in one convex polygon, from 3 to MaxVertsPerPolyLimit. 0 uses DefaultMaxVertsPerPoly.
	MaxVertsPerPoly int `yaml:"maxVertsPerPoly" schema:"minimum=0,maximum=12"`
	// DetailSampleDistance is the spacing of the height samples a polygon's detail triangles follow uneven ground with, at least CellSize. 0 uses DefaultDetailSampleDistance, or CellSize when that is larger.
	DetailSampleDistance float32 `yaml:"detailSampleDistance" schema:"minimum=0"`
	// DetailSampleMaxError is how far the detail triangles may stray from the sampled height before a sample is added. 0 uses DefaultDetailSampleMaxError.
	DetailSampleMaxError float32 `yaml:"detailSampleMaxError" schema:"minimum=0"`
	// LayerMask selects the geometry baked: a triangle is baked when its layers share a bit with the mask. 0 uses LayerAll.
	LayerMask uint32 `yaml:"layerMask"`
}

// withDefaults returns s with each zero field replaced by its default.
func (s Settings) withDefaults() Settings {
	if s.CellSize == 0 {
		s.CellSize = DefaultCellSize
	}
	if s.CellHeight == 0 {
		s.CellHeight = DefaultCellHeight
	}
	if s.MinRegionArea == 0 {
		s.MinRegionArea = DefaultMinRegionArea
	}
	if s.MaxEdgeLength == 0 {
		s.MaxEdgeLength = DefaultMaxEdgeLength
	}
	if s.MaxSimplificationError == 0 {
		s.MaxSimplificationError = DefaultMaxSimplificationError
	}
	if s.MaxVertsPerPoly == 0 {
		s.MaxVertsPerPoly = DefaultMaxVertsPerPoly
	}
	if s.DetailSampleDistance == 0 {
		s.DetailSampleDistance = max(DefaultDetailSampleDistance, s.CellSize)
	}
	if s.DetailSampleMaxError == 0 {
		s.DetailSampleMaxError = DefaultDetailSampleMaxError
	}
	if s.LayerMask == 0 {
		s.LayerMask = LayerAll
	}
	return s
}

// positive reports whether v is finite and greater than zero.
func positive(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) && v > 0
}

// validate checks settings whose defaults are already applied.
func (s Settings) validate(field string) error {
	checks := []struct {
		name string
		v    float32
	}{
		{"cellSize", s.CellSize},
		{"cellHeight", s.CellHeight},
		{"minRegionArea", s.MinRegionArea},
		{"maxEdgeLength", s.MaxEdgeLength},
		{"maxSimplificationError", s.MaxSimplificationError},
		{"detailSampleDistance", s.DetailSampleDistance},
		{"detailSampleMaxError", s.DetailSampleMaxError},
	}
	for _, c := range checks {
		if !positive(c.v) {
			return fieldError(field+"."+c.name, "navbake3d: %s %v must be a finite positive number", c.name, c.v)
		}
	}
	if s.DetailSampleDistance < s.CellSize {
		// Samples closer than a cell find nothing new, and their number grows with the inverse square of the spacing, so a tiny spacing would exhaust memory rather than bake.
		return fieldError(field+".detailSampleDistance", "navbake3d: detailSampleDistance %v must be at least cellSize %v", s.DetailSampleDistance, s.CellSize)
	}
	if s.MaxVertsPerPoly < 3 || s.MaxVertsPerPoly > MaxVertsPerPolyLimit {
		return fieldError(field+".maxVertsPerPoly", "navbake3d: maxVertsPerPoly %d must be from 3 to %d", s.MaxVertsPerPoly, MaxVertsPerPolyLimit)
	}
	if s.LayerMask == 0 {
		return fieldError(field+".layerMask", "navbake3d: layerMask must select at least one layer")
	}
	return nil
}

// validate checks an agent type whose defaults are already applied.
func (a AgentType) validate(field string) error {
	if a.Name == "" || strings.TrimSpace(a.Name) != a.Name || strings.IndexFunc(a.Name, unicode.IsControl) >= 0 {
		return fieldError(field+".name", "navbake3d: agent name %q must be non-empty without surrounding spaces or control characters", a.Name)
	}
	for _, c := range []struct {
		name string
		v    float32
	}{{"radius", a.Radius}, {"height", a.Height}, {"maxSlope", a.MaxSlope}, {"maxClimb", a.MaxClimb}} {
		if !positive(c.v) {
			return fieldError(field+"."+c.name, "navbake3d: agent %q %s %v must be a finite positive number", a.Name, c.name, c.v)
		}
	}
	if a.MaxSlope >= 90 {
		return fieldError(field+".maxSlope", "navbake3d: agent %q maxSlope %v must be below 90 degrees", a.Name, a.MaxSlope)
	}
	return nil
}

// validateAgents applies defaults to agents and checks them, refusing an empty list and duplicate names.
func validateAgents(agents []AgentType) ([]AgentType, error) {
	if len(agents) == 0 {
		return nil, fmt.Errorf("navbake3d: no agent types to bake for")
	}
	out := make([]AgentType, len(agents))
	seen := make(map[string]int, len(agents))
	for i, a := range agents {
		a = a.withDefaults()
		if err := a.validate(fmt.Sprintf("agents[%d]", i)); err != nil {
			return nil, err
		}
		if prev, dup := seen[a.Name]; dup {
			return nil, fmt.Errorf("navbake3d: agents[%d] repeats agent name %q of agents[%d]", i, a.Name, prev)
		}
		seen[a.Name] = i
		out[i] = a
	}
	return out, nil
}
