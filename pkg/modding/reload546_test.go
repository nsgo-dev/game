package modding_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/modding"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestReloadWithBadManifestKeepsRunningVersion(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "counter")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "manifestVersion: 1\nid: counter\nname: Counter\nversion: 1.0.0\nentry: mod.js\ncapabilities: [readState, writeState]\n"
	script := `import { onStep, getState, setState } from "game";
onStep(() => {
	setState("count", (getState("count") || 0) + 1);
});`
	if err := os.WriteFile(filepath.Join(modDir, "mod.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "mod.js"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	var mgr *modding.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{})
		if _, err := mgr.Discover(dir); err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if err := mgr.LoadAll(); err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		return core.View{}
	})

	for _, bad := range []string{
		"manifestVersion: 1\nid: [counter\n",
		"manifestVersion: 1\nid: renamed\nname: Counter\nversion: 1.0.0\nentry: mod.js\n",
		"manifestVersion: 1\nid: counter\nname: Counter\nversion: 1.0.0\nentry: mod.js\ndependencies:\n  missing: ^1.0.0\n",
	} {
		if err := os.WriteFile(filepath.Join(modDir, "mod.yaml"), []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		var reloadErr error
		app.Post(func() {
			reloadErr = mgr.Reload("counter")
			mgr.Step(time.Second / 60)
		})
		app.Settle()
		if reloadErr == nil {
			t.Fatalf("Reload accepted manifest %s", bad)
		}
		if info, _ := mgr.Info("counter"); info.State != modding.StateLoaded {
			t.Fatalf("manifest %s: mod state = %v after a refused reload, want loaded", bad, info.State)
		}
	}
	if c, _ := mgr.GetState("count"); c != int64(3) && c != float64(3) {
		t.Fatalf("count = %#v after three refused reloads and steps, want 3", c)
	}
}
