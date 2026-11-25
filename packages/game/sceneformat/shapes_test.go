package sceneformat_test

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func shapeRegistry(t testing.TB) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{
		Name: "shapes",
		Doc:  "World geometry shapes.",
		Fields: []sceneformat.Field{
			{Name: "anchor", Kind: sceneformat.FieldPoint, Default: sceneformat.Point{X: 4, Y: -2}, Doc: "Local anchor point."},
			{Name: "box", Kind: sceneformat.FieldRect, Default: sceneformat.Rect{X: -8, Y: -8, Width: 16, Height: 16}, Doc: "Bounding box."},
			{Name: "radius", Kind: sceneformat.FieldCircle, Default: sceneformat.Circle{X: 0, Y: 0, Radius: 12}, Doc: "Detection circle."},
			{Name: "poly", Kind: sceneformat.FieldPolygon, Default: []sceneformat.Point{{X: 0, Y: 0}, {X: 16, Y: 0}, {X: 8, Y: 16}}, Doc: "Collision polygon."},
		},
	}); err != nil {
		t.Fatalf("Register shapes: %v", err)
	}
	return reg
}

func TestShapeFieldKindsStringsAndPredicates(t *testing.T) {
	cases := []struct {
		kind    sceneformat.FieldKind
		want    string
		isShape bool
	}{
		{sceneformat.FieldBool, "bool", false},
		{sceneformat.FieldInt, "int", false},
		{sceneformat.FieldFloat, "float", false},
		{sceneformat.FieldString, "string", false},
		{sceneformat.FieldStringList, "[]string", false},
		{sceneformat.FieldPoint, "point", true},
		{sceneformat.FieldRect, "rect", true},
		{sceneformat.FieldCircle, "circle", true},
		{sceneformat.FieldPolygon, "polygon", true},
	}
	for _, tc := range cases {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("%v.String() = %q, want %q", tc.kind, got, tc.want)
		}
		if got := tc.kind.IsShape(); got != tc.isShape {
			t.Errorf("%v.IsShape() = %v, want %v", tc.kind, got, tc.isShape)
		}
		if tc.isShape && tc.kind.IsNumeric() {
			t.Errorf("%v.IsNumeric() = true, want false", tc.kind)
		}
	}
}

func TestShapeFieldsYAMLRoundTripAndResolve(t *testing.T) {
	reg := shapeRegistry(t)

	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Shapes",
		Layers: []sceneformat.LayerDoc{{
			Name: "main",
			Objects: []sceneformat.ObjectDoc{{
				ID: "hero",
				X:  32,
				Y:  48,
				Components: map[string]any{
					"shapes": map[string]any{
						"anchor": sceneformat.Point{X: 5.5, Y: -3},
						"box":    sceneformat.Rect{X: -10, Y: -12, Width: 20, Height: 24},
						"radius": sceneformat.Circle{X: 2, Y: 3, Radius: 14.5},
						"poly": []sceneformat.Point{
							{X: 0, Y: 0},
							{X: 20, Y: 0},
							{X: 10, Y: 18},
						},
					},
				},
			}},
		}},
	}

	first, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode 1: %v", err)
	}
	parsed, err := sceneformat.ParseWith(first, sceneformat.ParseOptions{File: "shapes.scene.yaml", Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v\nYAML:\n%s", err, first)
	}
	second, err := sceneformat.Encode(parsed)
	if err != nil {
		t.Fatalf("Encode 2: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("round trip changed YAML:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}

	raw := parsed.Layers[0].Objects[0].Components["shapes"].(map[string]any)
	resolved, err := reg.Resolve("shapes", raw)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := resolved["anchor"]; got != (sceneformat.Point{X: 5.5, Y: -3}) {
		t.Fatalf("anchor = %#v", got)
	}
	if got := resolved["box"]; got != (sceneformat.Rect{X: -10, Y: -12, Width: 20, Height: 24}) {
		t.Fatalf("box = %#v", got)
	}
	if got := resolved["radius"]; got != (sceneformat.Circle{X: 2, Y: 3, Radius: 14.5}) {
		t.Fatalf("radius = %#v", got)
	}
	wantPoly := []sceneformat.Point{{X: 0, Y: 0}, {X: 20, Y: 0}, {X: 10, Y: 18}}
	if !reflect.DeepEqual(resolved["poly"], wantPoly) {
		t.Fatalf("poly = %#v, want %#v", resolved["poly"], wantPoly)
	}

	// Empty component resolves all defaults without mutating registry defaults.
	defResolved, err := reg.Resolve("shapes", map[string]any{})
	if err != nil {
		t.Fatalf("Resolve defaults: %v", err)
	}
	defPoly := defResolved["poly"].([]sceneformat.Point)
	defPoly[0].X = 999
	defResolved2, err := reg.Resolve("shapes", map[string]any{})
	if err != nil {
		t.Fatalf("Resolve defaults 2: %v", err)
	}
	if defResolved2["poly"].([]sceneformat.Point)[0].X != 0 {
		t.Fatal("mutating Resolve polygon mutated the registry default")
	}
}

func TestShapeFieldValidationErrors(t *testing.T) {
	reg := shapeRegistry(t)

	// Registration rejects Min/Max/Step/Enum on shape kinds and invalid defaults.
	badTypes := []struct {
		ct   sceneformat.ComponentType
		want string
	}{
		{sceneformat.ComponentType{Name: "a", Fields: []sceneformat.Field{{Name: "p", Kind: sceneformat.FieldPoint, Min: sceneformat.Float64(0)}}}, "cannot have a range"},
		{sceneformat.ComponentType{Name: "a", Fields: []sceneformat.Field{{Name: "r", Kind: sceneformat.FieldRect, Step: 1}}}, "cannot have a step"},
		{sceneformat.ComponentType{Name: "a", Fields: []sceneformat.Field{{Name: "c", Kind: sceneformat.FieldCircle, Enum: []string{"x"}}}}, "cannot have an enum"},
		{sceneformat.ComponentType{Name: "a", Fields: []sceneformat.Field{{Name: "r", Kind: sceneformat.FieldRect, Default: sceneformat.Rect{Width: -1}}}}, "width -1 must be >= 0"},
		{sceneformat.ComponentType{Name: "a", Fields: []sceneformat.Field{{Name: "c", Kind: sceneformat.FieldCircle, Default: sceneformat.Circle{Radius: -2}}}}, "radius -2 must be >= 0"},
		{sceneformat.ComponentType{Name: "a", Fields: []sceneformat.Field{{Name: "p", Kind: sceneformat.FieldPoint, Default: sceneformat.Point{X: math.NaN()}}}}, "must be finite"},
		{sceneformat.ComponentType{Name: "a", Fields: []sceneformat.Field{{Name: "poly", Kind: sceneformat.FieldPolygon, Default: "bad"}}}, "want polygon"},
	}
	for _, tc := range badTypes {
		r := sceneformat.NewComponentRegistry()
		err := r.Register(tc.ct)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Register(%+v) = %v, want substring %q", tc.ct, err, tc.want)
		}
	}

	// Zero-value defaults register cleanly.
	zeroReg := sceneformat.NewComponentRegistry()
	if err := zeroReg.Register(sceneformat.ComponentType{
		Name: "zero",
		Fields: []sceneformat.Field{
			{Name: "p", Kind: sceneformat.FieldPoint},
			{Name: "r", Kind: sceneformat.FieldRect},
			{Name: "c", Kind: sceneformat.FieldCircle},
			{Name: "g", Kind: sceneformat.FieldPolygon},
		},
	}); err != nil {
		t.Fatalf("Register zero-default shape fields: %v", err)
	}

	// YAML ParseWith errors report line and column on ComponentError.
	badYAML := []struct {
		name string
		body string
		want string
	}{
		{"point unknown key", "anchor: {x: 1, z: 2}", `unknown key "z"`},
		{"point bad type", "anchor: 12", "want point"},
		{"rect negative width", "box: {x: 0, y: 0, width: -4, height: 8}", "width -4 must be >= 0"},
		{"rect unknown key", "box: {x: 0, y: 0, w: 10}", `unknown key "w"`},
		{"circle negative radius", "radius: {x: 0, y: 0, radius: -1}", "radius -1 must be >= 0"},
		{"circle unknown key", "radius: {x: 0, y: 0, r: 5}", `unknown key "r"`},
		{"polygon bad element", "poly: [{x: 0, y: 0}, 42]", "point 1: want point"},
		{"polygon unknown key", "poly: [{x: 0, z: 1}]", `point 0: unknown key "z"`},
		{"polygon not list", "poly: {x: 0, y: 0}", "want polygon"},
	}
	for _, tc := range badYAML {
		t.Run(tc.name, func(t *testing.T) {
			src := "version: 2\nname: Bad\nlayers:\n  - name: main\n    objects:\n      - id: o\n        components:\n          shapes:\n            " + tc.body + "\n"
			_, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{File: "bad.scene.yaml", Components: reg})
			var ce *sceneformat.ComponentError
			if !errors.As(err, &ce) {
				t.Fatalf("ParseWith err = %v (%T), want *ComponentError", err, err)
			}
			if ce.File != "bad.scene.yaml" || ce.Line != 9 || ce.Object != "o" || ce.Component != "shapes" {
				t.Fatalf("ComponentError = %+v", ce)
			}
			if !strings.Contains(ce.Msg, tc.want) {
				t.Fatalf("Msg = %q, want substring %q", ce.Msg, tc.want)
			}
		})
	}
}

func TestShapeHelpersNormalizeNegativeZero(t *testing.T) {
	negZero := math.Copysign(0, -1)
	p, err := sceneformat.ParsePoint(sceneformat.Point{X: negZero, Y: negZero})
	if err != nil {
		t.Fatalf("ParsePoint: %v", err)
	}
	if math.Signbit(p.X) || math.Signbit(p.Y) {
		t.Fatalf("negative zero not normalized: %+v", p)
	}
	r, err := sceneformat.ParseRect(map[string]any{"x": negZero, "y": 1, "width": 10, "height": 20})
	if err != nil || math.Signbit(r.X) {
		t.Fatalf("ParseRect = %+v, %v", r, err)
	}
	c, err := sceneformat.ParseCircle(map[string]any{"x": 0, "y": negZero, "radius": 5})
	if err != nil || math.Signbit(c.Y) {
		t.Fatalf("ParseCircle = %+v, %v", c, err)
	}
	pts, err := sceneformat.ParsePolygon([]any{map[string]any{"x": negZero, "y": 2}})
	if err != nil || len(pts) != 1 || math.Signbit(pts[0].X) {
		t.Fatalf("ParsePolygon = %+v, %v", pts, err)
	}
}
