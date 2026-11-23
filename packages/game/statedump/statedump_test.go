package statedump_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/replay"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/statedump"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

var update = flag.Bool("update", false, "rewrite testdata/*.yaml from the current output")

func TestMain(m *testing.M) { nsgotest.Main(m) }

type pos struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type health struct {
	HP   int      `json:"hp"`
	Tags []string `json:"tags,omitempty"`
}

type label string

// level is a small seeded game: a player steered by the "move_x" axis and "jump" action, enemies spawned at random places every 20 steps that lose health each step and die, a weapon parented to the player, and a scene stack that pauses on "pause".
type level struct {
	w       *entities.World
	src     replay.ActionSource
	rng     *rand.Rand
	player  entities.Entity
	enemies []entities.Entity
	step    int
	score   int
	mgr     *scene.Manager
	names   map[*scene.Controller]string
	play    *scene.Controller
	pause   *scene.Controller
}

func registerCodecs(t testing.TB, w *entities.World) {
	t.Helper()
	for _, err := range []error{
		entities.RegisterCodec[pos](w, "pos"),
		entities.RegisterCodec[health](w, "health"),
		entities.RegisterCodec[label](w, "name"),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func newLevel(t testing.TB, seed uint64, src replay.ActionSource, mgr *scene.Manager) *level {
	t.Helper()
	w := entities.NewWorld()
	registerCodecs(t, w)
	l := &level{w: w, src: src, rng: rand.New(rand.NewPCG(seed, 0x5eed)), mgr: mgr}
	l.player = w.Spawn()
	entities.Set(w, l.player, pos{X: 10, Y: 20})
	entities.Set(w, l.player, health{HP: 100, Tags: []string{"hero", "yes", "1"}})
	entities.Set(w, l.player, label("player: one\nsecond line"))
	weapon := w.Spawn()
	entities.Set(w, weapon, label("sword"))
	entities.Set(w, weapon, pos{X: 1})
	if err := entities.NewHierarchy2D(w).SetParent(weapon, l.player, false); err != nil {
		t.Fatal(err)
	}
	if mgr != nil {
		l.play = &scene.Controller{}
		l.pause = &scene.Controller{}
		l.names = map[*scene.Controller]string{l.play: "play", l.pause: "pause"}
		mgr.Switch(l.play, nil)
	}
	return l
}

func (l *level) Step(dt time.Duration) {
	l.src.Step()
	sec := dt.Seconds()
	if l.mgr != nil && l.src.JustPressed("pause") {
		if l.mgr.Current() == l.pause {
			l.mgr.Pop()
		} else {
			l.mgr.Push(l.pause)
		}
	}
	p, _ := entities.Get[pos](l.w, l.player)
	// The conversion rounds the product before the add, so arm64 does not fuse them into one FMA and the golden matches on every architecture.
	p.X += float64(l.src.Axis("move_x") * 120 * sec)
	if l.src.JustPressed("jump") {
		p.Y -= 8
	}
	if l.step%20 == 0 {
		e := l.w.Spawn()
		entities.Set(l.w, e, pos{X: float64(l.rng.IntN(320)), Y: float64(l.rng.IntN(180))})
		entities.Set(l.w, e, health{HP: 3 + l.rng.IntN(40)})
		l.enemies = append(l.enemies, e)
	}
	alive := l.enemies[:0]
	for _, e := range l.enemies {
		h, _ := entities.Get[health](l.w, e)
		h.HP--
		if h.HP <= 0 {
			l.w.Destroy(e)
			l.score += 10
			continue
		}
		alive = append(alive, e)
	}
	l.enemies = alive
	l.step++
}

func (l *level) sim() statedump.Sim {
	return statedump.Sim{
		Step: l.Step,
		Source: statedump.Source{
			World:     l.w,
			Scenes:    l.mgr,
			SceneName: func(c *scene.Controller) string { return l.names[c] },
			Values: func() map[string]any {
				return map[string]any{"score": l.score, "step": l.step, "best": map[string]any{"name": "ann", "at": 1.5}}
			},
		},
	}
}

func recording() *replay.Recording {
	return &replay.Recording{
		Version:    replay.FormatVersion,
		Seed:       42,
		Rate:       60,
		Actions:    []string{"jump", "pause"},
		Axes:       []string{"move_x"},
		TotalSteps: 90,
		Events: []replay.StepEvent{
			{Step: 5, Axes: []replay.AxisChange{{Index: 0, Value: 1}}},
			{Step: 12, Press: []int{0}},
			{Step: 13, Release: []int{0}},
			{Step: 30, Axes: []replay.AxisChange{{Index: 0, Value: -0.5}}},
			{Step: 60, Press: []int{1}},
			{Step: 61, Release: []int{1}},
		},
	}
}

// runLevel runs rec against level inside an NSGo scope, so the scene manager has one, and returns the encoded dump.
func runLevel(t *testing.T, rec *replay.Recording) []byte {
	t.Helper()
	var mgr *scene.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = scene.NewManager(cx)
		return core.Canvas{Style: core.Style{Width: core.Pt(10), Height: core.Pt(10)}}
	})
	var (
		d   *statedump.Dump
		err error
	)
	app.Post(func() {
		d, err = statedump.Run(rec, func(seed uint64, src replay.ActionSource) (statedump.Sim, error) {
			return newLevel(t, seed, src, mgr).sim(), nil
		})
	})
	app.Settle()
	if err != nil {
		t.Fatal(err)
	}
	data, err := statedump.Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if diff := statedump.Diff(want, got); diff != "" {
		t.Fatalf("%s differs from the run:\n%s", path, diff)
	}
}

func TestRunSameSeedAndInputsDumpIdenticalFiles(t *testing.T) {
	a := runLevel(t, recording())
	b := runLevel(t, recording())
	if !bytes.Equal(a, b) {
		t.Fatalf("two runs differ:\n%s", statedump.Diff(a, b))
	}
	checkGolden(t, "level.yaml", a)

	d, err := statedump.Parse(a)
	if err != nil {
		t.Fatal(err)
	}
	if d.Seed != 42 || d.Rate != 60 || d.Steps != 90 {
		t.Fatalf("header = seed %d rate %d steps %d", d.Seed, d.Rate, d.Steps)
	}
	if d.Scene == nil || d.Scene.Depth != 2 || d.Scene.Current != "pause" {
		t.Fatalf("scene = %+v, want pause on top of play", d.Scene)
	}
}

func TestRunChangedInputShowsReadableDiff(t *testing.T) {
	base := runLevel(t, recording())
	rec := recording()
	rec.Events[3].Axes[0].Value = 0.5 // steer right instead of left from step 30
	changed := runLevel(t, rec)

	diff := statedump.Diff(base, changed)
	if diff == "" {
		t.Fatal("changed input produced an identical dump")
	}
	var removed, added []string
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"):
		case strings.HasPrefix(line, "-"):
			removed = append(removed, line)
		case strings.HasPrefix(line, "+"):
			added = append(added, line)
		}
	}
	// Only the player's x moves: one line out, one line in.
	if len(removed) != 1 || len(added) != 1 {
		t.Fatalf("diff changes %d/%d lines, want 1/1:\n%s", len(removed), len(added), diff)
	}
	if !strings.HasPrefix(strings.TrimSpace(removed[0][1:]), "x: ") || !strings.HasPrefix(strings.TrimSpace(added[0][1:]), "x: ") {
		t.Fatalf("diff does not change the player's x:\n%s", diff)
	}
	if !strings.Contains(diff, "type: pos") {
		t.Fatalf("diff context does not name the component:\n%s", diff)
	}
	checkGolden(t, "level-changed.diff", []byte(diff))
}

func TestRunWithoutScenes(t *testing.T) {
	rec := recording()
	run := func() []byte {
		d, err := statedump.Run(rec, func(seed uint64, src replay.ActionSource) (statedump.Sim, error) {
			return newLevel(t, seed, src, nil).sim(), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if d.Scene != nil {
			t.Fatalf("scene = %+v without a manager", d.Scene)
		}
		data, err := statedump.Encode(d)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if a, b := run(), run(); !bytes.Equal(a, b) {
		t.Fatalf("two runs differ:\n%s", statedump.Diff(a, b))
	}
}

func TestRunErrors(t *testing.T) {
	ok := func(seed uint64, src replay.ActionSource) (statedump.Sim, error) {
		return statedump.Sim{Step: func(time.Duration) { src.Step() }}, nil
	}
	bad := recording()
	bad.Version = 99
	if _, err := statedump.Run(bad, ok); err == nil {
		t.Fatal("Run accepted an invalid recording")
	}
	if _, err := statedump.Run(nil, ok); err == nil {
		t.Fatal("Run accepted a nil recording")
	}
	if _, err := statedump.Run(recording(), nil); err == nil {
		t.Fatal("Run accepted a nil makeSim")
	}
	boom := errors.New("boom")
	if _, err := statedump.Run(recording(), func(uint64, replay.ActionSource) (statedump.Sim, error) {
		return statedump.Sim{}, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("Run error = %v, want boom", err)
	}
	if _, err := statedump.Run(recording(), func(uint64, replay.ActionSource) (statedump.Sim, error) {
		return statedump.Sim{}, nil
	}); err == nil {
		t.Fatal("Run accepted a sim without Step")
	}
	if _, err := statedump.Run(recording(), func(_ uint64, src replay.ActionSource) (statedump.Sim, error) {
		return statedump.Sim{Step: func(time.Duration) { src.Step() }, Source: statedump.Source{Values: func() map[string]any {
			return map[string]any{"bad": func() {}}
		}}}, nil
	}); err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("Run error = %v, want one about the value encoding/json cannot encode", err)
	}
	if _, err := statedump.Run(recording(), func(uint64, replay.ActionSource) (statedump.Sim, error) {
		return statedump.Sim{Step: func(time.Duration) {}}, nil
	}); err == nil || !strings.Contains(err.Error(), "stepped the ActionSource 0 times, want 90") {
		t.Fatalf("Run error = %v, want one about a sim that ignores the recording", err)
	}
	d, err := statedump.Run(&replay.Recording{Version: replay.FormatVersion, Seed: 9, Rate: 30, TotalSteps: 4}, ok)
	if err != nil {
		t.Fatal(err)
	}
	data, err := statedump.Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	if want := "version: 1\nseed: 9\nrate: 30\nsteps: 4\nentities: []\n"; string(data) != want {
		t.Fatalf("empty dump =\n%s\nwant\n%s", data, want)
	}
}

func TestDumpRestoresIntoWorld(t *testing.T) {
	l := newLevel(t, 3, replay.NewPlayer(recording()), nil)
	for range 50 {
		l.Step(time.Second / 60)
	}
	d, err := statedump.Capture(statedump.Source{World: l.w})
	if err != nil {
		t.Fatal(err)
	}
	data, err := statedump.Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := statedump.Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	w := entities.NewWorld()
	registerCodecs(t, w)
	if err := w.Restore(parsed.Snapshot()); err != nil {
		t.Fatal(err)
	}
	again, err := statedump.Capture(statedump.Source{World: w})
	if err != nil {
		t.Fatal(err)
	}
	data2, err := statedump.Encode(again)
	if err != nil {
		t.Fatal(err)
	}
	if diff := statedump.Diff(data, data2); diff != "" {
		t.Fatalf("restored world dumps differently:\n%s", diff)
	}
}

func TestEncodeParseRoundTrip(t *testing.T) {
	parent := uint32(0)
	d := &statedump.Dump{
		Version: statedump.FormatVersion,
		Seed:    1<<64 - 1,
		Rate:    60,
		Steps:   3,
		Scene:   &statedump.SceneState{Depth: 1, Current: "fade", Transitioning: true, Progress: 0.25},
		Entities: []entities.EntitySnapshot{
			{Index: 0, Generation: 1, Components: []entities.ComponentSnapshot{
				{Type: "a", Data: json.RawMessage(`{"z":1,"a":[true,false,null],"m":{},"s":[],"n":-0,"f":1.5e+300,"g":1E-7,"big":123456789012345678901234567890}`)},
				{Type: "b", Data: json.RawMessage(`["true","1","1.5","null","~","yes","2001-12-14","","  lead","trail ","a: b","- x","#c","multi\nline\n","tab\there","é中","\u003c\u0026\u003e","0x10","1e3",".inf","1:20","-1:30.5","190:20:30","12:60"]`)},
				{Type: "c", Data: json.RawMessage(`"plain"`)},
				{Type: "d", Data: json.RawMessage(`{"1":1,"true":2,"null":3,"":4,"key with: colon":5,"1:20":6}`)},
				{Type: "e", Data: json.RawMessage(`null`)},
			}},
			{Index: 4, Parent: &parent, Generation: 7},
		},
	}
	if err := d.SetValue("rng", map[string]uint64{"state": 1<<63 + 5}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetValue("long", strings.Repeat("word ", 40)); err != nil {
		t.Fatal(err)
	}
	if err := d.SetValue("none", nil); err != nil {
		t.Fatal(err)
	}
	data, err := statedump.Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := statedump.Parse(data)
	if err != nil {
		t.Fatalf("Parse(Encode) failed: %v\n%s", err, data)
	}
	again, err := statedump.Encode(got)
	if err != nil {
		t.Fatal(err)
	}
	if diff := statedump.Diff(data, again); diff != "" {
		t.Fatalf("round trip changed the file:\n%s", diff)
	}
	for i, e := range d.Entities {
		for j, c := range e.Components {
			if g := got.Entities[i].Components[j].Data; !bytes.Equal(g, c.Data) {
				t.Errorf("component %s data = %s, want %s", c.Type, g, c.Data)
			}
		}
	}
	if g := string(got.Values["none"]); g != "null" {
		t.Errorf("value none = %q, want null", g)
	}
	checkGolden(t, "roundtrip.yaml", data)
}

// TestNilComponentRoundTrip captures a registered component whose value is a nil slice, which encoding/json writes as null, and reads it back from the file.
func TestNilComponentRoundTrip(t *testing.T) {
	type inv []string
	w := entities.NewWorld()
	if err := entities.RegisterCodec[inv](w, "inv"); err != nil {
		t.Fatal(err)
	}
	entities.Set(w, w.Spawn(), inv(nil))
	d, err := statedump.Capture(statedump.Source{World: w})
	if err != nil {
		t.Fatal(err)
	}
	data, err := statedump.Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "data: null\n") {
		t.Fatalf("nil component not written as null:\n%s", data)
	}
	got, err := statedump.Parse(data)
	if err != nil {
		t.Fatalf("Parse refused a null component: %v\n%s", err, data)
	}
	if c := got.Entities[0].Components[0]; string(c.Data) != "null" {
		t.Fatalf("component data = %q, want null", c.Data)
	}
	again, err := statedump.Encode(got)
	if err != nil {
		t.Fatal(err)
	}
	if diff := statedump.Diff(data, again); diff != "" {
		t.Fatalf("round trip changed the file:\n%s", diff)
	}
}

func TestValuesInByteOrder(t *testing.T) {
	d := &statedump.Dump{Version: statedump.FormatVersion}
	for _, name := range []string{"a", "a9", "a10", "1", "2", "10", "B"} {
		if err := d.SetValue(name, 0); err != nil {
			t.Fatal(err)
		}
	}
	data, err := statedump.Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 1\nseed: 0\nsteps: 0\nvalues:\n  \"1\": 0\n  \"10\": 0\n  \"2\": 0\n  B: 0\n  a: 0\n  a10: 0\n  a9: 0\nentities: []\n"
	if string(data) != want {
		t.Fatalf("Encode =\n%s\nwant\n%s", data, want)
	}
	got, err := statedump.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := statedump.Encode(got); err != nil || string(again) != want {
		t.Fatalf("re-encode = %q, %v", again, err)
	}
}

// TestCaptureDuringOvershootingTransition captures a Switch whose ease-out-back curve takes the progress past 1 halfway through.
func TestCaptureDuringOvershootingTransition(t *testing.T) {
	var mgr *scene.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = scene.NewManager(cx)
		return core.Canvas{Style: core.Style{Width: core.Pt(10), Height: core.Pt(10)}}
	})
	from, to := &scene.Controller{}, &scene.Controller{}
	app.Post(func() { mgr.Switch(from, nil) })
	app.Settle()
	app.Post(func() {
		mgr.Switch(to, &scene.Transition{Duration: time.Second, Curve: core.Curve{X1: 0.34, Y1: 1.56, X2: 0.64, Y2: 1}})
	})
	app.Settle()
	app.Advance(600 * time.Millisecond)
	if !mgr.Transitioning() || mgr.TransitionProgress() <= 1 {
		t.Fatalf("transitioning=%v progress=%v, want an overshoot past 1", mgr.Transitioning(), mgr.TransitionProgress())
	}
	d, err := statedump.Capture(statedump.Source{Scenes: mgr})
	if err != nil {
		t.Fatalf("Capture during an overshooting transition: %v", err)
	}
	if d.Scene.Progress != mgr.TransitionProgress() {
		t.Fatalf("progress = %v, want %v", d.Scene.Progress, mgr.TransitionProgress())
	}
	data, err := statedump.Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := statedump.Parse(data); err != nil {
		t.Fatalf("Parse refused an overshooting progress: %v\n%s", err, data)
	}
}

func TestParseRefuses(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"version":           "version: 2\nseed: 0\nsteps: 0\nentities: []\n",
		"unknown top":       "version: 1\nseed: 0\nsteps: 0\nextra: 1\nentities: []\n",
		"unknown entity":    "version: 1\nseed: 0\nsteps: 0\nentities:\n  - index: 0\n    generation: 1\n    tag: x\n",
		"unknown component": "version: 1\nseed: 0\nsteps: 0\nentities:\n  - index: 0\n    generation: 1\n    components:\n      - type: a\n        data: 1\n        note: x\n",
		"unknown scene":     "version: 1\nseed: 0\nsteps: 0\nscene:\n  depth: 1\n  name: x\nentities: []\n",
		"trailing document": "version: 1\nseed: 0\nsteps: 0\nentities: []\n---\nversion: 1\n",
		"not a mapping":     "- 1\n",
		"zero generation":   "version: 1\nseed: 0\nsteps: 0\nentities:\n  - index: 0\n    generation: 0\n",
		"unsorted entities": "version: 1\nseed: 0\nsteps: 0\nentities:\n  - index: 2\n    generation: 1\n  - index: 1\n    generation: 1\n",
		"unknown parent":    "version: 1\nseed: 0\nsteps: 0\nentities:\n  - index: 0\n    generation: 1\n    parent: 5\n",
		"own parent":        "version: 1\nseed: 0\nsteps: 0\nentities:\n  - index: 0\n    generation: 1\n    parent: 0\n",
		"unsorted comps":    "version: 1\nseed: 0\nsteps: 0\nentities:\n  - index: 0\n    generation: 1\n    components:\n      - type: b\n        data: 1\n      - type: a\n        data: 1\n",
		"missing data":      "version: 1\nseed: 0\nsteps: 0\nentities:\n  - index: 0\n    generation: 1\n    components:\n      - type: a\n",
		"empty type":        "version: 1\nseed: 0\nsteps: 0\nentities:\n  - index: 0\n    generation: 1\n    components:\n      - type: \"\"\n        data: 1\n",
		"int key":           "version: 1\nseed: 0\nsteps: 0\nentities:\n  - index: 0\n    generation: 1\n    components:\n      - type: a\n        data:\n          1: x\n",
		"duplicate key":     "version: 1\nseed: 0\nsteps: 0\nentities:\n  - index: 0\n    generation: 1\n    components:\n      - type: a\n        data:\n          k: 1\n          k: 2\n",
		"alias":             "version: 1\nseed: 0\nsteps: 0\nvalues:\n  a: &x [1]\n  b: *x\nentities: []\n",
		"merge":             "version: 1\nseed: 0\nsteps: 0\nvalues:\n  a:\n    <<: {k: 1}\nentities: []\n",
		"nan":               "version: 1\nseed: 0\nsteps: 0\nvalues:\n  a: .nan\nentities: []\n",
		"inf":               "version: 1\nseed: 0\nsteps: 0\nvalues:\n  a: -.inf\nentities: []\n",
		"binary":            "version: 1\nseed: 0\nsteps: 0\nvalues:\n  a: !!binary aGk=\nentities: []\n",
		"custom tag":        "version: 1\nseed: 0\nsteps: 0\nvalues:\n  a: !thing x\nentities: []\n",
		"empty value name":  "version: 1\nseed: 0\nsteps: 0\nvalues:\n  \"\": 1\nentities: []\n",
		"negative steps":    "version: 1\nseed: 0\nsteps: -1\nentities: []\n",
		"negative rate":     "version: 1\nseed: 0\nrate: -1\nsteps: 0\nentities: []\n",
		"infinite progress": "version: 1\nseed: 0\nsteps: 0\nscene:\n  depth: 1\n  transitioning: true\n  progress: .inf\nentities: []\n",
		"nan progress":      "version: 1\nseed: 0\nsteps: 0\nscene:\n  depth: 1\n  transitioning: true\n  progress: .nan\nentities: []\n",
		"values sequence":   "version: 1\nseed: 0\nsteps: 0\nvalues: [1]\nentities: []\n",
		"int value name":    "version: 1\nseed: 0\nsteps: 0\nvalues:\n  1: x\nentities: []\n",
		"duplicate value":   "version: 1\nseed: 0\nsteps: 0\nvalues:\n  a: 1\n  a: 2\nentities: []\n",
		"idle progress":     "version: 1\nseed: 0\nsteps: 0\nscene:\n  depth: 1\n  progress: 0.5\nentities: []\n",
		"negative depth":    "version: 1\nseed: 0\nsteps: 0\nscene:\n  depth: -1\nentities: []\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if d, err := statedump.Parse([]byte(in)); err == nil {
				t.Fatalf("Parse accepted:\n%s\nas %+v", in, d)
			}
		})
	}
}

func TestParseNormalizesHandWrittenData(t *testing.T) {
	in := "version: 1\nseed: 0\nsteps: 0\nvalues:\n  a: {f: 1., h: 0x10, b: True, d: 2001-12-14, n: ~}\nentities: []\n"
	d, err := statedump.Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(d.Values["a"]), `{"f":1,"h":16,"b":true,"d":"2001-12-14","n":null}`; got != want {
		t.Fatalf("data = %s, want %s", got, want)
	}
	out, err := statedump.Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := statedump.Parse(out); err != nil || string(again.Values["a"]) != string(d.Values["a"]) {
		t.Fatalf("re-parse = %v, %v", again, err)
	}
}

func TestEncodeRefusesInvalidDump(t *testing.T) {
	for name, d := range map[string]*statedump.Dump{
		"nil":          nil,
		"version":      {},
		"bad json":     {Version: 1, Values: map[string]json.RawMessage{"a": json.RawMessage(`{`)}},
		"nil data":     {Version: 1, Entities: []entities.EntitySnapshot{{Index: 0, Generation: 1, Components: []entities.ComponentSnapshot{{Type: "a"}}}}},
		"duplicate":    {Version: 1, Entities: []entities.EntitySnapshot{{Index: 1, Generation: 1}, {Index: 1, Generation: 1}}},
		"dup json key": {Version: 1, Values: map[string]json.RawMessage{"a": json.RawMessage(`{"k":1,"k":2}`)}},
		"deep json":    {Version: 1, Values: map[string]json.RawMessage{"a": json.RawMessage(strings.Repeat("[", 600) + strings.Repeat("]", 600))}},
	} {
		if _, err := statedump.Encode(d); err == nil {
			t.Errorf("%s: Encode accepted an invalid dump", name)
		}
	}
	var nilDump *statedump.Dump
	if err := nilDump.SetValue("a", 1); err == nil {
		t.Error("SetValue on nil dump succeeded")
	}
	if err := (&statedump.Dump{}).SetValue("", 1); err == nil {
		t.Error("SetValue accepted an empty name")
	}
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	d := &statedump.Dump{Version: 1, Steps: 2, Entities: []entities.EntitySnapshot{{Index: 3, Generation: 2}}}
	if err := statedump.WriteFile(path, d); err != nil {
		t.Fatal(err)
	}
	got, err := statedump.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Steps != 2 || len(got.Entities) != 1 || got.Entities[0].Index != 3 {
		t.Fatalf("ReadFile = %+v", got)
	}
	if _, err := statedump.ReadFile(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Fatal("ReadFile read a missing file")
	}
	if err := statedump.WriteFile(path, &statedump.Dump{}); err == nil {
		t.Fatal("WriteFile wrote an invalid dump")
	}
}

func TestDiff(t *testing.T) {
	if got := statedump.Diff([]byte("a\nb\n"), []byte("a\nb\n")); got != "" {
		t.Fatalf("equal inputs diff = %q", got)
	}
	a := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n20\n"
	b := strings.Replace(strings.Replace(a, "3\n", "three\n", 1), "17\n", "", 1)
	want := "--- a\n+++ b\n" +
		"@@ -1,6 +1,6 @@\n 1\n 2\n-3\n+three\n 4\n 5\n 6\n" +
		"@@ -14,7 +14,6 @@\n 14\n 15\n 16\n-17\n 18\n 19\n 20\n"
	if got := statedump.Diff([]byte(a), []byte(b)); got != want {
		t.Fatalf("Diff =\n%s\nwant\n%s", got, want)
	}
	// Changes whose context overlaps share one hunk.
	b = strings.Replace(strings.Replace(a, "5\n", "five\n", 1), "10\n", "ten\n", 1)
	if got := statedump.Diff([]byte(a), []byte(b)); strings.Count(got, "@@ -") != 1 {
		t.Fatalf("Diff split close changes:\n%s", got)
	}
	if got, want := statedump.Diff(nil, []byte("x\n")), "--- a\n+++ b\n@@ -0,0 +1 @@\n+x\n"; got != want {
		t.Fatalf("Diff from empty =\n%q\nwant\n%q", got, want)
	}
	if got := statedump.Diff([]byte("x"), []byte("x\n")); !strings.Contains(got, "newline at the end") {
		t.Fatalf("Diff of a trailing newline = %q", got)
	}
}
