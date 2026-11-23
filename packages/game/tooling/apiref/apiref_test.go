package apiref_test

import (
	"bytes"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/apiref"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// fixture is a module that exercises each part of the reference: grouped values, types with fields, interface methods, constructors, generic methods, per-target files, doc links and the directories Load leaves out.
var fixture = map[string]string{
	"go.mod": "module example.com/m // the fixture\n\ngo 1.27\n",
	"m.go": `// Package m is a fixture. It links [sub.Thing], [Shape.Area] and [io.Reader].
//
// # Usage
//
// Call [New].
package m

import "example.com/m/sub"

// Limits of a shape.
const (
	// MaxSides is the most sides a shape has.
	MaxSides = 8
	minSides = 3
	// MinArea is the smallest area.
	MinArea = 1.0
)

// Default is the shape New returns.
var Default = Shape{Sides: 4}

// Kind is a kind of shape.
type Kind int

// The kinds.
const (
	Square Kind = iota
	Triangle
)

// Shape is a polygon.
type Shape struct {
	// Sides is how many sides it has.
	Sides int
	sub.Thing // an embedded type
	hidden    int
}

// New returns the default shape.
func New() Shape { return Default }

// Area is the shape's area.
func (s Shape) Area() float64 { return float64(s.Sides) }

func (s Shape) secret() {}

// Measurer measures.
type Measurer interface {
	// Measure measures.
	Measure() float64
	internal()
}

// Set holds values.
type Set[T comparable] struct{ items map[T]bool }

// Add adds v.
func (s *Set[T]) Add(v T) { s.items[v] = true }

type unexported struct{}

// Exported is unreachable.
func (unexported) Exported() {}

func init() {}
`,
	"native.go": `//go:build !js

package m

// Platform names the platform, natively.
func Platform() string { return "native" }

// FileSystem is only native.
func FileSystem() {}
`,
	"web.go": `//go:build js

package m

// Platform names the platform, on the web.
func Platform() string { return "web" }
`,
	"m_linux.go":           "package m\n\n// LinuxOnly exists on Linux and Android.\nconst LinuxOnly = true\n",
	"m_test.go":            "package m\n\n// TestOnly is not documented.\nfunc TestOnly() {}\n",
	"sub/sub.go":           "// Package sub links back to [m.Shape].\npackage sub\n\nimport \"example.com/m\"\n\nvar _ = m.MaxSides\n\n// Thing is a thing.\ntype Thing struct{}\n",
	"sub/deeper/deeper.go": "// Package deeper links to [example.com/m/sub.Thing].\npackage deeper\n\n// Deep is deep.\nconst Deep = 1\n",
	"webonly/w.go":         "//go:build js\n\n// Package webonly is only on the web.\npackage webonly\n\n// W is w.\nconst W = 1\n",
	"internal/i/i.go":      "package i\n\n// I is internal.\nconst I = 1\n",
	"testdata/t/t.go":      "package t\n\nconst T = 1\n",
	"_skip/s.go":           "package s\n\nconst S = 1\n",
	"cmd/tool/main.go":     "package main\n\nfunc main() {}\n",
	"nested/go.mod":        "module example.com/nested\n",
	"nested/n.go":          "package nested\n\nconst N = 1\n",
	"empty/README.md":      "nothing here\n",
}

func writeFixture(t testing.TB) string {
	t.Helper()
	root := t.TempDir()
	for name, src := range fixture {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func loadFixture(t *testing.T, opts apiref.Options) *apiref.Reference {
	t.Helper()
	ref, err := apiref.Load(writeFixture(t), opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return ref
}

func findPackage(t *testing.T, ref *apiref.Reference, path string) apiref.Package {
	t.Helper()
	for _, p := range ref.Packages {
		if p.ImportPath == path {
			return p
		}
	}
	t.Fatalf("no package %s", path)
	return apiref.Package{}
}

func findType(t *testing.T, p apiref.Package, name string) apiref.Type {
	t.Helper()
	for _, ty := range p.Types {
		if ty.Name == name {
			return ty
		}
	}
	t.Fatalf("%s has no type %s", p.ImportPath, name)
	return apiref.Type{}
}

func findFunc(t *testing.T, fs []apiref.Func, name string) apiref.Func {
	t.Helper()
	for _, f := range fs {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no func %s", name)
	return apiref.Func{}
}

func TestLoadPackages(t *testing.T) {
	ref := loadFixture(t, apiref.Options{})
	if ref.Version != apiref.Version || ref.Module != "example.com/m" {
		t.Fatalf("version %d module %q", ref.Version, ref.Module)
	}
	var got []string
	for _, p := range ref.Packages {
		got = append(got, p.ImportPath+" "+p.Page)
	}
	want := []string{"example.com/m m", "example.com/m/sub sub", "example.com/m/sub/deeper sub/deeper", "example.com/m/webonly webonly"}
	if !slices.Equal(got, want) {
		t.Fatalf("packages = %q, want %q", got, want)
	}
	if p := findPackage(t, ref, "example.com/m/webonly"); !slices.Equal(p.Platforms, []string{"web"}) {
		t.Fatalf("webonly platforms = %q, want web", p.Platforms)
	}
	if p := findPackage(t, ref, "example.com/m"); p.Platforms != nil || p.Synopsis != "Package m is a fixture." || p.Dir != "." {
		t.Fatalf("m: platforms %q synopsis %q dir %q", p.Platforms, p.Synopsis, p.Dir)
	}
}

func TestLoadDeclarations(t *testing.T) {
	p := findPackage(t, loadFixture(t, apiref.Options{}), "example.com/m")

	if len(p.Consts) != 2 {
		t.Fatalf("consts = %+v, want the limits and LinuxOnly", p.Consts)
	}
	limits := p.Consts[0]
	if got := limits.Idents; !slices.Equal(got, []apiref.Ident{{Name: "MaxSides", Anchor: "MaxSides"}, {Name: "MinArea", Anchor: "MinArea"}}) {
		t.Fatalf("limits idents = %+v", got)
	}
	if limits.Doc != "Limits of a shape.\n" || limits.File != "m.go" || limits.Line != 11 || limits.Platforms != nil {
		t.Fatalf("limits = %+v", limits)
	}
	if !strings.Contains(limits.Decl, "// MaxSides is the most sides a shape has.\n\tMaxSides = 8") || strings.Contains(limits.Decl, "minSides") {
		t.Fatalf("limits decl = %q", limits.Decl)
	}
	linux := p.Consts[1]
	if linux.Idents[0].Name != "LinuxOnly" || !slices.Equal(linux.Platforms, []string{"linux", "android"}) {
		t.Fatalf("LinuxOnly = %+v, want linux and android", linux)
	}

	if len(p.Vars) != 1 || p.Vars[0].Idents[0].Anchor != "Default" || p.Vars[0].Decl != "var Default = Shape{Sides: 4}" {
		t.Fatalf("package vars = %+v, want Default", p.Vars)
	}

	platform := findFunc(t, p.Funcs, "Platform")
	if platform.Platforms != nil || platform.Doc != "Platform names the platform, natively.\n" || platform.Decl != "func Platform() string" {
		t.Fatalf("Platform = %+v, want the native declaration on every target", platform)
	}
	fsys := findFunc(t, p.Funcs, "FileSystem")
	if !slices.Equal(fsys.Platforms, []string{"linux", "macos", "windows", "android", "ios"}) {
		t.Fatalf("FileSystem platforms = %q", fsys.Platforms)
	}
	for _, f := range p.Funcs {
		if f.Name == "TestOnly" || f.Name == "init" {
			t.Fatalf("%s is documented", f.Name)
		}
	}

	shape := findType(t, p, "Shape")
	if shape.Anchor != "Shape" || shape.Doc != "Shape is a polygon.\n" {
		t.Fatalf("Shape = %+v", shape)
	}
	if !strings.HasPrefix(shape.Decl, "type Shape struct {\n\t// Sides is how many sides it has.\n\tSides") || !strings.Contains(shape.Decl, "sub.Thing // an embedded type") || !strings.Contains(shape.Decl, "// contains filtered or unexported fields") || strings.Contains(shape.Decl, "hidden") {
		t.Fatalf("Shape decl = %q", shape.Decl)
	}
	wantMembers := []apiref.Member{
		{Name: "Sides", Anchor: "Shape.Sides", Kind: "field", Doc: "Sides is how many sides it has."},
		{Name: "Thing", Anchor: "Shape.Thing", Kind: "field", Doc: "an embedded type"},
	}
	if !slices.Equal(shape.Members, wantMembers) {
		t.Fatalf("Shape members = %+v, want %+v", shape.Members, wantMembers)
	}
	if f := findFunc(t, shape.Funcs, "New"); f.Anchor != "New" || f.Recv != "" {
		t.Fatalf("New = %+v", f)
	}
	area := findFunc(t, shape.Methods, "Area")
	if area.Anchor != "Shape.Area" || area.Recv != "Shape" || area.Decl != "func (s Shape) Area() float64" {
		t.Fatalf("Area = %+v", area)
	}
	if len(shape.Methods) != 1 {
		t.Fatalf("Shape methods = %+v, want Area alone", shape.Methods)
	}

	kind := findType(t, p, "Kind")
	if len(kind.Consts) != 1 || len(kind.Consts[0].Idents) != 2 || kind.Consts[0].Idents[1].Anchor != "Triangle" {
		t.Fatalf("Kind consts = %+v", kind.Consts)
	}

	measurer := findType(t, p, "Measurer")
	if !strings.Contains(measurer.Decl, "// Measure measures.\n\tMeasure() float64") || !strings.Contains(measurer.Decl, "// contains filtered or unexported methods") {
		t.Fatalf("Measurer decl = %q", measurer.Decl)
	}
	if !slices.Equal(measurer.Members, []apiref.Member{{Name: "Measure", Anchor: "Measurer.Measure", Kind: "method", Doc: "Measure measures."}}) {
		t.Fatalf("Measurer members = %+v", measurer.Members)
	}

	set := findType(t, p, "Set")
	add := findFunc(t, set.Methods, "Add")
	if add.Anchor != "Set.Add" || add.Recv != "*Set[T]" || add.Platforms != nil {
		t.Fatalf("Add = %+v", add)
	}
	if len(set.Members) != 0 {
		t.Fatalf("Set members = %+v, want none", set.Members)
	}

	for _, ty := range p.Types {
		if ty.Name == "unexported" {
			t.Fatal("unexported type documented")
		}
	}
}

func TestDocHTML(t *testing.T) {
	if p := findPackage(t, loadFixture(t, apiref.Options{}), "example.com/m"); p.DocHTML != "" {
		t.Fatalf("DocHTML = %q without Options.HTML", p.DocHTML)
	}
	ref := loadFixture(t, apiref.Options{HTML: true})
	m := findPackage(t, ref, "example.com/m")
	for _, want := range []string{
		`<a href="../sub/#Thing">sub.Thing</a>`,
		`<a href="#Shape.Area">Shape.Area</a>`,
		`<a href="https://pkg.go.dev/io#Reader">io.Reader</a>`,
		`<h3 id="hdr-Usage">Usage</h3>`,
		`<a href="#New">New</a>`,
	} {
		if !strings.Contains(m.DocHTML, want) {
			t.Errorf("m DocHTML lacks %s:\n%s", want, m.DocHTML)
		}
	}
	if got := findPackage(t, ref, "example.com/m/sub").DocHTML; !strings.Contains(got, `<a href="../m/#Shape">m.Shape</a>`) {
		t.Errorf("sub DocHTML = %q, want a link to the root package's page", got)
	}
	if got := findPackage(t, ref, "example.com/m/sub/deeper").DocHTML; !strings.Contains(got, `<a href="../../sub/#Thing">`) {
		t.Errorf("deeper DocHTML = %q, want a link two pages up", got)
	}
	if got := findFunc(t, findType(t, m, "Shape").Methods, "Area").DocHTML; got != "<p>Area is the shape&apos;s area.\n" {
		t.Errorf("Area DocHTML = %q", got)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := apiref.Load(t.TempDir(), apiref.Options{}); err == nil {
		t.Error("Load without go.mod succeeded")
	}

	noModule := t.TempDir()
	if err := os.WriteFile(filepath.Join(noModule, "go.mod"), []byte("go 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := apiref.Load(noModule, apiref.Options{}); err == nil {
		t.Error("Load of a go.mod without a module directive succeeded")
	}

	clash := writeFixture(t)
	if err := os.WriteFile(filepath.Join(clash, "sub", "other.go"), []byte("package other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := apiref.Load(clash, apiref.Options{}); err == nil || !strings.Contains(err.Error(), "in one directory") {
		t.Errorf("Load of two packages in a directory = %v", err)
	}

	page := writeFixture(t)
	if err := os.MkdirAll(filepath.Join(page, "m"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(page, "m", "m.go"), []byte("package m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := apiref.Load(page, apiref.Options{}); err == nil || !strings.Contains(err.Error(), "share the page") {
		t.Errorf("Load of a directory named like the root package = %v", err)
	}

	broken := writeFixture(t)
	if err := os.WriteFile(filepath.Join(broken, "bad.go"), []byte("package m\n\nfunc {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := apiref.Load(broken, apiref.Options{}); err == nil {
		t.Error("Load of a file that does not parse succeeded")
	}
}

func TestMarshalDeterministicRoundTrip(t *testing.T) {
	root := writeFixture(t)
	var first []byte
	for i := range 2 {
		ref, err := apiref.Load(root, apiref.Options{HTML: true})
		if err != nil {
			t.Fatal(err)
		}
		data, err := apiref.Marshal(ref)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = data
			continue
		}
		if !bytes.Equal(first, data) {
			t.Fatal("two loads of one module marshal differently")
		}
	}
	ref, err := apiref.Unmarshal(first)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	again, err := apiref.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, again) {
		t.Fatal("Marshal(Unmarshal(data)) differs from data")
	}
	if !bytes.HasSuffix(first, []byte("}\n")) || bytes.Contains(first, []byte(`\u003c`)) {
		t.Fatal("Marshal escapes HTML or lacks the final newline")
	}
}

func TestUnmarshalRefuses(t *testing.T) {
	for name, data := range map[string]string{
		"unknown field":  `{"version":1,"module":"m","packages":[],"extra":1}`,
		"nested unknown": `{"version":1,"module":"m","packages":[{"importPath":"m","bogus":true}]}`,
		"old version":    `{"version":0,"module":"m","packages":[]}`,
		"newer version":  `{"version":2,"module":"m","packages":[]}`,
		"trailing data":  `{"version":1,"module":"m","packages":[]} {}`,
		"not JSON":       `version: 1`,
	} {
		if _, err := apiref.Unmarshal([]byte(data)); err == nil {
			t.Errorf("%s: Unmarshal succeeded", name)
		}
	}
	if _, err := apiref.Unmarshal([]byte(`{"version":1,"module":"m","packages":[]}`)); err != nil {
		t.Errorf("Unmarshal of a valid reference: %v", err)
	}
}

func FuzzUnmarshal(f *testing.F) {
	f.Add([]byte(`{"version":1,"module":"m","packages":[]}`))
	f.Add([]byte(`{"version":1,"module":"m","packages":[{"importPath":"m","name":"m","dir":".","page":"m","synopsis":"","doc":"","consts":[],"vars":[],"funcs":[],"types":[]}]}`))
	f.Add([]byte(`{"version":2}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		ref, err := apiref.Unmarshal(data)
		if err != nil {
			return
		}
		out, err := apiref.Marshal(ref)
		if err != nil {
			t.Fatalf("Marshal of an unmarshaled reference: %v", err)
		}
		back, err := apiref.Unmarshal(out)
		if err != nil {
			t.Fatalf("Unmarshal of Marshal's output: %v", err)
		}
		again, err := apiref.Marshal(back)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, again) {
			t.Fatalf("round trip changed the output:\n%s\n%s", out, again)
		}
	})
}

// TestModuleReference loads this module, the one the website documents, and checks against a parse of its own that every exported identifier any target builds, in a development or a release build, has exactly one anchor on its package's page.
func TestModuleReference(t *testing.T) {
	root := filepath.Join("..", "..")
	ref, err := apiref.Load(root, apiref.Options{HTML: true})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	data, err := apiref.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	back, err := apiref.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if again, _ := apiref.Marshal(back); !bytes.Equal(data, again) {
		t.Fatal("the module's reference does not round-trip")
	}

	documented := map[string]map[string]bool{}
	pages := map[string]bool{}
	for _, p := range ref.Packages {
		if pages[p.Page] {
			t.Errorf("page %s used twice", p.Page)
		}
		pages[p.Page] = true
		anchors := map[string]bool{}
		for _, a := range packageAnchors(p) {
			if a == "" {
				t.Errorf("%s: an identifier without an anchor", p.ImportPath)
			}
			if anchors[a] {
				t.Errorf("%s: anchor %s used twice", p.ImportPath, a)
			}
			anchors[a] = true
		}
		documented[p.ImportPath] = anchors
	}

	exported := exportedIdentifiers(t, root, ref.Module)
	if len(exported) == 0 {
		t.Fatal("found no packages to check")
	}
	missing := 0
	for path, idents := range exported {
		anchors, ok := documented[path]
		if !ok {
			t.Errorf("package %s is not in the reference", path)
			continue
		}
		for _, id := range idents {
			if !anchors[id] {
				t.Errorf("%s.%s has no anchor", path, id)
				missing++
			}
		}
	}
	for path := range documented {
		if _, ok := exported[path]; !ok {
			t.Errorf("the reference has %s, which exports nothing", path)
		}
	}
	if missing > 0 {
		t.Logf("%d identifiers missing", missing)
	}
}

// packageAnchors lists every anchor on a package's page.
func packageAnchors(p apiref.Package) []string {
	var out []string
	values := func(vs []apiref.Value) {
		for _, v := range vs {
			for _, id := range v.Idents {
				out = append(out, id.Anchor)
			}
		}
	}
	funcs := func(fs []apiref.Func) {
		for _, f := range fs {
			out = append(out, f.Anchor)
		}
	}
	values(p.Consts)
	values(p.Vars)
	funcs(p.Funcs)
	for _, t := range p.Types {
		out = append(out, t.Anchor)
		for _, m := range t.Members {
			out = append(out, m.Anchor)
		}
		values(t.Consts)
		values(t.Vars)
		funcs(t.Funcs)
		funcs(t.Methods)
	}
	return out
}

// exportedIdentifiers parses each importable package of the module without go/doc and lists its exported identifiers as anchors: top-level names, Type.Method for the methods of exported types, and Type.Field or Type.Method for the fields of exported structs and methods of exported interfaces.
func exportedIdentifiers(t *testing.T, root, module string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel != "." {
			if n := d.Name(); n == "internal" || n == "testdata" || n == "vendor" || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return filepath.SkipDir
			}
		}
		fset := token.NewFileSet()
		seen := map[string]bool{}
		var idents []string
		add := func(id string) {
			if !seen[id] {
				seen[id] = true
				idents = append(idents, id)
			}
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		isMain := false
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || !builtByATarget(t, p, name) {
				continue
			}
			f, err := parser.ParseFile(fset, filepath.Join(p, name), nil, 0)
			if err != nil {
				return err
			}
			if f.Name.Name == "main" {
				isMain = true
				break
			}
			collectExported(f, add)
		}
		if isMain || len(idents) == 0 {
			return nil
		}
		path := module
		if rel != "." {
			path += "/" + rel
		}
		out[path] = idents
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func builtByATarget(t *testing.T, dir, name string) bool {
	t.Helper()
	for _, target := range apiref.Targets {
		for _, tags := range [][]string{nil, {"release"}} {
			ctx := build.Default
			ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled, ctx.BuildTags, ctx.ToolTags = target.GOOS, target.GOARCH, false, tags, nil
			ok, err := ctx.MatchFile(dir, name)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				return true
			}
		}
	}
	return false
}

func collectExported(f *ast.File, add func(string)) {
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			if d.Recv == nil {
				add(d.Name.Name)
				continue
			}
			if recv := receiverName(d.Recv.List[0].Type); token.IsExported(recv) {
				add(recv + "." + d.Name.Name)
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.IsExported() {
							add(n.Name)
						}
					}
				case *ast.TypeSpec:
					if !s.Name.IsExported() {
						continue
					}
					add(s.Name.Name)
					switch x := s.Type.(type) {
					case *ast.StructType:
						for _, field := range x.Fields.List {
							for _, n := range fieldNames(field) {
								if token.IsExported(n) {
									add(s.Name.Name + "." + n)
								}
							}
						}
					case *ast.InterfaceType:
						for _, m := range x.Methods.List {
							if _, ok := m.Type.(*ast.FuncType); !ok {
								continue
							}
							for _, n := range m.Names {
								if n.IsExported() {
									add(s.Name.Name + "." + n.Name)
								}
							}
						}
					}
				}
			}
		}
	}
}

func receiverName(x ast.Expr) string {
	switch t := x.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.ParenExpr:
		return receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

func fieldNames(f *ast.Field) []string {
	if len(f.Names) > 0 {
		names := make([]string, len(f.Names))
		for i, n := range f.Names {
			names[i] = n.Name
		}
		return names
	}
	x := f.Type
	for {
		switch t := x.(type) {
		case *ast.StarExpr:
			x = t.X
		case *ast.IndexExpr:
			x = t.X
		case *ast.IndexListExpr:
			x = t.X
		case *ast.SelectorExpr:
			return []string{t.Sel.Name}
		case *ast.Ident:
			return []string{t.Name}
		default:
			return nil
		}
	}
}
