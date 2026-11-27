package mcp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/automation"
)

// Asset and hot-reload tool names registered by Bridge with github.com/nsgo-dev/nsgo/automation.
const (
	// ToolAssetsList lists asset groups, asset keys/types, load states, and estimated memory usage in bytes.
	ToolAssetsList = "game_assets_list"
	// ToolAssetReload reloads a single named asset, URI, or path (or all loaded assets) in place.
	ToolAssetReload = "game_asset_reload"
	// ToolAssetsReloadGroup reloads every loaded asset in a named asset group in place.
	ToolAssetsReloadGroup = "game_assets_reload_group"
)

var (
	// ErrEmptyAssetKey reports registering a custom asset with an empty key.
	ErrEmptyAssetKey = errors.New("mcp: empty asset key")
	// ErrDuplicateAsset reports registering a custom asset key that is already registered on the Bridge.
	ErrDuplicateAsset = errors.New("mcp: duplicate asset registration")
)

// CustomAsset registers a game-managed asset outside assets.Manager (such as a standalone sprite.Atlas, scene file, or data table) so game_assets_list reports its memory and game_asset_reload / game_assets_reload_group can reload it on demand.
type CustomAsset struct {
	// Key is the unique asset identifier (required).
	Key string
	// Group is the asset group name reported by game_assets_list (defaults to "custom" when empty).
	Group string
	// Kind describes the asset type (e.g. "atlas", "image", "scene", "shader", "material", "sound", "bytes", "custom").
	Kind string
	// URI is an optional source path or URI shown in game_assets_list and matched by game_asset_reload.
	URI string
	// Bytes returns the asset's current estimated memory footprint in bytes (optional).
	Bytes func() int64
	// Frames returns the current frame count for atlas-like assets (optional).
	Frames func() int
	// Reload reloads the asset in place (optional).
	Reload func(ctx context.Context) error
}

type assetRegistry struct {
	entries map[string]CustomAsset
	order   []string
}

// AssetEntry describes one asset within an asset group returned by game_assets_list.
type AssetEntry struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	URI      string `json:"uri,omitempty"`
	ImageURI string `json:"image_uri,omitempty"`
	Loaded   bool   `json:"loaded"`
	Bytes    int64  `json:"bytes"`
	Frames   int    `json:"frames,omitempty"`
}

// AssetGroupSummary describes one asset group and its estimated memory usage returned by game_assets_list.
type AssetGroupSummary struct {
	Name   string       `json:"name"`
	Source string       `json:"source"`
	State  string       `json:"state"`
	Loaded int          `json:"loaded"`
	Total  int          `json:"total"`
	Bytes  int64        `json:"bytes"`
	Error  string       `json:"error,omitempty"`
	Assets []AssetEntry `json:"assets"`
}

// AssetsListResult is the JSON result of game_assets_list.
type AssetsListResult struct {
	Revision   int                 `json:"revision,omitempty"`
	GroupCount int                 `json:"group_count"`
	AssetCount int                 `json:"asset_count"`
	TotalBytes int64               `json:"total_bytes"`
	Groups     []AssetGroupSummary `json:"groups"`
}

// ReloadedItem describes one asset touched during a reload operation.
type ReloadedItem struct {
	Key    string `json:"key"`
	Group  string `json:"group,omitempty"`
	Kind   string `json:"kind,omitempty"`
	URI    string `json:"uri,omitempty"`
	Bytes  int64  `json:"bytes"`
	Frames int    `json:"frames,omitempty"`
	// Skipped says why the asset was left unchanged without an error, such as a sound or an image with no file behind it.
	Skipped string `json:"skipped,omitempty"`
	Error   string `json:"error,omitempty"`
}

func reloadedItem(r assets.ReloadedAsset) ReloadedItem {
	item := ReloadedItem{
		Key:     r.Name,
		Group:   r.Group,
		Kind:    r.Kind,
		URI:     r.URI,
		Bytes:   r.Bytes,
		Frames:  r.Frames,
		Skipped: r.Skipped,
	}
	if r.Err != nil {
		item.Error = r.Err.Error()
	}
	return item
}

// anyReloaded reports whether at least one item was reloaded rather than skipped or failed.
func anyReloaded(items []ReloadedItem) bool {
	for _, it := range items {
		if it.Error == "" && it.Skipped == "" {
			return true
		}
	}
	return false
}

// AssetReloadResult is the JSON result of game_asset_reload.
type AssetReloadResult struct {
	Reloaded   bool           `json:"reloaded"`
	Key        string         `json:"key,omitempty"`
	Group      string         `json:"group,omitempty"`
	Kind       string         `json:"kind,omitempty"`
	Bytes      int64          `json:"bytes"`
	DurationMS float64        `json:"duration_ms"`
	Revision   int            `json:"revision,omitempty"`
	Items      []ReloadedItem `json:"items,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// AssetsReloadGroupResult is the JSON result of game_assets_reload_group.
type AssetsReloadGroupResult struct {
	Group      string         `json:"group"`
	Reloaded   bool           `json:"reloaded"`
	Count      int            `json:"count"`
	Failed     int            `json:"failed,omitempty"`
	Skipped    int            `json:"skipped,omitempty"`
	Bytes      int64          `json:"bytes"`
	DurationMS float64        `json:"duration_ms"`
	Revision   int            `json:"revision,omitempty"`
	Items      []ReloadedItem `json:"items"`
	Error      string         `json:"error,omitempty"`
}

var (
	schemaAssetsList = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"group": {
				"type": "string",
				"description": "Filter to a specific asset group name."
			},
			"state": {
				"type": "string",
				"enum": ["loaded", "loading", "unloaded", "failed"],
				"description": "Filter asset groups by lifecycle state."
			},
			"kind": {
				"type": "string",
				"description": "Filter listed assets by kind (for example \"atlas\", \"image\", \"sound\", \"bytes\", \"custom\", \"shader\", \"material\")."
			}
		}
	}`)

	schemaAssetReload = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"key": {
				"type": "string",
				"description": "Asset name, URI (such as \"asset:sprites/hero.json\"), or relative path to reload. Required unless \"all\" is true."
			},
			"group": {
				"type": "string",
				"description": "Optional asset group name to scope the reload."
			},
			"all": {
				"type": "boolean",
				"description": "Reload every loaded asset across all configured groups and reloaders (default false)."
			}
		}
	}`)

	schemaAssetsReloadGroup = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["group"],
		"properties": {
			"group": {
				"type": "string",
				"description": "Name of the asset group to reload (from game_assets_list)."
			}
		}
	}`)
)

// SetAssets replaces the static Assets manager configured on b. If Config.AssetsFunc is set and returns a non-nil manager, AssetsFunc continues to take precedence.
func (b *Bridge) SetAssets(m *assets.Manager) {
	if b == nil {
		return
	}
	b.cfg.Assets = m
}

// RegisterAsset registers a custom game-managed asset on b for game_assets_list and game_asset_reload.
func (b *Bridge) RegisterAsset(asset CustomAsset) error {
	if b == nil {
		return ErrNilBridge
	}
	if strings.TrimSpace(asset.Key) == "" {
		return ErrEmptyAssetKey
	}
	if asset.Group == "" {
		asset.Group = "custom"
	}
	if asset.Kind == "" {
		asset.Kind = "custom"
	}
	if b.customAssets == nil {
		b.customAssets = &assetRegistry{entries: make(map[string]CustomAsset)}
	}
	if _, dup := b.customAssets.entries[asset.Key]; dup {
		return fmt.Errorf("mcp: asset %q: %w", asset.Key, ErrDuplicateAsset)
	}
	b.customAssets.entries[asset.Key] = asset
	b.customAssets.order = append(b.customAssets.order, asset.Key)
	slices.Sort(b.customAssets.order)
	return nil
}

// RegisterAtlas registers a standalone live *sprite.Atlas (not owned by an assets.Manager) on b so game_assets_list reports its frame count and estimated memory, and game_asset_reload re-parses jsonPath via readFile and updates *atlas in place without replacing the pointer.
func (b *Bridge) RegisterAtlas(key, group, jsonPath string, atlas *sprite.Atlas, readFile func(ctx context.Context, path string) ([]byte, error)) error {
	if b == nil {
		return ErrNilBridge
	}
	if atlas == nil {
		return errors.New("mcp: nil atlas")
	}
	if group == "" {
		group = "custom"
	}
	var lastJSONBytes int64
	return b.RegisterAsset(CustomAsset{
		Key:   key,
		Group: group,
		Kind:  "atlas",
		URI:   jsonPath,
		Bytes: func() int64 {
			return assets.EstimateAtlasBytes(atlas, lastJSONBytes)
		},
		Frames: func() int {
			return len(atlas.Frames)
		},
		Reload: func(ctx context.Context) error {
			if readFile == nil {
				return fmt.Errorf("mcp: atlas %q has no file reader configured", key)
			}
			data, err := readFile(ctx, jsonPath)
			if err != nil {
				return fmt.Errorf("mcp: atlas %q: read %q: %w", key, jsonPath, err)
			}
			next, err := sprite.LoadAtlas(atlas.Image, data)
			if err != nil {
				return fmt.Errorf("mcp: atlas %q: %w", key, loaderr.WithFile(err, jsonPath))
			}
			*atlas = *next
			lastJSONBytes = int64(len(data))
			return nil
		},
	})
}

func (b *Bridge) assetTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolAssetsList,
			Description: "List loaded asset groups, their load states, asset keys/types, and estimated memory usage in bytes.",
			InputSchema: schemaAssetsList,
			Handler:     b.handleAssetsList,
		},
		{
			Name:        ToolAssetReload,
			Description: "Reload a named asset, URI, or relative path (atlas, image, bytes, custom asset, shader, material, or resource) in place, or reload all loaded assets when all=true. Surfaces parse/compile errors cleanly without crashing.",
			InputSchema: schemaAssetReload,
			Handler:     b.handleAssetReload,
		},
		{
			Name:        ToolAssetsReloadGroup,
			Description: "Reload every loaded asset in a named asset group in place, reporting per-asset results and any parse or load errors.",
			InputSchema: schemaAssetsReloadGroup,
			Handler:     b.handleAssetsReloadGroup,
		},
	}
}

func (b *Bridge) activeAssets() *assets.Manager {
	if b == nil {
		return nil
	}
	if b.cfg.AssetsFunc != nil {
		if m := b.cfg.AssetsFunc(); m != nil {
			return m
		}
	}
	return b.cfg.Assets
}

func (b *Bridge) hasAnyAssetSource() bool {
	return b.activeAssets() != nil ||
		b.cfg.Resources != nil ||
		b.cfg.Shaders != nil ||
		b.cfg.Materials != nil ||
		(b.customAssets != nil && len(b.customAssets.entries) > 0)
}

func (b *Bridge) hasAnyReloadSource() bool {
	return b.hasAnyAssetSource() || b.cfg.ReloadAsset != nil
}

type assetsListArgs struct {
	Group string `json:"group"`
	State string `json:"state"`
	Kind  string `json:"kind"`
}

func (b *Bridge) handleAssetsList(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[assetsListArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.State != "" {
		switch args.State {
		case assets.StateLoaded, assets.StateLoading, assets.StateUnloaded, assets.StateFailed:
		default:
			return nil, fmt.Errorf("mcp: invalid asset state %q (want loaded, loading, unloaded, or failed)", args.State)
		}
	}
	if !b.hasAnyAssetSource() {
		return nil, errors.New("mcp: no asset manager configured")
	}

	mgr := b.activeAssets()
	if mgr != nil && mgr.Disposed() {
		return nil, errors.New("mcp: asset manager is disposed")
	}

	var groups []AssetGroupSummary
	if mgr != nil {
		for _, gs := range mgr.GroupStats() {
			summary := AssetGroupSummary{
				Name:   gs.Name,
				Source: "manager",
				State:  gs.State,
				Loaded: gs.Loaded,
				Total:  gs.Total,
				Bytes:  gs.Bytes,
				Error:  gs.Error,
				Assets: make([]AssetEntry, 0, len(gs.Assets)),
			}
			for _, a := range gs.Assets {
				summary.Assets = append(summary.Assets, AssetEntry{
					Key:      a.Name,
					Kind:     a.Kind,
					URI:      a.URI,
					ImageURI: a.ImageURI,
					Loaded:   a.Loaded,
					Bytes:    a.Bytes,
					Frames:   a.Frames,
				})
			}
			slices.SortFunc(summary.Assets, func(a, b AssetEntry) int {
				return cmp.Compare(a.Key, b.Key)
			})
			groups = append(groups, summary)
		}
	}

	if b.cfg.Resources != nil {
		if paths := b.cfg.Resources.Paths(); len(paths) > 0 {
			summary := AssetGroupSummary{
				Name:   "resources",
				Source: "resources",
				State:  assets.StateLoaded,
				Loaded: len(paths),
				Total:  len(paths),
				Assets: make([]AssetEntry, 0, len(paths)),
			}
			for _, p := range paths {
				kind := "resource"
				if r, err := b.cfg.Resources.Load(p); err == nil && r != nil {
					kind = string(r.Kind())
				}
				summary.Assets = append(summary.Assets, AssetEntry{
					Key:    p,
					Kind:   kind,
					URI:    p,
					Loaded: true,
				})
			}
			groups = append(groups, summary)
		}
	}

	if b.cfg.Shaders != nil {
		if names := b.cfg.Shaders.ShaderNames(); len(names) > 0 {
			summary := AssetGroupSummary{
				Name:   "shaders",
				Source: "shaders",
				State:  assets.StateLoaded,
				Loaded: len(names),
				Total:  len(names),
				Assets: make([]AssetEntry, 0, len(names)),
			}
			for _, name := range names {
				p, _ := b.cfg.Shaders.ShaderPath(name)
				summary.Assets = append(summary.Assets, AssetEntry{
					Key:    name,
					Kind:   "shader",
					URI:    p,
					Loaded: true,
				})
			}
			groups = append(groups, summary)
		}
	}

	if b.cfg.Materials != nil {
		if names := b.cfg.Materials.MaterialNames(); len(names) > 0 {
			summary := AssetGroupSummary{
				Name:   "materials",
				Source: "materials",
				State:  assets.StateLoaded,
				Loaded: len(names),
				Total:  len(names),
				Assets: make([]AssetEntry, 0, len(names)),
			}
			for _, name := range names {
				p, _ := b.cfg.Materials.MaterialPath(name)
				summary.Assets = append(summary.Assets, AssetEntry{
					Key:    name,
					Kind:   "material",
					URI:    p,
					Loaded: true,
				})
			}
			groups = append(groups, summary)
		}
	}

	if b.customAssets != nil && len(b.customAssets.entries) > 0 {
		byGroup := make(map[string]*AssetGroupSummary)
		var groupNames []string
		for _, key := range b.customAssets.order {
			ca := b.customAssets.entries[key]
			gs, ok := byGroup[ca.Group]
			if !ok {
				gs = &AssetGroupSummary{
					Name:   ca.Group,
					Source: "custom",
					State:  assets.StateLoaded,
					Assets: []AssetEntry{},
				}
				byGroup[ca.Group] = gs
				groupNames = append(groupNames, ca.Group)
			}
			var sz int64
			if ca.Bytes != nil {
				sz = max(0, ca.Bytes())
			}
			var frames int
			if ca.Frames != nil {
				frames = max(0, ca.Frames())
			}
			gs.Loaded++
			gs.Total++
			gs.Bytes += sz
			gs.Assets = append(gs.Assets, AssetEntry{
				Key:    ca.Key,
				Kind:   ca.Kind,
				URI:    ca.URI,
				Loaded: true,
				Bytes:  sz,
				Frames: frames,
			})
		}
		slices.Sort(groupNames)
		for _, name := range groupNames {
			groups = append(groups, *byGroup[name])
		}
	}

	// Deterministic ordering by group name, then source.
	slices.SortFunc(groups, func(a, b AssetGroupSummary) int {
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.Source, b.Source)
	})

	filtered := make([]AssetGroupSummary, 0, len(groups))
	var (
		totalAssets int
		totalBytes  int64
	)
	for _, g := range groups {
		if args.Group != "" && g.Name != args.Group {
			continue
		}
		if args.State != "" && g.State != args.State {
			continue
		}
		if args.Kind != "" {
			matched := make([]AssetEntry, 0, len(g.Assets))
			var bytes int64
			var loaded int
			for _, a := range g.Assets {
				if a.Kind != args.Kind {
					continue
				}
				matched = append(matched, a)
				bytes += a.Bytes
				if a.Loaded {
					loaded++
				}
			}
			if len(matched) == 0 {
				continue
			}
			g.Assets = matched
			g.Bytes = bytes
			g.Loaded = loaded
			g.Total = len(matched)
		}
		totalAssets += len(g.Assets)
		totalBytes += g.Bytes
		filtered = append(filtered, g)
	}

	res := AssetsListResult{
		GroupCount: len(filtered),
		AssetCount: totalAssets,
		TotalBytes: totalBytes,
		Groups:     filtered,
	}
	if mgr != nil {
		res.Revision = mgr.Revision()
	}
	return res, nil
}

type assetReloadArgs struct {
	Key   string `json:"key"`
	Group string `json:"group"`
	All   bool   `json:"all"`
}

func (b *Bridge) handleAssetReload(ctx context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[assetReloadArgs](raw)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(args.Key)
	if args.All && key != "" {
		return nil, errors.New("mcp: key and all cannot both be set")
	}
	if !args.All && key == "" {
		return nil, errors.New("mcp: key is required (or set all: true)")
	}
	if !b.hasAnyReloadSource() {
		return nil, errors.New("mcp: no asset manager or reloader configured")
	}

	mgr := b.activeAssets()
	if mgr != nil && mgr.Disposed() {
		return nil, errors.New("mcp: asset manager is disposed")
	}

	start := time.Now()
	if args.All {
		if !b.hasAnyAssetSource() {
			return nil, errors.New("mcp: all: true needs an asset manager, reloader or registered asset; Config.ReloadAsset reloads a single key only")
		}
		return b.reloadAllAssets(ctx, mgr, args.Group, start)
	}

	if args.Group != "" && !b.groupExists(mgr, args.Group) {
		return nil, fmt.Errorf("mcp: asset group %q not found", args.Group)
	}

	var (
		items []ReloadedItem
		errs  []error
	)

	// 1. assets.Manager
	if mgr != nil && (args.Group == "" || hasManagerGroup(mgr, args.Group)) {
		reloaded, err := mgr.ReloadAsset(ctx, args.Group, key)
		if err != nil && len(reloaded) == 0 {
			return nil, err
		}
		for _, r := range reloaded {
			items = append(items, reloadedItem(r))
			if r.Err != nil {
				errs = append(errs, r.Err)
			}
		}
	}

	// 2. Registered custom assets
	if b.customAssets != nil {
		for _, caKey := range b.customAssets.order {
			ca := b.customAssets.entries[caKey]
			if args.Group != "" && ca.Group != args.Group {
				continue
			}
			if !matchCustomAsset(ca, key) {
				continue
			}
			item, err := reloadCustomAsset(ctx, ca)
			items = append(items, item)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	// 3. hotreload.MaterialReloader
	if b.cfg.Materials != nil && (args.Group == "" || args.Group == "materials") {
		if p, sz, tracked, err := b.cfg.Materials.ReloadMaterialFile(key); tracked {
			item := ReloadedItem{
				Key:   key,
				Group: "materials",
				Kind:  "material",
				URI:   p,
				Bytes: sz,
			}
			if err != nil {
				item.Error = err.Error()
				errs = append(errs, err)
			}
			items = append(items, item)
		}
	}

	// 4. hotreload.ShaderReloader
	if b.cfg.Shaders != nil && (args.Group == "" || args.Group == "shaders") {
		if p, sz, tracked, err := b.cfg.Shaders.ReloadShaderFile(key); tracked {
			item := ReloadedItem{
				Key:   key,
				Group: "shaders",
				Kind:  "shader",
				URI:   p,
				Bytes: sz,
			}
			if err != nil {
				item.Error = err.Error()
				errs = append(errs, err)
			}
			items = append(items, item)
		}
	}

	// 5. resource.Library
	if b.cfg.Resources != nil && (args.Group == "" || args.Group == "resources") {
		resKey := normalizeRelKey(key)
		if sz, loaded, err := b.cfg.Resources.ReloadFile(resKey); loaded {
			kind := "resource"
			if r, loadErr := b.cfg.Resources.Load(resKey); loadErr == nil && r != nil {
				kind = string(r.Kind())
			}
			item := ReloadedItem{
				Key:   resKey,
				Group: "resources",
				Kind:  kind,
				URI:   resKey,
				Bytes: sz,
			}
			if err != nil {
				item.Error = err.Error()
				errs = append(errs, err)
			}
			items = append(items, item)
		}
	}

	// 6. Custom ReloadAsset fallback hook
	if len(items) == 0 {
		if b.cfg.ReloadAsset != nil {
			err := b.cfg.ReloadAsset(ctx, key)
			res := AssetReloadResult{
				Reloaded:   err == nil,
				Key:        key,
				Group:      args.Group,
				DurationMS: elapsedMS(start),
			}
			if mgr != nil {
				res.Revision = mgr.Revision()
			}
			if err != nil {
				res.Error = err.Error()
			}
			return res, nil
		}
		if args.Group != "" {
			return nil, fmt.Errorf("mcp: asset %q not found in group %q", key, args.Group)
		}
		return nil, fmt.Errorf("mcp: asset %q not found", key)
	}

	res := AssetReloadResult{
		Reloaded:   len(errs) == 0 && anyReloaded(items),
		Key:        key,
		Group:      args.Group,
		DurationMS: elapsedMS(start),
		Items:      items,
	}
	var totalBytes int64
	for _, it := range items {
		totalBytes += it.Bytes
	}
	res.Bytes = totalBytes
	if len(items) == 1 {
		res.Key = items[0].Key
		res.Group = items[0].Group
		res.Kind = items[0].Kind
	} else if res.Group == "" {
		firstGroup := items[0].Group
		sameGroup := true
		for _, it := range items[1:] {
			if it.Group != firstGroup {
				sameGroup = false
				break
			}
		}
		if sameGroup {
			res.Group = firstGroup
		}
	}
	if mgr != nil {
		res.Revision = mgr.Revision()
	}
	if len(errs) > 0 {
		res.Error = errors.Join(errs...).Error()
	}
	return res, nil
}

func (b *Bridge) reloadAllAssets(ctx context.Context, mgr *assets.Manager, groupFilter string, start time.Time) (any, error) {
	if groupFilter != "" && !b.groupExists(mgr, groupFilter) {
		return nil, fmt.Errorf("mcp: asset group %q not found", groupFilter)
	}
	var (
		items []ReloadedItem
		errs  []error
	)

	if mgr != nil {
		groupNames := mgr.GroupNames()
		slices.Sort(groupNames)
		for _, gName := range groupNames {
			if groupFilter != "" && gName != groupFilter {
				continue
			}
			g, ok := mgr.LookupGroup(gName)
			if !ok || g.Stats().State != assets.StateLoaded {
				continue
			}
			reloaded, _ := mgr.ReloadGroup(ctx, gName)
			for _, r := range reloaded {
				items = append(items, reloadedItem(r))
				if r.Err != nil {
					errs = append(errs, r.Err)
				}
			}
		}
	}

	if b.customAssets != nil {
		for _, caKey := range b.customAssets.order {
			ca := b.customAssets.entries[caKey]
			if groupFilter != "" && ca.Group != groupFilter {
				continue
			}
			if ca.Reload == nil {
				continue
			}
			item, err := reloadCustomAsset(ctx, ca)
			items = append(items, item)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	if b.cfg.Shaders != nil && (groupFilter == "" || groupFilter == "shaders") {
		seen := make(map[string]bool)
		for _, name := range b.cfg.Shaders.ShaderNames() {
			p, ok := b.cfg.Shaders.ShaderPath(name)
			if !ok || seen[p] {
				continue
			}
			seen[p] = true
			p, sz, tracked, err := b.cfg.Shaders.ReloadShaderFile(name)
			if !tracked {
				continue
			}
			item := ReloadedItem{
				Key:   name,
				Group: "shaders",
				Kind:  "shader",
				URI:   p,
				Bytes: sz,
			}
			if err != nil {
				item.Error = err.Error()
				errs = append(errs, err)
			}
			items = append(items, item)
		}
	}

	if b.cfg.Materials != nil && (groupFilter == "" || groupFilter == "materials") {
		seen := make(map[string]bool)
		for _, name := range b.cfg.Materials.MaterialNames() {
			p, ok := b.cfg.Materials.MaterialPath(name)
			if !ok || seen[p] {
				continue
			}
			seen[p] = true
			p, sz, tracked, err := b.cfg.Materials.ReloadMaterialFile(name)
			if !tracked {
				continue
			}
			item := ReloadedItem{
				Key:   name,
				Group: "materials",
				Kind:  "material",
				URI:   p,
				Bytes: sz,
			}
			if err != nil {
				item.Error = err.Error()
				errs = append(errs, err)
			}
			items = append(items, item)
		}
	}

	if b.cfg.Resources != nil && (groupFilter == "" || groupFilter == "resources") {
		for _, p := range b.cfg.Resources.Paths() {
			sz, loaded, err := b.cfg.Resources.ReloadFile(p)
			if !loaded {
				continue
			}
			kind := "resource"
			if r, loadErr := b.cfg.Resources.Load(p); loadErr == nil && r != nil {
				kind = string(r.Kind())
			}
			item := ReloadedItem{
				Key:   p,
				Group: "resources",
				Kind:  kind,
				URI:   p,
				Bytes: sz,
			}
			if err != nil {
				item.Error = err.Error()
				errs = append(errs, err)
			}
			items = append(items, item)
		}
	}

	var totalBytes int64
	for _, it := range items {
		totalBytes += it.Bytes
	}
	res := AssetReloadResult{
		Reloaded:   len(errs) == 0 && anyReloaded(items),
		Group:      groupFilter,
		Bytes:      totalBytes,
		DurationMS: elapsedMS(start),
		Items:      items,
	}
	if mgr != nil {
		res.Revision = mgr.Revision()
	}
	if len(errs) > 0 {
		res.Error = errors.Join(errs...).Error()
	}
	return res, nil
}

type assetsReloadGroupArgs struct {
	Group string `json:"group"`
}

func (b *Bridge) handleAssetsReloadGroup(ctx context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[assetsReloadGroupArgs](raw)
	if err != nil {
		return nil, err
	}
	groupName := strings.TrimSpace(args.Group)
	if groupName == "" {
		return nil, errors.New("mcp: group is required")
	}
	if !b.hasAnyAssetSource() {
		return nil, errors.New("mcp: no asset manager configured")
	}

	mgr := b.activeAssets()
	if mgr != nil && mgr.Disposed() {
		return nil, errors.New("mcp: asset manager is disposed")
	}

	start := time.Now()
	var (
		items   []ReloadedItem
		errs    []error
		matched bool
	)

	if hasManagerGroup(mgr, groupName) {
		matched = true
		reloaded, err := mgr.ReloadGroup(ctx, groupName)
		if err != nil && len(reloaded) == 0 {
			return nil, fmt.Errorf("mcp: %w", err)
		}
		for _, r := range reloaded {
			items = append(items, reloadedItem(r))
			if r.Err != nil {
				errs = append(errs, r.Err)
			}
		}
	}

	if b.customAssets != nil {
		for _, caKey := range b.customAssets.order {
			ca := b.customAssets.entries[caKey]
			if ca.Group != groupName {
				continue
			}
			matched = true
			item, err := reloadCustomAsset(ctx, ca)
			items = append(items, item)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	if groupName == "shaders" && b.cfg.Shaders != nil && len(b.cfg.Shaders.ShaderNames()) > 0 {
		matched = true
		seen := make(map[string]bool)
		for _, name := range b.cfg.Shaders.ShaderNames() {
			p, ok := b.cfg.Shaders.ShaderPath(name)
			if !ok || seen[p] {
				continue
			}
			seen[p] = true
			p, sz, tracked, err := b.cfg.Shaders.ReloadShaderFile(name)
			if !tracked {
				continue
			}
			item := ReloadedItem{
				Key:   name,
				Group: "shaders",
				Kind:  "shader",
				URI:   p,
				Bytes: sz,
			}
			if err != nil {
				item.Error = err.Error()
				errs = append(errs, err)
			}
			items = append(items, item)
		}
	}

	if groupName == "materials" && b.cfg.Materials != nil && len(b.cfg.Materials.MaterialNames()) > 0 {
		matched = true
		seen := make(map[string]bool)
		for _, name := range b.cfg.Materials.MaterialNames() {
			p, ok := b.cfg.Materials.MaterialPath(name)
			if !ok || seen[p] {
				continue
			}
			seen[p] = true
			p, sz, tracked, err := b.cfg.Materials.ReloadMaterialFile(name)
			if !tracked {
				continue
			}
			item := ReloadedItem{
				Key:   name,
				Group: "materials",
				Kind:  "material",
				URI:   p,
				Bytes: sz,
			}
			if err != nil {
				item.Error = err.Error()
				errs = append(errs, err)
			}
			items = append(items, item)
		}
	}

	if groupName == "resources" && b.cfg.Resources != nil && len(b.cfg.Resources.Paths()) > 0 {
		matched = true
		for _, p := range b.cfg.Resources.Paths() {
			sz, loaded, err := b.cfg.Resources.ReloadFile(p)
			if !loaded {
				continue
			}
			kind := "resource"
			if r, loadErr := b.cfg.Resources.Load(p); loadErr == nil && r != nil {
				kind = string(r.Kind())
			}
			item := ReloadedItem{
				Key:   p,
				Group: "resources",
				Kind:  kind,
				URI:   p,
				Bytes: sz,
			}
			if err != nil {
				item.Error = err.Error()
				errs = append(errs, err)
			}
			items = append(items, item)
		}
	}

	if !matched {
		return nil, fmt.Errorf("mcp: asset group %q not found", groupName)
	}

	if items == nil {
		items = []ReloadedItem{}
	}
	var (
		totalBytes int64
		succeeded  int
		failed     int
		skipped    int
	)
	for _, it := range items {
		totalBytes += it.Bytes
		switch {
		case it.Error != "":
			failed++
		case it.Skipped != "":
			skipped++
		default:
			succeeded++
		}
	}

	res := AssetsReloadGroupResult{
		Group:      groupName,
		Reloaded:   failed == 0,
		Count:      succeeded,
		Failed:     failed,
		Skipped:    skipped,
		Bytes:      totalBytes,
		DurationMS: elapsedMS(start),
		Items:      items,
	}
	if mgr != nil {
		res.Revision = mgr.Revision()
	}
	if len(errs) > 0 {
		res.Error = errors.Join(errs...).Error()
	}
	return res, nil
}

func hasManagerGroup(mgr *assets.Manager, name string) bool {
	if mgr == nil {
		return false
	}
	_, ok := mgr.LookupGroup(name)
	return ok
}

func (b *Bridge) groupExists(mgr *assets.Manager, name string) bool {
	if hasManagerGroup(mgr, name) {
		return true
	}
	if name == "shaders" && b.cfg.Shaders != nil && len(b.cfg.Shaders.ShaderNames()) > 0 {
		return true
	}
	if name == "materials" && b.cfg.Materials != nil && len(b.cfg.Materials.MaterialNames()) > 0 {
		return true
	}
	if name == "resources" && b.cfg.Resources != nil && len(b.cfg.Resources.Paths()) > 0 {
		return true
	}
	if b.customAssets != nil {
		for _, ca := range b.customAssets.entries {
			if ca.Group == name {
				return true
			}
		}
	}
	return false
}

func matchCustomAsset(ca CustomAsset, key string) bool {
	if ca.Key == key {
		return true
	}
	if ca.URI == "" {
		return false
	}
	if ca.URI == key {
		return true
	}
	return normalizeRelKey(ca.URI) == normalizeRelKey(key)
}

func reloadCustomAsset(ctx context.Context, ca CustomAsset) (ReloadedItem, error) {
	item := ReloadedItem{
		Key:   ca.Key,
		Group: ca.Group,
		Kind:  ca.Kind,
		URI:   ca.URI,
	}
	var err error
	if ca.Reload == nil {
		err = fmt.Errorf("mcp: asset %q does not support reload", ca.Key)
	} else {
		err = ca.Reload(ctx)
	}
	if ca.Bytes != nil {
		item.Bytes = max(0, ca.Bytes())
	}
	if ca.Frames != nil {
		item.Frames = max(0, ca.Frames())
	}
	if err != nil {
		item.Error = err.Error()
	}
	return item, err
}

func normalizeRelKey(k string) string {
	k = strings.TrimPrefix(k, "asset:")
	k = strings.ReplaceAll(k, "\\", "/")
	return strings.TrimPrefix(path.Clean("/"+k), "/")
}

func elapsedMS(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000.0
}
