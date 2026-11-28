// Package mcpui defines the optional runtime MCP contract for game UI trees: serializable widget records, a provider the game implements over its own widgets, and a lookup by widget path or label. It imports neither gameui nor pkg/mcp, so a game links it only when it opts in. Registering MCP tools on top of it is not part of this package yet.
package mcpui

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound reports that no widget matches the requested path or label.
var ErrNotFound = errors.New("mcpui: widget not found")

// ErrDisabled reports that the matched widget is disabled and cannot be activated.
var ErrDisabled = errors.New("mcpui: widget disabled")

// ErrAmbiguous reports that a label matches more than one widget; address the widget by its path instead.
var ErrAmbiguous = errors.New("mcpui: label matches more than one widget")

// Widget is one widget in a UI snapshot. Path is unique within a snapshot; X, Y, Width and Height are the widget's rectangle in the game's virtual coordinates.
type Widget struct {
	Path, Kind, Label   string
	X, Y, Width, Height float32
	Enabled, Focused    bool
	Value               string
}

// Snapshot is the game's UI tree flattened in traversal order.
type Snapshot struct{ Widgets []Widget }

// Provider is implemented by the game over its own widgets. Activate must run the widget's own handlers, so the same code runs as for a tap or a key press.
type Provider interface {
	Snapshot() Snapshot
	Activate(path string, action Action) error
}

// Action is what to do to a widget, such as Kind "press", "toggle", "focus" or "set" with Value holding the new value.
type Action struct {
	Kind  string
	Value string
}

// Find returns the widget whose path equals pathOrLabel, or else the single widget whose label equals it case-insensitively. A path match wins over a label match; a label shared by several widgets returns ErrAmbiguous.
func Find(snapshot Snapshot, pathOrLabel string) (Widget, error) {
	if pathOrLabel == "" {
		return Widget{}, ErrNotFound
	}
	for _, w := range snapshot.Widgets {
		if w.Path == pathOrLabel {
			return w, nil
		}
	}
	var match Widget
	n := 0
	for _, w := range snapshot.Widgets {
		if strings.EqualFold(w.Label, pathOrLabel) {
			match = w
			n++
		}
	}
	switch n {
	case 0:
		return Widget{}, ErrNotFound
	case 1:
		return match, nil
	default:
		return Widget{}, fmt.Errorf("%w: %q matches %d widgets", ErrAmbiguous, pathOrLabel, n)
	}
}

// Activate resolves pathOrLabel against the provider's current snapshot and passes the matched widget's path and the action to the provider. It returns ErrNotFound for a nil provider or no match and ErrDisabled for a disabled widget.
func Activate(p Provider, pathOrLabel string, action Action) error {
	if p == nil {
		return ErrNotFound
	}
	w, err := Find(p.Snapshot(), pathOrLabel)
	if err != nil {
		return err
	}
	if !w.Enabled {
		return fmt.Errorf("%w: %s", ErrDisabled, w.Path)
	}
	return p.Activate(w.Path, action)
}
