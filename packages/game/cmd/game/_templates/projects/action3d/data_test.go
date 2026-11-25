package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func readGameFile(t testing.TB, name string) []byte {
	t.Helper()
	data, err := gameFiles.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestActorsFileIsCanonical checks that data/actors.yaml is valid and in the form encodeActors writes, so a load and save changes nothing.
func TestActorsFileIsCanonical(t *testing.T) {
	data := readGameFile(t, "data/actors.yaml")
	a, err := parseActors(data)
	if err != nil {
		t.Fatal(err)
	}
	out, err := encodeActors(a)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("data/actors.yaml is not canonical; encodeActors writes:\n%s", out)
	}
}

// TestSceneIsCanonical checks that the level scene's components are the ones levelComponents registers and that the file is in the form sceneformat.Encode writes.
func TestSceneIsCanonical(t *testing.T) {
	data := readGameFile(t, "scenes/keep.scene.yaml")
	doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: "scenes/keep.scene.yaml", Components: levelComponents()})
	if err != nil {
		t.Fatal(err)
	}
	out, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("scenes/keep.scene.yaml is not canonical; sceneformat.Encode writes:\n%s", out)
	}
}

func TestParseActorsRefusesBrokenFiles(t *testing.T) {
	good := string(readGameFile(t, "data/actors.yaml"))
	for _, tc := range []struct {
		name, from, to, want string
	}{
		{"unknown field", "gravity: 22\n", "gravity: 22\nwind: 3\n", "field wind not found"},
		{"version", "version: 1\n", "version: 2\n", "unsupported version 2"},
		{"missing clip", "  - name: fall\n", "  - name: tumble\n", `clip "fall" is missing`},
		{"parent after child", "    parent: hips\n    at: [0, 0.1, 0]\n", "    parent: head\n    at: [0, 0.1, 0]\n", `parent "head" is not an earlier joint`},
		{"keys out of order", "          - at: 0.3\n            turn: [-12, -6, 0]\n", "          - at: 0\n            turn: [-12, -6, 0]\n", "not after the key before it"},
		{"bad paint", "    body: '#3b82f6'\n", "    body: 'not a color'\n", "paint body"},
		{"no hit", "      - name: hit\n", "      - name: miss\n", `fires "hit" 0 times`},
		{"short vector", "    at: [0, 0.9, 0]\n", "    at: [0, 0.9]\n", "has 2 numbers, want 3"},
		{"slow swing", "  attackSpeed: 0.55\n", "  attackSpeed: 1e-30\n", "attackSpeed 1e-30 is not from 0.1 to 10"},
		{"endless clip", "    length: 0.5\n", "    length: 1e10\n", "length 1e+10 is not above 0 and at most 60 seconds"},
		{"second document", "version: 1\n", "version: 1\n", "second YAML document"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := strings.Replace(good, tc.from, tc.to, 1)
			if tc.name == "second document" {
				bad += "---\nversion: 1\n"
			} else if bad == good {
				t.Fatalf("the case does not change the file: %q not found", tc.from)
			}
			_, err := parseActors([]byte(bad))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseActors error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestBuildLevelNeedsOneOfEachLandmark(t *testing.T) {
	data := string(readGameFile(t, "scenes/keep.scene.yaml"))
	broken := strings.Replace(data, "          goal: {}\n", "          block: {}\n", 1)
	doc, err := sceneformat.Parse([]byte(broken))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildLevel(doc, levelComponents()); err == nil || !strings.Contains(err.Error(), "goal") {
		t.Fatalf("buildLevel error = %v, want one naming the missing goal", err)
	}
}

func TestBuildLevelRefusesNestedAndRotatedObjects(t *testing.T) {
	data := readGameFile(t, "scenes/keep.scene.yaml")
	for _, tc := range []struct {
		name, want string
		edit       func(objs []sceneformat.ObjectDoc) []sceneformat.ObjectDoc
	}{
		{"rotated block", `"ledge": a block is not rotated`, func(objs []sceneformat.ObjectDoc) []sceneformat.ObjectDoc {
			for i := range objs {
				if objs[i].ID == "ledge" {
					objs[i].RotY = 0.5
				}
			}
			return objs
		}},
		{"tilted lamp", "a lamp is not rotated", func(objs []sceneformat.ObjectDoc) []sceneformat.ObjectDoc {
			objs = append(objs, sceneformat.ObjectDoc{ID: "tilted_lamp", RotX: 0.2, Components: map[string]any{compLamp: map[string]any{}}})
			return objs
		}},
		{"nested block", `"ledge": a level's objects sit at the top of their layer, not inside "floor"`, func(objs []sceneformat.ObjectDoc) []sceneformat.ObjectDoc {
			var ledge sceneformat.ObjectDoc
			kept := objs[:0]
			for _, o := range objs {
				if o.ID == "ledge" {
					ledge = o
					continue
				}
				kept = append(kept, o)
			}
			for i := range kept {
				if kept[i].ID == "floor" {
					kept[i].Children = append(kept[i].Children, ledge)
				}
			}
			return kept
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := sceneformat.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			doc.Layers[0].Objects = tc.edit(doc.Layers[0].Objects)
			if _, err := buildLevel(doc, levelComponents()); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("buildLevel error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

// FuzzParseActors checks that parseActors never panics and that whatever it accepts encodes to a file it reads back to the same bytes.
func FuzzParseActors(f *testing.F) {
	f.Add(readGameFile(f, "data/actors.yaml"))
	f.Add([]byte("version: 1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		a, err := parseActors(data)
		if err != nil {
			return
		}
		out, err := encodeActors(a)
		if err != nil {
			t.Fatalf("encodeActors refused what parseActors accepted: %v", err)
		}
		// Whatever the file accepts must build a rig and animate it: a clip length or a speed that overflowed would show up here.
		r := newRig(a)
		anim := r.newAnimator(a.Enemy.Speed, a.Enemy.AttackSpeed)
		anim.play(stateAttack)
		anim.step(step, a.Enemy.Speed)
		if d := r.clips[clipAttack].Duration; d <= 0 {
			t.Fatalf("the attack clip of length %v lasts %v", a.clip(clipAttack).Length, d)
		}
		if cd := float32(r.clips[clipAttack].Duration.Seconds())/a.Enemy.AttackSpeed + a.Enemy.Cooldown; !finite(cd) {
			t.Fatalf("an enemy's cooldown is %v", cd)
		}
		b, err := parseActors(out)
		if err != nil {
			t.Fatalf("parseActors refused what encodeActors wrote: %v\n%s", err, out)
		}
		again, err := encodeActors(b)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("a second round trip changed the file (%v):\n%s\n---\n%s", err, out, again)
		}
	})
}
