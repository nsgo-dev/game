package main

import (
	"bytes"
	_ "embed"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

//go:embed nsgo.yaml
var nsgoYAML []byte

const step = time.Second / StepRate

// TestProjectFileIsValidAndCanonical checks that game.yaml is in the form gameproject fmt writes, that nsgo.yaml agrees with it, that its autoloads load and that it turns on the health plugin.
func TestProjectFileIsValidAndCanonical(t *testing.T) {
	fsys := fstest.MapFS{project.FileName: {Data: gameYAML}, project.NSGoFileName: {Data: nsgoYAML}}
	scenes, err := fs.Glob(sceneFiles, "scenes/*.scene.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range scenes {
		data, _ := fs.ReadFile(sceneFiles, name)
		fsys[name] = &fstest.MapFile{Data: data}
	}
	if _, err := project.Check(fsys); err != nil {
		t.Fatal(err)
	}
	if canonical, err := project.Encode(gameProject); err != nil || !bytes.Equal(canonical, gameYAML) {
		t.Fatalf("game.yaml is not canonical (%v); run gameproject fmt:\n%s", err, canonical)
	}
	if gameProject.Editor == nil || len(gameProject.Editor.Plugins) != 1 || gameProject.Editor.Plugins[0] != "health" {
		t.Fatalf("game.yaml does not turn on the health plugin: %+v", gameProject.Editor)
	}
}

// TestSceneFilesAreCanonical checks that every scene round-trips unchanged through load and save, as the editor saves it.
func TestSceneFilesAreCanonical(t *testing.T) {
	scenes, _ := fs.Glob(sceneFiles, "scenes/*.scene.yaml")
	if len(scenes) == 0 {
		t.Fatal("no scene files")
	}
	for _, name := range scenes {
		data, _ := fs.ReadFile(sceneFiles, name)
		doc, err := sceneformat.Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out, err := sceneformat.Encode(doc); err != nil || !bytes.Equal(out, data) {
			t.Fatalf("%s is not canonical (%v):\n%s", name, err, out)
		}
	}
}

func mainWard(t *testing.T) *Ward {
	t.Helper()
	w, err := loadMainWard()
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestLoadWardReadsPatientsWithDefaults(t *testing.T) {
	w := mainWard(t)
	want := []Patient{
		{ID: "ada", Name: "Ada", X: 64, Y: 70, HP: 8, Max: 10},
		{ID: "bram", Name: "Bram", X: 128, Y: 110, HP: 5, Max: 12},
		{ID: "cleo", Name: "Cleo", X: 192, Y: 70, HP: 9, Max: 10},
		{ID: "dov", Name: "Dov", X: 256, Y: 110, HP: 4, Max: 8},
	}
	if len(w.Patients) != len(want) {
		t.Fatalf("patients = %+v", w.Patients)
	}
	for i := range want {
		if w.Patients[i] != want[i] {
			t.Errorf("patient %d = %+v, want %+v", i, w.Patients[i], want[i])
		}
	}
	if w.Width != 320 || w.Height != 180 || w.Background != core.RGB(0x10141F) {
		t.Fatalf("scene = %vx%v %v", w.Width, w.Height, w.Background)
	}
}

func TestLoadWardRefusesBadScenes(t *testing.T) {
	for name, scene := range map[string]string{
		"no patients":       "version: 1\nvirtualWidth: 320\nvirtualHeight: 180\nlayers:\n  - name: a\n    objects:\n      - id: rock\n",
		"no size":           "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: p\n        components:\n          health: {}\n",
		"unknown component": "version: 1\nvirtualWidth: 320\nvirtualHeight: 180\nlayers:\n  - name: a\n    objects:\n      - id: p\n        components:\n          armor: {}\n",
	} {
		if _, err := LoadWard([]byte(scene), "bad.yaml"); err == nil {
			t.Errorf("%s: LoadWard accepted the scene", name)
		}
	}
}

func TestPatientsBleedStaggeredUntilEveryOneIsLost(t *testing.T) {
	w := mainWard(t)
	// Patient i first bleeds on step BleedSteps - i*BleedSteps/4, so the last patient bleeds first, on step 23.
	for range BleedSteps - 3*BleedSteps/4 {
		w.Step(step)
	}
	if got := w.Patients[3].HP; got != 3 {
		t.Fatalf("dov after her first bleed = %d hp, want 3", got)
	}
	if got := w.Patients[0].HP; got != 8 {
		t.Fatalf("ada before her first bleed = %d hp, want 8", got)
	}
	for range 3 * BleedSteps / 4 {
		w.Step(step)
	}
	for i, want := range []int{7, 4, 8, 3} {
		if got := w.Patients[i].HP; got != want {
			t.Fatalf("patient %d after %d steps = %d hp, want %d", i, BleedSteps, got, want)
		}
	}

	for !w.Over() {
		w.Step(step)
		if w.Steps > 100*BleedSteps {
			t.Fatal("the ward never ended")
		}
	}
	// Cleo, at 9 hp and first bleeding on step 45, is the last lost, on her ninth bleed.
	if want := BleedSteps - 2*BleedSteps/4 + 8*BleedSteps; w.Steps != want {
		t.Fatalf("ward over after %d steps, want %d", w.Steps, want)
	}
	if w.LostCount() != 4 || w.Survived() != 12750*time.Millisecond {
		t.Fatalf("lost %d, survived %v", w.LostCount(), w.Survived())
	}
	steps := w.Steps
	w.Step(step)
	if w.Steps != steps {
		t.Fatal("an over ward kept stepping")
	}
	if w.HealAt(64, 70) {
		t.Fatal("a lost patient was healed")
	}

	w.Reset()
	if w.Steps != 0 || w.Over() || w.Patients[0].HP != 8 {
		t.Fatalf("Reset left %+v", w)
	}
}

func TestHealAtHealsThePressedPatientUpToMax(t *testing.T) {
	w := mainWard(t)
	if !w.HealAt(128+PatientSize/2, 110-PatientSize/2) {
		t.Fatal("a press on Bram's corner did not heal him")
	}
	if got := w.Patients[1].HP; got != 5+HealAmount {
		t.Fatalf("bram = %d hp, want %d", got, 5+HealAmount)
	}
	if !w.HealAt(64, 70) || w.Patients[0].HP != 10 {
		t.Fatalf("ada = %d hp, want her max 10", w.Patients[0].HP)
	}
	if w.HealAt(10, 10) {
		t.Fatal("a press beside every patient healed one")
	}
	if got := w.PatientAt(128+PatientSize/2+1, 110); got != -1 {
		t.Fatalf("PatientAt just outside Bram = %d", got)
	}
}

func TestPatientColorFollowsHitPoints(t *testing.T) {
	for _, c := range []struct {
		hp   int
		want core.Color
	}{{10, colorWell}, {6, colorWell}, {5, colorHurt}, {3, colorHurt}, {2, colorCritical}, {1, colorCritical}, {0, colorLost}} {
		if got := PatientColor(Patient{HP: c.hp, Max: 10}); got != c.want {
			t.Errorf("PatientColor(%d/10) = %v, want %v", c.hp, got, c.want)
		}
	}
}

func TestFitLetterboxesTheScene(t *testing.T) {
	w := mainWard(t)
	scale, ox, oy := w.Fit(core.Size{Width: 960, Height: 600})
	if scale != 3 || ox != 0 || oy != 30 {
		t.Fatalf("Fit = %v, %v, %v; want 3, 0, 30", scale, ox, oy)
	}
	if x, y := w.ToScene(core.Size{Width: 960, Height: 600}, core.Point{X: 192, Y: 240}); x != 64 || y != 70 {
		t.Fatalf("ToScene = %v, %v; want 64, 70", x, y)
	}
	if scale, _, _ := w.Fit(core.Size{}); scale != 0 {
		t.Fatalf("Fit of an empty canvas = %v", scale)
	}
}

func TestRootTitlesTheWindowAndShowsTheHUD(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(960, 540))
	if app.Title != gameProject.Title() {
		t.Fatalf("window title = %q, want %q", app.Title, gameProject.Title())
	}
	if !app.Exists("id:ward-canvas") || app.Find("id:hud").Text != "0 s  lost 0 of 4" {
		t.Fatalf("hud = %q", app.Find("id:hud").Text)
	}
}

func TestViewHealsOnPressAndRestarts(t *testing.T) {
	w := mainWard(t)
	var c *Controller
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		c, node = NewView(cx, w)
		c.Loop.Paused = true
		return node
	}, nsgotest.Size(960, 540))

	// The 960x540 canvas fits the 320x180 ward at 3x, so Bram's center (128, 110) is at (384, 330).
	app.PressAt("id:ward-canvas", core.Point{X: 384, Y: 330})
	if got := w.Patients[1].HP; got != 5+HealAmount {
		t.Fatalf("bram after a press = %d hp, want %d", got, 5+HealAmount)
	}

	for i := range w.Patients {
		w.Patients[i].HP = 0
	}
	app.Post(c.redraw)
	app.Settle()
	if !app.Exists("id:game-over") {
		t.Fatal("no game over screen once every patient is lost")
	}
	app.Press("id:restart")
	if app.Exists("id:game-over") || w.Over() || w.Patients[1].HP != 5 {
		t.Fatalf("Restart left the ward over: %+v", w.Patients)
	}
	if got := app.Find("id:hud").Text; got != "0 s  lost 0 of 4" {
		t.Fatalf("hud after Restart = %q", got)
	}
}
