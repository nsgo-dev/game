package jsonschema_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/jsonschema"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

type Mode string

type Base struct {
	Label string `yaml:"label,omitempty"`
}

type Node struct {
	// Name names the node.
	Name     string  `yaml:"name" schema:"required,minLength=1"`
	Children []*Node `yaml:"children,omitempty"`
}

type Doc struct {
	Version  int            `yaml:"version" schema:"required,const=2"`
	Mode     Mode           `yaml:"mode,omitempty" schema:"enum=fast|slow"`
	Ratio    *float32       `yaml:"ratio,omitempty" schema:"minimum=0,maximum=1"`
	Count    uint8          `yaml:"count,omitempty"`
	Scale    float64        `yaml:"scale,omitempty" schema:"exclusiveMinimum=0,exclusiveMaximum=10"`
	Flag     bool           `yaml:"flag,omitempty"`
	Tags     []string       `yaml:"tags,omitempty" schema:"minItems=1"`
	Root     *Node          `yaml:"root,omitempty"`
	ByName   map[string]int `yaml:"byName,omitempty"`
	Extra    map[string]any `yaml:"extra,omitempty"`
	Untagged string
	Skipped  string `yaml:"-"`
	Base     `yaml:",inline"`
	hidden   string
}

func mustReflect(t *testing.T, v any, opts jsonschema.Options) *jsonschema.Schema {
	t.Helper()
	s, err := jsonschema.Reflect(reflect.TypeOf(v), opts)
	if err != nil {
		t.Fatalf("Reflect: %v", err)
	}
	return s
}

func TestReflectMarshal(t *testing.T) {
	s := mustReflect(t, Doc{}, jsonschema.Options{
		ID:    "https://example.com/doc.json",
		Title: "Doc",
		Docs: map[string]string{
			"Doc":       "A test document.",
			"Doc.Root":  "Root is the tree's root.",
			"Node.Name": "Name names the node.",
		},
	})
	got, err := s.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "$id": "https://example.com/doc.json",
  "title": "Doc",
  "description": "A test document.",
  "type": "object",
  "properties": {
    "version": {
      "type": "integer",
      "const": 2
    },
    "mode": {
      "type": "string",
      "enum": [
        "fast",
        "slow"
      ]
    },
    "ratio": {
      "type": "number",
      "minimum": 0,
      "maximum": 1
    },
    "count": {
      "type": "integer",
      "minimum": 0,
      "maximum": 255
    },
    "scale": {
      "type": "number",
      "exclusiveMinimum": 0,
      "exclusiveMaximum": 10
    },
    "flag": {
      "type": "boolean"
    },
    "tags": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "string"
      }
    },
    "root": {
      "description": "Root is the tree's root.",
      "allOf": [
        {
          "$ref": "#/definitions/Node"
        }
      ]
    },
    "byName": {
      "type": "object",
      "additionalProperties": {
        "type": "integer",
        "minimum": -9223372036854776000,
        "maximum": 9223372036854776000
      }
    },
    "extra": {
      "type": "object",
      "additionalProperties": {}
    },
    "untagged": {
      "type": "string"
    },
    "label": {
      "type": "string"
    }
  },
  "required": [
    "version"
  ],
  "additionalProperties": false,
  "definitions": {
    "Node": {
      "type": "object",
      "properties": {
        "name": {
          "description": "Name names the node.",
          "type": "string",
          "minLength": 1
        },
        "children": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/Node"
          }
        }
      },
      "required": [
        "name"
      ],
      "additionalProperties": false
    }
  }
}
`
	if string(got) != want {
		t.Fatalf("Marshal =\n%s\nwant\n%s", got, want)
	}
	again, err := mustReflect(t, Doc{}, jsonschema.Options{ID: "https://example.com/doc.json", Title: "Doc", Docs: map[string]string{"Doc": "A test document.", "Doc.Root": "Root is the tree's root.", "Node.Name": "Name names the node."}}).Marshal()
	if err != nil || string(again) != want {
		t.Fatalf("a second Reflect and Marshal differs (err %v)", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("Marshal output is not JSON: %v", err)
	}
}

type Sized struct {
	Small  int8    `yaml:"small"`
	Count  uint32  `yaml:"count"`
	Order  int     `yaml:"order"`
	Weight float32 `yaml:"weight"`
	Exact  float64 `yaml:"exact"`
	Level  int16   `yaml:"level" schema:"minimum=1"`
}

// TestReflectSizedNumbers checks that a number field is bounded by what its Go type holds, so the schema refuses a value its loader would overflow, and that a schema tag's bound replaces the type's.
func TestReflectSizedNumbers(t *testing.T) {
	s := mustReflect(t, Sized{}, jsonschema.Options{})
	if err := s.ValidateYAML([]byte("small: -128\ncount: 4294967295\norder: -9000000000000000000\nweight: 3.4e38\nexact: 1e300\nlevel: 32767\n")); err != nil {
		t.Fatalf("values in range: %v", err)
	}
	err := s.ValidateYAML([]byte("small: 128\ncount: 4294967296\norder: 1e20\nweight: -1e39\nlevel: 0\n"))
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("ValidateYAML = %v, want a *ValidationError", err)
	}
	want := []string{
		"/count: 4294967296 is greater than the maximum 4294967295",
		"/level: 0 is less than the minimum 1",
		"/order: 100000000000000000000 is greater than the maximum 9223372036854776000",
		"/small: 128 is greater than the maximum 127",
		"/weight: -1e+39 is less than the minimum -3.4028234663852886e+38",
	}
	if len(verr.Issues) != len(want) {
		t.Fatalf("issues = %q, want %q", verr.Issues, want)
	}
	for i, issue := range verr.Issues {
		if issue.String() != want[i] {
			t.Fatalf("issue %d = %q, want %q", i, issue.String(), want[i])
		}
	}
}

func TestReflectRootSelfReference(t *testing.T) {
	s := mustReflect(t, Node{}, jsonschema.Options{})
	items := s.Property("children").Items
	if items.Ref != "#" {
		t.Fatalf("children items $ref = %q, want #", items.Ref)
	}
	if len(s.Definitions) != 0 {
		t.Fatalf("definitions = %v, want none", s.Definitions)
	}
	if err := s.ValidateYAML([]byte("name: a\nchildren:\n  - name: b\n    children: [{name: ''}]\n")); err == nil || !strings.Contains(err.Error(), "/children/0/children/0/name: string is shorter than 1 characters") {
		t.Fatalf("nested root refs: err = %v", err)
	}
}

func TestReflectAllowUnknownAndTypes(t *testing.T) {
	type Row struct {
		Wait Mode `yaml:"wait"`
	}
	override := &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeString, jsonschema.TypeInteger}}
	s := mustReflect(t, []Row{}, jsonschema.Options{AllowUnknown: true, Types: map[reflect.Type]*jsonschema.Schema{reflect.TypeFor[Mode](): override}})
	if s.Items == nil || s.Items.Ref != "#/definitions/Row" {
		t.Fatalf("root items = %+v, want a $ref to Row", s.Items)
	}
	row := s.Definitions["Row"]
	if row.AdditionalProperties != nil {
		t.Fatal("AllowUnknown still refuses unknown keys")
	}
	if got := row.Property("wait"); len(got.Types) != 2 || got == override {
		t.Fatalf("override not applied as a copy: %+v", got)
	}
	if err := s.ValidateYAML([]byte("- wait: 3\n  other: x\n- wait: soon\n")); err != nil {
		t.Fatalf("ValidateYAML: %v", err)
	}
}

func TestReflectErrors(t *testing.T) {
	type badChan struct {
		C chan int
	}
	type badKey struct {
		M map[int]string
	}
	type badTag struct {
		N int `schema:"pattern=x"`
	}
	type badConst struct {
		N int `schema:"const=x"`
	}
	type dup struct {
		A string `yaml:"a"`
		B string `yaml:"a"`
	}
	type badInline struct {
		S string `yaml:",inline"`
	}
	cases := map[string]any{
		"chan":   badChan{},
		"key":    badKey{},
		"tag":    badTag{},
		"const":  badConst{},
		"dup":    dup{},
		"inline": badInline{},
	}
	for name, v := range cases {
		if _, err := jsonschema.Reflect(reflect.TypeOf(v), jsonschema.Options{}); err == nil {
			t.Errorf("%s: Reflect succeeded, want an error", name)
		}
	}
	if _, err := jsonschema.Reflect(nil, jsonschema.Options{}); err == nil {
		t.Error("nil type: Reflect succeeded")
	}
}

func TestValidate(t *testing.T) {
	s := mustReflect(t, Doc{}, jsonschema.Options{})
	cases := []struct {
		name string
		yaml string
		want []string
	}{
		{name: "valid", yaml: "version: 2\nmode: fast\nratio: 0.5\ncount: 3\nscale: 9.5\nflag: true\ntags: [a]\nroot: {name: r, children: [{name: c}]}\nbyName: {a: 1}\nextra: {x: [1, {y: 2}]}\nuntagged: u\nlabel: l\n"},
		{name: "missing version", yaml: "mode: fast\n", want: []string{`/: missing required key "version"`}},
		{name: "wrong const", yaml: "version: 1\n", want: []string{"/version: 1 is not the allowed value 2"}},
		{name: "float for integer", yaml: "version: 2.5\n", want: []string{"/version: want integer, got the number 2.5"}},
		{name: "integral float is an integer", yaml: "version: 2.0\n"},
		{name: "enum", yaml: "version: 2\nmode: medium\n", want: []string{`/mode: "medium" is not one of "fast", "slow"`}},
		{name: "bounds", yaml: "version: 2\nratio: -1\ncount: -2\nscale: 10\n", want: []string{"/count: -2 is less than the minimum 0", "/ratio: -1 is less than the minimum 0", "/scale: 10 is not less than 10"}},
		{name: "bounds of the Go type", yaml: "version: 2\ncount: 256\n", want: []string{"/count: 256 is greater than the maximum 255"}},
		{name: "exclusive minimum", yaml: "version: 2\nscale: 0\n", want: []string{"/scale: 0 is not greater than 0"}},
		{name: "not finite", yaml: "version: 2\nscale: .inf\n", want: []string{"/scale: want number, got +Inf, which is not a finite number"}},
		{name: "min items", yaml: "version: 2\ntags: []\n", want: []string{"/tags: array has 0 items, want at least 1"}},
		{name: "unknown key", yaml: "version: 2\nnmae: x\n", want: []string{`/nmae: unknown key "nmae" (known keys: version, mode, ratio, count, scale, flag, tags, root, byName, extra, untagged, label)`}},
		{name: "skipped field is unknown", yaml: "version: 2\nskipped: x\n", want: []string{`/skipped: unknown key "skipped"`}},
		{name: "nested", yaml: "version: 2\nroot: {children: [{name: 1}]}\n", want: []string{`/root: missing required key "name"`, "/root/children/0/name: want string, got the integer 1"}},
		{name: "map values", yaml: "version: 2\nbyName: {a/b: x}\n", want: []string{"/byName/a~1b: want integer, got the string \"x\""}},
		{name: "null", yaml: "version: 2\nflag: null\n", want: []string{"/flag: want boolean, got null"}},
		{name: "root type", yaml: "[1]\n", want: []string{"/: want object, got an array"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.ValidateYAML([]byte(tc.yaml))
			if len(tc.want) == 0 {
				if err != nil {
					t.Fatalf("ValidateYAML: %v", err)
				}
				return
			}
			var verr *jsonschema.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("ValidateYAML err = %v, want a *ValidationError", err)
			}
			if len(verr.Issues) != len(tc.want) {
				t.Fatalf("issues = %q, want %q", verr.Issues, tc.want)
			}
			for i, w := range tc.want {
				if got := verr.Issues[i].String(); !strings.HasPrefix(got, w) {
					t.Errorf("issue %d = %q, want prefix %q", i, got, w)
				}
			}
		})
	}
}

func TestValidateGoValues(t *testing.T) {
	s := mustReflect(t, Doc{}, jsonschema.Options{})
	ok := map[string]any{"version": uint16(2), "ratio": float32(0.25), "tags": []string{"a"}, "byName": map[string]int{"a": 1}}
	if err := s.Validate(ok); err != nil {
		t.Fatalf("Validate Go values: %v", err)
	}
	var fromJSON any
	if err := json.Unmarshal([]byte(`{"version": 2, "mode": "slow", "root": {"name": "x"}}`), &fromJSON); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(fromJSON); err != nil {
		t.Fatalf("Validate JSON value: %v", err)
	}
}

func TestBooleanSchemasAndRefs(t *testing.T) {
	if err := jsonschema.True().Validate(map[string]any{"a": 1}); err != nil {
		t.Fatalf("True: %v", err)
	}
	if err := jsonschema.False().Validate(nil); err == nil {
		t.Fatal("False accepted a value")
	}
	if v, ok := jsonschema.False().Boolean(); !ok || v {
		t.Fatal("False().Boolean() wrong")
	}
	if _, ok := (&jsonschema.Schema{}).Boolean(); ok {
		t.Fatal("an empty schema reports itself boolean")
	}
	dangling := &jsonschema.Schema{Ref: "#/definitions/Missing"}
	if err := dangling.Validate(1); err == nil || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("dangling $ref: %v", err)
	}
	loop := &jsonschema.Schema{Ref: "#"}
	if err := loop.Validate(1); err == nil || !strings.Contains(err.Error(), "nested more than") {
		t.Fatalf("self $ref: %v", err)
	}
	b, err := jsonschema.False().Marshal()
	if err != nil || string(b) != "false\n" {
		t.Fatalf("False().Marshal() = %q, %v", b, err)
	}
}

func TestValidateYAMLForms(t *testing.T) {
	s := mustReflect(t, Doc{}, jsonschema.Options{})
	merge := "base: &b\n  name: a\nversion: 2\nroot:\n  <<: *b\n  children: [{<<: *b, name: c}]\n"
	if err := s.ValidateYAML([]byte(merge)); err == nil || !strings.Contains(err.Error(), `unknown key "base"`) || strings.Contains(err.Error(), "<<") {
		t.Fatalf("merge keys: %v", err)
	}
	if err := (&jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeString}}).ValidateYAML([]byte("2001-12-14\n")); err != nil {
		t.Fatalf("a timestamp is a string to a language server: %v", err)
	}
	if err := s.ValidateYAML([]byte("version: 2\n---\nversion: 2\n")); err == nil || errors.As(err, new(*jsonschema.ValidationError)) {
		t.Fatalf("two documents: %v", err)
	}
	if err := s.ValidateYAML([]byte("version: [\n")); err == nil || errors.As(err, new(*jsonschema.ValidationError)) {
		t.Fatalf("bad YAML: %v", err)
	}
	if err := s.ValidateYAML([]byte("version: 2\nroot:\n  name: a\n  name: b\n")); err == nil || errors.As(err, new(*jsonschema.ValidationError)) || !strings.Contains(err.Error(), `line 4: mapping key "name" already defined`) {
		t.Fatalf("a repeated key: %v", err)
	}
	if v, err := jsonschema.DecodeYAML(nil); v != nil || err != nil {
		t.Fatalf("DecodeYAML(nil) = %v, %v", v, err)
	}
	if v, err := jsonschema.DecodeYAML([]byte("big: 18446744073709551615\nhuge: 1e400\nkey: {1: a, null: b}\n")); err != nil {
		t.Fatalf("DecodeYAML: %v", err)
	} else if m := v.(map[string]any); m["big"] != uint64(18446744073709551615) || m["key"].(map[string]any)["null"] != "b" {
		t.Fatalf("DecodeYAML = %#v", v)
	}
	var bomb strings.Builder
	bomb.WriteString("a0: &a0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i < 9; i++ {
		prev := "*a" + string(rune('0'+i-1))
		bomb.WriteString("a" + string(rune('0'+i)) + ": &a" + string(rune('0'+i)) + " [" + strings.Repeat(prev+", ", 9) + prev + "]\n")
	}
	if err := s.ValidateYAML([]byte(bomb.String())); err == nil || !strings.Contains(err.Error(), "aliases are expanded") {
		t.Fatalf("alias bomb: %v", err)
	}
}

func TestPackageDocs(t *testing.T) {
	docs, err := jsonschema.PackageDocs("testdata/docpkg")
	if err != nil {
		t.Fatalf("PackageDocs: %v", err)
	}
	want := map[string]string{
		"Point":       "Point is a place.\n\nIt has two paragraphs.",
		"Point.X":     "X and Y are the coordinates, written on two lines.",
		"Point.Y":     "X and Y are the coordinates, written on two lines.",
		"Point.Label": "Label names it.",
		"Grouped":     "Grouped is declared in a group.",
	}
	if !reflect.DeepEqual(docs, want) {
		t.Fatalf("PackageDocs =\n%q\nwant\n%q", docs, want)
	}
	if _, err := jsonschema.PackageDocs("testdata/missing"); err == nil {
		t.Fatal("PackageDocs of a missing directory succeeded")
	}
}

func TestDefaultMarshaling(t *testing.T) {
	s := &jsonschema.Schema{
		Types: []jsonschema.Type{jsonschema.TypeObject},
		Properties: []jsonschema.Property{
			{Name: "flag", Schema: &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeBoolean}, Default: false}},
			{Name: "count", Schema: &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeInteger}, Default: 0}},
			{Name: "label", Schema: &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeString}, Default: ""}},
			{Name: "tags", Schema: &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeArray}, Default: []string{"a"}}},
			{Name: "unset", Schema: &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeString}}},
		},
	}
	raw, err := s.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		`"default": false`,
		`"default": 0`,
		`"default": ""`,
		"\"default\": [\n        \"a\"\n      ]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Marshal missing %q:\n%s", want, got)
		}
	}
	// unset has nil Default so it must not emit a default keyword.
	if strings.Count(got, `"default"`) != 4 {
		t.Errorf("expected 4 default entries, got %d:\n%s", strings.Count(got, `"default"`), got)
	}
	// Default is an annotation keyword and does not restrict validation.
	if err := s.Validate(map[string]any{"flag": true, "count": 5, "label": "hi"}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
