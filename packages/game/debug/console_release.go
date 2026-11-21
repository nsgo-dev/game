//go:build release

package debug

import "github.com/nsgo-dev/nsgo/core"

var releaseConsole Console

// Console is a zero-cost stub in release builds.
type Console struct{}

// NewConsole returns a no-op Console in release builds.
func NewConsole() *Console { return &releaseConsole }

// RegisterCommand is a no-op in release builds.
func (*Console) RegisterCommand(string, string, func([]string) (string, error)) {}

// RegisterFloat is a no-op in release builds.
func (*Console) RegisterFloat(string, float64, float64, *float64) {}

// RegisterInt is a no-op in release builds.
func (*Console) RegisterInt(string, int, int, *int) {}

// RegisterBool is a no-op in release builds.
func (*Console) RegisterBool(string, *bool) {}

// Exec is a no-op in release builds.
func (*Console) Exec(string) (string, error) { return "", nil }

// History always returns nil in release builds.
func (*Console) History() []string { return nil }

// HistoryPrev always returns "" in release builds.
func (*Console) HistoryPrev() string { return "" }

// HistoryNext always returns "" in release builds.
func (*Console) HistoryNext() string { return "" }

// Entries always returns nil in release builds.
func (*Console) Entries() []ConsoleEntry { return nil }

// Complete always returns nil in release builds.
func (*Console) Complete(string) []string { return nil }

// SaveTunables is a no-op in release builds.
func (*Console) SaveTunables(core.Storage, string) error { return nil }

// LoadTunables is a no-op in release builds.
func (*Console) LoadTunables(core.Storage, string) error { return nil }

// ConsoleView returns an empty core.View in release builds.
func ConsoleView(*core.Scope, *Console) core.View { return core.View{} }
