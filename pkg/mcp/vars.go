package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/nsgo/automation"
)

// Tool names for shared game variable and switch tools registered by Bridge.
const (
	// ToolVarsList lists declared flow.Vars entries and any active per-object self variables.
	ToolVarsList = "game_vars_list"
	// ToolVarGet reads one declared flow.Vars entry's current value and metadata by name.
	ToolVarGet = "game_var_get"
	// ToolVarSet updates one declared flow.Vars entry's value with strict kind and range checking.
	ToolVarSet = "game_var_set"
)

// VarResult describes one declared flow.Vars entry returned by game_vars_list and game_var_get.
type VarResult struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Value   any      `json:"value"`
	Default any      `json:"default"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Doc     string   `json:"doc,omitempty"`
}

// VarsListResult is the JSON result of game_vars_list.
type VarsListResult struct {
	Total int                       `json:"total"`
	Vars  []VarResult               `json:"vars"`
	Self  map[string]map[string]any `json:"self,omitempty"`
}

// VarSetResult is the JSON result of game_var_set.
type VarSetResult struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	OldValue any      `json:"old_value"`
	Value    any      `json:"value"`
	Default  any      `json:"default"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Clamped  bool     `json:"clamped,omitempty"`
}

var (
	schemaVarsList = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"prefix": {
				"type": "string",
				"description": "Filter declared variables to those whose name starts with this prefix."
			},
			"include_self": {
				"type": "boolean",
				"description": "Include per-object self variables in the response (default true)."
			}
		}
	}`)

	schemaVarGet = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["name"],
		"properties": {
			"name": {
				"type": "string",
				"description": "Declared variable name returned by game_vars_list (leading '$' optional)."
			}
		}
	}`)

	schemaVarSet = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["name", "value"],
		"properties": {
			"name": {
				"type": "string",
				"description": "Declared variable name returned by game_vars_list (leading '$' optional)."
			},
			"value": {
				"description": "New value matching the variable's declared kind (bool, int, float, or string)."
			},
			"clamp": {
				"type": "boolean",
				"description": "Clamp out-of-range numeric values to [min, max] instead of returning an error (default false)."
			}
		}
	}`)
)

func (b *Bridge) varsTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolVarsList,
			Description: "List declared project-wide game variables and switches with current values, defaults, bounds, docs, and active per-object self variables.",
			InputSchema: schemaVarsList,
			Handler:     b.handleVarsList,
		},
		{
			Name:        ToolVarGet,
			Description: "Read one declared project-wide game variable's current value, kind, default, bounds, and doc by name.",
			InputSchema: schemaVarGet,
			Handler:     b.handleVarGet,
		},
		{
			Name:        ToolVarSet,
			Description: "Update one declared project-wide game variable with strict kind and range validation (or optional clamping), returning both old and new values.",
			InputSchema: schemaVarSet,
			Handler:     b.handleVarSet,
		},
	}
}

func (b *Bridge) activeVars() *flow.Vars {
	if b == nil {
		return nil
	}
	if b.cfg.VarsFunc != nil {
		if v := b.cfg.VarsFunc(); v != nil {
			return v
		}
	}
	return b.cfg.Vars
}

func (b *Bridge) requireVars() (*flow.Vars, error) {
	v := b.activeVars()
	if v == nil {
		return nil, errors.New("mcp: no flow.Vars store configured")
	}
	return v, nil
}

func varResultFrom(info flow.VarInfo) VarResult {
	res := VarResult{
		Name:    info.Name,
		Kind:    string(info.Kind),
		Value:   sanitizeJSONNumber(info.Value),
		Default: sanitizeJSONNumber(info.Default),
		Doc:     info.Doc,
	}
	if info.Min != nil {
		m := *info.Min
		res.Min = &m
	}
	if info.Max != nil {
		m := *info.Max
		res.Max = &m
	}
	return res
}

func missingVarError(v *flow.Vars, name string) error {
	names := v.Names()
	if len(names) == 0 {
		return fmt.Errorf("mcp: variable %q not found (no variables declared)", name)
	}
	return fmt.Errorf("mcp: variable %q not found (available: %s)", name, strings.Join(names, ", "))
}

type varsListArgs struct {
	Prefix      string `json:"prefix"`
	IncludeSelf *bool  `json:"include_self"`
}

func (b *Bridge) handleVarsList(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[varsListArgs](raw)
	if err != nil {
		return nil, err
	}
	store, err := b.requireVars()
	if err != nil {
		return nil, err
	}
	all := store.List()
	out := make([]VarResult, 0, len(all))
	for _, info := range all {
		if args.Prefix != "" && !strings.HasPrefix(info.Name, args.Prefix) {
			continue
		}
		out = append(out, varResultFrom(info))
	}
	includeSelf := args.IncludeSelf == nil || *args.IncludeSelf
	var self map[string]map[string]any
	if includeSelf {
		snap := store.Snapshot()
		if len(snap.Self) > 0 {
			self = make(map[string]map[string]any, len(snap.Self))
			for objID, m := range snap.Self {
				cp := make(map[string]any, len(m))
				for k, v := range m {
					cp[k] = sanitizeJSONNumber(v)
				}
				self[objID] = cp
			}
		}
	}
	return VarsListResult{
		Total: len(out),
		Vars:  out,
		Self:  self,
	}, nil
}

type varGetArgs struct {
	Name string `json:"name"`
}

func (b *Bridge) handleVarGet(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[varGetArgs](raw)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Name) == "" {
		return nil, errors.New("mcp: name is required")
	}
	store, err := b.requireVars()
	if err != nil {
		return nil, err
	}
	info, ok := store.Info(args.Name)
	if !ok {
		return nil, missingVarError(store, args.Name)
	}
	return varResultFrom(info), nil
}

type varSetArgs struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
	Clamp bool            `json:"clamp"`
}

func (b *Bridge) handleVarSet(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[varSetArgs](raw)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Name) == "" {
		return nil, errors.New("mcp: name is required")
	}
	rawVal := bytes.TrimSpace(args.Value)
	if len(rawVal) == 0 {
		return nil, errors.New("mcp: value is required")
	}
	store, err := b.requireVars()
	if err != nil {
		return nil, err
	}
	info, ok := store.Info(args.Name)
	if !ok {
		return nil, missingVarError(store, args.Name)
	}
	if bytes.Equal(rawVal, []byte("null")) {
		return nil, fmt.Errorf("mcp: variable %q expects %s, got null", info.Name, info.Kind)
	}

	var (
		typedVal  any
		saturated bool
	)
	switch info.Kind {
	case flow.VarFloat:
		var v float64
		if err := json.Unmarshal(rawVal, &v); err != nil {
			if kind := jsonValueKind(rawVal); kind == "number" {
				return nil, fmt.Errorf("mcp: variable %q requires a finite float, got %s", info.Name, string(rawVal))
			}
			return nil, fmt.Errorf("mcp: variable %q expects float, got %s", info.Name, jsonValueKind(rawVal))
		}
		typedVal = v
	case flow.VarInt:
		v, sat, err := decodeStrictInt(info.Name, rawVal, args.Clamp)
		if err != nil {
			return nil, err
		}
		typedVal, saturated = v, sat
	case flow.VarBool:
		var v bool
		if err := json.Unmarshal(rawVal, &v); err != nil {
			return nil, fmt.Errorf("mcp: variable %q expects bool, got %s", info.Name, jsonValueKind(rawVal))
		}
		typedVal = v
	case flow.VarString:
		var v string
		if err := json.Unmarshal(rawVal, &v); err != nil {
			return nil, fmt.Errorf("mcp: variable %q expects string, got %s", info.Name, jsonValueKind(rawVal))
		}
		typedVal = v
	default:
		return nil, fmt.Errorf("mcp: unsupported variable kind %q for %q", info.Kind, info.Name)
	}

	oldVal, _ := store.Get(info.Name)
	var clamped bool
	if args.Clamp {
		var newVal any
		oldVal, newVal, clamped, err = store.SetClamped(info.Name, typedVal)
		_ = newVal
	} else {
		err = store.Set(info.Name, typedVal)
	}
	if err != nil {
		return nil, fmt.Errorf("mcp: %w", err)
	}
	afterInfo, _ := store.Info(info.Name)
	after := varResultFrom(afterInfo)
	return VarSetResult{
		Name:     after.Name,
		Kind:     after.Kind,
		OldValue: sanitizeJSONNumber(oldVal),
		Value:    after.Value,
		Default:  after.Default,
		Min:      after.Min,
		Max:      after.Max,
		Clamped:  clamped || saturated,
	}, nil
}
