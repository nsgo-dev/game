// Package formattest checks, in a format package's tests, that a YAML format this repository defines follows the rules in AGENTS.md: it refuses unknown fields and versions it does not support, writes the same bytes every time it saves the same value, and loads and saves each of its testdata files without changing a byte.
//
// Every package that defines such a format runs [Run] from a test, and TestFormatPackagesRunConformance in package game fails a package that reads strict YAML and does not.
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
	// Decode loads a document, as the format's Parse function does. It must refuse a document with an unknown top-level field, with no version field and with a version newer than the one it supports, naming the field in its error.
	Decode func(data []byte) (T, error)
	// Encode saves a value, as the format's Encode function does.
	Encode func(v T) ([]byte, error)
	// Files are glob patterns, relative to the package directory, of the testdata files to check; together they must match at least one file. Each file must be in the format's canonical form, the bytes Encode writes for it.
	Files []string
	// VersionField is the top-level key that holds the document's schema version, an integer; empty means "version".
	VersionField string
	// MaxVersion is the newest schema version the format supports; when set, the future-version check tests max(document version, MaxVersion) + 1 instead of the document's version + 1, so a multi-version format can check older fixtures against an unsupported future version.
	MaxVersion int
}

// check is one named conformance check of one format.
type check struct {
	name string
	run  func() error
}

// Run checks f, each rule in a subtest of its own: that Decode refuses an unknown field, a missing version and a future version in each testdata file; that Encode writes the same bytes each time it saves a value, including one it loaded from its own output; and that each testdata file loads and saves to the same bytes.
func Run[T any](t *testing.T, f Format[T]) {
	t.Helper()
	checks, err := f.checks(os.Getenv(UpdateEnv) == "1")
	if err != nil {
		t.Fatal(err)
	}
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

// refusesUnknownField fails unless Decode refuses data with an extra top-level key, naming it.
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
	return f.refuses(edited, "a document with the unknown field "+unknownField, unknownField)
}

// refusesMissingVersion fails unless Decode refuses data without its version field, naming the field.
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
	return f.refuses(edited, "a document without "+field, field)
}

// refusesZeroVersion fails unless Decode refuses data whose version is 0, naming the version field.
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
	return f.refuses(edited, fmt.Sprintf("a document with %s: 0", field), field)
}

// refusesFutureVersion fails unless Decode refuses data whose version is one more than the newest supported version, naming the version field.
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
	return f.refuses(edited, fmt.Sprintf("a document with %s: %d", field, future), field)
}

// refuses fails unless Decode returns an error for data that names word; what describes data in the failure.
func (f Format[T]) refuses(data []byte, what, word string) error {
	_, err := f.Decode(data)
	if err == nil {
		return fmt.Errorf("load accepted %s:\n%s", what, data)
	}
	if !strings.Contains(err.Error(), word) {
		return fmt.Errorf("load refused %s with an error that does not name %s: %v", what, word, err)
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
