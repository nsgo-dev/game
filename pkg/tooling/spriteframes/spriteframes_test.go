package spriteframes_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/spriteframes"
)

func frame(w, h int, points map[image.Point]color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for p, c := range points {
		img.SetNRGBA(p.X, p.Y, c)
	}
	return img
}

func TestChromaKeyAndHoleFill(t *testing.T) {
	key := color.NRGBA{R: 0, G: 255, B: 0, A: 255}
	red := color.NRGBA{R: 255, G: 0, B: 0, A: 255}
	in := frame(3, 3, map[image.Point]color.NRGBA{{0, 0}: red, {1, 0}: red, {2, 0}: red, {0, 1}: red, {2, 1}: red, {0, 2}: red, {1, 2}: red, {2, 2}: red})
	in.SetNRGBA(1, 1, key)
	out, err := spriteframes.ChromaKey([]*image.NRGBA{in}, spriteframes.KeyOptions{Color: key, FillHoles: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := out[0].NRGBAAt(1, 1); got != red {
		t.Fatalf("hole = %#v, want %#v", got, red)
	}
}

func TestLoopSeamDropsDuplicateAndRotates(t *testing.T) {
	a := frame(1, 1, map[image.Point]color.NRGBA{{0, 0}: {R: 0}})
	b := frame(1, 1, map[image.Point]color.NRGBA{{0, 0}: {R: 255}})
	c := frame(1, 1, map[image.Point]color.NRGBA{{0, 0}: {R: 128}})
	out, err := spriteframes.LoopSeam([]*image.NRGBA{a, b, c, a})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 || out[0].NRGBAAt(0, 0).R != 128 {
		t.Fatalf("seam order starts %#v", out[0].NRGBAAt(0, 0))
	}
}

func TestAlignBottomCenter(t *testing.T) {
	a := frame(4, 4, map[image.Point]color.NRGBA{{0, 0}: {A: 255}, {1, 1}: {A: 255}})
	b := frame(4, 4, map[image.Point]color.NRGBA{{2, 1}: {A: 255}, {3, 2}: {A: 255}})
	out, bounds, err := spriteframes.Align([]*image.NRGBA{a, b}, spriteframes.AnchorBottomCenter)
	if err != nil {
		t.Fatal(err)
	}
	if bounds.Dx() != 2 || bounds.Dy() != 2 {
		t.Fatalf("bounds %v", bounds)
	}
	if out[0].Bounds() != out[1].Bounds() {
		t.Fatal("frames differ in size")
	}
	if spriteframes.AnchorBottomCenter != "bottom-center" {
		t.Fatal("anchor constant changed")
	}
}
