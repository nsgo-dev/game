package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/pkg/loaderr"
)

// AutomationsVersion is the YAML schema version of project automations files (automations.yaml).
const AutomationsVersion = 1

// AutomationsFileName is the shared project-root YAML file holding agent automations beside game.yaml.
const AutomationsFileName = "automations.yaml"

// AutomationsSchemaURL is where the JSON Schema of version 1 automations files is published.
// EncodeAutomations writes it in a yaml-language-server comment on the first line so editors and AI tools validate and complete automations.yaml as it is written.
const AutomationsSchemaURL = "https://game.nsgo.dev/schemas/automations.v1.json"

// AutomationsSchemaComment is the first line EncodeAutomations writes: a YAML comment pointing a YAML language server at AutomationsSchemaURL. ParseAutomations ignores it like any other comment.
const AutomationsSchemaComment = "# yaml-language-server: $schema=" + AutomationsSchemaURL

// AutomationsCheckpointsDir is the git-ignored project-relative directory storing pre-run automation checkpoints.
const AutomationsCheckpointsDir = ".nsgo/checkpoints"

// AutomationsStateFile is the git-ignored project-relative YAML file storing local weekly run history, daily trigger watermarks, and pause-for-today state.
const AutomationsStateFile = ".nsgo/automations-state.yaml"

const (
	// maxAutomationsFileBytes caps automations.yaml reads/parses to prevent oversized YAML DoS.
	maxAutomationsFileBytes = 1 << 20 // 1 MiB
	// maxAutomationRules caps the number of rules per automations.yaml.
	maxAutomationRules = 256
)

// AutomationTriggerKind selects the event that starts an automation.
type AutomationTriggerKind string

const (
	// AutomationTriggerFileChanged fires when a project file matching Trigger.Glob is saved or changes on disk.
	AutomationTriggerFileChanged AutomationTriggerKind = "file_changed"
	// AutomationTriggerReviewKept fires when the author keeps an edit review (optionally filtered by Trigger.Glob).
	AutomationTriggerReviewKept AutomationTriggerKind = "review_kept"
	// AutomationTriggerFileAdded fires when a new project file of Trigger.Kind is created or appears on disk.
	AutomationTriggerFileAdded AutomationTriggerKind = "file_added"
	// AutomationTriggerBeforeExport fires before an export or release build starts (optionally filtered by Trigger.Platform).
	AutomationTriggerBeforeExport AutomationTriggerKind = "before_export"
	// AutomationTriggerDaily fires once per calendar day at or after Trigger.Time ("HH:MM") while the editor is open.
	AutomationTriggerDaily AutomationTriggerKind = "daily"
)

// Valid reports whether k is a recognized automation trigger kind.
func (k AutomationTriggerKind) Valid() bool {
	switch k {
	case AutomationTriggerFileChanged,
		AutomationTriggerReviewKept,
		AutomationTriggerFileAdded,
		AutomationTriggerBeforeExport,
		AutomationTriggerDaily:
		return true
	default:
		return false
	}
}

// AutomationFileKind classifies a newly added project file for file_added triggers.
type AutomationFileKind string

const (
	AutomationFileKindPrefab    AutomationFileKind = "prefab"
	AutomationFileKindScene     AutomationFileKind = "scene"
	AutomationFileKindTileset   AutomationFileKind = "tileset"
	AutomationFileKindCutscene  AutomationFileKind = "cutscene"
	AutomationFileKindMaterial  AutomationFileKind = "material"
	AutomationFileKindParticles AutomationFileKind = "particles"
	AutomationFileKindRig       AutomationFileKind = "rig"
	AutomationFileKindScreen    AutomationFileKind = "screen"
	AutomationFileKindDataTable AutomationFileKind = "datatable"
	AutomationFileKindScript    AutomationFileKind = "script"
	AutomationFileKindAsset     AutomationFileKind = "asset"
	AutomationFileKindAny       AutomationFileKind = "any"
)

// Valid reports whether k is a recognized file kind for file_added triggers.
func (k AutomationFileKind) Valid() bool {
	switch k {
	case AutomationFileKindPrefab,
		AutomationFileKindScene,
		AutomationFileKindTileset,
		AutomationFileKindCutscene,
		AutomationFileKindMaterial,
		AutomationFileKindParticles,
		AutomationFileKindRig,
		AutomationFileKindScreen,
		AutomationFileKindDataTable,
		AutomationFileKindScript,
		AutomationFileKindAsset,
		AutomationFileKindAny:
		return true
	default:
		return false
	}
}

// AutomationTrigger describes when an automation starts.
type AutomationTrigger struct {
	// Event selects the trigger event ("file_changed", "review_kept", "file_added", "before_export", or "daily").
	Event AutomationTriggerKind `yaml:"event" schema:"required,enum=file_changed|review_kept|file_added|before_export|daily"`
	// Glob matches slash-separated project-relative file paths using *, **, and ?. Required for file_changed and optional for review_kept and file_added.
	Glob string `yaml:"glob,omitempty"`
	// Kind selects the kind of added file for file_added triggers ("prefab", "scene", "tileset", "cutscene", "material", "particles", "rig", "screen", "datatable", "script", "asset", or "any").
	Kind AutomationFileKind `yaml:"kind,omitempty" schema:"enum=prefab|scene|tileset|cutscene|material|particles|rig|screen|datatable|script|asset|any"`
	// Platform optionally restricts before_export triggers to a target platform such as "ios", "android", "web", "macos", "windows", or "linux".
	Platform string `yaml:"platform,omitempty"`
	// Time is the 24-hour local clock time ("HH:MM") when a daily trigger runs while the editor is open.
	Time string `yaml:"time,omitempty"`
}

// AutomationRule defines one WHEN -> THE AGENT automation entry in automations.yaml.
type AutomationRule struct {
	// ID uniquely identifies the automation inside automations.yaml (lowercase letters, digits, hyphens, or underscores).
	ID string `yaml:"id" schema:"required"`
	// Name is the short human-readable WHEN summary shown in Settings › Automations. When omitted, the editor derives one from Trigger.
	Name string `yaml:"name,omitempty"`
	// Enabled controls whether the automation is active.
	Enabled bool `yaml:"enabled"`
	// Trigger defines the event that starts the automation.
	Trigger AutomationTrigger `yaml:"trigger" schema:"required"`
	// Instruction is the work the agent performs when Trigger fires; its result always lands in Ready to review.
	Instruction string `yaml:"instruction" schema:"required"`
}

// AutomationsFile holds a project's shared agent automations in versioned YAML (automations.yaml beside game.yaml).
// A file looks like this:
//
//	# yaml-language-server: $schema=https://game.nsgo.dev/schemas/automations.v1.json
//	version: 1
//	automations:
//	  - id: level-check
//	    name: A level file changes
//	    enabled: true
//	    trigger:
//	      event: file_changed
//	      glob: scenes/level_*.scene.yaml
//	    instruction: runs game check and 5 playtests, reports only problems
//
// ParseAutomations decodes it strictly and EncodeAutomations writes it back in canonical form, so a file EncodeAutomations wrote survives a load and save byte for byte.
type AutomationsFile struct {
	// Version is the schema version; it must equal AutomationsVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Automations lists the project's configured automations in display order.
	Automations []AutomationRule `yaml:"automations,omitempty"`
}

// DefaultAutomations returns the five starter automations shown in design/editor/Settings.dc.html.
func DefaultAutomations() *AutomationsFile {
	return &AutomationsFile{
		Version: AutomationsVersion,
		Automations: []AutomationRule{
			{
				ID:      "level-check",
				Name:    "A level file changes",
				Enabled: true,
				Trigger: AutomationTrigger{
					Event: AutomationTriggerFileChanged,
					Glob:  "scenes/level_*.scene.yaml",
				},
				Instruction: "runs game check and 5 playtests, reports only problems",
			},
			{
				ID:      "review-brief",
				Name:    "You keep a review",
				Enabled: true,
				Trigger: AutomationTrigger{
					Event: AutomationTriggerReviewKept,
				},
				Instruction: "updates the brief's open questions if the change answers one",
			},
			{
				ID:      "new-prefab-example",
				Name:    "A new prefab is added",
				Enabled: true,
				Trigger: AutomationTrigger{
					Event: AutomationTriggerFileAdded,
					Kind:  AutomationFileKindPrefab,
				},
				Instruction: "writes its example scene and a test",
			},
			{
				ID:      "before-export-mobile",
				Name:    "Before export",
				Enabled: false,
				Trigger: AutomationTrigger{
					Event: AutomationTriggerBeforeExport,
				},
				Instruction: "checks frame time and memory on a low-end phone profile",
			},
			{
				ID:      "nightly-balance",
				Name:    "Every night",
				Enabled: false,
				Trigger: AutomationTrigger{
					Event: AutomationTriggerDaily,
					Time:  "02:00",
				},
				Instruction: "plays all floors 50 times and leaves a balance report",
			},
		},
	}
}

const (
	fixAutomations        = "write automations.yaml as version: 1 with an automations list of {id, enabled, trigger, instruction}"
	fixAutomationsVersion = "set version: 1"
	fixAutomationsYAMLRef = "write values directly without YAML anchors, aliases or merge keys"
)

// ParseAutomations decodes and validates a versioned YAML automations document.
// Unknown fields, trailing YAML documents, YAML anchors/aliases/merge keys, duplicate keys, missing or unsupported versions, duplicate IDs, and invalid trigger configurations are refused.
//
// Errors are *loaderr.Error values carrying the line, column, field path, and fix; a caller that read data from a file names it with loaderr.WithFile.
func ParseAutomations(data []byte) (*AutomationsFile, error) {
	f, err := parseAutomations(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixAutomations)
	}
	return f, nil
}

func parseAutomations(data []byte) (*AutomationsFile, error) {
	if len(data) > maxAutomationsFileBytes {
		return nil, fmt.Errorf("editor: automations document exceeds %d bytes", maxAutomationsFileBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("editor: empty automations document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var f AutomationsFile
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("editor: empty automations document")
		}
		return nil, fmt.Errorf("editor: decode automations: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("editor: unexpected trailing YAML document in automations")
		}
		return nil, fmt.Errorf("editor: decode automations: %w", err)
	}
	if err := ValidateAutomations(&f); err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("editor: decode automations: %w", err)
	}
	if err := checkPlainAutomationsNode(&root, f.Version); err != nil {
		return nil, err
	}
	return &f, nil
}

func checkPlainAutomationsNode(n *yaml.Node, version int) error {
	if err := checkNoAutomationsYAMLReferences(n); err != nil {
		return err
	}
	if err := checkDuplicateYAMLKeys(n); err != nil {
		return fmt.Errorf("editor: automations: %w", err)
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
		if key == "version" {
			if want := strconv.Itoa(version); val.Value != want {
				return loaderr.Errorf("version", "write the version as "+want, "editor: automations version %q must be written as %s", val.Value, want)
			}
		}
	}
	return nil
}

func checkNoAutomationsYAMLReferences(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("editor: automations line %d: YAML aliases are not allowed", n.Line), Fix: fixAutomationsYAMLRef}
	}
	if n.Anchor != "" {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("editor: automations line %d: YAML anchors are not allowed", n.Line), Fix: fixAutomationsYAMLRef}
	}
	for i, c := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 && c.Tag == "!!merge" {
			return &loaderr.Error{Line: c.Line, Column: c.Column, Problem: fmt.Sprintf("editor: automations line %d: YAML merge keys are not allowed", c.Line), Fix: fixAutomationsYAMLRef}
		}
		if err := checkNoAutomationsYAMLReferences(c); err != nil {
			return err
		}
	}
	return nil
}

// ValidateAutomations checks f against the version 1 automations schema.
func ValidateAutomations(f *AutomationsFile) error {
	if f == nil {
		return errors.New("editor: nil automations file")
	}
	switch f.Version {
	case AutomationsVersion:
	case 0:
		return loaderr.Errorf("version", fixAutomationsVersion, "editor: automations is missing version (want %d)", AutomationsVersion)
	default:
		return loaderr.Errorf("version", fixAutomationsVersion, "editor: unsupported automations version %d (want %d)", f.Version, AutomationsVersion)
	}
	if len(f.Automations) > maxAutomationRules {
		return loaderr.Errorf("automations", fmt.Sprintf("keep at most %d automations in automations.yaml", maxAutomationRules), "editor: automations has %d rules (max %d)", len(f.Automations), maxAutomationRules)
	}
	seen := make(map[string]struct{}, len(f.Automations))
	for i, rule := range f.Automations {
		prefix := fmt.Sprintf("automations[%d]", i)
		if err := validateAutomationRule(rule, prefix); err != nil {
			return err
		}
		if _, dup := seen[rule.ID]; dup {
			return loaderr.Errorf(prefix+".id", "give each automation a unique id", "editor: duplicate automation id %q", rule.ID)
		}
		seen[rule.ID] = struct{}{}
	}
	return nil
}

func validateAutomationRule(rule AutomationRule, prefix string) error {
	if err := validateAutomationID(rule.ID); err != nil {
		return loaderr.Errorf(prefix+".id", "use a lowercase id made of letters, digits, hyphens, or underscores (for example level-check)", "%v", err)
	}
	if strings.TrimSpace(rule.Instruction) == "" {
		return loaderr.Errorf(prefix+".instruction", "describe what the agent should do when the trigger fires", "editor: automation %q has an empty instruction", rule.ID)
	}
	if err := validateAutomationTrigger(rule.Trigger, prefix+".trigger"); err != nil {
		return err
	}
	return nil
}

func validateAutomationID(id string) error {
	if id == "" {
		return errors.New("editor: automation id is required")
	}
	if len(id) > 64 {
		return fmt.Errorf("editor: automation id %q exceeds 64 characters", id)
	}
	for i, r := range id {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (i > 0 && (r == '-' || r == '_'))
		if !ok {
			return fmt.Errorf("editor: invalid automation id %q", id)
		}
	}
	return nil
}

func validateAutomationTrigger(tr AutomationTrigger, prefix string) error {
	if !tr.Event.Valid() {
		return loaderr.Errorf(prefix+".event", "set trigger.event to file_changed, review_kept, file_added, before_export, or daily", "editor: invalid automation trigger %q", tr.Event)
	}
	if tr.Glob != "" {
		if err := validateAutomationGlob(tr.Glob); err != nil {
			return loaderr.Errorf(prefix+".glob", "write a project-relative slash-separated glob such as scenes/level_*.scene.yaml", "%v", err)
		}
	}
	if tr.Platform != "" {
		if err := validateAutomationPlatform(tr.Platform); err != nil {
			return loaderr.Errorf(prefix+".platform", "set platform to a lowercase platform name such as ios, android, web, macos, windows, or linux", "%v", err)
		}
	}
	switch tr.Event {
	case AutomationTriggerFileChanged:
		if tr.Glob == "" {
			return loaderr.Errorf(prefix+".glob", "set glob to match the changed files (for example scenes/level_*.scene.yaml)", "editor: file_changed trigger requires glob")
		}
		if tr.Kind != "" {
			return loaderr.Errorf(prefix+".kind", "remove kind from file_changed triggers (use glob instead)", "editor: file_changed trigger does not accept kind")
		}
		if tr.Platform != "" {
			return loaderr.Errorf(prefix+".platform", "remove platform from file_changed triggers", "editor: file_changed trigger does not accept platform")
		}
		if tr.Time != "" {
			return loaderr.Errorf(prefix+".time", "remove time from file_changed triggers", "editor: file_changed trigger does not accept time")
		}
	case AutomationTriggerReviewKept:
		if tr.Kind != "" {
			return loaderr.Errorf(prefix+".kind", "remove kind from review_kept triggers", "editor: review_kept trigger does not accept kind")
		}
		if tr.Platform != "" {
			return loaderr.Errorf(prefix+".platform", "remove platform from review_kept triggers", "editor: review_kept trigger does not accept platform")
		}
		if tr.Time != "" {
			return loaderr.Errorf(prefix+".time", "remove time from review_kept triggers", "editor: review_kept trigger does not accept time")
		}
	case AutomationTriggerFileAdded:
		if !tr.Kind.Valid() {
			return loaderr.Errorf(prefix+".kind", "set kind to prefab, scene, tileset, cutscene, material, particles, rig, screen, datatable, script, asset, or any", "editor: file_added trigger has invalid kind %q", tr.Kind)
		}
		if tr.Platform != "" {
			return loaderr.Errorf(prefix+".platform", "remove platform from file_added triggers", "editor: file_added trigger does not accept platform")
		}
		if tr.Time != "" {
			return loaderr.Errorf(prefix+".time", "remove time from file_added triggers", "editor: file_added trigger does not accept time")
		}
	case AutomationTriggerBeforeExport:
		if tr.Glob != "" {
			return loaderr.Errorf(prefix+".glob", "remove glob from before_export triggers", "editor: before_export trigger does not accept glob")
		}
		if tr.Kind != "" {
			return loaderr.Errorf(prefix+".kind", "remove kind from before_export triggers", "editor: before_export trigger does not accept kind")
		}
		if tr.Time != "" {
			return loaderr.Errorf(prefix+".time", "remove time from before_export triggers", "editor: before_export trigger does not accept time")
		}
	case AutomationTriggerDaily:
		if _, _, err := parseDailyClockTime(tr.Time); err != nil {
			return loaderr.Errorf(prefix+".time", "write a 24-hour time in HH:MM format such as 02:00", "%v", err)
		}
		if tr.Glob != "" {
			return loaderr.Errorf(prefix+".glob", "remove glob from daily triggers", "editor: daily trigger does not accept glob")
		}
		if tr.Kind != "" {
			return loaderr.Errorf(prefix+".kind", "remove kind from daily triggers", "editor: daily trigger does not accept kind")
		}
		if tr.Platform != "" {
			return loaderr.Errorf(prefix+".platform", "remove platform from daily triggers", "editor: daily trigger does not accept platform")
		}
	}
	return nil
}

func validateAutomationGlob(glob string) error {
	if strings.TrimSpace(glob) == "" {
		return errors.New("editor: glob cannot be empty")
	}
	if strings.Contains(glob, "\\") {
		return fmt.Errorf("editor: glob %q must use forward slashes", glob)
	}
	if strings.HasPrefix(glob, "/") {
		return fmt.Errorf("editor: glob %q must be project-relative", glob)
	}
	for _, seg := range strings.Split(glob, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("editor: glob %q has an invalid path segment %q", glob, seg)
		}
		if seg != "**" && strings.Contains(seg, "**") {
			return fmt.Errorf("editor: glob %q must write ** as its own path segment", glob)
		}
		if seg != "**" {
			if _, err := path.Match(seg, "a"); err != nil {
				return fmt.Errorf("editor: invalid glob %q: %w", glob, err)
			}
		}
	}
	return nil
}

func validateAutomationPlatform(platform string) error {
	switch platform {
	case "ios", "android", "web", "macos", "windows", "linux":
		return nil
	default:
		return fmt.Errorf("editor: unsupported export platform %q", platform)
	}
}

// parseDailyClockTime validates a canonical 24-hour "HH:MM" time string.
func parseDailyClockTime(s string) (hour, minute int, err error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, fmt.Errorf("editor: daily trigger time %q must be in HH:MM format", s)
	}
	for _, idx := range []int{0, 1, 3, 4} {
		if s[idx] < '0' || s[idx] > '9' {
			return 0, 0, fmt.Errorf("editor: daily trigger time %q must be in HH:MM format", s)
		}
	}
	h, _ := strconv.Atoi(s[:2])
	m, _ := strconv.Atoi(s[3:])
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("editor: daily trigger time %q is out of range (00:00–23:59)", s)
	}
	return h, m, nil
}

// EncodeAutomations validates f and writes deterministic 2-space YAML prefixed with AutomationsSchemaComment.
func EncodeAutomations(f *AutomationsFile) ([]byte, error) {
	if err := ValidateAutomations(f); err != nil {
		return nil, err
	}
	out := AutomationsFile{
		Version: f.Version,
	}
	if len(f.Automations) > 0 {
		out.Automations = make([]AutomationRule, len(f.Automations))
		for i, r := range f.Automations {
			out.Automations[i] = AutomationRule{
				ID:          r.ID,
				Name:        strings.TrimSpace(r.Name),
				Enabled:     r.Enabled,
				Trigger:     r.Trigger,
				Instruction: strings.TrimSpace(r.Instruction),
			}
		}
	}
	var buf bytes.Buffer
	buf.WriteString(AutomationsSchemaComment)
	buf.WriteByte('\n')
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&out); err != nil {
		return nil, fmt.Errorf("editor: encode automations: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("editor: encode automations: %w", err)
	}
	return buf.Bytes(), nil
}

// LoadAutomationsFile reads and parses automations.yaml at path.
// When the file does not exist, it returns (nil, fs.ErrNotExist) so callers can decide whether to fall back to defaults.
func LoadAutomationsFile(filePath string) (*AutomationsFile, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, errors.New("editor: automations path is empty")
	}
	fp, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer fp.Close()
	data, err := io.ReadAll(io.LimitReader(fp, maxAutomationsFileBytes+1))
	if err != nil {
		return nil, err
	}
	f, err := ParseAutomations(data)
	if err != nil {
		return nil, loaderr.WithFile(err, filePath)
	}
	return f, nil
}

// SaveAutomationsFile encodes f in canonical YAML and writes it atomically to filePath.
func SaveAutomationsFile(filePath string, f *AutomationsFile) error {
	if strings.TrimSpace(filePath) == "" {
		return errors.New("editor: automations path is empty")
	}
	data, err := EncodeAutomations(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(filePath, data)
}

// DisplayName returns the human-readable WHEN summary for r, deriving one from r.Trigger when Name is empty.
func (r AutomationRule) DisplayName() string {
	if s := strings.TrimSpace(r.Name); s != "" {
		return s
	}
	return r.Trigger.Summary()
}

// Summary returns a concise human-readable WHEN description for t.
func (t AutomationTrigger) Summary() string {
	switch t.Event {
	case AutomationTriggerFileChanged:
		if t.Glob != "" {
			return "File matches " + t.Glob
		}
		return "A file changes"
	case AutomationTriggerReviewKept:
		if t.Glob != "" {
			return "You keep a review (" + t.Glob + ")"
		}
		return "You keep a review"
	case AutomationTriggerFileAdded:
		if t.Kind != "" && t.Kind != AutomationFileKindAny {
			return "A new " + string(t.Kind) + " is added"
		}
		return "A new file is added"
	case AutomationTriggerBeforeExport:
		if t.Platform != "" {
			return "Before " + t.Platform + " export"
		}
		return "Before export"
	case AutomationTriggerDaily:
		if t.Time != "" {
			return "Every day at " + t.Time
		}
		return "Every day"
	default:
		return string(t.Event)
	}
}

// MatchAutomationGlob reports whether project-relative slash-separated relPath matches pattern.
// Pattern supports * (single path segment wildcard), ? (single non-slash character), and ** (zero or more path segments).
func MatchAutomationGlob(pattern, relPath string) bool {
	p := normalizeProjectRelPath(pattern)
	s := normalizeProjectRelPath(relPath)
	if p == "" || s == "" {
		return false
	}
	pSegs := strings.Split(p, "/")
	sSegs := strings.Split(s, "/")
	return matchGlobSegments(pSegs, sSegs)
}

// isAutomationPathWithinRoot reports whether targetPath resolves strictly within rootDir
// after evaluating symlinks along its existing ancestors (preventing symlink escapes).
func isAutomationPathWithinRoot(rootDir, targetPath string) bool {
	if strings.TrimSpace(rootDir) == "" || strings.TrimSpace(targetPath) == "" {
		return false
	}
	realRoot, ok := resolvePathThroughExistingAncestors(rootDir)
	if !ok {
		return false
	}
	realTarget, ok := resolvePathThroughExistingAncestors(targetPath)
	if !ok {
		return false
	}
	rel, err := filepath.Rel(realRoot, realTarget)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// ClassifyAutomationFileKind infers the AutomationFileKind of a project-relative slash-separated file path.
func ClassifyAutomationFileKind(relPath string) AutomationFileKind {
	s := strings.ToLower(normalizeProjectRelPath(relPath))
	if s == "" {
		return AutomationFileKindAny
	}
	switch {
	case strings.HasSuffix(s, ".prefab.yaml") || strings.HasSuffix(s, ".prefab.yml") ||
		((strings.HasPrefix(s, "prefabs/") || strings.Contains(s, "/prefabs/")) && (strings.HasSuffix(s, ".yaml") || strings.HasSuffix(s, ".yml"))):
		return AutomationFileKindPrefab
	case strings.HasSuffix(s, ".scene.yaml") || strings.HasSuffix(s, ".scene.yml"):
		return AutomationFileKindScene
	case strings.HasSuffix(s, ".tileset.yaml") || strings.HasSuffix(s, ".tileset.yml"):
		return AutomationFileKindTileset
	case strings.HasSuffix(s, ".cutscene.yaml") || strings.HasSuffix(s, ".cutscene.yml"):
		return AutomationFileKindCutscene
	case strings.HasSuffix(s, ".material.yaml") || strings.HasSuffix(s, ".material.yml"):
		return AutomationFileKindMaterial
	case strings.HasSuffix(s, ".particles.yaml") || strings.HasSuffix(s, ".particles.yml"):
		return AutomationFileKindParticles
	case strings.HasSuffix(s, ".rig.yaml") || strings.HasSuffix(s, ".rig.yml"):
		return AutomationFileKindRig
	case strings.HasSuffix(s, ".screen.yaml") || strings.HasSuffix(s, ".screen.yml"):
		return AutomationFileKindScreen
	case strings.HasSuffix(s, ".table.yaml") || strings.HasSuffix(s, ".table.yml") ||
		((strings.HasPrefix(s, "data/") || strings.Contains(s, "/data/")) && (strings.HasSuffix(s, ".yaml") || strings.HasSuffix(s, ".yml"))):
		return AutomationFileKindDataTable
	case strings.HasSuffix(s, ".go"):
		return AutomationFileKindScript
	case strings.HasPrefix(s, "assets/") || hasAssetMediaExt(s):
		return AutomationFileKindAsset
	default:
		return AutomationFileKindAny
	}
}

func hasAssetMediaExt(lower string) bool {
	switch path.Ext(lower) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".wav", ".ogg", ".mp3", ".flac", ".glb", ".gltf", ".ttf", ".otf", ".fnt":
		return true
	default:
		return false
	}
}

func normalizeProjectRelPath(p string) string {
	s := strings.TrimSpace(filepath.ToSlash(p))
	for strings.HasPrefix(s, "./") {
		s = strings.TrimPrefix(s, "./")
	}
	s = strings.TrimPrefix(s, "/")
	if s == "" || s == "." {
		return ""
	}
	cleaned := path.Clean(s)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

// isInternalAutomationPath reports whether relPath is internal editor/runtime state that must never trigger automations.
func isInternalAutomationPath(relPath string) bool {
	s := normalizeProjectRelPath(relPath)
	if s == "" {
		return true
	}
	if s == AutomationsFileName || s == ".git" || strings.HasPrefix(s, ".git/") ||
		s == ".nsgo" || strings.HasPrefix(s, ".nsgo/") ||
		s == ".game" || strings.HasPrefix(s, ".game/") ||
		s == "dist" || strings.HasPrefix(s, "dist/") {
		return true
	}
	return false
}

// slugifyAutomationID builds a valid lowercase kebab-case automation ID from arbitrary text.
func slugifyAutomationID(text string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if unicode.IsSpace(r) || r == '-' || r == '_' || r == '/' || r == '.' {
			if b.Len() > 0 && !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
		if b.Len() >= 48 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "automation"
	}
	return out
}

// Ensure fs.ErrNotExist is referenced cleanly for callers checking LoadAutomationsFile.
var _ = fs.ErrNotExist
