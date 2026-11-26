package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/base/dock"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// LayoutVersion is the schema version of the per-user editor dock layout YAML file.
const LayoutVersion = 1

// LayoutFileName is the filename of the per-user editor dock layout stored in the OS user config directory.
const LayoutFileName = "layout.yaml"

const (
	// maxLayoutFileBytes caps the layout YAML file size read or parsed to guard against malformed/oversized inputs.
	maxLayoutFileBytes = 1 << 20 // 1 MiB
	// maxLayoutDepth caps split-tree nesting depth to guard against stack exhaustion on crafted YAML.
	maxLayoutDepth = 64
	// maxLayoutSize caps region and split sizes in points to prevent float32 rounding overflow or layout collapse.
	maxLayoutSize = 100000
)

// Built-in dock panel keys stored in LayoutFile and used by ui.DockArea.
const (
	PanelViewport     = "viewport"
	PanelSceneTree    = "scene_tree"
	PanelLayers       = "layers"
	PanelAssets       = "assets"
	PanelFiles        = "files"
	PanelLibrary      = "library"
	PanelInspector    = "inspector"
	PanelProblems     = "problems"
	PanelOutput       = "output"
	PanelProfiler     = "profiler"
	PanelAnimation    = "animation"
	PanelSearch       = "search"
	PanelStates       = "states"
	PanelTileset      = "tileset"
	PanelTerrain      = "terrain"
	PanelRig          = "rig"
	PanelParticles    = "particles"
	PanelMaterial     = "material"
	PanelEnvironment  = "environment"
	PanelDataTable    = "datatable"
	PanelNavMesh      = "navmesh"
	PanelLocalization = "localization"
)

const (
	defaultLeftDockSize   core.Points = 444
	defaultSceneColSize   core.Points = 220
	defaultAssetsColSize  core.Points = 224
	defaultLayersSize     core.Points = 200
	defaultInspectorSize  core.Points = 272
	defaultRightDockSize  core.Points = 224
	defaultBottomDockSize core.Points = 140
	defaultPluginSize     core.Points = 224
)

func preferredBottomHeight(key string) core.Points {
	switch key {
	case PanelStates:
		return 404
	case PanelProfiler, PanelNavMesh, PanelRig:
		return 324
	case PanelAnimation, PanelTileset, PanelDataTable, PanelLocalization, PanelParticles, PanelMaterial, PanelEnvironment:
		return 284
	case PanelTerrain:
		return 240
	case PanelSearch:
		return 204
	default:
		return defaultBottomDockSize
	}
}

// PluginPanelKey returns the stable dock panel key for panel id of plugin.
func PluginPanelKey(plugin, id string) string {
	return "plugin:" + plugin + ":" + id
}

// LayoutFile is the strict versioned YAML document storing a user's dock panel arrangement outside any project directory.
type LayoutFile struct {
	// Version is the schema version; it must equal LayoutVersion (1).
	Version int `yaml:"version"`
	// Center is the center region's split/tab tree (always containing the viewport panel).
	Center LayoutNode `yaml:"center"`
	// Left is the optional left dock region.
	Left *LayoutRegion `yaml:"left,omitempty"`
	// Right is the optional right dock region.
	Right *LayoutRegion `yaml:"right,omitempty"`
	// Bottom is the optional bottom dock region.
	Bottom *LayoutRegion `yaml:"bottom,omitempty"`
	// Hidden lists panel keys explicitly hidden by the user, sorted lexicographically.
	Hidden []string `yaml:"hidden,omitempty"`
}

// LayoutRegion describes one edge dock (left, right, or bottom): its size across its edge in points, whether it is collapsed, and its container tree.
type LayoutRegion struct {
	Size   float32    `yaml:"size"`
	Closed bool       `yaml:"closed,omitempty"`
	Root   LayoutNode `yaml:"root"`
}

// LayoutNode is either a tab group (Panels + optional Active index) or a split (Axis + Children + optional Sizes).
type LayoutNode struct {
	Panels   []string     `yaml:"panels,omitempty"`
	Active   int          `yaml:"active,omitempty"`
	Axis     string       `yaml:"axis,omitempty"`
	Sizes    []float32    `yaml:"sizes,omitempty,flow"`
	Children []LayoutNode `yaml:"children,omitempty"`
}

// PanelInfo describes one dockable editor panel for menus, commands, and layout inspection.
type PanelInfo struct {
	// Key is the stable panel key (such as PanelInspector or PluginPanelKey("health", "overview")).
	Key string
	// Title is the human-readable title shown on tabs and in the View > Panels menu.
	Title string
	// DefaultDock is where the panel docks by default.
	DefaultDock Dock
	// Visible reports whether the panel is currently shown in the dock area.
	Visible bool
	// Available reports whether the panel can currently be toggled or shown (for example Files requires an open project).
	Available bool
	// Closable reports whether the user can hide or close the panel (false for PanelViewport).
	Closable bool
	// Plugin and PluginID identify the owning plugin when this entry represents a plugin panel.
	Plugin   string
	PluginID string
}

type builtinPanelSpec struct {
	key         string
	title       string
	defaultDock Dock
	// persistent panels stay shown whenever available unless listed in hidden.
	// non-persistent panels (toggleable / document-contextual) follow their own open flag on Editor.
	persistent bool
	closable   bool
}

var builtinPanelSpecs = []builtinPanelSpec{
	{key: PanelViewport, title: "Viewport", defaultDock: DockLeft, persistent: true, closable: false},
	{key: PanelSceneTree, title: "Scene", defaultDock: DockLeft, persistent: true, closable: true},
	{key: PanelLayers, title: "Layers", defaultDock: DockLeft, persistent: true, closable: true},
	{key: PanelAssets, title: "Assets", defaultDock: DockLeft, persistent: true, closable: true},
	{key: PanelFiles, title: "Files", defaultDock: DockLeft, persistent: true, closable: true},
	{key: PanelLibrary, title: "Library", defaultDock: DockLeft, persistent: true, closable: true},
	{key: PanelInspector, title: "Inspector", defaultDock: DockRight, persistent: true, closable: true},
	{key: PanelProblems, title: "Problems", defaultDock: DockBottom, persistent: true, closable: true},
	{key: PanelOutput, title: "Output", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelProfiler, title: "Profiler", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelSearch, title: "Search", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelAnimation, title: "Animation", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelStates, title: "States", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelTileset, title: "Tileset", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelTerrain, title: "Terrain", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelRig, title: "2D Rig", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelParticles, title: "Particles", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelMaterial, title: "Material", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelEnvironment, title: "Scene Environment", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelDataTable, title: "Data Table", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelNavMesh, title: "3D NavMesh", defaultDock: DockBottom, persistent: false, closable: true},
	{key: PanelLocalization, title: "Localization", defaultDock: DockBottom, persistent: false, closable: true},
}

func builtinSpec(key string) (builtinPanelSpec, bool) {
	for _, s := range builtinPanelSpecs {
		if s.key == key {
			return s, true
		}
	}
	return builtinPanelSpec{}, false
}

// DefaultLayoutPath returns the default per-user path for layout.yaml under the OS user config directory.
func DefaultLayoutPath() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("editor: user config dir: %w", err)
	}
	return filepath.Join(cfgDir, "nsgo-game", LayoutFileName), nil
}

// DefaultLayoutFile returns the canonical default editor dock layout document.
func DefaultLayoutFile() *LayoutFile {
	return layoutFileFromDock(defaultDockBlueprint(), nil)
}

func defaultDockBlueprint() dock.Layout {
	sceneCol := dock.Split(base.Vertical, dock.Tabs(PanelSceneTree), dock.Tabs(PanelLayers))
	sceneCol.Sizes = []core.Points{0, defaultLayersSize}

	assetsCol := dock.Split(base.Vertical, dock.Tabs(PanelAssets), dock.Tabs(PanelFiles))

	leftRoot := dock.Split(base.Horizontal, sceneCol, assetsCol)
	leftRoot.Sizes = []core.Points{defaultSceneColSize, defaultAssetsColSize}

	centerRoot := dock.Split(base.Horizontal, dock.Tabs(PanelViewport), dock.Tabs(PanelInspector))
	centerRoot.Sizes = []core.Points{0, defaultInspectorSize}

	return dock.Layout{
		Center: centerRoot,
		Left: &dock.Region{
			Root: leftRoot,
			Size: defaultLeftDockSize,
		},
		Bottom: &dock.Region{
			Root:   dock.Tabs(PanelProblems),
			Size:   defaultBottomDockSize,
			Closed: true,
		},
	}
}

// ParseLayout strictly decodes and validates a versioned editor dock layout YAML document,
// refusing unknown fields, duplicate keys, trailing YAML documents, invalid nodes, and unsupported versions.
func ParseLayout(data []byte) (*LayoutFile, error) {
	if len(data) > maxLayoutFileBytes {
		return nil, fmt.Errorf("editor: layout: document exceeds %d bytes", maxLayoutFileBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("editor: layout: empty document (missing version)")
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("editor: layout: parse: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("editor: layout: document must be a YAML mapping with version")
	}
	if err := checkDuplicateYAMLKeys(root.Content[0]); err != nil {
		return nil, fmt.Errorf("editor: layout: %w", err)
	}
	if err := checkNoLayoutYAMLReferences(root.Content[0], 0); err != nil {
		return nil, fmt.Errorf("editor: layout: %w", err)
	}
	hasVersion := false
	for i := 0; i+1 < len(root.Content[0].Content); i += 2 {
		if root.Content[0].Content[i].Value == "version" {
			hasVersion = true
			break
		}
	}
	if !hasVersion {
		return nil, errors.New("editor: layout: missing required field version")
	}

	var f LayoutFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("editor: layout: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("editor: layout: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("editor: layout: decode: %w", err)
	}
	if err := ValidateLayout(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

// ValidateLayout checks that f conforms to version 1 of the editor dock layout schema.
func ValidateLayout(f *LayoutFile) error {
	if f == nil {
		return errors.New("editor: layout: nil document")
	}
	if f.Version != LayoutVersion {
		return fmt.Errorf("editor: layout: unsupported version %d (want %d)", f.Version, LayoutVersion)
	}
	seenPanels := make(map[string]bool)
	if err := validateLayoutNode("center", &f.Center, seenPanels, 0); err != nil {
		return err
	}
	for _, r := range []struct {
		name string
		reg  *LayoutRegion
	}{
		{"left", f.Left},
		{"right", f.Right},
		{"bottom", f.Bottom},
	} {
		if r.reg == nil {
			continue
		}
		if err := validateFiniteNonNegative(r.name+".size", r.reg.Size); err != nil {
			return err
		}
		if err := validateLayoutNode(r.name+".root", &r.reg.Root, seenPanels, 0); err != nil {
			return err
		}
	}
	seenHidden := make(map[string]bool, len(f.Hidden))
	for i, k := range f.Hidden {
		if err := validateLayoutPanelKey(fmt.Sprintf("hidden[%d]", i), k); err != nil {
			return err
		}
		if k == PanelViewport {
			return fmt.Errorf("editor: layout: hidden[%d]: %q cannot be hidden", i, PanelViewport)
		}
		if seenHidden[k] {
			return fmt.Errorf("editor: layout: hidden[%d]: duplicate panel %q", i, k)
		}
		seenHidden[k] = true
	}
	return nil
}

func validateLayoutNode(path string, n *LayoutNode, seen map[string]bool, depth int) error {
	if depth > maxLayoutDepth {
		return fmt.Errorf("editor: layout: %s: nesting depth exceeds %d", path, maxLayoutDepth)
	}
	hasTabs := len(n.Panels) > 0
	hasSplit := n.Axis != "" || len(n.Children) > 0 || len(n.Sizes) > 0
	switch {
	case hasTabs && hasSplit:
		return fmt.Errorf("editor: layout: %s: node cannot set both panels and split fields", path)
	case !hasTabs && !hasSplit:
		return fmt.Errorf("editor: layout: %s: node must have panels or children", path)
	case hasTabs:
		if n.Active < 0 || n.Active >= len(n.Panels) {
			return fmt.Errorf("editor: layout: %s: active index %d out of range [0, %d)", path, n.Active, len(n.Panels))
		}
		for i, k := range n.Panels {
			if err := validateLayoutPanelKey(fmt.Sprintf("%s.panels[%d]", path, i), k); err != nil {
				return err
			}
			if seen[k] {
				return fmt.Errorf("editor: layout: %s.panels[%d]: duplicate panel %q", path, i, k)
			}
			seen[k] = true
		}
		return nil
	default:
		if n.Active != 0 {
			return fmt.Errorf("editor: layout: %s.active: only valid on tab groups", path)
		}
		if n.Axis != "horizontal" && n.Axis != "vertical" {
			return fmt.Errorf("editor: layout: %s.axis: unsupported axis %q (want \"horizontal\" or \"vertical\")", path, n.Axis)
		}
		if len(n.Children) == 0 {
			return fmt.Errorf("editor: layout: %s: split must have at least one child", path)
		}
		if len(n.Sizes) > 0 && len(n.Sizes) != len(n.Children) {
			return fmt.Errorf("editor: layout: %s.sizes: length %d does not match %d children", path, len(n.Sizes), len(n.Children))
		}
		for i, sz := range n.Sizes {
			if err := validateFiniteNonNegative(fmt.Sprintf("%s.sizes[%d]", path, i), sz); err != nil {
				return err
			}
		}
		for i := range n.Children {
			if err := validateLayoutNode(fmt.Sprintf("%s.children[%d]", path, i), &n.Children[i], seen, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
}

func validateLayoutPanelKey(field, k string) error {
	if k == "" {
		return fmt.Errorf("editor: layout: %s is required", field)
	}
	if strings.TrimSpace(k) != k || strings.IndexFunc(k, unicode.IsControl) >= 0 {
		return fmt.Errorf("editor: layout: %s %q must have no surrounding spaces or control characters", field, k)
	}
	return nil
}

func checkNoLayoutYAMLReferences(n *yaml.Node, depth int) error {
	if depth > maxLayoutDepth*4 {
		return fmt.Errorf("line %d: YAML nesting depth exceeds limit", n.Line)
	}
	if n.Kind == yaml.AliasNode {
		return fmt.Errorf("line %d: YAML aliases are not allowed", n.Line)
	}
	if n.Anchor != "" {
		return fmt.Errorf("line %d: YAML anchors are not allowed", n.Line)
	}
	for i, c := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 && c.Tag == "!!merge" {
			return fmt.Errorf("line %d: YAML merge keys are not allowed", c.Line)
		}
		if err := checkNoLayoutYAMLReferences(c, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func validateFiniteNonNegative(field string, v float32) error {
	f := float64(v)
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > maxLayoutSize {
		return fmt.Errorf("editor: layout: %s must be finite and in [0, %d] (got %v)", field, maxLayoutSize, v)
	}
	return nil
}

// EncodeLayout validates f and encodes it as deterministic 2-space YAML that ParseLayout reads back unchanged.
func EncodeLayout(f *LayoutFile) ([]byte, error) {
	if err := ValidateLayout(f); err != nil {
		return nil, err
	}
	canon := canonicalizeLayoutFile(f)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(canon); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("editor: layout: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("editor: layout: encode: %w", err)
	}
	return buf.Bytes(), nil
}

func canonicalizeLayoutFile(f *LayoutFile) *LayoutFile {
	out := &LayoutFile{
		Version: f.Version,
		Center:  canonicalizeLayoutNode(f.Center),
	}
	if f.Left != nil {
		r := canonicalizeLayoutRegion(*f.Left)
		out.Left = &r
	}
	if f.Right != nil {
		r := canonicalizeLayoutRegion(*f.Right)
		out.Right = &r
	}
	if f.Bottom != nil {
		r := canonicalizeLayoutRegion(*f.Bottom)
		out.Bottom = &r
	}
	if len(f.Hidden) > 0 {
		out.Hidden = slices.Clone(f.Hidden)
		slices.Sort(out.Hidden)
	}
	return out
}

func canonicalizeLayoutRegion(r LayoutRegion) LayoutRegion {
	return LayoutRegion{
		Size:   roundLayoutPoints(r.Size),
		Closed: r.Closed,
		Root:   canonicalizeLayoutNode(r.Root),
	}
}

func canonicalizeLayoutNode(n LayoutNode) LayoutNode {
	if len(n.Panels) > 0 {
		return LayoutNode{
			Panels: slices.Clone(n.Panels),
			Active: n.Active,
		}
	}
	out := LayoutNode{
		Axis:     n.Axis,
		Children: make([]LayoutNode, len(n.Children)),
	}
	for i, c := range n.Children {
		out.Children[i] = canonicalizeLayoutNode(c)
	}
	anySize := false
	for _, sz := range n.Sizes {
		if sz > 0 {
			anySize = true
			break
		}
	}
	if anySize && len(n.Sizes) == len(n.Children) {
		out.Sizes = make([]float32, len(n.Sizes))
		for i, sz := range n.Sizes {
			out.Sizes[i] = roundLayoutPoints(sz)
		}
	}
	return out
}

func roundLayoutPoints(v float32) float32 {
	f := float64(v)
	if math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
		return 0
	}
	return float32(math.Round(f*2) / 2)
}

func readLayoutFileBounded(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("editor: layout: %q is not a regular file", path)
	}
	if info.Size() > maxLayoutFileBytes {
		return nil, fmt.Errorf("editor: layout: file exceeds %d bytes", maxLayoutFileBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxLayoutFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxLayoutFileBytes {
		return nil, fmt.Errorf("editor: layout: file exceeds %d bytes", maxLayoutFileBytes)
	}
	return data, nil
}

// LoadLayoutFile reads and parses the layout YAML file at path.
// When the file does not exist, it returns DefaultLayoutFile() and a nil error.
func LoadLayoutFile(path string) (*LayoutFile, error) {
	data, err := readLayoutFileBounded(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DefaultLayoutFile(), nil
		}
		return nil, err
	}
	return ParseLayout(data)
}

// SaveLayoutFile encodes f and atomically writes it to path, creating parent directories if needed.
func SaveLayoutFile(path string, f *LayoutFile) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("editor: layout: empty path")
	}
	data, err := EncodeLayout(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// --- Conversions between dock.Layout and LayoutFile ---

func sanitizeDockPoints(p core.Points) core.Points {
	f := float64(p)
	if math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
		return 0
	}
	if f > maxLayoutSize {
		return core.Points(maxLayoutSize)
	}
	return p
}

func sanitizeDockNode(n dock.Node, seen map[string]bool) (dock.Node, bool) {
	if n.Tabs {
		activeKey := ""
		if n.Active >= 0 && n.Active < len(n.Panels) {
			activeKey = n.Panels[n.Active]
		}
		kept := make([]string, 0, len(n.Panels))
		for _, k := range n.Panels {
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			kept = append(kept, k)
		}
		if len(kept) == 0 {
			return dock.Node{}, false
		}
		active := 0
		if idx := slices.Index(kept, activeKey); idx >= 0 {
			active = idx
		} else if n.Active >= 0 && n.Active < len(kept) {
			active = n.Active
		} else {
			active = len(kept) - 1
		}
		n.Panels = kept
		n.Active = active
		return n, true
	}
	hasSizes := len(n.Sizes) == len(n.Children)
	children := make([]dock.Node, 0, len(n.Children))
	sizes := make([]core.Points, 0, len(n.Children))
	anyPos := false
	for i := range n.Children {
		c, ok := sanitizeDockNode(n.Children[i], seen)
		if !ok {
			continue
		}
		children = append(children, c)
		if hasSizes {
			sz := sanitizeDockPoints(n.Sizes[i])
			sizes = append(sizes, sz)
			if sz > 0 {
				anyPos = true
			}
		}
	}
	if len(children) == 0 {
		return dock.Node{}, false
	}
	if len(children) == 1 {
		return children[0], true
	}
	n.Children = children
	if hasSizes && anyPos {
		n.Sizes = sizes
	} else {
		n.Sizes = nil
	}
	return n, true
}

func sanitizeDockRegion(r *dock.Region, fallback core.Points, seen map[string]bool) *dock.Region {
	if r == nil {
		return nil
	}
	root, ok := sanitizeDockNode(r.Root, seen)
	if !ok {
		return nil
	}
	cp := *r
	if sz := sanitizeDockPoints(cp.Size); sz > 0 {
		cp.Size = sz
	} else {
		cp.Size = fallback
	}
	cp.Root = root
	return &cp
}

func sanitizeDockLayout(l dock.Layout) dock.Layout {
	seen := make(map[string]bool)
	if c, ok := sanitizeDockNode(l.Center, seen); ok {
		l.Center = c
	} else {
		l.Center = dock.Tabs(PanelViewport)
		seen[PanelViewport] = true
	}
	l.Left = sanitizeDockRegion(l.Left, defaultLeftDockSize, seen)
	l.Right = sanitizeDockRegion(l.Right, defaultRightDockSize, seen)
	l.Bottom = sanitizeDockRegion(l.Bottom, defaultBottomDockSize, seen)
	return l
}

func layoutFileFromDock(l dock.Layout, hidden map[string]bool) *LayoutFile {
	out := &LayoutFile{
		Version: LayoutVersion,
		Center:  layoutNodeFromDock(l.Center),
	}
	if l.Left != nil && hasAnyPanel(&l.Left.Root) {
		r := layoutRegionFromDock(l.Left)
		out.Left = &r
	}
	if l.Right != nil && hasAnyPanel(&l.Right.Root) {
		r := layoutRegionFromDock(l.Right)
		out.Right = &r
	}
	if l.Bottom != nil && hasAnyPanel(&l.Bottom.Root) {
		r := layoutRegionFromDock(l.Bottom)
		out.Bottom = &r
	}
	for k, h := range hidden {
		if h && k != PanelViewport {
			out.Hidden = append(out.Hidden, k)
		}
	}
	slices.Sort(out.Hidden)
	return canonicalizeLayoutFile(out)
}

func layoutRegionFromDock(r *dock.Region) LayoutRegion {
	sz := float32(r.Size)
	if f := float64(sz); math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
		sz = float32(defaultPluginSize)
	}
	return LayoutRegion{
		Size:   roundLayoutPoints(sz),
		Closed: r.Closed,
		Root:   layoutNodeFromDock(r.Root),
	}
}

func layoutNodeFromDock(n dock.Node) LayoutNode {
	if n.Tabs {
		active := n.Active
		if active < 0 || active >= len(n.Panels) {
			active = 0
		}
		return LayoutNode{
			Panels: slices.Clone(n.Panels),
			Active: active,
		}
	}
	// Unwrap single-child splits (dock.Area wraps Center in a 1-child split).
	if len(n.Children) == 1 {
		return layoutNodeFromDock(n.Children[0])
	}
	axis := "horizontal"
	if n.Axis == base.Vertical {
		axis = "vertical"
	}
	out := LayoutNode{
		Axis:     axis,
		Children: make([]LayoutNode, len(n.Children)),
	}
	for i, c := range n.Children {
		out.Children[i] = layoutNodeFromDock(c)
	}
	anySize := false
	for _, s := range n.Sizes {
		if f := float64(s); !math.IsNaN(f) && !math.IsInf(f, 0) && f > 0 {
			anySize = true
			break
		}
	}
	if anySize && len(n.Sizes) == len(n.Children) {
		out.Sizes = make([]float32, len(n.Sizes))
		for i, s := range n.Sizes {
			out.Sizes[i] = roundLayoutPoints(float32(s))
		}
	}
	return out
}

func dockLayoutFromFile(f *LayoutFile) (dock.Layout, map[string]bool) {
	if f == nil {
		return defaultDockBlueprint(), nil
	}
	l := dock.Layout{
		Center: dockNodeFromFile(f.Center),
	}
	if f.Left != nil {
		l.Left = dockRegionFromFile(f.Left)
	}
	if f.Right != nil {
		l.Right = dockRegionFromFile(f.Right)
	}
	if f.Bottom != nil {
		l.Bottom = dockRegionFromFile(f.Bottom)
	}
	var hidden map[string]bool
	if len(f.Hidden) > 0 {
		hidden = make(map[string]bool, len(f.Hidden))
		for _, k := range f.Hidden {
			if k != PanelViewport {
				hidden[k] = true
			}
		}
	}
	return l, hidden
}

func dockRegionFromFile(r *LayoutRegion) *dock.Region {
	return &dock.Region{
		Root:   dockNodeFromFile(r.Root),
		Size:   core.Points(r.Size),
		Closed: r.Closed,
	}
}

func dockNodeFromFile(n LayoutNode) dock.Node {
	if len(n.Panels) > 0 {
		return dock.Node{
			Tabs:   true,
			Panels: slices.Clone(n.Panels),
			Active: n.Active,
		}
	}
	axis := base.Horizontal
	if n.Axis == "vertical" {
		axis = base.Vertical
	}
	out := dock.Node{
		Axis:     axis,
		Children: make([]dock.Node, len(n.Children)),
	}
	for i, c := range n.Children {
		out.Children[i] = dockNodeFromFile(c)
	}
	if len(n.Sizes) == len(n.Children) {
		out.Sizes = make([]core.Points, len(n.Sizes))
		for i, s := range n.Sizes {
			out.Sizes[i] = core.Points(s)
		}
	}
	return out
}

// --- Dock tree helpers ---

func hasAnyPanel(n *dock.Node) bool {
	if n == nil {
		return false
	}
	if n.Tabs {
		return len(n.Panels) > 0
	}
	for i := range n.Children {
		if hasAnyPanel(&n.Children[i]) {
			return true
		}
	}
	return false
}

func dockTreePanels(n *dock.Node, out *[]string) {
	if n == nil {
		return
	}
	if n.Tabs {
		*out = append(*out, n.Panels...)
		return
	}
	for i := range n.Children {
		dockTreePanels(&n.Children[i], out)
	}
}

func dockLayoutPanels(l *dock.Layout) []string {
	var out []string
	dockTreePanels(&l.Center, &out)
	for _, r := range []*dock.Region{l.Left, l.Right, l.Bottom} {
		if r != nil {
			dockTreePanels(&r.Root, &out)
		}
	}
	return out
}

func dockLayoutHasPanel(l *dock.Layout, key string) bool {
	_, g := findDockPanel(l, key)
	return g != nil
}

func findDockPanel(l *dock.Layout, key string) (dock.Placement, *dock.Node) {
	if g := findNodeGroup(&l.Center, key); g != nil {
		return dock.Center, g
	}
	for _, p := range []dock.Placement{dock.Left, dock.Right, dock.Bottom} {
		if r := l.Region(p); r != nil {
			if g := findNodeGroup(&r.Root, key); g != nil {
				return p, g
			}
		}
	}
	return dock.Center, nil
}

func findNodeGroup(n *dock.Node, key string) *dock.Node {
	if n == nil {
		return nil
	}
	if n.Tabs {
		if slices.Contains(n.Panels, key) {
			return n
		}
		return nil
	}
	for i := range n.Children {
		if g := findNodeGroup(&n.Children[i], key); g != nil {
			return g
		}
	}
	return nil
}

// filterDockNode returns a copy of n keeping only panels where keep(k) is true, collapsing empty nodes and single-child splits.
func filterDockNode(n dock.Node, keep func(string) bool) (dock.Node, bool) {
	if n.Tabs {
		activeKey := ""
		if n.Active >= 0 && n.Active < len(n.Panels) {
			activeKey = n.Panels[n.Active]
		}
		kept := make([]string, 0, len(n.Panels))
		for _, k := range n.Panels {
			if keep(k) {
				kept = append(kept, k)
			}
		}
		if len(kept) == 0 {
			return dock.Node{}, false
		}
		active := 0
		if idx := slices.Index(kept, activeKey); idx >= 0 {
			active = idx
		} else if n.Active < len(kept) && n.Active >= 0 {
			active = n.Active
		} else {
			active = len(kept) - 1
		}
		return dock.Node{
			ID:     n.ID,
			Tabs:   true,
			Panels: kept,
			Active: active,
		}, true
	}
	children := make([]dock.Node, 0, len(n.Children))
	sizes := make([]core.Points, 0, len(n.Children))
	hasSizes := len(n.Sizes) == len(n.Children)
	for i, c := range n.Children {
		fc, ok := filterDockNode(c, keep)
		if !ok {
			continue
		}
		children = append(children, fc)
		if hasSizes {
			sizes = append(sizes, n.Sizes[i])
		}
	}
	if len(children) == 0 {
		return dock.Node{}, false
	}
	if len(children) == 1 {
		return children[0], true
	}
	out := dock.Node{
		ID:       n.ID,
		Axis:     n.Axis,
		Children: children,
	}
	if hasSizes {
		out.Sizes = sizes
	}
	return out, true
}

func filterDockLayout(l dock.Layout, keep func(string) bool) dock.Layout {
	out := dock.Layout{}
	if c, ok := filterDockNode(l.Center, keep); ok {
		out.Center = c
	} else {
		out.Center = dock.Tabs(PanelViewport)
	}
	if l.Left != nil {
		if r, ok := filterDockNode(l.Left.Root, keep); ok {
			cp := *l.Left
			cp.Root = r
			out.Left = &cp
		}
	}
	if l.Right != nil {
		if r, ok := filterDockNode(l.Right.Root, keep); ok {
			cp := *l.Right
			cp.Root = r
			out.Right = &cp
		}
	}
	if l.Bottom != nil {
		if r, ok := filterDockNode(l.Bottom.Root, keep); ok {
			cp := *l.Bottom
			cp.Root = r
			out.Bottom = &cp
		}
	}
	return out
}

func detachDockPanel(l *dock.Layout, key string) {
	*l = filterDockLayout(*l, func(k string) bool { return k != key })
}

func activatePanelInLayout(l *dock.Layout, key string) bool {
	p, g := findDockPanel(l, key)
	if g == nil {
		return false
	}
	if idx := slices.Index(g.Panels, key); idx >= 0 {
		g.Active = idx
	}
	if p != dock.Center {
		if r := l.Region(p); r != nil {
			r.Closed = false
		}
	}
	return true
}

// --- Editor dock layout state & lifecycle ---

type savedPluginHint struct {
	region   dock.Placement
	siblings []string
	hidden   bool
}

type dockLayoutState struct {
	initialized        bool
	userSizedBottom    bool
	userSizedInspector bool
	userClosedRegion   map[dock.Placement]bool
	blueprint          dock.Layout
	hidden             map[string]bool
	pluginHints        map[string]savedPluginHint
	lastAvailable      map[string]bool
	lastActive         dock.Layout
	lastSyncKey        string
	rev                uint64
	areaRef            *dock.AreaRef
}

func (e *Editor) preferredInspectorWidth() core.Points {
	if e == nil {
		return defaultInspectorSize
	}
	if !e.Playing() {
		switch {
		case e.HistoryPanelOpen():
			return 340
		case e.ProblemAgentPanelOpen():
			return 380
		case e.ViewMode == ViewMode2D && e.TileReviewOpen():
			return 304
		case e.SketchPanelOpen():
			return 360
		case e.TasksPanelOpen():
			return 368
		}
	}
	if e.AgentMode() != AgentModeNone && e.RightRailTab() == RightTabAgent {
		return 380
	}
	return defaultInspectorSize
}

func inspectorSplitSize(l *dock.Layout) (core.Points, bool) {
	if l == nil || l.Center.Tabs || l.Center.Axis != base.Horizontal || len(l.Center.Sizes) != len(l.Center.Children) {
		return 0, false
	}
	for i := range l.Center.Children {
		c := &l.Center.Children[i]
		if c.Tabs && slices.Contains(c.Panels, PanelInspector) && !slices.Contains(c.Panels, PanelViewport) {
			return l.Center.Sizes[i], true
		}
	}
	return 0, false
}

func setInspectorSplitSize(l *dock.Layout, w core.Points) {
	if l == nil || l.Center.Tabs || l.Center.Axis != base.Horizontal || len(l.Center.Sizes) != len(l.Center.Children) {
		return
	}
	for i := range l.Center.Children {
		c := &l.Center.Children[i]
		if c.Tabs && slices.Contains(c.Panels, PanelInspector) && !slices.Contains(c.Panels, PanelViewport) {
			l.Center.Sizes[i] = w
			return
		}
	}
}

func (e *Editor) ensureDockLayout() {
	if e.dock.initialized {
		return
	}
	e.dock.initialized = true
	e.dock.blueprint = defaultDockBlueprint()
	e.dock.hidden = map[string]bool{}
	e.dock.lastAvailable = nil
	e.dock.lastSyncKey = ""
	e.syncDockLayout(false)
}

// SetLayoutPath configures where the editor persists its per-user dock layout YAML file
// and loads any existing layout from path (an absent file keeps the default layout).
// Panels in the saved file that no longer exist are dropped.
func (e *Editor) SetLayoutPath(path string) error {
	if e == nil {
		return nil
	}
	e.LayoutPath = path
	return e.LoadLayout()
}

// LoadLayout loads the dock layout from Editor.LayoutPath when set, dropping any panels that no longer exist.
func (e *Editor) LoadLayout() error {
	if e == nil {
		return nil
	}
	e.ensureDockLayout()
	if e.LayoutPath == "" {
		return nil
	}
	data, err := readLayoutFileBounded(e.LayoutPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	f, err := ParseLayout(data)
	if err != nil {
		return err
	}
	e.dock.rev++
	e.applyLoadedLayoutFile(f)
	e.notify()
	return nil
}

func (e *Editor) applyLoadedLayoutFile(f *LayoutFile) {
	raw, hidden := dockLayoutFromFile(f)
	if hidden == nil {
		hidden = map[string]bool{}
	}
	// Stash saved locations for unregistered plugin panels in memory only, in case
	// RegisterPlugin is called after SetLayoutPath; DockLayout and SaveLayout drop them.
	hints := map[string]savedPluginHint{}
	for _, k := range dockLayoutPanels(&raw) {
		if !e.isKnownPanel(k) && strings.HasPrefix(k, "plugin:") {
			p, g := findDockPanel(&raw, k)
			var sibs []string
			if g != nil {
				for _, s := range g.Panels {
					if s != k {
						sibs = append(sibs, s)
					}
				}
			}
			hints[k] = savedPluginHint{region: p, siblings: sibs, hidden: hidden[k]}
		}
	}
	e.dock.pluginHints = hints

	// Drop panels that no longer exist.
	pruned := filterDockLayout(raw, e.isKnownPanel)
	if !dockLayoutHasPanel(&pruned, PanelViewport) {
		pruned.Center = insertSplitOrTab(pruned.Center, PanelViewport, base.Horizontal, true, 0)
	}
	prunedHidden := map[string]bool{}
	for k, h := range hidden {
		if h && k != PanelViewport && e.isKnownPanel(k) {
			prunedHidden[k] = true
		}
	}
	e.dock.blueprint = pruned
	e.dock.hidden = prunedHidden
	e.dock.userSizedBottom = pruned.Bottom != nil && pruned.Bottom.Size > 0
	if sz, ok := inspectorSplitSize(&pruned); ok && sz > 0 && sz != defaultInspectorSize {
		e.dock.userSizedInspector = true
	} else {
		e.dock.userSizedInspector = false
	}
	e.dock.userClosedRegion = map[dock.Placement]bool{}
	for _, p := range []dock.Placement{dock.Left, dock.Right, dock.Bottom} {
		if r := pruned.Region(p); r != nil && r.Closed {
			e.dock.userClosedRegion[p] = true
		}
	}
	e.dock.lastAvailable = e.availablePanels()
	e.dock.lastSyncKey = ""
	e.syncDockLayout(false)
}

// SaveLayout encodes the editor's current dock layout (dropping any non-existent panels) and writes it atomically to Editor.LayoutPath when set.
func (e *Editor) SaveLayout() error {
	if e == nil || e.LayoutPath == "" {
		return nil
	}
	return SaveLayoutFile(e.LayoutPath, e.LayoutFile())
}

// LayoutFile returns a canonical copy of the editor's current persisted dock layout.
func (e *Editor) LayoutFile() *LayoutFile {
	if e == nil {
		return DefaultLayoutFile()
	}
	e.ensureDockLayout()
	e.syncDockLayout(false)
	pruned := filterDockLayout(e.dock.blueprint, e.isKnownPanel)
	if !dockLayoutHasPanel(&pruned, PanelViewport) {
		pruned.Center = insertSplitOrTab(pruned.Center, PanelViewport, base.Horizontal, true, 0)
	}
	return layoutFileFromDock(pruned, e.dock.hidden)
}

// DockLayout returns the effective dock.Layout currently displayed by ui.DockArea.
func (e *Editor) DockLayout() dock.Layout {
	if e == nil {
		return defaultDockBlueprint()
	}
	e.ensureDockLayout()
	e.syncDockLayout(false)
	return e.dock.lastActive.Clone()
}

// DockAreaRef returns the mounted ui.DockArea's AreaRef, or nil when EditorView is not mounted.
func (e *Editor) DockAreaRef() *dock.AreaRef {
	if e == nil {
		return nil
	}
	return e.dock.areaRef
}

// ResetLayout restores the default dock panel arrangement, clears hidden panels, persists to LayoutPath when set, and notifies subscribers.
func (e *Editor) ResetLayout() {
	if e == nil {
		return
	}
	e.dock.initialized = true
	e.dock.userSizedBottom = false
	e.dock.userSizedInspector = false
	e.dock.userClosedRegion = nil
	e.dock.blueprint = defaultDockBlueprint()
	e.dock.hidden = map[string]bool{}
	e.dock.pluginHints = nil
	e.dock.lastAvailable = nil
	e.dock.lastSyncKey = ""
	e.dock.rev++
	e.syncDockLayout(false)
	_ = e.SaveLayout()
	e.notify()
}

// SetDockLayout applies a dock.Layout edit (from ui.DockArea drag/drop, resize, tab switch, or close),
// reconciles it with the blueprint, persists to LayoutPath when set, and notifies subscribers.
func (e *Editor) SetDockLayout(l dock.Layout) {
	if e == nil {
		return
	}
	e.ensureDockLayout()
	l = sanitizeDockLayout(l)
	oldActive := e.dock.lastActive.Clone()
	oldBlueprint := e.dock.blueprint.Clone()

	// Guarantee viewport is never lost.
	if !dockLayoutHasPanel(&l, PanelViewport) {
		l.Center = insertSplitOrTab(l.Center, PanelViewport, base.Horizontal, true, 0)
	}

	// Detect panels closed directly via tab close button in ui.DockArea.
	newSet := make(map[string]bool)
	for _, k := range dockLayoutPanels(&l) {
		newSet[k] = true
	}
	closedPanels := make([]string, 0)
	for _, k := range dockLayoutPanels(&oldActive) {
		if !newSet[k] && k != PanelViewport {
			closedPanels = append(closedPanels, k)
		}
	}

	// Merge inactive and hidden known panels from oldBlueprint back into nextBlueprint
	// so their user-chosen slots survive while they are temporarily unavailable or hidden.
	nextBlueprint := stripEmptyRegions(l.Clone())
	for _, k := range dockLayoutPanels(&oldBlueprint) {
		if newSet[k] || !e.isKnownPanel(k) {
			continue
		}
		reinsertPreservedPanel(&nextBlueprint, &oldBlueprint, k)
	}

	if oldActive.Bottom != nil && l.Bottom != nil && oldActive.Bottom.Size != l.Bottom.Size {
		e.dock.userSizedBottom = true
	}
	if oldSz, ok1 := inspectorSplitSize(&oldActive); ok1 {
		if newSz, ok2 := inspectorSplitSize(&l); ok2 && oldSz != newSz {
			e.dock.userSizedInspector = true
		}
	}
	for _, p := range []dock.Placement{dock.Left, dock.Right, dock.Bottom} {
		if r := l.Region(p); r != nil {
			if e.dock.userClosedRegion == nil {
				e.dock.userClosedRegion = map[dock.Placement]bool{}
			}
			e.dock.userClosedRegion[p] = r.Closed
		}
	}
	e.dock.blueprint = nextBlueprint
	for _, k := range closedPanels {
		e.handleClosedDockPanel(k)
	}
	e.dock.lastSyncKey = ""
	e.dock.rev++
	e.syncDockLayout(false)
	_ = e.SaveLayout()
	e.notify()
}

// MovePanel moves panel key into the same tab group as targetPanel,
// updates and persists the layout, and returns whether the move succeeded.
func (e *Editor) MovePanel(key, targetPanel string) bool {
	if e == nil || key == "" || key == PanelViewport || targetPanel == "" || key == targetPanel || !e.isKnownPanel(key) {
		return false
	}
	e.ensureDockLayout()
	e.syncDockLayout(false)
	if !dockLayoutHasPanel(&e.dock.blueprint, targetPanel) {
		return false
	}
	next := e.dock.blueprint.Clone()
	detachDockPanel(&next, key)
	p, g := findDockPanel(&next, targetPanel)
	if g == nil {
		return false
	}
	g.Panels = append(g.Panels, key)
	g.Active = len(g.Panels) - 1
	if p != dock.Center {
		if r := next.Region(p); r != nil {
			r.Closed = false
		}
	}
	delete(e.dock.hidden, key)
	e.dock.blueprint = next
	e.dock.lastSyncKey = ""
	e.dock.rev++
	e.syncDockLayout(false)
	_ = e.SaveLayout()
	e.notify()
	return true
}

// PanelVisible reports whether panel key is currently shown in the dock area.
func (e *Editor) PanelVisible(key string) bool {
	if e == nil {
		return false
	}
	e.ensureDockLayout()
	e.syncDockLayout(false)
	return e.panelVisibleInActive(key)
}

func (e *Editor) panelVisibleInActive(key string) bool {
	p, g := findDockPanel(&e.dock.lastActive, key)
	if g == nil {
		return false
	}
	if p != dock.Center {
		if r := e.dock.lastActive.Region(p); r != nil && r.Closed {
			return false
		}
	}
	return true
}

// SetPanelVisible shows or hides panel key, persisting the layout when LayoutPath is set.
func (e *Editor) SetPanelVisible(key string, visible bool) {
	if e == nil || key == "" || key == PanelViewport || !e.isKnownPanel(key) {
		return
	}
	e.ensureDockLayout()
	if spec, ok := builtinSpec(key); ok && !spec.persistent {
		e.setToggleablePanelOpen(key, visible)
		if visible && e.availablePanels()[key] {
			e.ensurePanelInBlueprint(key)
			activatePanelInLayout(&e.dock.blueprint, key)
		}
		e.dock.lastSyncKey = ""
		e.dock.rev++
		e.syncDockLayout(false)
		_ = e.SaveLayout()
		e.notify()
		return
	}
	if visible {
		delete(e.dock.hidden, key)
		e.ensurePanelInBlueprint(key)
		activatePanelInLayout(&e.dock.blueprint, key)
	} else {
		if e.dock.hidden == nil {
			e.dock.hidden = map[string]bool{}
		}
		e.dock.hidden[key] = true
	}
	e.dock.lastSyncKey = ""
	e.dock.rev++
	e.syncDockLayout(false)
	_ = e.SaveLayout()
	e.notify()
}

// TogglePanel toggles panel key's visibility in the dock area.
func (e *Editor) TogglePanel(key string) {
	if e == nil {
		return
	}
	e.SetPanelVisible(key, !e.PanelVisible(key))
}

// revealPanelIfHiddenOrInactive ensures panel key is shown and active in its tab group
// without rewriting the layout when it is already visible and selected.
func (e *Editor) revealPanelIfHiddenOrInactive(key string) {
	if e == nil || !e.dock.initialized {
		return
	}
	if !e.PanelVisible(key) {
		e.SetPanelVisible(key, true)
		return
	}
	if _, g := findDockPanel(&e.dock.lastActive, key); g != nil {
		if idx := slices.Index(g.Panels, key); idx >= 0 && g.Active != idx {
			e.SetPanelVisible(key, true)
		}
	}
}

// Panels returns the list of user-toggleable dock panels (built-in panels and active plugin panels) in menu order.
func (e *Editor) Panels() []PanelInfo {
	if e == nil {
		return nil
	}
	e.ensureDockLayout()
	e.syncDockLayout(false)
	out := make([]PanelInfo, 0, len(builtinPanelSpecs)+4)
	for _, spec := range builtinPanelSpecs {
		if !spec.closable {
			continue
		}
		if spec.key == PanelLibrary && e.Library == nil {
			continue
		}
		out = append(out, PanelInfo{
			Key:         spec.key,
			Title:       spec.title,
			DefaultDock: spec.defaultDock,
			Visible:     e.panelVisibleInActive(spec.key),
			Available:   e.canTogglePanel(spec.key),
			Closable:    spec.closable,
		})
	}
	for _, p := range e.activePlugins() {
		for _, pn := range p.Panels {
			k := PluginPanelKey(p.Name, pn.ID)
			out = append(out, PanelInfo{
				Key:         k,
				Title:       e.translatePluginText(p, pn.TitleKey, pn.Title),
				DefaultDock: pn.Dock,
				Visible:     e.panelVisibleInActive(k),
				Available:   true,
				Closable:    true,
				Plugin:      p.Name,
				PluginID:    pn.ID,
			})
		}
	}
	return out
}

func (e *Editor) handleClosedDockPanel(key string) {
	if spec, ok := builtinSpec(key); ok && !spec.persistent {
		e.setToggleablePanelOpen(key, false)
		return
	}
	if e.dock.hidden == nil {
		e.dock.hidden = map[string]bool{}
	}
	e.dock.hidden[key] = true
}

func (e *Editor) setToggleablePanelOpen(key string, open bool) {
	switch key {
	case PanelOutput:
		e.SetOutputPanelOpen(open)
	case PanelProfiler:
		e.SetProfilerOpen(open)
	case PanelSearch:
		e.SetSearchOpen(open)
	case PanelNavMesh:
		e.SetNavMeshPanelOpen(open)
	case PanelEnvironment:
		e.SetEnvironmentPanelOpen(open)
	case PanelLocalization:
		if open {
			e.report(e.openDefaultLocalizations())
		} else {
			e.report(e.CloseLocalizations())
		}
	case PanelParticles:
		if open {
			if !e.ParticleEmitterOpen() {
				e.report(e.NewParticleEmitter())
			}
		} else {
			e.report(e.CloseParticleEmitter())
		}
	case PanelMaterial:
		if open {
			if !e.MaterialOpen() {
				if obj, _ := e.SelectedObject(); obj != nil && obj.Material != "" {
					e.report(e.OpenMaterial(obj.Material))
				} else {
					e.report(e.NewMaterial())
				}
			}
		} else {
			e.report(e.CloseMaterial())
		}
	case PanelTerrain:
		if !open {
			if e.Playing() {
				e.report(ErrPlaying)
				return
			}
			e.CloseTerrain()
		}
	case PanelRig:
		if !open {
			e.report(e.CloseRig())
		}
	case PanelAnimation:
		if !open {
			e.report(e.CloseClip())
		}
	case PanelStates:
		if !open {
			e.report(e.CloseStates())
		}
	case PanelTileset:
		if !open {
			e.report(e.CloseTileset())
		}
	case PanelDataTable:
		if !open {
			e.report(e.CloseDataTable())
		}
	}
}

func (e *Editor) canTogglePanel(key string) bool {
	hasProject := e.Project != nil && e.ProjectPath != ""
	switch key {
	case PanelFiles, PanelProblems, PanelSearch:
		return hasProject
	case PanelLibrary:
		return e.Library != nil
	case PanelAnimation:
		return e.clip != nil
	case PanelStates:
		return e.States() != nil
	case PanelTileset:
		return e.TilesetOpen()
	case PanelTerrain:
		return e.TerrainOpen() && !e.Playing()
	case PanelRig:
		return e.RigOpen()
	case PanelParticles, PanelMaterial:
		return !e.Playing()
	case PanelEnvironment:
		return e.Doc != nil
	case PanelDataTable:
		return e.table != nil
	case PanelLocalization:
		return !e.Playing()
	default:
		return true
	}
}

func (e *Editor) isKnownPanel(key string) bool {
	if _, ok := builtinSpec(key); ok {
		return true
	}
	_, _, ok := e.registeredPluginPanel(key)
	return ok
}

func (e *Editor) registeredPluginPanel(key string) (*Plugin, *Panel, bool) {
	if !strings.HasPrefix(key, "plugin:") {
		return nil, nil, false
	}
	for _, p := range e.plugins {
		for i := range p.Panels {
			if PluginPanelKey(p.Name, p.Panels[i].ID) == key {
				return p, &p.Panels[i], true
			}
		}
	}
	return nil, nil, false
}

func (e *Editor) activePluginPanel(key string) (*Plugin, *Panel, bool) {
	if !strings.HasPrefix(key, "plugin:") {
		return nil, nil, false
	}
	for _, p := range e.activePlugins() {
		for i := range p.Panels {
			if PluginPanelKey(p.Name, p.Panels[i].ID) == key {
				return p, &p.Panels[i], true
			}
		}
	}
	return nil, nil, false
}

func (e *Editor) panelTitle(key string) string {
	if spec, ok := builtinSpec(key); ok {
		return spec.title
	}
	if p, pn, ok := e.registeredPluginPanel(key); ok {
		return e.translatePluginText(p, pn.TitleKey, pn.Title)
	}
	return key
}

// dockSyncKey returns a lightweight signature of all editor state that can affect the dock layout,
// allowing EditorView's ui.DockArea and syncDockLayout to skip re-syncing and cloning on unrelated 60 Hz rev ticks.
func (e *Editor) dockSyncKey() string {
	var b strings.Builder
	var mask uint64
	if e.Project != nil && e.ProjectPath != "" {
		mask |= 1 << 0
	}
	if e.Library != nil {
		mask |= 1 << 1
	}
	if e.OutputPanelOpen() {
		mask |= 1 << 2
	}
	if e.ProfilerOpen() {
		mask |= 1 << 3
	}
	if e.SearchOpen() {
		mask |= 1 << 4
	}
	if e.clip != nil {
		mask |= 1 << 5
	}
	if e.States() != nil {
		mask |= 1 << 6
	}
	if e.TilesetOpen() {
		mask |= 1 << 7
	}
	if e.TerrainOpen() {
		mask |= 1 << 8
	}
	if e.RigOpen() {
		mask |= 1 << 9
	}
	if e.ParticleEmitterOpen() {
		mask |= 1 << 10
	}
	if e.EnvironmentPanelOpen() && e.Doc != nil {
		mask |= 1 << 11
	}
	if e.table != nil {
		mask |= 1 << 12
	}
	if e.NavMeshPanelOpen() {
		mask |= 1 << 13
	}
	if e.LocalizationOpen() {
		mask |= 1 << 14
	}
	if e.MaterialOpen() {
		mask |= 1 << 15
	}
	fmt.Fprintf(&b, "%d:%x:%d", e.dock.rev, mask, int(e.preferredInspectorWidth()))
	for _, p := range e.activePlugins() {
		for _, pn := range p.Panels {
			b.WriteByte('|')
			b.WriteString(p.Name)
			b.WriteByte(':')
			b.WriteString(pn.ID)
		}
	}
	return b.String()
}

// availablePanels returns every panel key that is currently open/eligible to be shown (before applying hidden).
func (e *Editor) availablePanels() map[string]bool {
	avail := map[string]bool{
		PanelViewport:  true,
		PanelSceneTree: true,
		PanelLayers:    true,
		PanelAssets:    true,
		PanelInspector: true,
	}
	if e.Project != nil && e.ProjectPath != "" {
		avail[PanelFiles] = true
		avail[PanelProblems] = true
	}
	if e.Library != nil {
		avail[PanelLibrary] = true
	}
	if e.OutputPanelOpen() {
		avail[PanelOutput] = true
	}
	if e.ProfilerOpen() {
		avail[PanelProfiler] = true
	}
	if e.Project != nil && e.ProjectPath != "" && e.SearchOpen() {
		avail[PanelSearch] = true
	}
	if e.clip != nil {
		avail[PanelAnimation] = true
	}
	if e.States() != nil {
		avail[PanelStates] = true
	}
	if e.TilesetOpen() {
		avail[PanelTileset] = true
	}
	if e.TerrainOpen() {
		avail[PanelTerrain] = true
	}
	if e.RigOpen() {
		avail[PanelRig] = true
	}
	if e.ParticleEmitterOpen() {
		avail[PanelParticles] = true
	}
	if e.MaterialOpen() {
		avail[PanelMaterial] = true
	}
	if e.EnvironmentPanelOpen() && e.Doc != nil {
		avail[PanelEnvironment] = true
	}
	if e.table != nil {
		avail[PanelDataTable] = true
	}
	if e.NavMeshPanelOpen() {
		avail[PanelNavMesh] = true
	}
	if e.LocalizationOpen() {
		avail[PanelLocalization] = true
	}
	for _, p := range e.activePlugins() {
		for _, pn := range p.Panels {
			avail[PluginPanelKey(p.Name, pn.ID)] = true
		}
	}
	return avail
}

// syncDockLayout reconciles newly available panels (contextual, library, plugins) into blueprint
// and computes lastActive for ui.DockArea.
func (e *Editor) syncDockLayout(onRegister bool) {
	if !e.dock.initialized {
		e.ensureDockLayout()
		return
	}
	syncKey := e.dockSyncKey()
	if !onRegister && len(e.dock.pluginHints) == 0 && e.dock.lastSyncKey == syncKey {
		return
	}
	// Restore any plugin hints for plugins registered after LoadLayout in deterministic key order.
	if len(e.dock.pluginHints) > 0 {
		hintKeys := make([]string, 0, len(e.dock.pluginHints))
		for k := range e.dock.pluginHints {
			hintKeys = append(hintKeys, k)
		}
		slices.Sort(hintKeys)
		for _, k := range hintKeys {
			hint := e.dock.pluginHints[k]
			if e.isKnownPanel(k) {
				if !dockLayoutHasPanel(&e.dock.blueprint, k) {
					insertPluginFromHint(&e.dock.blueprint, k, hint)
				}
				if hint.hidden {
					if e.dock.hidden == nil {
						e.dock.hidden = map[string]bool{}
					}
					e.dock.hidden[k] = true
				}
				delete(e.dock.pluginHints, k)
			}
		}
	}

	avail := e.availablePanels()
	// Ensure every currently available panel exists in blueprint; newly opened toggleable/contextual
	// panels activate their tab and open their dock, while persistent/plugin panels already placed
	// in blueprint preserve the user's saved active tab index.
	ordered := e.orderedAvailableKeys(avail)
	for _, k := range ordered {
		newlyAvailable := !e.dock.lastAvailable[k]
		if !dockLayoutHasPanel(&e.dock.blueprint, k) {
			e.ensurePanelInBlueprint(k)
			if !e.dock.hidden[k] {
				activatePanelInLayout(&e.dock.blueprint, k)
			}
		} else if newlyAvailable && !onRegister && !e.dock.hidden[k] {
			spec, isBuiltin := builtinSpec(k)
			if isBuiltin && !spec.persistent {
				activatePanelInLayout(&e.dock.blueprint, k)
			} else if p, _ := findDockPanel(&e.dock.blueprint, k); p != dock.Center {
				if r := e.dock.blueprint.Region(p); r != nil && r.Closed && !e.dock.userClosedRegion[p] {
					r.Closed = false
				}
			}
		}
	}
	e.dock.lastAvailable = avail

	// Project blueprint to active panels only (excluding hidden and unavailable panels).
	active := filterDockLayout(e.dock.blueprint, func(k string) bool {
		if k == PanelViewport {
			return true
		}
		return avail[k] && !e.dock.hidden[k]
	})
	// When the user has not explicitly sized the Bottom dock, adapt its height to the active bottom panel's preferred height.
	if !e.dock.userSizedBottom && active.Bottom != nil && !active.Bottom.Closed {
		want := defaultBottomDockSize
		for _, k := range dockLayoutPanels(&dock.Layout{Bottom: active.Bottom}) {
			if h := preferredBottomHeight(k); h > want {
				want = h
			}
		}
		active.Bottom.Size = want
	}
	if !e.dock.userSizedInspector {
		setInspectorSplitSize(&active, e.preferredInspectorWidth())
	}
	e.dock.lastActive = active
	e.dock.lastSyncKey = syncKey
}

func (e *Editor) orderedAvailableKeys(avail map[string]bool) []string {
	out := make([]string, 0, len(avail))
	for _, spec := range builtinPanelSpecs {
		if avail[spec.key] {
			out = append(out, spec.key)
		}
	}
	for _, p := range e.activePlugins() {
		for _, pn := range p.Panels {
			k := PluginPanelKey(p.Name, pn.ID)
			if avail[k] {
				out = append(out, k)
			}
		}
	}
	return out
}

func (e *Editor) ensurePanelInBlueprint(key string) {
	if dockLayoutHasPanel(&e.dock.blueprint, key) {
		return
	}
	if _, pn, ok := e.registeredPluginPanel(key); ok {
		insertPluginPanelDefault(&e.dock.blueprint, key, pn.Dock)
		return
	}
	spec, ok := builtinSpec(key)
	if !ok {
		return
	}
	switch key {
	case PanelViewport:
		e.dock.blueprint.Center = insertSplitOrTab(e.dock.blueprint.Center, key, base.Horizontal, true, 0)
	case PanelInspector:
		e.dock.blueprint.Center = insertSplitOrTab(e.dock.blueprint.Center, key, base.Horizontal, false, defaultInspectorSize)
	case PanelLibrary:
		if e.dock.blueprint.Left == nil {
			e.dock.blueprint.Left = &dock.Region{Root: dock.Tabs(key), Size: defaultAssetsColSize}
		} else {
			e.dock.blueprint.Left.Root = insertSplitOrTab(e.dock.blueprint.Left.Root, key, base.Horizontal, false, defaultAssetsColSize)
		}
	case PanelSceneTree, PanelLayers, PanelAssets, PanelFiles:
		if e.dock.blueprint.Left == nil {
			e.dock.blueprint.Left = &dock.Region{Root: dock.Tabs(key), Size: defaultAssetsColSize}
		} else {
			addTabToFirstGroup(&e.dock.blueprint.Left.Root, key)
		}
	default:
		switch spec.defaultDock {
		case DockLeft:
			if e.dock.blueprint.Left == nil {
				e.dock.blueprint.Left = &dock.Region{Root: dock.Tabs(key), Size: defaultAssetsColSize}
			} else {
				addTabToFirstGroup(&e.dock.blueprint.Left.Root, key)
			}
		case DockRight:
			if e.dock.blueprint.Right == nil {
				e.dock.blueprint.Right = &dock.Region{Root: dock.Tabs(key), Size: defaultRightDockSize}
			} else {
				addTabToFirstGroup(&e.dock.blueprint.Right.Root, key)
			}
		default:
			if e.dock.blueprint.Bottom == nil {
				e.dock.blueprint.Bottom = &dock.Region{Root: dock.Tabs(key), Size: defaultBottomDockSize}
			} else {
				addTabToFirstGroup(&e.dock.blueprint.Bottom.Root, key)
			}
		}
	}
}

// insertPluginPanelDefault places a plugin panel at its declared Dock on first use so it is
// immediately visible alongside existing panels (and mouse-draggable out of its group).
func insertPluginPanelDefault(l *dock.Layout, key string, d Dock) {
	switch d {
	case DockLeft:
		if l.Left == nil {
			l.Left = &dock.Region{Root: dock.Tabs(key), Size: defaultPluginSize}
			return
		}
		l.Left.Closed = false
		l.Left.Root = insertSplitOrTab(l.Left.Root, key, base.Horizontal, false, defaultPluginSize)
	case DockRight:
		// Split right of the inspector inside Center so both inspector and plugin panels are
		// visible simultaneously and every plugin panel can be dragged with the mouse out of its slot.
		l.Center = insertSplitOrTab(l.Center, key, base.Horizontal, false, defaultPluginSize)
	case DockBottom:
		if l.Bottom == nil {
			l.Bottom = &dock.Region{Root: dock.Tabs(key), Size: defaultBottomDockSize}
			return
		}
		l.Bottom.Closed = false
		l.Bottom.Root = insertSplitOrTab(l.Bottom.Root, key, base.Horizontal, false, 0)
	}
}

func insertPluginFromHint(l *dock.Layout, key string, hint savedPluginHint) {
	for _, sib := range hint.siblings {
		if _, g := findDockPanel(l, sib); g != nil {
			g.Panels = append(g.Panels, key)
			return
		}
	}
	switch hint.region {
	case dock.Left:
		if l.Left == nil {
			l.Left = &dock.Region{Root: dock.Tabs(key), Size: defaultPluginSize}
		} else {
			addTabToFirstGroup(&l.Left.Root, key)
		}
	case dock.Right:
		if l.Right == nil {
			l.Right = &dock.Region{Root: dock.Tabs(key), Size: defaultRightDockSize}
		} else {
			addTabToFirstGroup(&l.Right.Root, key)
		}
	case dock.Bottom:
		if l.Bottom == nil {
			l.Bottom = &dock.Region{Root: dock.Tabs(key), Size: defaultBottomDockSize}
		} else {
			addTabToFirstGroup(&l.Bottom.Root, key)
		}
	default:
		l.Center = insertSplitOrTab(l.Center, key, base.Horizontal, false, defaultPluginSize)
	}
}

func insertSplitOrTab(root dock.Node, key string, axis base.Axis, before bool, size core.Points) dock.Node {
	leaf := dock.Tabs(key)
	if !hasAnyPanel(&root) {
		return leaf
	}
	if !root.Tabs && root.Axis == axis {
		sizes := root.Sizes
		if len(sizes) != len(root.Children) {
			sizes = make([]core.Points, len(root.Children))
		}
		if before {
			root.Children = append([]dock.Node{leaf}, root.Children...)
			root.Sizes = append([]core.Points{size}, sizes...)
		} else {
			root.Children = append(root.Children, leaf)
			root.Sizes = append(sizes, size)
		}
		return root
	}
	if before {
		return dock.Node{Axis: axis, Children: []dock.Node{leaf, root}, Sizes: []core.Points{size, 0}}
	}
	return dock.Node{Axis: axis, Children: []dock.Node{root, leaf}, Sizes: []core.Points{0, size}}
}

func addTabToFirstGroup(n *dock.Node, key string) bool {
	if n == nil {
		return false
	}
	if n.Tabs {
		if !slices.Contains(n.Panels, key) {
			n.Panels = append(n.Panels, key)
		}
		return true
	}
	for i := range n.Children {
		if addTabToFirstGroup(&n.Children[i], key) {
			return true
		}
	}
	return false
}

func stripEmptyRegions(l dock.Layout) dock.Layout {
	if l.Left != nil && !hasAnyPanel(&l.Left.Root) {
		l.Left = nil
	}
	if l.Right != nil && !hasAnyPanel(&l.Right.Root) {
		l.Right = nil
	}
	if l.Bottom != nil && !hasAnyPanel(&l.Bottom.Root) {
		l.Bottom = nil
	}
	return l
}

// reinsertPreservedPanel keeps an inactive or hidden panel's position in dst from old.
func reinsertPreservedPanel(dst, old *dock.Layout, key string) {
	if dockLayoutHasPanel(dst, key) {
		return
	}
	p, oldGroup := findDockPanel(old, key)
	if oldGroup == nil {
		return
	}
	// First try to keep it in the same tab group as any remaining sibling.
	for _, sib := range oldGroup.Panels {
		if sib == key {
			continue
		}
		if _, g := findDockPanel(dst, sib); g != nil {
			g.Panels = append(g.Panels, key)
			return
		}
	}
	// Otherwise keep it in its original region (preserving vertical split for assets/files or scene/layers if present).
	switch p {
	case dock.Center:
		size := core.Points(0)
		if key == PanelInspector {
			size = defaultInspectorSize
		}
		dst.Center = insertSplitOrTab(dst.Center, key, base.Horizontal, false, size)
	case dock.Left:
		if dst.Left == nil {
			sz := defaultLeftDockSize
			closed := false
			if old.Left != nil {
				sz, closed = old.Left.Size, old.Left.Closed
			}
			dst.Left = &dock.Region{Root: dock.Tabs(key), Size: sz, Closed: closed}
			return
		}
		// Keep Files under Assets and Layers under Scene when split in Left.
		if key == PanelFiles {
			if splitBesidePanel(&dst.Left.Root, PanelAssets, key, base.Vertical, 0) {
				return
			}
		} else if key == PanelLayers {
			if splitBesidePanel(&dst.Left.Root, PanelSceneTree, key, base.Vertical, defaultLayersSize) {
				return
			}
		}
		addTabToFirstGroup(&dst.Left.Root, key)
	case dock.Right:
		if dst.Right == nil {
			sz := defaultRightDockSize
			closed := false
			if old.Right != nil {
				sz, closed = old.Right.Size, old.Right.Closed
			}
			dst.Right = &dock.Region{Root: dock.Tabs(key), Size: sz, Closed: closed}
			return
		}
		addTabToFirstGroup(&dst.Right.Root, key)
	case dock.Bottom:
		if dst.Bottom == nil {
			sz := defaultBottomDockSize
			closed := true
			if old.Bottom != nil {
				sz, closed = old.Bottom.Size, old.Bottom.Closed
			}
			dst.Bottom = &dock.Region{Root: dock.Tabs(key), Size: sz, Closed: closed}
			return
		}
		addTabToFirstGroup(&dst.Bottom.Root, key)
	}
}

func splitBesidePanel(n *dock.Node, anchor, key string, axis base.Axis, size core.Points) bool {
	if n == nil {
		return false
	}
	if n.Tabs {
		if slices.Contains(n.Panels, anchor) {
			old := *n
			*n = dock.Node{Axis: axis, Children: []dock.Node{old, dock.Tabs(key)}, Sizes: []core.Points{0, size}}
			return true
		}
		return false
	}
	for i := range n.Children {
		if splitBesidePanel(&n.Children[i], anchor, key, axis, size) {
			return true
		}
	}
	return false
}
