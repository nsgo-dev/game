// Package formattest checks, in a format package's tests, that a YAML format this repository defines follows the rules in AGENTS.md: it refuses unknown fields and versions it does not support, writes the same bytes every time it saves the same value, and loads and saves each of its testdata files without changing a byte. It also checks that the format has an entry on the File Formats reference page, docs/en/formats.md and docs/zh/formats.md, whose examples load and save to the same bytes.
//
// Every package that defines such a format runs [Run] from a test, and TestFormatPackagesRunConformance in internal/repocheck fails a package that decodes YAML and does not, unless it is listed there as one that reads no format of its own.
package formattest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// UpdateEnv is the environment variable that, set to 1, makes [Run] rewrite each testdata file with what the format saves for it before checking it, to write a new fixture in the format's canonical form. The other checks still run, so a format that saves different bytes each time still fails.
const UpdateEnv = "FORMATTEST_UPDATE"

// unknownField is the top-level key the unknown-field check adds to a document. No format defines it.
const unknownField = "formattestUnknownField"

// saves is how many times the determinism check saves one value; a format that writes a map in Go's random iteration order gives different bytes within that many saves.
const saves = 8

// Format describes one YAML format for [Run]: how to load and save it, and the testdata files that hold documents in it.
type Format[T any] struct {
	// Decode loads a document, as the format's Parse function does. It must refuse a document with an unknown top-level field, with no version field, with a version of 0 and with a version newer than the one it supports, naming the field in its error.
	Decode func(data []byte) (T, error)
	// Encode saves a value, as the format's Encode function does.
	Encode func(v T) ([]byte, error)
	// Files are glob patterns, relative to the package directory, of the testdata files to check; together they must match at least one file. Each file must be in the format's canonical form, the bytes Encode writes for it.
	Files []string
	// VersionField is the top-level key that holds the document's schema version, an integer; empty means "version".
	VersionField string
	// MaxVersion is the newest schema version the format supports; when set, the future-version check tests max(document version, MaxVersion) + 1 instead of the document's version + 1, so a multi-version format can check older fixtures against an unsupported future version.
	MaxVersion int
	// Doc names the format's entry on the reference pages docs/en/formats.md and docs/zh/formats.md: the package's path relative to the module root, such as "quest" or "importsettings", followed by "#" and a name of the format when the package defines more than one, such as "achievements#progress". Each page marks the entry with the line "<!-- format: Doc -->" before its heading, and every YAML code block in the entry must load and save to the same bytes, so the page never shows an example the format refuses or would rewrite. Run requires it.
	Doc string
}

// DocPages are the reference pages that list every format, relative to the module root. [Run] checks the entry [Format.Doc] names on each.
var DocPages = []string{"docs/en/formats.md", "docs/zh/formats.md"}

// Marker is how the line that marks a format's entry on a reference page starts; the entry's ID and " -->" follow, as in "<!-- format: quest -->".
const Marker = "<!-- format: "

// page is a reference page's name, for messages, and contents.
type page struct {
	name string
	data []byte
}

// check is one named conformance check of one format.
type check struct {
	name string
	run  func() error
}

// Run checks f, each rule in a subtest of its own: that Decode refuses an unknown field, a missing version, a version of 0 and a future version in each testdata file; that Encode writes the same bytes each time it saves a value, including one it loaded from its own output; and that each testdata file loads and saves to the same bytes.
//
// It also checks the entry f.Doc names on each of [DocPages]: that the entry exists once and has at least one YAML example, and that each example loads and saves to the same bytes. The test must run in the format's package directory, as go test runs it.
func Run[T any](t *testing.T, f Format[T]) {
	t.Helper()
	checks, err := f.checks(os.Getenv(UpdateEnv) == "1")
	if err != nil {
		t.Fatal(err)
	}
	pkg, pages, err := docPages()
	if err != nil {
		t.Fatal(err)
	}
	docs, err := f.docChecks(pkg, pages)
	if err != nil {
		t.Fatal(err)
	}
	runChecks(t, append(checks, docs...))
}

// runChecks runs each check in a subtest named after it.
func runChecks(t *testing.T, checks []check) {
	t.Helper()
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if err := c.run(); err != nil {
				t.Error(err)
			}
		})
	}
}

// checks returns the checks of every testdata file of f, after rewriting each file with its canonical form when update is set.
func (f Format[T]) checks(update bool) ([]check, error) {
	if f.Decode == nil || f.Encode == nil {
		return nil, errors.New("formattest: Format needs both Decode and Encode")
	}
	files, err := f.files()
	if err != nil {
		return nil, err
	}
	var checks []check
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if update {
			if data, err = f.canonical(data); err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
			if err := os.WriteFile(file, data, 0o644); err != nil {
				return nil, err
			}
		}
		name := filepath.ToSlash(file)
		checks = append(checks,
			check{name + "/round_trip", func() error { return f.roundTrip(data) }},
			check{name + "/deterministic", func() error { return f.deterministic(data) }},
			check{name + "/unknown_field", func() error { return f.refusesUnknownField(data) }},
			check{name + "/missing_version", func() error { return f.refusesMissingVersion(data) }},
			check{name + "/zero_version", func() error { return f.refusesZeroVersion(data) }},
			check{name + "/future_version", func() error { return f.refusesFutureVersion(data) }},
		)
	}
	return checks, nil
}

// docPages returns the path of the package in the working directory relative to its module's root, and the contents of [DocPages].
func docPages() (string, []page, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}
	root := wd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", nil, fmt.Errorf("formattest: no go.mod in %s or above it", wd)
		}
		root = parent
	}
	rel, err := filepath.Rel(root, wd)
	if err != nil {
		return "", nil, err
	}
	var pages []page
	for _, p := range DocPages {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			return "", nil, fmt.Errorf("formattest: read the reference page: %w", err)
		}
		pages = append(pages, page{name: p, data: data})
	}
	return strings.TrimPrefix(filepath.ToSlash(rel), "pkg/"), pages, nil
}

// docChecks returns the checks of f's entry on each of pages, for a format defined in the package at path pkg relative to the module root.
func (f Format[T]) docChecks(pkg string, pages []page) ([]check, error) {
	if f.Doc == "" {
		return nil, fmt.Errorf("formattest: Format needs Doc, the format's entry on the reference pages %s, such as %q; add the entry to both pages and name it here", strings.Join(DocPages, " and "), pkg)
	}
	if path, _, _ := strings.Cut(f.Doc, "#"); path != pkg {
		return nil, fmt.Errorf("formattest: Doc %q names package %s, but the format is defined in %s; start Doc with %q", f.Doc, path, pkg, pkg)
	}
	var checks []check
	for _, p := range pages {
		examples, err := entryExamples(p.data, f.Doc)
		if err != nil {
			checks = append(checks, check{p.name + "/entry", func() error { return fmt.Errorf("%s: %w", p.name, err) }})
			continue
		}
		for i, ex := range examples {
			checks = append(checks, check{fmt.Sprintf("%s/example_%d", p.name, i+1), func() error {
				out, err := f.canonical(ex.Data)
				if err != nil {
					return fmt.Errorf("%s:%d: the example of %s does not load: %w", p.name, ex.Line, f.Doc, err)
				}
				if !bytes.Equal(out, ex.Data) {
					return fmt.Errorf("%s:%d: the example of %s changes when loaded and saved, so it is not what the format writes; replace it with:\n%s", p.name, ex.Line, f.Doc, out)
				}
				return nil
			}})
		}
	}
	return checks, nil
}

// entryExamples returns the YAML code blocks of the entry doc on a reference page. It fails when the page does not parse, has no entry doc or the entry has no YAML block.
func entryExamples(data []byte, doc string) ([]Block, error) {
	entries, err := ParseEntries(data)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(entries, func(e Entry) bool { return e.ID == doc })
	if i < 0 {
		return nil, fmt.Errorf("no entry for %s; add a section that starts with the line %q, a heading, the format's files, version and functions, and a YAML example", doc, Marker+doc+" -->")
	}
	var examples []Block
	for _, b := range entries[i].Blocks {
		if b.Lang == "yaml" {
			examples = append(examples, b)
		}
	}
	if len(examples) == 0 {
		return nil, fmt.Errorf("the entry %s on line %d has no YAML example; add one in a ```yaml block, as the format saves it", doc, entries[i].Line)
	}
	return examples, nil
}

// Entry is a format's entry on a reference page: the ID its marker line names, the line of that marker, and the fenced code blocks from there to the next marker or level-1 or level-2 heading.
type Entry struct {
	ID     string
	Line   int
	Blocks []Block
}

// Block is a fenced code block: the language its opening fence names, such as "yaml" or "json", its contents with a newline after each line, and the line of its first line of contents.
type Block struct {
	Lang string
	Data []byte
	Line int
}

// ParseEntries returns the entries a reference page marks, in page order. [Run] and the test of the page's entries in internal/repocheck read the page with it, so both see the same entries and examples.
//
// An entry starts at a line "<!-- format: ID -->", where ID has no spaces, and ends at the next such line or at a line that starts with "# " or "## ". Trailing spaces and a carriage return on a line are ignored. A fenced code block starts at a line of three backticks and a language, possibly indented, and ends at a line of three backticks; markers and headings inside one are its contents. ParseEntries fails on a marker that is indented, malformed or given twice, on a block whose language is "yml" (write "yaml", which the checks look for) and on a block that is not closed, naming the line.
func ParseEntries(data []byte) ([]Entry, error) {
	var entries []Entry
	seen := map[string]int{}
	var open *Block // the code block being read, or nil
	var block []string
	for i, raw := range strings.Split(string(data), "\n") {
		n := i + 1
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimSpace(line)
		if open != nil {
			if trimmed == "```" {
				open.Data = []byte(strings.Join(block, "\n") + "\n")
				if len(entries) > 0 {
					entries[len(entries)-1].Blocks = append(entries[len(entries)-1].Blocks, *open)
				}
				open = nil
				continue
			}
			block = append(block, strings.TrimSuffix(raw, "\r"))
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "```"):
			lang, _, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(trimmed, "```")), " ")
			if lang == "yml" {
				return nil, fmt.Errorf("line %d: a code block in yml; write ```yaml, the language the checks read", n)
			}
			open = &Block{Lang: lang, Line: n + 1}
			block = nil
		case strings.HasPrefix(trimmed, "<!-- format:") || strings.HasPrefix(trimmed, "<!--format:"):
			id, ok := strings.CutSuffix(strings.TrimPrefix(line, Marker), " -->")
			if !strings.HasPrefix(line, Marker) || !ok || id == "" || strings.ContainsAny(id, " \t") {
				return nil, fmt.Errorf("line %d: malformed entry marker %q; write <!-- format: <package>[#<name>] --> at the start of the line", n, trimmed)
			}
			if first, dup := seen[id]; dup {
				return nil, fmt.Errorf("the entry %s is marked twice, on lines %d and %d; keep one", id, first, n)
			}
			seen[id] = n
			entries = append(entries, Entry{ID: id, Line: n})
		case strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## "):
			// A heading ends the entry: blocks after it, until the next marker, belong to none.
			entries = append(entries, Entry{Line: n})
		}
	}
	if open != nil {
		return nil, fmt.Errorf("line %d: a code block is not closed; end it with a line of three backticks", open.Line-1)
	}
	return slices.DeleteFunc(entries, func(e Entry) bool { return e.ID == "" }), nil
}

// files returns the sorted testdata files f.Files match, failing when they match none.
func (f Format[T]) files() ([]string, error) {
	var files []string
	for _, pattern := range f.Files {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("formattest: pattern %q: %w", pattern, err)
		}
		for _, m := range matches {
			if !slices.Contains(files, m) {
				files = append(files, m)
			}
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("formattest: Files %q match no testdata file; add a document in the format's canonical form", f.Files)
	}
	slices.Sort(files)
	return files, nil
}

// versionField returns the key that holds the schema version.
func (f Format[T]) versionField() string {
	if f.VersionField == "" {
		return "version"
	}
	return f.VersionField
}

// canonical returns what f saves for the document data.
func (f Format[T]) canonical(data []byte) ([]byte, error) {
	v, err := f.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	out, err := f.Encode(v)
	if err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}
	return out, nil
}

// roundTrip fails unless loading and saving data gives data back.
func (f Format[T]) roundTrip(data []byte) error {
	out, err := f.canonical(data)
	if err != nil {
		return err
	}
	if !bytes.Equal(out, data) {
		return fmt.Errorf("load and save changed the file (rewrite it with %s=1 if the change is intended):\n%s", UpdateEnv, diff(data, out))
	}
	return nil
}

// deterministic fails unless saving the value data holds gives the same bytes every time, from one loaded value, from a second load of data and from a load of the saved output.
func (f Format[T]) deterministic(data []byte) error {
	v, err := f.Decode(data)
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}
	first, err := f.Encode(v)
	if err != nil {
		return fmt.Errorf("save: %w", err)
	}
	same := func(what string, out []byte, err error) error {
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if !bytes.Equal(out, first) {
			return fmt.Errorf("%s wrote different bytes than the first save; output must not depend on map order, time or randomness:\n%s", what, diff(first, out))
		}
		return nil
	}
	for i := 2; i <= saves; i++ {
		out, err := f.Encode(v)
		if err := same(fmt.Sprintf("save %d of the same value", i), out, err); err != nil {
			return err
		}
	}
	again, err := f.Decode(data)
	if err != nil {
		return fmt.Errorf("second load: %w", err)
	}
	out, err := f.Encode(again)
	if err := same("saving a second load of the file", out, err); err != nil {
		return err
	}
	reloaded, err := f.Decode(first)
	if err != nil {
		return fmt.Errorf("load of the saved output: %w", err)
	}
	out, err = f.Encode(reloaded)
	return same("saving a load of the saved output", out, err)
}

// refusesUnknownField fails unless Decode refuses data with an extra top-level key, naming it as a *loaderr.Error at its line with a fix.
func (f Format[T]) refusesUnknownField(data []byte) error {
	edited, err := editRoot(data, func(root *yaml.Node) error {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: unknownField},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "1"},
		)
		return nil
	})
	if err != nil {
		return err
	}
	wantLine, _ := loaderr.FindField(edited, unknownField)
	return f.refuses(edited, "a document with the unknown field "+unknownField, unknownField, wantLine)
}

// refusesMissingVersion fails unless Decode refuses data without its version field, naming the field as a *loaderr.Error at line 1 (or the root mapping's line) with a fix.
func (f Format[T]) refusesMissingVersion(data []byte) error {
	field := f.versionField()
	edited, err := editRoot(data, func(root *yaml.Node) error {
		i, err := versionIndex(root, field)
		if err != nil {
			return err
		}
		root.Content = slices.Delete(root.Content, i, i+2)
		return nil
	})
	if err != nil {
		return err
	}
	wantLine, _ := loaderr.FindField(edited, field)
	return f.refuses(edited, "a document without "+field, field, wantLine)
}

// refusesZeroVersion fails unless Decode refuses data whose version is 0, naming the version field as a *loaderr.Error at its line with a fix.
func (f Format[T]) refusesZeroVersion(data []byte) error {
	field := f.versionField()
	edited, err := editRoot(data, func(root *yaml.Node) error {
		i, err := versionIndex(root, field)
		if err != nil {
			return err
		}
		value := root.Content[i+1]
		if value.Kind != yaml.ScalarNode {
			return fmt.Errorf("%s is %q, not an integer", field, value.Value)
		}
		if _, err := strconv.Atoi(value.Value); err != nil {
			return fmt.Errorf("%s is %q, not an integer", field, value.Value)
		}
		value.Value = "0"
		value.Tag = "!!int"
		value.Style = 0
		return nil
	})
	if err != nil {
		return err
	}
	wantLine, _ := loaderr.FindField(edited, field)
	return f.refuses(edited, fmt.Sprintf("a document with %s: 0", field), field, wantLine)
}

// refusesFutureVersion fails unless Decode refuses data whose version is one more than the newest supported version, naming the version field as a *loaderr.Error at its line with a fix.
func (f Format[T]) refusesFutureVersion(data []byte) error {
	field := f.versionField()
	var future int
	edited, err := editRoot(data, func(root *yaml.Node) error {
		i, err := versionIndex(root, field)
		if err != nil {
			return err
		}
		value := root.Content[i+1]
		current, err := strconv.Atoi(value.Value)
		if value.Kind != yaml.ScalarNode || err != nil {
			return fmt.Errorf("%s is %q, not an integer", field, value.Value)
		}
		future = max(current, f.MaxVersion) + 1
		value.Value = strconv.Itoa(future)
		value.Tag = "!!int"
		value.Style = 0
		return nil
	})
	if err != nil {
		return err
	}
	wantLine, _ := loaderr.FindField(edited, field)
	return f.refuses(edited, fmt.Sprintf("a document with %s: %d", field, future), field, wantLine)
}

// refuses fails unless Decode returns a *loaderr.Error for data that names word, points at wantLine and gives a fix; what describes data in the failure.
func (f Format[T]) refuses(data []byte, what, word string, wantLine int) error {
	_, err := f.Decode(data)
	if err == nil {
		return fmt.Errorf("load accepted %s:\n%s", what, data)
	}
	if !strings.Contains(err.Error(), word) {
		return fmt.Errorf("load refused %s with an error that does not name %s: %v", what, word, err)
	}
	e, ok := loaderr.As(err)
	if !ok {
		return fmt.Errorf("load refused %s with %T instead of *loaderr.Error: %v", what, err, err)
	}
	if wantLine > 0 && e.Line != wantLine {
		return fmt.Errorf("load refused %s at line %d, want line %d: %v", what, e.Line, wantLine, err)
	}
	if e.Fix == "" {
		return fmt.Errorf("load refused %s with an empty Fix: %v", what, err)
	}
	return nil
}

// editRoot parses data, calls edit with its top-level mapping and returns the edited document.
func editRoot(data []byte, edit func(root *yaml.Node) error) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse testdata: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("testdata is not a YAML mapping")
	}
	if err := edit(doc.Content[0]); err != nil {
		return nil, err
	}
	return yaml.Marshal(&doc)
}

// versionIndex returns the index in root.Content of the key field.
func versionIndex(root *yaml.Node, field string) (int, error) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == field {
			return i, nil
		}
	}
	return 0, fmt.Errorf("testdata has no top-level %s field", field)
}

// diff returns the first line where got differs from want, with a line of context, for a failure message.
func diff(want, got []byte) string {
	w := strings.Split(string(want), "\n")
	g := strings.Split(string(got), "\n")
	for i := 0; i < max(len(w), len(g)); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl || i >= len(w) || i >= len(g) {
			return fmt.Sprintf("line %d:\n  want %q\n  got  %q", i+1, wl, gl)
		}
	}
	return "(same lines)"
}

// Example is a code block of an entry on one of [DocPages], with the page's path relative to the repository root, for messages.
type Example struct {
	Page string
	Block
}

// Examples returns the code blocks in language lang, such as "json", of the entry id on each of [DocPages], for a test that loads a format's examples with its reader when the format is not YAML and so has no [Run]. It fails t when a page does not parse or the entry has no such block. The test must run in a package directory of the module, as go test runs it.
func Examples(t *testing.T, id, lang string) []Example {
	t.Helper()
	_, pages, err := docPages()
	if err != nil {
		t.Fatal(err)
	}
	var out []Example
	for _, p := range pages {
		entries, err := ParseEntries(p.data)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		i := slices.IndexFunc(entries, func(e Entry) bool { return e.ID == id })
		if i < 0 {
			t.Fatalf("%s has no entry %s", p.name, id)
		}
		n := len(out)
		for _, b := range entries[i].Blocks {
			if b.Lang == lang {
				out = append(out, Example{Page: p.name, Block: b})
			}
		}
		if len(out) == n {
			t.Fatalf("%s:%d: the entry %s has no %s example", p.name, entries[i].Line, id, lang)
		}
	}
	return out
}
