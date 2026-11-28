package main

import (
	"errors"
	"strings"
	"testing"

	pluginmanifest "github.com/nsgo-dev/game/pkg/gameplugin/manifest"
	"github.com/nsgo-dev/game/pkg/jsonschema"
)

func TestPluginManifestSchemaAcceptsWhatEncodeWrites(t *testing.T) {
	s := schemaAt(t, pluginmanifest.SchemaURL)
	manifests := []pluginmanifest.Manifest{
		{
			ManifestVersion: 1,
			Name:            "minimal",
			Module:          "example.com/minimal",
			Engine:          ">=0.4.0 <0.5.0",
			API:             1,
			Entries:         pluginmanifest.Entries{Runtime: "example.com/minimal"},
		},
		{
			ManifestVersion: 1,
			Name:            "healthkit",
			Module:          "example.com/healthkit",
			Description:     "Health component, ward panel and admit tool",
			License:         "MIT",
			Authors:         []string{"Jane Doe"},
			Repository:      "https://github.com/example/healthkit",
			Engine:          ">=0.4.0 <0.5.0",
			API:             1,
			Platforms:       []string{"all"},
			Entries: pluginmanifest.Entries{
				Runtime: "example.com/healthkit/health",
				Editor:  "example.com/healthkit/healtheditor",
				CLI:     []pluginmanifest.CLIEntry{{Name: "healthkit", Package: "example.com/healthkit/cmd/game-healthkit"}},
			},
			Requires:  map[string]string{"example.com/navkit": "^0.3.0"},
			Conflicts: []string{"example.com/oldhealth"},
			Contributes: &pluginmanifest.Contributes{
				Components: []string{"health"},
				Editor: &pluginmanifest.EditorContributes{
					Panels:   []string{"ward"},
					Tools:    []string{"admit"},
					Commands: []string{"heal-all"},
				},
				AgentTools: []string{"plugin.healthkit.heal_all"},
			},
		},
	}
	for _, m := range manifests {
		data, err := pluginmanifest.Encode(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ValidateYAML(data); err != nil {
			t.Fatalf("schema refuses what Encode wrote: %v\n%s", err, data)
		}
	}
}

func TestPluginManifestSchemaRefusesCommonMistakes(t *testing.T) {
	s := schemaAt(t, pluginmanifest.SchemaURL)
	for name, tc := range map[string]struct{ yaml, issue string }{
		"missing manifestVersion": {"name: a\nmodule: example.com/a\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/a\n", `/: missing required key "manifestVersion"`},
		"future manifestVersion":  {"manifestVersion: 2\nname: a\nmodule: example.com/a\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/a\n", "/manifestVersion: 2 is not the allowed value 1"},
		"disallowed version key":  {"manifestVersion: 1\nversion: 1.0.0\nname: a\nmodule: example.com/a\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/a\n", `/version: unknown key "version"`},
		"zero api":                {"manifestVersion: 1\nname: a\nmodule: example.com/a\nengine: '>=0.4.0'\napi: 0\nentries:\n  runtime: example.com/a\n", "/api: 0 is less than the minimum 1"},
		"unknown platform":        {"manifestVersion: 1\nname: a\nmodule: example.com/a\nengine: '>=0.4.0'\napi: 1\nplatforms: [freebsd]\nentries:\n  runtime: example.com/a\n", `/platforms/0: "freebsd" is not one of`},
		"unknown entries key":     {"manifestVersion: 1\nname: a\nmodule: example.com/a\nengine: '>=0.4.0'\napi: 1\nentries:\n  runtime: example.com/a\n  bad: x\n", `/entries/bad: unknown key "bad"`},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pluginmanifest.Parse([]byte(tc.yaml)); err == nil {
				t.Fatalf("Parse accepted %q", tc.yaml)
			}
			err := s.ValidateYAML([]byte(tc.yaml))
			var verr *jsonschema.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("ValidateYAML = %v, want *jsonschema.ValidationError", err)
			}
			if !strings.Contains(verr.Error(), tc.issue) {
				t.Fatalf("issues %q do not include %q", verr.Issues, tc.issue)
			}
		})
	}
}
