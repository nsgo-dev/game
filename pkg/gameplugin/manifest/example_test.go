package manifest_test

import (
	"fmt"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/gameplugin/manifest"
)

// Parse reads and validates a plugin.yaml manifest.
func ExampleParse() {
	m, err := manifest.Parse([]byte(`manifestVersion: 1
name: healthkit
module: example.com/healthkit
engine: ">=0.4.0 <0.5.0"
api: 1
platforms: [all]
entries:
  runtime: example.com/healthkit/health
  editor: example.com/healthkit/healtheditor
requires:
  example.com/navkit: "^0.3.0"
`))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(m.Name, m.Module, m.API)
	fmt.Println(m.Entries.Runtime, m.SupportsPlatform("ios"))
	// Output:
	// healthkit example.com/healthkit 1
	// example.com/healthkit/health true
}

// Encode validates a Manifest and writes canonical 2-space YAML with lists and maps sorted in byte order.
func ExampleEncode() {
	data, err := manifest.Encode(manifest.Manifest{
		Name:      "healthkit",
		Module:    "example.com/healthkit",
		Engine:    ">=0.4.0 <0.5.0",
		API:       1,
		Platforms: []string{"web", "macos"},
		Entries: manifest.Entries{
			Runtime: "example.com/healthkit/health",
		},
		Requires: map[string]string{
			"example.com/zeta":  "^1.0.0",
			"example.com/alpha": ">=0.2.0",
		},
		Conflicts: []string{"example.com/old", "example.com/ancient"},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Print(string(data))
	// Output:
	// manifestVersion: 1
	// name: healthkit
	// module: example.com/healthkit
	// engine: '>=0.4.0 <0.5.0'
	// api: 1
	// platforms: [macos, web]
	// entries:
	//   runtime: example.com/healthkit/health
	// requires:
	//   example.com/alpha: '>=0.2.0'
	//   example.com/zeta: ^1.0.0
	// conflicts: [example.com/ancient, example.com/old]
}

// ParseFS reads plugin.yaml from a directory in an fs.FS and attaches the file path to any load error.
func ExampleParseFS() {
	fsys := fstest.MapFS{
		"healthkit/plugin.yaml": {Data: []byte("manifestVersion: 1\nname: healthkit\nmodule: example.com/healthkit\nengine: '>=0.4.0 <0.5.0'\napi: 1\nentries:\n  runtime: example.com/healthkit/health\n")},
	}
	m, err := manifest.ParseFS(fsys, "healthkit")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(m.Name, m.Entries.Runtime)
	// Output:
	// healthkit example.com/healthkit/health
}
