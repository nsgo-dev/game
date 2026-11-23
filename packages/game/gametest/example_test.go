package gametest_test

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/nsgo/core"
)

// RenderOps rasterizes recorded drawing operations without a window or GPU, so a test can check pixels.
func ExampleRenderOps() {
	ops := (&core.Drawing{}).
		SetFillColor(core.RGB(0xff0000)).
		FillRect(2, 2, 4, 4).Ops
	img := gametest.RenderOps(8, 8, ops, gametest.RenderOptions{Background: color.RGBA{A: 255}})
	fmt.Println(img.Bounds().Dx(), img.Bounds().Dy())
	fmt.Println(img.RGBAAt(3, 3), img.RGBAAt(0, 0))
	// Output:
	// 8 8
	// {255 0 0 255} {0 0 0 255}
}

// DiffImage counts the pixels that differ between two frames and marks them in a diff image.
func ExampleDiffImage() {
	draw := func(x float32) *core.Drawing {
		return (&core.Drawing{}).SetFillColor(core.RGB(0xffffff)).FillRect(x, 0, 2, 2)
	}
	before := gametest.RenderOps(8, 2, draw(0).Ops)
	after := gametest.RenderOps(8, 2, draw(1).Ops)
	diff, mismatched := gametest.DiffImage(after, before)
	fmt.Println(mismatched, diff.RGBAAt(0, 0), diff.RGBAAt(1, 0))
	// Output:
	// 4 {255 0 255 255} {85 85 85 255}
}

// CompareGolden checks a frame against a PNG under testdata/golden; update writes the PNG instead, as running tests with the update flag does through AssertGolden.
func ExampleCompareGolden() {
	dir, err := os.MkdirTemp("", "golden")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(dir)
	path := gametest.GoldenPath(dir, "title screen")
	fmt.Println(filepath.Base(path))

	img := gametest.RenderOps(4, 4, (&core.Drawing{}).SetFillColor(core.RGB(0x2a9d8f)).FillRect(0, 0, 4, 4).Ops)
	if _, _, err := gametest.CompareGolden(img, path, true); err != nil {
		fmt.Println(err)
		return
	}
	_, mismatched, err := gametest.CompareGolden(img, path, false)
	fmt.Println(mismatched, err)
	// Output:
	// title screen.png
	// 0 <nil>
}
