package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// CurrentTaskVersion is the schema version of agent evaluation task files.
const CurrentTaskVersion = 1

// Task defines one fixed evaluation task loaded from a plain-text versioned YAML file.
type Task struct {
	Version      int        `yaml:"version"`
	ID           string     `yaml:"id"`
	Title        string     `yaml:"title"`
	Description  string     `yaml:"description"`
	Prompt       string     `yaml:"prompt"`
	MinScore     *int       `yaml:"min_score,omitempty"`
	ContextDocs  []string   `yaml:"context_docs"`
	StarterDir   string     `yaml:"starter_dir"`
	ReferenceDir string     `yaml:"reference_dir"`
	Checks       ChecksSpec `yaml:"checks"`
	// Family groups tasks by workflow so reports can identify editor, runtime, data, 3D and safety coverage.
	Family string `yaml:"family,omitempty"`
	// MCPServers lists the staged MCP server names the task expects an agent to use.
	MCPServers []string `yaml:"mcp_servers,omitempty"`
}

// ChecksSpec declares the deterministic grading rubric for a Task.
type ChecksSpec struct {
	YAMLFiles       []string        `yaml:"yaml_files"`
	PlatformerLevel *PlatformerSpec `yaml:"platformer_level,omitempty"`
	EnemySpec       *EnemySpec      `yaml:"enemy_spec,omitempty"`
	MenuSpec        *MenuSpec       `yaml:"menu_spec,omitempty"`
	MCPVerify       *MCPVerifySpec  `yaml:"mcp_verify,omitempty"`
	RequiredTools   []string        `yaml:"required_tools,omitempty"`
	Weights         CriteriaWeights `yaml:"weights"`
}

// CriteriaWeights distributes 100 points across the five rubric checks.
type CriteriaWeights struct {
	YAMLValid    int `yaml:"yaml_valid"`
	ProjectCheck int `yaml:"project_check"`
	DomainCheck  int `yaml:"domain_check"`
	BuildCheck   int `yaml:"build_check"`
	MCPVerify    int `yaml:"mcp_verify"`
}

// PlatformerSpec validates a platformer level scene's structure and reachability.
type PlatformerSpec struct {
	SceneFile    string  `yaml:"scene_file"`
	MinPlatforms int     `yaml:"min_platforms"`
	MaxGap       float64 `yaml:"max_gap"`
	MaxRise      float64 `yaml:"max_rise"`
	PlayerID     string  `yaml:"player_id"`
	GoalID       string  `yaml:"goal_id"`
}

// EnemySpec validates enemy definitions placed in a scene.
type EnemySpec struct {
	SceneFile         string `yaml:"scene_file"`
	MinEnemies        int    `yaml:"min_enemies"`
	RequiredComponent string `yaml:"required_component,omitempty"`
}

// MenuSpec validates a menu scene's screen-space layer and its buttons (objects with prefab `button` or a `button` component).
type MenuSpec struct {
	SceneFile          string `yaml:"scene_file"`
	MinMenuItems       int    `yaml:"min_menu_items"`
	RequireScreenLayer bool   `yaml:"require_screen_layer"`
}

// MCPVerifySpec configures live headless verification through game/mcp and nsgo/automation.
type MCPVerifySpec struct {
	MinScenes          int      `yaml:"min_scenes,omitempty"`
	RequireSceneNames  []string `yaml:"require_scene_names,omitempty"`
	InspectLayerScene  string   `yaml:"inspect_layer_scene,omitempty"`
	RequireLayerSpace  string   `yaml:"require_layer_space,omitempty"`
	RequireEntityTag   string   `yaml:"require_entity_tag,omitempty"`
	RequireEntityNames []string `yaml:"require_entity_names,omitempty"`
	StepCount          int      `yaml:"step_count,omitempty"`
	ExpectMovedEntity  string   `yaml:"expect_moved_entity,omitempty"`
	ExpectComponent    string   `yaml:"expect_component,omitempty"`
	// PressActions are input actions held through game_input_action before stepping, so movement comes from input.
	PressActions []string `yaml:"press_actions,omitempty"`
	// ApproachEntity names an entity that ExpectMovedEntity must get horizontally closer to while stepping.
	ApproachEntity string `yaml:"approach_entity,omitempty"`
	// Click is a pointer click sent through game_input_pointer after stepping, with the scene it must switch to.
	Click *ClickSpec `yaml:"click,omitempty"`
}

// ClickSpec is a click at game coordinates (X, Y) that must make ExpectScene the current scene.
type ClickSpec struct {
	X           float64 `yaml:"x"`
	Y           float64 `yaml:"y"`
	ExpectScene string  `yaml:"expect_scene"`
}

// ParseTask decodes a versioned YAML task definition, refusing unknown fields and trailing documents.
func ParseTask(data []byte) (*Task, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var t Task
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("parse task YAML: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("parse task YAML: unexpected trailing document")
		}
		return nil, fmt.Errorf("parse task YAML: %w", err)
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

// EncodeTask validates t and writes deterministic 2-space YAML that round-trips unchanged.
func EncodeTask(t *Task) ([]byte, error) {
	if t == nil {
		return nil, errors.New("encode task YAML: nil task")
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(t); err != nil {
		return nil, fmt.Errorf("encode task YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode task YAML: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks every required field and invariant on t.
func (t *Task) Validate() error {
	if t.Version != CurrentTaskVersion {
		return fmt.Errorf("task %q: unsupported version %d (want %d)", t.ID, t.Version, CurrentTaskVersion)
	}
	if strings.TrimSpace(t.ID) == "" {
		return errors.New("task: id is required")
	}
	if err := validateTaskID(t.ID); err != nil {
		return fmt.Errorf("task %q: %w", t.ID, err)
	}
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("task %q: title is required", t.ID)
	}
	if strings.TrimSpace(t.Description) == "" {
		return fmt.Errorf("task %q: description is required", t.ID)
	}
	if strings.TrimSpace(t.Prompt) == "" {
		return fmt.Errorf("task %q: prompt is required", t.ID)
	}
	if t.Family != "" && !slices.Contains([]string{"create", "editor", "debug", "3d", "data", "code", "safety"}, t.Family) {
		return fmt.Errorf("task %q: unknown family %q", t.ID, t.Family)
	}
	for i, server := range t.MCPServers {
		if strings.TrimSpace(server) == "" || strings.ContainsAny(server, " \t\n") {
			return fmt.Errorf("task %q: invalid mcp_servers entry %q", t.ID, server)
		}
		if slices.Contains(t.MCPServers[:i], server) {
			return fmt.Errorf("task %q: mcp_servers contains duplicate %q", t.ID, server)
		}
	}
	if t.MinScore != nil && (*t.MinScore < 0 || *t.MinScore > 100) {
		return fmt.Errorf("task %q: min_score must be in [0, 100] (got %d)", t.ID, *t.MinScore)
	}
	if len(t.ContextDocs) == 0 {
		return fmt.Errorf("task %q: context_docs must list at least one document", t.ID)
	}
	for _, doc := range t.ContextDocs {
		if err := validateRelPath(doc); err != nil {
			return fmt.Errorf("task %q: context_docs entry %q: %w", t.ID, doc, err)
		}
	}
	if err := validateRelPath(t.StarterDir); err != nil {
		return fmt.Errorf("task %q: starter_dir %q: %w", t.ID, t.StarterDir, err)
	}
	if err := validateRelPath(t.ReferenceDir); err != nil {
		return fmt.Errorf("task %q: reference_dir %q: %w", t.ID, t.ReferenceDir, err)
	}
	if len(t.Checks.YAMLFiles) == 0 {
		return fmt.Errorf("task %q: checks.yaml_files must list at least one file", t.ID)
	}
	for _, yf := range t.Checks.YAMLFiles {
		if err := validateRelPath(yf); err != nil {
			return fmt.Errorf("task %q: checks.yaml_files entry %q: %w", t.ID, yf, err)
		}
	}

	domainCount := 0
	if t.Checks.PlatformerLevel != nil {
		domainCount++
		if err := t.Checks.PlatformerLevel.validate(t.ID); err != nil {
			return err
		}
	}
	if t.Checks.EnemySpec != nil {
		domainCount++
		if err := t.Checks.EnemySpec.validate(t.ID); err != nil {
			return err
		}
	}
	if t.Checks.MenuSpec != nil {
		domainCount++
		if err := t.Checks.MenuSpec.validate(t.ID); err != nil {
			return err
		}
	}
	if domainCount != 1 {
		return fmt.Errorf("task %q: exactly one domain check spec (platformer_level, enemy_spec, or menu_spec) is required (got %d)", t.ID, domainCount)
	}
	if t.Checks.MCPVerify == nil {
		return fmt.Errorf("task %q: checks.mcp_verify is required", t.ID)
	}
	if err := validateRequiredTools(t.ID, t.Checks.RequiredTools); err != nil {
		return err
	}
	if err := t.Checks.MCPVerify.validate(t.ID); err != nil {
		return err
	}
	if err := t.Checks.Weights.validate(t.ID); err != nil {
		return err
	}
	return nil
}

func (m *MCPVerifySpec) validate(taskID string) error {
	if m.StepCount < 0 || m.StepCount > 3600 {
		return fmt.Errorf("task %q: checks.mcp_verify.step_count must be in [0, 3600] (got %d)", taskID, m.StepCount)
	}
	if m.ExpectMovedEntity != "" && m.StepCount == 0 {
		return fmt.Errorf("task %q: checks.mcp_verify.expect_moved_entity requires step_count > 0", taskID)
	}
	if m.ExpectComponent != "" && m.ExpectMovedEntity == "" {
		return fmt.Errorf("task %q: checks.mcp_verify.expect_component requires expect_moved_entity", taskID)
	}
	if m.ApproachEntity != "" && m.ExpectMovedEntity == "" {
		return fmt.Errorf("task %q: checks.mcp_verify.approach_entity requires expect_moved_entity", taskID)
	}
	if len(m.PressActions) > 0 && m.StepCount == 0 {
		return fmt.Errorf("task %q: checks.mcp_verify.press_actions requires step_count > 0", taskID)
	}
	for _, a := range m.PressActions {
		if strings.TrimSpace(a) == "" {
			return fmt.Errorf("task %q: checks.mcp_verify.press_actions has an empty name", taskID)
		}
	}
	if c := m.Click; c != nil {
		if math.IsNaN(c.X) || math.IsInf(c.X, 0) || math.IsNaN(c.Y) || math.IsInf(c.Y, 0) {
			return fmt.Errorf("task %q: checks.mcp_verify.click x and y must be finite", taskID)
		}
		if strings.TrimSpace(c.ExpectScene) == "" {
			return fmt.Errorf("task %q: checks.mcp_verify.click.expect_scene is required", taskID)
		}
	}
	return nil
}

func validateTaskID(id string) error {
	if strings.Contains(id, "..") {
		return errors.New("id must not contain '..'")
	}
	for i, r := range id {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		isPunct := r == '-' || r == '_'
		if (i == 0 && !isAlnum) || (!isAlnum && !isPunct) {
			return fmt.Errorf("id must match [A-Za-z0-9][A-Za-z0-9_-]* (got %q)", id)
		}
	}
	return nil
}

func validateRelPath(p string) error {
	if strings.TrimSpace(p) == "" {
		return errors.New("path is required")
	}
	if strings.ContainsRune(p, '\\') || strings.ContainsRune(p, 0) {
		return errors.New("path must use forward slashes without NUL bytes")
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) || filepath.VolumeName(filepath.FromSlash(p)) != "" {
		return errors.New("path must be relative")
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("path must not escape its root directory")
	}
	return nil
}

func safeJoin(base, rel string) (string, error) {
	if err := validateRelPath(rel); err != nil {
		return "", fmt.Errorf("invalid relative path %q: %w", rel, err)
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	target := filepath.Join(absBase, filepath.FromSlash(path.Clean(rel)))
	relCheck, err := filepath.Rel(absBase, target)
	if err != nil {
		return "", err
	}
	if relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) || filepath.IsAbs(relCheck) {
		return "", fmt.Errorf("path %q escapes base %q", rel, base)
	}
	return target, nil
}

func (p *PlatformerSpec) validate(taskID string) error {
	if err := validateRelPath(p.SceneFile); err != nil {
		return fmt.Errorf("task %q: platformer_level.scene_file %q: %w", taskID, p.SceneFile, err)
	}
	if p.MinPlatforms <= 0 {
		return fmt.Errorf("task %q: platformer_level.min_platforms must be > 0", taskID)
	}
	if !(p.MaxGap > 0) || math.IsInf(p.MaxGap, 0) {
		return fmt.Errorf("task %q: platformer_level.max_gap must be a finite number > 0 (got %v)", taskID, p.MaxGap)
	}
	if !(p.MaxRise > 0) || math.IsInf(p.MaxRise, 0) {
		return fmt.Errorf("task %q: platformer_level.max_rise must be a finite number > 0 (got %v)", taskID, p.MaxRise)
	}
	if strings.TrimSpace(p.PlayerID) == "" || strings.TrimSpace(p.GoalID) == "" {
		return fmt.Errorf("task %q: platformer_level player_id and goal_id are required", taskID)
	}
	return nil
}

func (e *EnemySpec) validate(taskID string) error {
	if err := validateRelPath(e.SceneFile); err != nil {
		return fmt.Errorf("task %q: enemy_spec.scene_file %q: %w", taskID, e.SceneFile, err)
	}
	if e.MinEnemies <= 0 {
		return fmt.Errorf("task %q: enemy_spec.min_enemies must be > 0", taskID)
	}
	return nil
}

func (m *MenuSpec) validate(taskID string) error {
	if err := validateRelPath(m.SceneFile); err != nil {
		return fmt.Errorf("task %q: menu_spec.scene_file %q: %w", taskID, m.SceneFile, err)
	}
	if m.MinMenuItems <= 0 {
		return fmt.Errorf("task %q: menu_spec.min_menu_items must be > 0", taskID)
	}
	return nil
}

func (w CriteriaWeights) validate(taskID string) error {
	values := []struct {
		name string
		val  int
	}{
		{"yaml_valid", w.YAMLValid},
		{"project_check", w.ProjectCheck},
		{"domain_check", w.DomainCheck},
		{"build_check", w.BuildCheck},
		{"mcp_verify", w.MCPVerify},
	}
	total := 0
	for _, item := range values {
		if item.val <= 0 {
			return fmt.Errorf("task %q: weight %s must be positive (got %d)", taskID, item.name, item.val)
		}
		total += item.val
	}
	if total != 100 {
		return fmt.Errorf("task %q: criteria weights must sum to 100 (got %d)", taskID, total)
	}
	return nil
}

// EffectiveMinScore returns the passing score threshold for t, falling back to fallback when min_score is unset (an explicit 0 is kept).
func (t *Task) EffectiveMinScore(fallback int) int {
	if t.MinScore != nil {
		return *t.MinScore
	}
	return fallback
}

// LoadTasksDir loads and validates all *.yaml task files from dir in sorted filename order.
func LoadTasksDir(dir string) ([]*Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read tasks directory %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no task YAML files found in %s", dir)
	}

	seen := make(map[string]string, len(names))
	tasks := make([]*Task, 0, len(names))
	for _, name := range names {
		p := filepath.Join(dir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read task %s: %w", p, err)
		}
		task, err := ParseTask(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if prev, dup := seen[task.ID]; dup {
			return nil, fmt.Errorf("duplicate task id %q in %s and %s", task.ID, prev, name)
		}
		seen[task.ID] = name
		tasks = append(tasks, task)
	}
	return tasks, nil
}
