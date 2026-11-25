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
