package assetcheck_test

import (
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
)

// connectionsRegistry declares a button that is pressed and a door that opens at a speed.
func connectionsRegistry(t testing.TB) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{Name: "button", Events: []sceneformat.Event{{Name: "pressed"}}}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(sceneformat.ComponentType{
		Name:     "door",
		Handlers: []sceneformat.Handler{{Name: "open", Params: []sceneformat.Field{{Name: "speed", Kind: sceneformat.FieldFloat, Default: 1.0, Min: sceneformat.Float64(0)}}}},
	}); err != nil {
		t.Fatal(err)
	}
	// switch declares pressed too, so an object with a button and a switch must name the component of its event.
	if err := reg.Register(sceneformat.ComponentType{Name: "switch", Events: []sceneformat.Event{{Name: "pressed"}}}); err != nil {
		t.Fatal(err)
	}
	return reg
}

const (
	// pairScene connects its own button to its own door.
	pairScene = `version: 2
layers:
  - name: actors
    objects:
      - id: button
        components:
          button: {}
      - id: door
        components:
          door: {}
connections:
  - from: button
    event: pressed
    to: door
    handler: open
`
	// levelScene instances pairScene and connects a lever of its own to the instanced door.
	levelScene = `version: 2
layers:
  - name: actors
    objects:
      - id: west
        scene: scenes/pair.scene.yaml
      - id: lever
        components:
          button: {}
connections:
  - from: lever
    event: pressed
    to: west/door
    handler: open
    args:
      speed: 2
`
	// derivedScene inherits pairScene and connects a lever of its own to the inherited door.
	derivedScene = `version: 2
inherits: scenes/pair.scene.yaml
layers:
  - name: actors
    objects:
      - id: lever
        components:
          button: {}
connections:
  - from: lever
    event: pressed
    to: door
    handler: open
`
)

func connectionsProject() fstest.MapFS {
	return fstest.MapFS{
		"nsgo.yaml":                 &fstest.MapFile{Data: []byte("name: test\n")},
		"scenes/pair.scene.yaml":    &fstest.MapFile{Data: []byte(pairScene)},
		"scenes/level.scene.yaml":   &fstest.MapFile{Data: []byte(levelScene)},
		"scenes/derived.scene.yaml": &fstest.MapFile{Data: []byte(derivedScene)},
	}
}

func TestConnectedScenesThatResolvePass(t *testing.T) {
	fsys := connectionsProject()
	for _, opts := range []assetcheck.Options{{}, {Components: connectionsRegistry(t)}} {
		r, err := assetcheck.CheckWith(fsys, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !r.OK() || r.Scenes != 3 {
			t.Fatalf("components %v: %d scenes, problems:\n%s", opts.Components != nil, r.Scenes, problems(r))
		}
	}
}

// Without the game's component types, a connection to an object that its scene, its instanced scenes and its base do not hold fails at the line that names it.
func TestCheckReportsConnectionsToMissingObjects(t *testing.T) {
	fsys := connectionsProject()
	badLevel := strings.Replace(levelScene, "to: west/door", "to: west/ghost", 1)
	badDerived := strings.Replace(derivedScene, "from: lever", "from: crank", 1)
	fsys["scenes/level.scene.yaml"].Data = []byte(badLevel)
	fsys["scenes/derived.scene.yaml"].Data = []byte(badDerived)
	r, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	want := []assetcheck.Problem{
		{File: "scenes/derived.scene.yaml", Line: lineOf(t, badDerived, "from: crank"), Message: `sceneformat: connection 0 ("crank" pressed -> "door" open): the scene has no object "crank", counting those of the scenes it instances and inherits`},
		{File: "scenes/level.scene.yaml", Line: lineOf(t, badLevel, "to: west/ghost"), Message: `sceneformat: connection 0 ("lever" pressed -> "west/ghost" open): the scene has no object "west/ghost", counting those of the scenes it instances and inherits`},
	}
	if got := r.Problems; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("problems:\n%s\nwant:\n%v\n%v", problems(r), want[0], want[1])
	}
}

// Given the game's component types, a connection naming an event, handler or argument its objects do not declare fails at the line that names it, in a scene of its own objects and in one that instances or inherits them.
func TestCheckWithComponentsReportsMissingEventsAndHandlers(t *testing.T) {
	fsys := connectionsProject()
	badPair := strings.Replace(pairScene, "handler: open", "handler: slam", 1)
	fsys["scenes/own.scene.yaml"] = &fstest.MapFile{Data: []byte(badPair)}
	badLevel := strings.Replace(levelScene, "event: pressed", "event: held", 1)
	fsys["scenes/level.scene.yaml"].Data = []byte(badLevel)
	badDerived := strings.Replace(derivedScene, "handler: open\n", "handler: open\n    args:\n      speed: -1\n", 1)
	fsys["scenes/derived.scene.yaml"].Data = []byte(badDerived)
	r, err := assetcheck.CheckWith(fsys, assetcheck.Options{Components: connectionsRegistry(t)})
	if err != nil {
		t.Fatal(err)
	}
	got := problems(r)
	for _, want := range []string{
		"scenes/derived.scene.yaml:" + strconv.Itoa(lineOf(t, badDerived, "speed: -1")) + `: sceneformat: connection 0 ("lever" pressed -> "door" open) argument "speed": `,
		"scenes/level.scene.yaml:" + strconv.Itoa(lineOf(t, badLevel, "event: held")) + `: sceneformat: connection 0 ("lever" held -> "west/door" open): no registered component of object "lever" declares event "held" (it has: button.pressed)`,
		"scenes/own.scene.yaml:" + strconv.Itoa(lineOf(t, badPair, "handler: slam")) + `: sceneformat: connection 0 ("button" pressed -> "door" slam): no registered component of object "door" declares handler "slam" (it has: door.open)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems do not include %q:\n%s", want, got)
		}
	}
	if len(r.Problems) != 3 {
		t.Fatalf("%d problems, want 3:\n%s", len(r.Problems), got)
	}

	// The command line's check, without the types, passes the same files' events and handlers.
	r, err = assetcheck.Check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK() {
		t.Fatalf("Check without components reported:\n%s", problems(r))
	}
}

// A component the registry does not know fails at its line too.
func TestCheckWithComponentsReportsComponents(t *testing.T) {
	fsys := connectionsProject()
	bad := strings.Replace(levelScene, "button: {}", "buton: {}", 1)
	fsys["scenes/level.scene.yaml"].Data = []byte(bad)
	r, err := assetcheck.CheckWith(fsys, assetcheck.Options{Components: connectionsRegistry(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Problems) != 1 || r.Problems[0].File != "scenes/level.scene.yaml" || r.Problems[0].Line != lineOf(t, bad, "buton") || !strings.HasPrefix(r.Problems[0].Message, `sceneformat: object "lever" component "buton"`) {
		t.Fatalf("problems:\n%s", problems(r))
	}
}

// A scene that a connected scene instances but that is missing or broken is reported once, on its reference or its own file, not again as a scene that does not resolve.
func TestBrokenInstancedSceneIsReportedOnce(t *testing.T) {
	fsys := connectionsProject()
	delete(fsys, "scenes/derived.scene.yaml")
	fsys["scenes/pair.scene.yaml"].Data = []byte("version: 2\nlayers: [\n")
	r, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range r.Problems {
		if p.File == "scenes/level.scene.yaml" {
			t.Fatalf("the instancing scene was blamed:\n%s", problems(r))
		}
	}
	if len(r.Problems) == 0 {
		t.Fatal("the broken scene was not reported")
	}

	fsys = connectionsProject()
	delete(fsys, "scenes/pair.scene.yaml")
	r, err = assetcheck.Check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if got := problems(r); strings.Contains(got, "has no object") || strings.Contains(got, "load scene") {
		t.Fatalf("a missing instanced scene was reported as a resolve failure:\n%s", got)
	}
}

// A scene that resolves to a fault of its own, such as a cycle, is reported on it.
func TestUnresolvableConnectedSceneIsReported(t *testing.T) {
	fsys := connectionsProject()
	fsys["scenes/pair.scene.yaml"].Data = []byte(strings.Replace(pairScene, "layers:", "inherits: scenes/derived.scene.yaml\nlayers:", 1))
	r, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if got := problems(r); !strings.Contains(got, "scenes/derived.scene.yaml:2: sceneformat: scene cycle") {
		t.Fatalf("problems:\n%s", got)
	}
}

func FuzzCheckConnections(f *testing.F) {
	f.Add([]byte(levelScene))
	f.Add([]byte(derivedScene))
	f.Add([]byte(strings.Replace(levelScene, "to: west/door", "to: west/ghost", 1)))
	reg := connectionsRegistry(f)
	f.Fuzz(func(t *testing.T, scene []byte) {
		fsys := connectionsProject()
		fsys["scenes/level.scene.yaml"].Data = scene
		r, err := assetcheck.CheckWith(fsys, assetcheck.Options{Components: reg})
		if err != nil {
			t.Fatal(err)
		}
		lines := yamlLines(scene)
		for _, p := range r.Problems {
			if p.Line < 0 || (p.File == "scenes/level.scene.yaml" && p.Line > lines) {
				t.Fatalf("problem %q has line %d outside the %d-line file %q", p, p.Line, lines, scene)
			}
		}
	})
}

// An override that breaks a connection a scene brings from the scene it instances or inherits fails at the override, in a scene with no connections of its own too, and the scene the connection comes from passes.
func TestCheckWithComponentsReportsConnectionsOverridesBreak(t *testing.T) {
	fsys := connectionsProject()
	level := `version: 2
layers:
  - name: actors
    objects:
      - id: west
        scene: scenes/pair.scene.yaml
        overrides:
          button:
            components:
              switch: {}
`
	derived := `version: 2
inherits: scenes/pair.scene.yaml
overrides:
  button:
    components:
      switch: {}
`
	fsys["scenes/level.scene.yaml"].Data = []byte(level)
	fsys["scenes/derived.scene.yaml"].Data = []byte(derived)
	r, err := assetcheck.CheckWith(fsys, assetcheck.Options{Components: connectionsRegistry(t)})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"scenes/derived.scene.yaml:" + strconv.Itoa(lineOf(t, derived, "button:")) + `: sceneformat: the override of object "button" breaks connection ("button" pressed -> "door" open), which a scene it instances or inherits brings: several components of object "button" declare event "pressed"`,
		"scenes/level.scene.yaml:" + strconv.Itoa(lineOf(t, level, "button:")) + `: sceneformat: the override of object "west/button" breaks connection ("west/button" pressed -> "west/door" open), which a scene it instances or inherits brings: several components of object "west/button" declare event "pressed"`,
	}
	if len(r.Problems) != len(want) {
		t.Fatalf("%d problems, want %d:\n%s", len(r.Problems), len(want), problems(r))
	}
	for i, w := range want {
		if got := r.Problems[i].String(); !strings.HasPrefix(got, w) {
			t.Errorf("problem %d = %q\nwant prefix %q", i, got, w)
		}
	}
}

// A component an override writes is checked against its type, at the override's field.
func TestCheckWithComponentsReportsOverriddenComponents(t *testing.T) {
	fsys := connectionsProject()
	level := strings.Replace(levelScene, "        scene: scenes/pair.scene.yaml\n", "        scene: scenes/pair.scene.yaml\n        overrides:\n          door:\n            components:\n              door: {nope: 3}\n", 1)
	derived := strings.Replace(derivedScene, "layers:", "overrides:\n  door:\n    components:\n      door:\n        nope: 3\nlayers:", 1)
	fsys["scenes/level.scene.yaml"].Data = []byte(level)
	fsys["scenes/derived.scene.yaml"].Data = []byte(derived)
	r, err := assetcheck.CheckWith(fsys, assetcheck.Options{Components: connectionsRegistry(t)})
	if err != nil {
		t.Fatal(err)
	}
	want := []assetcheck.Problem{
		{File: "scenes/derived.scene.yaml", Line: lineOf(t, derived, "nope: 3"), Message: `sceneformat: object "door" component "door" field "nope": unknown field (the type has no fields)`},
		{File: "scenes/level.scene.yaml", Line: lineOf(t, level, "door: {nope: 3}"), Message: `sceneformat: object "west/door" component "door" field "nope": unknown field (the type has no fields)`},
	}
	if got := r.Problems; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("problems:\n%s\nwant:\n%v\n%v", problems(r), want[0], want[1])
	}
}

// A connection of a scene's own that repeats one a scene it instances brings fails at its line.
func TestCheckWithComponentsReportsConnectionsRepeatingBroughtOnes(t *testing.T) {
	fsys := connectionsProject()
	level := strings.Replace(levelScene, "  - from: lever\n", "  - from: west/button\n    event: pressed\n    to: west/door\n    handler: open\n  - from: lever\n", 1)
	fsys["scenes/level.scene.yaml"].Data = []byte(level)
	r, err := assetcheck.CheckWith(fsys, assetcheck.Options{Components: connectionsRegistry(t)})
	if err != nil {
		t.Fatal(err)
	}
	want := assetcheck.Problem{File: "scenes/level.scene.yaml", Line: lineOf(t, level, "handler: open"), Message: `sceneformat: connection 0 ("west/button" pressed -> "west/door" open): connects the same event and handler as a connection that a scene it instances or inherits brings`}
	if len(r.Problems) != 1 || r.Problems[0] != want {
		t.Fatalf("problems:\n%s\nwant:\n%v", problems(r), want)
	}
}

// A scene that does not resolve is reported whether or not it has connections, at the instance at fault when there is one.
func TestUnresolvableSceneWithoutConnectionsIsReported(t *testing.T) {
	fsys := connectionsProject()
	level := `version: 2
layers:
  - name: actors
    objects:
      - id: lever
      - id: west
        scene: scenes/pair.scene.yaml
        overrides:
          ghost:
            name: boo
`
	fsys["scenes/level.scene.yaml"].Data = []byte(level)
	fsys["scenes/a.scene.yaml"] = &fstest.MapFile{Data: []byte("version: 2\ninherits: scenes/b.scene.yaml\n")}
	fsys["scenes/b.scene.yaml"] = &fstest.MapFile{Data: []byte("version: 2\ninherits: scenes/a.scene.yaml\n")}
	r, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	got := problems(r)
	for _, want := range []string{
		"scenes/a.scene.yaml:2: sceneformat: scene cycle",
		"scenes/b.scene.yaml:2: sceneformat: scene cycle",
		"scenes/level.scene.yaml:" + strconv.Itoa(lineOf(t, level, "overrides:")) + `: sceneformat: object "west" overrides "ghost", which scene "scenes/pair.scene.yaml" does not hold`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems do not include %q:\n%s", want, got)
		}
	}
	if len(r.Problems) != 3 {
		t.Fatalf("%d problems, want 3:\n%s", len(r.Problems), got)
	}
}
