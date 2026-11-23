package worldmap_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/worldmap"
	"github.com/nsgo-dev/nsgo/core"
)

func FuzzParse(f *testing.F) {
	if data, err := os.ReadFile("testdata/castle.yaml"); err == nil {
		f.Add(data)
	}
	f.Add([]byte("version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: 1, height: 1}\n"))
	f.Add([]byte("version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: 1, height: 1, links: [b]}\n  - {id: b, x: 1, y: 0, width: 1, height: 1}\nmarkers:\n  - {id: m, kind: k, room: b, x: 1.5, y: 0.5}\n"))
	f.Add([]byte("version: 1\nrooms: [{id: a, x: -1e30, y: 1e30, width: 1e-30, height: 3}]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := worldmap.Parse(data)
		if err != nil {
			return
		}
		out, err := worldmap.Encode(doc)
		if err != nil {
			t.Fatalf("Encode of a parsed document: %v", err)
		}
		again, err := worldmap.Parse(out)
		if err != nil {
			t.Fatalf("Parse(Encode(doc)): %v\n%s", err, out)
		}
		out2, err := worldmap.Encode(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("encoding is not stable (err %v):\n%s\n---\n%s", err, out, out2)
		}
		m, err := worldmap.New(doc)
		if err != nil {
			t.Fatalf("New of a valid document: %v", err)
		}
		for _, r := range m.Rooms() {
			m.Visit(r.Bounds.Center())
		}
		for _, area := range m.Areas() {
			var d core.Drawing
			m.DrawWorldMap(&d, core.Bounds{Size: core.Size{Width: 64, Height: 64}}, area, nil, nil)
		}
	})
}

func FuzzRestoreState(f *testing.F) {
	f.Add([]byte(`{"version":1,"current":"hall","visited":["entrance","hall"],"revealed":["tower"],"markers":[{"id":"p","kind":"k","room":"hall","x":400,"y":10}]}`))
	f.Add([]byte(`{"version":1,"visited":[]}`))
	f.Add([]byte(`{"version":1,"visited":["hall","hall"]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var s worldmap.State
		if err := json.Unmarshal(data, &s); err != nil {
			return
		}
		m := loadCastle(t)
		if err := m.Restore(s); err != nil {
			if !reflect.DeepEqual(m.State(), worldmap.State{Version: 1, Visited: []string{}}) {
				t.Fatal("a failed Restore changed the map")
			}
			return
		}
		got := m.State()
		other := loadCastle(t)
		if err := other.Restore(got); err != nil {
			t.Fatalf("Restore of the map's own state: %v", err)
		}
		if !reflect.DeepEqual(other.State(), got) {
			t.Fatalf("state is not stable: %+v vs %+v", other.State(), got)
		}
	})
}
