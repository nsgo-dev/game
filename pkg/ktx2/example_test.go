package ktx2_test

import (
	"fmt"
	"image"
	"image/color"

	"github.com/nsgo-dev/game/pkg/ktx2"
)

// Encode writes an image as a KTX2 texture, optionally block-compressed and with a full mipmap chain; DecodeHeader and DecodeRGBA read it back.
func ExampleEncode() {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 8))
	for y := range 8 {
		for x := range 16 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 16), G: uint8(y * 32), A: 255})
		}
	}
	data, err := ktx2.Encode(img, ktx2.Options{Format: ktx2.FormatRGBA8, Mipmaps: true})
	if err != nil {
		fmt.Println(err)
		return
	}
	h, levels, err := ktx2.DecodeHeader(data)
	fmt.Println(err, h.PixelWidth, h.PixelHeight, h.LevelCount, len(levels))
	back, _ := ktx2.DecodeRGBA(data, 0)
	fmt.Println(back.NRGBAAt(3, 2), ktx2.Validate(data))
	// Output:
	// <nil> 16 8 5 5
	// {48 64 0 255} <nil>
}

// MipLevelCount and MipDimensions give the size of each level in a mipmap chain.
func ExampleMipDimensions() {
	n := ktx2.MipLevelCount(64, 16)
	for level := range n {
		w, h := ktx2.MipDimensions(64, 16, level)
		fmt.Print(w, "x", h, " ")
	}
	fmt.Println()
	// Output:
	// 64x16 32x8 16x4 8x2 4x1 2x1 1x1
}
