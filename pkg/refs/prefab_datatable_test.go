package refs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/refs"
)

func TestScanAndPlanMovePrefabVariantsAndDataTable(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"game.yaml": `version: 1
name: Dungeon Run
id: dev.example.dungeon
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`,
		"prefabs/slime.prefab.yaml": `version: 1
name: slime
components:
  health: { max: 2 }
`,
		"prefabs/slime_blue.prefab.yaml": `version: 1
inherits: prefabs/slime.prefab.yaml
name: slime_blue
`,
		"scenes/level_01.scene.yaml": `version: 3
name: level_01
layers:
  - name: actors
    space: world
    objects:
      - id: slime_01
        prefab: prefabs/slime.prefab.yaml
        x: 100
        y: 200
      - id: bare_prefab
        prefab: hero
        x: 40
        y: 40
`,
		"data/spawn_waves.yaml": `waves:
  - id: wave_02
    prefab: prefabs/slime.prefab.yaml
    count: 4
`,
	}
	for rel, body := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	proj, err := refs.Scan(os.DirFS(dir))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if probs := proj.Check(); len(probs) != 0 {
		t.Fatalf("unexpected Check problems (bare prefab name must be ignored): %v", probs)
	}

	var matched []refs.Ref
	for _, r := range proj.Refs {
		if r.Target == "prefabs/slime.prefab.yaml" {
			matched = append(matched, r)
		}
	}
	if len(matched) != 3 {
		t.Fatalf("matched refs = %d (%+v), want 3 (variant, scene, data table)", len(matched), matched)
	}

	plan, err := refs.PlanMove(os.DirFS(dir), "prefabs/slime.prefab.yaml", "prefabs/slime_green.prefab.yaml")
	if err != nil {
		t.Fatalf("PlanMove: %v", err)
	}
	if err := plan.Apply(dir); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, checkPath := range []string{"prefabs/slime_blue.prefab.yaml", "scenes/level_01.scene.yaml", "data/spawn_waves.yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(checkPath)))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "prefabs/slime.prefab.yaml") || !strings.Contains(string(data), "prefabs/slime_green.prefab.yaml") {
			t.Fatalf("%s was not rewritten:\n%s", checkPath, data)
		}
	}
}

func TestDataTableMovedFileReportedAndRepairedByPlanIndex(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string, data []byte) {
		t.Helper()
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("game.yaml", []byte(`version: 1
name: TableTest
id: dev.example.tabletest
appVersion: 1.0.0
window:
  width: 640
  height: 360
`))
	write("art/hero.png", []byte("PNG-HERO-BYTES"))
	write("maps/level.tmj", []byte(`{"type":"map","width":1,"height":1}`))
	write("data/units.yaml", []byte(`version: 1
rows:
  - id: hero
    sprite: art/hero.png
    level: maps/level.tmj
    music: asset:audio/theme.wav
    sound: placeholder:hit.wav
`))

	// 1. Initial index records the hashes for art/hero.png and maps/level.tmj.
	pl, err := refs.PlanIndex(os.DirFS(dir), refs.IndexOptions{})
	if err != nil {
		t.Fatalf("initial PlanIndex: %v", err)
	}
	if err := pl.Apply(dir); err != nil {
		t.Fatalf("initial Apply: %v", err)
	}
	ixData, err := os.ReadFile(filepath.Join(dir, refs.IndexFileName))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	ix, err := refs.ParseIndex(ixData)
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	heroEntry, ok := ix.Lookup("art/hero.png")
	if !ok || heroEntry.SHA256 == "" {
		t.Fatalf("index missing art/hero.png entry: %+v", ix.Files)
	}
	if _, ok := ix.Lookup("maps/level.tmj"); !ok {
		t.Fatalf("index missing maps/level.tmj entry: %+v", ix.Files)
	}

	// 2. Move both files by hand without the tools.
	if err := os.MkdirAll(filepath.Join(dir, "art", "heroes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "art", "hero.png"), filepath.Join(dir, "art", "heroes", "hero.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "maps", "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "maps", "level.tmj"), filepath.Join(dir, "maps", "world", "level.tmj")); err != nil {
		t.Fatal(err)
	}

	// 3. Check reports both broken refs with moved hints (and ignores URI cells).
	proj, err := refs.Scan(os.DirFS(dir))
	if err != nil {
		t.Fatalf("Scan after hand-move: %v", err)
	}
	probs := proj.Check()
	if len(probs) != 2 {
		t.Fatalf("Check() = %v, want 2 moved-file problems", probs)
	}
	if !strings.Contains(probs[0].Message, `art/hero.png`) || !strings.Contains(probs[0].Message, `it moved to "art/heroes/hero.png"`) {
		t.Fatalf("problem[0] = %q, want moved hint for art/hero.png", probs[0])
	}
	if !strings.Contains(probs[1].Message, `maps/level.tmj`) || !strings.Contains(probs[1].Message, `it moved to "maps/world/level.tmj"`) {
		t.Fatalf("problem[1] = %q, want moved hint for maps/level.tmj", probs[1])
	}

	// 4. PlanIndex repairs both cells in data/units.yaml and updates the index to the new paths.
	repair, err := refs.PlanIndex(os.DirFS(dir), refs.IndexOptions{})
	if err != nil {
		t.Fatalf("repair PlanIndex: %v", err)
	}
	if len(repair.Updated) != 2 {
		t.Fatalf("repair.Updated = %+v, want 2 rewritten refs", repair.Updated)
	}
	if err := repair.Apply(dir); err != nil {
		t.Fatalf("repair Apply: %v", err)
	}
	gotTable, err := os.ReadFile(filepath.Join(dir, "data", "units.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotTable), "sprite: art/heroes/hero.png") || !strings.Contains(string(gotTable), "level: maps/world/level.tmj") {
		t.Fatalf("data/units.yaml was not repaired:\n%s", gotTable)
	}
	ixData2, err := os.ReadFile(filepath.Join(dir, refs.IndexFileName))
	if err != nil {
		t.Fatal(err)
	}
	ix2, err := refs.ParseIndex(ixData2)
	if err != nil {
		t.Fatal(err)
	}
	newHero, ok := ix2.Lookup("art/heroes/hero.png")
	if !ok || newHero.SHA256 != heroEntry.SHA256 {
		t.Fatalf("repaired index entry for art/heroes/hero.png = (%+v, %v), want sha %s", newHero, ok, heroEntry.SHA256)
	}
	if _, ok := ix2.Lookup("maps/world/level.tmj"); !ok {
		t.Fatalf("repaired index missing maps/world/level.tmj: %+v", ix2.Files)
	}
	after, err := refs.Scan(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if probs := after.Check(); len(probs) != 0 {
		t.Fatalf("Check after repair = %v, want none", probs)
	}

	// 5. Removing the file without moving keeps its index entry across PlanIndex so a later restore/move can still repair it.
	if err := os.Remove(filepath.Join(dir, "art", "heroes", "hero.png")); err != nil {
		t.Fatal(err)
	}
	keepPlan, err := refs.PlanIndex(os.DirFS(dir), refs.IndexOptions{})
	if err != nil {
		t.Fatalf("PlanIndex while missing: %v", err)
	}
	if err := keepPlan.Apply(dir); err != nil {
		t.Fatalf("Apply while missing: %v", err)
	}
	ixData3, err := os.ReadFile(filepath.Join(dir, refs.IndexFileName))
	if err != nil {
		t.Fatal(err)
	}
	ix3, err := refs.ParseIndex(ixData3)
	if err != nil {
		t.Fatal(err)
	}
	if kept, ok := ix3.Lookup("art/heroes/hero.png"); !ok || kept.SHA256 != heroEntry.SHA256 {
		t.Fatalf("index dropped missing data table target: got (%+v, %v), want sha %s", kept, ok, heroEntry.SHA256)
	}
}

func TestDataTableMissingUnindexedAssetReported(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("game.yaml", `version: 1
name: MissingCell
id: dev.example.missingcell
appVersion: 1.0.0
window:
  width: 640
  height: 360
`)
	write("data/items.yaml", `version: 1
rows:
  - id: sword
    icon: art/icons/sword.png
    script: mods/boss/mod.tsx
    uri: asset:art/icons/shield.png
    label: Iron Sword
`)
	proj, err := refs.Scan(os.DirFS(dir))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	probs := proj.Check()
	if len(probs) != 1 || !strings.Contains(probs[0].Message, `data table "art/icons/sword.png": not found`) {
		t.Fatalf("Check() = %v, want single missing problem for art/icons/sword.png", probs)
	}
	missing := proj.MissingAssets()
	if len(missing) != 1 || missing[0] != "art/icons/sword.png" {
		t.Fatalf("MissingAssets() = %v, want [art/icons/sword.png]", missing)
	}
}
