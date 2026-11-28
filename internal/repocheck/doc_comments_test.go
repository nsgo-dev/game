package repocheck_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestPublicPackageDocs checks the package overviews and exported declaration comments that generate the API reference.
func TestPublicPackageDocs(t *testing.T) {
	fset := token.NewFileSet()
	packages := map[string][]*ast.File{}
	parse := func(p string) error {
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		dir := filepath.Dir(p)
		packages[dir] = append(packages[dir], f)
		return nil
	}
	rootFiles, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range rootFiles {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		if err := parse(p); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range publicGoFiles(t, "pkg") {
		if err := parse(p); err != nil {
			t.Fatal(err)
		}
	}

	dirs := make([]string, 0, len(packages))
	for dir := range packages {
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	for _, dir := range dirs {
		files := packages[dir]
		slices.SortFunc(files, func(a, b *ast.File) int {
			return strings.Compare(fset.Position(a.Pos()).Filename, fset.Position(b.Pos()).Filename)
		})
		if !hasPackageDoc(files) {
			t.Errorf("%s: package %s has no package doc comment", dir, files[0].Name.Name)
		}
		for _, f := range files {
			for _, gap := range exportedDocGaps(f) {
				t.Errorf("%s: %s has no doc comment", fset.Position(gap.pos), gap.name)
			}
		}
	}
}

// TestExportedDocGaps proves the checker rejects removed comments without rejecting methods on unexported receivers.
func TestExportedDocGaps(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "sample.go", `// Package sample documents a package.
package sample

// DocumentedFunc is documented.
func DocumentedFunc() {}

func UndocumentedFunc() {}

// DocumentedType is documented.
type DocumentedType struct{}

type UndocumentedType struct{}

func (*DocumentedType) UndocumentedMethod() {}

type unexportedType struct{}

func (unexportedType) MethodOnUnexportedType() {}
`, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, gap := range exportedDocGaps(f) {
		got = append(got, gap.name)
	}
	want := []string{"UndocumentedFunc", "UndocumentedType", "UndocumentedMethod"}
	if !slices.Equal(got, want) {
		t.Fatalf("exportedDocGaps() = %v, want %v", got, want)
	}
}

// TestPackageDocDetached proves a blank line between a package comment and declaration is rejected.
func TestPackageDocDetached(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "doc.go", `// Package sample documents a package.

package sample
`, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if hasPackageDoc([]*ast.File{f}) {
		t.Fatal("hasPackageDoc accepted a detached package comment")
	}
}

type docGap struct {
	pos  token.Pos
	name string
}

func publicGoFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (d.Name() == "internal" || d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") && !strings.HasSuffix(d.Name(), "_test.go") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	return files
}

func hasPackageDoc(files []*ast.File) bool {
	for _, f := range files {
		if f.Doc == nil {
			continue
		}
		doc := strings.TrimSpace(f.Doc.Text())
		prefix := "Package " + f.Name.Name
		if doc == prefix || strings.HasPrefix(doc, prefix+" ") || strings.HasPrefix(doc, prefix+"\n") {
			return true
		}
	}
	return false
}

func exportedDocGaps(f *ast.File) []docGap {
	var gaps []docGap
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() || d.Recv != nil && !exportedReceiver(d.Recv) || hasDoc(d.Doc) {
				continue
			}
			gaps = append(gaps, docGap{pos: d.Pos(), name: d.Name.Name})
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				s := spec.(*ast.TypeSpec)
				if !s.Name.IsExported() || hasDoc(d.Doc) || hasDoc(s.Doc) {
					continue
				}
				gaps = append(gaps, docGap{pos: s.Pos(), name: s.Name.Name})
			}
		}
	}
	return gaps
}

func exportedReceiver(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) != 1 {
		return false
	}
	var expr ast.Expr = recv.List[0].Type
	for {
		switch e := expr.(type) {
		case *ast.Ident:
			return e.IsExported()
		case *ast.StarExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		default:
			return false
		}
	}
}

func hasDoc(doc *ast.CommentGroup) bool {
	return doc != nil && strings.TrimSpace(doc.Text()) != ""
}
