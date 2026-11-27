package atlaspack

import (
	"cmp"
	"fmt"
	"image"
	"math"
	"slices"
)

// CollisionMode selects how TraceCollision generates convex collision polygons for an image frame.
type CollisionMode string

const (
	// CollisionNone generates no collision polygons.
	CollisionNone CollisionMode = "none"
	// CollisionAlpha traces solid pixels (alpha > Tolerance) using marching squares, simplifies contours, and splits concave outlines into convex polygons.
	CollisionAlpha CollisionMode = "alpha"
	// CollisionBox fits a tight axis-aligned box around solid pixels (alpha > Tolerance).
	CollisionBox CollisionMode = "box"
	// CollisionEllipse fits an inscribed convex polygon (of MaxVertices sides) inside the tight bounding box of solid pixels (alpha > Tolerance).
	CollisionEllipse CollisionMode = "ellipse"
	// CollisionPolygon validates and splits explicit polygon vertices (in untrimmed frame pixels) into convex polygons of at most MaxVertices vertices.
	CollisionPolygon CollisionMode = "polygon"
)

const (
	// DefaultCollisionMaxVertices is the default maximum vertex count per baked convex polygon, matching physics.MaxPolygonVertices.
	DefaultCollisionMaxVertices = 8
	// minCollisionVertices is the minimum vertex count of a valid convex polygon.
	minCollisionVertices = 3
	// maxCollisionVertices is the maximum vertex count of a baked convex polygon, matching physics.MaxPolygonVertices.
	maxCollisionVertices = 8
	// staircaseRDPEpsilon sits just above sqrt(2)/2 (0.7071) so 1-pixel diagonal staircases collapse into clean edges while real 2px+ corners are preserved.
	staircaseRDPEpsilon = 0.75
	// geomEps guards floating-point cross-product and area comparisons.
	geomEps = 1e-6
)

// Point is a 2D coordinate in untrimmed frame pixels from the top-left corner.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Polygon is a convex collision polygon of 3 to 8 vertices in untrimmed frame pixels,
// wound clockwise in NSGo's +X right, +Y down screen space (positive signed area).
type Polygon []Point

// CollisionOptions configures TraceCollision.
type CollisionOptions struct {
	// Mode selects the generation mode: CollisionAlpha, CollisionBox, CollisionEllipse, CollisionPolygon, or CollisionNone.
	Mode CollisionMode
	// Tolerance is the alpha cutoff in [0, 255] for alpha, box, and ellipse modes: pixels with alpha > Tolerance are solid.
	Tolerance int
	// MaxVertices caps vertices per convex polygon (3..8; 0 defaults to DefaultCollisionMaxVertices).
	MaxVertices int
	// Points holds explicit polygon vertices in untrimmed frame pixels when Mode is CollisionPolygon.
	Points []Point
}

// TraceCollision generates deterministic convex collision polygons (wound clockwise in Y-down frame pixel coordinates)
// for img according to opts. A completely transparent frame returns (nil, nil).
func TraceCollision(img image.Image, opts CollisionOptions) ([]Polygon, error) {
	if opts.Mode == "" || opts.Mode == CollisionNone {
		return nil, nil
	}
	if opts.Tolerance < 0 || opts.Tolerance > 255 {
		return nil, fmt.Errorf("atlaspack: collision tolerance %d must be between 0 and 255", opts.Tolerance)
	}
	maxVerts := opts.MaxVertices
	if maxVerts == 0 {
		maxVerts = DefaultCollisionMaxVertices
	}
	if maxVerts < minCollisionVertices || maxVerts > maxCollisionVertices {
		return nil, fmt.Errorf("atlaspack: collision maxVertices %d must be between %d and %d", maxVerts, minCollisionVertices, maxCollisionVertices)
	}

	if opts.Mode == CollisionPolygon {
		return buildExplicitPolygonCollision(opts.Points, maxVerts)
	}
	if img == nil {
		return nil, nil
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, nil
	}

	switch opts.Mode {
	case CollisionBox:
		rect, ok := alphaBounds(img, uint8(opts.Tolerance))
		if !ok {
			return nil, nil
		}
		poly := Polygon{
			{X: float64(rect.Min.X), Y: float64(rect.Min.Y)},
			{X: float64(rect.Max.X), Y: float64(rect.Min.Y)},
			{X: float64(rect.Max.X), Y: float64(rect.Max.Y)},
			{X: float64(rect.Min.X), Y: float64(rect.Max.Y)},
		}
		return canonicalizePolygons([]Polygon{poly}), nil

	case CollisionEllipse:
		rect, ok := alphaBounds(img, uint8(opts.Tolerance))
		if !ok {
			return nil, nil
		}
		cx := float64(rect.Min.X+rect.Max.X) * 0.5
		cy := float64(rect.Min.Y+rect.Max.Y) * 0.5
		rx := float64(rect.Dx()) * 0.5
		ry := float64(rect.Dy()) * 0.5
		poly := make(Polygon, maxVerts)
		for i := range maxVerts {
			ang := 2 * math.Pi * float64(i) / float64(maxVerts)
			poly[i] = Point{
				X: roundCoord4(cx + rx*math.Cos(ang)),
				Y: roundCoord4(cy + ry*math.Sin(ang)),
			}
		}
		poly = cleanPolygon(poly)
		if len(poly) < minCollisionVertices || signedArea(poly) <= geomEps {
			return nil, nil
		}
		return canonicalizePolygons([]Polygon{poly}), nil

	case CollisionAlpha:
		solid, hasSolid := alphaMask(img, uint8(opts.Tolerance))
		if !hasSolid {
			return nil, nil
		}
		loops := traceAlphaContours(solid, w, h)
		if len(loops) == 0 {
			return nil, nil
		}
		var out []Polygon
		for _, loop := range loops {
			parts := contourToConvexPolygons(loop, maxVerts)
			out = append(out, parts...)
		}
		if len(out) == 0 {
			return nil, nil
		}
		return canonicalizePolygons(out), nil

	default:
		return nil, fmt.Errorf("atlaspack: unknown collision mode %q", opts.Mode)
	}
}

func buildExplicitPolygonCollision(pts []Point, maxVerts int) ([]Polygon, error) {
	if len(pts) < minCollisionVertices {
		return nil, fmt.Errorf("atlaspack: polygon collision requires at least %d points", minCollisionVertices)
	}
	clean := make(Polygon, len(pts))
	for i, p := range pts {
		if math.IsNaN(p.X) || math.IsInf(p.X, 0) || math.IsNaN(p.Y) || math.IsInf(p.Y, 0) {
			return nil, fmt.Errorf("atlaspack: polygon point %d (%v, %v) must be finite", i, p.X, p.Y)
		}
		clean[i] = Point{X: roundCoord4(p.X), Y: roundCoord4(p.Y)}
	}
	clean = cleanPolygon(clean)
	if len(clean) < minCollisionVertices {
		return nil, fmt.Errorf("atlaspack: polygon collision is degenerate after removing duplicate/collinear vertices")
	}
	area := signedArea(clean)
	if math.Abs(area) <= geomEps {
		return nil, fmt.Errorf("atlaspack: polygon collision has zero area")
	}
	if area < 0 {
		slices.Reverse(clean)
	}
	parts := decomposeOrReduce(clean, maxVerts)
	if len(parts) == 0 {
		return nil, fmt.Errorf("atlaspack: polygon collision produced no valid convex parts")
	}
	return canonicalizePolygons(parts), nil
}

// alphaBounds returns the tightest rectangle (relative to img.Bounds().Min) of pixels with alpha > tol.
func alphaBounds(img image.Image, tol uint8) (image.Rectangle, bool) {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X-1, b.Min.Y-1

	switch m := img.(type) {
	case *image.NRGBA:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			row := (y - m.Rect.Min.Y) * m.Stride
			for x := b.Min.X; x < b.Max.X; x++ {
				if m.Pix[row+(x-m.Rect.Min.X)*4+3] > tol {
					minX = min(minX, x)
					minY = min(minY, y)
					maxX = max(maxX, x)
					maxY = max(maxY, y)
				}
			}
		}
	case *image.RGBA:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			row := (y - m.Rect.Min.Y) * m.Stride
			for x := b.Min.X; x < b.Max.X; x++ {
				if m.Pix[row+(x-m.Rect.Min.X)*4+3] > tol {
					minX = min(minX, x)
					minY = min(minY, y)
					maxX = max(maxX, x)
					maxY = max(maxY, y)
				}
			}
		}
	default:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				_, _, _, a := img.At(x, y).RGBA()
				if uint8(a>>8) > tol {
					minX = min(minX, x)
					minY = min(minY, y)
					maxX = max(maxX, x)
					maxY = max(maxY, y)
				}
			}
		}
	}

	if maxX < minX || maxY < minY {
		return image.Rectangle{}, false
	}
	return image.Rect(minX-b.Min.X, minY-b.Min.Y, maxX+1-b.Min.X, maxY+1-b.Min.Y), true
}

func alphaMask(img image.Image, tol uint8) ([]bool, bool) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	solid := make([]bool, w*h)
	hasSolid := false

	switch m := img.(type) {
	case *image.NRGBA:
		for y := range h {
			row := (b.Min.Y + y - m.Rect.Min.Y) * m.Stride
			for x := range w {
				if m.Pix[row+(b.Min.X+x-m.Rect.Min.X)*4+3] > tol {
					solid[y*w+x] = true
					hasSolid = true
				}
			}
		}
	case *image.RGBA:
		for y := range h {
			row := (b.Min.Y + y - m.Rect.Min.Y) * m.Stride
			for x := range w {
				if m.Pix[row+(b.Min.X+x-m.Rect.Min.X)*4+3] > tol {
					solid[y*w+x] = true
					hasSolid = true
				}
			}
		}
	default:
		for y := range h {
			for x := range w {
				_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				if uint8(a>>8) > tol {
					solid[y*w+x] = true
					hasSolid = true
				}
			}
		}
	}
	return solid, hasSolid
}

// traceAlphaContours runs exact pixel-boundary marching squares on solid (w x h) and returns
// every outer contour (positive signed area in Y-down space) with collinear grid edges merged.
// Diagonal saddle corners (marching squares cases 5 and 10) always take the sharpest right turn,
// separating diagonal pixels into disjoint, non-self-intersecting simple loops.
func traceAlphaContours(solid []bool, w, h int) []Polygon {
	// Directions in Y-down screen space: 0=Right(+1,0), 1=Down(0,+1), 2=Left(-1,0), 3=Up(0,-1).
	const (
		dirRight = 0
		dirDown  = 1
		dirLeft  = 2
		dirUp    = 3
	)
	dx := [4]int{1, 0, -1, 0}
	dy := [4]int{0, 1, 0, -1}

	vw := w + 1
	outMask := make([]uint8, vw*(h+1))

	isSolid := func(x, y int) bool {
		return x >= 0 && x < w && y >= 0 && y < h && solid[y*w+x]
	}

	for y := range h {
		for x := range w {
			if !solid[y*w+x] {
				continue
			}
			// Top edge: (x, y) -> (x+1, y)
			if !isSolid(x, y-1) {
				outMask[y*vw+x] |= 1 << dirRight
			}
			// Right edge: (x+1, y) -> (x+1, y+1)
			if !isSolid(x+1, y) {
				outMask[y*vw+(x+1)] |= 1 << dirDown
			}
			// Bottom edge: (x+1, y+1) -> (x, y+1)
			if !isSolid(x, y+1) {
				outMask[(y+1)*vw+(x+1)] |= 1 << dirLeft
			}
			// Left edge: (x, y+1) -> (x, y)
			if !isSolid(x-1, y) {
				outMask[(y+1)*vw+x] |= 1 << dirUp
			}
		}
	}

	var loops []Polygon
	for sy := range h + 1 {
		for sx := range w + 1 {
			for sdir := range 4 {
				if outMask[sy*vw+sx]&(1<<uint(sdir)) == 0 {
					continue
				}
				var pts Polygon
				cx, cy, dir := sx, sy, sdir
				first := true
				prevDir := -1
				for {
					outMask[cy*vw+cx] &^= 1 << uint(dir)
					if first || dir != prevDir {
						pts = append(pts, Point{X: float64(cx), Y: float64(cy)})
					}
					first = false
					prevDir = dir
					cx += dx[dir]
					cy += dy[dir]
					if cx == sx && cy == sy {
						// Check if closing edge has the same direction as the starting edge.
						if dir == sdir && len(pts) > 1 {
							pts = pts[1:]
						}
						break
					}
					m := outMask[cy*vw+cx]
					// Sharpest right turn first: right -> straight -> left.
					nextDir := -1
					for _, cand := range [3]int{(dir + 1) & 3, dir, (dir + 3) & 3} {
						if m&(1<<uint(cand)) != 0 {
							nextDir = cand
							break
						}
					}
					if nextDir < 0 {
						break
					}
					dir = nextDir
				}
				if len(pts) >= minCollisionVertices && signedArea(pts) > geomEps {
					loops = append(loops, pts)
				}
			}
		}
	}
	return loops
}

func contourToConvexPolygons(loop Polygon, maxVerts int) []Polygon {
	cleaned := cleanPolygon(loop)
	if len(cleaned) < minCollisionVertices || signedArea(cleaned) <= geomEps {
		return nil
	}
	if isConvexCW(cleaned) {
		return []Polygon{reduceConvexPolygon(cleaned, maxVerts)}
	}
	simplified := simplifyClosedRDP(cleaned, staircaseRDPEpsilon)
	simplified = cleanPolygon(simplified)
	if len(simplified) < minCollisionVertices || signedArea(simplified) <= geomEps {
		hull := convexHullCW(cleaned)
		if len(hull) < minCollisionVertices {
			return nil
		}
		return []Polygon{reduceConvexPolygon(hull, maxVerts)}
	}
	// If the simplified contour is nearly identical to its convex hull (e.g. a rasterized circle
	// with a tiny pixel-grid quantization notch), treat it as convex so a circle stays one polygon
	// rather than splitting into slivers.
	if isConvexCW(simplified) {
		return []Polygon{reduceConvexPolygon(simplified, maxVerts)}
	}
	hull := convexHullCW(simplified)
	if len(hull) >= minCollisionVertices && nearConvex(simplified, hull, 1.0) {
		return []Polygon{reduceConvexPolygon(hull, maxVerts)}
	}
	return decomposeOrReduce(simplified, maxVerts)
}

// nearConvex reports whether every vertex of poly lies within maxDist pixels of the boundary of its convex hull
// and the area difference is tiny (<= 3%), preventing rasterization jaggies on circles from splitting into extra shapes
// while preserving genuine concavities like L-shapes.
func nearConvex(poly, hull Polygon, maxDist float64) bool {
	polyArea := signedArea(poly)
	hullArea := signedArea(hull)
	if hullArea <= geomEps || (hullArea-polyArea)/hullArea > 0.03 {
		return false
	}
	for _, p := range poly {
		if pointToPolygonEdgeDist(p, hull) > maxDist {
			return false
		}
	}
	return true
}

func pointToPolygonEdgeDist(p Point, poly Polygon) float64 {
	best := math.Inf(1)
	n := len(poly)
	for i := range n {
		d := pointToSegmentDist(p, poly[i], poly[(i+1)%n])
		if d < best {
			best = d
		}
	}
	return best
}

func decomposeOrReduce(poly Polygon, maxVerts int) []Polygon {
	poly = cleanPolygon(poly)
	if len(poly) < minCollisionVertices || signedArea(poly) <= geomEps {
		return nil
	}
	if isConvexCW(poly) {
		return []Polygon{reduceConvexPolygon(poly, maxVerts)}
	}
	tris := triangulateEarClip(poly)
	if len(tris) == 0 {
		hull := convexHullCW(poly)
		if len(hull) < minCollisionVertices {
			return nil
		}
		return []Polygon{reduceConvexPolygon(hull, maxVerts)}
	}
	merged := mergeConvexHertelMehlhorn(tris, maxVerts)
	var out []Polygon
	for _, p := range merged {
		p = cleanPolygon(p)
		if len(p) < minCollisionVertices || signedArea(p) <= geomEps {
			continue
		}
		if !isConvexCW(p) {
			p = convexHullCW(p)
		}
		if len(p) >= minCollisionVertices {
			out = append(out, reduceConvexPolygon(p, maxVerts))
		}
	}
	return out
}

// simplifyClosedRDP runs Ramer-Douglas-Peucker on a closed loop by splitting between the two farthest
// vertices so the result does not depend on the loop's starting vertex.
func simplifyClosedRDP(pts Polygon, eps float64) Polygon {
	n := len(pts)
	if n <= minCollisionVertices || eps <= 0 {
		return slices.Clone(pts)
	}
	// Find deterministic farthest pair (iA, iB).
	bestDist2 := -1.0
	iA, iB := 0, n/2
	for i := range n {
		for j := i + 1; j < n; j++ {
			dx := pts[j].X - pts[i].X
			dy := pts[j].Y - pts[i].Y
			d2 := dx*dx + dy*dy
			if d2 > bestDist2+1e-9 || (math.Abs(d2-bestDist2) <= 1e-9 && (cmpPoint(pts[i], pts[iA]) < 0 || (pts[i] == pts[iA] && cmpPoint(pts[j], pts[iB]) < 0))) {
				bestDist2 = d2
				iA, iB = i, j
			}
		}
	}
	chain1 := make(Polygon, 0, iB-iA+1)
	for i := iA; i <= iB; i++ {
		chain1 = append(chain1, pts[i])
	}
	chain2 := make(Polygon, 0, n-(iB-iA)+1)
	for i := iB; i < n; i++ {
		chain2 = append(chain2, pts[i])
	}
	for i := 0; i <= iA; i++ {
		chain2 = append(chain2, pts[i])
	}

	s1 := rdpOpen(chain1, eps)
	s2 := rdpOpen(chain2, eps)
	out := make(Polygon, 0, len(s1)+len(s2)-2)
	out = append(out, s1[:len(s1)-1]...)
	out = append(out, s2[:len(s2)-1]...)
	return out
}

func rdpOpen(pts Polygon, eps float64) Polygon {
	n := len(pts)
	if n <= 2 {
		return slices.Clone(pts)
	}
	a, b := pts[0], pts[n-1]
	maxDist := -1.0
	maxIdx := 0
	for i := 1; i < n-1; i++ {
		d := pointToSegmentDist(pts[i], a, b)
		if d > maxDist {
			maxDist = d
			maxIdx = i
		}
	}
	if maxDist <= eps {
		return Polygon{a, b}
	}
	left := rdpOpen(pts[:maxIdx+1], eps)
	right := rdpOpen(pts[maxIdx:], eps)
	out := make(Polygon, 0, len(left)+len(right)-1)
	out = append(out, left[:len(left)-1]...)
	out = append(out, right...)
	return out
}

func pointToSegmentDist(p, a, b Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	lenSq := dx*dx + dy*dy
	if lenSq <= 1e-18 {
		return math.Hypot(p.X-a.X, p.Y-a.Y)
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / lenSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	qx := a.X + t*dx
	qy := a.Y + t*dy
	return math.Hypot(p.X-qx, p.Y-qy)
}

// reduceConvexPolygon reduces a convex CW polygon to at most maxVerts vertices by iteratively
// dropping the vertex whose removal loses the least area (Visvalingam-Whyatt).
func reduceConvexPolygon(poly Polygon, maxVerts int) Polygon {
	out := cleanPolygon(poly)
	for len(out) > maxVerts && len(out) > minCollisionVertices {
		bestIdx := 0
		bestArea := math.Inf(1)
		n := len(out)
		for i := range n {
			prev := out[(i+n-1)%n]
			curr := out[i]
			next := out[(i+1)%n]
			area := math.Abs(cross2(prev, curr, next)) * 0.5
			if area < bestArea-1e-12 {
				bestArea = area
				bestIdx = i
			}
		}
		out = slices.Delete(out, bestIdx, bestIdx+1)
	}
	return cleanPolygon(out)
}

// triangulateEarClip triangulates a simple CW polygon (positive signed area in Y-down) into CW triangles.
func triangulateEarClip(poly Polygon) []Polygon {
	ring := slices.Clone(poly)
	if len(ring) < minCollisionVertices {
		return nil
	}
	var tris []Polygon
	maxIter := len(ring) * len(ring)
	for iter := 0; len(ring) > 3 && iter < maxIter; iter++ {
		n := len(ring)
		bestIdx := -1
		bestQuality := -1.0
		for i := range n {
			prev := ring[(i+n-1)%n]
			curr := ring[i]
			next := ring[(i+1)%n]
			c := cross2(prev, curr, next)
			if c <= geomEps {
				continue
			}
			hasInside := false
			for j := range n {
				if j == (i+n-1)%n || j == i || j == (i+1)%n {
					continue
				}
				p := ring[j]
				if p == prev || p == curr || p == next {
					continue
				}
				if pointInTriangleCW(p, prev, curr, next) {
					hasInside = true
					break
				}
			}
			if hasInside {
				continue
			}
			// Prefer well-conditioned triangles (higher area / perimeter^2) to avoid slivers.
			d1 := distSq(prev, curr)
			d2 := distSq(curr, next)
			d3 := distSq(next, prev)
			q := c / (d1 + d2 + d3 + 1e-12)
			if q > bestQuality {
				bestQuality = q
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			// Fallback if numeric degeneracy blocks strict ear clipping: drop the shallowest convex corner.
			for i := range n {
				prev := ring[(i+n-1)%n]
				curr := ring[i]
				next := ring[(i+1)%n]
				if cross2(prev, curr, next) > geomEps {
					bestIdx = i
					break
				}
			}
			if bestIdx < 0 {
				break
			}
		}
		prev := ring[(bestIdx+n-1)%n]
		curr := ring[bestIdx]
		next := ring[(bestIdx+1)%n]
		tris = append(tris, Polygon{prev, curr, next})
		ring = slices.Delete(ring, bestIdx, bestIdx+1)
	}
	if len(ring) == 3 && signedArea(ring) > geomEps {
		tris = append(tris, Polygon{ring[0], ring[1], ring[2]})
	}
	return tris
}

// mergeConvexHertelMehlhorn greedily merges adjacent convex polygons sharing an internal edge
// whenever the merged polygon remains convex and has at most maxVerts vertices.
func mergeConvexHertelMehlhorn(polys []Polygon, maxVerts int) []Polygon {
	out := make([]Polygon, len(polys))
	for i, p := range polys {
		out[i] = slices.Clone(p)
	}
	for {
		bestI, bestJ := -1, -1
		var bestMerged Polygon
		bestScore := -1.0
		for i := 0; i < len(out); i++ {
			for j := i + 1; j < len(out); j++ {
				merged, ok := tryMergeConvex(out[i], out[j], maxVerts)
				if !ok {
					continue
				}
				// Prefer merges with larger area and fewer resulting vertices (e.g. turning 2 triangles into a clean rectangle).
				score := signedArea(merged) / float64(len(merged))
				if score > bestScore+1e-9 {
					bestScore = score
					bestI, bestJ = i, j
					bestMerged = merged
				}
			}
		}
		if bestI < 0 {
			break
		}
		out[bestI] = bestMerged
		out = slices.Delete(out, bestJ, bestJ+1)
	}
	return out
}

func tryMergeConvex(a, b Polygon, maxVerts int) (Polygon, bool) {
	na, nb := len(a), len(b)
	for ia := range na {
		a0 := a[ia]
		a1 := a[(ia+1)%na]
		for ib := range nb {
			b0 := b[ib]
			b1 := b[(ib+1)%nb]
			if !pointsClose(a0, b1) || !pointsClose(a1, b0) {
				continue
			}
			// Splice b into a replacing edge (a0 -> a1) with b's chain from b1 (=a0) around to b0 (=a1).
			merged := make(Polygon, 0, na+nb-2)
			for k := 0; k < na-1; k++ {
				merged = append(merged, a[(ia+1+k)%na])
			}
			for k := 0; k < nb-1; k++ {
				merged = append(merged, b[(ib+1+k)%nb])
			}
			merged = cleanPolygon(merged)
			if len(merged) < minCollisionVertices || len(merged) > maxVerts {
				return nil, false
			}
			if !isConvexCW(merged) || signedArea(merged) <= geomEps {
				return nil, false
			}
			return merged, true
		}
	}
	return nil, false
}

func pointInTriangleCW(p, a, b, c Point) bool {
	c1 := cross2(a, b, p)
	c2 := cross2(b, c, p)
	c3 := cross2(c, a, p)
	return c1 >= -geomEps && c2 >= -geomEps && c3 >= -geomEps
}

func cross2(o, a, b Point) float64 {
	return (a.X-o.X)*(b.Y-o.Y) - (a.Y-o.Y)*(b.X-o.X)
}

func distSq(a, b Point) float64 {
	dx, dy := a.X-b.X, a.Y-b.Y
	return dx*dx + dy*dy
}

func pointsClose(a, b Point) bool {
	return math.Abs(a.X-b.X) <= 1e-5 && math.Abs(a.Y-b.Y) <= 1e-5
}

// signedArea returns the signed area of p in Y-down screen coordinates (> 0 for clockwise).
func signedArea(p Polygon) float64 {
	n := len(p)
	if n < 3 {
		return 0
	}
	var sum float64
	for i := range n {
		j := (i + 1) % n
		sum += p[i].X*p[j].Y - p[j].X*p[i].Y
	}
	return sum * 0.5
}

// isConvexCW reports whether p is a convex polygon wound clockwise in Y-down space (positive signed area).
func isConvexCW(p Polygon) bool {
	n := len(p)
	if n < 3 || signedArea(p) <= geomEps {
		return false
	}
	for i := range n {
		prev := p[(i+n-1)%n]
		curr := p[i]
		next := p[(i+1)%n]
		if cross2(prev, curr, next) < -geomEps {
			return false
		}
	}
	return true
}

// cleanPolygon removes consecutive near-duplicate and collinear vertices from a closed polygon.
func cleanPolygon(pts Polygon) Polygon {
	if len(pts) == 0 {
		return nil
	}
	out := make(Polygon, 0, len(pts))
	for _, p := range pts {
		if len(out) > 0 && pointsClose(out[len(out)-1], p) {
			continue
		}
		out = append(out, p)
	}
	for len(out) > 1 && pointsClose(out[0], out[len(out)-1]) {
		out = out[:len(out)-1]
	}
	for changed := true; changed && len(out) >= 3; {
		changed = false
		n := len(out)
		for i := range n {
			prev := out[(i+n-1)%n]
			curr := out[i]
			next := out[(i+1)%n]
			segLen := math.Hypot(next.X-prev.X, next.Y-prev.Y)
			if segLen <= 1e-9 || math.Abs(cross2(prev, next, curr))/segLen <= 1e-5 {
				out = slices.Delete(out, i, i+1)
				changed = true
				break
			}
		}
	}
	return out
}

// convexHullCW returns the convex hull of pts wound clockwise in Y-down space (Andrew's monotone chain).
func convexHullCW(pts Polygon) Polygon {
	sorted := slices.Clone(pts)
	slices.SortFunc(sorted, func(a, b Point) int {
		if c := cmp.Compare(a.X, b.X); c != 0 {
			return c
		}
		return cmp.Compare(a.Y, b.Y)
	})
	welded := sorted[:0]
	for _, p := range sorted {
		if len(welded) > 0 && pointsClose(welded[len(welded)-1], p) {
			continue
		}
		welded = append(welded, p)
	}
	if len(welded) < 3 {
		return nil
	}
	hull := make(Polygon, 0, 2*len(welded))
	for _, p := range welded {
		for len(hull) >= 2 && cross2(hull[len(hull)-2], hull[len(hull)-1], p) <= geomEps {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	lower := len(hull) + 1
	for i := len(welded) - 2; i >= 0; i-- {
		p := welded[i]
		for len(hull) >= lower && cross2(hull[len(hull)-2], hull[len(hull)-1], p) <= geomEps {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	hull = hull[:len(hull)-1]
	return cleanPolygon(hull)
}

func canonicalizePolygons(polys []Polygon) []Polygon {
	out := make([]Polygon, 0, len(polys))
	for _, p := range polys {
		if len(p) < minCollisionVertices {
			continue
		}
		norm := make(Polygon, len(p))
		for i, pt := range p {
			norm[i] = Point{X: roundCoord4(pt.X), Y: roundCoord4(pt.Y)}
		}
		norm = cleanPolygon(norm)
		if len(norm) < minCollisionVertices || signedArea(norm) <= geomEps {
			continue
		}
		// Rotate vertex slice so the lexicographically smallest (Y, X) vertex comes first.
		best := 0
		for i := 1; i < len(norm); i++ {
			if cmpPoint(norm[i], norm[best]) < 0 {
				best = i
			}
		}
		if best > 0 {
			rot := make(Polygon, len(norm))
			copy(rot, norm[best:])
			copy(rot[len(norm)-best:], norm[:best])
			norm = rot
		}
		out = append(out, norm)
	}
	slices.SortFunc(out, cmpPolygon)
	return out
}

func cmpPoint(a, b Point) int {
	if c := cmp.Compare(a.Y, b.Y); c != 0 {
		return c
	}
	return cmp.Compare(a.X, b.X)
}

func cmpPolygon(a, b Polygon) int {
	n := min(len(a), len(b))
	for i := range n {
		if c := cmpPoint(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

func roundCoord4(v float64) float64 {
	r := math.Round(v*10000) / 10000
	if r == 0 {
		return 0
	}
	return r
}
