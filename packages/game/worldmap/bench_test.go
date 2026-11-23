package worldmap_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/worldmap"
	"github.com/nsgo-dev/nsgo/core"
)

// gridMap returns a side*side grid of 100x60 rooms, each linked to its right and lower neighbours, with every other room visited.
func gridMap(b *testing.B, side int) *worldmap.Map {
	b.Helper()
	doc := &worldmap.Doc{Version: worldmap.Version}
	id := func(x, y int) string { return "r" + strconv.Itoa(x) + "_" + strconv.Itoa(y) }
	for y := range side {
		for x := range side {
			r := worldmap.RoomDoc{ID: id(x, y), X: float32(x) * 100, Y: float32(y) * 60, Width: 100, Height: 60}
			if x+1 < side {
				r.Links = append(r.Links, id(x+1, y))
			}
			if y+1 < side {
				r.Links = append(r.Links, id(x, y+1))
			}
			doc.Rooms = append(doc.Rooms, r)
		}
	}
	m, err := worldmap.New(doc)
	if err != nil {
		b.Fatal(err)
	}
	for i, r := range m.Rooms() {
		if i%2 == 0 {
			m.Visit(r.Bounds.Center())
		}
	}
	return m
}

func BenchmarkNew1024Rooms(b *testing.B) {
	m := gridMap(b, 32)
	doc := &worldmap.Doc{Version: worldmap.Version}
	for _, r := range m.Rooms() {
		doc.Rooms = append(doc.Rooms, worldmap.RoomDoc{ID: r.ID, X: r.Bounds.X, Y: r.Bounds.Y, Width: r.Bounds.W, Height: r.Bounds.H})
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := worldmap.New(doc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVisitSameRoom(b *testing.B) {
	m := gridMap(b, 32)
	p := math2d.Vec2{X: 1650, Y: 930}
	m.Visit(p)
	b.ReportAllocs()
	for b.Loop() {
		m.Visit(p)
	}
}

func BenchmarkVisitNewRoom1024(b *testing.B) {
	m := gridMap(b, 32)
	a, c := math2d.Vec2{X: 50, Y: 30}, math2d.Vec2{X: 3150, Y: 1890}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if i%2 == 0 {
			m.Visit(a)
		} else {
			m.Visit(c)
		}
	}
}

func BenchmarkDrawWorldMap1024Rooms(b *testing.B) {
	m := gridMap(b, 32)
	dst := core.Bounds{Size: core.Size{Width: 640, Height: 360}}
	style := worldmap.DefaultStyle()
	var d core.Drawing
	b.ReportAllocs()
	for b.Loop() {
		d.Ops = d.Ops[:0]
		m.DrawWorldMap(&d, dst, "", nil, &style)
	}
}

func BenchmarkDrawMinimap1024Rooms(b *testing.B) {
	m := gridMap(b, 32)
	dst := core.Bounds{Size: core.Size{Width: 160, Height: 160}}
	style := worldmap.DefaultStyle()
	player := math2d.Vec2{X: 1650, Y: 930}
	var d core.Drawing
	b.ReportAllocs()
	for b.Loop() {
		d.Ops = d.Ops[:0]
		m.DrawMinimap(&d, dst, player, 0.5, &style)
	}
}

func BenchmarkStateRoundTrip1024Rooms(b *testing.B) {
	m := gridMap(b, 32)
	other := gridMap(b, 32)
	b.ReportAllocs()
	for b.Loop() {
		data, err := json.Marshal(m.State())
		if err != nil {
			b.Fatal(err)
		}
		var s worldmap.State
		if err := json.Unmarshal(data, &s); err != nil {
			b.Fatal(err)
		}
		if err := other.Restore(s); err != nil {
			b.Fatal(err)
		}
	}
}
