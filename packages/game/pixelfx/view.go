package pixelfx

import (
	"image"

	"github.com/nsgo-dev/nsgo/core"
)

// Image holds a frame's picture as an NSGo image source: Set composites a frame over transparency into its pixels, premultiplied as core.ImageSource.Pixels wants them, and steps its version, so a core.Image, a canvas or a shader shows each new frame in place. The zero Image is ready to use.
type Image struct {
	buf     []byte
	img     *image.RGBA
	version uint64
}

// Set composites f into the image and returns its source.
func (im *Image) Set(f *Frame) core.ImageSource {
	im.buf = Composite(f, im.buf, nil, 1)
	if im.img == nil || im.img.Rect.Dx() != f.W || im.img.Rect.Dy() != f.H {
		im.img = image.NewRGBA(image.Rect(0, 0, f.W, f.H))
	}
	for i := 0; i < f.W*f.H*4; i += 4 {
		a := uint32(im.buf[i+3])
		im.img.Pix[i] = uint8((uint32(im.buf[i])*a + 127) / 255)
		im.img.Pix[i+1] = uint8((uint32(im.buf[i+1])*a + 127) / 255)
		im.img.Pix[i+2] = uint8((uint32(im.buf[i+2])*a + 127) / 255)
		im.img.Pix[i+3] = uint8(a)
	}
	im.version++
	return im.Source()
}

// Source returns the image's source as last Set, the zero source before then.
func (im *Image) Source() core.ImageSource {
	if im.img == nil {
		return core.ImageSource{}
	}
	b := im.img.Rect
	return core.ImageSource{Pixels: im.img, Version: im.version, Size: core.Size{Width: core.Points(b.Dx()), Height: core.Points(b.Dy())}}
}

// DrawScaled draws src at (x, y) scaled by scale with smoothing off, so pixel art stays crisp; src must say its Size.
func DrawScaled(d *core.Drawing, src core.ImageSource, x, y, scale float32) {
	w, h := float32(src.Size.Width)*scale, float32(src.Size.Height)*scale
	d.Save().SetImageSmoothing(false).DrawImageIn(src, x, y, w, h).Restore()
}

// View is a canvas that draws an effect's frames scaled up without smoothing: Source returns the picture to show (an Image's, usually), read again whenever a signal it reads changes, and Scale is how many points each pixel takes (1 when 0).
type View struct {
	Source func() core.ImageSource
	Scale  float32
	// Class is Tailwind CSS classes and Style the style, as core.Canvas's.
	Class string
	Style core.Style
	// Label names what the view shows for assistive technology.
	Label string
}

// Render implements core.Node.
func (v View) Render(*core.Scope) core.Node {
	scale := v.Scale
	if scale == 0 {
		scale = 1
	}
	src := v.Source
	return core.Canvas{
		Class: v.Class,
		Style: v.Style,
		Label: v.Label,
		Draw: func(d *core.Drawing, _ core.Size) {
			if src == nil {
				return
			}
			s := src()
			if s.Pixels == nil && s.URI == "" && len(s.Data) == 0 {
				return
			}
			DrawScaled(d, s, 0, 0, scale)
		},
	}
}
