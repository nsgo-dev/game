package assetbuild_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/assetbuild"
)

// Build packs the atlas game.yaml's assets section declares, then finds it up to date on the next run and writes nothing.
func ExampleBuilder_Build() {
	root, err := os.MkdirTemp("", "assetbuild-example")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	img.SetNRGBA(1, 1, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		fmt.Println(err)
		return
	}
	if err := os.MkdirAll(filepath.Join(root, "art", "ui"), 0o755); err != nil {
		fmt.Println(err)
		return
	}
	if err := os.WriteFile(filepath.Join(root, "art", "ui", "dot.png"), buf.Bytes(), 0o644); err != nil {
		fmt.Println(err)
		return
	}
	f := project.MustParse([]byte(`version: 1
name: Example
id: dev.example.assets
appVersion: '1'
window: {width: 320, height: 240}
assets:
  atlases:
    - name: ui
      in: art/ui
      out: assets
`))
	b := &assetbuild.Builder{Root: root}
	for range 2 {
		r, err := b.Build(context.Background(), f.Assets)
		if err != nil {
			fmt.Println(err)
			return
		}
		for _, t := range r.Targets {
			fmt.Println(t.Status, t.ID, t.Outputs)
		}
	}
	// Output:
	// built atlas assets/ui [assets/ui.json assets/ui.png]
	// up to date atlas assets/ui [assets/ui.json assets/ui.png]
}
