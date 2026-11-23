package assetcheck

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestParseJSONLines(t *testing.T) {
	data := "{\n  \"a\": \"x\",\n  \"b\": [\n    1,\n    {\"c\": null}\n  ],\n  \"a\": true\n}\n"
	v, err := parseJSON([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if v.line != 1 || v.kind != jsonObject {
		t.Fatalf("root at line %d kind %d", v.line, v.kind)
	}
	if a := v.field("a"); a.kind != jsonBool || a.line != 7 {
		t.Fatalf("a: kind %d line %d, want the last bool on line 7", a.kind, a.line)
	}
	b := v.field("b").list()
	if len(b) != 2 || b[0].line != 4 || b[1].line != 5 || b[1].field("c").kind != jsonNull {
		t.Fatalf("b: %+v", b)
	}
	if n, ok := b[0].number(); !ok || n != 1 {
		t.Fatalf("b[0] = %v, %v", n, ok)
	}
	if s, line := v.str("missing"); s != "" || line != 0 {
		t.Fatalf("missing field = %q, %d", s, line)
	}
}

func TestParseJSONErrors(t *testing.T) {
	cases := []struct {
		data string
		line int
		msg  string
	}{
		{"", 0, "empty file"},
		{"{\n  \"a\": 1,\n  \"b\": ,\n}", 3, "invalid JSON"},
		{"{\n  \"a\": [1,\n", 2, "unexpected end of JSON input"},
		{"{}\n{}", 2, "unexpected data after"},
		{"{\"a\": 1}}", 1, "invalid JSON"},
		{strings.Repeat("[", maxJSONDepth+1), 1, "nests deeper"},
	}
	for _, tc := range cases {
		_, err := parseJSON([]byte(tc.data))
		je, ok := err.(*jsonError)
		if !ok || je.line != tc.line || !strings.Contains(je.msg, tc.msg) {
			t.Errorf("parseJSON(%.20q) = %v, want line %d with %q", tc.data, err, tc.line, tc.msg)
		}
	}
}

func TestYAMLLine(t *testing.T) {
	src := "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: o\n        sprite:\n          atlas: x\nanchor: &obj\n  deep: 1\nref: *obj\n"
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(src), &root); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		steps []string
		line  int
	}{
		{nil, 1},
		{[]string{"version"}, 1},
		{[]string{"layers", "0", "objects", "0", "sprite", "atlas"}, 7},
		{[]string{"layers", "0", "objects", "0", "sprite"}, 6},
		{[]string{"layers", "0", "objects", "0", "material"}, 5},
		{[]string{"layers", "3", "name"}, 2},
		{[]string{"layers", "x"}, 2},
		{[]string{"version", "deeper"}, 1},
		{[]string{"ref", "deep"}, 9},
	}
	for _, tc := range cases {
		if got := yamlLine(&root, tc.steps); got != tc.line {
			t.Errorf("yamlLine(%q) = %d, want %d", tc.steps, got, tc.line)
		}
	}
}

func TestResolve(t *testing.T) {
	cases := []struct {
		dir, ref, want, err string
	}{
		{"maps", "tiles.tsj", "maps/tiles.tsj", ""},
		{"maps", "../art/a.png", "art/a.png", ""},
		{".", "a.png", "a.png", ""},
		{"maps", "../../a.png", "", "leads outside"},
		{"maps", "/a.png", "", "absolute path"},
		{"maps", "D:/a.png", "", "absolute path"},
		{"maps", `art\a.png`, "", "backslashes"},
		{"maps", "asset:a.png", "", "is a URI"},
		{"maps", "https://x/a.png", "", "is a URI"},
	}
	for _, tc := range cases {
		got, err := resolve(tc.dir, tc.ref)
		switch {
		case tc.err == "" && (err != nil || got != tc.want):
			t.Errorf("resolve(%q, %q) = %q, %v; want %q", tc.dir, tc.ref, got, err, tc.want)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("resolve(%q, %q) error = %v, want %q", tc.dir, tc.ref, err, tc.err)
		}
	}
}
