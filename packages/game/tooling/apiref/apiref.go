// Package apiref reads the exported API of a Go module's packages from their source and doc comments, as go doc shows it, into a Reference: the JSON the website renders as its API pages and publishes for agents.
//
// Load walks the module from its root and documents every importable package: it leaves out packages main, internal directories, testdata, directories starting with . or _, and nested modules. A package's files are those any NSGo target builds (linux, macos, windows, android, ios and web, without the release tag), so the API a target adds, such as one only native targets build, is in the reference with the targets that have it in Platforms.
//
// Every exported identifier has an anchor, the fragment of its place on its package's page, as pkg.go.dev names them: Name for a constant, variable, function or type, Type.Name for a method, a struct field or an interface method.
package apiref

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/doc/comment"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Version is the version of the Reference format, in its version field. A change that removes or renames a field, or changes what one holds, raises it.
const Version = 1

// Target is an NSGo target a package's files are chosen for: its name as the nsgo tool spells it and the GOOS and GOARCH it builds for.
type Target struct {
	Name   string
	GOOS   string
	GOARCH string
}

// Targets are the targets Load reads each package for, in the order Platforms lists them. Where two targets' files declare the same identifier, as a native and a web implementation of one function do, the reference shows the declaration of the earlier target.
var Targets = []Target{
	{Name: "linux", GOOS: "linux", GOARCH: "amd64"},
	{Name: "macos", GOOS: "darwin", GOARCH: "arm64"},
	{Name: "windows", GOOS: "windows", GOARCH: "amd64"},
	{Name: "android", GOOS: "android", GOARCH: "arm64"},
	{Name: "ios", GOOS: "ios", GOARCH: "arm64"},
	{Name: "web", GOOS: "js", GOARCH: "wasm"},
}

// Reference is the exported API of a module's packages.
type Reference struct {
	// Version is the format's version, Version.
	Version int `json:"version"`
	// Module is the module's path, from its go.mod.
	Module string `json:"module"`
	// Packages are the module's importable packages, by import path.
	Packages []Package `json:"packages"`
}

// Package is the exported API of one package, grouped as go doc groups it: a type's constants, variables, constructors and methods are under the type rather than the package.
type Package struct {
	ImportPath string `json:"importPath"`
	Name       string `json:"name"`
	// Dir is the package's directory below the module's root, with slashes, "." for the root.
	Dir string `json:"dir"`
	// Page is the path of the package's page below the reference's root: Dir, or Name for the module's root package. DocHTML links another package of the module by the path from one page to the other, each page a directory.
	Page string `json:"page"`
	// Synopsis is the first sentence of Doc.
	Synopsis string `json:"synopsis"`
	// Doc is the package's doc comment as written, without the comment markers.
	Doc string `json:"doc"`
	// DocHTML is Doc rendered as HTML, when Options.HTML is set.
	DocHTML string `json:"docHTML,omitempty"`
	// Platforms are the targets that build the package, empty when every target does.
	Platforms []string `json:"platforms,omitempty"`
	Consts    []Value  `json:"consts"`
	Vars      []Value  `json:"vars"`
	Funcs     []Func   `json:"funcs"`
	Types     []Type   `json:"types"`
}

// Ident is an exported identifier and its anchor.
type Ident struct {
	Name   string `json:"name"`
	Anchor string `json:"anchor"`
}

// Value is a declaration of constants or variables: one or a parenthesized group, which shares a doc comment.
type Value struct {
	// Idents are the exported names the declaration declares, in order.
	Idents []Ident `json:"idents"`
	// Decl is the declaration's source, gofmt-formatted, with the comments on its specs.
	Decl    string `json:"decl"`
	Doc     string `json:"doc"`
	DocHTML string `json:"docHTML,omitempty"`
	// File is the path of the declaration's file below the module's root, with slashes, and Line its line.
	File string `json:"file"`
	Line int    `json:"line"`
	// Platforms are the targets that declare it, empty when every target that builds the package does.
	Platforms []string `json:"platforms,omitempty"`
}

// Func is a function or a method.
type Func struct {
	Name   string `json:"name"`
	Anchor string `json:"anchor"`
	// Recv is a method's receiver type as written, such as *Batch, and empty for a function.
	Recv string `json:"recv,omitempty"`
	// Decl is the signature, without the body.
	Decl      string   `json:"decl"`
	Doc       string   `json:"doc"`
	DocHTML   string   `json:"docHTML,omitempty"`
	File      string   `json:"file"`
	Line      int      `json:"line"`
	Platforms []string `json:"platforms,omitempty"`
}

// Type is a type with the declarations go doc lists under it.
type Type struct {
	Name   string `json:"name"`
	Anchor string `json:"anchor"`
	// Decl is the type's declaration with its exported fields or methods and their comments.
	Decl      string   `json:"decl"`
	Doc       string   `json:"doc"`
	DocHTML   string   `json:"docHTML,omitempty"`
	File      string   `json:"file"`
	Line      int      `json:"line"`
	Platforms []string `json:"platforms,omitempty"`
	// Members are a struct's exported fields, embedded ones by their type's name, or an interface's methods.
	Members []Member `json:"members"`
	// Consts and Vars are the declarations of values of the type.
	Consts []Value `json:"consts"`
	Vars   []Value `json:"vars"`
	// Funcs are the functions that return the type.
	Funcs []Func `json:"funcs"`
	// Methods are the type's exported methods, with those promoted from an unexported embedded type.
	Methods []Func `json:"methods"`
}

// Member is a struct field or an interface method.
type Member struct {
	Name   string `json:"name"`
	Anchor string `json:"anchor"`
	// Kind is "field" or "method".
	Kind string `json:"kind"`
	// Doc is the member's comments, the one above it then the one after it on its line.
	Doc string `json:"doc"`
}

// Options are what Load adds to the reference.
type Options struct {
	// HTML renders each doc comment as HTML into its DocHTML. A doc link to an identifier of the same package is its anchor, to another package of the module the path to that package's page, and to any other package its page on pkg.go.dev.
	HTML bool
}

// pkgGoDev is where a doc link to a package outside the module points.
const pkgGoDev = "https://pkg.go.dev"

// Load reads the reference of the module whose go.mod is in root.
func Load(root string, opts Options) (*Reference, error) {
	module, err := modulePath(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, err
	}
	dirs, err := packageDirs(root)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var parsed []*parsedPackage
	for _, dir := range dirs {
		p, err := parsePackage(fset, root, dir, module)
		if err != nil {
			return nil, err
		}
		if p != nil {
			parsed = append(parsed, p)
		}
	}
	slices.SortFunc(parsed, func(a, b *parsedPackage) int { return cmp.Compare(a.importPath, b.importPath) })

	pages := make(map[string]string, len(parsed))
	owner := make(map[string]string, len(parsed))
	for _, p := range parsed {
		page := p.dir
		if page == "." {
			page = p.doc.Name
		}
		if other, ok := owner[page]; ok {
			return nil, fmt.Errorf("apiref: packages %s and %s would share the page %q", other, p.importPath, page)
		}
		owner[page] = p.importPath
		pages[p.importPath] = page
	}

	ref := &Reference{Version: Version, Module: module, Packages: make([]Package, 0, len(parsed))}
	for _, p := range parsed {
		b := &builder{p: p, fset: fset, root: root, opts: opts, page: pages[p.importPath], pages: pages}
		ref.Packages = append(ref.Packages, b.build())
	}
	return ref, nil
}

// Marshal encodes ref as indented JSON ending in a newline. The output depends only on ref, so the same source always gives the same bytes.
func Marshal(ref *Reference) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ref); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Unmarshal decodes a reference Marshal wrote, refusing unknown fields and any version but Version.
func Unmarshal(data []byte) (*Reference, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var ref Reference
	if err := dec.Decode(&ref); err != nil {
		return nil, fmt.Errorf("apiref: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("apiref: data after the reference")
	}
	if ref.Version != Version {
		return nil, fmt.Errorf("apiref: version %d, want %d", ref.Version, Version)
	}
	return &ref, nil
}

// modulePath reads the module directive of a go.mod.
func modulePath(gomod string) (string, error) {
	f, err := os.Open(gomod)
	if err != nil {
		return "", fmt.Errorf("apiref: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		rest, ok := strings.CutPrefix(line, "module")
		if !ok || (rest != "" && rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		rest, _, _ = strings.Cut(rest, "//")
		mod := strings.Trim(strings.TrimSpace(rest), `"`)
		if mod != "" {
			return mod, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("apiref: read %s: %w", gomod, err)
	}
	return "", fmt.Errorf("apiref: %s has no module directive", gomod)
}

// packageDirs lists the directories below root, root included, that may hold an importable package of the module, as slash paths.
func packageDirs(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel != "." {
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "internal" || name == "vendor" {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return filepath.SkipDir
			}
		}
		dirs = append(dirs, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("apiref: %w", err)
	}
	return dirs, nil
}

// parsedPackage is a package's files read for every target.
type parsedPackage struct {
	dir        string
	importPath string
	doc        *doc.Package
	// files are the package's files by their name in fset, for the comments of a declaration.
	files map[string]*ast.File
	// targets are the indexes into Targets of the targets that build the package.
	targets []int
	// declared maps the key of each top-level declaration, Name or Recv.Name for a method, to the indexes of the targets that declare it.
	declared map[string][]int
}

// parsePackage reads the package in dir for every target, or returns nil when it has no files any target builds or is a command.
func parsePackage(fset *token.FileSet, root, dir, module string) (*parsedPackage, error) {
	abs := filepath.Join(root, filepath.FromSlash(dir))
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("apiref: %w", err)
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.Type().IsRegular() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") && !strings.HasPrefix(n, ".") && !strings.HasPrefix(n, "_") {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}

	importPath := module
	if dir != "." {
		importPath = module + "/" + dir
	}
	p := &parsedPackage{dir: dir, importPath: importPath, files: map[string]*ast.File{}, declared: map[string][]int{}}
	parsed := map[string]*ast.File{}
	// order is the files in the order they are added to the package: each target's in turn, those an earlier target already added skipped.
	var order []string
	for ti, t := range Targets {
		ctx := build.Default
		ctx.GOOS, ctx.GOARCH = t.GOOS, t.GOARCH
		ctx.CgoEnabled = false
		ctx.BuildTags, ctx.ToolTags = nil, nil
		built := false
		for _, n := range names {
			ok, err := ctx.MatchFile(abs, n)
			if err != nil {
				return nil, fmt.Errorf("apiref: %s: %w", path.Join(dir, n), err)
			}
			if !ok {
				continue
			}
			f := parsed[n]
			if f == nil {
				f, err = parser.ParseFile(fset, filepath.Join(abs, n), nil, parser.ParseComments)
				if err != nil {
					return nil, fmt.Errorf("apiref: %w", err)
				}
				parsed[n] = f
				order = append(order, n)
			}
			built = true
			for _, key := range declKeys(f) {
				if ds := p.declared[key]; len(ds) == 0 || ds[len(ds)-1] != ti {
					p.declared[key] = append(ds, ti)
				}
			}
		}
		if built {
			p.targets = append(p.targets, ti)
		}
	}
	if len(order) == 0 {
		return nil, nil
	}

	var files []*ast.File
	seen := map[string]bool{}
	name := ""
	for _, n := range order {
		f := parsed[n]
		if name == "" {
			name = f.Name.Name
		} else if f.Name.Name != name {
			return nil, fmt.Errorf("apiref: %s: package %s and %s in one directory", dir, name, f.Name.Name)
		}
		dropDeclared(f, seen)
		for _, key := range declKeys(f) {
			seen[key] = true
		}
		files = append(files, f)
		p.files[fset.File(f.Pos()).Name()] = f
	}
	if name == "main" {
		return nil, nil
	}
	p.doc, err = doc.NewFromFiles(fset, files, importPath)
	if err != nil {
		return nil, fmt.Errorf("apiref: %s: %w", importPath, err)
	}
	return p, nil
}

// declKeys lists the keys of a file's top-level declarations: Name, or Recv.Name for a method. init and blank names have none, as they may be declared more than once.
func declKeys(f *ast.File) []string {
	var keys []string
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if key := funcKey(d); key != "" {
				keys = append(keys, key)
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				keys = append(keys, specKeys(s)...)
			}
		}
	}
	return keys
}

func funcKey(d *ast.FuncDecl) string {
	name := d.Name.Name
	if name == "_" || (d.Recv == nil && name == "init") {
		return ""
	}
	if d.Recv != nil && len(d.Recv.List) == 1 {
		return recvBase(d.Recv.List[0].Type) + "." + name
	}
	return name
}

func specKeys(s ast.Spec) []string {
	switch s := s.(type) {
	case *ast.TypeSpec:
		return []string{s.Name.Name}
	case *ast.ValueSpec:
		var keys []string
		for _, n := range s.Names {
			if n.Name != "_" {
				keys = append(keys, n.Name)
			}
		}
		return keys
	}
	return nil
}

// recvBase is the name of a receiver's type without a pointer or type parameters.
func recvBase(x ast.Expr) string {
	for {
		switch t := x.(type) {
		case *ast.StarExpr:
			x = t.X
		case *ast.ParenExpr:
			x = t.X
		case *ast.IndexExpr:
			x = t.X
		case *ast.IndexListExpr:
			x = t.X
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

// dropDeclared removes from f the declarations of keys an earlier target's file already declared, so each identifier is documented once.
func dropDeclared(f *ast.File, seen map[string]bool) {
	decls := f.Decls[:0]
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if key := funcKey(d); key != "" && seen[key] {
				continue
			}
		case *ast.GenDecl:
			if d.Tok != token.IMPORT {
				specs := d.Specs[:0]
				for _, s := range d.Specs {
					if !slices.ContainsFunc(specKeys(s), func(k string) bool { return seen[k] }) {
						specs = append(specs, s)
					}
				}
				d.Specs = specs
				if len(specs) == 0 {
					continue
				}
			}
		}
		decls = append(decls, d)
	}
	f.Decls = decls
}

// builder turns one parsed package into its Package.
type builder struct {
	p     *parsedPackage
	fset  *token.FileSet
	root  string
	opts  Options
	page  string
	pages map[string]string
}

func (b *builder) build() Package {
	d := b.p.doc
	pkg := Package{
		ImportPath: b.p.importPath,
		Name:       d.Name,
		Dir:        b.p.dir,
		Page:       b.page,
		Synopsis:   d.Synopsis(d.Doc),
		Doc:        d.Doc,
		DocHTML:    b.html(d.Doc),
		Consts:     b.values(d.Consts),
		Vars:       b.values(d.Vars),
		Funcs:      b.funcs(d.Funcs, ""),
		Types:      make([]Type, 0, len(d.Types)),
	}
	if len(b.p.targets) < len(Targets) {
		pkg.Platforms = targetNames(b.p.targets)
	}
	for _, t := range d.Types {
		file, line := b.position(t.Decl)
		pkg.Types = append(pkg.Types, Type{
			Name:      t.Name,
			Anchor:    t.Name,
			Decl:      b.source(t.Decl),
			Doc:       t.Doc,
			DocHTML:   b.html(t.Doc),
			File:      file,
			Line:      line,
			Platforms: b.platforms(t.Name),
			Members:   members(t),
			Consts:    b.values(t.Consts),
			Vars:      b.values(t.Vars),
			Funcs:     b.funcs(t.Funcs, ""),
			Methods:   b.funcs(t.Methods, t.Name),
		})
	}
	return pkg
}

func (b *builder) values(vs []*doc.Value) []Value {
	out := make([]Value, 0, len(vs))
	for _, v := range vs {
		var idents []Ident
		var keys []string
		for _, s := range v.Decl.Specs {
			for _, n := range s.(*ast.ValueSpec).Names {
				if n.IsExported() {
					idents = append(idents, Ident{Name: n.Name, Anchor: n.Name})
					keys = append(keys, n.Name)
				}
			}
		}
		if len(idents) == 0 {
			continue
		}
		file, line := b.position(v.Decl)
		out = append(out, Value{
			Idents:    idents,
			Decl:      b.source(v.Decl),
			Doc:       v.Doc,
			DocHTML:   b.html(v.Doc),
			File:      file,
			Line:      line,
			Platforms: b.platforms(keys...),
		})
	}
	return out
}

// funcs converts functions, or the methods of the type named typ.
func (b *builder) funcs(fs []*doc.Func, typ string) []Func {
	out := make([]Func, 0, len(fs))
	for _, f := range fs {
		anchor := f.Name
		if typ != "" {
			anchor = typ + "." + f.Name
		}
		file, line := b.position(f.Decl)
		out = append(out, Func{
			Name:      f.Name,
			Anchor:    anchor,
			Recv:      f.Recv,
			Decl:      b.source(f.Decl),
			Doc:       f.Doc,
			DocHTML:   b.html(f.Doc),
			File:      file,
			Line:      line,
			Platforms: b.platforms(anchor),
		})
	}
	return out
}

// members lists a type's exported fields or interface methods, in order.
func members(t *doc.Type) []Member {
	out := []Member{}
	for _, s := range t.Decl.Specs {
		ts, ok := s.(*ast.TypeSpec)
		if !ok || ts.Name.Name != t.Name {
			continue
		}
		var list *ast.FieldList
		kind := "field"
		switch x := ts.Type.(type) {
		case *ast.StructType:
			list = x.Fields
		case *ast.InterfaceType:
			list, kind = x.Methods, "method"
		}
		if list == nil {
			continue
		}
		for _, f := range list.List {
			var names []string
			switch {
			case len(f.Names) > 0:
				for _, n := range f.Names {
					names = append(names, n.Name)
				}
			case kind == "field":
				names = []string{embeddedName(f.Type)}
			}
			if kind == "method" {
				if _, isFunc := f.Type.(*ast.FuncType); !isFunc {
					continue
				}
			}
			text := strings.TrimSpace(strings.TrimSpace(f.Doc.Text()) + "\n" + strings.TrimSpace(f.Comment.Text()))
			for _, n := range names {
				if token.IsExported(n) {
					out = append(out, Member{Name: n, Anchor: t.Name + "." + n, Kind: kind, Doc: text})
				}
			}
		}
	}
	return out
}

// embeddedName is the field name of an embedded type: its name without a pointer, package or type arguments.
func embeddedName(x ast.Expr) string {
	for {
		switch t := x.(type) {
		case *ast.StarExpr:
			x = t.X
		case *ast.ParenExpr:
			x = t.X
		case *ast.IndexExpr:
			x = t.X
		case *ast.IndexListExpr:
			x = t.X
		case *ast.SelectorExpr:
			return t.Sel.Name
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

// platforms is the targets that declare every one of keys, nil when they are all the targets that build the package or a key is not declared at the top level, as a method promoted from an embedded type is not.
func (b *builder) platforms(keys ...string) []string {
	var common []int
	for i, key := range keys {
		ts, ok := b.p.declared[key]
		if !ok {
			return nil
		}
		if i == 0 {
			common = slices.Clone(ts)
			continue
		}
		common = slices.DeleteFunc(common, func(t int) bool { return !slices.Contains(ts, t) })
	}
	if len(common) >= len(b.p.targets) {
		return nil
	}
	return targetNames(common)
}

func targetNames(idx []int) []string {
	names := make([]string, 0, len(idx))
	for _, i := range idx {
		names = append(names, Targets[i].Name)
	}
	return names
}

// position is the file, relative to the module's root, and line of a declaration.
func (b *builder) position(n ast.Node) (string, int) {
	pos := b.fset.Position(n.Pos())
	rel, err := filepath.Rel(b.root, pos.Filename)
	if err != nil {
		rel = filepath.Base(pos.Filename)
	}
	return filepath.ToSlash(rel), pos.Line
}

// source prints a declaration as gofmt does, with the comments on its fields and specs and, where go/doc filtered unexported fields or methods out, the line saying so; its doc comment, which go/doc moved to Doc, is left out. The node is printed without printer.CommentedNode, which would turn off the printer's use of the comments attached to the nodes, the line go/doc adds among them.
func (b *builder) source(n ast.Node) string {
	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, b.fset, n); err != nil {
		// The printer fails only on a node it cannot print or a writer that fails, and a parsed declaration and a bytes.Buffer are neither.
		panic(fmt.Sprintf("apiref: print %s: %v", b.p.importPath, err))
	}
	return buf.String()
}

// html renders a doc comment, or returns "" without Options.HTML.
func (b *builder) html(text string) string {
	if !b.opts.HTML || text == "" {
		return ""
	}
	d := b.p.doc
	pr := d.Printer()
	pr.DocLinkURL = b.linkURL
	return string(pr.HTML(d.Parser().Parse(text)))
}

// linkURL is where a doc link points: an anchor on this page, the page of another of the module's packages relative to this one, or pkg.go.dev.
func (b *builder) linkURL(l *comment.DocLink) string {
	if l.ImportPath == "" || l.ImportPath == b.p.importPath {
		if l.Name == "" {
			return "./"
		}
		local := *l
		local.ImportPath = ""
		return local.DefaultURL("")
	}
	page, ok := b.pages[l.ImportPath]
	if !ok {
		return l.DefaultURL(pkgGoDev)
	}
	rel := strings.Repeat("../", strings.Count(b.page, "/")+1) + page + "/"
	if l.Name == "" {
		return rel
	}
	local := *l
	local.ImportPath = ""
	return rel + local.DefaultURL("")
}
