package physics3d

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

// hullJSON is the JSON form of a ConvexHull inside a WorldSnapshot. It carries the hull's whole derived geometry, not only its vertices, so a hull read back from a snapshot is bit-identical to the one saved and a restored world re-steps exactly as the original did.
type hullJSON struct {
	Center Vec3          `json:"center"`
	Geom   *hullGeomJSON `json:"geom,omitempty"`
}

type hullGeomJSON struct {
	Vertices  [][3]float64   `json:"vertices"`
	Faces     []hullFaceJSON `json:"faces"`
	Edges     [][2]int       `json:"edges"`
	Volume    float64        `json:"volume"`
	Centroid  [3]float64     `json:"centroid"`
	Inertia   [3][3]float64  `json:"inertia"`
	Radius    float64        `json:"radius"`
	HalfWidth float64        `json:"halfWidth"`
}

type hullFaceJSON struct {
	Indices []int      `json:"indices"`
	Normal  [3]float64 `json:"normal"`
	Offset  float64    `json:"offset"`
}

func (a dv) arr() [3]float64    { return [3]float64{a.x, a.y, a.z} }
func dvFromArr(a [3]float64) dv { return dv{a[0], a[1], a[2]} }

// MarshalJSON encodes h with its full geometry, so that WorldSnapshot round-trips bodies with convex hull shapes losslessly.
func (h ConvexHull) MarshalJSON() ([]byte, error) {
	out := hullJSON{Center: h.Center}
	if g := h.geom; g != nil {
		gj := &hullGeomJSON{
			Vertices:  make([][3]float64, len(g.v)),
			Faces:     make([]hullFaceJSON, len(g.faces)),
			Edges:     g.edges,
			Volume:    g.volume,
			Centroid:  g.centroid.arr(),
			Inertia:   g.inertia,
			Radius:    g.radius,
			HalfWidth: g.halfWidth,
		}
		if gj.Edges == nil {
			gj.Edges = [][2]int{}
		}
		for i, v := range g.v {
			gj.Vertices[i] = v.arr()
		}
		for i, f := range g.faces {
			gj.Faces[i] = hullFaceJSON{Indices: f.idx, Normal: f.n.arr(), Offset: f.d}
		}
		out.Geom = gj
	}
	return json.Marshal(out)
}

// UnmarshalJSON decodes a hull written by MarshalJSON. It refuses unknown fields and geometry whose indices are out of range or whose numbers are not finite.
func (h *ConvexHull) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var in hullJSON
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("physics3d: convex hull: %w", err)
	}
	*h = ConvexHull{Center: in.Center}
	if in.Geom == nil {
		return nil
	}
	gj := in.Geom
	if len(gj.Vertices) < 4 || len(gj.Faces) < 4 {
		return fmt.Errorf("physics3d: convex hull has %d vertices and %d faces, want at least 4 of each", len(gj.Vertices), len(gj.Faces))
	}
	finite := func(fs ...float64) bool {
		for _, f := range fs {
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return false
			}
		}
		return true
	}
	n := len(gj.Vertices)
	g := &hullGeom{
		v:         make([]dv, n),
		faces:     make([]hullFace, len(gj.Faces)),
		edges:     gj.Edges,
		volume:    gj.Volume,
		centroid:  dvFromArr(gj.Centroid),
		inertia:   gj.Inertia,
		radius:    gj.Radius,
		halfWidth: gj.HalfWidth,
	}
	for i, v := range gj.Vertices {
		if !finite(v[:]...) {
			return fmt.Errorf("physics3d: convex hull vertex %d is not finite", i)
		}
		g.v[i] = dvFromArr(v)
	}
	for i, f := range gj.Faces {
		if len(f.Indices) < 3 {
			return fmt.Errorf("physics3d: convex hull face %d has %d vertices, want at least 3", i, len(f.Indices))
		}
		for _, ix := range f.Indices {
			if ix < 0 || ix >= n {
				return fmt.Errorf("physics3d: convex hull face %d index %d is out of range for %d vertices", i, ix, n)
			}
		}
		if !finite(f.Normal[0], f.Normal[1], f.Normal[2], f.Offset) {
			return fmt.Errorf("physics3d: convex hull face %d plane is not finite", i)
		}
		g.faces[i] = hullFace{idx: f.Indices, n: dvFromArr(f.Normal), d: f.Offset}
	}
	for i, e := range gj.Edges {
		if e[0] < 0 || e[0] >= n || e[1] < 0 || e[1] >= n {
			return fmt.Errorf("physics3d: convex hull edge %d is out of range for %d vertices", i, n)
		}
	}
	in3 := gj.Inertia
	if !finite(gj.Centroid[0], gj.Centroid[1], gj.Centroid[2], gj.Radius, gj.HalfWidth, in3[0][0], in3[0][1], in3[0][2], in3[1][0], in3[1][1], in3[1][2], in3[2][0], in3[2][1], in3[2][2]) || !(gj.Volume > 0) || math.IsInf(gj.Volume, 0) {
		return fmt.Errorf("physics3d: convex hull mass properties are not finite and positive")
	}
	h.geom = g
	return nil
}
