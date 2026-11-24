package l10n

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/packages/game/loaderr"
)

// AssetManifestVersion is the asset manifest schema version this package reads and writes.
const AssetManifestVersion = 1

// AssetManifest is the versioned YAML document that maps logical asset names to per-locale paths and names asset groups. A manifest looks like this:
//
//	version: 1
//	base_locale: en
//	assets:
//	  voice/intro.wav:
//	    en: locales/en/voice/intro.wav
//	    ja: locales/ja/voice/intro.wav
//	groups:
//	  voice:
//	    - voice/intro.wav
//
// ParseAssetManifest decodes it strictly and EncodeAssetManifest writes it back in canonical form, so a manifest that EncodeAssetManifest wrote survives a load and save unchanged.
type AssetManifest struct {
	// Version is the schema version; it must equal AssetManifestVersion.
	Version int `yaml:"version"`
	// BaseLocale is the optional fallback locale, written in normalized BCP-47 form (for example "en" or "zh-Hans").
	BaseLocale string `yaml:"base_locale,omitempty"`
	// Assets maps a logical asset name (a clean slash-separated path such as "voice/intro.wav") to its variants, each a normalized locale mapped to a non-empty asset path.
	Assets map[string]map[string]string `yaml:"assets,omitempty"`
	// Groups maps a group name to the logical asset names it reloads on a locale switch. Members are clean asset names without duplicates; a group may name assets that the manifest itself does not list.
	Groups map[string][]string `yaml:"groups,omitempty"`
}

// ParseAssetManifest decodes and validates a YAML asset manifest. Unknown fields, duplicate keys, trailing YAML documents, a missing or unsupported version, and names or locales that are not in canonical form are refused.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it; a caller that read data from a file names it with loaderr.WithFile.
func ParseAssetManifest(data []byte) (*AssetManifest, error) {
	m, err := parseAssetManifest(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixAssetManifest)
	}
	return m, nil
}

// fixAssetManifest is the fix for an asset manifest error that has no more specific one.
const fixAssetManifest = "write the manifest as version: 1 with optional base_locale, assets (asset name to locale to path) and groups (group name to a list of asset names)"

func parseAssetManifest(data []byte) (*AssetManifest, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("l10n: empty asset manifest")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var m AssetManifest
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, io.EOF) {
			// A document of only comments decodes to nothing.
			return nil, errors.New("l10n: empty asset manifest")
		}
		return nil, fmt.Errorf("l10n: decode asset manifest: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("l10n: unexpected trailing YAML document in asset manifest")
		}
		return nil, fmt.Errorf("l10n: decode asset manifest: %w", err)
	}
	if err := ValidateAssetManifest(&m); err != nil {
		return nil, err
	}
	// The struct decode above cannot see how values were spelled, so check the node tree for forms that a save would silently rewrite into different text.
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("l10n: decode asset manifest: %w", err)
	}
	if err := checkPlainAssetManifestNode(&root, m.Version); err != nil {
		return nil, err
	}
	return &m, nil
}

// checkPlainAssetManifestNode refuses anchors, aliases and merge keys, which a save would expand, and a version not written as a plain decimal integer.
func checkPlainAssetManifestNode(n *yaml.Node, version int) error {
	if err := checkNoYAMLReferences(n); err != nil {
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
		if doc.Content[i].Value != "version" {
			continue
		}
		v := doc.Content[i+1]
		if want := strconv.Itoa(version); v.Value != want {
			return loaderr.Errorf("version", "write the version as "+want, "l10n: asset manifest version %q must be written as %s", v.Value, want)
		}
	}
	return nil
}

// fixYAMLReference is the fix for an anchor, alias or merge key, which a save would expand.
const fixYAMLReference = "write the value out in full where it is used; a save would expand the reference"

func checkNoYAMLReferences(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("l10n: asset manifest line %d: YAML aliases are not allowed", n.Line), Fix: fixYAMLReference}
	}
	if n.Anchor != "" {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("l10n: asset manifest line %d: YAML anchors are not allowed", n.Line), Fix: fixYAMLReference}
	}
	for i, c := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 && c.Tag == "!!merge" {
			return &loaderr.Error{Line: c.Line, Column: c.Column, Problem: fmt.Sprintf("l10n: asset manifest line %d: YAML merge keys are not allowed", c.Line), Fix: fixYAMLReference}
		}
		if err := checkNoYAMLReferences(c); err != nil {
			return err
		}
	}
	return nil
}

// EncodeAssetManifest validates m and writes it as deterministic 2-space YAML: fields in schema order, map keys sorted, and group members sorted.
func EncodeAssetManifest(m *AssetManifest) ([]byte, error) {
	if err := ValidateAssetManifest(m); err != nil {
		return nil, err
	}
	out := *m
	if len(m.Groups) > 0 {
		out.Groups = make(map[string][]string, len(m.Groups))
		for g, names := range m.Groups {
			sorted := slices.Clone(names)
			slices.Sort(sorted)
			if sorted == nil {
				sorted = []string{}
			}
			out.Groups[g] = sorted
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&out); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("l10n: encode asset manifest: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("l10n: encode asset manifest: %w", err)
	}
	return buf.Bytes(), nil
}

// Fixes ValidateAssetManifest gives.
const (
	fixVersion = "set version: 1"
	fixLocale  = "write the locale as a normalized BCP-47 tag such as en or zh-Hans"
	fixPath    = "write a non-empty path without surrounding spaces"
)

// ValidateAssetManifest checks m against the version 1 schema: the version, a normalized base locale, clean asset names with at least one variant each, normalized variant locales with non-empty paths that carry no surrounding spaces, and non-empty group names whose members are clean and unique.
func ValidateAssetManifest(m *AssetManifest) error {
	if m == nil {
		return errors.New("l10n: nil asset manifest")
	}
	switch m.Version {
	case AssetManifestVersion:
	case 0:
		return loaderr.Errorf("version", fixVersion, "l10n: asset manifest is missing version (want %d)", AssetManifestVersion)
	default:
		return loaderr.Errorf("version", fixVersion, "l10n: unsupported asset manifest version %d (want %d)", m.Version, AssetManifestVersion)
	}
	if m.BaseLocale != "" {
		if err := checkCanonicalLocale(m.BaseLocale); err != nil {
			return loaderr.Errorf("base_locale", fixLocale, "l10n: asset manifest base_locale: %w", err)
		}
	}
	for _, name := range sortedKeys(m.Assets) {
		if err := checkCanonicalAssetName(name); err != nil {
			return loaderr.Errorf(loaderr.Path("assets", name), "write the asset name as a clean slash-separated path such as voice/intro.wav", "l10n: asset manifest assets: %w", err)
		}
		variants := m.Assets[name]
		if len(variants) == 0 {
			return loaderr.Errorf(loaderr.Path("assets", name), "map at least one locale to the asset's path, such as en: locales/en/intro.wav", "l10n: asset %q has no locale variants", name)
		}
		for _, loc := range sortedKeys(variants) {
			if err := checkCanonicalLocale(loc); err != nil {
				return loaderr.Errorf(loaderr.Path("assets", name, loc), fixLocale, "l10n: asset %q: %w", name, err)
			}
			p := variants[loc]
			if p == "" {
				return loaderr.Errorf(loaderr.Path("assets", name, loc), fixPath, "l10n: asset %q locale %q has an empty path", name, loc)
			}
			if strings.TrimSpace(p) != p {
				return loaderr.Errorf(loaderr.Path("assets", name, loc), fixPath, "l10n: asset %q locale %q path %q has surrounding spaces", name, loc, p)
			}
		}
	}
	for _, group := range sortedKeys(m.Groups) {
		if group == "" || strings.TrimSpace(group) != group {
			return loaderr.Errorf(loaderr.Path("groups", group), "give the group a non-empty name without surrounding spaces", "l10n: asset manifest group name %q must be non-empty without surrounding spaces", group)
		}
		seen := make(map[string]struct{}, len(m.Groups[group]))
		for i, name := range m.Groups[group] {
			if err := checkCanonicalAssetName(name); err != nil {
				return loaderr.Errorf(loaderr.Path("groups", group, i), "write the member as a clean slash-separated asset name such as voice/intro.wav", "l10n: asset group %q: %w", group, err)
			}
			if _, dup := seen[name]; dup {
				return loaderr.Errorf(loaderr.Path("groups", group, i), "remove the repeated member", "l10n: asset group %q lists %q more than once", group, name)
			}
			seen[name] = struct{}{}
		}
	}
	return nil
}

// LoadAssetManifestYAML parses a YAML asset manifest with ParseAssetManifest and merges it into c with ApplyAssetManifest. Nothing is merged when the manifest is invalid.
func (c *AssetCatalog) LoadAssetManifestYAML(data []byte) error {
	m, err := ParseAssetManifest(data)
	if err != nil {
		return err
	}
	return c.ApplyAssetManifest(m)
}

// ApplyAssetManifest validates m and merges it into c: a non-empty BaseLocale replaces c's base locale, every variant is registered (replacing an existing path for the same asset and locale), and group members are added to their groups.
func (c *AssetCatalog) ApplyAssetManifest(m *AssetManifest) error {
	if err := ValidateAssetManifest(m); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if m.BaseLocale != "" {
		c.baseLocale = m.BaseLocale
		if c.currentLocale == "" {
			c.currentLocale = m.BaseLocale
		}
	}
	for name, variants := range m.Assets {
		for loc, p := range variants {
			c.registerLocked(name, loc, p)
		}
	}
	for group, names := range m.Groups {
		c.registerGroupLocked(group, names...)
	}
	return nil
}

// AssetManifest returns a snapshot of c as a manifest: its base locale, every registered variant, and every group with its members sorted. The result shares no maps or slices with c.
func (c *AssetCatalog) AssetManifest() *AssetManifest {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := &AssetManifest{Version: AssetManifestVersion, BaseLocale: c.baseLocale}
	if len(c.assets) > 0 {
		m.Assets = make(map[string]map[string]string, len(c.assets))
		for name, variants := range c.assets {
			m.Assets[name] = maps.Clone(variants)
		}
	}
	if len(c.groups) > 0 {
		m.Groups = make(map[string][]string, len(c.groups))
		for group, members := range c.groups {
			names := make([]string, 0, len(members))
			for name := range members {
				names = append(names, name)
			}
			slices.Sort(names)
			m.Groups[group] = names
		}
	}
	return m
}

// MarshalAssetManifestYAML writes c's AssetManifest snapshot with EncodeAssetManifest.
func (c *AssetCatalog) MarshalAssetManifestYAML() ([]byte, error) {
	return EncodeAssetManifest(c.AssetManifest())
}

func checkCanonicalLocale(loc string) error {
	norm := NormalizeLocale(loc)
	if norm == "" {
		return fmt.Errorf("empty locale %q", loc)
	}
	if norm != loc {
		return fmt.Errorf("locale %q must be written as %q", loc, norm)
	}
	if !isLocaleTagSyntax(loc) {
		return fmt.Errorf("locale %q is not a BCP-47 tag", loc)
	}
	return nil
}

// isLocaleTagSyntax reports whether norm, a NormalizeLocale result, has BCP-47 syntax: "-"-separated subtags of 1 to 8 ASCII letters or digits.
func isLocaleTagSyntax(norm string) bool {
	for sub := range strings.SplitSeq(norm, "-") {
		if len(sub) == 0 || len(sub) > 8 {
			return false
		}
		for i := 0; i < len(sub); i++ {
			c := sub[i]
			if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9') {
				return false
			}
		}
	}
	return true
}

func checkCanonicalAssetName(name string) error {
	clean := cleanAssetName(name)
	if clean == "" {
		return fmt.Errorf("invalid asset name %q", name)
	}
	if clean != name {
		return fmt.Errorf("asset name %q must be written as %q", name, clean)
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
