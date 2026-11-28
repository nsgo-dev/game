// Package mcpui defines the optional runtime MCP contract for game UI trees.
// It depends only on serializable records and callbacks, so pkg/mcp stays free
// of gameui and can attach this package only when a game opts in.
package mcpui

import (
	"errors"
	"strings"
)

var ErrNotFound = errors.New("mcpui: widget not found")

type Widget struct {
	Path, Kind, Label   string
	X, Y, Width, Height float32
	Enabled, Focused    bool
	Value               string
}
type Snapshot struct{ Widgets []Widget }
type Provider interface {
	Snapshot() Snapshot
	Activate(path string, action Action) error
}
type Action struct {
	Kind  string
	Value string
}

func Find(snapshot Snapshot, pathOrLabel string) (Widget, error) {
	for _, w := range snapshot.Widgets {
		if w.Path == pathOrLabel || (strings.EqualFold(w.Label, pathOrLabel) && pathOrLabel != "") {
			return w, nil
		}
	}
	return Widget{}, ErrNotFound
}
func Activate(p Provider, pathOrLabel string, action Action) error {
	if p == nil {
		return ErrNotFound
	}
	w, err := Find(p.Snapshot(), pathOrLabel)
	if err != nil {
		return err
	}
	if !w.Enabled {
		return errors.New("mcpui: widget disabled")
	}
	return p.Activate(w.Path, action)
}
