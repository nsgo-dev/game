//go:build !release

package debug

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	defaultTunablesKey = "debug.console.tunables"
	maxConsoleEntries  = 200
)

type consoleCommand struct {
	name string
	help string
	fn   func(args []string) (string, error)
}

type tunableKind uint8

const (
	tunableFloat tunableKind = iota
	tunableInt
	tunableBool
)

type consoleTunable struct {
	name     string
	kind     tunableKind
	minFloat float64
	maxFloat float64
	floatPtr *float64
	minInt   int
	maxInt   int
	intPtr   *int
	boolPtr  *bool
}

func (t *consoleTunable) formatValue() string {
	switch t.kind {
	case tunableFloat:
		return strconv.FormatFloat(*t.floatPtr, 'g', -1, 64)
	case tunableInt:
		return strconv.Itoa(*t.intPtr)
	case tunableBool:
		return strconv.FormatBool(*t.boolPtr)
	default:
		return ""
	}
}

func (t *consoleTunable) formatRange() string {
	switch t.kind {
	case tunableFloat:
		return fmt.Sprintf("[%g, %g]", t.minFloat, t.maxFloat)
	case tunableInt:
		return fmt.Sprintf("[%d, %d]", t.minInt, t.maxInt)
	case tunableBool:
		return "[true|false]"
	default:
		return ""
	}
}

func (t *consoleTunable) setString(raw string) error {
	switch t.kind {
	case tunableFloat:
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("invalid float %q for %q", raw, t.name)
		}
		if v < t.minFloat || v > t.maxFloat {
			return fmt.Errorf("%q value %g out of range [%g, %g]", t.name, v, t.minFloat, t.maxFloat)
		}
		*t.floatPtr = v
		return nil
	case tunableInt:
		v, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("invalid int %q for %q", raw, t.name)
		}
		if v < t.minInt || v > t.maxInt {
			return fmt.Errorf("%q value %d out of range [%d, %d]", t.name, v, t.minInt, t.maxInt)
		}
		*t.intPtr = v
		return nil
	case tunableBool:
		v, err := parseConsoleBool(raw)
		if err != nil {
			return fmt.Errorf("invalid bool %q for %q", raw, t.name)
		}
		*t.boolPtr = v
		return nil
	default:
		return fmt.Errorf("unsupported tunable %q", t.name)
	}
}

func parseConsoleBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "t", "true", "on", "yes":
		return true, nil
	case "0", "f", "false", "off", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool %q", s)
	}
}

// Console is an interactive development console supporting custom commands, range-checked typed tunables, history navigation, tab completion, and Storage persistence.
type Console struct {
	mu       sync.Mutex
	commands map[string]consoleCommand
	tunables map[string]*consoleTunable
	history  []string
	histIdx  int
	entries  []ConsoleEntry

	rev      uint64
	watchers map[int]func(uint64)
	nextWID  int
}

var builtinHelp = []struct {
	name string
	help string
}{
	{name: "clear", help: "Clear console output"},
	{name: "get", help: "get <var> - print a tunable's current value"},
	{name: "help", help: "help [command] - list commands or show command help"},
	{name: "list", help: "List registered tunables, values, and ranges"},
	{name: "set", help: "set <var> <value> - update a tunable within its range"},
}

// NewConsole returns an empty development Console with built-in commands enabled.
func NewConsole() *Console {
	return &Console{
		commands: make(map[string]consoleCommand),
		tunables: make(map[string]*consoleTunable),
		watchers: make(map[int]func(uint64)),
	}
}

// RegisterCommand registers a named console command and its help text.
func (c *Console) RegisterCommand(name, help string, fn func(args []string) (string, error)) {
	if c == nil || fn == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	c.mu.Lock()
	c.commands[name] = consoleCommand{name: name, help: help, fn: fn}
	c.mu.Unlock()
}

// RegisterFloat registers a range-checked float64 tunable bound to ptr.
func (c *Console) RegisterFloat(name string, minVal, maxVal float64, ptr *float64) {
	if c == nil || ptr == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if minVal > maxVal {
		minVal, maxVal = maxVal, minVal
	}
	c.mu.Lock()
	c.tunables[name] = &consoleTunable{
		name:     name,
		kind:     tunableFloat,
		minFloat: minVal,
		maxFloat: maxVal,
		floatPtr: ptr,
	}
	c.mu.Unlock()
}

// RegisterInt registers a range-checked int tunable bound to ptr.
func (c *Console) RegisterInt(name string, minVal, maxVal int, ptr *int) {
	if c == nil || ptr == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if minVal > maxVal {
		minVal, maxVal = maxVal, minVal
	}
	c.mu.Lock()
	c.tunables[name] = &consoleTunable{
		name:   name,
		kind:   tunableInt,
		minInt: minVal,
		maxInt: maxVal,
		intPtr: ptr,
	}
	c.mu.Unlock()
}

// RegisterBool registers a bool tunable bound to ptr.
func (c *Console) RegisterBool(name string, ptr *bool) {
	if c == nil || ptr == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	c.mu.Lock()
	c.tunables[name] = &consoleTunable{
		name:    name,
		kind:    tunableBool,
		boolPtr: ptr,
	}
	c.mu.Unlock()
}

// Exec parses and executes one command line, updating history and output entries.
func (c *Console) Exec(line string) (string, error) {
	if c == nil {
		return "", nil
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", nil
	}

	c.mu.Lock()
	c.history = append(c.history, trimmed)
	c.histIdx = len(c.history)
	c.mu.Unlock()

	tokens, err := splitConsoleArgs(trimmed)
	if err != nil {
		c.appendEntry(ConsoleEntry{Input: trimmed, Output: err.Error(), IsError: true})
		return "", err
	}
	if len(tokens) == 0 {
		return "", nil
	}

	cmdName := tokens[0]
	args := tokens[1:]

	var out string
	switch cmdName {
	case "clear":
		c.mu.Lock()
		c.entries = c.entries[:0]
		watchers, rev := c.bumpLocked()
		c.mu.Unlock()
		notifyWatchers(watchers, rev)
		return "", nil
	case "help":
		out, err = c.execHelp(args)
	case "list":
		out, err = c.execList(args)
	case "get":
		out, err = c.execGet(args)
	case "set":
		out, err = c.execSet(args)
	default:
		c.mu.Lock()
		cmd, ok := c.commands[cmdName]
		c.mu.Unlock()
		if !ok {
			err = fmt.Errorf("unknown command %q (type \"help\" for available commands)", cmdName)
		} else {
			out, err = cmd.fn(args)
		}
	}

	if err != nil {
		c.appendEntry(ConsoleEntry{Input: trimmed, Output: err.Error(), IsError: true})
		return "", err
	}
	c.appendEntry(ConsoleEntry{Input: trimmed, Output: out})
	return out, nil
}

func (c *Console) execHelp(args []string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(args) > 0 {
		target := args[0]
		for _, b := range builtinHelp {
			if b.name == target {
				return b.name + ": " + b.help, nil
			}
		}
		if cmd, ok := c.commands[target]; ok {
			if cmd.help == "" {
				return cmd.name, nil
			}
			return cmd.name + ": " + cmd.help, nil
		}
		return "", fmt.Errorf("unknown command %q", target)
	}

	lines := make([]string, 0, len(builtinHelp)+len(c.commands))
	for _, b := range builtinHelp {
		lines = append(lines, fmt.Sprintf("%-12s %s", b.name, b.help))
	}
	names := make([]string, 0, len(c.commands))
	for name := range c.commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cmd := c.commands[name]
		if cmd.help != "" {
			lines = append(lines, fmt.Sprintf("%-12s %s", cmd.name, cmd.help))
		} else {
			lines = append(lines, cmd.name)
		}
	}
	return strings.Join(lines, "\n"), nil
}

func (c *Console) execList(args []string) (string, error) {
	if len(args) != 0 {
		return "", errors.New("usage: list")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.tunables) == 0 {
		return "(no tunables registered)", nil
	}
	names := make([]string, 0, len(c.tunables))
	for name := range c.tunables {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, name := range names {
		t := c.tunables[name]
		lines = append(lines, fmt.Sprintf("%s = %s %s", t.name, t.formatValue(), t.formatRange()))
	}
	return strings.Join(lines, "\n"), nil
}

func (c *Console) execGet(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("usage: get <var>")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	t, ok := c.tunables[args[0]]
	if !ok {
		return "", fmt.Errorf("unknown tunable %q", args[0])
	}
	return t.formatValue(), nil
}

func (c *Console) execSet(args []string) (string, error) {
	if len(args) != 2 {
		return "", errors.New("usage: set <var> <value>")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	t, ok := c.tunables[args[0]]
	if !ok {
		return "", fmt.Errorf("unknown tunable %q", args[0])
	}
	if err := t.setString(args[1]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s = %s", t.name, t.formatValue()), nil
}

// History returns a copy of executed command lines, oldest first.
func (c *Console) History() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.history) == 0 {
		return nil
	}
	return slices.Clone(c.history)
}

// HistoryPrev steps backward through command history and returns the selected command line.
func (c *Console) HistoryPrev() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.history) == 0 {
		return ""
	}
	if c.histIdx > 0 {
		c.histIdx--
	}
	return c.history[c.histIdx]
}

// HistoryNext steps forward through command history, returning "" once past the most recent entry.
func (c *Console) HistoryNext() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.history) == 0 {
		return ""
	}
	if c.histIdx < len(c.history) {
		c.histIdx++
	}
	if c.histIdx >= len(c.history) {
		c.histIdx = len(c.history)
		return ""
	}
	return c.history[c.histIdx]
}

// Entries returns a copy of the current console output log.
func (c *Console) Entries() []ConsoleEntry {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) == 0 {
		return nil
	}
	return slices.Clone(c.entries)
}

// Complete returns sorted completion candidates for prefix, completing command names and tunable names for get/set/help.
func (c *Console) Complete(prefix string) []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	trimmedLeft := strings.TrimLeft(prefix, " \t")
	if spaceIdx := strings.IndexAny(trimmedLeft, " \t"); spaceIdx >= 0 {
		cmd := trimmedLeft[:spaceIdx]
		rest := strings.TrimLeft(trimmedLeft[spaceIdx+1:], " \t")
		// Only complete the first argument token.
		if strings.ContainsAny(rest, " \t") {
			return nil
		}
		var out []string
		switch cmd {
		case "get", "set":
			for name := range c.tunables {
				if strings.HasPrefix(name, rest) {
					out = append(out, cmd+" "+name)
				}
			}
		case "help":
			seen := make(map[string]struct{}, len(builtinHelp)+len(c.commands))
			for _, b := range builtinHelp {
				if strings.HasPrefix(b.name, rest) {
					seen[b.name] = struct{}{}
					out = append(out, "help "+b.name)
				}
			}
			for name := range c.commands {
				if _, dup := seen[name]; !dup && strings.HasPrefix(name, rest) {
					out = append(out, "help "+name)
				}
			}
		}
		sort.Strings(out)
		return out
	}

	seen := make(map[string]struct{}, len(builtinHelp)+len(c.commands))
	var out []string
	for _, b := range builtinHelp {
		if strings.HasPrefix(b.name, trimmedLeft) {
			seen[b.name] = struct{}{}
			out = append(out, b.name)
		}
	}
	for name := range c.commands {
		if _, dup := seen[name]; !dup && strings.HasPrefix(name, trimmedLeft) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// SaveTunables persists all registered tunables into st under key as JSON.
func (c *Console) SaveTunables(st core.Storage, key string) error {
	if c == nil {
		return nil
	}
	if key == "" {
		key = defaultTunablesKey
	}

	c.mu.Lock()
	payload := make(map[string]any, len(c.tunables))
	for name, t := range c.tunables {
		switch t.kind {
		case tunableFloat:
			payload[name] = *t.floatPtr
		case tunableInt:
			payload[name] = *t.intPtr
		case tunableBool:
			payload[name] = *t.boolPtr
		}
	}
	c.mu.Unlock()

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("debug: marshal tunables: %w", err)
	}
	return st.SetItem(key, string(data))
}

// LoadTunables restores registered tunables from st under key. Missing entries are a no-op; unknown keys are ignored; out-of-range values return an error without mutating the tunable.
//
// Its errors are *loaderr.Error values that name key as the file, with the line of the tunable at fault and how to fix it.
func (c *Console) LoadTunables(st core.Storage, key string) error {
	if c == nil {
		return nil
	}
	if key == "" {
		key = defaultTunablesKey
	}
	raw, ok := st.GetItem(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}

	const fixStored = "save the tunables again with SaveTunables, or remove the key so they keep their defaults"
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return loaderr.Locate(err, key, []byte(raw), fixStored)
	}
	// bad reports the stored value of the tunable name at its line under key.
	bad := func(name, fix, format string, args ...any) error {
		return loaderr.Locate(loaderr.Errorf(loaderr.Path(name), fix, format, args...), key, []byte(raw), fixStored)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	type pendingUpdate struct {
		t *consoleTunable
		f float64
		i int
		b bool
	}
	names := make([]string, 0, len(parsed))
	for name := range parsed {
		names = append(names, name)
	}
	sort.Strings(names)

	updates := make([]pendingUpdate, 0, len(names))
	for _, name := range names {
		t, exists := c.tunables[name]
		if !exists {
			continue
		}
		rawVal := parsed[name]
		switch t.kind {
		case tunableFloat:
			var v float64
			if err := json.Unmarshal(rawVal, &v); err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return bad(name, "store a finite number", "invalid stored float for %q", name)
			}
			if v < t.minFloat || v > t.maxFloat {
				return bad(name, fmt.Sprintf("store a value from %g to %g", t.minFloat, t.maxFloat), "stored %q value %g out of range [%g, %g]", name, v, t.minFloat, t.maxFloat)
			}
			updates = append(updates, pendingUpdate{t: t, f: v})
		case tunableInt:
			var v int
			if err := json.Unmarshal(rawVal, &v); err != nil {
				return bad(name, "store a whole number", "invalid stored int for %q", name)
			}
			if v < t.minInt || v > t.maxInt {
				return bad(name, fmt.Sprintf("store a value from %d to %d", t.minInt, t.maxInt), "stored %q value %d out of range [%d, %d]", name, v, t.minInt, t.maxInt)
			}
			updates = append(updates, pendingUpdate{t: t, i: v})
		case tunableBool:
			var v bool
			if err := json.Unmarshal(rawVal, &v); err != nil {
				return bad(name, "store true or false", "invalid stored bool for %q", name)
			}
			updates = append(updates, pendingUpdate{t: t, b: v})
		}
	}

	for _, u := range updates {
		switch u.t.kind {
		case tunableFloat:
			*u.t.floatPtr = u.f
		case tunableInt:
			*u.t.intPtr = u.i
		case tunableBool:
			*u.t.boolPtr = u.b
		}
	}
	return nil
}

func (c *Console) appendEntry(e ConsoleEntry) {
	c.mu.Lock()
	c.entries = append(c.entries, e)
	if len(c.entries) > maxConsoleEntries {
		drop := len(c.entries) - maxConsoleEntries
		copy(c.entries, c.entries[drop:])
		c.entries = c.entries[:maxConsoleEntries]
	}
	watchers, rev := c.bumpLocked()
	c.mu.Unlock()
	notifyWatchers(watchers, rev)
}

func (c *Console) bumpLocked() ([]func(uint64), uint64) {
	c.rev++
	rev := c.rev
	if len(c.watchers) == 0 {
		return nil, rev
	}
	out := make([]func(uint64), 0, len(c.watchers))
	for _, fn := range c.watchers {
		out = append(out, fn)
	}
	return out, rev
}

func notifyWatchers(watchers []func(uint64), rev uint64) {
	for _, fn := range watchers {
		fn(rev)
	}
}

func (c *Console) watch(fn func(uint64)) (uint64, func()) {
	c.mu.Lock()
	id := c.nextWID
	c.nextWID++
	c.watchers[id] = fn
	rev := c.rev
	c.mu.Unlock()
	return rev, func() {
		c.mu.Lock()
		delete(c.watchers, id)
		c.mu.Unlock()
	}
}

// splitConsoleArgs splits a command line into whitespace-separated tokens, supporting single quotes, double quotes, and backslash escapes.
func splitConsoleArgs(line string) ([]string, error) {
	var args []string
	var buf strings.Builder
	inToken := false
	var quote byte

	for i := 0; i < len(line); i++ {
		ch := line[i]
		if quote != 0 {
			if ch == '\\' && quote == '"' && i+1 < len(line) {
				i++
				switch line[i] {
				case 'n':
					buf.WriteByte('\n')
				case 't':
					buf.WriteByte('\t')
				default:
					buf.WriteByte(line[i])
				}
				continue
			}
			if ch == quote {
				quote = 0
				continue
			}
			buf.WriteByte(ch)
			continue
		}

		switch ch {
		case ' ', '\t', '\r', '\n':
			if inToken {
				args = append(args, buf.String())
				buf.Reset()
				inToken = false
			}
		case '"', '\'':
			quote = ch
			inToken = true
		case '\\':
			inToken = true
			if i+1 < len(line) {
				i++
				buf.WriteByte(line[i])
			} else {
				buf.WriteByte('\\')
			}
		default:
			inToken = true
			buf.WriteByte(ch)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if inToken {
		args = append(args, buf.String())
	}
	return args, nil
}

func longestCommonPrefix(items []string) string {
	if len(items) == 0 {
		return ""
	}
	prefix := items[0]
	for _, s := range items[1:] {
		for !strings.HasPrefix(s, prefix) {
			if len(prefix) == 0 {
				return ""
			}
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

var (
	consolePanelStyle = core.Style{
		MinWidth:     core.Pt(300),
		MaxWidth:     core.Pt(460),
		Background:   core.RGBA(0x0B0F17, 0.94),
		CornerRadius: 6,
		Padding:      core.All(8),
		BorderWidth:  1,
		BorderColor:  core.RGBA(0x334155, 0.9),
		Gap:          6,
	}
	consoleOutputStyle = core.Style{
		MaxHeight: core.Pt(140),
		Gap:       2,
	}
	consoleCmdStyle = core.Style{
		FontFamily: core.FontMonospace,
		FontSize:   11,
		Color:      core.RGB(0x94A3B8),
	}
	consoleOutStyle = core.Style{
		FontFamily: core.FontMonospace,
		FontSize:   11,
		Color:      core.RGB(0xE2E8F0),
	}
	consoleErrStyle = core.Style{
		FontFamily: core.FontMonospace,
		FontSize:   11,
		Color:      core.RGB(0xF87171),
	}
	consoleInputRowStyle = core.Style{
		Direction:   core.Row,
		AlignItems:  core.AlignCenter,
		Gap:         6,
		Padding:     core.Edges{Top: 4},
		Borders:     core.Edges{Top: 1},
		BorderColor: core.RGBA(0x334155, 0.7),
	}
	consoleInputStyle = core.Style{
		FlexGrow:     1,
		FontFamily:   core.FontMonospace,
		FontSize:     11,
		Color:        core.RGB(0xF8FAFC),
		Background:   core.RGBA(0x1E293B, 0.9),
		CornerRadius: 4,
		Padding:      core.Edges{Top: 4, Right: 6, Bottom: 4, Left: 6},
	}
	consoleRunBtnStyle = core.Style{
		Background:   core.RGB(0x0284C7),
		CornerRadius: 4,
		Padding:      core.Edges{Top: 4, Right: 8, Bottom: 4, Left: 8},
	}
	consoleRunTextStyle = core.Style{
		FontFamily: core.FontMonospace,
		FontSize:   11,
		FontWeight: core.FontWeightSemibold,
		Color:      core.RGB(0xF8FAFC),
	}
	consoleSuggestStyle = core.Style{
		FontFamily: core.FontMonospace,
		FontSize:   10,
		Color:      core.RGB(0x38BDF8),
	}
)

// ConsoleView returns a native NSGo view bound to c, composable inside Overlay or any game view tree.
func ConsoleView(cx *core.Scope, c *Console) core.View {
	if c == nil || cx == nil {
		return core.View{}
	}

	inputSig := core.NewSignal(cx, "")
	suggestSig := core.NewSignal(cx, "")
	revSig := core.NewSignal(cx, uint64(0))

	initialRev, unwatch := c.watch(func(rev uint64) {
		revSig.Set(rev)
	})
	revSig.Set(initialRev)
	cx.Cleanup(unwatch)

	runLine := func(raw string) {
		suggestSig.Set("")
		_, _ = c.Exec(raw)
		inputSig.Set("")
	}

	return core.View{
		TestID:        "debug-console",
		PointerEvents: core.PointerEventsAuto,
		Style:         consolePanelStyle,
		Children: []core.Node{
			core.Text{
				Content: "Console",
				Style:   sectionHeaderStyle,
			},
			core.Scroll{
				TestID: "debug-console-output",
				Style:  consoleOutputStyle,
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = revSig.Get()
						entries := c.Entries()
						if len(entries) == 0 {
							return core.Text{
								Content: "Type \"help\" for commands.",
								Style:   mutedTextStyle,
							}
						}
						nodes := make([]core.Node, 0, len(entries)*2)
						for _, e := range entries {
							nodes = append(nodes, core.Text{
								Content: "> " + e.Input,
								Style:   consoleCmdStyle,
							})
							if e.Output != "" {
								st := consoleOutStyle
								if e.IsError {
									st = consoleErrStyle
								}
								nodes = append(nodes, core.Text{
									Content: e.Output,
									Style:   st,
								})
							}
						}
						return core.Group(nodes)
					}),
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				hint := suggestSig.Get()
				if hint == "" {
					return nil
				}
				return core.Text{
					TestID:  "debug-console-suggestions",
					Content: hint,
					Style:   consoleSuggestStyle,
				}
			}),
			core.View{
				Style: consoleInputRowStyle,
				Children: []core.Node{
					core.Text{Content: ">", Style: sectionHeaderStyle},
					core.Input{
						TestID:         "debug-console-input",
						Value:          inputSig.Get,
						Placeholder:    "help, list, get/set ...",
						NoAutoCorrect:  true,
						SubmitBehavior: core.SubmitBehaviorSubmit,
						Style:          consoleInputStyle,
						OnChange: func(text string) {
							inputSig.Set(text)
							suggestSig.Set("")
						},
						OnSubmit: func(text string) {
							runLine(text)
						},
						OnKeyPress: func(key string) {
							switch key {
							case "ArrowUp", "Up":
								if prev := c.HistoryPrev(); prev != "" {
									inputSig.Set(prev)
									suggestSig.Set("")
								}
							case "ArrowDown", "Down":
								inputSig.Set(c.HistoryNext())
								suggestSig.Set("")
							case "Tab":
								matches := c.Complete(inputSig.Get())
								if len(matches) == 1 {
									inputSig.Set(matches[0])
									suggestSig.Set("")
								} else if len(matches) > 1 {
									if lcp := longestCommonPrefix(matches); len(lcp) > len(inputSig.Get()) {
										inputSig.Set(lcp)
									}
									suggestSig.Set(strings.Join(matches, "  "))
								}
							}
						},
					},
					core.View{
						TestID: "debug-console-run",
						Style:  consoleRunBtnStyle,
						OnPress: func(core.PressEvent) {
							runLine(inputSig.Get())
						},
						Children: []core.Node{
							core.Text{Content: "Run", Style: consoleRunTextStyle},
						},
					},
				},
			},
		},
	}
}
