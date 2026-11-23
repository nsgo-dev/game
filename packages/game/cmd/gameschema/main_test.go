package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/jsonschema"
)

// root is packages/game, and published the directory the website serves the schemas from, both relative to this package.
var (
	root      = filepath.Join("..", "..")
	published = filepath.Join("..", "..", "..", "..", "apps", "website", "public", "schemas")
)

func sceneSchema(t testing.TB) *jsonschema.Schema {
	t.Helper()
	files, err := generate(root)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, f := range files {
		if f.schema.ID == sceneformat.SchemaURL {
			return f.schema
		}
	}
	t.Fatalf("no schema for %s", sceneformat.SchemaURL)
	return nil
}

// TestPublishedSchemasMatchGoTypes fails when a format's Go types change without the committed schema being regenerated.
func TestPublishedSchemasMatchGoTypes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-root", root, "-out", published, "-check"}, &stdout, &stderr); err != nil {
		t.Fatalf("committed schemas are stale: %v", err)
	}
}

func TestWriteAndCheck(t *testing.T) {
	out := filepath.Join(t.TempDir(), "schemas")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-root", root, "-out", out, "-check"}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "scene.v1.json is missing") {
		t.Fatalf("-check of an empty directory: %v", err)
	}
	if err := run([]string{"-root", root, "-out", out}, &stdout, &stderr); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !strings.Contains(stdout.String(), "scene.v1.json") {
		t.Fatalf("write printed %q", stdout.String())
	}
	if err := run([]string{"-root", root, "-out", out, "-check"}, &stdout, &stderr); err != nil {
		t.Fatalf("-check after write: %v", err)
	}
	scene := filepath.Join(out, "scene.v1.json")
	data, err := os.ReadFile(scene)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scene, bytes.Replace(data, []byte(`"version"`), []byte(`"versions"`), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "old.v0.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = run([]string{"-root", root, "-out", out, "-check"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "scene.v1.json differs") || !strings.Contains(err.Error(), "old.v0.json is not generated") {
		t.Fatalf("-check of edited schemas: %v", err)
	}
	if err := run([]string{"-out", out, "extra"}, &stdout, &stderr); err == nil {
		t.Fatal("run accepted a positional argument")
	}
	if err := run([]string{"-root", t.TempDir()}, &stdout, &stderr); err == nil {
		t.Fatal("run succeeded without the package sources")
	}
}

func TestSceneSchemaShape(t *testing.T) {
	s := sceneSchema(t)
	if s.Schema != jsonschema.Draft07 || s.ID != sceneformat.SchemaURL {
		t.Fatalf("$schema %q, $id %q", s.Schema, s.ID)
	}
	version := s.Property("version")
	if version == nil || version.Const != int64(sceneformat.CurrentVersion) || !strings.Contains(version.Description, "CurrentVersion") {
		t.Fatalf("version property = %+v", version)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("scene schema accepts unknown top-level keys")
	}
	for _, name := range []string{"CameraDoc", "BoundsDoc", "LayerDoc", "TilemapDoc", "ObjectDoc", "SpriteRef"} {
		def := s.Definitions[name]
		if def == nil || def.Description == "" {
			t.Fatalf("definition %s = %+v, want one with a description", name, def)
		}
		if b, ok := def.AdditionalProperties.Boolean(); !ok || b {
			t.Fatalf("definition %s accepts unknown keys", name)
		}
		for _, p := range def.Properties {
			if p.Schema.Description == "" {
				t.Errorf("%s.%s has no description", name, p.Name)
			}
		}
	}
}

func TestSceneSchemaAcceptsTestdata(t *testing.T) {
	s := sceneSchema(t)
	paths, err := filepath.Glob(filepath.Join(root, "sceneformat", "testdata", "*.scene.yaml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no testdata scenes (%v)", err)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(data, []byte(sceneformat.SchemaComment+"\n")) {
			t.Errorf("%s does not start with %q", p, sceneformat.SchemaComment)
		}
		if err := s.ValidateYAML(data); err != nil {
			t.Errorf("%s: %v", p, err)
		}
		doc, err := sceneformat.Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		encoded, err := sceneformat.Encode(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ValidateYAML(encoded); err != nil {
			t.Errorf("%s re-encoded: %v", p, err)
		}
	}
}

// TestSceneSchemaRefusesWhatParseRefuses checks that the schema flags, at the right path, the common mistakes in a scene file that Parse refuses.
func TestSceneSchemaRefusesWhatParseRefuses(t *testing.T) {
	s := sceneSchema(t)
	cases := map[string]struct {
		yaml  string
		issue string
	}{
		"unknown top-level key": {"version: 1\nnmae: x\n", `/nmae: unknown key "nmae"`},
		"unknown object key":    {"version: 1\nlayers: [{name: a, objects: [{id: o, colour: red}]}]\n", `/layers/0/objects/0/colour: unknown key`},
		"missing version":       {"name: x\n", `/: missing required key "version"`},
		"future version":        {"version: 2\n", "/version: 2 is not the allowed value 1"},
		"layer without name":    {"version: 1\nlayers: [{order: 1}]\n", `/layers/0: missing required key "name"`},
		"bad space":             {"version: 1\nlayers: [{name: a, space: sky}]\n", `/layers/0/space: "sky" is not one of "world", "screen"`},
		"object without id":     {"version: 1\nlayers: [{name: a, objects: [{x: 1}]}]\n", `/layers/0/objects/0: missing required key "id"`},
		"alpha above 1":         {"version: 1\nlayers: [{name: a, objects: [{id: o, alpha: 1.5}]}]\n", "/layers/0/objects/0/alpha: 1.5 is greater than the maximum 1"},
		"zero zoom":             {"version: 1\ncamera: {zoom: 0}\n", "/camera/zoom: 0 is not greater than 0"},
		"empty bounds":          {"version: 1\ncamera: {bounds: {width: 0, height: 1}}\n", "/camera/bounds/width: 0 is not greater than 0"},
		"sprite without frame":  {"version: 1\nlayers: [{name: a, objects: [{id: o, sprite: {atlas: x}}]}]\n", `/layers/0/objects/0/sprite: missing required key "frame"`},
		"negative depth":        {"version: 1\nlayers: [{name: a, objects: [{id: o, depth: -1}]}]\n", "/layers/0/objects/0/depth: -1 is less than the minimum 0"},
		"string for number":     {"version: 1\nvirtualWidth: wide\n", `/virtualWidth: want number, got the string "wide"`},
		"fractional tile size":  {"version: 1\ntilemaps: {t: {tileWidth: 1.5}}\n", "/tilemaps/t/tileWidth: want integer, got the number 1.5"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := sceneformat.Parse([]byte(tc.yaml)); err == nil {
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

// FuzzSceneSchemaAcceptsEncodedScenes checks that every scene Encode writes validates against the published schema, so an editor never flags a file the engine saved.
func FuzzSceneSchemaAcceptsEncodedScenes(f *testing.F) {
	if data, err := os.ReadFile(filepath.Join(root, "sceneformat", "testdata", "cave_room.scene.yaml")); err == nil {
		f.Add(data)
	}
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("version: 1\nlayers: [{name: a, space: screen, visible: false, objects: [{id: o, alpha: 0, scaleX: -1, components: {c: {k: [1]}}}]}]\n"))
	f.Add([]byte("version: 1\ncamera: {zoom: 0.5, bounds: {width: 1, height: 2}}\ntilemaps: {g: {tileWidth: 1, tileHeight: 1, width: 1, height: 1, tileset: t, data: [0]}}\nlayers: [{name: a, tilemap: {ref: g}}, {name: b, tilemap: {source: m.tmj}}]\n"))
	s := sceneSchema(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := sceneformat.Parse(data)
		if err != nil {
			return
		}
		encoded, err := sceneformat.Encode(doc)
		if err != nil {
			t.Fatalf("Encode of a parsed scene: %v", err)
		}
		if err := s.ValidateYAML(encoded); err != nil {
			t.Fatalf("schema refuses an encoded scene: %v\n%s", err, encoded)
		}
	})
}
