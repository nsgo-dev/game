package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/nsgo/automation"
)

// Tool names for debug console and tunable tools registered by Bridge.
const (
	// ToolTunablesList lists registered tunables on the debug console with values, defaults and bounds.
	ToolTunablesList = "game_tunables_list"
	// ToolTunableGet reads one registered tunable's current value and metadata by name.
	ToolTunableGet = "game_tunable_get"
	// ToolTunableSet updates one registered tunable's value with type and range checking.
	ToolTunableSet = "game_tunable_set"
	// ToolConsoleCommandsList lists built-in and custom commands registered on the debug console.
	ToolConsoleCommandsList = "game_console_commands_list"
	// ToolConsoleExec runs a command string through the debug console and returns its output.
	ToolConsoleExec = "game_console_exec"
)

// TunableInfo describes one registered tunable variable returned by game_tunables_list and game_tunable_get.
type TunableInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Value   any    `json:"value"`
	Default any    `json:"default"`
	Min     any    `json:"min,omitempty"`
	Max     any    `json:"max,omitempty"`
}

// TunablesListResult is the JSON result of game_tunables_list.
type TunablesListResult struct {
	Total    int           `json:"total"`
	Tunables []TunableInfo `json:"tunables"`
}

// TunableSetResult is the JSON result of game_tunable_set.
type TunableSetResult struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	OldValue any    `json:"old_value"`
	Value    any    `json:"value"`
	Default  any    `json:"default"`
	Min      any    `json:"min,omitempty"`
	Max      any    `json:"max,omitempty"`
	Clamped  bool   `json:"clamped,omitempty"`
}

// ConsoleCommandInfo describes one debug console command returned by game_console_commands_list.
type ConsoleCommandInfo struct {
	Name    string `json:"name"`
	Help    string `json:"help,omitempty"`
	Builtin bool   `json:"builtin"`
}

// ConsoleCommandsListResult is the JSON result of game_console_commands_list.
type ConsoleCommandsListResult struct {
	Total    int                  `json:"total"`
	Commands []ConsoleCommandInfo `json:"commands"`
}

// ConsoleExecResult is the JSON result of game_console_exec.
type ConsoleExecResult struct {
	Command string   `json:"command"`
	OK      bool     `json:"ok"`
	Output  string   `json:"output"`
	Lines   []string `json:"lines"`
	Error   string   `json:"error,omitempty"`
}

var (
	schemaTunablesList = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"prefix": {
				"type": "string",
				"description": "Filter tunables to those whose name starts with this prefix."
			}
		}
	}`)

	schemaTunableGet = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["name"],
		"properties": {
			"name": {
				"type": "string",
				"description": "Registered tunable name returned by game_tunables_list."
			}
		}
	}`)

	schemaTunableSet = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["name", "value"],
		"properties": {
			"name": {
				"type": "string",
				"description": "Registered tunable name returned by game_tunables_list."
			},
			"value": {
				"description": "New value matching the tunable's type (number for float/int, boolean for bool, string for string)."
			},
			"clamp": {
				"type": "boolean",
				"description": "Clamp out-of-range numeric values to [min, max] instead of returning an error (default false)."
			}
		}
	}`)

	schemaConsoleCommandsList = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"include_builtin": {
				"type": "boolean",
				"description": "Include built-in console commands (clear, get, help, list, set) alongside game-registered commands (default true)."
			}
		}
	}`)

	schemaConsoleExec = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["command"],
		"properties": {
			"command": {
				"type": "string",
				"description": "Console command line to run (for example \"set gravity 1200\" or \"spawn enemy 3\")."
			}
		}
	}`)
)

func (b *Bridge) consoleTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolTunablesList,
			Description: "List registered debug console tunables sorted by name, including type, current value, registration default and numeric bounds.",
			InputSchema: schemaTunablesList,
			Handler:     b.handleTunablesList,
		},
		{
			Name:        ToolTunableGet,
			Description: "Read one registered debug console tunable's current value, type, default and numeric bounds by name.",
			InputSchema: schemaTunableGet,
			Handler:     b.handleTunableGet,
		},
		{
			Name:        ToolTunableSet,
			Description: "Set one registered debug console tunable's value with strict type checking and range validation (or optional clamping), returning both old and new values.",
			InputSchema: schemaTunableSet,
			Handler:     b.handleTunableSet,
		},
		{
			Name:        ToolConsoleCommandsList,
			Description: "List built-in and custom commands registered on the debug console with their usage descriptions.",
			InputSchema: schemaConsoleCommandsList,
			Handler:     b.handleConsoleCommandsList,
		},
		{
			Name:        ToolConsoleExec,
			Description: "Execute a command line on the debug console and return structured execution status, output text, output lines and any command error.",
			InputSchema: schemaConsoleExec,
			Handler:     b.handleConsoleExec,
		},
	}
}

func (b *Bridge) activeConsole() *debug.Console {
	if b == nil {
		return nil
	}
	if b.cfg.ConsoleFunc != nil {
		if c := b.cfg.ConsoleFunc(); c != nil {
			return c
		}
	}
	return b.cfg.Console
}

func (b *Bridge) requireConsole() (*debug.Console, error) {
	c := b.activeConsole()
	if c == nil {
		return nil, errors.New("mcp: no debug console configured")
	}
	return c, nil
}

func sanitizeJSONNumber(v any) any {
	switch f := v.(type) {
	case float64:
		switch {
		case math.IsNaN(f):
			return "NaN"
		case math.IsInf(f, 1):
			return "+Inf"
		case math.IsInf(f, -1):
			return "-Inf"
		}
	case float32:
		f64 := float64(f)
		switch {
		case math.IsNaN(f64):
			return "NaN"
		case math.IsInf(f64, 1):
			return "+Inf"
		case math.IsInf(f64, -1):
			return "-Inf"
		}
	}
	return v
}

func tunableInfoFrom(t debug.TunableInfo) TunableInfo {
	return TunableInfo{
		Name:    t.Name,
		Type:    t.Type,
		Value:   sanitizeJSONNumber(t.Value),
		Default: sanitizeJSONNumber(t.Default),
		Min:     sanitizeJSONNumber(t.Min),
		Max:     sanitizeJSONNumber(t.Max),
	}
}

func missingTunableError(c *debug.Console, name string) error {
	all := c.Tunables()
	if len(all) == 0 {
		return fmt.Errorf("mcp: tunable %q not found (no tunables registered)", name)
	}
	avail := make([]string, len(all))
	for i, t := range all {
		avail[i] = t.Name
	}
	return fmt.Errorf("mcp: tunable %q not found (available: %s)", name, strings.Join(avail, ", "))
}

type tunablesListArgs struct {
	Prefix string `json:"prefix"`
}

func (b *Bridge) handleTunablesList(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[tunablesListArgs](raw)
	if err != nil {
		return nil, err
	}
	c, err := b.requireConsole()
	if err != nil {
		return nil, err
	}
	all := c.Tunables()
	out := make([]TunableInfo, 0, len(all))
	for _, t := range all {
		if args.Prefix != "" && !strings.HasPrefix(t.Name, args.Prefix) {
			continue
		}
		out = append(out, tunableInfoFrom(t))
	}
	return TunablesListResult{
		Total:    len(out),
		Tunables: out,
	}, nil
}

type tunableGetArgs struct {
	Name string `json:"name"`
}

func (b *Bridge) handleTunableGet(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[tunableGetArgs](raw)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Name) == "" {
		return nil, errors.New("mcp: name is required")
	}
	c, err := b.requireConsole()
	if err != nil {
		return nil, err
	}
	info, ok := c.Tunable(args.Name)
	if !ok {
		return nil, missingTunableError(c, args.Name)
	}
	return tunableInfoFrom(info), nil
}

type tunableSetArgs struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
	Clamp bool            `json:"clamp"`
}

func (b *Bridge) handleTunableSet(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[tunableSetArgs](raw)
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
	c, err := b.requireConsole()
	if err != nil {
		return nil, err
	}
	info, ok := c.Tunable(args.Name)
	if !ok {
		return nil, missingTunableError(c, args.Name)
	}
	if bytes.Equal(rawVal, []byte("null")) {
		return nil, fmt.Errorf("mcp: tunable %q expects %s, got null", info.Name, info.Type)
	}

	var (
		typedVal any
		clamped  bool
	)
	switch info.Type {
	case "float":
		var v float64
		if err := json.Unmarshal(rawVal, &v); err != nil {
			if kind := jsonValueKind(rawVal); kind == "number" {
				return nil, fmt.Errorf("mcp: tunable %q requires a finite float, got %s", info.Name, string(rawVal))
			}
			return nil, fmt.Errorf("mcp: tunable %q expects float, got %s", info.Name, jsonValueKind(rawVal))
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("mcp: tunable %q requires a finite float", info.Name)
		}
		if args.Clamp {
			if minF, ok := info.Min.(float64); ok && !math.IsNaN(minF) && !math.IsInf(minF, 0) && v < minF {
				v = minF
				clamped = true
			}
			if maxF, ok := info.Max.(float64); ok && !math.IsNaN(maxF) && !math.IsInf(maxF, 0) && v > maxF {
				v = maxF
				clamped = true
			}
		}
		typedVal = v
	case "int":
		v, err := decodeStrictInt(info.Name, rawVal)
		if err != nil {
			return nil, err
		}
		if args.Clamp {
			if minI, ok := info.Min.(int); ok && v < minI {
				v = minI
				clamped = true
			}
			if maxI, ok := info.Max.(int); ok && v > maxI {
				v = maxI
				clamped = true
			}
		}
		typedVal = v
	case "bool":
		var v bool
		if err := json.Unmarshal(rawVal, &v); err != nil {
			return nil, fmt.Errorf("mcp: tunable %q expects bool, got %s", info.Name, jsonValueKind(rawVal))
		}
		typedVal = v
	case "string":
		var v string
		if err := json.Unmarshal(rawVal, &v); err != nil {
			return nil, fmt.Errorf("mcp: tunable %q expects string, got %s", info.Name, jsonValueKind(rawVal))
		}
		typedVal = v
	default:
		return nil, fmt.Errorf("mcp: unsupported tunable type %q for %q", info.Type, info.Name)
	}

	oldVal, newVal, err := c.SetTunable(args.Name, typedVal)
	if err != nil {
		return nil, fmt.Errorf("mcp: %w", err)
	}
	return TunableSetResult{
		Name:     info.Name,
		Type:     info.Type,
		OldValue: sanitizeJSONNumber(oldVal),
		Value:    sanitizeJSONNumber(newVal),
		Default:  sanitizeJSONNumber(info.Default),
		Min:      sanitizeJSONNumber(info.Min),
		Max:      sanitizeJSONNumber(info.Max),
		Clamped:  clamped,
	}, nil
}

func decodeStrictInt(name string, raw []byte) (int, error) {
	if kind := jsonValueKind(raw); kind != "number" {
		return 0, fmt.Errorf("mcp: tunable %q expects int, got %s", name, kind)
	}
	var i int
	if err := json.Unmarshal(raw, &i); err == nil {
		return i, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("mcp: tunable %q expects int, got invalid number %s", name, string(raw))
	}
	if f < float64(math.MinInt) || f > float64(math.MaxInt) {
		return 0, fmt.Errorf("mcp: tunable %q integer %s overflows int", name, string(raw))
	}
	r, ok := new(big.Rat).SetString(string(raw))
	if !ok {
		return 0, fmt.Errorf("mcp: tunable %q expects int, got invalid number %s", name, string(raw))
	}
	if !r.IsInt() {
		return 0, fmt.Errorf("mcp: tunable %q expects int, got non-integer number %s", name, string(raw))
	}
	num := r.Num()
	if !num.IsInt64() {
		return 0, fmt.Errorf("mcp: tunable %q integer %s overflows int", name, string(raw))
	}
	v64 := num.Int64()
	if v64 < math.MinInt || v64 > math.MaxInt {
		return 0, fmt.Errorf("mcp: tunable %q integer %s overflows int", name, string(raw))
	}
	return int(v64), nil
}

func jsonValueKind(raw []byte) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "empty"
	}
	switch trimmed[0] {
	case '"':
		return "string"
	case '{':
		return "object"
	case '[':
		return "array"
	case 't', 'f':
		return "bool"
	case 'n':
		return "null"
	default:
		if trimmed[0] == '-' || (trimmed[0] >= '0' && trimmed[0] <= '9') {
			return "number"
		}
		return "invalid"
	}
}

type consoleCommandsListArgs struct {
	IncludeBuiltin *bool `json:"include_builtin"`
}

func (b *Bridge) handleConsoleCommandsList(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[consoleCommandsListArgs](raw)
	if err != nil {
		return nil, err
	}
	includeBuiltin := true
	if args.IncludeBuiltin != nil {
		includeBuiltin = *args.IncludeBuiltin
	}
	c, err := b.requireConsole()
	if err != nil {
		return nil, err
	}
	all := c.Commands()
	out := make([]ConsoleCommandInfo, 0, len(all))
	for _, cmd := range all {
		if !includeBuiltin && cmd.Builtin {
			continue
		}
		out = append(out, ConsoleCommandInfo{
			Name:    cmd.Name,
			Help:    cmd.Help,
			Builtin: cmd.Builtin,
		})
	}
	return ConsoleCommandsListResult{
		Total:    len(out),
		Commands: out,
	}, nil
}

type consoleExecArgs struct {
	Command string `json:"command"`
}

func (b *Bridge) handleConsoleExec(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[consoleExecArgs](raw)
	if err != nil {
		return nil, err
	}
	cmd := strings.TrimSpace(args.Command)
	if cmd == "" {
		return nil, errors.New("mcp: command is required")
	}
	c, err := b.requireConsole()
	if err != nil {
		return nil, err
	}
	out, execErr := c.Exec(cmd)
	if execErr != nil {
		return ConsoleExecResult{
			Command: cmd,
			OK:      false,
			Output:  out,
			Lines:   splitOutputLines(out),
			Error:   execErr.Error(),
		}, nil
	}
	return ConsoleExecResult{
		Command: cmd,
		OK:      true,
		Output:  out,
		Lines:   splitOutputLines(out),
	}, nil
}

func splitOutputLines(out string) []string {
	if out == "" {
		return []string{}
	}
	normalized := strings.ReplaceAll(out, "\r\n", "\n")
	normalized = strings.TrimSuffix(normalized, "\n")
	if normalized == "" {
		return []string{}
	}
	return strings.Split(normalized, "\n")
}
