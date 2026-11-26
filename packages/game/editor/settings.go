package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

// SettingsVersion is the YAML schema version of per-user editor settings files.
const SettingsVersion = 1

// SettingsSchemaURL is where the JSON Schema of version 1 editor settings files is published.
// EncodeEditorSettings names it in a yaml-language-server comment on the first line so editors and AI tools validate and complete editor settings files as they are written.
const SettingsSchemaURL = "https://game.nsgo.dev/schemas/editor-settings.v1.json"

// SettingsSchemaComment is the first line EncodeEditorSettings writes: a YAML comment pointing a YAML language server at SettingsSchemaURL. ParseEditorSettings ignores it like any other comment.
const SettingsSchemaComment = "# yaml-language-server: $schema=" + SettingsSchemaURL

// Language selects the editor UI language.
type Language string

const (
	// LanguageSystem follows the operating system's locale (falling back to English when not Simplified Chinese).
	LanguageSystem Language = "system"
	// LanguageEnglish shows the editor UI in English.
	LanguageEnglish Language = "en"
	// LanguageChinese shows the editor UI in Simplified Chinese.
	LanguageChinese Language = "zh"
)

// Valid reports whether l is a recognized editor language choice.
func (l Language) Valid() bool {
	switch l {
	case LanguageSystem, LanguageEnglish, LanguageChinese:
		return true
	default:
		return false
	}
}

// Appearance selects the editor's color scheme.
type Appearance string

const (
	// AppearanceSystem follows the operating system's light or dark appearance.
	AppearanceSystem Appearance = "system"
	// AppearanceLight forces the light appearance regardless of the system setting.
	AppearanceLight Appearance = "light"
	// AppearanceDark forces the dark appearance regardless of the system setting.
	AppearanceDark Appearance = "dark"
)

// Valid reports whether a is a recognized editor appearance choice.
func (a Appearance) Valid() bool {
	switch a {
	case AppearanceSystem, AppearanceLight, AppearanceDark:
		return true
	default:
		return false
	}
}

// CoreAppearance converts a to NSGo's core.Appearance for Scope.SetAppearance.
func (a Appearance) CoreAppearance() core.Appearance {
	switch a {
	case AppearanceLight:
		return core.AppearanceLight
	case AppearanceDark:
		return core.AppearanceDark
	default:
		return core.AppearanceSystem
	}
}

// EditorSettings holds per-user editor preferences saved outside any game project as versioned YAML.
// A file looks like this:
//
//	# yaml-language-server: $schema=https://game.nsgo.dev/schemas/editor-settings.v1.json
//	version: 1
//	language: system
//	appearance: system
//
// ParseEditorSettings decodes it strictly and EncodeEditorSettings writes it back in canonical form, so a file EncodeEditorSettings wrote survives a load and save byte for byte.
type EditorSettings struct {
	// Version is the schema version; it must equal SettingsVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Language selects the editor UI language ("system", "en", or "zh").
	Language Language `yaml:"language" schema:"required,enum=system|en|zh"`
	// Appearance selects the editor color scheme ("system", "light", or "dark").
	Appearance Appearance `yaml:"appearance" schema:"required,enum=system|light|dark"`
	// ViewportOverlays lists the enabled 2D viewport overlay IDs in canonical sorted order.
	// When omitted, the editor uses DefaultViewportOverlays(); an empty list disables all overlays.
	ViewportOverlays *[]string `yaml:"viewportOverlays,omitempty"`
	// Shortcuts overrides default command shortcuts keyed by command ID (for example "view.frame": "g"). An empty string explicitly unbinds a command.
	Shortcuts map[string]string `yaml:"shortcuts,omitempty"`
}

// EditorSettingsTab selects which page is active in EditorSettingsDialogView.
type EditorSettingsTab int

const (
	// EditorSettingsTabGeneral shows language and appearance settings.
	EditorSettingsTabGeneral EditorSettingsTab = iota
	// EditorSettingsTabShortcuts shows searchable, scope-aware shortcut rebinding.
	EditorSettingsTabShortcuts
)

// DefaultEditorSettings returns the default per-user editor settings: version 1, following the system language and appearance.
func DefaultEditorSettings() EditorSettings {
	return EditorSettings{
		Version:    SettingsVersion,
		Language:   LanguageSystem,
		Appearance: AppearanceSystem,
	}
}

const (
	fixEditorSettings   = "write editor settings as version: 1 with language (system, en, or zh) and appearance (system, light, or dark)"
	fixSettingsVersion  = "set version: 1"
	fixSettingsYAMLRef  = "write values directly without YAML anchors, aliases or merge keys"
	fixSettingsLanguage = "set language to system, en, or zh"
	fixSettingsAppear   = "set appearance to system, light, or dark"
)

// ParseEditorSettings decodes and validates a versioned YAML editor settings document.
// Unknown fields, trailing YAML documents, YAML anchors/aliases/merge keys, missing or unsupported versions, and unknown language or appearance values are refused.
//
// Errors are *loaderr.Error values carrying the line, column, field path and fix; a caller that read data from a file names it with loaderr.WithFile.
func ParseEditorSettings(data []byte) (*EditorSettings, error) {
	s, err := parseEditorSettings(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixEditorSettings)
	}
	return s, nil
}

func parseEditorSettings(data []byte) (*EditorSettings, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("editor: empty settings document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var s EditorSettings
	if err := dec.Decode(&s); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("editor: empty settings document")
		}
		return nil, fmt.Errorf("editor: decode settings: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("editor: unexpected trailing YAML document in settings")
		}
		return nil, fmt.Errorf("editor: decode settings: %w", err)
	}
	if err := ValidateEditorSettings(&s); err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("editor: decode settings: %w", err)
	}
	if err := checkPlainSettingsNode(&root, s.Version); err != nil {
		return nil, err
	}
	return &s, nil
}

func checkPlainSettingsNode(n *yaml.Node, version int) error {
	if err := checkNoSettingsYAMLReferences(n); err != nil {
		return err
	}
	if err := checkDuplicateYAMLKeys(n); err != nil {
		return fmt.Errorf("editor: settings: %w", err)
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
				return loaderr.Errorf("version", "write the version as "+want, "editor: settings version %q must be written as %s", val.Value, want)
			}
		}
	}
	return nil
}

func checkNoSettingsYAMLReferences(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("editor: settings line %d: YAML aliases are not allowed", n.Line), Fix: fixSettingsYAMLRef}
	}
	if n.Anchor != "" {
		return &loaderr.Error{Line: n.Line, Column: n.Column, Problem: fmt.Sprintf("editor: settings line %d: YAML anchors are not allowed", n.Line), Fix: fixSettingsYAMLRef}
	}
	for i, c := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 && c.Tag == "!!merge" {
			return &loaderr.Error{Line: c.Line, Column: c.Column, Problem: fmt.Sprintf("editor: settings line %d: YAML merge keys are not allowed", c.Line), Fix: fixSettingsYAMLRef}
		}
		if err := checkNoSettingsYAMLReferences(c); err != nil {
			return err
		}
	}
	return nil
}

// ValidateEditorSettings checks s against the version 1 editor settings schema.
func ValidateEditorSettings(s *EditorSettings) error {
	if s == nil {
		return errors.New("editor: nil settings")
	}
	switch s.Version {
	case SettingsVersion:
	case 0:
		return loaderr.Errorf("version", fixSettingsVersion, "editor: settings is missing version (want %d)", SettingsVersion)
	default:
		return loaderr.Errorf("version", fixSettingsVersion, "editor: unsupported settings version %d (want %d)", s.Version, SettingsVersion)
	}
	if !s.Language.Valid() {
		return loaderr.Errorf("language", fixSettingsLanguage, "editor: invalid settings language %q (want system, en, or zh)", s.Language)
	}
	if !s.Appearance.Valid() {
		return loaderr.Errorf("appearance", fixSettingsAppear, "editor: invalid settings appearance %q (want system, light, or dark)", s.Appearance)
	}
	if s.ViewportOverlays != nil {
		seen := make(map[string]struct{}, len(*s.ViewportOverlays))
		for i, id := range *s.ViewportOverlays {
			if err := validateOverlayID(id); err != nil {
				return loaderr.Errorf(fmt.Sprintf("viewportOverlays[%d]", i), "use non-empty overlay IDs without spaces or control characters", "%v", err)
			}
			if _, dup := seen[id]; dup {
				return loaderr.Errorf(fmt.Sprintf("viewportOverlays[%d]", i), "remove duplicate overlay IDs", "editor: duplicate viewport overlay %q", id)
			}
			seen[id] = struct{}{}
		}
	}
	for _, cmdID := range slices.Sorted(maps.Keys(s.Shortcuts)) {
		raw := s.Shortcuts[cmdID]
		if err := validateCommandID(cmdID); err != nil {
			return loaderr.Errorf("shortcuts."+cmdID, "use a valid command ID without spaces or control characters", "%v", err)
		}
		norm, err := NormalizeShortcut(raw)
		if err != nil {
			return loaderr.Errorf("shortcuts."+cmdID, "write a valid shortcut such as f, mod+s, or empty to unbind", "%v", err)
		}
		if err := validateRebindableShortcut(norm); err != nil {
			return loaderr.Errorf("shortcuts."+cmdID, "choose a non-reserved shortcut (escape, tab, and OS system keys cannot be bound)", "%v", err)
		}
	}
	return nil
}

// EncodeEditorSettings validates s and writes deterministic 2-space YAML prefixed with SettingsSchemaComment.
func EncodeEditorSettings(s *EditorSettings) ([]byte, error) {
	if err := ValidateEditorSettings(s); err != nil {
		return nil, err
	}
	var shortcuts map[string]string
	if len(s.Shortcuts) > 0 {
		shortcuts = make(map[string]string, len(s.Shortcuts))
		for cmdID, raw := range s.Shortcuts {
			norm, err := NormalizeShortcut(raw)
			if err != nil {
				return nil, err
			}
			shortcuts[cmdID] = norm
		}
	}
	out := EditorSettings{
		Version:    s.Version,
		Language:   s.Language,
		Appearance: s.Appearance,
		Shortcuts:  shortcuts,
	}
	if s.ViewportOverlays != nil {
		sorted := slices.Clone(*s.ViewportOverlays)
		if sorted == nil {
			sorted = []string{}
		}
		slices.Sort(sorted)
		out.ViewportOverlays = &sorted
	}
	var buf bytes.Buffer
	buf.WriteString(SettingsSchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&out); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("editor: encode settings: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("editor: encode settings: %w", err)
	}
	return buf.Bytes(), nil
}

// DefaultSettingsPath returns the per-user path outside any project where editor settings are stored (for example "$UserConfigDir/nsgo-game/editor.yaml").
func DefaultSettingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("editor: resolve user config directory: %w", err)
	}
	return filepath.Join(dir, "nsgo-game", "editor.yaml"), nil
}

// LoadEditorSettings reads and parses per-user editor settings from path.
func LoadEditorSettings(path string) (*EditorSettings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := ParseEditorSettings(data)
	if err != nil {
		return nil, loaderr.WithFile(err, path)
	}
	return s, nil
}

// SaveEditorSettings validates and atomically writes s as deterministic YAML to path, creating parent directories as needed.
func SaveEditorSettings(path string, s *EditorSettings) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("editor: settings path is empty")
	}
	data, err := EncodeEditorSettings(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("editor: create settings directory: %w", err)
	}
	return writeFileAtomic(path, data)
}

// Settings returns the editor's current per-user settings.
func (e *Editor) Settings() EditorSettings {
	if e == nil {
		return DefaultEditorSettings()
	}
	s := e.settings
	if s.Version == 0 {
		s.Version = SettingsVersion
	}
	if !s.Language.Valid() {
		s.Language = LanguageSystem
	}
	if !s.Appearance.Valid() {
		s.Appearance = AppearanceSystem
	}
	if s.ViewportOverlays != nil {
		cp := slices.Clone(*s.ViewportOverlays)
		if cp == nil {
			cp = []string{}
		}
		s.ViewportOverlays = &cp
	}
	if len(s.Shortcuts) > 0 {
		s.Shortcuts = maps.Clone(s.Shortcuts)
	} else {
		s.Shortcuts = nil
	}
	return s
}

// Language returns the editor's configured language setting (LanguageSystem, LanguageEnglish, or LanguageChinese).
func (e *Editor) Language() Language {
	return e.Settings().Language
}

// Appearance returns the editor's configured appearance setting (AppearanceSystem, AppearanceLight, or AppearanceDark).
func (e *Editor) Appearance() Appearance {
	return e.Settings().Appearance
}

// Dark reports whether the editor is currently shown in dark appearance: when a UI scope is attached it reflects cx.Dark() (combining SetAppearance, SetSubtreeAppearance, and the system appearance); otherwise it reports Appearance() == AppearanceDark.
func (e *Editor) Dark() bool {
	if e == nil {
		return false
	}
	if e.scope != nil {
		return e.scope.Dark()
	}
	return e.Appearance() == AppearanceDark
}

// SetLanguage updates the editor's language setting live without a restart. When SettingsPath is non-empty, the updated settings are saved to SettingsPath.
func (e *Editor) SetLanguage(lang Language) error {
	if e == nil {
		return nil
	}
	if !lang.Valid() {
		return loaderr.Errorf("language", fixSettingsLanguage, "editor: invalid settings language %q (want system, en, or zh)", lang)
	}
	cur := e.Settings()
	if cur.Language == lang {
		return nil
	}
	cur.Language = lang
	e.settings = cur
	if e.SettingsPath != "" {
		if err := SaveEditorSettings(e.SettingsPath, &cur); err != nil {
			e.notify()
			return err
		}
	}
	e.notify()
	return nil
}

// SetAppearance updates the editor's appearance setting live through NSGo's Scope.SetAppearance. When SettingsPath is non-empty, the updated settings are saved to SettingsPath.
func (e *Editor) SetAppearance(appearance Appearance) error {
	if e == nil {
		return nil
	}
	if !appearance.Valid() {
		return loaderr.Errorf("appearance", fixSettingsAppear, "editor: invalid settings appearance %q (want system, light, or dark)", appearance)
	}
	cur := e.Settings()
	changed := cur.Appearance != appearance
	cur.Appearance = appearance
	e.settings = cur
	if e.scope != nil {
		e.scope.SetAppearance(appearance.CoreAppearance())
	}
	if !changed {
		return nil
	}
	if e.SettingsPath != "" {
		if err := SaveEditorSettings(e.SettingsPath, &cur); err != nil {
			e.notify()
			return err
		}
	}
	e.notify()
	return nil
}

// ApplySettings validates and applies s to the editor without writing to disk.
func (e *Editor) ApplySettings(s EditorSettings) error {
	if e == nil {
		return nil
	}
	if err := ValidateEditorSettings(&s); err != nil {
		return err
	}
	if s.ViewportOverlays != nil {
		cp := slices.Clone(*s.ViewportOverlays)
		if cp == nil {
			cp = []string{}
		}
		slices.Sort(cp)
		s.ViewportOverlays = &cp
	}
	if len(s.Shortcuts) > 0 {
		normalized := make(map[string]string, len(s.Shortcuts))
		for cmdID, raw := range s.Shortcuts {
			norm, err := NormalizeShortcut(raw)
			if err != nil {
				return err
			}
			normalized[cmdID] = norm
		}
		s.Shortcuts = normalized
	} else {
		s.Shortcuts = nil
	}
	e.settings = s
	e.syncOverlaysFromSettings()
	if e.scope != nil {
		e.scope.SetAppearance(s.Appearance.CoreAppearance())
	}
	e.notify()
	return nil
}

// LoadSettings sets SettingsPath to path and loads per-user editor settings from it. If path does not exist yet, default settings are applied without error; if the file exists and is invalid, an error is returned without changing the current settings.
func (e *Editor) LoadSettings(path string) error {
	if e == nil {
		return nil
	}
	e.SettingsPath = path
	s, err := LoadEditorSettings(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return e.ApplySettings(DefaultEditorSettings())
		}
		return err
	}
	return e.ApplySettings(*s)
}

// LoadUserSettings loads per-user editor settings from DefaultSettingsPath(), creating no file until a setting is changed or saved.
func (e *Editor) LoadUserSettings() error {
	path, err := DefaultSettingsPath()
	if err != nil {
		return err
	}
	return e.LoadSettings(path)
}

// SaveSettings persists the editor's current settings to SettingsPath (or DefaultSettingsPath() when SettingsPath is empty).
func (e *Editor) SaveSettings() error {
	if e == nil {
		return nil
	}
	path := e.SettingsPath
	if path == "" {
		var err error
		path, err = DefaultSettingsPath()
		if err != nil {
			return err
		}
		e.SettingsPath = path
	}
	s := e.Settings()
	return SaveEditorSettings(path, &s)
}

// SettingsPage identifies a page in the unified Settings view (docs/design/editor/Settings.dc.html).
type SettingsPage string

const (
	// SettingsPageProject edits game.yaml's project metadata, window, display, physics, autoload, and plugins.
	SettingsPageProject SettingsPage = "project"
	// SettingsPageInput edits game.yaml's input actions, bindings, touch/mouse regions, and axes.
	SettingsPageInput SettingsPage = "input"
	// SettingsPageAudio edits game.yaml's audio master level, buses, DSP effect chains, and live audition.
	SettingsPageAudio SettingsPage = "audio"
	// SettingsPageExport edits game.yaml's export output, platform presets, credits pre-flight, and release builds.
	SettingsPageExport SettingsPage = "export"
	// SettingsPageGeneral edits per-computer editor language and appearance preferences.
	SettingsPageGeneral SettingsPage = "general"
	// SettingsPageShortcuts edits per-computer command keyboard shortcuts within a scope.
	SettingsPageShortcuts SettingsPage = "shortcuts"
)

// Valid reports whether p is a recognized Settings page.
func (p SettingsPage) Valid() bool {
	switch p {
	case SettingsPageProject, SettingsPageInput, SettingsPageAudio, SettingsPageExport, SettingsPageGeneral, SettingsPageShortcuts:
		return true
	default:
		return false
	}
}

// IsProjectPage reports whether p edits the open project's game.yaml rather than per-computer editor preferences.
func (p SettingsPage) IsProjectPage() bool {
	switch p {
	case SettingsPageProject, SettingsPageInput, SettingsPageAudio, SettingsPageExport:
		return true
	default:
		return false
	}
}

// SettingsOpen reports whether the unified Settings view is currently open.
func (e *Editor) SettingsOpen() bool {
	return e != nil && e.settingsOpen
}

// SettingsPage returns the active page of the unified Settings view, defaulting to SettingsPageGeneral.
func (e *Editor) SettingsPage() SettingsPage {
	if e == nil || !e.settingsPage.Valid() {
		return SettingsPageGeneral
	}
	return e.settingsPage
}

// OpenSettingsPage opens the unified Settings view on page (or switches to page if already open).
func (e *Editor) OpenSettingsPage(page SettingsPage) {
	if e == nil || !page.Valid() {
		return
	}
	prevOpen := e.settingsOpen
	prevPage := e.settingsPage
	if prevOpen && prevPage == page {
		return
	}
	if prevOpen && prevPage == SettingsPageInput && page != SettingsPageInput {
		e.resetInputMapTransient()
	}
	if page != SettingsPageShortcuts {
		e.shortcutCaptureActive = false
	}
	e.shortcutsSheetOpen = false
	e.settingsOpen = true
	e.settingsPage = page
	if page == SettingsPageShortcuts {
		e.editorSettingsTab = EditorSettingsTabShortcuts
	} else if page == SettingsPageGeneral {
		e.editorSettingsTab = EditorSettingsTabGeneral
	}
	switch page {
	case SettingsPageProject:
		if e.projectSettings.section == "" {
			e.projectSettings.section = ProjectSectionGeneral
		}
	case SettingsPageAudio:
		e.syncAudioMixer()
	case SettingsPageExport:
		e.syncExportPanel()
		e.CheckExportCredits()
		return
	}
	e.notify()
}

// CloseSettings closes the unified Settings view, cancelling any active input key capture/drag and stopping audio audition when leaving the Audio buses page.
func (e *Editor) CloseSettings() {
	if e == nil || !e.settingsOpen {
		return
	}
	prevPage := e.settingsPage
	e.settingsOpen = false
	e.shortcutCaptureActive = false
	if prevPage == SettingsPageInput {
		e.resetInputMapTransient()
	}
	if prevPage == SettingsPageAudio {
		e.stopAudioAudition()
	}
	e.notify()
}

// SetSettingsOpen opens or closes the unified Settings view. Opening shows the previously active page (or SettingsPageProject when a project is open and no page has been selected yet).
func (e *Editor) SetSettingsOpen(open bool) {
	if e == nil {
		return
	}
	if !open {
		e.CloseSettings()
		return
	}
	page := e.settingsPage
	if !page.Valid() {
		if e.Project != nil {
			page = SettingsPageProject
		} else {
			page = SettingsPageGeneral
		}
	}
	e.OpenSettingsPage(page)
}

// EditorSettingsOpen reports whether the Settings view is currently open on the General or Shortcuts (editor preferences) page.
func (e *Editor) EditorSettingsOpen() bool {
	return e != nil && e.settingsOpen && (e.settingsPage == SettingsPageGeneral || e.settingsPage == SettingsPageShortcuts)
}

// SetEditorSettingsOpen opens the Settings view on the General page, or closes Settings when open is false and an editor preferences page is active.
func (e *Editor) SetEditorSettingsOpen(open bool) {
	if e == nil {
		return
	}
	if open {
		e.OpenSettingsPage(SettingsPageGeneral)
		return
	}
	if e.EditorSettingsOpen() {
		e.CloseSettings()
	}
}

// ToggleEditorSettings toggles the Settings view's General (editor preferences) page open or closed.
func (e *Editor) ToggleEditorSettings() {
	if e == nil {
		return
	}
	if e.EditorSettingsOpen() {
		e.CloseSettings()
		return
	}
	e.OpenSettingsPage(SettingsPageGeneral)
}

// EditorSettingsTab returns the active page in the Editor Settings dialog.
func (e *Editor) EditorSettingsTab() EditorSettingsTab {
	if e == nil {
		return EditorSettingsTabGeneral
	}
	return e.editorSettingsTab
}

// SetEditorSettingsTab switches the active page in the Editor Settings dialog.
func (e *Editor) SetEditorSettingsTab(tab EditorSettingsTab) {
	if e == nil {
		return
	}
	changed := e.editorSettingsTab != tab
	e.editorSettingsTab = tab
	if tab != EditorSettingsTabShortcuts {
		e.shortcutCaptureActive = false
	}
	if e.settingsOpen {
		if tab == EditorSettingsTabShortcuts {
			e.OpenSettingsPage(SettingsPageShortcuts)
			return
		}
		if tab == EditorSettingsTabGeneral {
			e.OpenSettingsPage(SettingsPageGeneral)
			return
		}
	}
	if changed {
		e.notify()
	}
}

// OpenShortcutsSettings opens the Settings view directly to the Shortcuts page.
func (e *Editor) OpenShortcutsSettings() {
	if e == nil {
		return
	}
	e.OpenSettingsPage(SettingsPageShortcuts)
}

// ShortcutsSheetOpen reports whether the ⌘/ Keyboard Shortcuts reference sheet is open.
func (e *Editor) ShortcutsSheetOpen() bool {
	return e != nil && e.shortcutsSheetOpen
}

// SetShortcutsSheetOpen opens or closes the ⌘/ Keyboard Shortcuts reference sheet.
func (e *Editor) SetShortcutsSheetOpen(open bool) {
	if e == nil || e.shortcutsSheetOpen == open {
		return
	}
	e.shortcutsSheetOpen = open
	if open {
		e.CloseSettings()
		e.shortcutCaptureActive = false
	}
	e.notify()
}

// ToggleShortcutsSheet toggles the ⌘/ Keyboard Shortcuts reference sheet open or closed.
func (e *Editor) ToggleShortcutsSheet() {
	if e == nil {
		return
	}
	e.SetShortcutsSheetOpen(!e.shortcutsSheetOpen)
}
