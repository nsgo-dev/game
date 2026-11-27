package project

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// BriefFileName is the game brief file name at a project's root.
const BriefFileName = "brief.yaml"

// BriefVersion is the brief.yaml schema version this package reads and writes.
const BriefVersion = 1

// BriefSchemaURL is where the JSON Schema of version 1 game brief files (brief.yaml) is published.
const BriefSchemaURL = "https://game.nsgo.dev/schemas/brief.v1.json"

// BriefSchemaComment is the first line EncodeBrief writes: a YAML comment pointing a YAML language server at BriefSchemaURL.
const BriefSchemaComment = "# yaml-language-server: $schema=" + BriefSchemaURL

// BriefSection identifies a top-level section of brief.yaml checked for completeness ("gaps").
type BriefSection string

const (
	BriefSectionPitch        BriefSection = "pitch"
	BriefSectionGenre        BriefSection = "genre"
	BriefSectionPlatforms    BriefSection = "platforms"
	BriefSectionArtDirection BriefSection = "artDirection"
	BriefSectionFeel         BriefSection = "feel"
	BriefSectionToneAndAudio BriefSection = "toneAndAudio"
)

// BriefSections lists every section checked by Brief.MissingSections in canonical display order.
var BriefSections = []BriefSection{
	BriefSectionPitch,
	BriefSectionGenre,
	BriefSectionPlatforms,
	BriefSectionArtDirection,
	BriefSectionFeel,
	BriefSectionToneAndAudio,
}

// Brief is the root of a versioned brief.yaml file at a game project's root.
// The agent reads it before every request and checks its work against it, and playtests can read structured targets from Feel.Targets.
type Brief struct {
	// Version is the schema version; it must equal BriefVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Pitch describes what the game is meant to be in a sentence or two.
	Pitch string `yaml:"pitch,omitempty"`
	// Genre lists the game's primary genre labels, such as "2D platformer" and "action".
	Genre []string `yaml:"genre,omitempty,flow"`
	// Tags lists optional extra keywords or sub-genre tags describing the game.
	Tags []string `yaml:"tags,omitempty,flow"`
	// Platforms lists the target platforms and control styles, such as "desktop", "phones, touch", and "web".
	Platforms []string `yaml:"platforms,omitempty,flow"`
	// ArtDirection describes visual style, resolution, palette, and silhouette rules.
	ArtDirection string `yaml:"artDirection,omitempty"`
	// Art holds structured visual direction notes and pinned reference images read by the agent before every request.
	Art *BriefArt `yaml:"art,omitempty"`
	// ToneAndAudio describes mood, music, and sound direction.
	ToneAndAudio string `yaml:"toneAndAudio,omitempty"`
	// Feel describes game feel, difficulty pacing, and structured playtest targets.
	Feel *BriefFeel `yaml:"feel,omitempty"`
}

// BriefArt holds structured art direction notes and pinned project-relative reference images.
type BriefArt struct {
	// Direction optionally describes visual direction inline under art.direction.
	Direction string `yaml:"direction,omitempty"`
	// References lists pinned project-relative reference images included with every agent request.
	References []BriefArtReference `yaml:"references,omitempty"`
}

// BriefArtReference names one pinned reference image listed under art.references in brief.yaml.
type BriefArtReference struct {
	// Path is the project-relative slash path to the reference image (for example "art/reference/spikes_ref.png").
	Path string `yaml:"path" schema:"required,minLength=1"`
	// Note is an optional short description of what the reference illustrates.
	Note string `yaml:"note,omitempty"`
}

// BriefFeel describes game feel, difficulty expectations, and machine-readable playtest targets.
type BriefFeel struct {
	// Summary describes target game feel, controls responsiveness, and difficulty pacing in plain language.
	Summary string `yaml:"summary,omitempty"`
	// Targets lists structured playtest targets readable by playtest runners and the editor.
	Targets []BriefPlaytestTarget `yaml:"targets,omitempty"`
}

// BriefPlaytestTarget declares one measurable playtest target in brief.yaml's feel and difficulty section.
type BriefPlaytestTarget struct {
	// Metric is the playtest metric name this target constrains, such as "median_time", "deaths", or "finished".
	Metric string `yaml:"metric" schema:"required,minLength=1"`
	// Scene optionally restricts this target to a specific scene or level name, such as "floor_01".
	Scene string `yaml:"scene,omitempty"`
	// Summary is an optional human-readable description of the target, such as "2–4 deaths on floor 3".
	Summary string `yaml:"summary,omitempty"`
	// Unit is an optional display unit, such as "s", "ms", or "%".
	Unit string `yaml:"unit,omitempty"`
	// Min is the inclusive lower bound (value >= Min).
	Min *float64 `yaml:"min,omitempty"`
	// Max is the inclusive upper bound (value <= Max).
	Max *float64 `yaml:"max,omitempty"`
	// Equal requires value == Equal.
	Equal *float64 `yaml:"equal,omitempty"`
}

// Float64 returns a pointer to v for optional numeric bounds such as BriefPlaytestTarget.Min, Max, and Equal.
func Float64(v float64) *float64 { return &v }

const (
	fixBrief        = "write brief.yaml as version: 1 with pitch, genre, tags, platforms, artDirection, art, toneAndAudio, and feel"
	fixBriefVersion = "set version: 1"
	fixBriefYAMLRef = "write values directly without YAML anchors, aliases, or merge keys"
)

// ParseBrief decodes and validates a versioned brief.yaml document.
// Unknown fields, trailing YAML documents, duplicate keys, YAML anchors/aliases/merge keys,
// missing or unsupported versions, and invalid playtest target bounds are refused with *loaderr.Error line info.
func ParseBrief(data []byte) (*Brief, error) {
	b, err := parseBrief(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixBrief)
	}
	return b, nil
}

func parseBrief(data []byte) (*Brief, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixBrief, "project: empty brief document")
	}

	var root yaml.Node
	nodeDec := yaml.NewDecoder(bytes.NewReader(data))
	if err := nodeDec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, loaderr.Errorf("", fixBrief, "project: empty brief document")
		}
		return nil, fmt.Errorf("project: decode brief: %w", err)
	}
	var extraNode yaml.Node
	if err := nodeDec.Decode(&extraNode); err != io.EOF {
		if err == nil {
			return nil, &loaderr.Error{
				Line:    extraNode.Line,
				Column:  extraNode.Column,
				Problem: "project: unexpected trailing YAML document in brief",
				Fix:     "keep one YAML document in brief.yaml",
			}
		}
		return nil, fmt.Errorf("project: decode brief: %w", err)
	}
	if err := checkPlainBriefNode(&root); err != nil {
		return nil, err
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var b Brief
	if err := dec.Decode(&b); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, loaderr.Errorf("", fixBrief, "project: empty brief document")
		}
		return nil, fmt.Errorf("project: decode brief: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document in brief.yaml", "project: unexpected trailing YAML document in brief")
		}
		return nil, fmt.Errorf("project: decode brief: %w", err)
	}
	normalizeBrief(&b)
	if err := ValidateBrief(&b); err != nil {
		return nil, err
	}
	if err := checkBriefVersionScalar(&root, b.Version); err != nil {
		return nil, err
	}
	return &b, nil
}

func checkPlainBriefNode(n *yaml.Node) error {
	if err := checkNoBriefYAMLReferences(n); err != nil {
		return err
	}
	if err := checkBriefDuplicateKeys(n); err != nil {
		return err
	}
	return nil
}

func checkNoBriefYAMLReferences(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return &loaderr.Error{
			Line:    n.Line,
			Column:  n.Column,
			Problem: fmt.Sprintf("project: brief line %d: YAML aliases are not allowed", n.Line),
			Fix:     fixBriefYAMLRef,
		}
	}
	if n.Anchor != "" {
		return &loaderr.Error{
			Line:    n.Line,
			Column:  n.Column,
			Problem: fmt.Sprintf("project: brief line %d: YAML anchors are not allowed", n.Line),
			Fix:     fixBriefYAMLRef,
		}
	}
	for i, c := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 && c.Tag == "!!merge" {
			return &loaderr.Error{
				Line:    c.Line,
				Column:  c.Column,
				Problem: fmt.Sprintf("project: brief line %d: YAML merge keys are not allowed", c.Line),
				Fix:     fixBriefYAMLRef,
			}
		}
		if err := checkNoBriefYAMLReferences(c); err != nil {
			return err
		}
	}
	return nil
}

func checkBriefDuplicateKeys(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		seen := make(map[string]int, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if firstLine, dup := seen[k.Value]; dup {
				return &loaderr.Error{
					Line:    k.Line,
					Column:  k.Column,
					Field:   k.Value,
					Problem: fmt.Sprintf("project: brief duplicate key %q (first defined at line %d)", k.Value, firstLine),
					Fix:     fmt.Sprintf("remove the duplicate %q key", k.Value),
				}
			}
			seen[k.Value] = k.Line
		}
	}
	for _, c := range n.Content {
		if err := checkBriefDuplicateKeys(c); err != nil {
			return err
		}
	}
	return nil
}

func checkBriefVersionScalar(n *yaml.Node, version int) error {
	doc := n
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value == "version" {
			val := doc.Content[i+1]
			if want := strconv.Itoa(version); val.Value != want {
				return &loaderr.Error{
					Line:     val.Line,
					Column:   val.Column,
					Field:    "version",
					Problem:  fmt.Sprintf("project: brief version %q must be written as %s", val.Value, want),
					Expected: want,
					Fix:      "write the version as " + want,
				}
			}
		}
	}
	return nil
}

// ValidateBrief checks b against the version 1 brief.yaml schema.
func ValidateBrief(b *Brief) error {
	if b == nil {
		return loaderr.Errorf("", fixBrief, "project: nil brief")
	}
	switch b.Version {
	case BriefVersion:
	case 0:
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("project: brief is missing version (want %d)", BriefVersion),
			Expected: strconv.Itoa(BriefVersion),
			Fix:      fixBriefVersion,
		}
	default:
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("project: unsupported brief version %d (want %d)", b.Version, BriefVersion),
			Expected: strconv.Itoa(BriefVersion),
			Fix:      fixBriefVersion,
		}
	}
	if err := validateBriefMultiline("pitch", b.Pitch); err != nil {
		return err
	}
	if err := validateBriefLabels("genre", b.Genre); err != nil {
		return err
	}
	if err := validateBriefLabels("tags", b.Tags); err != nil {
		return err
	}
	if err := validateBriefLabels("platforms", b.Platforms); err != nil {
		return err
	}
	if err := validateBriefMultiline("artDirection", b.ArtDirection); err != nil {
		return err
	}
	if b.Art != nil {
		if err := validateBriefMultiline("art.direction", b.Art.Direction); err != nil {
			return err
		}
		seenRefs := make(map[string]struct{}, len(b.Art.References))
		for i, ref := range b.Art.References {
			at := loaderr.Path("art", "references", i)
			if err := validateBriefArtReference(at, ref); err != nil {
				return err
			}
			if _, dup := seenRefs[ref.Path]; dup {
				return loaderr.Errorf(loaderr.Join(at, "path"), "remove duplicate reference paths from art.references", "project: duplicate brief art reference %q", ref.Path)
			}
			seenRefs[ref.Path] = struct{}{}
		}
	}
	if err := validateBriefMultiline("toneAndAudio", b.ToneAndAudio); err != nil {
		return err
	}
	if b.Feel != nil {
		if err := validateBriefMultiline("feel.summary", b.Feel.Summary); err != nil {
			return err
		}
		seen := make(map[string]struct{}, len(b.Feel.Targets))
		for i, target := range b.Feel.Targets {
			at := loaderr.Path("feel", "targets", i)
			if err := validateBriefTarget(at, target); err != nil {
				return err
			}
			key := target.Scene + "\x00" + target.Metric
			if _, dup := seen[key]; dup {
				return loaderr.Errorf(loaderr.Join(at, "metric"), "keep at most one target per metric and scene", "project: duplicate brief playtest target %q for scene %q", target.Metric, target.Scene)
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

func validateBriefMultiline(field, s string) error {
	for _, r := range s {
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			return loaderr.Errorf(field, "remove control characters from "+field, "project: brief %s contains a control character", field)
		}
	}
	return nil
}

func validateBriefLabels(field string, items []string) error {
	seen := make(map[string]struct{}, len(items))
	for i, item := range items {
		at := loaderr.Path(field, i)
		if item == "" {
			return loaderr.Errorf(at, "remove empty entries from "+field, "project: brief %s entry must not be empty", field)
		}
		if strings.TrimSpace(item) != item || strings.IndexFunc(item, unicode.IsControl) >= 0 {
			return loaderr.Errorf(at, "remove surrounding spaces and control characters", "project: brief %s entry %q must have no surrounding spaces or control characters", field, item)
		}
		norm := strings.ToLower(item)
		if _, dup := seen[norm]; dup {
			return loaderr.Errorf(at, "remove duplicate entries from "+field, "project: duplicate brief %s entry %q", field, item)
		}
		seen[norm] = struct{}{}
	}
	return nil
}

func validateBriefArtReference(at string, ref BriefArtReference) error {
	p := ref.Path
	if p == "" || strings.TrimSpace(p) != p || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return loaderr.Errorf(loaderr.Join(at, "path"), "set path to a clean project-relative slash path such as art/reference/spikes_ref.png", "project: brief art reference path %q is invalid", p)
	}
	if strings.Contains(p, "\\") || strings.HasPrefix(p, "/") {
		return loaderr.Errorf(loaderr.Join(at, "path"), "use a slash-separated project-relative path without a leading slash", "project: brief art reference path %q must be project-relative with forward slashes", p)
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != p {
		return loaderr.Errorf(loaderr.Join(at, "path"), "use a normalized project-relative path without . or .. segments", "project: brief art reference path %q must be a clean project-relative path", p)
	}
	if ref.Note != "" && (strings.TrimSpace(ref.Note) != ref.Note || strings.IndexFunc(ref.Note, unicode.IsControl) >= 0) {
		return loaderr.Errorf(loaderr.Join(at, "note"), "use a single-line note without surrounding spaces or control characters", "project: brief art reference note %q is invalid", ref.Note)
	}
	return nil
}

func validateBriefTarget(at string, t BriefPlaytestTarget) error {
	if strings.TrimSpace(t.Metric) == "" || strings.TrimSpace(t.Metric) != t.Metric || strings.IndexFunc(t.Metric, unicode.IsControl) >= 0 {
		return loaderr.Errorf(loaderr.Join(at, "metric"), "set metric to a non-empty metric name such as median_time or deaths", "project: brief playtest target metric %q is invalid", t.Metric)
	}
	if t.Scene != "" && (strings.TrimSpace(t.Scene) != t.Scene || strings.IndexFunc(t.Scene, unicode.IsControl) >= 0) {
		return loaderr.Errorf(loaderr.Join(at, "scene"), "use a scene name without surrounding spaces or control characters", "project: brief playtest target scene %q is invalid", t.Scene)
	}
	if t.Summary != "" && (strings.TrimSpace(t.Summary) != t.Summary || strings.IndexFunc(t.Summary, unicode.IsControl) >= 0) {
		return loaderr.Errorf(loaderr.Join(at, "summary"), "use a single-line summary without control characters", "project: brief playtest target summary %q is invalid", t.Summary)
	}
	if t.Unit != "" && (strings.TrimSpace(t.Unit) != t.Unit || strings.IndexFunc(t.Unit, unicode.IsControl) >= 0) {
		return loaderr.Errorf(loaderr.Join(at, "unit"), "use a unit string without surrounding spaces or control characters", "project: brief playtest target unit %q is invalid", t.Unit)
	}
	if t.Min == nil && t.Max == nil && t.Equal == nil {
		return loaderr.Errorf(at, "set at least one of min, max, or equal on the target", "project: brief playtest target %q must specify min, max, or equal", t.Metric)
	}
	if t.Equal != nil && (t.Min != nil || t.Max != nil) {
		return loaderr.Errorf(at, "use either equal or min/max on a target, not both", "project: brief playtest target %q cannot combine equal with min or max", t.Metric)
	}
	if t.Min != nil && !isFinite64(*t.Min) {
		return loaderr.Errorf(loaderr.Join(at, "min"), "use a finite number", "project: brief playtest target %q min is not finite", t.Metric)
	}
	if t.Max != nil && !isFinite64(*t.Max) {
		return loaderr.Errorf(loaderr.Join(at, "max"), "use a finite number", "project: brief playtest target %q max is not finite", t.Metric)
	}
	if t.Equal != nil && !isFinite64(*t.Equal) {
		return loaderr.Errorf(loaderr.Join(at, "equal"), "use a finite number", "project: brief playtest target %q equal is not finite", t.Metric)
	}
	if t.Min != nil && t.Max != nil && *t.Min > *t.Max {
		return loaderr.Errorf(loaderr.Join(at, "max"), "set max to at least min", "project: brief playtest target %q max %v is less than min %v", t.Metric, *t.Max, *t.Min)
	}
	return nil
}

func cleanZero64(v float64) float64 {
	if v == 0 {
		return 0
	}
	return v
}

func normalizeBrief(b *Brief) {
	if b == nil {
		return
	}
	b.Pitch = strings.TrimSpace(strings.ReplaceAll(b.Pitch, "\r\n", "\n"))
	b.ArtDirection = strings.TrimSpace(strings.ReplaceAll(b.ArtDirection, "\r\n", "\n"))
	b.ToneAndAudio = strings.TrimSpace(strings.ReplaceAll(b.ToneAndAudio, "\r\n", "\n"))
	if len(b.Genre) == 0 {
		b.Genre = nil
	}
	if len(b.Tags) == 0 {
		b.Tags = nil
	}
	if len(b.Platforms) == 0 {
		b.Platforms = nil
	}
	if b.Art != nil {
		b.Art.Direction = strings.TrimSpace(strings.ReplaceAll(b.Art.Direction, "\r\n", "\n"))
		if len(b.Art.References) == 0 {
			b.Art.References = nil
		}
		if b.Art.Direction == "" && len(b.Art.References) == 0 {
			b.Art = nil
		}
	}
	if b.Feel != nil {
		b.Feel.Summary = strings.TrimSpace(strings.ReplaceAll(b.Feel.Summary, "\r\n", "\n"))
		for i := range b.Feel.Targets {
			t := &b.Feel.Targets[i]
			if t.Min != nil {
				v := cleanZero64(*t.Min)
				t.Min = &v
			}
			if t.Max != nil {
				v := cleanZero64(*t.Max)
				t.Max = &v
			}
			if t.Equal != nil {
				v := cleanZero64(*t.Equal)
				t.Equal = &v
			}
		}
		if len(b.Feel.Targets) == 0 {
			b.Feel.Targets = nil
		}
		if b.Feel.Summary == "" && len(b.Feel.Targets) == 0 {
			b.Feel = nil
		}
	}
}

// EncodeBrief validates b and writes it as deterministic 2-space YAML prefixed with BriefSchemaComment.
// A file EncodeBrief wrote loads and saves back unchanged byte for byte.
func EncodeBrief(b *Brief) ([]byte, error) {
	if err := ValidateBrief(b); err != nil {
		return nil, err
	}
	cp := b.Clone()
	normalizeBrief(cp)

	var buf bytes.Buffer
	buf.WriteString(BriefSchemaComment)
	buf.WriteByte('\n')
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cp); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("project: encode brief: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("project: encode brief: %w", err)
	}
	return buf.Bytes(), nil
}

const maxBriefFileBytes = 1 << 20

func readBoundedBrief(r io.Reader, name string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBriefFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBriefFileBytes {
		return nil, loaderr.WithFile(
			loaderr.Errorf("", "keep brief.yaml under 1 MiB", "project: brief file exceeds maximum size of %d bytes", maxBriefFileBytes),
			name,
		)
	}
	return data, nil
}

// ReadBriefFS reads and parses BriefFileName at the root of fsys.
func ReadBriefFS(fsys fs.FS) (*Brief, error) {
	f, err := fsys.Open(BriefFileName)
	if err != nil {
		return nil, fmt.Errorf("project: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := readBoundedBrief(f, BriefFileName)
	if err != nil {
		return nil, err
	}
	b, err := ParseBrief(data)
	if err != nil {
		return nil, loaderr.WithFile(err, BriefFileName)
	}
	return b, nil
}

// LoadBrief reads and parses brief.yaml from path.
func LoadBrief(path string) (*Brief, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := readBoundedBrief(f, path)
	if err != nil {
		return nil, err
	}
	b, err := ParseBrief(data)
	if err != nil {
		return nil, loaderr.WithFile(err, path)
	}
	return b, nil
}

// SaveBrief validates, encodes, and writes b to path in canonical YAML form.
func SaveBrief(path string, b *Brief) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("project: brief path is empty")
	}
	data, err := EncodeBrief(b)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("project: create brief directory: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// Clone returns a deep copy of b, or nil when b is nil.
func (b *Brief) Clone() *Brief {
	if b == nil {
		return nil
	}
	cp := *b
	cp.Genre = slices.Clone(b.Genre)
	cp.Tags = slices.Clone(b.Tags)
	cp.Platforms = slices.Clone(b.Platforms)
	if b.Art != nil {
		artCopy := BriefArt{
			Direction:  b.Art.Direction,
			References: slices.Clone(b.Art.References),
		}
		cp.Art = &artCopy
	}
	if b.Feel != nil {
		f := BriefFeel{
			Summary: b.Feel.Summary,
		}
		if len(b.Feel.Targets) > 0 {
			f.Targets = make([]BriefPlaytestTarget, len(b.Feel.Targets))
			for i, t := range b.Feel.Targets {
				t.Min = clonePtr(t.Min)
				t.Max = clonePtr(t.Max)
				t.Equal = clonePtr(t.Equal)
				f.Targets[i] = t
			}
		}
		cp.Feel = &f
	}
	return &cp
}

// MissingSections returns the brief sections that are currently empty, in canonical BriefSections order.
func (b *Brief) MissingSections() []BriefSection {
	if b == nil {
		return slices.Clone(BriefSections)
	}
	var gaps []BriefSection
	if strings.TrimSpace(b.Pitch) == "" {
		gaps = append(gaps, BriefSectionPitch)
	}
	if len(b.Genre) == 0 {
		gaps = append(gaps, BriefSectionGenre)
	}
	if len(b.Platforms) == 0 {
		gaps = append(gaps, BriefSectionPlatforms)
	}
	if strings.TrimSpace(b.ArtDirection) == "" && (b.Art == nil || (strings.TrimSpace(b.Art.Direction) == "" && len(b.Art.References) == 0)) {
		gaps = append(gaps, BriefSectionArtDirection)
	}
	if b.Feel == nil || (strings.TrimSpace(b.Feel.Summary) == "" && len(b.Feel.Targets) == 0) {
		gaps = append(gaps, BriefSectionFeel)
	}
	if strings.TrimSpace(b.ToneAndAudio) == "" {
		gaps = append(gaps, BriefSectionToneAndAudio)
	}
	return gaps
}

// TargetsForScene returns the playtest targets defined in b that apply to scene.
// Targets with an empty Scene apply to every scene; when both a global target and a scene-specific
// target exist for the same metric, the scene-specific target takes precedence.
// When scene is empty, all targets are returned in declaration order.
func (b *Brief) TargetsForScene(scene string) []BriefPlaytestTarget {
	if b == nil || b.Feel == nil || len(b.Feel.Targets) == 0 {
		return nil
	}
	scene = strings.TrimSpace(scene)
	if scene == "" {
		out := make([]BriefPlaytestTarget, len(b.Feel.Targets))
		for i, t := range b.Feel.Targets {
			t.Min = clonePtr(t.Min)
			t.Max = clonePtr(t.Max)
			t.Equal = clonePtr(t.Equal)
			out[i] = t
		}
		return out
	}
	byMetric := make(map[string]int)
	var out []BriefPlaytestTarget
	for _, t := range b.Feel.Targets {
		if t.Scene != "" && !strings.EqualFold(t.Scene, scene) {
			continue
		}
		t.Min = clonePtr(t.Min)
		t.Max = clonePtr(t.Max)
		t.Equal = clonePtr(t.Equal)
		if idx, exists := byMetric[t.Metric]; exists {
			// Prefer scene-specific over global target for the same metric.
			if t.Scene != "" && out[idx].Scene == "" {
				out[idx] = t
			}
			continue
		}
		byMetric[t.Metric] = len(out)
		out = append(out, t)
	}
	return out
}
