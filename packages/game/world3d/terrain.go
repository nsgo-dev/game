package world3d

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// Heightmap represents a regular grid terrain surface in the XZ plane with +Y
// elevation. Sample (col, row) lives at world position:
//
//	(Origin.X + col*CellSize, Origin.Y + Heights[row*Cols+col]*HeightScale, Origin.Z + row*CellSize)
//
// Every quad cell (col, row) is triangulated consistently across HeightAt,
// NormalAt, Raycast, and BuildMesh along the diagonal from (col+1, row) to
// (col, row+1):
//   - Triangle 0 (fx+fz <= 1): (v00, v01, v10)
//   - Triangle 1 (fx+fz >  1): (v10, v01, v11)
type Heightmap struct {
	Cols        int
	Rows        int
	CellSize    float32
	HeightScale float32
	Origin      math3d.Vec3
	Heights     []float32
	// Splat optionally attaches splat-map material layers for gameplay queries via [Heightmap.LayerAt].
	Splat *TerrainSplatMap
	// FoliageDensity optionally attaches per-layer foliage scatter density maps keyed by foliage layer name, queried via [Heightmap.FoliageDensityAt].
	FoliageDensity map[string]*DensityMap
}

// TerrainHit describes the closest intersection between a ray and a Heightmap.
type TerrainHit struct {
	Point    math3d.Vec3
	Normal   math3d.Vec3
	Distance float32
	Col, Row int
}

// TerrainMesh holds indexed triangle-list geometry for a heightmap chunk ready
// for upload to a GPU vertex and index buffer. Triangles use counter-clockwise
// winding when viewed from +Y.
type TerrainMesh struct {
	Positions []math3d.Vec3
	Normals   []math3d.Vec3
	UVs       []math3d.Vec2
	Indices   []uint32
}

// NewHeightmap allocates a flat (zero-elevation) Heightmap of cols x rows
// samples with the given cellSize and heightScale.
func NewHeightmap(cols, rows int, cellSize, heightScale float32) *Heightmap {
	if cols <= 0 || rows <= 0 {
		return &Heightmap{CellSize: cellSize, HeightScale: heightScale}
	}
	return &Heightmap{
		Cols:        cols,
		Rows:        rows,
		CellSize:    cellSize,
		HeightScale: heightScale,
		Heights:     make([]float32, cols*rows),
	}
}

// LoadHeightmapGray constructs a Heightmap from a grayscale (or color, converted
// to 16-bit luminance) image. Pixel intensities are normalized to [0, 1] with
// full 16-bit precision for [*image.Gray16] inputs and scaled by heightScale
// during height evaluation.
func LoadHeightmapGray(img image.Image, cellSize, heightScale float32) *Heightmap {
	if img == nil {
		return &Heightmap{CellSize: cellSize, HeightScale: heightScale}
	}
	b := img.Bounds()
	cols := b.Dx()
	rows := b.Dy()
	heights := make([]float32, cols*rows)
	const inv65535 = float32(1.0 / 65535.0)
	if g16, ok := img.(*image.Gray16); ok {
		for r := range rows {
			off := g16.PixOffset(b.Min.X, b.Min.Y+r)
			rowBase := r * cols
			for c := range cols {
				p := off + c*2
				y := uint16(g16.Pix[p])<<8 | uint16(g16.Pix[p+1])
				heights[rowBase+c] = float32(y) * inv65535
			}
		}
	} else {
		for r := range rows {
			rowBase := r * cols
			for c := range cols {
				g := color.Gray16Model.Convert(img.At(b.Min.X+c, b.Min.Y+r)).(color.Gray16)
				heights[rowBase+c] = float32(g.Y) * inv65535
			}
		}
	}
	return &Heightmap{
		Cols:        cols,
		Rows:        rows,
		CellSize:    cellSize,
		HeightScale: heightScale,
		Heights:     heights,
	}
}

// ToGray16 converts h's normalized [0, 1] heights into a 16-bit grayscale image
// suitable for lossless 16-bit PNG encoding without 8-bit banding. Values outside
// [0, 1] are clamped. Returns nil if h has non-positive dimensions or insufficient samples.
func (h *Heightmap) ToGray16() *image.Gray16 {
	if h == nil || h.Cols <= 0 || h.Rows <= 0 || len(h.Heights) < h.Cols*h.Rows {
		return nil
	}
	img := image.NewGray16(image.Rect(0, 0, h.Cols, h.Rows))
	for r := range h.Rows {
		rowBase := r * h.Cols
		off := img.PixOffset(0, r)
		for c := range h.Cols {
			v := h.Heights[rowBase+c]
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v < 0 {
				v = 0
			} else if v > 1 {
				v = 1
			}
			y := uint16(math.Round(float64(v) * 65535.0))
			p := off + c*2
			img.Pix[p] = uint8(y >> 8)
			img.Pix[p+1] = uint8(y)
		}
	}
	return img
}

// EncodePNG encodes h as a 16-bit grayscale PNG image into w.
func (h *Heightmap) EncodePNG(w io.Writer) error {
	if w == nil {
		return errors.New("world3d: nil writer")
	}
	img := h.ToGray16()
	if img == nil {
		return errors.New("world3d: invalid heightmap for PNG encoding")
	}
	return png.Encode(w, img)
}

// EncodeHeightmapPNG encodes h as a 16-bit grayscale PNG byte slice.
func EncodeHeightmapPNG(h *Heightmap) ([]byte, error) {
	var buf bytes.Buffer
	if err := h.EncodePNG(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeHeightmapPNG decodes a PNG image (preserving full 16-bit precision for
// 16-bit grayscale PNGs) from r and returns a [*Heightmap] with cellSize and heightScale.
func DecodeHeightmapPNG(r io.Reader, cellSize, heightScale float32) (*Heightmap, error) {
	if r == nil {
		return nil, errors.New("world3d: nil reader")
	}
	img, err := png.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("world3d: decode heightmap PNG: %w", err)
	}
	b := img.Bounds()
	if b.Dx() < 2 || b.Dy() < 2 {
		return nil, fmt.Errorf("world3d: heightmap PNG is %dx%d, need at least 2x2", b.Dx(), b.Dy())
	}
	return LoadHeightmapGray(img, cellSize, heightScale), nil
}

// FoliageDensityAt samples the attached foliage density map for layer at world
// coordinates (x, z). The boolean reports whether (x, z) lies inside the
// heightmap's XZ footprint. Returns (0, false) when h is invalid or has no
// density map for layer.
func (h *Heightmap) FoliageDensityAt(layer string, x, z float32) (float32, bool) {
	if !h.valid() || len(h.FoliageDensity) == 0 {
		return 0, false
	}
	dm := h.FoliageDensity[layer]
	if dm == nil || dm.Width <= 0 || dm.Height <= 0 || len(dm.Data) < dm.Width*dm.Height {
		return 0, false
	}
	spanX := float32(h.Cols-1) * h.CellSize
	spanZ := float32(h.Rows-1) * h.CellSize
	u := (x - h.Origin.X) / spanX
	v := (z - h.Origin.Z) / spanZ
	inBounds := !math.IsNaN(float64(u)) && !math.IsNaN(float64(v)) &&
		!math.IsInf(float64(u), 0) && !math.IsInf(float64(v), 0) &&
		u >= 0 && u <= 1 && v >= 0 && v <= 1
	return dm.Sample(u, v), inBounds
}

func (h *Heightmap) valid() bool {
	return h != nil && h.Cols >= 2 && h.Rows >= 2 && h.CellSize > 0 && len(h.Heights) >= h.Cols*h.Rows
}

// SampleHeight returns the world-space Y height at grid sample (col, row),
// clamping col and row to valid sample indices.
func (h *Heightmap) SampleHeight(col, row int) float32 {
	if h == nil || h.Cols <= 0 || h.Rows <= 0 || len(h.Heights) < h.Cols*h.Rows {
		return 0
	}
	if col < 0 {
		col = 0
	} else if col >= h.Cols {
		col = h.Cols - 1
	}
	if row < 0 {
		row = 0
	} else if row >= h.Rows {
		row = h.Rows - 1
	}
	return h.Origin.Y + h.Heights[row*h.Cols+col]*h.HeightScale
}

// Bounds returns the world-space axis-aligned bounding box enclosing the terrain.
func (h *Heightmap) Bounds() math3d.AABB {
	if !h.valid() {
		return math3d.AABBEmpty()
	}
	minY := h.SampleHeight(0, 0)
	maxY := minY
	total := h.Cols * h.Rows
	for i := 1; i < total; i++ {
		y := h.Origin.Y + h.Heights[i]*h.HeightScale
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}
	return math3d.AABB{
		Min: math3d.Vec3{
			X: h.Origin.X,
			Y: minY,
			Z: h.Origin.Z,
		},
		Max: math3d.Vec3{
			X: h.Origin.X + float32(h.Cols-1)*h.CellSize,
			Y: maxY,
			Z: h.Origin.Z + float32(h.Rows-1)*h.CellSize,
		},
	}
}

// HeightAt returns the exact barycentric triangle-interpolated world Y height
// at world coordinates (x, z), or (0, false) if (x, z) lies outside the terrain.
func (h *Heightmap) HeightAt(x, z float32) (float32, bool) {
	if !h.valid() {
		return 0, false
	}
	cx, cz, fx, fz, ok := h.cellCoords(x, z)
	if !ok {
		return 0, false
	}
	h00 := h.SampleHeight(cx, cz)
	h10 := h.SampleHeight(cx+1, cz)
	h01 := h.SampleHeight(cx, cz+1)
	h11 := h.SampleHeight(cx+1, cz+1)

	if fx+fz <= 1 {
		return (1-fx-fz)*h00 + fx*h10 + fz*h01, true
	}
	return (1-fz)*h10 + (1-fx)*h01 + (fx+fz-1)*h11, true
}

// NormalAt returns the unit upward (+Y) surface normal of the terrain triangle
// containing (x, z). If (x, z) lies outside the terrain, Vec3Up is returned.
func (h *Heightmap) NormalAt(x, z float32) math3d.Vec3 {
	if !h.valid() {
		return math3d.Vec3Up()
	}
	cx, cz, fx, fz, ok := h.cellCoords(x, z)
	if !ok {
		return math3d.Vec3Up()
	}
	h00 := h.SampleHeight(cx, cz)
	h10 := h.SampleHeight(cx+1, cz)
	h01 := h.SampleHeight(cx, cz+1)
	h11 := h.SampleHeight(cx+1, cz+1)

	invS := 1 / h.CellSize
	if fx+fz <= 1 {
		return math3d.Vec3{
			X: -(h10 - h00) * invS,
			Y: 1,
			Z: -(h01 - h00) * invS,
		}.Norm()
	}
	return math3d.Vec3{
		X: -(h11 - h01) * invS,
		Y: 1,
		Z: -(h11 - h10) * invS,
	}.Norm()
}

func (h *Heightmap) cellCoords(x, z float32) (cx, cz int, fx, fz float32, ok bool) {
	gx := (x - h.Origin.X) / h.CellSize
	gz := (z - h.Origin.Z) / h.CellSize
	maxC := float32(h.Cols - 1)
	maxR := float32(h.Rows - 1)
	if gx < 0 || gz < 0 || gx > maxC || gz > maxR {
		return 0, 0, 0, 0, false
	}
	cx = int(math.Floor(float64(gx)))
	cz = int(math.Floor(float64(gz)))
	if cx >= h.Cols-1 {
		cx = h.Cols - 2
	}
	if cz >= h.Rows-1 {
		cz = h.Rows - 2
	}
	fx = math3d.Clamp(gx-float32(cx), 0, 1)
	fz = math3d.Clamp(gz-float32(cz), 0, 1)
	return cx, cz, fx, fz, true
}

func (h *Heightmap) cellVertices(cx, cz int) (v00, v10, v01, v11 math3d.Vec3) {
	x0 := h.Origin.X + float32(cx)*h.CellSize
	x1 := x0 + h.CellSize
	z0 := h.Origin.Z + float32(cz)*h.CellSize
	z1 := z0 + h.CellSize
	v00 = math3d.Vec3{X: x0, Y: h.SampleHeight(cx, cz), Z: z0}
	v10 = math3d.Vec3{X: x1, Y: h.SampleHeight(cx+1, cz), Z: z0}
	v01 = math3d.Vec3{X: x0, Y: h.SampleHeight(cx, cz+1), Z: z1}
	v11 = math3d.Vec3{X: x1, Y: h.SampleHeight(cx+1, cz+1), Z: z1}
	return v00, v10, v01, v11
}

// Raycast intersects ray with the heightmap using 2D grid DDA traversal paired
// with exact Möller-Trumbore triangle intersection. It visits only the cells under the ray, O(Cols+Rows) at most, and never scans the whole height grid. Distance and maxDist are
// measured in world-space Euclidean units (ray.Dir need not be unit-length).
func (h *Heightmap) Raycast(ray math3d.Ray, maxDist float32) (TerrainHit, bool) {
	if !h.valid() || maxDist < 0 {
		return TerrainHit{}, false
	}
	dirLen := ray.Dir.Len()
	if dirLen < 1e-7 {
		return TerrainHit{}, false
	}
	unitRay := math3d.Ray{
		Origin: ray.Origin,
		Dir:    ray.Dir.Scale(1 / dirLen),
	}

	// Clip the ray to the terrain's XZ footprint only. Bounds() scans every sample (milliseconds on a large map), and the DDA below already visits only the O(Cols+Rows) cells under the ray, so the Y range is not needed for correctness (#546).
	bounds := math3d.AABB{
		Min: math3d.Vec3{X: h.Origin.X, Y: -math.MaxFloat32, Z: h.Origin.Z},
		Max: math3d.Vec3{X: h.Origin.X + float32(h.Cols-1)*h.CellSize, Y: math.MaxFloat32, Z: h.Origin.Z + float32(h.Rows-1)*h.CellSize},
	}

	tMin, tMax, hitAABB := unitRay.IntersectAABB(bounds)
	if !hitAABB || tMin > maxDist {
		return TerrainHit{}, false
	}
	tStart := max(float32(0), tMin)
	tEnd := min(maxDist, tMax)
	if tStart > tEnd {
		return TerrainHit{}, false
	}

	var best TerrainHit
	bestT := maxDist + 1
	found := false

	testCell := func(cx, cz int) {
		v00, v10, v01, v11 := h.cellVertices(cx, cz)
		// Triangle 0: (v00, v01, v10)
		if t, _, _, ok := unitRay.IntersectTriangle(v00, v01, v10); ok && t >= 0 && t <= maxDist && t < bestT {
			bestT = t
			found = true
			best = TerrainHit{
				Point:    unitRay.At(t),
				Normal:   v01.Sub(v00).Cross(v10.Sub(v00)).Norm(),
				Distance: t,
				Col:      cx,
				Row:      cz,
			}
		}
		// Triangle 1: (v10, v01, v11)
		if t, _, _, ok := unitRay.IntersectTriangle(v10, v01, v11); ok && t >= 0 && t <= maxDist && t < bestT {
			bestT = t
			found = true
			best = TerrainHit{
				Point:    unitRay.At(t),
				Normal:   v01.Sub(v10).Cross(v11.Sub(v10)).Norm(),
				Distance: t,
				Col:      cx,
				Row:      cz,
			}
		}
	}

	// Vertical ray touches only a single cell.
	const axisEps = 1e-7
	absDX := float32(math.Abs(float64(unitRay.Dir.X)))
	absDZ := float32(math.Abs(float64(unitRay.Dir.Z)))
	if absDX < axisEps && absDZ < axisEps {
		cx, cz, _, _, ok := h.cellCoords(unitRay.Origin.X, unitRay.Origin.Z)
		if !ok {
			return TerrainHit{}, false
		}
		testCell(cx, cz)
		return best, found
	}

	// Amanatides-Woo 2D grid DDA traversal from pStart.
	pStart := unitRay.At(tStart)
	cx := int(math.Floor(float64((pStart.X - h.Origin.X) / h.CellSize)))
	cz := int(math.Floor(float64((pStart.Z - h.Origin.Z) / h.CellSize)))
	if cx < 0 {
		cx = 0
	} else if cx >= h.Cols-1 {
		cx = h.Cols - 2
	}
	if cz < 0 {
		cz = 0
	} else if cz >= h.Rows-1 {
		cz = h.Rows - 2
	}

	posInf := float32(math.Inf(1))
	stepX, stepZ := 0, 0
	tMaxX, tMaxZ := posInf, posInf
	tDeltaX, tDeltaZ := posInf, posInf

	if absDX >= axisEps {
		if unitRay.Dir.X > 0 {
			stepX = 1
			nextX := h.Origin.X + float32(cx+1)*h.CellSize
			tMaxX = (nextX - unitRay.Origin.X) / unitRay.Dir.X
		} else {
			stepX = -1
			nextX := h.Origin.X + float32(cx)*h.CellSize
			tMaxX = (nextX - unitRay.Origin.X) / unitRay.Dir.X
		}
		tDeltaX = h.CellSize / absDX
	}
	if absDZ >= axisEps {
		if unitRay.Dir.Z > 0 {
			stepZ = 1
			nextZ := h.Origin.Z + float32(cz+1)*h.CellSize
			tMaxZ = (nextZ - unitRay.Origin.Z) / unitRay.Dir.Z
		} else {
			stepZ = -1
			nextZ := h.Origin.Z + float32(cz)*h.CellSize
			tMaxZ = (nextZ - unitRay.Origin.Z) / unitRay.Dir.Z
		}
		tDeltaZ = h.CellSize / absDZ
	}

	maxSteps := h.Cols + h.Rows + 4
	for range maxSteps {
		if cx < 0 || cx >= h.Cols-1 || cz < 0 || cz >= h.Rows-1 {
			break
		}
		testCell(cx, cz)

		nextT := min(tMaxX, tMaxZ)
		if found && bestT <= nextT+1e-4 {
			return best, true
		}
		if nextT > tEnd+1e-4 {
			break
		}

		if tMaxX <= tMaxZ {
			cx += stepX
			tMaxX += tDeltaX
		} else {
			cz += stepZ
			tMaxZ += tDeltaZ
		}
	}

	return best, found
}

// BuildMesh generates an indexed triangle mesh for the rectangular chunk of
// grid cells starting at (chunkCol, chunkRow) and spanning (chunkCols, chunkRows)
// cells. If chunkCols or chunkRows is <= 0, the chunk extends to the remaining
// edge of the heightmap.
func (h *Heightmap) BuildMesh(chunkCol, chunkRow, chunkCols, chunkRows int) TerrainMesh {
	if !h.valid() {
		return TerrainMesh{}
	}
	if chunkCol < 0 {
		chunkCol = 0
	}
	if chunkRow < 0 {
		chunkRow = 0
	}
	if chunkCol >= h.Cols-1 || chunkRow >= h.Rows-1 {
		return TerrainMesh{}
	}
	if chunkCols <= 0 {
		chunkCols = (h.Cols - 1) - chunkCol
	}
	if chunkRows <= 0 {
		chunkRows = (h.Rows - 1) - chunkRow
	}
	endCol := min(chunkCol+chunkCols, h.Cols-1)
	endRow := min(chunkRow+chunkRows, h.Rows-1)
	nx := endCol - chunkCol
	nz := endRow - chunkRow
	if nx <= 0 || nz <= 0 {
		return TerrainMesh{}
	}

	vertCols := nx + 1
	vertRows := nz + 1
	vertCount := vertCols * vertRows
	mesh := TerrainMesh{
		Positions: make([]math3d.Vec3, vertCount),
		Normals:   make([]math3d.Vec3, vertCount),
		UVs:       make([]math3d.Vec2, vertCount),
		Indices:   make([]uint32, 0, nx*nz*6),
	}

	invMaxC := float32(1) / float32(h.Cols-1)
	invMaxR := float32(1) / float32(h.Rows-1)

	for lr := range vertRows {
		gr := chunkRow + lr
		for lc := range vertCols {
			gc := chunkCol + lc
			idx := lr*vertCols + lc
			mesh.Positions[idx] = math3d.Vec3{
				X: h.Origin.X + float32(gc)*h.CellSize,
				Y: h.SampleHeight(gc, gr),
				Z: h.Origin.Z + float32(gr)*h.CellSize,
			}
			mesh.Normals[idx] = h.vertexNormal(gc, gr)
			mesh.UVs[idx] = math3d.Vec2{
				X: float32(gc) * invMaxC,
				Y: float32(gr) * invMaxR,
			}
		}
	}

	for lr := range nz {
		for lc := range nx {
			i00 := uint32(lr*vertCols + lc)
			i10 := i00 + 1
			i01 := i00 + uint32(vertCols)
			i11 := i01 + 1
			mesh.Indices = append(mesh.Indices,
				i00, i01, i10,
				i10, i01, i11,
			)
		}
	}
	return mesh
}

// vertexNormal computes a smooth unit upward normal at grid vertex (col, row)
// using central differences (one-sided at terrain borders).
func (h *Heightmap) vertexNormal(col, row int) math3d.Vec3 {
	cL := max(0, col-1)
	cR := min(h.Cols-1, col+1)
	rU := max(0, row-1)
	rD := min(h.Rows-1, row+1)

	dhdx := (h.SampleHeight(cR, row) - h.SampleHeight(cL, row)) / (float32(cR-cL) * h.CellSize)
	dhdz := (h.SampleHeight(col, rD) - h.SampleHeight(col, rU)) / (float32(rD-rU) * h.CellSize)
	return math3d.Vec3{X: -dhdx, Y: 1, Z: -dhdz}.Norm()
}
