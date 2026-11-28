package procgen

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/nsgo-dev/game/pkg/random"
	"go.yaml.in/yaml/v3"
	"sort"
)

type Placement struct {
	Version int             `yaml:"version"`
	Seed    uint64          `yaml:"seed"`
	Steps   []PlacementStep `yaml:"steps"`
}
type PlacementStep struct {
	ID      string   `yaml:"id"`
	Sampler string   `yaml:"sampler"`
	Count   int      `yaml:"count,omitempty"`
	Spacing float32  `yaml:"spacing,omitempty"`
	Prefab  string   `yaml:"prefab,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
}
type Point struct {
	X, Y       float32
	ID, Prefab string
}

func ParsePlacement(data []byte) (Placement, error) {
	var p Placement
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&p); err != nil {
		return p, err
	}
	if p.Version != 1 || len(p.Steps) == 0 {
		return p, errors.New("procgen: placement requires version 1 and steps")
	}
	seen := map[string]bool{}
	for i, s := range p.Steps {
		if s.ID == "" || seen[s.ID] {
			return p, fmt.Errorf("procgen: invalid or duplicate step %q", s.ID)
		}
		seen[s.ID] = true
		switch s.Sampler {
		case "grid", "poisson", "shape", "tiles", "surface", "path":
		default:
			return p, fmt.Errorf("procgen: unknown sampler %q", s.Sampler)
		}
	}
	return p, nil
}
func EncodePlacement(p Placement) ([]byte, error) {
	if p.Version == 0 {
		p.Version = 1
	}
	if _, err := ParsePlacement(mustYAML(p)); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if err := e.Encode(p); err != nil {
		return nil, err
	}
	e.Close()
	return b.Bytes(), nil
}
func mustYAML(p Placement) []byte { b, _ := yaml.Marshal(p); return b }
func (p Placement) Grid(width, height int) []Point {
	out := []Point{}
	r := random.New(p.Seed)
	for _, s := range p.Steps {
		if s.Sampler != "grid" {
			continue
		}
		n := s.Count
		if n <= 0 {
			n = width * height
		}
		for i := 0; i < n; i++ {
			out = append(out, Point{X: float32(r.Intn(width)), Y: float32(r.Intn(height)), ID: fmt.Sprintf("%s-%04d", s.ID, i), Prefab: s.Prefab})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Y == out[j].Y {
			return out[i].X < out[j].X
		}
		return out[i].Y < out[j].Y
	})
	return out
}
