package editor

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"maps"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/a11y"
	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrNoHUDScreen is returned by HUD layout edits when no screen document is currently open.
var ErrNoHUDScreen = errors.New("editor: no HUD screen open")

// HUDOverlays controls the four diagnostic overlays shown on the HUD layout multi-size canvas.
type HUDOverlays struct {
	SafeAreas  bool
	Anchors    bool
	TouchAreas bool
	FocusOrder bool
}

// DefaultHUDOverlays returns the default overlay set with all four overlays enabled, matching docs/design/editor/HUDLayout.dc.html.
func DefaultHUDOverlays() HUDOverlays {
	return HUDOverlays{
		SafeAreas:  true,
		Anchors:    true,
		TouchAreas: true,
		FocusOrder: true,
	}
}

// HUDScreenEntry summarizes one *.screen.yaml file listed in the left rail's SCREENS section.
type HUDScreenEntry struct {
	// Path is the screen's slash-separated path from the project root (or file path for standalone screens).
	Path string
	// Name is the human-readable screen name (typically the file stem without .screen.yaml).
	Name string
	// NodeCount is the total number of UI nodes in the screen tree.
	NodeCount int
	// ProblemCount is the number of GAME CHECK problems for this screen in the active language.
	ProblemCount int
	// LoadErr holds any parse or validation error encountered when reading this screen file.
	LoadErr error
}

// HUDTreeRow is one flattened row of the active screen's NODES tree in the left rail.
type HUDTreeRow struct {
	// ID is the ScreenNodeDoc.ID, or "" for the synthetic root screen row.
	ID string
	// Name is the displayed label (node ID, or screen name on the root row).
	Name string
	// Type is the ScreenNodeDoc.Type (empty on the synthetic root row).
	Type gameui.ScreenNodeType
	// Depth is 0 for the root row and 1+ for nodes in the screen tree.
	Depth int
	// AnchorTag is the short monospace tag shown on the right ("screen", "TL", "TC", "TR", "CL", "C", "CR", "BL", "BC", "BR", "FILL", or "").
	AnchorTag string
	// HasProblem reports whether GAME CHECK found a problem on this node in the active language.
	HasProblem bool
	// ParentID is the containing node's ID, or "" for top-level screen nodes.
	ParentID string
	// Index is the node's position inside its parent's children slice (or doc.Nodes).
	Index int
	// SiblingLen is the total number of siblings in the parent container.
	SiblingLen int
}

// HUDTextWidthRow is one row of the TEXT WIDTH, PX table comparing a text node's box against its width in every project locale.
type HUDTextWidthRow struct {
	NodeID   string
	TextKey  string
	BoxLabel string
	FitsText bool
	BoxWidth float32
	Widths   map[string]float32
	Overflow map[string]float32
}

// HUDProblem is one diagnostic listed under GAME CHECK for the active screen and language.
type HUDProblem struct {
	Code   gameui.ScreenIssueCode
	NodeID string
	Title  string
	Detail string
	Locale string
	OverPx float32
	Issue  gameui.ScreenIssue
}

// HUDPromptRequest captures an agent prompt submitted from the HUD layout composer or "Ask the agent to fix N" button.
type HUDPromptRequest struct {
	Prompt     string
	ScreenPath string
	ScreenName string
	SelectedID string
	Locale     string
	Problems   []HUDProblem
}

// HUDFrameLayout describes one of the four simultaneous reference device previews on the multi-size canvas.
type HUDFrameLayout struct {
	Device gameui.DeviceProfile
	Label  string
	Scale  float32
	X, Y   float32
	W, H   float32
	Bezel  float32
	Built  *gameui.BuiltScreen
}

type hudScreenRecord struct {
	path    string
	absPath string
	name    string
	doc     *gameui.ScreenDoc
	raw     []byte
	loadErr error
}

type hudDragState struct {
	active       bool
	nodeID       string
	deviceID     string
	scale        float32
	anchor       gameui.ScreenAnchor
	startCanvasX float32
	startCanvasY float32
	origOffset   gameui.ScreenOffsetDoc
	beforeDoc    *gameui.ScreenDoc
	beforeRaw    []byte
	moved        bool
}

type hudLayoutState struct {
	open           bool
	rev            uint64
	session        uint64
	localeRev      uint64
	screens        []hudScreenRecord
	activePath     string
	selectedNodeID string
	locale         string
	locales        []string
	localeStrings  map[string]map[string]string
	localePlurals  map[string]map[string]l10n.PluralForms
	overlays       HUDOverlays
	drag           hudDragState
	canvasW        float32
	canvasH        float32
	lastPrompt     HUDPromptRequest
}

// isScreenFile reports whether relPath is a *.screen.yaml or *.screen.yml file.
func isScreenFile(relPath string) bool {
	lower := strings.ToLower(relPath)
	return strings.HasSuffix(lower, ".screen.yaml") || strings.HasSuffix(lower, ".screen.yml")
}

// screenDisplayName derives a concise screen display name from a path and optional ScreenDoc.
func screenDisplayName(relPath string, doc *gameui.ScreenDoc) string {
	base := path.Base(strings.ReplaceAll(relPath, `\`, "/"))
	lower := strings.ToLower(base)
	switch {
	case strings.HasSuffix(lower, ".screen.yaml"):
		base = base[:len(base)-len(".screen.yaml")]
	case strings.HasSuffix(lower, ".screen.yml"):
		base = base[:len(base)-len(".screen.yml")]
	}
	if base != "" && base != "." && base != "/" {
		return base
	}
	if doc != nil && doc.Name != "" {
		return doc.Name
	}
	return "screen"
}

// screenFileName returns the base filename (for example "hud.screen.yaml") for the breadcrumb and NODES header.
func screenFileName(relPath string, name string) string {
	if relPath != "" {
		return path.Base(strings.ReplaceAll(relPath, `\`, "/"))
	}
	if name != "" {
		return name + ".screen.yaml"
	}
	return "screen.screen.yaml"
}

// hudLocaleLabel returns the human-readable segmented control label for a BCP-47 locale tag.
func hudLocaleLabel(loc string) string {
	low := strings.ToLower(strings.TrimSpace(loc))
	switch {
	case low == "en" || strings.HasPrefix(low, "en-"):
		if low == "en" {
			return "English"
		}
		return loc
	case low == "zh" || low == "zh-hans" || low == "zh-cn" || low == "zh-hans-cn":
		return "中文"
	case low == "zh-hant" || low == "zh-tw" || low == "zh-hk":
		return "繁體中文"
	case low == "ja" || strings.HasPrefix(low, "ja-"):
		return "日本語"
	case low == "ko" || strings.HasPrefix(low, "ko-"):
		return "한국어"
	case low == "fr":
		return "Français"
	case low == "de":
		return "Deutsch"
	case low == "es":
		return "Español"
	default:
		return loc
	}
}

// hudLocaleShortHeader returns the compact column header used in the TEXT WIDTH, PX table.
func hudLocaleShortHeader(loc string) string {
	low := strings.ToLower(strings.TrimSpace(loc))
	switch {
	case low == "en":
		return "EN"
	case low == "zh" || low == "zh-hans" || low == "zh-cn" || low == "zh-hans-cn":
		return "中文"
	case low == "zh-hant" || low == "zh-tw" || low == "zh-hk":
		return "繁中"
	case low == "ja":
		return "日本語"
	case low == "ko":
		return "한국어"
	default:
		return strings.ToUpper(loc)
	}
}

// HUDLayoutOpen reports whether the HUD and menu layout screen is currently active in EditorView.
func (e *Editor) HUDLayoutOpen() bool {
	return e != nil && e.hud.open
}

// SetHUDLayoutOpen opens or closes the HUD and menu layout screen.
// Opening it while a project is open automatically scans project *.screen.yaml files and locales/*.yaml tables.
func (e *Editor) SetHUDLayoutOpen(open bool) {
	if e == nil {
		return
	}
	if open && e.playing {
		return
	}
	if e.hud.open == open {
		return
	}
	if open {
		e.startScreen.open = false
		e.merge.open = false
		e.playtest.open = false
		e.closeBuildReleaseScreenSilent()
		if e.Project != nil && e.ProjectPath != "" && len(e.hud.screens) == 0 {
			_ = e.RefreshHUDScreens()
		}
		e.ensureHUDDefaults()
	} else {
		e.CancelHUDTouchDrag()
	}
	e.hud.open = open
	e.hud.rev++
	if open {
		e.Status = "Opened HUD & menu layout"
	} else {
		e.Status = "Returned to scene editor"
	}
	e.notify()
}

func (e *Editor) ensureHUDDefaults() {
	if e.hud.overlays == (HUDOverlays{}) && e.hud.activePath == "" && len(e.hud.locales) == 0 {
		e.hud.overlays = DefaultHUDOverlays()
	}
	if len(e.hud.locales) == 0 {
		e.hud.locales = []string{"en"}
	}
	if e.hud.localeStrings == nil {
		e.hud.localeStrings = map[string]map[string]string{"en": {}}
	}
	if e.hud.localePlurals == nil {
		e.hud.localePlurals = map[string]map[string]l10n.PluralForms{}
	}
	if e.hud.locale == "" || !slices.Contains(e.hud.locales, e.hud.locale) {
		e.hud.locale = e.hud.locales[0]
	}
	if e.hud.activePath == "" && len(e.hud.screens) > 0 {
		e.hud.activePath = e.hud.screens[0].path
	}
	e.ensureHUDSelection()
}

func (e *Editor) resetHUDForProject() {
	e.CancelHUDTouchDrag()
	e.hud.session++
	e.hud.rev++
	e.hud.screens = nil
	e.hud.activePath = ""
	e.hud.selectedNodeID = ""
	e.hud.locales = nil
	e.hud.locale = ""
	e.hud.localeStrings = nil
	e.hud.localePlurals = nil
	if e.Project != nil && e.ProjectPath != "" {
		_ = e.refreshHUDScreensInternal(false)
	}
}

// checkHUDProjectSymlink enforces that absPath does not escape the open project's root via symlinks.
// When mustContain is true (project-relative reads and scans), absPath must resolve strictly inside
// the project root. When mustContain is false (disk writes), containment is enforced whenever absPath
// is lexically inside the project root so symlinks inside the project cannot redirect writes outside.
func (e *Editor) checkHUDProjectSymlink(absPath string, mustContain bool) error {
	if e == nil || e.ProjectPath == "" {
		return nil
	}
	root := filepath.Clean(filepath.Dir(e.ProjectPath))
	cleanPath := filepath.Clean(absPath)
	if !mustContain {
		lexRel, err := filepath.Rel(root, cleanPath)
		if err != nil || lexRel == ".." || strings.HasPrefix(lexRel, ".."+string(filepath.Separator)) || filepath.IsAbs(lexRel) {
			return nil
		}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}
	var realPath string
	if mustContain {
		realPath, err = filepath.EvalSymlinks(cleanPath)
		if err != nil {
			return err
		}
	} else {
		realPath, err = evalHUDSymlinksOrParent(cleanPath)
		if err != nil {
			return err
		}
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("editor: screen path %q resolves outside the project", absPath)
	}
	return nil
}

func evalHUDSymlinksOrParent(p string) (string, error) {
	p = filepath.Clean(p)
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if fi, lerr := os.Lstat(p); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("editor: refusing to follow unresolved symlink %q", p)
	}
	cur := p
	var tail []string
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			slices.Reverse(tail)
			parts := append([]string{real}, tail...)
			return filepath.Join(parts...), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if fi, lerr := os.Lstat(cur); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("editor: refusing to follow unresolved symlink %q", cur)
		}
	}
}

// RefreshHUDScreens rescans *.screen.yaml files and locales/*.yaml string tables from the open project directory.
func (e *Editor) RefreshHUDScreens() error {
	if e == nil {
		return nil
	}
	err := e.refreshHUDScreensInternal(true)
	e.notify()
	return err
}

func (e *Editor) refreshHUDScreensInternal(keepActive bool) error {
	if e.ProjectPath == "" {
		e.ensureHUDDefaults()
		return nil
	}
	root := filepath.Dir(e.ProjectPath)
	e.loadHUDLocalesFromDir(filepath.Join(root, "locales"), true)

	var records []hudScreenRecord
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name != "." && (skippedProjectDir(name) || name == "node_modules" || name == "dist" || name == "build") {
				return filepath.SkipDir
			}
			return nil
		}
		if !isScreenFile(d.Name()) {
			return nil
		}
		if err := e.checkHUDProjectSymlink(p, true); err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		records = append(records, loadHUDScreenRecord(rel, p))
		return nil
	})
	if err != nil {
		return err
	}
	slices.SortFunc(records, func(a, b hudScreenRecord) int {
		return cmp.Compare(a.path, b.path)
	})

	prevActive := e.hud.activePath
	e.hud.screens = records
	if keepActive && prevActive != "" && slices.ContainsFunc(records, func(r hudScreenRecord) bool { return r.path == prevActive }) {
		e.hud.activePath = prevActive
	} else if len(records) > 0 {
		// Prefer a screen named "hud" when available, matching HUDLayout.dc.html.
		e.hud.activePath = records[0].path
		for _, r := range records {
			if strings.EqualFold(r.name, "hud") {
				e.hud.activePath = r.path
				break
			}
		}
	} else {
		e.hud.activePath = ""
	}
	e.ensureHUDDefaults()
	e.hud.rev++
	return nil
}

func loadHUDScreenRecord(relPath, absPath string) hudScreenRecord {
	rec := hudScreenRecord{
		path:    relPath,
		absPath: absPath,
		name:    screenDisplayName(relPath, nil),
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		rec.loadErr = err
		return rec
	}
	rec.raw = raw
	doc, err := gameui.ParseScreen(raw)
	if err != nil {
		rec.loadErr = loaderr.WithFile(err, relPath)
		return rec
	}
	rec.doc = doc
	rec.name = screenDisplayName(relPath, doc)
	return rec
}

func (e *Editor) loadHUDLocalesFromDir(dir string, enforceProjectRoot bool) {
	locales := make([]string, 0, 4)
	stringsMap := make(map[string]map[string]string)
	pluralsMap := make(map[string]map[string]l10n.PluralForms)

	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			low := strings.ToLower(ent.Name())
			if !strings.HasSuffix(low, ".yaml") && !strings.HasSuffix(low, ".yml") {
				continue
			}
			locFile := filepath.Join(dir, ent.Name())
			if enforceProjectRoot {
				if err := e.checkHUDProjectSymlink(locFile, true); err != nil {
					continue
				}
			}
			data, err := os.ReadFile(locFile)
			if err != nil {
				continue
			}
			tbl, err := l10n.ParseYAML(data)
			if err != nil || tbl.Locale == "" {
				continue
			}
			if !slices.Contains(locales, tbl.Locale) {
				locales = append(locales, tbl.Locale)
			}
			if tbl.Strings != nil {
				stringsMap[tbl.Locale] = maps.Clone(tbl.Strings)
			} else {
				stringsMap[tbl.Locale] = map[string]string{}
			}
			if tbl.Plurals != nil {
				pluralsMap[tbl.Locale] = maps.Clone(tbl.Plurals)
			} else {
				pluralsMap[tbl.Locale] = map[string]l10n.PluralForms{}
			}
		}
	}

	// Overlay any unsaved live edits from Editor.l10n when open.
	if e.l10n != nil {
		for _, loc := range e.l10n.locales {
			if !slices.Contains(locales, loc) {
				locales = append(locales, loc)
			}
			stringsMap[loc] = maps.Clone(e.l10n.strings[loc])
			pluralsMap[loc] = maps.Clone(e.l10n.plurals[loc])
		}
	}

	if len(locales) == 0 {
		locales = []string{"en"}
		stringsMap["en"] = map[string]string{}
		pluralsMap["en"] = map[string]l10n.PluralForms{}
	} else {
		slices.Sort(locales)
	}
	e.hud.locales = locales
	e.hud.localeStrings = stringsMap
	e.hud.localePlurals = pluralsMap
	e.hud.localeRev++
	if e.hud.locale == "" || !slices.Contains(locales, e.hud.locale) {
		e.hud.locale = locales[0]
	}
}

// OpenHUDScreen opens the screen at relPath (relative to the open project root, or matching an already-loaded screen path)
// and activates the HUD and menu layout screen.
func (e *Editor) OpenHUDScreen(relPath string) error {
	if e == nil {
		return ErrNoHUDScreen
	}
	if e.playing {
		return ErrPlaying
	}
	relPath = strings.TrimSpace(strings.ReplaceAll(relPath, `\`, "/"))
	if relPath == "" {
		return ErrNoHUDScreen
	}
	e.CancelHUDTouchDrag()

	// Refresh from project if open and not yet scanned or missing this screen.
	if e.Project != nil && e.ProjectPath != "" {
		if len(e.hud.screens) == 0 || !slices.ContainsFunc(e.hud.screens, func(r hudScreenRecord) bool { return r.path == relPath }) {
			_ = e.refreshHUDScreensInternal(false)
		}
	}

	idx := slices.IndexFunc(e.hud.screens, func(r hudScreenRecord) bool {
		return r.path == relPath || r.name == relPath
	})
	if idx < 0 && e.ProjectPath != "" {
		abs, err := e.projectFile(relPath)
		if err != nil {
			return err
		}
		if err := e.checkHUDProjectSymlink(abs, true); err != nil {
			return err
		}
		rec := loadHUDScreenRecord(relPath, abs)
		if rec.loadErr != nil && errors.Is(rec.loadErr, os.ErrNotExist) {
			return rec.loadErr
		}
		e.hud.screens = append(e.hud.screens, rec)
		slices.SortFunc(e.hud.screens, func(a, b hudScreenRecord) int {
			return cmp.Compare(a.path, b.path)
		})
		idx = slices.IndexFunc(e.hud.screens, func(r hudScreenRecord) bool { return r.path == relPath })
	}
	if idx < 0 {
		return fmt.Errorf("editor: screen %q not found", relPath)
	}

	e.hud.activePath = e.hud.screens[idx].path
	e.hud.open = true
	e.hud.selectedNodeID = ""
	e.ensureHUDDefaults()
	e.selectFirstInterestingHUDNode()
	e.hud.rev++
	e.Status = "Opened screen " + e.hud.activePath
	e.notify()
	return e.hud.screens[idx].loadErr
}

// OpenHUDScreenFile opens a standalone *.screen.yaml file from disk (scanning sibling *.screen.yaml files and a nearby locales/ folder if present).
func (e *Editor) OpenHUDScreenFile(absPath string) error {
	if e == nil {
		return ErrNoHUDScreen
	}
	if e.playing {
		return ErrPlaying
	}
	abs, err := filepath.Abs(absPath)
	if err != nil {
		return err
	}
	e.CancelHUDTouchDrag()
	e.hud.session++

	dir := filepath.Dir(abs)
	// Look for a locales directory beside or above the screen directory.
	if info, err := os.Stat(filepath.Join(dir, "locales")); err == nil && info.IsDir() {
		e.loadHUDLocalesFromDir(filepath.Join(dir, "locales"), false)
	} else if info, err := os.Stat(filepath.Join(filepath.Dir(dir), "locales")); err == nil && info.IsDir() {
		e.loadHUDLocalesFromDir(filepath.Join(filepath.Dir(dir), "locales"), false)
	}

	var records []hudScreenRecord
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, ent := range entries {
			if ent.IsDir() || !isScreenFile(ent.Name()) {
				continue
			}
			p := filepath.Join(dir, ent.Name())
			records = append(records, loadHUDScreenRecord(ent.Name(), p))
		}
	}
	if !slices.ContainsFunc(records, func(r hudScreenRecord) bool { return r.absPath == abs }) {
		records = append(records, loadHUDScreenRecord(filepath.Base(abs), abs))
	}
	slices.SortFunc(records, func(a, b hudScreenRecord) int {
		return cmp.Compare(a.path, b.path)
	})
	e.hud.screens = records
	e.hud.activePath = filepath.Base(abs)
	e.hud.open = true
	e.hud.selectedNodeID = ""
	e.ensureHUDDefaults()
	e.selectFirstInterestingHUDNode()
	e.hud.rev++
	e.Status = "Opened screen " + filepath.Base(abs)
	e.notify()
	return e.HUDLoadErr()
}

// OpenHUDScreenDoc opens an in-memory ScreenDoc (and optional locale string tables) in the HUD layout screen.
// If locales is nil, existing locale tables are kept (or defaulted to an empty "en" table).
func (e *Editor) OpenHUDScreenDoc(name string, doc *gameui.ScreenDoc, locales map[string]map[string]string) error {
	if e == nil || doc == nil {
		return ErrNoHUDScreen
	}
	if e.playing {
		return ErrPlaying
	}
	if err := gameui.ValidateScreen(doc); err != nil {
		return err
	}
	e.CancelHUDTouchDrag()

	if name == "" {
		name = doc.Name
	}
	if name == "" {
		name = "hud"
	}
	relPath := name
	if !isScreenFile(relPath) {
		relPath = "screens/" + name + ".screen.yaml"
	}
	raw, err := gameui.EncodeScreen(doc)
	if err != nil {
		return err
	}
	rec := hudScreenRecord{
		path: relPath,
		name: screenDisplayName(relPath, doc),
		doc:  doc.Clone(),
		raw:  raw,
	}
	if idx := slices.IndexFunc(e.hud.screens, func(r hudScreenRecord) bool { return r.path == relPath }); idx >= 0 {
		e.hud.session++
		e.hud.screens[idx] = rec
	} else {
		e.hud.screens = append(e.hud.screens, rec)
		slices.SortFunc(e.hud.screens, func(a, b hudScreenRecord) int {
			return cmp.Compare(a.path, b.path)
		})
	}
	if locales != nil {
		locs := make([]string, 0, len(locales))
		sm := make(map[string]map[string]string, len(locales))
		pm := make(map[string]map[string]l10n.PluralForms, len(locales))
		for loc, m := range locales {
			locs = append(locs, loc)
			sm[loc] = maps.Clone(m)
			pm[loc] = map[string]l10n.PluralForms{}
		}
		slices.Sort(locs)
		e.hud.locales = locs
		e.hud.localeStrings = sm
		e.hud.localePlurals = pm
		e.hud.localeRev++
	}
	e.hud.activePath = relPath
	e.hud.open = true
	e.hud.selectedNodeID = ""
	e.ensureHUDDefaults()
	e.selectFirstInterestingHUDNode()
	e.hud.rev++
	e.Status = "Opened screen " + rec.name
	e.notify()
	return nil
}

func (e *Editor) activeHUDScreenRecord() *hudScreenRecord {
	if e == nil {
		return nil
	}
	for i := range e.hud.screens {
		if e.hud.screens[i].path == e.hud.activePath {
			return &e.hud.screens[i]
		}
	}
	if len(e.hud.screens) > 0 {
		e.hud.activePath = e.hud.screens[0].path
		return &e.hud.screens[0]
	}
	return nil
}

// HUDActiveScreenPath returns the active screen's project-relative path, or "".
func (e *Editor) HUDActiveScreenPath() string {
	rec := e.activeHUDScreenRecord()
	if rec == nil {
		return ""
	}
	return rec.path
}

// HUDActiveScreen returns a deep clone of the active ScreenDoc, or nil if no valid screen is open.
func (e *Editor) HUDActiveScreen() *gameui.ScreenDoc {
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		return nil
	}
	return rec.doc.Clone()
}

// HUDLoadErr returns the parse/validation error of the active screen, or nil.
func (e *Editor) HUDLoadErr() error {
	rec := e.activeHUDScreenRecord()
	if rec == nil {
		return nil
	}
	return rec.loadErr
}

// HUDProjectName returns the project name displayed at the start of the HUD layout breadcrumb.
func (e *Editor) HUDProjectName() string {
	if e != nil && e.Project != nil && e.Project.Name != "" {
		return e.Project.Name
	}
	if e != nil && e.ProjectPath != "" {
		return filepath.Base(filepath.Dir(e.ProjectPath))
	}
	return "project"
}

// HUDScreens returns the list of project screens shown in the left rail's SCREENS section.
func (e *Editor) HUDScreens() []HUDScreenEntry {
	if e == nil || len(e.hud.screens) == 0 {
		return nil
	}
	out := make([]HUDScreenEntry, 0, len(e.hud.screens))
	for i := range e.hud.screens {
		rec := &e.hud.screens[i]
		entry := HUDScreenEntry{
			Path:    rec.path,
			Name:    rec.name,
			LoadErr: rec.loadErr,
		}
		if rec.doc != nil {
			entry.NodeCount = countScreenNodes(rec.doc.Nodes)
			entry.ProblemCount = len(e.hudProblemsForDoc(rec.doc, e.HUDLocale()))
		}
		out = append(out, entry)
	}
	return out
}

func countScreenNodes(nodes []gameui.ScreenNodeDoc) int {
	n := 0
	for i := range nodes {
		n++
		n += countScreenNodes(nodes[i].Children)
	}
	return n
}

// HUDLocales returns the sorted list of available locales in the HUD layout screen.
func (e *Editor) HUDLocales() []string {
	if e == nil {
		return nil
	}
	if len(e.hud.locales) == 0 {
		return []string{"en"}
	}
	return slices.Clone(e.hud.locales)
}

// HUDLocale returns the currently selected language tag in the HUD layout screen.
func (e *Editor) HUDLocale() string {
	if e == nil {
		return "en"
	}
	if e.hud.locale != "" {
		return e.hud.locale
	}
	if len(e.hud.locales) > 0 {
		return e.hud.locales[0]
	}
	return "en"
}

// SetHUDLocale switches the preview language across all four screen sizes, the text width table, inspector, and GAME CHECK.
func (e *Editor) SetHUDLocale(locale string) {
	if e == nil {
		return
	}
	locale = strings.TrimSpace(locale)
	if locale == "" || locale == e.hud.locale {
		return
	}
	if !slices.Contains(e.hud.locales, locale) {
		// Allow matching by case-insensitive prefix (e.g. "zh" -> "zh-Hans").
		found := ""
		low := strings.ToLower(locale)
		for _, loc := range e.hud.locales {
			if strings.EqualFold(loc, locale) || strings.HasPrefix(strings.ToLower(loc), low+"-") {
				found = loc
				break
			}
		}
		if found == "" {
			return
		}
		locale = found
	}
	e.hud.locale = locale
	e.hud.rev++
	e.Status = "HUD language: " + hudLocaleLabel(locale)
	e.notify()
}

// HUDLocaleStrings returns a copy of the string map for locale.
func (e *Editor) HUDLocaleStrings(locale string) map[string]string {
	if e == nil || e.hud.localeStrings == nil {
		return nil
	}
	return maps.Clone(e.hud.localeStrings[locale])
}

func (e *Editor) hudLookupText(locale, key string) (string, bool) {
	if key == "" {
		return "", false
	}
	if m := e.hud.localeStrings[locale]; m != nil {
		if v, ok := m[key]; ok {
			return v, true
		}
	}
	if pm := e.hud.localePlurals[locale]; pm != nil {
		if pf, ok := pm[key]; ok {
			for _, candidate := range []string{pf.Other, pf.Many, pf.Few, pf.Two, pf.One, pf.Zero} {
				if candidate != "" {
					return candidate, true
				}
			}
		}
	}
	return "", false
}

// HUDOverlays returns the active viewport overlay toggles.
func (e *Editor) HUDOverlays() HUDOverlays {
	if e == nil {
		return DefaultHUDOverlays()
	}
	return e.hud.overlays
}

// SetHUDOverlays replaces the active viewport overlay toggles.
func (e *Editor) SetHUDOverlays(ov HUDOverlays) {
	if e == nil || e.hud.overlays == ov {
		return
	}
	e.hud.overlays = ov
	e.notify()
}

// ToggleHUDOverlay flips one overlay by name ("safe", "anchors", "touch", or "focus").
func (e *Editor) ToggleHUDOverlay(name string) {
	if e == nil {
		return
	}
	ov := e.hud.overlays
	switch strings.ToLower(name) {
	case "safe":
		ov.SafeAreas = !ov.SafeAreas
	case "anchors":
		ov.Anchors = !ov.Anchors
	case "touch":
		ov.TouchAreas = !ov.TouchAreas
	case "focus":
		ov.FocusOrder = !ov.FocusOrder
	default:
		return
	}
	e.SetHUDOverlays(ov)
}

// HUDSelectedNodeID returns the currently selected node's ID in the active screen, or "".
func (e *Editor) HUDSelectedNodeID() string {
	if e == nil {
		return ""
	}
	return e.hud.selectedNodeID
}

// SelectHUDNode selects the node with id in the active screen.
func (e *Editor) SelectHUDNode(id string) {
	if e == nil || id == e.hud.selectedNodeID {
		return
	}
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		return
	}
	if id != "" && findHUDNode(rec.doc.Nodes, id) == nil {
		return
	}
	e.hud.selectedNodeID = id
	e.hud.rev++
	e.notify()
}

func (e *Editor) selectFirstInterestingHUDNode() {
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		e.hud.selectedNodeID = ""
		return
	}
	probs := e.hudProblemsForDoc(rec.doc, e.HUDLocale())
	if len(probs) > 0 && probs[0].NodeID != "" {
		e.hud.selectedNodeID = probs[0].NodeID
		return
	}
	if len(rec.doc.Nodes) > 0 {
		e.hud.selectedNodeID = rec.doc.Nodes[0].ID
	}
}

func (e *Editor) ensureHUDSelection() {
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		e.hud.selectedNodeID = ""
		return
	}
	if e.hud.selectedNodeID != "" && findHUDNode(rec.doc.Nodes, e.hud.selectedNodeID) != nil {
		return
	}
	e.selectFirstInterestingHUDNode()
}

func (e *Editor) syncHUDAfterMutate() {
	if e == nil {
		return
	}
	// If l10n workspace is open, mirror its live edits into HUD locale strings.
	if e.l10n != nil && e.hud.open {
		for _, loc := range e.l10n.locales {
			if !slices.Contains(e.hud.locales, loc) {
				e.hud.locales = append(e.hud.locales, loc)
			}
			if e.hud.localeStrings == nil {
				e.hud.localeStrings = map[string]map[string]string{}
			}
			if e.hud.localePlurals == nil {
				e.hud.localePlurals = map[string]map[string]l10n.PluralForms{}
			}
			e.hud.localeStrings[loc] = maps.Clone(e.l10n.strings[loc])
			e.hud.localePlurals[loc] = maps.Clone(e.l10n.plurals[loc])
		}
		slices.Sort(e.hud.locales)
		e.hud.localeRev++
	}
	e.ensureHUDSelection()
}

// hudStableFingerprint summarizes the HUD layout state rendered by the left rail,
// text-width table, and inspector so unrelated editor notifications (such as async
// project-wide problem rechecks or overlay toggles) do not needlessly remount those
// subtrees and drop in-flight pointer presses.
func (e *Editor) hudStableFingerprint() uint64 {
	if e == nil {
		return 0
	}
	h := fnv.New64a()
	var buf [8]byte
	writeU64 := func(v uint64) {
		binary.LittleEndian.PutUint64(buf[:], v)
		_, _ = h.Write(buf[:])
	}
	writeStr := func(s string) {
		writeU64(uint64(len(s)))
		_, _ = h.Write([]byte(s))
	}
	writeU64(e.hud.rev)
	writeU64(e.hud.session)
	writeU64(e.hud.localeRev)
	writeStr(e.hud.activePath)
	writeStr(e.hud.selectedNodeID)
	writeStr(e.hud.locale)
	writeU64(uint64(len(e.hud.locales)))
	for _, loc := range e.hud.locales {
		writeStr(loc)
	}
	writeU64(uint64(len(e.hud.screens)))
	for i := range e.hud.screens {
		rec := &e.hud.screens[i]
		writeStr(rec.path)
		writeU64(uint64(len(rec.raw)))
		_, _ = h.Write(rec.raw)
		if rec.doc != nil {
			writeU64(1)
		} else {
			writeU64(0)
		}
		if rec.loadErr != nil {
			writeStr(rec.loadErr.Error())
		} else {
			writeU64(0)
		}
	}
	return h.Sum64()
}

// HUDTree returns the flattened node tree for the active screen's NODES rail.
func (e *Editor) HUDTree() []HUDTreeRow {
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		return nil
	}
	probs := e.hudProblemsForDoc(rec.doc, e.HUDLocale())
	probIDs := make(map[string]bool, len(probs))
	for _, p := range probs {
		probIDs[p.NodeID] = true
	}

	rows := []HUDTreeRow{{
		ID:        "",
		Name:      rec.name,
		Depth:     0,
		AnchorTag: "screen",
	}}

	var walk func(list []gameui.ScreenNodeDoc, depth int, parentID string, parentIsBox bool)
	walk = func(list []gameui.ScreenNodeDoc, depth int, parentID string, parentIsBox bool) {
		for i := range list {
			n := &list[i]
			tag := ""
			if !parentIsBox {
				if n.Type != gameui.NodeContainer || n.Anchor != "" {
					tag = hudAnchorTag(n.EffectiveAnchor())
				}
			}
			rows = append(rows, HUDTreeRow{
				ID:         n.ID,
				Name:       n.ID,
				Type:       n.Type,
				Depth:      depth,
				AnchorTag:  tag,
				HasProblem: probIDs[n.ID],
				ParentID:   parentID,
				Index:      i,
				SiblingLen: len(list),
			})
			if len(n.Children) > 0 {
				isBox := n.Type == gameui.NodeVBox || n.Type == gameui.NodeHBox
				walk(n.Children, depth+1, n.ID, isBox)
			}
		}
	}
	walk(rec.doc.Nodes, 1, "", false)
	return rows
}

func hudAnchorTag(a gameui.ScreenAnchor) string {
	switch a {
	case gameui.AnchorTopLeft:
		return "TL"
	case gameui.AnchorTopCenter:
		return "TC"
	case gameui.AnchorTopRight:
		return "TR"
	case gameui.AnchorCenterLeft:
		return "CL"
	case gameui.AnchorCenter:
		return "C"
	case gameui.AnchorCenterRight:
		return "CR"
	case gameui.AnchorBottomLeft:
		return "BL"
	case gameui.AnchorBottomCenter:
		return "BC"
	case gameui.AnchorBottomRight:
		return "BR"
	case gameui.AnchorFill:
		return "FILL"
	default:
		return "TL"
	}
}

func hudAnchorWords(a gameui.ScreenAnchor) string {
	switch a {
	case gameui.AnchorTopLeft:
		return "Top left"
	case gameui.AnchorTopCenter:
		return "Top centre"
	case gameui.AnchorTopRight:
		return "Top right"
	case gameui.AnchorCenterLeft:
		return "Centre left"
	case gameui.AnchorCenter:
		return "Centre"
	case gameui.AnchorCenterRight:
		return "Centre right"
	case gameui.AnchorBottomLeft:
		return "Bottom left"
	case gameui.AnchorBottomCenter:
		return "Bottom centre"
	case gameui.AnchorBottomRight:
		return "Bottom right"
	case gameui.AnchorFill:
		return "Fills the whole screen"
	default:
		return "Top left"
	}
}

func hudNodeTypeLabel(n *gameui.ScreenNodeDoc) string {
	if n == nil {
		return ""
	}
	switch n.Type {
	case gameui.NodeVBox:
		return "VBox"
	case gameui.NodeHBox:
		return "HBox"
	case gameui.NodeLabel:
		return "Label"
	case gameui.NodeButton:
		return "Button"
	case gameui.NodeSpriteRow:
		return "Sprite row"
	case gameui.NodeColorRect:
		return "ColorRect"
	case gameui.NodeTouchStick:
		return "Touch stick"
	case gameui.NodeTouchButton:
		return "Touch button"
	case gameui.NodeContainer:
		if len(n.DeviceClasses) > 0 {
			parts := make([]string, len(n.DeviceClasses))
			for i, dc := range n.DeviceClasses {
				switch dc {
				case gameui.DevicePhone:
					parts[i] = "phones"
				case gameui.DeviceTablet:
					parts[i] = "tablets"
				case gameui.DeviceDesktop:
					parts[i] = "desktop"
				case gameui.DeviceUltrawide:
					parts[i] = "ultrawide"
				default:
					parts[i] = string(dc)
				}
			}
			return "Container · " + strings.Join(parts, ", ")
		}
		return "Container"
	default:
		return string(n.Type)
	}
}

// HUDTextWidths computes the TEXT WIDTH, PX table for every text-bearing node in the active screen across all project locales.
func (e *Editor) HUDTextWidths() []HUDTextWidthRow {
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		return nil
	}
	locales := e.HUDLocales()
	var rows []HUDTextWidthRow

	var walk func(list []gameui.ScreenNodeDoc)
	walk = func(list []gameui.ScreenNodeDoc) {
		for i := range list {
			n := &list[i]
			if n.TextKey != "" {
				fits := n.SizeMode == gameui.SizeFitText || n.Width <= 0
				boxLabel := "fits"
				if !fits {
					boxLabel = formatHUDPoints(n.Width)
				}
				widths := make(map[string]float32, len(locales))
				overflows := make(map[string]float32, len(locales))
				for _, loc := range locales {
					needed, over := e.hudMeasureNodeText(n, loc)
					widths[loc] = needed
					if over > 0.5 {
						overflows[loc] = over
					}
				}
				rows = append(rows, HUDTextWidthRow{
					NodeID:   n.ID,
					TextKey:  n.TextKey,
					BoxLabel: boxLabel,
					FitsText: fits,
					BoxWidth: n.Width,
					Widths:   widths,
					Overflow: overflows,
				})
			}
			if len(n.Children) > 0 {
				walk(n.Children)
			}
		}
	}
	walk(rec.doc.Nodes)
	return rows
}

func (e *Editor) hudMeasureNodeText(n *gameui.ScreenNodeDoc, locale string) (neededW, overflow float32) {
	if n == nil || n.TextKey == "" {
		return 0, 0
	}
	txt, ok := e.hudLookupText(locale, n.TextKey)
	if !ok {
		txt = n.TextKey
	}
	fontSize := n.FontSize
	if fontSize <= 0 {
		fontSize = gameui.DefaultScreenFontSize
	}
	tw, _ := gameui.MeasureScreenText(txt, fontSize)
	var padX float32
	if n.Padding != nil {
		padX = n.Padding.Left + n.Padding.Right
	} else if n.Type == gameui.NodeButton && n.Width == 0 {
		padX = 8
	}
	neededW = tw + padX
	if n.SizeMode != gameui.SizeFitText && n.Width > 0 {
		if over := neededW - n.Width; over > 0.5 {
			overflow = over
		}
	}
	return neededW, overflow
}

// HUDProblems returns the active screen's GAME CHECK diagnostics in the active language, backed by gameui.CheckScreen.
func (e *Editor) HUDProblems() []HUDProblem {
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		return nil
	}
	return e.hudProblemsForDoc(rec.doc, e.HUDLocale())
}

func (e *Editor) hudProblemsForDoc(doc *gameui.ScreenDoc, activeLocale string) []HUDProblem {
	if doc == nil {
		return nil
	}
	opts := gameui.ScreenCheckOptions{
		Locales:          e.hud.localeStrings,
		Plurals:          e.hud.localePlurals,
		FallbackBackdrop: specPanel,
	}
	rawIssues := gameui.CheckScreen(doc, opts)
	if len(rawIssues) == 0 {
		return nil
	}

	out := make([]HUDProblem, 0, len(rawIssues))
	for _, iss := range rawIssues {
		if iss.Locale != "" && activeLocale != "" && iss.Locale != activeLocale {
			continue
		}
		node := findHUDNode(doc.Nodes, iss.NodeID)
		prob := HUDProblem{
			Code:   iss.Code,
			NodeID: iss.NodeID,
			Locale: iss.Locale,
			Issue:  iss,
		}
		switch iss.Code {
		case gameui.IssueTextOverflow:
			_, over := e.hudMeasureNodeText(node, activeLocale)
			prob.OverPx = over
			prob.Title = iss.NodeID + " · text overflows"
			if node != nil && node.Width > 0 && over > 0 {
				sizeNote := "at every size"
				if len(iss.Devices) > 0 && len(iss.Devices) < 4 {
					sizeNote = "on " + strings.Join(iss.Devices, ", ")
				}
				prob.Detail = fmt.Sprintf("%s is %s px wider than its %s px box, %s", hudLocaleLabel(activeLocale), formatHUDPoints(over), formatHUDPoints(node.Width), sizeNote)
			} else {
				prob.Detail = iss.Message
			}
		case gameui.IssueTouchAreaTooSmall:
			hit := effectiveTouchHitSize(node)
			prob.Title = fmt.Sprintf("%s · touch area %s pt", iss.NodeID, formatHUDPoints(hit))
			prob.Detail = fmt.Sprintf("under the %s pt minimum on Phone and Tablet", formatHUDPoints(gameui.MinTouchTargetSize))
		case gameui.IssueLowContrast:
			ratio := hudNodeContrastRatio(doc, node)
			if ratio > 0 {
				prob.Title = fmt.Sprintf("%s · contrast %.1f : 1", iss.NodeID, ratio)
			} else {
				prob.Title = iss.NodeID + " · low contrast"
			}
			prob.Detail = "under 4.5 : 1 against its fill"
		case gameui.IssueMissingTextKey:
			prob.Title = iss.NodeID + " · missing translation"
			key := ""
			if node != nil {
				key = node.TextKey
			}
			prob.Detail = fmt.Sprintf("key %q is missing in %s", key, hudLocaleLabel(activeLocale))
		default:
			prob.Title = iss.NodeID
			prob.Detail = iss.Message
		}
		out = append(out, prob)
	}
	return out
}

func effectiveTouchHitSize(n *gameui.ScreenNodeDoc) float32 {
	if n == nil {
		return 0
	}
	if n.TouchHitSize > 0 {
		return n.TouchHitSize
	}
	if n.Width > 0 && n.Height > 0 {
		return min(n.Width, n.Height)
	}
	if n.Width > 0 {
		return n.Width
	}
	return n.Height
}

func hudNodeContrastRatio(doc *gameui.ScreenDoc, n *gameui.ScreenNodeDoc) float64 {
	if n == nil {
		return 0
	}
	th := gameui.DefaultTheme()
	fg := th.Text
	if n.TextColor != "" {
		if parsed, err := core.ParseColor(n.TextColor); err == nil {
			fg = parsed
		}
	}
	bg := specPanel
	if n.Background != "" {
		if parsed, err := core.ParseColor(n.Background); err == nil && parsed != core.Transparent {
			if parsed&0xff < 0xff {
				parsed = parsed.Over(specPanel)
			}
			bg = parsed
		}
	} else if n.Type == gameui.NodeButton {
		bg = th.Background
	}
	_ = doc
	return a11y.ContrastRatio(fg, bg)
}

// HUDLayoutFrames computes the four side-by-side device frames (Phone 19.5:9 with notch, Tablet 4:3,
// Desktop 16:9, Ultrawide 21:9) scaled to fit inside canvasW×canvasH, each built via gameui.BuildScreen
// in the active language.
func (e *Editor) HUDLayoutFrames(canvasW, canvasH float32) []HUDFrameLayout {
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		return nil
	}
	if !isFiniteFloat32(canvasW) || canvasW <= 0 {
		canvasW = 848
	}
	if !isFiniteFloat32(canvasH) || canvasH <= 0 {
		canvasH = 812
	}
	devices := gameui.ReferenceDevicesFor(rec.doc.DesignWidth, rec.doc.DesignHeight)
	if len(devices) < 4 {
		return nil
	}

	// Base artboard coordinate space matching docs/design/editor/HUDLayout.dc.html (800×600 content inside 848×812 viewport),
	// uniformly scaled to fit canvasW × canvasH while keeping the bottom-right slot open for the TEXT WIDTH, PX table.
	const (
		padLeft  float32 = 24
		padTop   float32 = 20
		baseW    float32 = 800
		baseH    float32 = 600
		rightPad float32 = 24
		botPad   float32 = 24
	)
	availW := max(float32(320), canvasW-padLeft-rightPad)
	availH := max(float32(280), canvasH-padTop-botPad)
	k := min(availW/baseW, availH/baseH)
	if k > 1.15 {
		k = 1.15
	}
	if k < 0.35 {
		k = 0.35
	}

	// Reference scales relative to 640×360 design space (adapted for arbitrary designHeight).
	designScale := float32(360) / max(float32(1), rec.doc.DesignHeight)
	type slot struct {
		sc    float32
		x, y  float32
		label string
		bezel float32
	}
	slots := []slot{
		{sc: 0.58 * designScale * k, x: padLeft + 0*k, y: padTop + 24*k, label: "Phone · 19.5 : 9 · notch", bezel: 8 * k},
		{sc: 0.50 * designScale * k, x: padLeft + 476*k, y: padTop + 24*k, label: "Tablet · 4 : 3", bezel: 4 * k},
		{sc: 0.70 * designScale * k, x: padLeft + 0*k, y: padTop + 320*k, label: "Desktop · 16 : 9", bezel: 4 * k},
		{sc: 0.39 * designScale * k, x: padLeft + 472*k, y: padTop + 320*k, label: "Ultrawide · 21 : 9", bezel: 4 * k},
	}

	loc := e.HUDLocale()
	textFn := func(key string) string {
		if v, ok := e.hudLookupText(loc, key); ok {
			return v
		}
		return key
	}

	frames := make([]HUDFrameLayout, 0, len(devices))
	for i, dev := range devices {
		if i >= len(slots) {
			break
		}
		sl := slots[i]
		built, err := gameui.BuildScreen(rec.doc, gameui.ScreenBuildOptions{
			SafeArea:       dev.SafeArea,
			DeviceClass:    dev.Class,
			ViewportWidth:  dev.Width,
			ViewportHeight: dev.Height,
			Text:           textFn,
		})
		if err == nil && built != nil {
			built.Layout(core.Bounds{
				Size: core.Size{Width: core.Points(dev.Width), Height: core.Points(dev.Height)},
			})
		}
		frames = append(frames, HUDFrameLayout{
			Device: dev,
			Label:  fmt.Sprintf("%s · %s × %s", sl.label, formatHUDPoints(dev.Width), formatHUDPoints(dev.Height)),
			Scale:  sl.sc,
			X:      sl.x,
			Y:      sl.y,
			W:      dev.Width * sl.sc,
			H:      dev.Height * sl.sc,
			Bezel:  sl.bezel,
			Built:  built,
		})
	}
	return frames
}

// --- Undoable Screen Mutations ---

type hudScreenCmd struct {
	ed            *Editor
	session       uint64
	path          string
	absPath       string
	label         string
	beforeDoc     *gameui.ScreenDoc
	afterDoc      *gameui.ScreenDoc
	beforeRaw     []byte
	afterRaw      []byte
	beforeSelect  string
	afterSelect   string
	writeDiskFile bool
}

func (c *hudScreenCmd) Label() string { return c.label }
func (c *hudScreenCmd) Do()           { _ = c.do() }
func (c *hudScreenCmd) Undo()         { _ = c.undo() }

func (c *hudScreenCmd) do() error {
	if c.ed == nil || c.ed.hud.session != c.session {
		return nil
	}
	if err := c.persist(c.afterRaw); err != nil {
		return err
	}
	c.applyState(c.afterDoc, c.afterRaw, c.afterSelect)
	return nil
}

func (c *hudScreenCmd) undo() error {
	if c.ed == nil || c.ed.hud.session != c.session {
		return nil
	}
	if err := c.persist(c.beforeRaw); err != nil {
		return err
	}
	c.applyState(c.beforeDoc, c.beforeRaw, c.beforeSelect)
	return nil
}

func (c *hudScreenCmd) persist(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if c.ed.OnSaveScreen != nil {
		if err := c.ed.OnSaveScreen(c.path, data); err != nil {
			return err
		}
	}
	if c.writeDiskFile && c.absPath != "" {
		if err := c.ed.checkHUDProjectSymlink(c.absPath, false); err != nil {
			return err
		}
		if err := writeFileAtomic(c.absPath, data); err != nil {
			return err
		}
	}
	return nil
}

func (c *hudScreenCmd) applyState(doc *gameui.ScreenDoc, raw []byte, sel string) {
	found := false
	for i := range c.ed.hud.screens {
		if c.ed.hud.screens[i].path == c.path && c.ed.hud.screens[i].absPath == c.absPath {
			c.ed.hud.screens[i].doc = doc.Clone()
			c.ed.hud.screens[i].raw = bytes.Clone(raw)
			c.ed.hud.screens[i].loadErr = nil
			found = true
			break
		}
	}
	if found {
		c.ed.hud.activePath = c.path
		c.ed.hud.selectedNodeID = sel
	}
	c.ed.hud.rev++
	if c.writeDiskFile && c.absPath != "" {
		c.ed.invalidateProjectIndex()
		c.ed.CheckProblems()
	}
}

func (e *Editor) commitHUDScreenEdit(label string, edit func(doc *gameui.ScreenDoc) error) error {
	if e == nil {
		return ErrNoHUDScreen
	}
	if e.playing {
		return ErrPlaying
	}
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		return ErrNoHUDScreen
	}
	beforeDoc := rec.doc.Clone()
	beforeRaw := bytes.Clone(rec.raw)
	if len(beforeRaw) == 0 {
		encoded, err := gameui.EncodeScreen(beforeDoc)
		if err != nil {
			return err
		}
		beforeRaw = encoded
	}
	afterDoc := beforeDoc.Clone()
	if err := edit(afterDoc); err != nil {
		return err
	}
	afterRaw, err := gameui.EncodeScreen(afterDoc)
	if err != nil {
		return err
	}
	if bytes.Equal(beforeRaw, afterRaw) {
		return nil
	}
	cmd := &hudScreenCmd{
		ed:            e,
		session:       e.hud.session,
		path:          rec.path,
		absPath:       rec.absPath,
		label:         label,
		beforeDoc:     beforeDoc,
		afterDoc:      afterDoc,
		beforeRaw:     beforeRaw,
		afterRaw:      afterRaw,
		beforeSelect:  e.hud.selectedNodeID,
		afterSelect:   e.hud.selectedNodeID,
		writeDiskFile: rec.absPath != "",
	}
	if err := e.History.execute(cmd); err != nil {
		return err
	}
	e.Status = label
	e.notify()
	return nil
}

// SetHUDNodeAnchor changes nodeID's anchor point (one of the 9 anchor positions or fill) in one undo step and saves the screen file.
func (e *Editor) SetHUDNodeAnchor(nodeID string, anchor gameui.ScreenAnchor) error {
	return e.commitHUDScreenEdit("Set anchor "+nodeID, func(doc *gameui.ScreenDoc) error {
		n := findHUDNode(doc.Nodes, nodeID)
		if n == nil {
			return fmt.Errorf("editor: HUD node %q not found", nodeID)
		}
		n.Anchor = anchor
		if anchor == gameui.AnchorFill {
			n.Offset = nil
		}
		return nil
	})
}

// SetHUDNodeSafeArea sets whether nodeID stays inside the display safe area in one undo step and saves the screen file.
func (e *Editor) SetHUDNodeSafeArea(nodeID string, keepInSafeArea bool) error {
	return e.commitHUDScreenEdit("Toggle safe area "+nodeID, func(doc *gameui.ScreenDoc) error {
		n := findHUDNode(doc.Nodes, nodeID)
		if n == nil {
			return fmt.Errorf("editor: HUD node %q not found", nodeID)
		}
		v := keepInSafeArea
		n.SafeArea = &v
		return nil
	})
}

// SetHUDNodeSizeMode toggles nodeID's sizing mode between fixed and fit_text in one undo step and saves the screen file.
func (e *Editor) SetHUDNodeSizeMode(nodeID string, mode gameui.SizeMode) error {
	return e.commitHUDScreenEdit("Set size mode "+nodeID, func(doc *gameui.ScreenDoc) error {
		n := findHUDNode(doc.Nodes, nodeID)
		if n == nil {
			return fmt.Errorf("editor: HUD node %q not found", nodeID)
		}
		n.SizeMode = mode
		return nil
	})
}

// SetHUDNodeOffset updates nodeID's anchor offset (x, y) in one undo step and saves the screen file.
func (e *Editor) SetHUDNodeOffset(nodeID string, x, y float32) error {
	return e.commitHUDScreenEdit("Move "+nodeID, func(doc *gameui.ScreenDoc) error {
		n := findHUDNode(doc.Nodes, nodeID)
		if n == nil {
			return fmt.Errorf("editor: HUD node %q not found", nodeID)
		}
		if x == 0 && y == 0 {
			n.Offset = nil
		} else {
			n.Offset = &gameui.ScreenOffsetDoc{X: x, Y: y}
		}
		return nil
	})
}

// SetHUDNodeSpacing updates the child spacing of a container (vbox, hbox, sprite_row) in one undo step and saves the screen file.
func (e *Editor) SetHUDNodeSpacing(nodeID string, spacing float32) error {
	if spacing < 0 {
		spacing = 0
	}
	return e.commitHUDScreenEdit("Set spacing "+nodeID, func(doc *gameui.ScreenDoc) error {
		n := findHUDNode(doc.Nodes, nodeID)
		if n == nil {
			return fmt.Errorf("editor: HUD node %q not found", nodeID)
		}
		n.Spacing = spacing
		return nil
	})
}

// ReorderHUDNode moves nodeID by delta (-1 for earlier, +1 for later) within its parent container in one undo step and saves the screen file.
func (e *Editor) ReorderHUDNode(nodeID string, delta int) error {
	if delta == 0 {
		return nil
	}
	return e.commitHUDScreenEdit("Reorder "+nodeID, func(doc *gameui.ScreenDoc) error {
		slicePtr, idx := findHUDNodeSlot(&doc.Nodes, nodeID)
		if slicePtr == nil || idx < 0 {
			return fmt.Errorf("editor: HUD node %q not found", nodeID)
		}
		list := *slicePtr
		target := idx + delta
		if target < 0 || target >= len(list) {
			return nil
		}
		list[idx], list[target] = list[target], list[idx]
		return nil
	})
}

// --- Hit Testing & Touch Control Dragging ---

func isHUDTouchDraggable(n *gameui.ScreenNodeDoc) bool {
	if n == nil {
		return false
	}
	return n.Type == gameui.NodeTouchButton || n.Type == gameui.NodeTouchStick || (n.Type == gameui.NodeButton && n.TouchHitSize > 0)
}

// HitTestHUDCanvas returns the topmost visible node ID and device profile ID under (canvasX, canvasY) on the multi-size canvas.
func (e *Editor) HitTestHUDCanvas(canvasX, canvasY, canvasW, canvasH float32) (nodeID, deviceID string) {
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		return "", ""
	}
	frames := e.HUDLayoutFrames(canvasW, canvasH)
	for i := range frames {
		fr := &frames[i]
		if fr.Built == nil {
			continue
		}
		if canvasX < fr.X || canvasX > fr.X+fr.W || canvasY < fr.Y || canvasY > fr.Y+fr.H {
			continue
		}
		vx := (canvasX - fr.X) / fr.Scale
		vy := (canvasY - fr.Y) / fr.Scale
		if hit := hitTestScreenNodes(rec.doc.Nodes, fr.Built, vx, vy); hit != "" {
			return hit, fr.Device.ID
		}
	}
	return "", ""
}

func hitTestScreenNodes(nodes []gameui.ScreenNodeDoc, built *gameui.BuiltScreen, vx, vy float32) string {
	// Walk in reverse draw order so topmost interactive nodes win.
	for i := len(nodes) - 1; i >= 0; i-- {
		n := &nodes[i]
		if len(n.Children) > 0 {
			if hit := hitTestScreenNodes(n.Children, built, vx, vy); hit != "" {
				return hit
			}
		}
		if n.Type == gameui.NodeColorRect && n.Anchor == gameui.AnchorFill {
			continue
		}
		if n.Type == gameui.NodeContainer {
			continue
		}
		w := built.Widget(n.ID)
		if w == nil {
			continue
		}
		b := w.Bounds()
		if b.Size.Width <= 0 || b.Size.Height <= 0 {
			continue
		}
		x := float32(b.Origin.X)
		y := float32(b.Origin.Y)
		bw := float32(b.Size.Width)
		bh := float32(b.Size.Height)
		if vx >= x && vx <= x+bw && vy >= y && vy <= y+bh {
			return n.ID
		}
	}
	return ""
}

// BeginHUDTouchDrag starts dragging a touch control under (canvasX, canvasY) on a touch device preview.
// It returns true when a touch control was grabbed.
func (e *Editor) BeginHUDTouchDrag(canvasX, canvasY, canvasW, canvasH float32) bool {
	if e == nil || e.playing {
		return false
	}
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		return false
	}
	frames := e.HUDLayoutFrames(canvasW, canvasH)
	for i := range frames {
		fr := &frames[i]
		if !fr.Device.Touch || fr.Built == nil {
			continue
		}
		if canvasX < fr.X || canvasX > fr.X+fr.W || canvasY < fr.Y || canvasY > fr.Y+fr.H {
			continue
		}
		vx := (canvasX - fr.X) / fr.Scale
		vy := (canvasY - fr.Y) / fr.Scale
		hit := hitTestScreenNodes(rec.doc.Nodes, fr.Built, vx, vy)
		if hit == "" {
			continue
		}
		node := findHUDNode(rec.doc.Nodes, hit)
		if !isHUDTouchDraggable(node) {
			return false
		}
		var orig gameui.ScreenOffsetDoc
		if node.Offset != nil {
			orig = *node.Offset
		}
		beforeRaw := bytes.Clone(rec.raw)
		if len(beforeRaw) == 0 {
			beforeRaw, _ = gameui.EncodeScreen(rec.doc)
		}
		e.hud.selectedNodeID = node.ID
		e.hud.drag = hudDragState{
			active:       true,
			nodeID:       node.ID,
			deviceID:     fr.Device.ID,
			scale:        fr.Scale,
			anchor:       node.EffectiveAnchor(),
			startCanvasX: canvasX,
			startCanvasY: canvasY,
			origOffset:   orig,
			beforeDoc:    rec.doc.Clone(),
			beforeRaw:    beforeRaw,
		}
		e.notify()
		return true
	}
	return false
}

// HUDTouchDragActive reports whether a touch control drag is currently in progress.
func (e *Editor) HUDTouchDragActive() bool {
	return e != nil && e.hud.drag.active
}

// UpdateHUDTouchDrag previews the dragged touch control's anchor-relative offset across all four device frames.
func (e *Editor) UpdateHUDTouchDrag(canvasX, canvasY float32) bool {
	if e == nil || e.playing || !e.hud.drag.active {
		return false
	}
	if !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		return false
	}
	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		e.hud.drag = hudDragState{}
		return false
	}
	sc := e.hud.drag.scale
	if sc <= 0 {
		sc = 1
	}
	dx := (canvasX - e.hud.drag.startCanvasX) / sc
	dy := (canvasY - e.hud.drag.startCanvasY) / sc

	// Positive X insets inward from right edge; positive Y insets inward from bottom edge.
	sx := float32(1)
	switch e.hud.drag.anchor {
	case gameui.AnchorTopRight, gameui.AnchorCenterRight, gameui.AnchorBottomRight:
		sx = -1
	}
	sy := float32(1)
	switch e.hud.drag.anchor {
	case gameui.AnchorBottomLeft, gameui.AnchorBottomCenter, gameui.AnchorBottomRight:
		sy = -1
	}

	newX := float32(math.Round(float64(e.hud.drag.origOffset.X + dx*sx)))
	newY := float32(math.Round(float64(e.hud.drag.origOffset.Y + dy*sy)))

	n := findHUDNode(rec.doc.Nodes, e.hud.drag.nodeID)
	if n == nil {
		return false
	}
	if newX == 0 && newY == 0 {
		n.Offset = nil
	} else {
		n.Offset = &gameui.ScreenOffsetDoc{X: newX, Y: newY}
	}
	e.hud.drag.moved = newX != e.hud.drag.origOffset.X || newY != e.hud.drag.origOffset.Y
	e.notify()
	return true
}

// EndHUDTouchDrag finishes an active touch control drag and writes the new offset to the screen file in one undo step.
func (e *Editor) EndHUDTouchDrag() error {
	if e == nil || !e.hud.drag.active {
		return nil
	}
	if e.playing {
		e.CancelHUDTouchDrag()
		return ErrPlaying
	}
	drag := e.hud.drag
	e.hud.drag = hudDragState{}

	rec := e.activeHUDScreenRecord()
	if rec == nil || rec.doc == nil {
		return nil
	}
	if !drag.moved {
		rec.doc = drag.beforeDoc.Clone()
		e.hud.rev++
		e.notify()
		return nil
	}
	afterDoc := rec.doc.Clone()
	afterRaw, err := gameui.EncodeScreen(afterDoc)
	if err != nil {
		rec.doc = drag.beforeDoc.Clone()
		e.hud.rev++
		e.notify()
		return err
	}
	// Restore before executing the command so History.execute applies the full diff cleanly.
	rec.doc = drag.beforeDoc.Clone()
	cmd := &hudScreenCmd{
		ed:            e,
		session:       e.hud.session,
		path:          rec.path,
		absPath:       rec.absPath,
		label:         "Move " + drag.nodeID,
		beforeDoc:     drag.beforeDoc,
		afterDoc:      afterDoc,
		beforeRaw:     drag.beforeRaw,
		afterRaw:      afterRaw,
		beforeSelect:  drag.nodeID,
		afterSelect:   drag.nodeID,
		writeDiskFile: rec.absPath != "",
	}
	if err := e.History.execute(cmd); err != nil {
		return err
	}
	e.Status = "Moved " + drag.nodeID
	e.notify()
	return nil
}

// CancelHUDTouchDrag aborts an active touch control drag and restores the node's original offset without recording undo history.
func (e *Editor) CancelHUDTouchDrag() {
	if e == nil || !e.hud.drag.active {
		return
	}
	drag := e.hud.drag
	e.hud.drag = hudDragState{}
	if rec := e.activeHUDScreenRecord(); rec != nil && drag.beforeDoc != nil {
		rec.doc = drag.beforeDoc.Clone()
	}
	e.hud.rev++
	e.notify()
}

// --- Agent Composer ---

// SubmitHUDPrompt records a prompt from the HUD layout composer (and invokes Editor.OnHUDPrompt when set).
func (e *Editor) SubmitHUDPrompt(prompt string) HUDPromptRequest {
	if e == nil {
		return HUDPromptRequest{}
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return HUDPromptRequest{}
	}
	rec := e.activeHUDScreenRecord()
	name := ""
	screenPath := ""
	if rec != nil {
		name = rec.name
		screenPath = rec.path
	}
	req := HUDPromptRequest{
		Prompt:     prompt,
		ScreenPath: screenPath,
		ScreenName: name,
		SelectedID: e.hud.selectedNodeID,
		Locale:     e.HUDLocale(),
		Problems:   e.HUDProblems(),
	}
	e.hud.lastPrompt = req
	e.Status = "Asked agent: " + prompt
	if e.OnHUDPrompt != nil {
		e.OnHUDPrompt(req)
	}
	e.notify()
	return req
}

// AskAgentToFixHUDProblems submits an agent prompt asking to fix all active GAME CHECK problems in the active screen and language.
func (e *Editor) AskAgentToFixHUDProblems() HUDPromptRequest {
	probs := e.HUDProblems()
	if len(probs) == 0 {
		return HUDPromptRequest{}
	}
	titles := make([]string, len(probs))
	for i, p := range probs {
		titles[i] = p.Title
	}
	noun := "problems"
	if len(probs) == 1 {
		noun = "problem"
	}
	prompt := fmt.Sprintf("Fix %d %s in %s (%s): %s",
		len(probs), noun, screenFileName(e.HUDActiveScreenPath(), ""), hudLocaleLabel(e.HUDLocale()), strings.Join(titles, "; "))
	return e.SubmitHUDPrompt(prompt)
}

// LastHUDPrompt returns the most recent prompt submitted from the HUD layout screen.
func (e *Editor) LastHUDPrompt() HUDPromptRequest {
	if e == nil {
		return HUDPromptRequest{}
	}
	return e.hud.lastPrompt
}

// --- Node Lookup Helpers ---

func findHUDNode(nodes []gameui.ScreenNodeDoc, id string) *gameui.ScreenNodeDoc {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
		if len(nodes[i].Children) > 0 {
			if found := findHUDNode(nodes[i].Children, id); found != nil {
				return found
			}
		}
	}
	return nil
}

func findHUDNodeSlot(nodes *[]gameui.ScreenNodeDoc, id string) (*[]gameui.ScreenNodeDoc, int) {
	list := *nodes
	for i := range list {
		if list[i].ID == id {
			return nodes, i
		}
		if len(list[i].Children) > 0 {
			if s, idx := findHUDNodeSlot(&list[i].Children, id); s != nil {
				return s, idx
			}
		}
	}
	return nil, -1
}

func formatHUDPoints(v float32) string {
	if !isFiniteFloat32(v) {
		return "0"
	}
	rounded := math.Round(float64(v)*10) / 10
	if rounded == 0 {
		rounded = 0
	}
	if rounded == math.Trunc(rounded) {
		return strconv.FormatFloat(rounded, 'f', 0, 64)
	}
	return strconv.FormatFloat(rounded, 'f', 1, 64)
}
