package pack_test

import (
	"fmt"
	"image"
	"image/color"

	"github.com/nsgo-dev/game/packages/game/tooling/pack"
)

func filled(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, A: 255})
		}
	}
	return img
}

// PackImages packs sprites onto atlas pages and writes the TexturePacker JSON that sprite.LoadAtlas reads.
func ExamplePackImages() {
	res, err := pack.PackImages([]pack.Input{
		{Name: "hero.png", Image: filled(32, 32)},
		{Name: "coin.png", Image: filled(8, 8)},
		{Name: "door.png", Image: filled(16, 32)},
	}, pack.Options{Name: "sprites", Padding: 1, MaxWidth: 64, MaxHeight: 64})
	if err != nil {
		fmt.Println(err)
		return
	}
	page := res.Pages[0]
	fmt.Println(len(res.Pages), page.ImageName, page.JSONName)
	for _, f := range page.Frames {
		fmt.Println(f.Name, f.Frame.Dx(), f.Frame.Dy())
	}
	// Output:
	// 1 sprites.png sprites.json
	// coin.png 8 8
	// door.png 16 32
	// hero.png 32 32
}
