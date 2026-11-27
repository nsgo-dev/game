package liveops

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"

	"github.com/nsgo-dev/nsgo/core"
	xdraw "golang.org/x/image/draw"
)

const (
	// CardSnapshotAspectWidth and CardSnapshotAspectHeight define the 5:4 aspect ratio used by mini-game share cards.
	CardSnapshotAspectWidth  = 5
	CardSnapshotAspectHeight = 4

	// DefaultCardSnapshotWidth and DefaultCardSnapshotHeight are the default 5:4 share-card snapshot dimensions in pixels.
	DefaultCardSnapshotWidth  = 500
	DefaultCardSnapshotHeight = 400
)

// CardSnapshotOptions configures CaptureCardSnapshot.
type CardSnapshotOptions struct {
	// MaxWidth caps the output snapshot width in pixels while preserving the 5:4 aspect ratio (height = MaxWidth * 4 / 5).
	// Values <= 0 default to DefaultCardSnapshotWidth (500, giving 500x400).
	// Snapshots narrower than MaxWidth are center-cropped to 5:4 without upscaling.
	MaxWidth int
}

// CropCardSnapshot center-crops src to a 5:4 rectangle and scales it down to at most maxWidth pixels wide (DefaultCardSnapshotWidth when <= 0).
func CropCardSnapshot(src image.Image, maxWidth int) (*image.RGBA, error) {
	if src == nil {
		return nil, errors.New("liveops: nil image for card snapshot")
	}
	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return nil, fmt.Errorf("liveops: empty image bounds %dx%d for card snapshot", srcW, srcH)
	}

	var cropW, cropH int
	if srcW*CardSnapshotAspectHeight > srcH*CardSnapshotAspectWidth {
		// Wider than 5:4 (for example 16:9 landscape): match full height and center-crop width.
		cropH = srcH
		cropW = (srcH * CardSnapshotAspectWidth) / CardSnapshotAspectHeight
	} else {
		// Taller than 5:4 (for example 9:16 portrait) or exact 5:4: match full width and center-crop height.
		cropW = srcW
		cropH = (srcW * CardSnapshotAspectHeight) / CardSnapshotAspectWidth
	}
	if cropW < 1 {
		cropW = 1
	}
	if cropH < 1 {
		cropH = 1
	}

	x0 := bounds.Min.X + (srcW-cropW)/2
	y0 := bounds.Min.Y + (srcH-cropH)/2
	cropRect := image.Rect(x0, y0, x0+cropW, y0+cropH)

	if maxWidth <= 0 {
		maxWidth = DefaultCardSnapshotWidth
	}
	dstW, dstH := cropW, cropH
	if dstW > maxWidth {
		dstW = maxWidth
		dstH = (dstW*CardSnapshotAspectHeight + CardSnapshotAspectWidth/2) / CardSnapshotAspectWidth
		if dstH < 1 {
			dstH = 1
		}
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	if dstW == cropW && dstH == cropH {
		draw.Draw(dst, dst.Bounds(), src, cropRect.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, cropRect, xdraw.Src, nil)
	}
	return dst, nil
}

// EncodeCardSnapshotPNG center-crops src to 5:4, scales to at most maxWidth pixels wide, and encodes the result as PNG bytes.
func EncodeCardSnapshotPNG(src image.Image, maxWidth int) ([]byte, error) {
	cropped, err := CropCardSnapshot(src, maxWidth)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, cropped); err != nil {
		return nil, fmt.Errorf("liveops: encode card snapshot PNG: %w", err)
	}
	return buf.Bytes(), nil
}

// CaptureCardSnapshot captures target (or the owning surface when target is nil) through core.Scope.Capture,
// center-crops and scales it into a 5:4 PNG image, and returns a core.ImageSource ready for Card.Image.
func CaptureCardSnapshot(cx *core.Scope, target *core.Ref, opts CardSnapshotOptions, done func(core.ImageSource, error)) {
	if done == nil {
		return
	}
	if cx == nil {
		done(core.ImageSource{}, errors.New("liveops: CaptureCardSnapshot needs a core.Scope"))
		return
	}
	cx.Capture(target, func(rawPNG []byte, err error) {
		if err != nil {
			done(core.ImageSource{}, err)
			return
		}
		img, err := png.Decode(bytes.NewReader(rawPNG))
		if err != nil {
			done(core.ImageSource{}, fmt.Errorf("liveops: decode captured frame: %w", err))
			return
		}
		cropped, err := CropCardSnapshot(img, opts.MaxWidth)
		if err != nil {
			done(core.ImageSource{}, err)
			return
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, cropped); err != nil {
			done(core.ImageSource{}, fmt.Errorf("liveops: encode card snapshot PNG: %w", err))
			return
		}
		b := cropped.Bounds()
		done(core.ImageSource{
			Data: buf.Bytes(),
			Size: core.Size{Width: core.Points(b.Dx()), Height: core.Points(b.Dy())},
		}, nil)
	})
}
