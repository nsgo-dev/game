package editormcp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor"
)

func toolDisabledError(name string) error {
	return fmt.Errorf("editormcp: tool %s is turned off in Settings › Tools; ask the person to enable it", name)
}

func (s *Server) isToolEnabled(name string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	_, ok := s.tools[name]
	disabled := s.disabled[name]
	s.mu.Unlock()
	if !ok {
		return false
	}
	return !disabled
}

func (s *Server) isRegisteredAndDisabled(name string) bool {
	if s == nil || name == "" {
		return false
	}
	s.mu.Lock()
	_, ok := s.tools[name]
	disabled := s.disabled[name]
	s.mu.Unlock()
	return ok && disabled
}

// syncTools mirrors the server's registered tools to ed.SetRegisteredMCPTools and ensures
// s.sdk advertises only currently enabled tools (removing disabled tools and re-adding enabled ones,
// which notifies connected clients via notifications/tools/list_changed).
// onEditor reports whether the caller is already executing on the editor's goroutine.
func (s *Server) syncTools(onEditor bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	names := slices.Sorted(maps.Keys(s.tools))
	infos := make([]editor.AgentToolInfo, 0, len(names))
	type toolSyncItem struct {
		tool    Tool
		enabled bool
		wasAdv  bool
	}
	items := make([]toolSyncItem, 0, len(names))
	for _, name := range names {
		t := s.tools[name]
		infos = append(infos, editor.AgentToolInfo{
			Name:        t.Name,
			Title:       t.Title,
			Description: t.Description,
			DefaultOff:  t.DefaultOff,
		})
		enabled := !t.DefaultOff
		if s.ed != nil {
			enabled = s.ed.IsMCPToolEnabled(t.Name)
		}
		if enabled {
			delete(s.disabled, t.Name)
		} else {
			s.disabled[t.Name] = true
		}
		items = append(items, toolSyncItem{
			tool:    t,
			enabled: enabled,
			wasAdv:  s.advertised[t.Name],
		})
	}
	var toRemove []string
	var toAdd []Tool
	for _, it := range items {
		if it.enabled && !it.wasAdv {
			s.advertised[it.tool.Name] = true
			toAdd = append(toAdd, it.tool)
		} else if !it.enabled && it.wasAdv {
			delete(s.advertised, it.tool.Name)
			toRemove = append(toRemove, it.tool.Name)
		}
	}
	// Also clean up any advertised tool that was removed from s.tools.
	for name := range s.advertised {
		if _, exists := s.tools[name]; !exists {
			delete(s.advertised, name)
			delete(s.disabled, name)
			toRemove = append(toRemove, name)
		}
	}
	s.mu.Unlock()

	if len(toRemove) > 0 {
		s.sdk.RemoveTools(toRemove...)
	}
	for _, t := range toAdd {
		s.sdk.AddTool(sdkTool(t), s.handler(t))
	}
	if s.ed != nil {
		if onEditor {
			s.ed.SetRegisteredMCPTools(infos)
		} else {
			s.notifyEditor(func(ed *editor.Editor) {
				ed.SetRegisteredMCPTools(infos)
			})
		}
	}
}

func (s *Server) readPrivacyContext(ctx context.Context, onEditor bool) (editor.AgentPrivacyPolicy, string, error) {
	if s == nil || s.ed == nil {
		return editor.DefaultAgentPrivacyPolicy(), "", nil
	}
	var (
		policy editor.AgentPrivacyPolicy
		root   string
	)
	read := func(ed *editor.Editor) error {
		policy = ed.AgentPrivacy()
		root = projectRoot(ed)
		return nil
	}
	var err error
	if onEditor {
		err = s.asAgent(read)
	} else {
		err = s.Editor(ctx, read)
	}
	if err != nil {
		return editor.DefaultAgentPrivacyPolicy(), "", err
	}
	return policy, root, nil
}

// resolveProjectPrivacyKinds classifies both rel and (when root is set) its symlink-resolved
// project-relative path so an in-project symlink pointing to a .go or art/audio file cannot bypass privacy.
func resolveProjectPrivacyKinds(root, rel string) []editor.PrivacyContentKind {
	k1 := editor.ClassifyProjectFilePrivacy(rel)
	kinds := []editor.PrivacyContentKind{k1}
	if root == "" {
		return kinds
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Fall back to resolving the parent directory for non-existent destination paths (e.g. refactor_rename).
		realParent, perr := filepath.EvalSymlinks(filepath.Dir(abs))
		if perr != nil {
			return kinds
		}
		real = filepath.Join(realParent, filepath.Base(abs))
	}
	inside, err := filepath.Rel(root, real)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return kinds
	}
	k2 := editor.ClassifyProjectFilePrivacy(filepath.ToSlash(inside))
	if k2 != k1 {
		kinds = append(kinds, k2)
	}
	return kinds
}

func checkPathPrivacy(policy editor.AgentPrivacyPolicy, root, rawPath string) error {
	if strings.TrimSpace(rawPath) == "" {
		return nil
	}
	clean, err := cleanProjectPath(rawPath)
	if err != nil {
		return nil
	}
	for _, kind := range resolveProjectPrivacyKinds(root, clean) {
		if !policy.Allows(kind) {
			return editor.PrivacyRefusalError(kind, clean)
		}
	}
	return nil
}

// secretPathArgKeys are the argument names under which tools take project file paths, at any depth of the arguments (refactor_rename's from and to are checked by its own case).
var secretPathArgKeys = map[string]bool{"file": true, "path": true, "files": true}

// refuseSecretPathArgs refuses a call whose arguments name, under any of secretPathArgKeys, a project file that looks like a credential or the editor's private state, directly or through an in-project symlink, before any tool reads, greps, edits, renames or deletes it.
func refuseSecretPathArgs(root string, args json.RawMessage) error {
	var v any
	if len(args) == 0 || json.Unmarshal(args, &v) != nil {
		return nil
	}
	return refuseSecretPathValue(root, v, false)
}

func refuseSecretPathValue(root string, v any, isPath bool) error {
	switch x := v.(type) {
	case string:
		if !isPath {
			return nil
		}
		clean, err := cleanProjectPath(x)
		if err != nil {
			return nil
		}
		if slices.Contains(resolveProjectPrivacyKinds(root, clean), editor.PrivacySecrets) {
			return editor.PrivacyRefusalError(editor.PrivacySecrets, clean)
		}
	case []any:
		for _, e := range x {
			if err := refuseSecretPathValue(root, e, isPath); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(x)) {
			if err := refuseSecretPathValue(root, x[k], secretPathArgKeys[k]); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkPrivacyPre enforces WHAT MAY LEAVE THIS COMPUTER before a tool runs.
func (s *Server) checkPrivacyPre(ctx context.Context, t Tool, args json.RawMessage, onEditor bool) error {
	policy, root, err := s.readPrivacyContext(ctx, onEditor)
	if err != nil {
		return err
	}
	if err := refuseSecretPathArgs(root, args); err != nil {
		return err
	}
	switch t.Name {
	case ToolViewportImage:
		if !policy.ViewportImages {
			return editor.PrivacyRefusalError(editor.PrivacyViewportImages, ToolViewportImage)
		}
	case ToolPlayFrozenContext:
		var a struct {
			IncludeImage *bool `json:"includeImage"`
		}
		_ = json.Unmarshal(args, &a)
		includeImage := a.IncludeImage == nil || *a.IncludeImage
		if includeImage && !policy.ViewportImages {
			return editor.PrivacyRefusalError(editor.PrivacyViewportImages, ToolPlayFrozenContext)
		}
		if !policy.SceneAndData {
			return editor.PrivacyRefusalError(editor.PrivacySceneAndData, ToolPlayFrozenContext)
		}
	case ToolPlayReplayStep:
		var a struct {
			IncludeImage bool `json:"includeImage"`
		}
		_ = json.Unmarshal(args, &a)
		if a.IncludeImage && !policy.ViewportImages {
			return editor.PrivacyRefusalError(editor.PrivacyViewportImages, ToolPlayReplayStep)
		}
		if !policy.SceneAndData {
			return editor.PrivacyRefusalError(editor.PrivacySceneAndData, ToolPlayReplayStep)
		}
	case ToolAgentTurnGet:
		var a struct {
			IncludeScene *bool `json:"includeScene"`
			IncludeImage *bool `json:"includeImage"`
		}
		_ = json.Unmarshal(args, &a)
		includeImage := a.IncludeImage == nil || *a.IncludeImage
		if includeImage && !policy.ViewportImages {
			return editor.PrivacyRefusalError(editor.PrivacyViewportImages, ToolAgentTurnGet)
		}
		if !policy.SceneAndData {
			return editor.PrivacyRefusalError(editor.PrivacySceneAndData, ToolAgentTurnGet)
		}
	case ToolSketchRead:
		var a struct {
			IncludeImage *bool `json:"includeImage"`
		}
		_ = json.Unmarshal(args, &a)
		if a.IncludeImage != nil && *a.IncludeImage && !policy.ViewportImages {
			return editor.PrivacyRefusalError(editor.PrivacyViewportImages, ToolSketchRead)
		}
		if !policy.SceneAndData {
			return editor.PrivacyRefusalError(editor.PrivacySceneAndData, ToolSketchRead)
		}
	case ToolSceneRead:
		var a struct {
			File string `json:"file"`
		}
		_ = json.Unmarshal(args, &a)
		if strings.TrimSpace(a.File) == "" {
			if !policy.SceneAndData {
				return editor.PrivacyRefusalError(editor.PrivacySceneAndData, "the open scene")
			}
			return nil
		}
		if clean, cerr := cleanProjectPath(a.File); cerr == nil {
			// Go source and scene/data files (including via in-project symlinks) are refused upfront before reading;
			// art/audio text files are refused in sceneRead right after binary-file detection so binary files still
			// report "not a text file" while zero art/audio content ever leaves this computer.
			for _, kind := range resolveProjectPrivacyKinds(root, clean) {
				if kind == editor.PrivacySecrets {
					return editor.PrivacyRefusalError(editor.PrivacySecrets, clean)
				}
				if kind == editor.PrivacyGoSource && !policy.GoSource {
					return editor.PrivacyRefusalError(editor.PrivacyGoSource, clean)
				}
				if kind == editor.PrivacySceneAndData && !policy.SceneAndData {
					return editor.PrivacyRefusalError(editor.PrivacySceneAndData, clean)
				}
			}
		}
	case ToolRefsFind:
		var a struct {
			File string `json:"file"`
		}
		_ = json.Unmarshal(args, &a)
		if err := checkPathPrivacy(policy, root, a.File); err != nil {
			return err
		}
		if !policy.SceneAndData {
			return editor.PrivacyRefusalError(editor.PrivacySceneAndData, ToolRefsFind)
		}
	case ToolRefactorRename:
		var a struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		_ = json.Unmarshal(args, &a)
		if err := checkPathPrivacy(policy, root, a.From); err != nil {
			return err
		}
		if err := checkPathPrivacy(policy, root, a.To); err != nil {
			return err
		}
		if !policy.SceneAndData {
			return editor.PrivacyRefusalError(editor.PrivacySceneAndData, ToolRefactorRename)
		}
	case ToolFileEdit:
		var a struct {
			File string `json:"file"`
		}
		_ = json.Unmarshal(args, &a)
		if err := checkPathPrivacy(policy, root, a.File); err != nil {
			return err
		}
		if !policy.SceneAndData {
			return editor.PrivacyRefusalError(editor.PrivacySceneAndData, ToolFileEdit)
		}
	case ToolGameDeleteFile:
		var a struct {
			File string `json:"file"`
		}
		_ = json.Unmarshal(args, &a)
		if err := checkPathPrivacy(policy, root, a.File); err != nil {
			return err
		}
		if !policy.SceneAndData {
			return editor.PrivacyRefusalError(editor.PrivacySceneAndData, ToolGameDeleteFile)
		}
	case ToolDataTableReviewPropose:
		var a struct {
			Files []struct {
				File string `json:"file"`
			} `json:"files"`
		}
		_ = json.Unmarshal(args, &a)
		for _, f := range a.Files {
			if err := checkPathPrivacy(policy, root, f.File); err != nil {
				return err
			}
		}
		if !policy.SceneAndData {
			return editor.PrivacyRefusalError(editor.PrivacySceneAndData, ToolDataTableReviewPropose)
		}
	case ToolCodeReviewPropose:
		if !policy.GoSource {
			return editor.PrivacyRefusalError(editor.PrivacyGoSource, ToolCodeReviewPropose)
		}
		var a struct {
			Files []struct {
				Path string `json:"path"`
			} `json:"files"`
		}
		_ = json.Unmarshal(args, &a)
		for _, f := range a.Files {
			if err := checkPathPrivacy(policy, root, f.Path); err != nil {
				return err
			}
		}
	case ToolCodeReviewStatus,
		ToolCodeReviewRunChecks:
		if !policy.GoSource {
			return editor.PrivacyRefusalError(editor.PrivacyGoSource, t.Name)
		}
	case ToolPhoneCallTool:
		var a struct {
			Tool string `json:"tool"`
		}
		_ = json.Unmarshal(args, &a)
		if strings.TrimSpace(a.Tool) == "game_screenshot" && !policy.ViewportImages {
			return editor.PrivacyRefusalError(editor.PrivacyViewportImages, t.Name)
		}
		if !policy.SceneAndData {
			return editor.PrivacyRefusalError(editor.PrivacySceneAndData, t.Name)
		}
	case ToolSelectionGet,
		ToolSceneEdit,
		ToolDataTableReviewChecks,
		ToolDataTableReviewStatus,
		ToolObjectCopyYAML,
		ToolObjectPasteYAML,
		ToolObjectDuplicate,
		ToolObjectGroup,
		ToolObjectMoveUnder,
		ToolObjectSelectSamePrefab,
		ToolObjectVisibility,
		ToolObjectFindUsages,
		ToolObjectSetField,
		ToolObjectLocateYAML,
		ToolTilesetRead,
		ToolTilesetEdit,
		ToolTileQuery,
		ToolTilePaint,
		ToolTileAutotile,
		ToolTileErase,
		ToolTileReviewStatus,
		ToolTileReviewRevertRegion,
		ToolTileReviewRestoreRegion,
		ToolTileReviewKeep,
		ToolTileReviewRevertAll,
		ToolCutsceneWrite,
		ToolCutsceneScrub,
		ToolStatesWrite,
		ToolStatesTest,
		ToolMergeStatus,
		ToolMergeResolve,
		ToolMergeFinish,
		ToolMergeAbort,
		ToolGamePlaytest,
		ToolPlaytestDefine,
		ToolPlaytestReport,
		ToolProblemProposeFix,
		ToolProjectContext,
		ToolBriefEdit,
		ToolRulesEdit,
		ToolHistoryList,
		ToolSketchReading,
		ToolReleaseNotesDraft,
		ToolReviewStatus,
		ToolReviewNotesGet,
		ToolReviewRedo,
		ToolGameCheck,
		ToolTurnStart,
		ToolTurnEnd,
		ToolTurnStatus:
		if !policy.SceneAndData {
			return editor.PrivacyRefusalError(editor.PrivacySceneAndData, t.Name)
		}
	}
	return nil
}

// checkPrivacyPost is a defense-in-depth gate after any tool handler runs, refusing any result
// whose payload carries a kind of content switched off under WHAT MAY LEAVE THIS COMPUTER.
func (s *Server) checkPrivacyPost(ctx context.Context, t Tool, res *Result, onEditor bool) error {
	if res == nil {
		return nil
	}
	policy, _, err := s.readPrivacyContext(ctx, onEditor)
	if err != nil {
		return err
	}
	if len(res.PNG) > 0 && !policy.ViewportImages {
		return editor.PrivacyRefusalError(editor.PrivacyViewportImages, t.Name)
	}
	return nil
}
