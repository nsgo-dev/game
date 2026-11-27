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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/pkg/loaderr"
)

// PermissionsVersion is the YAML schema version of project permission rules files (permissions.yaml).
const PermissionsVersion = 1

// PermissionsFileName is the tracked project file beside game.yaml that stores shared MCP agent permission profiles and rules.
// Per-machine editor state stays under git-ignored .nsgo/ and .game/, while shared project permissions live beside game.yaml so teams track and review them in Git.
const PermissionsFileName = "permissions.yaml"

// PermissionsSchemaURL is where the JSON Schema of version 1 project permission rules files is published.
// EncodePermissions writes it in a yaml-language-server comment on the first line so editors and AI tools validate and complete permissions.yaml as it is edited.
const PermissionsSchemaURL = "https://game.nsgo.dev/schemas/permissions.v1.json"

// PermissionsSchemaComment is the first line EncodePermissions writes: a YAML comment pointing a YAML language server at PermissionsSchemaURL. ParsePermissions ignores it like any other comment.
const PermissionsSchemaComment = "# yaml-language-server: $schema=" + PermissionsSchemaURL

const (
	maxPermissionsFileBytes      = 1 << 20 // 1 MiB cap on permissions.yaml reads/parses
	maxPermissionsRules          = 512     // cap on rules per permissions.yaml
	maxPermissionsPatternsPerRow = 128     // cap on patterns per rule
	maxPendingPermissionRequests = 64      // cap on queued permission requests in memory
)

// PermissionProfile selects a preset rule table for what the editor's MCP server lets an agent do on its own.
// Profiles are presets of rules; the rules themselves decide every tool call.
type PermissionProfile string

const (
	// PermissionProfileCautious asks before every edit, creation, game check, and playtest, and denies deleting files or running shell commands.
	PermissionProfileCautious PermissionProfile = "cautious"
	// PermissionProfileBalanced edits content (scenes/**, prefabs/**, data/**) and runs game check and playtests on its own, asks before game.yaml, brief.yaml, Go code, and file creation, and denies deleting files or running shell commands.
	PermissionProfileBalanced PermissionProfile = "balanced"
	// PermissionProfileAutonomous does whole tasks end to end while keeping reviewable editor undo steps; deletes and shell commands remain denied unless explicitly allowed.
	PermissionProfileAutonomous PermissionProfile = "autonomous"
)

// Valid reports whether p is a recognized permission profile name.
func (p PermissionProfile) Valid() bool {
	switch p {
	case PermissionProfileCautious, PermissionProfileBalanced, PermissionProfileAutonomous:
		return true
	default:
		return false
	}
}

// DisplayName returns the English label for p.
func (p PermissionProfile) DisplayName() string {
	switch p {
	case PermissionProfileCautious:
		return "Cautious"
	case PermissionProfileBalanced:
		return "Balanced"
	case PermissionProfileAutonomous:
		return "Autonomous"
	default:
		return string(p)
	}
}

// PermissionAction is the kind of mutating or running work a permission rule governs.
type PermissionAction string

const (
	// PermissionActionEdit modifies existing project files or open editor documents.
	PermissionActionEdit PermissionAction = "edit"
	// PermissionActionCreate creates new files anywhere in the project.
	PermissionActionCreate PermissionAction = "create"
	// PermissionActionDelete deletes files from the project.
	PermissionActionDelete PermissionAction = "delete"
	// PermissionActionRun runs project work such as game check, headless playtests, or shell commands.
	PermissionActionRun PermissionAction = "run"
	// PermissionActionCode edits Go source code (such as **/*.go).
	PermissionActionCode PermissionAction = "code"
)

// Valid reports whether a is a recognized permission action.
func (a PermissionAction) Valid() bool {
	switch a {
	case PermissionActionEdit, PermissionActionCreate, PermissionActionDelete, PermissionActionRun, PermissionActionCode:
		return true
	default:
		return false
	}
}

// DisplayName returns the English label for a.
func (a PermissionAction) DisplayName() string {
	switch a {
	case PermissionActionEdit:
		return "Edit"
	case PermissionActionCreate:
		return "Create"
	case PermissionActionDelete:
		return "Delete"
	case PermissionActionRun:
		return "Run"
	case PermissionActionCode:
		return "Code"
	default:
		return string(a)
	}
}

// PermissionDecision is what the MCP server does when a rule matches a tool call.
type PermissionDecision string

const (
	// PermissionAllow runs the tool call immediately.
	PermissionAllow PermissionDecision = "allow"
	// PermissionAsk opens a permission request for the person to allow once, always allow, or deny.
	PermissionAsk PermissionDecision = "ask"
	// PermissionDeny refuses the tool call before running it and returns an error naming the rule.
	PermissionDeny PermissionDecision = "deny"
)

// Valid reports whether d is a recognized permission decision.
func (d PermissionDecision) Valid() bool {
	switch d {
	case PermissionAllow, PermissionAsk, PermissionDeny:
		return true
	default:
		return false
	}
}

// DisplayName returns the English label for d.
func (d PermissionDecision) DisplayName() string {
	switch d {
	case PermissionAllow:
		return "Allow"
	case PermissionAsk:
		return "Ask"
	case PermissionDeny:
		return "Deny"
	default:
		return string(d)
	}
}

// Strictness ranks decisions from loosest (0 = Allow) to strictest (2 = Deny; invalid = 3 fail-closed).
func (d PermissionDecision) Strictness() int {
	switch d {
	case PermissionAllow:
		return 0
	case PermissionAsk:
		return 1
	case PermissionDeny:
		return 2
	default:
		return 3
	}
}

// Well-known run target names matched by PermissionActionRun rules.
const (
	PermissionRunGameCheck = "game check"
	PermissionRunPlaytests = "playtests"
	PermissionRunShell     = "shell"
)

// PermissionRule is one row in the first-match-wins permission rule table.
type PermissionRule struct {
	// Action is the kind of operation this rule matches ("edit", "create", "delete", "run", or "code").
	Action PermissionAction `yaml:"action" json:"action" schema:"required,enum=edit|create|delete|run|code"`
	// Patterns lists slash-separated file globs (such as "scenes/**", "game.yaml", "**/*.go", "**") or run targets (such as "game check", "playtests", "shell").
	Patterns []string `yaml:"patterns" json:"patterns" schema:"required,minItems=1"`
	// Decision is what happens when this rule is the first match ("allow", "ask", or "deny").
	Decision PermissionDecision `yaml:"decision" json:"decision" schema:"required,enum=allow|ask|deny"`
}

// Clone returns a deep copy of r.
func (r PermissionRule) Clone() PermissionRule {
	return PermissionRule{
		Action:   r.Action,
		Patterns: slices.Clone(r.Patterns),
		Decision: r.Decision,
	}
}

// Summary formats r as a compact readable string such as "Run · shell commands → Deny".
func (r PermissionRule) Summary() string {
	return fmt.Sprintf("%s · %s → %s", r.Action.DisplayName(), FormatPermissionPatterns(r.Action, r.Patterns), r.Decision.DisplayName())
}

// FormatPermissionPatterns formats a rule's pattern list for human-readable UI and error messages.
func FormatPermissionPatterns(action PermissionAction, patterns []string) string {
	parts := make([]string, 0, len(patterns))
	for _, p := range patterns {
		switch {
		case action == PermissionActionRun && strings.EqualFold(strings.TrimSpace(p), PermissionRunShell):
			parts = append(parts, "shell commands")
		case action == PermissionActionDelete && p == "**":
			parts = append(parts, "any file")
		case action == PermissionActionCreate && p == "**":
			parts = append(parts, "anywhere in the project")
		default:
			parts = append(parts, p)
		}
	}
	sep := "  "
	if action == PermissionActionRun {
		sep = " · "
	}
	return strings.Join(parts, sep)
}

// PermissionsFile is the versioned YAML document stored in permissions.yaml beside game.yaml.
// Rules are evaluated from top to bottom and the first matching rule decides.
// A file looks like this:
//
//	# yaml-language-server: $schema=https://game.nsgo.dev/schemas/permissions.v1.json
//	version: 1
//	profile: balanced
//	rules:
//	  - action: edit
//	    patterns:
//	      - scenes/**
//	      - prefabs/**
//	      - data/**
//	    decision: allow
type PermissionsFile struct {
	// Version is the schema version; it must equal PermissionsVersion (1).
	Version int `yaml:"version" json:"version" schema:"required"`
	// Profile names the base preset ("cautious", "balanced", or "autonomous").
	// Editing individual rules keeps Profile so the editor can mark the profile as changed from its preset.
	Profile PermissionProfile `yaml:"profile" json:"profile" schema:"required,enum=cautious|balanced|autonomous"`
	// Rules is the ordered first-match-wins permission table checked before every mutating or running MCP tool call.
	Rules []PermissionRule `yaml:"rules" json:"rules" schema:"required"`
}

// Clone returns a deep copy of f.
func (f PermissionsFile) Clone() PermissionsFile {
	out := PermissionsFile{
		Version: f.Version,
		Profile: f.Profile,
		Rules:   make([]PermissionRule, len(f.Rules)),
	}
	for i, r := range f.Rules {
		out.Rules[i] = r.Clone()
	}
	return out
}

// PermissionPreset returns the canonical rule table for profile (defaulting to Balanced for an unknown profile).
func PermissionPreset(profile PermissionProfile) PermissionsFile {
	if !profile.Valid() {
		profile = PermissionProfileBalanced
	}
	contentDec := PermissionAllow
	runChecksDec := PermissionAllow
	configDec := PermissionAsk
	codeDec := PermissionAsk
	createDec := PermissionAsk
	switch profile {
	case PermissionProfileCautious:
		contentDec = PermissionAsk
		runChecksDec = PermissionAsk
		configDec = PermissionAsk
		codeDec = PermissionAsk
		createDec = PermissionAsk
	case PermissionProfileBalanced:
		contentDec = PermissionAllow
		runChecksDec = PermissionAllow
		configDec = PermissionAsk
		codeDec = PermissionAsk
		createDec = PermissionAsk
	case PermissionProfileAutonomous:
		contentDec = PermissionAllow
		runChecksDec = PermissionAllow
		configDec = PermissionAllow
		codeDec = PermissionAllow
		createDec = PermissionAllow
	}
	return PermissionsFile{
		Version: PermissionsVersion,
		Profile: profile,
		Rules: []PermissionRule{
			{
				Action:   PermissionActionEdit,
				Patterns: []string{"scenes/**", "prefabs/**", "data/**"},
				Decision: contentDec,
			},
			{
				Action:   PermissionActionRun,
				Patterns: []string{PermissionRunGameCheck, PermissionRunPlaytests},
				Decision: runChecksDec,
			},
			{
				Action:   PermissionActionEdit,
				Patterns: []string{"game.yaml", "brief.yaml"},
				Decision: configDec,
			},
			{
				Action:   PermissionActionCode,
				Patterns: []string{"**/*.go"},
				Decision: codeDec,
			},
			{
				Action:   PermissionActionCreate,
				Patterns: []string{"**"},
				Decision: createDec,
			},
			{
				Action:   PermissionActionDelete,
				Patterns: []string{"**"},
				Decision: PermissionDeny,
			},
			{
				Action:   PermissionActionRun,
				Patterns: []string{PermissionRunShell},
				Decision: PermissionDeny,
			},
		},
	}
}

// DefaultPermissions returns the editor's default project permissions: version 1 with the Balanced profile preset.
func DefaultPermissions() PermissionsFile {
	return PermissionPreset(PermissionProfileBalanced)
}

// IsModified reports whether f's rules differ from PermissionPreset(f.Profile).
func (f PermissionsFile) IsModified() bool {
	preset := PermissionPreset(f.Profile)
	if len(f.Rules) != len(preset.Rules) {
		return true
	}
	for i := range f.Rules {
		a, b := f.Rules[i], preset.Rules[i]
		if a.Action != b.Action || a.Decision != b.Decision || !slices.Equal(a.Patterns, b.Patterns) {
			return true
		}
	}
	return false
}

const (
	fixPermissionsDoc      = "write permissions.yaml as version: 1 with profile (cautious, balanced, or autonomous) and rules"
	fixPermissionsVersion  = "set version: 1"
	fixPermissionsProfile  = "set profile to cautious, balanced, or autonomous"
	fixPermissionsAction   = "set action to edit, create, delete, run, or code"
	fixPermissionsDecision = "set decision to allow, ask, or deny"
	fixPermissionsPattern  = "provide one or more non-empty slash-separated file globs (such as scenes/**) or run targets (such as game check, playtests, shell)"
	fixPermissionsYAMLRef  = "write values directly without YAML anchors, aliases or merge keys"
)

// ParsePermissions decodes and validates a versioned YAML permissions.yaml document.
// Unknown fields, trailing YAML documents, YAML anchors/aliases/merge keys, duplicate mapping keys, missing or unsupported versions, and invalid profiles, actions, patterns, or decisions are refused.
//
// Errors are *loaderr.Error values carrying the line, column, field path and fix; a caller that read data from a file names it with loaderr.WithFile.
func ParsePermissions(data []byte) (*PermissionsFile, error) {
	f, err := parsePermissions(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixPermissionsDoc)
	}
	return f, nil
}

func parsePermissions(data []byte) (*PermissionsFile, error) {
	if len(data) > maxPermissionsFileBytes {
		return nil, fmt.Errorf("editor: permissions document exceeds %d bytes", maxPermissionsFileBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("editor: empty permissions document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var f PermissionsFile
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("editor: empty permissions document")
		}
		return nil, fmt.Errorf("editor: decode permissions: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("editor: unexpected trailing YAML document in permissions")
		}
		return nil, fmt.Errorf("editor: decode permissions: %w", err)
	}
	if err := ValidatePermissions(&f); err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("editor: decode permissions: %w", err)
	}
	if err := checkPlainPermissionsNode(&root, f.Version); err != nil {
		return nil, err
	}
	if f.Rules == nil {
		f.Rules = []PermissionRule{}
	}
	return &f, nil
}

func checkPlainPermissionsNode(n *yaml.Node, version int) error {
	if err := checkNoPermissionsYAMLReferences(n); err != nil {
		return err
	}
	if err := checkDuplicateYAMLKeys(n); err != nil {
		return fmt.Errorf("editor: permissions: %w", err)
	}
	doc := n
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	hasRules := false
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i].Value
		val := doc.Content[i+1]
		switch key {
		case "version":
			if want := strconv.Itoa(version); val.Value != want {
				return loaderr.Errorf("version", "write the version as "+want, "editor: permissions version %q must be written as %s", val.Value, want)
			}
		case "rules":
			hasRules = true
		}
	}
	if !hasRules {
		return loaderr.Errorf("rules", "include a rules list (use rules: [] for no rules)", "editor: permissions is missing rules")
	}
	return nil
}

func checkNoPermissionsYAMLReferences(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("editor: permissions line %d: YAML aliases are not allowed", n.Line), Fix: fixPermissionsYAMLRef}
	}
	if n.Anchor != "" {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("editor: permissions line %d: YAML anchors are not allowed", n.Line), Fix: fixPermissionsYAMLRef}
	}
	for i, c := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 && c.Tag == "!!merge" {
			return &loaderr.Error{Line: c.Line, Column: c.Column, Problem: fmt.Sprintf("editor: permissions line %d: YAML merge keys are not allowed", c.Line), Fix: fixPermissionsYAMLRef}
		}
		if err := checkNoPermissionsYAMLReferences(c); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePermissions checks f against the version 1 permissions schema.
func ValidatePermissions(f *PermissionsFile) error {
	if f == nil {
		return errors.New("editor: nil permissions")
	}
	switch f.Version {
	case PermissionsVersion:
	case 0:
		return loaderr.Errorf("version", fixPermissionsVersion, "editor: permissions is missing version (want %d)", PermissionsVersion)
	default:
		return loaderr.Errorf("version", fixPermissionsVersion, "editor: unsupported permissions version %d (want %d)", f.Version, PermissionsVersion)
	}
	if !f.Profile.Valid() {
		return loaderr.Errorf("profile", fixPermissionsProfile, "editor: invalid permissions profile %q (want cautious, balanced, or autonomous)", f.Profile)
	}
	if len(f.Rules) > maxPermissionsRules {
		return loaderr.Errorf("rules", fixPermissionsDoc, "editor: permissions has %d rules, exceeding limit (%d)", len(f.Rules), maxPermissionsRules)
	}
	for i, r := range f.Rules {
		if !r.Action.Valid() {
			return loaderr.Errorf(fmt.Sprintf("rules[%d].action", i), fixPermissionsAction, "editor: rule %d has invalid action %q (want edit, create, delete, run, or code)", i+1, r.Action)
		}
		if len(r.Patterns) == 0 {
			return loaderr.Errorf(fmt.Sprintf("rules[%d].patterns", i), fixPermissionsPattern, "editor: rule %d must list at least one pattern", i+1)
		}
		if len(r.Patterns) > maxPermissionsPatternsPerRow {
			return loaderr.Errorf(fmt.Sprintf("rules[%d].patterns", i), fixPermissionsPattern, "editor: rule %d has %d patterns, exceeding limit (%d)", i+1, len(r.Patterns), maxPermissionsPatternsPerRow)
		}
		for j, pat := range r.Patterns {
			if err := validatePermissionPattern(r.Action, pat); err != nil {
				return loaderr.Errorf(fmt.Sprintf("rules[%d].patterns[%d]", i, j), fixPermissionsPattern, "editor: rule %d pattern %q: %v", i+1, pat, err)
			}
		}
		if !r.Decision.Valid() {
			return loaderr.Errorf(fmt.Sprintf("rules[%d].decision", i), fixPermissionsDecision, "editor: rule %d has invalid decision %q (want allow, ask, or deny)", i+1, r.Decision)
		}
	}
	return nil
}

func validatePermissionPattern(action PermissionAction, pattern string) error {
	trimmed := strings.TrimSpace(pattern)
	if trimmed == "" {
		return errors.New("pattern must not be empty")
	}
	if trimmed != pattern {
		return errors.New("pattern must not have leading or trailing whitespace")
	}
	for _, r := range pattern {
		if unicode.IsControl(r) {
			return errors.New("pattern must not contain control characters")
		}
	}
	if action != PermissionActionRun {
		if strings.ContainsRune(pattern, '\\') {
			return errors.New("use forward slashes '/' in file patterns, not backslashes")
		}
		if strings.HasPrefix(pattern, "/") {
			return errors.New("file pattern must be relative to the project root")
		}
		for _, seg := range strings.Split(pattern, "/") {
			if seg == ".." {
				return errors.New("file pattern must not contain '..'")
			}
			if seg != "**" {
				if _, err := path.Match(seg, "a"); err != nil {
					return fmt.Errorf("invalid glob syntax: %w", err)
				}
			}
		}
	} else {
		if _, err := path.Match(pattern, "a"); err != nil {
			return fmt.Errorf("invalid pattern syntax: %w", err)
		}
	}
	return nil
}

// EncodePermissions validates f and writes deterministic 2-space YAML prefixed with PermissionsSchemaComment.
func EncodePermissions(f *PermissionsFile) ([]byte, error) {
	if err := ValidatePermissions(f); err != nil {
		return nil, err
	}
	out := f.Clone()
	if out.Rules == nil {
		out.Rules = []PermissionRule{}
	}
	var buf bytes.Buffer
	buf.WriteString(PermissionsSchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&out); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("editor: encode permissions: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("editor: encode permissions: %w", err)
	}
	return buf.Bytes(), nil
}

// PermissionsFilePath returns the canonical path to permissions.yaml beside game.yaml in projectDir.
func PermissionsFilePath(projectDir string) string {
	if strings.TrimSpace(projectDir) == "" {
		return ""
	}
	return filepath.Join(projectDir, PermissionsFileName)
}

// LoadPermissions reads and parses permissions.yaml from path.
func LoadPermissions(filePath string) (*PermissionsFile, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxPermissionsFileBytes {
		return nil, loaderr.WithFile(fmt.Errorf("editor: permissions file exceeds %d bytes", maxPermissionsFileBytes), filePath)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	f, err := ParsePermissions(data)
	if err != nil {
		return nil, loaderr.WithFile(err, filePath)
	}
	return f, nil
}

// SavePermissions validates and atomically writes f as deterministic YAML to path, creating parent directories as needed.
func SavePermissions(path string, f *PermissionsFile) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("editor: permissions path is empty")
	}
	data, err := EncodePermissions(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("editor: create permissions directory: %w", err)
	}
	return writeFileAtomic(path, data)
}

// PermissionRequest describes an MCP tool call to evaluate against a PermissionsFile.
type PermissionRequest struct {
	// Tool is the MCP tool name (for example "scene_edit", "game_playtest", or "shell_run").
	Tool string
	// Action is the mutating or running operation category.
	Action PermissionAction
	// Target is a slash-separated project-relative file path (such as "scenes/level_01.scene.yaml") or a run target (such as "game check", "playtests", or "shell: go generate ./assets").
	Target string
	// Summary optionally provides a short human-readable description of the call for UI cards and logs.
	Summary string
}

// PermissionEvaluation is the result of evaluating a PermissionRequest against a PermissionsFile.
// Every decision identifies which rule index or base profile produced it.
type PermissionEvaluation struct {
	// Decision is PermissionAllow, PermissionAsk, or PermissionDeny.
	Decision PermissionDecision `json:"decision"`
	// Profile is the active base profile name.
	Profile PermissionProfile `json:"profile"`
	// Modified reports whether the active rule table differs from PermissionPreset(Profile).
	Modified bool `json:"modified,omitempty"`
	// RuleIndex is the 0-based index of the first matching rule, or -1 when no rule matched and the profile default decided.
	RuleIndex int `json:"ruleIndex"`
	// RuleNumber is the 1-based rule number (RuleIndex + 1), or 0 when no rule matched.
	RuleNumber int `json:"ruleNumber,omitempty"`
	// RuleTotal is the total number of rules in the table.
	RuleTotal int `json:"ruleTotal,omitempty"`
	// Rule is a copy of the matched rule, or nil when no rule matched.
	Rule *PermissionRule `json:"rule,omitempty"`
	// Reason is a clear, readable explanation naming the rule and/or profile that caused Decision.
	Reason string `json:"reason"`
	// Link is the Settings navigation label for this decision, such as "Settings › Permissions · rule 7 of 7".
	Link string `json:"link"`
	// HeldByDisk reports that an external permissions.yaml change tried to loosen this rule or profile,
	// and the editor is holding the stricter decision until the person confirms it in Settings › Permissions.
	HeldByDisk bool `json:"heldByDisk,omitempty"`
	// Tool, Action, and Target echo the evaluated request.
	Tool   string           `json:"tool,omitempty"`
	Action PermissionAction `json:"action"`
	Target string           `json:"target,omitempty"`
}

// Evaluate checks req against f's rules from top to bottom (first match wins).
// When no rule matches, the active profile's default decision applies.
// Unrecognized actions or escaping file paths fail closed (PermissionDeny).
func (f PermissionsFile) Evaluate(req PermissionRequest) PermissionEvaluation {
	profile := f.Profile
	if !profile.Valid() {
		profile = PermissionProfileCautious
	}
	modified := f.IsModified()
	total := len(f.Rules)

	if !req.Action.Valid() {
		return PermissionEvaluation{
			Decision:  PermissionDeny,
			Profile:   profile,
			Modified:  modified,
			RuleIndex: -1,
			RuleTotal: total,
			Reason:    fmt.Sprintf("Denied: unrecognized permission action %q.", req.Action),
			Link:      "Settings › Permissions",
			Tool:      req.Tool,
			Action:    req.Action,
			Target:    strings.TrimSpace(req.Target),
		}
	}

	target := ""
	if req.Action == PermissionActionRun {
		target = strings.TrimSpace(req.Target)
		if hasControlRune(target) {
			return PermissionEvaluation{
				Decision:  PermissionDeny,
				Profile:   profile,
				Modified:  modified,
				RuleIndex: -1,
				RuleTotal: total,
				Reason:    "Denied: run target contains control characters.",
				Link:      "Settings › Permissions",
				Tool:      req.Tool,
				Action:    req.Action,
				Target:    target,
			}
		}
	} else {
		var ok bool
		target, ok = cleanPermissionFileTarget(req.Target)
		if !ok {
			return PermissionEvaluation{
				Decision:  PermissionDeny,
				Profile:   profile,
				Modified:  modified,
				RuleIndex: -1,
				RuleTotal: total,
				Reason:    fmt.Sprintf("Denied: target path %q is invalid or escapes the project root.", req.Target),
				Link:      "Settings › Permissions",
				Tool:      req.Tool,
				Action:    req.Action,
				Target:    strings.TrimSpace(req.Target),
			}
		}
	}

	for i, rule := range f.Rules {
		if !actionMatchesRule(req.Action, rule.Action, target) {
			continue
		}
		for _, pat := range rule.Patterns {
			if MatchPermissionPattern(rule.Action, pat, target) {
				matched := rule.Clone()
				link := fmt.Sprintf("Settings › Permissions · rule %d of %d", i+1, total)
				reason := formatMatchedPermissionReason(rule, i+1, total, profile, modified)
				return PermissionEvaluation{
					Decision:   rule.Decision,
					Profile:    profile,
					Modified:   modified,
					RuleIndex:  i,
					RuleNumber: i + 1,
					RuleTotal:  total,
					Rule:       &matched,
					Reason:     reason,
					Link:       link,
					Tool:       req.Tool,
					Action:     req.Action,
					Target:     target,
				}
			}
		}
	}

	decision := defaultProfileDecision(profile, req.Action, target)
	reason := formatProfileDefaultReason(decision, profile, req.Action, target)
	return PermissionEvaluation{
		Decision:  decision,
		Profile:   profile,
		Modified:  modified,
		RuleIndex: -1,
		RuleTotal: total,
		Reason:    reason,
		Link:      "Settings › Permissions",
		Tool:      req.Tool,
		Action:    req.Action,
		Target:    target,
	}
}

func actionMatchesRule(reqAction, ruleAction PermissionAction, target string) bool {
	if reqAction == ruleAction {
		return true
	}
	// Go code edits also match Edit rules whose patterns cover the Go file (such as "**/*.go" or "**"),
	// and Edit calls targeting a ".go" file also match Code rules (such as "**/*.go").
	if reqAction == PermissionActionCode && ruleAction == PermissionActionEdit {
		return true
	}
	if reqAction == PermissionActionEdit && ruleAction == PermissionActionCode && isGoSourceTarget(target) {
		return true
	}
	return false
}

func isGoSourceTarget(target string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(target)), ".go")
}

func defaultProfileDecision(profile PermissionProfile, action PermissionAction, target string) PermissionDecision {
	if !action.Valid() {
		return PermissionDeny
	}
	if action == PermissionActionDelete {
		return PermissionDeny
	}
	if action == PermissionActionRun && MatchPermissionPattern(PermissionActionRun, PermissionRunShell, target) {
		return PermissionDeny
	}
	if profile == PermissionProfileAutonomous {
		return PermissionAllow
	}
	return PermissionAsk
}

func formatMatchedPermissionReason(rule PermissionRule, num, total int, profile PermissionProfile, modified bool) string {
	profileLabel := profile.DisplayName()
	if modified {
		profileLabel += " (changed)"
	}
	switch rule.Decision {
	case PermissionDeny:
		return fmt.Sprintf("Denied by rule %d of %d (%s) in %s profile — Settings › Permissions · rule %d of %d", num, total, rule.Summary(), profileLabel, num, total)
	case PermissionAsk:
		if !modified {
			return fmt.Sprintf("Asked because the %s profile sets %s · %s to Ask (rule %d of %d).", profile.DisplayName(), rule.Action.DisplayName(), FormatPermissionPatterns(rule.Action, rule.Patterns), num, total)
		}
		return fmt.Sprintf("Asked by rule %d of %d (%s) in %s profile.", num, total, rule.Summary(), profileLabel)
	default:
		return fmt.Sprintf("Allowed by rule %d of %d (%s) in %s profile.", num, total, rule.Summary(), profileLabel)
	}
}

func formatProfileDefaultReason(decision PermissionDecision, profile PermissionProfile, action PermissionAction, target string) string {
	desc := action.DisplayName()
	if target != "" {
		desc += " · " + target
	}
	switch decision {
	case PermissionDeny:
		return fmt.Sprintf("Denied by the %s profile default for %s (no matching rule).", profile.DisplayName(), desc)
	case PermissionAsk:
		return fmt.Sprintf("Asked because no rule matched %s under the %s profile.", desc, profile.DisplayName())
	default:
		return fmt.Sprintf("Allowed by the %s profile default for %s.", profile.DisplayName(), desc)
	}
}

func hasControlRune(s string) bool {
	for _, r := range s {
		if r == 0 || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// cleanPermissionFileTarget normalizes a slash-separated project-relative file path for permission matching.
// It returns ("", true) for an empty target (or "."), and ("", false) for any path containing NUL/control characters,
// an absolute prefix, a Windows drive prefix, or ".." traversal escaping the project root.
func cleanPermissionFileTarget(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", true
	}
	if hasControlRune(s) {
		return "", false
	}
	s = strings.ReplaceAll(s, `\`, "/")
	if strings.HasPrefix(s, "/") {
		return "", false
	}
	// Reject Windows drive-letter prefixes such as "C:/foo" or "C:foo".
	if len(s) >= 2 && s[1] == ':' && ((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')) {
		return "", false
	}
	cleaned := path.Clean(s)
	if cleaned == "." {
		return "", true
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", false
	}
	return cleaned, true
}

// ResolvePermissionFileTarget canonicalizes raw against projectRoot, refusing control characters, absolute paths,
// ".." escapes, and symlinks pointing outside projectRoot. When a symlink inside projectRoot resolves to another
// location inside projectRoot (for example "scenes/link.scene.yaml" -> "../game.yaml"), the real project-relative
// target ("game.yaml") is returned so permission rules match the actual file being touched.
func ResolvePermissionFileTarget(projectRoot, raw string) (string, error) {
	cleaned, ok := cleanPermissionFileTarget(raw)
	if !ok {
		return "", fmt.Errorf("editor: target path %q leaves the project root", raw)
	}
	if cleaned == "" || strings.TrimSpace(projectRoot) == "" {
		return cleaned, nil
	}
	realRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		realRoot, err = filepath.Abs(projectRoot)
		if err != nil {
			return "", fmt.Errorf("editor: resolve project root: %w", err)
		}
	}
	absCandidate := filepath.Join(realRoot, filepath.FromSlash(cleaned))
	// Walk upward to the deepest existing ancestor so non-existent targets (such as Create) still resolve parent symlinks.
	cur := absCandidate
	var tail []string
	var resolvedBase string
	for {
		real, rerr := filepath.EvalSymlinks(cur)
		if rerr == nil {
			resolvedBase = real
			break
		}
		if !errors.Is(rerr, fs.ErrNotExist) {
			return "", fmt.Errorf("editor: resolve target %q: %w", raw, rerr)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("editor: target path %q escapes the project root", raw)
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
	slices.Reverse(tail)
	resolved := resolvedBase
	if len(tail) > 0 {
		resolved = filepath.Join(append([]string{resolvedBase}, tail...)...)
	}
	rel, err := filepath.Rel(realRoot, resolved)
	if err != nil {
		return "", fmt.Errorf("editor: target path %q escapes the project root", raw)
	}
	relSlash := filepath.ToSlash(rel)
	finalClean, ok := cleanPermissionFileTarget(relSlash)
	if !ok {
		return "", fmt.Errorf("editor: target path %q resolves outside the project root", raw)
	}
	return finalClean, nil
}

// permissionFileCaseInsensitive reports whether file patterns should fold ASCII/Unicode case
// to match the host OS filesystem's case-insensitivity (macOS APFS/HFS+ and Windows NTFS).
func permissionFileCaseInsensitive() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

// MatchPermissionPattern reports whether target matches pattern for action.
// For file actions, "**" matches zero or more path segments (so "**/*.go" matches both "main.go" and "scripts/slime.go", and "scenes/**" matches everything under "scenes/"),
// and empty, absolute, or ".."-escaping targets never match any pattern (including "**" and "*").
// For run actions, matching is case-insensitive and supports target prefixes such as "shell" matching "shell: go generate ./assets", as well as "*" and "**" wildcards.
func MatchPermissionPattern(action PermissionAction, pattern, target string) bool {
	pattern = strings.TrimSpace(pattern)
	target = strings.TrimSpace(target)
	if pattern == "" || target == "" {
		return false
	}
	if action == PermissionActionRun {
		if hasControlRune(target) {
			return false
		}
		if pattern == "**" || pattern == "*" {
			return true
		}
		return matchRunPattern(pattern, target)
	}
	normTarget, ok := cleanPermissionFileTarget(target)
	if !ok || normTarget == "" {
		return false
	}
	if pattern == "**" || pattern == "*" {
		return true
	}
	if permissionFileCaseInsensitive() {
		pattern = strings.ToLower(pattern)
		normTarget = strings.ToLower(normTarget)
	}
	return matchGlobPath(pattern, normTarget)
}

func matchRunPattern(pattern, target string) bool {
	p := normalizeRunToken(pattern)
	t := normalizeRunToken(target)
	if p == "" || t == "" {
		return false
	}
	if p == t {
		return true
	}
	// "shell" matches "shell: go generate ./assets", "playtests" matches "playtests: level_01".
	if strings.HasPrefix(t, p+":") || strings.HasPrefix(t, p+" ") {
		return true
	}
	if ok, err := path.Match(p, t); err == nil && ok {
		return true
	}
	return false
}

func normalizeRunToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "shell commands", "shell command":
		return PermissionRunShell
	case "playtest", "playtests (headless)", "headless playtests":
		return PermissionRunPlaytests
	case "game_check":
		return PermissionRunGameCheck
	}
	return s
}

func matchGlobPath(pattern, target string) bool {
	pSegs := strings.Split(strings.TrimPrefix(pattern, "./"), "/")
	tSegs := strings.Split(target, "/")
	return matchGlobSegments(pSegs, tSegs)
}

// matchGlobSegments matches slash-separated glob segments in O(len(pSegs) * len(tSegs)) time using DP,
// preventing exponential backtracking on patterns with multiple "**" segments.
func matchGlobSegments(pSegs, tSegs []string) bool {
	// Collapse consecutive "**" segments up front.
	collapsed := make([]string, 0, len(pSegs))
	for _, seg := range pSegs {
		if seg == "**" && len(collapsed) > 0 && collapsed[len(collapsed)-1] == "**" {
			continue
		}
		collapsed = append(collapsed, seg)
	}
	m, n := len(collapsed), len(tSegs)
	dp := make([]bool, n+1)
	dp[0] = true
	for i := range m {
		next := make([]bool, n+1)
		seg := collapsed[i]
		if seg == "**" {
			// "**" matches 0 or more target segments: next[j] = dp[j] || next[j-1].
			for j := 0; j <= n; j++ {
				if dp[j] || (j > 0 && next[j-1]) {
					next[j] = true
				}
			}
		} else {
			for j := 1; j <= n; j++ {
				if !dp[j-1] {
					continue
				}
				if ok, err := path.Match(seg, tSegs[j-1]); err == nil && ok {
					next[j] = true
				}
			}
		}
		dp = next
	}
	return dp[n]
}

// BlockedPermissionStep records an MCP tool call stopped by a Deny rule so the editor can show the blocked step and link directly to its rule in Settings › Permissions.
type BlockedPermissionStep struct {
	ID         string               `json:"id"`
	Summary    string               `json:"summary"`
	Command    string               `json:"command,omitempty"`
	Evaluation PermissionEvaluation `json:"evaluation"`
}

// PermissionResponse is the person's answer to a pending Ask permission request.
type PermissionResponse int

const (
	// PermissionResponseAllowOnce allows this one request without modifying permissions.yaml.
	PermissionResponseAllowOnce PermissionResponse = iota + 1
	// PermissionResponseAlways adds an Allow rule to permissions.yaml, persists it, and shows the "Always permission rule saved" toast with Show and Undo.
	PermissionResponseAlways
	// PermissionResponseDeny refuses the request without modifying permissions.yaml.
	PermissionResponseDeny
)

// PendingPermissionRequest represents an MCP tool call paused on an Ask rule awaiting the person's answer.
type PendingPermissionRequest struct {
	ID         string               `json:"id"`
	Summary    string               `json:"summary"`
	Evaluation PermissionEvaluation `json:"evaluation"`
	Status     string               `json:"status"` // "pending", "allowed_once", "allowed_always", "denied"
	onResolve  func(PermissionResponse)
}

// PermissionToast holds the active "Always permission rule saved" confirmation toast state, with enough state to Show the saved rule or Undo it byte-for-byte.
type PermissionToast struct {
	Message   string
	RuleIndex int
	Rule      PermissionRule
	previous  PermissionsFile
	hadFile   bool
}

type permissionsState struct {
	file          PermissionsFile
	initialized   bool
	path          string
	loadErr       error
	diskFile      *PermissionsFile
	diskSnap      diskSnapshot
	hasFileOnDisk bool
	held          []HeldPermissionLoosening
	draft         *PermissionsDraft
	draftSeq      uint64
	toolEnabled   map[string]bool
	focusedRule   int
	focusSeq      uint64
	focusPending  bool
	nextID        int
	blocked       []BlockedPermissionStep
	pending       []PendingPermissionRequest
	toast         *PermissionToast
	rulesRev      uint64
	rev           uint64
	searchQuery   string
	activeSection string // "permissions" (default)
}

func (e *Editor) ensurePermissionsInit() {
	if e == nil {
		return
	}
	if !e.perms.initialized {
		e.perms.file = DefaultPermissions()
		e.perms.focusedRule = -1
		e.perms.activeSection = "permissions"
		e.perms.toolEnabled = defaultToolEnabledMap()
		e.perms.initialized = true
	} else if e.perms.toolEnabled == nil {
		e.perms.toolEnabled = defaultToolEnabledMap()
	}
}

func (e *Editor) syncBuildReleasePermissionProfile() {
	if e == nil {
		return
	}
	p := e.perms.file.Profile
	if !p.Valid() {
		p = PermissionProfileBalanced
	}
	e.buildRelease.mu.Lock()
	if e.buildRelease.permissionProfile != p {
		e.buildRelease.permissionProfile = p
		e.buildRelease.rev++
	}
	e.buildRelease.mu.Unlock()
}

func (e *Editor) bumpPermissionsRulesRev() {
	e.syncBuildReleasePermissionProfile()
	e.perms.rulesRev++
	e.perms.rev++
	e.notify()
}

func (e *Editor) bumpPermissionsRev() {
	e.perms.rev++
	e.notify()
}

func (e *Editor) permissionsRulesRev() uint64 {
	if e == nil {
		return 0
	}
	return e.perms.rulesRev
}

func (e *Editor) permissionsStateRev() uint64 {
	if e == nil {
		return 0
	}
	return e.perms.rev
}

// Permissions returns a copy of the editor's active project permission profile and rules (defaulting to the Balanced preset).
func (e *Editor) Permissions() PermissionsFile {
	if e == nil {
		return DefaultPermissions()
	}
	e.ensurePermissionsInit()
	return e.perms.file.Clone()
}

// PermissionsPath returns the path of permissions.yaml for the open project (or an explicit path loaded via LoadProjectPermissions), or "" when no project is open.
func (e *Editor) PermissionsPath() string {
	if e == nil {
		return ""
	}
	if e.perms.path != "" {
		return e.perms.path
	}
	if e.ProjectPath != "" {
		return PermissionsFilePath(filepath.Dir(e.ProjectPath))
	}
	return ""
}

// PermissionsErr returns the parse/load error if the project's permissions.yaml was invalid when opened.
func (e *Editor) PermissionsErr() error {
	if e == nil {
		return nil
	}
	return e.perms.loadErr
}

// PermissionsSettingsOpen reports whether the Settings › Permissions page is open.
func (e *Editor) PermissionsSettingsOpen() bool {
	return e != nil && e.settingsOpen && e.settingsPage == SettingsPagePermissions
}

// OpenPermissionsSettings opens Settings › Permissions and optionally focuses rule index focusRule (0-based; pass -1 to keep or clear rule focus).
func (e *Editor) OpenPermissionsSettings(focusRule int) {
	if e == nil || e.playing {
		return
	}
	e.ensurePermissionsInit()
	e.perms.activeSection = "permissions"
	if focusRule >= 0 && focusRule < len(e.perms.file.Rules) {
		e.perms.focusedRule = focusRule
		e.perms.focusSeq++
		e.perms.focusPending = true
	} else if focusRule < 0 {
		e.perms.focusedRule = -1
	}
	e.perms.rulesRev++
	e.perms.rev++
	wasOpen := e.PermissionsSettingsOpen()
	e.OpenSettingsPage(SettingsPagePermissions)
	if wasOpen {
		e.notify()
	}
}

// ClosePermissionsSettings closes Settings › Permissions.
func (e *Editor) ClosePermissionsSettings() {
	if e == nil || !e.PermissionsSettingsOpen() {
		return
	}
	e.perms.rev++
	e.CloseSettings()
}

// SetPermissionsSettingsOpen opens or closes Settings › Permissions.
func (e *Editor) SetPermissionsSettingsOpen(open bool) {
	if open {
		e.OpenPermissionsSettings(-1)
	} else {
		e.ClosePermissionsSettings()
	}
}

// PermissionsFocusedRule returns the 0-based index of the currently focused rule in Settings › Permissions, or -1 if none.
func (e *Editor) PermissionsFocusedRule() int {
	if e == nil {
		return -1
	}
	e.ensurePermissionsInit()
	if e.perms.focusedRule >= len(e.perms.file.Rules) {
		return -1
	}
	return e.perms.focusedRule
}

// FocusPermissionRule moves focus to the 0-based rule index in Settings › Permissions.
func (e *Editor) FocusPermissionRule(index int) {
	if e == nil {
		return
	}
	e.ensurePermissionsInit()
	if index < 0 || index >= len(e.perms.file.Rules) {
		e.perms.focusedRule = -1
	} else {
		e.perms.focusedRule = index
		e.perms.focusSeq++
		e.perms.focusPending = true
	}
	e.bumpPermissionsRulesRev()
}

// SetPermissions validates, persists permissions.yaml when a project path is available, and atomically applies f to the editor.
// If writing permissions.yaml fails, in-memory rules remain unchanged so disk and memory never diverge.
func (e *Editor) SetPermissions(f PermissionsFile) error {
	if e == nil {
		return nil
	}
	e.ensurePermissionsInit()
	if f.Version == 0 {
		f.Version = PermissionsVersion
	}
	if err := ValidatePermissions(&f); err != nil {
		return err
	}
	next := f.Clone()
	if p := e.PermissionsPath(); p != "" {
		encoded, err := EncodePermissions(&next)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return fmt.Errorf("editor: create permissions directory: %w", err)
		}
		if err := writeFileAtomic(p, encoded); err != nil {
			return err
		}
		e.perms.hasFileOnDisk = true
		e.perms.diskSnap = makeDiskSnapshot(p, encoded)
		cp := next.Clone()
		e.perms.diskFile = &cp
	}
	e.perms.file = next
	e.perms.loadErr = nil
	if e.perms.focusedRule >= len(e.perms.file.Rules) {
		e.perms.focusedRule = len(e.perms.file.Rules) - 1
	}
	e.syncDiskWatchTargets()
	e.bumpPermissionsRulesRev()
	return nil
}

// SelectPermissionProfile switches to profile and fills the rule table with PermissionPreset(profile), persisting permissions.yaml when a project is open.
func (e *Editor) SelectPermissionProfile(profile PermissionProfile) error {
	if e == nil {
		return nil
	}
	if !profile.Valid() {
		return loaderr.Errorf("profile", fixPermissionsProfile, "editor: invalid permissions profile %q", profile)
	}
	return e.SetPermissions(PermissionPreset(profile))
}

// ResetPermissionsToPreset resets the current profile's rules back to PermissionPreset(currentProfile).
func (e *Editor) ResetPermissionsToPreset() error {
	if e == nil {
		return nil
	}
	e.ensurePermissionsInit()
	return e.SelectPermissionProfile(e.perms.file.Profile)
}

// SetPermissionRuleDecision updates the decision (allow, ask, deny) of rule index, keeping the active profile name (so IsModified marks it as changed when it differs from the preset), and persists permissions.yaml.
func (e *Editor) SetPermissionRuleDecision(index int, decision PermissionDecision) error {
	if e == nil {
		return nil
	}
	e.ensurePermissionsInit()
	if index < 0 || index >= len(e.perms.file.Rules) {
		return fmt.Errorf("editor: permission rule index %d out of range (0..%d)", index, len(e.perms.file.Rules)-1)
	}
	if !decision.Valid() {
		return loaderr.Errorf("decision", fixPermissionsDecision, "editor: invalid permission decision %q", decision)
	}
	next := e.perms.file.Clone()
	next.Rules[index].Decision = decision
	return e.SetPermissions(next)
}

// SetPermissionRuleAction updates the action of rule index and persists permissions.yaml.
func (e *Editor) SetPermissionRuleAction(index int, action PermissionAction) error {
	if e == nil {
		return nil
	}
	e.ensurePermissionsInit()
	if index < 0 || index >= len(e.perms.file.Rules) {
		return fmt.Errorf("editor: permission rule index %d out of range", index)
	}
	if !action.Valid() {
		return loaderr.Errorf("action", fixPermissionsAction, "editor: invalid permission action %q", action)
	}
	next := e.perms.file.Clone()
	next.Rules[index].Action = action
	// Keep patterns valid for the new action.
	if action == PermissionActionRun && len(next.Rules[index].Patterns) == 1 && next.Rules[index].Patterns[0] == "**" {
		next.Rules[index].Patterns = []string{PermissionRunGameCheck}
	} else if action == PermissionActionCode && len(next.Rules[index].Patterns) == 1 && next.Rules[index].Patterns[0] == "**" {
		next.Rules[index].Patterns = []string{"**/*.go"}
	}
	return e.SetPermissions(next)
}

// SetPermissionRulePatterns updates the file globs / run targets of rule index and persists permissions.yaml.
func (e *Editor) SetPermissionRulePatterns(index int, patterns []string) error {
	if e == nil {
		return nil
	}
	e.ensurePermissionsInit()
	if index < 0 || index >= len(e.perms.file.Rules) {
		return fmt.Errorf("editor: permission rule index %d out of range", index)
	}
	cleaned := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if s := strings.TrimSpace(p); s != "" {
			cleaned = append(cleaned, s)
		}
	}
	next := e.perms.file.Clone()
	next.Rules[index].Patterns = cleaned
	return e.SetPermissions(next)
}

// ParsePatternList splits a comma-separated (or whitespace-separated for file globs) user input into trimmed patterns.
func ParsePatternList(action PermissionAction, raw string) []string {
	var parts []string
	if strings.ContainsRune(raw, ',') {
		parts = strings.Split(raw, ",")
	} else if action == PermissionActionRun {
		if strings.ContainsRune(raw, '·') {
			parts = strings.Split(raw, "·")
		} else {
			parts = []string{raw}
		}
	} else {
		parts = strings.Fields(raw)
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			if action == PermissionActionRun && strings.EqualFold(s, "shell commands") {
				s = PermissionRunShell
			}
			out = append(out, s)
		}
	}
	return out
}

// AddPermissionRule appends rule to the table, focuses the new row, and persists permissions.yaml.
func (e *Editor) AddPermissionRule(rule PermissionRule) error {
	if e == nil {
		return nil
	}
	e.ensurePermissionsInit()
	next := e.perms.file.Clone()
	next.Rules = append(next.Rules, rule.Clone())
	if err := e.SetPermissions(next); err != nil {
		return err
	}
	e.FocusPermissionRule(len(e.perms.file.Rules) - 1)
	return nil
}

// RemovePermissionRule removes rule at index and persists permissions.yaml.
func (e *Editor) RemovePermissionRule(index int) error {
	if e == nil {
		return nil
	}
	e.ensurePermissionsInit()
	if index < 0 || index >= len(e.perms.file.Rules) {
		return fmt.Errorf("editor: permission rule index %d out of range", index)
	}
	next := e.perms.file.Clone()
	next.Rules = slices.Delete(next.Rules, index, index+1)
	if err := e.SetPermissions(next); err != nil {
		return err
	}
	if len(e.perms.file.Rules) == 0 {
		e.perms.focusedRule = -1
	} else if e.perms.focusedRule >= len(e.perms.file.Rules) {
		e.perms.focusedRule = len(e.perms.file.Rules) - 1
	}
	e.bumpPermissionsRulesRev()
	return nil
}

// MovePermissionRule moves rule at index by delta (-1 up, +1 down), keeps focus on the moved rule, and persists permissions.yaml.
func (e *Editor) MovePermissionRule(index, delta int) error {
	if e == nil {
		return nil
	}
	e.ensurePermissionsInit()
	dst := index + delta
	if index < 0 || index >= len(e.perms.file.Rules) || dst < 0 || dst >= len(e.perms.file.Rules) {
		return nil
	}
	next := e.perms.file.Clone()
	next.Rules[index], next.Rules[dst] = next.Rules[dst], next.Rules[index]
	if err := e.SetPermissions(next); err != nil {
		return err
	}
	e.FocusPermissionRule(dst)
	return nil
}

// LoadProjectPermissions loads permissions.yaml from path (falling back to DefaultPermissions when the file does not exist yet).
// When permissions.yaml exists but fails to parse or validate, the editor fails closed: it records PermissionsErr(),
// loads PermissionProfileCautious into memory, and EvaluatePermission denies all mutating/running calls until fixed.
func (e *Editor) LoadProjectPermissions(path string) error {
	if e == nil {
		return nil
	}
	e.ensurePermissionsInit()
	e.perms.path = path
	e.perms.loadErr = nil
	e.perms.held = nil
	if strings.TrimSpace(path) == "" {
		e.perms.file = DefaultPermissions()
		e.perms.hasFileOnDisk = false
		e.perms.diskSnap = diskSnapshot{}
		e.perms.diskFile = nil
		e.syncDiskWatchTargets()
		e.bumpPermissionsRulesRev()
		return nil
	}
	f, err := LoadPermissions(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			e.perms.file = DefaultPermissions()
			e.perms.hasFileOnDisk = false
			e.perms.diskSnap = diskSnapshot{path: filepath.Clean(path), exists: false}
			e.perms.diskFile = nil
			e.syncDiskWatchTargets()
			e.bumpPermissionsRulesRev()
			return nil
		}
		if raw, rerr := readBoundedPermissionsFile(path); rerr == nil {
			e.perms.hasFileOnDisk = true
			e.perms.diskSnap = makeDiskSnapshot(path, raw)
		}
		e.perms.loadErr = err
		e.perms.file = PermissionPreset(PermissionProfileCautious)
		e.syncDiskWatchTargets()
		e.bumpPermissionsRulesRev()
		return err
	}
	if raw, rerr := readBoundedPermissionsFile(path); rerr == nil {
		e.perms.hasFileOnDisk = true
		e.perms.diskSnap = makeDiskSnapshot(path, raw)
	}
	cp := f.Clone()
	e.perms.diskFile = &cp
	e.perms.file = f.Clone()
	e.syncDiskWatchTargets()
	e.bumpPermissionsRulesRev()
	return nil
}

// SaveProjectPermissions writes the editor's active permission rules to PermissionsPath().
func (e *Editor) SaveProjectPermissions() error {
	if e == nil {
		return nil
	}
	e.ensurePermissionsInit()
	p := e.PermissionsPath()
	if p == "" {
		return ErrNoProject
	}
	return SavePermissions(p, &e.perms.file)
}

func (e *Editor) resetPermissionsForProject() {
	if e == nil {
		return
	}
	e.perms.blocked = nil
	e.perms.pending = nil
	e.perms.toast = nil
	e.perms.draft = nil
	e.perms.held = nil
	e.perms.diskFile = nil
	e.perms.diskSnap = diskSnapshot{}
	e.perms.hasFileOnDisk = false
	e.perms.toolEnabled = defaultToolEnabledMap()
	e.perms.focusedRule = -1
	e.perms.loadErr = nil
	if e.ProjectPath != "" {
		e.loadProjectPermissionsOnOpen(filepath.Dir(e.ProjectPath))
	} else {
		e.perms.path = ""
		e.perms.file = DefaultPermissions()
		e.perms.initialized = true
		e.syncBuildReleasePermissionProfile()
		e.perms.rulesRev++
		e.perms.rev++
	}
}

// EvaluatePermission evaluates req against the editor's current permission rules (including held external loosenings).
// If the project's permissions.yaml failed to load, EvaluatePermission fails closed with PermissionDeny.
func (e *Editor) EvaluatePermission(req PermissionRequest) PermissionEvaluation {
	if e != nil && e.perms.loadErr != nil {
		profile := e.perms.file.Profile
		if !profile.Valid() {
			profile = PermissionProfileCautious
		}
		return PermissionEvaluation{
			Decision:  PermissionDeny,
			Profile:   profile,
			RuleIndex: -1,
			RuleTotal: len(e.perms.file.Rules),
			Reason:    fmt.Sprintf("Denied because %s is invalid and could not be loaded: %v", PermissionsFileName, e.perms.loadErr),
			Link:      "Settings › Permissions",
			Tool:      req.Tool,
			Action:    req.Action,
			Target:    strings.TrimSpace(req.Target),
		}
	}
	eval := e.Permissions().Evaluate(req)
	if e != nil && len(e.perms.held) > 0 {
		e.annotateHeldEvaluation(&eval)
	}
	return eval
}

// RecordBlockedPermission records a step blocked by a Deny rule so the UI can show it and link directly to its rule in Settings › Permissions.
func (e *Editor) RecordBlockedPermission(eval PermissionEvaluation, summary string) BlockedPermissionStep {
	if e == nil {
		return BlockedPermissionStep{}
	}
	e.ensurePermissionsInit()
	e.perms.nextID++
	if strings.TrimSpace(summary) == "" {
		summary = eval.Action.DisplayName()
		if eval.Target != "" {
			summary += " " + eval.Target
		}
	}
	cmd := ""
	if eval.Action == PermissionActionRun {
		if after, ok := strings.CutPrefix(eval.Target, PermissionRunShell+":"); ok {
			cmd = strings.TrimSpace(after)
		}
	}
	step := BlockedPermissionStep{
		ID:         fmt.Sprintf("blocked-%d", e.perms.nextID),
		Summary:    summary,
		Command:    cmd,
		Evaluation: eval,
	}
	e.perms.blocked = append(e.perms.blocked, step)
	// Keep the most recent 16 blocked steps.
	if len(e.perms.blocked) > 16 {
		e.perms.blocked = e.perms.blocked[len(e.perms.blocked)-16:]
	}
	e.bumpPermissionsRev()
	return step
}

// BlockedPermissions returns the recorded blocked permission steps in chronological order.
func (e *Editor) BlockedPermissions() []BlockedPermissionStep {
	if e == nil {
		return nil
	}
	return slices.Clone(e.perms.blocked)
}

// DismissBlockedPermission removes the blocked step with id from the editor's list.
func (e *Editor) DismissBlockedPermission(id string) {
	if e == nil {
		return
	}
	for i, b := range e.perms.blocked {
		if b.ID == id {
			e.perms.blocked = slices.Delete(e.perms.blocked, i, i+1)
			e.bumpPermissionsRev()
			return
		}
	}
}

// OpenBlockedPermissionRule opens Settings › Permissions with the matching rule for blocked step id focused.
func (e *Editor) OpenBlockedPermissionRule(id string) bool {
	if e == nil {
		return false
	}
	for _, b := range e.perms.blocked {
		if b.ID == id {
			e.OpenPermissionsSettings(b.Evaluation.RuleIndex)
			return true
		}
	}
	return false
}

// EnqueuePermissionRequest adds a pending Ask permission request to the editor so the person can answer Allow once, Always, or Deny.
func (e *Editor) EnqueuePermissionRequest(eval PermissionEvaluation, summary string, onResolve func(PermissionResponse)) PendingPermissionRequest {
	if e == nil {
		return PendingPermissionRequest{}
	}
	e.ensurePermissionsInit()
	e.perms.nextID++
	if strings.TrimSpace(summary) == "" {
		summary = eval.Action.DisplayName()
		if eval.Target != "" {
			summary += " " + eval.Target
		}
	}
	req := PendingPermissionRequest{
		ID:         fmt.Sprintf("perm-ask-%d", e.perms.nextID),
		Summary:    summary,
		Evaluation: eval,
		Status:     "pending",
		onResolve:  onResolve,
	}
	e.perms.pending = append(e.perms.pending, req)
	// Prune already-resolved requests and cap total queue length to prevent unbounded growth.
	if len(e.perms.pending) > maxPendingPermissionRequests {
		active := make([]PendingPermissionRequest, 0, len(e.perms.pending))
		for _, r := range e.perms.pending {
			if r.Status == "pending" {
				active = append(active, r)
			}
		}
		if len(active) > maxPendingPermissionRequests {
			active = active[len(active)-maxPendingPermissionRequests:]
		}
		e.perms.pending = active
	}
	e.bumpPermissionsRev()
	return req
}

// PendingPermissionRequests returns all currently pending Ask permission requests.
func (e *Editor) PendingPermissionRequests() []PendingPermissionRequest {
	if e == nil {
		return nil
	}
	var out []PendingPermissionRequest
	for _, r := range e.perms.pending {
		if r.Status == "pending" {
			cp := r
			cp.onResolve = nil
			out = append(out, cp)
		}
	}
	return out
}

// RespondPermissionRequest resolves the pending Ask permission request id with resp.
// When resp is PermissionResponseAlways, it also inserts and persists the new Allow rule and opens the "Always permission rule saved" toast with Show and Undo.
func (e *Editor) RespondPermissionRequest(id string, resp PermissionResponse) error {
	if e == nil {
		return nil
	}
	for i := range e.perms.pending {
		if e.perms.pending[i].ID != id {
			continue
		}
		if e.perms.pending[i].Status != "pending" {
			return fmt.Errorf("editor: permission request %q is already %s", id, e.perms.pending[i].Status)
		}
		eval := e.perms.pending[i].Evaluation
		switch resp {
		case PermissionResponseAllowOnce:
			e.perms.pending[i].Status = "allowed_once"
		case PermissionResponseAlways:
			if _, err := e.ApproveAlwaysPermission(eval); err != nil {
				return err
			}
			e.perms.pending[i].Status = "allowed_always"
		case PermissionResponseDeny:
			e.perms.pending[i].Status = "denied"
		default:
			return fmt.Errorf("editor: invalid permission response %d", resp)
		}
		cb := e.perms.pending[i].onResolve
		e.perms.pending[i].onResolve = nil
		e.bumpPermissionsRev()
		if cb != nil {
			cb(resp)
		}
		return nil
	}
	return fmt.Errorf("editor: unknown permission request %q", id)
}

// escapePermissionGlobLiteral escapes glob metacharacters ('*', '?', '[') into single-character bracket classes
// so a literal file path containing glob characters never becomes a wildcard Allow rule and stays valid without backslashes.
func escapePermissionGlobLiteral(s string) string {
	if !strings.ContainsAny(s, "*?[") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '*':
			b.WriteString("[*]")
		case '?':
			b.WriteString("[?]")
		case '[':
			b.WriteString("[[]")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// AlwaysRuleForEvaluation constructs the specific Allow rule added when a person chooses "Always" on eval.
// Returns a rule with empty Patterns when eval lacks a safe, concrete target (callers must refuse to persist it).
func AlwaysRuleForEvaluation(eval PermissionEvaluation) PermissionRule {
	pat := strings.TrimSpace(eval.Target)
	if !eval.Action.Valid() {
		return PermissionRule{Action: eval.Action, Decision: PermissionAllow}
	}
	if eval.Action == PermissionActionRun {
		if hasControlRune(pat) {
			return PermissionRule{Action: eval.Action, Decision: PermissionAllow}
		}
		if strings.HasPrefix(strings.ToLower(pat), PermissionRunShell+":") {
			pat = PermissionRunShell
		}
		if pat == "" {
			return PermissionRule{Action: eval.Action, Decision: PermissionAllow}
		}
		pat = escapePermissionGlobLiteral(pat)
	} else {
		clean, ok := cleanPermissionFileTarget(pat)
		if !ok || clean == "" {
			return PermissionRule{Action: eval.Action, Decision: PermissionAllow}
		}
		pat = escapePermissionGlobLiteral(clean)
	}
	return PermissionRule{
		Action:   eval.Action,
		Patterns: []string{pat},
		Decision: PermissionAllow,
	}
}

// ApproveAlwaysPermission inserts an Allow rule for eval ahead of its current asking rule (so first-match-wins lets future matching calls through immediately), saves permissions.yaml when a project is open, and shows the "Always permission rule saved" toast with Show and Undo.
// It re-evaluates eval against the live rule table so a stale RuleIndex can never insert Allow ahead of a Deny rule, refuses empty/escaping targets, and refuses when permissions.yaml failed to load.
// It returns the 0-based index of the newly inserted (or already matching) rule.
func (e *Editor) ApproveAlwaysPermission(eval PermissionEvaluation) (int, error) {
	if e == nil {
		return -1, nil
	}
	e.ensurePermissionsInit()
	if e.perms.loadErr != nil {
		return -1, fmt.Errorf("editor: cannot save Always permission rule while %s is invalid: %w", PermissionsFileName, e.perms.loadErr)
	}
	rule := AlwaysRuleForEvaluation(eval)
	if len(rule.Patterns) == 0 {
		return -1, errors.New("editor: cannot save Always permission rule without a valid concrete target")
	}
	// Re-evaluate against the current rules so a stale eval.RuleIndex can never bypass a Deny rule.
	fresh := e.perms.file.Evaluate(PermissionRequest{
		Tool:   eval.Tool,
		Action: eval.Action,
		Target: eval.Target,
	})
	if fresh.Decision == PermissionDeny {
		return -1, fmt.Errorf("editor: cannot save Always permission rule: target is denied (%s)", fresh.Reason)
	}
	if fresh.Decision == PermissionAllow {
		return fresh.RuleIndex, nil
	}

	prev := e.perms.file.Clone()
	hadFile := false
	if p := e.PermissionsPath(); p != "" {
		if _, err := os.Stat(p); err == nil {
			hadFile = true
		}
	}
	insertAt := len(e.perms.file.Rules)
	if fresh.RuleIndex >= 0 && fresh.RuleIndex <= len(e.perms.file.Rules) {
		insertAt = fresh.RuleIndex
	}
	next := e.perms.file.Clone()
	next.Rules = slices.Insert(next.Rules, insertAt, rule)
	if err := e.SetPermissions(next); err != nil {
		return -1, err
	}
	e.perms.toast = &PermissionToast{
		Message:   e.T("permissions.toastSaved"),
		RuleIndex: insertAt,
		Rule:      rule,
		previous:  prev,
		hadFile:   hadFile,
	}
	e.bumpPermissionsRev()
	return insertAt, nil
}

// PermissionToast returns the currently visible "Always permission rule saved" toast, if any.
func (e *Editor) PermissionToast() (PermissionToast, bool) {
	if e == nil || e.perms.toast == nil {
		return PermissionToast{}, false
	}
	return *e.perms.toast, true
}

// ShowPermissionToastRule opens Settings › Permissions with the newly saved "Always" rule focused.
func (e *Editor) ShowPermissionToastRule() {
	if e == nil || e.perms.toast == nil {
		return
	}
	idx := e.perms.toast.RuleIndex
	e.OpenPermissionsSettings(idx)
}

// UndoPermissionToast reverts the last "Always" permission rule addition, restores permissions.yaml (or removes it if it did not exist before), and clears the toast.
func (e *Editor) UndoPermissionToast() error {
	if e == nil || e.perms.toast == nil {
		return nil
	}
	toast := e.perms.toast
	prev := toast.previous.Clone()
	if p := e.PermissionsPath(); p != "" {
		if toast.hadFile {
			encoded, err := EncodePermissions(&prev)
			if err != nil {
				return err
			}
			if err := writeFileAtomic(p, encoded); err != nil {
				return err
			}
			e.perms.hasFileOnDisk = true
			e.perms.diskSnap = makeDiskSnapshot(p, encoded)
			cp := prev.Clone()
			e.perms.diskFile = &cp
		} else {
			if rerr := os.Remove(p); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				return rerr
			}
			e.perms.hasFileOnDisk = false
			e.perms.diskSnap = diskSnapshot{path: filepath.Clean(p), exists: false}
			e.perms.diskFile = nil
		}
	}
	e.perms.toast = nil
	e.perms.file = prev
	if e.perms.focusedRule >= len(e.perms.file.Rules) {
		e.perms.focusedRule = len(e.perms.file.Rules) - 1
	}
	e.syncDiskWatchTargets()
	e.bumpPermissionsRulesRev()
	return nil
}

// DismissPermissionToast hides the active permission toast without changing saved rules.
func (e *Editor) DismissPermissionToast() {
	if e == nil || e.perms.toast == nil {
		return
	}
	e.perms.toast = nil
	e.bumpPermissionsRev()
}
