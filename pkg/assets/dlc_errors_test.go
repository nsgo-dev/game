package assets_test

import (
	"encoding/json"
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/game/pkg/loaderr"
)

func TestParsePackManifestErrorsNameFileLineAndFix(t *testing.T) {
	cases := []struct {
		name, json string
		line       int
	}{
		{"object", "{\n  \"packs\": [\n    {\"id\": \"chapter-2\", \"size\": \"big\"}\n  ]\n}", 3},
		{"array", "\n\n[\n  {\"id\": \"chapter-2\"},\n  {\"id\": 2}\n]", 5},
		{"syntax", "{\n  \"packs\": [\n    {\"id\": \"chapter-2\",}\n  ]\n}", 3},
		{"empty", "  \n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := assets.ParsePackManifest([]byte(tc.json))
			loaderrtest.Expect(t, loaderr.WithFile(err, "dlc.json"), "dlc.json", tc.line)
		})
	}
}

func TestParsePackManifestRefusesUnknownFields(t *testing.T) {
	cases := []struct {
		name, json string
		line       int
	}{
		{"pack field", "{\n  \"packs\": [\n    {\"id\": \"chapter-2\", \"sha265\": \"ab\"}\n  ]\n}", 3},
		{"top-level field", "{\n  \"packs\": [],\n  \"signature\": \"x\"\n}", 3},
		{"array", "[\n  {\"id\": \"chapter-2\",\n   \"min_bulid\": 3}\n]", 3},
		{"trailing data", "{\"packs\": []}\n{\"packs\": []}\n", 2},
		{"duplicate key", "{\"packs\": [\n  {\"id\": \"a\", \"sha256\": \"ab\",\n   \"sha256\": \"cd\"}\n]}", 3},
		{"duplicate key in another case", "{\"packs\": [\n  {\"id\": \"a\", \"signature\": \"ab\",\n   \"Signature\": \"cd\"}\n]}", 3},
		{"duplicate top-level key", "{\n  \"packs\": [],\n  \"packs\": [{\"id\": \"a\"}]\n}", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := assets.ParsePackManifest([]byte(tc.json))
			loaderrtest.Expect(t, loaderr.WithFile(err, "dlc.json"), "dlc.json", tc.line)
		})
	}
	if _, err := assets.ParsePackManifest([]byte(`{"packs": [{"id": "a", "group": "g", "url": "u", "version": "1", "min_build": 1, "max_build": 2, "size": 3, "sha256": "s", "signature": "x"}]}`)); err != nil {
		t.Fatalf("every defined field: %v", err)
	}
}

func FuzzParsePackManifest(f *testing.F) {
	f.Add([]byte(`{"packs": [{"id": "forest", "url": "https://cdn.example.com/forest.zip", "version": "1.0.0", "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}]}`))
	f.Add([]byte(`[{"id": "a", "size": 12, "min_build": 3}]`))
	f.Add([]byte(`{"packs": [], "x": 1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		packs, err := assets.ParsePackManifest(data)
		if err != nil {
			return
		}
		// What parses encodes to a manifest that parses to the same packs.
		out, err := json.Marshal(assets.PackManifest{Packs: packs})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		again, err := assets.ParsePackManifest(out)
		if err != nil {
			t.Fatalf("reparse %s: %v", out, err)
		}
		if len(again) != len(packs) {
			t.Fatalf("reparse has %d packs, want %d", len(again), len(packs))
		}
		for i := range packs {
			if again[i] != packs[i] {
				t.Fatalf("pack %d = %+v, want %+v", i, again[i], packs[i])
			}
		}
	})
}
