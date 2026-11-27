package fog

import (
	"math"
	"slices"
	"testing"
)

// fov returns every cell a source at (col, row) with radius r sees on f, as a fresh bitset.
func fov(f *Fog, col, row, r int) []uint64 {
	vis := make([]uint64, wordsFor(f.width*f.height))
	f.see(Source{Col: col, Row: row, Radius: r}, vis)
	return vis
}

func TestShadowcastMatchesDiscOnOpenGround(t *testing.T) {
	const size = 25
	for r := range 12 {
		open := New(size, size, 1, 1)
		disc := fov(open, 12, 12, r)

		// One opaque cell in a far corner switches see to shadowcasting without blocking anything in range.
		walled := New(size, size, 1, 1)
		walled.SetOpaque(0, 0, true)
		cast := fov(walled, 12, 12, r)
		clearBit(cast, 0)
		if !slices.Equal(disc, cast) {
			t.Fatalf("radius %d: shadowcast differs from the disc", r)
		}
	}
}

func TestHugeRadiusIsClamped(t *testing.T) {
	f := New(4, 4, 1, 1)
	f.SetOpaque(3, 3, true)
	vis := fov(f, 0, 0, math.MaxInt)
	n := 0
	for i := range 16 {
		if bit(vis, i) {
			n++
		}
	}
	if n != 16 {
		t.Fatalf("a huge radius saw %d of 16 cells", n)
	}
}

// FuzzShadowcastSymmetric checks that on any map, one floor cell sees another exactly when the other sees it.
func FuzzShadowcastSymmetric(f *testing.F) {
	f.Add([]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x0f, 0xf0, 0x55, 0xaa}, uint8(3), uint8(40), uint8(9))
	f.Add([]byte{0xff, 0x00, 0xff, 0x00, 0x81, 0x18}, uint8(0), uint8(255), uint8(16))
	f.Add([]byte{}, uint8(10), uint8(200), uint8(4))
	f.Fuzz(func(t *testing.T, walls []byte, a, b, r uint8) {
		const size = 16
		m := New(size, size, 1, 1)
		for i := range size * size {
			if i/8 < len(walls) && walls[i/8]&(1<<(i%8)) != 0 {
				m.SetOpaque(i%size, i/size, true)
			}
		}
		ax, ay := int(a)%size, int(a)/size
		bx, by := int(b)%size, int(b)/size
		if m.Opaque(ax, ay) || m.Opaque(bx, by) {
			return
		}
		radius := int(r % 24)
		fromA := bit(fov(m, ax, ay, radius), m.index(bx, by))
		fromB := bit(fov(m, bx, by, radius), m.index(ax, ay))
		if fromA != fromB {
			t.Fatalf("(%d,%d) sees (%d,%d): %v, but the reverse is %v (radius %d)", ax, ay, bx, by, fromA, fromB, radius)
		}
	})
}
