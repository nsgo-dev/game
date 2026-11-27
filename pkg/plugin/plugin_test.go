package plugin

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	rtdebug "runtime/debug"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/datatable"
	"github.com/nsgo-dev/game/pkg/debug"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

type testItemRow struct {
	ID   string `yaml:"id"`
	Cost int    `yaml:"cost"`
}

type testHealth struct {
	HP  int `json:"hp"`
	Max int `json:"max"`
}

type testMana struct {
	MP int `json:"mp"`
}

type noopFXInstance struct{}

func (noopFXInstance) Update(float64)                     {}
func (noopFXInstance) Draw(*pixelfx.Painter, float64)     {}
func (noopFXInstance) DrawBack(*pixelfx.Painter, float64) {}

func testEffect(name string) *pixelfx.Effect {
	return &pixelfx.Effect{
		Meta: pixelfx.Meta{Name: name, Size: [2]int{16, 16}, Duration: 0.5, FPS: 12},
		New:  func(*pixelfx.Options) pixelfx.Instance { return noopFXInstance{} },
	}
}

func newFullHost() *Host {
	return &Host{
		Components: sceneformat.NewComponentRegistry(),
		Scheduler:  entities.NewScheduler(),
		DataTables: datatable.NewRegistry(),
		Console:    debug.NewConsole(),
	}
}

func mustRowType[T any](t *testing.T) *datatable.RowType {
	t.Helper()
	rt, err := datatable.RowTypeFor[T]()
	if err != nil {
		t.Fatalf("RowTypeFor: %v", err)
	}
	return rt
}

func TestInstallAPIMismatch(t *testing.T) {
	for _, api := range []int{0, -1, APIVersion + 1} {
		h := newFullHost()
		p := Plugin{
			Name: "badapi",
			API:  api,
			Components: []sceneformat.ComponentType{{
				Name: "badapi_comp",
			}},
			Systems: []System{{
				Name:  "badapi_sys",
				Stage: StageUpdate,
				Fn:    func(*entities.World, time.Duration) {},
			}},
			PixelFX: map[string]*pixelfx.Effect{
				"testbadapi/spark": testEffect("spark"),
			},
			RowTypes: map[string]*datatable.RowType{
				"badapi.yaml": mustRowType[testItemRow](t),
			},
			Console: []ConsoleCommand{{
				Name: "badapi_cmd",
				Fn:   func([]string) (string, error) { return "ok", nil },
			}},
		}
		err := Install(h, p)
		if !errors.Is(err, ErrAPIMismatch) {
			t.Fatalf("API=%d: Install = %v, want ErrAPIMismatch", api, err)
		}
		assertHostEmpty(t, h)
		if _, ok := pixelfx.Lookup("testbadapi/spark"); ok {
			t.Fatalf("API=%d: pixelfx effect registered despite API mismatch", api)
		}
	}
}

func TestInstallDuplicatePlugin(t *testing.T) {
	t.Run("same call", func(t *testing.T) {
		h := newFullHost()
		p1 := Plugin{
			Name:       "dup",
			API:        APIVersion,
			Components: []sceneformat.ComponentType{{Name: "dup_a"}},
		}
		p2 := Plugin{
			Name:       "dup",
			API:        APIVersion,
			Components: []sceneformat.ComponentType{{Name: "dup_b"}},
		}
		err := Install(h, p1, p2)
		if !errors.Is(err, ErrDuplicatePlugin) {
			t.Fatalf("Install = %v, want ErrDuplicatePlugin", err)
		}
		assertHostEmpty(t, h)
	})

	t.Run("across calls", func(t *testing.T) {
		h := newFullHost()
		p1 := Plugin{
			Name:       "already",
			API:        APIVersion,
			Components: []sceneformat.ComponentType{{Name: "already_comp"}},
		}
		if err := Install(h, p1); err != nil {
			t.Fatalf("first Install: %v", err)
		}
		p2 := Plugin{
			Name:       "already",
			API:        APIVersion,
			Components: []sceneformat.ComponentType{{Name: "second_comp"}},
		}
		if err := Install(h, p2); !errors.Is(err, ErrDuplicatePlugin) {
			t.Fatalf("second Install = %v, want ErrDuplicatePlugin", err)
		}
		if _, ok := h.Components.Lookup("second_comp"); ok {
			t.Fatal("second_comp registered despite duplicate plugin")
		}
		if len(h.Plugins()) != 1 {
			t.Fatalf("Plugins() = %+v, want 1 entry", h.Plugins())
		}
	})
}

func TestInstallCollisionLeavesNothingRegistered(t *testing.T) {
	itemRT := mustRowType[testItemRow](t)

	cases := []struct {
		name    string
		prepare func(*Host)
		plugins []Plugin
		fxID    string
	}{
		{
			name: "component collides with host",
			prepare: func(h *Host) {
				h.Components.MustRegister(sceneformat.ComponentType{Name: "taken_comp"})
			},
			fxID: "testcol1/fx",
			plugins: []Plugin{
				{
					Name:       "first",
					API:        APIVersion,
					Components: []sceneformat.ComponentType{{Name: "fresh_comp"}},
					Systems:    []System{{Name: "fresh_sys", Stage: StageUpdate, Fn: func(*entities.World, time.Duration) {}}},
					PixelFX:    map[string]*pixelfx.Effect{"testcol1/fx": testEffect("fx")},
					RowTypes:   map[string]*datatable.RowType{"fresh.yaml": itemRT},
					Console:    []ConsoleCommand{{Name: "fresh_cmd", Fn: func([]string) (string, error) { return "", nil }}},
				},
				{
					Name:       "second",
					API:        APIVersion,
					Components: []sceneformat.ComponentType{{Name: "taken_comp"}},
				},
			},
		},
		{
			name: "component collides across plugins",
			fxID: "testcol2/fx",
			plugins: []Plugin{
				{
					Name:       "first",
					API:        APIVersion,
					Components: []sceneformat.ComponentType{{Name: "shared_comp"}},
					PixelFX:    map[string]*pixelfx.Effect{"testcol2/fx": testEffect("fx")},
				},
				{
					Name:       "second",
					API:        APIVersion,
					Components: []sceneformat.ComponentType{{Name: "shared_comp"}},
				},
			},
		},
		{
			name: "system collides with scheduler",
			prepare: func(h *Host) {
				_ = h.Scheduler.AddSystem("existing_sys", func(*entities.World, time.Duration) {})
			},
			fxID: "testcol3/fx",
			plugins: []Plugin{
				{
					Name:       "first",
					API:        APIVersion,
					Components: []sceneformat.ComponentType{{Name: "fresh_comp"}},
					PixelFX:    map[string]*pixelfx.Effect{"testcol3/fx": testEffect("fx")},
				},
				{
					Name:    "second",
					API:     APIVersion,
					Systems: []System{{Name: "existing_sys", Fn: func(*entities.World, time.Duration) {}}},
				},
			},
		},
		{
			name: "system collides with reserved stage anchor",
			fxID: "testcol4/fx",
			plugins: []Plugin{
				{
					Name:    "first",
					API:     APIVersion,
					PixelFX: map[string]*pixelfx.Effect{"testcol4/fx": testEffect("fx")},
					Systems: []System{{Name: StageInput.Start(), Fn: func(*entities.World, time.Duration) {}}},
				},
			},
		},
		{
			name: "row type collides with host using normalized path",
			prepare: func(h *Host) {
				_ = h.DataTables.Register("items.yaml", itemRT)
			},
			fxID: "testcol5/fx",
			plugins: []Plugin{
				{
					Name:       "first",
					API:        APIVersion,
					Components: []sceneformat.ComponentType{{Name: "fresh_comp"}},
					PixelFX:    map[string]*pixelfx.Effect{"testcol5/fx": testEffect("fx")},
					RowTypes:   map[string]*datatable.RowType{"./items.yaml": itemRT},
				},
			},
		},
		{
			name: "console command collides with builtin help",
			fxID: "testcol6/fx",
			plugins: []Plugin{
				{
					Name:       "first",
					API:        APIVersion,
					Components: []sceneformat.ComponentType{{Name: "fresh_comp"}},
					PixelFX:    map[string]*pixelfx.Effect{"testcol6/fx": testEffect("fx")},
					Console:    []ConsoleCommand{{Name: "help", Fn: func([]string) (string, error) { return "", nil }}},
				},
			},
		},
		{
			name: "pixelfx collides with non-plugin registry entry",
			prepare: func(*Host) {
				if _, ok := pixelfx.Lookup("testbuiltin/torch"); !ok {
					pixelfx.Register("testbuiltin/torch", testEffect("torch"))
				}
			},
			fxID: "testcol7/fx",
			plugins: []Plugin{
				{
					Name:       "first",
					API:        APIVersion,
					Components: []sceneformat.ComponentType{{Name: "fresh_comp"}},
					Systems:    []System{{Name: "fresh_sys", Stage: StageUpdate, Fn: func(*entities.World, time.Duration) {}}},
					PixelFX: map[string]*pixelfx.Effect{
						"testcol7/fx":       testEffect("fx"),
						"testbuiltin/torch": testEffect("override"),
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newFullHost()
			if tc.prepare != nil {
				tc.prepare(h)
			}
			beforeComps := h.Components.Names()
			beforeOrder := h.Scheduler.Order()
			beforeCmds := h.Console.Commands()

			err := Install(h, tc.plugins...)
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("Install = %v, want ErrConflict", err)
			}
			if !reflect.DeepEqual(h.Components.Names(), beforeComps) {
				t.Fatalf("Components mutated: got %v, want %v", h.Components.Names(), beforeComps)
			}
			if !reflect.DeepEqual(h.Scheduler.Order(), beforeOrder) {
				t.Fatalf("Scheduler mutated: got %v, want %v", h.Scheduler.Order(), beforeOrder)
			}
			if !reflect.DeepEqual(h.Console.Commands(), beforeCmds) {
				t.Fatalf("Console mutated: got %v, want %v", h.Console.Commands(), beforeCmds)
			}
			if _, ok := h.DataTables.Lookup("fresh.yaml"); ok {
				t.Fatal("DataTables mutated with fresh.yaml")
			}
			if len(h.Plugins()) != 0 {
				t.Fatalf("Plugins() mutated: %+v", h.Plugins())
			}
			if tc.fxID != "" {
				if _, ok := pixelfx.Lookup(tc.fxID); ok {
					t.Fatalf("pixelfx effect %q registered despite collision", tc.fxID)
				}
			}
		})
	}
}

func TestInstallDeterministicSystemOrderAndStages(t *testing.T) {
	h := &Host{Scheduler: entities.NewScheduler()}
	var log []string
	rec := func(name string) func(*entities.World, time.Duration) {
		return func(*entities.World, time.Duration) {
			log = append(log, name)
		}
	}

	// Register out of stage order across two plugins, plus Before/After inside a stage.
	p1 := Plugin{
		Name: "alpha",
		API:  APIVersion,
		Systems: []System{
			{Name: "camera", Stage: StageLate, Fn: rec("camera")},
			{Name: "physics-b", Stage: StagePhysics, Fn: rec("physics-b")},
			{Name: "update-2", Stage: StageUpdate, After: []string{"update-1"}, Fn: rec("update-2")},
			{Name: "input-1", Stage: StageInput, Fn: rec("input-1")},
		},
	}
	p2 := Plugin{
		Name: "beta",
		API:  APIVersion,
		Systems: []System{
			{Name: "physics-a", Stage: StagePhysics, Before: []string{"physics-b"}, Fn: rec("physics-a")},
			{Name: "update-1", Stage: StageUpdate, Fn: rec("update-1")},
			{Name: "input-2", Stage: StageInput, Fn: rec("input-2")},
			{Name: "late-2", Stage: StageLate, Fn: rec("late-2")},
		},
	}

	if err := Install(h, p1, p2); err != nil {
		t.Fatalf("Install: %v", err)
	}
	// A second Install on the same Host must reuse existing stage anchors without error.
	p3 := Plugin{
		Name: "gamma",
		API:  APIVersion,
		Systems: []System{
			{Name: "update-3", Stage: StageUpdate, Fn: rec("update-3")},
		},
	}
	if err := Install(h, p3); err != nil {
		t.Fatalf("second Install: %v", err)
	}

	w := entities.NewWorld()
	if err := h.Scheduler.Step(w, time.Second/60); err != nil {
		t.Fatalf("Step: %v", err)
	}

	want := []string{
		"input-1", "input-2",
		"update-1", "update-2", "update-3",
		"physics-a", "physics-b",
		"camera", "late-2",
	}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("execution order = %v, want %v (full schedule: %s)", log, want, h.Scheduler.String())
	}

	// Calling EnsureStages again is a no-op.
	if err := EnsureStages(h.Scheduler); err != nil {
		t.Fatalf("EnsureStages repeat: %v", err)
	}
}

func TestInstallDoublePixelFXAndCrossPluginConflict(t *testing.T) {
	const fxID = "testdouble/burst"
	fx := testEffect("burst")

	p := Plugin{
		Name:    "spark",
		Module:  "example.com/spark",
		API:     APIVersion,
		PixelFX: map[string]*pixelfx.Effect{fxID: fx},
	}

	h1 := newFullHost()
	if err := Install(h1, p); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	got, ok := pixelfx.Lookup(fxID)
	if !ok || got != fx {
		t.Fatalf("Lookup(%q) = %v, %v", fxID, got, ok)
	}

	// Second Install of the same plugin on a new Host succeeds idempotently without panicking.
	h2 := newFullHost()
	if err := Install(h2, p); err != nil {
		t.Fatalf("second Install on fresh host: %v", err)
	}

	// Concurrent re-installs are race-free.
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Install(&Host{}, p)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Install: %v", err)
		}
	}

	// A different plugin claiming the same effect ID is refused with ErrConflict and leaves Host untouched.
	other := Plugin{
		Name:       "rival",
		Module:     "example.com/rival",
		API:        APIVersion,
		Components: []sceneformat.ComponentType{{Name: "rival_comp"}},
		PixelFX:    map[string]*pixelfx.Effect{fxID: testEffect("rival-burst")},
	}
	h3 := newFullHost()
	if err := Install(h3, other); !errors.Is(err, ErrConflict) {
		t.Fatalf("rival Install = %v, want ErrConflict", err)
	}
	assertHostEmpty(t, h3)
	// Original effect must remain intact.
	if cur, ok := pixelfx.Lookup(fxID); !ok || cur != fx {
		t.Fatalf("original pixelfx effect replaced: %v", cur)
	}
}

func TestInstallCodecsAppliedToMultipleWorlds(t *testing.T) {
	h := newFullHost()
	p1 := Plugin{
		Name: "health",
		API:  APIVersion,
		Codecs: func(w *entities.World) error {
			return entities.RegisterCodec[testHealth](w, "health")
		},
	}
	p2 := Plugin{
		Name: "mana",
		API:  APIVersion,
		Codecs: func(w *entities.World) error {
			return entities.RegisterCodec[testMana](w, "mana")
		},
	}
	if err := Install(h, p1, p2); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// First world: populate and snapshot.
	w1 := entities.NewWorld()
	if err := h.OnWorld(w1); err != nil {
		t.Fatalf("OnWorld(w1): %v", err)
	}
	e1 := w1.Spawn()
	entities.Set(w1, e1, testHealth{HP: 7, Max: 10})
	entities.Set(w1, e1, testMana{MP: 42})
	snap, err := w1.Snapshot()
	if err != nil {
		t.Fatalf("w1.Snapshot: %v", err)
	}

	// Second world: apply codecs again and restore snapshot.
	w2 := entities.NewWorld()
	if err := h.OnWorld(w2); err != nil {
		t.Fatalf("OnWorld(w2): %v", err)
	}
	if err := w2.Restore(snap); err != nil {
		t.Fatalf("w2.Restore: %v", err)
	}
	gotH, ok := entities.Get[testHealth](w2, e1)
	if !ok || *gotH != (testHealth{HP: 7, Max: 10}) {
		t.Fatalf("w2 health = %+v, %v", gotH, ok)
	}
	gotM, ok := entities.Get[testMana](w2, e1)
	if !ok || *gotM != (testMana{MP: 42}) {
		t.Fatalf("w2 mana = %+v, %v", gotM, ok)
	}

	if err := h.OnWorld(nil); !errors.Is(err, ErrNilWorld) {
		t.Fatalf("OnWorld(nil) = %v, want ErrNilWorld", err)
	}
	var nilHost *Host
	if err := nilHost.OnWorld(w2); !errors.Is(err, ErrNilHost) {
		t.Fatalf("nilHost.OnWorld = %v, want ErrNilHost", err)
	}
}

func TestInstallComponentSpawnPreserved(t *testing.T) {
	h := newFullHost()
	p := Plugin{
		Name:   "health",
		Module: "example.com/health",
		API:    APIVersion,
		Components: []sceneformat.ComponentType{{
			Name: "health",
			Fields: []sceneformat.Field{
				{Name: "hp", Kind: sceneformat.FieldInt, Default: 10},
				{Name: "max", Kind: sceneformat.FieldInt, Default: 10},
			},
			Spawn: func(w *entities.World, e entities.Entity, fields map[string]any) error {
				entities.Set(w, e, testHealth{HP: fields["hp"].(int), Max: fields["max"].(int)})
				return nil
			},
		}},
	}
	if err := Install(h, p); err != nil {
		t.Fatalf("Install: %v", err)
	}

	doc, err := sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: main
    objects:
      - id: hero
        components:
          health:
            hp: 6
`), sceneformat.ParseOptions{Components: h.Components})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	w := entities.NewWorld()
	spawned, err := sceneformat.SpawnEntitiesWith(doc, w, sceneformat.SpawnOptions{Components: h.Components})
	if err != nil {
		t.Fatalf("SpawnEntitiesWith: %v", err)
	}
	got, ok := entities.Get[testHealth](w, spawned["hero"])
	if !ok || *got != (testHealth{HP: 6, Max: 10}) {
		t.Fatalf("spawned health = %+v, %v", got, ok)
	}
}

func TestInstallRowTypesConsoleAndEscapeHatch(t *testing.T) {
	h := newFullHost()
	itemRT := mustRowType[testItemRow](t)

	var hookOrder []string
	var sawInstalled []Installed
	p1 := Plugin{
		Name:     "economy",
		Module:   "github.com/nsgo-dev/game",
		API:      APIVersion,
		RowTypes: map[string]*datatable.RowType{"data/items.yaml": itemRT},
		Console: []ConsoleCommand{{
			Name: "grant",
			Help: "grant gold",
			Fn: func(args []string) (string, error) {
				return "granted " + strings.Join(args, " "), nil
			},
		}},
		Install: func(host *Host) error {
			hookOrder = append(hookOrder, "economy")
			sawInstalled = host.Plugins()
			if host.DataTables.Match("data/items.yaml") != itemRT {
				return errors.New("row type not registered before escape hatch")
			}
			return nil
		},
	}
	p2 := Plugin{
		Name: "quests",
		API:  APIVersion,
		Install: func(*Host) error {
			hookOrder = append(hookOrder, "quests")
			return nil
		},
	}

	if err := Install(h, p1, p2); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !reflect.DeepEqual(hookOrder, []string{"economy", "quests"}) {
		t.Fatalf("hookOrder = %v", hookOrder)
	}
	if len(sawInstalled) != 2 || sawInstalled[0].Name != "economy" || sawInstalled[1].Name != "quests" {
		t.Fatalf("sawInstalled = %+v", sawInstalled)
	}
	if !h.Has("economy") || !h.Has("quests") || h.Has("missing") {
		t.Fatal("Host.Has mismatch")
	}
	if h.DataTables.Match("data/items.yaml") != itemRT {
		t.Fatal("DataTables did not match registered row type")
	}
	out, err := h.Console.Exec("grant 100")
	if err != nil || out != "granted 100" {
		t.Fatalf("Console.Exec = %q, %v", out, err)
	}
}

func TestInstallNilHostFieldsSkipContributions(t *testing.T) {
	itemRT := mustRowType[testItemRow](t)
	h := &Host{}
	p := Plugin{
		Name:       "minimal",
		API:        APIVersion,
		Components: []sceneformat.ComponentType{{Name: "minimal_comp"}},
		Systems:    []System{{Name: "minimal_sys", Stage: StageUpdate, Fn: func(*entities.World, time.Duration) {}}},
		RowTypes:   map[string]*datatable.RowType{"minimal.yaml": itemRT},
		Console:    []ConsoleCommand{{Name: "minimal_cmd", Fn: func([]string) (string, error) { return "", nil }}},
	}
	if err := Install(h, p); err != nil {
		t.Fatalf("Install on empty Host: %v", err)
	}
	if !h.Has("minimal") || len(h.Plugins()) != 1 {
		t.Fatalf("Plugins() = %+v", h.Plugins())
	}
}

func TestInstallValidationErrors(t *testing.T) {
	if err := Install(nil, Plugin{Name: "a", API: APIVersion}); !errors.Is(err, ErrNilHost) {
		t.Fatalf("Install(nil) = %v, want ErrNilHost", err)
	}
	if err := Install(&Host{}); err != nil {
		t.Fatalf("Install(empty) = %v", err)
	}

	itemRT := mustRowType[testItemRow](t)
	cases := []struct {
		name string
		p    Plugin
	}{
		{"invalid name", Plugin{Name: "BadName", API: APIVersion}},
		{"invalid module", Plugin{Name: "ok", Module: "/bad", API: APIVersion}},
		{"empty component name", Plugin{Name: "ok", API: APIVersion, Components: []sceneformat.ComponentType{{Name: ""}}}},
		{"invalid component field", Plugin{Name: "ok", API: APIVersion, Components: []sceneformat.ComponentType{{Name: "c", Fields: []sceneformat.Field{{Name: ""}}}}}},
		{"empty system name", Plugin{Name: "ok", API: APIVersion, Systems: []System{{Name: "", Fn: noopSystem}}}},
		{"nil system fn", Plugin{Name: "ok", API: APIVersion, Systems: []System{{Name: "s"}}}},
		{"unknown stage", Plugin{Name: "ok", API: APIVersion, Systems: []System{{Name: "s", Stage: Stage("bogus"), Fn: noopSystem}}}},
		{"empty before target", Plugin{Name: "ok", API: APIVersion, Systems: []System{{Name: "s", Before: []string{""}, Fn: noopSystem}}}},
		{"self before target", Plugin{Name: "ok", API: APIVersion, Systems: []System{{Name: "s", Before: []string{"s"}, Fn: noopSystem}}}},
		{"empty after target", Plugin{Name: "ok", API: APIVersion, Systems: []System{{Name: "s", After: []string{""}, Fn: noopSystem}}}},
		{"self after target", Plugin{Name: "ok", API: APIVersion, Systems: []System{{Name: "s", After: []string{"s"}, Fn: noopSystem}}}},
		{"malformed pixelfx id", Plugin{Name: "ok", API: APIVersion, PixelFX: map[string]*pixelfx.Effect{"bad": testEffect("bad")}}},
		{"nil pixelfx effect", Plugin{Name: "ok", API: APIVersion, PixelFX: map[string]*pixelfx.Effect{"cat/nil": nil}}},
		{"nil row type", Plugin{Name: "ok", API: APIVersion, RowTypes: map[string]*datatable.RowType{"a.yaml": nil}}},
		{"invalid row pattern", Plugin{Name: "ok", API: APIVersion, RowTypes: map[string]*datatable.RowType{"[bad": itemRT}}},
		{"empty console name", Plugin{Name: "ok", API: APIVersion, Console: []ConsoleCommand{{Name: "", Fn: func([]string) (string, error) { return "", nil }}}}},
		{"spaced console name", Plugin{Name: "ok", API: APIVersion, Console: []ConsoleCommand{{Name: "a b", Fn: func([]string) (string, error) { return "", nil }}}}},
		{"nil console fn", Plugin{Name: "ok", API: APIVersion, Console: []ConsoleCommand{{Name: "cmd"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newFullHost()
			if err := Install(h, tc.p); !errors.Is(err, ErrInvalidPlugin) {
				t.Fatalf("Install = %v, want ErrInvalidPlugin", err)
			}
			assertHostEmpty(t, h)
		})
	}
}

func TestModuleVersionFromBuildInfo(t *testing.T) {
	bi := &rtdebug.BuildInfo{
		Main: rtdebug.Module{
			Path:    "example.com/mygame",
			Version: "(devel)",
		},
		Deps: []*rtdebug.Module{
			{Path: "example.com/healthkit", Version: "v1.2.3"},
			{Path: "example.com/localplug", Version: "v0.0.0", Replace: &rtdebug.Module{Path: "../localplug"}},
			{Path: "example.com/forkplug", Version: "v1.0.0", Replace: &rtdebug.Module{Path: "example.com/fork", Version: "v1.0.1"}},
		},
	}

	cases := []struct {
		mod  string
		want string
	}{
		{"example.com/mygame", "(devel)"},
		{"example.com/healthkit", "v1.2.3"},
		{"example.com/localplug", "(devel)"},
		{"example.com/forkplug", "v1.0.1"},
		{"example.com/unknown", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := moduleVersionFromBuildInfo(bi, tc.mod); got != tc.want {
			t.Errorf("moduleVersionFromBuildInfo(%q) = %q, want %q", tc.mod, got, tc.want)
		}
	}
	if got := moduleVersionFromBuildInfo(nil, "example.com/mygame"); got != "" {
		t.Errorf("nil build info = %q, want empty", got)
	}
	if got := ModuleVersion(""); got != "" {
		t.Errorf("ModuleVersion(\"\") = %q, want empty", got)
	}
}

func TestStagesMetadata(t *testing.T) {
	stages := Stages()
	want := []Stage{StageInput, StageUpdate, StagePhysics, StageLate}
	if !reflect.DeepEqual(stages, want) {
		t.Fatalf("Stages() = %v, want %v", stages, want)
	}
	for _, s := range stages {
		if !s.Valid() || s.Start() == "" || s.End() == "" || s.String() == "" {
			t.Fatalf("stage %q invalid metadata", s)
		}
	}
	invalid := Stage("other")
	if invalid.Valid() || invalid.Start() != "" || invalid.End() != "" {
		t.Fatalf("invalid stage reported valid metadata")
	}
}

// TestNoForbiddenImports verifies that pkg/plugin does not directly or transitively import pkg/editor, pkg/mcp, or pkg/playtest.
func TestNoForbiddenImports(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	const modPrefix = "github.com/nsgo-dev/game/"
	forbidden := []string{
		modPrefix + "pkg/editor",
		modPrefix + "pkg/mcp",
		modPrefix + "pkg/playtest",
	}

	visited := map[string]bool{}
	queue := []string{modPrefix + "pkg/plugin"}
	fset := token.NewFileSet()

	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if visited[pkg] {
			continue
		}
		visited[pkg] = true

		for _, bad := range forbidden {
			if pkg == bad || strings.HasPrefix(pkg, bad+"/") {
				t.Fatalf("pkg/plugin transitively imports forbidden package %s", pkg)
			}
		}

		dir := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(pkg, modPrefix)))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir(%s): %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("ParseFile(%s): %v", name, err)
			}
			for _, spec := range f.Imports {
				imp, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatalf("Unquote(%s): %v", spec.Path.Value, err)
				}
				if strings.HasPrefix(imp, modPrefix) && !visited[imp] {
					queue = append(queue, imp)
				}
			}
		}
	}
}

func assertHostEmpty(t *testing.T, h *Host) {
	t.Helper()
	if h.Components != nil && len(h.Components.Names()) != 0 {
		t.Fatalf("Components not empty: %v", h.Components.Names())
	}
	if h.Scheduler != nil && len(h.Scheduler.Order()) != 0 {
		t.Fatalf("Scheduler not empty: %v", h.Scheduler.Order())
	}
	if len(h.Plugins()) != 0 {
		t.Fatalf("Plugins not empty: %+v", h.Plugins())
	}
}
