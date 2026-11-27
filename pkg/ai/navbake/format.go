package navbake

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// FormatVersion is the baked mesh file schema version this package reads and writes.
const FormatVersion = 1

// Mesh is a baked navigation mesh: convex polygons for one agent type, kept in YAML so it can be committed beside the level it was baked from and reviewed as a diff.
//
//	version: 1
//	agent: small
//	radius: 4
//	polygons:
//	  - [[4, 4], [60, 4], [60, 28], [4, 28]]
//	  - [[60, 4], [124, 4], [124, 28], [60, 28]]
//
// Each polygon is a list of [x, y] vertices in world units, counter-clockwise in a y-up frame (clockwise on a y-down screen), starting from its lowest vertex. A bake writes its polygons in a fixed order, so baking the same input gives the same bytes.
type Mesh struct {
	// Version is the schema version; it must be FormatVersion.
	Version int `yaml:"version"`
	// Agent names the agent type the mesh was baked for; empty for an unnamed agent.
	Agent string `yaml:"agent,omitempty"`
	// Radius is the agent radius the mesh was baked for, in world units.
	Radius float32 `yaml:"radius"`
	// Polygons are the mesh's convex polygons.
	Polygons []Polygon `yaml:"polygons"`
	// Links are optional point-to-point off-mesh connections (jumps, drops, ladders, teleports) applied by NavMesh.
	Links []OffMeshLink `yaml:"links,omitempty"`
}

// Point is a 2D position in world space, written as a flow [x, y] list.
type Point [2]float32

// CorePoint returns p as a core.Point.
func (p Point) CorePoint() core.Point {
	return core.Point{X: core.Points(p[0]), Y: core.Points(p[1])}
}

// MarshalYAML writes p as a flow [x, y] sequence, each coordinate in the shortest form that reads back as the same float32.
func (p Point) MarshalYAML() (any, error) {
	return &yaml.Node{
		Kind:  yaml.SequenceNode,
		Style: yaml.FlowStyle,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: formatCoord(core.Points(p[0]))},
			{Kind: yaml.ScalarNode, Value: formatCoord(core.Points(p[1]))},
		},
	}, nil
}

// OffMeshLink is a point-to-point off-mesh connection stored in a baked 2D navigation mesh,
// such as a jump, drop-down, ladder, or teleporter.
type OffMeshLink struct {
	// ID is an optional game-defined link identifier.
	ID int `yaml:"id,omitempty"`
	// Start is the world-space [x, y] entry point of the link.
	Start Point `yaml:"start"`
	// End is the world-space [x, y] exit point of the link.
	End Point `yaml:"end"`
	// Radius is the optional endpoint snap and entry tolerance in world units (>= 0).
	Radius float32 `yaml:"radius,omitempty"`
	// Bidirectional allows traversal from End to Start as well as Start to End when true.
	Bidirectional bool `yaml:"bidirectional,omitempty"`
	// Cost overrides the A* traversal cost when positive; 0 uses 2D Euclidean distance.
	Cost float32 `yaml:"cost,omitempty"`
	// Area is the link's navigation area ID (0 for default).
	Area ai.NavArea `yaml:"area,omitempty"`
	// Flags holds optional custom link flag bits; ai.NavLinkOffMesh is always added on load.
	Flags uint32 `yaml:"flags,omitempty"`
	// UserData holds optional game-defined metadata (such as "jump", "ladder", or "teleport").
	UserData string `yaml:"userData,omitempty"`
}

// Polygon is one convex polygon of a Mesh. In YAML it is a flow sequence of [x, y] pairs on one line.
type Polygon []core.Point

// newMesh converts baked polygons from grid steps to world units and copies any off-mesh links.
func newMesh(a Agent, polys [][]ipt, step float64, links []OffMeshLink) *Mesh {
	m := &Mesh{Version: FormatVersion, Agent: a.Name, Radius: a.Radius, Polygons: make([]Polygon, len(polys))}
	for i, poly := range polys {
		out := make(Polygon, len(poly))
		for k, p := range poly {
			out[k] = core.Point{X: core.Points(float64(p.x) * step), Y: core.Points(float64(p.y) * step)}
		}
		m.Polygons[i] = out
	}
	if len(links) > 0 {
		m.Links = make([]OffMeshLink, len(links))
		for i, l := range links {
			l.Start = Point{noNegZero32(l.Start[0]), noNegZero32(l.Start[1])}
			l.End = Point{noNegZero32(l.End[0]), noNegZero32(l.End[1])}
			l.Radius = noNegZero32(l.Radius)
			l.Cost = noNegZero32(l.Cost)
			m.Links[i] = l
		}
	}
	return m
}

func noNegZero32(f float32) float32 {
	if f == 0 {
		return 0
	}
	return f
}

// NavMesh builds the ai.NavMesh of m, ready for FindPath; polygons that touch along an edge are connected there,
// and any OffMeshLinks stored in m.Links are registered via AddOffMeshLink.
func (m *Mesh) NavMesh() *ai.NavMesh {
	if m == nil {
		return ai.BuildNavMesh(nil)
	}
	polys := make([][]core.Point, len(m.Polygons))
	for i, p := range m.Polygons {
		polys[i] = p
	}
	nav := ai.BuildNavMesh(polys)
	for _, l := range m.Links {
		var ud any
		if l.UserData != "" {
			ud = l.UserData
		}
		nav.AddOffMeshLink(ai.NavOffMeshLink{
			Start:         l.Start.CorePoint(),
			End:           l.End.CorePoint(),
			Radius:        l.Radius,
			Bidirectional: l.Bidirectional,
			Flags:         l.Flags,
			ID:            l.ID,
			Cost:          l.Cost,
			Area:          l.Area,
			UserData:      ud,
		})
	}
	return nav
}

// TiledNavMesh builds an ai.TiledNavMesh partitioned into fixed square tiles of size tileSize
// from m's baked polygons, ready for runtime tile replacement and dynamic obstacle carving.
func (m *Mesh) TiledNavMesh(tileSize float32) *ai.TiledNavMesh {
	if m == nil {
		return ai.BuildTiledNavMesh(nil, tileSize)
	}
	polys := make([][]core.Point, len(m.Polygons))
	for i, p := range m.Polygons {
		polys[i] = p
	}
	return ai.BuildTiledNavMesh(polys, tileSize)
}

// Parse decodes and validates a baked mesh file. Unknown fields, a second YAML document and any value Validate refuses are errors.
func Parse(data []byte) (*Mesh, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("navbake: empty mesh file")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var m Mesh
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("navbake: decode mesh: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("navbake: unexpected trailing YAML document in mesh file")
		}
		return nil, fmt.Errorf("navbake: decode mesh: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	yamlzero.Normalize(&m)
	return &m, nil
}

// Encode validates m and writes it as YAML with two-space indentation, fields in a fixed order and one polygon per line, so encoding a parsed file gives back the same bytes for files written in that form.
func (m *Mesh) Encode() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(m)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("navbake: encode mesh: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("navbake: encode mesh: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate reports the first problem with m: a version other than FormatVersion, a radius that is negative or not finite, or a polygon with fewer than three vertices, a coordinate that is not finite, or a shape that is not convex, wound counter-clockwise in a y-up frame with a positive area.
func (m *Mesh) Validate() error {
	if m == nil {
		return errors.New("navbake: nil mesh")
	}
	if m.Version != FormatVersion {
		return fmt.Errorf("navbake: unsupported mesh file version %d (want %d)", m.Version, FormatVersion)
	}
	if !(m.Radius >= 0) || math.IsInf(float64(m.Radius), 0) {
		return fmt.Errorf("navbake: radius %v is not a finite number of 0 or more", m.Radius)
	}
	for i, p := range m.Polygons {
		if err := p.validate(); err != nil {
			return fmt.Errorf("navbake: polygon %d: %w", i, err)
		}
	}
	for i, l := range m.Links {
		if err := l.validate(); err != nil {
			return fmt.Errorf("navbake: link %d: %w", i, err)
		}
	}
	return nil
}

func (l OffMeshLink) validate() error {
	if !finite(core.Points(l.Start[0])) || !finite(core.Points(l.Start[1])) {
		return errors.New("start is not finite")
	}
	if !finite(core.Points(l.End[0])) || !finite(core.Points(l.End[1])) {
		return errors.New("end is not finite")
	}
	if !(l.Radius >= 0) || math.IsInf(float64(l.Radius), 0) {
		return fmt.Errorf("radius %v is not a finite number of 0 or more", l.Radius)
	}
	if !(l.Cost >= 0) || math.IsInf(float64(l.Cost), 0) {
		return fmt.Errorf("cost %v is not a finite number of 0 or more", l.Cost)
	}
	return nil
}

// validate checks that p is a convex polygon wound counter-clockwise in a y-up frame: every turn goes left or straight, the area is positive, and every vertex lies on or left of every edge, which also refuses star polygons whose turns all go the same way.
func (p Polygon) validate() error {
	n := len(p)
	if n < 3 {
		return fmt.Errorf("has %d vertices, want at least 3", n)
	}
	var area float64
	turn := 0
	for i, a := range p {
		if !finite(a.X) || !finite(a.Y) {
			return fmt.Errorf("vertex %d is not finite", i)
		}
		b, c := p[(i+1)%n], p[(i+2)%n]
		area += float64(a.X)*float64(b.Y) - float64(b.X)*float64(a.Y)
		switch cr := cross(a, b, c); {
		case cr > 0:
			turn |= 1
		case cr < 0:
			turn |= 2
		}
	}
	if turn == 3 {
		return errors.New("is not convex")
	}
	if area == 0 {
		return errors.New("has no area")
	}
	if area < 0 {
		return errors.New("is wound clockwise, want counter-clockwise in a y-up frame")
	}
	for i, a := range p {
		next := (i + 1) % n
		b := p[next]
		for k, c := range p {
			if k == i || k == next {
				continue
			}
			if cross(a, b, c) < 0 {
				return fmt.Errorf("is not convex: vertex %d lies right of edge %d", k, i)
			}
		}
	}
	return nil
}

// cross returns twice the signed area of triangle (a, b, c), positive when a->b->c turns counter-clockwise in a y-up frame.
// Explicit float64 conversions disable FMA contraction so arm64 and amd64 agree on collinear vertices.
func cross(a, b, c core.Point) float64 {
	p := float64((float64(b.X) - float64(a.X)) * (float64(c.Y) - float64(a.Y)))
	q := float64((float64(b.Y) - float64(a.Y)) * (float64(c.X) - float64(a.X)))
	return p - q
}

func finite(v core.Points) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// MarshalYAML writes p as a flow sequence of [x, y] pairs, each coordinate in the shortest form that reads back as the same float32.
func (p Polygon) MarshalYAML() (any, error) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, v := range p {
		seq.Content = append(seq.Content, &yaml.Node{
			Kind:  yaml.SequenceNode,
			Style: yaml.FlowStyle,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: formatCoord(v.X)},
				{Kind: yaml.ScalarNode, Value: formatCoord(v.Y)},
			},
		})
	}
	return seq, nil
}

// UnmarshalYAML reads p from a sequence of [x, y] pairs.
func (p *Polygon) UnmarshalYAML(node *yaml.Node) error {
	var raw [][]float32
	if err := node.Decode(&raw); err != nil {
		return err
	}
	out := make(Polygon, len(raw))
	for i, v := range raw {
		if len(v) != 2 {
			return fmt.Errorf("line %d: vertex %d has %d coordinates, want 2", node.Line, i, len(v))
		}
		out[i] = core.Point{X: core.Points(v[0]), Y: core.Points(v[1])}
	}
	*p = out
	return nil
}

func formatCoord(v core.Points) string {
	return strconv.FormatFloat(float64(v), 'g', -1, 32)
}
