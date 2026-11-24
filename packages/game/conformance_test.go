package game_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// handWrittenFormats are the packages whose YAML format is read by walking the yaml.Node tree, not through a decoder with KnownFields(true), so TestFormatPackagesRunConformance cannot find them by that call. Each still runs formattest.Run.
var handWrittenFormats = []string{
	"tooling/datatable", // LoadYAML checks the top-level keys and each row's columns itself.
}

// strictYAMLPackages returns, by slash path relative to the module root, the packages whose non-test files call KnownFields(true) on a YAML decoder, that is, those that read a format refusing unknown fields.
func strictYAMLPackages(t *testing.T) []string {
	t.Helper()
	var pkgs []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != "." && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			if p != "." {
				if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
					return filepath.SkipDir // another module
				}
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(filepath.Dir(p))
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "KnownFields" {
				return true
			}
			if arg, ok := call.Args[0].(*ast.Ident); ok && arg.Name == "true" && !slices.Contains(pkgs, pkg) {
				pkgs = append(pkgs, pkg)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(pkgs)
	return pkgs
}

// runsFormattest reports whether a test file of the package in dir calls formattest.Run.
func runsFormattest(t *testing.T, dir string) bool {
	t.Helper()
	tests, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range tests {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "formattest.Run(") {
			return true
		}
	}
	return false
}

// TestFormatPackagesRunConformance checks that every package reading a YAML format runs the shared conformance checks of internal/formattest on it, so a format that accepts unknown fields or versions it does not support, writes different bytes for the same value or changes a file on a load and save fails CI.
func TestFormatPackagesRunConformance(t *testing.T) {
	pkgs := strictYAMLPackages(t)
	if len(pkgs) == 0 {
		t.Fatal("found no package that reads strict YAML; the scan is broken")
	}
	for _, pkg := range handWrittenFormats {
		if slices.Contains(pkgs, pkg) {
			t.Errorf("%s calls KnownFields(true) now; remove it from handWrittenFormats", pkg)
		}
		if _, err := os.Stat(filepath.FromSlash(pkg)); err != nil {
			t.Errorf("handWrittenFormats lists %s: %v", pkg, err)
		}
	}
	for _, pkg := range append(pkgs, handWrittenFormats...) {
		if !runsFormattest(t, filepath.FromSlash(pkg)) {
			t.Errorf("%s reads a YAML format but no test of it calls formattest.Run; add a conformance_test.go that runs it on a testdata file in the format's canonical form", pkg)
		}
	}
}
