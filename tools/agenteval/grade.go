package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"go.yaml.in/yaml/v3"
)

// GradeWorkspace evaluates workspaceDir against every rubric criterion defined in task, building it against this checkout's packages/game through a temporary go.work.
func GradeWorkspace(ctx context.Context, repoRoot, workspaceDir string, task *Task, minScore int) TaskReport {
	if resolved, err := filepath.EvalSymlinks(workspaceDir); err == nil {
		workspaceDir = resolved
	}
	workFile, cleanup, err := prepareGoWorkspace(ctx, repoRoot, workspaceDir)
	if err != nil {
		workFile = ""
	} else {
		defer cleanup()
	}
	rep := gradeWorkspace(ctx, workspaceDir, workFile, task, minScore)
	if err != nil {
		for i := range rep.Criteria {
			if rep.Criteria[i].Name == "builds_cleanly" || rep.Criteria[i].Name == "mcp_verify_passes" {
				rep.Criteria[i].Detail = fmt.Sprintf("set up temporary go.work: %v", err)
			}
		}
	}
	return rep
}

// gradeWorkspace scores workspaceDir with workFile as its GOWORK; an empty workFile fails the build and MCP criteria.
func gradeWorkspace(ctx context.Context, workspaceDir, workFile string, task *Task, minScore int) TaskReport {
	criteria := make([]CriterionResult, 0, 5)

	// 1. Versioned YAML project & scene files parse and round-trip deterministically.
	criteria = append(criteria, gradeYAMLFiles(workspaceDir, task))

	// 2. assetcheck.Check reports zero problems on the workspace.
	criteria = append(criteria, gradeProjectCheck(workspaceDir, task))

	// 3. Task-specific domain validation (platformer geometry, enemy definition, or menu scene).
	criteria = append(criteria, gradeDomain(workspaceDir, task))

	if workFile == "" {
		criteria = append(criteria,
			CriterionResult{Name: "builds_cleanly", Weight: task.Checks.Weights.BuildCheck, Detail: "no go.work"},
			CriterionResult{Name: "mcp_verify_passes", Weight: task.Checks.Weights.MCPVerify, Detail: "no go.work"},
		)
	} else {
		// 4. Build and vet cleanly with go tool nsgo check.
		criteria = append(criteria, gradeBuildCheck(ctx, workspaceDir, workFile, task))
		// 5. Live headless MCP verification through nsgo test + automation.Call.
		criteria = append(criteria, gradeMCPVerify(ctx, workspaceDir, workFile, task))
	}

	score := 0
	maxScore := 0
	for _, c := range criteria {
		score += c.Earned
		maxScore += c.Weight
	}

	return TaskReport{
		ID:       task.ID,
		Title:    task.Title,
		Score:    score,
		MaxScore: maxScore,
		MinScore: minScore,
		Passed:   score >= minScore,
		Criteria: criteria,
	}
}

func gradeYAMLFiles(workspaceDir string, task *Task) CriterionResult {
	w := task.Checks.Weights.YAMLValid
	var problems []string
	for _, rel := range task.Checks.YAMLFiles {
		p, err := safeJoin(workspaceDir, rel)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
			continue
		}
		base := filepath.Base(rel)
		lower := strings.ToLower(rel)
		switch {
		case base == "game.yaml":
			f, err := project.Parse(data)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
				continue
			}
			enc1, err := project.Encode(f)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: encode failed: %v", rel, err))
				continue
			}
			f2, err := project.Parse(enc1)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: re-parse failed: %v", rel, err))
				continue
			}
			enc2, err := project.Encode(f2)
			if err != nil || !bytes.Equal(enc1, enc2) {
				problems = append(problems, fmt.Sprintf("%s: non-deterministic round-trip", rel))
			}
		case strings.HasSuffix(lower, ".scene.yaml") || strings.HasSuffix(lower, ".scene.yml"):
			doc, err := sceneformat.Parse(data)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
				continue
			}
			enc1, err := sceneformat.Encode(doc)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: encode failed: %v", rel, err))
				continue
			}
			doc2, err := sceneformat.Parse(enc1)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: re-parse failed: %v", rel, err))
				continue
			}
			enc2, err := sceneformat.Encode(doc2)
			if err != nil || !bytes.Equal(enc1, enc2) {
				problems = append(problems, fmt.Sprintf("%s: non-deterministic round-trip", rel))
			}
		default:
			if len(bytes.TrimSpace(data)) == 0 {
				problems = append(problems, fmt.Sprintf("%s: empty YAML document", rel))
				continue
			}
			dec := yaml.NewDecoder(bytes.NewReader(data))
			var node yaml.Node
			if err := dec.Decode(&node); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
				continue
			}
			var extra any
			if err := dec.Decode(&extra); err != io.EOF {
				if err == nil {
					problems = append(problems, fmt.Sprintf("%s: unexpected trailing YAML document", rel))
				} else {
					problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
				}
			}
		}
	}
	if len(problems) > 0 {
		return CriterionResult{
			Name:   "yaml_valid",
			Weight: w,
			Detail: strings.Join(problems, "; "),
		}
	}
	return CriterionResult{
		Name:   "yaml_valid",
		Weight: w,
		Earned: w,
		Passed: true,
		Detail: fmt.Sprintf("validated %d YAML files with deterministic round-trip", len(task.Checks.YAMLFiles)),
	}
}

func gradeProjectCheck(workspaceDir string, task *Task) CriterionResult {
	w := task.Checks.Weights.ProjectCheck
	rep, err := assetcheck.Check(os.DirFS(workspaceDir))
	if err != nil {
		return CriterionResult{
			Name:   "project_check_passes",
			Weight: w,
			Detail: fmt.Sprintf("assetcheck error: %v", err),
		}
	}
	if !rep.OK() {
		msgs := make([]string, 0, min(len(rep.Problems), 4))
		for _, p := range rep.Problems {
			if len(msgs) >= 4 {
				msgs = append(msgs, fmt.Sprintf("... and %d more", len(rep.Problems)-4))
				break
			}
			msgs = append(msgs, p.String())
		}
		return CriterionResult{
			Name:   "project_check_passes",
			Weight: w,
			Detail: strings.Join(msgs, "; "),
		}
	}
	return CriterionResult{
		Name:   "project_check_passes",
		Weight: w,
		Earned: w,
		Passed: true,
		Detail: fmt.Sprintf("0 problems (%d scenes, %d maps, %d atlases)", rep.Scenes, rep.Maps, rep.Atlases),
	}
}

func gradeDomain(workspaceDir string, task *Task) CriterionResult {
	switch {
	case task.Checks.PlatformerLevel != nil:
		return gradePlatformerLevel(workspaceDir, task.Checks.PlatformerLevel, task.Checks.Weights.DomainCheck)
	case task.Checks.EnemySpec != nil:
		return gradeEnemySpec(workspaceDir, task.Checks.EnemySpec, task.Checks.Weights.DomainCheck)
	case task.Checks.MenuSpec != nil:
		return gradeMenuSpec(workspaceDir, task.Checks.MenuSpec, task.Checks.Weights.DomainCheck)
	default:
		return CriterionResult{
			Name:   "domain_check",
			Weight: task.Checks.Weights.DomainCheck,
			Detail: "no domain check spec configured",
		}
	}
}

type xInterval struct {
	start float64
	end   float64
	y     float64
}

func gradePlatformerLevel(workspaceDir string, spec *PlatformerSpec, weight int) CriterionResult {
	const name = "level_geometry_valid"
	doc, err := loadSceneFile(workspaceDir, spec.SceneFile)
	if err != nil {
		return CriterionResult{Name: name, Weight: weight, Detail: err.Error()}
	}

	var (
		playerObj *sceneformat.ObjectDoc
		goalObj   *sceneformat.ObjectDoc
		platforms []xInterval
	)

	for i := range doc.Layers {
		layer := &doc.Layers[i]
		if !layer.IsVisible() || layer.EffectiveSpace() != sceneformat.SpaceWorld {
			continue
		}
		walkLayerObjects(layer.Objects, func(o *sceneformat.ObjectDoc) {
			if o.ID == spec.PlayerID || strings.EqualFold(o.Prefab, spec.PlayerID) {
				playerObj = o
			}
			if o.ID == spec.GoalID || strings.EqualFold(o.Prefab, spec.GoalID) {
				goalObj = o
			}
			if isPlatformObject(o) {
				w := objectWidth(o, 64)
				platforms = append(platforms, xInterval{start: float64(o.X), end: float64(o.X) + w, y: float64(o.Y)})
			}
		})
	}

	if playerObj == nil {
		return CriterionResult{Name: name, Weight: weight, Detail: fmt.Sprintf("missing player spawn %q in %s", spec.PlayerID, spec.SceneFile)}
	}
	if goalObj == nil {
		return CriterionResult{Name: name, Weight: weight, Detail: fmt.Sprintf("missing goal %q in %s", spec.GoalID, spec.SceneFile)}
	}
	if playerObj.ID == goalObj.ID || math.Hypot(float64(goalObj.X-playerObj.X), float64(goalObj.Y-playerObj.Y)) < 32 {
		return CriterionResult{Name: name, Weight: weight, Detail: "player spawn and goal must be distinct and separated"}
	}
	if len(platforms) < spec.MinPlatforms {
		return CriterionResult{Name: name, Weight: weight, Detail: fmt.Sprintf("found %d platforms, want at least %d", len(platforms), spec.MinPlatforms)}
	}

	// Verify platform chain spans from player X to goal X without any gap exceeding MaxGap.
	slices.SortFunc(platforms, func(a, b xInterval) int {
		if c := cmp.Compare(a.start, b.start); c != 0 {
			return c
		}
		return cmp.Compare(a.end, b.end)
	})
	// Walk from the leftmost of spawn and goal to the rightmost, ignoring platforms that end before it. The first platform must start within max_gap of it, each next one must start within max_gap of the reach so far, and each platform that extends the reach must sit within max_rise vertically of the previous one; a platform that is too high or too low is skipped as unusable.
	minX := min(float64(playerObj.X), float64(goalObj.X))
	maxX := max(float64(playerObj.X), float64(goalObj.X))
	reach := minX
	var lastY float64
	bridged := false
	skipped := 0
	for _, iv := range platforms {
		if iv.end < minX || (bridged && iv.end <= reach) {
			continue
		}
		if iv.start > reach+spec.MaxGap {
			break
		}
		if bridged && math.Abs(iv.y-lastY) > spec.MaxRise {
			skipped++
			continue
		}
		bridged = true
		reach = max(reach, iv.end)
		lastY = iv.y
		if reach >= maxX {
			break
		}
	}
	if !bridged || reach < maxX {
		detail := fmt.Sprintf("platforms reach x=%.1f from x=%.1f, goal side at x=%.1f is unreachable (max gap %.0f, max rise %.0f)", reach, minX, maxX, spec.MaxGap, spec.MaxRise)
		if skipped > 0 {
			detail += fmt.Sprintf("; %d platforms skipped for a vertical step above max rise", skipped)
		}
		return CriterionResult{Name: name, Weight: weight, Detail: detail}
	}

	return CriterionResult{
		Name:   name,
		Weight: weight,
		Earned: weight,
		Passed: true,
		Detail: fmt.Sprintf("player %q -> goal %q bridged by %d platforms (max gap <= %.0f, max rise <= %.0f)", playerObj.ID, goalObj.ID, len(platforms), spec.MaxGap, spec.MaxRise),
	}
}

func gradeEnemySpec(workspaceDir string, spec *EnemySpec, weight int) CriterionResult {
	const name = "enemy_definition_present"
	doc, err := loadSceneFile(workspaceDir, spec.SceneFile)
	if err != nil {
		return CriterionResult{Name: name, Weight: weight, Detail: err.Error()}
	}

	enemies := 0
	withComp := 0
	for i := range doc.Layers {
		if !doc.Layers[i].IsVisible() {
			continue
		}
		walkLayerObjects(doc.Layers[i].Objects, func(o *sceneformat.ObjectDoc) {
			if !isEnemyObject(o) {
				return
			}
			enemies++
			if spec.RequiredComponent == "" {
				withComp++
				return
			}
			if _, ok := o.Components[spec.RequiredComponent]; ok {
				withComp++
			}
		})
	}
	if enemies < spec.MinEnemies {
		return CriterionResult{
			Name:   name,
			Weight: weight,
			Detail: fmt.Sprintf("found %d enemy objects in %s, want at least %d", enemies, spec.SceneFile, spec.MinEnemies),
		}
	}
	if spec.RequiredComponent != "" && withComp < spec.MinEnemies {
		return CriterionResult{
			Name:   name,
			Weight: weight,
			Detail: fmt.Sprintf("only %d of %d enemies carry required component %q", withComp, enemies, spec.RequiredComponent),
		}
	}
	return CriterionResult{
		Name:   name,
		Weight: weight,
		Earned: weight,
		Passed: true,
		Detail: fmt.Sprintf("found %d enemy objects in %s", enemies, spec.SceneFile),
	}
}

func gradeMenuSpec(workspaceDir string, spec *MenuSpec, weight int) CriterionResult {
	const name = "menu_scene_valid"
	doc, err := loadSceneFile(workspaceDir, spec.SceneFile)
	if err != nil {
		return CriterionResult{Name: name, Weight: weight, Detail: err.Error()}
	}

	screenLayers := 0
	menuItems := 0
	for i := range doc.Layers {
		layer := &doc.Layers[i]
		if !layer.IsVisible() {
			continue
		}
		isScreen := layer.EffectiveSpace() == sceneformat.SpaceScreen
		if isScreen {
			screenLayers++
		}
		if !spec.RequireScreenLayer || isScreen {
			walkLayerObjects(layer.Objects, func(o *sceneformat.ObjectDoc) {
				if isButtonObject(o) {
					menuItems++
				}
			})
		}
	}
	if spec.RequireScreenLayer && screenLayers == 0 {
		return CriterionResult{
			Name:   name,
			Weight: weight,
			Detail: fmt.Sprintf("%s has no visible screen-space layer", spec.SceneFile),
		}
	}
	if menuItems < spec.MinMenuItems {
		return CriterionResult{
			Name:   name,
			Weight: weight,
			Detail: fmt.Sprintf("%s has %d buttons, want at least %d", spec.SceneFile, menuItems, spec.MinMenuItems),
		}
	}
	return CriterionResult{
		Name:   name,
		Weight: weight,
		Earned: weight,
		Passed: true,
		Detail: fmt.Sprintf("%d screen layers and %d buttons in %s", screenLayers, menuItems, spec.SceneFile),
	}
}

func gradeBuildCheck(ctx context.Context, workspaceDir, workFile string, task *Task) CriterionResult {
	const name = "builds_cleanly"
	w := task.Checks.Weights.BuildCheck
	cmd := exec.CommandContext(ctx, "go", "tool", "nsgo", "check")
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = workspaceDir
	cmd.Env = cleanGoEnv(workFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return CriterionResult{
			Name:   name,
			Weight: w,
			Detail: fmt.Sprintf("nsgo check failed: %v (%s)", err, compactOutput(out, 240)),
		}
	}
	return CriterionResult{
		Name:   name,
		Weight: w,
		Earned: w,
		Passed: true,
		Detail: "go tool nsgo check passed",
	}
}

func gradeMCPVerify(ctx context.Context, workspaceDir, workFile string, task *Task) CriterionResult {
	const name = "mcp_verify_passes"
	w := task.Checks.Weights.MCPVerify

	hasTestMain, err := workspaceHasTestMain(workspaceDir)
	if err != nil {
		return CriterionResult{Name: name, Weight: w, Detail: fmt.Sprintf("scan test files: %v", err)}
	}
	src, err := renderMCPVerifyTest(task.Checks.MCPVerify, !hasTestMain)
	if err != nil {
		return CriterionResult{Name: name, Weight: w, Detail: fmt.Sprintf("render MCP verify test: %v", err)}
	}
	verifyPath := filepath.Join(workspaceDir, "agenteval_mcp_verify_test.go")
	if err := os.WriteFile(verifyPath, src, 0o644); err != nil {
		return CriterionResult{Name: name, Weight: w, Detail: fmt.Sprintf("write MCP verify test: %v", err)}
	}
	defer os.Remove(verifyPath)

	cmd := exec.CommandContext(ctx, "go", "tool", "nsgo", "test", ".", "--", "-v", "-run", "^TestAgentEvalMCPVerify$")
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = workspaceDir
	cmd.Env = cleanGoEnv(workFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return CriterionResult{
			Name:   name,
			Weight: w,
			Detail: fmt.Sprintf("MCP verification failed: %v (%s)", err, compactOutput(out, 300)),
		}
	}
	// A clean exit is not enough: a workspace TestMain that exits without calling m.Run would skip the verification test.
	if !mcpVerifyPassLine.Match(out) {
		return CriterionResult{
			Name:   name,
			Weight: w,
			Detail: fmt.Sprintf("TestAgentEvalMCPVerify did not report PASS (%s)", compactOutput(out, 300)),
		}
	}
	return CriterionResult{
		Name:   name,
		Weight: w,
		Earned: w,
		Passed: true,
		Detail: "headless MCP verification passed",
	}
}

// mcpVerifyPassLine matches the pass line of the verification test in go test -v output or in nsgo test's summary.
var mcpVerifyPassLine = regexp.MustCompile(`(?m)^\s*(--- PASS: |✓ )TestAgentEvalMCPVerify \(`)

func loadSceneFile(workspaceDir, rel string) (*sceneformat.SceneDoc, error) {
	p, err := safeJoin(workspaceDir, rel)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", rel, err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	doc, err := sceneformat.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rel, err)
	}
	return doc, nil
}

func walkLayerObjects(objs []sceneformat.ObjectDoc, fn func(*sceneformat.ObjectDoc)) {
	for i := range objs {
		fn(&objs[i])
		if len(objs[i].Children) > 0 {
			walkLayerObjects(objs[i].Children, fn)
		}
	}
}

func isPlatformObject(o *sceneformat.ObjectDoc) bool {
	if o == nil {
		return false
	}
	id := strings.ToLower(o.ID)
	prefab := strings.ToLower(o.Prefab)
	name := strings.ToLower(o.Name)
	for _, s := range []string{id, prefab, name} {
		if strings.Contains(s, "platform") || strings.Contains(s, "ground") || strings.Contains(s, "floor") {
			return true
		}
	}
	if _, ok := o.Components["platform"]; ok {
		return true
	}
	return false
}

// isButtonObject reports whether o is a menu button: prefab `button` or a `button` component.
func isButtonObject(o *sceneformat.ObjectDoc) bool {
	if strings.EqualFold(o.Prefab, "button") {
		return true
	}
	_, ok := o.Components["button"]
	return ok
}

// isEnemyObject reports whether o is an enemy: prefab `enemy`, an `enemy` component, or a `tag: enemy` property.
func isEnemyObject(o *sceneformat.ObjectDoc) bool {
	if o == nil {
		return false
	}
	if strings.EqualFold(o.Prefab, "enemy") {
		return true
	}
	if _, ok := o.Components["enemy"]; ok {
		return true
	}
	if tag, ok := o.Properties["tag"].(string); ok && strings.EqualFold(tag, "enemy") {
		return true
	}
	return false
}

func objectWidth(o *sceneformat.ObjectDoc, fallback float64) float64 {
	if o == nil {
		return fallback
	}
	for _, m := range []map[string]any{o.Properties, compMap(o.Components["platform"]), compMap(o.Components["collider"])} {
		if m == nil {
			continue
		}
		if v, ok := numericValue(m["width"]); ok && v > 0 {
			return v
		}
	}
	return fallback
}

func compMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func numericValue(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

// prepareGoWorkspace writes a temporary go.work that uses this checkout's packages/game and workspaceDir, and points the workspace's packages/game requirement at the checkout with `go mod edit -replace`, which also rewrites a replace the workspace already has. The cleanup restores go.mod and removes the go.work.
func prepareGoWorkspace(ctx context.Context, repoRoot, workspaceDir string) (string, func(), error) {
	absGame, err := filepath.Abs(filepath.Join(repoRoot, "packages", "game"))
	if err != nil {
		return "", nil, err
	}
	if resolved, err := filepath.EvalSymlinks(absGame); err == nil {
		absGame = resolved
	}
	absWS, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", nil, err
	}
	if resolved, err := filepath.EvalSymlinks(absWS); err == nil {
		absWS = resolved
	}
	goVer, err := readGoVersion(filepath.Join(absGame, "go.mod"))
	if err != nil {
		return "", nil, err
	}

	goModPath := filepath.Join(absWS, "go.mod")
	origGoMod, err := os.ReadFile(goModPath)
	if err != nil {
		return "", nil, err
	}
	if err := pinGamePackage(ctx, repoRoot, absWS); err != nil {
		return "", nil, err
	}
	restore := func() { _ = os.WriteFile(goModPath, origGoMod, 0o644) }

	tmpDir, err := os.MkdirTemp("", "agenteval-gowork-*")
	if err != nil {
		restore()
		return "", nil, err
	}
	workFile := filepath.Join(tmpDir, "go.work")
	content := fmt.Sprintf("go %s\n\nuse (\n\t%q\n\t%q\n)\n", goVer, absGame, absWS)
	if err := os.WriteFile(workFile, []byte(content), 0o644); err != nil {
		restore()
		os.RemoveAll(tmpDir)
		return "", nil, err
	}
	return workFile, func() {
		restore()
		_ = os.RemoveAll(tmpDir)
	}, nil
}

// pinGamePackage points the packages/game requirement in workspaceDir's go.mod at this checkout with `go mod edit -replace`, which also rewrites a replace already there, in a parenthesized block or not.
func pinGamePackage(ctx context.Context, repoRoot, workspaceDir string) error {
	absGame, err := filepath.Abs(filepath.Join(repoRoot, "packages", "game"))
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(absGame); err == nil {
		absGame = resolved
	}
	edit := exec.CommandContext(ctx, "go", "mod", "edit", "-replace=github.com/nsgo-dev/game/packages/game="+absGame)
	edit.Dir = workspaceDir
	edit.Env = append(cleanGoEnv(""), "GOWORK=off")
	if out, err := edit.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod edit -replace: %v (%s)", err, compactOutput(out, 240))
	}
	return nil
}

// cleanGoEnv returns the process environment without GOROOT and GOWORK, adding GOWORK=workFile when workFile is set.
func cleanGoEnv(workFile string) []string {
	src := os.Environ()
	out := make([]string, 0, len(src)+1)
	for _, kv := range src {
		if strings.HasPrefix(kv, "GOROOT=") || strings.HasPrefix(kv, "GOWORK=") {
			continue
		}
		out = append(out, kv)
	}
	if workFile != "" {
		out = append(out, "GOWORK="+workFile)
	}
	return out
}

func workspaceHasTestMain(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return false, err
		}
		if bytes.Contains(data, []byte("func TestMain(")) {
			return true, nil
		}
	}
	return false, nil
}

func compactOutput(b []byte, limit int) string {
	s := strings.TrimSpace(string(b))
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= limit {
		return s
	}
	return s[len(s)-limit:]
}

func renderMCPVerifyTest(spec *MCPVerifySpec, includeTestMain bool) ([]byte, error) {
	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	data := struct {
		IncludeTestMain bool
		SpecJSON        string
	}{
		IncludeTestMain: includeTestMain,
		SpecJSON:        string(specJSON),
	}
	var buf bytes.Buffer
	if err := mcpVerifyTmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var mcpVerifyTmpl = template.Must(template.New("mcp_verify").Parse(`package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/nsgo/automation"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

{{if .IncludeTestMain}}
func TestMain(m *testing.M) { nsgotest.Main(m) }
{{end}}

type evalMCPSpec struct {
	MinScenes          int      ` + "`json:\"MinScenes\"`" + `
	RequireSceneNames  []string ` + "`json:\"RequireSceneNames\"`" + `
	InspectLayerScene  string   ` + "`json:\"InspectLayerScene\"`" + `
	RequireLayerSpace  string   ` + "`json:\"RequireLayerSpace\"`" + `
	RequireEntityTag   string   ` + "`json:\"RequireEntityTag\"`" + `
	RequireEntityNames []string ` + "`json:\"RequireEntityNames\"`" + `
	StepCount          int      ` + "`json:\"StepCount\"`" + `
	ExpectMovedEntity  string   ` + "`json:\"ExpectMovedEntity\"`" + `
	ExpectComponent    string   ` + "`json:\"ExpectComponent\"`" + `
	PressActions       []string ` + "`json:\"PressActions\"`" + `
	ApproachEntity     string   ` + "`json:\"ApproachEntity\"`" + `
	Click              *struct {
		X           float64 ` + "`json:\"X\"`" + `
		Y           float64 ` + "`json:\"Y\"`" + `
		ExpectScene string  ` + "`json:\"ExpectScene\"`" + `
	} ` + "`json:\"Click\"`" + `
}

func evalCallMCP[T any](t *testing.T, app *nsgotest.App, cx *core.Scope, name string, args any) T {
	t.Helper()
	var payload json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("marshal %s args: %v", name, err)
		}
		payload = b
	}
	var (
		raw json.RawMessage
		err error
	)
	app.Post(func() {
		raw, err = automation.Call(cx, t.Context(), name, payload)
	})
	app.Settle()
	if err != nil {
		t.Fatalf("MCP tool %s failed: %v", name, err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("MCP tool %s unmarshal %s: %v", name, string(raw), err)
	}
	return out
}

func evalExtractPos(t *testing.T, comps map[string]json.RawMessage) (float64, float64) {
	t.Helper()
	for _, key := range []string{"position", "transform2d", "world_transform2d"} {
		raw, ok := comps[key]
		if !ok {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		x, okX := evalFloat(m, "x", "X")
		y, okY := evalFloat(m, "y", "Y")
		if okX && okY {
			return x, y
		}
	}
	t.Fatalf("no position/transform2d component found in %v", comps)
	return 0, 0
}

func evalFloat(m map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if f, ok := v.(float64); ok {
				return f, true
			}
		}
	}
	return 0, false
}

func TestAgentEvalMCPVerify(t *testing.T) {
	var spec evalMCPSpec
	if err := json.Unmarshal([]byte({{printf "%q" .SpecJSON}}), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	var rootScope *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		rootScope = cx
		return Root(cx)
	})

	// 1. Verify scenes via game_scenes_list.
	scenesRes := evalCallMCP[mcp.ScenesListResult](t, app, rootScope, mcp.ToolScenesList, nil)
	if spec.MinScenes > 0 && len(scenesRes.Scenes) < spec.MinScenes {
		t.Fatalf("game_scenes_list returned %d scenes, want at least %d", len(scenesRes.Scenes), spec.MinScenes)
	}
	sceneSet := make(map[string]bool, len(scenesRes.Scenes))
	for _, s := range scenesRes.Scenes {
		sceneSet[s.Name] = true
	}
	for _, want := range spec.RequireSceneNames {
		if !sceneSet[want] {
			t.Fatalf("game_scenes_list missing required scene %q (got %+v)", want, scenesRes.Scenes)
		}
	}

	// 2. Verify layers via game_layers_list.
	var layerArgs any
	if spec.InspectLayerScene != "" {
		layerArgs = map[string]any{"scene": spec.InspectLayerScene}
	}
	layersRes := evalCallMCP[mcp.LayersListResult](t, app, rootScope, mcp.ToolLayersList, layerArgs)
	if len(layersRes.Layers) == 0 {
		t.Fatalf("game_layers_list returned 0 layers for scene %q", layersRes.Scene)
	}
	if spec.RequireLayerSpace != "" {
		foundSpace := false
		for _, l := range layersRes.Layers {
			if l.Visible && l.Space == spec.RequireLayerSpace {
				foundSpace = true
				break
			}
		}
		if !foundSpace {
			t.Fatalf("game_layers_list found no visible layer with space %q in scene %q", spec.RequireLayerSpace, layersRes.Scene)
		}
	}

	// 3. Verify entities via game_entities_list.
	allEnts := evalCallMCP[mcp.EntitiesListResult](t, app, rootScope, mcp.ToolEntitiesList, nil)
	byName := make(map[string]mcp.EntitySummary, len(allEnts.Entities))
	for _, e := range allEnts.Entities {
		if _, err := mcp.ParseEntityRef(e.Ref); err != nil {
			t.Fatalf("invalid entity ref %q: %v", e.Ref, err)
		}
		if e.Name != "" {
			byName[e.Name] = e
		}
	}
	for _, wantName := range spec.RequireEntityNames {
		if _, ok := byName[wantName]; !ok {
			t.Fatalf("game_entities_list missing entity named %q", wantName)
		}
	}

	var taggedEnts mcp.EntitiesListResult
	if spec.RequireEntityTag != "" {
		taggedEnts = evalCallMCP[mcp.EntitiesListResult](t, app, rootScope, mcp.ToolEntitiesList, map[string]any{
			"tag": spec.RequireEntityTag,
		})
		if len(taggedEnts.Entities) == 0 {
			t.Fatalf("game_entities_list(tag=%q) returned 0 entities", spec.RequireEntityTag)
		}
	}

	// Select target entity for movement/component checks.
	var targetRef string
	if spec.ExpectMovedEntity != "" {
		if e, ok := byName[spec.ExpectMovedEntity]; ok {
			targetRef = e.Ref
		} else {
			for _, e := range allEnts.Entities {
				if strings.Contains(strings.ToLower(e.Name), strings.ToLower(spec.ExpectMovedEntity)) {
					targetRef = e.Ref
					break
				}
			}
		}
		if targetRef == "" && len(taggedEnts.Entities) > 0 {
			targetRef = taggedEnts.Entities[0].Ref
		}
		if targetRef == "" {
			t.Fatalf("could not find entity matching %q to verify movement", spec.ExpectMovedEntity)
		}
	}

	// 4. Pause loop before reading baseline position so movement comes strictly from game_loop_step.
	var paused mcp.LoopStatusResult
	if spec.StepCount > 0 {
		paused = evalCallMCP[mcp.LoopStatusResult](t, app, rootScope, mcp.ToolLoopPause, nil)
		if !paused.Paused {
			t.Fatalf("game_loop_pause returned paused=false")
		}
	}

	var x0, y0 float64
	if targetRef != "" {
		before := evalCallMCP[mcp.EntityInspectResult](t, app, rootScope, mcp.ToolEntityInspect, map[string]any{"ref": targetRef})
		x0, y0 = evalExtractPos(t, before.Components)
	}
	var approachRef string
	var approachX float64
	if spec.ApproachEntity != "" {
		e, ok := byName[spec.ApproachEntity]
		if !ok {
			t.Fatalf("game_entities_list missing approach entity %q", spec.ApproachEntity)
		}
		approachRef = e.Ref
		target := evalCallMCP[mcp.EntityInspectResult](t, app, rootScope, mcp.ToolEntityInspect, map[string]any{"ref": approachRef})
		approachX, _ = evalExtractPos(t, target.Components)
	}

	// Movement must come from input: hold each action through game_input_action before stepping.
	for _, name := range spec.PressActions {
		res := evalCallMCP[mcp.InputActionResult](t, app, rootScope, mcp.ToolInputAction, map[string]any{"name": name, "mode": "press"})
		if res.Mode != "press" {
			t.Fatalf("game_input_action(%q, press) ran mode %q", name, res.Mode)
		}
	}

	if spec.StepCount > 0 {
		stepped := evalCallMCP[mcp.LoopStepResult](t, app, rootScope, mcp.ToolLoopStep, map[string]any{"count": spec.StepCount})
		if stepped.StepsAdvanced != spec.StepCount {
			t.Fatalf("game_loop_step advanced %d steps, want %d", stepped.StepsAdvanced, spec.StepCount)
		}
		if stepped.DeltaSeconds <= 0 {
			t.Fatalf("game_loop_step delta_seconds = %v, want > 0", stepped.DeltaSeconds)
		}
		if spec.StepCount == 60 && paused.StepHz == 60 && stepped.DeltaSeconds != 1.0 {
			t.Fatalf("60 steps at 60 Hz advanced %v s, want exact 1.0 s", stepped.DeltaSeconds)
		}
	}

	// 5. Re-inspect target entity using its stable ref after stepping.
	if targetRef != "" {
		after := evalCallMCP[mcp.EntityInspectResult](t, app, rootScope, mcp.ToolEntityInspect, map[string]any{"ref": targetRef})
		x1, y1 := evalExtractPos(t, after.Components)
		if math.IsNaN(x1) || math.IsInf(x1, 0) || math.IsNaN(y1) || math.IsInf(y1, 0) {
			t.Fatalf("entity %s position became non-finite: (%v, %v)", targetRef, x1, y1)
		}
		if math.Hypot(x1-x0, y1-y0) <= 0.01 {
			t.Fatalf("entity %s did not move after %d steps: before=(%v,%v) after=(%v,%v)", targetRef, spec.StepCount, x0, y0, x1, y1)
		}
		if approachRef != "" && math.Abs(approachX-x1) >= math.Abs(approachX-x0)-1 {
			t.Fatalf("entity %s did not move toward %q (x=%v) after %d steps: x before=%v after=%v", targetRef, spec.ApproachEntity, approachX, spec.StepCount, x0, x1)
		}
		if spec.ExpectComponent != "" {
			comp := evalCallMCP[mcp.ComponentGetResult](t, app, rootScope, mcp.ToolComponentGet, map[string]any{
				"ref":       targetRef,
				"component": spec.ExpectComponent,
			})
			if len(comp.Value) == 0 || !json.Valid(comp.Value) {
				t.Fatalf("game_component_get(%s, %q) returned invalid JSON: %q", targetRef, spec.ExpectComponent, string(comp.Value))
			}
		}
	}

	// 6. Click through game_input_pointer and check that the press switched scenes.
	if c := spec.Click; c != nil {
		res := evalCallMCP[mcp.InputPointerResult](t, app, rootScope, mcp.ToolInputPointer, map[string]any{"action": "click", "x": c.X, "y": c.Y})
		if !res.Dispatched {
			t.Fatalf("game_input_pointer click at (%v, %v) reached no OnPress handler (hit=%v layer=%q)", c.X, c.Y, res.Hit, res.HitLayer)
		}
		if spec.StepCount > 0 {
			evalCallMCP[mcp.LoopStepResult](t, app, rootScope, mcp.ToolLoopStep, map[string]any{"count": 1})
		}
		after := evalCallMCP[mcp.ScenesListResult](t, app, rootScope, mcp.ToolScenesList, nil)
		if after.Current != c.ExpectScene {
			t.Fatalf("after clicking (%v, %v) the current scene is %q, want %q", c.X, c.Y, after.Current, c.ExpectScene)
		}
	}
}
`))

// readGoVersion returns the go directive of the go.mod at goModPath, so the temporary go.work follows the repository's Go version.
func readGoVersion(goModPath string) (string, error) {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "go" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("%s has no go directive", goModPath)
}

// copyDir recursively copies src into dst, refusing symlinks so staged fixtures cannot escape src.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in fixture: %s", p)
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
