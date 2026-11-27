package navbake3d

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"math"
	"strconv"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind3d"
	"go.yaml.in/yaml/v3"
)

// FormatVersion is the navigation mesh file schema version this package reads and writes.
const FormatVersion = 1

// SchemaURL is where the JSON Schema of version 1 navigation mesh files is published.
const SchemaURL = "https://game.nsgo.dev/schemas/navmesh.v1.json"

const fixNavMeshDoc = "correct the navigation mesh file to match its version 1 schema in docs/en/navbake3d.md, or bake it again with game navmesh"

// File is a versioned YAML navigation mesh file, such as levels/level1.navmesh.yaml, that a scene names in its navMesh field: the settings it was baked with and one Mesh per agent type.
type File struct {
	// Version is the schema version; it must be FormatVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Settings are the values the meshes were baked with, every field set.
	Settings Settings `yaml:"settings"`
	// Meshes holds one mesh per agent type, in the order the agent types were baked.
	Meshes []Mesh `yaml:"meshes,omitempty"`
}

// Mesh is the navigation mesh baked for one agent type: convex polygons over shared vertices, each with triangles that follow the height of the ground under it, plus any point-to-point off-mesh links.
type Mesh struct {
	// Agent is the agent type the mesh was baked for, every field set.
	Agent AgentType `yaml:"agent"`
	// Vertices are the polygons' corners in world space, shared between neighbouring polygons.
	Vertices []Point `yaml:"vertices,omitempty"`
	// Polygons are the mesh's convex walkable polygons.
	Polygons []Polygon `yaml:"polygons,omitempty"`
	// Links are optional point-to-point off-mesh connections (jumps, drops, ladders, teleports) applied by NavMesh.
	Links []OffMeshLink `yaml:"links,omitempty"`
}

// OffMeshLink is a point-to-point off-mesh connection stored in a baked 3D navigation mesh,
// such as a jump, drop-down, ladder, or teleporter.
type OffMeshLink struct {
	// ID is an optional game-defined link identifier.
	ID int `yaml:"id,omitempty"`
	// Start is the world-space [x, y, z] entry point of the link.
	Start Point `yaml:"start" schema:"required"`
	// End is the world-space [x, y, z] exit point of the link.
	End Point `yaml:"end" schema:"required"`
	// Radius is the optional endpoint snap and entry tolerance in world units (>= 0).
	Radius float32 `yaml:"radius,omitempty" schema:"minimum=0"`
	// Bidirectional allows traversal from End to Start as well as Start to End when true.
	Bidirectional bool `yaml:"bidirectional,omitempty"`
	// Cost overrides the A* traversal cost when positive; 0 uses 3D Euclidean distance.
	Cost float32 `yaml:"cost,omitempty" schema:"minimum=0"`
	// Area is the link's navigation area ID (0 for default).
	Area pathfind3d.NavArea `yaml:"area,omitempty"`
	// Flags holds optional custom link flag bits; pathfind3d.NavLinkOffMesh is always added on load.
	Flags uint32 `yaml:"flags,omitempty"`
	// UserData holds optional game-defined metadata (such as "jump", "ladder", or "teleport").
	UserData string `yaml:"userData,omitempty"`
}

// Polygon is one convex walkable polygon of a Mesh with its height detail.
type Polygon struct {
	// Vertices index Mesh.Vertices, from 3 to Settings.MaxVertsPerPoly of them, in order around the polygon.
	Vertices []int `yaml:"vertices,flow"`
	// Detail holds the extra vertices sampled on the polygon's edges and inside it where the ground is uneven.
	Detail []Point `yaml:"detail,omitempty"`
	// Triangles cover the polygon, winding counter-clockwise seen from above. An index below len(Vertices) names that polygon vertex, and index len(Vertices)+i names Detail[i].
	Triangles []Triangle `yaml:"triangles"`
}

// Point is a position in world space, written as a [x, y, z] list.
type Point [3]float32

// Vec3 returns p as a math3d.Vec3.
func (p Point) Vec3() math3d.Vec3 { return math3d.Vec3{X: p[0], Y: p[1], Z: p[2]} }

// MarshalYAML writes p on one line, as [x, y, z].
func (p Point) MarshalYAML() (any, error) {
	return flowNode([]float32{p[0], p[1], p[2]})
}

// Triangle is three vertex indices of a Polygon's height detail, written as a [a, b, c] list.
type Triangle [3]int

// MarshalYAML writes t on one line, as [a, b, c].
func (t Triangle) MarshalYAML() (any, error) {
	return flowNode([]int{t[0], t[1], t[2]})
}

func flowNode(v any) (*yaml.Node, error) {
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return nil, err
	}
	n.Style = yaml.FlowStyle
	return &n, nil
}

// fieldError returns a *loaderr.Error about field.
func fieldError(field, format string, args ...any) error {
	return &loaderr.Error{Field: field, Problem: fmt.Sprintf(format, args...), Fix: fixNavMeshDoc}
}

// Parse decodes and validates a YAML navigation mesh file. Unknown fields and trailing YAML documents are refused; errors are *loaderr.Error values with the line, column and field of the problem.
func Parse(data []byte) (*File, error) {
	return ParseWith(data, "")
}

// ParseWith is Parse with the file's name attached to errors.
func ParseWith(data []byte, file string) (*File, error) {
	f, err := parse(data)
	if err != nil {
		return nil, loaderr.Locate(err, file, data, fixNavMeshDoc)
	}
	return f, nil
}

func parse(data []byte) (*File, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &loaderr.Error{Problem: "navbake3d: empty navigation mesh file", Fix: fixNavMeshDoc}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("navbake3d: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, &loaderr.Error{Problem: "navbake3d: unexpected trailing YAML document", Fix: "keep one navigation mesh file per file; remove the --- and the document after it"}
		}
		return nil, fmt.Errorf("navbake3d: decode: %w", err)
	}
	if err := Validate(&f); err != nil {
		return nil, err
	}
	yamlzero.Normalize(&f)
	return &f, nil
}

// LoadFS reads and parses the navigation mesh file at path in fsys, such as the one a scene's navMesh field names from the project root.
func LoadFS(fsys fs.FS, path string) (*File, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, loaderr.Locate(err, path, nil, "bake the scene's navigation mesh with game navmesh")
	}
	return ParseWith(data, path)
}

// Encode validates f and writes it as deterministic 2-space YAML that Parse reads back unchanged.
func Encode(f *File) ([]byte, error) {
	if err := Validate(f); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(f)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("navbake3d: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("navbake3d: encode: %w", err)
	}
	return buf.Bytes(), nil
}

func finite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

// Validate checks that f conforms to the version 1 schema: positive settings, distinct valid agent types, finite coordinates, and polygons and triangles whose indices are in range.
func Validate(f *File) error {
	if f == nil {
		return &loaderr.Error{Problem: "navbake3d: nil navigation mesh file", Fix: fixNavMeshDoc}
	}
	if f.Version != FormatVersion {
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("navbake3d: unsupported version %d", f.Version),
			Expected: strconv.Itoa(FormatVersion),
			Fix:      fmt.Sprintf("set version to %d", FormatVersion),
		}
	}
	if err := f.Settings.validate("settings"); err != nil {
		return err
	}
	seen := make(map[string]int, len(f.Meshes))
	for mi := range f.Meshes {
		m := &f.Meshes[mi]
		at := loaderr.Path("meshes", mi)
		if err := m.Agent.validate(at + ".agent"); err != nil {
			return err
		}
		if prev, dup := seen[m.Agent.Name]; dup {
			return fieldError(at+".agent.name", "navbake3d: agent %q already has the mesh at meshes[%d]", m.Agent.Name, prev)
		}
		seen[m.Agent.Name] = mi
		for vi, v := range m.Vertices {
			if !finite32(v[0]) || !finite32(v[1]) || !finite32(v[2]) {
				return fieldError(loaderr.Path("meshes", mi, "vertices", vi), "navbake3d: vertex has a non-finite coordinate")
			}
		}
		for pi := range m.Polygons {
			if err := validatePolygon(&m.Polygons[pi], loaderr.Path("meshes", mi, "polygons", pi), len(m.Vertices), f.Settings.MaxVertsPerPoly); err != nil {
				return err
			}
		}
		for li := range m.Links {
			if err := validateOffMeshLink(&m.Links[li], loaderr.Path("meshes", mi, "links", li)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateOffMeshLink(l *OffMeshLink, at string) error {
	if !finite32(l.Start[0]) || !finite32(l.Start[1]) || !finite32(l.Start[2]) {
		return fieldError(at+".start", "navbake3d: off-mesh link start has a non-finite coordinate")
	}
	if !finite32(l.End[0]) || !finite32(l.End[1]) || !finite32(l.End[2]) {
		return fieldError(at+".end", "navbake3d: off-mesh link end has a non-finite coordinate")
	}
	if !finite32(l.Radius) || l.Radius < 0 {
		return fieldError(at+".radius", "navbake3d: off-mesh link radius %v must be finite and >= 0", l.Radius)
	}
	if !finite32(l.Cost) || l.Cost < 0 {
		return fieldError(at+".cost", "navbake3d: off-mesh link cost %v must be finite and >= 0", l.Cost)
	}
	return nil
}

func validatePolygon(p *Polygon, at string, nverts, maxVerts int) error {
	n := len(p.Vertices)
	if n < 3 || n > maxVerts {
		return fieldError(at+".vertices", "navbake3d: a polygon has %d vertices, want 3 to maxVertsPerPoly (%d)", n, maxVerts)
	}
	for i, v := range p.Vertices {
		if v < 0 || v >= nverts {
			return fieldError(fmt.Sprintf("%s.vertices[%d]", at, i), "navbake3d: vertex index %d is out of range [0, %d)", v, nverts)
		}
		for j := range i {
			if p.Vertices[j] == v {
				return fieldError(fmt.Sprintf("%s.vertices[%d]", at, i), "navbake3d: vertex index %d repeats", v)
			}
		}
	}
	for i, d := range p.Detail {
		if !finite32(d[0]) || !finite32(d[1]) || !finite32(d[2]) {
			return fieldError(fmt.Sprintf("%s.detail[%d]", at, i), "navbake3d: detail vertex has a non-finite coordinate")
		}
	}
	if len(p.Triangles) == 0 {
		return fieldError(at+".triangles", "navbake3d: a polygon needs at least one triangle")
	}
	total := n + len(p.Detail)
	for i, t := range p.Triangles {
		for _, k := range t {
			if k < 0 || k >= total {
				return fieldError(fmt.Sprintf("%s.triangles[%d]", at, i), "navbake3d: triangle index %d is out of range [0, %d)", k, total)
			}
		}
		if t[0] == t[1] || t[1] == t[2] || t[0] == t[2] {
			return fieldError(fmt.Sprintf("%s.triangles[%d]", at, i), "navbake3d: a triangle repeats a vertex")
		}
	}
	return nil
}

// Mesh returns the mesh baked for the agent type called agent.
func (f *File) Mesh(agent string) (*Mesh, bool) {
	if f == nil {
		return nil, false
	}
	for i := range f.Meshes {
		if f.Meshes[i].Agent.Name == agent {
			return &f.Meshes[i], true
		}
	}
	return nil, false
}

// TriangleVertices returns the world-space corners of triangle t of polygon p.
func (m *Mesh) TriangleVertices(p *Polygon, t Triangle) [3]math3d.Vec3 {
	var out [3]math3d.Vec3
	for k, i := range t {
		if i < len(p.Vertices) {
			out[k] = m.Vertices[p.Vertices[i]].Vec3()
		} else {
			out[k] = p.Detail[i-len(p.Vertices)].Vec3()
		}
	}
	return out
}

// NavMesh builds the pathfind3d.NavMesh agents path find on from m: each detail triangle becomes one of its walkable polygons, linked to its neighbours across shared edges, so paths follow ramps, stairs and terrain at the baked heights and keep stacked floors apart. Any OffMeshLinks stored in m.Links are registered via AddOffMeshLink. The bake already decided what is walkable, so no triangle is dropped for its slope. m must be valid, as Parse and Bake return it.
func (m *Mesh) NavMesh() *pathfind3d.NavMesh {
	var polys [][]math3d.Vec3
	for pi := range m.Polygons {
		p := &m.Polygons[pi]
		for _, t := range p.Triangles {
			v := m.TriangleVertices(p, t)
			polys = append(polys, v[:])
		}
	}
	nm := pathfind3d.BuildNavMesh(polys, math.Pi/2)
	nm.VerticalTolerance = max(m.Agent.Height, m.Agent.MaxClimb)
	for _, l := range m.Links {
		var ud any
		if l.UserData != "" {
			ud = l.UserData
		}
		nm.AddOffMeshLink(pathfind3d.NavOffMeshLink{
			Start:         l.Start.Vec3(),
			End:           l.End.Vec3(),
			Radius:        l.Radius,
			Bidirectional: l.Bidirectional,
			Flags:         l.Flags,
			ID:            l.ID,
			Cost:          l.Cost,
			Area:          l.Area,
			UserData:      ud,
		})
	}
	return nm
}

// TiledNavMesh builds a pathfind3d.TiledNavMesh partitioned into fixed square XZ tiles of size tileSize
// from m's detail triangles, ready for runtime tile replacement, dynamic obstacle carving, and chunk streaming.
func (m *Mesh) TiledNavMesh(tileSize float32) *pathfind3d.TiledNavMesh {
	if m == nil {
		return pathfind3d.BuildTiledNavMesh(nil, math.Pi/2, tileSize)
	}
	var polys [][]math3d.Vec3
	for pi := range m.Polygons {
		p := &m.Polygons[pi]
		for _, t := range p.Triangles {
			v := m.TriangleVertices(p, t)
			polys = append(polys, v[:])
		}
	}
	tm := pathfind3d.BuildTiledNavMesh(polys, math.Pi/2, tileSize)
	if vtol := max(m.Agent.Height, m.Agent.MaxClimb); vtol > 0 {
		tm.VerticalTolerance = vtol
	}
	return tm
}
