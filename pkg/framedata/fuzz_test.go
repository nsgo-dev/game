package framedata_test

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/framedata"
)

// FuzzParse checks that Parse never panics and that every document it accepts encodes to YAML that parses back to the same document and encodes to the same bytes.
func FuzzParse(f *testing.F) {
	if data, err := os.ReadFile("testdata/fighter.yaml"); err == nil {
		f.Add(data)
	}
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("version: 1\nmoves:\n  - {name: a, length: 3, loop: true, boxes: [{kind: hurt, start: 1, end: 3, width: 1, height: 1}]}\n"))
	f.Add([]byte("version: 1\nmoves:\n  - {name: a, length: 2, cancels: [{start: 1, end: 2, when: hit, into: [a]}]}\n"))
	f.Add([]byte("version: 1\nmoves:\n  - {name: a, length: 2, boxes: [{kind: hit, start: 1, end: 2, x: -0, width: 1e-30, height: 3.4e38, group: 63}]}\n"))
	f.Add([]byte("version: 1\nmoves: [{name: &x a, length: 1}, {name: *x, length: 1}]\n"))
	f.Add([]byte("{version: 1, moves: null}"))

	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := framedata.Parse(data)
		if err != nil {
			return
		}
		out, err := framedata.Encode(doc)
		if err != nil {
			t.Fatalf("Encode of a parsed document failed: %v", err)
		}
		back, err := framedata.Parse(out)
		if err != nil {
			t.Fatalf("Parse of encoded output failed: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(doc, back) {
			t.Fatalf("round trip changed the document:\n%+v\n%+v", doc, back)
		}
		again, err := framedata.Encode(back)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, again) {
			t.Fatalf("Encode is not stable:\n%s\n%s", out, again)
		}
		// A parsed document must also be safe to play: step a fighter through every move.
		f := &framedata.Fighter{Moves: doc}
		g := &framedata.Fighter{Moves: doc, FacingLeft: true}
		w := &framedata.World{Fighters: []*framedata.Fighter{f, g}}
		for i := range doc.Moves {
			_ = f.Force(doc.Moves[i].Name)
			_ = g.Force(doc.Moves[i].Name)
			for range min(doc.Moves[i].Length, 64) {
				w.Step()
			}
		}
	})
}
