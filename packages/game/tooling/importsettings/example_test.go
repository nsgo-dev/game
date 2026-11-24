package importsettings_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
)

// Parse reads the sidecar beside a sprite sheet, which game assets slices into frames and keeps sharp.
func ExampleParse() {
	f, err := importsettings.Parse([]byte(`version: 1
image:
  filter: nearest
  slice: {width: 16, height: 16, count: 6}
`))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(importsettings.Path("art/hero/run.png"))
	fmt.Println(f.Image.Filter, f.Image.Slice.Width, f.Image.Slice.Height, f.Image.Slice.Count)
	// Output:
	// art/hero/run.png.import.yaml
	// nearest 16 16 6
}

// Encode writes a sound's sidecar in the canonical form, with the schema comment editors read.
func ExampleEncode() {
	data, err := importsettings.Encode(&importsettings.File{
		Version: importsettings.CurrentVersion,
		Audio:   &importsettings.Audio{Rate: 22050, Loop: &importsettings.Loop{Start: 4410}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Print(string(data))
	// Output:
	// # yaml-language-server: $schema=https://game.nsgo.dev/schemas/import.v1.json
	// version: 1
	// audio:
	//   rate: 22050
	//   loop:
	//     start: 4410
}
