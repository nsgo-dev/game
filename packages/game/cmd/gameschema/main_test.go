package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/cutscene"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/navbake3d"
	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/game/packages/game/playtest"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/jsonschema"
	"github.com/nsgo-dev/game/packages/game/voxel"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

// root is packages/game, and published the directory the website serves the schemas from, both relative to this package.
var (
	root      = filepath.Join("..", "..")
	published = filepath.Join("..", "..", "..", "..", "apps", "website", "public", "schemas")
)

// sceneSchema returns the generated version 1 scene schema.
func sceneSchema(t testing.TB) *jsonschema.Schema {
	t.Helper()
	return schemaAt(t, sceneformat.SchemaURL)
}

// schemaAt returns the generated schema published at url.
func schemaAt(t testing.TB, url string) *jsonschema.Schema {
	t.Helper()
	files, err := generate(root)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, f := range files {
		if f.schema.ID == url {
			return f.schema
		}
	}
	t.Fatalf("no schema for %s", url)
	return nil
}

// sceneSchemas returns the generated scene schemas keyed by the version they describe.
func sceneSchemas(t testing.TB) map[int]*jsonschema.Schema {
	t.Helper()
	return map[int]*jsonschema.Schema{
		1: schemaAt(t, sceneformat.SchemaURL),
		2: schemaAt(t, sceneformat.SchemaURLV2),
		3: schemaAt(t, sceneformat.SchemaURLV3),
		4: schemaAt(t, sceneformat.SchemaURLV4),
	}
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
	if version == nil || version.Const != int64(1) || !strings.Contains(version.Description, "CurrentVersion") {
		t.Fatalf("version property = %+v", version)
	}
	for _, key := range []string{"inherits", "overrides", "layerOverrides"} {
		if s.Property(key) != nil {
			t.Errorf("version 1 schema has the version 2 key %q", key)
		}
	}
	if obj := s.Definitions["ObjectDoc"]; obj == nil || obj.Property("scene") != nil || obj.Property("overrides") != nil {
		t.Errorf("version 1 ObjectDoc = %+v, want one without scene and overrides", obj)
	}
	if _, ok := s.Definitions["ObjectOverride"]; ok {
		t.Error("version 1 schema defines ObjectOverride")
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("scene schema accepts unknown top-level keys")
	}
	for _, name := range []string{"CameraDoc", "BoundsDoc", "LayerDoc", "TilemapDoc", "ObjectDoc", "SpriteRef", "Point", "Rect", "Circle", "NavRegion2DComponent"} {
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
	if poly := s.Definitions["Polygon"]; poly == nil || poly.Description == "" || poly.Items == nil || poly.Items.Ref != "#/definitions/Point" {
		t.Fatalf("definition Polygon = %+v", poly)
	}
	if obj := s.Definitions["ObjectDoc"]; obj == nil || obj.Property("components").Property(sceneformat.NavRegion2DComponentName) == nil {
		t.Fatal("ObjectDoc.components missing navregion2d property")
	}
	navPts := s.Definitions["NavRegion2DComponent"].Property("points")
	if navPts == nil || navPts.Items == nil || navPts.Items.MinItems == nil || *navPts.Items.MinItems != 2 || navPts.Items.MaxItems == nil || *navPts.Items.MaxItems != 2 {
		t.Fatalf("NavRegion2DComponent.points items = %+v, want minItems=2 and maxItems=2", navPts)
	}
	badPointYAML := []byte("version: 1\nname: Bad\nlayers:\n  - name: world\n    objects:\n      - id: r1\n        components:\n          navregion2d:\n            version: 1\n            points: [[0, 0, 1], [10, 0], [10, 10]]\n")
	if err := s.ValidateYAML(badPointYAML); err == nil {
		t.Fatal("expected schema to reject 3-element point tuple in navregion2d.points")
	}
}

func TestSceneSchemaV2Shape(t *testing.T) {
	s := schemaAt(t, sceneformat.SchemaURLV2)
	if version := s.Property("version"); version == nil || version.Const != int64(2) {
		t.Fatalf("version property = %+v", version)
	}
	for _, key := range []string{"inherits", "overrides", "layerOverrides"} {
		if p := s.Property(key); p == nil || p.Description == "" {
			t.Errorf("version 2 schema property %q = %+v, want one with a description", key, p)
		}
	}
	if obj := s.Definitions["ObjectDoc"]; obj == nil || obj.Property("scene") == nil || obj.Property("overrides") == nil {
		t.Errorf("version 2 ObjectDoc = %+v, want one with scene and overrides", obj)
	}
	for _, name := range []string{"LayerDoc", "LayerOverride"} {
		def := s.Definitions[name]
		for _, key := range []string{"parallaxX", "parallaxY", "repeatX", "repeatY", "repeatWidth", "repeatHeight"} {
			if def != nil && def.Property(key) != nil {
				t.Errorf("version 2 %s has version 3 key %q", name, key)
			}
		}
	}
	for _, name := range []string{"ObjectOverride", "LayerOverride"} {
		def := s.Definitions[name]
		if def == nil || def.Description == "" {
			t.Fatalf("definition %s = %+v, want one with a description", name, def)
		}
		for _, p := range def.Properties {
			if p.Schema.Description == "" {
				t.Errorf("%s.%s has no description", name, p.Name)
			}
		}
	}
}

func TestSceneSchemaV3Shape(t *testing.T) {
	s := schemaAt(t, sceneformat.SchemaURLV3)
	if version := s.Property("version"); version == nil || version.Const != int64(3) {
		t.Fatalf("version property = %+v", version)
	}
	if s.Property("environment") != nil {
		t.Error("version 3 schema has version 4 key environment")
	}
	if obj := s.Definitions["ObjectDoc"]; obj != nil && (obj.Property("light") != nil || obj.Property("camera") != nil) {
		t.Error("version 3 ObjectDoc has version 4 light or camera key")
	}
	for _, name := range []string{"LayerDoc", "LayerOverride"} {
		def := s.Definitions[name]
		if def == nil || def.Description == "" {
			t.Fatalf("definition %s = %+v, want one with a description", name, def)
		}
		for _, key := range []string{"parallaxX", "parallaxY", "repeatX", "repeatY", "repeatWidth", "repeatHeight"} {
			if p := def.Property(key); p == nil || p.Description == "" {
				t.Errorf("version 3 %s.%s = %+v, want one with a description", name, key, p)
			}
		}
	}
}

func TestSceneSchemaV4Shape(t *testing.T) {
	s := schemaAt(t, sceneformat.SchemaURLV4)
	if version := s.Property("version"); version == nil || version.Const != int64(sceneformat.CurrentVersion) {
		t.Fatalf("version property = %+v", version)
	}
	if p := s.Property("environment"); p == nil || p.Description == "" {
		t.Errorf("version 4 environment = %+v, want one with a description", p)
	}
	for _, name := range []string{"ObjectDoc", "ObjectOverride"} {
		def := s.Definitions[name]
		if def == nil {
			t.Fatalf("version 4 lacks %s", name)
		}
		for _, key := range []string{"light", "camera", "reflectionProbe", "lightProbeGrid"} {
			if p := def.Property(key); p == nil || p.Description == "" {
				t.Errorf("version 4 %s.%s = %+v, want one with a description", name, key, p)
			}
		}
	}
	for _, name := range []string{"EnvironmentDoc", "SkyDoc", "FogDoc", "LightDoc", "Camera3DDoc", "ReflectionProbeDoc", "LightProbeGridDoc"} {
		def := s.Definitions[name]
		if def == nil || def.Description == "" {
			t.Fatalf("version 4 definition %s = %+v, want one with a description", name, def)
		}
		for _, p := range def.Properties {
			if p.Schema.Description == "" {
				t.Errorf("version 4 %s.%s has no description", name, p.Name)
			}
		}
	}
}

// TestSceneSchemaAcceptsTestdata checks every scene fixture, and its re-encoding, against the schema its version names.
func TestSceneSchemaAcceptsTestdata(t *testing.T) {
	schemas := sceneSchemas(t)
	var paths []string
	for _, pattern := range []string{"*.scene.yaml", "project/*.scene.yaml", "project/scenes/*.scene.yaml"} {
		matches, err := filepath.Glob(filepath.Join(root, "sceneformat", "testdata", filepath.FromSlash(pattern)))
		if err != nil || len(matches) == 0 {
			t.Fatalf("no testdata scenes match %s (%v)", pattern, err)
		}
		paths = append(paths, matches...)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := sceneformat.Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		comment := sceneformat.SchemaCommentFor(doc.Version)
		if !bytes.HasPrefix(data, []byte(comment+"\n")) {
			t.Errorf("%s does not start with %q", p, comment)
		}
		s := schemas[doc.Version]
		if err := s.ValidateYAML(data); err != nil {
			t.Errorf("%s: %v", p, err)
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
		"future version":        {"version: 5\n", "/version: 5 is not the allowed value 1"},
		"inherits in version 1": {"version: 1\ninherits: base.scene.yaml\n", `/inherits: unknown key "inherits"`},
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
		"order overflows int":   {"version: 1\nlayers: [{name: a, order: 1e20}]\n", "/layers/0/order: 100000000000000000000 is greater than the maximum 9223372036854776000"},
		"tile overflows int":    {"version: 1\ntilemaps: {t: {data: [1e20]}}\n", "/tilemaps/t/data/0: 100000000000000000000 is greater than the maximum"},
		"connection arg list":   {"version: 1\nlayers: [{name: a, objects: [{id: b}]}]\nconnections: [{from: b, event: e, to: b, handler: h, args: {a: [1]}}]\n", "/connections/0/args/a: want boolean or number or string"},
		"connection no handler": {"version: 1\nlayers: [{name: a, objects: [{id: b}]}]\nconnections: [{from: b, event: e, to: b}]\n", `/connections/0: missing required key "handler"`},
		"x overflows float32":   {"version: 1\nlayers: [{name: a, objects: [{id: o, x: 1e39}]}]\n", "/layers/0/objects/0/x: 1e+39 is greater than the maximum 3.4028234663852886e+38"},
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

// TestSceneSchemaRefusesRepeatedKeys checks that ValidateYAML refuses a key written twice in one mapping, as Parse and a language server do, rather than letting the later one win.
func TestSceneSchemaRefusesRepeatedKeys(t *testing.T) {
	data := []byte("version: 1\nname: a\nname: b\n")
	if _, err := sceneformat.Parse(data); err == nil {
		t.Fatal("Parse accepted a repeated key")
	}
	if err := sceneSchema(t).ValidateYAML(data); err == nil || !strings.Contains(err.Error(), `line 3: mapping key "name" already defined`) {
		t.Fatalf("ValidateYAML = %v", err)
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
	f.Add([]byte("version: 2\ninherits: base.scene.yaml\noverrides: {o: {x: 1, alpha: 0.5}}\nlayerOverrides: {a: {order: 2}}\nlayers: [{name: b, objects: [{id: e, scene: enemy.scene.yaml, overrides: {body: {z: 1}}}]}]\n"))
	f.Add([]byte("version: 3\ninherits: base.scene.yaml\nlayerOverrides: {sky: {parallaxX: 0.5, parallaxY: 0.25, repeatX: true, repeatWidth: 320}}\nlayers: [{name: clouds, parallaxX: 0.5, parallaxY: 0, repeatX: true, repeatWidth: 160}]\n"))
	f.Add([]byte("version: 1\nlayers: [{name: a, objects: [{id: b}, {id: d}]}]\nconnections: [{from: b, event: e, to: d, handler: h, args: {s: 2.5, n: x, t: true}, once: true}]\n"))
	schemas := sceneSchemas(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := sceneformat.Parse(data)
		if err != nil {
			return
		}
		encoded, err := sceneformat.Encode(doc)
		if err != nil {
			t.Fatalf("Encode of a parsed scene: %v", err)
		}
		if err := schemas[doc.Version].ValidateYAML(encoded); err != nil {
			t.Fatalf("schema refuses an encoded scene: %v\n%s", err, encoded)
		}
	})
}

func TestVoxelSchemaShapeAndValidation(t *testing.T) {
	s := schemaAt(t, voxel.SchemaURL)
	if s.Schema != jsonschema.Draft07 || s.ID != voxel.SchemaURL {
		t.Fatalf("$schema %q, $id %q", s.Schema, s.ID)
	}
	if version := s.Property("version"); version == nil || version.Const != int64(voxel.FormatVersion) {
		t.Fatalf("version property = %+v", version)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("voxel schema accepts unknown top-level keys")
	}
	for _, name := range []string{"BlockDoc", "ChunkDoc"} {
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

	reg := voxel.NewRegistry()
	stone := reg.MustRegister(voxel.BlockDef{Name: "stone"})
	glass := reg.MustRegister(voxel.BlockDef{Name: "glass", Transparent: true, MaterialID: 1})
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 4, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	w.FillBox(voxel.BlockCoord{}, voxel.BlockCoord{X: 3, Y: 0, Z: 3}, stone)
	w.SetBlock(voxel.BlockCoord{X: 1, Y: 1, Z: 1}, glass)

	data, err := voxel.SaveYAML(w, "Sample")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateYAML(data); err != nil {
		t.Fatalf("voxel schema refused SaveYAML output: %v\n%s", err, data)
	}
	if err := s.ValidateYAML([]byte("version: 1\nchunkSize: 16\nunknownKey: true\n")); err == nil {
		t.Fatal("voxel schema accepted unknown top-level key")
	}
}

func TestL10nSchemaShapeAndValidation(t *testing.T) {
	s := schemaAt(t, l10n.SchemaURL)
	if s.Schema != jsonschema.Draft07 || s.ID != l10n.SchemaURL {
		t.Fatalf("$schema %q, $id %q", s.Schema, s.ID)
	}
	if version := s.Property("version"); version == nil || version.Const != int64(l10n.StringTableVersion) {
		t.Fatalf("version property = %+v", version)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("l10n schema accepts unknown top-level keys")
	}
	def := s.Definitions["PluralForms"]
	if def == nil || def.Description == "" {
		t.Fatalf("definition PluralForms = %+v, want one with a description", def)
	}
	if b, ok := def.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("PluralForms accepts unknown plural categories")
	}
	for _, p := range def.Properties {
		if p.Schema.Description == "" {
			t.Errorf("PluralForms.%s has no description", p.Name)
		}
	}

	data, err := l10n.EncodeYAML(&l10n.StringTable{
		Version: l10n.StringTableVersion,
		Locale:  "en",
		Strings: map[string]string{"menu.play": "Play"},
		Plurals: map[string]l10n.PluralForms{
			"coins": {Zero: "No coins", One: "{count} coin", Other: "{count} coins"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateYAML(data); err != nil {
		t.Fatalf("l10n schema refused EncodeYAML output: %v\n%s", err, data)
	}
	if err := s.ValidateYAML([]byte("version: 1\nlocale: en\nunknownKey: true\n")); err == nil {
		t.Fatal("l10n schema accepted unknown top-level key")
	}
	if err := s.ValidateYAML([]byte("version: 1\nlocale: en\nplurals:\n  coins:\n    singular: 1\n    other: n\n")); err == nil {
		t.Fatal("l10n schema accepted unknown plural category")
	}
}

func TestWorld3DSchemasShapeAndValidation(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		version int
		defs    []string
		fixture string
		badYAML []string
	}{
		{
			name:    "terrain",
			url:     world3d.TerrainSchemaURL,
			version: world3d.TerrainFormatVersion,
			defs:    []string{"TerrainTriplanarDoc", "TerrainLayerDoc"},
			fixture: filepath.Join(root, "world3d", "testdata", "alpine.terrain.yaml"),
			badYAML: []string{
				"version: 1\nunknownKey: true\nlayers: [{name: grass}]\n",
				"version: 1\nchunkSize: 0\nlayers: [{name: grass}]\n",
			},
		},
		{
			name:    "gridmap",
			url:     world3d.GridMapSchemaURL,
			version: world3d.GridMapFormatVersion,
			defs:    []string{"GridBoxDoc", "GridCellDoc"},
			fixture: filepath.Join(root, "world3d", "testdata", "dungeon.gridmap.yaml"),
			badYAML: []string{
				"version: 1\nunknownKey: true\n",
				"version: 1\ncells: [{at: [0, 0, 0], tile: floor, orientation: 24}]\n",
			},
		},
		{
			name:    "foliage",
			url:     world3d.FoliageSchemaURL,
			version: world3d.FoliageFormatVersion,
			defs:    []string{"FoliageLayerDoc"},
			fixture: filepath.Join(root, "world3d", "testdata", "meadow.foliage.yaml"),
			badYAML: []string{
				"version: 1\nunknownKey: true\nlayers: [{name: grass, mesh: grass.glb, density: 1}]\n",
				"version: 1\nlayers: [{name: grass, mesh: grass.glb, maxSlopeDeg: 95}]\n",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := schemaAt(t, tc.url)
			if s.Schema != jsonschema.Draft07 || s.ID != tc.url {
				t.Fatalf("$schema %q, $id %q", s.Schema, s.ID)
			}
			if version := s.Property("version"); version == nil || version.Const != int64(tc.version) {
				t.Fatalf("version property = %+v", version)
			}
			if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
				t.Fatalf("%s schema accepts unknown top-level keys", tc.name)
			}
			for _, p := range s.Properties {
				if p.Schema.Description == "" {
					t.Errorf("%s.%s has no description", tc.name, p.Name)
				}
			}
			for _, defName := range tc.defs {
				def := s.Definitions[defName]
				if def == nil || def.Description == "" {
					t.Fatalf("definition %s = %+v, want one with a description", defName, def)
				}
				if b, ok := def.AdditionalProperties.Boolean(); !ok || b {
					t.Fatalf("definition %s accepts unknown keys", defName)
				}
				for _, p := range def.Properties {
					if p.Schema.Description == "" {
						t.Errorf("%s.%s has no description", defName, p.Name)
					}
				}
			}
			data, err := os.ReadFile(tc.fixture)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.ValidateYAML(data); err != nil {
				t.Fatalf("%s schema refused %s: %v", tc.name, tc.fixture, err)
			}
			for _, bad := range tc.badYAML {
				if err := s.ValidateYAML([]byte(bad)); err == nil {
					t.Fatalf("%s schema accepted invalid YAML:\n%s", tc.name, bad)
				}
			}
		})
	}
}

func TestNavMeshSchemaShapeAndValidation(t *testing.T) {
	s := schemaAt(t, navbake3d.SchemaURL)
	if version := s.Property("version"); version == nil || version.Const != int64(navbake3d.FormatVersion) {
		t.Fatalf("version property = %+v", version)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("navmesh schema accepts unknown top-level keys")
	}
	data, err := os.ReadFile(filepath.Join(root, "navbake3d", "testdata", "reference.navmesh.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateYAML(data); err != nil {
		t.Fatalf("navmesh schema refused the reference bake: %v", err)
	}
	for _, bad := range []string{
		"version: 1\nunknownKey: true\n",
		"version: 2\nsettings: {}\n",
		"version: 1\nsettings: {cellSize: -1}\n",
		"version: 1\nmeshes: [{agent: {name: a, maxSlope: 90}}]\n",
	} {
		if err := s.ValidateYAML([]byte(bad)); err == nil {
			t.Fatalf("navmesh schema accepted invalid YAML:\n%s", bad)
		}
	}
}

func TestLightProbeGridSchemaShapeAndValidation(t *testing.T) {
	s := schemaAt(t, render3d.LightProbeGridSchemaURL)
	if version := s.Property("version"); version == nil || version.Const != int64(render3d.LightProbeGridVersion) {
		t.Fatalf("version property = %+v", version)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("lightprobes schema accepts unknown top-level keys")
	}
	data, err := os.ReadFile(filepath.Join(root, "render3d", "testdata", "room.probes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateYAML(data); err != nil {
		t.Fatalf("lightprobes schema refused room.probes.yaml: %v", err)
	}
	for _, bad := range []string{
		"version: 1\nunknownKey: true\n",
		"version: 2\nboundsMin: [0, 0, 0]\nboundsMax: [1, 1, 1]\ncounts: [1, 1, 1]\nprobes: []\n",
	} {
		if err := s.ValidateYAML([]byte(bad)); err == nil {
			t.Fatalf("lightprobes schema accepted invalid YAML:\n%s", bad)
		}
	}
}

func TestProjectSchemaShapeAndValidation(t *testing.T) {
	s := schemaAt(t, project.SchemaURL)
	if s.Schema != jsonschema.Draft07 || s.ID != project.SchemaURL {
		t.Fatalf("$schema %q, $id %q", s.Schema, s.ID)
	}
	if version := s.Property("version"); version == nil || version.Const != int64(project.CurrentVersion) {
		t.Fatalf("version property = %+v", version)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("project schema accepts unknown top-level keys")
	}
	for _, name := range []string{"Window", "Display", "Input", "Action", "Binding", "Rect", "Axis", "MotionAxis", "GamepadAxis"} {
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
	for _, file := range []string{"game.yaml", "input_bindings.yaml"} {
		data, err := os.ReadFile(filepath.Join(root, "project", "testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ValidateYAML(data); err != nil {
			t.Fatalf("project schema refused testdata/%s: %v", file, err)
		}
	}
	const head = "version: 1\nname: G\nid: dev.nsgo.g\nappVersion: 1.0.0\nwindow: {width: 640, height: 360}\n"
	for name, src := range map[string]string{
		"unknown top-level key": head + "extra: true\n",
		"unknown binding kind":  head + "input: {actions: [{name: a, bindings: [{kind: voice}]}]}\n",
		"unknown binding key":   head + "input: {actions: [{name: a, bindings: [{kind: key, key: space, extra: 1}]}]}\n",
		"unknown gamepad axis":  head + "input: {gamepadAxes: [{name: g, axis: triggerL}]}\n",
	} {
		if err := s.ValidateYAML([]byte(src)); err == nil {
			t.Fatalf("project schema accepted %s", name)
		}
	}
}

func TestBTSchemaShapeAndValidation(t *testing.T) {
	s := schemaAt(t, ai.BTSchemaURL)
	if s.Schema != jsonschema.Draft07 || s.ID != ai.BTSchemaURL {
		t.Fatalf("$schema %q, $id %q", s.Schema, s.ID)
	}
	if version := s.Property("version"); version == nil || version.Const != int64(ai.BTFormatVersion) {
		t.Fatalf("version property = %+v", version)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("bt schema accepts unknown top-level keys")
	}
	for _, p := range s.Properties {
		if p.Schema.Description == "" {
			t.Errorf("BehaviorTreeDoc.%s has no description", p.Name)
		}
	}
	for _, defName := range []string{"BTBlackboardKeyDoc", "BTNodeDoc"} {
		def := s.Definitions[defName]
		if def == nil || def.Description == "" {
			t.Fatalf("definition %s = %+v, want one with a description", defName, def)
		}
		if b, ok := def.AdditionalProperties.Boolean(); !ok || b {
			t.Fatalf("definition %s accepts unknown keys", defName)
		}
		for _, p := range def.Properties {
			if p.Schema.Description == "" {
				t.Errorf("%s.%s has no description", defName, p.Name)
			}
		}
	}
	for _, name := range []string{"guard.bt.yaml", "patrol.bt.yaml"} {
		p := filepath.Join(root, "ai", "testdata", "bt", name)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ValidateYAML(data); err != nil {
			t.Fatalf("bt schema refused %s: %v", name, err)
		}
	}
	badDocs := []string{
		"version: 1\nunknownKey: true\nroot:\n  type: wait\n  duration: 1s\n",
		"version: 1\nroot:\n  type: teleport\n",
		"version: 1\nroot:\n  type: blackboard_condition\n  key: alert\n  op: approx\n  value: true\n",
		"version: 1\nroot:\n  type: blackboard_condition\n  key: alert\n  op: eq\n  value: true\n  abort: always\n",
		"version: 1\nblackboard:\n  - key: hp\n    type: map\nroot:\n  type: wait\n  duration: 1s\n",
		"version: 1\nroot:\n  type: action\n  action: attack\n  params:\n    nested:\n      a: 1\n",
	}
	for _, bad := range badDocs {
		if err := s.ValidateYAML([]byte(bad)); err == nil {
			t.Fatalf("bt schema accepted invalid YAML:\n%s", bad)
		}
	}
}
func TestCutsceneSchemasShapeAndValidation(t *testing.T) {
	s1 := schemaAt(t, cutscene.SchemaURL)
	if s1.Schema != jsonschema.Draft07 || s1.ID != cutscene.SchemaURL {
		t.Fatalf("v1 $schema %q, $id %q", s1.Schema, s1.ID)
	}
	if version := s1.Property("version"); version == nil || version.Const != int64(cutscene.MinVersion) {
		t.Fatalf("v1 version property = %+v", version)
	}
	for _, key := range []string{"kind", "scene", "length", "tracks"} {
		if s1.Property(key) != nil {
			t.Errorf("v1 cutscene schema has v2 key %q", key)
		}
	}
	if b, ok := s1.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("v1 cutscene schema accepts unknown top-level keys")
	}
	v1Data, err := os.ReadFile(filepath.Join(root, "cutscene", "testdata", "intro.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.ValidateYAML(v1Data); err != nil {
		t.Fatalf("v1 schema refused intro.yaml: %v", err)
	}
	for _, bad := range []string{
		"version: 1\n",
		"version: 1\nsource: asset:a.mp4\nextra: true\n",
		"version: 1\nsource: asset:a.mp4\nkind: video\n",
		"version: 1\nsource: asset:a.mp4\nskipAfter: 2\n",
	} {
		if err := s1.ValidateYAML([]byte(bad)); err == nil {
			t.Fatalf("v1 cutscene schema accepted invalid YAML:\n%s", bad)
		}
	}

	s2 := schemaAt(t, cutscene.SchemaURLV2)
	if s2.Schema != jsonschema.Draft07 || s2.ID != cutscene.SchemaURLV2 {
		t.Fatalf("v2 $schema %q, $id %q", s2.Schema, s2.ID)
	}
	if version := s2.Property("version"); version == nil || version.Const != int64(cutscene.CurrentVersion) {
		t.Fatalf("v2 version property = %+v", version)
	}
	if b, ok := s2.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("v2 cutscene schema accepts unknown top-level keys")
	}
	for _, p := range s2.Properties {
		if p.Schema.Description == "" {
			t.Errorf("Cutscene.%s has no description", p.Name)
		}
	}
	for _, name := range []string{"Track", "Clip"} {
		def := s2.Definitions[name]
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
	v2Data, err := os.ReadFile(filepath.Join(root, "cutscene", "testdata", "floor1_intro.cutscene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(v2Data, []byte(cutscene.SchemaCommentV2+"\n")) {
		t.Fatalf("floor1_intro.cutscene.yaml does not start with %q", cutscene.SchemaCommentV2)
	}
	if err := s2.ValidateYAML(v2Data); err != nil {
		t.Fatalf("v2 schema refused floor1_intro.cutscene.yaml: %v", err)
	}
	for _, bad := range []string{
		"version: 2\nkind: audio\nlength: 5s\n",
		"version: 2\nkind: timeline\nlength: 5s\nunknownKey: true\n",
		"version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: lights\n",
		"version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: dialogue\n    clips:\n      - start: 0s\n        duration: 1s\n        line: intro.01\n        text: inline\n",
		"version: 2\nkind: timeline\nlength: 5\n",
	} {
		if err := s2.ValidateYAML([]byte(bad)); err == nil {
			t.Fatalf("v2 cutscene schema accepted invalid YAML:\n%s", bad)
		}
	}
}
func TestPlaytestSchemaShapeAndValidation(t *testing.T) {
	s := schemaAt(t, playtest.SchemaURL)
	if s.Schema != jsonschema.Draft07 || s.ID != playtest.SchemaURL {
		t.Fatalf("$schema %q, $id %q", s.Schema, s.ID)
	}
	if version := s.Property("version"); version == nil || version.Const != int64(playtest.CurrentVersion) {
		t.Fatalf("version property = %+v", version)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("playtest schema accepts unknown top-level keys")
	}
	for _, p := range s.Properties {
		if p.Schema.Description == "" {
			t.Errorf("Config.%s has no description", p.Name)
		}
	}
	for _, defName := range []string{"Scope", "Runs", "BotConfig", "Metric", "Measure", "Goal"} {
		def := s.Definitions[defName]
		if def == nil || def.Description == "" {
			t.Fatalf("definition %s = %+v, want one with a description", defName, def)
		}
		if b, ok := def.AdditionalProperties.Boolean(); !ok || b {
			t.Fatalf("definition %s accepts unknown keys", defName)
		}
		for _, p := range def.Properties {
			if p.Schema.Description == "" {
				t.Errorf("%s.%s has no description", defName, p.Name)
			}
		}
	}
	for _, fixture := range []string{
		"platformer.playtest.yaml",
		"rpg2d.playtest.yaml",
		"shmup2d.playtest.yaml",
		"puzzle2d.playtest.yaml",
		"cards2d.playtest.yaml",
	} {
		data, err := os.ReadFile(filepath.Join(root, "playtest", "testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ValidateYAML(data); err != nil {
			t.Fatalf("playtest schema refused %s: %v", fixture, err)
		}
	}
	badDocs := []string{
		"version: 1\nunknownKey: true\nscope: {scene: level_01}\nruns: {count: 1}\nbot: {profile: platformer-runner}\n",
		"version: 2\nscope: {scene: level_01}\nruns: {count: 1}\nbot: {profile: platformer-runner}\n",
		"version: 1\nscope: {scene: level_01}\nruns: {count: -1}\nbot: {profile: platformer-runner}\n",
		"version: 1\nscope: {scene: level_01}\nruns: {count: 1}\nbot: {profile: platformer-runner, params: {nested: {a: 1}}}\n",
		"version: 1\nscope: {scene: level_01}\nruns: {count: 1}\nbot: {profile: platformer-runner}\nmetrics: [{name: m, measure: {kind: bogus, key: k}}]\n",
	}
	for _, bad := range badDocs {
		if err := s.ValidateYAML([]byte(bad)); err == nil {
			t.Fatalf("playtest schema accepted invalid YAML:\n%s", bad)
		}
	}
}

func TestScreenSchemaShapeAndValidation(t *testing.T) {
	s := schemaAt(t, gameui.ScreenSchemaURL)
	if s.Schema != jsonschema.Draft07 || s.ID != gameui.ScreenSchemaURL {
		t.Fatalf("$schema %q, $id %q", s.Schema, s.ID)
	}
	if version := s.Property("version"); version == nil || version.Const != int64(gameui.ScreenFormatVersion) {
		t.Fatalf("version property = %+v", version)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("screen schema accepts unknown top-level keys")
	}
	for _, p := range s.Properties {
		if p.Schema.Description == "" {
			t.Errorf("ScreenDoc.%s has no description", p.Name)
		}
	}
	for _, defName := range []string{"ScreenNodeDoc", "ScreenOffsetDoc", "ScreenPaddingDoc"} {
		def := s.Definitions[defName]
		if def == nil || def.Description == "" {
			t.Fatalf("definition %s = %+v, want one with a description", defName, def)
		}
		if b, ok := def.AdditionalProperties.Boolean(); !ok || b {
			t.Fatalf("definition %s accepts unknown keys", defName)
		}
		for _, p := range def.Properties {
			if p.Schema.Description == "" {
				t.Errorf("%s.%s has no description", defName, p.Name)
			}
		}
	}
	for _, name := range []string{"hud.screen.yaml", "pause.screen.yaml"} {
		data, err := os.ReadFile(filepath.Join(root, "gameui", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ValidateYAML(data); err != nil {
			t.Fatalf("screen schema refused %s: %v", name, err)
		}
	}
	badDocs := []string{
		"version: 1\ndesignWidth: 640\ndesignHeight: 360\nunknownKey: true\n",
		"version: 1\ndesignWidth: 0\ndesignHeight: 360\n",
		"version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: a\n    type: unknown\n",
		"version: 1\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: a\n    type: container\n    deviceClasses: [watch]\n",
	}
	for _, bad := range badDocs {
		if err := s.ValidateYAML([]byte(bad)); err == nil {
			t.Fatalf("screen schema accepted invalid YAML:\n%s", bad)
		}
	}
}
func TestStatesSchemaShapeAndValidation(t *testing.T) {
	s := schemaAt(t, animclip.StateMachineSchemaURL)
	if s.Schema != jsonschema.Draft07 || s.ID != animclip.StateMachineSchemaURL {
		t.Fatalf("$schema %q, $id %q", s.Schema, s.ID)
	}
	if version := s.Property("version"); version == nil || version.Const != int64(animclip.StateMachineVersion) {
		t.Fatalf("version property = %+v", version)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("states schema accepts unknown top-level keys")
	}
	for _, p := range s.Properties {
		if p.Schema.Description == "" {
			t.Errorf("StateMachineDoc.%s has no description", p.Name)
		}
	}
	for _, defName := range []string{"ParamDoc", "StateDoc", "TransitionDoc"} {
		def := s.Definitions[defName]
		if def == nil || def.Description == "" {
			t.Fatalf("definition %s = %+v, want one with a description", defName, def)
		}
		if b, ok := def.AdditionalProperties.Boolean(); !ok || b {
			t.Fatalf("definition %s accepts unknown keys", defName)
		}
		for _, p := range def.Properties {
			if p.Schema.Description == "" {
				t.Errorf("%s.%s has no description", defName, p.Name)
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "animclip", "testdata", "knight.states.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateYAML(data); err != nil {
		t.Fatalf("states schema refused testdata/knight.states.yaml: %v", err)
	}
	badDocs := []string{
		"version: 1\nentry: idle\nunknownKey: true\nstates:\n  - name: idle\n    clip: a.animation.yaml\n",
		"version: 1\nentry: idle\nparameters:\n  - name: x\n    type: string\nstates:\n  - name: idle\n    clip: a.animation.yaml\n",
		"version: 1\nentry: idle\nparameters:\n  - name: x\n    type: float\n    default: nope\nstates:\n  - name: idle\n    clip: a.animation.yaml\n",
	}
	for _, bad := range badDocs {
		if err := s.ValidateYAML([]byte(bad)); err == nil {
			t.Fatalf("states schema accepted invalid YAML:\n%s", bad)
		}
	}
}

func TestParticlesSchemaShapeAndValidation(t *testing.T) {
	s := schemaAt(t, particles.SchemaURL)
	if s.Schema != jsonschema.Draft07 || s.ID != particles.SchemaURL {
		t.Fatalf("$schema %q, $id %q", s.Schema, s.ID)
	}
	if version := s.Property("version"); version == nil || version.Const != int64(particles.FormatVersion) {
		t.Fatalf("version property = %+v", version)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("particles schema accepts unknown top-level keys")
	}
	for _, p := range s.Properties {
		if p.Schema.Description == "" {
			t.Errorf("EmitterDoc.%s has no description", p.Name)
		}
	}
	for _, defName := range []string{"ShapeDoc", "BurstDoc", "FloatStopDoc", "ColorStopDoc", "CollisionDoc", "TrailDoc", "SubEmitterDoc", "EmitterFieldsDoc"} {
		def := s.Definitions[defName]
		if def == nil || def.Description == "" {
			t.Fatalf("definition %s = %+v, want one with a description", defName, def)
		}
		if b, ok := def.AdditionalProperties.Boolean(); !ok || b {
			t.Fatalf("definition %s accepts unknown keys", defName)
		}
		for _, p := range def.Properties {
			if p.Schema.Description == "" {
				t.Errorf("%s.%s has no description", defName, p.Name)
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "particles", "testdata", "sparks.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateYAML(data); err != nil {
		t.Fatalf("particles schema refused sparks.yaml: %v", err)
	}
	for _, bad := range []string{
		"version: 1\nunknownKey: true\n",
		"version: 2\nrate: 10\n",
		"version: 1\nspace: universe\n",
		"version: 1\nshape:\n  kind: triangle\n",
		"version: 1\nsubEmitters:\n  - trigger: birth\n    config:\n      version: 1\n",
	} {
		if err := s.ValidateYAML([]byte(bad)); err == nil {
			t.Fatalf("particles schema accepted invalid YAML:\n%s", bad)
		}
	}
}

func TestPermissionsSchemaShapeAndValidation(t *testing.T) {
	s := schemaAt(t, editor.PermissionsSchemaURL)
	if s.Schema != jsonschema.Draft07 || s.ID != editor.PermissionsSchemaURL {
		t.Fatalf("$schema %q, $id %q", s.Schema, s.ID)
	}
	if version := s.Property("version"); version == nil || version.Const != int64(editor.PermissionsVersion) {
		t.Fatalf("version property = %+v", version)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("permissions schema accepts unknown top-level keys")
	}
	for _, p := range s.Properties {
		if p.Schema.Description == "" {
			t.Errorf("PermissionsFile.%s has no description", p.Name)
		}
	}
	ruleDef := s.Definitions["PermissionRule"]
	if ruleDef == nil || ruleDef.Description == "" {
		t.Fatalf("definition PermissionRule = %+v, want one with description", ruleDef)
	}
	if b, ok := ruleDef.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("definition PermissionRule accepts unknown keys")
	}
	data, err := os.ReadFile(filepath.Join(root, "editor", "testdata", "permissions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateYAML(data); err != nil {
		t.Fatalf("permissions schema refused testdata/permissions.yaml: %v", err)
	}
	for _, bad := range []string{
		"version: 1\nprofile: balanced\nunknownKey: true\nrules: []\n",
		"version: 2\nprofile: balanced\nrules: []\n",
		"version: 1\nprofile: reckless\nrules: []\n",
		"version: 1\nprofile: balanced\nrules:\n  - action: explode\n    patterns: ['**']\n    decision: deny\n",
		"version: 1\nprofile: balanced\nrules:\n  - action: edit\n    patterns: []\n    decision: allow\n",
		"version: 1\nprofile: balanced\nrules:\n  - action: edit\n    patterns: ['**']\n    decision: maybe\n",
	} {
		if err := s.ValidateYAML([]byte(bad)); err == nil {
			t.Fatalf("permissions schema accepted invalid YAML:\n%s", bad)
		}
	}
}
