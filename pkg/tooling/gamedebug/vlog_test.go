//go:build !release

package gamedebug_test

import (
	"bytes"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
)

type statsCategory struct{}

func (statsCategory) Name() string { return "stats" }
func (statsCategory) Snapshot(entity string) []gamedebug.Field {
	return []gamedebug.Field{{Name: "owner", Value: entity}, {Name: "health", Value: "40"}}
}

func TestVLogFiltersByEntityCategoryAndStep(t *testing.T) {
	l := gamedebug.NewVLog(8)
	l.Log(1, "enemy", "ai", gamedebug.VLogInfo, "patrol")
	l.Log(2, "enemy", "ai", gamedebug.VLogWarn, "blocked")
	l.Log(2, "enemy", "nav", gamedebug.VLogInfo, "repath")
	l.Log(3, "player", "ai", gamedebug.VLogInfo, "idle")

	got := l.Entries("enemy", "ai", 2, 2)
	want := []gamedebug.VLogEntry{{Step: 2, Entity: "enemy", Category: "ai", Level: gamedebug.VLogWarn, Message: "blocked"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Entries(enemy, ai, 2, 2) = %+v, want %+v", got, want)
	}
	if got := l.Entries("", "", 2, 0); len(got) != 3 {
		t.Fatalf("Entries from step 2 with no upper bound = %d entries, want 3", len(got))
	}
	if got := l.Entries("", "ai", 0, 0); len(got) != 3 {
		t.Fatalf("Entries(any, ai) = %d entries, want 3", len(got))
	}
}

func TestVLogDropsOldestOnceFull(t *testing.T) {
	l := gamedebug.NewVLog(3)
	for step := uint64(1); step <= 5; step++ {
		l.Log(step, "enemy", "ai", gamedebug.VLogInfo, "tick")
		l.Shape(step, "enemy", "point", [][3]float32{{float32(step), 0, 0}})
	}
	var steps []uint64
	for _, e := range l.Entries("", "", 0, 0) {
		steps = append(steps, e.Step)
	}
	if !reflect.DeepEqual(steps, []uint64{3, 4, 5}) {
		t.Fatalf("entry steps after overflow = %v, want [3 4 5]", steps)
	}
	shapes := l.Shapes("", 0, 0)
	if len(shapes) != 3 || shapes[0].Step != 3 || shapes[2].Step != 5 {
		t.Fatalf("shapes after overflow = %+v, want steps 3..5", shapes)
	}
}

func TestVLogShapeCopiesPoints(t *testing.T) {
	l := gamedebug.NewVLog(4)
	points := [][3]float32{{1, 2, 3}}
	l.Shape(1, "enemy", "path", points)
	points[0] = [3]float32{9, 9, 9}
	if got := l.Shapes("enemy", 1, 1)[0].Points[0]; got != [3]float32{1, 2, 3} {
		t.Fatalf("recorded point = %v after caller reused its slice, want [1 2 3]", got)
	}
}

func TestVLogJSONLRoundTrip(t *testing.T) {
	l := gamedebug.NewVLog(8)
	l.Log(2, "enemy", "ai", gamedebug.VLogWarn, "blocked")
	l.Snapshot(2, "enemy", statsCategory{})
	l.Shape(2, "enemy", "path", [][3]float32{{1, 2, 3}, {4, 5, 6}})

	var first bytes.Buffer
	if err := l.WriteJSONL(&first); err != nil {
		t.Fatal(err)
	}
	wantText := `{"format":"gamedebug.vlog","version":1}
{"type":"entry","step":2,"entity":"enemy","category":"ai","level":"warn","message":"blocked"}
{"type":"entry","step":2,"entity":"enemy","category":"stats","level":"info","fields":[{"name":"owner","value":"enemy"},{"name":"health","value":"40"}]}
{"type":"shape","step":2,"entity":"enemy","kind":"path","points":[[1,2,3],[4,5,6]]}
`
	if first.String() != wantText {
		t.Fatalf("WriteJSONL =\n%s\nwant\n%s", first.String(), wantText)
	}

	entries, shapes, err := gamedebug.ReadVLog(bytes.NewReader(first.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(entries, l.Entries("", "", 0, 0)) || !reflect.DeepEqual(shapes, l.Shapes("", 0, 0)) {
		t.Fatalf("ReadVLog = %+v, %+v; want the recorded entries and shapes", entries, shapes)
	}

	again := gamedebug.NewVLog(8)
	for _, e := range entries {
		if e.Fields != nil {
			again.Snapshot(e.Step, e.Entity, statsCategory{})
			continue
		}
		again.Log(e.Step, e.Entity, e.Category, e.Level, e.Message)
	}
	for _, s := range shapes {
		again.Shape(s.Step, s.Entity, s.Kind, s.Points)
	}
	var second bytes.Buffer
	if err := again.WriteJSONL(&second); err != nil {
		t.Fatal(err)
	}
	if second.String() != first.String() {
		t.Fatalf("second save differs from the first:\n%s\nvs\n%s", second.String(), first.String())
	}
}

func TestReadVLogRefusesMalformedInput(t *testing.T) {
	header := `{"format":"gamedebug.vlog","version":1}` + "\n"
	cases := map[string]string{
		"empty":         "",
		"no header":     `{"type":"entry","step":1,"entity":"e","category":"ai","level":"info","message":"m"}`,
		"wrong version": `{"format":"gamedebug.vlog","version":2}`,
		"unknown type":  header + `{"type":"sound","step":1}`,
		"unknown field": header + `{"type":"entry","step":1,"entity":"e","category":"ai","level":"info","message":"m","color":"red"}`,
		"unknown level": header + `{"type":"entry","step":1,"entity":"e","category":"ai","level":"debug","message":"m"}`,
		"not json":      header + `{"type":`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := gamedebug.ReadVLog(strings.NewReader(in)); err == nil {
				t.Fatalf("ReadVLog(%q) succeeded, want an error", in)
			}
		})
	}
}

func TestVLogConcurrentUse(t *testing.T) {
	l := gamedebug.NewVLog(16)
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			for step := range uint64(100) {
				l.Log(step, "enemy", "ai", gamedebug.VLogInfo, "tick")
				l.Shape(step, "enemy", "point", [][3]float32{{float32(i), 0, 0}})
				_ = l.Entries("enemy", "", 0, 0)
				_ = l.String()
			}
		})
	}
	wg.Wait()
	if entries, shapes := l.Len(); entries != 16 || shapes != 16 {
		t.Fatalf("Len() = %d, %d, want 16, 16", entries, shapes)
	}
}

func TestNilVLogIsNoOp(t *testing.T) {
	var l *gamedebug.VLog
	l.Log(1, "e", "ai", gamedebug.VLogInfo, "m")
	l.Snapshot(1, "e", statsCategory{})
	l.Shape(1, "e", "point", nil)
	if l.Entries("", "", 0, 0) != nil || l.Shapes("", 0, 0) != nil {
		t.Fatal("nil VLog returned records")
	}
}
