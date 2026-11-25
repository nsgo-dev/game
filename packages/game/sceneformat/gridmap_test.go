package sceneformat_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func TestObjectGridMapValidationAndRoundTrip(t *testing.T) {
	valid := []byte(`# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v2.json
version: 2
name: Dungeon
layers:
  - name: world
    objects:
      - id: keep
        gridMap: levels/dungeon.gridmap.yaml
`)
	doc, err := sceneformat.Parse(valid)
	if err != nil {
		t.Fatalf("Parse valid gridMap scene: %v", err)
	}
	if got := doc.Layers[0].Objects[0].GridMap; got != "levels/dungeon.gridmap.yaml" {
		t.Fatalf("GridMap = %q, want levels/dungeon.gridmap.yaml", got)
	}
	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(encoded, valid) {
		t.Fatalf("round-trip changed bytes:\n--- got ---\n%s\n--- want ---\n%s", encoded, valid)
	}

	badCases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "version 1 rejects gridMap",
			yaml: "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: o\n        gridMap: levels/keep.gridmap.yaml\n",
			want: "version 2",
		},
		{
			name: "non gridmap extension",
			yaml: "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: o\n        gridMap: levels/keep.yaml\n",
			want: ".gridmap.yaml",
		},
		{
			name: "invalid path",
			yaml: "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: o\n        gridMap: ../keep.gridmap.yaml\n",
			want: "relative to the project root",
		},
		{
			name: "mesh and gridMap",
			yaml: "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: o\n        mesh: box\n        gridMap: levels/keep.gridmap.yaml\n",
			want: "both mesh",
		},
		{
			name: "model and gridMap",
			yaml: "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: o\n        model: models/crate.glb\n        gridMap: levels/keep.gridmap.yaml\n",
			want: "both model",
		},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}
