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

// A credit section records who made an asset and its license; a font, which game assets does not import, may carry one on its own.
func ExampleCredit() {
	f, err := importsettings.Parse([]byte(`version: 1
credit:
  author: Kenney
  license: CC0-1.0
  source: https://kenney.nl/assets/kenney-fonts
`))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(f.Credit.Author, f.Credit.License, f.Image == nil && f.Audio == nil)
	// Output:
	// Kenney CC0-1.0 true
}

// ValidateLicense checks the form of an SPDX license expression.
func ExampleValidateLicense() {
	fmt.Println(importsettings.ValidateLicense("MIT OR Apache-2.0"))
	fmt.Println(importsettings.ValidateLicense("CC BY 4.0"))
	// Output:
	// <nil>
	// license "CC BY 4.0": unexpected "BY"
}
