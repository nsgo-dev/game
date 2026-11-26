package game_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	// yamlImport is the YAML library every format in this module decodes with.
	yamlImport = "go.yaml.in/yaml/v3"
	// formattestImport is the package of the shared conformance checks.
	formattestImport = "github.com/nsgo-dev/game/packages/game/internal/formattest"
)

// notFormats are the packages that decode YAML without defining a format of their own, each with the reason, so they need not run formattest.Run. Every other package that decodes YAML must.
var notFormats = map[string]string{
	"internal/gamecli":     "reads single settings out of game.yaml and nsgo.yaml, whose formats package project and NSGo define",
	"internal/formattest":  "the conformance checks themselves, which decode a document to edit it",
	"internal/releaseplan": "reads single settings out of nsgo.yaml, whose format NSGo defines",
	"loaderr":              "decodes a document only to find a field's line and column for an error",
	"tooling/assetcheck":   "scans other formats' documents for asset references without loading them",
	"tooling/jsonschema":   "validates documents of other formats against their schemas",
}

// yamlDecoders returns, by slash path relative to the module root, the packages whose non-test files import go.yaml.in/yaml/v3 and decode with it: they call its Unmarshal or NewDecoder, or a Decode method (as on a yaml.Node). The scan works on syntax, so it finds a format whatever decoder settings it uses, including a lenient yaml.Unmarshal, KnownFields with a variable and a walk of the node tree.
func yamlDecoders(t *testing.T) []string {
	t.Helper()
	var pkgs []string
	walkSources(t, false, func(pkg, _ string, f *ast.File) {
		if slices.Contains(pkgs, pkg) {
			return
		}
		yamlName, ok := importName(f, yamlImport)
		if !ok {
			return
		}
		decodes := false
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					x, isIdent := sel.X.(*ast.Ident)
					switch {
					case isIdent && x.Name == yamlName && (sel.Sel.Name == "Unmarshal" || sel.Sel.Name == "NewDecoder"):
						decodes = true
					case sel.Sel.Name == "Decode":
						decodes = true
					}
				}
			}
			return !decodes
		})
		if decodes {
			pkgs = append(pkgs, pkg)
		}
	})
	slices.Sort(pkgs)
	return pkgs
}

// importName returns the name by which the file f refers to the package at importPath, and whether f imports it at all. Without an explicit name it is the last path element, or the one before it when the last is a major version such as v3.
func importName(f *ast.File, importPath string) (string, bool) {
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil || p != importPath {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name, true
		}
		dir, last := path.Split(p)
		if len(last) > 1 && last[0] == 'v' && strings.Trim(last[1:], "0123456789") == "" {
			last = path.Base(dir)
		}
		return last, true
	}
	return "", false
}

// runsFormattest reports whether a test file of the package in dir calls formattest.Run. It looks at the files' syntax trees, so a call in a comment or a string literal does not count.
func runsFormattest(t *testing.T, dir string) bool {
	t.Helper()
	tests, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, file := range tests {
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		name, ok := importName(f, formattestImport)
		if !ok {
			continue
		}
		runs := false
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				fn := call.Fun
				if idx, ok := fn.(*ast.IndexExpr); ok { // formattest.Run[T](...)
					fn = idx.X
				}
				if sel, ok := fn.(*ast.SelectorExpr); ok && sel.Sel.Name == "Run" {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == name {
						runs = true
					}
				}
			}
			return !runs
		})
		if runs {
			return true
		}
	}
	return false
}

// TestFormatPackagesRunConformance checks that every package decoding YAML runs the shared conformance checks of internal/formattest, unless notFormats lists it, so a format that accepts unknown fields or versions it does not support, writes different bytes for the same value or changes a file on a load and save fails CI.
//
// The check is per package: one formattest.Run call satisfies it, so a package that defines a second format must run the checks on that one as well without being told to.
func TestFormatPackagesRunConformance(t *testing.T) {
	pkgs := yamlDecoders(t)
	if len(pkgs) == 0 {
		t.Fatal("found no package that decodes YAML; the scan is broken")
	}
	for pkg := range notFormats {
		if !slices.Contains(pkgs, pkg) {
			t.Errorf("notFormats lists %s, which no longer decodes YAML; remove it", pkg)
		}
	}
	for _, pkg := range pkgs {
		if _, ok := notFormats[pkg]; ok {
			continue
		}
		if !runsFormattest(t, filepath.FromSlash(pkg)) {
			t.Errorf("%s decodes YAML but no test of it calls formattest.Run; add a conformance_test.go that runs it on a testdata file in the format's canonical form, or, if the package reads no format of its own, list it in notFormats with the reason", pkg)
		}
	}
}
