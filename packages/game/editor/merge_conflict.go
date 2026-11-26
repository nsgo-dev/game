package editor

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"go.yaml.in/yaml/v3"
)

// MergeChoice records which resolution the author picked for one scene merge conflict item.
type MergeChoice string

const (
	// MergeChoiceUnresolved indicates the author has not yet chosen a resolution.
	MergeChoiceUnresolved MergeChoice = ""
	// MergeChoiceAgent selects the agent's suggested resolution.
	MergeChoiceAgent MergeChoice = "agent"
	// MergeChoiceMine selects the current branch's (ours) version.
	MergeChoiceMine MergeChoice = "mine"
	// MergeChoiceTheirs selects the incoming branch's (theirs) version.
	MergeChoiceTheirs MergeChoice = "theirs"
	// MergeChoiceCustom selects hand-edited YAML entered by the author.
	MergeChoiceCustom MergeChoice = "custom"
)

// MergeSideMeta describes one branch participating in a scene merge.
type MergeSideMeta struct {
	Branch  string `json:"branch"`
	Author  string `json:"author,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Message string `json:"message,omitempty"`
}

// MergeOption describes one selectable resolution card (Agent's suggestion, Mine, or Theirs).
type MergeOption struct {
	Choice  MergeChoice            `json:"choice"`
	Title   string                 `json:"title"`
	Meta    string                 `json:"meta"`
	YAML    string                 `json:"yaml"`
	Reason  string                 `json:"reason,omitempty"`
	Deleted bool                   `json:"deleted,omitempty"`
	Object  *sceneformat.ObjectDoc `json:"-"`
	// fieldSide picks "ours" or "theirs" per conflict field path for non-object or field-level merges.
	fieldSide map[string]MergeChoice
}

// MergeConflictItem groups all sceneformat.MergeConflict entries that belong to one scene item
// (normally one object ID, or a layer/scene/connection/tilemap/override entry) so conflicts are
// resolved object by object.
type MergeConflictItem struct {
	ID           string                      `json:"id"`
	Scope        string                      `json:"scope"`
	Layer        string                      `json:"layer,omitempty"`
	ObjectID     string                      `json:"objectId,omitempty"`
	Target       string                      `json:"target,omitempty"`
	Name         string                      `json:"name"`
	Kind         string                      `json:"kind"`
	Summary      string                      `json:"summary"`
	Conflicts    []sceneformat.MergeConflict `json:"conflicts"`
	BaseObj      *sceneformat.ObjectDoc      `json:"-"`
	MineObj      *sceneformat.ObjectDoc      `json:"-"`
	TheirsObj    *sceneformat.ObjectDoc      `json:"-"`
	MineLayer    string                      `json:"mineLayer,omitempty"`
	TheirsLayer  string                      `json:"theirsLayer,omitempty"`
	MineParent   string                      `json:"mineParent,omitempty"`
	TheirsParent string                      `json:"theirsParent,omitempty"`

	Agent  MergeOption `json:"agent"`
	Mine   MergeOption `json:"mine"`
	Theirs MergeOption `json:"theirs"`

	Choice        MergeChoice `json:"choice"`
	EditingCustom bool        `json:"editingCustom,omitempty"`
	CustomYAML    string      `json:"customYaml,omitempty"`
	CustomErr     string      `json:"customErr,omitempty"`

	customObj     *sceneformat.ObjectDoc
	customDeleted bool
}

// MergeAutoItem describes one scene item joined cleanly by identity in MERGED BY ID.
type MergeAutoItem struct {
	Scope     string `json:"scope"`
	ObjectID  string `json:"objectId,omitempty"`
	Name      string `json:"name"`
	Side      string `json:"side"`
	Action    string `json:"action"`
	SideLabel string `json:"sideLabel"`
}

// MergeSuggestRequest provides full three-way context to Editor.MergeSuggester for one conflicted item.
type MergeSuggestRequest struct {
	ScenePath  string
	Item       MergeConflictItem
	Mine       MergeSideMeta
	Theirs     MergeSideMeta
	BaseDoc    *sceneformat.SceneDoc
	MineDoc    *sceneformat.SceneDoc
	TheirsDoc  *sceneformat.SceneDoc
	MergedDoc  *sceneformat.SceneDoc
	AutoMerged []MergeAutoItem
}

// MergeSuggestion is the agent's suggested resolution for one MergeConflictItem.
type MergeSuggestion struct {
	Meta      string
	YAML      string
	Reason    string
	Deleted   bool
	Object    *sceneformat.ObjectDoc
	FieldSide map[string]MergeChoice
}

// MergePlaytestResult summarizes the short playtest run executed by FinishMerge.
type MergePlaytestResult struct {
	Duration time.Duration `json:"duration"`
	Steps    int           `json:"steps"`
	Summary  string        `json:"summary"`
}

// MergeConflictSessionConfig configures OpenMergeConflictSession for in-memory or fixture merges.
type MergeConflictSessionConfig struct {
	RepoDir     string
	ScenePath   string
	ProjectName string
	SceneName   string
	Mine        MergeSideMeta
	Theirs      MergeSideMeta
	BaseDoc     *sceneformat.SceneDoc
	MineDoc     *sceneformat.SceneDoc
	TheirsDoc   *sceneformat.SceneDoc
}

// MergeConflictSession holds the live state of an object-by-object scene merge conflict screen.
type MergeConflictSession struct {
	RepoDir       string
	ScenePath     string
	ProjectName   string
	SceneName     string
	Mine          MergeSideMeta
	Theirs        MergeSideMeta
	BaseDoc       *sceneformat.SceneDoc
	MineDoc       *sceneformat.SceneDoc
	TheirsDoc     *sceneformat.SceneDoc
	BaseMergedDoc *sceneformat.SceneDoc
	Conflicts     []MergeConflictItem
	AutoMerged    []MergeAutoItem
	FocusedID     string
	Finished      bool
	CommitSHA     string
	VerifySummary string
	VerifyErr     string

	// Cached built scenes for fast 2D preview rendering in MINE, THEIRS and RESULT canvases.
	mineBuilt   *sceneformat.BuiltScene
	theirsBuilt *sceneformat.BuiltScene
	resultDoc   *sceneformat.SceneDoc
	resultBuilt *sceneformat.BuiltScene
	resultDirty bool
}

type mergeConflictState struct {
	open           bool
	session        *MergeConflictSession
	detectedScenes []string
	rev            uint64
}

func (e *Editor) mergeStateRev() uint64 {
	if e == nil {
		return 0
	}
	return e.merge.rev
}

// ErrNoMergeConflict is returned when a merge resolution operation is called without an active conflict session.
var ErrNoMergeConflict = errors.New("editor: no active scene merge conflict session")

// MergeConflictOpen reports whether the Scene Merge Conflict resolution screen is currently active.
func (e *Editor) MergeConflictOpen() bool {
	return e != nil && e.merge.open && e.merge.session != nil
}

// MergeSession returns the active scene merge conflict session, or nil when closed.
func (e *Editor) MergeSession() *MergeConflictSession {
	if e == nil {
		return nil
	}
	return e.merge.session
}

// DetectedMergeConflicts returns the slash-separated paths of unmerged scene files last detected in the open project's Git repository.
func (e *Editor) DetectedMergeConflicts() []string {
	if e == nil || len(e.merge.detectedScenes) == 0 {
		return nil
	}
	return slices.Clone(e.merge.detectedScenes)
}

// CloseMergeConflict closes the Scene Merge Conflict screen without aborting Git state (for example after FinishMerge completes or to inspect the scene).
func (e *Editor) CloseMergeConflict() {
	if e == nil || !e.merge.open {
		return
	}
	e.merge.open = false
	e.merge.rev++
	e.notify()
}

// OpenMergeConflictSession opens the object-by-object Scene Merge Conflict screen from three parsed SceneDoc versions.
func (e *Editor) OpenMergeConflictSession(cfg MergeConflictSessionConfig) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.Playing() {
		return ErrPlaying
	}
	if cfg.MineDoc == nil || cfg.TheirsDoc == nil {
		return errors.New("editor: merge conflict session requires non-nil MineDoc and TheirsDoc")
	}
	res, err := sceneformat.Merge(cfg.BaseDoc, cfg.MineDoc, cfg.TheirsDoc)
	if err != nil {
		return fmt.Errorf("editor: merge scene: %w", err)
	}
	if !res.HasConflicts() {
		return errors.New("editor: scenes merge cleanly with no conflicts")
	}

	mineMeta := cfg.Mine
	if mineMeta.Branch == "" {
		mineMeta.Branch = "HEAD"
	}
	theirsMeta := cfg.Theirs
	if theirsMeta.Branch == "" {
		theirsMeta.Branch = "theirs"
	}

	sceneName := cfg.SceneName
	if sceneName == "" {
		switch {
		case cfg.MineDoc.Name != "":
			sceneName = cfg.MineDoc.Name
		case cfg.ScenePath != "":
			sceneName = trimSceneFilename(cfg.ScenePath)
		default:
			sceneName = "scene"
		}
	}
	projName := cfg.ProjectName
	if projName == "" {
		switch {
		case e.Project != nil && e.Project.Name != "":
			projName = e.Project.Name
		case cfg.RepoDir != "":
			projName = filepath.Base(cfg.RepoDir)
		default:
			projName = "project"
		}
	}

	autoMerged := buildAutoMergedItems(res.Merged, res.Conflicts, cfg.BaseDoc, cfg.MineDoc, cfg.TheirsDoc)
	items := groupMergeConflictItems(res, cfg.BaseDoc, cfg.MineDoc, cfg.TheirsDoc, mineMeta, theirsMeta)

	// Populate agent suggestions (custom MergeSuggester if set, otherwise deterministic analyzer).
	ctx := context.Background()
	for i := range items {
		req := MergeSuggestRequest{
			ScenePath:  cfg.ScenePath,
			Item:       items[i],
			Mine:       mineMeta,
			Theirs:     theirsMeta,
			BaseDoc:    cfg.BaseDoc,
			MineDoc:    cfg.MineDoc,
			TheirsDoc:  cfg.TheirsDoc,
			MergedDoc:  res.Doc,
			AutoMerged: autoMerged,
		}
		var sug MergeSuggestion
		if e.MergeSuggester != nil {
			var serr error
			sug, serr = e.MergeSuggester(ctx, req)
			if serr != nil {
				sug = DefaultMergeSuggestion(req)
			}
		} else {
			sug = DefaultMergeSuggestion(req)
		}
		items[i].Agent = MergeOption{
			Choice:    MergeChoiceAgent,
			Title:     "Agent's suggestion",
			Meta:      sug.Meta,
			YAML:      sug.YAML,
			Reason:    sug.Reason,
			Deleted:   sug.Deleted,
			Object:    mergeCloneObjectDocPtr(sug.Object),
			fieldSide: mapsCloneChoice(sug.FieldSide),
		}
	}

	focusedID := ""
	if len(items) > 0 {
		focusedID = items[0].ID
	}

	sess := &MergeConflictSession{
		RepoDir:       cfg.RepoDir,
		ScenePath:     cfg.ScenePath,
		ProjectName:   projName,
		SceneName:     sceneName,
		Mine:          mineMeta,
		Theirs:        theirsMeta,
		BaseDoc:       cloneSceneDoc(cfg.BaseDoc),
		MineDoc:       cloneSceneDoc(cfg.MineDoc),
		TheirsDoc:     cloneSceneDoc(cfg.TheirsDoc),
		BaseMergedDoc: cloneSceneDoc(res.Doc),
		Conflicts:     items,
		AutoMerged:    autoMerged,
		FocusedID:     focusedID,
		resultDirty:   true,
	}
	sess.rebuildSidePreviews(e.Resolver)
	sess.refreshResultPreview(e.Resolver)

	e.merge.session = sess
	e.merge.open = true
	e.merge.rev++
	e.Status = fmt.Sprintf("Merge conflicts in %s: %d of %d objects need you", nonEmpty(cfg.ScenePath, sceneName), len(items), len(items))
	e.notify()
	return nil
}

// ResolvedCount returns how many conflict items in s currently have a valid resolution chosen.
func (s *MergeConflictSession) ResolvedCount() int {
	if s == nil {
		return 0
	}
	n := 0
	for _, item := range s.Conflicts {
		if item.Choice != MergeChoiceUnresolved {
			n++
		}
	}
	return n
}

// RemainingCount returns how many conflict items in s are still unresolved.
func (s *MergeConflictSession) RemainingCount() int {
	if s == nil {
		return 0
	}
	return len(s.Conflicts) - s.ResolvedCount()
}

// AllResolved reports whether every conflict item has a chosen resolution.
func (s *MergeConflictSession) AllResolved() bool {
	return s != nil && len(s.Conflicts) > 0 && s.RemainingCount() == 0
}

// FocusedItem returns a pointer to the currently focused conflict item, or nil when empty.
func (s *MergeConflictSession) FocusedItem() *MergeConflictItem {
	if s == nil || len(s.Conflicts) == 0 {
		return nil
	}
	for i := range s.Conflicts {
		if s.Conflicts[i].ID == s.FocusedID {
			return &s.Conflicts[i]
		}
	}
	return &s.Conflicts[0]
}

// FocusMergeConflict focuses the conflict item with id.
func (e *Editor) FocusMergeConflict(id string) error {
	if !e.MergeConflictOpen() {
		return ErrNoMergeConflict
	}
	sess := e.merge.session
	for i := range sess.Conflicts {
		if sess.Conflicts[i].ID == id {
			sess.FocusedID = id
			e.merge.rev++
			e.notify()
			return nil
		}
	}
	return fmt.Errorf("editor: unknown merge conflict item %q", id)
}

// StepMergeConflictFocus moves focus by delta (+1 next, -1 previous), wrapping cleanly.
func (e *Editor) StepMergeConflictFocus(delta int) {
	if !e.MergeConflictOpen() || len(e.merge.session.Conflicts) == 0 {
		return
	}
	sess := e.merge.session
	idx := 0
	for i := range sess.Conflicts {
		if sess.Conflicts[i].ID == sess.FocusedID {
			idx = i
			break
		}
	}
	n := len(sess.Conflicts)
	idx = ((idx+delta)%n + n) % n
	sess.FocusedID = sess.Conflicts[idx].ID
	e.merge.rev++
	e.notify()
}

// ResolveMergeConflict picks choice (MergeChoiceAgent, MergeChoiceMine, MergeChoiceTheirs, or MergeChoiceUnresolved) for conflict item id.
func (e *Editor) ResolveMergeConflict(id string, choice MergeChoice) error {
	if !e.MergeConflictOpen() {
		return ErrNoMergeConflict
	}
	switch choice {
	case MergeChoiceUnresolved, MergeChoiceAgent, MergeChoiceMine, MergeChoiceTheirs:
	default:
		return fmt.Errorf("editor: invalid merge choice %q (use ApplyMergeCustomYAML for custom YAML)", choice)
	}
	sess := e.merge.session
	for i := range sess.Conflicts {
		if sess.Conflicts[i].ID == id {
			sess.Conflicts[i].Choice = choice
			sess.Conflicts[i].EditingCustom = false
			sess.Conflicts[i].CustomErr = ""
			sess.Finished = false
			sess.CommitSHA = ""
			sess.VerifyErr = ""
			sess.resultDirty = true
			sess.refreshResultPreview(e.Resolver)
			e.merge.rev++
			e.notify()
			return nil
		}
	}
	return fmt.Errorf("editor: unknown merge conflict item %q", id)
}

// SetMergeAgentSuggestion updates the agent's suggestion for conflict item id (for example from an MCP agent tool).
func (e *Editor) SetMergeAgentSuggestion(id string, sug MergeSuggestion) error {
	if !e.MergeConflictOpen() {
		return ErrNoMergeConflict
	}
	sess := e.merge.session
	for i := range sess.Conflicts {
		if sess.Conflicts[i].ID == id {
			item := &sess.Conflicts[i]
			obj := mergeCloneObjectDocPtr(sug.Object)
			deleted := sug.Deleted
			fieldSide := mapsCloneChoice(sug.FieldSide)
			hasExplicitResolution := sug.Object != nil || sug.Deleted || strings.TrimSpace(sug.YAML) != ""
			if !hasExplicitResolution {
				// Metadata-only update (for example refining Reason or Meta): preserve the existing suggested resolution.
				obj = item.Agent.Object
				deleted = item.Agent.Deleted
				if len(fieldSide) == 0 {
					fieldSide = item.Agent.fieldSide
				}
			} else if obj == nil && !deleted && item.Scope == "object" && strings.TrimSpace(sug.YAML) != "" {
				parsed, del, err := parseCustomObjectYAML(item, sug.YAML, sess.BaseMergedDoc)
				if err != nil {
					return err
				}
				obj = parsed
				deleted = del
			}
			item.Agent = MergeOption{
				Choice:    MergeChoiceAgent,
				Title:     "Agent's suggestion",
				Meta:      nonEmpty(sug.Meta, item.Agent.Meta),
				YAML:      nonEmpty(sug.YAML, item.Agent.YAML),
				Reason:    nonEmpty(sug.Reason, item.Agent.Reason),
				Deleted:   deleted,
				Object:    obj,
				fieldSide: fieldSide,
			}
			if item.Choice == MergeChoiceAgent {
				sess.resultDirty = true
				sess.refreshResultPreview(e.Resolver)
			}
			e.merge.rev++
			e.notify()
			return nil
		}
	}
	return fmt.Errorf("editor: unknown merge conflict item %q", id)
}

// TakeAllAgentMergeSuggestions selects MergeChoiceAgent for every conflict item in the active session.
func (e *Editor) TakeAllAgentMergeSuggestions() error {
	if !e.MergeConflictOpen() {
		return ErrNoMergeConflict
	}
	sess := e.merge.session
	for i := range sess.Conflicts {
		sess.Conflicts[i].Choice = MergeChoiceAgent
		sess.Conflicts[i].EditingCustom = false
		sess.Conflicts[i].CustomErr = ""
	}
	sess.Finished = false
	sess.CommitSHA = ""
	sess.VerifyErr = ""
	sess.resultDirty = true
	sess.refreshResultPreview(e.Resolver)
	e.merge.rev++
	e.notify()
	return nil
}

// SetMergeCustomEditing toggles the inline "Edit the result YAML by hand" editor for conflict item id.
func (e *Editor) SetMergeCustomEditing(id string, open bool) error {
	if !e.MergeConflictOpen() {
		return ErrNoMergeConflict
	}
	sess := e.merge.session
	for i := range sess.Conflicts {
		if sess.Conflicts[i].ID == id {
			item := &sess.Conflicts[i]
			item.EditingCustom = open
			item.CustomErr = ""
			if open && strings.TrimSpace(item.CustomYAML) == "" {
				item.CustomYAML = initialCustomYAMLForItem(item, sess.BaseMergedDoc)
			}
			e.merge.rev++
			e.notify()
			return nil
		}
	}
	return fmt.Errorf("editor: unknown merge conflict item %q", id)
}

// SetMergeCustomYAML updates the live hand-edited YAML text draft for conflict item id without applying it yet.
func (e *Editor) SetMergeCustomYAML(id, raw string) {
	if !e.MergeConflictOpen() {
		return
	}
	sess := e.merge.session
	for i := range sess.Conflicts {
		if sess.Conflicts[i].ID == id {
			sess.Conflicts[i].CustomYAML = raw
			sess.Conflicts[i].CustomErr = ""
			return
		}
	}
}

// ApplyMergeCustomYAML parses, validates, and applies hand-edited result YAML for conflict item id.
func (e *Editor) ApplyMergeCustomYAML(id, raw string) error {
	if !e.MergeConflictOpen() {
		return ErrNoMergeConflict
	}
	sess := e.merge.session
	for i := range sess.Conflicts {
		if sess.Conflicts[i].ID == id {
			item := &sess.Conflicts[i]
			item.CustomYAML = raw
			if item.Scope != "object" {
				err := fmt.Errorf("hand-editing YAML is currently supported for object conflicts; choose Mine, Theirs, or Agent for %s", item.Scope)
				item.CustomErr = err.Error()
				e.merge.rev++
				e.notify()
				return err
			}
			obj, deleted, err := parseCustomObjectYAML(item, raw, sess.BaseMergedDoc)
			if err != nil {
				item.CustomErr = err.Error()
				e.merge.rev++
				e.notify()
				return err
			}
			// Dry-run into the full scene to guarantee sceneformat.Validate passes with this edit.
			prevChoice, prevObj, prevDel := item.Choice, item.customObj, item.customDeleted
			item.Choice = MergeChoiceCustom
			item.customObj = obj
			item.customDeleted = deleted
			doc, rerr := sess.BuildResultDoc()
			if rerr == nil {
				rerr = sceneformat.Validate(doc)
			}
			if rerr != nil {
				item.Choice, item.customObj, item.customDeleted = prevChoice, prevObj, prevDel
				item.CustomErr = rerr.Error()
				e.merge.rev++
				e.notify()
				return rerr
			}
			item.CustomErr = ""
			item.EditingCustom = false
			sess.Finished = false
			sess.CommitSHA = ""
			sess.VerifyErr = ""
			sess.resultDirty = true
			sess.refreshResultPreview(e.Resolver)
			e.merge.rev++
			e.notify()
			return nil
		}
	}
	return fmt.Errorf("editor: unknown merge conflict item %q", id)
}

// ResultDoc returns the current assembled SceneDoc reflecting every resolved conflict choice.
func (s *MergeConflictSession) ResultDoc() (*sceneformat.SceneDoc, error) {
	if s == nil {
		return nil, ErrNoMergeConflict
	}
	if !s.resultDirty && s.resultDoc != nil {
		return s.resultDoc, nil
	}
	doc, err := s.BuildResultDoc()
	if err != nil {
		return nil, err
	}
	s.resultDoc = doc
	s.resultDirty = false
	return doc, nil
}

// BuildResultDoc constructs a fresh SceneDoc by starting from BaseMergedDoc and applying every chosen conflict item.
func (s *MergeConflictSession) BuildResultDoc() (*sceneformat.SceneDoc, error) {
	if s == nil || s.BaseMergedDoc == nil {
		return nil, ErrNoMergeConflict
	}
	doc := s.BaseMergedDoc.Clone()
	for _, item := range s.Conflicts {
		if item.Choice == MergeChoiceUnresolved {
			continue
		}
		if item.Scope == "object" {
			applyObjectResolution(doc, item)
			continue
		}
		applyNonObjectResolution(doc, s.MineDoc, s.TheirsDoc, item)
	}
	return doc, nil
}

func resolveMergeCheckDir(e *Editor, repoDir, absScenePath string) string {
	if e != nil && e.ProjectPath != "" {
		if dir := filepath.Dir(e.ProjectPath); fileExists(filepath.Join(dir, "game.yaml")) {
			return dir
		}
	}
	if absScenePath != "" && repoDir != "" {
		absRepo, err := filepath.Abs(repoDir)
		if err != nil {
			absRepo = repoDir
		}
		if realRepo, err := filepath.EvalSymlinks(absRepo); err == nil {
			absRepo = realRepo
		}
		cur := filepath.Dir(absScenePath)
		for {
			if fileExists(filepath.Join(cur, "game.yaml")) {
				return cur
			}
			if cur == absRepo {
				break
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
			if rel, rerr := filepath.Rel(absRepo, parent); rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				break
			}
			cur = parent
		}
	}
	if repoDir != "" {
		return repoDir
	}
	if e != nil && e.ProjectPath != "" {
		return filepath.Dir(e.ProjectPath)
	}
	return ""
}

// FinishMerge validates and writes the resolved scene, runs game check and a short playtest,
// stages the resolved scene, and commits the merge when backed by Git.
func (e *Editor) FinishMerge() error {
	if !e.MergeConflictOpen() {
		return ErrNoMergeConflict
	}
	if e.Playing() {
		return ErrPlaying
	}
	sess := e.merge.session
	if rem := sess.RemainingCount(); rem > 0 {
		err := fmt.Errorf("editor: %d of %d conflicts are still unresolved", rem, len(sess.Conflicts))
		sess.VerifyErr = err.Error()
		e.merge.rev++
		e.notify()
		return err
	}

	doc, err := sess.BuildResultDoc()
	if err != nil {
		sess.VerifyErr = err.Error()
		e.merge.rev++
		e.notify()
		return err
	}
	if err := sceneformat.Validate(doc); err != nil {
		sess.VerifyErr = "Scene validation failed: " + err.Error()
		e.merge.rev++
		e.notify()
		return fmt.Errorf("editor: resolved scene invalid: %w", err)
	}
	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		sess.VerifyErr = "Encode failed: " + err.Error()
		e.merge.rev++
		e.notify()
		return fmt.Errorf("editor: encode resolved scene: %w", err)
	}

	// Write the resolved scene atomically to disk when backed by a repository/file path.
	var absScenePath, relScenePath string
	var backup []byte
	hadBackup := false
	if sess.RepoDir != "" && sess.ScenePath != "" {
		rel, abs, perr := resolveRepoScenePath(sess.RepoDir, "", sess.ScenePath)
		if perr != nil {
			sess.VerifyErr = "Write scene failed: " + perr.Error()
			e.merge.rev++
			e.notify()
			return fmt.Errorf("editor: write resolved scene: %w", perr)
		}
		relScenePath = rel
		absScenePath = abs
		if prev, rerr := os.ReadFile(absScenePath); rerr == nil {
			backup = prev
			hadBackup = true
		}
		if err := writeFileAtomic(absScenePath, encoded); err != nil {
			sess.VerifyErr = "Write scene failed: " + err.Error()
			e.merge.rev++
			e.notify()
			return fmt.Errorf("editor: write resolved scene: %w", err)
		}
	}
	restoreOnFailure := func() {
		if absScenePath == "" {
			return
		}
		if hadBackup {
			_ = writeFileAtomic(absScenePath, backup)
		} else {
			_ = os.Remove(absScenePath)
		}
	}

	// 1. Run game check on the resolved project/scene.
	checkDir := resolveMergeCheckDir(e, sess.RepoDir, absScenePath)
	var probs []assetcheck.Problem
	if e.MergeCheckRunner != nil {
		probs, err = e.MergeCheckRunner(checkDir, sess.ScenePath, doc.Clone())
	} else if checkDir != "" && fileExists(filepath.Join(checkDir, "game.yaml")) {
		probs, err = runProjectCheck(checkDir, e.components)
	}
	if err != nil {
		restoreOnFailure()
		sess.VerifyErr = "game check error: " + err.Error()
		e.merge.rev++
		e.notify()
		return fmt.Errorf("editor: merge game check: %w", err)
	}
	if len(probs) > 0 {
		restoreOnFailure()
		msg := fmt.Sprintf("game check found %d problem(s): %s (%s)", len(probs), probs[0].Message, probs[0].File)
		sess.VerifyErr = msg
		e.merge.rev++
		e.notify()
		return errors.New("editor: " + msg)
	}

	// 2. Run a short headless playtest on the resolved scene.
	var pt MergePlaytestResult
	if e.MergePlaytestRunner != nil {
		pt, err = e.MergePlaytestRunner(checkDir, sess.ScenePath, doc.Clone())
	} else {
		pt, err = e.runDefaultMergePlaytest(doc)
	}
	if err != nil {
		restoreOnFailure()
		sess.VerifyErr = "Playtest failed: " + err.Error()
		e.merge.rev++
		e.notify()
		return fmt.Errorf("editor: merge playtest: %w", err)
	}

	// 3. Stage and commit in Git when inside an active Git merge.
	commitSHA := ""
	if sess.RepoDir != "" && relScenePath != "" {
		sha, staged, gerr := e.stageAndCommitMerge(sess.RepoDir, relScenePath)
		if gerr != nil {
			if !staged {
				restoreOnFailure()
			}
			sess.VerifyErr = "Git commit failed: " + gerr.Error()
			e.merge.rev++
			e.notify()
			return gerr
		}
		commitSHA = sha
	}

	// Update session and reload the resolved scene into the editor.
	sess.Finished = true
	sess.CommitSHA = commitSHA
	sess.VerifyErr = ""
	playtestDesc := pt.Summary
	if playtestDesc == "" {
		secs := int(math.Round(pt.Duration.Seconds()))
		if secs <= 0 {
			secs = 10
		}
		playtestDesc = fmt.Sprintf("Ran game check and a %d s playtest on the result: no problems.", secs)
	}
	sess.VerifySummary = playtestDesc
	sess.resultDoc = doc
	sess.resultDirty = false
	sess.refreshResultPreview(e.Resolver)

	// Synchronize the editor's active scene with the resolved result.
	e.setDoc(doc.Clone())
	if absScenePath != "" {
		e.FilePath = absScenePath
		e.syncScenePath()
	}
	e.sceneSaved = encoded
	e.History.Clear()
	e.Rebuild()
	e.filesRev++
	e.invalidateProjectIndex()
	e.refreshDetectedMergeConflicts()
	e.CheckProblems()

	if commitSHA != "" {
		e.Status = "Merged as " + commitSHA
	} else {
		e.Status = "Merged scene resolved and verified"
	}
	e.merge.rev++
	e.scheduleAutoSaveIfNeeded()
	e.notify()
	return nil
}

// AbortMerge aborts the active Git merge (restoring the branch and working tree as they were)
// and closes the Scene Merge Conflict screen.
func (e *Editor) AbortMerge() error {
	if !e.MergeConflictOpen() {
		return ErrNoMergeConflict
	}
	if e.Playing() {
		return ErrPlaying
	}
	sess := e.merge.session
	if sess.RepoDir != "" {
		if err := e.abortGitMerge(sess.RepoDir); err != nil {
			sess.VerifyErr = "Abort merge failed: " + err.Error()
			e.merge.rev++
			e.notify()
			return err
		}
	}
	scenePath := sess.ScenePath
	repoDir := sess.RepoDir

	e.merge.open = false
	e.merge.session = nil
	e.merge.rev++
	e.refreshDetectedMergeConflicts()

	// Reload the restored scene file from disk if present.
	if repoDir != "" && scenePath != "" {
		if _, abs, perr := resolveRepoScenePath(repoDir, "", scenePath); perr == nil {
			if data, err := os.ReadFile(abs); err == nil {
				if doc, perr := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: abs, Components: e.components}); perr == nil {
					e.setDoc(doc)
					e.FilePath = abs
					e.syncScenePath()
					e.sceneSaved, _ = sceneformat.Encode(doc)
					e.History.Clear()
					e.Rebuild()
				}
			}
		}
	}

	e.filesRev++
	e.invalidateProjectIndex()
	e.CheckProblems()
	e.scheduleAutoSaveIfNeeded()
	e.Status = "Merge aborted"
	e.notify()
	return nil
}

// runDefaultMergePlaytest builds doc, initializes any 2D/3D physics scene, and steps a short
// deterministic 10-second (600-step at 60 Hz) simulation to verify runtime health.
func (e *Editor) runDefaultMergePlaytest(doc *sceneformat.SceneDoc) (MergePlaytestResult, error) {
	const (
		steps = 600
		dt    = time.Second / 60
	)
	built, err := sceneformat.Build(doc.Clone(), e.Resolver)
	if err != nil {
		return MergePlaytestResult{}, fmt.Errorf("build scene: %w", err)
	}
	phys, err := sceneformat.BuildPhysics(built, e.physicsSettings(), e.Resolver)
	if err != nil {
		return MergePlaytestResult{}, fmt.Errorf("init physics: %w", err)
	}
	for range steps {
		built.Update(dt)
		if phys != nil {
			phys.Step(dt)
		}
	}
	// Verify no object ended up with non-finite coordinates (deterministic ID order).
	for _, id := range mergeSortedMapKeys(built.Objects) {
		bo := built.Objects[id]
		if bo == nil || bo.Doc == nil {
			continue
		}
		if math.IsNaN(float64(bo.Doc.X)) || math.IsInf(float64(bo.Doc.X), 0) ||
			math.IsNaN(float64(bo.Doc.Y)) || math.IsInf(float64(bo.Doc.Y), 0) ||
			math.IsNaN(float64(bo.Doc.PosZ)) || math.IsInf(float64(bo.Doc.PosZ), 0) {
			return MergePlaytestResult{}, fmt.Errorf("object %q reached non-finite position during playtest", id)
		}
	}
	return MergePlaytestResult{
		Duration: 10 * time.Second,
		Steps:    steps,
		Summary:  fmt.Sprintf("Ran game check and a 10 s playtest on the result: no problems, %d objects verified.", len(built.Objects)),
	}, nil
}

// --- Conflict grouping, summaries, YAML formatting, and deterministic Agent suggester ---

type objectPlacement struct {
	obj    *sceneformat.ObjectDoc
	layer  string
	parent string
}

func indexObjects(doc *sceneformat.SceneDoc) map[string]objectPlacement {
	out := make(map[string]objectPlacement)
	if doc == nil {
		return out
	}
	var walk func(layer, parent string, objs []sceneformat.ObjectDoc)
	walk = func(layer, parent string, objs []sceneformat.ObjectDoc) {
		for i := range objs {
			o := &objs[i]
			out[o.ID] = objectPlacement{obj: o, layer: layer, parent: parent}
			if len(o.Children) > 0 {
				walk(layer, o.ID, o.Children)
			}
		}
	}
	for i := range doc.Layers {
		walk(doc.Layers[i].Name, "", doc.Layers[i].Objects)
	}
	return out
}

func groupMergeConflictItems(
	res *sceneformat.MergeResult,
	baseDoc, mineDoc, theirsDoc *sceneformat.SceneDoc,
	mineMeta, theirsMeta MergeSideMeta,
) []MergeConflictItem {
	baseIdx := indexObjects(baseDoc)
	mineIdx := indexObjects(mineDoc)
	theirsIdx := indexObjects(theirsDoc)
	mergedIdx := indexObjects(res.Doc)

	type group struct {
		key       string
		scope     string
		layer     string
		objectID  string
		target    string
		conflicts []sceneformat.MergeConflict
	}
	var order []string
	groups := make(map[string]*group)
	for _, c := range res.Conflicts {
		key := conflictGroupKey(c)
		g, ok := groups[key]
		if !ok {
			g = &group{
				key:      key,
				scope:    c.Scope,
				layer:    c.Layer,
				objectID: c.Object,
				target:   c.Target,
			}
			groups[key] = g
			order = append(order, key)
		}
		g.conflicts = append(g.conflicts, c)
	}

	items := make([]MergeConflictItem, 0, len(order))
	for _, key := range order {
		g := groups[key]
		item := MergeConflictItem{
			ID:        conflictItemID(g.scope, g.objectID, g.target, g.layer),
			Scope:     g.scope,
			Layer:     g.layer,
			ObjectID:  g.objectID,
			Target:    g.target,
			Conflicts: g.conflicts,
		}
		if g.scope == "object" {
			bp := baseIdx[g.objectID]
			mp := mineIdx[g.objectID]
			tp := theirsIdx[g.objectID]
			item.BaseObj = mergeCloneObjectDocPtr(bp.obj)
			item.MineObj = mergeCloneObjectDocPtr(mp.obj)
			item.TheirsObj = mergeCloneObjectDocPtr(tp.obj)
			item.MineLayer, item.MineParent = mp.layer, mp.parent
			item.TheirsLayer, item.TheirsParent = tp.layer, tp.parent
			if item.Layer == "" {
				item.Layer = nonEmpty(mp.layer, tp.layer, bp.layer)
			}

			refObj := firstNonNilObj(mp.obj, tp.obj, bp.obj, mergedIdx[g.objectID].obj)
			item.Name = objectDisplayName(refObj, g.objectID)
			item.Kind = objectKindLabel(refObj)
			item.Summary = summarizeObjectConflicts(item, theirsMeta)
			item.Mine = buildSideObjectOption(MergeChoiceMine, "Mine", item, bp.obj, mp.obj, mineMeta)
			item.Theirs = buildSideObjectOption(MergeChoiceTheirs, "Theirs", item, bp.obj, tp.obj, theirsMeta)
		} else {
			item.Name = nonEmpty(g.target, g.layer, g.scope)
			item.Kind = g.scope
			item.Summary = summarizeNonObjectConflicts(g.conflicts, theirsMeta)
			item.Mine = buildSideNonObjectOption(MergeChoiceMine, "Mine", g.conflicts)
			item.Theirs = buildSideNonObjectOption(MergeChoiceTheirs, "Theirs", g.conflicts)
		}
		items = append(items, item)
	}
	return items
}

func conflictGroupKey(c sceneformat.MergeConflict) string {
	switch c.Scope {
	case "object":
		return "object:" + c.Object
	case "layer":
		return "layer:" + nonEmpty(c.Layer, c.Target)
	case "scene":
		return "scene"
	default:
		return c.Scope + ":" + nonEmpty(c.Target, c.Layer, c.Field)
	}
}

func conflictItemID(scope, objectID, target, layer string) string {
	if scope == "object" && objectID != "" {
		return objectID
	}
	if target != "" {
		return scope + ":" + target
	}
	if layer != "" {
		return scope + ":" + layer
	}
	return scope
}

func firstNonNilObj(objs ...*sceneformat.ObjectDoc) *sceneformat.ObjectDoc {
	for _, o := range objs {
		if o != nil {
			return o
		}
	}
	return nil
}

func objectDisplayName(o *sceneformat.ObjectDoc, fallback string) string {
	if o != nil && strings.TrimSpace(o.Name) != "" {
		return o.Name
	}
	return fallback
}

func objectKindLabel(o *sceneformat.ObjectDoc) string {
	if o == nil {
		return "object"
	}
	var parts []string
	if o.Components != nil {
		if _, ok := o.Components["trigger"]; ok {
			parts = append(parts, "trigger")
		} else if _, ok := o.Components["collider"]; ok {
			parts = append(parts, "collider")
		} else if _, ok := o.Components["body"]; ok {
			parts = append(parts, "body")
		}
	}
	if o.Prefab != "" {
		base := path.Base(o.Prefab)
		base = strings.TrimSuffix(base, ".prefab.yaml")
		base = strings.TrimSuffix(base, ".prefab.yml")
		base = strings.TrimSuffix(base, ".yaml")
		parts = append(parts, "prefab "+base)
	} else if o.Scene != "" {
		parts = append(parts, "scene instance")
	} else if o.Model != "" {
		parts = append(parts, "model")
	} else if o.Mesh != "" {
		parts = append(parts, "mesh "+o.Mesh)
	} else if o.Sprite != nil {
		parts = append(parts, "sprite")
	}
	if len(parts) == 0 {
		return "object"
	}
	return strings.Join(parts, " · ")
}

func shortBranchLabel(branch string) string {
	branch = strings.TrimPrefix(branch, "origin/")
	if branch == "" {
		return "theirs"
	}
	return branch
}

func summarizeObjectConflicts(item MergeConflictItem, theirs MergeSideMeta) string {
	tb := shortBranchLabel(theirs.Branch)
	for _, c := range item.Conflicts {
		if c.Kind == sceneformat.ConflictDeleteModify {
			if c.Theirs == nil && c.Ours != nil {
				changed := firstChangedFieldName(item.BaseObj, item.MineObj)
				if changed != "" {
					return fmt.Sprintf("Your %s change; %s deleted it", changed, tb)
				}
				return fmt.Sprintf("Changed here; %s deleted it", tb)
			}
			if c.Ours == nil && c.Theirs != nil {
				changed := firstChangedFieldName(item.BaseObj, item.TheirsObj)
				if changed != "" {
					return fmt.Sprintf("Deleted here; %s changed %s", tb, changed)
				}
				return fmt.Sprintf("Deleted here; %s modified it", tb)
			}
		}
		if c.Kind == sceneformat.ConflictMove {
			return fmt.Sprintf("Both moved it to different parents (%s vs %s)", formatScalar(c.Ours), formatScalar(c.Theirs))
		}
	}
	if len(item.Conflicts) == 1 {
		c := item.Conflicts[0]
		switch c.Field {
		case "x", "y":
			return fmt.Sprintf("Both moved it: %s %s here, %s %s on %s", c.Field, formatScalar(c.Ours), c.Field, formatScalar(c.Theirs), tb)
		default:
			return fmt.Sprintf("Both changed %s: %s here, %s on %s", c.Field, formatScalar(c.Ours), formatScalar(c.Theirs), tb)
		}
	}
	// Multiple conflicting fields on the same object.
	hasX, hasY := false, false
	var fields []string
	for _, c := range item.Conflicts {
		if c.Field == "x" {
			hasX = true
		}
		if c.Field == "y" {
			hasY = true
		}
		if c.Field != "" {
			fields = append(fields, c.Field)
		}
	}
	if hasX && hasY && len(fields) == 2 && item.MineObj != nil && item.TheirsObj != nil {
		return fmt.Sprintf("Both moved it: (%s, %s) here, (%s, %s) on %s",
			formatFloat(float64(item.MineObj.X)), formatFloat(float64(item.MineObj.Y)),
			formatFloat(float64(item.TheirsObj.X)), formatFloat(float64(item.TheirsObj.Y)), tb)
	}
	return fmt.Sprintf("Both changed %s", strings.Join(fields, ", "))
}

func summarizeNonObjectConflicts(conflicts []sceneformat.MergeConflict, theirs MergeSideMeta) string {
	tb := shortBranchLabel(theirs.Branch)
	if len(conflicts) == 1 {
		c := conflicts[0]
		if c.Message != "" {
			return c.Message
		}
		if c.Field != "" {
			return fmt.Sprintf("Both changed %s: %s here, %s on %s", c.Field, formatScalar(c.Ours), formatScalar(c.Theirs), tb)
		}
		return c.Summary()
	}
	return fmt.Sprintf("%d conflicting fields between here and %s", len(conflicts), tb)
}

func buildSideObjectOption(
	choice MergeChoice,
	title string,
	item MergeConflictItem,
	baseObj, sideObj *sceneformat.ObjectDoc,
	sideMeta MergeSideMeta,
) MergeOption {
	if sideObj == nil {
		var lines []string
		if sideMeta.Commit != "" {
			lines = append(lines, fmt.Sprintf("# %s deleted in %s", item.ObjectID, sideMeta.Commit))
		} else {
			lines = append(lines, fmt.Sprintf("# %s deleted", item.ObjectID))
		}
		if sideMeta.Message != "" {
			lines = append(lines, fmt.Sprintf("# %q", sideMeta.Message))
		}
		return MergeOption{
			Choice:  choice,
			Title:   title,
			Meta:    "deleted",
			YAML:    strings.Join(lines, "\n"),
			Deleted: true,
		}
	}
	meta := summarizeSideDelta(baseObj, sideObj, item.Conflicts, choice)
	snippet := formatChangedObjectYAML(item.ObjectID, baseObj, sideObj, item.Conflicts, choice)
	return MergeOption{
		Choice: choice,
		Title:  title,
		Meta:   meta,
		YAML:   snippet,
		Object: mergeCloneObjectDocPtr(sideObj),
	}
}

func buildSideNonObjectOption(choice MergeChoice, title string, conflicts []sceneformat.MergeConflict) MergeOption {
	var lines []string
	var metaParts []string
	for _, c := range conflicts {
		val := c.Ours
		if choice == MergeChoiceTheirs {
			val = c.Theirs
		}
		field := nonEmpty(c.Field, c.Target, c.Scope)
		lines = append(lines, fmt.Sprintf("%s: %s", field, formatScalar(val)))
		if len(metaParts) < 2 {
			metaParts = append(metaParts, fmt.Sprintf("%s %s → %s", field, formatScalar(c.Base), formatScalar(val)))
		}
	}
	return MergeOption{
		Choice: choice,
		Title:  title,
		Meta:   strings.Join(metaParts, ", "),
		YAML:   strings.Join(lines, "\n"),
	}
}

// DefaultMergeSuggestion produces a deterministic, reasoned suggestion for req without requiring an external LLM.
func DefaultMergeSuggestion(req MergeSuggestRequest) MergeSuggestion {
	item := req.Item
	if item.Scope != "object" {
		// Default to mine unless theirs has a non-nil value and mine is nil.
		pick := MergeChoiceMine
		meta := "same as mine"
		reason := "Keeps your branch's value while preserving every non-conflicting scene change."
		if len(item.Conflicts) > 0 && item.Conflicts[0].Ours == nil && item.Conflicts[0].Theirs != nil {
			pick = MergeChoiceTheirs
			meta = "same as theirs"
			reason = "Takes the incoming branch's value because your branch cleared this entry."
		}
		opt := item.Mine
		if pick == MergeChoiceTheirs {
			opt = item.Theirs
		}
		return MergeSuggestion{
			Meta:   meta,
			YAML:   opt.YAML,
			Reason: reason,
		}
	}

	// Case 1: Delete-modify conflict.
	if item.MineObj == nil || item.TheirsObj == nil {
		if item.TheirsObj == nil && item.MineObj != nil {
			who := nonEmpty(req.Theirs.Author, shortBranchLabel(req.Theirs.Branch))
			changed := nonEmpty(firstChangedFieldName(item.BaseObj, item.MineObj), "property")
			reason := fmt.Sprintf("%s removed %s", who, item.Name)
			if req.Theirs.Message != "" {
				reason += fmt.Sprintf(" (%s)", req.Theirs.Message)
			}
			reason += fmt.Sprintf("; keeping the deletion avoids reintroducing a removed object, and your %s change has nothing left to apply to.", changed)
			return MergeSuggestion{
				Meta:    "same as theirs",
				YAML:    fmt.Sprintf("# %s stays deleted", item.ObjectID),
				Reason:  reason,
				Deleted: true,
			}
		}
		who := nonEmpty(req.Mine.Author, shortBranchLabel(req.Mine.Branch))
		reason := fmt.Sprintf("%s removed %s; keeping the deletion avoids restoring an object your branch removed.", who, item.Name)
		return MergeSuggestion{
			Meta:    "same as mine",
			YAML:    fmt.Sprintf("# %s stays deleted", item.ObjectID),
			Reason:  reason,
			Deleted: true,
		}
	}

	// Case 2: Both sides exist — start from BaseMergedDoc's merged object (which already carries
	// every non-conflicting field from BOTH mine and theirs!).
	mergedIdx := indexObjects(req.MergedDoc)
	suggestedObj := mergeCloneObjectDocPtr(mergedIdx[item.ObjectID].obj)
	if suggestedObj == nil {
		suggestedObj = mergeCloneObjectDocPtr(item.MineObj)
	}

	mineUnique := uniqueChangedFields(item.BaseObj, item.MineObj, item.TheirsObj)
	theirsUnique := uniqueChangedFields(item.BaseObj, item.TheirsObj, item.MineObj)
	combinedFromBoth := len(mineUnique) > 0 && len(theirsUnique) > 0

	// Decide each conflicting field on the object.
	theirsAddedIDs := addedObjectIDSet(req.AutoMerged, "theirs")
	theirsReferencesAdded := objectReferencesAnyID(item.TheirsObj, theirsAddedIDs)
	mineAddedIDs := addedObjectIDSet(req.AutoMerged, "ours")
	mineReferencesAdded := objectReferencesAnyID(item.MineObj, mineAddedIDs)

	choseMine, choseTheirs := false, false
	var reasons []string
	var fieldSides = make(map[string]MergeChoice)

	for _, c := range item.Conflicts {
		if c.Kind != sceneformat.ConflictField {
			continue
		}
		switch {
		case c.Field == "z":
			// Higher z keeps the object above both layers/thresholds.
			if item.TheirsObj.Z > item.MineObj.Z {
				suggestedObj.Z = item.TheirsObj.Z
				fieldSides["z"] = MergeChoiceTheirs
				choseTheirs = true
				reasons = append(reasons, fmt.Sprintf("z %d keeps %s above both targets (%d and %d).", item.TheirsObj.Z, item.Name, item.MineObj.Z, item.TheirsObj.Z))
			} else {
				suggestedObj.Z = item.MineObj.Z
				fieldSides["z"] = MergeChoiceMine
				choseMine = true
				reasons = append(reasons, fmt.Sprintf("z %d keeps %s above both targets (%d and %d).", item.MineObj.Z, item.Name, item.MineObj.Z, item.TheirsObj.Z))
			}
		case (c.Field == "x" || c.Field == "y") && theirsReferencesAdded && !mineReferencesAdded:
			// Theirs ties this object to a newly added object on their branch (such as a lever puzzle).
			if c.Field == "x" {
				suggestedObj.X = item.TheirsObj.X
			} else {
				suggestedObj.Y = item.TheirsObj.Y
			}
			fieldSides[c.Field] = MergeChoiceTheirs
			choseTheirs = true
		case (c.Field == "x" || c.Field == "y") && mineReferencesAdded && !theirsReferencesAdded:
			if c.Field == "x" {
				suggestedObj.X = item.MineObj.X
			} else {
				suggestedObj.Y = item.MineObj.Y
			}
			fieldSides[c.Field] = MergeChoiceMine
			choseMine = true
		case len(theirsUnique) > len(mineUnique):
			applyConflictingFieldFrom(suggestedObj, item.TheirsObj, c.Field)
			fieldSides[c.Field] = MergeChoiceTheirs
			choseTheirs = true
		default:
			applyConflictingFieldFrom(suggestedObj, item.MineObj, c.Field)
			fieldSides[c.Field] = MergeChoiceMine
			choseMine = true
		}
	}

	var meta, reason string
	if combinedFromBoth || (choseMine && choseTheirs) {
		meta = "fields from both"
		if theirsReferencesAdded {
			who := nonEmpty(req.Theirs.Author, shortBranchLabel(req.Theirs.Branch))
			reason = fmt.Sprintf("Keeps %s's position and %s while preserving your %s from this branch.",
				who, strings.Join(theirsUnique, ", "), strings.Join(mineUnique, ", "))
		} else if len(reasons) > 0 {
			reason = strings.Join(reasons, " ") + fmt.Sprintf(" Combines non-conflicting fields from both sides (%s and %s).",
				strings.Join(mineUnique, ", "), strings.Join(theirsUnique, ", "))
		} else {
			reason = fmt.Sprintf("Combines non-conflicting fields from both sides (%s from mine, %s from theirs).",
				strings.Join(mineUnique, ", "), strings.Join(theirsUnique, ", "))
		}
	} else if choseTheirs && !choseMine {
		meta = "same as theirs"
		if len(reasons) > 0 {
			reason = strings.Join(reasons, " ")
		} else {
			reason = fmt.Sprintf("Takes %s's version of %s, which also carries its related edits.", shortBranchLabel(req.Theirs.Branch), item.Name)
		}
	} else {
		meta = "same as mine"
		if len(reasons) > 0 {
			reason = strings.Join(reasons, " ")
		} else {
			reason = fmt.Sprintf("Keeps your branch's version of %s while preserving all other auto-merged scene changes.", item.Name)
		}
	}

	snippet := formatSuggestedObjectYAML(item.ObjectID, item.BaseObj, item.MineObj, item.TheirsObj, suggestedObj, item.Conflicts, fieldSides)
	return MergeSuggestion{
		Meta:      meta,
		YAML:      snippet,
		Reason:    reason,
		Object:    suggestedObj,
		FieldSide: fieldSides,
	}
}

func buildAutoMergedItems(merged []sceneformat.MergedItem, conflicts []sceneformat.MergeConflict, baseDoc, mineDoc, theirsDoc *sceneformat.SceneDoc) []MergeAutoItem {
	if len(merged) == 0 {
		return nil
	}
	conflicted := make(map[string]bool, len(conflicts))
	for _, c := range conflicts {
		conflicted[conflictGroupKey(c)] = true
	}
	baseIdx := indexObjects(baseDoc)
	mineIdx := indexObjects(mineDoc)
	theirsIdx := indexObjects(theirsDoc)

	out := make([]MergeAutoItem, 0, len(merged))
	for _, m := range merged {
		if m.Scope == "scene" && m.Target == "" {
			continue
		}
		if conflicted[m.Scope+":"+m.Target] {
			continue
		}
		name := nonEmpty(m.Target, m.Scope)
		objID := ""
		if m.Scope == "object" {
			objID = m.Target
			if p, ok := mineIdx[m.Target]; ok && p.obj != nil && p.obj.Name != "" {
				name = p.obj.Name
			} else if p, ok := theirsIdx[m.Target]; ok && p.obj != nil && p.obj.Name != "" {
				name = p.obj.Name
			} else if p, ok := baseIdx[m.Target]; ok && p.obj != nil && p.obj.Name != "" {
				name = p.obj.Name
			}
		}
		sideName := m.Side
		if sideName == "ours" {
			sideName = "mine"
		}
		detail := m.Action
		if m.Scope == "object" && m.Action == "changed" {
			var sideObj *sceneformat.ObjectDoc
			if m.Side == "ours" {
				sideObj = mineIdx[m.Target].obj
			} else if m.Side == "theirs" {
				sideObj = theirsIdx[m.Target].obj
			}
			if delta := shortObjectDelta(baseIdx[m.Target].obj, sideObj); delta != "" {
				detail = delta
			}
		}
		out = append(out, MergeAutoItem{
			Scope:     m.Scope,
			ObjectID:  objID,
			Name:      name,
			Side:      m.Side,
			Action:    m.Action,
			SideLabel: sideName + " · " + detail,
		})
	}
	return out
}

// --- Resolution application helpers ---

func applyObjectResolution(doc *sceneformat.SceneDoc, item MergeConflictItem) {
	if doc == nil || item.ObjectID == "" {
		return
	}
	var chosen *sceneformat.ObjectDoc
	var deleted bool
	var targetLayer, targetParent string

	switch item.Choice {
	case MergeChoiceMine:
		chosen = mergeCloneObjectDocPtr(item.MineObj)
		deleted = chosen == nil
		targetLayer, targetParent = item.MineLayer, item.MineParent
	case MergeChoiceTheirs:
		chosen = mergeCloneObjectDocPtr(item.TheirsObj)
		deleted = chosen == nil
		targetLayer, targetParent = item.TheirsLayer, item.TheirsParent
	case MergeChoiceAgent:
		deleted = item.Agent.Deleted || item.Agent.Object == nil
		if !deleted {
			chosen = mergeCloneObjectDocPtr(item.Agent.Object)
		}
		targetLayer, targetParent = nonEmpty(item.MineLayer, item.TheirsLayer, item.Layer), nonEmpty(item.MineParent, item.TheirsParent)
	case MergeChoiceCustom:
		deleted = item.customDeleted || item.customObj == nil
		if !deleted {
			chosen = mergeCloneObjectDocPtr(item.customObj)
		}
		targetLayer, targetParent = nonEmpty(item.MineLayer, item.TheirsLayer, item.Layer), nonEmpty(item.MineParent, item.TheirsParent)
	default:
		return
	}

	if deleted {
		removeObjectFromDoc(doc, item.ObjectID)
		// Also prune connections referencing the deleted object so sceneformat.Validate stays clean.
		doc.Connections = slices.DeleteFunc(doc.Connections, func(c sceneformat.ConnectionDoc) bool {
			return c.From == item.ObjectID || c.To == item.ObjectID
		})
		return
	}

	// Preserve existing merged children when replacing an existing object's fields.
	if existing, _, _ := findObjectInDoc(doc, item.ObjectID); existing != nil {
		if len(chosen.Children) == 0 && len(existing.Children) > 0 {
			chosen.Children = existing.Children
		}
		*existing = *chosen
		// Handle move conflict if target layer/parent differs.
		if hasMoveConflict(item.Conflicts) && targetLayer != "" {
			moveObjectInDoc(doc, item.ObjectID, targetLayer, targetParent)
		}
		return
	}

	// Object was absent in BaseMergedDoc (e.g. restoring after delete-modify): insert it into targetLayer/targetParent.
	insertObjectInDoc(doc, chosen, targetLayer, targetParent)
}

func mergeCloneTilemapDoc(t *sceneformat.TilemapDoc) *sceneformat.TilemapDoc {
	if t == nil {
		return nil
	}
	cp := *t
	cp.Data = slices.Clone(t.Data)
	if len(t.TileLayers) > 0 {
		cp.TileLayers = make([]sceneformat.TileLayerDoc, len(t.TileLayers))
		for i, tl := range t.TileLayers {
			cp.TileLayers[i] = sceneformat.TileLayerDoc{
				Name:    tl.Name,
				Visible: cloneBoolPtr(tl.Visible),
				Opacity: cloneFloat32Ptr(tl.Opacity),
				Rows:    slices.Clone(tl.Rows),
			}
		}
	}
	return &cp
}

func mergeCloneLayerOverride(lo sceneformat.LayerOverride) sceneformat.LayerOverride {
	return sceneformat.LayerOverride{
		Space:        lo.Space,
		Order:        clonePtr(lo.Order),
		Visible:      cloneBoolPtr(lo.Visible),
		ParallaxX:    cloneFloat32Ptr(lo.ParallaxX),
		ParallaxY:    cloneFloat32Ptr(lo.ParallaxY),
		RepeatX:      cloneBoolPtr(lo.RepeatX),
		RepeatY:      cloneBoolPtr(lo.RepeatY),
		RepeatWidth:  cloneFloat32Ptr(lo.RepeatWidth),
		RepeatHeight: cloneFloat32Ptr(lo.RepeatHeight),
		Tilemap:      mergeCloneTilemapDoc(lo.Tilemap),
	}
}

func mergeCloneObjectOverride(oo sceneformat.ObjectOverride) sceneformat.ObjectOverride {
	return sceneformat.ObjectOverride{
		Name:       clonePtr(oo.Name),
		Prefab:     clonePtr(oo.Prefab),
		X:          cloneFloat32Ptr(oo.X),
		Y:          cloneFloat32Ptr(oo.Y),
		PosZ:       cloneFloat32Ptr(oo.PosZ),
		OriginX:    cloneFloat32Ptr(oo.OriginX),
		OriginY:    cloneFloat32Ptr(oo.OriginY),
		Rotation:   cloneFloat32Ptr(oo.Rotation),
		RotX:       cloneFloat32Ptr(oo.RotX),
		RotY:       cloneFloat32Ptr(oo.RotY),
		RotZ:       cloneFloat32Ptr(oo.RotZ),
		ScaleX:     cloneFloat32Ptr(oo.ScaleX),
		ScaleY:     cloneFloat32Ptr(oo.ScaleY),
		ScaleZ:     cloneFloat32Ptr(oo.ScaleZ),
		Depth:      cloneFloat32Ptr(oo.Depth),
		Mesh:       clonePtr(oo.Mesh),
		Model:      clonePtr(oo.Model),
		ModelNode:  clonePtr(oo.ModelNode),
		ModelMesh:  clonePtr(oo.ModelMesh),
		GridMap:    clonePtr(oo.GridMap),
		Occluder:   cloneOccluderDoc(oo.Occluder),
		FlipX:      cloneBoolPtr(oo.FlipX),
		FlipY:      cloneBoolPtr(oo.FlipY),
		Alpha:      cloneFloat32Ptr(oo.Alpha),
		Z:          clonePtr(oo.Z),
		Sprite:     cloneSprite(oo.Sprite),
		Material:   clonePtr(oo.Material),
		Particles:  cloneParticlesRef(oo.Particles),
		Light:      oo.Light.Clone(),
		Camera:     oo.Camera.Clone(),
		Components: cloneMap(oo.Components),
		Properties: cloneMap(oo.Properties),
	}
}

func applyTilemapFieldFrom(dst **sceneformat.TilemapDoc, src *sceneformat.TilemapDoc, field string) {
	if field == "" || src == nil || *dst == nil {
		*dst = mergeCloneTilemapDoc(src)
		return
	}
	switch field {
	case "ref":
		(*dst).Ref = src.Ref
	case "source":
		(*dst).Source = src.Source
	case "level":
		(*dst).Level = src.Level
	case "tileWidth":
		(*dst).TileWidth = src.TileWidth
	case "tileHeight":
		(*dst).TileHeight = src.TileHeight
	case "width":
		(*dst).Width = src.Width
	case "height":
		(*dst).Height = src.Height
	case "tileset":
		(*dst).Tileset = src.Tileset
	case "tilesetFile":
		(*dst).TilesetFile = src.TilesetFile
	case "data":
		(*dst).Data = slices.Clone(src.Data)
	case "tileLayers":
		if cp := mergeCloneTilemapDoc(src); cp != nil {
			(*dst).TileLayers = cp.TileLayers
		} else {
			(*dst).TileLayers = nil
		}
	}
}

func applyAnyMapPathFrom(dst *map[string]any, src map[string]any, keyPath string) {
	if keyPath == "" {
		*dst = cloneMap(src)
		return
	}
	parts := strings.Split(keyPath, ".")
	if len(parts) == 1 {
		key := parts[0]
		if srcVal, ok := src[key]; ok {
			if *dst == nil {
				*dst = make(map[string]any)
			}
			(*dst)[key] = cloneAny(srcVal)
		} else if *dst != nil {
			delete(*dst, key)
			if len(*dst) == 0 {
				*dst = nil
			}
		}
		return
	}
	// Walk nested map[string]any along parts.
	var srcCur any = src
	srcFound := true
	for _, p := range parts {
		sm, ok := srcCur.(map[string]any)
		if !ok {
			srcFound = false
			break
		}
		val, exists := sm[p]
		if !exists {
			srcFound = false
			break
		}
		srcCur = val
	}
	if !srcFound {
		if *dst == nil {
			return
		}
		cur := *dst
		for _, p := range parts[:len(parts)-1] {
			next, ok := cur[p].(map[string]any)
			if !ok {
				return
			}
			cur = next
		}
		delete(cur, parts[len(parts)-1])
		return
	}
	if *dst == nil {
		*dst = make(map[string]any)
	}
	cur := *dst
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = make(map[string]any)
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = cloneAny(srcCur)
}

func applySpriteFieldFrom(dst **sceneformat.SpriteRef, src *sceneformat.SpriteRef, sub string) {
	if sub == "" || src == nil || *dst == nil {
		*dst = cloneSprite(src)
		return
	}
	switch sub {
	case "atlas":
		(*dst).Atlas = src.Atlas
	case "frame":
		(*dst).Frame = src.Frame
	}
}

func applyParticlesFieldFrom(dst **sceneformat.ParticlesRef, src *sceneformat.ParticlesRef, sub string) {
	if sub == "" || src == nil || *dst == nil {
		*dst = cloneParticlesRef(src)
		return
	}
	switch sub {
	case "file":
		(*dst).File = src.File
	case "atlas":
		(*dst).Atlas = src.Atlas
	}
}

func applyOccluderFieldFrom(dst **sceneformat.OccluderDoc, src *sceneformat.OccluderDoc, sub string) {
	if sub == "" || src == nil || *dst == nil {
		*dst = cloneOccluderDoc(src)
		return
	}
	switch sub {
	case "center":
		(*dst).Center = clonePtr(src.Center)
	case "size":
		(*dst).Size = clonePtr(src.Size)
	case "doubleSided":
		(*dst).DoubleSided = src.DoubleSided
	default:
		*dst = cloneOccluderDoc(src)
	}
}

func applyLightFieldFrom(dst **sceneformat.LightDoc, src *sceneformat.LightDoc, sub string) {
	if sub == "" || src == nil || *dst == nil {
		*dst = src.Clone()
		return
	}
	switch sub {
	case "color":
		(*dst).Color = src.Color
	case "intensity":
		(*dst).Intensity = cloneFloat32Ptr(src.Intensity)
	case "range":
		(*dst).Range = src.Range
	case "innerCone":
		(*dst).InnerCone = src.InnerCone
	case "outerCone":
		(*dst).OuterCone = cloneFloat32Ptr(src.OuterCone)
	case "shadows":
		(*dst).Shadows = src.Shadows
	default:
		*dst = src.Clone()
	}
}

func applyCamera3DFieldFrom(dst **sceneformat.Camera3DDoc, src *sceneformat.Camera3DDoc, sub string) {
	if sub == "" || src == nil || *dst == nil {
		*dst = src.Clone()
		return
	}
	switch sub {
	case "projection":
		(*dst).Projection = src.Projection
	case "fov":
		(*dst).FOV = cloneFloat32Ptr(src.FOV)
	case "orthoHeight":
		(*dst).OrthoHeight = cloneFloat32Ptr(src.OrthoHeight)
	case "near":
		(*dst).Near = cloneFloat32Ptr(src.Near)
	case "far":
		(*dst).Far = cloneFloat32Ptr(src.Far)
	case "current":
		(*dst).Current = src.Current
	default:
		*dst = src.Clone()
	}
}

func applyEnvironmentFieldFrom(dst **sceneformat.EnvironmentDoc, src *sceneformat.EnvironmentDoc, sub string) {
	if sub == "" || src == nil || *dst == nil {
		*dst = src.Clone()
		return
	}
	switch {
	case sub == "sky":
		(*dst).Sky = src.Sky.Clone()
	case strings.HasPrefix(sub, "sky."):
		if src.Sky == nil || (*dst).Sky == nil {
			(*dst).Sky = src.Sky.Clone()
			return
		}
		switch strings.TrimPrefix(sub, "sky.") {
		case "color":
			(*dst).Sky.Color = src.Sky.Color
		case "zenith":
			(*dst).Sky.Zenith = src.Sky.Zenith
		case "horizon":
			(*dst).Sky.Horizon = src.Sky.Horizon
		case "ground":
			(*dst).Sky.Ground = src.Sky.Ground
		case "hdr":
			(*dst).Sky.HDR = src.Sky.HDR
		default:
			(*dst).Sky = src.Sky.Clone()
		}
	case sub == "ambientColor":
		(*dst).AmbientColor = src.AmbientColor
	case sub == "ambientIntensity":
		(*dst).AmbientIntensity = cloneFloat32Ptr(src.AmbientIntensity)
	case sub == "iblIntensity":
		(*dst).IBLIntensity = cloneFloat32Ptr(src.IBLIntensity)
	case sub == "fog":
		(*dst).Fog = src.Fog.Clone()
	case strings.HasPrefix(sub, "fog."):
		if src.Fog == nil || (*dst).Fog == nil {
			(*dst).Fog = src.Fog.Clone()
			return
		}
		switch strings.TrimPrefix(sub, "fog.") {
		case "mode":
			(*dst).Fog.Mode = src.Fog.Mode
		case "color":
			(*dst).Fog.Color = src.Fog.Color
		case "start":
			(*dst).Fog.Start = src.Fog.Start
		case "end":
			(*dst).Fog.End = src.Fog.End
		case "density":
			(*dst).Fog.Density = src.Fog.Density
		case "maxOpacity":
			(*dst).Fog.MaxOpacity = cloneFloat32Ptr(src.Fog.MaxOpacity)
		case "heightEnabled":
			(*dst).Fog.HeightEnabled = src.Fog.HeightEnabled
		case "heightBase":
			(*dst).Fog.HeightBase = src.Fog.HeightBase
		case "heightFalloff":
			(*dst).Fog.HeightFalloff = src.Fog.HeightFalloff
		case "heightDensity":
			(*dst).Fog.HeightDensity = src.Fog.HeightDensity
		default:
			(*dst).Fog = src.Fog.Clone()
		}
	case sub == "tonemapping":
		(*dst).Tonemapping = src.Tonemapping
	case sub == "exposure":
		(*dst).Exposure = cloneFloat32Ptr(src.Exposure)
	default:
		*dst = src.Clone()
	}
}

func applyObjectOverrideFieldFrom(dst, src *sceneformat.ObjectOverride, field string) {
	if dst == nil || src == nil {
		return
	}
	switch field {
	case "":
		*dst = mergeCloneObjectOverride(*src)
	case "name":
		dst.Name = clonePtr(src.Name)
	case "prefab":
		dst.Prefab = clonePtr(src.Prefab)
	case "x":
		dst.X = cloneFloat32Ptr(src.X)
	case "y":
		dst.Y = cloneFloat32Ptr(src.Y)
	case "posZ":
		dst.PosZ = cloneFloat32Ptr(src.PosZ)
	case "originX":
		dst.OriginX = cloneFloat32Ptr(src.OriginX)
	case "originY":
		dst.OriginY = cloneFloat32Ptr(src.OriginY)
	case "rotation":
		dst.Rotation = cloneFloat32Ptr(src.Rotation)
	case "rotX":
		dst.RotX = cloneFloat32Ptr(src.RotX)
	case "rotY":
		dst.RotY = cloneFloat32Ptr(src.RotY)
	case "rotZ":
		dst.RotZ = cloneFloat32Ptr(src.RotZ)
	case "scaleX":
		dst.ScaleX = cloneFloat32Ptr(src.ScaleX)
	case "scaleY":
		dst.ScaleY = cloneFloat32Ptr(src.ScaleY)
	case "scaleZ":
		dst.ScaleZ = cloneFloat32Ptr(src.ScaleZ)
	case "depth":
		dst.Depth = cloneFloat32Ptr(src.Depth)
	case "mesh":
		dst.Mesh = clonePtr(src.Mesh)
	case "model":
		dst.Model = clonePtr(src.Model)
	case "modelNode":
		dst.ModelNode = clonePtr(src.ModelNode)
	case "modelMesh":
		dst.ModelMesh = clonePtr(src.ModelMesh)
	case "gridMap":
		dst.GridMap = clonePtr(src.GridMap)
	case "occluder":
		dst.Occluder = cloneOccluderDoc(src.Occluder)
	case "flipX":
		dst.FlipX = cloneBoolPtr(src.FlipX)
	case "flipY":
		dst.FlipY = cloneBoolPtr(src.FlipY)
	case "alpha":
		dst.Alpha = cloneFloat32Ptr(src.Alpha)
	case "z":
		dst.Z = clonePtr(src.Z)
	case "sprite":
		dst.Sprite = cloneSprite(src.Sprite)
	case "material":
		dst.Material = clonePtr(src.Material)
	case "particles":
		dst.Particles = cloneParticlesRef(src.Particles)
	case "light":
		dst.Light = src.Light.Clone()
	case "camera":
		dst.Camera = src.Camera.Clone()
	case "components":
		dst.Components = cloneMap(src.Components)
	case "properties":
		dst.Properties = cloneMap(src.Properties)
	default:
		switch {
		case strings.HasPrefix(field, "occluder."):
			applyOccluderFieldFrom(&dst.Occluder, src.Occluder, strings.TrimPrefix(field, "occluder."))
		case strings.HasPrefix(field, "sprite."):
			applySpriteFieldFrom(&dst.Sprite, src.Sprite, strings.TrimPrefix(field, "sprite."))
		case strings.HasPrefix(field, "particles."):
			applyParticlesFieldFrom(&dst.Particles, src.Particles, strings.TrimPrefix(field, "particles."))
		case strings.HasPrefix(field, "light."):
			applyLightFieldFrom(&dst.Light, src.Light, strings.TrimPrefix(field, "light."))
		case strings.HasPrefix(field, "camera."):
			applyCamera3DFieldFrom(&dst.Camera, src.Camera, strings.TrimPrefix(field, "camera."))
		case strings.HasPrefix(field, "components."):
			applyAnyMapPathFrom(&dst.Components, src.Components, strings.TrimPrefix(field, "components."))
		case strings.HasPrefix(field, "properties."):
			applyAnyMapPathFrom(&dst.Properties, src.Properties, strings.TrimPrefix(field, "properties."))
		}
	}
}

func mergeConnectionLabels(conns []sceneformat.ConnectionDoc) []string {
	type baseKey struct {
		from, event, to, handler string
	}
	seen := make(map[baseKey]int, len(conns))
	labels := make([]string, len(conns))
	for i, c := range conns {
		bk := baseKey{c.From, c.Event, c.To, c.Handler}
		occ := seen[bk]
		seen[bk] = occ + 1
		s := fmt.Sprintf("%s.%s -> %s.%s", c.From, c.Event, c.To, c.Handler)
		if occ > 0 {
			s = fmt.Sprintf("%s#%d", s, occ+1)
		}
		labels[i] = s
	}
	return labels
}

func applyNonObjectResolution(doc, mineDoc, theirsDoc *sceneformat.SceneDoc, item MergeConflictItem) {
	if doc == nil {
		return
	}
	sideDoc := func(field string) *sceneformat.SceneDoc {
		switch item.Choice {
		case MergeChoiceTheirs:
			return theirsDoc
		case MergeChoiceMine:
			return mineDoc
		case MergeChoiceAgent:
			if item.Agent.fieldSide != nil {
				if fc, ok := item.Agent.fieldSide[field]; ok {
					if fc == MergeChoiceTheirs {
						return theirsDoc
					}
					return mineDoc
				}
			}
			if item.Agent.Meta == "same as theirs" {
				return theirsDoc
			}
			return mineDoc
		default:
			return nil
		}
	}
	defaultSrc := sideDoc("")
	if defaultSrc == nil {
		return
	}

	switch item.Scope {
	case "scene":
		for _, c := range item.Conflicts {
			src := sideDoc(c.Field)
			if src == nil {
				continue
			}
			switch c.Field {
			case "version":
				doc.Version = src.Version
			case "name":
				doc.Name = src.Name
			case "virtualWidth":
				doc.VirtualWidth = src.VirtualWidth
			case "virtualHeight":
				doc.VirtualHeight = src.VirtualHeight
			case "backgroundColor":
				doc.BackgroundColor = src.BackgroundColor
			case "ambientColor":
				doc.AmbientColor = src.AmbientColor
			case "navMesh":
				doc.NavMesh = src.NavMesh
			case "inherits":
				doc.Inherits = src.Inherits
			case "uid":
				doc.UID = src.UID
			case "camera":
				doc.Camera = cloneCameraDoc(src.Camera)
			case "camera.x":
				if src.Camera == nil {
					doc.Camera = nil
				} else {
					if doc.Camera == nil {
						doc.Camera = &sceneformat.CameraDoc{}
					}
					doc.Camera.X = src.Camera.X
				}
			case "camera.y":
				if src.Camera == nil {
					doc.Camera = nil
				} else {
					if doc.Camera == nil {
						doc.Camera = &sceneformat.CameraDoc{}
					}
					doc.Camera.Y = src.Camera.Y
				}
			case "camera.zoom":
				if src.Camera == nil {
					doc.Camera = nil
				} else {
					if doc.Camera == nil {
						doc.Camera = &sceneformat.CameraDoc{}
					}
					doc.Camera.Zoom = cloneFloat32Ptr(src.Camera.Zoom)
				}
			case "camera.rotation":
				if src.Camera == nil {
					doc.Camera = nil
				} else {
					if doc.Camera == nil {
						doc.Camera = &sceneformat.CameraDoc{}
					}
					doc.Camera.Rotation = src.Camera.Rotation
				}
			case "camera.bounds":
				if src.Camera == nil {
					doc.Camera = nil
				} else {
					if doc.Camera == nil {
						doc.Camera = &sceneformat.CameraDoc{}
					}
					doc.Camera.Bounds = cloneBoundsDoc(src.Camera.Bounds)
				}
			case "camera.bounds.x", "camera.bounds.y", "camera.bounds.width", "camera.bounds.height":
				if src.Camera == nil || src.Camera.Bounds == nil {
					if doc.Camera != nil {
						doc.Camera.Bounds = nil
					}
				} else {
					if doc.Camera == nil {
						doc.Camera = &sceneformat.CameraDoc{}
					}
					if doc.Camera.Bounds == nil {
						doc.Camera.Bounds = &sceneformat.BoundsDoc{}
					}
					switch c.Field {
					case "camera.bounds.x":
						doc.Camera.Bounds.X = src.Camera.Bounds.X
					case "camera.bounds.y":
						doc.Camera.Bounds.Y = src.Camera.Bounds.Y
					case "camera.bounds.width":
						doc.Camera.Bounds.Width = src.Camera.Bounds.Width
					case "camera.bounds.height":
						doc.Camera.Bounds.Height = src.Camera.Bounds.Height
					}
				}
			case "environment":
				doc.Environment = src.Environment.Clone()
			default:
				if strings.HasPrefix(c.Field, "environment.") {
					applyEnvironmentFieldFrom(&doc.Environment, src.Environment, strings.TrimPrefix(c.Field, "environment."))
				}
			}
		}

	case "layer":
		layerName := nonEmpty(item.Layer, item.Target)
		var srcLayer *sceneformat.LayerDoc
		for i := range defaultSrc.Layers {
			if defaultSrc.Layers[i].Name == layerName {
				srcLayer = &defaultSrc.Layers[i]
				break
			}
		}
		foundIdx := -1
		for i := range doc.Layers {
			if doc.Layers[i].Name == layerName {
				foundIdx = i
				break
			}
		}
		if srcLayer == nil {
			if foundIdx >= 0 {
				doc.Layers = append(doc.Layers[:foundIdx], doc.Layers[foundIdx+1:]...)
			}
			return
		}
		if foundIdx < 0 {
			// Restore layer deleted on ours.
			doc.Layers = append(doc.Layers, cloneLayerDoc(srcLayer))
			return
		}
		for _, c := range item.Conflicts {
			switch {
			case c.Field == "":
				doc.Layers[foundIdx] = cloneLayerDoc(srcLayer)
			case c.Field == "space":
				doc.Layers[foundIdx].Space = srcLayer.Space
			case c.Field == "order":
				doc.Layers[foundIdx].Order = srcLayer.Order
			case c.Field == "visible":
				doc.Layers[foundIdx].Visible = cloneBoolPtr(srcLayer.Visible)
			case c.Field == "parallaxX":
				doc.Layers[foundIdx].ParallaxX = cloneFloat32Ptr(srcLayer.ParallaxX)
			case c.Field == "parallaxY":
				doc.Layers[foundIdx].ParallaxY = cloneFloat32Ptr(srcLayer.ParallaxY)
			case c.Field == "repeatX":
				doc.Layers[foundIdx].RepeatX = srcLayer.RepeatX
			case c.Field == "repeatY":
				doc.Layers[foundIdx].RepeatY = srcLayer.RepeatY
			case c.Field == "repeatWidth":
				doc.Layers[foundIdx].RepeatWidth = srcLayer.RepeatWidth
			case c.Field == "repeatHeight":
				doc.Layers[foundIdx].RepeatHeight = srcLayer.RepeatHeight
			case c.Field == "tilemap":
				doc.Layers[foundIdx].Tilemap = mergeCloneTilemapDoc(srcLayer.Tilemap)
			case strings.HasPrefix(c.Field, "tilemap."):
				applyTilemapFieldFrom(&doc.Layers[foundIdx].Tilemap, srcLayer.Tilemap, strings.TrimPrefix(c.Field, "tilemap."))
			}
		}

	case "tilemap":
		key := item.Target
		srcTM, hasSrc := defaultSrc.Tilemaps[key]
		if !hasSrc || srcTM == nil {
			if doc.Tilemaps != nil {
				delete(doc.Tilemaps, key)
				if len(doc.Tilemaps) == 0 {
					doc.Tilemaps = nil
				}
			}
			return
		}
		if doc.Tilemaps == nil {
			doc.Tilemaps = make(map[string]*sceneformat.TilemapDoc)
		}
		cur, exists := doc.Tilemaps[key]
		if !exists || cur == nil {
			doc.Tilemaps[key] = mergeCloneTilemapDoc(srcTM)
			return
		}
		for _, c := range item.Conflicts {
			applyTilemapFieldFrom(&cur, srcTM, c.Field)
		}
		doc.Tilemaps[key] = cur

	case "layerOverride":
		key := item.Target
		srcLO, hasSrc := defaultSrc.LayerOverrides[key]
		if !hasSrc {
			if doc.LayerOverrides != nil {
				delete(doc.LayerOverrides, key)
				if len(doc.LayerOverrides) == 0 {
					doc.LayerOverrides = nil
				}
			}
			return
		}
		if doc.LayerOverrides == nil {
			doc.LayerOverrides = make(map[string]sceneformat.LayerOverride)
		}
		cur, exists := doc.LayerOverrides[key]
		if !exists {
			doc.LayerOverrides[key] = mergeCloneLayerOverride(srcLO)
			return
		}
		for _, c := range item.Conflicts {
			switch {
			case c.Field == "":
				cur = mergeCloneLayerOverride(srcLO)
			case c.Field == "space":
				cur.Space = srcLO.Space
			case c.Field == "order":
				cur.Order = clonePtr(srcLO.Order)
			case c.Field == "visible":
				cur.Visible = cloneBoolPtr(srcLO.Visible)
			case c.Field == "parallaxX":
				cur.ParallaxX = cloneFloat32Ptr(srcLO.ParallaxX)
			case c.Field == "parallaxY":
				cur.ParallaxY = cloneFloat32Ptr(srcLO.ParallaxY)
			case c.Field == "repeatX":
				cur.RepeatX = cloneBoolPtr(srcLO.RepeatX)
			case c.Field == "repeatY":
				cur.RepeatY = cloneBoolPtr(srcLO.RepeatY)
			case c.Field == "repeatWidth":
				cur.RepeatWidth = cloneFloat32Ptr(srcLO.RepeatWidth)
			case c.Field == "repeatHeight":
				cur.RepeatHeight = cloneFloat32Ptr(srcLO.RepeatHeight)
			case c.Field == "tilemap":
				cur.Tilemap = mergeCloneTilemapDoc(srcLO.Tilemap)
			case strings.HasPrefix(c.Field, "tilemap."):
				applyTilemapFieldFrom(&cur.Tilemap, srcLO.Tilemap, strings.TrimPrefix(c.Field, "tilemap."))
			}
		}
		doc.LayerOverrides[key] = cur

	case "override":
		key := item.Target
		srcOO, hasSrc := defaultSrc.Overrides[key]
		if !hasSrc {
			if doc.Overrides != nil {
				delete(doc.Overrides, key)
				if len(doc.Overrides) == 0 {
					doc.Overrides = nil
				}
			}
			return
		}
		if doc.Overrides == nil {
			doc.Overrides = make(map[string]sceneformat.ObjectOverride)
		}
		cur, exists := doc.Overrides[key]
		if !exists {
			doc.Overrides[key] = mergeCloneObjectOverride(srcOO)
			return
		}
		for _, c := range item.Conflicts {
			applyObjectOverrideFieldFrom(&cur, &srcOO, c.Field)
		}
		doc.Overrides[key] = cur

	case "connection":
		target := item.Target
		srcLabels := mergeConnectionLabels(defaultSrc.Connections)
		var srcConn *sceneformat.ConnectionDoc
		for i, lbl := range srcLabels {
			if lbl == target {
				cp := cloneConnectionDoc(defaultSrc.Connections[i])
				srcConn = &cp
				break
			}
		}
		docLabels := mergeConnectionLabels(doc.Connections)
		foundIdx := -1
		for i, lbl := range docLabels {
			if lbl == target {
				foundIdx = i
				break
			}
		}
		if srcConn == nil {
			if foundIdx >= 0 {
				doc.Connections = append(doc.Connections[:foundIdx], doc.Connections[foundIdx+1:]...)
			}
			return
		}
		if foundIdx < 0 {
			doc.Connections = append(doc.Connections, *srcConn)
			return
		}
		for _, c := range item.Conflicts {
			switch {
			case c.Field == "":
				doc.Connections[foundIdx] = cloneConnectionDoc(*srcConn)
			case c.Field == "once":
				doc.Connections[foundIdx].Once = srcConn.Once
			case c.Field == "args":
				doc.Connections[foundIdx].Args = cloneMap(srcConn.Args)
			case strings.HasPrefix(c.Field, "args."):
				applyAnyMapPathFrom(&doc.Connections[foundIdx].Args, srcConn.Args, strings.TrimPrefix(c.Field, "args."))
			}
		}
	}
}

func hasMoveConflict(conflicts []sceneformat.MergeConflict) bool {
	return slices.ContainsFunc(conflicts, func(c sceneformat.MergeConflict) bool {
		return c.Kind == sceneformat.ConflictMove
	})
}

func findObjectInDoc(doc *sceneformat.SceneDoc, id string) (*sceneformat.ObjectDoc, string, string) {
	if doc == nil {
		return nil, "", ""
	}
	var walk func(layer, parent string, objs []sceneformat.ObjectDoc) (*sceneformat.ObjectDoc, string, string)
	walk = func(layer, parent string, objs []sceneformat.ObjectDoc) (*sceneformat.ObjectDoc, string, string) {
		for i := range objs {
			if objs[i].ID == id {
				return &objs[i], layer, parent
			}
			if found, l, p := walk(layer, objs[i].ID, objs[i].Children); found != nil {
				return found, l, p
			}
		}
		return nil, "", ""
	}
	for i := range doc.Layers {
		if found, l, p := walk(doc.Layers[i].Name, "", doc.Layers[i].Objects); found != nil {
			return found, l, p
		}
	}
	return nil, "", ""
}

func removeObjectFromDoc(doc *sceneformat.SceneDoc, id string) (*sceneformat.ObjectDoc, bool) {
	if doc == nil {
		return nil, false
	}
	var walk func(objs *[]sceneformat.ObjectDoc) (*sceneformat.ObjectDoc, bool)
	walk = func(objs *[]sceneformat.ObjectDoc) (*sceneformat.ObjectDoc, bool) {
		list := *objs
		for i := range list {
			if list[i].ID == id {
				removed := mergeCloneObjectDocPtr(&list[i])
				*objs = append(list[:i], list[i+1:]...)
				return removed, true
			}
			if removed, ok := walk(&list[i].Children); ok {
				return removed, true
			}
		}
		return nil, false
	}
	for i := range doc.Layers {
		if removed, ok := walk(&doc.Layers[i].Objects); ok {
			return removed, true
		}
	}
	return nil, false
}

func moveObjectInDoc(doc *sceneformat.SceneDoc, id, targetLayer, targetParent string) {
	removed, ok := removeObjectFromDoc(doc, id)
	if !ok || removed == nil {
		return
	}
	insertObjectInDoc(doc, removed, targetLayer, targetParent)
}

func insertObjectInDoc(doc *sceneformat.SceneDoc, obj *sceneformat.ObjectDoc, targetLayer, targetParent string) {
	if doc == nil || obj == nil {
		return
	}
	if targetParent != "" {
		if parent, _, _ := findObjectInDoc(doc, targetParent); parent != nil {
			parent.Children = append(parent.Children, *obj)
			return
		}
	}
	for i := range doc.Layers {
		if doc.Layers[i].Name == targetLayer {
			doc.Layers[i].Objects = append(doc.Layers[i].Objects, *obj)
			return
		}
	}
	if len(doc.Layers) > 0 {
		doc.Layers[0].Objects = append(doc.Layers[0].Objects, *obj)
		return
	}
	doc.Layers = append(doc.Layers, sceneformat.LayerDoc{
		Name:    nonEmpty(targetLayer, "main"),
		Objects: []sceneformat.ObjectDoc{*obj},
	})
}

// --- Custom YAML parsing for "Edit the result YAML by hand" ---

func initialCustomYAMLForItem(item *MergeConflictItem, mergedDoc *sceneformat.SceneDoc) string {
	if item == nil {
		return ""
	}
	if item.Scope != "object" {
		return item.Agent.YAML
	}
	var obj *sceneformat.ObjectDoc
	switch item.Choice {
	case MergeChoiceMine:
		obj = item.MineObj
	case MergeChoiceTheirs:
		obj = item.TheirsObj
	case MergeChoiceAgent:
		if item.Agent.Deleted {
			return fmt.Sprintf("# %s stays deleted\n", item.ObjectID)
		}
		obj = item.Agent.Object
	case MergeChoiceCustom:
		if item.customDeleted {
			return fmt.Sprintf("# %s stays deleted\n", item.ObjectID)
		}
		obj = item.customObj
	}
	if obj == nil {
		if m := indexObjects(mergedDoc)[item.ObjectID].obj; m != nil {
			obj = m
		} else {
			obj = firstNonNilObj(item.Agent.Object, item.MineObj, item.TheirsObj)
		}
	}
	if obj == nil {
		return fmt.Sprintf("# %s stays deleted\n", item.ObjectID)
	}
	copyObj := mergeCloneObjectDocPtr(obj)
	copyObj.Children = nil // Edit this object's fields without repeating nested children.
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(copyObj)
	_ = enc.Close()
	return strings.TrimRight(buf.String(), "\n")
}

func parseCustomObjectYAML(item *MergeConflictItem, raw string, mergedDoc *sceneformat.SceneDoc) (*sceneformat.ObjectDoc, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || isCommentOnlyYAML(trimmed) {
		return nil, true, nil
	}

	// Accept either a single YAML mapping (`id: door\nx: 580`) or a 1-element sequence (`- id: door\n  x: 580`).
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &root); err != nil {
		return nil, false, fmt.Errorf("invalid YAML: %w", err)
	}
	if root.Kind == 0 || len(root.Content) == 0 {
		return nil, true, nil
	}
	docNode := root.Content[0]
	if docNode.Kind == yaml.SequenceNode {
		if len(docNode.Content) == 0 {
			return nil, true, nil
		}
		if len(docNode.Content) > 1 {
			return nil, false, errors.New("expected a single object YAML entry, got multiple items")
		}
		docNode = docNode.Content[0]
	}
	if docNode.Kind != yaml.MappingNode {
		return nil, false, errors.New("expected a YAML mapping for the object")
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(docNode); err != nil {
		return nil, false, fmt.Errorf("invalid YAML object: %w", err)
	}
	_ = enc.Close()

	dec := yaml.NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.KnownFields(true)
	var obj sceneformat.ObjectDoc
	if err := dec.Decode(&obj); err != nil && !errors.Is(err, io.EOF) {
		return nil, false, fmt.Errorf("invalid object YAML: %w", err)
	}
	if obj.ID == "" {
		obj.ID = item.ObjectID
	}
	if obj.ID != item.ObjectID {
		return nil, false, fmt.Errorf("object id %q must stay %q", obj.ID, item.ObjectID)
	}
	// Preserve children from the existing merged object if the user did not specify them.
	if len(obj.Children) == 0 {
		if existing := indexObjects(mergedDoc)[item.ObjectID].obj; existing != nil && len(existing.Children) > 0 {
			obj.Children = cloneObjectSlice(existing.Children)
		}
	}
	return &obj, false, nil
}

func isCommentOnlyYAML(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t != "" && !strings.HasPrefix(t, "#") {
			return false
		}
	}
	return true
}

// --- Field comparison & concise YAML formatting helpers ---

func summarizeSideDelta(base, side *sceneformat.ObjectDoc, conflicts []sceneformat.MergeConflict, choice MergeChoice) string {
	if side == nil {
		return "deleted"
	}
	for _, c := range conflicts {
		if c.Kind == sceneformat.ConflictField {
			val := c.Ours
			if choice == MergeChoiceTheirs {
				val = c.Theirs
			}
			return fmt.Sprintf("%s %s → %s", shortFieldName(c.Field), formatScalar(c.Base), formatScalar(val))
		}
	}
	if d := shortObjectDelta(base, side); d != "" {
		return d
	}
	if base == nil {
		return "added"
	}
	return "changed"
}

func shortObjectDelta(base, side *sceneformat.ObjectDoc) string {
	if base == nil || side == nil {
		return ""
	}
	if base.X != side.X {
		return fmt.Sprintf("x %s → %s", formatFloat(float64(base.X)), formatFloat(float64(side.X)))
	}
	if base.Y != side.Y {
		return fmt.Sprintf("y %s → %s", formatFloat(float64(base.Y)), formatFloat(float64(side.Y)))
	}
	if base.Z != side.Z {
		return fmt.Sprintf("z %d → %d", base.Z, side.Z)
	}
	for _, k := range mergeSortedMapKeys(side.Properties) {
		if !valuesEqual(base.Properties[k], side.Properties[k]) {
			if base.Properties[k] == nil {
				return fmt.Sprintf("%s %s", k, formatScalar(side.Properties[k]))
			}
			return fmt.Sprintf("%s %s → %s", k, formatScalar(base.Properties[k]), formatScalar(side.Properties[k]))
		}
	}
	for _, cname := range mergeSortedMapKeys(side.Components) {
		if !valuesEqual(base.Components[cname], side.Components[cname]) {
			if bm, ok1 := base.Components[cname].(map[string]any); ok1 {
				if sm, ok2 := side.Components[cname].(map[string]any); ok2 {
					for _, fk := range mergeSortedMapKeys(sm) {
						if !valuesEqual(bm[fk], sm[fk]) {
							return fmt.Sprintf("%s %s → %s", fk, formatScalar(bm[fk]), formatScalar(sm[fk]))
						}
					}
				}
			}
			return fmt.Sprintf("%s changed", cname)
		}
	}
	return ""
}

func firstChangedFieldName(base, side *sceneformat.ObjectDoc) string {
	if base == nil || side == nil {
		return ""
	}
	if base.X != side.X || base.Y != side.Y {
		return "position"
	}
	if base.Z != side.Z {
		return "z"
	}
	for _, cname := range mergeSortedMapKeys(side.Components) {
		if !valuesEqual(base.Components[cname], side.Components[cname]) {
			if bm, ok1 := base.Components[cname].(map[string]any); ok1 {
				if sm, ok2 := side.Components[cname].(map[string]any); ok2 {
					for _, fk := range mergeSortedMapKeys(sm) {
						if !valuesEqual(bm[fk], sm[fk]) {
							return fk
						}
					}
				}
			}
			return cname
		}
	}
	for _, k := range mergeSortedMapKeys(side.Properties) {
		if !valuesEqual(base.Properties[k], side.Properties[k]) {
			return k
		}
	}
	return ""
}

func uniqueChangedFields(base, side, other *sceneformat.ObjectDoc) []string {
	if side == nil {
		return nil
	}
	var out []string
	if base != nil {
		if side.X != base.X && (other == nil || other.X == base.X) {
			out = append(out, "x")
		}
		if side.Y != base.Y && (other == nil || other.Y == base.Y) {
			out = append(out, "y")
		}
		if side.Z != base.Z && (other == nil || other.Z == base.Z) {
			out = append(out, "z")
		}
	}
	for _, k := range mergeSortedMapKeys(side.Properties) {
		var bv, ov any
		if base != nil {
			bv = base.Properties[k]
		}
		if other != nil {
			ov = other.Properties[k]
		}
		if !valuesEqual(side.Properties[k], bv) && valuesEqual(ov, bv) {
			out = append(out, k)
		}
	}
	for _, cname := range mergeSortedMapKeys(side.Components) {
		var bv, ov any
		if base != nil {
			bv = base.Components[cname]
		}
		if other != nil {
			ov = other.Components[cname]
		}
		if !valuesEqual(side.Components[cname], bv) && valuesEqual(ov, bv) {
			out = append(out, cname)
		}
	}
	return out
}

func addedObjectIDSet(auto []MergeAutoItem, side string) map[string]bool {
	out := make(map[string]bool)
	for _, a := range auto {
		if a.Scope == "object" && a.Action == "added" && a.Side == side && a.ObjectID != "" {
			out[a.ObjectID] = true
		}
	}
	return out
}

func objectReferencesAnyID(o *sceneformat.ObjectDoc, ids map[string]bool) bool {
	if o == nil || len(ids) == 0 {
		return false
	}
	var scan func(v any) bool
	scan = func(v any) bool {
		switch tv := v.(type) {
		case string:
			return ids[tv]
		case map[string]any:
			for _, val := range tv {
				if scan(val) {
					return true
				}
			}
		case []any:
			for _, val := range tv {
				if scan(val) {
					return true
				}
			}
		}
		return false
	}
	return scan(o.Properties) || scan(o.Components)
}

func applyConflictingFieldFrom(dst, src *sceneformat.ObjectDoc, field string) {
	if dst == nil || src == nil {
		return
	}
	switch field {
	case "name":
		dst.Name = src.Name
	case "prefab":
		dst.Prefab = src.Prefab
	case "x":
		dst.X = src.X
	case "y":
		dst.Y = src.Y
	case "posZ":
		dst.PosZ = src.PosZ
	case "originX":
		dst.OriginX = src.OriginX
	case "originY":
		dst.OriginY = src.OriginY
	case "z":
		dst.Z = src.Z
	case "rotation":
		dst.Rotation = src.Rotation
	case "rotX":
		dst.RotX = src.RotX
	case "rotY":
		dst.RotY = src.RotY
	case "rotZ":
		dst.RotZ = src.RotZ
	case "scaleX":
		dst.ScaleX = cloneFloat32Ptr(src.ScaleX)
	case "scaleY":
		dst.ScaleY = cloneFloat32Ptr(src.ScaleY)
	case "scaleZ":
		dst.ScaleZ = cloneFloat32Ptr(src.ScaleZ)
	case "depth":
		dst.Depth = src.Depth
	case "mesh":
		dst.Mesh = src.Mesh
	case "model":
		dst.Model = src.Model
	case "modelNode":
		dst.ModelNode = src.ModelNode
	case "modelMesh":
		dst.ModelMesh = src.ModelMesh
	case "gridMap":
		dst.GridMap = src.GridMap
	case "occluder":
		dst.Occluder = cloneOccluderDoc(src.Occluder)
	case "alpha":
		dst.Alpha = cloneFloat32Ptr(src.Alpha)
	case "flipX":
		dst.FlipX = src.FlipX
	case "flipY":
		dst.FlipY = src.FlipY
	case "sprite":
		dst.Sprite = cloneSprite(src.Sprite)
	case "material":
		dst.Material = src.Material
	case "particles":
		dst.Particles = cloneParticlesRef(src.Particles)
	case "light":
		dst.Light = src.Light.Clone()
	case "camera":
		dst.Camera = src.Camera.Clone()
	case "scene":
		dst.Scene = src.Scene
	case "components":
		dst.Components = cloneMap(src.Components)
	case "properties":
		dst.Properties = cloneMap(src.Properties)
	case "overrides":
		if len(src.Overrides) == 0 {
			dst.Overrides = nil
		} else {
			dst.Overrides = make(map[string]sceneformat.ObjectOverride, len(src.Overrides))
			for k, v := range src.Overrides {
				dst.Overrides[k] = mergeCloneObjectOverride(v)
			}
		}
	default:
		switch {
		case strings.HasPrefix(field, "occluder."):
			applyOccluderFieldFrom(&dst.Occluder, src.Occluder, strings.TrimPrefix(field, "occluder."))
		case strings.HasPrefix(field, "sprite."):
			applySpriteFieldFrom(&dst.Sprite, src.Sprite, strings.TrimPrefix(field, "sprite."))
		case strings.HasPrefix(field, "particles."):
			applyParticlesFieldFrom(&dst.Particles, src.Particles, strings.TrimPrefix(field, "particles."))
		case strings.HasPrefix(field, "light."):
			applyLightFieldFrom(&dst.Light, src.Light, strings.TrimPrefix(field, "light."))
		case strings.HasPrefix(field, "camera."):
			applyCamera3DFieldFrom(&dst.Camera, src.Camera, strings.TrimPrefix(field, "camera."))
		case strings.HasPrefix(field, "properties."):
			applyAnyMapPathFrom(&dst.Properties, src.Properties, strings.TrimPrefix(field, "properties."))
		case strings.HasPrefix(field, "components."):
			applyAnyMapPathFrom(&dst.Components, src.Components, strings.TrimPrefix(field, "components."))
		case strings.HasPrefix(field, "overrides."):
			rest := strings.TrimPrefix(field, "overrides.")
			target, subField, _ := strings.Cut(rest, ".")
			srcOO, hasSrc := src.Overrides[target]
			if !hasSrc {
				if dst.Overrides != nil {
					delete(dst.Overrides, target)
					if len(dst.Overrides) == 0 {
						dst.Overrides = nil
					}
				}
				return
			}
			if dst.Overrides == nil {
				dst.Overrides = make(map[string]sceneformat.ObjectOverride)
			}
			cur, exists := dst.Overrides[target]
			if !exists || subField == "" {
				dst.Overrides[target] = mergeCloneObjectOverride(srcOO)
				return
			}
			applyObjectOverrideFieldFrom(&cur, &srcOO, subField)
			dst.Overrides[target] = cur
		}
	}
}

func formatChangedObjectYAML(id string, base, side *sceneformat.ObjectDoc, conflicts []sceneformat.MergeConflict, _ MergeChoice) string {
	lines := []string{"- id: " + id}
	if side == nil {
		return "# " + id + " deleted"
	}
	conflictFields := make(map[string]bool, len(conflicts))
	for _, c := range conflicts {
		conflictFields[c.Field] = true
	}
	if side.Prefab != "" && (base == nil || base.Prefab != side.Prefab || len(conflicts) == 1 && conflicts[0].Kind == sceneformat.ConflictDeleteModify) {
		lines = append(lines, "  prefab: "+side.Prefab)
	}
	if base == nil || side.X != base.X || conflictFields["x"] {
		lines = append(lines, "  x: "+formatFloat(float64(side.X)))
	}
	if base == nil || side.Y != base.Y || conflictFields["y"] {
		lines = append(lines, "  y: "+formatFloat(float64(side.Y)))
	}
	if base == nil || side.Z != base.Z || conflictFields["z"] {
		lines = append(lines, fmt.Sprintf("  z: %d", side.Z))
	}
	if base != nil && side.Name != base.Name {
		lines = append(lines, "  name: "+side.Name)
	}
	for _, k := range mergeSortedMapKeys(side.Properties) {
		var bv any
		if base != nil {
			bv = base.Properties[k]
		}
		if !valuesEqual(side.Properties[k], bv) || conflictFields["properties."+k] {
			lines = append(lines, fmt.Sprintf("  %s: %s", k, formatScalar(side.Properties[k])))
		}
	}
	for _, cname := range mergeSortedMapKeys(side.Components) {
		var bv any
		if base != nil {
			bv = base.Components[cname]
		}
		if !valuesEqual(side.Components[cname], bv) || strings.HasPrefix(firstConflictField(conflicts), "components."+cname) {
			lines = append(lines, fmt.Sprintf("  %s: %s", cname, formatInlineMapOrScalar(side.Components[cname])))
		}
	}
	return strings.Join(lines, "\n")
}

func formatSuggestedObjectYAML(
	id string,
	base, mine, theirs, sug *sceneformat.ObjectDoc,
	conflicts []sceneformat.MergeConflict,
	fieldSides map[string]MergeChoice,
) string {
	if sug == nil {
		return "# " + id + " stays deleted"
	}
	lines := []string{"- id: " + id}
	conflictFields := make(map[string]bool, len(conflicts))
	for _, c := range conflicts {
		conflictFields[c.Field] = true
	}
	tag := func(field string, fromMine, fromTheirs bool) string {
		if s, ok := fieldSides[field]; ok {
			if s == MergeChoiceTheirs {
				return "  # theirs"
			}
			if s == MergeChoiceMine {
				return "  # mine"
			}
		}
		if fromMine && !fromTheirs {
			return "  # mine"
		}
		if fromTheirs && !fromMine {
			return "  # theirs"
		}
		return ""
	}
	if base == nil || sug.X != base.X || conflictFields["x"] {
		lines = append(lines, "  x: "+formatFloat(float64(sug.X))+tag("x", mine != nil && sug.X == mine.X, theirs != nil && sug.X == theirs.X))
	}
	if base == nil || sug.Y != base.Y || conflictFields["y"] {
		lines = append(lines, "  y: "+formatFloat(float64(sug.Y))+tag("y", mine != nil && sug.Y == mine.Y, theirs != nil && sug.Y == theirs.Y))
	}
	if base == nil || sug.Z != base.Z || conflictFields["z"] {
		lines = append(lines, fmt.Sprintf("  z: %d%s", sug.Z, tag("z", mine != nil && sug.Z == mine.Z, theirs != nil && sug.Z == theirs.Z)))
	}
	for _, k := range mergeSortedMapKeys(sug.Properties) {
		var bv, mv, tv any
		if base != nil {
			bv = base.Properties[k]
		}
		if mine != nil {
			mv = mine.Properties[k]
		}
		if theirs != nil {
			tv = theirs.Properties[k]
		}
		if !valuesEqual(sug.Properties[k], bv) || conflictFields["properties."+k] {
			lines = append(lines, fmt.Sprintf("  %s: %s%s", k, formatScalar(sug.Properties[k]),
				tag("properties."+k, !valuesEqual(mv, bv) && valuesEqual(sug.Properties[k], mv), !valuesEqual(tv, bv) && valuesEqual(sug.Properties[k], tv))))
		}
	}
	for _, cname := range mergeSortedMapKeys(sug.Components) {
		var bv, mv, tv any
		if base != nil {
			bv = base.Components[cname]
		}
		if mine != nil {
			mv = mine.Components[cname]
		}
		if theirs != nil {
			tv = theirs.Components[cname]
		}
		if !valuesEqual(sug.Components[cname], bv) {
			lines = append(lines, fmt.Sprintf("  %s: %s%s", cname, formatInlineMapOrScalar(sug.Components[cname]),
				tag("components."+cname, !valuesEqual(mv, bv) && valuesEqual(sug.Components[cname], mv), !valuesEqual(tv, bv) && valuesEqual(sug.Components[cname], tv))))
		}
	}
	return strings.Join(lines, "\n")
}

func firstConflictField(conflicts []sceneformat.MergeConflict) string {
	for _, c := range conflicts {
		if c.Field != "" {
			return c.Field
		}
	}
	return ""
}

func shortFieldName(field string) string {
	if i := strings.LastIndexByte(field, '.'); i >= 0 && i+1 < len(field) {
		return field[i+1:]
	}
	return field
}

func formatScalar(v any) string {
	if v == nil {
		return "null"
	}
	switch x := v.(type) {
	case float64:
		return formatFloat(x)
	case float32:
		return formatFloat(float64(x))
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	case string:
		return x
	default:
		return fmt.Sprintf("%v", v)
	}
}

func formatFloat(f float64) string {
	if math.Trunc(f) == f && math.Abs(f) < 1e12 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func formatInlineMapOrScalar(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return formatScalar(v)
	}
	keys := mergeSortedMapKeys(m)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", k, formatScalar(m[k])))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func mergeSortedMapKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, cmp.Compare[string])
	return keys
}

func valuesEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return fmt.Sprintf("%#v", a) == fmt.Sprintf("%#v", b)
}

func cloneSceneDoc(doc *sceneformat.SceneDoc) *sceneformat.SceneDoc {
	if doc == nil {
		return nil
	}
	return doc.Clone()
}

func mergeCloneObjectDocPtr(o *sceneformat.ObjectDoc) *sceneformat.ObjectDoc {
	if o == nil {
		return nil
	}
	cp := cloneObjectDoc(*o)
	return &cp
}

func cloneObjectSlice(objs []sceneformat.ObjectDoc) []sceneformat.ObjectDoc {
	if len(objs) == 0 {
		return nil
	}
	out := make([]sceneformat.ObjectDoc, len(objs))
	for i := range objs {
		out[i] = cloneObjectDoc(objs[i])
	}
	return out
}

func cloneBoolPtr(p *bool) *bool {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func mapsCloneChoice(m map[string]MergeChoice) map[string]MergeChoice {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]MergeChoice, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func nonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func trimSceneFilename(p string) string {
	base := path.Base(p)
	for _, suf := range []string{".scene.yaml", ".scene.yml", ".yaml", ".yml"} {
		if strings.HasSuffix(strings.ToLower(base), suf) {
			return base[:len(base)-len(suf)]
		}
	}
	return base
}

func (s *MergeConflictSession) rebuildSidePreviews(resolver sceneformat.AssetResolver) {
	if s == nil {
		return
	}
	if s.MineDoc != nil {
		s.mineBuilt, _ = sceneformat.Build(s.MineDoc.Clone(), resolver)
	}
	if s.TheirsDoc != nil {
		s.theirsBuilt, _ = sceneformat.Build(s.TheirsDoc.Clone(), resolver)
	}
}

func (s *MergeConflictSession) refreshResultPreview(resolver sceneformat.AssetResolver) {
	if s == nil {
		return
	}
	doc, err := s.ResultDoc()
	if err != nil || doc == nil {
		return
	}
	s.resultBuilt, _ = sceneformat.Build(doc.Clone(), resolver)
}
