package refs_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/refs"
)

func TestScanAndPlanMoveFollowsStatesAndClips(t *testing.T) {
	fsys := fstest.MapFS{
		"game.yaml": &fstest.MapFile{Data: []byte("version: 1\nname: Knight\nid: dev.nsgo.knight\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n")},
		"scenes/level.scene.yaml": &fstest.MapFile{Data: []byte(`version: 1
name: Level
layers:
  - name: actors
    objects:
      - id: knight
        components:
          animator:
            states: anim/knight.states.yaml
            startClip: idle
`)},
		"anim/knight.states.yaml": &fstest.MapFile{Data: []byte(`version: 1
entry: idle
parameters:
  - name: move.x
    type: float
states:
  - name: idle
    clip: anim/knight_idle.animation.yaml
  - name: run
    clip: anim/knight_run.animation.yaml
transitions:
  - from: idle
    to: run
    when: '|move.x| > 0.1'
`)},
		"anim/knight_idle.animation.yaml": &fstest.MapFile{Data: []byte("type: animation\nversion: 1\nname: idle\nlength: 0.4\nloop: true\n")},
		"anim/knight_run.animation.yaml":  &fstest.MapFile{Data: []byte("type: animation\nversion: 1\nname: run\nlength: 0.4\nloop: true\n")},
	}

	proj, err := refs.Scan(fsys)
	if err != nil {
		t.Fatalf("refs.Scan: %v", err)
	}
	if len(proj.Problems) > 0 {
		t.Fatalf("unexpected problems: %v", proj.Problems)
	}

	var foundStates, foundIdle, foundRun bool
	for _, r := range proj.Refs {
		switch {
		case r.File == "scenes/level.scene.yaml" && r.Field == refs.FieldStates && r.Target == "anim/knight.states.yaml":
			foundStates = true
		case r.File == "anim/knight.states.yaml" && r.Field == refs.FieldClip && r.Target == "anim/knight_idle.animation.yaml":
			foundIdle = true
		case r.File == "anim/knight.states.yaml" && r.Field == refs.FieldClip && r.Target == "anim/knight_run.animation.yaml":
			foundRun = true
		}
	}
	if !foundStates || !foundIdle || !foundRun {
		t.Fatalf("refs = %+v, want states=%t idle=%t run=%t", proj.Refs, foundStates, foundIdle, foundRun)
	}

	// Moving a clip rewrites knight.states.yaml.
	plan, err := refs.PlanMove(fsys, "anim/knight_idle.animation.yaml", "anim/clips/knight_idle.animation.yaml")
	if err != nil {
		t.Fatalf("PlanMove clip: %v", err)
	}
	var rewroteStates bool
	for _, w := range plan.Writes {
		if w.Path == "anim/knight.states.yaml" && strings.Contains(string(w.Data), "clip: anim/clips/knight_idle.animation.yaml") {
			rewroteStates = true
		}
	}
	if !rewroteStates {
		t.Fatalf("clip move writes = %+v, want rewrite of anim/knight.states.yaml", plan.Writes)
	}

	// Moving knight.states.yaml rewrites scenes/level.scene.yaml.
	plan2, err := refs.PlanMove(fsys, "anim/knight.states.yaml", "anim/machines/knight.states.yaml")
	if err != nil {
		t.Fatalf("PlanMove states: %v", err)
	}
	var rewroteScene bool
	for _, w := range plan2.Writes {
		if w.Path == "scenes/level.scene.yaml" && strings.Contains(string(w.Data), "states: anim/machines/knight.states.yaml") {
			rewroteScene = true
		}
	}
	if !rewroteScene {
		t.Fatalf("states move writes = %+v, want rewrite of scenes/level.scene.yaml", plan2.Writes)
	}
}
