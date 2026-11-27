package repocheck_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/anim3d"
	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/game/pkg/debug"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/game/pkg/fog"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost"
	"github.com/nsgo-dev/game/pkg/online/liveops"
	"github.com/nsgo-dev/game/pkg/online/netplay"
	"github.com/nsgo-dev/game/pkg/quest"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/tooling/apiref"
	"github.com/nsgo-dev/game/pkg/tooling/devstate"
	"github.com/nsgo-dev/game/pkg/worldmap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// jsonImport is the JSON package every JSON format in this module decodes with.
const jsonImport = "encoding/json"

// notJSONFormats are the packages that decode JSON without defining a file format of their own, each with the reason, so the File Formats page need not list them. Every other package that decodes JSON must have an entry there.
var notJSONFormats = map[string]string{
	"online/accounts":       "decodes the JSON bodies of its HTTP API, which is data on the wire",
	"online/accounts/token": "decodes the JSON payload of a signed token, which is data on the wire",
	"editor":                "reads meta.image from packed sprite atlas JSON files and decodes JSON RPC payloads of remote MCP profiler tool calls",
	"editor/editormcp":      "decodes JSON arguments of the editor's MCP tool calls, which are RPC payloads on the wire",
	"gameui":                "marshals and unmarshals settings values inside save.Settings.Custom, the format of the save#settings entry",
	"idle":                  "decodes a Number from a JSON value inside another format",
	"internal/jsonedit":     "strictly decodes component edits sent through mcp tool calls, which are RPC payloads on the wire",
	"internal/samplegltf":   "validates the sample glTF models, glTF being the gltf entry's format",
	"loaderr":               "decodes a document only to find a field's line and column for an error",
	"mcp":                   "decodes JSON arguments and results of MCP automation tool calls, which are RPC payloads on the wire",
	"physics3d":             "decodes a ConvexHull's geometry inside WorldSnapshot, an in-memory/JSON world snapshot for Save/Restore and rollback netcode rather than a standalone file format, as physics3d.SnapshotVersion explains",
	"online/presence":       "decodes JSON frames of Discord's local IPC protocol, which are RPC messages on the wire",
	"refs":                  "scans other formats' JSON documents for file references",
	"rooms":                 "reads LDtk projects, the format of the tilemap#ldtk entry",
	"tooling/assetcheck":    "scans other formats' JSON documents for asset references",
}

// codeFormat is an entry of the File Formats page for a format that is not a YAML format with conformance checks: a JSON format, a Yarn script or another tool's format.
type codeFormat struct {
	// decl is the declaration in the entry's package that reads, writes or defines the format: a function, type, constant or variable, or a method written as "Type.Method". The test fails when the package no longer declares it, so an entry does not outlive its format.
	decl string
	// lang is the language of the examples the entry must show, such as "json", or empty for another tool's format, which the page describes without one.
	lang string
	// read loads one example with the format's reader and returns the reader's error. Nil when checkedBy names the test that does it instead.
	read func(t *testing.T, data []byte) error
	// checkedBy is the test, in the entry's package, that loads the entry's examples with formattest.Examples, for a reader that is unexported or in a main package.
	checkedBy string
}

// codeFormats are the entries of the File Formats page that formattest.Run does not check, by ID. Every entry is either the Doc of a formattest.Format or listed here.
var codeFormats = map[string]codeFormat{
	"save":                                {decl: "Store.Load", lang: "json", read: readSaveSlot},
	"save#settings":                       {decl: "SettingsStore.Load", lang: "json", read: readSettings},
	"worldmap#state":                      {decl: "State.UnmarshalJSON", lang: "json", read: readStrict[worldmap.State]},
	"fog":                                 {decl: "Fog.UnmarshalJSON", lang: "json", read: readStrict[fog.Fog]},
	"entities":                            {decl: "World.Restore", lang: "json", read: readWorldSnapshot},
	"input":                               {decl: "Actions.Load", lang: "json", read: readInputBindings},
	"debug":                               {decl: "Console.LoadTunables", lang: "json", read: readTunables},
	"tooling/devstate":                    {decl: "Store.LoadSnapshot", lang: "json", read: readDevSnapshot},
	"online/liveops":                      {decl: "DecodeCloudSave", lang: "json", read: readCloudSave},
	"online/liveops#sync":                 {decl: "syncState", lang: "json", checkedBy: "TestSyncStateExamplesOnFormatsPage"},
	"quest#snapshot":                      {decl: "Snapshot", lang: "json", read: readQuestSnapshot},
	"online/leaderboard/selfhost#records": {decl: "FileStore.Load", lang: "json", read: readLeaderboardRecords},
	"online/netplay":                      {decl: "ReadCapture", lang: "json", read: readNetplayCapture},
	"l10n#catalog":                        {decl: "Catalog.LoadJSON", lang: "json", read: readStringTable},
	"anim3d":                              {decl: "LoadEventsSidecar", lang: "json", read: readClipEvents},
	"assets":                              {decl: "ParsePackManifest", lang: "json", read: readPackManifest},
	"cmd/sizecheck":                       {decl: "LoadBudget", lang: "json", checkedBy: "TestBudgetExamplesOnFormatsPage"},
	"cmd/sizecheck#mobile":                {decl: "LoadMobileBudget", lang: "json", checkedBy: "TestBudgetExamplesOnFormatsPage"},
	"tooling/apiref":                      {decl: "Unmarshal", lang: "json", read: readAPIReference},
	"flow":                                {decl: "ParseYarn", lang: "yarn", read: readYarn},
	"tilemap":                             {decl: "LoadTiled"},
	"tilemap#world":                       {decl: "ParseTiledWorld"},
	"tilemap#ldtk":                        {decl: "LoadLDtk"},
	"sprite":                              {decl: "LoadAtlas"},
	"text":                                {decl: "LoadBMFont"},
	"gltf":                                {decl: "Load"},
	"world3d":                             {decl: "LoadMeshLibraryFromGLTF"},
	"debug#trace":                         {decl: "Profiler.WriteChromeTrace"},
}

// formatVersions maps each exported constant whose name ends in "Version" and that holds an integer, as "<package>.<name>", to the entry of the File Formats page for the format whose version it is, or to "" for one notFormatVersions explains. A versioned format the page lacks is found this way even when its package already has an entry for another format.
var formatVersions = map[string]string{
	"ability.CurrentVersion":                        "ability",
	"online/accounts.ConfigVersion":                 "online/accounts",
	"online/accounts/sqlstore.SchemaVersion":        "",
	"online/accounts/token.KeySetVersion":           "online/accounts/token",
	"online/achievements.CurrentVersion":            "online/achievements",
	"ai.BTFormatVersion":                            "ai#bt",
	"ai.BTSnapshotVersion":                          "",
	"ai.GOAPVersion":                                "ai#goap",
	"ai.UtilityVersion":                             "ai",
	"anim3d.EventsSidecarVersion":                   "anim3d",
	"animclip.StateMachineVersion":                  "animclip",
	"bullets.FormatVersion":                         "bullets",
	"cards.FormatVersion":                           "cards",
	"online/cloudsave.ConfigVersion":                "online/cloudsave",
	"online/cloudsave/sqlstore.SchemaVersion":       "",
	"cutscene.CurrentVersion":                       "cutscene",
	"cutscene.MinVersion":                           "cutscene",
	"datatable.FormatVersion":                       "datatable",
	"editor.AgentThreadFileVersion":                 "editor#agent-thread",
	"editor.AgentThreadVersion":                     "editor#thread",
	"editor.AutomationsVersion":                     "editor#automations",
	"editor.CheckpointVersion":                      "editor#checkpoint",
	"editor.LayoutVersion":                          "editor#layout",
	"editor.PermissionsVersion":                     "editor#permissions",
	"editor.RecentProjectsVersion":                  "editor",
	"editor.SettingsVersion":                        "editor",
	"flow.MachineSnapshotVersion":                   "",
	"fog.FormatVersion":                             "fog",
	"fighting.FormatVersion":                        "fighting",
	"framedata.CurrentVersion":                      "framedata",
	"gameui.ScreenFormatVersion":                    "gameui",
	"idle.LocaleVersion":                            "idle",
	"importsettings.AssetSourceVersion":             "importsettings#source",
	"importsettings.CurrentVersion":                 "importsettings",
	"input.TouchLayoutVersion":                      "input#touch",
	"internal/samplegltf.GLBVersion":                "",
	"inventory.CurrentVersion":                      "inventory",
	"l10n.AssetManifestVersion":                     "l10n",
	"l10n.StringTableVersion":                       "l10n#strings",
	"online/leaderboard.CurrentVersion":             "online/leaderboard",
	"online/leaderboard/selfhost.ConfigVersion":     "online/leaderboard/selfhost",
	"online/liveops.CloudSaveVersion":               "online/liveops",
	"online/liveops.RemindersVersion":               "online/liveops#reminders",
	"loot.CurrentVersion":                           "loot",
	"modding.ManifestVersion":                       "modding",
	"modding.MinManifestVersion":                    "modding",
	"modding.WorkshopMetadataVersion":               "modding#workshop",
	"ai/navbake.FormatVersion":                      "ai/navbake",
	"ai/navbake3d.FormatVersion":                    "ai/navbake3d",
	"online/netplay.CaptureVersion":                 "online/netplay",
	"online/netplay.ReplicationVersion":             "",
	"online/netplay/wire.CurrentVersion":            "online/netplay/wire",
	"online/netplay/wire.HandshakeHelloVersion":     "",
	"online/netplay/wire.HandshakeRejectVersion":    "",
	"online/netplay/wire.HandshakeWelcomeVersion":   "",
	"online/netplay/wire.RelayFrameVersion":         "",
	"online/netplay/wire.ReplicationMessageVersion": "",
	"particles.FormatVersion":                       "particles",
	"physics.SnapshotVersion":                       "",
	"physics3d.SnapshotVersion":                     "",
	"platformer.CurrentVersion":                     "platformer",
	"playtest.CurrentVersion":                       "playtest",
	"playtest.ReportVersion":                        "playtest#report",
	"plugin.APIVersion":                             "",
	"plugin/manifest.ManifestVersion":               "plugin/manifest",
	"procgen.RoomTemplatesVersion":                  "procgen#rooms",
	"procgen.RulesVersion":                          "procgen",
	"project.BriefVersion":                          "project#brief",
	"project.CurrentVersion":                        "project",
	"quality.FormatVersion":                         "quality",
	"quest.FormatVersion":                           "quest",
	"quest.SnapshotVersion":                         "quest#snapshot",
	"refs.IndexVersion":                             "refs",
	"render3d.LightProbeGridVersion":                "render3d#lightprobes",
	"replay.FormatVersion":                          "replay",
	"resource.CurrentVersion":                       "resource",
	"rhythm.BeatMapVersion":                         "rhythm",
	"rhythm.CalibrationsVersion":                    "rhythm#calibrations",
	"rooms.FormatVersion":                           "rooms",
	"rts.FormationsVersion":                         "rts",
	"sceneformat.ConflictVersion":                   "sceneformat#conflict",
	"sceneformat.CurrentVersion":                    "sceneformat",
	"sceneformat.Lights2DComponentVersion":          "sceneformat",
	"sceneformat.MinVersion":                        "sceneformat",
	"sceneformat.NavRegion2DComponentVersion":       "sceneformat",
	"sceneformat.OnScreenComponentVersion":          "sceneformat",
	"sceneformat.PhysicsComponentVersion":           "sceneformat",
	"sceneformat.SkeletalComponentVersion":          "sceneformat",
	"skeletal.FormatVersion":                        "skeletal",
	"statedump.FormatVersion":                       "statedump",
	"stats.CurrentVersion":                          "stats",
	"tilemap.ChunkFormatVersion":                    "tilemap#chunk",
	"tilemap.TilesetFormatVersion":                  "tilemap#tileset",
	"tooling/apiref.Version":                        "tooling/apiref",
	"tooling/bench.FormatVersion":                   "tooling/bench",
	"tooling/hotreload.MaterialVersion":             "tooling/hotreload",
	"tooling/library.IndexVersion":                  "tooling/library#index",
	"tooling/library.ManifestVersion":               "tooling/library",
	"undo.LogVersion":                               "undo",
	"vehicle.ConfigVersion":                         "vehicle",
	"vehicle.GhostVersion":                          "vehicle#ghost",
	"vehicle.SnapshotVersion":                       "",
	"vn.CastVersion":                                "vn",
	"voxel.FormatVersion":                           "voxel",
	"world3d.FoliageFormatVersion":                  "world3d#foliage",
	"world3d.GridMapFormatVersion":                  "world3d#gridmap",
	"world3d.TerrainFormatVersion":                  "world3d#terrain",
	"worldmap.StateVersion":                         "worldmap#state",
	"worldmap.Version":                              "worldmap",
}

// notFormatVersions are the version constants formatVersions maps to no entry, each with the reason.
var notFormatVersions = map[string]string{
	"online/accounts/sqlstore.SchemaVersion":        "the version of a PostgreSQL schema, which lives in the database",
	"ai.BTSnapshotVersion":                          "the version of in-memory/JSON behavior tree snapshots saved inside game save slots rather than a standalone file format; docs/en/ai.md documents it",
	"online/cloudsave/sqlstore.SchemaVersion":       "the version of a PostgreSQL schema, which lives in the database",
	"flow.MachineSnapshotVersion":                   "the version of in-memory/JSON state machine snapshots saved inside game save slots rather than a standalone file format; docs/en/flow.md documents it",
	"internal/samplegltf.GLBVersion":                "the version of the binary glTF container, the gltf entry's format, that the sample models are checked against",
	"online/netplay.ReplicationVersion":             "the version of the replication protocol on the wire between a ServerWorld and its ClientWorlds, which no file holds; docs/en/netplay.md documents it",
	"online/netplay/wire.HandshakeHelloVersion":     "the version of the connection handshake hello message on the wire, generated from pkg/online/netplay/wire/handshake.wire.yaml; docs/en/netplay-wire.md documents it",
	"online/netplay/wire.HandshakeRejectVersion":    "the version of the connection handshake reject message on the wire, generated from pkg/online/netplay/wire/handshake.wire.yaml; docs/en/netplay-wire.md documents it",
	"online/netplay/wire.HandshakeWelcomeVersion":   "the version of the connection handshake welcome message on the wire, generated from pkg/online/netplay/wire/handshake.wire.yaml; docs/en/netplay-wire.md documents it",
	"online/netplay/wire.RelayFrameVersion":         "the version of the relay outer frame on the wire, generated from pkg/online/netplay/wire/relay.wire.yaml; docs/en/netplay-wire.md documents it",
	"online/netplay/wire.ReplicationMessageVersion": "the version of the replication v2 nibble header on the wire, generated from pkg/online/netplay/wire/replication.wire.yaml; docs/en/netplay-wire.md documents it",
	"physics.SnapshotVersion":                       "the version of in-memory/JSON 2D physics world snapshots used by Save/Restore and rollback netcode rather than a standalone file format; docs/en/physics.md documents it",
	"physics3d.SnapshotVersion":                     "the version of in-memory/JSON 3D physics world snapshots used by Save/Restore and rollback netcode rather than a standalone file format; docs/en/physics3d.md documents it",
	"plugin.APIVersion":                             "the Go API contract version of runtime and editor plugins rather than a standalone file format; docs/en/plugins.md documents it",
	"vehicle.SnapshotVersion":                       "the version of in-memory/JSON vehicle simulation snapshots used by Save/Restore and rollback netcode rather than a standalone file format; docs/en/determinism.md documents it",
}

// readStrict loads data as a T, refusing unknown fields and trailing data, for a type whose own UnmarshalJSON is the format's reader or that a save slot holds as it is.
func readStrict[T any](_ *testing.T, data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var v T
	if err := dec.Decode(&v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

func readSaveSlot(t *testing.T, data []byte) error {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// The example is a game's second schema version, as a game that has migrated its saves once writes.
	var v map[string]any
	return save.NewFileStore(dir, save.WithVersion(2)).Load("main", &v)
}

func readSettings(t *testing.T, data []byte) error {
	// The settings store keeps its defaults for a field it does not know, so the envelope's data is decoded strictly as Settings too.
	type envelope struct {
		Version int             `json:"version"`
		Data    json.RawMessage `json:"data"`
	}
	if err := readStrict[envelope](t, data); err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	if err := readStrict[save.Settings](t, env.Data); err != nil {
		return err
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := save.NewFileSettingsStore(dir).Load()
	return err
}

// examplePosition and exampleHealth are the components the entities example holds, as a game registers its own with entities.RegisterCodec.
type (
	examplePosition struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	exampleHealth struct {
		HP int `json:"hp"`
	}
)

func readWorldSnapshot(t *testing.T, data []byte) error {
	if err := readStrict[entities.WorldSnapshot](t, data); err != nil {
		return err
	}
	var snap entities.WorldSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}
	w := entities.NewWorld()
	if err := entities.RegisterCodec[examplePosition](w, "position"); err != nil {
		t.Fatal(err)
	}
	if err := entities.RegisterCodec[exampleHealth](w, "health"); err != nil {
		t.Fatal(err)
	}
	return w.Restore(snap)
}

// storage returns the core.Storage of a test app.
func storage(t *testing.T) core.Storage {
	return nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} }).Storage()
}

func readInputBindings(t *testing.T, data []byte) error {
	st := storage(t)
	if err := st.SetItem("controls", string(data)); err != nil {
		t.Fatal(err)
	}
	ok, err := input.NewActions().Load(st, "controls")
	if err == nil && !ok {
		err = errors.New("Load found no bindings")
	}
	return err
}

// exampleTunables are the tunables the debug example sets, registered as a game registers its own, since LoadTunables skips a name nothing registered.
var exampleTunables = []string{"cheat.god", "level.name", "player.lives", "player.speed"}

func readTunables(t *testing.T, data []byte) error {
	var names map[string]json.RawMessage
	if err := json.Unmarshal(data, &names); err != nil {
		return err
	}
	for name := range names {
		if !slices.Contains(exampleTunables, name) {
			return fmt.Errorf("tunable %q is not one this test registers; add it to exampleTunables and readTunables", name)
		}
	}
	st := storage(t)
	if err := st.SetItem("debug.console.tunables", string(data)); err != nil {
		t.Fatal(err)
	}
	c := debug.NewConsole()
	var speed float64
	var lives int
	var god bool
	var level string
	c.RegisterFloat("player.speed", 0, 10, &speed)
	c.RegisterInt("player.lives", 1, 9, &lives)
	c.RegisterBool("cheat.god", &god)
	c.RegisterString("level.name", &level)
	return c.LoadTunables(st, "")
}

func readDevSnapshot(t *testing.T, data []byte) error {
	if err := readStrict[devstate.SessionSnapshot](t, data); err != nil {
		return err
	}
	file := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok, err := devstate.NewStore(file).LoadSnapshot()
	if err == nil && !ok {
		err = errors.New("LoadSnapshot found no snapshot")
	}
	return err
}

func readCloudSave(_ *testing.T, data []byte) error {
	v, err := liveops.DecodeCloudSave(data)
	if err == nil && v.Revision == 0 {
		err = errors.New("DecodeCloudSave read a plain save, not a cloud payload")
	}
	return err
}

func readQuestSnapshot(t *testing.T, data []byte) error {
	if err := readStrict[quest.Snapshot](t, data); err != nil {
		return err
	}
	var s quest.Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s.Version != quest.SnapshotVersion {
		return fmt.Errorf("version %d, want %d", s.Version, quest.SnapshotVersion)
	}
	return nil
}

func readLeaderboardRecords(t *testing.T, data []byte) error {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scores"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scores", selfhost.AllTimePeriod+".jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := selfhost.OpenFileStore(dir, selfhost.FileStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	n := 0
	err = store.Load(context.Background(), func(selfhost.Record) error { n++; return nil })
	if err == nil && n == 0 {
		err = errors.New("Load read no record")
	}
	return err
}

func readNetplayCapture(_ *testing.T, data []byte) error {
	recs, err := netplay.ReadCapture(bytes.NewReader(data))
	if err == nil && len(recs) == 0 {
		err = errors.New("ReadCapture read no record")
	}
	return err
}

func readStringTable(_ *testing.T, data []byte) error {
	return l10n.NewCatalog("en").LoadJSON("en", data)
}

func readClipEvents(_ *testing.T, data []byte) error {
	if _, err := anim3d.LoadEventsSidecar(data); err != nil {
		return err
	}
	// Verify the sidecar loader refuses unknown fields and unsupported versions.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	withUnknown := make(map[string]json.RawMessage, len(obj)+1)
	for k, v := range obj {
		withUnknown[k] = v
	}
	withUnknown["formattestUnknownField"] = json.RawMessage("true")
	mutated, err := json.Marshal(withUnknown)
	if err != nil {
		return err
	}
	if _, err := anim3d.LoadEventsSidecar(mutated); err == nil {
		return errors.New("LoadEventsSidecar accepted an unknown top-level field")
	}

	withFutureVersion := make(map[string]json.RawMessage, len(obj))
	for k, v := range obj {
		withFutureVersion[k] = v
	}
	withFutureVersion["version"] = json.RawMessage(strconv.Itoa(anim3d.EventsSidecarVersion + 1))
	mutated, err = json.Marshal(withFutureVersion)
	if err != nil {
		return err
	}
	if _, err := anim3d.LoadEventsSidecar(mutated); err == nil {
		return errors.New("LoadEventsSidecar accepted an unsupported future version")
	}

	// Also verify unknown fields inside a clip event object are refused.
	if rawClips, ok := obj["clips"]; ok {
		var clips map[string][]map[string]json.RawMessage
		if err := json.Unmarshal(rawClips, &clips); err == nil {
			for clipName, events := range clips {
				if len(events) == 0 {
					continue
				}
				events[0]["formattestUnknownField"] = json.RawMessage("true")
				clips[clipName] = events
				encodedClips, err := json.Marshal(clips)
				if err != nil {
					return err
				}
				withUnknownEvent := make(map[string]json.RawMessage, len(obj))
				for k, v := range obj {
					withUnknownEvent[k] = v
				}
				withUnknownEvent["clips"] = encodedClips
				mutated, err = json.Marshal(withUnknownEvent)
				if err != nil {
					return err
				}
				if _, err := anim3d.LoadEventsSidecar(mutated); err == nil {
					return errors.New("LoadEventsSidecar accepted an unknown clip event field")
				}
				break
			}
		}
	}
	return nil
}

func readPackManifest(_ *testing.T, data []byte) error {
	_, err := assets.ParsePackManifest(data)
	return err
}

func readAPIReference(_ *testing.T, data []byte) error {
	_, err := apiref.Unmarshal(data)
	return err
}

func readYarn(_ *testing.T, data []byte) error {
	_, err := flow.ParseYarn(string(data))
	return err
}

// readFormatEntries returns the entries docs/<lang>/formats.md marks, in page order, as formattest reads them.
func readFormatEntries(t *testing.T, lang string) []formattest.Entry {
	t.Helper()
	name := "docs/" + lang + "/formats.md"
	data, err := os.ReadFile(filepath.Join("docs", lang, "formats.md"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := formattest.ParseEntries(data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(entries) == 0 {
		t.Fatalf("%s marks no entries", name)
	}
	return entries
}

// walkSources calls visit with the slash path of the package directory, the slash path of the file and its parsed syntax for every Go file of the module, test files included when tests is set, skipping testdata, hidden and underscored directories and other modules.
func walkSources(t *testing.T, tests bool, visit func(pkg, file string, f *ast.File)) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != "." && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			if p != "." {
				if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
					return filepath.SkipDir // another module
				}
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || (!tests && strings.HasSuffix(name, "_test.go")) {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		visit(strings.TrimPrefix(filepath.ToSlash(filepath.Dir(p)), "pkg/"), filepath.ToSlash(p), f)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// jsonDecoders returns, by slash path relative to the module root, the packages whose non-test files call encoding/json's Unmarshal or NewDecoder.
func jsonDecoders(t *testing.T) []string {
	t.Helper()
	var pkgs []string
	walkSources(t, false, func(pkg, _ string, f *ast.File) {
		if slices.Contains(pkgs, pkg) {
			return
		}
		jsonName, ok := importName(f, jsonImport)
		if !ok {
			return
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == jsonName && (sel.Sel.Name == "Unmarshal" || sel.Sel.Name == "NewDecoder") {
					if !slices.Contains(pkgs, pkg) {
						pkgs = append(pkgs, pkg)
					}
					return false
				}
			}
			return true
		})
	})
	slices.Sort(pkgs)
	return pkgs
}

// declarations returns, by package, the names its non-test files declare at top level: functions, types, constants and variables by name, and methods as "Type.Method".
func declarations(t *testing.T) map[string]map[string]bool {
	t.Helper()
	decls := map[string]map[string]bool{}
	walkSources(t, false, func(pkg, _ string, f *ast.File) {
		if decls[pkg] == nil {
			decls[pkg] = map[string]bool{}
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				name := d.Name.Name
				if d.Recv != nil && len(d.Recv.List) == 1 {
					name = recvName(d.Recv.List[0].Type) + "." + name
				}
				decls[pkg][name] = true
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						decls[pkg][s.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range s.Names {
							decls[pkg][n.Name] = true
						}
					}
				}
			}
		}
	})
	return decls
}

// recvName returns the type name of a method's receiver, without a pointer or type parameters.
func recvName(x ast.Expr) string {
	for {
		switch e := x.(type) {
		case *ast.StarExpr:
			x = e.X
		case *ast.IndexExpr:
			x = e.X
		case *ast.IndexListExpr:
			x = e.X
		case *ast.Ident:
			return e.Name
		default:
			return ""
		}
	}
}

// versionConstants returns, as "<package>.<name>" and sorted, the exported top-level constants of the module's non-test files whose name ends in "Version" and whose value is an integer literal.
func versionConstants(t *testing.T) []string {
	t.Helper()
	var out []string
	walkSources(t, false, func(pkg, _ string, f *ast.File) {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, s := range g.Specs {
				v := s.(*ast.ValueSpec)
				for i, n := range v.Names {
					if !n.IsExported() || !strings.HasSuffix(n.Name, "Version") || i >= len(v.Values) {
						continue
					}
					if lit, ok := v.Values[i].(*ast.BasicLit); ok && lit.Kind == token.INT {
						out = append(out, pkg+"."+n.Name)
					}
				}
			}
		}
	})
	slices.Sort(out)
	return out
}

// conformanceDocs returns the Doc of every formattest.Format composite literal in the module's tests, by the test file that holds it, whether the literal is passed to formattest.Run directly or built in a variable or helper first. Doc may be a string literal or a string constant of the package; any other Doc, or a literal without one, is returned as "".
func conformanceDocs(t *testing.T) map[string][]string {
	t.Helper()
	consts := map[string]map[string]string{} // package -> constant -> its string value
	type format struct {
		pkg string
		doc ast.Expr // the Doc field's value, or nil
	}
	formats := map[string][]format{}
	walkSources(t, true, func(pkg, file string, f *ast.File) {
		if consts[pkg] == nil {
			consts[pkg] = map[string]string{}
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, s := range g.Specs {
				v := s.(*ast.ValueSpec)
				for i, n := range v.Names {
					if i < len(v.Values) {
						if b, ok := v.Values[i].(*ast.BasicLit); ok && b.Kind == token.STRING {
							consts[pkg][n.Name], _ = strconv.Unquote(b.Value)
						}
					}
				}
			}
		}
		if !strings.HasSuffix(file, "_test.go") {
			return
		}
		name, ok := importName(f, formattestImport)
		if !ok {
			return
		}
		ast.Inspect(f, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok || !isFormatType(cl.Type, name) {
				return true
			}
			fm := format{pkg: pkg}
			for _, elt := range cl.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Doc" {
						fm.doc = kv.Value
					}
				}
			}
			formats[file] = append(formats[file], fm)
			return true
		})
	})
	docs := map[string][]string{}
	for file, list := range formats {
		for _, fm := range list {
			doc := ""
			switch e := fm.doc.(type) {
			case *ast.BasicLit:
				if e.Kind == token.STRING {
					doc, _ = strconv.Unquote(e.Value)
				}
			case *ast.Ident:
				doc = consts[fm.pkg][e.Name]
			}
			docs[file] = append(docs[file], doc)
		}
	}
	return docs
}

// isFormatType reports whether x is the type formattest.Format, with or without type arguments, where name is the name the file imports formattest by.
func isFormatType(x ast.Expr, name string) bool {
	switch e := x.(type) {
	case *ast.IndexExpr:
		x = e.X
	case *ast.IndexListExpr:
		x = e.X
	}
	sel, ok := x.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Format" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == name
}

// testFuncs returns, by package, the names of the top-level test functions in the module's test files.
func testFuncs(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	walkSources(t, true, func(pkg, file string, f *ast.File) {
		if !strings.HasSuffix(file, "_test.go") {
			return
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				out[pkg] = append(out[pkg], fn.Name.Name)
			}
		}
	})
	return out
}

// validJSONLines reports whether data is JSON Lines of more than one line: one well-formed JSON value on each line.
func validJSONLines(data []byte) bool {
	lines := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
	for _, l := range lines {
		if !json.Valid(l) {
			return false
		}
	}
	return len(lines) > 1
}

// pkgDir returns the directory on disk for a logical package path (checking pkg/<pkg> first, then <pkg>).
func pkgDir(pkg string) string {
	p := filepath.FromSlash(pkg)
	if _, err := os.Stat(filepath.Join("pkg", p)); err == nil {
		return filepath.Join("pkg", p)
	}
	return p
}

// TestFormatsPage checks the File Formats page, docs/en/formats.md and docs/zh/formats.md, against the code. Both pages mark the same entries in the same order, and each entry names a package of the module. Each entry is either the Doc of a formattest.Format, whose formattest.Run checks the entry's YAML examples, or in codeFormats, whose declaration the package must still have and whose JSON or Yarn examples the format's reader must load. Every package that decodes JSON has an entry unless notJSONFormats lists it, every exported integer constant named *Version is the version of an entry's format unless notFormatVersions explains it, and every JSON block on the page is well-formed.
//
// A format can still be missed when it is not YAML with conformance checks, its package decodes no JSON or already has an entry, and no such constant versions it, so a new format adds its entry to the page in the same change.
func TestFormatsPage(t *testing.T) {
	en := readFormatEntries(t, "en")
	zh := readFormatEntries(t, "zh")
	ids := func(entries []formattest.Entry) []string {
		var out []string
		for _, e := range entries {
			out = append(out, e.ID)
		}
		return out
	}
	if !slices.Equal(ids(en), ids(zh)) {
		t.Errorf("docs/en/formats.md and docs/zh/formats.md mark different entries or order them differently:\nen: %v\nzh: %v", ids(en), ids(zh))
	}
	onPage := map[string]bool{}
	pkgsOnPage := map[string]bool{}
	for _, e := range en {
		onPage[e.ID] = true
		pkg, _, _ := strings.Cut(e.ID, "#")
		pkgsOnPage[pkg] = true
		if goFiles, _ := filepath.Glob(filepath.Join(pkgDir(pkg), "*.go")); len(goFiles) == 0 {
			t.Errorf("docs/en/formats.md:%d: entry %s names %s, which is not a package of the module", e.Line, e.ID, pkg)
		}
	}
	for lang, entries := range map[string][]formattest.Entry{"en": en, "zh": zh} {
		for _, e := range entries {
			for _, b := range e.Blocks {
				if b.Lang == "json" && !json.Valid(b.Data) && !validJSONLines(b.Data) {
					t.Errorf("docs/%s/formats.md:%d: the JSON example of %s is not well-formed JSON", lang, b.Line, e.ID)
				}
			}
		}
	}

	conformance := map[string]bool{}
	docs := conformanceDocs(t)
	if len(docs) == 0 {
		t.Fatal("found no formattest.Format literal; the scan is broken")
	}
	for file, list := range docs {
		for _, doc := range list {
			if doc == "" {
				t.Errorf("%s: a formattest.Format has no Doc this test can read; set it to a string literal or a string constant naming the format's entry on docs/en/formats.md", file)
				continue
			}
			conformance[doc] = true
			if !onPage[doc] {
				t.Errorf("%s runs the conformance checks of %s, which docs/en/formats.md has no entry for; add one marked <!-- format: %s --> to both pages", file, doc, doc)
			}
		}
	}

	decls := declarations(t)
	tests := testFuncs(t)
	for _, e := range en {
		cf, listed := codeFormats[e.ID]
		switch {
		case conformance[e.ID] && listed:
			t.Errorf("entry %s is the Doc of a formattest.Format and in codeFormats; remove it from codeFormats", e.ID)
		case !conformance[e.ID] && !listed:
			t.Errorf("docs/en/formats.md:%d: entry %s is neither the Doc of a formattest.Format nor in codeFormats; remove the entry if its format is gone, or list it", e.Line, e.ID)
		}
		if !listed {
			continue
		}
		pkg, _, _ := strings.Cut(e.ID, "#")
		if !decls[pkg][cf.decl] {
			t.Errorf("codeFormats lists %s as read by %s.%s, which the package no longer declares; update the entry and codeFormats, or remove both if the format is gone", e.ID, pkg, cf.decl)
		}
		switch {
		case cf.lang == "":
		case cf.read != nil:
			for _, ex := range formattest.Examples(t, e.ID, cf.lang) {
				if err := cf.read(t, ex.Data); err != nil {
					t.Errorf("%s:%d: the example of %s does not load: %v", ex.Page, ex.Line, e.ID, err)
				}
			}
		case cf.checkedBy != "":
			if !slices.Contains(tests[pkg], cf.checkedBy) {
				t.Errorf("codeFormats says %s in %s loads the examples of %s, but the package has no such test", cf.checkedBy, pkg, e.ID)
			}
		default:
			t.Errorf("codeFormats gives %s examples in %s but neither a reader nor the test that loads them", e.ID, cf.lang)
		}
	}
	for id := range codeFormats {
		if !onPage[id] {
			t.Errorf("codeFormats lists %s, which docs/en/formats.md has no entry for; add the entry or remove it from the list", id)
		}
	}

	decoders := jsonDecoders(t)
	if len(decoders) == 0 {
		t.Fatal("found no package that decodes JSON; the scan is broken")
	}
	for pkg := range notJSONFormats {
		if !slices.Contains(decoders, pkg) {
			t.Errorf("notJSONFormats lists %s, which no longer decodes JSON; remove it", pkg)
		}
	}
	for _, pkg := range decoders {
		if _, ok := notJSONFormats[pkg]; ok || pkgsOnPage[pkg] {
			continue
		}
		t.Errorf("%s decodes JSON but docs/en/formats.md has no entry for it; add one marked <!-- format: %s --> to both pages, or, if it reads no file format of its own, list it in notJSONFormats with the reason", pkg, pkg)
	}

	versions := versionConstants(t)
	if len(versions) == 0 {
		t.Fatal("found no version constant; the scan is broken")
	}
	for _, c := range versions {
		id, ok := formatVersions[c]
		switch {
		case !ok:
			t.Errorf("%s looks like the version of a format; map it in formatVersions to the format's entry on docs/en/formats.md, adding the entry to both pages if there is none, or to \"\" with the reason in notFormatVersions", c)
		case id == "" && notFormatVersions[c] == "":
			t.Errorf("formatVersions maps %s to no entry, but notFormatVersions gives no reason", c)
		case id != "" && !onPage[id]:
			t.Errorf("formatVersions maps %s to %s, which docs/en/formats.md has no entry for", c, id)
		}
	}
	for c := range formatVersions {
		if !slices.Contains(versions, c) {
			t.Errorf("formatVersions lists %s, which the module no longer declares as an integer constant; remove it", c)
		}
	}
	for c := range notFormatVersions {
		if id, ok := formatVersions[c]; !ok || id != "" {
			t.Errorf("notFormatVersions explains %s, which formatVersions does not map to \"\"", c)
		}
	}
}
