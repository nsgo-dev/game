package editor

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// Keyboard scope identifiers for editor and plugin commands.
// Rebinding checks for conflicts only within the same scope; the same key in different scopes is allowed.
const (
	ScopeGlobal     = "global"
	ScopeViewport   = "viewport"
	ScopeViewport2D = "viewport2d"
	ScopeViewport3D = "viewport3d"
	ScopeSceneTree  = "sceneTree"
	ScopeTile       = "tile"
	ScopeAnimation  = "animation"
	ScopePlay       = "play"
)

// scopeDisplayOrder lists built-in scopes in presentation order for Settings › Shortcuts and the ⌘/ sheet.
var scopeDisplayOrder = []string{
	ScopeGlobal,
	ScopeViewport,
	ScopeViewport2D,
	ScopeViewport3D,
	ScopeTile,
	ScopeSceneTree,
	ScopeAnimation,
	ScopePlay,
}

// ShortcutConflictError is returned by RebindShortcut when another command in the same scope
// already owns the requested shortcut. Callers can resolve it in place with SwapShortcut.
type ShortcutConflictError struct {
	CommandID     string
	Scope         string
	Shortcut      string
	ConflictID    string
	ConflictLabel string
}

func (e *ShortcutConflictError) Error() string {
	return fmt.Sprintf("editor: shortcut %q is already bound to %q (%s) in scope %q", e.Shortcut, e.ConflictLabel, e.ConflictID, e.Scope)
}

// normalizeCommandScope returns scope trimmed, defaulting to ScopeGlobal when empty.
func normalizeCommandScope(scope string) string {
	s := strings.TrimSpace(scope)
	if s == "" {
		return ScopeGlobal
	}
	return s
}

// CommandScopeLabel returns the localized display name for a command scope.
func (e *Editor) CommandScopeLabel(scope string) string {
	switch normalizeCommandScope(scope) {
	case ScopeGlobal:
		return e.T("scope.global")
	case ScopeViewport:
		return e.T("scope.viewport")
	case ScopeViewport2D:
		return e.T("scope.viewport2d")
	case ScopeViewport3D:
		return e.T("scope.viewport3d")
	case ScopeSceneTree:
		return e.T("scope.sceneTree")
	case ScopeTile:
		return e.T("scope.tile")
	case ScopeAnimation:
		return e.T("scope.animation")
	case ScopePlay:
		return e.T("scope.play")
	default:
		return normalizeCommandScope(scope)
	}
}

// commandScopeSearchTerms returns English, localized, and raw scope tokens for shortcut search.
func (e *Editor) commandScopeSearchTerms(scope string) []string {
	norm := normalizeCommandScope(scope)
	terms := []string{strings.ToLower(norm), strings.ToLower(e.CommandScopeLabel(norm))}
	switch norm {
	case ScopeGlobal:
		terms = append(terms, "global")
	case ScopeViewport:
		terms = append(terms, "viewport")
	case ScopeViewport2D:
		terms = append(terms, "2d viewport", "viewport 2d", "2d")
	case ScopeViewport3D:
		terms = append(terms, "3d viewport", "viewport 3d", "3d")
	case ScopeSceneTree:
		terms = append(terms, "scene tree", "scenetree", "tree")
	case ScopeTile:
		terms = append(terms, "tile", "tilemap")
	case ScopeAnimation:
		terms = append(terms, "animation", "timeline")
	case ScopePlay:
		terms = append(terms, "play", "run")
	}
	return terms
}

// ShortcutFromKeyEvent converts a hardware KeyEvent captured during rebinding into NSGo's
// canonical portable shortcut form (converting Meta to "mod" on Apple platforms and Ctrl to "mod"
// on Windows and Linux). Returns "", false for bare modifier presses or unsupported empty events.
func ShortcutFromKeyEvent(ev core.KeyEvent, targetOS core.OS) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(ev.Key))
	if key == "" {
		return "", false
	}
	switch key {
	case "shift", "ctrl", "control", "alt", "option", "meta", "cmd", "command", "mod":
		return "", false
	case "return":
		key = "enter"
	case "del":
		key = "delete"
	case "esc":
		key = "escape"
	case " ":
		key = "space"
	}

	apple := targetOS == core.OSMacOS || targetOS == core.OSIOS
	var parts []string
	if apple {
		if ev.Modifiers.Platform {
			parts = append(parts, "mod")
		}
		if ev.Modifiers.Control {
			parts = append(parts, "ctrl")
		}
	} else {
		if ev.Modifiers.Control {
			parts = append(parts, "mod")
		}
		if ev.Modifiers.Platform {
			parts = append(parts, "meta")
		}
	}
	if ev.Modifiers.Alt {
		parts = append(parts, "alt")
	}
	if ev.Modifiers.Shift {
		parts = append(parts, "shift")
	}
	parts = append(parts, key)
	norm, err := NormalizeShortcut(strings.Join(parts, "+"))
	if err != nil || norm == "" {
		return "", false
	}
	return norm, true
}

// defaultCommandInfo looks up cmdID across built-in, custom, and registered plugin commands,
// returning its default primary shortcut, normalized scope, display label, and whether it exists.
func (e *Editor) defaultCommandInfo(cmdID string) (defShortcut, scope, label string, ok bool) {
	e.initBuiltinCommands()
	for _, c := range e.commands {
		if c.ID == cmdID {
			lbl := c.Label
			if keys, hasKeys := builtinCommandKeys[c.ID]; hasKeys {
				lbl = e.T(keys[0])
			}
			return c.Shortcut, normalizeCommandScope(c.Scope), lbl, true
		}
	}
	for _, p := range e.plugins {
		for _, pc := range p.Commands {
			if pluginCommandID(p.Name, pc.ID) == cmdID {
				return pc.Shortcut, normalizeCommandScope(pc.Scope), e.translatePluginText(p, pc.LabelKey, pc.Label), true
			}
		}
	}
	return "", "", "", false
}

// EffectiveShortcut returns the active canonical shortcut for cmdID, applying any user override
// stored in EditorSettings.Shortcuts on top of the command's registered default.
func (e *Editor) EffectiveShortcut(cmdID string) string {
	if e == nil {
		return ""
	}
	def, _, _, ok := e.defaultCommandInfo(cmdID)
	if !ok {
		return ""
	}
	if e.settings.Shortcuts != nil {
		if custom, overridden := e.settings.Shortcuts[cmdID]; overridden {
			return custom
		}
	}
	return def
}

// DefaultShortcut returns the registered default primary shortcut for cmdID.
func (e *Editor) DefaultShortcut(cmdID string) string {
	if e == nil {
		return ""
	}
	def, _, _, _ := e.defaultCommandInfo(cmdID)
	return def
}

// ShortcutOverridden reports whether cmdID currently has a custom shortcut differing from its default.
func (e *Editor) ShortcutOverridden(cmdID string) bool {
	if e == nil || len(e.settings.Shortcuts) == 0 {
		return false
	}
	custom, ok := e.settings.Shortcuts[cmdID]
	if !ok {
		return false
	}
	def, _, _, exists := e.defaultCommandInfo(cmdID)
	if !exists {
		return false
	}
	return custom != def
}

// validateRebindableShortcut refuses modal, focus-traversal, and OS-reserved shortcuts that cannot be safely bound.
func validateRebindableShortcut(norm string) error {
	if norm == "" {
		return nil
	}
	switch norm {
	case "escape", "tab", "shift+tab", "mod+q", "meta+q", "alt+f4", "alt+tab", "meta+tab", "mod+tab", "ctrl+alt+delete":
		return fmt.Errorf("editor: shortcut %q is reserved and cannot be bound", norm)
	}
	return nil
}

// CustomShortcutCount returns the number of user shortcut overrides that differ from command defaults.
func (e *Editor) CustomShortcutCount() int {
	if e == nil || len(e.settings.Shortcuts) == 0 {
		return 0
	}
	count := 0
	for cmdID, custom := range e.settings.Shortcuts {
		def, _, _, exists := e.defaultCommandInfo(cmdID)
		if !exists || custom != def {
			count++
		}
	}
	return count
}

// HasCustomShortcuts reports whether any command currently has a user shortcut override.
func (e *Editor) HasCustomShortcuts() bool {
	return e.CustomShortcutCount() > 0
}

// ShortcutConflict reports whether binding cmdID to newShortcut conflicts with another active
// command in the SAME scope. Keys used only in a different scope do NOT conflict.
func (e *Editor) ShortcutConflict(cmdID, newShortcut string) (conflictID, conflictLabel string, ok bool) {
	if e == nil {
		return "", "", false
	}
	_, targetScope, _, exists := e.defaultCommandInfo(cmdID)
	if !exists {
		return "", "", false
	}
	norm, err := NormalizeShortcut(newShortcut)
	if err != nil || norm == "" {
		return "", "", false
	}
	wantKeys := shortcutCollisionKeys(norm)
	for _, cmd := range e.Commands() {
		if cmd.ID == cmdID {
			continue
		}
		if normalizeCommandScope(cmd.Scope) != targetScope {
			continue
		}
		for _, sc := range shortcutsForCommand(cmd) {
			for _, ck := range shortcutCollisionKeys(sc) {
				if slices.Contains(wantKeys, ck) {
					return cmd.ID, cmd.Label, true
				}
			}
		}
	}
	return "", "", false
}

// setShortcutOverride updates e.settings.Shortcuts for cmdID against its default without saving.
func (e *Editor) setShortcutOverride(cmdID, norm string) {
	def, _, _, ok := e.defaultCommandInfo(cmdID)
	if !ok {
		return
	}
	if norm == def {
		if e.settings.Shortcuts != nil {
			delete(e.settings.Shortcuts, cmdID)
			if len(e.settings.Shortcuts) == 0 {
				e.settings.Shortcuts = nil
			}
		}
		return
	}
	if e.settings.Shortcuts == nil {
		e.settings.Shortcuts = make(map[string]string)
	}
	e.settings.Shortcuts[cmdID] = norm
}

func (e *Editor) persistSettingsAndNotify() error {
	if e.SettingsPath != "" {
		cur := e.Settings()
		if err := SaveEditorSettings(e.SettingsPath, &cur); err != nil {
			e.notify()
			return err
		}
	}
	e.notify()
	return nil
}

// RebindShortcut binds cmdID to newShortcut (or unbinds it when newShortcut is empty).
// It checks for conflicts ONLY within cmdID's scope, returning *ShortcutConflictError if another
// command in the same scope already owns newShortcut. Keys used only in a different scope bind
// immediately. When Editor.SettingsPath is set, the change is persisted to disk.
func (e *Editor) RebindShortcut(cmdID, newShortcut string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	_, targetScope, _, exists := e.defaultCommandInfo(cmdID)
	if !exists {
		return fmt.Errorf("%w: %q", ErrCommandNotFound, cmdID)
	}
	norm, err := NormalizeShortcut(newShortcut)
	if err != nil {
		return err
	}
	if err := validateRebindableShortcut(norm); err != nil {
		return err
	}
	if norm != "" {
		if cid, clabel, conflict := e.ShortcutConflict(cmdID, norm); conflict {
			return &ShortcutConflictError{
				CommandID:     cmdID,
				Scope:         targetScope,
				Shortcut:      norm,
				ConflictID:    cid,
				ConflictLabel: clabel,
			}
		}
	}
	prev := e.EffectiveShortcut(cmdID)
	if prev == norm && !e.ShortcutOverridden(cmdID) && norm == e.DefaultShortcut(cmdID) {
		return nil
	}
	e.setShortcutOverride(cmdID, norm)
	return e.persistSettingsAndNotify()
}

// SwapShortcut binds cmdID to newShortcut and, if another command in the same scope already owns
// newShortcut, assigns cmdID's previous shortcut to that conflicting command so both remain bound.
// When Editor.SettingsPath is set, the updated bindings are persisted to disk.
func (e *Editor) SwapShortcut(cmdID, newShortcut string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if _, _, _, exists := e.defaultCommandInfo(cmdID); !exists {
		return fmt.Errorf("%w: %q", ErrCommandNotFound, cmdID)
	}
	norm, err := NormalizeShortcut(newShortcut)
	if err != nil {
		return err
	}
	if err := validateRebindableShortcut(norm); err != nil {
		return err
	}
	prev := e.EffectiveShortcut(cmdID)
	conflictID, _, hasConflict := e.ShortcutConflict(cmdID, norm)
	e.setShortcutOverride(cmdID, norm)
	if hasConflict && conflictID != "" && conflictID != cmdID {
		e.setShortcutOverride(conflictID, prev)
	}
	return e.persistSettingsAndNotify()
}

// ResetShortcut restores cmdID to its default shortcut. If another command in the same scope
// currently holds cmdID's default shortcut, that command receives cmdID's previous shortcut so
// both remain bound without conflict.
func (e *Editor) ResetShortcut(cmdID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	def, _, _, exists := e.defaultCommandInfo(cmdID)
	if !exists {
		return fmt.Errorf("%w: %q", ErrCommandNotFound, cmdID)
	}
	if !e.ShortcutOverridden(cmdID) {
		return nil
	}
	prev := e.EffectiveShortcut(cmdID)
	if def != "" {
		if conflictID, _, hasConflict := e.ShortcutConflict(cmdID, def); hasConflict && conflictID != cmdID {
			e.setShortcutOverride(conflictID, prev)
		}
	}
	e.setShortcutOverride(cmdID, def)
	return e.persistSettingsAndNotify()
}

// ResetAllShortcuts removes all custom shortcut overrides, restoring every command to its default.
func (e *Editor) ResetAllShortcuts() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if len(e.settings.Shortcuts) == 0 {
		return nil
	}
	e.settings.Shortcuts = nil
	return e.persistSettingsAndNotify()
}

// FilterShortcutCommands returns active commands matching query across command label, description,
// ID, category, keywords, scope name, and effective shortcut (both canonical and platform-formatted).
func (e *Editor) FilterShortcutCommands(query string, targetOS core.OS) []EditorCommand {
	cmds := e.Commands()
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return cmds
	}
	words := strings.Fields(q)
	var out []EditorCommand
	for _, cmd := range cmds {
		haystack := make([]string, 0, 8+len(cmd.Keywords))
		haystack = append(haystack,
			strings.ToLower(cmd.Label),
			strings.ToLower(cmd.Description),
			strings.ToLower(cmd.ID),
			strings.ToLower(cmd.Category),
		)
		haystack = append(haystack, e.commandScopeSearchTerms(cmd.Scope)...)
		for _, kw := range cmd.Keywords {
			haystack = append(haystack, strings.ToLower(kw))
		}
		for _, sc := range shortcutsForCommand(cmd) {
			haystack = append(haystack,
				strings.ToLower(sc),
				strings.ToLower(FormatShortcut(sc, targetOS)),
			)
		}
		allMatch := true
		for _, w := range words {
			matched := false
			for _, h := range haystack {
				if strings.Contains(h, w) {
					matched = true
					break
				}
			}
			if !matched {
				allMatch = false
				break
			}
		}
		if allMatch {
			out = append(out, cmd)
		}
	}
	return out
}

// shortcutScopePriority ranks how strongly scope applies to the editor's current state.
// Higher scores win when two enabled commands in different scopes share a key.
func (e *Editor) shortcutScopePriority(scope string) int {
	switch normalizeCommandScope(scope) {
	case ScopeViewport3D:
		if e.ViewMode == ViewMode3D {
			return 4
		}
		return 0
	case ScopeAnimation:
		if e.clip != nil {
			return 4
		}
		return 0
	case ScopePlay:
		if e.Playing() {
			return 4
		}
		return 2
	case ScopeViewport2D:
		if e.ViewMode == ViewMode2D {
			return 3
		}
		return 0
	case ScopeTile:
		if e.ViewMode == ViewMode2D && isTileTool(e.Tool) {
			return 3
		}
		if e.ViewMode == ViewMode2D {
			return 1
		}
		return 0
	case ScopeViewport, ScopeSceneTree:
		return 3
	case ScopeGlobal:
		return 2
	default:
		return 2
	}
}

// resolveShortcutCommand finds the highest-priority enabled active command bound to norm.
func (e *Editor) resolveShortcutCommand(norm string) (EditorCommand, bool) {
	want := shortcutCollisionKeys(norm)
	var best EditorCommand
	bestFound := false
	bestScore := -1
	bestCustom := false

	for _, cmd := range e.Commands() {
		matched := false
		for _, sc := range shortcutsForCommand(cmd) {
			if sc == norm {
				matched = true
				break
			}
			for _, ck := range shortcutCollisionKeys(sc) {
				if slices.Contains(want, ck) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			continue
		}
		if cmd.Enabled != nil && !cmd.Enabled() {
			continue
		}
		score := e.shortcutScopePriority(cmd.Scope)
		custom := e.ShortcutOverridden(cmd.ID)
		if !bestFound || score > bestScore || (score == bestScore && custom && !bestCustom) {
			best = cmd
			bestFound = true
			bestScore = score
			bestCustom = custom
		}
	}
	return best, bestFound
}

// shortcutsDigest returns a deterministic summary of custom shortcut overrides for reactive memos.
func (e *Editor) shortcutsDigest() string {
	if e == nil || len(e.settings.Shortcuts) == 0 {
		return ""
	}
	keys := slices.Sorted(maps.Keys(e.settings.Shortcuts))
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(e.settings.Shortcuts[k])
		b.WriteByte(';')
	}
	return b.String()
}

// shortcutsUIStateKey returns a narrow deterministic key covering only state that affects the
// Settings › Shortcuts list and ⌘/ shortcuts sheet (language, custom shortcut overrides, custom
// commands, and active plugin commands), avoiding 60 Hz rebuilds on unrelated viewport/frame ticks.
func (e *Editor) shortcutsUIStateKey() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(e.ResolvedLanguage())
	b.WriteByte('|')
	b.WriteString(e.shortcutsDigest())
	b.WriteByte('|')
	e.initBuiltinCommands()
	for _, c := range e.commands {
		b.WriteString(c.ID)
		b.WriteByte(':')
		b.WriteString(c.Scope)
		b.WriteByte(':')
		b.WriteString(c.Shortcut)
		b.WriteByte(';')
	}
	for _, p := range e.activePlugins() {
		b.WriteByte('#')
		b.WriteString(p.Name)
		for _, pc := range p.Commands {
			b.WriteByte(';')
			b.WriteString(pc.ID)
			b.WriteByte(':')
			b.WriteString(pc.Scope)
			b.WriteByte(':')
			b.WriteString(pc.Shortcut)
			b.WriteByte(':')
			b.WriteString(e.translatePluginText(p, pc.LabelKey, pc.Label))
		}
	}
	return b.String()
}
