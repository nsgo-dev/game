package token_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestImportsStayLight checks that the package imports only the standard library and the YAML package, so a game server that verifies tokens links none of the account service's password hashing or database drivers. The standard library and go.yaml.in/yaml/v3 import nothing outside themselves, so the package's direct imports decide its whole dependency graph.
func TestImportsStayLight(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			first, _, _ := strings.Cut(path, "/")
			if path != "go.yaml.in/yaml/v3" && strings.Contains(first, ".") {
				t.Errorf("%s imports %s; the token package may import only the standard library and go.yaml.in/yaml/v3", name, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no source files to check")
	}
}
