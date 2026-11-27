package l10n

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"strconv"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/pkg/loaderr"
)

// StringTableVersion is the YAML string table schema version this package reads and writes.
const StringTableVersion = 1

// SchemaURL is where the JSON Schema of version 1 YAML string tables is published.
// EncodeYAML names it in a yaml-language-server comment on the first line so editors and AI tools validate and complete string table files as they are written.
const SchemaURL = "https://game.nsgo.dev/schemas/l10n.v1.json"

// SchemaComment is the first line EncodeYAML writes: a YAML comment pointing a YAML language server at SchemaURL. ParseYAML ignores it like any other comment.
const SchemaComment = "# yaml-language-server: $schema=" + SchemaURL

// StringTable is a versioned YAML localization string table for one locale. A file looks like this:
//
//	# yaml-language-server: $schema=https://game.nsgo.dev/schemas/l10n.v1.json
//	version: 1
//	locale: en
//	strings:
//	  hud.score: "Score: {score}"
//	  menu.play: Play
//	plurals:
//	  coins:
//	    zero: No coins
//	    one: "{count} coin"
//	    other: "{count} coins"
//
// ParseYAML decodes it strictly and EncodeYAML writes it back in canonical form with sorted keys, so a table EncodeYAML wrote survives a load and save byte for byte.
type StringTable struct {
	// Version is the schema version; it must equal StringTableVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Locale is the normalized BCP-47 locale tag this table translates (for example "en" or "zh-Hans").
	Locale string `yaml:"locale" schema:"required,minLength=1"`
	// Strings maps message keys (such as "menu.play" or "hud.score") to localized string templates.
	Strings map[string]string `yaml:"strings,omitempty"`
	// Plurals maps message keys to CLDR plural form templates (zero, one, two, few, many, other).
	Plurals map[string]PluralForms `yaml:"plurals,omitempty"`
}

// fixYAMLStringTable is the fix for a YAML string table error that has no more specific one.
const fixYAMLStringTable = "write the string table as version: 1 with a normalized BCP-47 locale and optional strings and plurals maps"

// ParseYAML decodes and validates a versioned YAML string table. Unknown fields, invalid CLDR plural categories, duplicate keys, trailing YAML documents, YAML anchors/aliases/merge keys, null values, a missing or unsupported version, non-canonical locales, empty keys, keys defined in both strings and plurals, and empty plural entries are refused.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it; a caller that read data from a file names it with loaderr.WithFile.
func ParseYAML(data []byte) (*StringTable, error) {
	t, err := parseStringTable(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixYAMLStringTable)
	}
	return t, nil
}

func parseStringTable(data []byte) (*StringTable, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("l10n: empty string table")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var t StringTable
	if err := dec.Decode(&t); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("l10n: empty string table")
		}
		return nil, fmt.Errorf("l10n: decode string table: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("l10n: unexpected trailing YAML document in string table")
		}
		return nil, fmt.Errorf("l10n: decode string table: %w", err)
	}
	if err := ValidateStringTable(&t); err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("l10n: decode string table: %w", err)
	}
	if err := checkPlainStringTableNode(&root, t.Version); err != nil {
		return nil, err
	}
	return &t, nil
}

// checkPlainStringTableNode refuses YAML anchors, aliases and merge keys, a version not written as a plain decimal integer, and null values in string or plural entries.
func checkPlainStringTableNode(n *yaml.Node, version int) error {
	if err := checkNoStringTableYAMLReferences(n); err != nil {
		return err
	}
	doc := n
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i].Value
		val := doc.Content[i+1]
		switch key {
		case "version":
			if want := strconv.Itoa(version); val.Value != want {
				return loaderr.Errorf("version", "write the version as "+want, "l10n: string table version %q must be written as %s", val.Value, want)
			}
		case "strings":
			if val.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(val.Content); j += 2 {
					k, v := val.Content[j], val.Content[j+1]
					if v.Kind == yaml.ScalarNode && v.ShortTag() == "!!null" {
						return loaderr.Errorf(loaderr.Path("strings", k.Value), "write a string value or remove the key", "l10n: string table key %q has null value", k.Value)
					}
				}
			}
		case "plurals":
			if val.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(val.Content); j += 2 {
					k, forms := val.Content[j], val.Content[j+1]
					if forms.Kind != yaml.MappingNode {
						continue
					}
					for f := 0; f+1 < len(forms.Content); f += 2 {
						cat, fv := forms.Content[f], forms.Content[f+1]
						if fv.Kind == yaml.ScalarNode && fv.ShortTag() == "!!null" {
							return loaderr.Errorf(loaderr.Path("plurals", k.Value, cat.Value), "write a string template or omit the plural form", "l10n: string table plural %q form %q has null value", k.Value, cat.Value)
						}
					}
				}
			}
		}
	}
	return nil
}

func checkNoStringTableYAMLReferences(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("l10n: string table line %d: YAML aliases are not allowed", n.Line), Fix: fixYAMLReference}
	}
	if n.Anchor != "" {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("l10n: string table line %d: YAML anchors are not allowed", n.Line), Fix: fixYAMLReference}
	}
	for i, c := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 && c.Tag == "!!merge" {
			return &loaderr.Error{Line: c.Line, Column: c.Column, Problem: fmt.Sprintf("l10n: string table line %d: YAML merge keys are not allowed", c.Line), Fix: fixYAMLReference}
		}
		if err := checkNoStringTableYAMLReferences(c); err != nil {
			return err
		}
	}
	return nil
}

// ValidateStringTable checks t against the version 1 string table schema: version == StringTableVersion, canonical BCP-47 locale, valid non-empty keys with no overlap between Strings and Plurals, and non-empty PluralForms entries.
func ValidateStringTable(t *StringTable) error {
	if t == nil {
		return errors.New("l10n: nil string table")
	}
	switch t.Version {
	case StringTableVersion:
	case 0:
		return loaderr.Errorf("version", fixVersion, "l10n: string table is missing version (want %d)", StringTableVersion)
	default:
		return loaderr.Errorf("version", fixVersion, "l10n: unsupported string table version %d (want %d)", t.Version, StringTableVersion)
	}
	if err := checkCanonicalLocale(t.Locale); err != nil {
		return loaderr.Errorf("locale", fixLocale, "l10n: string table locale: %w", err)
	}
	for _, k := range sortedKeys(t.Strings) {
		if err := checkStringTableKey(k); err != nil {
			return loaderr.Errorf(loaderr.Path("strings", k), "give the entry a non-empty key without surrounding spaces or control characters", "l10n: string table strings: %w", err)
		}
	}
	for _, k := range sortedKeys(t.Plurals) {
		if err := checkStringTableKey(k); err != nil {
			return loaderr.Errorf(loaderr.Path("plurals", k), "give the entry a non-empty key without surrounding spaces or control characters", "l10n: string table plurals: %w", err)
		}
		if _, dup := t.Strings[k]; dup {
			return loaderr.Errorf(loaderr.Path("plurals", k), "keep each message key in either strings or plurals, not both", "l10n: string table key %q is defined in both strings and plurals", k)
		}
		if t.Plurals[k] == (PluralForms{}) {
			return loaderr.Errorf(loaderr.Path("plurals", k), "provide at least one CLDR plural form such as other", "l10n: string table plural %q has no plural forms", k)
		}
	}
	return nil
}

func checkStringTableKey(key string) error {
	if key == "" {
		return errors.New("empty message key")
	}
	if strings.TrimSpace(key) != key {
		return fmt.Errorf("message key %q must not have surrounding spaces", key)
	}
	if strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return fmt.Errorf("message key %q must not contain control characters", key)
	}
	return nil
}

// EncodeYAML validates t and writes it as deterministic 2-space YAML prefixed with SchemaComment: fields in schema order (version, locale, strings, plurals), map keys sorted lexicographically, and CLDR plural categories in canonical order (zero, one, two, few, many, other).
func EncodeYAML(t *StringTable) ([]byte, error) {
	if err := ValidateStringTable(t); err != nil {
		return nil, err
	}
	out := StringTable{
		Version: t.Version,
		Locale:  t.Locale,
	}
	if len(t.Strings) > 0 {
		out.Strings = maps.Clone(t.Strings)
	}
	if len(t.Plurals) > 0 {
		out.Plurals = maps.Clone(t.Plurals)
	}

	var buf bytes.Buffer
	buf.WriteString(SchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&out); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("l10n: encode string table: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("l10n: encode string table: %w", err)
	}
	return buf.Bytes(), nil
}

// LoadYAML parses a versioned YAML string table with ParseYAML and merges its entries into the table's locale. Nothing is merged when data is invalid.
//
// Errors about data are *loaderr.Error values that give the line and field of the problem and how to fix it; a caller that read data from a file names it with loaderr.WithFile.
func (c *Catalog) LoadYAML(data []byte) error {
	t, err := ParseYAML(data)
	if err != nil {
		return err
	}
	return c.ApplyStringTable(t)
}

// ApplyStringTable validates t and merges its string and plural entries into c under t.Locale. Nothing is merged when t is invalid.
func (c *Catalog) ApplyStringTable(t *StringTable) error {
	if err := ValidateStringTable(t); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	tbl := c.tableLocked(t.Locale)
	for k, v := range t.Strings {
		tbl[k] = entry{text: v}
	}
	for k, forms := range t.Plurals {
		tbl[k] = entry{isPlural: true, plural: forms}
	}
	return nil
}

// StringTable returns a snapshot of locale's entries in c as a StringTable, and reports whether locale exists in c. The result shares no maps with c.
func (c *Catalog) StringTable(locale string) (*StringTable, bool) {
	loc := NormalizeLocale(locale)
	if loc == "" {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	tbl, ok := c.tables[loc]
	if !ok {
		return nil, false
	}
	out := &StringTable{
		Version: StringTableVersion,
		Locale:  loc,
	}
	for k, e := range tbl {
		if e.isPlural {
			if out.Plurals == nil {
				out.Plurals = make(map[string]PluralForms)
			}
			out.Plurals[k] = e.plural
		} else {
			if out.Strings == nil {
				out.Strings = make(map[string]string)
			}
			out.Strings[k] = e.text
		}
	}
	return out, true
}

// MarshalYAML encodes locale's entries in c with EncodeYAML. It returns an error when locale has no entries in c or is not a valid BCP-47 tag.
func (c *Catalog) MarshalYAML(locale string) ([]byte, error) {
	t, ok := c.StringTable(locale)
	if !ok {
		return nil, fmt.Errorf("l10n: locale %q not found in catalog", locale)
	}
	return EncodeYAML(t)
}
