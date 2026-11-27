package modding_test

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/modding"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestParseManifestErrorsNameFileLineAndFix(t *testing.T) {
	base := `"manifestVersion": 1, "id": "more.enemies", "name": "More Enemies", "version": "1.0.0", "entry": "main.ts"`
	cases := []struct {
		name, json, field string
		sentinel          error
	}{
		{"id", `{"manifestVersion": 1, "id": "../x", "name": "n", "version": "1.0.0", "entry": "main.ts"}`, "id", modding.ErrInvalidManifest},
		{"name", `{"manifestVersion": 1, "id": "a", "name": " ", "version": "1.0.0", "entry": "main.ts"}`, "name", modding.ErrInvalidManifest},
		{"version", `{"manifestVersion": 1, "id": "a", "name": "n", "version": "one", "entry": "main.ts"}`, "version", modding.ErrInvalidManifest},
		{"entry", `{"manifestVersion": 1, "id": "a", "name": "n", "version": "1.0.0", "entry": "../escape.ts"}`, "entry", modding.ErrInvalidManifest},
		{"game version", `{` + base + `, "gameVersion": ">>1"}`, "gameVersion", modding.ErrInvalidManifest},
		{"dependency", `{` + base + `, "dependencies": {"core.mod": "^1.0.0", "z.mod": "nope"}}`, "dependencies.z.mod", modding.ErrInvalidManifest},
		{"self dependency", `{` + base + `, "dependencies": {"more.enemies": "^1.0.0"}}`, "dependencies.more.enemies", modding.ErrCyclicDependency},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := loaderrtest.Indent(tc.json)
			_, err := modding.ParseManifest(data)
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("err = %v, want one matching %v", err, tc.sentinel)
			}
			field := tc.field
			if tc.name == "dependency" || tc.name == "self dependency" {
				// Path quotes a key with dots.
				field = loaderr.Path("dependencies", field[len("dependencies."):])
			}
			loaderrtest.ExpectField(t, loaderr.WithFile(err, "more/mod.yaml"), "more/mod.yaml", data, field)
		})
	}
	t.Run("type", func(t *testing.T) {
		_, err := modding.ParseManifest([]byte("manifestVersion: 1\nid: a\nname: {a: b}\n"))
		if !errors.Is(err, modding.ErrInvalidManifest) {
			t.Fatalf("err = %v, want ErrInvalidManifest", err)
		}
		loaderrtest.Expect(t, loaderr.WithFile(err, "more/mod.yaml"), "more/mod.yaml", 3)
	})
	t.Run("capability", func(t *testing.T) {
		_, err := modding.ParseManifest([]byte("manifestVersion: 1\nid: a\nname: n\nversion: 1.0.0\nentry: main.ts\ncapabilities:\n  - readState\n  - teleport\n"))
		if !errors.Is(err, modding.ErrInvalidManifest) {
			t.Fatalf("err = %v, want ErrInvalidManifest", err)
		}
		e := loaderrtest.Expect(t, loaderr.WithFile(err, "more/mod.yaml"), "more/mod.yaml", 8)
		if e.Field != "capabilities" {
			t.Errorf("Field = %q, want capabilities", e.Field)
		}
	})
}

func TestDiscoverNamesTheManifestFile(t *testing.T) {
	manifest := loaderrtest.Indent(`{"manifestVersion": 1, "id": "more.enemies", "name": "More Enemies", "version": "1.0.0", "entry": "missing.ts"}`)
	fsys := fstest.MapFS{"more/mod.yaml": {Data: manifest}}
	var mgr *modding.Manager
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{})
		return core.View{}
	})
	_, err := mgr.DiscoverFS(fsys)
	loaderrtest.ExpectField(t, err, "more/mod.yaml", manifest, "entry")

	fsys["more/mod.yaml"] = &fstest.MapFile{Data: []byte("manifestVersion: 1\nid: [7]\n")}
	_, err = mgr.DiscoverFS(fsys)
	loaderrtest.Expect(t, err, "more/mod.yaml", 2)

	other := loaderrtest.Indent(`{"manifestVersion": 1, "id": "more.enemies", "name": "Copy", "version": "1.0.0", "entry": "main.ts"}`)
	fsys["more/mod.yaml"] = &fstest.MapFile{Data: loaderrtest.Indent(`{"manifestVersion": 1, "id": "more.enemies", "name": "More Enemies", "version": "1.0.0", "entry": "main.ts"}`)}
	fsys["more/main.ts"] = &fstest.MapFile{Data: []byte("export {}")}
	fsys["zcopy/mod.yaml"] = &fstest.MapFile{Data: other}
	fsys["zcopy/main.ts"] = &fstest.MapFile{Data: []byte("export {}")}
	_, err = mgr.DiscoverFS(fsys)
	if !errors.Is(err, modding.ErrDuplicateMod) {
		t.Fatalf("err = %v, want ErrDuplicateMod", err)
	}
	loaderrtest.ExpectField(t, err, "zcopy/mod.yaml", other, "id")
	delete(fsys, "zcopy/mod.yaml")
	delete(fsys, "zcopy/main.ts")

	newer := loaderrtest.Indent(`{"manifestVersion": 1, "id": "more.enemies", "name": "More Enemies", "version": "1.0.0", "entry": "main.ts", "gameVersion": ">=2.0.0"}`)
	fsys["more/mod.yaml"] = &fstest.MapFile{Data: newer}
	var versioned *modding.Manager
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		versioned = modding.NewManager(cx, modding.ManagerOptions{GameVersion: "1.0.0"})
		return core.View{}
	})
	_, err = versioned.DiscoverFS(fsys)
	if !errors.Is(err, modding.ErrVersionMismatch) {
		t.Fatalf("err = %v, want ErrVersionMismatch", err)
	}
	loaderrtest.ExpectField(t, err, "more/mod.yaml", newer, "gameVersion")
}
