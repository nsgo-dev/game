package sceneformat_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/jsonschema"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

func TestComponentRegistryMerge(t *testing.T) {
	base := sceneformat.NewComponentRegistry()
	base.MustRegister(sceneformat.ComponentType{
		Name: "health",
		Doc:  "Game health.",
		Fields: []sceneformat.Field{
			{Name: "hp", Kind: sceneformat.FieldInt, Default: 10, Min: sceneformat.Float64(0)},
		},
	})

	plugin := sceneformat.NewComponentRegistry()
	plugin.MustRegister(sceneformat.ComponentType{
		Name: "mana",
		Doc:  "Plugin mana.",
		Fields: []sceneformat.Field{
			{Name: "mp", Kind: sceneformat.FieldInt, Default: 5},
		},
	})
	plugin.MustRegister(sceneformat.ComponentType{
		Name:   "aura",
		Events: []sceneformat.Event{{Name: "tick"}},
	})

	if err := base.Merge(plugin); err != nil {
		t.Fatalf("Merge disjoint: %v", err)
	}
	if got, want := base.Names(), []string{"aura", "health", "mana"}; !slices.Equal(got, want) {
		t.Fatalf("Names = %v, want %v", got, want)
	}
	if ct, ok := base.Lookup("mana"); !ok || ct.Doc != "Plugin mana." || len(ct.Fields) != 1 || ct.Fields[0].Default != 5 {
		t.Fatalf("Lookup(mana) = %+v, %t", ct, ok)
	}

	// Mutating plugin after Merge does not affect base.
	plugin.MustRegister(sceneformat.ComponentType{Name: "extra"})
	if _, ok := base.Lookup("extra"); ok {
		t.Fatal("mutating source registry affected merged destination")
	}

	// Nil and empty merges are no-ops; merging into a zero-value registry works.
	if err := base.Merge(nil); err != nil {
		t.Fatalf("Merge(nil): %v", err)
	}
	if err := base.Merge(sceneformat.NewComponentRegistry()); err != nil {
		t.Fatalf("Merge(empty): %v", err)
	}
	var zero sceneformat.ComponentRegistry
	if err := zero.Merge(base); err != nil {
		t.Fatalf("Merge into zero registry: %v", err)
	}
	if !slices.Equal(zero.Names(), base.Names()) {
		t.Fatalf("zero.Names() = %v, want %v", zero.Names(), base.Names())
	}

	// Nil receiver is refused.
	var nilReg *sceneformat.ComponentRegistry
	if err := nilReg.Merge(base); err == nil {
		t.Fatal("Merge on nil receiver succeeded")
	}
}

func TestComponentRegistryMergeCollisionIsAllOrNothing(t *testing.T) {
	dst := sceneformat.NewComponentRegistry()
	dst.MustRegister(sceneformat.ComponentType{
		Name: "health",
		Doc:  "Player health.",
		Fields: []sceneformat.Field{
			{Name: "hp", Kind: sceneformat.FieldInt, Default: 3},
		},
	})

	src := sceneformat.NewComponentRegistry()
	src.MustRegister(sceneformat.ComponentType{
		Name: "alpha",
		Doc:  "Should not be added on collision.",
	})
	src.MustRegister(sceneformat.ComponentType{
		Name: "health",
		Doc:  "Plugin health.",
		Fields: []sceneformat.Field{
			{Name: "max", Kind: sceneformat.FieldInt, Default: 100},
		},
	})
	src.MustRegister(sceneformat.ComponentType{
		Name: "zebra",
		Doc:  "Also should not be added.",
	})

	err := dst.Merge(src)
	if err == nil {
		t.Fatal("Merge with colliding name succeeded")
	}
	msg := err.Error()
	for _, want := range []string{`"health"`, "existing:", "Player health.", "fields: hp", "incoming:", "Plugin health.", "fields: max"} {
		if !strings.Contains(msg, want) {
			t.Errorf("collision error %q missing %q", msg, want)
		}
	}

	// All-or-nothing: neither alpha nor zebra was added, and health kept its original definition.
	if got := dst.Names(); !slices.Equal(got, []string{"health"}) {
		t.Fatalf("dst.Names() after failed Merge = %v, want [health]", got)
	}
	ct, ok := dst.Lookup("health")
	if !ok || ct.Doc != "Player health." || !slices.Equal(ct.FieldNames(), []string{"hp"}) {
		t.Fatalf("dst health changed after failed Merge: %+v", ct)
	}
}

func allFieldKindsComponent() sceneformat.ComponentType {
	return sceneformat.ComponentType{
		Name: "everything",
		Doc:  "Exercises every FieldKind.",
		Fields: []sceneformat.Field{
			{Name: "flag", Kind: sceneformat.FieldBool, Default: true, Doc: "A boolean."},
			{Name: "count", Kind: sceneformat.FieldInt, Default: 3, Min: sceneformat.Float64(0), Max: sceneformat.Float64(10), Doc: "An integer."},
			{Name: "ratio", Kind: sceneformat.FieldFloat, Default: 0.5, Min: sceneformat.Float64(0), Max: sceneformat.Float64(1), Doc: "A float."},
			{Name: "mode", Kind: sceneformat.FieldString, Default: "fast", Enum: []string{"slow", "fast"}, Doc: "An enum string."},
			{Name: "tags", Kind: sceneformat.FieldStringList, Default: []string{"a", "b"}, Doc: "A list of strings."},
			{Name: "unsetTags", Kind: sceneformat.FieldStringList, Doc: "An unset list of strings."},
			{Name: "verts", Kind: sceneformat.FieldPointList, Default: [][2]float64{{0, 0}, {4, 2}}, Doc: "A list of [x, y] pairs."},
			{Name: "unsetVerts", Kind: sceneformat.FieldPointList, Doc: "An unset point list."},
			{Name: "anchor", Kind: sceneformat.FieldPoint, Default: sceneformat.Point{X: 1, Y: 2}, Doc: "A local point."},
			{Name: "box", Kind: sceneformat.FieldRect, Default: sceneformat.Rect{X: -4, Y: -4, Width: 8, Height: 8}, Doc: "A local rect."},
			{Name: "radius", Kind: sceneformat.FieldCircle, Default: sceneformat.Circle{X: 0, Y: 0, Radius: 6}, Doc: "A local circle."},
			{Name: "poly", Kind: sceneformat.FieldPolygon, Default: []sceneformat.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 5, Y: 5}}, Doc: "A local polygon."},
			{Name: "unsetPoly", Kind: sceneformat.FieldPolygon, Doc: "An unset polygon."},
		},
	}
}

func TestComponentTypeSchemaCoversEveryFieldKind(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(allFieldKindsComponent())
	ct, ok := reg.Lookup("everything")
	if !ok {
		t.Fatal("Lookup(everything) failed")
	}
	s := sceneformat.ComponentTypeSchema(ct)
	if s.Description != "Exercises every FieldKind." {
		t.Errorf("Description = %q", s.Description)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("component schema accepts unknown keys")
	}

	wantTypes := map[string]jsonschema.Type{
		"flag":       jsonschema.TypeBoolean,
		"count":      jsonschema.TypeInteger,
		"ratio":      jsonschema.TypeNumber,
		"mode":       jsonschema.TypeString,
		"tags":       jsonschema.TypeArray,
		"unsetTags":  jsonschema.TypeArray,
		"verts":      jsonschema.TypeArray,
		"unsetVerts": jsonschema.TypeArray,
		"anchor":     jsonschema.TypeObject,
		"box":        jsonschema.TypeObject,
		"radius":     jsonschema.TypeObject,
		"poly":       jsonschema.TypeArray,
		"unsetPoly":  jsonschema.TypeArray,
	}
	for name, wantType := range wantTypes {
		prop := s.Property(name)
		if prop == nil {
			t.Fatalf("missing property %q", name)
		}
		if len(prop.Types) != 1 || prop.Types[0] != wantType {
			t.Errorf("property %q types = %v, want [%s]", name, prop.Types, wantType)
		}
	}

	// Verify enum and bounds.
	if mode := s.Property("mode"); len(mode.Enum) != 2 || mode.Enum[0] != "slow" || mode.Enum[1] != "fast" {
		t.Errorf("mode enum = %v", mode.Enum)
	}
	if count := s.Property("count"); count.Minimum == nil || *count.Minimum != 0 || count.Maximum == nil || *count.Maximum != 10 {
		t.Errorf("count bounds = %v..%v", count.Minimum, count.Maximum)
	}

	// Verify defaults marshal cleanly with lowercase JSON keys, and typed-nil slices omit default.
	raw, err := s.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, raw)
	}
	propsMap := parsed["properties"].(map[string]any)
	for _, unset := range []string{"unsetTags", "unsetVerts", "unsetPoly"} {
		if _, hasDefault := propsMap[unset].(map[string]any)["default"]; hasDefault {
			t.Errorf("property %q has default, want omitted for nil slice", unset)
		}
	}
	boxDef, ok := propsMap["box"].(map[string]any)["default"].(map[string]any)
	if !ok || boxDef["x"] != float64(-4) || boxDef["y"] != float64(-4) || boxDef["width"] != float64(8) || boxDef["height"] != float64(8) {
		t.Errorf("box default = %#v", propsMap["box"].(map[string]any)["default"])
	}
	anchorDef, ok := propsMap["anchor"].(map[string]any)["default"].(map[string]any)
	if !ok || anchorDef["x"] != float64(1) || anchorDef["y"] != float64(2) {
		t.Errorf("anchor default = %#v", propsMap["anchor"].(map[string]any)["default"])
	}
	radiusDef, ok := propsMap["radius"].(map[string]any)["default"].(map[string]any)
	if !ok || radiusDef["radius"] != float64(6) {
		t.Errorf("radius default = %#v", propsMap["radius"].(map[string]any)["default"])
	}

	// Valid document passes validation.
	valid := map[string]any{
		"flag":   false,
		"count":  5,
		"ratio":  0.25,
		"mode":   "slow",
		"tags":   []any{"solid"},
		"verts":  []any{[]any{0, 0}, []any{1, 2}},
		"anchor": map[string]any{"x": 1, "y": 2},
		"box":    map[string]any{"x": 0, "y": 0, "width": 16, "height": 24},
		"radius": map[string]any{"x": 2, "y": 3, "radius": 8},
		"poly":   []any{map[string]any{"x": 0, "y": 0}, map[string]any{"x": 5, "y": 5}},
	}
	if err := s.Validate(valid); err != nil {
		t.Fatalf("valid component failed validation: %v", err)
	}

	// Invalid values for every kind fail validation.
	badCases := map[string]map[string]any{
		"bool":               {"flag": "yes"},
		"int float":          {"count": 1.5},
		"int out of range":   {"count": 11},
		"float string":       {"ratio": "half"},
		"string enum":        {"mode": "turbo"},
		"string list elem":   {"tags": []any{123}},
		"point list tuple":   {"verts": []any{[]any{1, 2, 3}}},
		"point wrong type":   {"anchor": "origin"},
		"point unknown key":  {"anchor": map[string]any{"x": 1, "z": 2}},
		"rect wrong type":    {"box": "10x10"},
		"rect negative w":    {"box": map[string]any{"width": -1}},
		"rect unknown key":   {"box": map[string]any{"w": 10}},
		"circle wrong type":  {"radius": 5},
		"circle negative r":  {"radius": map[string]any{"radius": -2}},
		"polygon wrong type": {"poly": map[string]any{"x": 0, "y": 0}},
		"polygon bad vertex": {"poly": []any{map[string]any{"x": "bad"}}},
		"unknown field":      {"nope": 1},
	}
	for name, val := range badCases {
		if err := s.Validate(val); err == nil {
			t.Errorf("%s: Validate(%#v) succeeded, want failure", name, val)
		}
	}
}

func TestComponentRegistryJSONSchemaOrderAndDeterminism(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	// Register out of alphabetical order to verify Names() ordering.
	reg.MustRegister(sceneformat.ComponentType{Name: "zeta", Fields: []sceneformat.Field{{Name: "v", Kind: sceneformat.FieldInt}}})
	reg.MustRegister(sceneformat.ComponentType{Name: "alpha", Fields: []sceneformat.Field{{Name: "ok", Kind: sceneformat.FieldBool}}})
	reg.MustRegister(sceneformat.ComponentType{Name: "mid", Fields: []sceneformat.Field{{Name: "box", Kind: sceneformat.FieldRect}}})

	s := reg.JSONSchema()
	gotNames := make([]string, len(s.Properties))
	for i, p := range s.Properties {
		gotNames[i] = p.Name
	}
	if want := []string{"alpha", "mid", "zeta"}; !slices.Equal(gotNames, want) {
		t.Fatalf("JSONSchema properties = %v, want %v", gotNames, want)
	}
	if b, ok := s.AdditionalProperties.Boolean(); !ok || b {
		t.Fatal("non-nil registry JSONSchema accepts unknown components")
	}

	first, err := s.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	second, err := reg.JSONSchema().Marshal()
	if err != nil {
		t.Fatalf("second Marshal: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("JSONSchema Marshal output is not deterministic")
	}

	// Nil registry allows arbitrary components.
	var nilReg *sceneformat.ComponentRegistry
	if err := nilReg.JSONSchema().Validate(map[string]any{"anything": map[string]any{"k": 1}}); err != nil {
		t.Fatalf("nil registry JSONSchema rejected free-form component: %v", err)
	}
}

func TestSceneSchemaValidatesRectFieldAndRejectsWrongType(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name: "hitbox",
		Doc:  "2D attack hitbox.",
		Fields: []sceneformat.Field{
			{Name: "bounds", Kind: sceneformat.FieldRect, Default: sceneformat.Rect{Width: 16, Height: 16}},
			{Name: "damage", Kind: sceneformat.FieldInt, Default: 1, Min: sceneformat.Float64(0)},
		},
	})

	s, err := sceneformat.SceneSchema(reg)
	if err != nil {
		t.Fatalf("SceneSchema: %v", err)
	}

	goodScene := `version: 2
name: Arena
layers:
  - name: actors
    objects:
      - id: hero
        components:
          hitbox:
            bounds:
              x: -8
              "y": -8
              width: 16
              height: 16
            damage: 5
`
	if err := s.ValidateYAML([]byte(goodScene)); err != nil {
		t.Fatalf("valid scene failed SceneSchema validation: %v", err)
	}

	// Wrong-typed rect field (string instead of {x, y, width, height}) fails validation.
	wrongRectType := strings.Replace(goodScene, "            bounds:\n              x: -8\n              \"y\": -8\n              width: 16\n              height: 16", "            bounds: \"not-a-rect\"", 1)
	err = s.ValidateYAML([]byte(wrongRectType))
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("wrong-typed rect ValidateYAML = %v, want *ValidationError", err)
	}
	if !strings.Contains(verr.Error(), "/layers/0/objects/0/components/hitbox/bounds") {
		t.Fatalf("unexpected error path: %v", verr)
	}

	// Rect with negative width fails validation.
	negWidth := strings.Replace(goodScene, "width: 16", "width: -4", 1)
	if err := s.ValidateYAML([]byte(negWidth)); err == nil {
		t.Fatal("negative rect width passed SceneSchema validation")
	}

	// Unregistered component fails validation when reg != nil.
	unknownComp := strings.Replace(goodScene, "          hitbox:", "          unknown:", 1)
	if err := s.ValidateYAML([]byte(unknownComp)); err == nil {
		t.Fatal("unregistered component passed SceneSchema validation")
	}

	// SceneSchema(nil) validates built-in components while still allowing custom components.
	builtin, err := sceneformat.SceneSchema(nil)
	if err != nil {
		t.Fatalf("SceneSchema(nil): %v", err)
	}
	if err := builtin.ValidateYAML([]byte(goodScene)); err != nil {
		t.Fatalf("SceneSchema(nil) rejected custom component: %v", err)
	}
	badBuiltin := `version: 1
layers:
  - name: main
    objects:
      - id: box
        components:
          body2d:
            kind: bogus
`
	if err := builtin.ValidateYAML([]byte(badBuiltin)); err == nil {
		t.Fatal("SceneSchema(nil) accepted invalid built-in body2d kind")
	}
}
