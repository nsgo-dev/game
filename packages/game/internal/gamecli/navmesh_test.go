package gamecli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/navbake3d"
)

// navGameYAML declares two agent types for game navmesh.
const navGameYAML = assetsProjectHead + `navigation:
  cellSize: 0.1
  cellHeight: 0.05
  agents:
    - name: humanoid
      radius: 0.3
      height: 1.5
    - name: rat
      radius: 0.1
      height: 0.3
      maxClimb: 0.1
`

// navScene is a 10 by 10 floor with a pillar in the middle, both the unit box model scaled, naming its navigation mesh.
const navScene = `version: 2
navMesh: levels/room.navmesh.yaml
layers:
  - name: level
    objects:
      - id: floor
        x: 5
        "y": -0.1
        posZ: 5
        scaleX: 10
        scaleY: 0.2
        scaleZ: 10
        model: models/box.gltf
      - id: pillar
        x: 5
        "y": 1
        posZ: 5
        scaleX: 1.5
        scaleY: 2
        scaleZ: 1.5
        model: models/box.gltf
`

// newNavGame writes a project with gameYAML, the box model and navScene as scenes/room.scene.yaml, and returns its directory.
func newNavGame(t *testing.T, gameYAML, scene string) string {
	t.Helper()
	dir := newGameDir(t, gameYAML)
	model, err := os.ReadFile(filepath.Join("..", "..", "navbake3d", "testdata", "level", "box.gltf"))
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, map[string]string{"models/box.gltf": string(model), "scenes/room.scene.yaml": scene})
	return dir
}

func TestNavmeshBakesEveryAgentType(t *testing.T) {
	dir := newNavGame(t, navGameYAML, navScene)
	scene := filepath.Join(dir, "scenes", "room.scene.yaml")
	out := filepath.Join(dir, "levels", "room.navmesh.yaml")
	bake := func() []byte {
		a, stdout, stderr := newTestApp(t)
		if code := run([]string{"navmesh", "-in", dir, scene}, a); code != 0 {
			t.Fatalf("exit %d (stderr: %s)", code, stderr)
		}
		if !strings.HasPrefix(stdout.String(), out+": humanoid ") || !strings.Contains(stdout.String(), ", rat ") {
			t.Fatalf("stdout = %q", stdout)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := bake()
	if second := bake(); !bytes.Equal(first, second) {
		t.Fatal("baking the scene twice wrote different files")
	}
	f, err := navbake3d.Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	if f.Settings.CellSize != 0.1 || len(f.Meshes) != 2 || f.Meshes[0].Agent.Name != "humanoid" || f.Meshes[1].Agent.MaxClimb != 0.1 {
		t.Fatalf("baked settings %+v and meshes for %d agents", f.Settings, len(f.Meshes))
	}
	m, _ := f.Mesh("humanoid")
	nm := m.NavMesh()
	path, ok := nm.FindPath(math3d.Vec3{X: 2, Z: 5}, math3d.Vec3{X: 8, Z: 5})
	if !ok {
		t.Fatal("no path across the room")
	}
	for _, p := range path {
		if p.X > 3.9 && p.X < 6.1 && p.Z > 3.9 && p.Z < 6.1 {
			t.Fatalf("path %v passes through the pillar", path)
		}
	}
}

func TestNavmeshOutFlagAndDefaults(t *testing.T) {
	// Without a navigation section, one default agent type is baked with the default settings.
	dir := newNavGame(t, assetsProjectHead, strings.Replace(navScene, "navMesh: levels/room.navmesh.yaml\n", "", 1))
	out := filepath.Join(dir, "baked", "room.yaml")
	a, _, stderr := newTestApp(t)
	if code := run([]string{"navmesh", "-in", dir, "-out", out, filepath.Join(dir, "scenes", "room.scene.yaml")}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	f, err := navbake3d.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Meshes) != 1 || f.Meshes[0].Agent.Name != "default" || f.Settings.CellSize != navbake3d.DefaultCellSize {
		t.Fatalf("baked %+v for %d agents", f.Settings, len(f.Meshes))
	}
}

func TestNavmeshErrors(t *testing.T) {
	dir := newNavGame(t, navGameYAML, strings.Replace(navScene, "navMesh: levels/room.navmesh.yaml\n", "", 1))
	scene := filepath.Join(dir, "scenes", "room.scene.yaml")
	writeFiles(t, dir, map[string]string{"scenes/empty.scene.yaml": "version: 1\nnavMesh: x.navmesh.yaml\n"})
	bad := newNavGame(t, assetsProjectHead+"navigation: {cellSize: -1}\n", navScene)
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"navmesh", "-in", dir}, 2, "expected one scene file"},
		{[]string{"navmesh", "-in", dir, scene}, 2, "names no navMesh file"},
		{[]string{"navmesh", "-in", dir, filepath.Join(dir, "scenes", "empty.scene.yaml")}, 1, "no level geometry"},
		{[]string{"navmesh", "-in", dir, filepath.Join(t.TempDir(), "elsewhere.scene.yaml")}, 1, "not inside the project directory"},
		{[]string{"navmesh", "-in", bad, filepath.Join(bad, "scenes", "room.scene.yaml")}, 1, "cellSize"},
	}
	for _, c := range cases {
		a, _, stderr := newTestApp(t)
		if code := run(c.args, a); code != c.code || !strings.Contains(stderr.String(), c.want) {
			t.Errorf("%q: exit %d, stderr %q; want %d mentioning %q", c.args, code, stderr, c.code, c.want)
		}
	}
}
