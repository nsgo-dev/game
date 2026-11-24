package game_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	// jsonImport is the JSON package every JSON format in this module decodes with.
	jsonImport = "encoding/json"
	// formatMarker starts the line that marks an entry on the File Formats page; the entry's ID and " -->" follow.
	formatMarker = "<!-- format: "
)

// notJSONFormats are the packages that decode JSON without defining a file format of their own, each with the reason, so the File Formats page need not list them. Every other package that decodes JSON must have an entry there.
var notJSONFormats = map[string]string{
	"accounts":            "decodes the JSON bodies of its HTTP API, which is data on the wire",
	"accounts/token":      "decodes the JSON payload of a signed token, which is data on the wire",
	"idle":                "decodes a Number from a JSON value inside another format",
	"internal/samplegltf": "validates the sample glTF models, glTF being the gltf entry's format",
	"loaderr":             "decodes a document only to find a field's line and column for an error",
	"refs":                "scans other formats' JSON documents for file references",
	"rooms":               "reads LDtk projects, the format of the tilemap#ldtk entry",
	"tooling/assetcheck":  "scans other formats' JSON documents for asset references",
}

// formatEntry is an entry of the File Formats page: its ID, as in "<!-- format: ID -->", and the line it is marked on.
type formatEntry struct {
	id   string
	line int
}

// readFormatEntries returns the entries docs/<lang>/formats.md marks, in page order.
func readFormatEntries(t *testing.T, lang string) []formatEntry {
	t.Helper()
	name := "docs/" + lang + "/formats.md"
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", lang, "formats.md"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []formatEntry
	for i, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, formatMarker) {
			continue
		}
		id, ok := strings.CutSuffix(strings.TrimPrefix(line, formatMarker), " -->")
		if !ok || id == "" || strings.ContainsAny(id, " \t") {
			t.Errorf("%s:%d: malformed entry marker %q; write <!-- format: <package>[#<name>] -->", name, i+1, line)
			continue
		}
		entries = append(entries, formatEntry{id: id, line: i + 1})
	}
	if len(entries) == 0 {
		t.Fatalf("%s marks no entries", name)
	}
	return entries
}

// walkSources calls visit with the slash path of the package directory and the parsed syntax of every non-test Go file of the module, skipping testdata, hidden and underscored directories and other modules.
func walkSources(t *testing.T, visit func(pkg string, f *ast.File)) {
	t.Helper()
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
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		visit(filepath.ToSlash(filepath.Dir(p)), f)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// jsonDecoders returns, by slash path relative to the module root, the packages whose non-test files call encoding/json's Unmarshal or NewDecoder.
func jsonDecoders(t *testing.T) []string {
	t.Helper()
	var pkgs []string
	walkSources(t, func(pkg string, f *ast.File) {
		if slices.Contains(pkgs, pkg) {
			return
		}
		jsonName, ok := importName(f, jsonImport)
		if !ok {
			return
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == jsonName && (sel.Sel.Name == "Unmarshal" || sel.Sel.Name == "NewDecoder") {
					if !slices.Contains(pkgs, pkg) {
						pkgs = append(pkgs, pkg)
					}
					return false
				}
			}
			return true
		})
	})
	slices.Sort(pkgs)
	return pkgs
}

// conformanceDocs returns the Doc of every formattest.Run call in the module's tests, by the test file that makes it. A call whose Format has no Doc string literal is reported with an empty Doc.
func conformanceDocs(t *testing.T) map[string][]string {
	t.Helper()
	docs := map[string][]string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		name, ok := importName(f, formattestImport)
		if !ok {
			return nil
		}
		file := filepath.ToSlash(p)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Run" {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != name {
				return true
			}
			doc := ""
			if lit, ok := call.Args[1].(*ast.CompositeLit); ok {
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Doc" {
						if s, ok := kv.Value.(*ast.BasicLit); ok && s.Kind == token.STRING {
							doc, _ = strconv.Unquote(s.Value)
						}
					}
				}
			}
			docs[file] = append(docs[file], doc)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

// TestFormatsPage checks the File Formats page, docs/en/formats.md and docs/zh/formats.md, against the code: both pages mark the same entries in the same order, each entry names a package of the module, every YAML format that runs the conformance checks has its entry (whose examples formattest.Run checks), every package that decodes JSON has an entry unless notJSONFormats lists it, and an entry for a package that runs no conformance check is one that decodes JSON, so an entry outlives neither its format nor its package.
func TestFormatsPage(t *testing.T) {
	en := readFormatEntries(t, "en")
	zh := readFormatEntries(t, "zh")
	ids := func(entries []formatEntry) []string {
		var out []string
		for _, e := range entries {
			out = append(out, e.id)
		}
		return out
	}
	if !slices.Equal(ids(en), ids(zh)) {
		t.Errorf("docs/en/formats.md and docs/zh/formats.md mark different entries or order them differently:\nen: %v\nzh: %v", ids(en), ids(zh))
	}
	onPage := map[string]bool{}
	pkgsOnPage := map[string]bool{}
	for _, e := range en {
		if onPage[e.id] {
			t.Errorf("docs/en/formats.md:%d: entry %s is marked twice", e.line, e.id)
		}
		onPage[e.id] = true
		pkg, _, _ := strings.Cut(e.id, "#")
		pkgsOnPage[pkg] = true
		if goFiles, _ := filepath.Glob(filepath.Join(filepath.FromSlash(pkg), "*.go")); len(goFiles) == 0 {
			t.Errorf("docs/en/formats.md:%d: entry %s names %s, which is not a package of the module", e.line, e.id, pkg)
		}
	}

	conformance := map[string]bool{}
	docs := conformanceDocs(t)
	if len(docs) == 0 {
		t.Fatal("found no formattest.Run call; the scan is broken")
	}
	for file, list := range docs {
		for _, doc := range list {
			if doc == "" {
				t.Errorf("%s: a formattest.Run call has no Doc string literal; name the format's entry on docs/en/formats.md", file)
				continue
			}
			conformance[doc] = true
			if !onPage[doc] {
				t.Errorf("%s runs the conformance checks of %s, which docs/en/formats.md has no entry for; add one marked <!-- format: %s --> to both pages", file, doc, doc)
			}
		}
	}

	decoders := jsonDecoders(t)
	if len(decoders) == 0 {
		t.Fatal("found no package that decodes JSON; the scan is broken")
	}
	for pkg := range notJSONFormats {
		if !slices.Contains(decoders, pkg) {
			t.Errorf("notJSONFormats lists %s, which no longer decodes JSON; remove it", pkg)
		}
	}
	for _, pkg := range decoders {
		if _, ok := notJSONFormats[pkg]; ok || pkgsOnPage[pkg] {
			continue
		}
		t.Errorf("%s decodes JSON but docs/en/formats.md has no entry for it; add one marked <!-- format: %s --> to both pages, or, if it reads no file format of its own, list it in notJSONFormats with the reason", pkg, pkg)
	}
	for _, e := range en {
		pkg, _, _ := strings.Cut(e.id, "#")
		if !conformance[e.id] && !slices.Contains(decoders, pkg) {
			t.Errorf("docs/en/formats.md:%d: entry %s is neither a YAML format whose conformance checks name it nor in a package that decodes JSON; remove it or name it in its format's formattest.Run Doc", e.line, e.id)
		}
	}
}
