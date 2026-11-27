//go:build release

package gamedebug

import (
	"context"
	"io"
	"time"
)

var releaseProfiler Profiler

// ScopeToken is a zero-cost stub in release builds.
type ScopeToken struct{}

// End is a no-op in release builds.
func (ScopeToken) End() {}

// Profiler is a zero-cost stub in release builds.
type Profiler struct{}

// NewProfiler returns a no-op Profiler in release builds.
func NewProfiler(int) *Profiler { return &releaseProfiler }

// SetNow is a no-op in release builds.
func (*Profiler) SetNow(func() time.Time) {}

// SetTraceContext is a no-op in release builds.
func (*Profiler) SetTraceContext(context.Context) {}

// BeginFrame is a no-op in release builds.
func (*Profiler) BeginFrame() {}

// EndFrame is a no-op in release builds.
func (*Profiler) EndFrame() {}

// Begin is a no-op in release builds.
func (*Profiler) Begin(string) ScopeToken { return ScopeToken{} }

// Scope is a no-op in release builds.
func (*Profiler) Scope(string) ScopeToken { return ScopeToken{} }

// End is a no-op in release builds.
func (*Profiler) End(ScopeToken) {}

// StartRecording is a no-op in release builds.
func (*Profiler) StartRecording(int) {}

// StopRecording is a no-op in release builds.
func (*Profiler) StopRecording() {}

// Recording always returns false in release builds.
func (*Profiler) Recording() bool { return false }

// RecordedFrames always returns 0 in release builds.
func (*Profiler) RecordedFrames() int { return 0 }

// Reset is a no-op in release builds.
func (*Profiler) Reset() {}

// TopScopes always returns nil in release builds.
func (*Profiler) TopScopes(int) []ScopeSummary { return nil }

// WriteChromeTrace is a no-op in release builds.
func (*Profiler) WriteChromeTrace(io.Writer) error { return nil }
