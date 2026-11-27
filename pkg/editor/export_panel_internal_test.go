package editor

import (
	"context"
	"slices"
	"testing"
)

func TestDefaultExportCommandSetsReleaseBuildTag(t *testing.T) {
	cmd := newExportCommand(context.Background(), []string{"-C", "/demo", "tool", "nsgo", "build", "-target", "web"}, []string{"PATH=/usr/bin"})
	if !slices.Contains(cmd.Env, "GOFLAGS=-tags=release") {
		t.Fatalf("cmd.Env = %q, want GOFLAGS=-tags=release", cmd.Env)
	}

	merged := newExportCommand(context.Background(), []string{"tool", "nsgo", "build"}, []string{"PATH=/usr/bin", "GOFLAGS=-mod=mod -tags=custom"})
	if !slices.Contains(merged.Env, "GOFLAGS=-mod=mod -tags=custom,release") {
		t.Fatalf("merged cmd.Env = %q, want GOFLAGS=-mod=mod -tags=custom,release", merged.Env)
	}
}
