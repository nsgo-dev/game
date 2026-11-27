package devtools

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/game/pkg/plugin"
	"github.com/nsgo-dev/game/pkg/random"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
)

type stubBot struct{ tag string }

func (stubBot) Decide(int, time.Duration, any, *random.Stream) playtest.InputState {
	return playtest.InputState{}
}

func stubBotFactory(tag string) playtest.BotFactory {
	return func(playtest.BotConfig) (playtest.Bot, error) {
		return stubBot{tag: tag}, nil
	}
}

func validSingleRootScene() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "crate",
		Layers: []sceneformat.LayerDoc{{
			Name:    "main",
			Objects: []sceneformat.ObjectDoc{{ID: "crate-root"}},
		}},
	}
}

func TestInstallPrefabsAndBots(t *testing.T) {
	bridge := mcp.New(mcp.Config{})
	h := &Host{Bridge: bridge}

	var hookRan bool
	p := Plugin{
		Name:   "kit",
		Module: "example.com/kit",
		API:    plugin.APIVersion,
		Prefabs: []PrefabSpec{
			{Prefab: entities.NewPrefab("crate")},
			{Name: "barrel", Scene: validSingleRootScene()},
			{Name: "slime", Spawn: func(w *entities.World, _ map[string]any) (entities.Entity, error) {
				return w.Spawn(), nil
			}},
		},
		Bots: map[string]playtest.BotFactory{
			"kit-test-bot": stubBotFactory("kit"),
		},
		Install: func(host *Host) error {
			hookRan = true
			if !host.Bridge.HasPrefab("crate") {
				return errors.New("prefab not registered before hook")
			}
			return nil
		},
	}

	if err := Install(h, p); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !hookRan {
		t.Fatal("Install hook did not run")
	}
	for _, name := range []string{"crate", "barrel", "slime"} {
		if !bridge.HasPrefab(name) {
			t.Errorf("Bridge missing prefab %q", name)
		}
	}
	if !slices.Contains(playtest.BotProfiles(), "kit-test-bot") {
		t.Fatal("playtest missing kit-test-bot")
	}
	bot, err := playtest.NewBot(playtest.BotConfig{Profile: "kit-test-bot"})
	if err != nil {
		t.Fatalf("NewBot: %v", err)
	}
	if sb, ok := bot.(stubBot); !ok || sb.tag != "kit" {
		t.Fatalf("NewBot = %#v, want stubBot{kit}", bot)
	}
	if !h.Has("kit") || len(h.Plugins()) != 1 {
		t.Fatalf("Plugins() = %+v", h.Plugins())
	}

	// Re-installing the same plugin on a fresh Host re-registers its prefabs on the new bridge and skips re-writing the process-global bot without error.
	h2 := &Host{Bridge: mcp.New(mcp.Config{})}
	if err := Install(h2, p); err != nil {
		t.Fatalf("second Install on fresh Host: %v", err)
	}
	if !h2.Bridge.HasPrefab("crate") {
		t.Fatal("second Host bridge missing crate")
	}
}

func TestInstallConflictsLeaveNothingRegistered(t *testing.T) {
	// Register a bot owned by "owner-bot-plugin" first.
	ownerPlugin := Plugin{
		Name:   "owner-bot-plugin",
		Module: "example.com/owner",
		API:    plugin.APIVersion,
		Bots: map[string]playtest.BotFactory{
			"owner-unique-bot": stubBotFactory("owner"),
		},
	}
	if err := Install(&Host{}, ownerPlugin); err != nil {
		t.Fatalf("seed owner plugin: %v", err)
	}

	cases := []struct {
		name    string
		prepare func(*Host)
		plugins []Plugin
		wantErr error
		botName string
	}{
		{
			name:    "api mismatch",
			wantErr: plugin.ErrAPIMismatch,
			botName: "never-bot-1",
			plugins: []Plugin{{
				Name:    "badapi",
				API:     plugin.APIVersion + 1,
				Prefabs: []PrefabSpec{{Prefab: entities.NewPrefab("never-prefab")}},
				Bots:    map[string]playtest.BotFactory{"never-bot-1": stubBotFactory("x")},
			}},
		},
		{
			name:    "duplicate plugin in batch",
			wantErr: plugin.ErrDuplicatePlugin,
			botName: "never-bot-2",
			plugins: []Plugin{
				{
					Name:    "dup",
					API:     plugin.APIVersion,
					Prefabs: []PrefabSpec{{Prefab: entities.NewPrefab("never-prefab")}},
					Bots:    map[string]playtest.BotFactory{"never-bot-2": stubBotFactory("x")},
				},
				{Name: "dup", API: plugin.APIVersion},
			},
		},
		{
			name:    "prefab collides with bridge",
			wantErr: plugin.ErrConflict,
			botName: "never-bot-3",
			prepare: func(h *Host) {
				_ = h.Bridge.RegisterEntityPrefab(entities.NewPrefab("taken"))
			},
			plugins: []Plugin{{
				Name: "col",
				API:  plugin.APIVersion,
				Prefabs: []PrefabSpec{
					{Prefab: entities.NewPrefab("never-prefab")},
					{Prefab: entities.NewPrefab("taken")},
				},
				Bots: map[string]playtest.BotFactory{"never-bot-3": stubBotFactory("x")},
			}},
		},
		{
			name:    "bot collides with another plugin",
			wantErr: plugin.ErrConflict,
			botName: "never-bot-4",
			plugins: []Plugin{{
				Name:    "rival",
				Module:  "example.com/rival",
				API:     plugin.APIVersion,
				Prefabs: []PrefabSpec{{Prefab: entities.NewPrefab("never-prefab")}},
				Bots: map[string]playtest.BotFactory{
					"never-bot-4":      stubBotFactory("rival"),
					"owner-unique-bot": stubBotFactory("rival"),
				},
			}},
		},
		{
			name:    "bot collides with builtin playtest profile",
			wantErr: plugin.ErrConflict,
			botName: "never-bot-5",
			plugins: []Plugin{{
				Name:    "builtin-col",
				API:     plugin.APIVersion,
				Prefabs: []PrefabSpec{{Prefab: entities.NewPrefab("never-prefab")}},
				Bots: map[string]playtest.BotFactory{
					"never-bot-5":       stubBotFactory("x"),
					"platformer-runner": stubBotFactory("override"),
				},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &Host{Bridge: mcp.New(mcp.Config{})}
			if tc.prepare != nil {
				tc.prepare(h)
			}
			err := Install(h, tc.plugins...)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Install = %v, want %v", err, tc.wantErr)
			}
			if h.Bridge.HasPrefab("never-prefab") {
				t.Fatal("never-prefab registered despite error")
			}
			if len(h.Plugins()) != 0 {
				t.Fatalf("Plugins() = %+v, want empty", h.Plugins())
			}
			if tc.botName != "" && slices.Contains(playtest.BotProfiles(), tc.botName) {
				t.Fatalf("bot %q registered despite error", tc.botName)
			}
		})
	}

	// Ensure owner-unique-bot was not overwritten by rival.
	bot, err := playtest.NewBot(playtest.BotConfig{Profile: "owner-unique-bot"})
	if err != nil {
		t.Fatalf("NewBot: %v", err)
	}
	if sb, ok := bot.(stubBot); !ok || sb.tag != "owner" {
		t.Fatalf("owner-unique-bot overwritten: %#v", bot)
	}
}

func TestInstallValidationErrors(t *testing.T) {
	if err := Install(nil, Plugin{Name: "a", API: plugin.APIVersion}); !errors.Is(err, plugin.ErrNilHost) {
		t.Fatalf("Install(nil) = %v, want ErrNilHost", err)
	}
	if err := Install(&Host{}); err != nil {
		t.Fatalf("Install(empty) = %v", err)
	}

	cases := []struct {
		name string
		p    Plugin
	}{
		{"invalid name", Plugin{Name: "Bad", API: plugin.APIVersion}},
		{"invalid module", Plugin{Name: "ok", Module: "/bad", API: plugin.APIVersion}},
		{"empty prefab spec", Plugin{Name: "ok", API: plugin.APIVersion, Prefabs: []PrefabSpec{{Name: "a"}}}},
		{"multiple prefab kinds", Plugin{Name: "ok", API: plugin.APIVersion, Prefabs: []PrefabSpec{{
			Name:   "a",
			Prefab: entities.NewPrefab("a"),
			Scene:  validSingleRootScene(),
		}}}},
		{"mismatched entity prefab name", Plugin{Name: "ok", API: plugin.APIVersion, Prefabs: []PrefabSpec{{
			Name:   "a",
			Prefab: entities.NewPrefab("b"),
		}}}},
		{"empty entity prefab name", Plugin{Name: "ok", API: plugin.APIVersion, Prefabs: []PrefabSpec{{
			Prefab: entities.NewPrefab(""),
		}}}},
		{"scene prefab empty name", Plugin{Name: "ok", API: plugin.APIVersion, Prefabs: []PrefabSpec{{
			Scene: validSingleRootScene(),
		}}}},
		{"scene prefab two roots", Plugin{Name: "ok", API: plugin.APIVersion, Prefabs: []PrefabSpec{{
			Name: "two",
			Scene: &sceneformat.SceneDoc{
				Version: sceneformat.CurrentVersion,
				Name:    "two",
				Layers: []sceneformat.LayerDoc{{
					Name:    "main",
					Objects: []sceneformat.ObjectDoc{{ID: "a"}, {ID: "b"}},
				}},
			},
		}}}},
		{"custom prefab empty name", Plugin{Name: "ok", API: plugin.APIVersion, Prefabs: []PrefabSpec{{
			Spawn: func(w *entities.World, _ map[string]any) (entities.Entity, error) { return w.Spawn(), nil },
		}}}},
		{"empty bot name", Plugin{Name: "ok", API: plugin.APIVersion, Bots: map[string]playtest.BotFactory{"": stubBotFactory("a")}}},
		{"nil bot factory", Plugin{Name: "ok", API: plugin.APIVersion, Bots: map[string]playtest.BotFactory{"bot-nil": nil}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &Host{Bridge: mcp.New(mcp.Config{})}
			if err := Install(h, tc.p); !errors.Is(err, plugin.ErrInvalidPlugin) {
				t.Fatalf("Install = %v, want ErrInvalidPlugin", err)
			}
			if !reflect.DeepEqual(h.Plugins(), []plugin.Installed(nil)) {
				t.Fatalf("Plugins() = %+v, want nil", h.Plugins())
			}
		})
	}
}

// TestNoEditorImport verifies that pkg/plugin/devtools does not directly or transitively import pkg/editor.
func TestNoEditorImport(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	const modPrefix = "github.com/nsgo-dev/game/"
	const forbidden = modPrefix + "pkg/editor"

	visited := map[string]bool{}
	queue := []string{modPrefix + "pkg/plugin/devtools"}
	fset := token.NewFileSet()

	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if visited[pkg] {
			continue
		}
		visited[pkg] = true
		if pkg == forbidden || strings.HasPrefix(pkg, forbidden+"/") {
			t.Fatalf("pkg/plugin/devtools transitively imports forbidden package %s", pkg)
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
