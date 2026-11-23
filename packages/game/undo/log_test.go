package undo_test

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/undo"
)

const sampleLog = `version: 1
applied: 2
moves:
  - dir: right
  - dir: down
  - dir: down
`

func TestLogRoundTrip(t *testing.T) {
	start := newBoard()
	b := start.clone()
	h := undo.New[*board, *push](b)
	for _, d := range []string{"right", "down", "down"} {
		h.Do(&push{Dir: d})
	}
	h.Undo()
	want := b.clone()

	data, err := undo.EncodeLog(h.Log())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != sampleLog {
		t.Fatalf("EncodeLog wrote\n%s\nwant\n%s", data, sampleLog)
	}

	log, err := undo.DecodeLog[*push](data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := undo.EncodeLog(log)
	if err != nil || string(again) != sampleLog {
		t.Fatalf("re-encoding a decoded log changed it (err %v):\n%s", err, again)
	}

	restoredBoard := start.clone()
	r, err := undo.Restore(restoredBoard, log)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restoredBoard, want) || r.Applied() != 2 || r.Len() != 3 {
		t.Fatalf("restored applied=%d len=%d, board equal=%v", r.Applied(), r.Len(), reflect.DeepEqual(restoredBoard, want))
	}
	r.Redo()
	h.Redo()
	if !reflect.DeepEqual(restoredBoard, b) {
		t.Fatal("redo after Restore differs from redo on the original history")
	}
	r.Seek(0)
	if !reflect.DeepEqual(restoredBoard, start) {
		t.Fatal("undoing every restored move did not return to the start")
	}
}

func TestLogRoundTripRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 520))
	start := newBoard()
	b := start.clone()
	h := undo.New[*board, *push](b)
	for _, p := range randomPushes(r, 300) {
		h.Do(p)
	}
	h.Seek(180)
	want := b.clone()
	data, err := undo.EncodeLog(h.Log())
	if err != nil {
		t.Fatal(err)
	}
	log, err := undo.DecodeLog[*push](data)
	if err != nil {
		t.Fatal(err)
	}
	got := start.clone()
	rh, err := undo.Restore(got, log)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("restored board differs from the saved one")
	}
	rh.Seek(0)
	if !reflect.DeepEqual(got, start) {
		t.Fatal("undoing every restored move did not return to the start")
	}
}

func TestEmptyLog(t *testing.T) {
	h := undo.New[*board, *push](newBoard())
	data, err := undo.EncodeLog(h.Log())
	if err != nil {
		t.Fatal(err)
	}
	const want = "version: 1\napplied: 0\nmoves: []\n"
	if string(data) != want {
		t.Fatalf("empty log encoded as %q, want %q", data, want)
	}
	log, err := undo.DecodeLog[*push](data)
	if err != nil || log.Moves == nil || len(log.Moves) != 0 {
		t.Fatalf("decoded empty log %+v, err %v", log, err)
	}
}

func TestDecodeLogRefuses(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"empty":           {"  \n", "empty log"},
		"unknown field":   {"version: 1\napplied: 0\nmoves: []\nseed: 4\n", "seed"},
		"unknown in move": {"version: 1\napplied: 1\nmoves:\n  - dir: up\n    box: true\n", "box"},
		"version":         {"version: 2\napplied: 0\nmoves: []\n", "unsupported log version 2"},
		"missing version": {"applied: 0\nmoves: []\n", "unsupported log version 0"},
		"applied past":    {"version: 1\napplied: 2\nmoves:\n  - dir: up\n", "applies 2 of 1"},
		"applied below":   {"version: 1\napplied: -1\nmoves: []\n", "applies -1 of 0"},
		"null move":       {"version: 1\napplied: 0\nmoves:\n  - null\n", "move 0 is nil"},
		"trailing":        {"version: 1\napplied: 0\nmoves: []\n---\nversion: 1\n", "trailing"},
		"not yaml":        {"version: [\n", "decode log"},
	}
	for name, c := range cases {
		_, err := undo.DecodeLog[*push]([]byte(c.yaml))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want one containing %q", name, err, c.want)
		}
	}
}

func TestRestoreAndEncodeValidate(t *testing.T) {
	bad := undo.Log[*push]{Version: 1, Applied: 3, Moves: []*push{{Dir: "up"}}}
	if _, err := undo.Restore(newBoard(), bad); err == nil {
		t.Fatal("Restore accepted a log that applies more moves than it has")
	}
	if _, err := undo.EncodeLog(bad); err == nil {
		t.Fatal("EncodeLog accepted a log that applies more moves than it has")
	}
	if _, err := undo.EncodeLog(undo.Log[*push]{Version: 1, Moves: []*push{nil}}); err == nil {
		t.Fatal("EncodeLog accepted a nil move")
	}
}
