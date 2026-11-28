package importsettings

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// AssetSourceSuffix is appended to a generated asset's file name to name its provenance and license sidecar, such as slime_frost.png.source.yaml beside slime_frost.png.
const AssetSourceSuffix = ".source.yaml"

// AssetSourceVersion is the YAML schema version of generated asset source sidecars (*.source.yaml).
const AssetSourceVersion = 1

// AssetSourceSchemaURL is where the JSON Schema of version 1 asset source sidecars is published.
// EncodeAssetSource names it in a yaml-language-server comment on the first line so editors and AI tools validate and complete source files as they are written.
const AssetSourceSchemaURL = "https://game.nsgo.dev/schemas/source.v1.json"

// AssetSourceSchemaComment is the first line EncodeAssetSource writes: a YAML comment pointing a YAML language server at AssetSourceSchemaURL. ParseAssetSource ignores it like any other comment.
const AssetSourceSchemaComment = "# yaml-language-server: $schema=" + AssetSourceSchemaURL

// AssetSourcePath returns the slash-separated path of the source sidecar of the asset at the slash-separated path asset.
func AssetSourcePath(asset string) string {
	return asset + AssetSourceSuffix
}

// IsAssetSource reports whether the file name or path name is a generated asset source sidecar (*.source.yaml), so asset scans can skip it.
func IsAssetSource(name string) bool {
	return strings.HasSuffix(name, AssetSourceSuffix)
}

// AssetSource records the provenance, generation parameters, license, and approval of an asset generated through an MCP generator server.
// It is written beside the picked asset as <file>.source.yaml in deterministic YAML.
type AssetSource struct {
	// Version is the schema version; it must equal AssetSourceVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Kind identifies the generated media family. Empty preserves version-1 sidecars.
	Kind string `yaml:"kind,omitempty"`
	// Files lists additional generated files covered by this provenance record.
	Files []string `yaml:"files,omitempty"`
	// Generator is the MCP generator server name that produced the asset, such as "pixel-gen" or "sfx-gen".
	Generator string `yaml:"generator" schema:"required,minLength=1"`
	// Model is the generator model identifier reported by the server, such as "pixflux-1".
	Model string `yaml:"model,omitempty"`
	// Prompt is the text prompt sent to the generator.
	Prompt string `yaml:"prompt" schema:"required,minLength=1"`
	// Seed is the random seed that produced this candidate, when reported by the generator.
	Seed *int64 `yaml:"seed,omitempty"`
	// References lists the project-relative asset or brief paths passed to the generator as references (for example "art/characters/slime.png", "brief.yaml#art").
	References []string `yaml:"references,omitempty"`
	// License names the license under which the generated asset is used (for example "pixellab-commercial-1.2" or "CC0-1.0"); an empty or "unknown" license blocks release builds until resolved.
	License string `yaml:"license,omitempty"`
	// CommercialUse records whether the asset's license permits commercial use.
	CommercialUse bool `yaml:"commercialUse"`
	// ApprovedBy names the person who picked and approved the generated candidate.
	ApprovedBy string `yaml:"approvedBy,omitempty"`
}

// IsUnknownLicense reports whether license is empty or explicitly marks the license as unknown/unasserted (case-insensitive: "", "unknown", "none", "noassertion", "tbd", "unspecified").
// A generated asset whose source sidecar has an unknown license blocks release builds.
func IsUnknownLicense(license string) bool {
	switch strings.ToLower(strings.TrimSpace(license)) {
	case "", "unknown", "none", "noassertion", "tbd", "unspecified":
		return true
	default:
		return false
	}
}

// ParseAssetSource decodes and validates a versioned YAML generated asset source sidecar (*.source.yaml).
// Unknown fields, duplicate keys, YAML anchors/aliases/merge keys, trailing YAML documents, and missing or unsupported versions are refused.
func ParseAssetSource(data []byte) (*AssetSource, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("add an asset source sidecar starting with version: %d, generator, and prompt", AssetSourceVersion), "importsettings: empty asset source document"), "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var s AssetSource
	if err := dec.Decode(&s); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("add an asset source sidecar starting with version: %d, generator, and prompt", AssetSourceVersion), "importsettings: empty asset source document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("importsettings: decode asset source: %w", err), "", data, "check the YAML syntax and field names in the asset source sidecar")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the asset source sidecar and remove any --- separator", "importsettings: unexpected trailing YAML document in asset source"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("importsettings: decode asset source: %w", err), "", data, "remove trailing YAML content after the asset source sidecar")
	}
	if err := ValidateAssetSource(&s); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("importsettings: decode asset source: %w", err), "", data, "check the YAML syntax in the asset source sidecar")
	}
	if err := checkPlainAssetSourceNode(&root, s.Version); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	return &s, nil
}

func checkPlainAssetSourceNode(n *yaml.Node, version int) error {
	if err := checkNoSourceYAMLRefs(n); err != nil {
		return err
	}
	if err := checkDuplicateSourceYAMLKeys(n); err != nil {
		return err
	}
	doc := n
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return &loaderr.Error{
			Line:    doc.Line,
			Column:  doc.Column,
			Problem: "importsettings: asset source must be a YAML mapping",
			Fix:     fmt.Sprintf("write the asset source sidecar as a YAML mapping starting with version: %d", AssetSourceVersion),
		}
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value == "version" {
			val := doc.Content[i+1]
			if want := strconv.Itoa(version); val.Value != want {
				return &loaderr.Error{
					Line:    val.Line,
					Column:  val.Column,
					Field:   "version",
					Problem: fmt.Sprintf("importsettings: asset source version %q must be written as %s", val.Value, want),
					Fix:     fmt.Sprintf("write version: %s as a plain base-10 integer", want),
				}
			}
		}
	}
	return nil
}

func checkNoSourceYAMLRefs(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return &loaderr.Error{
			Line:    n.Line,
			Column:  n.Column,
			Problem: fmt.Sprintf("importsettings: asset source line %d: YAML aliases are not allowed", n.Line),
			Fix:     "expand YAML aliases inline instead of using *",
		}
	}
	if n.Anchor != "" {
		return &loaderr.Error{
			Line:    n.Line,
			Column:  n.Column,
			Problem: fmt.Sprintf("importsettings: asset source line %d: YAML anchors are not allowed", n.Line),
			Fix:     "remove YAML anchors (&name) and write values inline",
		}
	}
	for i, c := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 && c.Tag == "!!merge" {
			return &loaderr.Error{
				Line:    c.Line,
				Column:  c.Column,
				Problem: fmt.Sprintf("importsettings: asset source line %d: YAML merge keys are not allowed", c.Line),
				Fix:     "remove YAML merge keys (<<) and list fields explicitly",
			}
		}
		if err := checkNoSourceYAMLRefs(c); err != nil {
			return err
		}
	}
	return nil
}

func checkDuplicateSourceYAMLKeys(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		seen := make(map[string]int, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if prev, dup := seen[k.Value]; dup {
				return &loaderr.Error{
					Line:    k.Line,
					Column:  k.Column,
					Field:   k.Value,
					Problem: fmt.Sprintf("importsettings: asset source line %d: duplicate key %q (first seen on line %d)", k.Line, k.Value, prev),
					Fix:     fmt.Sprintf("remove the duplicate %q key", k.Value),
				}
			}
			seen[k.Value] = k.Line
		}
	}
	for _, c := range n.Content {
		if err := checkDuplicateSourceYAMLKeys(c); err != nil {
			return err
		}
	}
	return nil
}

// ValidateAssetSource checks s against the version 1 generated asset source schema.
// Unknown or empty License values are accepted so unresolved generated assets can be represented on disk and blocked by release/credits checks.
func ValidateAssetSource(s *AssetSource) error {
	if s == nil {
		return loaderr.Errorf("", "pass a non-nil *importsettings.AssetSource to ValidateAssetSource", "importsettings: nil asset source")
	}
	switch s.Version {
	case AssetSourceVersion:
	case 0:
		return loaderr.Errorf("version", fmt.Sprintf("add version: %d at the top of the asset source sidecar", AssetSourceVersion), "importsettings: asset source is missing version (want %d)", AssetSourceVersion)
	default:
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the asset source sidecar", AssetSourceVersion), "importsettings: unsupported asset source version %d (want %d)", s.Version, AssetSourceVersion)
	}
	if err := checkLine("generator", s.Generator, true); err != nil {
		return loaderr.Errorf("generator", "set generator to a non-empty single-line generator name without surrounding spaces", "importsettings: asset source: %w", err)
	}
	if err := checkLine("model", s.Model, false); err != nil {
		return loaderr.Errorf("model", "set model to a single-line model identifier without surrounding spaces", "importsettings: asset source: %w", err)
	}
	if strings.TrimSpace(s.Prompt) == "" {
		return loaderr.Errorf("prompt", "set prompt to the non-empty text prompt sent to the generator", "importsettings: asset source: prompt is required")
	}
	if strings.TrimSpace(s.Prompt) != s.Prompt {
		return loaderr.Errorf("prompt", "trim leading and trailing spaces and blank lines from prompt", "importsettings: asset source: prompt must not start or end with spaces or blank lines")
	}
	if i := strings.IndexFunc(s.Prompt, func(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) }); i >= 0 {
		return loaderr.Errorf("prompt", "remove control characters other than line breaks and tabs from prompt", "importsettings: asset source: prompt holds control character %q", s.Prompt[i:i+1])
	}
	for idx, ref := range s.References {
		field := fmt.Sprintf("references[%d]", idx)
		if err := checkLine(field, ref, true); err != nil {
			return loaderr.Errorf(field, "set each reference to a non-empty single-line path without surrounding spaces", "importsettings: asset source: %w", err)
		}
	}
	if err := checkLine("kind", s.Kind, false); err != nil {
		return loaderr.Errorf("kind", "set kind to a single-line media family", "importsettings: asset source: %w", err)
	}
	for idx, file := range s.Files {
		if err := checkLine(fmt.Sprintf("files[%d]", idx), file, true); err != nil {
			return loaderr.Errorf(fmt.Sprintf("files[%d]", idx), "set each covered file to a project-relative path", "importsettings: asset source: %w", err)
		}
	}
	if err := checkLine("license", s.License, false); err != nil {
		return loaderr.Errorf("license", "set license to a single-line license identifier without surrounding spaces", "importsettings: asset source: %w", err)
	}
	if err := checkLine("approvedBy", s.ApprovedBy, false); err != nil {
		return loaderr.Errorf("approvedBy", "set approvedBy to a single-line approver name without surrounding spaces", "importsettings: asset source: %w", err)
	}
	return nil
}

// EncodeAssetSource validates s and writes deterministic 2-space YAML prefixed with AssetSourceSchemaComment.
func EncodeAssetSource(s *AssetSource) ([]byte, error) {
	if err := ValidateAssetSource(s); err != nil {
		return nil, err
	}
	out := AssetSource{
		Version:       s.Version,
		Kind:          s.Kind,
		Files:         slices.Clone(s.Files),
		Generator:     s.Generator,
		Model:         s.Model,
		Prompt:        s.Prompt,
		References:    slices.Clone(s.References),
		License:       s.License,
		CommercialUse: s.CommercialUse,
		ApprovedBy:    s.ApprovedBy,
	}
	if s.Seed != nil {
		v := *s.Seed
		out.Seed = &v
	}
	var buf bytes.Buffer
	buf.WriteString(AssetSourceSchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&out); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("importsettings: encode asset source: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("importsettings: encode asset source: %w", err)
	}
	return buf.Bytes(), nil
}
