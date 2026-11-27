package ktx2

import (
	"image"
	"image/draw"
	"math"
)

var srgbToLinearLUT [256]float64

func init() {
	for i := range 256 {
		s := float64(i) / 255.0
		if s <= 0.04045 {
			srgbToLinearLUT[i] = s / 12.92
		} else {
			srgbToLinearLUT[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
}

func linearToSRGBByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	var s float64
	if v <= 0.0031308 {
		s = v * 12.92
	} else {
		s = 1.055*math.Pow(v, 1.0/2.4) - 0.055
	}
	return clampByte(int(math.Round(s * 255.0)))
}

func clampByte(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// ToNRGBA converts any image.Image into a tightly-packed 0-origin *image.NRGBA.
func ToNRGBA(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// MipLevelCount returns the total number of mipmap levels from (width, height) down to 1x1.
func MipLevelCount(width, height int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	m := max(width, height)
	levels := 1
	for m > 1 {
		m >>= 1
		levels++
	}
	return levels
}

// MipDimensions returns the width and height of mip level (0-based) for a base texture size.
func MipDimensions(baseW, baseH, level int) (int, int) {
	if level < 0 {
		return baseW, baseH
	}
	w := max(1, baseW>>level)
	h := max(1, baseH>>level)
	return w, h
}

// GenerateMipmaps generates a complete mipmap chain down to 1x1 starting from src.
// When srgb is true, RGB channels are converted to linear light prior to filtering
// and converted back to sRGB afterward.
func GenerateMipmaps(src image.Image, srgb bool) []*image.NRGBA {
	base := ToNRGBA(src)
	w, h := base.Bounds().Dx(), base.Bounds().Dy()
	count := MipLevelCount(w, h)
	chain := make([]*image.NRGBA, 0, count)
	chain = append(chain, base)

	cur := base
	for cur.Bounds().Dx() > 1 || cur.Bounds().Dy() > 1 {
		nextW := max(1, cur.Bounds().Dx()>>1)
		nextH := max(1, cur.Bounds().Dy()>>1)
		cur = downsampleBox(cur, nextW, nextH, srgb)
		chain = append(chain, cur)
	}
	return chain
}

// downsampleBox downsamples src into dstW x dstH using an exact area-weighted box filter,
// which reduces to a 2x2 average for even dimensions and preserves edge energy for odd sizes.
func downsampleBox(src *image.NRGBA, dstW, dstH int, srgb bool) *image.NRGBA {
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))

	scaleX := float64(srcW) / float64(dstW)
	scaleY := float64(srcH) / float64(dstH)

	for dy := range dstH {
		sy0 := float64(dy) * scaleY
		sy1 := float64(dy+1) * scaleY
		iy0 := int(math.Floor(sy0))
		iy1 := min(srcH-1, int(math.Ceil(sy1))-1)

		for dx := range dstW {
			sx0 := float64(dx) * scaleX
			sx1 := float64(dx+1) * scaleX
			ix0 := int(math.Floor(sx0))
			ix1 := min(srcW-1, int(math.Ceil(sx1))-1)

			var sumR, sumG, sumB, sumA, sumW float64
			for sy := iy0; sy <= iy1; sy++ {
				wy := min(float64(sy+1), sy1) - max(float64(sy), sy0)
				if wy <= 0 {
					continue
				}
				rowOff := sy * src.Stride
				for sx := ix0; sx <= ix1; sx++ {
					wx := min(float64(sx+1), sx1) - max(float64(sx), sx0)
					if wx <= 0 {
						continue
					}
					w := wx * wy
					off := rowOff + sx*4
					r := src.Pix[off+0]
					g := src.Pix[off+1]
					b := src.Pix[off+2]
					a := src.Pix[off+3]

					if srgb {
						sumR += srgbToLinearLUT[r] * w
						sumG += srgbToLinearLUT[g] * w
						sumB += srgbToLinearLUT[b] * w
					} else {
						sumR += float64(r) * w
						sumG += float64(g) * w
						sumB += float64(b) * w
					}
					sumA += float64(a) * w
					sumW += w
				}
			}

			outOff := dy*dst.Stride + dx*4
			if sumW <= 0 {
				continue
			}
			invW := 1.0 / sumW
			if srgb {
				dst.Pix[outOff+0] = linearToSRGBByte(sumR * invW)
				dst.Pix[outOff+1] = linearToSRGBByte(sumG * invW)
				dst.Pix[outOff+2] = linearToSRGBByte(sumB * invW)
			} else {
				dst.Pix[outOff+0] = clampByte(int(math.Round(sumR * invW)))
				dst.Pix[outOff+1] = clampByte(int(math.Round(sumG * invW)))
				dst.Pix[outOff+2] = clampByte(int(math.Round(sumB * invW)))
			}
			dst.Pix[outOff+3] = clampByte(int(math.Round(sumA * invW)))
		}
	}
	return dst
}
