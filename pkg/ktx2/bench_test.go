package ktx2_test

import (
	"fmt"
	"image"
	"testing"

	"github.com/nsgo-dev/game/pkg/ktx2"
)

// BenchmarkTranscodeUASTC measures what render3d.LoadKTX2 does at load on a device without a direct UASTC path: decode every block of a base level and encode it again in the device's block format.
func BenchmarkTranscodeUASTC(b *testing.B) {
	for _, size := range []int{1024, 2048} {
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		for y := range size {
			for x := range size {
				i := img.PixOffset(x, y)
				img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(x), uint8(y), uint8(x^y), 255
			}
		}
		data, err := ktx2.Encode(img, ktx2.Options{Format: ktx2.FormatUASTC})
		if err != nil {
			b.Fatal(err)
		}
		tex, err := ktx2.Decode(data)
		if err != nil {
			b.Fatal(err)
		}
		for _, target := range []ktx2.Format{ktx2.FormatBC7, ktx2.FormatETC2, ktx2.FormatASTC4x4, ktx2.FormatRGBA8} {
			b.Run(fmt.Sprintf("%s/%d", target, size), func(b *testing.B) {
				for b.Loop() {
					if _, err := tex.Transcode(target); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
