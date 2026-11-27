package refs_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	projpkg "github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/refs"
)

const testPackSHA = "0000000000000000000000000000000000000000000000000000000000000000"

const packGameYAML = `version: 1
name: Dungeon
id: dev.example.dungeon
appVersion: 0.1.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level
  scenes:
    - name: level
      path: packs/tiles/level.scene.yaml
packs:
  # Installed tiles pack.
  - name: tiles
    kind: asset
    packVersion: 1.0.0
    sha256: ` + testPackSHA + `
    dir: "packs/tiles" # keep quotes and comment
    files:
      - level.scene.yaml
      - pack.yaml
      - sub/floor.png
      - tiles.png
    scenes: [level]
`

func packProjectFS() fstest.MapFS {
	return fstest.MapFS{
		"game.yaml": {Data: []byte(packGameYAML)},
		"packs/tiles/level.scene.yaml": {Data: []byte(`version: 2
uid: scene00000001
layers:
  - name: ground
    objects:
      - id: floor
        material: materials/floor.material.yaml
`)},
		"packs/tiles/pack.yaml":     {Data: []byte("version: 1\nname: tiles\n")},
		"packs/tiles/sub/floor.png": {Data: []byte("floor")},
		"packs/tiles/tiles.png":     {Data: []byte("tiles")},
		"materials/floor.material.yaml": {Data: []byte(`type: material
version: 1
uid: mat0000000001
name: Floor
texture: packs/tiles/sub/floor.png
`)},
	}
}

func TestPlanMovePackFolderDirect(t *testing.T) {
	dir := dirFS(t, packProjectFS())

	// Plan without applying (dry-run inspection).
	pl, err := refs.PlanMove(os.DirFS(dir), "packs/tiles", "art/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pl.Moves, []refs.Move{{From: "packs/tiles", To: "art/tiles"}}) {
		t.Fatalf("Moves = %+v", pl.Moves)
	}
	var updatedFields []string
	for _, r := range pl.Updated {
		updatedFields = append(updatedFields, r.File+":"+r.Field+":"+r.Value)
	}
	wantUpdated := []string{
		"game.yaml:autoload scene:packs/tiles/level.scene.yaml",
		"game.yaml:pack dir:packs/tiles",
		"materials/floor.material.yaml:texture:packs/tiles/sub/floor.png",
	}
	if !slices.Equal(updatedFields, wantUpdated) {
		t.Fatalf("Updated = %v, want %v", updatedFields, wantUpdated)
	}
	// Dry-run plan did not mutate files on disk.
	if got := read(t, dir, "game.yaml"); got != packGameYAML {
		t.Fatalf("PlanMove mutated game.yaml:\n%s", got)
	}

	if err := pl.Apply(dir); err != nil {
		t.Fatal(err)
	}

	// game.yaml is rewritten byte-for-byte in place, keeping quotes and comments.
	wantGame := strings.NewReplacer(
		"path: packs/tiles/level.scene.yaml", "path: art/tiles/level.scene.yaml",
		`dir: "packs/tiles"`, `dir: "art/tiles"`,
	).Replace(packGameYAML)
	if got := read(t, dir, "game.yaml"); got != wantGame {
		t.Fatalf("game.yaml:\n%s\nwant:\n%s", got, wantGame)
	}
	if got := read(t, dir, "materials/floor.material.yaml"); !strings.Contains(got, "texture: art/tiles/sub/floor.png\n") {
		t.Fatalf("material:\n%s", got)
	}
	// Index updates the moved files and never indexes the pack folder itself.
	ix := read(t, dir, refs.IndexFileName)
	if !strings.Contains(ix, "path: art/tiles/level.scene.yaml\n") || !strings.Contains(ix, "path: art/tiles/sub/floor.png\n") {
		t.Fatalf("index missing moved pack files:\n%s", ix)
	}
	if strings.Contains(ix, "path: art/tiles\n") || strings.Contains(ix, "packs/tiles") {
		t.Fatalf("index has stale or folder entry:\n%s", ix)
	}
	clean(t, dir)
	if _, err := projpkg.Check(os.DirFS(dir)); err != nil {
		t.Fatalf("project.Check after pack folder move: %v", err)
	}
}

func TestPlanMoveParentDirContainingPacks(t *testing.T) {
	f := &projpkg.File{
		Version:    projpkg.CurrentVersion,
		Name:       "MultiPack",
		ID:         "dev.example.multipack",
		AppVersion: "0.1.0",
		Window:     projpkg.Window{Width: 640, Height: 360},
		Packs: []projpkg.Pack{
			{
				Name:        "tiles",
				Kind:        projpkg.PackAsset,
				PackVersion: "1.0.0",
				SHA256:      testPackSHA,
				Dir:         "packs/tiles",
				Files:       []string{"pack.yaml", "tiles.png"},
			},
			{
				Name:        "sfx",
				Kind:        projpkg.PackAsset,
				PackVersion: "1.2.0",
				SHA256:      strings.Repeat("1", 64),
				Dir:         "packs/audio/sfx",
				Files:       []string{"jump.wav", "pack.yaml"},
			},
		},
	}
	canonicalBefore, err := projpkg.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	dir := dirFS(t, fstest.MapFS{
		"game.yaml":                 {Data: canonicalBefore},
		"packs/tiles/pack.yaml":     {Data: []byte("version: 1\n")},
		"packs/tiles/tiles.png":     {Data: []byte("tiles")},
		"packs/audio/sfx/pack.yaml": {Data: []byte("version: 1\n")},
		"packs/audio/sfx/jump.wav":  {Data: []byte("wav")},
	})

	pl := move(t, dir, "packs", "vendor/packs")
	if len(pl.Updated) != 2 {
		t.Fatalf("Updated = %+v, want 2 pack dir refs", pl.Updated)
	}
	for _, r := range pl.Updated {
		if r.Field != projpkg.FieldPackDir {
			t.Fatalf("Updated field = %q, want %q", r.Field, projpkg.FieldPackDir)
		}
	}

	gotBytes, err := os.ReadFile(filepath.Join(dir, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := projpkg.Parse(gotBytes)
	if err != nil {
		t.Fatalf("Parse rewritten game.yaml: %v", err)
	}
	if parsed.Packs[0].Dir != "vendor/packs/tiles" || parsed.Packs[1].Dir != "vendor/packs/audio/sfx" {
		t.Fatalf("packs dirs = %q, %q", parsed.Packs[0].Dir, parsed.Packs[1].Dir)
	}
	// Canonical round-trip: rewritten canonical game.yaml matches project.Encode byte for byte.
	reencoded, err := projpkg.Encode(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBytes, reencoded) {
		t.Fatalf("rewritten game.yaml is not canonical:\n%s\nwant:\n%s", gotBytes, reencoded)
	}
	clean(t, dir)
	if _, err := projpkg.Check(os.DirFS(dir)); err != nil {
		t.Fatalf("project.Check after parent move: %v", err)
	}
}

func TestPlanMoveRefusesMovingTrackedPackFileOrSubfolder(t *testing.T) {
	fsys := packProjectFS()
	fsys["packs/tiles/notes.txt"] = &fstest.MapFile{Data: []byte("user note")}

	for _, tc := range []struct {
		name, from, to, want string
	}{
		{
			name: "file out of pack",
			from: "packs/tiles/tiles.png",
			to:   "art/tiles.png",
			want: `belongs to installed pack "tiles" in game.yaml; move the pack folder packs/tiles instead`,
		},
		{
			name: "rename file inside pack",
			from: "packs/tiles/tiles.png",
			to:   "packs/tiles/ground.png",
			want: `belongs to installed pack "tiles" in game.yaml; move the pack folder packs/tiles instead`,
		},
		{
			name: "subfolder with tracked files out of pack",
			from: "packs/tiles/sub",
			to:   "art/sub",
			want: `it holds packs/tiles/sub/floor.png, which belongs to installed pack "tiles" in game.yaml; move the pack folder packs/tiles instead`,
		},
		{
			name: "subfolder with tracked files inside pack",
			from: "packs/tiles/sub",
			to:   "packs/tiles/nested",
			want: `it holds packs/tiles/sub/floor.png, which belongs to installed pack "tiles" in game.yaml; move the pack folder packs/tiles instead`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := refs.PlanMove(fsys, tc.from, tc.to)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PlanMove(%q, %q) = %v, want error containing %q", tc.from, tc.to, err, tc.want)
			}
		})
	}

	// Moving an untracked file inside a pack folder is allowed and does not touch game.yaml.
	dir := dirFS(t, fsys)
	pl := move(t, dir, "packs/tiles/notes.txt", "docs/notes.txt")
	if len(pl.Updated) != 0 {
		t.Fatalf("moving untracked file updated refs: %+v", pl.Updated)
	}
	if got := read(t, dir, "game.yaml"); got != packGameYAML {
		t.Fatalf("game.yaml changed when moving untracked file:\n%s", got)
	}
	clean(t, dir)
	if _, err := projpkg.Check(os.DirFS(dir)); err != nil {
		t.Fatalf("project.Check: %v", err)
	}
}

func TestScanAndCheckReportMissingPackDir(t *testing.T) {
	fsys := fstest.MapFS{
		"game.yaml": {Data: []byte(gameHead + `packs:
  - name: tiles
    kind: asset
    packVersion: 1.0.0
    sha256: ` + testPackSHA + `
    dir: packs/tiles
    files:
      - tiles.png
`)},
	}
	p := scan(t, fsys)
	wantRefs := []string{`game.yaml:11 pack dir "packs/tiles" -> packs/tiles`}
	if got := refList(p); !slices.Equal(got, wantRefs) {
		t.Fatalf("refs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(wantRefs, "\n"))
	}
	wantProbs := []string{`game.yaml:11: pack dir "packs/tiles": not found`}
	if got := checkList(p); !slices.Equal(got, wantProbs) {
		t.Fatalf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(wantProbs, "\n"))
	}
}

func TestPlanMoveRefusesPackClaimCollision(t *testing.T) {
	// Pack b claims "sub/x.png" under "packs/b" (file missing on disk so dest does not exist yet);
	// moving pack a ("packs/a" with "x.png") to "packs/b/sub" would make both packs claim packs/b/sub/x.png.
	fsys := fstest.MapFS{
		"game.yaml": {Data: []byte(gameHead + `packs:
  - name: alpha
    kind: asset
    packVersion: 1.0.0
    sha256: ` + testPackSHA + `
    dir: packs/alpha
    files:
      - x.png
  - name: beta
    kind: asset
    packVersion: 1.0.0
    sha256: ` + strings.Repeat("1", 64) + `
    dir: packs/beta
    files:
      - sub/x.png
`)},
		"packs/alpha/x.png": {Data: []byte("x")},
	}
	_, err := refs.PlanMove(fsys, "packs/alpha", "packs/beta/sub")
	if err == nil || !strings.Contains(err.Error(), "both claim") {
		t.Fatalf("PlanMove colliding packs = %v, want claim collision error", err)
	}
}

func TestPlanMovePackInSubprojectRoot(t *testing.T) {
	fsys := fstest.MapFS{
		"sub/game.yaml": {Data: []byte(gameHead + `packs:
  - name: tiles
    kind: asset
    packVersion: 1.0.0
    sha256: ` + testPackSHA + `
    dir: packs/tiles
    files:
      - pack.yaml
      - tiles.png
`)},
		"sub/packs/tiles/pack.yaml": {Data: []byte("version: 1\n")},
		"sub/packs/tiles/tiles.png": {Data: []byte("tiles")},
	}
	if _, err := refs.PlanMove(fsys, "sub/packs/tiles/tiles.png", "sub/art/tiles.png"); err == nil || !strings.Contains(err.Error(), `installed pack "tiles" in sub/game.yaml`) {
		t.Fatalf("subproject tracked file move = %v", err)
	}
	dir := dirFS(t, fsys)
	move(t, dir, "sub/packs/tiles", "sub/art/tiles")
	if got := read(t, dir, "sub/game.yaml"); !strings.Contains(got, "dir: art/tiles\n") {
		t.Fatalf("sub/game.yaml:\n%s", got)
	}
	clean(t, dir)
}
