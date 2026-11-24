package project_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/project"
)

// pathsYAML sets every path field of a project file, each to a different value, with a quoted value among them.
const pathsYAML = assetsHead + `autoload:
  scenes:
    - name: room
      path: scenes/room.scene.yaml
  resources:
    - name: stone
      path: "materials/stone.material.yaml"
assets:
  cache: build/cache.yaml
  atlases:
    - name: ui
      in: art/ui
      out: gen/atlases
  textures:
    - in: art/albedo.png
      out: gen/albedo.ktx2
  audio:
    - in: sfx/jump.wav
      out: gen/jump.wav
  manifest:
    in: gen
    out: gen/manifest_gen.go
export:
  output: dist
`

func TestPathRefs(t *testing.T) {
	refs, err := project.PathRefs([]byte(pathsYAML))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range refs {
		got = append(got, fmt.Sprintf("%d:%d %s %s dir=%t output=%t", r.Node.Line, r.Node.Column, r.Field, r.Node.Value, r.Dir, r.Output))
	}
	want := []string{
		"9:13 autoload scene scenes/room.scene.yaml dir=false output=false",
		"12:13 autoload resource materials/stone.material.yaml dir=false output=false",
		"14:10 assets cache build/cache.yaml dir=false output=true",
		"17:11 assets atlas in art/ui dir=true output=false",
		"18:12 assets atlas out gen/atlases dir=true output=true",
		"20:11 assets texture in art/albedo.png dir=false output=false",
		"21:12 assets texture out gen/albedo.ktx2 dir=false output=true",
		"23:11 assets audio in sfx/jump.wav dir=false output=false",
		"24:12 assets audio out gen/jump.wav dir=false output=true",
		"26:9 assets manifest in gen dir=true output=true",
		"27:10 assets manifest out gen/manifest_gen.go dir=false output=true",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("PathRefs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// EachPath visits the same fields in the same order, and changes them in place.
	f := project.MustParse([]byte(pathsYAML))
	var fields []string
	f.EachPath(func(field string, p *string) {
		fields = append(fields, field)
		*p = "moved/" + *p
	})
	for i, r := range refs {
		if fields[i] != r.Field {
			t.Fatalf("EachPath field %d = %q, want %q", i, fields[i], r.Field)
		}
	}
	if len(fields) != len(refs) || f.Assets.Atlases[0].In != "moved/art/ui" || f.Autoload.Resources[0].Path != "moved/materials/stone.material.yaml" || f.Export.Output != "dist" {
		t.Fatalf("EachPath visited %d fields; atlas in %q, resource %q, export output %q", len(fields), f.Assets.Atlases[0].In, f.Autoload.Resources[0].Path, f.Export.Output)
	}

	// A file Parse refuses is refused.
	if _, err := project.PathRefs([]byte("version: 1\nautoload:\n  scenes: []\n")); err == nil {
		t.Fatal("PathRefs accepted an invalid project file")
	}
}
