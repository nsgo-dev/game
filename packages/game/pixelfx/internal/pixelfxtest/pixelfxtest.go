// Package pixelfxtest checks the Go port of pixelfx against pixelfx itself: it renders an effect the way tools/pixelfxref renders it with pixelfx at the pinned commit, and compares the frames with the reference PNGs that tool wrote. Every category's tests call CheckEffect for each of its effects.
//
// The cases and frames, the same on both sides:
//
//   - "default": the effect with its default options, as a sprite sheet renders it (pixelfx.EachFrame), at the frames 25%, 50% and 75% of the way through it.
//   - "flip": the same with Flip set.
//   - "act", for an effect a character acts out: the performance (pixelfx.EachActFrame, 60 fps), at the first frame whose time reaches 25%, 50% and 75% of the way from the first pose to the end (two loops for a loop).
//
// A reference is <case>-<frame>.png in the effect's testdata/ref/<name> directory, frame the 0-based frame index in three digits.
package pixelfxtest

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// The one tolerance every parity test uses. A pixel matches its reference when each of its RGBA channels is within MaxChannelDelta of the reference's; a frame matches when at most MaxDifferingShare of its pixels do not. JavaScript and Go compute the same float64 operations in the same order, but their sin, cos, atan2, exp and pow may differ in the last bit, and arm64 may fuse a multiply and an add; such a difference moves the odd pixel across a rounding edge, never a whole shape.
const (
	MaxChannelDelta   = 2
	MaxDifferingShare = 0.005
)

// Samples are the share of the way through an effect each sampled frame is.
var Samples = []float64{0.25, 0.5, 0.75}

// ActFPS is the frame rate the act case is rendered at.
const ActFPS = 60

// SheetSamples returns the frames sampled from an effect's n sheet frames.
func SheetSamples(n int) []int {
	var out []int
	for _, f := range Samples {
		i := min(n-1, int(pixelfx.Round(float64(n)*f)))
		if !slices.Contains(out, i) {
			out = append(out, i)
		}
	}
	return out
}

// sheetCount returns how many frames a sprite sheet of e has.
func sheetCount(e *pixelfx.Effect) int {
	m := &e.Meta
	n := int(pixelfx.Round(m.Duration * float64(m.FPS)))
	if !m.Loop {
		n++
	}
	return max(1, n)
}

// Frame is one rendered frame: its case and index, its size, and its RGBA bytes (straight alpha).
type Frame struct {
	Case  string
	Index int
	W, H  int
	Pix   []byte
}

// Name returns the frame's reference file name.
func (f Frame) Name() string { return fmt.Sprintf("%s-%03d.png", f.Case, f.Index) }

// Render renders the frames of e that CheckEffect compares, in case order.
func Render(e *pixelfx.Effect) []Frame {
	var out []Frame
	for _, c := range []struct {
		name string
		o    pixelfx.Options
	}{{"default", pixelfx.Options{}}, {"flip", pixelfx.Options{Flip: true}}} {
		want := SheetSamples(sheetCount(e))
		pixelfx.EachFrame(e, c.o, 0, func(i int, f *pixelfx.Frame) {
			if slices.Contains(want, i) {
				out = append(out, Frame{Case: c.name, Index: i, W: f.W, H: f.H, Pix: pixelfx.Composite(f, nil, nil, 1)})
			}
		})
	}
	if e.Act != nil {
		from, to := pixelfx.ActSpan(e)
		end := to
		if e.Meta.Loop {
			end = e.Meta.Duration * 2
		}
		targets := make([]float64, len(Samples))
		for i, f := range Samples {
			targets[i] = from + f*(end-from)
		}
		next := 0
		pixelfx.EachActFrame(e, pixelfx.Options{}, ActFPS, func(i int, t, _, _ float64, f *pixelfx.Frame) {
			if next < len(targets) && t >= targets[next] {
				out = append(out, Frame{Case: "act", Index: i, W: f.W, H: f.H, Pix: pixelfx.Composite(f, nil, nil, 1)})
				for next < len(targets) && t >= targets[next] {
					next++
				}
			}
		})
	}
	return out
}

// Diff is how a frame differs from its reference: how many pixels differ by more than MaxChannelDelta, out of how many, the largest channel difference, and where the first differing pixel is.
type Diff struct {
	Differing, Total int
	MaxDelta         int
	FirstX, FirstY   int
}

// OK reports whether the difference is within the tolerance.
func (d Diff) OK() bool { return float64(d.Differing) <= MaxDifferingShare*float64(d.Total) }

// Compare returns how got differs from want, both RGBA bytes of a w x h frame.
func Compare(got, want []byte, w, h int) Diff {
	d := Diff{Total: w * h, FirstX: -1, FirstY: -1}
	for i := 0; i < w*h; i++ {
		worst := 0
		for c := range 4 {
			worst = max(worst, absInt(int(got[i*4+c])-int(want[i*4+c])))
		}
		d.MaxDelta = max(d.MaxDelta, worst)
		if worst > MaxChannelDelta {
			if d.Differing == 0 {
				d.FirstX, d.FirstY = i%w, i/w
			}
			d.Differing++
		}
	}
	return d
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ReadPNG reads a reference PNG as straight-alpha RGBA bytes.
func ReadPNG(path string) (w, h int, pix []byte, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, nil, err
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0, nil, fmt.Errorf("%s: %w", path, err)
	}
	b := img.Bounds()
	w, h = b.Dx(), b.Dy()
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		nrgba = image.NewNRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				nrgba.Set(x, y, img.At(x, y))
			}
		}
	}
	pix = make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		copy(pix[y*w*4:(y+1)*w*4], nrgba.Pix[y*nrgba.Stride:y*nrgba.Stride+w*4])
	}
	return w, h, pix, nil
}

// CheckEffect renders e and compares every frame with its reference in refDir (testdata/ref/<name>), failing t when a reference is missing, the wrong size, or differs beyond the tolerance, or when refDir holds a reference no case renders. Set PIXELFXTEST_OUT to a directory to have the frames that differ written there.
func CheckEffect(t *testing.T, e *pixelfx.Effect, refDir string) {
	t.Helper()
	frames := Render(e)
	seen := map[string]bool{}
	for _, f := range frames {
		seen[f.Name()] = true
		path := filepath.Join(refDir, f.Name())
		w, h, want, err := ReadPNG(path)
		if err != nil {
			t.Errorf("%s: no reference for %s: %v (render it with tools/pixelfxref)", e.ID, f.Name(), err)
			continue
		}
		if w != f.W || h != f.H {
			t.Errorf("%s %s: %dx%d, reference is %dx%d", e.ID, f.Name(), f.W, f.H, w, h)
			continue
		}
		d := Compare(f.Pix, want, w, h)
		t.Logf("%s %s: %d of %d pixels differ by more than %d, largest difference %d", e.ID, f.Name(), d.Differing, d.Total, MaxChannelDelta, d.MaxDelta)
		if !d.OK() {
			t.Errorf("%s %s: %d of %d pixels (%.2f%%) differ by more than %d (largest %d), first at (%d, %d); tolerance is %.1f%%", e.ID, f.Name(), d.Differing, d.Total, 100*float64(d.Differing)/float64(d.Total), MaxChannelDelta, d.MaxDelta, d.FirstX, d.FirstY, 100*MaxDifferingShare)
			dump(t, e, f)
		}
	}
	entries, err := os.ReadDir(refDir)
	if err != nil {
		t.Fatalf("%s: %v", e.ID, err)
	}
	for _, de := range entries {
		if strings.HasSuffix(de.Name(), ".png") && !seen[de.Name()] {
			t.Errorf("%s: reference %s is not one CheckEffect renders; render the references again with tools/pixelfxref", e.ID, de.Name())
		}
	}
}

func dump(t *testing.T, e *pixelfx.Effect, f Frame) {
	dir := os.Getenv("PIXELFXTEST_OUT")
	if dir == "" {
		return
	}
	img := image.NewNRGBA(image.Rect(0, 0, f.W, f.H))
	copy(img.Pix, f.Pix)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Logf("dump: %v", err)
		return
	}
	name := strings.ReplaceAll(e.ID, "/", "_") + "_" + f.Name()
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
		t.Logf("dump: %v", err)
	}
}

// CheckDeterministic renders e twice with the same options, and acted out twice when it has an act, and fails t unless every frame is the same to the byte.
func CheckDeterministic(t *testing.T, e *pixelfx.Effect) {
	t.Helper()
	a, b := Render(e), Render(e)
	if len(a) != len(b) {
		t.Fatalf("%s: %d frames, then %d", e.ID, len(a), len(b))
	}
	for i := range a {
		if a[i].Name() != b[i].Name() || !bytes.Equal(a[i].Pix, b[i].Pix) {
			t.Errorf("%s: %s differs between two renders with the same options", e.ID, a[i].Name())
		}
	}
}

// BenchmarkFrames measures playing e at its frame rate: each iteration updates it by one frame and draws the frame, so the allocations reported are per frame. It starts over when a one-shot ends.
func BenchmarkFrames(b *testing.B, e *pixelfx.Effect) {
	m := &e.Meta
	frame := pixelfx.NewFrame(m.Size[0], m.Size[1], 0, 0)
	o := pixelfx.Options{X: float64(m.Origin[0]), Y: float64(m.Origin[1])}
	p := pixelfx.NewPlayer(e, o)
	dt := 1 / float64(m.FPS)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if p.Done() {
			p = pixelfx.NewPlayer(e, o)
		}
		p.Update(dt)
		frame.Clear()
		p.Draw(frame, pixelfx.LayerBoth)
	}
}

// BenchmarkActFrames measures performing e at ActFPS with the stock character and its target: each iteration updates the performance by one frame and draws it, so the allocations reported are per frame. It starts over when the performance ends.
func BenchmarkActFrames(b *testing.B, e *pixelfx.Effect) {
	rig := pixelfx.NewRig()
	w, h, x, y := pixelfx.ActFrameSize(e, rig)
	frame := pixelfx.NewFrame(w, h, 0, 0)
	o := pixelfx.Options{X: x, Y: y}
	pf := pixelfx.Perform(e, o, rig)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if pf.Done() {
			pf = pixelfx.Perform(e, o, rig)
		}
		pf.Update(1.0 / ActFPS)
		frame.Clear()
		pf.Draw(frame, true, true)
	}
}
