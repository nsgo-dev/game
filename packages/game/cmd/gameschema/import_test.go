package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/game/packages/game/tooling/jsonschema"
)

// TestImportSchemaAcceptsWhatEncodeWrites checks that every sidecar Encode writes validates against the published import settings schema.
func TestImportSchemaAcceptsWhatEncodeWrites(t *testing.T) {
	s := schemaAt(t, importsettings.SchemaURL)
	no, yes := false, true
	peak := -1.0
	for _, f := range []*importsettings.File{
		{Version: 1, Image: &importsettings.Image{}},
		{Version: 1, Image: &importsettings.Image{Filter: "nearest", Format: "etc2", Mipmaps: &yes, SRGB: &no, Slice: &importsettings.Slice{Width: 16, Height: 16, Margin: 1, Spacing: 2, Count: 3}}},
		{Version: 1, Audio: &importsettings.Audio{Format: "wav", Rate: 22050, Channels: 2, PeakDB: &peak, Bitrate: 128, Loop: &importsettings.Loop{Start: 10, End: 20}}},
		{Version: 1, Credit: &importsettings.Credit{Author: "Kenney", License: "CC0-1.0"}},
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
		"missing version":        {"image: {}\n", `/: missing required key "version"`},
		"future version":         {"version: 2\nimage: {}\n", "/version: 2 is not the allowed value 1"},
		"unknown section":        {"version: 1\nfont: {}\n", `/font: unknown key "font"`},
		"unknown image key":      {"version: 1\nimage: {filtre: nearest}\n", `/image/filtre: unknown key "filtre"`},
		"bad filter":             {"version: 1\nimage: {filter: cubic}\n", `/image/filter: "cubic" is not one of "linear", "nearest"`},
		"bad format":             {"version: 1\nimage: {format: astc}\n", `/image/format: "astc" is not one of`},
		"zero cell":              {"version: 1\nimage: {slice: {width: 0, height: 4}}\n", "/image/slice/width: 0 is less than the minimum 1"},
		"slice without size":     {"version: 1\nimage: {slice: {width: 4}}\n", `/image/slice: missing required key "height"`},
		"audio format":           {"version: 1\naudio: {format: ogg}\n", `/audio/format: "ogg" is not one of "wav", "aac", "mp3"`},
		"three channels":         {"version: 1\naudio: {channels: 3}\n", "/audio/channels: 3 is greater than the maximum 2"},
		"negative loop":          {"version: 1\naudio: {loop: {start: -1}}\n", "/audio/loop/start: -1 is less than the minimum 0"},
		"credit without license": {"version: 1\ncredit: {author: Ana}\n", `/credit: missing required key "license"`},
		"unknown credit key":     {"version: 1\ncredit: {author: Ana, license: MIT, licence: MIT}\n", `/credit/licence: unknown key "licence"`},
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
