package pack

import (
	"image"
	"math"
)

// bin packs non-overlapping rectangles into a fixed width and height using the MaxRects Best Short Side Fit heuristic without rotation.
type bin struct {
	width, height int
	free          []image.Rectangle
}

func newBin(width, height int) *bin {
	return &bin{
		width:  width,
		height: height,
		free:   []image.Rectangle{image.Rect(0, 0, width, height)},
	}
}

// insert places a w by h rectangle in the bin and returns its top-left corner, or false when no free rectangle fits it.
func (b *bin) insert(w, h int) (image.Point, bool) {
	bestShort := math.MaxInt
	bestLong := math.MaxInt
	bestPos := image.Point{}
	found := false

	for _, r := range b.free {
		if r.Dx() < w || r.Dy() < h {
			continue
		}
		remX := r.Dx() - w
		remY := r.Dy() - h
		shortFit := min(remX, remY)
		longFit := max(remX, remY)
		if !found ||
			shortFit < bestShort ||
			(shortFit == bestShort && longFit < bestLong) ||
			(shortFit == bestShort && longFit == bestLong && r.Min.Y < bestPos.Y) ||
			(shortFit == bestShort && longFit == bestLong && r.Min.Y == bestPos.Y && r.Min.X < bestPos.X) {
			bestShort = shortFit
			bestLong = longFit
			bestPos = r.Min
			found = true
		}
	}
	if !found {
		return image.Point{}, false
	}

	placed := image.Rect(bestPos.X, bestPos.Y, bestPos.X+w, bestPos.Y+h)
	next := make([]image.Rectangle, 0, len(b.free)+4)
	for _, f := range b.free {
		if !f.Overlaps(placed) {
			next = append(next, f)
			continue
		}
		if placed.Min.X > f.Min.X {
			next = append(next, image.Rect(f.Min.X, f.Min.Y, placed.Min.X, f.Max.Y))
		}
		if placed.Max.X < f.Max.X {
			next = append(next, image.Rect(placed.Max.X, f.Min.Y, f.Max.X, f.Max.Y))
		}
		if placed.Min.Y > f.Min.Y {
			next = append(next, image.Rect(f.Min.X, f.Min.Y, f.Max.X, placed.Min.Y))
		}
		if placed.Max.Y < f.Max.Y {
			next = append(next, image.Rect(f.Min.X, placed.Max.Y, f.Max.X, f.Max.Y))
		}
	}
	b.free = pruneFree(next)
	return bestPos, true
}

// pruneFree drops every rectangle contained in another free rectangle, keeping the first copy of any equal pair.
func pruneFree(rects []image.Rectangle) []image.Rectangle {
	out := rects[:0]
	for i, r := range rects {
		contained := false
		for j, o := range rects {
			if i == j {
				continue
			}
			if r == o {
				if j < i {
					contained = true
					break
				}
				continue
			}
			if r.In(o) {
				contained = true
				break
			}
		}
		if !contained {
			out = append(out, r)
		}
	}
	return out
}
