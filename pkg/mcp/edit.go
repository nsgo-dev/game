package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/internal/jsonedit"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/automation"
)

// Tool names for object mutation registered by Bridge with github.com/nsgo-dev/nsgo/automation.
const (
	// ToolPrefabsList lists registered prefabs available to game_entity_spawn.
	ToolPrefabsList = "game_prefabs_list"
	// ToolEntitySpawn spawns a registered prefab or an empty entity with optional parent, position and component overrides.
	ToolEntitySpawn = "game_entity_spawn"
	// ToolEntityDestroy destroys a live entity by stable reference with an optional hierarchy child policy.
	ToolEntityDestroy = "game_entity_destroy"
	// ToolComponentSet sets or patches one component's value on a live entity.
	ToolComponentSet = "game_component_set"
	// ToolComponentRemove removes one component from a live entity.
	ToolComponentRemove = "game_component_remove"
)

var (
	// ErrEmptyPrefabName reports registering a prefab with an empty name.
	ErrEmptyPrefabName = errors.New("mcp: empty prefab name")
	// ErrDuplicatePrefab reports registering a prefab name that is already registered on the Bridge.
	ErrDuplicatePrefab = errors.New("mcp: duplicate prefab registration")
)

// PrefabSpawner spawns a live entity into w with optional override parameters from game_entity_spawn.
type PrefabSpawner func(w *entities.World, overrides map[string]any) (entities.Entity, error)

type prefabEntry struct {
	name  string
	kind  string
	spawn func(w *entities.World, overrides map[string]any) (entities.Entity, []entities.Entity, error)
}

// PrefabInfo describes one registered prefab returned by game_prefabs_list.
type PrefabInfo struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// PrefabsListResult is the JSON result of game_prefabs_list.
type PrefabsListResult struct {
	Prefabs []PrefabInfo `json:"prefabs"`
}

// EntitySpawnResult is the JSON result of game_entity_spawn.
type EntitySpawnResult struct {
	Ref        string                     `json:"ref"`
	Index      uint32                     `json:"index"`
	Generation uint32                     `json:"generation"`
	Prefab     string                     `json:"prefab,omitempty"`
	Name       string                     `json:"name,omitempty"`
	Layer      string                     `json:"layer,omitempty"`
	Tags       []string                   `json:"tags,omitempty"`
	Parent     string                     `json:"parent,omitempty"`
	Children   []string                   `json:"children,omitempty"`
	Components map[string]json.RawMessage `json:"components"`
}

// EntityDestroyResult is the JSON result of game_entity_destroy.
type EntityDestroyResult struct {
	Ref       string `json:"ref"`
	Policy    string `json:"policy"`
	Destroyed int    `json:"destroyed"`
}

// ComponentSetResult is the JSON result of game_component_set.
type ComponentSetResult struct {
	Ref       string          `json:"ref"`
	Component string          `json:"component"`
	Value     json.RawMessage `json:"value"`
	// ReadError is set when the write succeeded but reading the component back failed; Value is then null.
	ReadError string `json:"read_error,omitempty"`
}

// ComponentRemoveResult is the JSON result of game_component_remove.
type ComponentRemoveResult struct {
	Ref       string `json:"ref"`
	Component string `json:"component"`
	Removed   bool   `json:"removed"`
}

var (
	schemaPrefabsList = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {}
	}`)

	schemaEntitySpawn = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"prefab": {
				"type": "string",
				"description": "Registered prefab name from game_prefabs_list; omit to spawn an empty entity."
			},
			"overrides": {
				"type": "object",
				"description": "Prefab-specific override values passed to a custom PrefabSpawner."
			},
			"parent": {
				"type": "string",
				"description": "Stable entity reference (e.g. \"entity:0:1\") to parent the spawned entity under in Hierarchy2D."
			},
			"x": {
				"type": "number",
				"description": "Initial local Transform2D.X position."
			},
			"y": {
				"type": "number",
				"description": "Initial local Transform2D.Y position."
			},
			"components": {
				"type": "object",
				"description": "Map of component names to JSON values to attach or patch on the spawned entity."
			}
		}
	}`)

	schemaEntityDestroy = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["ref"],
		"properties": {
			"ref": {
				"type": "string",
				"description": "Stable entity reference returned by game_entities_list or game_entity_spawn (e.g. \"entity:0:1\")."
			},
			"policy": {
				"type": "string",
				"enum": ["cascade", "orphan", "keep_world", "reparent"],
				"description": "How to handle hierarchy children: \"cascade\" destroys descendants, \"orphan\" detaches direct children to root keeping local transforms, \"keep_world\" detaches direct children to root preserving world transforms, \"reparent\" moves direct children to the destroyed entity's parent (or root) preserving world transforms. Omit to use the hierarchy's default policy."
			}
		}
	}`)

	schemaComponentSet = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["ref", "component", "value"],
		"properties": {
			"ref": {
				"type": "string",
				"description": "Stable entity reference returned by game_entities_list or game_entity_spawn."
			},
			"component": {
				"type": "string",
				"description": "Component name to set or patch on the entity."
			},
			"value": {
				"description": "JSON value for the component (patches existing fields by default unless replace is true)."
			},
			"replace": {
				"type": "boolean",
				"description": "When true, decode value into a zero-valued component instead of patching existing fields (default false)."
			}
		}
	}`)

	schemaComponentRemove = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["ref", "component"],
		"properties": {
			"ref": {
				"type": "string",
				"description": "Stable entity reference returned by game_entities_list or game_entity_spawn."
			},
			"component": {
				"type": "string",
				"description": "Component name to remove from the entity."
			}
		}
	}`)
)

func (b *Bridge) editTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolPrefabsList,
			Description: "List registered prefabs that can be spawned with game_entity_spawn.",
			InputSchema: schemaPrefabsList,
			Handler:     b.handlePrefabsList,
		},
		{
			Name:        ToolEntitySpawn,
			Description: "Spawn a registered prefab (or an empty entity) into the live world with optional parent, 2D position and component values, returning its stable reference and initial components.",
			InputSchema: schemaEntitySpawn,
			Handler:     b.handleEntitySpawn,
		},
		{
			Name:        ToolEntityDestroy,
			Description: "Destroy a live entity by its stable reference, with an optional hierarchy policy (cascade, orphan, keep_world, or reparent).",
			InputSchema: schemaEntityDestroy,
			Handler:     b.handleEntityDestroy,
		},
		{
			Name:        ToolComponentSet,
			Description: "Set or patch one component's value on a live entity by its stable reference so the edited value takes effect on the next simulation step.",
			InputSchema: schemaComponentSet,
			Handler:     b.handleComponentSet,
		},
		{
			Name:        ToolComponentRemove,
			Description: "Remove one component from a live entity by its stable reference and component name.",
			InputSchema: schemaComponentRemove,
			Handler:     b.handleComponentRemove,
		},
	}
}

func (b *Bridge) initPrefabs(prefabs map[string]PrefabSpawner) {
	if len(prefabs) == 0 {
		return
	}
	names := make([]string, 0, len(prefabs))
	for name, spawner := range prefabs {
		if name == "" || spawner == nil {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		_ = b.RegisterPrefab(name, prefabs[name])
	}
}

func (b *Bridge) addPrefabEntry(entry prefabEntry) error {
	if b == nil {
		return ErrNilBridge
	}
	if entry.name == "" {
		return ErrEmptyPrefabName
	}
	if _, dup := b.prefabs[entry.name]; dup {
		return fmt.Errorf("mcp: prefab %q: %w", entry.name, ErrDuplicatePrefab)
	}
	if b.prefabs == nil {
		b.prefabs = make(map[string]prefabEntry)
	}
	b.prefabs[entry.name] = entry
	b.prefabOrder = append(b.prefabOrder, entry.name)
	slices.Sort(b.prefabOrder)
	return nil
}

// RegisterPrefab registers a custom prefab spawner under name on b for game_prefabs_list and game_entity_spawn.
func (b *Bridge) RegisterPrefab(name string, spawn PrefabSpawner) error {
	if b == nil {
		return ErrNilBridge
	}
	if name == "" {
		return ErrEmptyPrefabName
	}
	if spawn == nil {
		return errors.New("mcp: nil prefab spawner")
	}
	return b.addPrefabEntry(prefabEntry{
		name: name,
		kind: "custom",
		spawn: func(w *entities.World, overrides map[string]any) (entities.Entity, []entities.Entity, error) {
			e, err := spawn(w, overrides)
			if err != nil {
				return entities.Zero, nil, err
			}
			if e == entities.Zero || !w.Alive(e) {
				return entities.Zero, nil, fmt.Errorf("mcp: prefab %q returned a non-live entity", name)
			}
			return e, []entities.Entity{e}, nil
		},
	})
}

// RegisterEntityPrefab registers an *entities.Prefab under its Prefab.Name on b for game_prefabs_list and game_entity_spawn.
func (b *Bridge) RegisterEntityPrefab(p *entities.Prefab) error {
	if b == nil {
		return ErrNilBridge
	}
	if p == nil {
		return errors.New("mcp: nil entity prefab")
	}
	name := p.Name()
	if name == "" {
		return ErrEmptyPrefabName
	}
	return b.addPrefabEntry(prefabEntry{
		name: name,
		kind: "entity_prefab",
		spawn: func(w *entities.World, overrides map[string]any) (entities.Entity, []entities.Entity, error) {
			if len(overrides) > 0 {
				return entities.Zero, nil, fmt.Errorf("mcp: entity prefab %q does not accept overrides (pass component values in \"components\" instead)", name)
			}
			e := p.Spawn(w)
			if e == entities.Zero || !w.Alive(e) {
				return entities.Zero, nil, fmt.Errorf("mcp: prefab %q failed to spawn entity", name)
			}
			return e, []entities.Entity{e}, nil
		},
	})
}

// RegisterScenePrefab registers a self-contained *sceneformat.SceneDoc template under name on b so game_entity_spawn can instantiate it via sceneformat.SpawnEntities.
//
// doc must pass sceneformat.Validate, must not refer to unresolved external scenes, and must hold exactly one top-level object across its layers, which becomes the spawned root entity; nest every other object under it as a child. Each spawn attaches the document's sceneformat.ObjectRef IDs unchanged, so repeated spawns produce entities with the same object IDs and names.
func (b *Bridge) RegisterScenePrefab(name string, doc *sceneformat.SceneDoc) error {
	if b == nil {
		return ErrNilBridge
	}
	if name == "" {
		return ErrEmptyPrefabName
	}
	if doc == nil {
		return errors.New("mcp: nil scene prefab")
	}
	if err := sceneformat.Validate(doc); err != nil {
		return fmt.Errorf("mcp: scene prefab %q: %w", name, err)
	}
	if doc.RefersToScenes() {
		return fmt.Errorf("mcp: scene prefab %q refers to external scenes; call sceneformat.Resolve first", name)
	}
	rootID, top := sceneRootObjectID(doc)
	if top != 1 {
		return fmt.Errorf("mcp: scene prefab %q has %d top-level objects, want exactly 1 root object", name, top)
	}
	return b.addPrefabEntry(prefabEntry{
		name: name,
		kind: "scene",
		spawn: func(w *entities.World, overrides map[string]any) (entities.Entity, []entities.Entity, error) {
			if len(overrides) > 0 {
				return entities.Zero, nil, fmt.Errorf("mcp: scene prefab %q does not accept overrides (pass component values in \"components\" instead)", name)
			}
			spawned, err := sceneformat.SpawnEntities(doc, w)
			if err != nil {
				return entities.Zero, nil, fmt.Errorf("mcp: spawn scene prefab %q: %w", name, err)
			}
			root, ok := spawned[rootID]
			if !ok || !w.Alive(root) {
				return entities.Zero, nil, fmt.Errorf("mcp: scene prefab %q did not produce root object %q", name, rootID)
			}
			all := make([]entities.Entity, 0, len(spawned))
			for _, ent := range spawned {
				all = append(all, ent)
			}
			return root, all, nil
		},
	})
}

func sceneRootObjectID(doc *sceneformat.SceneDoc) (string, int) {
	var id string
	top := 0
	for li := range doc.Layers {
		for oi := range doc.Layers[li].Objects {
			if top == 0 {
				id = doc.Layers[li].Objects[oi].ID
			}
			top++
		}
	}
	return id, top
}

// RegisterMutableComponent registers component type T under name on b for both inspection (game_entities_list, game_entity_inspect, game_component_get) and mutation (game_component_set, game_component_remove, game_entity_spawn).
//
// Edits are strictly decoded with DisallowUnknownFields, validated against NaN/Inf floats, and written with entities.Set[T].
func RegisterMutableComponent[T any](b *Bridge, name string) error {
	if b == nil {
		return ErrNilBridge
	}
	if name == "" {
		return ErrEmptyComponentName
	}
	if _, dup := b.components[name]; dup {
		return fmt.Errorf("mcp: component %q: %w", name, ErrDuplicateComponent)
	}
	return registerTypedComponentEditor[T](b, name, func(w *entities.World, e entities.Entity, v T) error {
		entities.Set[T](w, e, v)
		return nil
	})
}

// RegisterComponentEditor registers a typed component editor for component type T under name on b.
//
// When name is not yet registered on b, it also registers inspection and removal for T. When name was previously registered read-only with RegisterComponent[T] for the same type T, it attaches the editor to that registration. It refuses a name registered with RegisterComponentFunc, whose reader may expose data that is not stored as T, and a name registered for another type. apply receives the strictly decoded and finite-validated value and may enforce domain invariants or synchronize secondary runtime state before writing.
func RegisterComponentEditor[T any](b *Bridge, name string, apply func(w *entities.World, e entities.Entity, v T) error) error {
	if b == nil {
		return ErrNilBridge
	}
	if name == "" {
		return ErrEmptyComponentName
	}
	if apply == nil {
		return errors.New("mcp: nil component editor")
	}
	return registerTypedComponentEditor[T](b, name, apply)
}

func registerTypedComponentEditor[T any](b *Bridge, name string, apply func(w *entities.World, e entities.Entity, v T) error) error {
	k := compTypeKeyOf[T]()
	writeFn := func(w *entities.World, e entities.Entity, raw json.RawMessage, replace bool) error {
		v, err := decodeTypedComponent[T](w, e, name, raw, replace)
		if err != nil {
			return err
		}
		return apply(w, e, v)
	}
	removeFn := func(w *entities.World, e entities.Entity) (bool, error) {
		if !entities.Has[T](w, e) {
			return false, nil
		}
		entities.Remove[T](w, e)
		return true, nil
	}

	if existing, exists := b.components[name]; exists {
		if existing.write != nil {
			return fmt.Errorf("mcp: component %q: %w", name, ErrDuplicateComponent)
		}
		if existing.typeKey == nil {
			return fmt.Errorf("mcp: component %q is registered with RegisterComponentFunc, whose reader is not backed by a stored type: %w", name, ErrDuplicateComponent)
		}
		if existing.typeKey != k {
			return fmt.Errorf("mcp: component %q already registered with a different type: %w", name, ErrDuplicateComponent)
		}
		existing.write = writeFn
		existing.remove = removeFn
		b.components[name] = existing
		return nil
	}

	if b.components == nil {
		b.components = make(map[string]componentEntry)
		b.compByType = make(map[compTypeKey]string)
	}
	b.components[name] = componentEntry{
		name:    name,
		typeKey: k,
		has: func(w *entities.World, e entities.Entity) bool {
			return entities.Has[T](w, e)
		},
		read: func(w *entities.World, e entities.Entity) (any, bool, error) {
			v, ok := entities.Get[T](w, e)
			if !ok {
				return nil, false, nil
			}
			return *v, true, nil
		},
		write:  writeFn,
		remove: removeFn,
	}
	b.compOrder = append(b.compOrder, name)
	slices.Sort(b.compOrder)
	b.compByType[k] = name
	return nil
}

func (b *Bridge) handlePrefabsList(_ context.Context, raw json.RawMessage) (any, error) {
	if _, err := decodeArgs[struct{}](raw); err != nil {
		return nil, err
	}
	res := PrefabsListResult{
		Prefabs: make([]PrefabInfo, 0, len(b.prefabOrder)),
	}
	for _, name := range b.prefabOrder {
		entry := b.prefabs[name]
		res.Prefabs = append(res.Prefabs, PrefabInfo{
			Name: entry.name,
			Kind: entry.kind,
		})
	}
	return res, nil
}

type entitySpawnArgs struct {
	Prefab     string                     `json:"prefab"`
	Overrides  map[string]any             `json:"overrides"`
	Parent     string                     `json:"parent"`
	X          *float64                   `json:"x"`
	Y          *float64                   `json:"y"`
	Components map[string]json.RawMessage `json:"components"`
}

func (b *Bridge) handleEntitySpawn(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[entitySpawnArgs](raw)
	if err != nil {
		return nil, err
	}
	w, err := b.requireWorld()
	if err != nil {
		return nil, err
	}

	if args.X != nil {
		if err := validateFloat32Arg("x", *args.X); err != nil {
			return nil, err
		}
	}
	if args.Y != nil {
		if err := validateFloat32Arg("y", *args.Y); err != nil {
			return nil, err
		}
	}

	var parentEnt entities.Entity
	if args.Parent != "" {
		parentEnt, err = ParseEntityRef(args.Parent)
		if err != nil {
			return nil, err
		}
		if !w.Alive(parentEnt) {
			return nil, fmt.Errorf("mcp: parent entity %q is not alive", args.Parent)
		}
	}

	var pEntry prefabEntry
	hasPrefab := args.Prefab != ""
	if hasPrefab {
		var ok bool
		pEntry, ok = b.prefabs[args.Prefab]
		if !ok {
			if len(b.prefabOrder) == 0 {
				return nil, fmt.Errorf("mcp: unknown prefab %q (no prefabs registered)", args.Prefab)
			}
			return nil, fmt.Errorf("mcp: unknown prefab %q (available: %s)", args.Prefab, strings.Join(b.prefabOrder, ", "))
		}
	} else if len(args.Overrides) > 0 {
		return nil, errors.New("mcp: overrides require a prefab name")
	}

	var (
		e       entities.Entity
		spawned []entities.Entity
	)
	if hasPrefab {
		e, spawned, err = pEntry.spawn(w, args.Overrides)
		if err != nil {
			return nil, err
		}
	} else {
		e = w.Spawn()
		if e == entities.Zero {
			return nil, fmt.Errorf("mcp: %w", entities.ErrWorldDisposed)
		}
		spawned = []entities.Entity{e}
	}

	rollback := func() {
		for _, ent := range spawned {
			if !w.Alive(ent) {
				continue
			}
			if len(w.ChildrenOf(ent)) > 0 {
				w.Hierarchy2D().DestroyWithPolicy(ent, entities.RemoveCascade)
			} else {
				w.Destroy(ent)
			}
		}
	}

	if args.X != nil || args.Y != nil {
		t := entities.IdentityTransform2D()
		if cur, ok := entities.Get[entities.Transform2D](w, e); ok {
			t = *cur
		}
		if args.X != nil {
			t.X = float32(*args.X)
		}
		if args.Y != nil {
			t.Y = float32(*args.Y)
		}
		entities.Set(w, e, t)
	}

	if parentEnt != entities.Zero {
		if err := w.Hierarchy2D().SetParent(e, parentEnt, false); err != nil {
			rollback()
			return nil, fmt.Errorf("mcp: set parent %q: %w", args.Parent, err)
		}
	}

	if len(args.Components) > 0 {
		compNames := make([]string, 0, len(args.Components))
		for name := range args.Components {
			compNames = append(compNames, name)
		}
		slices.Sort(compNames)
		for _, name := range compNames {
			if err := b.writeComponent(w, e, name, args.Components[name], false); err != nil {
				rollback()
				return nil, err
			}
		}
	}

	vals, err := b.readAllComponents(w, e)
	if err != nil {
		rollback()
		return nil, err
	}
	res := EntitySpawnResult{
		Ref:        FormatEntityRef(e),
		Index:      e.Index(),
		Generation: e.Generation(),
		Prefab:     args.Prefab,
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

type entityDestroyArgs struct {
	Ref    string `json:"ref"`
	Policy string `json:"policy"`
}

func (b *Bridge) handleEntityDestroy(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[entityDestroyArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.Ref == "" {
		return nil, errors.New("mcp: ref is required")
	}
	switch args.Policy {
	case "", "cascade", "orphan", "keep_world", "reparent":
	default:
		return nil, fmt.Errorf("mcp: invalid destroy policy %q (want \"cascade\", \"orphan\", \"keep_world\", or \"reparent\")", args.Policy)
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

	before := w.Count()
	children := w.ChildrenOf(e)
	effectivePolicy := args.Policy
	if effectivePolicy == "" {
		effectivePolicy = "none"
		if len(children) > 0 {
			effectivePolicy = removePolicyName(w.Hierarchy2D().RemovePolicy())
		}
	}

	switch args.Policy {
	case "":
		w.Destroy(e)
	case "cascade":
		if len(w.ChildrenOf(e)) > 0 {
			w.Hierarchy2D().DestroyWithPolicy(e, entities.RemoveCascade)
		} else {
			w.Destroy(e)
		}
	case "orphan":
		if len(w.ChildrenOf(e)) > 0 {
			w.Hierarchy2D().DestroyWithPolicy(e, entities.RemoveDetach)
		} else {
			w.Destroy(e)
		}
	case "keep_world":
		if len(w.ChildrenOf(e)) > 0 {
			w.Hierarchy2D().DestroyWithPolicy(e, entities.RemoveDetachKeepWorld)
		} else {
			w.Destroy(e)
		}
	case "reparent":
		if len(children) > 0 {
			h := w.Hierarchy2D()
			parent, _ := w.ParentOf(e)
			// Every child is alive and parent is one of its ancestors, so SetParent cannot fail with a dead entity or a cycle; it is checked anyway before any child moves so a change in entities cannot leave a half-moved family.
			for _, ch := range children {
				if !w.Alive(ch) || (parent != entities.Zero && !w.Alive(parent)) {
					return nil, fmt.Errorf("mcp: reparent child %s: %w", FormatEntityRef(ch), entities.ErrDeadEntity)
				}
			}
			for _, ch := range children {
				if err := h.SetParent(ch, parent, true); err != nil {
					return nil, fmt.Errorf("mcp: reparent child %s: %w", FormatEntityRef(ch), err)
				}
			}
		}
		w.Destroy(e)
	}

	return EntityDestroyResult{
		Ref:       FormatEntityRef(e),
		Policy:    effectivePolicy,
		Destroyed: before - w.Count(),
	}, nil
}

func removePolicyName(p entities.RemovePolicy) string {
	switch p {
	case entities.RemoveCascade:
		return "cascade"
	case entities.RemoveDetachKeepWorld:
		return "keep_world"
	default:
		return "orphan"
	}
}

type componentSetArgs struct {
	Ref       string          `json:"ref"`
	Component string          `json:"component"`
	Value     json.RawMessage `json:"value"`
	Replace   bool            `json:"replace"`
}

func (b *Bridge) handleComponentSet(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[componentSetArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.Ref == "" {
		return nil, errors.New("mcp: ref is required")
	}
	if args.Component == "" {
		return nil, errors.New("mcp: component is required")
	}
	if len(bytes.TrimSpace(args.Value)) == 0 {
		return nil, errors.New("mcp: value is required")
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

	if err := b.writeComponent(w, e, args.Component, args.Value, args.Replace); err != nil {
		return nil, err
	}

	res := ComponentSetResult{
		Ref:       FormatEntityRef(e),
		Component: args.Component,
		Value:     json.RawMessage("null"),
	}
	// The write has already happened, so a failing read-back is reported next to the result rather than as an error that would suggest the edit did not apply.
	val, ok, err := b.readComponent(w, e, args.Component)
	switch {
	case err != nil:
		res.ReadError = err.Error()
	case ok:
		res.Value = val
	}
	return res, nil
}

type componentRemoveArgs struct {
	Ref       string `json:"ref"`
	Component string `json:"component"`
}

func (b *Bridge) handleComponentRemove(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[componentRemoveArgs](raw)
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

	removed, err := b.removeComponent(w, e, args.Component)
	if err != nil {
		return nil, err
	}
	return ComponentRemoveResult{
		Ref:       FormatEntityRef(e),
		Component: args.Component,
		Removed:   removed,
	}, nil
}

func (b *Bridge) writeComponent(w *entities.World, e entities.Entity, name string, raw json.RawMessage, replace bool) error {
	if entry, ok := b.components[name]; ok {
		if entry.write == nil {
			return fmt.Errorf("mcp: component %q is read-only (register with RegisterMutableComponent or RegisterComponentEditor to allow edits)", name)
		}
		if err := entry.write(w, e, raw, replace); err != nil {
			return fmt.Errorf("mcp: set component %q on %s: %w", name, FormatEntityRef(e), err)
		}
		return nil
	}

	var codecErr error
	if replace {
		codecErr = w.DecodeComponent(e, name, raw)
	} else {
		codecErr = w.PatchComponent(e, name, raw)
	}
	if codecErr == nil {
		return nil
	}
	if !errors.Is(codecErr, entities.ErrUnknownCodec) {
		return fmt.Errorf("mcp: set component %q on %s: %w", name, FormatEntityRef(e), codecErr)
	}

	for _, bc := range builtinComponents {
		if bc.name != name || !b.builtinActive(w, bc) {
			continue
		}
		switch bc.name {
		case "transform2d":
			return writeBuiltinTransform2D(w, e, raw, replace)
		case "object_ref":
			return writeBuiltinObjectRef(w, e, raw, replace)
		case "world_transform2d":
			return errors.New(`mcp: component "world_transform2d" is derived from hierarchy and read-only; edit "transform2d" instead`)
		}
	}

	return b.unknownEditableComponentError(w, name)
}

func (b *Bridge) removeComponent(w *entities.World, e entities.Entity, name string) (bool, error) {
	if entry, ok := b.components[name]; ok {
		if entry.remove == nil {
			return false, fmt.Errorf("mcp: component %q is read-only (register with RegisterMutableComponent or RegisterComponentEditor to allow edits)", name)
		}
		return entry.remove(w, e)
	}

	removed, err := w.RemoveComponent(e, name)
	if err == nil {
		return removed, nil
	}
	if !errors.Is(err, entities.ErrUnknownCodec) {
		return false, fmt.Errorf("mcp: remove component %q on %s: %w", name, FormatEntityRef(e), err)
	}

	for _, bc := range builtinComponents {
		if bc.name != name || !b.builtinActive(w, bc) {
			continue
		}
		switch bc.name {
		case "transform2d":
			if !entities.Has[entities.Transform2D](w, e) {
				return false, nil
			}
			entities.Remove[entities.Transform2D](w, e)
			if entities.Has[entities.WorldTransform2D](w, e) || len(w.ChildrenOf(e)) > 0 {
				w.Hierarchy2D().MarkDirty(e)
			}
			return true, nil
		case "object_ref":
			if !entities.Has[sceneformat.ObjectRef](w, e) {
				return false, nil
			}
			entities.Remove[sceneformat.ObjectRef](w, e)
			return true, nil
		case "world_transform2d":
			return false, errors.New(`mcp: component "world_transform2d" is derived from hierarchy and read-only`)
		}
	}

	return false, b.unknownEditableComponentError(w, name)
}

func (b *Bridge) unknownEditableComponentError(w *entities.World, name string) error {
	editable := b.editableComponentNames(w)
	if len(editable) == 0 {
		return fmt.Errorf("mcp: unknown component %q (no editable components registered)", name)
	}
	return fmt.Errorf("mcp: unknown component %q (editable: %s)", name, strings.Join(editable, ", "))
}

func (b *Bridge) editableComponentNames(w *entities.World) []string {
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
		if b.components[name].write != nil {
			add(name)
		}
	}
	for _, name := range w.CodecNames() {
		if _, overridden := b.components[name]; overridden {
			continue
		}
		add(name)
	}
	for _, bc := range builtinComponents {
		if bc.name == "world_transform2d" {
			continue
		}
		if b.builtinActive(w, bc) {
			add(bc.name)
		}
	}
	slices.Sort(names)
	return names
}

func writeBuiltinTransform2D(w *entities.World, e entities.Entity, raw json.RawMessage, replace bool) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return errors.New("mcp: empty transform2d value")
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return errors.New(`mcp: cannot set "transform2d" to null; use game_component_remove to remove a component`)
	}
	var t entities.Transform2D
	if !replace {
		if cur, ok := entities.Get[entities.Transform2D](w, e); ok {
			t = *cur
		} else {
			t = entities.IdentityTransform2D()
		}
	}
	if err := jsonedit.DecodeStrict(trimmed, &t); err != nil {
		return fmt.Errorf("mcp: set component %q on %s: %w", "transform2d", FormatEntityRef(e), err)
	}
	for _, pair := range []struct {
		field string
		val   float32
	}{
		{"x", t.X},
		{"y", t.Y},
		{"rotation", t.Rotation},
		{"scaleX", t.ScaleX},
		{"scaleY", t.ScaleY},
	} {
		if math.IsNaN(float64(pair.val)) || math.IsInf(float64(pair.val), 0) {
			return fmt.Errorf("mcp: transform2d.%s must be finite", pair.field)
		}
	}
	entities.Set(w, e, t)
	return nil
}

func writeBuiltinObjectRef(w *entities.World, e entities.Entity, raw json.RawMessage, replace bool) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return errors.New("mcp: empty object_ref value")
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return errors.New(`mcp: cannot set "object_ref" to null; use game_component_remove to remove a component`)
	}
	var ref sceneformat.ObjectRef
	if !replace {
		if cur, ok := entities.Get[sceneformat.ObjectRef](w, e); ok {
			ref = *cur
		}
	}
	if err := jsonedit.DecodeStrict(trimmed, &ref); err != nil {
		return fmt.Errorf("mcp: set component %q on %s: %w", "object_ref", FormatEntityRef(e), err)
	}
	entities.Set(w, e, ref)
	return nil
}

func decodeTypedComponent[T any](w *entities.World, e entities.Entity, name string, raw json.RawMessage, replace bool) (T, error) {
	var v T
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return v, errors.New("empty JSON value")
	}
	if bytes.Equal(trimmed, []byte("null")) {
		rt := reflect.TypeFor[T]()
		switch rt.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
			return v, nil
		default:
			return v, fmt.Errorf("cannot set non-nullable component %q to null; use game_component_remove to remove a component", name)
		}
	}
	if cur, ok := entities.Get[T](w, e); ok && !replace {
		patched, err := jsonedit.Patch(cur, trimmed)
		if err != nil {
			return v, err
		}
		v = patched
	} else if err := jsonedit.DecodeStrict(trimmed, &v); err != nil {
		return v, err
	}
	if err := validateFiniteValue(reflect.ValueOf(v), 0, make(map[visitKey]bool)); err != nil {
		return v, err
	}
	return v, nil
}

func validateFloat32Arg(field string, v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > math.MaxFloat32 {
		return fmt.Errorf("mcp: %s must be a finite float32", field)
	}
	return nil
}

func validateFiniteValue(rv reflect.Value, depth int, seen map[visitKey]bool) error {
	if !rv.IsValid() || depth >= maxEncodeDepth {
		return nil
	}
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return errors.New("non-finite float value (NaN or Inf)")
		}
	case reflect.Interface:
		if !rv.IsNil() {
			return validateFiniteValue(rv.Elem(), depth+1, seen)
		}
	case reflect.Pointer:
		if rv.IsNil() {
			return nil
		}
		key := visitKey{ptr: rv.Pointer(), typ: rv.Type()}
		if seen[key] {
			return nil
		}
		seen[key] = true
		defer delete(seen, key)
		return validateFiniteValue(rv.Elem(), depth+1, seen)
	case reflect.Slice:
		if rv.IsNil() || rv.Len() == 0 {
			return nil
		}
		key := visitKey{ptr: rv.Pointer(), typ: rv.Type()}
		if seen[key] {
			return nil
		}
		seen[key] = true
		defer delete(seen, key)
		for i := range rv.Len() {
			if err := validateFiniteValue(rv.Index(i), depth+1, seen); err != nil {
				return err
			}
		}
	case reflect.Array:
		for i := range rv.Len() {
			if err := validateFiniteValue(rv.Index(i), depth+1, seen); err != nil {
				return err
			}
		}
	case reflect.Map:
		if rv.IsNil() {
			return nil
		}
		key := visitKey{ptr: rv.Pointer(), typ: rv.Type()}
		if seen[key] {
			return nil
		}
		seen[key] = true
		defer delete(seen, key)
		iter := rv.MapRange()
		for iter.Next() {
			if err := validateFiniteValue(iter.Value(), depth+1, seen); err != nil {
				return err
			}
		}
	case reflect.Struct:
		rt := rv.Type()
		for i := range rt.NumField() {
			if !rt.Field(i).IsExported() {
				continue
			}
			if err := validateFiniteValue(rv.Field(i), depth+1, seen); err != nil {
				return err
			}
		}
	}
	return nil
}
