package credits_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/credits"
	"github.com/nsgo-dev/game/pkg/tooling/importsettings"
)

// Collect reads the credit beside each asset game.yaml's assets section reaches, here the sound an audio target converts.
func ExampleCollect() {
	root, err := os.MkdirTemp("", "credits-example")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(root)
	if err := os.MkdirAll(filepath.Join(root, "sfx"), 0o755); err != nil {
		fmt.Println(err)
		return
	}
	for name, data := range map[string]string{
		"sfx/jump.wav":             "RIFF",
		"sfx/jump.wav.import.yaml": "version: 1\ncredit: {author: Kenney, license: CC0-1.0}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(data), 0o644); err != nil {
			fmt.Println(err)
			return
		}
	}
	entries, err := credits.Collect(root, &project.Assets{Audio: []project.AudioTarget{{In: "sfx/jump.wav", Out: "assets/jump.wav"}}})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, e := range entries {
		fmt.Println(e.Asset, e.Credit.Author, e.Credit.License)
	}
	// Output:
	// sfx/jump.wav Kenney CC0-1.0
}

// Markdown writes the credits file a release ships with.
func ExampleMarkdown() {
	fmt.Print(string(credits.Markdown([]credits.Entry{
		{Asset: "art/hero.png", Credit: importsettings.Credit{Author: "Ana", License: "CC-BY-4.0", Source: "https://example.com/hero", Notice: "Hero by Ana, CC BY 4.0"}},
	})))
	// Output:
	// # Credits
	//
	// <!-- Generated from the credit section of each asset's import settings sidecar (*.import.yaml). DO NOT EDIT. -->
	//
	// | Asset | Author | License | Source |
	// | --- | --- | --- | --- |
	// | `art/hero.png` | Ana | CC-BY-4.0 | <https://example.com/hero> |
	//
	// ## Licenses
	//
	// | License | Assets |
	// | --- | --- |
	// | [CC-BY-4.0](https://spdx.org/licenses/CC-BY-4.0.html) | 1 |
	//
	// ## Notices
	//
	// ### `art/hero.png`
	//
	// ```text
	// Hero by Ana, CC BY 4.0
	// ```
}
