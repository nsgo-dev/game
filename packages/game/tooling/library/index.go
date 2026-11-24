package library

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/project"
	"go.yaml.in/yaml/v3"
)

// IndexFileName is the index's file name, which Open reads from a directory.
const IndexFileName = "index.yaml"

// IndexVersion is the index schema version this package reads and writes.
const IndexVersion = 1

// DefaultIndex is the shared library's index, in this repository's library directory, which the game library command reads unless told otherwise.
const DefaultIndex = "https://raw.githubusercontent.com/nsgo-dev/game/main/library/index.yaml"

// Index is a library's versioned index file.
type Index struct {
	// Version is the schema version; it must equal IndexVersion (1).
	Version int `yaml:"version"`
	// Packs are the library's packs, in the order the index lists them.
	Packs []Entry `yaml:"packs,omitempty"`
}

// Entry is one pack of a library.
type Entry struct {
	// Name identifies the pack, as project.ValidatePackName accepts it; it is unique in the index and equals the name in the pack's manifest.
	Name string `yaml:"name"`
	// Kind is one of project.PackKinds and equals the manifest's.
	Kind string `yaml:"kind"`
	// PackVersion is the version the index offers, which equals the manifest's.
	PackVersion string `yaml:"packVersion"`
	// Description says in one line what the pack holds.
	Description string `yaml:"description"`
	// Tags are lower-case words a search also matches, such as 2d or tiles.
	Tags []string `yaml:"tags,omitempty,flow"`
	// License is the license of the pack's files, as an SPDX identifier such as CC0-1.0 or MIT.
	License string `yaml:"license"`
	// Source is the directory holding the pack's pack.yaml: a slash-separated path relative to the index's directory that stays inside it, or an absolute https URL.
	Source string `yaml:"source"`
	// SHA256 is the pack's digest (Digest), 64 lower-case hexadecimal digits; a fetched pack whose digest differs is refused.
	SHA256 string `yaml:"sha256"`
}

// ParseIndex decodes a YAML index and validates it. Unknown fields and trailing YAML documents are refused.
func ParseIndex(data []byte) (*Index, error) {
	var idx Index
	if err := decodeStrict(data, &idx); err != nil {
		return nil, fmt.Errorf("library: index: %w", err)
	}
	if err := ValidateIndex(&idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// EncodeIndex validates idx and serializes it as deterministic 2-space YAML that ParseIndex reads back unchanged.
func EncodeIndex(idx *Index) ([]byte, error) {
	if err := ValidateIndex(idx); err != nil {
		return nil, err
	}
	return encode(idx)
}

// ValidateIndex checks that idx conforms to the version 1 schema.
func ValidateIndex(idx *Index) error {
	if idx == nil {
		return errors.New("library: nil index")
	}
	if idx.Version != IndexVersion {
		return fmt.Errorf("library: index: unsupported version %d (want %d)", idx.Version, IndexVersion)
	}
	names := make(map[string]struct{}, len(idx.Packs))
	for i := range idx.Packs {
		e := &idx.Packs[i]
		if err := validateEntry(e); err != nil {
			return fmt.Errorf("library: index: pack %d: %w", i, err)
		}
		if _, dup := names[e.Name]; dup {
			return fmt.Errorf("library: index: pack %q is listed twice", e.Name)
		}
		names[e.Name] = struct{}{}
	}
	return nil
}

func validateEntry(e *Entry) error {
	if err := project.ValidatePackName(e.Name); err != nil {
		return err
	}
	if err := validateKind(e.Kind); err != nil {
		return fmt.Errorf("%s: %w", e.Name, err)
	}
	if err := project.ValidatePackVersion(e.PackVersion); err != nil {
		return fmt.Errorf("%s: %w", e.Name, err)
	}
	if err := validateLine("description", e.Description, true); err != nil {
		return fmt.Errorf("%s: %w", e.Name, err)
	}
	if err := validateLicense(e.License); err != nil {
		return fmt.Errorf("%s: %w", e.Name, err)
	}
	seen := make(map[string]struct{}, len(e.Tags))
	for _, tag := range e.Tags {
		if tag == "" || strings.IndexFunc(tag, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.IsUpper(r) }) >= 0 {
			return fmt.Errorf("%s: tag %q must be a lower-case word without spaces", e.Name, tag)
		}
		if _, dup := seen[tag]; dup {
			return fmt.Errorf("%s: tag %q is listed twice", e.Name, tag)
		}
		seen[tag] = struct{}{}
	}
	if err := validateSource(e.Source); err != nil {
		return fmt.Errorf("%s: %w", e.Name, err)
	}
	if !project.IsSHA256(e.SHA256) {
		return fmt.Errorf("%s: sha256 %q must be 64 lower-case hexadecimal digits", e.Name, e.SHA256)
	}
	return nil
}

func validateKind(kind string) error {
	if !slices.Contains(project.PackKinds, kind) {
		return fmt.Errorf("kind %q must be one of %s", kind, strings.Join(project.PackKinds, ", "))
	}
	return nil
}

// validateLine refuses control characters and surrounding spaces, and an empty value when required.
func validateLine(field, s string, required bool) error {
	if s == "" {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	if strings.TrimSpace(s) != s || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return fmt.Errorf("%s %q must have no surrounding spaces or control characters", field, s)
	}
	return nil
}

// validateLicense accepts an SPDX license expression's characters: letters, digits, '.', '-', '+', parentheses and single spaces between words, such as "MIT OR Apache-2.0".
func validateLicense(license string) error {
	if license == "" {
		return errors.New("license is required, as an SPDX identifier such as CC0-1.0")
	}
	for word := range strings.SplitSeq(license, " ") {
		if word == "" || strings.IndexFunc(word, func(r rune) bool {
			return r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(".-+():", r))
		}) >= 0 {
			return fmt.Errorf("license %q must be an SPDX license expression such as CC0-1.0", license)
		}
	}
	return nil
}

// validateSource accepts an absolute URL Open would read (https, or http to a loopback host, as parseFetchURL accepts) without credentials, query or fragment, or a slash-separated path that stays inside the index's directory.
func validateSource(source string) error {
	if isURL(source) {
		u, err := parseFetchURL(source)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("source %q must be an https URL, or http to a loopback host, without credentials, query or fragment, or a path inside the library", source)
		}
		return nil
	}
	if !isPackPath(source) {
		return fmt.Errorf("source %q must be a slash-separated directory inside the library, or an https URL", source)
	}
	return nil
}

// isURL reports whether s is written as an http or https URL rather than a path.
func isURL(s string) bool {
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}

// Search returns the entries of kind, or of every kind when kind is empty, whose name, description or tags contain every word of query, ignoring case, sorted by name. An empty query matches every entry.
func (idx *Index) Search(kind, query string) []Entry {
	words := strings.Fields(strings.ToLower(query))
	var out []Entry
	for _, e := range idx.Packs {
		if kind != "" && e.Kind != kind {
			continue
		}
		text := strings.ToLower(e.Name + " " + e.Description + " " + strings.Join(e.Tags, " "))
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Find returns the entry called name, or false.
func (idx *Index) Find(name string) (Entry, bool) {
	i := slices.IndexFunc(idx.Packs, func(e Entry) bool { return e.Name == name })
	if i < 0 {
		return Entry{}, false
	}
	return idx.Packs[i], true
}

// decodeStrict decodes one YAML document into v, refusing unknown fields, an empty input and a trailing document.
func decodeStrict(data []byte, v any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return errors.New("empty file")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing YAML document")
		}
		return err
	}
	return nil
}

// encode writes v as 2-space YAML.
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("library: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("library: encode: %w", err)
	}
	return buf.Bytes(), nil
}
