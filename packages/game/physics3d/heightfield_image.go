package physics3d

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
)

// HeightmapChannel selects which color channel of an image.Image supplies
// normalized height samples in [0, 1] for NewHeightfieldFromImage.
type HeightmapChannel int

const (
	// HeightmapLuminance converts each pixel to 16-bit luminance (matching
	// color.Gray16Model and preserving exact 16-bit grayscale values).
	HeightmapLuminance HeightmapChannel = iota
	// HeightmapRed reads the red channel.
	HeightmapRed
	// HeightmapGreen reads the green channel.
	HeightmapGreen
	// HeightmapBlue reads the blue channel.
	HeightmapBlue
	// HeightmapAlpha reads the alpha channel.
	HeightmapAlpha
)

// HeightfieldImageOptions configures NewHeightfieldFromImage.
type HeightfieldImageOptions struct {
	// CellSize sets the horizontal spacing between adjacent grid vertices in
	// world units. If WorldWidth > 0, CellSize is derived as
	// WorldWidth / float32(cols-1) instead. Defaults to 1.0 when both are <= 0.
	CellSize float32
	// WorldWidth optionally sets the total X span of the heightfield in world
	// units (overriding CellSize).
	WorldWidth float32
	// WorldDepth optionally sets the total Z span of the heightfield in world
	// units. When WorldDepth > 0 and Rows == 0, Rows is derived from
	// round(WorldDepth/CellSize)+1 (at least 2) so the terrain spans WorldDepth
	// using HeightfieldCollider's square CellSize.
	WorldDepth float32
	// Cols and Rows optionally resample the image to a target vertex grid
	// resolution (0 keeps the image's width/height; when set, must be >= 2).
	Cols int
	Rows int
	// Bilinear enables bilinear filtering when sampling the image at fractional
	// pixel coordinates (used when Cols/Rows differ from the source image
	// dimensions). When false, nearest-neighbor sampling is used.
	Bilinear bool
	// MinHeight and MaxHeight map normalized pixel values [0, 1] linearly to
	// [MinHeight, MaxHeight]. When both are 0, MaxHeight defaults to 1.0.
	MinHeight float32
	MaxHeight float32
	// Origin is the world-space position of vertex (0, 0) at height 0.
	Origin Vec3
	// Channel selects how pixel colors map to normalized height (defaults to
	// HeightmapLuminance).
	Channel HeightmapChannel
	// FlipZ reverses the image Y -> world Z row order when true.
	FlipZ bool
	// Tag, Friction, Restitution, Layer, and Mask populate the corresponding
	// HeightfieldCollider fields. Friction <= 0 defaults to 0.5.
	Tag         string
	Friction    float32
	Restitution float32
	Layer       LayerMask
	Mask        LayerMask
}

// NewHeightfieldFromImage builds a static HeightfieldCollider from any standard
// Go image.Image (grayscale 8-bit, 16-bit Gray16, RGBA, NRGBA, RGBA64, etc.).
func NewHeightfieldFromImage(img image.Image, opts HeightfieldImageOptions) (*HeightfieldCollider, error) {
	if img == nil {
		return nil, errors.New("physics3d: nil heightmap image")
	}
	b := img.Bounds()
	imgW, imgH := b.Dx(), b.Dy()
	if imgW <= 0 || imgH <= 0 {
		return nil, fmt.Errorf("physics3d: empty heightmap image (%dx%d)", imgW, imgH)
	}
	if opts.Channel < HeightmapLuminance || opts.Channel > HeightmapAlpha {
		return nil, fmt.Errorf("physics3d: invalid heightmap channel %d", opts.Channel)
	}
	if !isFinite32(opts.CellSize) || opts.CellSize < 0 {
		return nil, fmt.Errorf("physics3d: invalid heightmap CellSize %v", opts.CellSize)
	}
	if !isFinite32(opts.WorldWidth) || opts.WorldWidth < 0 {
		return nil, fmt.Errorf("physics3d: invalid heightmap WorldWidth %v", opts.WorldWidth)
	}
	if !isFinite32(opts.WorldDepth) || opts.WorldDepth < 0 {
		return nil, fmt.Errorf("physics3d: invalid heightmap WorldDepth %v", opts.WorldDepth)
	}
	if !isFinite32(opts.MinHeight) || !isFinite32(opts.MaxHeight) {
		return nil, errors.New("physics3d: heightmap MinHeight and MaxHeight must be finite")
	}
	if !isFiniteVec(opts.Origin) {
		return nil, errors.New("physics3d: heightmap Origin must be finite")
	}
	if opts.Cols < 0 || opts.Rows < 0 {
		return nil, fmt.Errorf("physics3d: heightmap Cols (%d) and Rows (%d) must be non-negative", opts.Cols, opts.Rows)
	}
	if (opts.Cols > 0 && opts.Cols < 2) || (opts.Rows > 0 && opts.Rows < 2) {
		return nil, fmt.Errorf("physics3d: heightmap Cols (%d) and Rows (%d) must be >= 2 when specified", opts.Cols, opts.Rows)
	}

	cols := opts.Cols
	if cols == 0 {
		cols = imgW
	}
	if cols < 2 {
		return nil, fmt.Errorf("physics3d: heightmap requires at least 2 columns, got %d", cols)
	}

	cellSize := opts.CellSize
	if opts.WorldWidth > 0 {
		cellSize = opts.WorldWidth / float32(cols-1)
	} else if cellSize <= 0 {
		cellSize = 1.0
	}

	rows := opts.Rows
	if rows == 0 {
		if opts.WorldDepth > 0 {
			rows = max(2, int(math.Round(float64(opts.WorldDepth)/float64(cellSize)))+1)
		} else {
			rows = imgH
		}
	}
	if rows < 2 {
		return nil, fmt.Errorf("physics3d: heightmap requires at least 2 rows, got %d", rows)
	}

	minH := opts.MinHeight
	maxH := opts.MaxHeight
	if minH == 0 && maxH == 0 {
		maxH = 1.0
	}
	spanH := float64(maxH - minH)

	heights := make([]float32, cols*rows)
	direct := cols == imgW && rows == imgH
	for cz := 0; cz < rows; cz++ {
		rz := cz
		if opts.FlipZ {
			rz = (rows - 1) - cz
		}
		for cx := 0; cx < cols; cx++ {
			var norm float64
			if direct {
				norm = sampleImageChannel(img, b.Min.X+cx, b.Min.Y+rz, opts.Channel)
			} else {
				u := float64(cx) * float64(imgW-1) / float64(cols-1)
				v := float64(rz) * float64(imgH-1) / float64(rows-1)
				if opts.Bilinear {
					norm = sampleImageBilinear(img, b, u, v, opts.Channel)
				} else {
					px := b.Min.X + clampInt(int(math.Round(u)), 0, imgW-1)
					py := b.Min.Y + clampInt(int(math.Round(v)), 0, imgH-1)
					norm = sampleImageChannel(img, px, py, opts.Channel)
				}
			}
			heights[cz*cols+cx] = float32(float64(minH) + norm*spanH)
		}
	}

	hf := NewHeightfieldCollider(cols, rows, cellSize, opts.Origin, heights)
	hf.Tag = opts.Tag
	if opts.Friction > 0 {
		hf.Friction = opts.Friction
	}
	hf.Restitution = clamp32(opts.Restitution, 0, 1)
	hf.Layer = opts.Layer
	hf.Mask = opts.Mask
	return hf, nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func sampleImageBilinear(img image.Image, b image.Rectangle, u, v float64, ch HeightmapChannel) float64 {
	maxX := b.Dx() - 1
	maxY := b.Dy() - 1
	x0 := clampInt(int(math.Floor(u)), 0, maxX)
	y0 := clampInt(int(math.Floor(v)), 0, maxY)
	x1 := clampInt(x0+1, 0, maxX)
	y1 := clampInt(y0+1, 0, maxY)
	tx := u - float64(x0)
	ty := v - float64(y0)
	if tx < 0 {
		tx = 0
	} else if tx > 1 {
		tx = 1
	}
	if ty < 0 {
		ty = 0
	} else if ty > 1 {
		ty = 1
	}

	s00 := sampleImageChannel(img, b.Min.X+x0, b.Min.Y+y0, ch)
	s10 := sampleImageChannel(img, b.Min.X+x1, b.Min.Y+y0, ch)
	s01 := sampleImageChannel(img, b.Min.X+x0, b.Min.Y+y1, ch)
	s11 := sampleImageChannel(img, b.Min.X+x1, b.Min.Y+y1, ch)

	top := s00 + (s10-s00)*tx
	bot := s01 + (s11-s01)*tx
	return top + (bot-top)*ty
}

func sampleImageChannel(img image.Image, x, y int, ch HeightmapChannel) float64 {
	// Fast, exact paths for common Go image types (avoiding premultiplication
	// distortion for NRGBA / NRGBA64).
	switch m := img.(type) {
	case *image.Gray:
		if ch == HeightmapAlpha {
			return 1.0
		}
		return float64(m.GrayAt(x, y).Y) / 255.0
	case *image.Gray16:
		if ch == HeightmapAlpha {
			return 1.0
		}
		return float64(m.Gray16At(x, y).Y) / 65535.0
	case *image.NRGBA:
		c := m.NRGBAAt(x, y)
		switch ch {
		case HeightmapRed:
			return float64(c.R) / 255.0
		case HeightmapGreen:
			return float64(c.G) / 255.0
		case HeightmapBlue:
			return float64(c.B) / 255.0
		case HeightmapAlpha:
			return float64(c.A) / 255.0
		default:
			g := color.Gray16Model.Convert(c).(color.Gray16)
			return float64(g.Y) / 65535.0
		}
	case *image.NRGBA64:
		c := m.NRGBA64At(x, y)
		switch ch {
		case HeightmapRed:
			return float64(c.R) / 65535.0
		case HeightmapGreen:
			return float64(c.G) / 65535.0
		case HeightmapBlue:
			return float64(c.B) / 65535.0
		case HeightmapAlpha:
			return float64(c.A) / 65535.0
		default:
			g := color.Gray16Model.Convert(c).(color.Gray16)
			return float64(g.Y) / 65535.0
		}
	}

	c := img.At(x, y)
	if ch == HeightmapLuminance {
		g := color.Gray16Model.Convert(c).(color.Gray16)
		return float64(g.Y) / 65535.0
	}
	r, g, b, a := c.RGBA()
	switch ch {
	case HeightmapRed:
		return float64(r) / 65535.0
	case HeightmapGreen:
		return float64(g) / 65535.0
	case HeightmapBlue:
		return float64(b) / 65535.0
	case HeightmapAlpha:
		return float64(a) / 65535.0
	default:
		gy := color.Gray16Model.Convert(c).(color.Gray16)
		return float64(gy.Y) / 65535.0
	}
}
