package loaderr_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nsgo-dev/game/packages/game/loaderr"
)

// FuzzFindField checks that FindField never panics or loops on arbitrary documents and paths, and that a position it reports is inside the document.
func FuzzFindField(f *testing.F) {
	f.Add([]byte(sceneYAML), "layers[0].objects[1].alpha")
	f.Add([]byte(sceneJSON), "layers[0].objects[1]")
	f.Add([]byte("a: &x\n  b: *x\n"), "a.b.b.b")
	f.Add([]byte("[1, [2, {\"a\": 3}]]"), "[1][1].a")
	f.Add([]byte("{\"a\":"), "a")
	f.Add([]byte(""), "")
	f.Add([]byte("a: 1"), `["\"]`)
	f.Fuzz(func(t *testing.T, data []byte, field string) {
		line, col := loaderr.FindField(data, field)
		if n := maxLines(data); line < 0 || col < 0 || line > n {
			t.Fatalf("FindField = %d:%d outside a document of at most %d lines", line, col, n)
		}
	})
}

// FuzzPath checks that every path Path writes is found again by FindField in a document that holds it.
func FuzzPath(f *testing.F) {
	f.Add("layers", 2, "a.b")
	f.Add("", 0, "")
	f.Add("x\"y", 1, "[z]")
	f.Fuzz(func(t *testing.T, a string, i int, b string) {
		if i < 0 || i > 8 || strings.ContainsFunc(a+b, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }) || !utf8.ValidString(a) || !utf8.ValidString(b) {
			return
		}
		items := strings.Repeat("{}, ", i)
		data := []byte("{" + quote(a) + ": [" + items + "{" + quote(b) + ": 1}]}")
		line, col := loaderr.FindField(data, loaderr.Path(a, i, b))
		if line != 1 || col <= 1 {
			t.Fatalf("FindField(%s, %q) = %d:%d", data, loaderr.Path(a, i, b), line, col)
		}
	})
}

// maxLines bounds the lines of data, counting every character YAML may take as a line break.
func maxLines(data []byte) int {
	n := 1
	for _, r := range string(data) {
		switch r {
		case '\n', '\r', '\u0085', ' ', ' ':
			n++
		}
	}
	return n
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }
