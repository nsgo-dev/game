//go:build release

package devstate_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/tooling/devstate"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

type sampleState struct {
	HP int
}

type noopSnapshotter struct {
	calls int
}

func (n *noopSnapshotter) Snapshot() (any, error) {
	n.calls++
	return sampleState{HP: 10}, nil
}

func (n *noopSnapshotter) Restore(any) error {
	n.calls++
	return nil
}

func TestReleaseStubsAreNoOps(t *testing.T) {
	if h := devstate.ComputeSchemaHash(sampleState{HP: 10}); h != "" {
		t.Fatalf("release ComputeSchemaHash = %q, want empty string", h)
	}

	path := filepath.Join(t.TempDir(), "should_not_exist.json")
	store := devstate.NewStore(path)
	store.SetLogger(func(string, ...any) {
		t.Error("release logger should not be invoked")
	})

	if err := store.Save("level1", 100, time.Second, sampleState{HP: 42}); err != nil {
		t.Fatalf("release Save error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("release Save should not create file on disk, stat err=%v", err)
	}

	if snap, ok, err := store.LoadSnapshot(); err != nil || ok || snap.SceneID != "" {
		t.Fatalf("release LoadSnapshot = (%+v, %v, %v), want zero/false/nil", snap, ok, err)
	}

	got := sampleState{HP: 99}
	res, err := store.Restore(&got)
	if err != nil {
		t.Fatalf("release Restore error: %v", err)
	}
	if res.Restored || got.HP != 99 {
		t.Fatalf("release Restore should not restore state: res=%+v got=%+v", res, got)
	}
	if err := store.Clear(); err != nil {
		t.Fatalf("release Clear error: %v", err)
	}

	snap := &noopSnapshotter{}
	var hook *devstate.Hook
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		sStore := devstate.NewStorageStore(cx.Storage(), "k")
		hook = devstate.Attach(cx, sStore, snap, devstate.HookOptions{
			SceneID:      func() string { return "lvl" },
			SaveInterval: 10 * time.Millisecond,
		})
		return core.View{}
	})
	app.Settle()

	if err := hook.SaveNow(); err != nil {
		t.Fatalf("release SaveNow error: %v", err)
	}
	if res, err := hook.RestoreInto(&got); err != nil || res.Restored {
		t.Fatalf("release RestoreInto = (%+v, %v), want Restored=false, nil", res, err)
	}
	hook.Stop()

	if snap.calls != 0 {
		t.Fatalf("release snapshotter called %d times, want 0", snap.calls)
	}
}
