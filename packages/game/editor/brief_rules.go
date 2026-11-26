package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
)

// ProjectRulesFileName is the shared Markdown instructions file at a project's root
// read by Claude Code, Codex, and the built-in editor agent.
const ProjectRulesFileName = "AGENTS.md"

const reviewSuggestionWindow = 30

// DocReviewTarget identifies a shared project document whose agent or suggested edits go through review.
type DocReviewTarget string

const (
	// DocReviewBrief targets the project root brief.yaml file.
	DocReviewBrief DocReviewTarget = project.BriefFileName
	// DocReviewRules targets the project root AGENTS.md file.
	DocReviewRules DocReviewTarget = ProjectRulesFileName
)

// DocReview is one pending or resolved reviewable change to brief.yaml or AGENTS.md.
// Files on disk are never modified until the person keeps the review.
type DocReview struct {
	ID        string          `json:"id"`
	Target    DocReviewTarget `json:"target"`
	Summary   string          `json:"summary"`
	Rationale string          `json:"rationale,omitempty"`
	Source    string          `json:"source,omitempty"` // "agent" or "suggestion"
	Before    string          `json:"before"`
	After     string          `json:"after"`
	Diff      string          `json:"diff"`
	Status    string          `json:"status"` // "pending", "kept", "reverted"
}

// ReviewOutcomeRecord records whether a reviewed agent change was kept or reverted,
// used to detect repeated revert patterns for Settings › Project rules suggestions.
type ReviewOutcomeRecord struct {
	ReviewID string `json:"reviewId,omitempty"`
	// Action describes the normalized kind of change, such as "moved the camera bounds".
	Action  string `json:"action"`
	Field   string `json:"field,omitempty"`
	File    string `json:"file,omitempty"`
	Summary string `json:"summary,omitempty"`
	Kept    bool   `json:"kept"`
}

// SuggestedProjectRule is a project rule suggested from repeated reverted reviews.
type SuggestedProjectRule struct {
	ID            string `json:"id"`
	Pattern       string `json:"pattern"`
	Reason        string `json:"reason"`
	Rule          string `json:"rule"`
	RevertedCount int    `json:"revertedCount"`
	TotalMatches  int    `json:"totalMatches"`
	WindowSize    int    `json:"windowSize"`
}

// SettingsAgentRequest captures a request sent from a Settings page ("Agent on this page" card
// or the Settings search bar) to the Agent panel with the active Settings page as context.
type SettingsAgentRequest struct {
	Page          string `json:"page"`
	PageTitle     string `json:"pageTitle"`
	Scope         string `json:"scope,omitempty"`
	File          string `json:"file,omitempty"`
	Source        string `json:"source"` // "suggestion", "page_composer", or "search"
	Prompt        string `json:"prompt"`
	ContextPrompt string `json:"contextPrompt"`
}

type agentDocsState struct {
	brief        *project.Brief
	briefExists  bool
	briefPath    string
	briefSaved   []byte
	briefLoadErr error

	rules       string
	rulesExists bool
	rulesPath   string
	rulesSaved  string

	reviews          []DocReview
	nextReviewID     int
	reviewOutcomes   []ReviewOutcomeRecord
	dismissedSuggest map[string]bool

	lastAgentReq SettingsAgentRequest
	rev          uint64
}

func (e *Editor) agentDocsRev() uint64 {
	if e == nil {
		return 0
	}
	return e.agentDocs.rev
}

func (e *Editor) loadAgentDocsForProject(projectDir string) {
	if e == nil {
		return
	}
	e.agentDocs.brief = nil
	e.agentDocs.briefExists = false
	e.agentDocs.briefPath = ""
	e.agentDocs.briefSaved = nil
	e.agentDocs.briefLoadErr = nil

	e.agentDocs.rules = ""
	e.agentDocs.rulesExists = false
	e.agentDocs.rulesPath = ""
	e.agentDocs.rulesSaved = ""

	e.agentDocs.reviews = nil
	e.agentDocs.dismissedSuggest = nil

	if projectDir == "" {
		e.agentDocs.rev++
		return
	}

	briefPath := filepath.Join(projectDir, project.BriefFileName)
	e.agentDocs.briefPath = briefPath
	if raw, err := os.ReadFile(briefPath); err == nil {
		if b, perr := project.ParseBrief(raw); perr == nil {
			e.agentDocs.brief = b
			e.agentDocs.briefExists = true
			if enc, eerr := project.EncodeBrief(b); eerr == nil {
				e.agentDocs.briefSaved = enc
			} else {
				e.agentDocs.briefSaved = bytes.Clone(raw)
			}
		} else {
			e.agentDocs.briefLoadErr = perr
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		e.agentDocs.briefLoadErr = err
	}

	rulesPath := filepath.Join(projectDir, ProjectRulesFileName)
	e.agentDocs.rulesPath = rulesPath
	if raw, err := os.ReadFile(rulesPath); err == nil {
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		e.agentDocs.rules = text
		e.agentDocs.rulesExists = true
		e.agentDocs.rulesSaved = text
	}

	e.agentDocs.rev++
}

// Brief returns a deep copy of the project's brief.yaml document, or nil when no brief exists yet.
func (e *Editor) Brief() *project.Brief {
	if e == nil || !e.agentDocs.briefExists || e.agentDocs.brief == nil {
		return nil
	}
	return e.agentDocs.brief.Clone()
}

// BriefExists reports whether the open project has a game brief (loaded from brief.yaml or created in the editor).
func (e *Editor) BriefExists() bool {
	return e != nil && e.agentDocs.briefExists && e.agentDocs.brief != nil
}

// BriefPath returns the path to brief.yaml in the open project, or empty when no project is open.
func (e *Editor) BriefPath() string {
	if e == nil {
		return ""
	}
	return e.agentDocs.briefPath
}

// BriefLoadErr returns any parse error encountered when loading brief.yaml from disk.
func (e *Editor) BriefLoadErr() error {
	if e == nil {
		return nil
	}
	return e.agentDocs.briefLoadErr
}

// BriefDirty reports whether the in-memory game brief differs from the last saved canonical brief.yaml bytes.
func (e *Editor) BriefDirty() bool {
	if e == nil || !e.agentDocs.briefExists || e.agentDocs.brief == nil {
		return false
	}
	data, err := project.EncodeBrief(e.agentDocs.brief)
	return err != nil || !bytes.Equal(data, e.agentDocs.briefSaved)
}

// BriefYAML returns the canonical YAML encoding of the current brief, or nil when no brief exists.
func (e *Editor) BriefYAML() ([]byte, error) {
	if !e.BriefExists() {
		return nil, nil
	}
	return project.EncodeBrief(e.agentDocs.brief)
}

// BriefGaps returns the missing sections of brief.yaml in canonical order.
// When no brief exists yet, every section in project.BriefSections is returned.
func (e *Editor) BriefGaps() []project.BriefSection {
	if !e.BriefExists() {
		return slices.Clone(project.BriefSections)
	}
	return e.agentDocs.brief.MissingSections()
}

// BriefPlaytestTargets returns the structured playtest targets defined in brief.yaml for scene
// (empty scene returns all targets).
func (e *Editor) BriefPlaytestTargets(scene string) []project.BriefPlaytestTarget {
	if !e.BriefExists() {
		return nil
	}
	return e.agentDocs.brief.TargetsForScene(scene)
}

type briefCmd struct {
	ed            *Editor
	label         string
	before        *project.Brief
	beforeExists  bool
	beforeDisk    []byte
	beforeHasDisk bool
	after         *project.Brief
	afterBytes    []byte
}

func (c *briefCmd) Label() string { return c.label }
func (c *briefCmd) Do()           { _ = c.do() }
func (c *briefCmd) Undo()         { _ = c.undo() }

func (c *briefCmd) do() error {
	ed := c.ed
	if ed.OnSaveBrief != nil {
		if err := ed.OnSaveBrief(bytes.Clone(c.afterBytes)); err != nil {
			return err
		}
	} else if ed.agentDocs.briefPath != "" {
		if err := os.MkdirAll(filepath.Dir(ed.agentDocs.briefPath), 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(ed.agentDocs.briefPath, c.afterBytes); err != nil {
			return err
		}
	}
	ed.agentDocs.brief = c.after.Clone()
	ed.agentDocs.briefExists = true
	ed.agentDocs.briefSaved = bytes.Clone(c.afterBytes)
	ed.agentDocs.briefLoadErr = nil
	if !c.beforeExists {
		ed.filesRev++
	}
	ed.agentDocs.rev++
	ed.notify()
	return nil
}

func (c *briefCmd) undo() error {
	ed := c.ed
	if ed.OnSaveBrief != nil {
		if c.beforeExists && c.before != nil {
			prev, err := project.EncodeBrief(c.before)
			if err != nil {
				return err
			}
			if err := ed.OnSaveBrief(prev); err != nil {
				return err
			}
		}
	} else if ed.agentDocs.briefPath != "" {
		if c.beforeHasDisk {
			if err := writeFileAtomic(ed.agentDocs.briefPath, c.beforeDisk); err != nil {
				return err
			}
		} else if !c.beforeExists {
			if err := os.Remove(ed.agentDocs.briefPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	ed.agentDocs.brief = c.before.Clone()
	ed.agentDocs.briefExists = c.beforeExists
	if c.beforeExists && c.before != nil {
		if enc, err := project.EncodeBrief(c.before); err == nil {
			ed.agentDocs.briefSaved = enc
		}
	} else {
		ed.agentDocs.briefSaved = nil
	}
	if !c.beforeExists {
		ed.filesRev++
	}
	ed.agentDocs.rev++
	ed.notify()
	return nil
}

// EditBrief applies edit to a copy of the project's brief (creating version 1 when none exists yet),
// validates and encodes it in canonical YAML, writes brief.yaml when a project is open, and records
// the change as one undoable step in Editor.History.
func (e *Editor) EditBrief(label string, edit func(b *project.Brief)) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	var next *project.Brief
	if e.agentDocs.briefExists && e.agentDocs.brief != nil {
		next = e.agentDocs.brief.Clone()
	} else {
		next = &project.Brief{Version: project.BriefVersion}
	}
	edit(next)
	if next.Version == 0 {
		next.Version = project.BriefVersion
	}
	encoded, err := project.EncodeBrief(next)
	if err != nil {
		return err
	}
	// Re-parse canonical form so in-memory state matches disk byte for byte.
	canonical, err := project.ParseBrief(encoded)
	if err != nil {
		return err
	}
	if e.agentDocs.briefExists && e.agentDocs.brief != nil {
		if cur, cerr := project.EncodeBrief(e.agentDocs.brief); cerr == nil && bytes.Equal(cur, encoded) {
			return nil
		}
	}
	var beforeDisk []byte
	var beforeHasDisk bool
	if e.agentDocs.briefPath != "" {
		if raw, rerr := os.ReadFile(e.agentDocs.briefPath); rerr == nil {
			beforeDisk = raw
			beforeHasDisk = true
		}
	}
	if label == "" {
		label = "Edit Game Brief"
	}
	cmd := &briefCmd{
		ed:            e,
		label:         label,
		before:        e.agentDocs.brief.Clone(),
		beforeExists:  e.agentDocs.briefExists,
		beforeDisk:    beforeDisk,
		beforeHasDisk: beforeHasDisk,
		after:         canonical,
		afterBytes:    encoded,
	}
	return e.History.execute(cmd)
}

// CreateBrief creates or replaces brief.yaml with b as one undoable step.
func (e *Editor) CreateBrief(b project.Brief) error {
	if b.Version == 0 {
		b.Version = project.BriefVersion
	}
	return e.EditBrief("Create Game Brief", func(dst *project.Brief) {
		*dst = *b.Clone()
	})
}

// SetBriefPitch updates brief.yaml's pitch field as one undoable step.
func (e *Editor) SetBriefPitch(pitch string) error {
	return e.EditBrief("Edit Brief Pitch", func(b *project.Brief) {
		b.Pitch = strings.TrimSpace(pitch)
	})
}

// SetBriefGenre updates brief.yaml's genre list as one undoable step.
func (e *Editor) SetBriefGenre(genre []string) error {
	return e.EditBrief("Edit Brief Genre", func(b *project.Brief) {
		b.Genre = cleanBriefLabelList(genre)
	})
}

// AddBriefGenre appends a genre label to brief.yaml as one undoable step.
func (e *Editor) AddBriefGenre(label string) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil
	}
	return e.EditBrief("Add Brief Genre", func(b *project.Brief) {
		for _, g := range b.Genre {
			if strings.EqualFold(g, label) {
				return
			}
		}
		b.Genre = append(b.Genre, label)
	})
}

// RemoveBriefGenre removes a genre label from brief.yaml as one undoable step.
func (e *Editor) RemoveBriefGenre(label string) error {
	return e.EditBrief("Remove Brief Genre", func(b *project.Brief) {
		b.Genre = removeBriefLabel(b.Genre, label)
	})
}

// SetBriefTags updates brief.yaml's tags list as one undoable step.
func (e *Editor) SetBriefTags(tags []string) error {
	return e.EditBrief("Edit Brief Tags", func(b *project.Brief) {
		b.Tags = cleanBriefLabelList(tags)
	})
}

// SetBriefPlatforms updates brief.yaml's platforms list as one undoable step.
func (e *Editor) SetBriefPlatforms(platforms []string) error {
	return e.EditBrief("Edit Brief Platforms", func(b *project.Brief) {
		b.Platforms = cleanBriefLabelList(platforms)
	})
}

// AddBriefPlatform appends a platform to brief.yaml as one undoable step.
func (e *Editor) AddBriefPlatform(platform string) error {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil
	}
	return e.EditBrief("Add Brief Platform", func(b *project.Brief) {
		for _, p := range b.Platforms {
			if strings.EqualFold(p, platform) {
				return
			}
		}
		b.Platforms = append(b.Platforms, platform)
	})
}

// RemoveBriefPlatform removes a platform from brief.yaml as one undoable step.
func (e *Editor) RemoveBriefPlatform(platform string) error {
	return e.EditBrief("Remove Brief Platform", func(b *project.Brief) {
		b.Platforms = removeBriefLabel(b.Platforms, platform)
	})
}

// SetBriefArtDirection updates brief.yaml's artDirection field as one undoable step.
func (e *Editor) SetBriefArtDirection(art string) error {
	return e.EditBrief("Edit Brief Art Direction", func(b *project.Brief) {
		b.ArtDirection = strings.TrimSpace(art)
	})
}

// SetBriefToneAndAudio updates brief.yaml's toneAndAudio field as one undoable step.
func (e *Editor) SetBriefToneAndAudio(tone string) error {
	return e.EditBrief("Edit Brief Tone and Audio", func(b *project.Brief) {
		b.ToneAndAudio = strings.TrimSpace(tone)
	})
}

// SetBriefFeelSummary updates brief.yaml's feel.summary field as one undoable step.
func (e *Editor) SetBriefFeelSummary(summary string) error {
	return e.EditBrief("Edit Brief Feel and Difficulty", func(b *project.Brief) {
		if b.Feel == nil {
			b.Feel = &project.BriefFeel{}
		}
		b.Feel.Summary = strings.TrimSpace(summary)
	})
}

// SetBriefPlaytestTargets replaces brief.yaml's feel.targets slice as one undoable step.
func (e *Editor) SetBriefPlaytestTargets(targets []project.BriefPlaytestTarget) error {
	return e.EditBrief("Edit Brief Playtest Targets", func(b *project.Brief) {
		if b.Feel == nil {
			b.Feel = &project.BriefFeel{}
		}
		b.Feel.Targets = slices.Clone(targets)
	})
}

// AddBriefPlaytestTarget adds a playtest target to brief.yaml's feel.targets as one undoable step.
func (e *Editor) AddBriefPlaytestTarget(target project.BriefPlaytestTarget) error {
	return e.EditBrief("Add Brief Playtest Target", func(b *project.Brief) {
		if b.Feel == nil {
			b.Feel = &project.BriefFeel{}
		}
		b.Feel.Targets = append(b.Feel.Targets, target)
	})
}

// RemoveBriefPlaytestTarget removes the playtest target at index from brief.yaml as one undoable step.
func (e *Editor) RemoveBriefPlaytestTarget(index int) error {
	return e.EditBrief("Remove Brief Playtest Target", func(b *project.Brief) {
		if b.Feel == nil || index < 0 || index >= len(b.Feel.Targets) {
			return
		}
		b.Feel.Targets = slices.Delete(b.Feel.Targets, index, index+1)
	})
}

// SaveBrief writes the current game brief to brief.yaml in canonical YAML form.
func (e *Editor) SaveBrief() error {
	if e == nil || !e.BriefExists() {
		return nil
	}
	data, err := project.EncodeBrief(e.agentDocs.brief)
	if err != nil {
		return err
	}
	if e.OnSaveBrief != nil {
		if err := e.OnSaveBrief(data); err != nil {
			return err
		}
	} else if e.agentDocs.briefPath != "" {
		if err := writeFileAtomic(e.agentDocs.briefPath, data); err != nil {
			return err
		}
	} else {
		return ErrNoProject
	}
	e.agentDocs.briefSaved = data
	e.agentDocs.rev++
	e.notify()
	return nil
}

// ReloadBrief reloads brief.yaml from disk for the open project.
func (e *Editor) ReloadBrief() error {
	if e == nil || e.agentDocs.briefPath == "" {
		return ErrNoProject
	}
	raw, err := os.ReadFile(e.agentDocs.briefPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			e.agentDocs.brief = nil
			e.agentDocs.briefExists = false
			e.agentDocs.briefSaved = nil
			e.agentDocs.briefLoadErr = nil
			e.agentDocs.rev++
			e.notify()
			return nil
		}
		e.agentDocs.briefLoadErr = err
		e.agentDocs.rev++
		e.notify()
		return err
	}
	b, err := project.ParseBrief(raw)
	if err != nil {
		e.agentDocs.briefLoadErr = err
		e.agentDocs.rev++
		e.notify()
		return err
	}
	enc, err := project.EncodeBrief(b)
	if err != nil {
		return err
	}
	e.agentDocs.brief = b
	e.agentDocs.briefExists = true
	e.agentDocs.briefSaved = enc
	e.agentDocs.briefLoadErr = nil
	e.agentDocs.rev++
	e.notify()
	return nil
}

func cleanBriefLabelList(items []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		s := strings.TrimSpace(item)
		if s == "" {
			continue
		}
		norm := strings.ToLower(s)
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, s)
	}
	return out
}

func removeBriefLabel(items []string, target string) []string {
	var out []string
	for _, item := range items {
		if strings.EqualFold(item, target) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// ProjectRules returns the plain Markdown contents of the project's AGENTS.md file.
func (e *Editor) ProjectRules() string {
	if e == nil {
		return ""
	}
	return e.agentDocs.rules
}

// ProjectRulesExists reports whether the project has an AGENTS.md file on disk or authored in the editor.
func (e *Editor) ProjectRulesExists() bool {
	return e != nil && e.agentDocs.rulesExists
}

// ProjectRulesPath returns the path to AGENTS.md in the open project, or empty when no project is open.
func (e *Editor) ProjectRulesPath() string {
	if e == nil {
		return ""
	}
	return e.agentDocs.rulesPath
}

// ProjectRulesDirty reports whether the in-memory AGENTS.md content differs from disk.
func (e *Editor) ProjectRulesDirty() bool {
	return e != nil && e.agentDocs.rulesExists && e.agentDocs.rules != e.agentDocs.rulesSaved
}

// ProjectRulesCount returns the number of Markdown list-item rules (lines starting with "- " or "* ") in AGENTS.md.
func (e *Editor) ProjectRulesCount() int {
	if e == nil || strings.TrimSpace(e.agentDocs.rules) == "" {
		return 0
	}
	count := 0
	for _, line := range strings.Split(e.agentDocs.rules, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			count++
		}
	}
	return count
}

type projectRulesCmd struct {
	ed            *Editor
	label         string
	before        string
	beforeExists  bool
	beforeDisk    []byte
	beforeHasDisk bool
	after         string
}

func (c *projectRulesCmd) Label() string { return c.label }
func (c *projectRulesCmd) Do()           { _ = c.do() }
func (c *projectRulesCmd) Undo()         { _ = c.undo() }

func (c *projectRulesCmd) do() error {
	ed := c.ed
	data := []byte(c.after)
	if ed.OnSaveProjectRules != nil {
		if err := ed.OnSaveProjectRules(bytes.Clone(data)); err != nil {
			return err
		}
	} else if ed.agentDocs.rulesPath != "" {
		if err := os.MkdirAll(filepath.Dir(ed.agentDocs.rulesPath), 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(ed.agentDocs.rulesPath, data); err != nil {
			return err
		}
	}
	ed.agentDocs.rules = c.after
	ed.agentDocs.rulesExists = true
	ed.agentDocs.rulesSaved = c.after
	if !c.beforeExists {
		ed.filesRev++
	}
	ed.agentDocs.rev++
	ed.notify()
	return nil
}

func (c *projectRulesCmd) undo() error {
	ed := c.ed
	if ed.OnSaveProjectRules != nil {
		if c.beforeExists {
			if err := ed.OnSaveProjectRules([]byte(c.before)); err != nil {
				return err
			}
		}
	} else if ed.agentDocs.rulesPath != "" {
		if c.beforeHasDisk {
			if err := writeFileAtomic(ed.agentDocs.rulesPath, c.beforeDisk); err != nil {
				return err
			}
		} else if !c.beforeExists {
			if err := os.Remove(ed.agentDocs.rulesPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	ed.agentDocs.rules = c.before
	ed.agentDocs.rulesExists = c.beforeExists
	if c.beforeExists {
		ed.agentDocs.rulesSaved = c.before
	} else {
		ed.agentDocs.rulesSaved = ""
	}
	if !c.beforeExists {
		ed.filesRev++
	}
	ed.agentDocs.rev++
	ed.notify()
	return nil
}

// SetProjectRules updates AGENTS.md as plain Markdown in one undoable step in Editor.History,
// writing the file when a project is open.
func (e *Editor) SetProjectRules(label, content string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	norm := strings.ReplaceAll(content, "\r\n", "\n")
	if e.agentDocs.rulesExists && e.agentDocs.rules == norm {
		return nil
	}
	var beforeDisk []byte
	var beforeHasDisk bool
	if e.agentDocs.rulesPath != "" {
		if raw, err := os.ReadFile(e.agentDocs.rulesPath); err == nil {
			beforeDisk = raw
			beforeHasDisk = true
		}
	}
	if label == "" {
		label = "Edit Project Rules"
	}
	return e.History.execute(&projectRulesCmd{
		ed:            e,
		label:         label,
		before:        e.agentDocs.rules,
		beforeExists:  e.agentDocs.rulesExists,
		beforeDisk:    beforeDisk,
		beforeHasDisk: beforeHasDisk,
		after:         norm,
	})
}

// SaveProjectRules writes the current AGENTS.md content to disk.
func (e *Editor) SaveProjectRules() error {
	if e == nil || !e.agentDocs.rulesExists {
		return nil
	}
	data := []byte(e.agentDocs.rules)
	if e.OnSaveProjectRules != nil {
		if err := e.OnSaveProjectRules(data); err != nil {
			return err
		}
	} else if e.agentDocs.rulesPath != "" {
		if err := writeFileAtomic(e.agentDocs.rulesPath, data); err != nil {
			return err
		}
	} else {
		return ErrNoProject
	}
	e.agentDocs.rulesSaved = e.agentDocs.rules
	e.agentDocs.rev++
	e.notify()
	return nil
}

// ReloadProjectRules reloads AGENTS.md from disk for the open project.
func (e *Editor) ReloadProjectRules() error {
	if e == nil || e.agentDocs.rulesPath == "" {
		return ErrNoProject
	}
	raw, err := os.ReadFile(e.agentDocs.rulesPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			e.agentDocs.rules = ""
			e.agentDocs.rulesExists = false
			e.agentDocs.rulesSaved = ""
			e.agentDocs.rev++
			e.notify()
			return nil
		}
		return err
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	e.agentDocs.rules = text
	e.agentDocs.rulesExists = true
	e.agentDocs.rulesSaved = text
	e.agentDocs.rev++
	e.notify()
	return nil
}

// RecordReviewOutcome records a kept or reverted review outcome for rule suggestion detection.
func (e *Editor) RecordReviewOutcome(rec ReviewOutcomeRecord) {
	if e == nil {
		return
	}
	e.agentDocs.reviewOutcomes = append(e.agentDocs.reviewOutcomes, rec)
	e.agentDocs.rev++
	e.notify()
}

// RecordReviewOutcomes appends multiple review outcomes at once.
func (e *Editor) RecordReviewOutcomes(recs ...ReviewOutcomeRecord) {
	if e == nil || len(recs) == 0 {
		return
	}
	e.agentDocs.reviewOutcomes = append(e.agentDocs.reviewOutcomes, recs...)
	e.agentDocs.rev++
	e.notify()
}

// ReviewOutcomes returns a copy of all recorded review outcomes.
func (e *Editor) ReviewOutcomes() []ReviewOutcomeRecord {
	if e == nil || len(e.agentDocs.reviewOutcomes) == 0 {
		return nil
	}
	return slices.Clone(e.agentDocs.reviewOutcomes)
}

// SuggestedProjectRules analyzes patterns in the last 30 review outcomes and returns
// suggested rules for actions that the author consistently reverted and has not yet
// added to AGENTS.md or dismissed.
func (e *Editor) SuggestedProjectRules() []SuggestedProjectRule {
	if e == nil || len(e.agentDocs.reviewOutcomes) == 0 {
		return nil
	}
	window := e.agentDocs.reviewOutcomes
	if len(window) > reviewSuggestionWindow {
		window = window[len(window)-reviewSuggestionWindow:]
	}
	windowSize := len(window)

	type bucket struct {
		pattern  string
		reverted int
		total    int
		order    int
	}
	buckets := map[string]*bucket{}
	var keys []string
	for _, rec := range window {
		pat := normalizeReviewPattern(rec)
		if pat == "" {
			continue
		}
		key := strings.ToLower(pat)
		b, ok := buckets[key]
		if !ok {
			b = &bucket{pattern: pat, order: len(keys)}
			buckets[key] = b
			keys = append(keys, key)
		}
		b.total++
		if !rec.Kept {
			b.reverted++
		}
	}

	existingLower := strings.ToLower(e.agentDocs.rules)
	var out []SuggestedProjectRule
	for _, key := range keys {
		b := buckets[key]
		if b.reverted < 2 {
			continue
		}
		every := b.reverted == b.total
		mostly := b.reverted >= 3 && float64(b.reverted)/float64(b.total) >= 0.8
		if !every && !mostly {
			continue
		}
		id := "suggest-" + slugifyRuleID(b.pattern)
		if e.agentDocs.dismissedSuggest[id] {
			continue
		}
		rule := formatSuggestedRuleText(b.pattern)
		if rulesAlreadyCover(existingLower, b.pattern, rule) {
			continue
		}
		var reason string
		if every {
			reason = fmt.Sprintf("Suggested from your last %d reviews: you reverted every change that %s.", windowSize, b.pattern)
		} else {
			reason = fmt.Sprintf("Suggested from your last %d reviews: you reverted %d of %d changes that %s.", windowSize, b.reverted, b.total, b.pattern)
		}
		out = append(out, SuggestedProjectRule{
			ID:            id,
			Pattern:       b.pattern,
			Reason:        reason,
			Rule:          rule,
			RevertedCount: b.reverted,
			TotalMatches:  b.total,
			WindowSize:    windowSize,
		})
	}
	// Highest revert count first, preserving insertion order on ties.
	slices.SortStableFunc(out, func(a, b SuggestedProjectRule) int {
		if a.RevertedCount != b.RevertedCount {
			if a.RevertedCount > b.RevertedCount {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// DismissSuggestedRule hides suggestion id from SuggestedProjectRules.
func (e *Editor) DismissSuggestedRule(id string) {
	if e == nil || id == "" {
		return
	}
	if e.agentDocs.dismissedSuggest == nil {
		e.agentDocs.dismissedSuggest = map[string]bool{}
	}
	e.agentDocs.dismissedSuggest[id] = true
	e.agentDocs.rev++
	e.notify()
}

// ProposeSuggestedRule turns suggested rule id into a reviewable DocReview targeting AGENTS.md.
// AGENTS.md is not modified until the person keeps the review.
func (e *Editor) ProposeSuggestedRule(id string) (*DocReview, error) {
	if e == nil {
		return nil, errors.New("editor: nil editor")
	}
	for _, s := range e.SuggestedProjectRules() {
		if s.ID == id {
			next := appendRuleToMarkdown(e.agentDocs.rules, e.projectDisplayName(), s.Rule)
			return e.ProposeProjectRulesReview(next, "Add rule: "+s.Rule, s.Reason, "suggestion")
		}
	}
	return nil, fmt.Errorf("editor: suggested rule %q not found", id)
}

func (e *Editor) projectDisplayName() string {
	if e != nil && e.Project != nil && strings.TrimSpace(e.Project.Name) != "" {
		return e.Project.Name
	}
	return "this project"
}

func appendRuleToMarkdown(existing, projectName, rule string) string {
	rule = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(rule, "- "), "* "))
	line := "- " + rule
	norm := strings.ReplaceAll(existing, "\r\n", "\n")
	if strings.TrimSpace(norm) == "" {
		return fmt.Sprintf("# AGENTS.md · how to work on %s\n\n%s\n", projectName, line)
	}
	if !strings.HasSuffix(norm, "\n") {
		norm += "\n"
	}
	return norm + line + "\n"
}

func normalizeReviewPattern(rec ReviewOutcomeRecord) string {
	if s := strings.TrimSpace(rec.Action); s != "" {
		return s
	}
	if f := strings.TrimSpace(rec.Field); f != "" {
		return "changed " + f
	}
	if s := strings.TrimSpace(rec.Summary); s != "" {
		return s
	}
	return ""
}

func slugifyRuleID(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func formatSuggestedRuleText(pattern string) string {
	p := strings.TrimSpace(strings.TrimSuffix(pattern, "."))
	lower := strings.ToLower(p)
	switch {
	case strings.HasPrefix(lower, "moved "):
		return "Never move " + p[len("moved "):] + " without asking first."
	case strings.HasPrefix(lower, "edited "):
		return "Never edit " + p[len("edited "):] + " without asking first."
	case strings.HasPrefix(lower, "deleted "):
		return "Never delete " + p[len("deleted "):] + " without asking first."
	case strings.HasPrefix(lower, "removed "):
		return "Never remove " + p[len("removed "):] + " without asking first."
	case strings.HasPrefix(lower, "changed "):
		return "Never change " + p[len("changed "):] + " without asking first."
	case strings.HasPrefix(lower, "renamed "):
		return "Never rename " + p[len("renamed "):] + " without asking first."
	default:
		return "Do not make changes that " + p + " without asking first."
	}
}

func rulesAlreadyCover(existingLower, pattern, rule string) bool {
	if existingLower == "" {
		return false
	}
	if strings.Contains(existingLower, strings.ToLower(rule)) {
		return true
	}
	p := strings.ToLower(strings.TrimSpace(pattern))
	if strings.Contains(existingLower, p) {
		return true
	}
	// Strip leading past-tense verb to catch rules like "Never move the camera bounds".
	for _, prefix := range []string{"moved ", "edited ", "deleted ", "removed ", "changed ", "renamed "} {
		if rest, ok := strings.CutPrefix(p, prefix); ok && len(rest) >= 6 && strings.Contains(existingLower, rest) {
			return true
		}
	}
	return false
}

// ProposeBriefReview validates next, builds a canonical YAML diff against the current brief.yaml,
// and queues a pending DocReview without modifying the open brief or disk until KeepDocReview is called.
func (e *Editor) ProposeBriefReview(next *project.Brief, summary, rationale, source string) (*DocReview, error) {
	if e == nil {
		return nil, errors.New("editor: nil editor")
	}
	if next == nil {
		return nil, errors.New("editor: nil brief")
	}
	if next.Version == 0 {
		next = next.Clone()
		next.Version = project.BriefVersion
	}
	afterBytes, err := project.EncodeBrief(next)
	if err != nil {
		return nil, err
	}
	var before string
	if e.BriefExists() {
		if raw, berr := project.EncodeBrief(e.agentDocs.brief); berr == nil {
			before = string(raw)
		}
	}
	after := string(afterBytes)
	if before == after {
		return nil, errors.New("editor: proposed brief.yaml makes no changes")
	}
	if strings.TrimSpace(summary) == "" {
		summary = "Update brief.yaml"
	}
	if source == "" {
		source = "agent"
	}
	return e.addDocReview(DocReviewBrief, summary, rationale, source, before, after), nil
}

// ProposeProjectRulesReview builds a line diff against the current AGENTS.md and queues a pending
// DocReview without modifying AGENTS.md or disk until KeepDocReview is called.
func (e *Editor) ProposeProjectRulesReview(nextMarkdown, summary, rationale, source string) (*DocReview, error) {
	if e == nil {
		return nil, errors.New("editor: nil editor")
	}
	after := strings.ReplaceAll(nextMarkdown, "\r\n", "\n")
	before := e.agentDocs.rules
	if before == after {
		return nil, errors.New("editor: proposed AGENTS.md makes no changes")
	}
	if strings.TrimSpace(summary) == "" {
		summary = "Update AGENTS.md"
	}
	if source == "" {
		source = "agent"
	}
	return e.addDocReview(DocReviewRules, summary, rationale, source, before, after), nil
}

func (e *Editor) addDocReview(target DocReviewTarget, summary, rationale, source, before, after string) *DocReview {
	e.agentDocs.nextReviewID++
	rev := DocReview{
		ID:        fmt.Sprintf("doc-review-%d", e.agentDocs.nextReviewID),
		Target:    target,
		Summary:   summary,
		Rationale: rationale,
		Source:    source,
		Before:    before,
		After:     after,
		Diff:      buildUnifiedLineDiff(string(target), before, after),
		Status:    "pending",
	}
	e.agentDocs.reviews = append(e.agentDocs.reviews, rev)
	e.agentDocs.rev++
	e.notify()
	cp := rev
	return &cp
}

// PendingDocReviews returns all pending DocReviews matching target (or all targets when target is empty).
func (e *Editor) PendingDocReviews(target DocReviewTarget) []DocReview {
	if e == nil || len(e.agentDocs.reviews) == 0 {
		return nil
	}
	var out []DocReview
	for _, r := range e.agentDocs.reviews {
		if r.Status != "pending" {
			continue
		}
		if target != "" && r.Target != target {
			continue
		}
		out = append(out, r)
	}
	return out
}

// DocReviews returns a copy of all DocReviews (pending, kept, and reverted).
func (e *Editor) DocReviews() []DocReview {
	if e == nil || len(e.agentDocs.reviews) == 0 {
		return nil
	}
	return slices.Clone(e.agentDocs.reviews)
}

// KeepDocReview accepts the pending DocReview with id, applying the change as one undoable step
// in Editor.History and writing the file when a project is open.
func (e *Editor) KeepDocReview(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	idx := -1
	for i := range e.agentDocs.reviews {
		if e.agentDocs.reviews[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("editor: review %q not found", id)
	}
	r := e.agentDocs.reviews[idx]
	if r.Status != "pending" {
		return fmt.Errorf("editor: review %q is already %s", id, r.Status)
	}
	switch r.Target {
	case DocReviewBrief:
		parsed, err := project.ParseBrief([]byte(r.After))
		if err != nil {
			return err
		}
		if err := e.EditBrief("Keep review: "+r.Summary, func(dst *project.Brief) {
			*dst = *parsed
		}); err != nil {
			return err
		}
	case DocReviewRules:
		if err := e.SetProjectRules("Keep review: "+r.Summary, r.After); err != nil {
			return err
		}
	default:
		return fmt.Errorf("editor: unknown review target %q", r.Target)
	}
	e.agentDocs.reviews[idx].Status = "kept"
	e.agentDocs.reviewOutcomes = append(e.agentDocs.reviewOutcomes, ReviewOutcomeRecord{
		ReviewID: r.ID,
		Action:   "updated " + string(r.Target),
		File:     string(r.Target),
		Summary:  r.Summary,
		Kept:     true,
	})
	e.agentDocs.rev++
	e.notify()
	return nil
}

// RevertDocReview dismisses the pending DocReview with id without modifying the document or disk.
func (e *Editor) RevertDocReview(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	idx := -1
	for i := range e.agentDocs.reviews {
		if e.agentDocs.reviews[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("editor: review %q not found", id)
	}
	r := e.agentDocs.reviews[idx]
	if r.Status != "pending" {
		return fmt.Errorf("editor: review %q is already %s", id, r.Status)
	}
	e.agentDocs.reviews[idx].Status = "reverted"
	e.agentDocs.reviewOutcomes = append(e.agentDocs.reviewOutcomes, ReviewOutcomeRecord{
		ReviewID: r.ID,
		Action:   "updated " + string(r.Target),
		File:     string(r.Target),
		Summary:  r.Summary,
		Kept:     false,
	})
	e.agentDocs.rev++
	e.notify()
	return nil
}

func buildUnifiedLineDiff(filename, before, after string) string {
	oldLines := splitLinesForDiff(before)
	newLines := splitLinesForDiff(after)
	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", filename, filename)
	// Find shared prefix and suffix so diffs stay concise and readable.
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	oldEnd := len(oldLines)
	newEnd := len(newLines)
	for oldEnd > prefix && newEnd > prefix && oldLines[oldEnd-1] == newLines[newEnd-1] {
		oldEnd--
		newEnd--
	}
	ctxStart := max(0, prefix-2)
	ctxOldEnd := min(len(oldLines), oldEnd+2)
	ctxNewEnd := min(len(newLines), newEnd+2)
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", ctxStart+1, ctxOldEnd-ctxStart, ctxStart+1, ctxNewEnd-ctxStart)
	for i := ctxStart; i < prefix; i++ {
		b.WriteString(" " + oldLines[i] + "\n")
	}
	for i := prefix; i < oldEnd; i++ {
		b.WriteString("-" + oldLines[i] + "\n")
	}
	for i := prefix; i < newEnd; i++ {
		b.WriteString("+" + newLines[i] + "\n")
	}
	for i := oldEnd; i < ctxOldEnd; i++ {
		b.WriteString(" " + oldLines[i] + "\n")
	}
	return b.String()
}

func splitLinesForDiff(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), "\n")
}

// SettingsPageContext returns the page ID, display title, scope pill, and associated project file
// for the active Settings tab.
func (e *Editor) SettingsPageContext() (pageID, title, scope, file string) {
	switch e.EditorSettingsTab() {
	case EditorSettingsTabBrief:
		return "brief", e.T("settings.brief"), e.T("settings.scopeBrief"), project.BriefFileName
	case EditorSettingsTabRules:
		return "rules", e.T("settings.rules"), e.T("settings.scopeRules"), ProjectRulesFileName
	case EditorSettingsTabShortcuts:
		return "shortcuts", e.T("settings.shortcuts"), e.T("settings.thisComputer"), ""
	default:
		return "general", e.T("settings.general"), e.T("settings.thisComputer"), ""
	}
}

// AskAgentFromSettings dispatches an agent request from the Settings dialog ("Agent on this page"
// card or the top Settings search bar) with the current Settings page as context, updating
// ComposerDraft and invoking OnSettingsAgentRequest and OnComposerPrompt when set.
func (e *Editor) AskAgentFromSettings(source, prompt string) SettingsAgentRequest {
	if e == nil {
		return SettingsAgentRequest{}
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return SettingsAgentRequest{}
	}
	if source == "" {
		source = "page_composer"
	}
	pageID, title, scope, file := e.SettingsPageContext()
	ctxHeader := fmt.Sprintf("[Settings › %s]", title)
	if file != "" {
		ctxHeader = fmt.Sprintf("[Settings › %s (%s)]", title, file)
	}
	req := SettingsAgentRequest{
		Page:          pageID,
		PageTitle:     title,
		Scope:         scope,
		File:          file,
		Source:        source,
		Prompt:        prompt,
		ContextPrompt: ctxHeader + " " + prompt,
	}
	e.agentDocs.lastAgentReq = req
	e.composerDraft = req.ContextPrompt
	e.Status = req.ContextPrompt
	if e.OnSettingsAgentRequest != nil {
		e.OnSettingsAgentRequest(req)
	}
	if e.OnComposerPrompt != nil {
		e.OnComposerPrompt(req.ContextPrompt)
	}
	e.agentDocs.rev++
	e.notify()
	return req
}

// LastSettingsAgentRequest returns the most recent request dispatched via AskAgentFromSettings.
func (e *Editor) LastSettingsAgentRequest() SettingsAgentRequest {
	if e == nil {
		return SettingsAgentRequest{}
	}
	return e.agentDocs.lastAgentReq
}

// BriefAgentNote computes the contextual note and suggested asks for the "Agent on this page" card
// on Settings › Game brief.
func (e *Editor) BriefAgentNote() (note string, suggestions []string) {
	if !e.BriefExists() {
		return e.T("settings.briefAgentEmptyNote"), []string{
			e.T("settings.briefAgentDraftAsk"),
			e.T("settings.briefAgentTargetsAsk"),
		}
	}
	gaps := e.BriefGaps()
	// Check if an active playtest report has a missed metric sourced from brief.yaml.
	if sess := e.playtest.session; sess != nil {
		if entry := sess.ActiveEntry(); entry != nil && entry.Report != nil {
			for _, m := range entry.Report.Metrics {
				if m.Goal != nil && m.Goal.Source == project.BriefFileName && m.Status == "missed" {
					label := m.Label
					if label == "" {
						label = m.Name
					}
					scene := entry.Report.Scope.Scene
					if scene == "" {
						scene = "playtests"
					}
					note = fmt.Sprintf("%s %s average %g, outside the %s your brief asks for.", scene, strings.ToLower(label), m.Value, m.Goal.Describe(m.Unit))
					sug := []string{fmt.Sprintf("Tune %s toward the brief", scene)}
					if len(gaps) > 0 {
						sug = append(sug, "Fill in the missing "+briefSectionDisplayNames(gaps)+" section")
					} else {
						sug = append(sug, e.T("settings.briefAgentTargetsAsk"))
					}
					return note, sug
				}
			}
		}
	}
	if len(gaps) > 0 {
		gapText := briefSectionDisplayNames(gaps)
		return fmt.Sprintf("The agent reads brief.yaml before every request. Missing: %s.", gapText), []string{
			"Fill in the missing " + gapText + " section",
			e.T("settings.briefAgentTargetsAsk"),
		}
	}
	return e.T("settings.briefAgentCompleteNote"), []string{
		e.T("settings.briefAgentTuneAsk"),
		e.T("settings.briefAgentTargetsAsk"),
	}
}

// RulesAgentNote computes the contextual note and suggested asks for the "Agent on this page" card
// on Settings › Project rules.
func (e *Editor) RulesAgentNote() (note string, suggestions []string) {
	return e.T("settings.rulesAgentNote"), []string{
		e.T("settings.rulesAgentRevertsAsk"),
		e.T("settings.rulesAgentCheckAsk"),
	}
}

func briefSectionDisplayNames(sections []project.BriefSection) string {
	names := make([]string, 0, len(sections))
	for _, s := range sections {
		names = append(names, briefSectionLabel(s))
	}
	return strings.Join(names, ", ")
}

func briefSectionLabel(s project.BriefSection) string {
	switch s {
	case project.BriefSectionPitch:
		return "pitch"
	case project.BriefSectionGenre:
		return "genre"
	case project.BriefSectionPlatforms:
		return "platforms"
	case project.BriefSectionArtDirection:
		return "art direction"
	case project.BriefSectionFeel:
		return "feel and difficulty"
	case project.BriefSectionToneAndAudio:
		return "tone and audio"
	default:
		return string(s)
	}
}
