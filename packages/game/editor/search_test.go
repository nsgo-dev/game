package editor_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const searchProjectYAML = `version: 1
name: SearchFixture
id: dev.example.searchfixture
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
`

const searchRoomScene = `version: 2
uid: scn0000000001
name: Entrance Room
layers:
  - name: actors
    objects:
      - id: hero
        name: Brave Hero
        prefab: prefabs/player
        material: materials/stone.material.yaml
        components:
          controller:
            speed: 120
`

const searchBaseScene = `version: 2
uid: scn0000000002
name: Base Arena
layers:
  - name: actors
    objects:
      - id: floor_anchor
        name: Floor Anchor
`

const searchTrapScene = `version: 2
uid: scn0000000003
name: Spike Trap
layers:
  - name: actors
    objects:
      - id: spikes
        name: Iron Spikes
        components:
          hazard:
            damage: 5
`

const searchDungeonScene = `version: 2
uid: scn0000000004
inherits: scenes/base.scene.yaml
name: Dark Dungeon
overrides:
  floor_anchor:
    components:
      ambient_audio: {}
layers:
  - name: actors
    objects:
      - id: wall_1
        name: North Wall
        material: materials/stone.material.yaml
      - id: goblin_king
        name: Goblin King
        prefab: prefabs/goblin_boss
        components:
          health:
            hp: 50
          ai_patrol: {}
        children:
          - id: crown_gem
            name: Crown Gem
            components:
              glint: {}
      - id: trap_east
        name: East Trap
        scene: scenes/trap.scene.yaml
      - id: wall_2
        name: South Wall
        material: materials/stone.material.yaml
`

const searchStoneMaterial = `type: material
version: 1
uid: mat0000000001
texture: art/stone.png
`

func writeSearchFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName:                searchProjectYAML,
		project.NSGoFileName:            "name: SearchFixture\nid: dev.example.searchfixture\nversion: 1.0.0\n",
		"scenes/room.scene.yaml":        searchRoomScene,
		"scenes/base.scene.yaml":        searchBaseScene,
		"scenes/trap.scene.yaml":        searchTrapScene,
		"scenes/dungeon.scene.yaml":     searchDungeonScene,
		"materials/stone.material.yaml": searchStoneMaterial,
	}, map[string][]byte{
		"art/stone.png": encodeTestPNG(t, 16, 16),
	})
	return dir
}

// TestSearchOpensClosedSceneAndSelectsObject verifies the primary acceptance
// criterion: searching a fixture project for an object ID in a scene that is
// NOT currently open, activating the result in the UI, and verifying the editor
// opens that scene and selects that object.
func TestSearchOpensClosedSceneAndSelectsObject(t *testing.T) {
	dir := writeSearchFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	if app.Exists("id:search-panel") {
		t.Fatal("search-panel shown before a project is open")
	}
	if app.Exists("id:files-search") {
		t.Fatal("files-search shown before a project is open")
	}

	app.Post(func() {
		if err := ed.OpenProject(dir); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	// Main scene (room) is open; dungeon is NOT open.
	roomPath := filepath.Join(dir, "scenes", "room.scene.yaml")
	if ed.FilePath != roomPath || ed.Doc.Name != "Entrance Room" {
		t.Fatalf("after OpenProject: FilePath=%q Name=%q", ed.FilePath, ed.Doc.Name)
	}
	if !app.Exists("id:files-search") {
		t.Fatal("files-search missing with project open")
	}

	// Open the Search panel via FilesView's Search toggle and search for goblin_king (in scenes/dungeon.scene.yaml).
	app.Press("id:files-search")
	app.Settle()
	if !app.Exists("id:search-panel") {
		t.Fatal("search-panel not shown after pressing files-search")
	}

	app.Type("id:project-search-input", "goblin_king")
	app.Settle()

	results := ed.SearchResults()
	if len(results) != 1 {
		t.Fatalf("SearchResults(goblin_king) = %+v, want 1 match", results)
	}
	if results[0].Kind != editor.SearchObject || results[0].File != "scenes/dungeon.scene.yaml" || results[0].ObjectID != "goblin_king" {
		t.Fatalf("result[0] = %+v, want object goblin_king in scenes/dungeon.scene.yaml", results[0])
	}
	rowText := allText(app.Find("id:search-result-0"))
	if !strings.Contains(rowText, "goblin_king") || !strings.Contains(rowText, "scenes/dungeon.scene.yaml") {
		t.Fatalf("search-result-0 text = %q, want object ID and scene path", rowText)
	}

	// Activate the search result row: editor opens scenes/dungeon.scene.yaml and selects goblin_king.
	app.Press("id:search-result-0")
	app.Settle()

	dungeonPath := filepath.Join(dir, "scenes", "dungeon.scene.yaml")
	if ed.FilePath != dungeonPath || ed.ScenePath != "scenes/dungeon.scene.yaml" || ed.Doc.Name != "Dark Dungeon" {
		t.Fatalf("after activating result: FilePath=%q ScenePath=%q Name=%q", ed.FilePath, ed.ScenePath, ed.Doc.Name)
	}
	if ed.SelectedID != "goblin_king" {
		t.Fatalf("SelectedID = %q, want %q", ed.SelectedID, "goblin_king")
	}
	if ed.SelectedFile != "scenes/dungeon.scene.yaml" {
		t.Fatalf("SelectedFile = %q, want %q", ed.SelectedFile, "scenes/dungeon.scene.yaml")
	}
}

// TestFindReferencesOnMaterialMatchesRefsScan verifies the second acceptance
// criterion: running Find References on a material file selected in the file
// browser lists every scene and line that names it, matching refs.Scan exactly,
// and activating a row opens that scene and selects the referring object.
func TestFindReferencesOnMaterialMatchesRefsScan(t *testing.T) {
	dir := writeSearchFixture(t)

	// Compute the ground truth directly from refs.Scan.
	scanned, err := refs.Scan(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	var wantRefs []refs.Ref
	for _, r := range scanned.Refs {
		if r.Target == "materials/stone.material.yaml" {
			wantRefs = append(wantRefs, r)
		}
	}
	if len(wantRefs) != 3 {
		t.Fatalf("expected 3 material references in fixture (2 in dungeon, 1 in room), got %+v", wantRefs)
	}

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	app.Post(func() {
		if err := ed.OpenProject(dir); err != nil {
			t.Error(err)
		}
	})
	app.Settle()

	if app.Enabled("id:files-find-refs") {
		t.Fatal("files-find-refs enabled with no file selected")
	}

	// Select the material file in the file browser and press Find Refs.
	app.Press("id:files-row-materials/stone.material.yaml")
	app.Settle()
	if !app.Enabled("id:files-find-refs") {
		t.Fatal("files-find-refs disabled after selecting a file")
	}

	app.Press("id:files-find-refs")
	app.Settle()

	if !app.Exists("id:search-panel") {
		t.Fatal("search-panel not shown after pressing files-find-refs")
	}
	if ed.SearchTab() != editor.SearchTabReferences {
		t.Fatalf("SearchTab = %v, want SearchTabReferences", ed.SearchTab())
	}
	if ed.ReferencesTarget() != "materials/stone.material.yaml" {
		t.Fatalf("ReferencesTarget = %q, want materials/stone.material.yaml", ed.ReferencesTarget())
	}

	gotRefs := ed.References()
	if len(gotRefs) != len(wantRefs) {
		t.Fatalf("References() len = %d, want %d\ngot:  %+v\nwant: %+v", len(gotRefs), len(wantRefs), gotRefs, wantRefs)
	}
	for i := range wantRefs {
		if gotRefs[i].File != wantRefs[i].File ||
			gotRefs[i].Line != wantRefs[i].Line ||
			gotRefs[i].Column != wantRefs[i].Column ||
			gotRefs[i].Field != wantRefs[i].Field ||
			gotRefs[i].Value != wantRefs[i].Value ||
			gotRefs[i].Target != wantRefs[i].Target {
			t.Fatalf("ref[%d] = %+v, want %+v", i, gotRefs[i], wantRefs[i])
		}
		rowText := allText(app.Find("id:refs-row-" + strconv.Itoa(i)))
		wantLoc := wantRefs[i].File + ":" + strconv.Itoa(wantRefs[i].Line)
		if !strings.Contains(rowText, wantLoc) {
			t.Fatalf("refs-row-%d text = %q, want %q", i, rowText, wantLoc)
		}
	}

	// Activate the second dungeon reference (wall_2): editor opens dungeon.scene.yaml and selects wall_2.
	dungeonRows := make([]int, 0, 2)
	for i, r := range gotRefs {
		if r.File == "scenes/dungeon.scene.yaml" {
			dungeonRows = append(dungeonRows, i)
		}
	}
	if len(dungeonRows) != 2 {
		t.Fatalf("expected 2 dungeon rows, got %v", dungeonRows)
	}
	app.Press("id:refs-row-" + strconv.Itoa(dungeonRows[1]))
	app.Settle()

	dungeonPath := filepath.Join(dir, "scenes", "dungeon.scene.yaml")
	if ed.FilePath != dungeonPath || ed.ScenePath != "scenes/dungeon.scene.yaml" {
		t.Fatalf("after pressing ref row: FilePath=%q ScenePath=%q", ed.FilePath, ed.ScenePath)
	}
	if ed.SelectedID != "wall_2" {
		t.Fatalf("SelectedID after pressing wall_2 ref row = %q, want wall_2", ed.SelectedID)
	}

	// Find References on a texture referenced by a non-scene file (material):
	// activating that row selects the material file in the file browser.
	app.Press("id:files-row-art/stone.png")
	app.Settle()
	app.Press("id:files-find-refs")
	app.Settle()
	texRefs := ed.References()
	if len(texRefs) != 1 || texRefs[0].File != "materials/stone.material.yaml" || texRefs[0].Field != refs.FieldTexture {
		t.Fatalf("texture refs = %+v, want 1 ref from materials/stone.material.yaml", texRefs)
	}
	app.Press("id:refs-row-0")
	app.Settle()
	if ed.SelectedFile != "materials/stone.material.yaml" {
		t.Fatalf("SelectedFile after activating non-scene ref = %q, want materials/stone.material.yaml", ed.SelectedFile)
	}
}

// TestSearchCoversFilesUIDsObjectsComponentsPrefabsAndSceneRefs verifies every
// search dimension required by #914, including nested children, overrides,
// multi-term queries, and preserving unsaved edits when jumping within the
// already-open scene.
func TestSearchCoversFilesUIDsObjectsComponentsPrefabsAndSceneRefs(t *testing.T) {
	dir := writeSearchFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)

	// Without an open project, all project search/refs APIs return ErrNoProject.
	if _, err := ed.Search("hero"); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("Search without project = %v, want ErrNoProject", err)
	}
	if _, err := ed.FindReferences("materials/stone.material.yaml"); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("FindReferences without project = %v, want ErrNoProject", err)
	}
	if err := ed.RefreshSearch(); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("RefreshSearch without project = %v, want ErrNoProject", err)
	}
	if err := ed.OpenSearchResult(editor.SearchResult{File: "game.yaml"}); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("OpenSearchResult without project = %v, want ErrNoProject", err)
	}
	if err := ed.OpenReference(refs.Ref{File: "game.yaml"}); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("OpenReference without project = %v, want ErrNoProject", err)
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// 1. File path & name search -> activating non-scene file selects it in file browser.
	fileHits, err := ed.Search("stone.material")
	if err != nil {
		t.Fatal(err)
	}
	if len(fileHits) != 1 || fileHits[0].Kind != editor.SearchFile || fileHits[0].File != "materials/stone.material.yaml" {
		t.Fatalf("Search(stone.material) = %+v", fileHits)
	}
	if err := ed.OpenSearchResult(fileHits[0]); err != nil {
		t.Fatal(err)
	}
	if ed.SelectedFile != "materials/stone.material.yaml" {
		t.Fatalf("SelectedFile = %q, want materials/stone.material.yaml", ed.SelectedFile)
	}

	// 2. UID search on material and scene files.
	matUIDHits, err := ed.Search("mat0000000001")
	if err != nil {
		t.Fatal(err)
	}
	if len(matUIDHits) != 1 || matUIDHits[0].Kind != editor.SearchUID || matUIDHits[0].File != "materials/stone.material.yaml" || matUIDHits[0].UID != "mat0000000001" {
		t.Fatalf("Search(mat0000000001) = %+v", matUIDHits)
	}
	sceneUIDHits, err := ed.Search("scn0000000003")
	if err != nil {
		t.Fatal(err)
	}
	if len(sceneUIDHits) != 1 || sceneUIDHits[0].Kind != editor.SearchUID || sceneUIDHits[0].File != "scenes/trap.scene.yaml" {
		t.Fatalf("Search(scn0000000003) = %+v", sceneUIDHits)
	}
	if err := ed.OpenSearchResult(sceneUIDHits[0]); err != nil {
		t.Fatal(err)
	}
	if ed.ScenePath != "scenes/trap.scene.yaml" || ed.Doc.Name != "Spike Trap" {
		t.Fatalf("after opening scene by UID: ScenePath=%q Name=%q", ed.ScenePath, ed.Doc.Name)
	}

	// 3. Object display name search (case-insensitive).
	nameHits, err := ed.Search("brave hero")
	if err != nil {
		t.Fatal(err)
	}
	if len(nameHits) != 1 || nameHits[0].Kind != editor.SearchObject || nameHits[0].File != "scenes/room.scene.yaml" || nameHits[0].ObjectID != "hero" {
		t.Fatalf("Search(brave hero) = %+v", nameHits)
	}

	// 4. Component type search (direct component + nested child component + scene override component).
	compHits, err := ed.Search("ai_patrol")
	if err != nil {
		t.Fatal(err)
	}
	if len(compHits) != 1 || compHits[0].Kind != editor.SearchComponent || compHits[0].File != "scenes/dungeon.scene.yaml" || compHits[0].ObjectID != "goblin_king" || compHits[0].Match != "ai_patrol" {
		t.Fatalf("Search(ai_patrol) = %+v", compHits)
	}

	// Nested child component search: activating falls back to selecting top-level ancestor goblin_king.
	childCompHits, err := ed.Search("glint")
	if err != nil {
		t.Fatal(err)
	}
	if len(childCompHits) != 1 || childCompHits[0].Kind != editor.SearchComponent || childCompHits[0].ObjectID != "crown_gem" || childCompHits[0].TopObjectID != "goblin_king" {
		t.Fatalf("Search(glint) = %+v", childCompHits)
	}
	if err := ed.OpenSearchResult(childCompHits[0]); err != nil {
		t.Fatal(err)
	}
	if ed.ScenePath != "scenes/dungeon.scene.yaml" || ed.SelectedID != "goblin_king" {
		t.Fatalf("after activating nested child hit: ScenePath=%q SelectedID=%q", ed.ScenePath, ed.SelectedID)
	}

	// Scene-level override component search.
	ovCompHits, err := ed.Search("ambient_audio")
	if err != nil {
		t.Fatal(err)
	}
	if len(ovCompHits) != 1 || ovCompHits[0].Kind != editor.SearchComponent || ovCompHits[0].File != "scenes/dungeon.scene.yaml" || ovCompHits[0].ObjectID != "floor_anchor" {
		t.Fatalf("Search(ambient_audio) = %+v", ovCompHits)
	}

	// 5. Prefab reference search.
	prefabHits, err := ed.Search("prefabs/goblin_boss")
	if err != nil {
		t.Fatal(err)
	}
	if len(prefabHits) != 1 || prefabHits[0].Kind != editor.SearchPrefab || prefabHits[0].File != "scenes/dungeon.scene.yaml" || prefabHits[0].ObjectID != "goblin_king" {
		t.Fatalf("Search(prefabs/goblin_boss) = %+v", prefabHits)
	}

	// 6. Scene reference search: both object.scene instance AND scene inherits.
	instHits, err := ed.Search("scenes/trap.scene.yaml")
	if err != nil {
		t.Fatal(err)
	}
	hasTrapFile := slices.ContainsFunc(instHits, func(r editor.SearchResult) bool {
		return r.Kind == editor.SearchFile && r.File == "scenes/trap.scene.yaml"
	})
	hasTrapInst := slices.ContainsFunc(instHits, func(r editor.SearchResult) bool {
		return r.Kind == editor.SearchSceneRef && r.File == "scenes/dungeon.scene.yaml" && r.ObjectID == "trap_east"
	})
	if !hasTrapFile || !hasTrapInst {
		t.Fatalf("Search(scenes/trap.scene.yaml) = %+v, want file and instance ref", instHits)
	}

	inhHits, err := ed.Search("scenes/base.scene.yaml")
	if err != nil {
		t.Fatal(err)
	}
	hasBaseFile := slices.ContainsFunc(inhHits, func(r editor.SearchResult) bool {
		return r.Kind == editor.SearchFile && r.File == "scenes/base.scene.yaml"
	})
	hasBaseInh := slices.ContainsFunc(inhHits, func(r editor.SearchResult) bool {
		return r.Kind == editor.SearchSceneRef && r.File == "scenes/dungeon.scene.yaml" && r.Match == "scenes/base.scene.yaml"
	})
	if !hasBaseFile || !hasBaseInh {
		t.Fatalf("Search(scenes/base.scene.yaml) = %+v, want file and inherits ref", inhHits)
	}

	// 7. Multi-term query combining scene path and component type.
	multiHits, err := ed.Search("dungeon health")
	if err != nil {
		t.Fatal(err)
	}
	if len(multiHits) != 1 || multiHits[0].File != "scenes/dungeon.scene.yaml" || multiHits[0].ObjectID != "goblin_king" {
		t.Fatalf("Search(dungeon health) = %+v", multiHits)
	}

	// 8. Activating a result in the ALREADY OPEN scene preserves unsaved edits & undo history.
	if err := ed.MoveObject("goblin_king", 99, 77); err != nil {
		t.Fatal(err)
	}
	if !ed.History.CanUndo() {
		t.Fatal("expected undoable MoveObject")
	}
	wallHits, err := ed.Search("wall_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(wallHits) != 1 {
		t.Fatalf("Search(wall_1) = %+v", wallHits)
	}
	if err := ed.OpenSearchResult(wallHits[0]); err != nil {
		t.Fatal(err)
	}
	if ed.SelectedID != "wall_1" {
		t.Fatalf("SelectedID = %q, want wall_1", ed.SelectedID)
	}
	if !ed.History.CanUndo() {
		t.Fatal("activating result in already-open scene cleared undo history")
	}
	if gk, _, _ := ed.FindObject("goblin_king"); gk == nil || gk.X != 99 || gk.Y != 77 {
		t.Fatalf("unsaved MoveObject was lost: %+v", gk)
	}
}

// TestSearchAndReferencesRefreshAfterEditsSavesMovesAndExternalChanges verifies
// that search results and Find References update cleanly on live scene edits,
// saves, file moves (including Undo), and external disk changes / RefreshSearch.
func TestSearchAndReferencesRefreshAfterEditsSavesMovesAndExternalChanges(t *testing.T) {
	dir := writeSearchFixture(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Live edit in open scene (room.scene.yaml) updates active search query immediately, even before saving.
	if err := ed.SetSearchQuery("secret_chest"); err != nil {
		t.Fatal(err)
	}
	if len(ed.SearchResults()) != 0 {
		t.Fatalf("expected 0 results before placing object, got %+v", ed.SearchResults())
	}
	if _, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{
		ID:       "secret_chest",
		Name:     "Golden Chest",
		Material: "materials/stone.material.yaml",
		Components: map[string]any{
			"loot_table": map[string]any{"tier": 3},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if got := ed.SearchResults(); len(got) != 1 || got[0].ObjectID != "secret_chest" || got[0].File != "scenes/room.scene.yaml" {
		t.Fatalf("active search results after PlaceObject = %+v, want secret_chest", got)
	}
	// Undo removes the live object from search results; Redo brings it back.
	if !ed.Undo() {
		t.Fatal("Undo failed")
	}
	if got := ed.SearchResults(); len(got) != 0 {
		t.Fatalf("active search results after Undo = %+v, want empty", got)
	}
	if !ed.Redo() {
		t.Fatal("Redo failed")
	}
	if got := ed.SearchResults(); len(got) != 1 || got[0].ObjectID != "secret_chest" {
		t.Fatalf("active search results after Redo = %+v, want secret_chest", got)
	}

	// Start tracking Find References on materials/stone.material.yaml (3 refs on disk before saving).
	beforeSaveRefs, err := ed.ShowReferences("materials/stone.material.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeSaveRefs) != 3 {
		t.Fatalf("refs before SaveFile = %d, want 3", len(beforeSaveRefs))
	}

	// Saving room.scene.yaml writes secret_chest's material reference to disk and updates active Find References to 4!
	if err := ed.SaveFile(ed.FilePath); err != nil {
		t.Fatal(err)
	}
	if afterSaveRefs := ed.References(); len(afterSaveRefs) != 4 {
		t.Fatalf("active References() after SaveFile = %d (%+v), want 4", len(afterSaveRefs), afterSaveRefs)
	}

	// Moving the material file updates ReferencesTarget, References, and Search results; Undo restores them.
	if err := ed.MoveFile("materials/stone.material.yaml", "materials/granite.material.yaml"); err != nil {
		t.Fatal(err)
	}
	if ed.ReferencesTarget() != "materials/granite.material.yaml" {
		t.Fatalf("ReferencesTarget after MoveFile = %q, want materials/granite.material.yaml", ed.ReferencesTarget())
	}
	if got := ed.References(); len(got) != 4 || got[0].Target != "materials/granite.material.yaml" {
		t.Fatalf("References after MoveFile = %+v, want 4 granite refs", got)
	}
	if hits, err := ed.Search("granite.material"); err != nil || len(hits) != 1 || hits[0].File != "materials/granite.material.yaml" {
		t.Fatalf("Search(granite.material) after MoveFile = %+v, %v", hits, err)
	}
	if hits, err := ed.Search("stone.material"); err != nil || len(hits) != 0 {
		t.Fatalf("Search(stone.material) after MoveFile = %+v, %v, want empty", hits, err)
	}

	if !ed.Undo() {
		t.Fatal("Undo MoveFile failed")
	}
	if ed.ReferencesTarget() != "materials/stone.material.yaml" || len(ed.References()) != 4 {
		t.Fatalf("after Undo MoveFile: target=%q refs=%d", ed.ReferencesTarget(), len(ed.References()))
	}

	// External edit on disk: add a new scene file referencing stone.material.yaml with a new object.
	bonusScene := `version: 2
uid: scn0000000099
name: Secret Vault
layers:
  - name: actors
    objects:
      - id: vault_keeper
        name: Vault Keeper
        material: materials/stone.material.yaml
`
	writeFiles(t, dir, map[string]string{"scenes/vault.scene.yaml": bonusScene})

	// Automatic fingerprint detection finds the new scene and object immediately.
	vaultHits, err := ed.Search("vault_keeper")
	if err != nil {
		t.Fatal(err)
	}
	if len(vaultHits) != 1 || vaultHits[0].File != "scenes/vault.scene.yaml" || vaultHits[0].ObjectID != "vault_keeper" {
		t.Fatalf("Search(vault_keeper) after external write = %+v", vaultHits)
	}

	// Explicit RefreshSearch updates the active References() list to 5 (including vault.scene.yaml).
	if err := ed.RefreshSearch(); err != nil {
		t.Fatal(err)
	}
	if got := ed.References(); len(got) != 5 {
		t.Fatalf("References() after external scene + RefreshSearch = %d (%+v), want 5", len(got), got)
	}

	// Removing the external file and refreshing removes it from search and references.
	if err := os.Remove(filepath.Join(dir, "scenes", "vault.scene.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := ed.RefreshSearch(); err != nil {
		t.Fatal(err)
	}
	if hits, err := ed.Search("vault_keeper"); err != nil || len(hits) != 0 {
		t.Fatalf("Search(vault_keeper) after removal = %+v, %v, want empty", hits, err)
	}
	if got := ed.References(); len(got) != 4 {
		t.Fatalf("References() after removal = %d, want 4", len(got))
	}
}
