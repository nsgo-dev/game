package main

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/fighting"
	"github.com/nsgo-dev/game/packages/game/framedata"
)

// TestDataFilesAreCanonical checks that every data file is in the form its package's encoder writes, so a round trip through load and save changes nothing and a diff shows only what a person changed.
func TestDataFilesAreCanonical(t *testing.T) {
	check := func(name string, data, canonical []byte, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(data, canonical) {
			t.Errorf("%s is not canonical; its encoding is:\n%s", name, canonical)
		}
	}
	err := fs.WalkDir(dataFS, "data", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(dataFS, name)
		if err != nil {
			return err
		}
		switch {
		case name == matchPath:
			m, err := parseMatch(data)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			out, err := encodeMatch(m)
			check(name, data, out, err)
		case strings.HasSuffix(name, ".moves.yaml"):
			set, err := fighting.Parse(data)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			out, err := fighting.Encode(set)
			check(name, data, out, err)
		case strings.HasSuffix(name, ".frames.yaml"):
			doc, err := framedata.Parse(data)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			out, err := framedata.Encode(doc)
			check(name, data, out, err)
		default:
			t.Errorf("%s is not a data file the game reads", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRosterLoads(t *testing.T) {
	r, err := loadRoster(dataFS)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.fighters) < 2 {
		t.Fatalf("the roster has %d fighters, want at least 2", len(r.fighters))
	}
	for _, f := range r.fighters {
		// Every recognized move is something the fighter can do standing.
		for _, m := range f.moves.Moves {
			if _, ok := f.variant(m.Name, false, false); !ok {
				t.Errorf("%s: move %q has no standing frame data move", f.def.ID, m.Name)
			}
		}
		// Every attack has frame data a table can show.
		for _, name := range []string{"light", "heavy", "crouch_light", "air_heavy"} {
			mv, ok := f.frames.Move(name)
			if !ok {
				t.Fatalf("%s has no %s", f.def.ID, name)
			}
			if fd, ok := mv.FrameData(); !ok || fd.Startup < 1 {
				t.Errorf("%s %s: frame data %+v, %v", f.def.ID, name, fd, ok)
			}
		}
	}
}

func TestParseMatchRefusesBadFiles(t *testing.T) {
	good, err := fs.ReadFile(dataFS, matchPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, from, to, want string
	}{
		{"version", "version: 1", "version: 2", "unsupported version"},
		{"unknown field", "  hitstop: 5", "  hitstop: 5\n  slowmo: 2", "field slowmo not found"},
		{"rounds", "roundsToWin: 2", "roundsToWin: 0", "roundsToWin"},
		{"stage right nan", "stageRight: 456", "stageRight: .nan", "stage wider"},
		{"stage right inf", "stageRight: 456", "stageRight: .inf", "stage wider"},
		{"stage left nan", "stageLeft: 24", "stageLeft: .nan", "stage wider"},
		{"stage narrow", "stageRight: 456", "stageRight: 100", "stage wider"},
		{"percentage", "block: 45", "block: 145", "not a percentage"},
		{"color", "'#2dd4bf'", "teal", "is not #rrggbb"},
		{"level", "level: low", "level: knee", "has level"},
		{"script", "[2*2, 3*2, 6+light*2]", "[2*2, 3*2, x+light*2]", "numpad direction"},
		{"script count", "[2*2, 3*2, 6+light*2]", "[2*0, 3*2, 6+light*2]", "steps"},
		{"duplicate", "id: basalt", "id: kestrel", "duplicate id"},
		{"trailing", "jumpIn: 12", "jumpIn: 12\n---\nversion: 1", "trailing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := strings.Replace(string(good), tc.from, tc.to, 1)
			if bad == string(good) {
				t.Fatalf("%q is not in %s", tc.from, matchPath)
			}
			if _, err := parseMatch([]byte(bad)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseMatch = %v, want an error with %q", err, tc.want)
			}
		})
	}
	if _, err := parseMatch(nil); err == nil {
		t.Fatal("an empty match file parsed")
	}
}

func TestLoadRosterChecksReferences(t *testing.T) {
	files := fstest.MapFS{}
	err := fs.WalkDir(dataFS, "data", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(dataFS, name)
		files[name] = &fstest.MapFile{Data: data}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	edit := func(name, from, to string) fstest.MapFS {
		t.Helper()
		out := fstest.MapFS{}
		for k, v := range files {
			out[k] = v
		}
		data := string(files[name].Data)
		if !strings.Contains(data, from) {
			t.Fatalf("%q is not in %s", from, name)
		}
		out[name] = &fstest.MapFile{Data: []byte(strings.Replace(data, from, to, 1))}
		return out
	}
	for _, tc := range []struct {
		name string
		fsys fstest.MapFS
		want string
	}{
		{"missing move set", edit(matchPath, "moveSet: kestrel.moves.yaml", "moveSet: nobody.moves.yaml"), "nobody.moves.yaml"},
		{"extra for unknown move", edit(matchPath, "- name: dash", "- name: dodge"), "unknown move \"dodge\""},
		{"motion past the end", edit(matchPath, "{start: 2, end: 12, dx: 5}", "{start: 2, end: 99, dx: 5}"), "ends on frame 99"},
		{"projectile move", edit(matchPath, "move: gale_shot", "move: gale"), "not a looping move"},
		{"script button", edit(matchPath, "[2*2, 3*2, 6+light*2]", "[2*2, 3*2, 6+kick*2]"), "presses \"kick\""},
		{"stance", edit("data/basalt.frames.yaml", "- name: crouch_guard", "- name: kneel"), "no \"crouch_guard\" move"},
		{"move without frames", edit("data/kestrel.moves.yaml", "- name: dash", "- name: roll"), "move \"roll\" has no frame data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loadRoster(tc.fsys); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadRoster = %v, want an error with %q", err, tc.want)
			}
		})
	}
}

func TestParseScriptStep(t *testing.T) {
	step, err := parseScriptStep("3+light+heavy*4")
	if err != nil || step.dir != fighting.DownForward || step.steps != 4 || len(step.buttons) != 2 || step.buttons[1] != "heavy" {
		t.Fatalf("parseScriptStep = %+v, %v", step, err)
	}
	for _, bad := range []string{"", "0", "55", "5+", "5*", "5*x", "a+light"} {
		if _, err := parseScriptStep(bad); err == nil {
			t.Errorf("parseScriptStep(%q) parsed", bad)
		}
	}
}

// FuzzParseMatch checks that any match file parseMatch accepts encodes and parses back to the same bytes.
func FuzzParseMatch(f *testing.F) {
	good, err := fs.ReadFile(dataFS, matchPath)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(good)
	f.Add([]byte("version: 1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := parseMatch(data)
		if err != nil {
			return
		}
		out, err := encodeMatch(m)
		if err != nil {
			t.Fatalf("encodeMatch of a parsed file: %v", err)
		}
		again, err := parseMatch(out)
		if err != nil {
			t.Fatalf("parseMatch of encodeMatch's output: %v\n%s", err, out)
		}
		if out2, _ := encodeMatch(again); !bytes.Equal(out, out2) {
			t.Fatalf("the round trip changed the file:\n%s\n---\n%s", out, out2)
		}
	})
}
