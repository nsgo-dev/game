package sprite

import "github.com/nsgo-dev/nsgo/core"

// Insets are the margins kept unscaled around the edges of a nine-slice frame, in the frame's points.
type Insets struct {
	Left, Top, Right, Bottom float32
}

// NineSlice draws a frame into any rectangle while keeping its four corners unscaled and stretching its edges and center to fill the space.
type NineSlice struct {
	// Image is the packed image that holds Frame.
	Image core.ImageSource
	// Frame is the part of Image to slice.
	Frame Frame
	// Insets are the margins on each side of Frame that stay unscaled.
	Insets Insets
	// TileCenter repeats the center region at its natural size instead of stretching it.
	TileCenter bool
}

// Draw draws the nine-slice into d across the rectangle x, y, width, height, skipping any slice whose source or target dimension is zero.
//
// When width is smaller than Left+Right or height is smaller than Top+Bottom, the margins shrink proportionally to fit and the middle spans are omitted.
func (n *NineSlice) Draw(d *core.Drawing, x, y, width, height float32) {
	f := n.Frame
	if f.W <= 0 || f.H <= 0 || width <= 0 || height <= 0 {
		return
	}

	sl, sr, scw := splitSpan(n.Insets.Left, n.Insets.Right, f.W)
	st, sb, sch := splitSpan(n.Insets.Top, n.Insets.Bottom, f.H)

	dl, dr, dcw := splitSpan(sl, sr, width)
	dt, db, dch := splitSpan(st, sb, height)

	sx := [3]float32{f.X, f.X + sl, f.X + f.W - sr}
	sw := [3]float32{sl, scw, sr}
	dx := [3]float32{x, x + dl, x + width - dr}
	dw := [3]float32{dl, dcw, dr}

	sy := [3]float32{f.Y, f.Y + st, f.Y + f.H - sb}
	sh := [3]float32{st, sch, sb}
	dy := [3]float32{y, y + dt, y + height - db}
	dh := [3]float32{dt, dch, db}

	for row := range 3 {
		if sh[row] <= 0 || dh[row] <= 0 {
			continue
		}
		for col := range 3 {
			if sw[col] <= 0 || dw[col] <= 0 {
				continue
			}
			if n.TileCenter && row == 1 && col == 1 {
				tileRect(d, n.Image, sx[1], sy[1], sw[1], sh[1], dx[1], dy[1], dw[1], dh[1])
				continue
			}
			d.DrawImageFrom(n.Image, sx[col], sy[row], sw[col], sh[row], dx[col], dy[row], dw[col], dh[row])
		}
	}
}

// splitSpan clamps margins a and b to non-negative values, scales them down proportionally if their sum exceeds total, and returns the left/top margin, right/bottom margin, and remaining middle span.
func splitSpan(a, b, total float32) (first, second, mid float32) {
	a, b = max(a, 0), max(b, 0)
	sum := a + b
	if sum >= total {
		if sum > total && sum > 0 {
			scale := total / sum
			return a * scale, b * scale, 0
		}
		return a, b, 0
	}
	return a, b, total - sum
}

// tileRect repeats the source rectangle (sx, sy, sw, sh) across the target rectangle (dx, dy, dw, dh), cropping the final column and row to fit.
func tileRect(d *core.Drawing, img core.ImageSource, sx, sy, sw, sh, dx, dy, dw, dh float32) {
	for ty := float32(0); ty < dh; {
		th := min(sh, dh-ty)
		for tx := float32(0); tx < dw; {
			tw := min(sw, dw-tx)
			d.DrawImageFrom(img, sx, sy, tw, th, dx+tx, dy+ty, tw, th)
			nextX := tx + sw
			if nextX <= tx {
				break
			}
			tx = nextX
		}
		nextY := ty + sh
		if nextY <= ty {
			break
		}
		ty = nextY
	}
}
