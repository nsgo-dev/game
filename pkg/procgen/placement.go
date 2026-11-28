package procgen

import (
	"bytes"
	"fmt"
	"io"
	"slices"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/random"
	"go.yaml.in/yaml/v3"
)

const fixPlacement = "write the placement file as version: 1 YAML with a seed and grid steps"

// PlacementVersion is the placement file schema version this package reads and writes.
const PlacementVersion = 1

// SamplerGrid is the sampler name of a step that places points on a regular lattice of cells.
const SamplerGrid = "grid"

// Placement is a procedural placement file, usually named *.placement.yaml: an ordered list of steps that each sample points over an area and name the prefab to spawn at them, for example:
//
//	version: 1
//	seed: 7
//	steps:
//	  - id: trees
//	    sampler: grid
//	    spacing: 2
//	    count: 8
//	    prefab: tree
//
// Grid is the only sampler so far. Points runs the steps over an area of cells.
type Placement struct {
	// Version is the schema version; it must equal PlacementVersion.
	Version int `yaml:"version"`
	// Seed seeds every step's random stream. Each step forks its own stream from Seed by its ID, so adding, removing or reordering steps leaves the points of the others unchanged.
	Seed uint64 `yaml:"seed"`
	// Steps lists the steps in the order Points runs them.
	Steps []PlacementStep `yaml:"steps"`
}

// PlacementStep is one step of a Placement.
type PlacementStep struct {
	// ID names the step; it must be non-empty and unique. It prefixes the IDs of the step's points and names the step's random stream.
	ID string `yaml:"id"`
	// Sampler selects how the step samples points; it must be SamplerGrid.
	Sampler string `yaml:"sampler"`
	// Spacing is the distance in cells between neighboring lattice points of a grid step, at least 1; 0 means 1.
	Spacing int `yaml:"spacing,omitempty"`
	// Count, when positive, keeps that many lattice points chosen at random from the step's stream; 0 keeps every lattice point, and a Count above the number of lattice points keeps them all.
	Count int `yaml:"count,omitempty"`
	// Prefab names the prefab to spawn at each of the step's points; it must be non-empty.
	Prefab string `yaml:"prefab"`
}

// PlacementPoint is one point a Placement step produced.
type PlacementPoint struct {
	// X and Y are the point's position in cells.
	X, Y float32
	// ID identifies the point: the step's ID, a dash and the point's index within the step, such as "trees-3".
	ID string
	// Step is the ID of the step that produced the point.
	Step string
	// Prefab is the prefab of the step that produced the point.
	Prefab string
}

// ParsePlacement decodes and validates a YAML placement file. Unknown fields and trailing YAML documents are rejected.
func ParsePlacement(data []byte) (*Placement, error) {
	p, err := parsePlacement(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixPlacement)
	}
	return p, nil
}

func parsePlacement(data []byte) (*Placement, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixPlacement, "procgen: empty placement document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var p Placement
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("procgen: decode placement: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document in the placement file; remove the trailing --- document", "procgen: unexpected trailing YAML document in placement")
		}
		return nil, fmt.Errorf("procgen: decode placement: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// EncodePlacement validates p and writes it as 2-space YAML with fields in a fixed order and steps in their given order, so ParsePlacement followed by EncodePlacement reproduces a file EncodePlacement wrote.
func EncodePlacement(p *Placement) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(p); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("procgen: encode placement: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("procgen: encode placement: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks p against the version 1 schema: the version, at least one step, unique non-empty step IDs, a known sampler, a non-negative spacing and count, and a non-empty prefab.
func (p *Placement) Validate() error {
	if p.Version != PlacementVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version to %d", PlacementVersion), "procgen: unsupported placement version %d (want %d)", p.Version, PlacementVersion)
	}
	if len(p.Steps) == 0 {
		return loaderr.Errorf("steps", "add at least one step under steps", "procgen: placement has no steps")
	}
	seen := make(map[string]bool, len(p.Steps))
	for i, s := range p.Steps {
		sPath := loaderr.Path("steps", i)
		if s.ID == "" {
			return loaderr.Errorf(loaderr.Join(sPath, "id"), "give the step a non-empty id", "procgen: placement step %d has no id", i)
		}
		if seen[s.ID] {
			return loaderr.Errorf(loaderr.Join(sPath, "id"), "give each step a unique id", "procgen: duplicate placement step %q", s.ID)
		}
		seen[s.ID] = true
		if s.Sampler != SamplerGrid {
			return loaderr.Errorf(loaderr.Join(sPath, "sampler"), fmt.Sprintf("set sampler to %s", SamplerGrid), "procgen: placement step %q has unknown sampler %q", s.ID, s.Sampler)
		}
		if s.Spacing < 0 {
			return loaderr.Errorf(loaderr.Join(sPath, "spacing"), "set spacing to 1 or more, or leave it out", "procgen: placement step %q spacing %d is negative", s.ID, s.Spacing)
		}
		if s.Count < 0 {
			return loaderr.Errorf(loaderr.Join(sPath, "count"), "set count to 1 or more, or leave it out to keep every point", "procgen: placement step %q count %d is negative", s.ID, s.Count)
		}
		if s.Prefab == "" {
			return loaderr.Errorf(loaderr.Join(sPath, "prefab"), "name the prefab to spawn", "procgen: placement step %q has no prefab", s.ID)
		}
	}
	return nil
}

// Points runs p's steps over an area of width by height cells and returns their points: the steps' points in step order, and each step's points ordered by Y and then X. A grid step places a point at every cell whose coordinates are multiples of its spacing, then keeps Count of them chosen with a random.Stream forked from Seed by the step's ID, so the same file and area give the same points on every machine. An area with no cells gives no points.
func (p *Placement) Points(width, height int) []PlacementPoint {
	var out []PlacementPoint
	if width <= 0 || height <= 0 {
		return out
	}
	root := random.New(p.Seed)
	for _, s := range p.Steps {
		if s.Sampler != SamplerGrid {
			continue
		}
		spacing := max(s.Spacing, 1)
		var cells [][2]int
		for y := 0; y < height; y += spacing {
			for x := 0; x < width; x += spacing {
				cells = append(cells, [2]int{x, y})
			}
		}
		if s.Count > 0 && s.Count < len(cells) {
			random.Shuffle(root.Fork(s.ID), cells)
			cells = cells[:s.Count]
			slices.SortFunc(cells, func(a, b [2]int) int {
				if a[1] != b[1] {
					return a[1] - b[1]
				}
				return a[0] - b[0]
			})
		}
		for i, c := range cells {
			out = append(out, PlacementPoint{X: float32(c[0]), Y: float32(c[1]), ID: fmt.Sprintf("%s-%d", s.ID, i), Step: s.ID, Prefab: s.Prefab})
		}
	}
	return out
}
