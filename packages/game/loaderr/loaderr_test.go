package loaderr_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"go.yaml.in/yaml/v3"
)

func TestErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		err  *loaderr.Error
		want string
	}{
		{"all parts", &loaderr.Error{File: "a.yaml", Line: 3, Column: 5, Field: "layers[0].space", Problem: "invalid space \"iso\"", Expected: "world or screen", Fix: "set space to world"}, `a.yaml:3:5: layers[0].space: invalid space "iso"; expected world or screen; fix: set space to world`},
		{"no column", &loaderr.Error{File: "a.yaml", Line: 3, Problem: "bad"}, "a.yaml:3: bad"},
		{"no file", &loaderr.Error{Line: 7, Problem: "bad", Fix: "f"}, "<input>:7: bad; fix: f"},
		{"no position", &loaderr.Error{File: "a.yaml", Problem: "bad"}, "a.yaml: bad"},
		{"field only", &loaderr.Error{Field: "x", Problem: "bad"}, "x: bad"},
		{"problem from Err", &loaderr.Error{Err: io.EOF}, "EOF"},
		{"nothing", &loaderr.Error{}, "invalid value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestErrorfKeepsWrapped(t *testing.T) {
	e := loaderr.Errorf("a.b", "do x", "open %q: %w", "f", fs.ErrNotExist)
	if !errors.Is(e, fs.ErrNotExist) {
		t.Fatalf("errors.Is(%v, fs.ErrNotExist) = false", e)
	}
	if e.Field != "a.b" || e.Fix != "do x" || e.Problem != `open "f": file does not exist` {
		t.Fatalf("Errorf = %+v", e)
	}
}

func TestWithFile(t *testing.T) {
	if loaderr.WithFile(nil, "a") != nil {
		t.Fatal("WithFile(nil) != nil")
	}
	plain := errors.New("plain")
	if loaderr.WithFile(plain, "a") != plain {
		t.Fatal("WithFile changed a plain error")
	}
	orig := &loaderr.Error{Line: 2, Problem: "p"}
	got, _ := loaderr.As(loaderr.WithFile(orig, "a.yaml"))
	if got.File != "a.yaml" || orig.File != "" {
		t.Fatalf("WithFile: got %q, original %q", got.File, orig.File)
	}
	named := &loaderr.Error{File: "b.yaml"}
	if loaderr.WithFile(named, "a.yaml") != error(named) {
		t.Fatal("WithFile replaced a file name already set")
	}
}

func TestJoin(t *testing.T) {
	cases := []struct {
		base  string
		parts []any
		want  string
	}{
		{"", []any{"a", 1}, "a[1]"},
		{"a[1]", nil, "a[1]"},
		{"a[1]", []any{"b"}, "a[1].b"},
		{"a", []any{2, "c.d"}, `a[2]["c.d"]`},
	}
	for _, tc := range cases {
		if got := loaderr.Join(tc.base, tc.parts...); got != tc.want {
			t.Errorf("Join(%q, %v) = %q, want %q", tc.base, tc.parts, got, tc.want)
		}
	}
}

func TestPath(t *testing.T) {
	cases := []struct {
		parts []any
		want  string
	}{
		{nil, ""},
		{[]any{"layers", 2, "objects", 0, "id"}, "layers[2].objects[0].id"},
		{[]any{0, "a"}, "[0].a"},
		{[]any{"tilemaps", "a.b", "ref"}, `tilemaps["a.b"].ref`},
		{[]any{"m", ""}, `m[""]`},
	}
	for _, tc := range cases {
		if got := loaderr.Path(tc.parts...); got != tc.want {
			t.Errorf("Path(%v) = %q, want %q", tc.parts, got, tc.want)
		}
	}
}

const sceneYAML = `version: 1
layers:
  - name: bg
    space: iso
    objects:
      - id: a
      - id: b
        alpha: 3
tilemaps:
  "a.b":
    ref: x
`

const sceneJSON = `{
  "version": 1,
  "layers": [
    {"name": "bg",
     "objects": [{"id": "a"}, {"id": "b", "alpha": 3}]}
  ]
}`

func TestFindField(t *testing.T) {
	cases := []struct {
		data, field        string
		wantLine, wantColn int
	}{
		{sceneYAML, "", 1, 1},
		{sceneYAML, "version", 1, 1},
		{sceneYAML, "layers[0].space", 4, 5},
		{sceneYAML, "layers[0].objects[1].alpha", 8, 9},
		{sceneYAML, "layers[0].objects[1]", 7, 9},
		{sceneYAML, `tilemaps["a.b"].ref`, 11, 5},
		// A missing field reports the nearest enclosing value that is there.
		{sceneYAML, "layers[0].objects[1].sprite", 7, 9},
		{sceneYAML, "layers[5].name", 2, 1},
		{sceneJSON, "version", 2, 3},
		{sceneJSON, "layers[0].objects[1].alpha", 5, 43},
		{sceneJSON, "layers[0].objects[1]", 5, 31},
		{"version: [", "version", 0, 0},
		{sceneYAML, "bad[path", 0, 0},
	}
	for _, tc := range cases {
		l, c := loaderr.FindField([]byte(tc.data), tc.field)
		if l != tc.wantLine || c != tc.wantColn {
			t.Errorf("FindField(%q) = %d:%d, want %d:%d", tc.field, l, c, tc.wantLine, tc.wantColn)
		}
	}
}

func TestFindFieldAliasCycle(t *testing.T) {
	// An alias that refers to the node holding it must not loop.
	data := []byte("a: &x\n  b: *x\n  c: 1\n")
	if l, _ := loaderr.FindField(data, "a.c"); l != 3 {
		t.Fatalf("line = %d, want 3", l)
	}
}

func TestLocateAddsLineFileAndFix(t *testing.T) {
	err := loaderr.Errorf("layers[0].objects[1].alpha", "", "alpha %v out of range", 3)
	got, ok := loaderr.As(loaderr.Locate(err, "scene.yaml", []byte(sceneYAML), "default fix"))
	if !ok {
		t.Fatal("Locate did not return a *loaderr.Error")
	}
	if got.File != "scene.yaml" || got.Line != 8 || got.Column != 9 || got.Fix != "default fix" {
		t.Fatalf("Locate = %+v", got)
	}
	if err.File != "" || err.Line != 0 {
		t.Fatal("Locate changed the original error")
	}
	if loaderr.Locate(nil, "a", nil, "f") != nil {
		t.Fatal("Locate(nil) != nil")
	}
}

func TestLocateKeepsOwnFix(t *testing.T) {
	err := loaderr.Errorf("version", "set version: 1", "bad version")
	got, _ := loaderr.As(loaderr.Locate(err, "", []byte(sceneYAML), "default"))
	if got.Fix != "set version: 1" || got.Line != 1 {
		t.Fatalf("Locate = %+v", got)
	}
}

func TestLocatePlainError(t *testing.T) {
	got, _ := loaderr.As(loaderr.Locate(fs.ErrNotExist, "a.yaml", nil, "create the file"))
	if got.File != "a.yaml" || got.Line != 0 || got.Fix != "create the file" || !errors.Is(got, fs.ErrNotExist) {
		t.Fatalf("Locate = %+v", got)
	}
}

type doc struct {
	Version int `yaml:"version" json:"version"`
	Layers  []struct {
		Name  string `yaml:"name" json:"name"`
		Order int    `yaml:"order" json:"order"`
	} `yaml:"layers" json:"layers"`
}

func decodeYAML(data string) error {
	dec := yaml.NewDecoder(strings.NewReader(data))
	dec.KnownFields(true)
	var d doc
	return dec.Decode(&d)
}

func decodeJSON(data string) error {
	dec := json.NewDecoder(strings.NewReader(data))
	dec.DisallowUnknownFields()
	var d doc
	return dec.Decode(&d)
}

func TestLocateDecodeErrors(t *testing.T) {
	cases := []struct {
		name      string
		data      string
		decode    func(string) error
		line      int
		field     string
		fix       string
		problem   string
		wantColum int
	}{
		{"yaml unknown field", "version: 1\nlayers:\n  - name: a\n    nmae: b\n", decodeYAML, 4, "layers[0].nmae", loaderr.FixUnknownField, `unknown field "nmae"`, 5},
		{"yaml wrong type", "version: 1\nlayers:\n  - name: a\n    order: high\n", decodeYAML, 4, "layers[0].order", loaderr.FixType, "cannot use !!str `high` here", 5},
		{"yaml syntax", "version: 1\nlayers: [\n", decodeYAML, 2, "", loaderr.FixSyntax, "invalid YAML", 0},
		{"yaml flow sequence", "version: 1\n\nlayers: [1, 2\nname: x\n", decodeYAML, 3, "", loaderr.FixSyntax, "did not find expected ',' or ']'", 0},
		{"yaml parser error on line 1", "version: [1, 2", decodeYAML, 1, "", loaderr.FixSyntax, "did not find expected", 0},
		{"yaml scanner error", "version: 1\nname: \"x\nlayers: 2\n", decodeYAML, 2, "", loaderr.FixSyntax, "unexpected end", 0},
		{"yaml duplicate key", "version: 1\nversion: 2\n", decodeYAML, 2, "version", loaderr.FixDuplicateKey, "already defined", 1},
		{"yaml wrapped", "version: 1\nbogus: 1\n", func(s string) error { return errors.Join(errors.New("ctx"), decodeYAML(s)) }, 2, "bogus", loaderr.FixUnknownField, `unknown field "bogus"`, 1},
		{"json unknown field", "{\n  \"version\": 1,\n  \"bogus\": 2\n}", decodeJSON, 3, "bogus", loaderr.FixUnknownField, `unknown field "bogus"`, 3},
		{"json wrong type", "{\n  \"version\": 1,\n  \"layers\": [{\"name\": \"a\", \"order\": \"x\"}]\n}", decodeJSON, 3, "layers[0].order", loaderr.FixType, "cannot use a JSON string here", 28},
		{"json syntax", "{\n  \"version\": 1,\n  \"layers\": ]\n}", decodeJSON, 3, "", loaderr.FixSyntax, "invalid JSON", 13},
		{"json truncated", "{\n  \"version\": 1,\n", decodeJSON, 3, "", loaderr.FixTruncated, "unexpected end", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			derr := tc.decode(tc.data)
			if derr == nil {
				t.Fatal("decode succeeded")
			}
			got, ok := loaderr.As(loaderr.Locate(derr, "f", []byte(tc.data), "default"))
			if !ok {
				t.Fatalf("Locate(%v) is not a *loaderr.Error", derr)
			}
			if got.Line != tc.line || got.Field != tc.field || got.Fix != tc.fix || !strings.Contains(got.Problem, tc.problem) {
				t.Fatalf("Locate(%v) = %+v", derr, got)
			}
			if tc.wantColum != 0 && got.Column != tc.wantColum {
				t.Fatalf("Column = %d, want %d (%+v)", got.Column, tc.wantColum, got)
			}
			if !errors.Is(got, derr) && got.Err != derr {
				t.Fatalf("Locate lost the decode error: %+v", got)
			}
		})
	}
}

func TestErrorfKeepsEveryWrapped(t *testing.T) {
	a, b := errors.New("a"), errors.New("b")
	e := loaderr.Errorf("f", "fix", "%w: %w", a, b)
	if !errors.Is(e, a) || !errors.Is(e, b) {
		t.Fatalf("Errorf with two %%w: errors.Is lost one of them (Err = %v)", e.Err)
	}
	if e.Problem != "a: b" {
		t.Fatalf("Problem = %q, want %q", e.Problem, "a: b")
	}
}

func TestWithFileKeepsWrappingContext(t *testing.T) {
	sentinel := errors.New("sentinel")
	inner := &loaderr.Error{Line: 2, Problem: "p"}
	for _, err := range []error{fmt.Errorf("ctx: %w", inner), errors.Join(sentinel, inner)} {
		if got := loaderr.WithFile(err, "a.yaml"); got != err {
			t.Fatalf("WithFile(%v) = %v, want the error unchanged", err, got)
		}
	}
}

func TestLocateUnknownJSONFieldNamedTwice(t *testing.T) {
	data := "{\n  \"a\": {\n    \"name\": 1\n  },\n  \"b\": {\n    \"name\": 2\n  }\n}"
	var v struct {
		A struct {
			Name int `json:"name"`
		} `json:"a"`
		B struct{} `json:"b"`
	}
	dec := json.NewDecoder(strings.NewReader(data))
	dec.DisallowUnknownFields()
	derr := dec.Decode(&v)
	if derr == nil {
		t.Fatal("decode succeeded")
	}
	got, _ := loaderr.As(loaderr.Locate(derr, "f.json", []byte(data), ""))
	if got.Field != "" || got.Line != 0 {
		t.Fatalf("Locate = %+v, want no field or line when the key is named more than once", got)
	}
	if got.Fix != loaderr.FixUnknownField {
		t.Fatalf("Fix = %q, want FixUnknownField", got.Fix)
	}
}

func TestLocateJSONTypeErrorUnderMapKeys(t *testing.T) {
	cases := []struct {
		name, data, field string
		line              int
	}{
		{"numeric key", "{\n  \"5\": [],\n  \"10\": [\n    {\"time\": \"x\"}\n  ]\n}", loaderr.Path("10", 0, "time"), 4},
		{"dotted key", "{\n  \"hero.run\": [\n    {\"time\": \"x\"}\n  ]\n}", loaderr.Path("hero.run", 0, "time"), 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v map[string][]struct {
				Time float64 `json:"time"`
			}
			derr := json.Unmarshal([]byte(tc.data), &v)
			if derr == nil {
				t.Fatal("decode succeeded")
			}
			got, _ := loaderr.As(loaderr.Locate(derr, "f.json", []byte(tc.data), ""))
			if got.Field != tc.field || got.Line != tc.line {
				t.Fatalf("Locate = %+v, want field %q on line %d", got, tc.field, tc.line)
			}
		})
	}
}

// TestLocateJSONTypeErrorFromFirstImplementation builds the errors encoding/json's first implementation returns, which nsgo build links (GOEXPERIMENT=nojsonv2): a Field without array indexes or map keys and an Offset just past the value at fault.
func TestLocateJSONTypeErrorFromFirstImplementation(t *testing.T) {
	cases := []struct {
		name, data, value, jsonField, field string
		line                                int
	}{
		{"array index", "{\n  \"version\": 1,\n  \"layers\": [{\"name\": \"a\"}, {\"name\": \"b\", \"order\": \"x\"}]\n}", `"x"`, "layers.order", "layers[1].order", 3},
		{"map key", "{\n  \"5\": [],\n  \"10\": [\n    {\"time\": \"x\"}\n  ]\n}", `"x"`, "time", loaderr.Path("10", 0, "time"), 4},
		{"inside a field", "{\n  \"items\": [\n    \"a\",\n    3\n  ]\n}", "3", "items", "items[1]", 4},
		{"object", "{\n  \"version\": {}\n}", "{", "version", "version", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			derr := &json.UnmarshalTypeError{Value: "string", Type: reflect.TypeFor[int](), Offset: int64(strings.LastIndex(tc.data, tc.value) + len(tc.value)), Field: tc.jsonField}
			got, _ := loaderr.As(loaderr.Locate(derr, "f.json", []byte(tc.data), ""))
			if got.Field != tc.field || got.Line != tc.line {
				t.Fatalf("Locate = %+v, want field %q on line %d", got, tc.field, tc.line)
			}
		})
	}
}

func TestLocateWithoutDataHasNoLine(t *testing.T) {
	data := "{\n  \"version\": 1,\n  \"layers\": ]\n}"
	got, _ := loaderr.As(loaderr.Locate(decodeJSON(data), "f.json", nil, ""))
	if got.Line != 0 || got.Column != 0 {
		t.Fatalf("Locate without data = %+v, want line 0", got)
	}
	got, _ = loaderr.As(loaderr.Locate(json.Unmarshal([]byte(`{"version": "x"}`), new(doc)), "f.json", nil, ""))
	if got.Line != 0 || got.Field != "version" {
		t.Fatalf("Locate without data = %+v, want field version at line 0", got)
	}
}

func TestLocateJSONColumnCountsCharacters(t *testing.T) {
	data := `{"name": "勇者", "version": ]}`
	got, _ := loaderr.As(loaderr.Locate(decodeJSON(data), "f.json", []byte(data), ""))
	if want := strings.Index(data, "]") - len("勇者") + len([]rune("勇者")) + 1; got.Column != want {
		t.Fatalf("Column = %d, want %d (%+v)", got.Column, want, got)
	}
}
