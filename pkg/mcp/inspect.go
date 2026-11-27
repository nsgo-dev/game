package mcp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/automation"
)

const entityRefPrefix = "entity:"

// FormatEntityRef formats e as a stable generational reference string ("entity:<index>:<generation>") usable across MCP calls and simulation steps.
// It returns "" for entities.Zero.
func FormatEntityRef(e entities.Entity) string {
	if e == entities.Zero {
		return ""
	}
	return fmt.Sprintf("%s%d:%d", entityRefPrefix, e.Index(), e.Generation())
}

// ParseEntityRef parses a reference returned by FormatEntityRef back into an entities.Entity handle.
// It refuses malformed strings and zero generations.
func ParseEntityRef(ref string) (entities.Entity, error) {
	rest, ok := strings.CutPrefix(ref, entityRefPrefix)
	if !ok {
		return entities.Zero, fmt.Errorf("mcp: invalid entity ref %q (want %q)", ref, "entity:<index>:<generation>")
	}
	idxStr, genStr, ok := strings.Cut(rest, ":")
	if !ok || idxStr == "" || genStr == "" || strings.Contains(genStr, ":") {
		return entities.Zero, fmt.Errorf("mcp: invalid entity ref %q (want %q)", ref, "entity:<index>:<generation>")
	}
	idx, err := strconv.ParseUint(idxStr, 10, 32)
	if err != nil {
		return entities.Zero, fmt.Errorf("mcp: invalid entity index in %q: %w", ref, err)
	}
	gen, err := strconv.ParseUint(genStr, 10, 32)
	if err != nil {
		return entities.Zero, fmt.Errorf("mcp: invalid entity generation in %q: %w", ref, err)
	}
	if gen == 0 {
		return entities.Zero, fmt.Errorf("mcp: invalid entity ref %q: generation must be non-zero", ref)
	}
	return entities.MakeEntity(uint32(idx), uint32(gen)), nil
}

// CameraInfo describes a 2D scene's camera state.
type CameraInfo struct {
	X        float32 `json:"x"`
	Y        float32 `json:"y"`
	Zoom     float32 `json:"zoom"`
	Rotation float32 `json:"rotation,omitempty"`
	Width    float32 `json:"width"`
	Height   float32 `json:"height"`
}

// SceneInfo describes one active or named scene returned by game_scenes_list.
type SceneInfo struct {
	Name        string      `json:"name"`
	Active      bool        `json:"active"`
	Current     bool        `json:"current,omitempty"`
	StackIndex  *int        `json:"stack_index,omitempty"`
	Transparent bool        `json:"transparent,omitempty"`
	LayerCount  int         `json:"layer_count"`
	Camera      *CameraInfo `json:"camera,omitempty"`
}

// ScenesListResult is the JSON result of game_scenes_list.
type ScenesListResult struct {
	Current       string      `json:"current,omitempty"`
	StackDepth    int         `json:"stack_depth"`
	Transitioning bool        `json:"transitioning,omitempty"`
	Progress      float64     `json:"progress,omitempty"`
	Scenes        []SceneInfo `json:"scenes"`
}

// LayerInfo describes one scene layer returned by game_layers_list.
type LayerInfo struct {
	Index         int     `json:"index"`
	DrawIndex     int     `json:"draw_index"`
	Name          string  `json:"name"`
	Space         string  `json:"space"`
	Visible       bool    `json:"visible"`
	Order         int     `json:"order"`
	ParallaxX     float32 `json:"parallax_x"`
	ParallaxY     float32 `json:"parallax_y"`
	OffsetX       float32 `json:"offset_x,omitempty"`
	OffsetY       float32 `json:"offset_y,omitempty"`
	RepeatX       bool    `json:"repeat_x,omitempty"`
	RepeatY       bool    `json:"repeat_y,omitempty"`
	RepeatWidth   float32 `json:"repeat_width,omitempty"`
	RepeatHeight  float32 `json:"repeat_height,omitempty"`
	DepthSort     string  `json:"depth_sort"`
	ItemCount     int     `json:"item_count"`
	BatchCount    int     `json:"batch_count,omitempty"`
	SpriteCount   int     `json:"sprite_count,omitempty"`
	TilemapCount  int     `json:"tilemap_count,omitempty"`
	DrawableCount int     `json:"drawable_count,omitempty"`
}

// LayersListResult is the JSON result of game_layers_list.
type LayersListResult struct {
	Scene  string      `json:"scene"`
	Layers []LayerInfo `json:"layers"`
}

// EntitySummary describes one live entity returned by game_entities_list.
type EntitySummary struct {
	Ref        string                     `json:"ref"`
	Index      uint32                     `json:"index"`
	Generation uint32                     `json:"generation"`
	Name       string                     `json:"name,omitempty"`
	Layer      string                     `json:"layer,omitempty"`
	Tags       []string                   `json:"tags,omitempty"`
	Parent     string                     `json:"parent,omitempty"`
	ChildCount int                        `json:"child_count,omitempty"`
	Components []string                   `json:"components,omitempty"`
	Values     map[string]json.RawMessage `json:"values,omitempty"`
}

// EntitiesListResult is the JSON result of game_entities_list.
type EntitiesListResult struct {
	Total    int             `json:"total"`
	Returned int             `json:"returned"`
	Entities []EntitySummary `json:"entities"`
}

// EntityInspectResult is the JSON result of game_entity_inspect.
type EntityInspectResult struct {
	Ref        string                     `json:"ref"`
	Index      uint32                     `json:"index"`
	Generation uint32                     `json:"generation"`
	Name       string                     `json:"name,omitempty"`
	Layer      string                     `json:"layer,omitempty"`
	Tags       []string                   `json:"tags,omitempty"`
	Parent     string                     `json:"parent,omitempty"`
	Children   []string                   `json:"children,omitempty"`
	Components map[string]json.RawMessage `json:"components"`
}

// ComponentGetResult is the JSON result of game_component_get.
type ComponentGetResult struct {
	Ref       string          `json:"ref"`
	Component string          `json:"component"`
	Value     json.RawMessage `json:"value"`
}

var (
	schemaScenesList = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {}
	}`)

	schemaLayersList = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"scene": {
				"type": "string",
				"description": "Scene name from game_scenes_list; omit to inspect the current active scene."
			},
			"include_hidden": {
				"type": "boolean",
				"description": "Include layers whose Visible flag is false (default true)."
			}
		}
	}`)

	schemaEntitiesList = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"component": {
				"type": "string",
				"description": "Filter to live entities carrying this component name."
			},
			"layer": {
				"type": "string",
				"description": "Filter to live entities on this layer."
			},
			"name": {
				"type": "string",
				"description": "Filter to live entities whose name contains this substring."
			},
			"tag": {
				"type": "string",
				"description": "Filter to live entities carrying this tag."
			},
			"parent": {
				"type": "string",
				"description": "Filter to direct children of this parent entity ref, or \"root\" for entities with no parent."
			},
			"include_components": {
				"type": "boolean",
				"description": "Include serialized component values inline on each returned entity (default false)."
			},
			"offset": {
				"type": "integer",
				"minimum": 0,
				"description": "Number of matching entities to skip (default 0)."
			},
			"limit": {
				"type": "integer",
				"minimum": 1,
				"description": "Maximum number of matching entities to return (omit or 0 for all)."
			}
		}
	}`)

	schemaEntityInspect = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["ref"],
		"properties": {
			"ref": {
				"type": "string",
				"description": "Stable entity reference returned by game_entities_list (e.g. \"entity:0:1\")."
			}
		}
	}`)

	schemaComponentGet = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["ref", "component"],
		"properties": {
			"ref": {
				"type": "string",
				"description": "Stable entity reference returned by game_entities_list (e.g. \"entity:0:1\")."
			},
			"component": {
				"type": "string",
				"description": "Component type name on the entity."
			}
		}
	}`)
)

func (b *Bridge) inspectTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolScenesList,
			Description: "List active scenes on the scene stack and any registered named scenes, including the current scene and transition state.",
			InputSchema: schemaScenesList,
			Handler:     b.handleScenesList,
		},
		{
			Name:        ToolLayersList,
			Description: "List layers of a scene in draw order (defaults to the current scene) with visibility, z-order, coordinate space, parallax, depth sort and item counts.",
			InputSchema: schemaLayersList,
			Handler:     b.handleLayersList,
		},
		{
			Name:        ToolEntitiesList,
			Description: "List live entities in the game world with stable generational references across steps, hierarchy links and component names.",
			InputSchema: schemaEntitiesList,
			Handler:     b.handleEntitiesList,
		},
		{
			Name:        ToolEntityInspect,
			Description: "Inspect a single live entity by its stable reference, returning metadata, hierarchy links and all readable component values.",
			InputSchema: schemaEntityInspect,
			Handler:     b.handleEntityInspect,
		},
		{
			Name:        ToolComponentGet,
			Description: "Read one component's value on a live entity by its stable reference and component name.",
			InputSchema: schemaComponentGet,
			Handler:     b.handleComponentGet,
		},
	}
}

type resolvedScene struct {
	info  SceneInfo
	scene *scene.Scene
}

func (b *Bridge) collectScenes() (ScenesListResult, []resolvedScene) {
	res := ScenesListResult{
		Scenes: []SceneInfo{},
	}
	var resolved []resolvedScene
	seen := make(map[string]bool)

	if mgr := b.cfg.Scenes; mgr != nil {
		stack := mgr.Stack()
		res.StackDepth = len(stack)
		res.Transitioning = mgr.Transitioning()
		if res.Transitioning {
			res.Progress = mgr.TransitionProgress()
		}
		for i, ctrl := range stack {
			name := ""
			if b.cfg.SceneName != nil && ctrl != nil {
				name = b.cfg.SceneName(ctrl)
			}
			if name == "" {
				name = fmt.Sprintf("scene:%d", i)
			}
			idx := i
			info := SceneInfo{
				Name:       name,
				Active:     true,
				Current:    i == len(stack)-1,
				StackIndex: &idx,
			}
			var sc *scene.Scene
			if ctrl != nil {
				info.Transparent = ctrl.Transparent
				sc = ctrl.Scene
			}
			if info.Current && sc == nil {
				sc = b.standaloneScene()
			}
			populateSceneMeta(&info, sc)
			res.Scenes = append(res.Scenes, info)
			resolved = append(resolved, resolvedScene{info: info, scene: sc})
			seen[name] = true
			if info.Current {
				res.Current = name
			}
		}
	} else if sc := b.standaloneScene(); sc != nil {
		name := b.nameForStandaloneScene(sc)
		idx := 0
		info := SceneInfo{
			Name:       name,
			Active:     true,
			Current:    true,
			StackIndex: &idx,
		}
		populateSceneMeta(&info, sc)
		res.Current = name
		res.StackDepth = 1
		res.Scenes = append(res.Scenes, info)
		resolved = append(resolved, resolvedScene{info: info, scene: sc})
		seen[name] = true
	}

	if len(b.cfg.NamedScenes) > 0 {
		names := make([]string, 0, len(b.cfg.NamedScenes))
		for name := range b.cfg.NamedScenes {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			if seen[name] {
				continue
			}
			sc := b.cfg.NamedScenes[name]
			info := SceneInfo{
				Name:   name,
				Active: false,
			}
			populateSceneMeta(&info, sc)
			res.Scenes = append(res.Scenes, info)
			resolved = append(resolved, resolvedScene{info: info, scene: sc})
			seen[name] = true
		}
	}

	return res, resolved
}

func (b *Bridge) nameForStandaloneScene(sc *scene.Scene) string {
	var matches []string
	for name, candidate := range b.cfg.NamedScenes {
		if candidate == sc {
			matches = append(matches, name)
		}
	}
	if len(matches) > 0 {
		slices.Sort(matches)
		return matches[0]
	}
	return "main"
}

func populateSceneMeta(info *SceneInfo, sc *scene.Scene) {
	if sc == nil {
		return
	}
	info.LayerCount = len(sc.Layers)
	if sc.Camera != nil {
		info.Camera = cameraInfoFrom(sc.Camera)
	}
}

func cameraInfoFrom(cam *tilemap.Camera) *CameraInfo {
	if cam == nil {
		return nil
	}
	zoom := cam.Zoom
	if zoom <= 0 {
		zoom = 1
	}
	return &CameraInfo{
		X:        cam.X,
		Y:        cam.Y,
		Zoom:     zoom,
		Rotation: cam.Rotation,
		Width:    cam.Width,
		Height:   cam.Height,
	}
}

func (b *Bridge) handleScenesList(_ context.Context, raw json.RawMessage) (any, error) {
	if _, err := decodeArgs[struct{}](raw); err != nil {
		return nil, err
	}
	res, _ := b.collectScenes()
	return res, nil
}

type layersListArgs struct {
	Scene         string `json:"scene"`
	IncludeHidden *bool  `json:"include_hidden"`
}

func (b *Bridge) handleLayersList(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[layersListArgs](raw)
	if err != nil {
		return nil, err
	}
	includeHidden := true
	if args.IncludeHidden != nil {
		includeHidden = *args.IncludeHidden
	}

	_, resolved := b.collectScenes()
	var target *resolvedScene
	if args.Scene != "" {
		// Search active stack entries top-down before falling back to inactive named scenes.
		for i := len(resolved) - 1; i >= 0; i-- {
			if resolved[i].info.Active && resolved[i].info.Name == args.Scene {
				target = &resolved[i]
				break
			}
		}
		if target == nil {
			for i := range resolved {
				if resolved[i].info.Name == args.Scene {
					target = &resolved[i]
					break
				}
			}
		}
		if target == nil {
			avail := make([]string, len(resolved))
			for i, r := range resolved {
				avail[i] = r.info.Name
			}
			if len(avail) == 0 {
				return nil, fmt.Errorf("mcp: scene %q not found (no scenes configured)", args.Scene)
			}
			return nil, fmt.Errorf("mcp: scene %q not found (available: %s)", args.Scene, strings.Join(avail, ", "))
		}
	} else {
		for i := len(resolved) - 1; i >= 0; i-- {
			if resolved[i].info.Current {
				target = &resolved[i]
				break
			}
		}
		if target == nil {
			return nil, errors.New("mcp: no active scene")
		}
	}

	result := LayersListResult{
		Scene:  target.info.Name,
		Layers: []LayerInfo{},
	}
	if target.scene == nil || len(target.scene.Layers) == 0 {
		return result, nil
	}

	type indexedLayer struct {
		index int
		layer *scene.Layer
	}
	ordered := make([]indexedLayer, 0, len(target.scene.Layers))
	for i, l := range target.scene.Layers {
		if l == nil {
			continue
		}
		ordered = append(ordered, indexedLayer{index: i, layer: l})
	}
	slices.SortStableFunc(ordered, func(a, b indexedLayer) int {
		return cmp.Compare(a.layer.Order, b.layer.Order)
	})

	for drawIdx, entry := range ordered {
		l := entry.layer
		if !includeHidden && !l.Visible {
			continue
		}
		px, py := l.Parallax()
		info := LayerInfo{
			Index:        entry.index,
			DrawIndex:    drawIdx,
			Name:         l.Name,
			Space:        spaceName(l.Space),
			Visible:      l.Visible,
			Order:        l.Order,
			ParallaxX:    px,
			ParallaxY:    py,
			OffsetX:      l.OffsetX,
			OffsetY:      l.OffsetY,
			RepeatX:      l.RepeatX,
			RepeatY:      l.RepeatY,
			RepeatWidth:  l.RepeatWidth,
			RepeatHeight: l.RepeatHeight,
			DepthSort:    sortModeName(l.DepthSort),
			ItemCount:    len(l.Items),
		}
		for _, it := range l.Items {
			if it.Batch != nil {
				info.BatchCount++
				for _, sp := range it.Batch.Sprites {
					if sp != nil {
						info.SpriteCount++
					}
				}
			}
			if it.Tilemap != nil {
				info.TilemapCount++
			}
			if it.Drawable != nil {
				info.DrawableCount++
			}
		}
		result.Layers = append(result.Layers, info)
	}
	return result, nil
}

func spaceName(sp scene.Space) string {
	switch sp {
	case scene.SpaceScreen:
		return "screen"
	default:
		return "world"
	}
}

func sortModeName(m scene.SortMode) string {
	switch m {
	case scene.SortY:
		return "y"
	case scene.SortCustom:
		return "custom"
	default:
		return "none"
	}
}

type entitiesListArgs struct {
	Component         string `json:"component"`
	Layer             string `json:"layer"`
	Name              string `json:"name"`
	Tag               string `json:"tag"`
	Parent            string `json:"parent"`
	IncludeComponents bool   `json:"include_components"`
	Offset            int    `json:"offset"`
	Limit             int    `json:"limit"`
}

func (b *Bridge) requireWorld() (*entities.World, error) {
	w := b.activeWorld()
	if w == nil {
		return nil, errors.New("mcp: no entity world configured")
	}
	if w.Disposed() {
		return nil, fmt.Errorf("mcp: %w", entities.ErrWorldDisposed)
	}
	return w, nil
}

func (b *Bridge) handleEntitiesList(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[entitiesListArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.Offset < 0 {
		return nil, fmt.Errorf("mcp: offset must be >= 0 (got %d)", args.Offset)
	}
	if args.Limit < 0 {
		return nil, fmt.Errorf("mcp: limit must be >= 0 (got %d)", args.Limit)
	}
	w, err := b.requireWorld()
	if err != nil {
		return nil, err
	}

	var (
		filterRoot   bool
		filterParent entities.Entity
	)
	if args.Parent == "root" {
		filterRoot = true
	} else if args.Parent != "" {
		p, err := ParseEntityRef(args.Parent)
		if err != nil {
			return nil, err
		}
		filterParent = p
	}

	var matched []entities.Entity
	w.Each(func(e entities.Entity) bool {
		if filterRoot {
			if _, hasParent := w.ParentOf(e); hasParent {
				return true
			}
		} else if filterParent != entities.Zero {
			p, hasParent := w.ParentOf(e)
			if !hasParent || p != filterParent {
				return true
			}
		}
		if args.Name != "" && !strings.Contains(b.entityName(w, e), args.Name) {
			return true
		}
		if args.Layer != "" && b.entityLayer(w, e) != args.Layer {
			return true
		}
		if args.Tag != "" && !slices.Contains(b.entityTags(e), args.Tag) {
			return true
		}
		if args.Component != "" && !b.entityHasComponent(w, e, args.Component) {
			return true
		}
		matched = append(matched, e)
		return true
	})

	total := len(matched)
	start := min(args.Offset, total)
	end := total
	if args.Limit > 0 && start+args.Limit < end {
		end = start + args.Limit
	}
	page := matched[start:end]

	out := EntitiesListResult{
		Total:    total,
		Returned: len(page),
		Entities: make([]EntitySummary, 0, len(page)),
	}
	for _, e := range page {
		summary := EntitySummary{
			Ref:        FormatEntityRef(e),
			Index:      e.Index(),
			Generation: e.Generation(),
			Name:       b.entityName(w, e),
			Layer:      b.entityLayer(w, e),
			Tags:       b.entityTags(e),
			Components: b.entityComponentNames(w, e),
		}
		if p, ok := w.ParentOf(e); ok {
			summary.Parent = FormatEntityRef(p)
		}
		if ch := w.ChildrenOf(e); len(ch) > 0 {
			summary.ChildCount = len(ch)
		}
		if args.IncludeComponents {
			vals, err := b.readAllComponents(w, e)
			if err != nil {
				return nil, err
			}
			summary.Values = vals
		}
		out.Entities = append(out.Entities, summary)
	}
	return out, nil
}

type entityInspectArgs struct {
	Ref string `json:"ref"`
}

func (b *Bridge) handleEntityInspect(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[entityInspectArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.Ref == "" {
		return nil, errors.New("mcp: ref is required")
	}
	w, err := b.requireWorld()
	if err != nil {
		return nil, err
	}
	e, err := ParseEntityRef(args.Ref)
	if err != nil {
		return nil, err
	}
	if !w.Alive(e) {
		return nil, fmt.Errorf("mcp: entity %q is not alive", args.Ref)
	}

	vals, err := b.readAllComponents(w, e)
	if err != nil {
		return nil, err
	}
	res := EntityInspectResult{
		Ref:        FormatEntityRef(e),
		Index:      e.Index(),
		Generation: e.Generation(),
		Name:       b.entityName(w, e),
		Layer:      b.entityLayer(w, e),
		Tags:       b.entityTags(e),
		Components: vals,
	}
	if p, ok := w.ParentOf(e); ok {
		res.Parent = FormatEntityRef(p)
	}
	if ch := w.ChildrenOf(e); len(ch) > 0 {
		res.Children = make([]string, len(ch))
		for i, c := range ch {
			res.Children[i] = FormatEntityRef(c)
		}
	}
	return res, nil
}

type componentGetArgs struct {
	Ref       string `json:"ref"`
	Component string `json:"component"`
}

func (b *Bridge) handleComponentGet(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[componentGetArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.Ref == "" {
		return nil, errors.New("mcp: ref is required")
	}
	if args.Component == "" {
		return nil, errors.New("mcp: component is required")
	}
	w, err := b.requireWorld()
	if err != nil {
		return nil, err
	}
	e, err := ParseEntityRef(args.Ref)
	if err != nil {
		return nil, err
	}
	if !w.Alive(e) {
		return nil, fmt.Errorf("mcp: entity %q is not alive", args.Ref)
	}

	val, ok, err := b.readComponent(w, e, args.Component)
	if err != nil {
		return nil, err
	}
	if !ok {
		avail := b.entityComponentNames(w, e)
		if len(avail) == 0 {
			return nil, fmt.Errorf("mcp: entity %q does not have component %q (no readable components attached)", args.Ref, args.Component)
		}
		return nil, fmt.Errorf("mcp: entity %q does not have component %q (available: %s)", args.Ref, args.Component, strings.Join(avail, ", "))
	}
	return ComponentGetResult{
		Ref:       FormatEntityRef(e),
		Component: args.Component,
		Value:     val,
	}, nil
}

func (b *Bridge) entityName(w *entities.World, e entities.Entity) string {
	if b.cfg.EntityName != nil {
		if name := b.cfg.EntityName(e); name != "" {
			return name
		}
	}
	if ref, ok := entities.Get[sceneformat.ObjectRef](w, e); ok && ref.ID != "" {
		return ref.ID
	}
	return ""
}

func (b *Bridge) entityLayer(w *entities.World, e entities.Entity) string {
	if b.cfg.EntityLayer != nil {
		if layer := b.cfg.EntityLayer(e); layer != "" {
			return layer
		}
	}
	if ref, ok := entities.Get[sceneformat.ObjectRef](w, e); ok && ref.Layer != "" {
		return ref.Layer
	}
	return ""
}

func (b *Bridge) entityTags(e entities.Entity) []string {
	if b.cfg.EntityTags == nil {
		return nil
	}
	tags := b.cfg.EntityTags(e)
	if len(tags) == 0 {
		return nil
	}
	return slices.Clone(tags)
}

type builtinComponent struct {
	name       string
	typeKey    compTypeKey
	registered func(w *entities.World) bool
	has        func(w *entities.World, e entities.Entity) bool
	read       func(w *entities.World, e entities.Entity) (any, bool)
}

var builtinComponents = []builtinComponent{
	{
		name:    "object_ref",
		typeKey: compTypeKeyOf[sceneformat.ObjectRef](),
		registered: func(w *entities.World) bool {
			_, ok := entities.CodecName[sceneformat.ObjectRef](w)
			return ok
		},
		has: func(w *entities.World, e entities.Entity) bool {
			return entities.Has[sceneformat.ObjectRef](w, e)
		},
		read: func(w *entities.World, e entities.Entity) (any, bool) {
			v, ok := entities.Get[sceneformat.ObjectRef](w, e)
			if !ok {
				return nil, false
			}
			return *v, true
		},
	},
	{
		name:    "transform2d",
		typeKey: compTypeKeyOf[entities.Transform2D](),
		registered: func(w *entities.World) bool {
			_, ok := entities.CodecName[entities.Transform2D](w)
			return ok
		},
		has: func(w *entities.World, e entities.Entity) bool {
			return entities.Has[entities.Transform2D](w, e)
		},
		read: func(w *entities.World, e entities.Entity) (any, bool) {
			v, ok := entities.Get[entities.Transform2D](w, e)
			if !ok {
				return nil, false
			}
			return *v, true
		},
	},
	{
		name:    "world_transform2d",
		typeKey: compTypeKeyOf[entities.WorldTransform2D](),
		registered: func(w *entities.World) bool {
			_, ok := entities.CodecName[entities.WorldTransform2D](w)
			return ok
		},
		has: func(w *entities.World, e entities.Entity) bool {
			return entities.Has[entities.WorldTransform2D](w, e)
		},
		read: func(w *entities.World, e entities.Entity) (any, bool) {
			v, ok := entities.Get[entities.WorldTransform2D](w, e)
			if !ok {
				return nil, false
			}
			return *v, true
		},
	},
}

func (b *Bridge) builtinActive(w *entities.World, bc builtinComponent) bool {
	if _, namedOnBridge := b.components[bc.name]; namedOnBridge {
		return false
	}
	if _, typedOnBridge := b.compByType[bc.typeKey]; typedOnBridge {
		return false
	}
	if bc.registered(w) {
		return false
	}
	return true
}

func (b *Bridge) entityHasComponent(w *entities.World, e entities.Entity, name string) bool {
	if entry, ok := b.components[name]; ok {
		if entry.has != nil {
			return entry.has(w, e)
		}
		_, present, err := entry.read(w, e)
		return err == nil && present
	}
	if w.HasCodec(e, name) {
		return true
	}
	for _, bc := range builtinComponents {
		if bc.name == name && b.builtinActive(w, bc) {
			return bc.has(w, e)
		}
	}
	return false
}

func (b *Bridge) entityComponentNames(w *entities.World, e entities.Entity) []string {
	seen := make(map[string]struct{})
	var names []string
	add := func(name string) {
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	for _, name := range b.compOrder {
		entry := b.components[name]
		var present bool
		if entry.has != nil {
			present = entry.has(w, e)
		} else {
			_, ok, err := entry.read(w, e)
			present = err == nil && ok
		}
		if present {
			add(name)
		}
	}
	for _, name := range w.CodecNames() {
		if _, overridden := b.components[name]; overridden {
			continue
		}
		if w.HasCodec(e, name) {
			add(name)
		}
	}
	for _, bc := range builtinComponents {
		if b.builtinActive(w, bc) && bc.has(w, e) {
			add(bc.name)
		}
	}
	slices.Sort(names)
	return names
}

func (b *Bridge) readComponent(w *entities.World, e entities.Entity, name string) (json.RawMessage, bool, error) {
	if entry, ok := b.components[name]; ok {
		v, present, err := entry.read(w, e)
		if err != nil {
			return nil, false, fmt.Errorf("mcp: read component %q on %s: %w", name, FormatEntityRef(e), err)
		}
		if !present {
			return nil, false, nil
		}
		raw, err := encodeValue(v)
		if err != nil {
			return nil, false, fmt.Errorf("mcp: encode component %q on %s: %w", name, FormatEntityRef(e), err)
		}
		return raw, true, nil
	}
	if raw, present, err := w.EncodeComponent(e, name); err != nil {
		return nil, false, fmt.Errorf("mcp: read component %q on %s: %w", name, FormatEntityRef(e), err)
	} else if present {
		return raw, true, nil
	}
	for _, bc := range builtinComponents {
		if bc.name == name && b.builtinActive(w, bc) {
			v, present := bc.read(w, e)
			if !present {
				return nil, false, nil
			}
			raw, err := encodeValue(v)
			if err != nil {
				return nil, false, fmt.Errorf("mcp: encode component %q on %s: %w", name, FormatEntityRef(e), err)
			}
			return raw, true, nil
		}
	}
	return nil, false, nil
}

func (b *Bridge) readAllComponents(w *entities.World, e entities.Entity) (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage)

	for _, name := range b.compOrder {
		entry := b.components[name]
		v, present, err := entry.read(w, e)
		if err != nil {
			return nil, fmt.Errorf("mcp: read component %q on %s: %w", name, FormatEntityRef(e), err)
		}
		if !present {
			continue
		}
		raw, err := encodeValue(v)
		if err != nil {
			return nil, fmt.Errorf("mcp: encode component %q on %s: %w", name, FormatEntityRef(e), err)
		}
		out[name] = raw
	}

	codecComps, err := w.EntityComponents(e)
	if err != nil {
		return nil, fmt.Errorf("mcp: read components on %s: %w", FormatEntityRef(e), err)
	}
	for _, c := range codecComps {
		if _, overridden := b.components[c.Type]; overridden {
			continue
		}
		out[c.Type] = c.Data
	}

	for _, bc := range builtinComponents {
		if !b.builtinActive(w, bc) {
			continue
		}
		if _, exists := out[bc.name]; exists {
			continue
		}
		v, present := bc.read(w, e)
		if !present {
			continue
		}
		raw, err := encodeValue(v)
		if err != nil {
			return nil, fmt.Errorf("mcp: encode component %q on %s: %w", bc.name, FormatEntityRef(e), err)
		}
		out[bc.name] = raw
	}

	return out, nil
}
