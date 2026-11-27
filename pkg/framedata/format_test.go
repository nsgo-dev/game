package framedata_test

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/framedata"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func loadDoc(t testing.TB) *framedata.Doc {
	t.Helper()
	data, err := os.ReadFile("testdata/fighter.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := framedata.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

func TestRoundTripIsLossless(t *testing.T) {
	data, err := os.ReadFile("testdata/fighter.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := framedata.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := framedata.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("Encode did not reproduce the document:\n--- got ---\n%s\n--- want ---\n%s", out, data)
	}
	again, err := framedata.Parse(out)
	if err != nil {
		t.Fatalf("Parse(Encode): %v", err)
	}
	if !reflect.DeepEqual(doc, again) {
		t.Fatalf("round trip changed the document:\n%+v\n%+v", doc, again)
	}
}

func TestMoveLookupAndFrameData(t *testing.T) {
	doc := loadDoc(t)
	jab, ok := doc.Move("jab")
	if !ok || jab.Name != "jab" || jab.Animation != "jab" {
		t.Fatalf("Move(jab) = %+v, %v", jab, ok)
	}
	if _, ok := doc.Move("missing"); ok {
		t.Fatal("Move(missing) found a move")
	}
	fd, ok := jab.FrameData()
	want := framedata.FrameData{Startup: 4, Active: 3, Recovery: 8, OnHit: 4, OnBlock: -1}
	if !ok || fd != want {
		t.Fatalf("jab FrameData = %+v, %v; want %+v", fd, ok, want)
	}
	double, _ := doc.Move("double")
	fd, _ = double.FrameData()
	if want := (framedata.FrameData{Startup: 5, Active: 6, Recovery: 10, OnHit: -3, OnBlock: -9}); fd != want {
		t.Fatalf("double FrameData = %+v, want %+v", fd, want)
	}
	idle, _ := doc.Move("idle")
	if _, ok := idle.FrameData(); ok {
		t.Fatal("idle has no hitbox but FrameData reported one")
	}
}

func TestParseRefusesInvalidDocuments(t *testing.T) {
	const hurt = "      - {kind: hurt, start: 1, end: 4, width: 10, height: 10}\n"
	cases := []struct {
		name, yaml, want string
	}{
		{"empty", "", "empty document"},
		{"version", "version: 2\n", "unsupported version 2"},
		{"unknown root field", "version: 1\ncolour: red\n", "colour"},
		{"unknown box field", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n      - {kind: hurt, start: 1, end: 4, width: 10, height: 10, spin: 3}\n", "spin"},
		{"trailing document", "version: 1\n---\nversion: 1\n", "trailing"},
		{"empty name", "version: 1\nmoves:\n  - length: 4\n", "empty name"},
		{"duplicate name", "version: 1\nmoves:\n  - {name: a, length: 4}\n  - {name: a, length: 4}\n", "duplicate move name"},
		{"zero length", "version: 1\nmoves:\n  - {name: a, length: 0}\n", "length 0"},
		{"long length", "version: 1\nmoves:\n  - {name: a, length: 3601}\n", "length 3601"},
		{"bad kind", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n      - {kind: push, start: 1, end: 4, width: 10, height: 10}\n", "unknown kind"},
		{"box past end", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n      - {kind: hurt, start: 2, end: 5, width: 10, height: 10}\n", "start <= end"},
		{"box zero start", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n      - {kind: hurt, start: 0, end: 2, width: 10, height: 10}\n", "start <= end"},
		{"box reversed", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n      - {kind: hurt, start: 3, end: 2, width: 10, height: 10}\n", "start <= end"},
		{"box zero width", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n      - {kind: hurt, start: 1, end: 4, width: 0, height: 10}\n", "must be positive"},
		{"box nan", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n      - {kind: hurt, start: 1, end: 4, x: .nan, width: 10, height: 10}\n", "finite"},
		{"hurtbox damage", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n      - {kind: hurt, start: 1, end: 4, width: 10, height: 10, damage: 5}\n", "hurtbox"},
		{"group range", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n      - {kind: hit, start: 1, end: 4, width: 10, height: 10, group: 64}\n", "group 64"},
		{"negative damage", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n      - {kind: hit, start: 1, end: 4, width: 10, height: 10, damage: -1}\n", "negative"},
		{"huge hitstun", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n      - {kind: hit, start: 1, end: 4, width: 10, height: 10, hitstun: 99999}\n", "exceed"},
		{"knockback inf", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n      - {kind: hit, start: 1, end: 4, width: 10, height: 10, knockbackY: .inf}\n", "knockback"},
		{"cancel range", "version: 1\nmoves:\n  - name: a\n    length: 4\n    boxes:\n" + hurt + "    cancels:\n      - {start: 3, end: 9, into: [a]}\n", "start <= end"},
		{"cancel condition", "version: 1\nmoves:\n  - name: a\n    length: 4\n    cancels:\n      - {start: 1, end: 2, when: whiff, into: [a]}\n", "unknown condition"},
		{"cancel empty", "version: 1\nmoves:\n  - name: a\n    length: 4\n    cancels:\n      - {start: 1, end: 2, into: []}\n", "names no moves"},
		{"cancel unknown move", "version: 1\nmoves:\n  - name: a\n    length: 4\n    cancels:\n      - {start: 1, end: 2, into: [b]}\n", "unknown move \"b\""},
		{"cancel duplicate move", "version: 1\nmoves:\n  - name: a\n    length: 4\n    cancels:\n      - {start: 1, end: 2, into: [a, a]}\n", "twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := framedata.Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("Parse accepted %q", tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestEncodeValidates(t *testing.T) {
	if _, err := framedata.Encode(nil); err == nil {
		t.Fatal("Encode(nil) succeeded")
	}
	if _, err := framedata.Encode(&framedata.Doc{Version: 1, Moves: []framedata.Move{{Name: "a"}}}); err == nil {
		t.Fatal("Encode accepted a move of length 0")
	}
}

func TestEncodeIsDeterministic(t *testing.T) {
	doc := &framedata.Doc{Version: 1, Moves: []framedata.Move{{
		Name:   "kick",
		Length: 10,
		Boxes: []framedata.Box{
			{Kind: framedata.KindHit, Start: 3, End: 5, X: 0.1, Y: -1.5, Width: 1.0 / 3, Height: 7, Damage: 1},
		},
		Cancels: []framedata.Cancel{{Start: 3, End: 9, When: framedata.OnHit, Into: []string{"kick"}}},
	}}}
	first, err := framedata.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		again, err := framedata.Encode(doc)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("Encode is not deterministic:\n%s\n%s", first, again)
		}
	}
	back, err := framedata.Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc, back) {
		t.Fatalf("float32 fields did not survive a round trip:\n%+v\n%+v", doc, back)
	}
}
