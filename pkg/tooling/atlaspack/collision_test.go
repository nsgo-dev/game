package atlaspack_test

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tooling/atlaspack"
	"github.com/nsgo-dev/nsgo/core"
)

func filledRectImage(w, h int, rect image.Rectangle, a uint8) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: a})
		}
	}
	return img
}

func circleImage(size int, cx, cy, r float64) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			dx := (float64(x) + 0.5) - cx
			dy := (float64(y) + 0.5) - cy
			if dx*dx+dy*dy <= r*r {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 200, B: 80, A: 255})
			}
		}
	}
	return img
}

func lShapeImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	for y := 2; y < 18; y++ {
		for x := 2; x < 8; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 80, G: 220, B: 120, A: 255})
		}
	}
	for y := 12; y < 18; y++ {
		for x := 2; x < 18; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 80, G: 220, B: 120, A: 255})
		}
	}
	return img
}

func polyContains(poly atlaspack.Polygon, px, py float64) bool {
	pts := make([]core.Point, len(poly))
	for i, p := range poly {
		pts[i] = core.Point{X: core.Points(p.X), Y: core.Points(p.Y)}
	}
	pp, ok := physics.NewPolygon(pts, 0)
	if !ok {
		return false
	}
	return pp.Contains(float32(px), float32(py))
}

func assertValidConvexPolygons(t *testing.T, polys []atlaspack.Polygon, maxVerts int) {
	t.Helper()
	for i, poly := range polys {
		if len(poly) < 3 || len(poly) > maxVerts {
			t.Fatalf("polygon %d has %d vertices, want 3..%d: %+v", i, len(poly), maxVerts, poly)
		}
		pts := make([]core.Point, len(poly))
		for j, v := range poly {
			pts[j] = core.Point{X: core.Points(v.X), Y: core.Points(v.Y)}
		}
		if _, ok := physics.NewPolygon(pts, 0); !ok {
			t.Fatalf("polygon %d is not a valid convex physics.Polygon: %+v", i, poly)
		}
	}
}

func TestTraceCollisionKnownShapes(t *testing.T) {
	t.Run("square", func(t *testing.T) {
		img := filledRectImage(20, 20, image.Rect(4, 3, 16, 15), 255)
		polys, err := atlaspack.TraceCollision(img, atlaspack.CollisionOptions{Mode: atlaspack.CollisionAlpha, MaxVertices: 8})
		if err != nil {
			t.Fatalf("TraceCollision: %v", err)
		}
		assertValidConvexPolygons(t, polys, 8)
		if len(polys) != 1 || len(polys[0]) != 4 {
			t.Fatalf("square produced %d polygons (%+v), want 1 rectangle", len(polys), polys)
		}
		want := atlaspack.Polygon{{X: 4, Y: 3}, {X: 16, Y: 3}, {X: 16, Y: 15}, {X: 4, Y: 15}}
		if !reflect.DeepEqual(polys[0], want) {
			t.Fatalf("square polygon = %+v, want %+v", polys[0], want)
		}
	})

	t.Run("circle within tolerance", func(t *testing.T) {
		const size = 32
		const cx, cy, r = 16.0, 16.0, 12.0
		img := circleImage(size, cx, cy, r)
		polys, err := atlaspack.TraceCollision(img, atlaspack.CollisionOptions{Mode: atlaspack.CollisionAlpha, MaxVertices: 8})
		if err != nil {
			t.Fatalf("TraceCollision: %v", err)
		}
		assertValidConvexPolygons(t, polys, 8)
		if len(polys) != 1 {
			t.Fatalf("circle produced %d polygons, want 1 convex polygon: %+v", len(polys), polys)
		}
		// Every vertex should lie close to radius r from (cx, cy).
		for _, v := range polys[0] {
			dist := math.Hypot(v.X-cx, v.Y-cy)
			if math.Abs(dist-r) > 2.0 {
				t.Fatalf("circle vertex %+v at distance %.2f from center, want within 2px of %.1f", v, dist, r)
			}
		}
		// Interior point must be inside; far corner outside.
		if !polyContains(polys[0], cx, cy) {
			t.Fatal("circle polygon does not contain center")
		}
		if polyContains(polys[0], 2, 2) {
			t.Fatal("circle polygon erroneously contains outer corner (2, 2)")
		}
	})

	t.Run("L-shape splits into convex parts", func(t *testing.T) {
		img := lShapeImage()
		polys, err := atlaspack.TraceCollision(img, atlaspack.CollisionOptions{Mode: atlaspack.CollisionAlpha, MaxVertices: 8})
		if err != nil {
			t.Fatalf("TraceCollision: %v", err)
		}
		assertValidConvexPolygons(t, polys, 8)
		if len(polys) != 2 {
			t.Fatalf("L-shape produced %d polygons, want 2 convex rectangles: %+v", len(polys), polys)
		}
		hitAny := func(x, y float64) bool {
			for _, p := range polys {
				if polyContains(p, x, y) {
					return true
				}
			}
			return false
		}
		// Both arms of the L must be covered.
		if !hitAny(5, 6) {
			t.Fatal("L-shape vertical arm (5, 6) not covered")
		}
		if !hitAny(14, 15) {
			t.Fatal("L-shape horizontal arm (14, 15) not covered")
		}
		// Concave inner cutout (14, 6) must NOT be covered!
		if hitAny(14, 6) {
			t.Fatal("L-shape concave cutout (14, 6) was filled in instead of split")
		}
	})

	t.Run("transparent frame returns nil", func(t *testing.T) {
		img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
		for _, mode := range []atlaspack.CollisionMode{atlaspack.CollisionAlpha, atlaspack.CollisionBox, atlaspack.CollisionEllipse} {
			polys, err := atlaspack.TraceCollision(img, atlaspack.CollisionOptions{Mode: mode})
			if err != nil {
				t.Fatalf("mode %s: %v", mode, err)
			}
			if len(polys) != 0 {
				t.Fatalf("mode %s on transparent frame returned %+v, want empty", mode, polys)
			}
		}
	})

	t.Run("alpha tolerance filters semi-transparent fringe", func(t *testing.T) {
		img := filledRectImage(16, 16, image.Rect(1, 1, 15, 15), 80)
		for y := 4; y < 12; y++ {
			for x := 4; x < 12; x++ {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
		polys, err := atlaspack.TraceCollision(img, atlaspack.CollisionOptions{Mode: atlaspack.CollisionAlpha, Tolerance: 128})
		if err != nil {
			t.Fatalf("TraceCollision: %v", err)
		}
		want := atlaspack.Polygon{{X: 4, Y: 4}, {X: 12, Y: 4}, {X: 12, Y: 12}, {X: 4, Y: 12}}
		if len(polys) != 1 || !reflect.DeepEqual(polys[0], want) {
			t.Fatalf("tolerance=128 got %+v, want %+v", polys, want)
		}
	})

	t.Run("maxVertices cap", func(t *testing.T) {
		img := circleImage(40, 20, 20, 16)
		for _, capV := range []int{4, 6, 8} {
			polys, err := atlaspack.TraceCollision(img, atlaspack.CollisionOptions{Mode: atlaspack.CollisionAlpha, MaxVertices: capV})
			if err != nil {
				t.Fatalf("cap %d: %v", capV, err)
			}
			assertValidConvexPolygons(t, polys, capV)
		}
	})

	t.Run("diagonal saddle and multi-island", func(t *testing.T) {
		img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
		// Diagonal touch at (5,5)
		for y := 2; y < 5; y++ {
			for x := 2; x < 5; x++ {
				img.SetNRGBA(x, y, color.NRGBA{A: 255})
			}
		}
		for y := 5; y < 8; y++ {
			for x := 5; x < 8; x++ {
				img.SetNRGBA(x, y, color.NRGBA{A: 255})
			}
		}
		// Separate island
		for y := 11; y < 14; y++ {
			for x := 10; x < 14; x++ {
				img.SetNRGBA(x, y, color.NRGBA{A: 255})
			}
		}
		polys, err := atlaspack.TraceCollision(img, atlaspack.CollisionOptions{Mode: atlaspack.CollisionAlpha})
		if err != nil {
			t.Fatalf("TraceCollision: %v", err)
		}
		assertValidConvexPolygons(t, polys, 8)
		if len(polys) != 3 {
			t.Fatalf("expected 3 convex boxes for diagonal saddle + island, got %d: %+v", len(polys), polys)
		}
	})

	t.Run("box, ellipse, and explicit concave polygon", func(t *testing.T) {
		img := filledRectImage(16, 16, image.Rect(2, 4, 14, 12), 255)
		boxPolys, err := atlaspack.TraceCollision(img, atlaspack.CollisionOptions{Mode: atlaspack.CollisionBox})
		if err != nil || len(boxPolys) != 1 || len(boxPolys[0]) != 4 {
			t.Fatalf("box mode = %+v, %v", boxPolys, err)
		}
		ellPolys, err := atlaspack.TraceCollision(img, atlaspack.CollisionOptions{Mode: atlaspack.CollisionEllipse, MaxVertices: 6})
		if err != nil || len(ellPolys) != 1 || len(ellPolys[0]) != 6 {
			t.Fatalf("ellipse mode = %+v, %v", ellPolys, err)
		}
		assertValidConvexPolygons(t, ellPolys, 6)

		// Concave L-shape supplied in CCW order should be normalized and split into 2 convex parts.
		lCCW := []atlaspack.Point{{0, 0}, {0, 16}, {6, 16}, {6, 6}, {16, 6}, {16, 0}}
		polyParts, err := atlaspack.TraceCollision(nil, atlaspack.CollisionOptions{Mode: atlaspack.CollisionPolygon, Points: lCCW, MaxVertices: 8})
		if err != nil {
			t.Fatalf("polygon mode: %v", err)
		}
		assertValidConvexPolygons(t, polyParts, 8)
		if len(polyParts) != 2 {
			t.Fatalf("concave explicit polygon got %d parts, want 2: %+v", len(polyParts), polyParts)
		}
	})
}

func TestTraceAndPackDeterminismAndLoadAtlasRoundTrip(t *testing.T) {
	img := lShapeImage()
	firstPolys, err := atlaspack.TraceCollision(img, atlaspack.CollisionOptions{Mode: atlaspack.CollisionAlpha, MaxVertices: 8})
	if err != nil {
		t.Fatal(err)
	}
	pts := map[string]atlaspack.Point{
		"muzzle": {X: 16, Y: 14},
		"feet":   {X: 5, Y: 18},
	}
	res1, err := atlaspack.PackImages([]atlaspack.Input{{Name: "hero.png", Image: img, Shapes: firstPolys, Points: pts}}, atlaspack.Options{Name: "hero", Scale: 2, Trim: true})
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		p, err := atlaspack.TraceCollision(img, atlaspack.CollisionOptions{Mode: atlaspack.CollisionAlpha, MaxVertices: 8})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(p, firstPolys) {
			t.Fatalf("non-deterministic trace: %+v != %+v", p, firstPolys)
		}
		res2, err := atlaspack.PackImages([]atlaspack.Input{{Name: "hero.png", Image: img, Shapes: p, Points: pts}}, atlaspack.Options{Name: "hero", Scale: 2, Trim: true})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(res1.Pages[0].JSON, res2.Pages[0].JSON) {
			t.Fatalf("non-deterministic atlas JSON")
		}
	}

	atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:hero.png"}, res1.Pages[0].JSON)
	if err != nil {
		t.Fatalf("LoadAtlas: %v", err)
	}
	fr := atlas.Frames["hero.png"]
	if len(fr.Shapes) != 2 {
		t.Fatalf("loaded frame has %d shapes, want 2", len(fr.Shapes))
	}
	// Scale=2 divides frame pixel coordinates by 2 into points.
	muzzle, ok := fr.Point("muzzle")
	if !ok || muzzle.X != 8 || muzzle.Y != 7 {
		t.Fatalf("muzzle = %+v, %v; want (8, 7)", muzzle, ok)
	}
	feet, ok := fr.Point("feet")
	if !ok || feet.X != 2.5 || feet.Y != 9 {
		t.Fatalf("feet = %+v, %v; want (2.5, 9)", feet, ok)
	}
}
