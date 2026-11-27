package undo_test

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/undo"
)

const sampleLog = `version: 2
applied: 2
forgotten: 0
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

func TestLogAfterClearRestoresOntoClearedState(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 520))
	b := newBoard()
	h := undo.New[*board, *push](b)
	for _, p := range randomPushes(r, 40) {
		h.Do(p)
	}
	h.Clear()
	checkpoint := b.clone()
	for _, p := range randomPushes(r, 60) {
		h.Do(p)
	}
	h.Seek(45)
	want := b.clone()

	data, err := undo.EncodeLog(h.Log())
	if err != nil {
		t.Fatal(err)
	}
	log, err := undo.DecodeLog[*push](data)
	if err != nil {
		t.Fatal(err)
	}
	got := checkpoint.clone()
	rh, err := undo.Restore(got, log)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || rh.Applied() != 45 || rh.Len() != 60 {
		t.Fatalf("restored onto the cleared state: applied=%d len=%d, board equal=%v", rh.Applied(), rh.Len(), reflect.DeepEqual(got, want))
	}
	rh.Seek(0)
	if !reflect.DeepEqual(got, checkpoint) {
		t.Fatal("undoing every restored move did not return to the state at Clear")
	}
}

func TestEmptyLog(t *testing.T) {
	h := undo.New[*board, *push](newBoard())
	data, err := undo.EncodeLog(h.Log())
	if err != nil {
		t.Fatal(err)
	}
	const want = "version: 2\napplied: 0\nforgotten: 0\nmoves: []\n"
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
		"version":         {"version: 3\napplied: 0\nmoves: []\n", "unsupported log version 3"},
		"v1 forgotten":    {"version: 1\napplied: 0\nforgotten: 2\nmoves: []\n", "version 1 records 2 forgotten"},
		"forgotten below": {"version: 2\napplied: 0\nforgotten: -1\nmoves: []\n", "forgets -1"},
		"missing version": {"applied: 0\nmoves: []\n", "no version"},
		"zero version":    {"version: 0\napplied: 0\nmoves: []\n", "unsupported log version 0"},
		"missing applied": {"version: 1\nmoves:\n  - dir: up\n", "no applied"},
		"missing moves":   {"version: 1\napplied: 0\n", "no moves"},
		"null moves":      {"version: 1\napplied: 0\nmoves: null\n", "moves is not a list"},
		"not a mapping":   {"- 1\n", "cannot use a !!seq"},
		"applied past":    {"version: 1\napplied: 2\nmoves:\n  - dir: up\n", "applies 2 of 1"},
		"applied below":   {"version: 1\napplied: -1\nmoves: []\n", "applies -1 of 0"},
		"null move":       {"version: 1\napplied: 0\nmoves:\n  - null\n", "move 0 is nil"},
		"trailing":        {"version: 1\napplied: 0\nmoves: []\n---\nversion: 1\n", "trailing"},
		"not yaml":        {"version: [\n", "invalid YAML"},
	}
	for name, c := range cases {
		_, err := undo.DecodeLog[*push]([]byte(c.yaml))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want one containing %q", name, err, c.want)
		}
	}
}

func TestDecodeLogReadsVersion1(t *testing.T) {
	const v1 = `version: 1
applied: 2
moves:
  - dir: right
  - dir: down
  - dir: down
`
	log, err := undo.DecodeLog[*push]([]byte(v1))
	if err != nil {
		t.Fatal(err)
	}
	if log.Forgotten != 0 || log.Applied != 2 || len(log.Moves) != 3 {
		t.Fatalf("decoded version 1 log as %+v", log)
	}
	data, err := undo.EncodeLog(log)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != sampleLog {
		t.Fatalf("a version 1 log saved again became\n%s\nwant the current form\n%s", data, sampleLog)
	}
}

func TestLogWithForgottenMoves(t *testing.T) {
	x := 0
	h := undo.New[*int, *stepBy](&x)
	h.Limit = 3
	for i := 1; i <= 5; i++ {
		h.Do(&stepBy{D: i})
	}
	log := h.Log()
	if log.Forgotten != 2 || log.Applied != 3 || len(log.Moves) != 3 {
		t.Fatalf("Log after forgetting gave %+v, want 2 forgotten of 3 moves, all applied", log)
	}
	data, err := undo.EncodeLog(log)
	if err != nil {
		t.Fatal(err)
	}
	back, err := undo.DecodeLog[*stepBy](data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Forgotten != 2 || !reflect.DeepEqual(back, log) {
		t.Fatalf("forgotten count did not round trip: %+v, want %+v", back, log)
	}
	if _, err := undo.Restore(new(int), back); err == nil || !strings.Contains(err.Error(), "forgot 2 moves") {
		t.Fatalf("Restore of a log with forgotten moves gave %v, want a refusal", err)
	}
	h.Clear()
	if log := h.Log(); log.Forgotten != 0 {
		t.Fatalf("Log after Clear forgets %d moves, want 0", log.Forgotten)
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
