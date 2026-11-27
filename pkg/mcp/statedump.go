package mcp

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/statedump"
	"github.com/nsgo-dev/nsgo/automation"
)

// State dump, capture and diff tool names registered by Bridge with github.com/nsgo-dev/nsgo/automation.
const (
	// ToolStateDump captures a deterministic snapshot of the running game's entities,
	// registered components, scene state, loop counters, and custom state values as both
	// structured JSON and canonical statedump YAML.
	ToolStateDump = "game_state_dump"
	// ToolStateCapture runs statedump.Capture over the running game and stores the snapshot in memory under a name.
	ToolStateCapture = "game_state_capture"
	// ToolStateDiff compares two named state captures (or a named capture against live state) and returns statedump.Diff output plus changed entity references and structured field diffs.
	ToolStateDiff = "game_state_diff"
)

const (
	// MaxStateCaptures is the maximum number of named state captures retained in memory on a Bridge.
	MaxStateCaptures = 32
	// MaxStateCaptureBytes is the maximum canonical YAML size in bytes allowed for a single named state capture.
	MaxStateCaptureBytes = 4 << 20
	// MaxStateCaptureTotalBytes is the maximum combined canonical YAML size retained across all named state captures.
	MaxStateCaptureTotalBytes = 16 << 20

	maxStateCaptureNameLen = 128
	maxStateDiffBytes      = 256 << 10
	maxStructuredChanges   = 256
)

// StateDumpResult is the JSON result of game_state_dump.
type StateDumpResult struct {
	Version         int                        `json:"version"`
	Rate            int                        `json:"rate,omitempty"`
	Steps           uint64                     `json:"steps,omitempty"`
	GameTimeSeconds float64                    `json:"game_time_seconds,omitempty"`
	Scene           *statedump.SceneState      `json:"scene,omitempty"`
	Entities        []entities.EntitySnapshot  `json:"entities,omitempty"`
	Values          map[string]json.RawMessage `json:"values,omitempty"`
	YAML            string                     `json:"yaml"`
}

// StateCaptureResult is the JSON result of game_state_capture.
type StateCaptureResult struct {
	Name            string   `json:"name"`
	Overwritten     bool     `json:"overwritten,omitempty"`
	Evicted         []string `json:"evicted,omitempty"`
	StoredCount     int      `json:"stored_count"`
	Version         int      `json:"version"`
	Rate            int      `json:"rate,omitempty"`
	Steps           uint64   `json:"steps,omitempty"`
	GameTimeSeconds float64  `json:"game_time_seconds,omitempty"`
	Scene           string   `json:"scene,omitempty"`
	EntityCount     int      `json:"entity_count"`
	ValueKeys       []string `json:"value_keys,omitempty"`
	MissingValues   []string `json:"missing_values,omitempty"`
	Bytes           int      `json:"bytes"`
	YAML            string   `json:"yaml,omitempty"`
}

// StateFieldChange records one added, removed, or modified value/component field between two state captures.
type StateFieldChange struct {
	Path   string          `json:"path"`
	Kind   string          `json:"kind"` // "added", "removed", "changed"
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

// StateEntityChange summarizes how one entity changed between two state captures.
type StateEntityChange struct {
	Ref               string             `json:"ref"`
	Kind              string             `json:"kind"` // "added", "removed", "modified"
	ParentChanged     bool               `json:"parent_changed,omitempty"`
	ParentBefore      string             `json:"parent_before,omitempty"`
	ParentAfter       string             `json:"parent_after,omitempty"`
	AddedComponents   []string           `json:"added_components,omitempty"`
	RemovedComponents []string           `json:"removed_components,omitempty"`
	ChangedComponents []string           `json:"changed_components,omitempty"`
	FieldChanges      []StateFieldChange `json:"field_changes,omitempty"`
}

// StateDiffResult is the JSON result of game_state_diff.
type StateDiffResult struct {
	From             string              `json:"from"`
	To               string              `json:"to"`
	Equal            bool                `json:"equal"`
	Diff             string              `json:"diff,omitempty"`
	DiffTruncated    bool                `json:"diff_truncated,omitempty"`
	StepsDelta       int64               `json:"steps_delta,omitempty"`
	SceneChanged     bool                `json:"scene_changed,omitempty"`
	ChangedEntities  []string            `json:"changed_entities,omitempty"`
	AddedEntities    []string            `json:"added_entities,omitempty"`
	RemovedEntities  []string            `json:"removed_entities,omitempty"`
	ModifiedEntities []string            `json:"modified_entities,omitempty"`
	EntityChanges    []StateEntityChange `json:"entity_changes,omitempty"`
	ValueChanges     []StateFieldChange  `json:"value_changes,omitempty"`
	ChangesTruncated bool                `json:"changes_truncated,omitempty"`
}

type storedStateCapture struct {
	name            string
	dump            *statedump.Dump
	yamlBytes       []byte
	steps           uint64
	gameTimeSeconds float64
	valueFilter     []string
}

type stateCaptureStore struct {
	mu         sync.Mutex
	maxCount   int
	maxItem    int
	maxTotal   int
	totalBytes int
	order      []string
	byName     map[string]*storedStateCapture
}

func newStateCaptureStore() *stateCaptureStore {
	return &stateCaptureStore{
		maxCount: MaxStateCaptures,
		maxItem:  MaxStateCaptureBytes,
		maxTotal: MaxStateCaptureTotalBytes,
		byName:   make(map[string]*storedStateCapture),
	}
}

func (s *stateCaptureStore) put(entry *storedStateCapture) (overwritten bool, evicted []string, storedCount int, err error) {
	if s == nil {
		return false, nil, 0, errors.New("mcp: state capture store unavailable")
	}
	size := len(entry.yamlBytes)
	if size > s.maxItem {
		return false, nil, 0, fmt.Errorf("mcp: state capture %q is %d bytes, exceeds maximum %d bytes", entry.name, size, s.maxItem)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if prev, ok := s.byName[entry.name]; ok {
		overwritten = true
		s.totalBytes -= len(prev.yamlBytes)
		s.order = slices.DeleteFunc(s.order, func(k string) bool { return k == entry.name })
		delete(s.byName, entry.name)
	}
	for len(s.order) > 0 && (len(s.order) >= s.maxCount || s.totalBytes+size > s.maxTotal) {
		oldest := s.order[0]
		s.order = s.order[1:]
		if old, ok := s.byName[oldest]; ok {
			s.totalBytes -= len(old.yamlBytes)
			delete(s.byName, oldest)
			evicted = append(evicted, oldest)
		}
	}
	s.byName[entry.name] = entry
	s.order = append(s.order, entry.name)
	s.totalBytes += size
	return overwritten, evicted, len(s.order), nil
}

func (s *stateCaptureStore) get(name string) (*storedStateCapture, []string) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.byName[name]; ok {
		return entry, nil
	}
	names := make([]string, 0, len(s.byName))
	for k := range s.byName {
		names = append(names, k)
	}
	slices.Sort(names)
	return nil, names
}

var (
	schemaStateDump = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {}
	}`)
	schemaStateCapture = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["name"],
		"properties": {
			"name": {
				"type": "string",
				"minLength": 1,
				"maxLength": 128,
				"description": "Name under which to store this state capture in memory (for later comparison with game_state_diff)."
			},
			"values": {
				"type": "array",
				"items": {
					"type": "string",
					"minLength": 1
				},
				"description": "Optional list of Config.StateValues keys to include; omit to include all keys returned by Config.StateValues."
			},
			"include_yaml": {
				"type": "boolean",
				"description": "Optional: include the full canonical statedump YAML text in the response (default false)."
			}
		}
	}`)
	schemaStateDiff = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["from"],
		"properties": {
			"from": {
				"type": "string",
				"minLength": 1,
				"description": "Name of the baseline state capture saved with game_state_capture."
			},
			"to": {
				"type": "string",
				"minLength": 1,
				"description": "Name of the target state capture saved with game_state_capture; omit to compare against the current live game state."
			},
			"paths": {
				"type": "array",
				"items": {
					"type": "string",
					"minLength": 1
				},
				"description": "Optional dot-separated path prefixes to filter the diff (for example \"entities\", \"entity:0:1\", \"entity:0:1.pos\", \"values.score\", \"scene\", \"steps\")."
			}
		}
	}`)
)

func (b *Bridge) stateDumpTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolStateDump,
			Description: "Capture a deterministic state dump of the running game's entities, codec components, scene state, loop step count/rate, and custom state values as both structured JSON and canonical statedump YAML.",
			InputSchema: schemaStateDump,
			Handler:     b.handleStateDump,
		},
		{
			Name:        ToolStateCapture,
			Description: "Capture a deterministic state snapshot of the running game via statedump.Capture and store it in memory under a name (bounded count and size) for later diffing with game_state_diff.",
			InputSchema: schemaStateCapture,
			Handler:     b.handleStateCapture,
		},
		{
			Name:        ToolStateDiff,
			Description: "Diff two named state captures (or one capture against current live state) using statedump.Diff, returning the unified YAML line diff, changed entity references, and structured entity/value field changes.",
			InputSchema: schemaStateDiff,
			Handler:     b.handleStateDiff,
		},
	}
}

func (b *Bridge) ensureStateStore() *stateCaptureStore {
	if b.stateCaptures == nil {
		b.stateCaptures = newStateCaptureStore()
	}
	return b.stateCaptures
}

func (b *Bridge) captureLiveDump(valueKeys []string) (dump *statedump.Dump, yamlBytes []byte, steps uint64, gameTimeSeconds float64, missingValues []string, err error) {
	var valuesFn func() map[string]any
	if b.cfg.StateValues != nil {
		if len(valueKeys) == 0 {
			valuesFn = b.cfg.StateValues
		} else {
			want := make(map[string]struct{}, len(valueKeys))
			for _, k := range valueKeys {
				want[k] = struct{}{}
			}
			raw := b.cfg.StateValues()
			filtered := make(map[string]any, len(want))
			for _, k := range valueKeys {
				if v, ok := raw[k]; ok {
					filtered[k] = v
				} else {
					missingValues = append(missingValues, k)
				}
			}
			slices.Sort(missingValues)
			missingValues = slices.Compact(missingValues)
			valuesFn = func() map[string]any { return filtered }
		}
	} else if len(valueKeys) > 0 {
		missingValues = slices.Clone(valueKeys)
		slices.Sort(missingValues)
		missingValues = slices.Compact(missingValues)
	}

	dump, err = statedump.Capture(statedump.Source{
		World:     b.activeWorld(),
		Scenes:    b.cfg.Scenes,
		SceneName: b.cfg.SceneName,
		Values:    valuesFn,
	})
	if err != nil {
		return nil, nil, 0, 0, nil, fmt.Errorf("mcp: state dump: %w", err)
	}
	if dump.Scene == nil && b.standaloneScene() != nil {
		dump.Scene = &statedump.SceneState{
			Depth:   1,
			Current: "main",
		}
	}
	if loop := b.activeLoop(); loop != nil {
		dump.Rate = loop.StepRate()
		steps = loop.Steps()
		if steps > uint64(math.MaxInt) {
			dump.Steps = math.MaxInt
		} else {
			dump.Steps = int(steps)
		}
		gameTimeSeconds = loop.ElapsedSeconds()
	}
	yamlBytes, err = statedump.Encode(dump)
	if err != nil {
		return nil, nil, 0, 0, nil, fmt.Errorf("mcp: encode state dump: %w", err)
	}
	return dump, yamlBytes, steps, gameTimeSeconds, missingValues, nil
}

func (b *Bridge) handleStateDump(_ context.Context, raw json.RawMessage) (any, error) {
	if _, err := decodeArgs[struct{}](raw); err != nil {
		return nil, err
	}
	dump, yamlBytes, steps, gameTimeSeconds, _, err := b.captureLiveDump(nil)
	if err != nil {
		return nil, err
	}
	return StateDumpResult{
		Version:         dump.Version,
		Rate:            dump.Rate,
		Steps:           steps,
		GameTimeSeconds: gameTimeSeconds,
		Scene:           dump.Scene,
		Entities:        dump.Entities,
		Values:          dump.Values,
		YAML:            string(yamlBytes),
	}, nil
}

type stateCaptureArgs struct {
	Name        string   `json:"name"`
	Values      []string `json:"values"`
	IncludeYAML bool     `json:"include_yaml"`
}

func validateCaptureName(kind, name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("mcp: %s name is required", kind)
	}
	if trimmed != name {
		return "", fmt.Errorf("mcp: %s name %q must not have leading or trailing whitespace", kind, name)
	}
	if len(name) > maxStateCaptureNameLen {
		return "", fmt.Errorf("mcp: %s name %q exceeds %d bytes", kind, name, maxStateCaptureNameLen)
	}
	if strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("mcp: %s name %q contains NUL byte", kind, name)
	}
	return name, nil
}

func (b *Bridge) handleStateCapture(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[stateCaptureArgs](raw)
	if err != nil {
		return nil, err
	}
	name, err := validateCaptureName("state capture", args.Name)
	if err != nil {
		return nil, err
	}
	for _, k := range args.Values {
		if strings.TrimSpace(k) == "" {
			return nil, errors.New("mcp: state capture values keys must be non-empty")
		}
	}

	dump, yamlBytes, steps, gameTimeSeconds, missingValues, err := b.captureLiveDump(args.Values)
	if err != nil {
		return nil, err
	}
	entry := &storedStateCapture{
		name:            name,
		dump:            dump,
		yamlBytes:       yamlBytes,
		steps:           steps,
		gameTimeSeconds: gameTimeSeconds,
		valueFilter:     slices.Clone(args.Values),
	}
	overwritten, evicted, storedCount, err := b.ensureStateStore().put(entry)
	if err != nil {
		return nil, err
	}

	var valueKeys []string
	if len(dump.Values) > 0 {
		valueKeys = make([]string, 0, len(dump.Values))
		for k := range dump.Values {
			valueKeys = append(valueKeys, k)
		}
		slices.Sort(valueKeys)
	}
	var sceneName string
	if dump.Scene != nil {
		sceneName = dump.Scene.Current
	}
	res := StateCaptureResult{
		Name:            name,
		Overwritten:     overwritten,
		Evicted:         evicted,
		StoredCount:     storedCount,
		Version:         dump.Version,
		Rate:            dump.Rate,
		Steps:           steps,
		GameTimeSeconds: gameTimeSeconds,
		Scene:           sceneName,
		EntityCount:     len(dump.Entities),
		ValueKeys:       valueKeys,
		MissingValues:   missingValues,
		Bytes:           len(yamlBytes),
	}
	if args.IncludeYAML {
		res.YAML = string(yamlBytes)
	}
	return res, nil
}

type stateDiffArgs struct {
	From  string   `json:"from"`
	To    string   `json:"to"`
	Paths []string `json:"paths"`
}

func (b *Bridge) handleStateDiff(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[stateDiffArgs](raw)
	if err != nil {
		return nil, err
	}
	fromName, err := validateCaptureName("from capture", args.From)
	if err != nil {
		return nil, err
	}
	store := b.ensureStateStore()
	fromEntry, avail := store.get(fromName)
	if fromEntry == nil {
		if len(avail) == 0 {
			return nil, fmt.Errorf("mcp: state capture %q not found (no captures stored; call game_state_capture first)", fromName)
		}
		return nil, fmt.Errorf("mcp: state capture %q not found (available: %s)", fromName, strings.Join(avail, ", "))
	}

	var (
		toName  string
		toEntry *storedStateCapture
	)
	if args.To != "" {
		toName, err = validateCaptureName("to capture", args.To)
		if err != nil {
			return nil, err
		}
		toEntry, avail = store.get(toName)
		if toEntry == nil {
			if len(avail) == 0 {
				return nil, fmt.Errorf("mcp: state capture %q not found (no captures stored)", toName)
			}
			return nil, fmt.Errorf("mcp: state capture %q not found (available: %s)", toName, strings.Join(avail, ", "))
		}
	} else {
		toName = "<live>"
		dump, yamlBytes, steps, gameTimeSeconds, _, err := b.captureLiveDump(fromEntry.valueFilter)
		if err != nil {
			return nil, err
		}
		toEntry = &storedStateCapture{
			name:            toName,
			dump:            dump,
			yamlBytes:       yamlBytes,
			steps:           steps,
			gameTimeSeconds: gameTimeSeconds,
		}
	}

	filters, err := parseStateDiffFilters(args.Paths)
	if err != nil {
		return nil, err
	}

	fromDump := fromEntry.dump
	toDump := toEntry.dump
	fromYAML := fromEntry.yamlBytes
	toYAML := toEntry.yamlBytes
	if len(filters) > 0 {
		fromDump = filterDumpByPaths(fromEntry.dump, filters)
		toDump = filterDumpByPaths(toEntry.dump, filters)
		fromYAML, err = statedump.Encode(fromDump)
		if err != nil {
			return nil, fmt.Errorf("mcp: encode filtered from dump: %w", err)
		}
		toYAML, err = statedump.Encode(toDump)
		if err != nil {
			return nil, fmt.Errorf("mcp: encode filtered to dump: %w", err)
		}
	}

	diffText := statedump.Diff(fromYAML, toYAML)
	diffTruncated := false
	if len(diffText) > maxStateDiffBytes {
		cut := maxStateDiffBytes
		for cut > 0 && (diffText[cut]&0xc0) == 0x80 {
			cut--
		}
		diffText = diffText[:cut] + "\n... (diff truncated)\n"
		diffTruncated = true
	}

	res := StateDiffResult{
		From:          fromName,
		To:            toName,
		Diff:          diffText,
		DiffTruncated: diffTruncated,
	}
	if len(filters) == 0 || filtersMatchTop(filters, "steps") {
		res.StepsDelta = int64(toDump.Steps) - int64(fromDump.Steps)
	}
	res.SceneChanged = !sceneStateEqual(fromDump.Scene, toDump.Scene)

	var truncated bool
	res.AddedEntities, res.RemovedEntities, res.ModifiedEntities, res.ChangedEntities, res.EntityChanges, truncated = diffDumpEntities(fromDump, toDump)
	valueChanges, valTrunc := diffDumpValues(fromDump.Values, toDump.Values)
	res.ValueChanges = valueChanges
	res.ChangesTruncated = truncated || valTrunc
	res.Equal = diffText == "" && !res.SceneChanged && len(res.ChangedEntities) == 0 && len(res.ValueChanges) == 0
	return res, nil
}

type statePathFilter struct {
	raw   string
	parts []string
}

func parseStateDiffFilters(paths []string) ([]statePathFilter, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]statePathFilter, 0, len(paths))
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			return nil, errors.New("mcp: state diff paths entries must be non-empty")
		}
		// Allow shorthand "entity:<idx>:<gen>[.<comp>[.<field>...]]" as "entities.entity:<idx>:<gen>...".
		norm := trimmed
		if strings.HasPrefix(norm, entityRefPrefix) {
			norm = "entities." + norm
		}
		parts := strings.Split(norm, ".")
		for _, seg := range parts {
			if seg == "" {
				return nil, fmt.Errorf("mcp: invalid state diff path %q: empty segment", p)
			}
		}
		out = append(out, statePathFilter{raw: p, parts: parts})
	}
	return out, nil
}

func filtersMatchTop(filters []statePathFilter, top string) bool {
	for _, f := range filters {
		if f.parts[0] == top {
			return true
		}
	}
	return false
}

func filterDumpByPaths(d *statedump.Dump, filters []statePathFilter) *statedump.Dump {
	out := &statedump.Dump{Version: d.Version}
	if filtersMatchTop(filters, "seed") {
		out.Seed = d.Seed
	}
	if filtersMatchTop(filters, "rate") {
		out.Rate = d.Rate
	}
	if filtersMatchTop(filters, "steps") {
		out.Steps = d.Steps
	}
	if d.Scene != nil && filtersMatchTop(filters, "scene") {
		sc := &statedump.SceneState{}
		wholeScene := false
		for _, f := range filters {
			if f.parts[0] != "scene" {
				continue
			}
			if len(f.parts) == 1 {
				wholeScene = true
				break
			}
			switch f.parts[1] {
			case "depth":
				sc.Depth = d.Scene.Depth
			case "current":
				sc.Current = d.Scene.Current
			case "transitioning":
				sc.Transitioning = d.Scene.Transitioning
				if sc.Transitioning {
					sc.Progress = d.Scene.Progress
				}
			case "progress":
				sc.Transitioning = d.Scene.Transitioning
				sc.Progress = d.Scene.Progress
			}
		}
		if wholeScene {
			cp := *d.Scene
			out.Scene = &cp
		} else {
			out.Scene = sc
		}
	}
	if len(d.Values) > 0 && filtersMatchTop(filters, "values") {
		filtered := make(map[string]json.RawMessage)
		for k, raw := range d.Values {
			var subPaths [][]string
			wholeKey := false
			for _, f := range filters {
				if f.parts[0] != "values" {
					continue
				}
				if len(f.parts) == 1 {
					wholeKey = true
					break
				}
				if f.parts[1] == k {
					if len(f.parts) == 2 {
						wholeKey = true
						break
					}
					subPaths = append(subPaths, f.parts[2:])
				}
			}
			if wholeKey {
				filtered[k] = bytes.Clone(raw)
			} else if len(subPaths) > 0 {
				if sub, ok := filterJSONByPaths(raw, subPaths); ok {
					filtered[k] = sub
				}
			}
		}
		if len(filtered) > 0 {
			out.Values = filtered
		}
	}
	if len(d.Entities) > 0 && filtersMatchTop(filters, "entities") {
		keptIndices := make(map[uint32]struct{}, len(d.Entities))
		var kept []entities.EntitySnapshot
		for _, e := range d.Entities {
			ref := formatSnapshotRef(e)
			wholeEntity := false
			wantParent := false
			compWhole := make(map[string]bool)
			compSub := make(map[string][][]string)
			for _, f := range filters {
				if f.parts[0] != "entities" {
					continue
				}
				if len(f.parts) == 1 {
					wholeEntity = true
					break
				}
				if f.parts[1] != ref && f.parts[1] != "*" {
					continue
				}
				if len(f.parts) == 2 {
					wholeEntity = true
					break
				}
				if f.parts[2] == "parent" {
					wantParent = true
					continue
				}
				compName := f.parts[2]
				if len(f.parts) == 3 {
					compWhole[compName] = true
				} else {
					compSub[compName] = append(compSub[compName], f.parts[3:])
				}
			}
			if wholeEntity {
				keptIndices[e.Index] = struct{}{}
				kept = append(kept, cloneEntitySnapshot(e))
				continue
			}
			if !wantParent && len(compWhole) == 0 && len(compSub) == 0 {
				continue
			}
			es := entities.EntitySnapshot{
				Index:      e.Index,
				Generation: e.Generation,
			}
			if wantParent && e.Parent != nil {
				p := *e.Parent
				es.Parent = &p
			}
			for _, c := range e.Components {
				if compWhole[c.Type] {
					es.Components = append(es.Components, entities.ComponentSnapshot{
						Type: c.Type,
						Data: bytes.Clone(c.Data),
					})
				} else if subs := compSub[c.Type]; len(subs) > 0 {
					if filteredData, ok := filterJSONByPaths(c.Data, subs); ok {
						es.Components = append(es.Components, entities.ComponentSnapshot{
							Type: c.Type,
							Data: filteredData,
						})
					}
				}
			}
			if wantParent || len(es.Components) > 0 {
				keptIndices[es.Index] = struct{}{}
				kept = append(kept, es)
			}
		}
		// Ensure statedump.Validate's parent-in-dump invariant holds when a child was filtered without its parent.
		for i := range kept {
			if kept[i].Parent != nil {
				if _, ok := keptIndices[*kept[i].Parent]; !ok {
					kept[i].Parent = nil
				}
			}
		}
		out.Entities = kept
	}
	return out
}

func cloneEntitySnapshot(e entities.EntitySnapshot) entities.EntitySnapshot {
	cp := entities.EntitySnapshot{
		Index:      e.Index,
		Generation: e.Generation,
	}
	if e.Parent != nil {
		p := *e.Parent
		cp.Parent = &p
	}
	if len(e.Components) > 0 {
		cp.Components = make([]entities.ComponentSnapshot, len(e.Components))
		for i, c := range e.Components {
			cp.Components[i] = entities.ComponentSnapshot{
				Type: c.Type,
				Data: bytes.Clone(c.Data),
			}
		}
	}
	return cp
}

func filterJSONByPaths(raw json.RawMessage, paths [][]string) (json.RawMessage, bool) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false
	}
	filtered, ok := filterAnyByPaths(v, paths)
	if !ok {
		return nil, false
	}
	b, err := json.Marshal(filtered)
	if err != nil {
		return nil, false
	}
	return json.RawMessage(b), true
}

func filterAnyByPaths(v any, paths [][]string) (any, bool) {
	for _, p := range paths {
		if len(p) == 0 {
			return v, true
		}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	out := make(map[string]any)
	for k, child := range m {
		var sub [][]string
		whole := false
		for _, p := range paths {
			if p[0] == k || p[0] == "*" {
				if len(p) == 1 {
					whole = true
					break
				}
				sub = append(sub, p[1:])
			}
		}
		if whole {
			out[k] = child
		} else if len(sub) > 0 {
			if fc, ok := filterAnyByPaths(child, sub); ok {
				out[k] = fc
			}
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func formatSnapshotRef(e entities.EntitySnapshot) string {
	return fmt.Sprintf("%s%d:%d", entityRefPrefix, e.Index, e.Generation)
}

func sceneStateEqual(a, b *statedump.SceneState) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func diffDumpEntities(from, to *statedump.Dump) (added, removed, modified, changed []string, details []StateEntityChange, truncated bool) {
	fromByRef := make(map[string]entities.EntitySnapshot, len(from.Entities))
	fromParentRef := make(map[uint32]string, len(from.Entities))
	for _, e := range from.Entities {
		ref := formatSnapshotRef(e)
		fromByRef[ref] = e
		fromParentRef[e.Index] = ref
	}
	toByRef := make(map[string]entities.EntitySnapshot, len(to.Entities))
	toParentRef := make(map[uint32]string, len(to.Entities))
	for _, e := range to.Entities {
		ref := formatSnapshotRef(e)
		toByRef[ref] = e
		toParentRef[e.Index] = ref
	}

	allRefs := make([]string, 0, len(fromByRef)+len(toByRef))
	seen := make(map[string]struct{}, len(fromByRef)+len(toByRef))
	for _, e := range from.Entities {
		ref := formatSnapshotRef(e)
		seen[ref] = struct{}{}
		allRefs = append(allRefs, ref)
	}
	for _, e := range to.Entities {
		ref := formatSnapshotRef(e)
		if _, ok := seen[ref]; !ok {
			allRefs = append(allRefs, ref)
		}
	}
	// Sort deterministically by (Index, Generation).
	slices.SortFunc(allRefs, func(a, b string) int {
		ea, errA := ParseEntityRef(a)
		eb, errB := ParseEntityRef(b)
		if errA == nil && errB == nil {
			if c := cmp.Compare(ea.Index(), eb.Index()); c != 0 {
				return c
			}
			return cmp.Compare(ea.Generation(), eb.Generation())
		}
		return cmp.Compare(a, b)
	})

	for _, ref := range allRefs {
		fe, inFrom := fromByRef[ref]
		te, inTo := toByRef[ref]
		switch {
		case !inFrom && inTo:
			added = append(added, ref)
			changed = append(changed, ref)
			if len(details) < maxStructuredChanges {
				compNames := make([]string, len(te.Components))
				for i, c := range te.Components {
					compNames[i] = c.Type
				}
				var pAfter string
				if te.Parent != nil {
					pAfter = toParentRef[*te.Parent]
				}
				details = append(details, StateEntityChange{
					Ref:             ref,
					Kind:            "added",
					ParentAfter:     pAfter,
					AddedComponents: compNames,
				})
			} else {
				truncated = true
			}
		case inFrom && !inTo:
			removed = append(removed, ref)
			changed = append(changed, ref)
			if len(details) < maxStructuredChanges {
				compNames := make([]string, len(fe.Components))
				for i, c := range fe.Components {
					compNames[i] = c.Type
				}
				var pBefore string
				if fe.Parent != nil {
					pBefore = fromParentRef[*fe.Parent]
				}
				details = append(details, StateEntityChange{
					Ref:               ref,
					Kind:              "removed",
					ParentBefore:      pBefore,
					RemovedComponents: compNames,
				})
			} else {
				truncated = true
			}
		default:
			var pBefore, pAfter string
			if fe.Parent != nil {
				pBefore = fromParentRef[*fe.Parent]
			}
			if te.Parent != nil {
				pAfter = toParentRef[*te.Parent]
			}
			parentChanged := pBefore != pAfter

			fcMap := make(map[string]json.RawMessage, len(fe.Components))
			for _, c := range fe.Components {
				fcMap[c.Type] = c.Data
			}
			tcMap := make(map[string]json.RawMessage, len(te.Components))
			for _, c := range te.Components {
				tcMap[c.Type] = c.Data
			}
			compKeys := make([]string, 0, len(fcMap)+len(tcMap))
			for k := range fcMap {
				compKeys = append(compKeys, k)
			}
			for k := range tcMap {
				if _, ok := fcMap[k]; !ok {
					compKeys = append(compKeys, k)
				}
			}
			slices.Sort(compKeys)

			var (
				addedComps   []string
				removedComps []string
				changedComps []string
				fieldChanges []StateFieldChange
			)
			for _, ck := range compKeys {
				fd, okF := fcMap[ck]
				td, okT := tcMap[ck]
				switch {
				case !okF && okT:
					addedComps = append(addedComps, ck)
					fieldChanges = append(fieldChanges, StateFieldChange{
						Path:  ck,
						Kind:  "added",
						After: canonicalJSON(td),
					})
				case okF && !okT:
					removedComps = append(removedComps, ck)
					fieldChanges = append(fieldChanges, StateFieldChange{
						Path:   ck,
						Kind:   "removed",
						Before: canonicalJSON(fd),
					})
				default:
					if !jsonValuesEqual(fd, td) {
						changedComps = append(changedComps, ck)
						fieldChanges = append(fieldChanges, diffJSONValues(ck, fd, td)...)
					}
				}
			}
			if parentChanged || len(addedComps) > 0 || len(removedComps) > 0 || len(changedComps) > 0 {
				modified = append(modified, ref)
				changed = append(changed, ref)
				if len(details) < maxStructuredChanges {
					if len(fieldChanges) > maxStructuredChanges {
						fieldChanges = fieldChanges[:maxStructuredChanges]
						truncated = true
					}
					details = append(details, StateEntityChange{
						Ref:               ref,
						Kind:              "modified",
						ParentChanged:     parentChanged,
						ParentBefore:      pBefore,
						ParentAfter:       pAfter,
						AddedComponents:   addedComps,
						RemovedComponents: removedComps,
						ChangedComponents: changedComps,
						FieldChanges:      fieldChanges,
					})
				} else {
					truncated = true
				}
			}
		}
	}
	return added, removed, modified, changed, details, truncated
}

func diffDumpValues(from, to map[string]json.RawMessage) ([]StateFieldChange, bool) {
	if len(from) == 0 && len(to) == 0 {
		return nil, false
	}
	var keys []string
	for k := range from {
		keys = append(keys, k)
	}
	for k := range to {
		if _, ok := from[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)

	var (
		out       []StateFieldChange
		truncated bool
	)
	for _, k := range keys {
		fv, okF := from[k]
		tv, okT := to[k]
		path := "values." + k
		switch {
		case !okF && okT:
			out = append(out, StateFieldChange{Path: path, Kind: "added", After: canonicalJSON(tv)})
		case okF && !okT:
			out = append(out, StateFieldChange{Path: path, Kind: "removed", Before: canonicalJSON(fv)})
		default:
			if !jsonValuesEqual(fv, tv) {
				out = append(out, diffJSONValues(path, fv, tv)...)
			}
		}
		if len(out) > maxStructuredChanges {
			out = out[:maxStructuredChanges]
			truncated = true
			break
		}
	}
	return out, truncated
}

func canonicalJSON(raw json.RawMessage) json.RawMessage {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return bytes.Clone(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return bytes.Clone(raw)
	}
	return json.RawMessage(b)
}

func jsonValuesEqual(a, b json.RawMessage) bool {
	if bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b)) {
		return true
	}
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

func diffJSONValues(prefix string, a, b json.RawMessage) []StateFieldChange {
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		return []StateFieldChange{{Path: prefix, Kind: "changed", Before: bytes.Clone(a), After: bytes.Clone(b)}}
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		return []StateFieldChange{{Path: prefix, Kind: "changed", Before: bytes.Clone(a), After: bytes.Clone(b)}}
	}
	var out []StateFieldChange
	diffAnyValues(prefix, va, vb, 0, &out)
	return out
}

func diffAnyValues(prefix string, a, b any, depth int, out *[]StateFieldChange) {
	if len(*out) >= maxStructuredChanges {
		return
	}
	if reflect.DeepEqual(a, b) {
		return
	}
	am, aIsMap := a.(map[string]any)
	bm, bIsMap := b.(map[string]any)
	if aIsMap && bIsMap && depth < 16 {
		var keys []string
		for k := range am {
			keys = append(keys, k)
		}
		for k := range bm {
			if _, ok := am[k]; !ok {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		for _, k := range keys {
			if len(*out) >= maxStructuredChanges {
				return
			}
			childPath := prefix + "." + k
			av, okA := am[k]
			bv, okB := bm[k]
			switch {
			case !okA && okB:
				*out = append(*out, StateFieldChange{Path: childPath, Kind: "added", After: mustMarshalJSON(bv)})
			case okA && !okB:
				*out = append(*out, StateFieldChange{Path: childPath, Kind: "removed", Before: mustMarshalJSON(av)})
			default:
				diffAnyValues(childPath, av, bv, depth+1, out)
			}
		}
		return
	}
	*out = append(*out, StateFieldChange{
		Path:   prefix,
		Kind:   "changed",
		Before: mustMarshalJSON(a),
		After:  mustMarshalJSON(b),
	})
}

func mustMarshalJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}
