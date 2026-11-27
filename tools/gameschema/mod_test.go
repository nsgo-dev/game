package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/jsonschema"
	"github.com/nsgo-dev/game/pkg/modding"
)

func TestModSchemasAcceptEncodedManifests(t *testing.T) {
	v1 := schemaAt(t, modding.SchemaURL)
	v2 := schemaAt(t, modding.SchemaURLV2)

	v1Manifest := modding.ModManifest{
		ManifestVersion: modding.MinManifestVersion,
		ID:              "com.example.boss-rush",
		Name:            "Boss Rush",
		Version:         "1.2.0",
		Author:          "Example Studio",
		Description:     "Spawns bosses.",
		Entry:           "mod.tsx",
		GameVersion:     ">=0.2.0",
		Capabilities:    modding.CapReadState | modding.CapUI,
		Dependencies:    map[string]string{"com.example.core": "^1.0.0"},
	}
	v1Data, err := modding.EncodeManifest(v1Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := v1.ValidateYAML(v1Data); err != nil {
		t.Fatalf("mod.v1.json refused valid v1 manifest: %v\n%s", err, v1Data)
	}

	v2Manifests := []modding.ModManifest{
		{
			ManifestVersion:      modding.ManifestVersion,
			ID:                   "com.example.boss-rush",
			Name:                 "Boss Rush",
			Version:              "1.3.0",
			Entry:                "mod.tsx",
			Capabilities:         modding.CapReadState | modding.CapSpawnEntity,
			Dependencies:         map[string]string{"com.example.core": "^1.0.0"},
			OptionalDependencies: map[string]string{"com.example.music": ">=0.2.0"},
			Conflicts:            map[string]string{"com.example.old-boss": "*"},
			Platforms:            []string{"windows", "macos", "linux"},
			Contributes: modding.ModContributes{
				Assets:     "assets",
				Scenes:     "scenes",
				L10n:       "locales",
				DataTables: "tables",
			},
		},
		{
			ManifestVersion: modding.ManifestVersion,
			ID:              "com.example.slime-pack",
			Name:            "Slime Pack",
			Version:         "1.0.0",
			Contributes:     modding.ModContributes{Assets: "assets", L10n: "locales"},
		},
	}
	for _, m := range v2Manifests {
		data, err := modding.EncodeManifest(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := v2.ValidateYAML(data); err != nil {
			t.Fatalf("mod.v2.json refused valid v2 manifest %q: %v\n%s", m.ID, err, data)
		}
	}
}

func TestModSchemasRefuseInvalidFields(t *testing.T) {
	v1 := schemaAt(t, modding.SchemaURL)
	v2 := schemaAt(t, modding.SchemaURLV2)

	v1Cases := map[string]struct{ yaml, want string }{
		"missing entry":       {"manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\n", `missing required key "entry"`},
		"v2 field in v1":      {"manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\nplatforms: [macos]\n", `unknown key "platforms"`},
		"wrong version":       {"manifestVersion: 2\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\n", "/manifestVersion: 2 is not the allowed value 1"},
		"bad capability type": {"manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\ncapabilities: [123]\n", "/capabilities/0"},
	}
	for name, tc := range v1Cases {
		t.Run("v1/"+name, func(t *testing.T) {
			err := v1.ValidateYAML([]byte(tc.yaml))
			var verr *jsonschema.ValidationError
			if !errors.As(err, &verr) || !strings.Contains(verr.Error(), tc.want) {
				t.Fatalf("ValidateYAML = %v, want ValidationError containing %q", err, tc.want)
			}
		})
	}

	v2Cases := map[string]struct{ yaml, want string }{
		"wrong version":   {"manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\n", "/manifestVersion: 1 is not the allowed value 2"},
		"bad platform":    {"manifestVersion: 2\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\nplatforms: [freebsd]\n", `"freebsd" is not one of`},
		"unknown field":   {"manifestVersion: 2\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\nextra: 1\n", `unknown key "extra"`},
		"bad contributes": {"manifestVersion: 2\nid: a\nname: A\nversion: 1.0.0\ncontributes:\n  music: bgm\n", `unknown key "music"`},
	}
	for name, tc := range v2Cases {
		t.Run("v2/"+name, func(t *testing.T) {
			err := v2.ValidateYAML([]byte(tc.yaml))
			var verr *jsonschema.ValidationError
			if !errors.As(err, &verr) || !strings.Contains(verr.Error(), tc.want) {
				t.Fatalf("ValidateYAML = %v, want ValidationError containing %q", err, tc.want)
			}
		})
	}
}
