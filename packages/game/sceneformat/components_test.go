package sceneformat_test

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func testComponents(t testing.TB) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{
		Name: "body",
		Doc:  "A moving body.",
		Fields: []sceneformat.Field{
			{Name: "solid", Kind: sceneformat.FieldBool, Default: true, Doc: "Blocks other bodies."},
			{Name: "speed", Kind: sceneformat.FieldFloat, Default: float32(32), Min: sceneformat.Float64(0), Max: sceneformat.Float64(500), Doc: "Top speed."},
			{Name: "layer", Kind: sceneformat.FieldInt, Default: 1, Min: sceneformat.Float64(0), Max: sceneformat.Float64(31)},
			{Name: "tag", Kind: sceneformat.FieldString},
		},
	}); err != nil {
		t.Fatalf("Register body: %v", err)
	}
	if err := reg.Register(sceneformat.ComponentType{Name: "marker"}); err != nil {
		t.Fatalf("Register marker: %v", err)
	}
	return reg
}

// componentScene is written exactly as Encode writes it, so a load and save must reproduce it byte for byte.
const componentScene = `version: 1
name: Room
layers:
  - name: actors
    objects:
      - id: hero
        x: 10
        components:
          body:
            layer: 3
            speed: 48.5
          marker: {}
      - id: slime
        components:
          body:
            solid: false
`

func TestParseWithComponentsRoundTripsWithoutWritingDefaults(t *testing.T) {
	reg := testComponents(t)
	doc, err := sceneformat.ParseWith([]byte(componentScene), sceneformat.ParseOptions{File: "room.scene.yaml", Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	out, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(out) != componentScene {
		t.Fatalf("round trip changed the file:\n--- got ---\n%s\n--- want ---\n%s", out, componentScene)
	}

	resolved, err := reg.Resolve("body", doc.Layers[0].Objects[0].Components["body"].(map[string]any))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := map[string]any{"solid": true, "speed": 48.5, "layer": 3, "tag": ""}
	if !reflect.DeepEqual(resolved, want) {
		t.Fatalf("Resolve = %#v, want %#v", resolved, want)
	}
}

func TestParseWithMisspelledFieldReportsFileAndLine(t *testing.T) {
	src := strings.Replace(componentScene, "            speed: 48.5", "            sped: 48.5", 1)
	_, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{File: "scenes/room.scene.yaml", Components: testComponents(t)})
	var ce *sceneformat.ComponentError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v (%T), want *ComponentError", err, err)
	}
	if ce.File != "scenes/room.scene.yaml" || ce.Line != 11 || ce.Column != 13 {
		t.Fatalf("position = %s:%d:%d, want scenes/room.scene.yaml:11:13", ce.File, ce.Line, ce.Column)
	}
	if ce.Object != "hero" || ce.Component != "body" || ce.Field != "sped" {
		t.Fatalf("error names object %q component %q field %q", ce.Object, ce.Component, ce.Field)
	}
	const want = `scenes/room.scene.yaml:11:13: sceneformat: object "hero" component "body" field "sped": unknown field (registered: solid, speed, layer, tag)`
	if err.Error() != want {
		t.Fatalf("message:\n got %s\nwant %s", err, want)
	}
}

func TestParseWithComponentErrors(t *testing.T) {
	reg := testComponents(t)
	cases := []struct {
		name      string
		old, new  string
		line, col int // col 0 skips the column check
		contains  string
	}{
		{"unknown type", "          marker: {}", "          markr: {}", 12, 11, `component "markr": unknown component type (registered: body, marker)`},
		{"wrong kind", "            solid: false", "            solid: \"no\"", 16, 20, `field "solid": want bool, got "no"`},
		{"int given float", "            layer: 3", "            layer: 3.0", 10, 20, `field "layer": want int, got float 3`},
		{"below minimum", "            layer: 3", "            layer: -1", 10, 20, `value -1 is below the minimum 0`},
		{"above maximum", "            speed: 48.5", "            speed: 900", 11, 20, `value 900 is above the maximum 500`},
		{"not finite", "            speed: 48.5", "            speed: .nan", 11, 20, `must be finite`},
		{"not a mapping", "          marker: {}", "          marker: 3", 12, 19, `component "marker": must be a mapping of fields, got int 3`},
		{"null component", "          marker: {}", "          marker:", 12, 0, `must be a mapping of fields, got null`},
		{"field on empty type", "          marker: {}", "          marker: {x: 1}", 12, 20, `field "x": unknown field (the type has no fields)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(componentScene, tc.old, tc.new, 1)
			if src == componentScene {
				t.Fatalf("replacement %q did not apply", tc.old)
			}
			_, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{File: "f.yaml", Components: reg})
			var ce *sceneformat.ComponentError
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v, want *ComponentError", err)
			}
			if ce.Line != tc.line || (tc.col != 0 && ce.Column != tc.col) {
				t.Fatalf("position %d:%d, want %d:%d (%v)", ce.Line, ce.Column, tc.line, tc.col, err)
			}
			if !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("error %q does not contain %q", err, tc.contains)
			}
		})
	}
}

func TestParseWithoutRegistryKeepsFreeFormComponents(t *testing.T) {
	src := strings.Replace(componentScene, "          marker: {}", "          anything: {x: [1, 2]}", 1)
	if _, err := sceneformat.Parse([]byte(src)); err != nil {
		t.Fatalf("Parse without registry: %v", err)
	}
	if _, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{File: "f.yaml"}); err != nil {
		t.Fatalf("ParseWith without registry: %v", err)
	}
}

func TestParseWithPrefixesOtherErrorsWithFile(t *testing.T) {
	_, err := sceneformat.ParseWith([]byte("version: 1\nnmae: x\n"), sceneformat.ParseOptions{File: "bad.yaml", Components: testComponents(t)})
	if err == nil || !strings.HasPrefix(err.Error(), "bad.yaml: sceneformat: decode:") || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v, want a decode error prefixed with the file and naming line 2", err)
	}
}

func TestParseWithMergeKeysAreStillChecked(t *testing.T) {
	src := `version: 1
layers:
  - name: actors
    objects:
      - id: a
        components:
          body: &base
            speed: 10
      - id: b
        components:
          body:
            <<: *base
            sped: 2
      - id: c
        components:
          body:
            <<: {sped: 2}
`
	_, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{File: "m.yaml", Components: testComponents(t)})
	var ce *sceneformat.ComponentError
	if !errors.As(err, &ce) || ce.Object != "b" || ce.Line != 13 || ce.Field != "sped" {
		t.Fatalf("err = %v, want object b field sped at line 13", err)
	}

	src = strings.Replace(src, "            sped: 2\n", "", 1)
	_, err = sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{File: "m.yaml", Components: testComponents(t)})
	if !errors.As(err, &ce) || ce.Object != "c" || ce.Field != "sped" || ce.File != "m.yaml" {
		t.Fatalf("err = %v, want object c field sped caught through the merge key", err)
	}
}

func TestValidateComponentsAndCheckField(t *testing.T) {
	reg := testComponents(t)
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{{Name: "l", Objects: []sceneformat.ObjectDoc{
			{ID: "a", Components: map[string]any{"body": map[string]any{"speed": float32(4), "layer": int64(2)}}},
			{ID: "b", Components: map[string]any{"body": map[string]any{"speed": 4, "zzz": 1, "aaa": 1}}},
		}}},
	}
	err := sceneformat.ValidateComponents(doc, reg)
	var ce *sceneformat.ComponentError
	if !errors.As(err, &ce) || ce.Object != "b" || ce.Field != "aaa" || ce.Line != 0 {
		t.Fatalf("ValidateComponents = %v, want the first unknown field of b in name order", err)
	}
	if err := sceneformat.ValidateComponents(doc, nil); err != nil {
		t.Fatalf("nil registry: %v", err)
	}

	if v, err := reg.CheckField("body", "speed", 7); err != nil || v != 7.0 {
		t.Fatalf("CheckField float from int = %v, %v; want 7.0", v, err)
	}
	if v, err := reg.CheckField("body", "layer", uint8(5)); err != nil || v != 5 {
		t.Fatalf("CheckField int from uint8 = %v, %v; want 5", v, err)
	}
	if _, err := reg.CheckField("body", "layer", uint64(1)<<63); err == nil {
		t.Fatal("CheckField accepted a uint64 that overflows int")
	}
	if _, err := reg.CheckField("nope", "x", 1); err == nil || !strings.Contains(err.Error(), "unknown component type") {
		t.Fatalf("CheckField unknown type = %v", err)
	}
}

func TestRegisterRefusesBadTypes(t *testing.T) {
	cases := []struct {
		name string
		typ  sceneformat.ComponentType
		want string
	}{
		{"empty name", sceneformat.ComponentType{}, "empty name"},
		{"duplicate type", sceneformat.ComponentType{Name: "body"}, "already registered"},
		{"empty field", sceneformat.ComponentType{Name: "x", Fields: []sceneformat.Field{{Kind: sceneformat.FieldBool}}}, "field 0 has an empty name"},
		{"duplicate field", sceneformat.ComponentType{Name: "x", Fields: []sceneformat.Field{{Name: "a", Kind: sceneformat.FieldBool}, {Name: "a", Kind: sceneformat.FieldInt}}}, `duplicate field "a"`},
		{"unknown kind", sceneformat.ComponentType{Name: "x", Fields: []sceneformat.Field{{Name: "a"}}}, "unknown kind"},
		{"range on string", sceneformat.ComponentType{Name: "x", Fields: []sceneformat.Field{{Name: "a", Kind: sceneformat.FieldString, Min: sceneformat.Float64(0)}}}, "cannot have a range"},
		{"min above max", sceneformat.ComponentType{Name: "x", Fields: []sceneformat.Field{{Name: "a", Kind: sceneformat.FieldInt, Min: sceneformat.Float64(2), Max: sceneformat.Float64(1)}}}, "above max"},
		{"default kind", sceneformat.ComponentType{Name: "x", Fields: []sceneformat.Field{{Name: "a", Kind: sceneformat.FieldInt, Default: "1"}}}, "want int"},
		{"zero default out of range", sceneformat.ComponentType{Name: "x", Fields: []sceneformat.Field{{Name: "a", Kind: sceneformat.FieldFloat, Min: sceneformat.Float64(1)}}}, "below the minimum 1"},
		{"fractional int range", sceneformat.ComponentType{Name: "x", Fields: []sceneformat.Field{{Name: "a", Kind: sceneformat.FieldInt, Min: sceneformat.Float64(0.5), Max: sceneformat.Float64(31.5), Default: 1}}}, "range must be whole numbers"},
		{"step on bool", sceneformat.ComponentType{Name: "x", Fields: []sceneformat.Field{{Name: "a", Kind: sceneformat.FieldBool, Step: 1}}}, "cannot have a step"},
		{"negative step", sceneformat.ComponentType{Name: "x", Fields: []sceneformat.Field{{Name: "a", Kind: sceneformat.FieldFloat, Step: -0.1}}}, "step must be positive and finite"},
		{"fractional int step", sceneformat.ComponentType{Name: "x", Fields: []sceneformat.Field{{Name: "a", Kind: sceneformat.FieldInt, Step: 0.5}}}, "step must be a whole number"},
	}
	reg := testComponents(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := reg.Register(tc.typ)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Register = %v, want an error containing %q", err, tc.want)
			}
		})
	}
	if got := reg.Names(); !reflect.DeepEqual(got, []string{"body", "marker"}) {
		t.Fatalf("failed registrations changed the registry: %v", got)
	}
}

func TestLookupReturnsNormalizedCopies(t *testing.T) {
	reg := testComponents(t)
	body, ok := reg.Lookup("body")
	if !ok {
		t.Fatal("Lookup body failed")
	}
	if got := body.FieldNames(); !reflect.DeepEqual(got, []string{"solid", "speed", "layer", "tag"}) {
		t.Fatalf("FieldNames = %v", got)
	}
	speed, _ := body.Field("speed")
	if speed.Default != 32.0 || *speed.Max != 500 || speed.Doc != "Top speed." {
		t.Fatalf("speed field = %#v", speed)
	}
	if tag, _ := body.Field("tag"); tag.Default != "" {
		t.Fatalf("tag default = %#v, want the empty string", tag.Default)
	}
	*body.Fields[1].Max = 1
	body.Fields[0].Name = "changed"
	again, _ := reg.Lookup("body")
	if f, _ := again.Field("speed"); *f.Max != 500 {
		t.Fatal("mutating a Lookup copy changed the registry")
	}
	if _, ok := again.Field("solid"); !ok {
		t.Fatal("mutating a Lookup copy renamed a registered field")
	}
	if types := reg.Types(); len(types) != 2 || types[0].Name != "body" || types[1].Name != "marker" {
		t.Fatalf("Types = %v", types)
	}
	var nilReg *sceneformat.ComponentRegistry
	if _, ok := nilReg.Lookup("body"); ok || nilReg.Names() != nil {
		t.Fatal("a nil registry must look up nothing")
	}
}

func FuzzParseWithComponents(f *testing.F) {
	f.Add([]byte(componentScene))
	f.Add([]byte(strings.Replace(componentScene, "speed", "sped", 1)))
	f.Add([]byte("version: 1\nlayers:\n  - name: a\n    objects:\n      - id: x\n        components: {body: {<<: {layer: 99}}}\n"))
	reg := testComponents(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: "fuzz.yaml", Components: reg})
		if err != nil {
			return
		}
		out, err := sceneformat.Encode(doc)
		if err != nil {
			t.Fatalf("Encode of a parsed scene: %v", err)
		}
		again, err := sceneformat.ParseWith(out, sceneformat.ParseOptions{Components: reg})
		if err != nil {
			t.Fatalf("re-parse of encoded scene: %v\n%s", err, out)
		}
		out2, err := sceneformat.Encode(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("encoding is not stable: %v\n%s\n---\n%s", err, out, out2)
		}
	})
}
