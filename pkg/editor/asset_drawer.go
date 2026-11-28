package editor

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tooling/refs"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	// maxAssetScanEntries caps project WalkDir entries inspected by the Asset Drawer so huge trees stay responsive.
	maxAssetScanEntries = 20000
	// maxAssetThumbBytes caps image file size read from disk for thumbnail decoding.
	maxAssetThumbBytes = 16 << 20
	// maxAssetThumbPixels caps decoded image pixel count (e.g. 4096x4096) to prevent decompression-bomb OOMs.
	maxAssetThumbPixels = 16_777_216
	// maxAssetAudioWaveBytes caps WAV file size read from disk for waveform extraction.
	maxAssetAudioWaveBytes = 16 << 20
	// maxAssetAudioPreviewBytes caps audio file size read into memory for right-panel preview playback.
	maxAssetAudioPreviewBytes = 32 << 20
	// maxAssetImportBytes caps source file size read by ImportAssetFiles and ReplaceMissingAsset.
	maxAssetImportBytes = 64 << 20
)

// LeftRailMode selects whether the editor's left rail shows the Scene tree or the Assets drawer browser.
type LeftRailMode int

const (
	// LeftRailScene shows the Scene tree, layers, and standard scene inspector.
	LeftRailScene LeftRailMode = iota
	// LeftRailAssets shows asset folders, SHOW ONLY filters, the bottom asset drawer, and the selected-asset inspector.
	LeftRailAssets
)

// AssetDrawerKind selects the kind tab in the asset drawer header.
type AssetDrawerKind string

const (
	AssetKindAll     AssetDrawerKind = "all"
	AssetKindSprites AssetDrawerKind = "sprites"
	AssetKindTiles   AssetDrawerKind = "tiles"
	AssetKindAudio   AssetDrawerKind = "audio"
	AssetKindFonts   AssetDrawerKind = "fonts"
)

// AssetStatusFilter selects an optional SHOW ONLY filter in the Assets left rail.
type AssetStatusFilter string

const (
	AssetFilterNone      AssetStatusFilter = ""
	AssetFilterUnused    AssetStatusFilter = "unused"
	AssetFilterMissing   AssetStatusFilter = "missing"
	AssetFilterGenerated AssetStatusFilter = "generated"
	AssetFilterProposed  AssetStatusFilter = "proposed"
)

// AssetSortMode selects how cards in the asset drawer are sorted.
type AssetSortMode int

const (
	// AssetSortRecent sorts by file modification time descending, breaking ties by path.
	AssetSortRecent AssetSortMode = iota
	// AssetSortName sorts alphabetically by display name, then path.
	AssetSortName
)

// AssetRefEntry summarizes one incoming reference to an asset in the right panel's USED BY list.
type AssetRefEntry struct {
	File string
	What string
	Line int
	Ref  refs.Ref
}

// AssetClipPreview describes one playable frame sequence for a sprite asset in the right panel.
type AssetClipPreview struct {
	Name    string
	FrameMS int
	Frames  []AssetEntry
}

// AssetDrawerItem describes one asset card in the drawer and its right-panel metadata.
type AssetDrawerItem struct {
	Path          string
	Name          string
	Folder        string
	Kind          AssetDrawerKind
	KindLabel     string
	Meta          string
	Dims          string
	Badge         string
	Unused        bool
	Missing       bool
	Generated     bool
	Proposed      bool
	ModTime       time.Time
	Atlas         string
	Frame         string
	Image         core.ImageSource
	Rect          sprite.Frame
	ImageWidth    int
	ImageHeight   int
	Clips         []AssetClipPreview
	AudioWave     []float32
	AudioDuration time.Duration
	AudioRate     int
	AudioChannels int
	FontSample    string
	UsedBy        []AssetRefEntry
	UseCount      int
}

// AssetFolderEntry describes one folder row in the Assets left rail's FOLDERS list.
type AssetFolderEntry struct {
	Path  string // "" for "All assets"
	Label string
	Depth int
	Count int
}

// AssetDrawerCounts summarizes the SHOW ONLY counts across the full asset catalog.
type AssetDrawerCounts struct {
	Unused    int
	Missing   int
	Generated int
	Proposed  int
}

// AssetDrawerSnapshot is the immutable view model rendered by the Assets left rail, drawer, and right panel.
type AssetDrawerSnapshot struct {
	AllItems      []AssetDrawerItem
	VisibleItems  []AssetDrawerItem
	Folders       []AssetFolderEntry
	Counts        AssetDrawerCounts
	MissingNames  []string
	Selected      *AssetDrawerItem
	EmptyFolder   string
	ActiveFolder  string
	ActiveStatus  AssetStatusFilter
	ActiveKind    AssetDrawerKind
	Query         string
	Sort          AssetSortMode
	ProposedCount int
}

// AssetDrawerDrag is the drag payload carried from an asset card to the 2D viewport.
type AssetDrawerDrag struct {
	Path  string
	Name  string
	Atlas string
	Frame string
	Image core.ImageSource
	Rect  sprite.Frame
}

// AssetDragState describes an active sprite drag from the asset drawer over the 2D viewport.
type AssetDragState struct {
	Active           bool
	HoveringViewport bool
	Drag             AssetDrawerDrag
	CanvasX, CanvasY float32
	LayerX, LayerY   float32
	SnapPx           float32
	TargetObjectID   string
	TargetObjectName string
	PillName         string
	PillDetail       string
}

// assetDrawerState holds all runtime UI state for the Asset Drawer (issue #1052).
type assetDrawerState struct {
	mode         LeftRailMode
	folder       string
	status       AssetStatusFilter
	kind         AssetDrawerKind
	query        string
	sort         AssetSortMode
	selectedPath string
	proposed     map[string]bool
	proposedNote string

	// Preview playback state (sprite clips frame-by-frame + audio playback).
	previewPlaying   bool
	previewClip      string
	previewFrame     int
	previewAccum     time.Duration
	previewClipStop  func()
	audioPlayingPath string
	audioMusic       *sound.Music
	audioMixer       *sound.Mixer
	audioMixerScope  *core.Scope

	// Drag-to-viewport state.
	drag AssetDragState

	// Catalog, view, preview, and metadata caches.
	drawerRev  uint64
	viewRev    uint64
	previewRev uint64
	cacheKey   string
	cachedAll  []AssetDrawerItem
	cachedDir  []string
	thumbMap   map[string]decodedThumb
	audioCache map[string]decodedAudioMeta

	// Cached InspectAssetImport summary for AssetInspectorView (avoids per-rev disk I/O).
	importCacheKey  string
	importCacheInfo AssetImportInfo
	importCacheOK   bool
	importDiskReads uint64
}

type decodedThumb struct {
	modTime time.Time
	size    int64
	img     core.ImageSource
	w, h    int
}

type decodedAudioMeta struct {
	modTime  time.Time
	size     int64
	filesRev uint64
	lang     string
	wave     []float32
	dur      time.Duration
	rate     int
	channels int
	dims     string
	prefix   string
}

type assetDrawerRailKey struct {
	projectPath string
	filesRev    uint64
	assetsRev   uint64
	drawerRev   uint64
	scenesRev   uint64
	viewRev     uint64
	hasProject  bool
	lang        string
}

type assetDrawerInspectorKey struct {
	rail      assetDrawerRailKey
	importRev uint64
}

type assetDrawerPreviewKey struct {
	rail             assetDrawerRailKey
	previewRev       uint64
	audioPlayingPath string
}

func (e *Editor) assetDrawerRailKey() assetDrawerRailKey {
	if e == nil {
		return assetDrawerRailKey{}
	}
	return assetDrawerRailKey{
		projectPath: e.ProjectPath,
		filesRev:    e.filesRev,
		assetsRev:   e.assetsRev,
		drawerRev:   e.assetDrawer.drawerRev,
		scenesRev:   e.scenesRev,
		viewRev:     e.assetDrawer.viewRev,
		hasProject:  e.Project != nil,
		lang:        e.ResolvedLanguage(),
	}
}

func (e *Editor) assetDrawerInspectorKey() assetDrawerInspectorKey {
	if e == nil {
		return assetDrawerInspectorKey{}
	}
	return assetDrawerInspectorKey{
		rail:      e.assetDrawerRailKey(),
		importRev: e.importRev,
	}
}

func (e *Editor) assetDrawerPreviewKey() assetDrawerPreviewKey {
	if e == nil {
		return assetDrawerPreviewKey{}
	}
	return assetDrawerPreviewKey{
		rail:             e.assetDrawerRailKey(),
		previewRev:       e.assetDrawer.previewRev,
		audioPlayingPath: e.assetDrawer.audioPlayingPath,
	}
}

func (e *Editor) cachedAssetImportSummary(relPath string) (AssetImportInfo, bool) {
	if e == nil || e.Project == nil || e.ProjectPath == "" || relPath == "" {
		return AssetImportInfo{}, false
	}
	key := fmt.Sprintf("%s\x00%s\x00%d\x00%d", e.ProjectPath, relPath, e.filesRev, e.importRev)
	if e.assetDrawer.importCacheKey == key {
		return e.assetDrawer.importCacheInfo, e.assetDrawer.importCacheOK
	}
	e.assetDrawer.importDiskReads++
	info, err := e.InspectAssetImport(relPath)
	ok := err == nil && info.Kind != AssetImportNone
	e.assetDrawer.importCacheKey = key
	e.assetDrawer.importCacheInfo = info
	e.assetDrawer.importCacheOK = ok
	return info, ok
}

func (e *Editor) resetAssetDrawerState() {
	if e == nil {
		return
	}
	e.stopAssetPreviewLoop()
	e.stopAssetAudioPreview()
	e.assetDrawer.drag = AssetDragState{}
	e.assetDrawer.folder = ""
	e.assetDrawer.status = AssetFilterNone
	e.assetDrawer.query = ""
	e.assetDrawer.selectedPath = ""
	e.assetDrawer.proposed = nil
	e.assetDrawer.proposedNote = ""
	e.assetDrawer.previewPlaying = false
	e.assetDrawer.previewClip = ""
	e.assetDrawer.previewFrame = 0
	e.assetDrawer.previewAccum = 0
	e.assetDrawer.cacheKey = ""
	e.assetDrawer.cachedAll = nil
	e.assetDrawer.cachedDir = nil
	e.assetDrawer.thumbMap = nil
	e.assetDrawer.audioCache = nil
	e.assetDrawer.importCacheKey = ""
	e.assetDrawer.importCacheInfo = AssetImportInfo{}
	e.assetDrawer.importCacheOK = false
	e.assetDrawer.drawerRev++
	e.assetDrawer.viewRev++
	e.assetDrawer.previewRev++
}

func normalizeAssetDrawerFolder(folder string) (string, bool) {
	trimmed := strings.TrimSpace(folder)
	if trimmed == "" || trimmed == "." {
		return "", true
	}
	if filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "\\") {
		return "", false
	}
	slashed := strings.TrimRight(strings.ReplaceAll(trimmed, "\\", "/"), "/")
	if slashed == "" || slashed == "." {
		return "", true
	}
	if len(slashed) >= 2 && slashed[1] == ':' {
		return "", false
	}
	for _, seg := range strings.Split(slashed, "/") {
		if seg == ".." {
			return "", false
		}
	}
	clean := path.Clean(slashed)
	if clean == "." {
		return "", true
	}
	if !fs.ValidPath(clean) {
		return "", false
	}
	return clean, true
}

// LeftRailMode returns whether the editor's left rail is in Scene mode or Assets mode.
func (e *Editor) LeftRailMode() LeftRailMode {
	if e == nil {
		return LeftRailScene
	}
	return e.assetDrawer.mode
}

// SetLeftRailMode switches the left rail between Scene mode and Assets mode.
func (e *Editor) SetLeftRailMode(mode LeftRailMode) {
	if e == nil || e.assetDrawer.mode == mode {
		return
	}
	e.assetDrawer.mode = mode
	if mode != LeftRailAssets {
		e.stopAssetPreviewLoop()
		e.assetDrawer.previewPlaying = false
		e.stopAssetAudioPreview()
		e.CancelAssetDrag()
	} else if e.assetDrawer.kind == "" {
		e.assetDrawer.kind = AssetKindAll
	}
	e.assetDrawer.viewRev++
	e.notify()
}

// AssetDrawerFolder returns the currently selected folder filter ("" for "All assets").
func (e *Editor) AssetDrawerFolder() string {
	if e == nil {
		return ""
	}
	return e.assetDrawer.folder
}

// SetAssetDrawerFolder selects a folder in the Assets left rail ("" for "All assets")
// and clears any active SHOW ONLY status filter, matching Assets.dc.html.
// Invalid, absolute, or parent-traversing paths are ignored.
func (e *Editor) SetAssetDrawerFolder(folder string) {
	if e == nil {
		return
	}
	clean, ok := normalizeAssetDrawerFolder(folder)
	if !ok {
		return
	}
	if e.assetDrawer.folder == clean && e.assetDrawer.status == AssetFilterNone {
		return
	}
	e.assetDrawer.folder = clean
	e.assetDrawer.status = AssetFilterNone
	e.assetDrawer.viewRev++
	e.notify()
}

// AssetDrawerStatusFilter returns the active SHOW ONLY filter.
func (e *Editor) AssetDrawerStatusFilter() AssetStatusFilter {
	if e == nil {
		return AssetFilterNone
	}
	return e.assetDrawer.status
}

// SetAssetDrawerStatusFilter sets or toggles a SHOW ONLY filter in the Assets left rail.
func (e *Editor) SetAssetDrawerStatusFilter(filter AssetStatusFilter) {
	if e == nil || e.assetDrawer.status == filter {
		return
	}
	e.assetDrawer.status = filter
	e.assetDrawer.viewRev++
	e.notify()
}

// ToggleAssetDrawerStatusFilter toggles filter on or off.
func (e *Editor) ToggleAssetDrawerStatusFilter(filter AssetStatusFilter) {
	if e == nil {
		return
	}
	if e.assetDrawer.status == filter {
		e.SetAssetDrawerStatusFilter(AssetFilterNone)
		return
	}
	e.SetAssetDrawerStatusFilter(filter)
}

// AssetDrawerKind returns the active kind tab in the asset drawer.
func (e *Editor) AssetDrawerKind() AssetDrawerKind {
	if e == nil || e.assetDrawer.kind == "" {
		return AssetKindAll
	}
	return e.assetDrawer.kind
}

// SetAssetDrawerKind sets the kind tab filter in the asset drawer.
func (e *Editor) SetAssetDrawerKind(kind AssetDrawerKind) {
	if e == nil {
		return
	}
	if kind == "" {
		kind = AssetKindAll
	}
	if e.assetDrawer.kind == kind {
		return
	}
	e.assetDrawer.kind = kind
	e.assetDrawer.viewRev++
	e.notify()
}

// AssetDrawerQuery returns the name/path filter text in the asset drawer.
func (e *Editor) AssetDrawerQuery() string {
	if e == nil {
		return ""
	}
	return e.assetDrawer.query
}

// SetAssetDrawerQuery sets the name/path filter text in the asset drawer.
func (e *Editor) SetAssetDrawerQuery(query string) {
	if e == nil || e.assetDrawer.query == query {
		return
	}
	e.assetDrawer.query = query
	e.assetDrawer.viewRev++
	e.notify()
}

// AssetDrawerSort returns the current asset drawer sort mode.
func (e *Editor) AssetDrawerSort() AssetSortMode {
	if e == nil {
		return AssetSortRecent
	}
	return e.assetDrawer.sort
}

// SetAssetDrawerSort sets the asset drawer sort mode.
func (e *Editor) SetAssetDrawerSort(sort AssetSortMode) {
	if e == nil || e.assetDrawer.sort == sort {
		return
	}
	e.assetDrawer.sort = sort
	e.assetDrawer.viewRev++
	e.notify()
}

// ClearAssetDrawerFilters resets kind, query, status, and folder filters so all assets show.
func (e *Editor) ClearAssetDrawerFilters() {
	if e == nil {
		return
	}
	e.assetDrawer.folder = ""
	e.assetDrawer.status = AssetFilterNone
	e.assetDrawer.kind = AssetKindAll
	e.assetDrawer.query = ""
	e.assetDrawer.viewRev++
	e.notify()
}

// SelectedAssetPath returns the currently selected asset path in the asset drawer.
func (e *Editor) SelectedAssetPath() string {
	if e == nil {
		return ""
	}
	return e.assetDrawer.selectedPath
}

// SelectAssetPath selects the asset at relPath in the asset drawer and resets frame preview state.
func (e *Editor) SelectAssetPath(relPath string) {
	if e == nil {
		return
	}
	clean := strings.TrimPrefix(path.Clean(strings.ReplaceAll(relPath, "\\", "/")), "./")
	if clean == "." {
		clean = ""
	}
	if e.assetDrawer.selectedPath == clean {
		return
	}
	e.stopAssetAudioPreview()
	if e.assetDrawer.previewPlaying {
		e.assetDrawer.previewPlaying = false
		e.stopAssetPreviewLoop()
	}
	e.assetDrawer.selectedPath = clean
	e.assetDrawer.previewClip = ""
	e.assetDrawer.previewFrame = 0
	if clean != "" {
		e.SelectedFile = clean
		e.selectedFileRev++
		e.Status = e.T("assetDrawer.statusSelected", l10n.Param("path", clean))
	}
	e.assetDrawer.viewRev++
	e.assetDrawer.previewRev++
	e.notify()
}

// RefreshAssetDrawer invalidates the cached asset drawer scan and forces a fresh rescan from disk and refs.
func (e *Editor) RefreshAssetDrawer() {
	if e == nil {
		return
	}
	e.assetDrawer.drawerRev++
	e.assetDrawer.viewRev++
	e.assetDrawer.cacheKey = ""
	e.assetDrawer.importCacheKey = ""
	e.invalidateProjectIndex()
	e.notify()
}

// ProposeAssetImports marks the given project-relative asset paths as newly proposed imports awaiting review.
func (e *Editor) ProposeAssetImports(paths []string) {
	if e == nil {
		return
	}
	if e.assetDrawer.proposed == nil {
		e.assetDrawer.proposed = make(map[string]bool)
	}
	for _, p := range paths {
		clean := strings.TrimPrefix(path.Clean(strings.ReplaceAll(p, "\\", "/")), "./")
		if clean != "" && clean != "." {
			e.assetDrawer.proposed[clean] = true
		}
	}
	e.assetDrawer.proposedNote = ""
	e.assetDrawer.drawerRev++
	e.assetDrawer.viewRev++
	e.assetDrawer.cacheKey = ""
	e.notify()
}

// KeepProposedAssets approves all pending proposed imports, clearing their "new" / Proposed badge.
func (e *Editor) KeepProposedAssets() {
	if e == nil || len(e.assetDrawer.proposed) == 0 {
		return
	}
	n := len(e.assetDrawer.proposed)
	clear(e.assetDrawer.proposed)
	e.assetDrawer.proposedNote = e.Plural("assetDrawer.statusKeptProposed", n)
	e.Status = e.assetDrawer.proposedNote
	e.assetDrawer.drawerRev++
	e.assetDrawer.viewRev++
	e.assetDrawer.cacheKey = ""
	e.notify()
}

// RevertProposedAssets clears pending proposed imports from review state.
func (e *Editor) RevertProposedAssets() {
	if e == nil || len(e.assetDrawer.proposed) == 0 {
		return
	}
	clear(e.assetDrawer.proposed)
	e.assetDrawer.proposedNote = e.T("assetDrawer.statusProposalDismissed")
	e.Status = e.assetDrawer.proposedNote
	e.assetDrawer.drawerRev++
	e.assetDrawer.viewRev++
	e.assetDrawer.cacheKey = ""
	e.notify()
}

// AssetDrawerSnapshot returns the current filtered, sorted asset catalog, folder counts,
// SHOW ONLY counts, missing-file problem list, and selected asset metadata.
func (e *Editor) AssetDrawerSnapshot() AssetDrawerSnapshot {
	if e == nil {
		return AssetDrawerSnapshot{ActiveKind: AssetKindAll}
	}
	all, dirs := e.ensureAssetDrawerCatalog()

	// Folder counts across all items.
	folders := buildAssetFolders(e, all, dirs)

	// SHOW ONLY counts across all items.
	var counts AssetDrawerCounts
	var missingNames []string
	for _, it := range all {
		if it.Unused {
			counts.Unused++
		}
		if it.Missing {
			counts.Missing++
			missingNames = append(missingNames, it.Name)
		}
		if it.Generated {
			counts.Generated++
		}
		if it.Proposed {
			counts.Proposed++
		}
	}

	kind := e.AssetDrawerKind()
	status := e.assetDrawer.status
	folder := e.assetDrawer.folder
	query := strings.TrimSpace(strings.ToLower(e.assetDrawer.query))

	// Check if the selected folder itself has zero assets before applying other filters.
	folderHasAny := folder == ""
	if folder != "" {
		for _, it := range all {
			if assetInFolder(it.Path, folder) {
				folderHasAny = true
				break
			}
		}
	}

	visible := make([]AssetDrawerItem, 0, len(all))
	for _, it := range all {
		if folder != "" && !assetInFolder(it.Path, folder) {
			continue
		}
		if kind != AssetKindAll && it.Kind != kind {
			continue
		}
		switch status {
		case AssetFilterUnused:
			if !it.Unused {
				continue
			}
		case AssetFilterMissing:
			if !it.Missing {
				continue
			}
		case AssetFilterGenerated:
			if !it.Generated {
				continue
			}
		case AssetFilterProposed:
			if !it.Proposed {
				continue
			}
		}
		if query != "" && !strings.Contains(strings.ToLower(it.Name), query) && !strings.Contains(strings.ToLower(it.Path), query) {
			continue
		}
		visible = append(visible, it)
	}

	sortAssetDrawerItems(visible, e.assetDrawer.sort)

	var sel *AssetDrawerItem
	if e.assetDrawer.selectedPath != "" {
		for i := range all {
			if all[i].Path == e.assetDrawer.selectedPath {
				cp := all[i]
				sel = &cp
				break
			}
		}
	}
	if sel == nil && len(visible) > 0 {
		cp := visible[0]
		sel = &cp
	} else if sel == nil && len(all) > 0 {
		cp := all[0]
		sel = &cp
	}

	emptyFolder := ""
	if folder != "" && !folderHasAny && status == AssetFilterNone && kind == AssetKindAll && query == "" {
		emptyFolder = folder
	}

	return AssetDrawerSnapshot{
		AllItems:      all,
		VisibleItems:  visible,
		Folders:       folders,
		Counts:        counts,
		MissingNames:  missingNames,
		Selected:      sel,
		EmptyFolder:   emptyFolder,
		ActiveFolder:  folder,
		ActiveStatus:  status,
		ActiveKind:    kind,
		Query:         e.assetDrawer.query,
		Sort:          e.assetDrawer.sort,
		ProposedCount: counts.Proposed,
	}
}

func (e *Editor) ensureAssetDrawerCatalog() ([]AssetDrawerItem, []string) {
	key := fmt.Sprintf("%s\x00%d\x00%d\x00%d\x00%d\x00%s", e.ProjectPath, e.filesRev, e.assetsRev, e.assetDrawer.drawerRev, e.scenesRev, e.ResolvedLanguage())
	if e.Project == nil || e.ProjectPath == "" {
		// Standalone mode without a project directory: list resolver atlas frames.
		return e.buildResolverOnlyAssets(), nil
	}
	if e.assetDrawer.cacheKey == key && e.assetDrawer.cachedAll != nil {
		return e.assetDrawer.cachedAll, e.assetDrawer.cachedDir
	}

	root := filepath.Dir(e.ProjectPath)
	fsys := os.DirFS(root)
	scan, _ := refs.Scan(fsys)

	unusedSet := make(map[string]bool)
	missingSet := make(map[string]bool)
	tilesetImages := make(map[string]bool)
	outputSet := make(map[string]bool)
	var outputDirs []string
	outputRoot := false
	if scan != nil {
		for _, p := range scan.UnreferencedAssets() {
			unusedSet[p] = true
		}
		for _, p := range scan.MissingAssets() {
			missingSet[p] = true
		}
		for _, r := range scan.Refs {
			if r.Field == refs.FieldTilesetImage && r.Target != "" {
				tilesetImages[r.Target] = true
			}
			if r.Output && r.Target != "" {
				if r.Dir {
					if r.Target == "." {
						outputRoot = true
					} else {
						outputDirs = append(outputDirs, r.Target+"/")
					}
				} else {
					outputSet[r.Target] = true
				}
			}
		}
	}

	// Collect all existing files (with modTimes) and directories within project root, capped for safety.
	var dirs []string
	diskAssets := make(map[string]os.FileInfo)
	sourceSidecars := make(map[string]bool)
	scannedEntries := 0
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			return nil
		}
		if p == "." {
			return nil
		}
		scannedEntries++
		if scannedEntries > maxAssetScanEntries {
			return fs.SkipAll
		}
		absEntry := filepath.Join(root, filepath.FromSlash(p))
		if d.IsDir() {
			if skippedProjectDir(d.Name()) || verifyFileWithinRoot(root, absEntry, p) != nil {
				return fs.SkipDir
			}
			dirs = append(dirs, p)
			return nil
		}
		if verifyFileWithinRoot(root, absEntry, p) != nil {
			return nil
		}
		lower := strings.ToLower(p)
		if strings.HasSuffix(lower, ".source.yaml") {
			sourceSidecars[p[:len(p)-len(".source.yaml")]] = true
			return nil
		}
		if strings.HasSuffix(lower, ".source.yml") {
			sourceSidecars[p[:len(p)-len(".source.yml")]] = true
			return nil
		}
		if !scan.IsAsset(p) {
			return nil
		}
		if outputRoot || outputSet[p] {
			return nil
		}
		for _, pref := range outputDirs {
			if strings.HasPrefix(p, pref) {
				return nil
			}
		}
		if info, serr := d.Info(); serr == nil {
			diskAssets[p] = info
		}
		return nil
	})

	// Evict removed/renamed paths from thumbnail and audio metadata caches so memory stays bounded.
	for k := range e.assetDrawer.thumbMap {
		if _, exists := diskAssets[k]; !exists {
			delete(e.assetDrawer.thumbMap, k)
		}
	}
	for k := range e.assetDrawer.audioCache {
		if _, exists := diskAssets[k]; !exists {
			delete(e.assetDrawer.audioCache, k)
		}
	}

	// Index available atlas entries from Resolver so sprite assets know their placeable (Atlas, Frame) and clip frames.
	atlasEntries := e.Assets()
	entriesByAtlas := make(map[string][]AssetEntry)
	for _, ae := range atlasEntries {
		entriesByAtlas[ae.Atlas] = append(entriesByAtlas[ae.Atlas], ae)
	}

	// Also count scene placements per sprite/frame across the open scene for rich usage counts.
	placementCounts := countScenePlacements(e.Doc)

	allPaths := make([]string, 0, len(diskAssets)+len(missingSet))
	for p := range diskAssets {
		allPaths = append(allPaths, p)
	}
	for p := range missingSet {
		if _, exists := diskAssets[p]; !exists {
			allPaths = append(allPaths, p)
		}
	}
	slices.Sort(allPaths)

	items := make([]AssetDrawerItem, 0, len(allPaths))
	for _, rel := range allPaths {
		info := diskAssets[rel]
		isMissing := missingSet[rel] && info == nil
		isUnused := unusedSet[rel]
		isGenerated := sourceSidecars[rel]
		isProposed := e.assetDrawer.proposed[rel]

		kind, kindLabel := e.classifyAssetPath(rel, tilesetImages[rel])
		folder := path.Dir(rel)
		if folder == "." {
			folder = ""
		}

		var usedBy []AssetRefEntry
		if scan != nil {
			rawRefs := scan.UsedBy(rel)
			usedBy = summarizeAssetRefs(rawRefs)
		}

		item := AssetDrawerItem{
			Path:      rel,
			Name:      path.Base(rel),
			Folder:    folder,
			Kind:      kind,
			KindLabel: kindLabel,
			Unused:    isUnused,
			Missing:   isMissing,
			Generated: isGenerated,
			Proposed:  isProposed,
			UsedBy:    usedBy,
			UseCount:  len(usedBy),
		}
		if info != nil {
			item.ModTime = info.ModTime()
		}

		// Badge priority: Missing > new (Proposed) > Generated > Not used.
		switch {
		case item.Missing:
			item.Badge = e.T("assetDrawer.badgeMissing")
		case item.Proposed:
			item.Badge = e.T("assetDrawer.badgeNew")
		case item.Generated:
			item.Badge = e.T("assetDrawer.badgeGenerated")
		case item.Unused:
			item.Badge = e.T("assetDrawer.badgeNotUsed")
		}

		absPath := filepath.Join(root, filepath.FromSlash(rel))
		if item.Missing {
			item.Dims = e.T("assetDrawer.dimsNotOnDisk")
			if item.UseCount > 0 {
				item.Meta = e.T("assetDrawer.metaMissingUsed", l10n.Param("count", item.UseCount))
			} else {
				item.Meta = e.T("assetDrawer.badgeMissing")
			}
		} else {
			switch kind {
			case AssetKindSprites, AssetKindTiles:
				e.populateImageAssetItem(&item, root, absPath, info, atlasEntries, entriesByAtlas, placementCounts)
			case AssetKindAudio:
				e.populateAudioAssetItem(&item, root, absPath, info)
			case AssetKindFonts:
				populateFontAssetItem(e, &item)
			default:
				item.Meta = formatAssetMeta(e, "", item.UseCount, item.Unused, item.Generated)
			}
		}

		items = append(items, item)
	}

	e.assetDrawer.cacheKey = key
	e.assetDrawer.cachedAll = items
	e.assetDrawer.cachedDir = dirs
	return items, dirs
}

func (e *Editor) buildResolverOnlyAssets() []AssetDrawerItem {
	entries := e.Assets()
	if len(entries) == 0 {
		return nil
	}
	placementCounts := countScenePlacements(e.Doc)
	items := make([]AssetDrawerItem, 0, len(entries))
	for _, ae := range entries {
		p := ae.Atlas + "/" + ae.Frame
		uses := placementCounts[ae.Key()]
		unused := uses == 0
		badge := ""
		if unused {
			badge = e.T("assetDrawer.badgeNotUsed")
		}
		meta := formatAssetMeta(e, "", uses, unused, false)
		dims := fmt.Sprintf("%.0f × %.0f", ae.Rect.W, ae.Rect.H)
		items = append(items, AssetDrawerItem{
			Path:        p,
			Name:        ae.Frame,
			Folder:      ae.Atlas,
			Kind:        AssetKindSprites,
			KindLabel:   e.T("assetDrawer.kindLabelSprite"),
			Meta:        meta,
			Dims:        dims,
			Badge:       badge,
			Unused:      unused,
			Atlas:       ae.Atlas,
			Frame:       ae.Frame,
			Image:       ae.Image,
			Rect:        ae.Rect,
			ImageWidth:  int(ae.Rect.W),
			ImageHeight: int(ae.Rect.H),
			UseCount:    uses,
		})
	}
	return items
}

func hasImageSource(img core.ImageSource) bool {
	return img.Pixels != nil || img.URI != "" || len(img.Data) > 0
}

func (e *Editor) populateImageAssetItem(
	item *AssetDrawerItem,
	root string,
	absPath string,
	info os.FileInfo,
	atlasEntries []AssetEntry,
	entriesByAtlas map[string][]AssetEntry,
	placementCounts map[string]int,
) {
	// Match against resolved atlas frames if available.
	stem := strings.TrimSuffix(item.Name, path.Ext(item.Name))
	var matched []AssetEntry
	for _, ae := range atlasEntries {
		if ae.Frame == item.Name || ae.Frame == stem || ae.Frame == item.Path ||
			strings.HasPrefix(ae.Frame, stem+"_") || strings.HasPrefix(ae.Frame, stem+"/") || strings.HasPrefix(ae.Frame, stem+".") {
			matched = append(matched, ae)
		}
	}
	if len(matched) == 0 {
		if byA := entriesByAtlas[stem]; len(byA) > 0 {
			matched = byA
		}
	}
	if len(matched) > 0 {
		first := matched[0]
		item.Atlas = first.Atlas
		item.Frame = first.Frame
		item.Image = first.Image
		item.Rect = first.Rect
		item.ImageWidth = int(first.Rect.W)
		item.ImageHeight = int(first.Rect.H)
		if len(matched) > 1 {
			item.Clips = groupAtlasFramesIntoClips(stem, matched)
		}
		// Include live scene placement count when higher than static refs count.
		if uses := placementCounts[first.Key()]; uses > item.UseCount {
			item.UseCount = uses
		}
	}

	// Decode thumbnail directly from disk when no atlas frame supplied pixels.
	if !hasImageSource(item.Image) && info != nil {
		if thumb, ok := e.loadDiskThumb(root, item.Path, absPath, info); ok {
			item.Image = thumb.img
			item.Rect = sprite.Frame{W: float32(thumb.w), H: float32(thumb.h)}
			item.ImageWidth = thumb.w
			item.ImageHeight = thumb.h
		}
	}

	// If the asset has an import sidecar slice grid and no atlas clips yet, build playable slice frames.
	if len(item.Clips) == 0 && item.Image.Pixels != nil && item.ImageWidth > 0 && item.ImageHeight > 0 {
		if impInfo, err := e.InspectAssetImport(item.Path); err == nil {
			if sl, ok := impInfo.EffectiveImageSlice(); ok && sl != nil && sl.Width > 0 && sl.Height > 0 {
				cols := item.ImageWidth / sl.Width
				rows := item.ImageHeight / sl.Height
				if total := cols * rows; total > 1 && total <= 64 {
					frames := make([]AssetEntry, 0, total)
					for r := range rows {
						for c := range cols {
							frames = append(frames, AssetEntry{
								Atlas: item.Atlas,
								Frame: fmt.Sprintf("%s_%d", stem, len(frames)),
								Image: item.Image,
								Rect: sprite.Frame{
									X: float32(c * sl.Width),
									Y: float32(r * sl.Height),
									W: float32(sl.Width),
									H: float32(sl.Height),
								},
							})
						}
					}
					item.Clips = []AssetClipPreview{{
						Name:    "sheet",
						FrameMS: 120,
						Frames:  frames,
					}}
				}
			}
		}
	}

	totalFrames := 0
	for _, cl := range item.Clips {
		totalFrames += len(cl.Frames)
	}
	prefix := ""
	switch {
	case item.Kind == AssetKindTiles:
		if item.ImageWidth > 0 && item.ImageHeight > 0 {
			cols, rows := max(1, item.ImageWidth/16), max(1, item.ImageHeight/16)
			item.Dims = e.T("assetDrawer.dimsTilesetGrid", l10n.Param("cols", cols), l10n.Param("rows", rows))
			prefix = e.T("assetDrawer.metaTilesCount", l10n.Param("count", cols*rows))
		} else {
			item.Dims = e.T("assetDrawer.dimsTileset")
			prefix = e.T("assetDrawer.kindLabelTileset")
		}
	case len(item.Clips) > 1:
		item.Dims = e.T("assetDrawer.dimsClipsFrames",
			l10n.Param("w", item.ImageWidth),
			l10n.Param("h", item.ImageHeight),
			l10n.Param("clips", len(item.Clips)),
			l10n.Param("frames", totalFrames),
		)
		prefix = e.T("assetDrawer.metaClipsCount", l10n.Param("count", len(item.Clips)))
	case totalFrames > 1:
		item.Dims = e.T("assetDrawer.dimsFrames",
			l10n.Param("w", item.ImageWidth),
			l10n.Param("h", item.ImageHeight),
			l10n.Param("frames", totalFrames),
		)
		prefix = e.T("assetDrawer.metaFramesCount", l10n.Param("count", totalFrames))
	case item.ImageWidth > 0 && item.ImageHeight > 0:
		item.Dims = fmt.Sprintf("%d × %d", item.ImageWidth, item.ImageHeight)
	}
	item.Meta = formatAssetMeta(e, prefix, item.UseCount, item.Unused, item.Generated)
}

func (e *Editor) loadDiskThumb(root, rel, absPath string, info os.FileInfo) (decodedThumb, bool) {
	if e.assetDrawer.thumbMap == nil {
		e.assetDrawer.thumbMap = make(map[string]decodedThumb)
	}
	if prev, ok := e.assetDrawer.thumbMap[rel]; ok && prev.modTime.Equal(info.ModTime()) && prev.size == info.Size() {
		return prev, true
	}
	ext := strings.ToLower(path.Ext(rel))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
		return decodedThumb{}, false
	}
	if info.Size() <= 0 || info.Size() > maxAssetThumbBytes {
		return decodedThumb{}, false
	}
	if err := verifyFileWithinRoot(root, absPath, rel); err != nil {
		return decodedThumb{}, false
	}
	data, err := readFileLimited(absPath, maxAssetThumbBytes)
	if err != nil {
		return decodedThumb{}, false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxAssetThumbPixels {
		return decodedThumb{}, false
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return decodedThumb{}, false
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 || int64(b.Dx())*int64(b.Dy()) > maxAssetThumbPixels {
		return decodedThumb{}, false
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	}
	th := decodedThumb{
		modTime: info.ModTime(),
		size:    info.Size(),
		img:     core.ImageSource{Pixels: rgba},
		w:       b.Dx(),
		h:       b.Dy(),
	}
	e.assetDrawer.thumbMap[rel] = th
	return th, true
}

func (e *Editor) populateAudioAssetItem(item *AssetDrawerItem, root, absPath string, info os.FileInfo) {
	ext := strings.ToLower(path.Ext(item.Name))
	lang := e.ResolvedLanguage()
	if info != nil && e.assetDrawer.audioCache != nil {
		if prev, ok := e.assetDrawer.audioCache[item.Path]; ok &&
			prev.filesRev == e.filesRev &&
			prev.lang == lang &&
			prev.modTime.Equal(info.ModTime()) &&
			prev.size == info.Size() {
			item.AudioWave = prev.wave
			item.AudioDuration = prev.dur
			item.AudioRate = prev.rate
			item.AudioChannels = prev.channels
			item.Dims = prev.dims
			item.Meta = formatAssetMeta(e, prev.prefix, item.UseCount, item.Unused, item.Generated)
			return
		}
	}

	meta := decodedAudioMeta{
		filesRev: e.filesRev,
		lang:     lang,
		wave:     synthWaveform(item.Path, 48),
		dims:     strings.TrimPrefix(ext, "."),
	}
	if info != nil {
		meta.modTime = info.ModTime()
		meta.size = info.Size()
	}

	// Only decode PCM headers/waveforms for bounded WAV files inside the project root;
	// skip full-file disk reads for compressed formats (OGG/MP3/FLAC) and oversized files.
	if ext == ".wav" && info != nil && info.Size() > 0 && info.Size() <= maxAssetAudioWaveBytes {
		if verifyFileWithinRoot(root, absPath, item.Path) == nil {
			if data, err := readFileLimited(absPath, maxAssetAudioWaveBytes); err == nil {
				if wave, dur, rate, ch, ok := decodeWAVPreview(data, 48); ok {
					chLabel := e.T("assetDrawer.audioMono")
					if ch == 2 {
						chLabel = e.T("assetDrawer.audioStereo")
					} else if ch > 2 {
						chLabel = e.T("assetDrawer.audioChannels", l10n.Param("count", ch))
					}
					sec := dur.Seconds()
					meta.wave = wave
					meta.dur = dur
					meta.rate = rate
					meta.channels = ch
					meta.dims = fmt.Sprintf("%.2f s · %s · %.1f kHz", sec, chLabel, float64(rate)/1000.0)
					meta.prefix = fmt.Sprintf("%.2f s", sec)
				}
			}
		}
	}

	if info != nil {
		if e.assetDrawer.audioCache == nil {
			e.assetDrawer.audioCache = make(map[string]decodedAudioMeta)
		}
		e.assetDrawer.audioCache[item.Path] = meta
	}

	item.AudioWave = meta.wave
	item.AudioDuration = meta.dur
	item.AudioRate = meta.rate
	item.AudioChannels = meta.channels
	item.Dims = meta.dims
	item.Meta = formatAssetMeta(e, meta.prefix, item.UseCount, item.Unused, item.Generated)
}

func populateFontAssetItem(e *Editor, item *AssetDrawerItem) {
	lower := strings.ToLower(item.Name)
	if strings.Contains(lower, "cjk") || strings.Contains(lower, "zh") || strings.Contains(lower, "zpix") || strings.Contains(lower, "sc") {
		item.FontSample = "中文 Aa"
		item.Dims = e.T("assetDrawer.dimsFontCJKLatin")
		item.Meta = formatAssetMeta(e, e.T("assetDrawer.metaFontCJKLatin"), item.UseCount, item.Unused, item.Generated)
		return
	}
	item.FontSample = "Aa 12"
	item.Dims = e.T("assetDrawer.dimsFontLatin")
	item.Meta = formatAssetMeta(e, e.T("assetDrawer.metaFontLatin"), item.UseCount, item.Unused, item.Generated)
}

func formatAssetMeta(e *Editor, prefix string, useCount int, unused, generated bool) string {
	var parts []string
	if prefix != "" {
		parts = append(parts, prefix)
	}
	switch {
	case unused:
		if len(parts) == 0 {
			return e.T("assetDrawer.badgeNotUsed")
		}
		parts = append(parts, e.T("assetDrawer.badgeNotUsed"))
	case generated && useCount > 0:
		if prefix == "" {
			parts = append(parts, e.T("assetDrawer.badgeGenerated"))
		}
		parts = append(parts, e.T("assetDrawer.metaUsedCount", l10n.Param("count", useCount)))
	case useCount > 0:
		parts = append(parts, e.T("assetDrawer.metaUsedCount", l10n.Param("count", useCount)))
	case generated:
		parts = append(parts, e.T("assetDrawer.badgeGenerated"))
	}
	if len(parts) == 0 {
		return e.T("assetDrawer.badgeNotUsed")
	}
	return strings.Join(parts, " · ")
}

func (e *Editor) classifyAssetPath(rel string, isTilesetImage bool) (AssetDrawerKind, string) {
	lower := strings.ToLower(rel)
	if strings.HasSuffix(lower, ".tileset.yaml") || strings.HasSuffix(lower, ".tileset.yml") {
		return AssetKindTiles, e.T("assetDrawer.kindLabelTileset")
	}
	ext := strings.ToLower(path.Ext(lower))
	switch ext {
	case ".tsj", ".tsx":
		return AssetKindTiles, e.T("assetDrawer.kindLabelTileset")
	case ".wav", ".ogg", ".mp3", ".flac", ".m4a", ".aac", ".opus":
		return AssetKindAudio, e.T("assetDrawer.kindLabelSound")
	case ".ttf", ".otf", ".woff", ".woff2":
		return AssetKindFonts, e.T("assetDrawer.kindLabelFont")
	case ".aseprite", ".ase":
		return AssetKindSprites, "Aseprite"
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		if isTilesetImage || strings.Contains("/"+lower, "/tiles/") || strings.Contains("/"+lower, "/tilesets/") {
			return AssetKindTiles, e.T("assetDrawer.kindLabelTileset")
		}
		return AssetKindSprites, e.T("assetDrawer.kindLabelSprite")
	default:
		return AssetKindAll, e.T("assetDrawer.kindLabelAsset")
	}
}

func assetInFolder(assetPath, folder string) bool {
	if folder == "" {
		return true
	}
	return strings.HasPrefix(assetPath, folder+"/")
}

func buildAssetFolders(e *Editor, items []AssetDrawerItem, diskDirs []string) []AssetFolderEntry {
	dirSet := make(map[string]bool)
	for _, it := range items {
		for d := path.Dir(it.Path); d != "." && d != "/" && d != ""; d = path.Dir(d) {
			dirSet[d] = true
		}
	}
	// Include empty asset-like directories on disk (e.g. audio/music, art/ui, fonts) so empty-folder state works.
	for _, d := range diskDirs {
		if isAssetFolderCandidate(d, dirSet) {
			for cur := d; cur != "." && cur != "/" && cur != ""; cur = path.Dir(cur) {
				dirSet[cur] = true
			}
		}
	}
	sortedDirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		sortedDirs = append(sortedDirs, d)
	}
	slices.Sort(sortedDirs)

	out := make([]AssetFolderEntry, 0, len(sortedDirs)+1)
	out = append(out, AssetFolderEntry{
		Path:  "",
		Label: e.T("assetDrawer.folderAll"),
		Depth: 0,
		Count: len(items),
	})
	for _, d := range sortedDirs {
		count := 0
		for _, it := range items {
			if assetInFolder(it.Path, d) {
				count++
			}
		}
		out = append(out, AssetFolderEntry{
			Path:  d,
			Label: path.Base(d),
			Depth: strings.Count(d, "/"),
			Count: count,
		})
	}
	return out
}

func isAssetFolderCandidate(dir string, known map[string]bool) bool {
	if known[dir] {
		return true
	}
	top := dir
	if i := strings.IndexByte(dir, '/'); i >= 0 {
		top = dir[:i]
	}
	switch strings.ToLower(top) {
	case "art", "assets", "audio", "fonts", "sprites", "tiles", "tilesets", "sfx", "music", "ui", "models":
		return true
	}
	parent := path.Dir(dir)
	return parent != "." && known[parent]
}

func sortAssetDrawerItems(items []AssetDrawerItem, mode AssetSortMode) {
	slices.SortStableFunc(items, func(a, b AssetDrawerItem) int {
		if mode == AssetSortRecent {
			// Proposed and missing items float up when timestamps match; otherwise newest ModTime first.
			if !a.ModTime.Equal(b.ModTime) {
				if a.ModTime.After(b.ModTime) {
					return -1
				}
				return 1
			}
		}
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.Path, b.Path),
		)
	})
}

func summarizeAssetRefs(raw []refs.Ref) []AssetRefEntry {
	if len(raw) == 0 {
		return nil
	}
	type fileGroup struct {
		file  string
		field string
		count int
		first refs.Ref
	}
	var groups []fileGroup
	index := make(map[string]int)
	for _, r := range raw {
		key := r.File + "\x00" + r.Field
		if idx, ok := index[key]; ok {
			groups[idx].count++
			continue
		}
		index[key] = len(groups)
		groups = append(groups, fileGroup{file: r.File, field: r.Field, count: 1, first: r})
	}
	out := make([]AssetRefEntry, 0, len(groups))
	for _, g := range groups {
		what := g.field
		if g.count > 1 {
			what = fmt.Sprintf("%s (%d×)", g.field, g.count)
		}
		out = append(out, AssetRefEntry{
			File: g.file,
			What: what,
			Line: g.first.Line,
			Ref:  g.first,
		})
	}
	return out
}

func groupAtlasFramesIntoClips(stem string, frames []AssetEntry) []AssetClipPreview {
	order := []string{}
	byClip := make(map[string][]AssetEntry)
	for _, f := range frames {
		clipName := "frames"
		rest := f.Frame
		rest = strings.TrimPrefix(rest, stem)
		rest = strings.TrimLeft(rest, "_/.")
		if rest != "" {
			// Split clip prefix before trailing digits or separator (e.g. "run_0" -> "run", "idle/1" -> "idle").
			if idx := strings.LastIndexAny(rest, "_/."); idx > 0 {
				clipName = rest[:idx]
			}
		}
		if _, exists := byClip[clipName]; !exists {
			order = append(order, clipName)
		}
		byClip[clipName] = append(byClip[clipName], f)
	}
	out := make([]AssetClipPreview, 0, len(order))
	for _, name := range order {
		out = append(out, AssetClipPreview{
			Name:    name,
			FrameMS: 100,
			Frames:  byClip[name],
		})
	}
	return out
}

func countScenePlacements(doc *sceneformat.SceneDoc) map[string]int {
	counts := make(map[string]int)
	if doc == nil {
		return counts
	}
	for i := range doc.Layers {
		sceneformat.WalkObjects(doc.Layers[i].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			if o.Sprite != nil && o.Sprite.Atlas != "" && o.Sprite.Frame != "" {
				counts[o.Sprite.Atlas+"/"+o.Sprite.Frame]++
			}
			return true
		})
	}
	return counts
}

// --- Clip & Audio Preview Playback ---

// AssetPreviewPlaying reports whether the selected asset's clip preview is playing frame-by-frame.
func (e *Editor) AssetPreviewPlaying() bool {
	return e != nil && e.assetDrawer.previewPlaying
}

// SetAssetPreviewPlaying starts or pauses frame-by-frame clip preview playback for the selected asset.
func (e *Editor) SetAssetPreviewPlaying(playing bool) {
	if e == nil {
		return
	}
	if e.assetDrawer.previewPlaying == playing {
		return
	}
	e.assetDrawer.previewPlaying = playing
	if playing {
		e.startAssetPreviewLoop()
	} else {
		e.stopAssetPreviewLoop()
	}
	e.assetDrawer.previewRev++
	e.notify()
}

// AssetPreviewClip returns the active clip name in the selected asset's right-panel preview.
func (e *Editor) AssetPreviewClip() string {
	if e == nil {
		return ""
	}
	return e.assetDrawer.previewClip
}

// SetAssetPreviewClip picks a clip in the selected asset's right-panel preview and resets to frame 0.
func (e *Editor) SetAssetPreviewClip(clipName string) {
	if e == nil {
		return
	}
	e.assetDrawer.previewClip = clipName
	e.assetDrawer.previewFrame = 0
	e.assetDrawer.previewAccum = 0
	e.assetDrawer.previewRev++
	e.notify()
}

// AssetPreviewFrame returns the active 0-based frame index in the selected asset's right-panel preview.
func (e *Editor) AssetPreviewFrame() int {
	if e == nil || e.assetDrawer.previewFrame < 0 {
		return 0
	}
	return e.assetDrawer.previewFrame
}

// SetAssetPreviewFrame selects a specific frame index and pauses auto-playback.
func (e *Editor) SetAssetPreviewFrame(idx int) {
	if e == nil {
		return
	}
	if idx < 0 {
		idx = 0
	}
	e.assetDrawer.previewFrame = idx
	if e.assetDrawer.previewPlaying {
		e.assetDrawer.previewPlaying = false
		e.stopAssetPreviewLoop()
	}
	e.assetDrawer.previewRev++
	e.notify()
}

// StepAssetPreviewFrame advances or rewinds the selected asset's preview frame by delta (wrapping around).
func (e *Editor) StepAssetPreviewFrame(delta int) {
	if e == nil {
		return
	}
	snap := e.AssetDrawerSnapshot()
	if snap.Selected == nil || len(snap.Selected.Clips) == 0 {
		return
	}
	clip := activePreviewClip(snap.Selected, e.assetDrawer.previewClip)
	if len(clip.Frames) == 0 {
		return
	}
	n := len(clip.Frames)
	next := (e.assetDrawer.previewFrame + delta) % n
	if next < 0 {
		next += n
	}
	e.assetDrawer.previewFrame = next
	e.assetDrawer.previewRev++
	e.notify()
}

func activePreviewClip(item *AssetDrawerItem, wanted string) AssetClipPreview {
	if item == nil || len(item.Clips) == 0 {
		return AssetClipPreview{}
	}
	for _, cl := range item.Clips {
		if cl.Name == wanted {
			return cl
		}
	}
	return item.Clips[0]
}

func (e *Editor) activePreviewFrameInterval() time.Duration {
	snap := e.AssetDrawerSnapshot()
	if snap.Selected != nil && len(snap.Selected.Clips) > 0 {
		clip := activePreviewClip(snap.Selected, e.assetDrawer.previewClip)
		if clip.FrameMS > 0 {
			return time.Duration(clip.FrameMS) * time.Millisecond
		}
	}
	return 120 * time.Millisecond
}

func (e *Editor) startAssetPreviewLoop() {
	e.stopAssetPreviewLoop()
	if e.scope == nil {
		return
	}
	e.assetDrawer.previewAccum = 0
	loop := &game.Loop{
		Rate: e.PlayRate,
		Update: func(dt time.Duration) {
			if !e.assetDrawer.previewPlaying || e.assetDrawer.mode != LeftRailAssets {
				e.stopAssetPreviewLoop()
				return
			}
			interval := e.activePreviewFrameInterval()
			e.assetDrawer.previewAccum += dt
			if e.assetDrawer.previewAccum >= interval {
				e.assetDrawer.previewAccum %= interval
				e.StepAssetPreviewFrame(1)
			}
		},
	}
	var stop func()
	e.scope.Run(func() {
		stop = loop.Run(e.scope)
	})
	e.assetDrawer.previewClipStop = stop
}

func (e *Editor) stopAssetPreviewLoop() {
	if e == nil {
		return
	}
	if e.assetDrawer.previewClipStop != nil {
		e.assetDrawer.previewClipStop()
		e.assetDrawer.previewClipStop = nil
	}
}

// AssetAudioPlayingPath returns the project-relative path of the audio asset currently playing in the right panel, or "".
func (e *Editor) AssetAudioPlayingPath() string {
	if e == nil {
		return ""
	}
	return e.assetDrawer.audioPlayingPath
}

// ToggleAssetAudioPreview plays or stops the audio asset at relPath in the right-panel preview.
func (e *Editor) ToggleAssetAudioPreview(relPath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.assetDrawer.audioPlayingPath == relPath && relPath != "" {
		e.StopAssetAudioPreview()
		return nil
	}
	e.stopAssetAudioPreview()
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	clean, err := normalizeProjectAssetPath(relPath)
	if err != nil {
		return err
	}
	root := filepath.Dir(e.ProjectPath)
	abs := filepath.Join(root, filepath.FromSlash(clean))
	if err := verifyFileWithinRoot(root, abs, clean); err != nil {
		return err
	}
	raw, err := readFileLimited(abs, maxAssetAudioPreviewBytes)
	if err != nil {
		return fmt.Errorf("editor: read audio %q: %w", clean, err)
	}
	if e.scope != nil {
		if e.assetDrawer.audioMixer == nil || e.assetDrawer.audioMixerScope != e.scope {
			var m *sound.Mixer
			e.scope.Run(func() {
				m = sound.NewMixer(e.scope, "editor.assetPreview")
			})
			e.assetDrawer.audioMixer = m
			e.assetDrawer.audioMixerScope = e.scope
			e.assetDrawer.audioMusic = nil
		}
		if e.assetDrawer.audioMixer != nil {
			if e.assetDrawer.audioMusic == nil {
				e.assetDrawer.audioMusic = e.assetDrawer.audioMixer.Music("preview")
			}
			e.assetDrawer.audioMusic.Loop = false
			e.assetDrawer.audioMusic.Play(audio.Source{Data: raw}, 0)
		}
	}
	e.assetDrawer.audioPlayingPath = clean
	e.assetDrawer.previewRev++
	e.Status = e.T("assetDrawer.statusPlaying", l10n.Param("name", path.Base(clean)))
	e.notify()
	return nil
}

// StopAssetAudioPreview stops any active right-panel sound preview.
func (e *Editor) StopAssetAudioPreview() {
	if e == nil {
		return
	}
	was := e.assetDrawer.audioPlayingPath != ""
	e.stopAssetAudioPreview()
	if was {
		e.assetDrawer.previewRev++
		e.Status = e.T("assetDrawer.statusStoppedPreview")
		e.notify()
	}
}

func (e *Editor) stopAssetAudioPreview() {
	if e == nil {
		return
	}
	if e.assetDrawer.audioMusic != nil {
		e.assetDrawer.audioMusic.Stop(0)
	}
	e.assetDrawer.audioPlayingPath = ""
}

func (e *Editor) cleanupAssetDrawerScope(cx *core.Scope) {
	if e == nil {
		return
	}
	e.stopAssetPreviewLoop()
	e.stopAssetAudioPreview()
	if e.assetDrawer.audioMixerScope == cx {
		e.assetDrawer.audioMixer = nil
		e.assetDrawer.audioMixerScope = nil
		e.assetDrawer.audioMusic = nil
	}
}

// --- Drag & Drop onto 2D Viewport (Place Snapped or Swap Sprite, 1 Undo Step, Esc Cancels) ---

// AssetSnapStep returns the snap grid spacing in pixels used when dragging a sprite asset onto the 2D viewport.
// When SnapToGrid is enabled, it uses EffectiveGridSize; otherwise it uses the active layer's tile width if present or 16 px.
func (e *Editor) AssetSnapStep() float32 {
	if e == nil {
		return 16
	}
	if e.SnapToGrid {
		return e.EffectiveGridSize()
	}
	if li := e.layerIndex(e.ActiveLayer); li >= 0 {
		if tm := e.Doc.Layers[li].Tilemap; tm != nil && tm.TileWidth > 0 {
			return float32(tm.TileWidth)
		}
	}
	if e.GridSize > 0 && isFiniteFloat32(e.GridSize) && e.GridSize != defaultGridSize2D {
		return e.GridSize
	}
	return 16
}

func snapAssetCoord(v, step float32) float32 {
	if step <= 0 || !isFiniteFloat32(step) {
		return v
	}
	r := float32(math.Round(float64(v)/float64(step))) * step
	if r == 0 {
		return 0
	}
	return r
}

// AssetDragActive reports whether a sprite drag from the asset drawer is currently in progress.
func (e *Editor) AssetDragActive() bool {
	return e != nil && e.assetDrawer.drag.Active
}

// AssetDragState returns a copy of the active asset drag preview state.
func (e *Editor) AssetDragState() AssetDragState {
	if e == nil {
		return AssetDragState{}
	}
	return e.assetDrawer.drag
}

// BeginAssetDrag starts dragging the sprite asset at assetPath from the asset drawer.
func (e *Editor) BeginAssetDrag(assetPath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	drag, err := e.resolveAssetDrag(assetPath)
	if err != nil {
		return err
	}
	e.beginAssetDragPayload(drag)
	return nil
}

func (e *Editor) beginAssetDragPayload(drag AssetDrawerDrag) {
	if e == nil || e.playing {
		return
	}
	e.assetDrawer.drag = AssetDragState{
		Active: true,
		Drag:   drag,
		SnapPx: e.AssetSnapStep(),
	}
	e.notify()
}

// UpdateAssetDrag updates the active sprite drag preview position in viewport canvas coordinates,
// detecting whether the pointer is over an existing object (swap mode) or empty space (snapped placement).
func (e *Editor) UpdateAssetDrag(canvasX, canvasY float32) {
	if e == nil || !e.assetDrawer.drag.Active {
		return
	}
	if !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		return
	}
	e.updateAssetDragPayload(e.assetDrawer.drag.Drag, canvasX, canvasY)
}

func (e *Editor) updateAssetDragPayload(drag AssetDrawerDrag, canvasX, canvasY float32) {
	if e == nil || e.playing {
		return
	}
	if !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		return
	}
	step := e.AssetSnapStep()
	lx, ly := e.layerCoordsForActive(canvasX, canvasY)
	sx, sy := snapAssetCoord(lx, step), snapAssetCoord(ly, step)
	if !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return
	}

	hitID := e.HitTest(canvasX, canvasY)
	hitName := hitID
	if hitID != "" {
		if obj, _, _ := e.FindObject(hitID); obj != nil && obj.Name != "" {
			hitName = obj.Name
		}
	}

	layerName := e.ActiveLayer
	if layerName == "" && e.Doc != nil && len(e.Doc.Layers) > 0 {
		layerName = e.Doc.Layers[0].Name
	}
	var detail string
	if hitID != "" {
		detail = e.T("assetDrawer.dragSwapDetail", l10n.Param("name", hitName))
	} else {
		detail = e.T("assetDrawer.dragNewDetail",
			l10n.Param("layer", layerName),
			l10n.Param("step", fmt.Sprintf("%.0f", step)),
		)
	}

	e.assetDrawer.drag = AssetDragState{
		Active:           true,
		HoveringViewport: true,
		Drag:             drag,
		CanvasX:          canvasX,
		CanvasY:          canvasY,
		LayerX:           sx,
		LayerY:           sy,
		SnapPx:           step,
		TargetObjectID:   hitID,
		TargetObjectName: hitName,
		PillName:         drag.Name,
		PillDetail:       detail,
	}
	e.notify()
}

// ClearAssetDragHover hides the viewport ghost preview while keeping the drag alive until dropped or cancelled.
func (e *Editor) ClearAssetDragHover() {
	if e == nil || !e.assetDrawer.drag.HoveringViewport {
		return
	}
	e.assetDrawer.drag.HoveringViewport = false
	e.notify()
}

// CancelAssetDrag cancels any in-progress sprite drag cleanly without mutating the scene.
func (e *Editor) CancelAssetDrag() {
	if e == nil || !e.assetDrawer.drag.Active {
		return
	}
	e.assetDrawer.drag = AssetDragState{}
	e.Status = e.T("assetDrawer.statusDragCancelled")
	e.notify()
}

// DropAssetAt drops the sprite asset at assetPath onto viewport canvas point (canvasX, canvasY).
// If an existing visible unlocked object lies under (canvasX, canvasY), its sprite is swapped in one undo step
// and swapped is true. Otherwise, a new sprite object is placed on the active layer, snapped to AssetSnapStep,
// in one undo step and swapped is false.
func (e *Editor) DropAssetAt(assetPath string, canvasX, canvasY float32) (objectID string, swapped bool, err error) {
	if e == nil {
		return "", false, errors.New("editor: nil editor")
	}
	if !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		e.CancelAssetDrag()
		return "", false, errors.New("editor: drop coordinates must be finite")
	}
	drag, err := e.resolveAssetDrag(assetPath)
	if err != nil {
		e.CancelAssetDrag()
		return "", false, err
	}
	return e.dropAssetDragAt(drag, canvasX, canvasY)
}

func (e *Editor) dropAssetDragAt(drag AssetDrawerDrag, canvasX, canvasY float32) (string, bool, error) {
	wasDragging := e.assetDrawer.drag.Active || e.assetDrawer.drag.HoveringViewport
	notified := false
	defer func() {
		e.assetDrawer.drag = AssetDragState{}
		if wasDragging && !notified {
			e.notify()
		}
	}()
	if e.playing {
		return "", false, ErrPlaying
	}
	if e.Doc == nil {
		return "", false, errors.New("editor: no scene document is open")
	}
	if e.ViewMode != ViewMode2D {
		return "", false, errors.New("editor: switch to 2D view to drop sprite assets")
	}
	if !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		return "", false, errors.New("editor: drop coordinates must be finite")
	}
	ref, err := e.resolveSpriteRefForDrag(drag)
	if err != nil {
		return "", false, err
	}

	// If dropped on an existing object, swap its sprite in ONE undo step (SetObjectSprite).
	if hitID := e.HitTest(canvasX, canvasY); hitID != "" {
		if err := e.SetObjectSprite(hitID, ref); err != nil {
			return "", false, err
		}
		e.SelectObject(hitID)
		e.assetDrawer.viewRev++
		e.Status = e.T("assetDrawer.statusSwappedSprite",
			l10n.Param("target", hitID),
			l10n.Param("name", drag.Name),
		)
		notified = true
		e.notify()
		return hitID, true, nil
	}

	// Otherwise place a new sprite object on the active layer, snapped, in ONE undo step (PlaceObject).
	if err := e.lockedLayerErr(); err != nil {
		return "", false, err
	}
	step := e.AssetSnapStep()
	lx, ly := e.layerCoordsForActive(canvasX, canvasY)
	sx, sy := snapAssetCoord(lx, step), snapAssetCoord(ly, step)
	if !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return "", false, errors.New("editor: snapped drop coordinates must be finite")
	}
	name := strings.TrimSuffix(drag.Name, path.Ext(drag.Name))
	if name == "" {
		name = ref.Frame
	}
	id, err := e.PlaceObject(e.ActiveLayer, sceneformat.ObjectDoc{
		Name:   name,
		X:      sx,
		Y:      sy,
		Sprite: ref,
	})
	if err != nil {
		return "", false, err
	}
	e.assetDrawer.viewRev++
	e.Status = e.T("assetDrawer.statusPlacedSprite",
		l10n.Param("name", drag.Name),
		l10n.Param("layer", e.ActiveLayer),
		l10n.Param("step", fmt.Sprintf("%.0f", step)),
	)
	notified = true
	e.notify()
	return id, false, nil
}

func (e *Editor) resolveAssetDrag(assetPath string) (AssetDrawerDrag, error) {
	snap := e.AssetDrawerSnapshot()
	clean := strings.TrimPrefix(path.Clean(strings.ReplaceAll(assetPath, "\\", "/")), "./")
	for _, it := range snap.AllItems {
		if it.Path == clean || it.Path == assetPath {
			if it.Missing {
				return AssetDrawerDrag{}, fmt.Errorf("editor: asset %q is missing on disk", it.Path)
			}
			if it.Kind != AssetKindSprites && it.Kind != AssetKindTiles {
				return AssetDrawerDrag{}, fmt.Errorf("editor: %q is not a placeable sprite asset", it.Path)
			}
			return AssetDrawerDrag{
				Path:  it.Path,
				Name:  it.Name,
				Atlas: it.Atlas,
				Frame: it.Frame,
				Image: it.Image,
				Rect:  it.Rect,
			}, nil
		}
	}
	// Fallback: direct "atlas/frame" lookup in Resolver.
	if atlas, frame, ok := strings.Cut(assetPath, "/"); ok {
		for _, ae := range e.Assets() {
			if ae.Atlas == atlas && ae.Frame == frame {
				return AssetDrawerDrag{
					Path:  assetPath,
					Name:  frame,
					Atlas: ae.Atlas,
					Frame: ae.Frame,
					Image: ae.Image,
					Rect:  ae.Rect,
				}, nil
			}
		}
	}
	return AssetDrawerDrag{}, fmt.Errorf("editor: asset %q not found", assetPath)
}

func (e *Editor) resolveSpriteRefForDrag(drag AssetDrawerDrag) (*sceneformat.SpriteRef, error) {
	if drag.Atlas != "" && drag.Frame != "" {
		return &sceneformat.SpriteRef{Atlas: drag.Atlas, Frame: drag.Frame}, nil
	}
	stem := strings.TrimSuffix(drag.Name, path.Ext(drag.Name))
	for _, ae := range e.Assets() {
		if ae.Frame == drag.Name || ae.Frame == stem || ae.Frame == drag.Path {
			return &sceneformat.SpriteRef{Atlas: ae.Atlas, Frame: ae.Frame}, nil
		}
	}
	if e.Resolver == nil {
		atlas := path.Dir(drag.Path)
		if atlas == "." || atlas == "" {
			atlas = "sprites"
		}
		return &sceneformat.SpriteRef{Atlas: atlas, Frame: stem}, nil
	}
	return nil, fmt.Errorf("editor: sprite %q is not in a loaded atlas yet; run Build Assets to pack it", drag.Path)
}

// drawAssetDragOverlay draws the snapped placement ghost box or swap highlight on the 2D viewport canvas.
func (e *Editor) drawAssetDragOverlay(d *core.Drawing, scene *sceneformat.BuiltScene) {
	st := e.assetDrawer.drag
	if !st.Active || !st.HoveringViewport || e.ViewMode != ViewMode2D {
		return
	}
	p := e.activePalette()
	addedFill := ui.Alpha(p.Added.Fg, 0.12)
	d.Save()
	defer d.Restore()

	if st.TargetObjectID != "" && scene != nil {
		// Highlight the target object whose sprite will be swapped.
		if bo, layer := findBuiltObjectAndLayer(scene, st.TargetObjectID); bo != nil {
			x, y, w, h := builtObjectCanvasBounds(e, bo, layer)
			d.SetFillColor(addedFill).BeginPath().RoundRect(x-3, y-3, w+6, h+6, 4).Fill()
			d.SetStrokeColor(p.Added.Fg).SetLineWidth(2).SetLineDash(5, 3)
			d.BeginPath().RoundRect(x-3, y-3, w+6, h+6, 4).Stroke()
			d.SetLineDash()
			return
		}
	}

	// Draw snapped drop footprint and sprite ghost on the active layer.
	space, px, py := e.layerSpaceAndParallax(e.ActiveLayer)
	cx, cy := e.assetLayerToCanvas(space, px, py, st.LayerX, st.LayerY)
	zoom := e.View2D().Zoom
	if zoom <= 0 {
		zoom = 1
	}
	gw := float32(32)
	gh := float32(32)
	if st.Drag.Rect.W > 0 && st.Drag.Rect.H > 0 {
		gw = max(24, st.Drag.Rect.W*zoom)
		gh = max(24, st.Drag.Rect.H*zoom)
	}
	boxW := max(gw, st.SnapPx*zoom)
	boxH := max(gh, st.SnapPx*zoom)
	bx := cx - boxW/2
	by := cy - boxH/2
	d.SetFillColor(addedFill).BeginPath().RoundRect(bx, by, boxW, boxH, 3).Fill()
	d.SetStrokeColor(p.Added.Fg).SetLineWidth(1.5).SetLineDash(4, 3)
	d.BeginPath().RoundRect(bx, by, boxW, boxH, 3).Stroke()
	d.SetLineDash()

	if st.Drag.Rect.W > 0 && st.Drag.Rect.H > 0 && hasImageSource(st.Drag.Image) {
		f := st.Drag.Rect
		d.SetAlpha(0.88)
		d.DrawImageFrom(st.Drag.Image, f.X, f.Y, f.W, f.H, cx-gw/2, cy-gh/2, gw, gh)
	}
}

func (e *Editor) assetLayerToCanvas(space sceneformat.Space, px, py, lx, ly float32) (float32, float32) {
	if space == sceneformat.SpaceScreen {
		return e.ScreenToCanvas(lx, ly)
	}
	if px == 1 && py == 1 {
		return e.WorldToCanvas(lx, ly)
	}
	v := e.View2D()
	cw, ch := e.CanvasSize()
	dx, dy := lx-v.X*px, ly-v.Y*py
	if v.Rotation == 0 {
		return cw/2 + dx*v.Zoom, ch/2 + dy*v.Zoom
	}
	sin, cos := math.Sincos(float64(-v.Rotation))
	rx := float32(float64(dx)*cos - float64(dy)*sin)
	ry := float32(float64(dx)*sin + float64(dy)*cos)
	return cw/2 + rx*v.Zoom, ch/2 + ry*v.Zoom
}

func findBuiltObjectAndLayer(scene *sceneformat.BuiltScene, id string) (*sceneformat.BuiltObject, *sceneformat.BuiltLayer) {
	if scene == nil || id == "" {
		return nil, nil
	}
	var walk func([]*sceneformat.BuiltObject) *sceneformat.BuiltObject
	walk = func(objs []*sceneformat.BuiltObject) *sceneformat.BuiltObject {
		for _, bo := range objs {
			if bo.ID == id {
				return bo
			}
			if found := walk(bo.Children); found != nil {
				return found
			}
		}
		return nil
	}
	for _, l := range scene.Layers {
		if found := walk(l.Objects); found != nil {
			return found, l
		}
	}
	return nil, nil
}

func builtObjectCanvasBounds(e *Editor, bo *sceneformat.BuiltObject, layer *sceneformat.BuiltLayer) (x, y, w, h float32) {
	if bo == nil || bo.Doc == nil {
		return 0, 0, 24, 24
	}
	o := bo.Doc
	var lx0, ly0, lx1, ly1 float32
	if bo.Sprite != nil && bo.Sprite.Frame.Width > 0 && bo.Sprite.Frame.Height > 0 {
		lx0 = -o.OriginX
		ly0 = -o.OriginY
		lx1 = lx0 + bo.Sprite.Frame.Width
		ly1 = ly0 + bo.Sprite.Frame.Height
	} else {
		lx0, ly0, lx1, ly1 = -defaultMarkerHalfSize, -defaultMarkerHalfSize, defaultMarkerHalfSize, defaultMarkerHalfSize
	}
	space := sceneformat.SpaceWorld
	px, py := float32(1), float32(1)
	if layer != nil {
		space = layer.Space
		px, py = layer.ParallaxX, layer.ParallaxY
	}
	corners := [4]math2d.Vec2{
		bo.World.TransformPoint(math2d.Vec2{X: lx0, Y: ly0}),
		bo.World.TransformPoint(math2d.Vec2{X: lx1, Y: ly0}),
		bo.World.TransformPoint(math2d.Vec2{X: lx1, Y: ly1}),
		bo.World.TransformPoint(math2d.Vec2{X: lx0, Y: ly1}),
	}
	minX, minY := float32(math.Inf(1)), float32(math.Inf(1))
	maxX, maxY := float32(math.Inf(-1)), float32(math.Inf(-1))
	for _, pt := range corners {
		cx, cy := e.assetLayerToCanvas(space, px, py, pt.X, pt.Y)
		if cx < minX {
			minX = cx
		}
		if cx > maxX {
			maxX = cx
		}
		if cy < minY {
			minY = cy
		}
		if cy > maxY {
			maxY = cy
		}
	}
	if !isFiniteFloat32(minX) || !isFiniteFloat32(minY) || !isFiniteFloat32(maxX) || !isFiniteFloat32(maxY) {
		return 0, 0, 24, 24
	}
	return minX, minY, max(12, maxX-minX), max(12, maxY-minY)
}

// --- Path validation & bounded I/O helpers ---

func parseLocalFileInput(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", errors.New("editor: empty file path")
	}
	lower := strings.ToLower(p)
	if strings.HasPrefix(lower, "file://") {
		u, err := url.Parse(p)
		if err != nil {
			return "", fmt.Errorf("editor: invalid file URI %q: %w", raw, err)
		}
		if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
			return "", fmt.Errorf("editor: unsupported file URI host %q", u.Host)
		}
		unescaped := u.Path
		if unescaped == "" {
			return "", fmt.Errorf("editor: empty file URI path %q", raw)
		}
		if len(unescaped) >= 3 && unescaped[0] == '/' && unescaped[2] == ':' &&
			(('a' <= unescaped[1] && unescaped[1] <= 'z') || ('A' <= unescaped[1] && unescaped[1] <= 'Z')) {
			unescaped = unescaped[1:]
		}
		p = filepath.FromSlash(unescaped)
	}
	return p, nil
}

func readFileLimited(p string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("editor: %q is a directory", p)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return nil, fmt.Errorf("editor: file %q exceeds size limit (%d bytes)", p, maxBytes)
	}
	if maxBytes <= 0 {
		return io.ReadAll(f)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("editor: file %q exceeds size limit (%d bytes)", p, maxBytes)
	}
	return data, nil
}

func verifyProjectWritePath(rootDir, absPath, label string) error {
	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		rootReal = rootAbs
	}
	targetReal, err := evalHUDSymlinksOrParent(absPath)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootReal, targetReal)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("editor: asset path %q resolves outside the project root", label)
	}
	return nil
}

func isSourceSidecarName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".source.yaml") || strings.HasSuffix(lower, ".source.yml")
}

func validateImportBaseName(base string) error {
	if base == "" || base == "." || base == ".." || strings.ContainsAny(base, "/\\") {
		return fmt.Errorf("editor: invalid asset filename %q", base)
	}
	if strings.HasPrefix(base, ".") {
		return fmt.Errorf("editor: refusing to import hidden file %q", base)
	}
	if importsettings.IsSidecar(base) || isSourceSidecarName(base) {
		return fmt.Errorf("editor: refusing to import sidecar file %q", base)
	}
	return nil
}

// --- Import, Replace Missing, Generate & Ask Agent Actions ---

// ImportAssetFiles copies external files (OS paths or file:// URIs) into the active asset folder
// (or an inferred folder such as art/props, audio/sfx, or fonts), marks them as proposed imports,
// and refreshes the asset drawer.
func (e *Editor) ImportAssetFiles(srcPaths []string) ([]string, error) {
	if e == nil {
		return nil, errors.New("editor: nil editor")
	}
	if e.playing {
		return nil, ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	root := filepath.Dir(e.ProjectPath)
	var imported []string
	for _, raw := range srcPaths {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		p, err := parseLocalFileInput(raw)
		if err != nil {
			return imported, err
		}
		base := filepath.Base(p)
		if err := validateImportBaseName(base); err != nil {
			return imported, err
		}
		destFolder := e.assetDrawer.folder
		if destFolder != "" {
			cleanFolder, ok := normalizeAssetDrawerFolder(destFolder)
			if !ok {
				return imported, fmt.Errorf("editor: invalid destination folder %q", destFolder)
			}
			destFolder = cleanFolder
		} else {
			kind, _ := e.classifyAssetPath(base, false)
			switch kind {
			case AssetKindAudio:
				destFolder = "audio/sfx"
			case AssetKindFonts:
				destFolder = "fonts"
			case AssetKindTiles:
				destFolder = "art/tiles"
			default:
				destFolder = "art/sprites"
			}
		}
		rel, err := normalizeProjectAssetPath(path.Join(destFolder, base))
		if err != nil {
			return imported, err
		}
		destAbs := filepath.Join(root, filepath.FromSlash(rel))
		if err := verifyProjectWritePath(root, destAbs, rel); err != nil {
			return imported, err
		}
		data, err := readFileLimited(p, maxAssetImportBytes)
		if err != nil {
			return imported, fmt.Errorf("editor: read %q: %w", p, err)
		}
		if err := os.MkdirAll(filepath.Dir(destAbs), 0o755); err != nil {
			return imported, err
		}
		if err := verifyProjectWritePath(root, destAbs, rel); err != nil {
			return imported, err
		}
		if err := writeFileAtomic(destAbs, data); err != nil {
			return imported, err
		}
		imported = append(imported, rel)
	}
	if len(imported) > 0 {
		e.filesRev++
		e.assetsRev++
		e.assetDrawer.importCacheKey = ""
		e.ProposeAssetImports(imported)
		e.SelectAssetPath(imported[0])
		e.Status = e.T("assetDrawer.statusImported", l10n.Param("count", len(imported)))
		e.notify()
	}
	return imported, nil
}

// ReplaceMissingAsset replaces a missing asset path by copying srcFilePath to missingPath on disk,
// refreshing refs and the asset drawer so the missing problem resolves immediately.
func (e *Editor) ReplaceMissingAsset(missingPath, srcFilePath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	clean, err := normalizeProjectAssetPath(missingPath)
	if err != nil {
		return err
	}
	src, err := parseLocalFileInput(srcFilePath)
	if err != nil {
		return err
	}
	root := filepath.Dir(e.ProjectPath)
	destAbs := filepath.Join(root, filepath.FromSlash(clean))
	if err := verifyProjectWritePath(root, destAbs, clean); err != nil {
		return err
	}
	data, err := readFileLimited(src, maxAssetImportBytes)
	if err != nil {
		return fmt.Errorf("editor: read replacement %q: %w", srcFilePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(destAbs), 0o755); err != nil {
		return err
	}
	if err := verifyProjectWritePath(root, destAbs, clean); err != nil {
		return err
	}
	if err := writeFileAtomic(destAbs, data); err != nil {
		return err
	}
	e.filesRev++
	e.assetsRev++
	e.assetDrawer.drawerRev++
	e.assetDrawer.viewRev++
	e.assetDrawer.cacheKey = ""
	e.assetDrawer.importCacheKey = ""
	e.invalidateProjectIndex()
	e.CheckProblems()
	e.Status = e.T("assetDrawer.statusReplacedMissing", l10n.Param("path", clean))
	e.notify()
	return nil
}

// TriggerAssetImport runs OnAssetImport when configured, or opens a native file picker when a scope is attached.
func (e *Editor) TriggerAssetImport() {
	if e == nil || e.playing {
		return
	}
	if e.OnAssetImport != nil {
		e.OnAssetImport(e.assetDrawer.folder)
		return
	}
	if e.scope != nil {
		e.scope.PickFiles(core.PickFilesOptions{
			Title:    e.T("assetDrawer.pickImportTitle"),
			Multiple: true,
		}, func(files []core.File, err error) {
			if err != nil {
				e.report(err)
				return
			}
			var paths []string
			for _, f := range files {
				if f.Path != "" {
					paths = append(paths, f.Path)
				}
			}
			if len(paths) > 0 {
				_, ierr := e.ImportAssetFiles(paths)
				e.report(ierr)
			}
		})
		return
	}
	e.Status = e.T("assetDrawer.statusSelectImport", l10n.Param("dest", cmp.Or(e.assetDrawer.folder, e.T("assetDrawer.projectFallback"))))
	e.notify()
}

// TriggerAssetGenerate invokes OnAssetGenerate when configured, or reports ready status.
func (e *Editor) TriggerAssetGenerate() {
	if e == nil || e.playing {
		return
	}
	if e.OnAssetGenerate != nil {
		e.OnAssetGenerate(e.assetDrawer.folder)
		return
	}
	e.Status = e.T("assetDrawer.statusConnectGenerator")
	e.notify()
}

// TriggerReplaceMissingAsset invokes OnAssetReplaceMissing when configured, or opens a native file picker to replace missingPath.
func (e *Editor) TriggerReplaceMissingAsset(missingPath string) {
	if e == nil || e.playing || missingPath == "" {
		return
	}
	if e.OnAssetReplaceMissing != nil {
		e.OnAssetReplaceMissing(missingPath)
		return
	}
	if e.scope != nil {
		e.scope.PickFiles(core.PickFilesOptions{
			Title: e.T("assetDrawer.pickReplaceTitle", l10n.Param("name", path.Base(missingPath))),
		}, func(files []core.File, err error) {
			if err != nil {
				e.report(err)
				return
			}
			if len(files) > 0 && files[0].Path != "" {
				e.report(e.ReplaceMissingAsset(missingPath, files[0].Path))
			}
		})
		return
	}
	e.Status = e.T("assetDrawer.statusReplaceMissing", l10n.Param("path", missingPath))
	e.notify()
}

// AskAgentAboutAsset notifies OnAssetAskAgent (when configured) and updates status with the asset prompt.
func (e *Editor) AskAgentAboutAsset(assetPath, prompt string) {
	if e == nil {
		return
	}
	if e.OnAssetAskAgent != nil {
		e.OnAssetAskAgent(assetPath, prompt)
	}
	e.Status = e.T("assetDrawer.statusAskedAgent",
		l10n.Param("target", cmp.Or(assetPath, e.T("assetDrawer.assetsFallback"))),
		l10n.Param("prompt", prompt),
	)
	e.notify()
}

// --- Pure-Go WAV waveform & metadata decoder ---

func decodeWAVPreview(data []byte, bars int) (wave []float32, dur time.Duration, sampleRate, channels int, ok bool) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, 0, 0, false
	}
	var audioFmt, bitsPerSample uint16
	var pcm []byte
	pos := 12
	for pos+8 <= len(data) {
		id := string(data[pos : pos+4])
		rawSize := binary.LittleEndian.Uint32(data[pos+4 : pos+8])
		pos += 8
		if uint64(rawSize) > uint64(len(data)-pos) {
			break
		}
		size := int(rawSize)
		chunk := data[pos : pos+size]
		switch id {
		case "fmt ":
			if len(chunk) >= 16 {
				audioFmt = binary.LittleEndian.Uint16(chunk[0:2])
				channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
				sampleRate = int(binary.LittleEndian.Uint32(chunk[4:8]))
				bitsPerSample = binary.LittleEndian.Uint16(chunk[14:16])
			}
		case "data":
			pcm = chunk
		}
		pos += size
		if size%2 == 1 {
			pos++
		}
	}
	if audioFmt != 1 || channels <= 0 || sampleRate <= 0 || (bitsPerSample != 8 && bitsPerSample != 16) || len(pcm) == 0 {
		return nil, 0, 0, 0, false
	}
	bytesPerSample := int(bitsPerSample / 8)
	frameBytes := channels * bytesPerSample
	totalFrames := len(pcm) / frameBytes
	if totalFrames <= 0 {
		return nil, 0, 0, 0, false
	}
	dur = time.Duration(float64(totalFrames) / float64(sampleRate) * float64(time.Second))
	if bars <= 0 {
		bars = 48
	}
	wave = make([]float32, bars)
	for i := range bars {
		start := int(int64(i) * int64(totalFrames) / int64(bars))
		end := max(start+1, int(int64(i+1)*int64(totalFrames)/int64(bars)))
		if end > totalFrames {
			end = totalFrames
		}
		var peak float32
		step := max(1, (end-start)/32)
		for f := start; f < end; f += step {
			off := f * frameBytes
			var v float32
			if bitsPerSample == 8 {
				v = float32(int(pcm[off])-128) / 128.0
			} else {
				s := int16(binary.LittleEndian.Uint16(pcm[off : off+2]))
				v = float32(s) / 32768.0
			}
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		if peak < 0.08 {
			peak = 0.08
		}
		wave[i] = peak
	}
	return wave, dur, sampleRate, channels, true
}

func synthWaveform(seedKey string, bars int) []float32 {
	var seed float64 = 1.7
	for i := range len(seedKey) {
		seed += float64(seedKey[i]) * 0.13
	}
	out := make([]float32, bars)
	for i := range bars {
		t := float64(i) / float64(max(1, bars))
		env := min(1.0, t*9.0) * math.Pow(1.0-t, 1.3)
		a := max(0.08, env*(0.5+0.5*math.Abs(math.Sin(float64(i)*seed*0.7+seed))))
		out[i] = float32(a)
	}
	return out
}
