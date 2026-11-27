//go:build !release

package hotreload_test

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tooling/hotreload"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type sceneResolver struct {
	sceneformat.AtlasMap
	*resource.Library
}

func TestWatchResourcesReloadsAMaterialUsedByTwoScenes(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "materials"), 0o755); err != nil {
		t.Fatal(err)
	}
	matPath := filepath.Join(dir, "materials", "glow.material.yaml")
	if err := os.WriteFile(matPath, []byte("type: material\nversion: 1\nopacity: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An unloaded resource file and a broken edit must not disturb the loaded material.
	if err := os.WriteFile(filepath.Join(dir, "unused.yaml"), []byte("type: curve\nversion: 1\nkeys: [{time: 0, value: 0}]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lib := resource.NewLibrary(os.DirFS(dir))
	atlas := &sprite.Atlas{
		Image:  core.ImageSource{URI: "asset:props.png"},
		Frames: map[string]sprite.Frame{"lamp": {Name: "lamp", W: 8, H: 8, Width: 8, Height: 8}},
	}
	resolver := sceneResolver{AtlasMap: sceneformat.AtlasMap{"props": atlas}, Library: lib}
	build := func(id string) *sceneformat.BuiltScene {
		t.Helper()
		built, err := sceneformat.Build(&sceneformat.SceneDoc{
			Version: sceneformat.CurrentVersion,
			Layers: []sceneformat.LayerDoc{{Name: "props", Objects: []sceneformat.ObjectDoc{{
				ID:       id,
				Sprite:   &sceneformat.SpriteRef{Atlas: "props", Frame: "lamp"},
				Material: "materials/glow.material.yaml",
			}}}},
		}, resolver)
		if err != nil {
			t.Fatalf("Build %s: %v", id, err)
		}
		return built
	}
	first, second := build("lamp_a"), build("lamp_b")

	type reload struct {
		path string
		err  error
	}
	var (
		reloads []reload
		count   atomic.Int32
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w := hotreload.NewWatcher(cx, dir,
			hotreload.WithPollInterval(10*time.Millisecond),
			hotreload.WithDebounce(10*time.Millisecond),
		)
		w.WatchResources(lib, func(path string, err error) {
			reloads = append(reloads, reload{path, err})
			first.SyncMaterials()
			second.SyncMaterials()
			count.Add(1)
		})
		return core.View{}
	})
	if err := os.WriteFile(filepath.Join(dir, "unused.yaml"), []byte("type: curve\nversion: 1\nkeys: [{time: 0, value: 1}]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(matPath, []byte("type: material\nversion: 1\nopacity: 0.5\nblend: lighter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(time.Second, func() bool { return count.Load() >= 1 })

	var a, b sprite.Sprite
	var got []reload
	app.Post(func() {
		a, b = *first.Object("lamp_a").Sprite, *second.Object("lamp_b").Sprite
		got = append([]reload(nil), reloads...)
	})
	app.Settle()
	if len(got) != 1 || got[0].path != "materials/glow.material.yaml" || got[0].err != nil {
		t.Fatalf("reloads = %+v, want one clean reload of the material", got)
	}
	if a.Alpha != 0.5 || b.Alpha != 0.5 || a.Blend != core.CompositeLighter || b.Blend != core.CompositeLighter {
		t.Fatalf("sprites after the edit: a alpha %v blend %v, b alpha %v blend %v", a.Alpha, a.Blend, b.Alpha, b.Blend)
	}

	if err := os.WriteFile(matPath, []byte("type: material\nversion: 1\nopacity: 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(time.Second, func() bool { return count.Load() >= 2 })
	app.Post(func() {
		a = *first.Object("lamp_a").Sprite
		got = append([]reload(nil), reloads...)
	})
	app.Settle()
	if got[1].err == nil {
		t.Fatal("a broken edit reported no error")
	}
	if a.Alpha != 0.5 {
		t.Fatalf("a broken edit changed the sprite's alpha to %v", a.Alpha)
	}
}
