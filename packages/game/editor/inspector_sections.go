package editor

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

const maxInspectorYAMLBytes = 4 << 20 // 4 MiB cap when reading clean scene YAML from disk.

func isFiniteFloat64(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func clampInt32Float(v float64) float64 {
	if v < math.MinInt32 {
		return math.MinInt32
	}
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return v
}

func (e *Editor) resetInspectorSessionState() {
	if e == nil {
		return
	}
	e.inspectorFilter = ""
	e.inspectorReviewChanges = nil
	e.inspectorFieldProblems = nil
	e.composerDraft = ""
}

// ComponentCategory groups registered component types in the Inspector's Add component picker.
type ComponentCategory string

const (
	ComponentCategoryGameplay    ComponentCategory = "GAMEPLAY"
	ComponentCategoryPhysics     ComponentCategory = "PHYSICS"
	ComponentCategoryAudioVisual ComponentCategory = "AUDIO AND VISUAL"
)

// ComponentGroup is one titled section of the Add component picker.
type ComponentGroup struct {
	Category ComponentCategory
	Names    []string
}

// InspectorScrubField identifies one numeric field of an object that can be scrubbed by dragging its label.
//
// Field names either a built-in object field ("x", "y", "z", "rotation", "scaleX", "scaleY",
// "alpha", "originX", "originY", "posZ", "rotX", "rotY", "rotZ", "scaleZ", "depth"), a custom
// property ("prop:<key>"), or a component field ("comp:<component>.<field>").
type InspectorScrubField struct {
	ObjectID string
	Field    string
	Step     float64
	Min      *float64
	Max      *float64
	Integer  bool
}

type inspectorScrubState struct {
	active     bool
	spec       InspectorScrubField
	startVal   float64
	currentVal float64
	preview    previewGroup
}

// InspectorReviewChange records a field value modified in an open agent review so the inspector
// can highlight the field and display "was <OldValue> · in review".
type InspectorReviewChange struct {
	ObjectID  string
	Component string // empty for top-level object field or custom property
	Field     string // e.g. "x", "speed", or "prop:<key>"
	OldValue  string
	NewValue  string
}

// InspectorFieldProblem records a validation or project-check problem attached to a specific
// object field so the inspector can show a problem pill beside the control and its section header.
type InspectorFieldProblem struct {
	ObjectID  string
	Component string // empty for top-level object field or custom property
	Field     string // e.g. "alpha", "coyote", or "prop:<key>"
	Message   string
}

// InspectorFilter returns the active inspector field filter query.
func (e *Editor) InspectorFilter() string {
	if e == nil {
		return ""
	}
	return e.inspectorFilter
}

// SetInspectorFilter sets the inspector field filter query, narrowing visible sections and fields
// to those whose label, component name, or section title matches (case-insensitive substring).
func (e *Editor) SetInspectorFilter(query string) {
	if e == nil || e.inspectorFilter == query {
		return
	}
	e.inspectorFilter = query
	e.notify()
}

// InspectorSectionCollapsed reports whether the titled inspector section id is currently collapsed.
func (e *Editor) InspectorSectionCollapsed(sectionID string) bool {
	if e == nil || len(e.inspectorCollapsed) == 0 {
		return false
	}
	return e.inspectorCollapsed[sectionID]
}

// SetInspectorSectionCollapsed expands or collapses the titled inspector section id.
func (e *Editor) SetInspectorSectionCollapsed(sectionID string, collapsed bool) {
	if e == nil || sectionID == "" {
		return
	}
	if e.inspectorCollapsed == nil {
		if !collapsed {
			return
		}
		e.inspectorCollapsed = make(map[string]bool)
	}
	if e.inspectorCollapsed[sectionID] == collapsed {
		return
	}
	if collapsed {
		e.inspectorCollapsed[sectionID] = true
	} else {
		delete(e.inspectorCollapsed, sectionID)
	}
	e.notify()
}

// ToggleInspectorSection flips the collapsed state of the titled inspector section id and
// reports whether the section is now expanded.
func (e *Editor) ToggleInspectorSection(sectionID string) bool {
	if e == nil || sectionID == "" {
		return true
	}
	collapsed := !e.InspectorSectionCollapsed(sectionID)
	e.SetInspectorSectionCollapsed(sectionID, collapsed)
	return !collapsed
}

// ComposerDraft returns the current draft text staged for the agent composer (for example via
// Ask to add in the inspector).
func (e *Editor) ComposerDraft() string {
	if e == nil {
		return ""
	}
	return e.composerDraft
}

// SetComposerDraft updates the agent composer draft text, refreshes @-mention trigger detection, and notifies listeners.
func (e *Editor) SetComposerDraft(text string) {
	if e == nil || (e.composerDraft == text && !e.agent.composerHighlighted) {
		return
	}
	e.updateComposerDraftFromInput(text)
}

// AskToAddComponent populates the agent composer draft with "Add a component to <object> that "
// for objectID (defaulting to the selected object when empty), invokes Editor.OnComposerPrompt
// when set, updates the status bar, and returns the prompt string.
func (e *Editor) AskToAddComponent(objectID string) string {
	if e == nil {
		return ""
	}
	target := strings.TrimSpace(objectID)
	if target == "" {
		target = e.SelectedID
	}
	if target == "" {
		if e.Doc != nil && e.Doc.Name != "" {
			target = e.Doc.Name
		} else {
			target = "this object"
		}
	}
	prompt := fmt.Sprintf("Add a component to %s that ", target)
	e.composerDraft = prompt
	e.agent.composerText = prompt
	e.agent.composerHighlighted = false
	e.Status = prompt
	if e.OnComposerPrompt != nil {
		e.OnComposerPrompt(prompt)
	}
	e.notify()
	return prompt
}

// AskToAddSelectedComponent is AskToAddComponent for the currently selected object.
func (e *Editor) AskToAddSelectedComponent() string {
	if e == nil {
		return ""
	}
	return e.AskToAddComponent(e.SelectedID)
}

// SetInspectorReviewChanges records the active agent review field changes shown in the inspector
// ("was <old> · in review"). Pass nil to clear them once a review is kept or reverted.
func (e *Editor) SetInspectorReviewChanges(changes []InspectorReviewChange) {
	if e == nil {
		return
	}
	e.inspectorReviewChanges = slices.Clone(changes)
	e.notify()
}

// InspectorReviewChanges returns a copy of the active review changes shown in the inspector.
func (e *Editor) InspectorReviewChanges() []InspectorReviewChange {
	if e == nil || len(e.inspectorReviewChanges) == 0 {
		return nil
	}
	return slices.Clone(e.inspectorReviewChanges)
}

func (e *Editor) inspectorReviewChangeFor(objectID, component, field string) (InspectorReviewChange, bool) {
	if e == nil {
		return InspectorReviewChange{}, false
	}
	for _, c := range e.inspectorReviewChanges {
		if c.ObjectID == objectID && c.Component == component && c.Field == field {
			return c, true
		}
	}
	return InspectorReviewChange{}, false
}

// SetInspectorFieldProblems records explicit field-level problems shown as problem pills in the
// inspector alongside live project-check problems (Editor.Problems) and component range checks.
func (e *Editor) SetInspectorFieldProblems(problems []InspectorFieldProblem) {
	if e == nil {
		return
	}
	e.inspectorFieldProblems = slices.Clone(problems)
	e.notify()
}

// InspectorFieldProblems returns explicit field-level problems recorded via SetInspectorFieldProblems.
func (e *Editor) InspectorFieldProblems() []InspectorFieldProblem {
	if e == nil || len(e.inspectorFieldProblems) == 0 {
		return nil
	}
	return slices.Clone(e.inspectorFieldProblems)
}

// inspectorFieldProblemFor returns the first problem matching (objectID, component, field) from
// explicit field problems, live component schema range checks, or the last project check (Problems).
func (e *Editor) inspectorFieldProblemFor(objectID, component, field string) (string, bool) {
	if e == nil || objectID == "" || field == "" {
		return "", false
	}
	for _, p := range e.inspectorFieldProblems {
		if p.ObjectID == objectID && p.Component == component && p.Field == field && p.Message != "" {
			return p.Message, true
		}
	}
	if component != "" {
		if obj, _ := e.FindResolvedObject(objectID); obj != nil {
			if cm, ok := obj.Components[component].(map[string]any); ok {
				if val, exists := cm[field]; exists {
					if ct, ok := e.lookupComponentType(component); ok {
						if f, ok := ct.Field(field); ok {
							if msg, bad := checkFieldBoundsProblem(f, val); bad {
								return msg, true
							}
						}
					}
				}
			}
		}
	}
	// Match problems from Editor.Problems() referencing the open scene, objectID, and field/component.
	probs := e.Problems()
	if len(probs) == 0 {
		return "", false
	}
	sceneRel := e.ScenePath
	quotedObj := strconv.Quote(objectID)
	quotedComp := strconv.Quote(component)
	quotedField := strconv.Quote(field)
	bareField := strings.TrimPrefix(field, "prop:")
	for _, p := range probs {
		if sceneRel != "" && p.File != "" && p.File != sceneRel {
			continue
		}
		if !strings.Contains(p.Message, quotedObj) && !containsIdentifierToken(p.Message, objectID, false) {
			continue
		}
		if component != "" {
			if (strings.Contains(p.Message, quotedComp) || containsIdentifierToken(p.Message, component, false)) &&
				(strings.Contains(p.Message, quotedField) || matchesProblemFieldToken(p.Message, bareField)) {
				return p.Message, true
			}
			continue
		}
		if strings.Contains(p.Message, quotedField) || matchesProblemFieldToken(p.Message, bareField) {
			return p.Message, true
		}
	}
	return "", false
}

func isIdentRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// containsIdentifierToken reports whether msg contains token bounded by non-identifier runes,
// preventing short IDs like "box" from matching "skybox" or "box_2".
func containsIdentifierToken(msg, token string, foldCase bool) bool {
	if msg == "" || token == "" {
		return false
	}
	hay := msg
	needle := token
	if foldCase {
		hay = strings.ToLower(msg)
		needle = strings.ToLower(token)
	}
	for offset := 0; offset < len(hay); {
		idx := strings.Index(hay[offset:], needle)
		if idx < 0 {
			return false
		}
		start := offset + idx
		end := start + len(needle)
		leftOK := start == 0
		if !leftOK {
			r, _ := utf8DecodeLast(hay[:start])
			leftOK = !isIdentRune(r)
		}
		rightOK := end == len(hay)
		if !rightOK {
			r, _ := utf8DecodeFirst(hay[end:])
			rightOK = !isIdentRune(r)
		}
		if leftOK && rightOK {
			return true
		}
		offset = start + 1
	}
	return false
}

func utf8DecodeFirst(s string) (rune, int) {
	return utf8.DecodeRuneInString(s)
}

func utf8DecodeLast(s string) (rune, int) {
	return utf8.DecodeLastRuneInString(s)
}

// matchesProblemFieldToken matches a field name in a problem message without letting 1-character
// field names like "x", "y", or "z" false-positive on ordinary words like "texture", "layer", or "size".
func matchesProblemFieldToken(msg, bareField string) bool {
	if bareField == "" {
		return false
	}
	if len(bareField) == 1 {
		lower := strings.ToLower(msg)
		f := strings.ToLower(bareField)
		// Require explicit field reference syntax for single-letter axes (".x", "x:", "x=", "field x").
		for _, pat := range []string{"." + f, f + ":", f + "=", "field " + f} {
			if containsFieldPattern(lower, pat) {
				return true
			}
		}
		return false
	}
	return containsIdentifierToken(msg, bareField, true)
}

func containsFieldPattern(lower, pat string) bool {
	for offset := 0; offset < len(lower); {
		idx := strings.Index(lower[offset:], pat)
		if idx < 0 {
			return false
		}
		start := offset + idx
		end := start + len(pat)
		leftOK := start == 0
		if !leftOK {
			r, _ := utf8DecodeLast(lower[:start])
			leftOK = !isIdentRune(r) || pat[0] == '.'
		}
		rightOK := end == len(lower)
		if !rightOK {
			r, _ := utf8DecodeFirst(lower[end:])
			rightOK = !isIdentRune(r) || pat[len(pat)-1] == ':' || pat[len(pat)-1] == '='
		}
		if leftOK && rightOK {
			return true
		}
		offset = start + 1
	}
	return false
}

func checkFieldBoundsProblem(f sceneformat.Field, val any) (string, bool) {
	if !f.Kind.IsNumeric() {
		return "", false
	}
	num, ok := anyToFloat64(val)
	if !ok {
		return fmt.Sprintf("must be %s", f.Kind.String()), true
	}
	if f.Min != nil && num < *f.Min {
		return fmt.Sprintf("Must be %g or more", *f.Min), true
	}
	if f.Max != nil && num > *f.Max {
		return fmt.Sprintf("Must be %g or less", *f.Max), true
	}
	return "", false
}

// verifyInspectorYAMLPath resolves e.FilePath and ensures it stays strictly inside the project
// root (following symlinks via resolvePathWithinRoot) whenever a project is open.
func (e *Editor) verifyInspectorYAMLPath() (string, error) {
	if e == nil || e.FilePath == "" {
		return "", nil
	}
	absFile, err := filepath.Abs(e.FilePath)
	if err != nil {
		return "", err
	}
	if e.ProjectPath == "" {
		return absFile, nil
	}
	rootAbs, err := filepath.Abs(filepath.Dir(e.ProjectPath))
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		realRoot = filepath.Clean(rootAbs)
	}
	_, resolved, err := resolvePathWithinRoot(realRoot, absFile)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func readCappedYAMLFile(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > maxBytes {
		return nil, fmt.Errorf("editor: %s exceeds %d byte YAML inspect cap", path, maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("editor: %s exceeds %d byte YAML inspect cap", path, maxBytes)
	}
	return data, nil
}

// ReadSceneYAMLForLocate returns the open scene's YAML bytes for line/snippet inspection: when
// the scene is clean on disk it reads the verified, size-capped disk file (preserving comments);
// otherwise it returns Editor.SaveYAML.
func (e *Editor) ReadSceneYAMLForLocate() ([]byte, error) {
	if e == nil || e.Doc == nil {
		return nil, errors.New("editor: no scene is open")
	}
	if e.FilePath != "" {
		verified, err := e.verifyInspectorYAMLPath()
		if err != nil {
			return nil, err
		}
		if !e.SceneDirty() && verified != "" {
			if diskData, err := readCappedYAMLFile(verified, maxInspectorYAMLBytes); err == nil {
				return diskData, nil
			}
		}
	}
	return e.SaveYAML()
}

// FindObjectYAMLLine locates the 1-based line number where objectID begins in the open scene's
// YAML and returns (path, line, nil). When objectID is empty, it returns line 1 of the open scene.
// While the open scene has no unsaved edits and a file on disk, line numbers match the file on
// disk (preserving hand-written comments and layout); otherwise they match Editor.SaveYAML.
func (e *Editor) FindObjectYAMLLine(objectID string) (string, int, error) {
	if e == nil || e.Doc == nil {
		return "", 0, errors.New("editor: no scene is open")
	}
	openPath := e.FilePath
	if e.FilePath != "" {
		if _, err := e.verifyInspectorYAMLPath(); err != nil {
			return "", 0, err
		}
	}
	if openPath == "" {
		openPath = e.ScenePath
	}
	if openPath == "" {
		openPath = "scene.yaml"
	}
	if objectID == "" {
		return openPath, 1, nil
	}
	if obj, _, _ := e.FindObject(objectID); obj == nil {
		return "", 0, fmt.Errorf("editor: object %q not found", objectID)
	}
	if e.FilePath != "" && !e.SceneDirty() {
		if verified, err := e.verifyInspectorYAMLPath(); err == nil && verified != "" {
			if diskData, err := readCappedYAMLFile(verified, maxInspectorYAMLBytes); err == nil {
				if line := findObjectLineInYAML(diskData, objectID); line > 0 {
					return openPath, line, nil
				}
			}
		}
	}
	data, err := e.SaveYAML()
	if err != nil {
		return "", 0, err
	}
	if line := findObjectLineInYAML(data, objectID); line > 0 {
		return openPath, line, nil
	}
	return "", 0, fmt.Errorf("editor: object %q not found in scene YAML", objectID)
}

// OpenObjectYAML locates objectID's 1-based line in the scene YAML (or line 1 when empty),
// verifies project-root containment, invokes Editor.OnOpenFileAtLine(path, line) when set,
// updates Editor.Status, and reports whether OnOpenFileAtLine actually ran.
func (e *Editor) OpenObjectYAML(objectID string) (string, int, bool, error) {
	if e == nil {
		return "", 0, false, errors.New("editor: nil editor")
	}
	if e.playing {
		return "", 0, false, ErrPlaying
	}
	path, line, err := e.FindObjectYAMLLine(objectID)
	if err != nil {
		return "", 0, false, err
	}
	opened := false
	if e.OnOpenFileAtLine != nil {
		e.OnOpenFileAtLine(path, line)
		opened = true
	}
	e.Status = fmt.Sprintf("Opened %s:%d", path, line)
	e.notify()
	return path, line, opened, nil
}

// OpenSelectedObjectYAML locates the selected object's 1-based line in the scene YAML (or line 1
// when no object is selected), invokes Editor.OnOpenFileAtLine(path, line) when set, and updates
// Editor.Status with "Opened <path>:<line>".
func (e *Editor) OpenSelectedObjectYAML() error {
	_, _, _, err := e.OpenObjectYAML(e.SelectedID)
	return err
}

// findObjectLineInYAML parses data as a YAML document and returns the 1-based starting line of
// the object mapping under layers[*].objects (recursing into children) whose "id" equals id, or
// 0 when absent.
func findObjectLineInYAML(data []byte, id string) int {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return findObjectLineScan(data, id)
	}
	doc := &root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return 0
	}
	layersNode := yamlMappingValue(doc, "layers")
	if layersNode == nil || layersNode.Kind != yaml.SequenceNode {
		return 0
	}
	for _, layer := range layersNode.Content {
		if layer.Kind != yaml.MappingNode {
			continue
		}
		if line := findObjectLineInSeq(yamlMappingValue(layer, "objects"), id); line > 0 {
			return line
		}
	}
	return 0
}

func findObjectLineInSeq(seq *yaml.Node, id string) int {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return 0
	}
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if idNode := yamlMappingValue(item, "id"); idNode != nil && idNode.Kind == yaml.ScalarNode && idNode.Value == id {
			if item.Line > 0 {
				return item.Line
			}
			return idNode.Line
		}
		if line := findObjectLineInSeq(yamlMappingValue(item, "children"), id); line > 0 {
			return line
		}
	}
	return 0
}

func yamlMappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func findObjectLineScan(data []byte, id string) int {
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		v, ok := strings.CutPrefix(trimmed, "- id:")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if unq, err := strconv.Unquote(v); err == nil {
			v = unq
		} else if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
			v = strings.ReplaceAll(v[1:len(v)-1], "''", "'")
		}
		if v == id {
			return i + 1
		}
	}
	return 0
}

// ComponentCategoryOf assigns a registered component type name to one of the inspector's three
// Add component groups: GAMEPLAY, PHYSICS, or AUDIO AND VISUAL.
func ComponentCategoryOf(name string) ComponentCategory {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "body2d", "collider2d", "trigger2d", "joint2d",
		"body3d", "collider3d", "trigger3d", "joint3d",
		"physics", "body", "area", "collider", "trigger", "rigidbody", "character":
		return ComponentCategoryPhysics
	case "light2d", "occluder2d", "animator", "skeletal",
		"sound", "sound_emitter", "soundemitter", "audio", "audio_source", "audiosource",
		"particles", "particle", "emitter", "sprite", "light", "camera", "mesh", "model", "trail", "vfx":
		return ComponentCategoryAudioVisual
	}
	switch {
	case strings.Contains(n, "physics"),
		strings.Contains(n, "collider"),
		strings.Contains(n, "rigidbody"),
		strings.Contains(n, "body2d"),
		strings.Contains(n, "body3d"),
		strings.Contains(n, "trigger2d"),
		strings.Contains(n, "trigger3d"),
		strings.Contains(n, "joint"):
		return ComponentCategoryPhysics
	case strings.Contains(n, "sound"),
		strings.Contains(n, "audio"),
		strings.Contains(n, "light"),
		strings.Contains(n, "occluder"),
		strings.Contains(n, "particle"),
		strings.Contains(n, "anim"),
		strings.Contains(n, "skeletal"),
		strings.Contains(n, "sprite"),
		strings.Contains(n, "visual"):
		return ComponentCategoryAudioVisual
	default:
		return ComponentCategoryGameplay
	}
}

// AddableComponentGroups returns the registered component types objectID lacks whose names match
// query, grouped in fixed order under GAMEPLAY, PHYSICS, and AUDIO AND VISUAL (omitting empty groups).
func (e *Editor) AddableComponentGroups(objectID, query string) []ComponentGroup {
	names := e.addableComponents(objectID, query)
	if len(names) == 0 {
		return nil
	}
	buckets := map[ComponentCategory][]string{}
	for _, n := range names {
		cat := ComponentCategoryOf(n)
		buckets[cat] = append(buckets[cat], n)
	}
	order := []ComponentCategory{
		ComponentCategoryGameplay,
		ComponentCategoryPhysics,
		ComponentCategoryAudioVisual,
	}
	out := make([]ComponentGroup, 0, len(order))
	for _, cat := range order {
		if len(buckets[cat]) > 0 {
			out = append(out, ComponentGroup{Category: cat, Names: buckets[cat]})
		}
	}
	return out
}

// InspectorScrubbing reports whether an interactive inspector label scrub is currently in progress.
func (e *Editor) InspectorScrubbing() bool {
	return e != nil && e.inspectorScrub.active
}

// ActiveInspectorScrub returns the currently scrubbing field spec and whether a scrub is active.
func (e *Editor) ActiveInspectorScrub() (InspectorScrubField, bool) {
	if e == nil || !e.inspectorScrub.active {
		return InspectorScrubField{}, false
	}
	return e.inspectorScrub.spec, true
}

// BeginInspectorScrub starts a live interactive scrub on spec. Updates preview immediately in
// the scene, EndInspectorScrub records the entire scrub as one undo step, and CancelInspectorScrub
// (or pressing Escape) restores the original value and records nothing.
func (e *Editor) BeginInspectorScrub(spec InspectorScrubField) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(spec.ObjectID); err != nil {
		return err
	}
	if spec.ObjectID == "" || spec.Field == "" {
		return errors.New("editor: scrub requires objectID and field")
	}
	if math.IsNaN(spec.Step) || math.IsInf(spec.Step, 0) {
		return errors.New("editor: scrub step must be finite")
	}
	if spec.Min != nil && !isFiniteFloat64(*spec.Min) {
		return errors.New("editor: scrub min must be finite")
	}
	if spec.Max != nil && !isFiniteFloat64(*spec.Max) {
		return errors.New("editor: scrub max must be finite")
	}
	if spec.Min != nil && spec.Max != nil && *spec.Min > *spec.Max {
		return errors.New("editor: scrub min must not exceed max")
	}
	e.CancelInspectorScrub()

	obj, _ := e.FindResolvedObject(spec.ObjectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", spec.ObjectID)
	}
	startVal, isInt, err := e.readInspectorNumericField(obj, spec)
	if err != nil {
		return err
	}
	if !isFiniteFloat64(startVal) {
		return fmt.Errorf("editor: field %q has non-finite value", spec.Field)
	}
	if isInt {
		spec.Integer = true
	}
	if spec.Step <= 0 {
		if spec.Integer {
			spec.Step = 1
		} else {
			spec.Step = 0.1
		}
	}
	pg := e.History.beginPreview("Scrub " + scrubFieldLabel(spec))
	e.inspectorScrub = inspectorScrubState{
		active:     true,
		spec:       spec,
		startVal:   startVal,
		currentVal: startVal,
		preview:    pg,
	}
	e.notify()
	return nil
}

// scrubPixelsPerStep is the horizontal drag distance in points per scrub increment.
const scrubPixelsPerStep = 4.0

// UpdateInspectorScrub updates the active scrub from the cumulative horizontal drag distance
// deltaPx (in points) since BeginInspectorScrub. When shift is true, 3D view scrubs in 0.01 fine
// steps (for non-integer fields) while 2D view steps ×10.
func (e *Editor) UpdateInspectorScrub(deltaPx float64, shift bool) (float64, error) {
	if e == nil || !e.inspectorScrub.active {
		return 0, errors.New("editor: no inspector scrub in progress")
	}
	if e.playing {
		e.CancelInspectorScrub()
		return 0, ErrPlaying
	}
	if !isFiniteFloat64(deltaPx) {
		return e.inspectorScrub.currentVal, errors.New("editor: scrub delta must be finite")
	}
	s := &e.inspectorScrub
	effStep := s.spec.Step
	if !isFiniteFloat64(effStep) || effStep <= 0 {
		effStep = 1
	}
	if shift {
		if e.ViewMode == ViewMode3D && !s.spec.Integer {
			effStep = 0.01
		} else {
			effStep *= 10
		}
	}
	if !isFiniteFloat64(effStep) {
		return s.currentVal, errors.New("editor: scrub step overflow")
	}
	steps := math.Round(deltaPx / scrubPixelsPerStep)
	rawNext := s.startVal + steps*effStep
	if !isFiniteFloat64(rawNext) {
		return s.currentVal, errors.New("editor: scrub value overflow")
	}
	next := roundToStepPrecision(rawNext, effStep)
	if !isFiniteFloat64(next) {
		next = rawNext
	}
	if s.spec.Min != nil && next < *s.spec.Min {
		next = *s.spec.Min
	}
	if s.spec.Max != nil && next > *s.spec.Max {
		next = *s.spec.Max
	}
	if s.spec.Integer {
		next = clampInt32Float(math.Round(next))
	} else if isFloat32ScrubField(s.spec.Field) && math.Abs(next) > math.MaxFloat32 {
		return s.currentVal, errors.New("editor: scrub value exceeds float32 range")
	}
	if next == s.currentVal {
		return s.currentVal, nil
	}

	// Roll back any prior preview command so History records a single step from startVal -> next.
	if s.preview.g != nil && len(s.preview.g.cmds) > 0 {
		s.preview.g.rollback(len(s.preview.g.cmds))
		s.preview.g.cmds = s.preview.g.cmds[:0]
	}
	if next != s.startVal {
		if err := applyInspectorNumericField(e, s.spec, next); err != nil {
			// Restore previous live value if the new value was refused (e.g. shear or bounds).
			if s.currentVal != s.startVal {
				_ = applyInspectorNumericField(e, s.spec, s.currentVal)
			}
			e.Rebuild()
			e.notify()
			return s.currentVal, err
		}
	}
	s.currentVal = next
	e.Rebuild()
	e.notify()
	return next, nil
}

// EndInspectorScrub finishes the active scrub and commits the net change as one undo step.
// It returns false (recording nothing) when no scrub is active or the value did not change.
func (e *Editor) EndInspectorScrub() bool {
	if e == nil || !e.inspectorScrub.active {
		return false
	}
	s := e.inspectorScrub
	cmds := e.History.endPreview(s.preview)
	e.inspectorScrub = inspectorScrubState{}

	if s.currentVal == s.startVal || len(cmds) == 0 {
		if len(cmds) > 0 {
			(&groupCmd{cmds: cmds}).rollback(len(cmds))
		}
		e.Rebuild()
		e.resumeAutoSaveAfterInteraction()
		e.notify()
		return false
	}
	label := ""
	if s.preview.g != nil {
		label = s.preview.g.label
	}
	committed := e.History.commitPreview(label, cmds)
	e.Rebuild()
	if committed {
		e.scheduleAutoSaveIfNeeded()
	} else {
		e.resumeAutoSaveAfterInteraction()
	}
	e.notify()
	return committed
}

// CancelInspectorScrub aborts the active scrub, restoring the original pre-scrub value without
// recording an undo step. It returns false when no scrub is active.
func (e *Editor) CancelInspectorScrub() bool {
	if e == nil || !e.inspectorScrub.active {
		return false
	}
	s := e.inspectorScrub
	cmds := e.History.endPreview(s.preview)
	e.inspectorScrub = inspectorScrubState{}
	if len(cmds) > 0 {
		(&groupCmd{cmds: cmds}).rollback(len(cmds))
	}
	e.Status = "Restored " + scrubFieldLabel(s.spec)
	e.Rebuild()
	e.resumeAutoSaveAfterInteraction()
	e.notify()
	return true
}

func scrubFieldLabel(spec InspectorScrubField) string {
	switch {
	case strings.HasPrefix(spec.Field, "prop:"):
		return strings.TrimPrefix(spec.Field, "prop:")
	case strings.HasPrefix(spec.Field, "comp:"):
		return strings.TrimPrefix(spec.Field, "comp:")
	default:
		return spec.ObjectID + "." + spec.Field
	}
}

func roundToStepPrecision(v, step float64) float64 {
	d := stepDecimals(step)
	if d > 6 {
		d = 6
	}
	pow := math.Pow(10, float64(d))
	r := math.Round(v*pow) / pow
	if r == 0 {
		return 0
	}
	return r
}

func stepDecimals(step float64) int {
	if step <= 0 || math.IsNaN(step) || math.IsInf(step, 0) {
		return 0
	}
	s := strconv.FormatFloat(step, 'f', -1, 64)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return len(s) - i - 1
	}
	return 0
}

func (e *Editor) readInspectorNumericField(obj *sceneformat.ObjectDoc, spec InspectorScrubField) (float64, bool, error) {
	switch spec.Field {
	case "x":
		return float64(obj.X), false, nil
	case "y":
		return float64(obj.Y), false, nil
	case "z":
		return float64(obj.Z), true, nil
	case "rotation":
		return float64(obj.Rotation), false, nil
	case "scaleX":
		return float64(obj.EffectiveScaleX()), false, nil
	case "scaleY":
		return float64(obj.EffectiveScaleY()), false, nil
	case "alpha":
		return float64(obj.EffectiveAlpha()), false, nil
	case "originX":
		return float64(obj.OriginX), false, nil
	case "originY":
		return float64(obj.OriginY), false, nil
	case "posZ":
		return float64(obj.PosZ), false, nil
	case "rotX":
		return float64(obj.RotX), false, nil
	case "rotY":
		return float64(obj.RotY), false, nil
	case "rotZ":
		return float64(obj.RotZ), false, nil
	case "scaleZ":
		return float64(obj.EffectiveScaleZ()), false, nil
	case "depth":
		return float64(obj.Depth), false, nil
	}
	if key, ok := strings.CutPrefix(spec.Field, "prop:"); ok {
		raw, exists := obj.Properties[key]
		if !exists {
			return 0, false, fmt.Errorf("editor: object %q has no property %q", obj.ID, key)
		}
		switch n := raw.(type) {
		case int:
			return float64(n), true, nil
		case int64:
			return float64(n), true, nil
		case int32:
			return float64(n), true, nil
		case float64:
			return n, false, nil
		case float32:
			return float64(n), false, nil
		default:
			return 0, false, fmt.Errorf("editor: property %q is not numeric", key)
		}
	}
	if rest, ok := strings.CutPrefix(spec.Field, "comp:"); ok {
		comp, field, ok := strings.Cut(rest, ".")
		if !ok || comp == "" || field == "" {
			return 0, false, fmt.Errorf("editor: invalid component scrub target %q", spec.Field)
		}
		var raw any
		if cm, ok := obj.Components[comp].(map[string]any); ok {
			raw = cm[field]
		}
		isInt := spec.Integer
		if raw == nil {
			if ct, ok := e.lookupComponentType(comp); ok {
				if f, ok := ct.Field(field); ok {
					raw = f.Default
					isInt = f.Kind == sceneformat.FieldInt
				}
			}
		}
		if raw == nil {
			return 0, isInt, nil
		}
		switch n := raw.(type) {
		case int:
			return float64(n), true, nil
		case int64:
			return float64(n), true, nil
		case int32:
			return float64(n), true, nil
		case float64:
			return n, isInt, nil
		case float32:
			return float64(n), isInt, nil
		default:
			return 0, false, fmt.Errorf("editor: component field %s.%s is not numeric", comp, field)
		}
	}
	return 0, false, fmt.Errorf("editor: unknown numeric scrub field %q", spec.Field)
}

func isFloat32ScrubField(field string) bool {
	switch field {
	case "x", "y", "rotation", "scaleX", "scaleY", "alpha", "originX", "originY",
		"posZ", "rotX", "rotY", "rotZ", "scaleZ", "depth":
		return true
	default:
		return false
	}
}

func applyInspectorNumericField(e *Editor, spec InspectorScrubField, v float64) error {
	if !isFiniteFloat64(v) {
		return errors.New("editor: scrub value must be finite")
	}
	if isFloat32ScrubField(spec.Field) && math.Abs(v) > math.MaxFloat32 {
		return errors.New("editor: scrub value exceeds float32 range")
	}
	obj, _ := e.FindResolvedObject(spec.ObjectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", spec.ObjectID)
	}
	switch spec.Field {
	case "x":
		return e.MoveObject(spec.ObjectID, float32(v), obj.Y)
	case "y":
		return e.MoveObject(spec.ObjectID, obj.X, float32(v))
	case "z":
		return e.SetObjectZ(spec.ObjectID, int(clampInt32Float(math.Round(v))))
	case "rotation":
		return e.RotateObject(spec.ObjectID, float32(v))
	case "scaleX":
		return e.ScaleObject(spec.ObjectID, float32(v), obj.EffectiveScaleY())
	case "scaleY":
		return e.ScaleObject(spec.ObjectID, obj.EffectiveScaleX(), float32(v))
	case "alpha":
		return e.SetObjectAlpha(spec.ObjectID, float32(v))
	case "originX":
		return e.SetObjectOrigin(spec.ObjectID, float32(v), obj.OriginY)
	case "originY":
		return e.SetObjectOrigin(spec.ObjectID, obj.OriginX, float32(v))
	case "posZ":
		return e.MoveObject3D(spec.ObjectID, obj.X, obj.Y, float32(v))
	case "rotX":
		return e.RotateObject3D(spec.ObjectID, float32(v), obj.RotY, obj.RotZ)
	case "rotY":
		return e.RotateObject3D(spec.ObjectID, obj.RotX, float32(v), obj.RotZ)
	case "rotZ":
		return e.RotateObject3D(spec.ObjectID, obj.RotX, obj.RotY, float32(v))
	case "scaleZ":
		return e.ScaleObject3D(spec.ObjectID, obj.EffectiveScaleX(), obj.EffectiveScaleY(), float32(v))
	case "depth":
		return e.SetObjectDepth(spec.ObjectID, float32(v))
	}
	if key, ok := strings.CutPrefix(spec.Field, "prop:"); ok {
		if spec.Integer {
			return e.SetProperty(spec.ObjectID, key, int(clampInt32Float(math.Round(v))))
		}
		return e.SetProperty(spec.ObjectID, key, v)
	}
	if rest, ok := strings.CutPrefix(spec.Field, "comp:"); ok {
		comp, field, ok := strings.Cut(rest, ".")
		if !ok || comp == "" || field == "" {
			return fmt.Errorf("editor: invalid component scrub target %q", spec.Field)
		}
		if spec.Integer {
			return e.SetComponentField(spec.ObjectID, comp, field, int(clampInt32Float(math.Round(v))))
		}
		return e.SetComponentField(spec.ObjectID, comp, field, v)
	}
	return fmt.Errorf("editor: unknown numeric scrub field %q", spec.Field)
}

// inspectorObjectKindLabel returns a short human-readable kind label for the inspector header
// subtitle (such as "Sprite", "Prefab instance", "Model", "Light", "Camera", or "Object").
func inspectorObjectKindLabel(obj *sceneformat.ObjectDoc) string {
	if obj == nil {
		return "Object"
	}
	switch {
	case obj.Prefab != "" || obj.Scene != "":
		return "Prefab instance"
	case obj.Light != nil:
		return "Light"
	case obj.Camera != nil:
		return "Camera"
	case obj.ReflectionProbe != nil:
		return "Reflection probe"
	case obj.LightProbeGrid != nil:
		return "Light probe grid"
	case obj.Model != "":
		return "Model"
	case obj.GridMap != "":
		return "GridMap"
	case obj.Mesh != "":
		return "Mesh"
	case obj.Particles != nil:
		return "Particles"
	case obj.Sprite != nil:
		return "Sprite"
	default:
		return "Object"
	}
}

// objectSwatchColor returns a Spec token swatch color identifying the object's kind in the
// inspector header.
func objectSwatchColor(obj *sceneformat.ObjectDoc) core.Color {
	if obj == nil {
		return specTextMuted
	}
	switch {
	case obj.Prefab != "" || obj.Scene != "":
		return specAgent
	case obj.Light != nil:
		return specWarning
	case obj.Camera != nil:
		return specAccent
	case obj.ReflectionProbe != nil || obj.LightProbeGrid != nil:
		return specAccent
	case obj.Model != "" || obj.Mesh != "" || obj.GridMap != "":
		return specAdded
	case obj.Sprite != nil:
		return specChanged
	default:
		return specTextMuted
	}
}

// matchesInspectorFilter reports whether a field (given its section title, optional component name,
// and field label/keys) matches the case-insensitive substring query.
func matchesInspectorFilter(query string, sectionTitle, componentName string, fieldLabels ...string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	if componentName != "" && strings.Contains(strings.ToLower(componentName), q) {
		return true
	}
	if sectionTitle != "" && strings.Contains(strings.ToLower(sectionTitle), q) {
		return true
	}
	for _, label := range fieldLabels {
		if label != "" && strings.Contains(strings.ToLower(label), q) {
			return true
		}
	}
	return false
}
