// Package gametest provides deterministic software rendering of nsgo Canvas
// draw operations, golden PNG comparison, and visual diff generation for tests.
package gametest

import (
	"bytes"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

var updateGolden = flag.Bool("update-golden", false, "update golden PNG test fixtures")

// DefaultGoldenDir is the relative directory used by AssertGolden and AssertOpsGolden.
const DefaultGoldenDir = "testdata/golden"

// RenderOptions configures optional inputs for RenderOps.
type RenderOptions struct {
	// Background fills the output image before replaying ops.
	// The zero value leaves the canvas transparent.
	Background color.RGBA
	// Images resolves core.ImageSource.URI values referenced by DrawImage ops
	// when neither Pixels nor Data is set on the ImageSource.
	Images map[string]image.Image
}

// RenderOps deterministically rasterizes recorded core.DrawOp operations into an RGBA image of size width by height pixels.
//
// Supported operations include Save, Restore, Translate, Scale, Rotate, Transform, SetTransform, SetAlpha, SetFillColor, SetStrokeColor, SetLineWidth, Clear, Rect + Fill/Stroke (including FillRect and StrokeRect), polygon paths (MoveTo, LineTo, ClosePath + Fill/Stroke), DrawImage / DrawImageIn / DrawImageFrom and DrawAtlas.
func RenderOps(width, height int, ops []core.DrawOp, opts ...RenderOptions) *image.RGBA {
	if width <= 0 || height <= 0 {
		return image.NewRGBA(image.Rectangle{})
	}
	var opt RenderOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	if opt.Background != (color.RGBA{}) {
		for y := 0; y < height; y++ {
			row := dst.Pix[y*dst.Stride:]
			for x := 0; x < width; x++ {
				off := x * 4
				row[off] = opt.Background.R
				row[off+1] = opt.Background.G
				row[off+2] = opt.Background.B
				row[off+3] = opt.Background.A
			}
		}
	}

	type state struct {
		m           affine
		alpha       float64
		fillColor   core.Color
		strokeColor core.Color
		lineWidth   float64
	}
	type pathRect struct {
		x, y, w, h float64
		m          affine
	}

	cur := state{
		m:           identityAffine(),
		alpha:       1,
		fillColor:   core.RGB(0x000000),
		strokeColor: core.RGB(0x000000),
		lineWidth:   1,
	}
	var stack []state
	var rects []pathRect
	var subs []pathSub
	var curSub []pathPt
	decoded := make(map[string]image.Image)

	resolveImage := func(src *core.ImageSource) image.Image {
		if src == nil {
			return nil
		}
		if src.Pixels != nil {
			return src.Pixels
		}
		if len(src.Data) > 0 {
			key := string(src.Data)
			if img, ok := decoded[key]; ok {
				return img
			}
			img, _, err := image.Decode(bytes.NewReader(src.Data))
			if err != nil {
				return nil
			}
			decoded[key] = img
			return img
		}
		if src.URI != "" && opt.Images != nil {
			return opt.Images[src.URI]
		}
		return nil
	}

	for _, op := range ops {
		switch op.Kind {
		case core.DrawSave:
			stack = append(stack, cur)
		case core.DrawRestore:
			if n := len(stack); n > 0 {
				cur = stack[n-1]
				stack = stack[:n-1]
			}
		case core.DrawTranslate:
			cur.m = cur.m.mul(affine{a: 1, d: 1, e: float64(op.X), f: float64(op.Y)})
		case core.DrawScale:
			cur.m = cur.m.mul(affine{a: float64(op.X), d: float64(op.Y)})
		case core.DrawRotate:
			sin, cos := math.Sincos(float64(op.A))
			cur.m = cur.m.mul(affine{a: cos, b: sin, c: -sin, d: cos})
		case core.DrawTransform:
			cur.m = cur.m.mul(affine{
				a: float64(op.X1), b: float64(op.Y1),
				c: float64(op.X2), d: float64(op.Y2),
				e: float64(op.X), f: float64(op.Y),
			})
		case core.DrawSetTransform:
			cur.m = affine{
				a: float64(op.X1), b: float64(op.Y1),
				c: float64(op.X2), d: float64(op.Y2),
				e: float64(op.X), f: float64(op.Y),
			}
		case core.DrawAlpha:
			cur.alpha = clamp01(float64(op.A))
		case core.DrawFillColor:
			cur.fillColor = op.Color
		case core.DrawStrokeColor:
			cur.strokeColor = op.Color
		case core.DrawLineWidth:
			if op.W > 0 {
				cur.lineWidth = float64(op.W)
			}
		case core.DrawBeginPath:
			rects = rects[:0]
			subs = subs[:0]
			curSub = nil
		case core.DrawRect:
			rects = append(rects, pathRect{
				x: float64(op.X), y: float64(op.Y),
				w: float64(op.W), h: float64(op.H),
				m: cur.m,
			})
		case core.DrawMoveTo:
			if len(curSub) > 0 {
				subs = append(subs, pathSub{pts: curSub})
			}
			px, py := cur.m.apply(float64(op.X), float64(op.Y))
			curSub = []pathPt{{x: px, y: py}}
		case core.DrawLineTo:
			px, py := cur.m.apply(float64(op.X), float64(op.Y))
			curSub = append(curSub, pathPt{x: px, y: py})
		case core.DrawClosePath:
			if len(curSub) > 0 {
				subs = append(subs, pathSub{pts: curSub, closed: true})
				curSub = nil
			}
		case core.DrawFill:
			sr, sg, sb, sa := premulFromColor(cur.fillColor, cur.alpha)
			if sa <= 0 {
				continue
			}
			for _, r := range rects {
				fillTransformedRect(dst, r.m, r.x, r.y, r.w, r.h, sr, sg, sb, sa)
			}
			if len(subs) > 0 || len(curSub) >= 3 {
				all := subs
				if len(curSub) >= 3 {
					all = append(slices.Clone(subs), pathSub{pts: curSub, closed: true})
				}
				fillPolygons(dst, all, op.EvenOdd, sr, sg, sb, sa)
			}
		case core.DrawStroke:
			sr, sg, sb, sa := premulFromColor(cur.strokeColor, cur.alpha)
			if sa <= 0 || cur.lineWidth <= 0 {
				continue
			}
			for _, r := range rects {
				strokeTransformedRect(dst, r.m, r.x, r.y, r.w, r.h, cur.lineWidth, sr, sg, sb, sa)
			}
			if len(subs) > 0 || len(curSub) >= 2 {
				all := subs
				if len(curSub) >= 2 {
					all = append(slices.Clone(subs), pathSub{pts: curSub})
				}
				strokePolygons(dst, all, cur.lineWidth, sr, sg, sb, sa)
			}
		case core.DrawClear:
			clearTransformedRect(dst, cur.m, float64(op.X), float64(op.Y), float64(op.W), float64(op.H))
		case core.DrawImage:
			if cur.alpha <= 0 {
				continue
			}
			img := resolveImage(op.Image)
			if img == nil {
				continue
			}
			b := img.Bounds()
			if b.Empty() {
				continue
			}
			sx, sy, sw, sh := float64(op.X1), float64(op.Y1), float64(op.X2), float64(op.Y2)
			if sw == 0 && sh == 0 {
				sw, sh = float64(b.Dx()), float64(b.Dy())
			}
			dx, dy, dw, dh := float64(op.X), float64(op.Y), float64(op.W), float64(op.H)
			if dw == 0 && dh == 0 {
				dw, dh = sw, sh
			}
			drawTransformedImage(dst, cur.m, img, sx, sy, sw, sh, dx, dy, dw, dh, cur.alpha, op.Tinted, op.Color)
		case core.DrawAtlas:
			if cur.alpha <= 0 || op.Atlas == nil {
				continue
			}
			img := resolveImage(op.Image)
			if img == nil || img.Bounds().Empty() {
				continue
			}
			for _, s := range *op.Atlas {
				alpha := cur.alpha * float64(s.Alpha)
				if alpha <= 0 {
					continue
				}
				t := s.Transform
				m := cur.m.mul(affine{
					a: float64(t.SCos), b: float64(t.SSin),
					c: -float64(t.SSin), d: float64(t.SCos),
					e: float64(t.TX), f: float64(t.TY),
				})
				sw, sh := float64(s.Rect.Size.Width), float64(s.Rect.Size.Height)
				drawTransformedImage(dst, m, img, float64(s.Rect.Origin.X), float64(s.Rect.Origin.Y), sw, sh, 0, 0, sw, sh, alpha, false, 0)
			}
		}
	}

	return dst
}

// DiffImage compares got and want pixel by pixel and returns a visual diff
// image along with the count of mismatched pixels.
//
// Matching pixels are rendered as a dimmed grayscale context; mismatched pixels
// (including pixels outside either image's bounds when dimensions differ) are
// highlighted in solid magenta (#ff00ff).
func DiffImage(got, want image.Image) (diff *image.RGBA, mismatchedPixels int) {
	var gw, gh, ww, wh int
	var gb, wb image.Rectangle
	if got != nil {
		gb = got.Bounds()
		gw, gh = gb.Dx(), gb.Dy()
	}
	if want != nil {
		wb = want.Bounds()
		ww, wh = wb.Dx(), wb.Dy()
	}

	w := max(gw, ww)
	h := max(gh, wh)
	diff = image.NewRGBA(image.Rect(0, 0, w, h))

	magenta := color.RGBA{R: 255, G: 0, B: 255, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			inGot := x < gw && y < gh
			inWant := x < ww && y < wh
			if !inGot || !inWant {
				mismatchedPixels++
				diff.SetRGBA(x, y, magenta)
				continue
			}
			gr, gg, gbVal, ga := got.At(gb.Min.X+x, gb.Min.Y+y).RGBA()
			wr, wg, wbVal, wa := want.At(wb.Min.X+x, wb.Min.Y+y).RGBA()
			if gr != wr || gg != wg || gbVal != wbVal || ga != wa {
				mismatchedPixels++
				diff.SetRGBA(x, y, magenta)
				continue
			}
			// Dimmed grayscale context for unchanged pixels.
			lum := (299*(gr>>8) + 587*(gg>>8) + 114*(gbVal>>8)) / 1000
			dim := uint8(lum / 3)
			alpha := uint8(ga >> 8)
			diff.SetRGBA(x, y, color.RGBA{R: dim, G: dim, B: dim, A: alpha})
		}
	}
	return diff, mismatchedPixels
}

// ShouldUpdateGolden reports whether golden updates are enabled via the
// -update-golden flag or the UPDATE_GOLDEN environment variable.
func ShouldUpdateGolden() bool {
	if *updateGolden {
		return true
	}
	v := strings.TrimSpace(strings.ToLower(os.Getenv("UPDATE_GOLDEN")))
	return v == "1" || v == "true" || v == "yes"
}

// GoldenPath returns the standard golden PNG file path under dir for name.
func GoldenPath(dir, name string) string {
	if dir == "" {
		dir = DefaultGoldenDir
	}
	if !strings.HasSuffix(name, ".png") {
		name += ".png"
	}
	return filepath.Join(dir, filepath.FromSlash(name))
}

// WritePNG encodes img as a PNG file at path, creating parent directories as needed.
func WritePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// ReadPNG opens and decodes a PNG image from path.
func ReadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// CompareGolden compares got against the PNG stored at goldenPath.
//
// When update is true, got is written to goldenPath and CompareGolden returns
// (nil, 0, nil). When update is false, CompareGolden decodes goldenPath and
// returns DiffImage(got, want).
func CompareGolden(got image.Image, goldenPath string, update bool) (*image.RGBA, int, error) {
	if got == nil {
		return nil, 0, errors.New("gametest: got image is nil")
	}
	if update {
		if err := WritePNG(goldenPath, got); err != nil {
			return nil, 0, fmt.Errorf("gametest: write golden %s: %w", goldenPath, err)
		}
		return nil, 0, nil
	}
	want, err := ReadPNG(goldenPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, fmt.Errorf("gametest: golden %s not found (re-run with -update-golden or UPDATE_GOLDEN=1 to create it)", goldenPath)
		}
		return nil, 0, fmt.Errorf("gametest: read golden %s: %w", goldenPath, err)
	}
	diff, mismatched := DiffImage(got, want)
	return diff, mismatched, nil
}

// AssertGolden compares got against testdata/golden/<name>.png and fails t on mismatch,
// writing the actual and diff PNGs into a test temporary directory for inspection.
func AssertGolden(t testing.TB, name string, got image.Image) {
	t.Helper()
	AssertGoldenAt(t, GoldenPath(DefaultGoldenDir, name), got)
}

// AssertGoldenAt compares got against goldenPath and fails t on mismatch.
func AssertGoldenAt(t testing.TB, goldenPath string, got image.Image) {
	t.Helper()
	diff, mismatched, err := CompareGolden(got, goldenPath, ShouldUpdateGolden())
	if err != nil {
		t.Fatal(err)
	}
	if mismatched == 0 {
		return
	}
	outDir := t.TempDir()
	base := strings.TrimSuffix(filepath.Base(goldenPath), ".png")
	gotPath := filepath.Join(outDir, base+".got.png")
	diffPath := filepath.Join(outDir, base+".diff.png")
	_ = WritePNG(gotPath, got)
	_ = WritePNG(diffPath, diff)
	t.Fatalf("golden mismatch for %s: %d mismatched pixel(s) (got=%s diff=%s; re-run with -update-golden or UPDATE_GOLDEN=1 if intentional)",
		goldenPath, mismatched, gotPath, diffPath)
}

// AssertOpsGolden rasterizes ops with RenderOps and asserts the resulting image
// matches testdata/golden/<name>.png.
func AssertOpsGolden(t testing.TB, name string, width, height int, ops []core.DrawOp, opts ...RenderOptions) {
	t.Helper()
	img := RenderOps(width, height, ops, opts...)
	AssertGolden(t, name, img)
}

type affine struct {
	a, b, c, d, e, f float64
}

func identityAffine() affine {
	return affine{a: 1, d: 1}
}

func (m affine) mul(n affine) affine {
	return affine{
		a: m.a*n.a + m.c*n.b,
		b: m.b*n.a + m.d*n.b,
		c: m.a*n.c + m.c*n.d,
		d: m.b*n.c + m.d*n.d,
		e: m.a*n.e + m.c*n.f + m.e,
		f: m.b*n.e + m.d*n.f + m.f,
	}
}

func (m affine) apply(x, y float64) (float64, float64) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}

func (m affine) invert() (affine, bool) {
	det := m.a*m.d - m.b*m.c
	if math.Abs(det) < 1e-12 {
		return affine{}, false
	}
	invDet := 1 / det
	return affine{
		a: m.d * invDet,
		b: -m.b * invDet,
		c: -m.c * invDet,
		d: m.a * invDet,
		e: (m.c*m.f - m.d*m.e) * invDet,
		f: (m.b*m.e - m.a*m.f) * invDet,
	}, true
}

func transformedBounds(m affine, x, y, w, h float64, maxW, maxH int) (minX, minY, maxX, maxY int, ok bool) {
	x0, y0 := m.apply(x, y)
	x1, y1 := m.apply(x+w, y)
	x2, y2 := m.apply(x, y+h)
	x3, y3 := m.apply(x+w, y+h)

	loX := min(min(x0, x1), min(x2, x3))
	hiX := max(max(x0, x1), max(x2, x3))
	loY := min(min(y0, y1), min(y2, y3))
	hiY := max(max(y0, y1), max(y2, y3))

	minX = max(0, int(math.Floor(loX)))
	minY = max(0, int(math.Floor(loY)))
	maxX = min(maxW, int(math.Ceil(hiX)))
	maxY = min(maxH, int(math.Ceil(hiY)))
	return minX, minY, maxX, maxY, minX < maxX && minY < maxY
}

func normalizeRect(x, y, w, h float64) (float64, float64, float64, float64) {
	if w < 0 {
		x += w
		w = -w
	}
	if h < 0 {
		y += h
		h = -h
	}
	return x, y, w, h
}

func fillTransformedRect(dst *image.RGBA, m affine, x, y, w, h, sr, sg, sb, sa float64) {
	x, y, w, h = normalizeRect(x, y, w, h)
	if w <= 0 || h <= 0 {
		return
	}
	inv, ok := m.invert()
	if !ok {
		return
	}
	b := dst.Bounds()
	minX, minY, maxX, maxY, visible := transformedBounds(m, x, y, w, h, b.Dx(), b.Dy())
	if !visible {
		return
	}
	for py := minY; py < maxY; py++ {
		cy := float64(py) + 0.5
		for px := minX; px < maxX; px++ {
			lx, ly := inv.apply(float64(px)+0.5, cy)
			if lx >= x && lx < x+w && ly >= y && ly < y+h {
				blendOver(dst, px, py, sr, sg, sb, sa)
			}
		}
	}
}

func strokeTransformedRect(dst *image.RGBA, m affine, x, y, w, h, lineWidth, sr, sg, sb, sa float64) {
	x, y, w, h = normalizeRect(x, y, w, h)
	if (w <= 0 && h <= 0) || lineWidth <= 0 {
		return
	}
	inv, ok := m.invert()
	if !ok {
		return
	}
	half := lineWidth / 2
	ox, oy, ow, oh := x-half, y-half, w+lineWidth, h+lineWidth
	ix0, iy0, ix1, iy1 := x+half, y+half, x+w-half, y+h-half
	hasHole := ix0 < ix1 && iy0 < iy1

	b := dst.Bounds()
	minX, minY, maxX, maxY, visible := transformedBounds(m, ox, oy, ow, oh, b.Dx(), b.Dy())
	if !visible {
		return
	}
	for py := minY; py < maxY; py++ {
		cy := float64(py) + 0.5
		for px := minX; px < maxX; px++ {
			lx, ly := inv.apply(float64(px)+0.5, cy)
			if lx < ox || lx >= ox+ow || ly < oy || ly >= oy+oh {
				continue
			}
			if hasHole && lx > ix0 && lx < ix1 && ly > iy0 && ly < iy1 {
				continue
			}
			blendOver(dst, px, py, sr, sg, sb, sa)
		}
	}
}

func clearTransformedRect(dst *image.RGBA, m affine, x, y, w, h float64) {
	x, y, w, h = normalizeRect(x, y, w, h)
	if w <= 0 || h <= 0 {
		return
	}
	inv, ok := m.invert()
	if !ok {
		return
	}
	b := dst.Bounds()
	minX, minY, maxX, maxY, visible := transformedBounds(m, x, y, w, h, b.Dx(), b.Dy())
	if !visible {
		return
	}
	for py := minY; py < maxY; py++ {
		cy := float64(py) + 0.5
		for px := minX; px < maxX; px++ {
			lx, ly := inv.apply(float64(px)+0.5, cy)
			if lx >= x && lx < x+w && ly >= y && ly < y+h {
				off := dst.PixOffset(px, py)
				dst.Pix[off] = 0
				dst.Pix[off+1] = 0
				dst.Pix[off+2] = 0
				dst.Pix[off+3] = 0
			}
		}
	}
}

func drawTransformedImage(dst *image.RGBA, m affine, src image.Image, sx, sy, sw, sh, dx, dy, dw, dh, alpha float64, tinted bool, tint core.Color) {
	if sw == 0 || sh == 0 || dw == 0 || dh == 0 {
		return
	}
	inv, ok := m.invert()
	if !ok {
		return
	}
	db := dst.Bounds()
	nx, ny, nw, nh := normalizeRect(dx, dy, dw, dh)
	minX, minY, maxX, maxY, visible := transformedBounds(m, nx, ny, nw, nh, db.Dx(), db.Dy())
	if !visible {
		return
	}
	var tr, tg, tb, ta float64 = 1, 1, 1, 1
	if tinted && tint != 0 && tint != core.RGB(0xffffff) {
		tr = float64(tint>>24&0xff) / 255
		tg = float64(tint>>16&0xff) / 255
		tb = float64(tint>>8&0xff) / 255
		ta = float64(tint&0xff) / 255
		if ta <= 0 {
			return
		}
	}
	sb := src.Bounds()
	for py := minY; py < maxY; py++ {
		cy := float64(py) + 0.5
		for px := minX; px < maxX; px++ {
			lx, ly := inv.apply(float64(px)+0.5, cy)
			if lx < nx || lx >= nx+nw || ly < ny || ly >= ny+nh {
				continue
			}
			u := (lx - dx) / dw
			v := (ly - dy) / dh
			ix := sb.Min.X + int(math.Floor(sx+u*sw))
			iy := sb.Min.Y + int(math.Floor(sy+v*sh))
			ix = min(max(ix, sb.Min.X), sb.Max.X-1)
			iy = min(max(iy, sb.Min.Y), sb.Max.Y-1)

			r16, g16, b16, a16 := src.At(ix, iy).RGBA()
			if a16 == 0 {
				continue
			}
			effA := alpha * ta
			sr := (float64(r16) / 65535) * tr * effA
			sg := (float64(g16) / 65535) * tg * effA
			sbVal := (float64(b16) / 65535) * tb * effA
			sa := (float64(a16) / 65535) * effA
			blendOver(dst, px, py, sr, sg, sbVal, sa)
		}
	}
}

func premulFromColor(c core.Color, globalAlpha float64) (r, g, b, a float64) {
	if c == core.Transparent {
		return 0, 0, 0, 0
	}
	a = (float64(c&0xff) / 255) * globalAlpha
	r = (float64(c>>24&0xff) / 255) * a
	g = (float64(c>>16&0xff) / 255) * a
	b = (float64(c>>8&0xff) / 255) * a
	return r, g, b, a
}

func blendOver(dst *image.RGBA, x, y int, sr, sg, sb, sa float64) {
	off := dst.PixOffset(x, y)
	if sa >= 1 {
		dst.Pix[off] = toByte(sr)
		dst.Pix[off+1] = toByte(sg)
		dst.Pix[off+2] = toByte(sb)
		dst.Pix[off+3] = 255
		return
	}
	invA := 1 - sa
	dr := float64(dst.Pix[off]) / 255
	dg := float64(dst.Pix[off+1]) / 255
	db := float64(dst.Pix[off+2]) / 255
	da := float64(dst.Pix[off+3]) / 255

	dst.Pix[off] = toByte(sr + dr*invA)
	dst.Pix[off+1] = toByte(sg + dg*invA)
	dst.Pix[off+2] = toByte(sb + db*invA)
	dst.Pix[off+3] = toByte(sa + da*invA)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func toByte(v float64) uint8 {
	return uint8(math.Round(clamp01(v) * 255))
}

type pathPt struct {
	x, y float64
}

type pathSub struct {
	pts    []pathPt
	closed bool
}

type polyEdge struct {
	x0, y0, x1, y1 float64
}

type scanCrossing struct {
	x   float64
	dir int
}

// fillPolygons rasterizes all polygon subpaths together with scanline winding (nonzero or even-odd), blending each covered pixel once so adjacent tiles sharing an edge neither crack nor double-blend.
func fillPolygons(dst *image.RGBA, subs []pathSub, evenOdd bool, sr, sg, sb, sa float64) {
	b := dst.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	var edges []polyEdge
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, sub := range subs {
		n := len(sub.pts)
		if n < 3 {
			continue
		}
		for i := range n {
			p0 := sub.pts[i]
			p1 := sub.pts[(i+1)%n]
			if p0.y == p1.y {
				continue
			}
			edges = append(edges, polyEdge{x0: p0.x, y0: p0.y, x1: p1.x, y1: p1.y})
			if p0.y < minY {
				minY = p0.y
			}
			if p1.y < minY {
				minY = p1.y
			}
			if p0.y > maxY {
				maxY = p0.y
			}
			if p1.y > maxY {
				maxY = p1.y
			}
		}
	}
	if len(edges) == 0 {
		return
	}
	yStart := max(0, int(math.Floor(minY)))
	yEnd := min(h, int(math.Ceil(maxY)))
	var crossings []scanCrossing
	for py := yStart; py < yEnd; py++ {
		cy := float64(py) + 0.5
		crossings = crossings[:0]
		for _, e := range edges {
			if e.y0 <= cy && e.y1 > cy {
				ix := e.x0 + (cy-e.y0)*(e.x1-e.x0)/(e.y1-e.y0)
				crossings = append(crossings, scanCrossing{x: ix, dir: 1})
			} else if e.y1 <= cy && e.y0 > cy {
				ix := e.x0 + (cy-e.y0)*(e.x1-e.x0)/(e.y1-e.y0)
				crossings = append(crossings, scanCrossing{x: ix, dir: -1})
			}
		}
		if len(crossings) < 2 {
			continue
		}
		slices.SortFunc(crossings, func(a, b scanCrossing) int {
			return cmp.Compare(a.x, b.x)
		})
		winding := 0
		inside := false
		spanStart := 0.0
		for _, c := range crossings {
			winding += c.dir
			nowInside := winding != 0
			if evenOdd {
				nowInside = (winding & 1) != 0
			}
			if !inside && nowInside {
				spanStart = c.x
				inside = true
			} else if inside && !nowInside {
				px0 := max(0, int(math.Ceil(spanStart-0.5)))
				px1 := min(w, int(math.Ceil(c.x-0.5)))
				for px := px0; px < px1; px++ {
					blendOver(dst, px, py, sr, sg, sb, sa)
				}
				inside = false
			}
		}
	}
}

func strokePolygons(dst *image.RGBA, subs []pathSub, lineWidth, sr, sg, sb, sa float64) {
	b := dst.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || lineWidth <= 0 {
		return
	}
	half := lineWidth / 2
	halfSq := half * half
	for _, sub := range subs {
		n := len(sub.pts)
		if n < 2 {
			continue
		}
		segCount := n - 1
		if sub.closed {
			segCount = n
		}
		for i := range segCount {
			p0 := sub.pts[i]
			p1 := sub.pts[(i+1)%n]
			minX := max(0, int(math.Floor(min(p0.x, p1.x)-half)))
			maxX := min(w, int(math.Ceil(max(p0.x, p1.x)+half)))
			minY := max(0, int(math.Floor(min(p0.y, p1.y)-half)))
			maxY := min(h, int(math.Ceil(max(p0.y, p1.y)+half)))
			dx, dy := p1.x-p0.x, p1.y-p0.y
			lenSq := dx*dx + dy*dy
			for py := minY; py < maxY; py++ {
				cy := float64(py) + 0.5
				for px := minX; px < maxX; px++ {
					cx := float64(px) + 0.5
					t := 0.0
					if lenSq > 0 {
						t = clamp01(((cx-p0.x)*dx + (cy-p0.y)*dy) / lenSq)
					}
					qx, qy := p0.x+t*dx, p0.y+t*dy
					ddx, ddy := cx-qx, cy-qy
					if ddx*ddx+ddy*ddy <= halfSq {
						blendOver(dst, px, py, sr, sg, sb, sa)
					}
				}
			}
		}
	}
}
