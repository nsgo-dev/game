package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/jsonschema"
)

// TestImportSchemaAcceptsWhatEncodeWrites checks that every sidecar Encode writes validates against the published import settings schema.
func TestImportSchemaAcceptsWhatEncodeWrites(t *testing.T) {
	s := schemaAt(t, importsettings.SchemaURL)
	no, yes := false, true
	peak, zero := -1.0, 0
	defaultPivot := importsettings.PivotPoint{0.5, 1}
	for _, f := range []*importsettings.File{
		{Version: 1, Image: &importsettings.Image{}},
		{Version: 1, Image: &importsettings.Image{Filter: "nearest", Format: "etc2", Mipmaps: &yes, SRGB: &no, Slice: &importsettings.Slice{Width: 16, Height: 16, Margin: 1, Spacing: 2, Count: 3}, Pivot: &importsettings.Pivot{Default: &defaultPivot, Cells: []importsettings.CellPivot{{Cell: 1, Pivot: importsettings.PivotPoint{0.25, 0.75}}}}}},
		{Version: 1, Image: &importsettings.Image{Pivot: &importsettings.Pivot{Default: &defaultPivot}}},
		{Version: 1, Image: &importsettings.Image{Slice: &importsettings.Slice{Width: 16, Height: 16, Count: 3}, Collision: &importsettings.Collision{Mode: "alpha", Tolerance: 8, MaxVertices: 8, Cells: []importsettings.CellCollision{{Cell: 1, Mode: "polygon", Points: []importsettings.Point2D{{0, 0}, {16, 0}, {16, 16}}}}}, Points: map[string]importsettings.Point2D{"muzzle": {14, 6}, "feet": {8, 16}}, CellPoints: []importsettings.CellPoints{{Cell: 1, Points: map[string]importsettings.Point2D{"muzzle": {15, 5}}}}}},
		{Version: 1, Audio: &importsettings.Audio{Format: "wav", Rate: 22050, Channels: 2, PeakDB: &peak, Bitrate: 128, Loop: &importsettings.Loop{Start: 10, End: 20}}},
		{Version: 1, Credit: &importsettings.Credit{Author: "Kenney", License: "CC0-1.0"}},
		{Version: 1, Font: &importsettings.Font{Size: 24, Chars: " 0123456789", Padding: &zero}},
		{Version: 1, Model: &importsettings.Model{Scale: 0.01, Animations: &no, LODHysteresis: 0.05, LODs: []importsettings.ModelLOD{{Node: "Tree_LOD0", MinScreenRatio: 0.3}, {Node: "Tree_LOD1", MinScreenRatio: 0}}}},
		{Version: 1, Image: &importsettings.Image{Filter: "nearest"}, Credit: &importsettings.Credit{Author: "Ana", License: "CC-BY-4.0", Source: "https://example.com", Notice: "Hero by Ana\nCC BY 4.0"}},
	} {
		data, err := importsettings.Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ValidateYAML(data); err != nil {
			t.Fatalf("schema refuses what Encode wrote: %v\n%s", err, data)
		}
	}
}

// TestImportSchemaRefusesWhatParseRefuses checks that the schema flags, at the right path, the common mistakes in a sidecar that Parse refuses.
func TestImportSchemaRefusesWhatParseRefuses(t *testing.T) {
	s := schemaAt(t, importsettings.SchemaURL)
	for name, tc := range map[string]struct{ yaml, issue string }{
		"missing version":            {"image: {}\n", `/: missing required key "version"`},
		"future version":             {"version: 2\nimage: {}\n", "/version: 2 is not the allowed value 1"},
		"unknown section":            {"version: 1\nshader: {}\n", `/shader: unknown key "shader"`},
		"negative font size":         {"version: 1\nfont: {size: -1}\n", "/font/size: -1 is less than the minimum 0"},
		"unknown font key":           {"version: 1\nfont: {sizes: 3}\n", `/font/sizes: unknown key "sizes"`},
		"negative model scale":       {"version: 1\nmodel: {scale: -2}\n", "/model/scale: -2 is less than the minimum 0"},
		"unknown model key":          {"version: 1\nmodel: {animation: false}\n", `/model/animation: unknown key "animation"`},
		"unknown image key":          {"version: 1\nimage: {filtre: nearest}\n", `/image/filtre: unknown key "filtre"`},
		"bad filter":                 {"version: 1\nimage: {filter: cubic}\n", `/image/filter: "cubic" is not one of "linear", "nearest"`},
		"bad format":                 {"version: 1\nimage: {format: astc}\n", `/image/format: "astc" is not one of`},
		"zero cell":                  {"version: 1\nimage: {slice: {width: 0, height: 4}}\n", "/image/slice/width: 0 is less than the minimum 1"},
		"slice without size":         {"version: 1\nimage: {slice: {width: 4}}\n", `/image/slice: missing required key "height"`},
		"pivot short default":        {"version: 1\nimage: {pivot: {default: [0.5]}}\n", "/image/pivot/default: array has 1 items, want at least 2"},
		"pivot unknown key":          {"version: 1\nimage: {pivot: {center: [0.5, 0.5]}}\n", `/image/pivot/center: unknown key "center"`},
		"pivot cell negative":        {"version: 1\nimage: {slice: {width: 8, height: 8}, pivot: {cells: [{cell: -1, pivot: [0.5, 0.5]}]}}\n", "/image/pivot/cells/0/cell: -1 is less than the minimum 0"},
		"pivot cell missing key":     {"version: 1\nimage: {slice: {width: 8, height: 8}, pivot: {cells: [{cell: 0}]}}\n", `/image/pivot/cells/0: missing required key "pivot"`},
		"collision unknown key":      {"version: 1\nimage: {collision: {mode: alpha, bogus: 1}}\n", `/image/collision/bogus: unknown key "bogus"`},
		"collision bad mode":         {"version: 1\nimage: {collision: {mode: diamond}}\n", `/image/collision/mode: "diamond" is not one of`},
		"collision maxVertices high": {"version: 1\nimage: {collision: {mode: alpha, maxVertices: 9}}\n", `/image/collision/maxVertices: 9 is greater than the maximum 8`},
		"cellPoints unknown key":     {"version: 1\nimage: {slice: {width: 8, height: 8}, cellPoints: [{cell: 0, points: {a: [1, 2]}, extra: 1}]}\n", `/image/cellPoints/0/extra: unknown key "extra"`},
		"audio format":               {"version: 1\naudio: {format: ogg}\n", `/audio/format: "ogg" is not one of "wav", "aac", "mp3"`},
		"three channels":             {"version: 1\naudio: {channels: 3}\n", "/audio/channels: 3 is greater than the maximum 2"},
		"negative loop":              {"version: 1\naudio: {loop: {start: -1}}\n", "/audio/loop/start: -1 is less than the minimum 0"},
		"credit without license":     {"version: 1\ncredit: {author: Ana}\n", `/credit: missing required key "license"`},
		"unknown credit key":         {"version: 1\ncredit: {author: Ana, license: MIT, licence: MIT}\n", `/credit/licence: unknown key "licence"`},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := importsettings.Parse([]byte(tc.yaml)); err == nil {
				t.Fatalf("Parse accepted %q", tc.yaml)
			}
			err := s.ValidateYAML([]byte(tc.yaml))
			var verr *jsonschema.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("ValidateYAML = %v, want a *ValidationError", err)
			}
			if !strings.Contains(verr.Error(), tc.issue) {
				t.Fatalf("issues %q do not include %q", verr.Issues, tc.issue)
			}
		})
	}
}
