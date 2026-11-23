package fog_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/fog"
	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const (
	blue fog.Team = 1
	red  fog.Team = 2
)

// picture renders team's view of f as rows of 'v' (visible), 'e' (explored) and '.' (unseen), with '#' for opaque cells the team cannot see.
func picture(f *fog.Fog, team fog.Team) string {
	var b strings.Builder
	for row := range f.Height() {
		for col := range f.Width() {
			switch f.State(team, col, row) {
			case fog.Visible:
				b.WriteByte('v')
			case fog.Explored:
				b.WriteByte('e')
			default:
				if f.Opaque(col, row) {
					b.WriteByte('#')
				} else {
					b.WriteByte('.')
				}
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func wantPicture(t *testing.T, f *fog.Fog, team fog.Team, want string) {
	t.Helper()
	want = strings.TrimLeft(want, "\n")
	if got := picture(f, team); got != want {
		t.Fatalf("team %d sees:\n%s\nwant:\n%s", team, got, want)
	}
}

func TestVisibilityFollowsMovingUnit(t *testing.T) {
	f := fog.New(10, 5, 16, 16)
	scout := f.AddSource(fog.Source{Team: blue, Col: 1, Row: 2, Radius: 1})
	if !f.Update() {
		t.Fatal("first Update reported no change")
	}
	wantPicture(t, f, blue, `
..........
vvv.......
vvv.......
vvv.......
..........
`)
	if f.Update() {
		t.Fatal("Update with nothing moved reported a change")
	}

	rev := f.Revision(blue)
	if !f.Move(scout, 5, 2) {
		t.Fatal("Move lost the scout")
	}
	if !f.Update() || f.Revision(blue) == rev {
		t.Fatal("moving the scout did not change the fog")
	}
	wantPicture(t, f, blue, `
..........
eee.vvv...
eee.vvv...
eee.vvv...
..........
`)

	if !f.MoveTo(scout, core.Point{X: 8*16 + 3, Y: 2*16 + 15}) {
		t.Fatal("MoveTo lost the scout")
	}
	f.Update()
	wantPicture(t, f, blue, `
..........
eee.eeevvv
eee.eeevvv
eee.eeevvv
..........
`)
	if !f.VisibleAt(blue, core.Point{X: 8 * 16, Y: 16}) || f.VisibleAt(blue, core.Point{X: 16, Y: 16}) {
		t.Fatal("VisibleAt disagrees with the fog")
	}
	if f.Visible(red, 8, 2) {
		t.Fatal("red sees through blue's scout")
	}

	if !f.RemoveSource(scout) {
		t.Fatal("RemoveSource lost the scout")
	}
	f.Update()
	wantPicture(t, f, blue, `
..........
eee.eeeeee
eee.eeeeee
eee.eeeeee
..........
`)
	if got := f.ExploredCount(blue); got != 27 {
		t.Fatalf("ExploredCount = %d, want 27", got)
	}
}

func TestRadiusIsARoundedDisc(t *testing.T) {
	f := fog.New(7, 7, 1, 1)
	f.AddSource(fog.Source{Team: blue, Col: 3, Row: 3, Radius: 2})
	f.Update()
	wantPicture(t, f, blue, `
.......
..vvv..
.vvvvv.
.vvvvv.
.vvvvv.
..vvv..
.......
`)
}

func TestWallsBlockSight(t *testing.T) {
	f := fog.New(9, 5, 1, 1)
	for row := range 5 {
		if row != 2 {
			f.SetOpaque(4, row, true)
		}
	}
	f.AddSource(fog.Source{Team: blue, Col: 1, Row: 2, Radius: 8})
	f.Update()
	wantPicture(t, f, blue, `
vvvvv....
vvvvv..vv
vvvvvvvvv
vvvvv..vv
vvvvv....
`)

	// Widening the doorway to three cells opens the whole room beyond it at the next Update.
	f.SetOpaque(4, 1, false)
	f.SetOpaque(4, 3, false)
	if !f.Update() {
		t.Fatal("opening the wall did not change the fog")
	}
	wantPicture(t, f, blue, `
vvvvvvvvv
vvvvvvvvv
vvvvvvvvv
vvvvvvvvv
vvvvvvvvv
`)
}

func TestTeamsShareVisionOnlyWithinATeam(t *testing.T) {
	f := fog.New(6, 1, 1, 1)
	f.AddSource(fog.Source{Team: blue, Col: 0, Row: 0, Radius: 1})
	f.AddSource(fog.Source{Team: blue, Col: 5, Row: 0, Radius: 0})
	spy := f.AddSource(fog.Source{Team: red, Col: 3, Row: 0, Radius: 0})
	f.Update()
	wantPicture(t, f, blue, "vv...v\n")
	wantPicture(t, f, red, "...v..\n")

	// Defecting moves the spy's vision to blue.
	s, _ := f.Source(spy)
	s.Team = blue
	f.SetSource(spy, s)
	f.Update()
	wantPicture(t, f, blue, "vv.v.v\n")
	wantPicture(t, f, red, "...e..\n")
	if got := f.Teams(); len(got) != 2 || got[0] != blue || got[1] != red {
		t.Fatalf("Teams = %v", got)
	}
}

func TestExploreAndForget(t *testing.T) {
	f := fog.New(4, 3, 1, 1)
	f.AddSource(fog.Source{Team: blue, Col: 0, Row: 0, Radius: 0})
	f.Update()
	rev := f.Revision(blue)
	f.ExploreRect(blue, 2, 1, 5, 5)
	f.Explore(blue, -1, 0)
	if f.Revision(blue) == rev {
		t.Fatal("ExploreRect did not bump the revision")
	}
	wantPicture(t, f, blue, `
v...
..ee
..ee
`)
	f.Forget(blue)
	wantPicture(t, f, blue, `
v...
....
....
`)
	f.ExploreAll(red)
	if f.ExploredCount(red) != 12 || f.Visible(red, 0, 0) {
		t.Fatal("ExploreAll should explore every cell without making any visible")
	}
}

func TestExploreRectClipsANegativeOrigin(t *testing.T) {
	f := fog.New(10, 3, 1, 1)
	f.ExploreRect(blue, -5, 0, 1000, 1)
	f.ExploreRect(red, -5, -2, 6, 4)
	f.ExploreRect(red, -5, 2, 5, 1)
	wantPicture(t, f, blue, `
eeeeeeeeee
..........
..........
`)
	wantPicture(t, f, red, `
e.........
e.........
..........
`)
}

func TestSourceOutsideTheGridSeesNothing(t *testing.T) {
	f := fog.New(3, 3, 1, 1)
	id := f.AddSource(fog.Source{Team: blue, Col: -1, Row: 1, Radius: 5})
	f.Update()
	if f.ExploredCount(blue) != 0 {
		t.Fatal("a source off the grid revealed cells")
	}
	if f.MoveTo(id, core.Point{X: 1e30, Y: 1}) {
		f.Update()
	}
	if f.ExploredCount(blue) != 0 {
		t.Fatal("a source moved far off the grid revealed cells")
	}
	if f.Move(99, 1, 1) || f.RemoveSource(99) || f.SetSource(99, fog.Source{}) {
		t.Fatal("an unknown source ID was accepted")
	}
}

func TestNewFromTilemap(t *testing.T) {
	m := tilemap.New(3, 2, 32, 16)
	m.SetCellFlags(1, 0, tilemap.TileSolid)
	m.SetCellFlags(2, 1, tilemap.TileOneWay)
	f := fog.NewFromTilemap(m, tilemap.TileSolid)
	if f.Width() != 3 || f.Height() != 2 || f.TileWidth != 32 || f.TileHeight != 16 {
		t.Fatalf("size %dx%d tiles %vx%v", f.Width(), f.Height(), f.TileWidth, f.TileHeight)
	}
	if !f.Opaque(1, 0) || f.Opaque(2, 1) || f.Opaque(0, 0) {
		t.Fatal("opaque cells do not follow the TileSolid mask")
	}
	if col, row, ok := f.CellAt(core.Point{X: 70, Y: 20}); !ok || col != 2 || row != 1 {
		t.Fatalf("CellAt = %d,%d,%v", col, row, ok)
	}
}

func TestNewRefusesBadSizes(t *testing.T) {
	for _, size := range [][2]int{{-1, 1}, {1, -1}, {fog.MaxCells, 2}, {0, 100_000_000_000}, {100_000_000_000, 0}, {0, 1}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("New(%d, %d) did not panic", size[0], size[1])
				}
			}()
			fog.New(size[0], size[1], 1, 1)
		}()
	}
}

func TestDraw(t *testing.T) {
	f := fog.New(4, 2, 10, 20)
	f.AddSource(fog.Source{Team: blue, Col: 0, Row: 0, Radius: 0})
	f.Update()
	f.Explore(blue, 1, 0)
	style := fog.Style{Unseen: core.RGB(0x000000), Explored: core.RGBA(0x000000, 0.5)}

	var d core.Drawing
	f.Draw(&d, blue, nil, style)
	want := []core.DrawOp{
		{Kind: core.DrawSave},
		{Kind: core.DrawFillColor, Color: style.Unseen},
		{Kind: core.DrawBeginPath},
		{Kind: core.DrawRect, X: 20, Y: 0, W: 20, H: 20},
		{Kind: core.DrawRect, X: 0, Y: 20, W: 40, H: 20},
		{Kind: core.DrawFill},
		{Kind: core.DrawFillColor, Color: style.Explored},
		{Kind: core.DrawBeginPath},
		{Kind: core.DrawRect, X: 10, Y: 0, W: 10, H: 20},
		{Kind: core.DrawFill},
		{Kind: core.DrawRestore},
	}
	if len(d.Ops) != len(want) {
		t.Fatalf("%d ops, want %d: %+v", len(d.Ops), len(want), d.Ops)
	}
	for i, w := range want {
		g := d.Ops[i]
		if g.Kind != w.Kind || g.Color != w.Color || g.X != w.X || g.Y != w.Y || g.W != w.W || g.H != w.H {
			t.Fatalf("op %d = %+v, want %+v", i, g, w)
		}
	}

	// A camera over the right half draws only the cells it shows.
	d = core.Drawing{}
	f.Draw(&d, blue, &tilemap.Camera{X: 30, Y: 20, Width: 20, Height: 40}, style)
	rects := 0
	for _, op := range d.Ops {
		if op.Kind == core.DrawRect {
			rects++
			if op.X < 20 {
				t.Fatalf("culled draw covered x=%v", op.X)
			}
		}
	}
	if rects != 2 {
		t.Fatalf("culled draw made %d rects, want 2: %+v", rects, d.Ops)
	}

	// A fully visible view and a transparent style draw nothing.
	d = core.Drawing{}
	f.Draw(&d, blue, &tilemap.Camera{X: 5, Y: 10, Width: 10, Height: 20}, style)
	f.Draw(&d, red, nil, fog.Style{})
	if len(d.Ops) != 0 {
		t.Fatalf("expected no ops, got %+v", d.Ops)
	}
}

// savedGame is the shape a game might save: its own data with the fog inside.
type savedGame struct {
	Turn  int      `json:"turn"`
	Scout uint32   `json:"scout"`
	Fog   *fog.Fog `json:"fog"`
}

func TestFogPersistsThroughASave(t *testing.T) {
	f := fog.New(8, 4, 16, 16)
	f.SetOpaque(4, 0, true)
	f.SetOpaque(4, 1, true)
	scout := f.AddSource(fog.Source{Team: blue, Col: 1, Row: 1, Radius: 3})
	f.AddSource(fog.Source{Team: red, Col: 7, Row: 3, Radius: 1})
	f.Update()
	f.Move(scout, 2, 3)
	f.Update()
	f.Explore(red, 0, 0)

	store := save.NewFileStore(t.TempDir())
	if err := store.Save("slot1", savedGame{Turn: 9, Scout: uint32(scout), Fog: f}); err != nil {
		t.Fatal(err)
	}
	var loaded savedGame
	if err := store.Load("slot1", &loaded); err != nil {
		t.Fatal(err)
	}
	g := loaded.Fog
	for _, team := range []fog.Team{blue, red} {
		if got, want := picture(g, team), picture(f, team); got != want {
			t.Fatalf("team %d after load:\n%s\nwant:\n%s", team, got, want)
		}
	}
	a, _ := json.Marshal(f)
	b, _ := json.Marshal(g)
	if !bytes.Equal(a, b) {
		t.Fatalf("round trip changed the snapshot:\n%s\n%s", a, b)
	}

	// The loaded fog keeps the source IDs the game saved and goes on updating as units move.
	id := fog.SourceID(loaded.Scout)
	if !g.Move(id, 6, 1) {
		t.Fatal("the saved scout ID is gone after load")
	}
	g.Update()
	if !g.Visible(blue, 6, 1) || g.Visible(blue, 2, 3) || !g.Explored(blue, 2, 3) {
		t.Fatalf("loaded fog did not follow the scout:\n%s", picture(g, blue))
	}
	if next := g.AddSource(fog.Source{Team: red}); next != 3 {
		t.Fatalf("next source ID after load = %d, want 3", next)
	}
}

func TestRevisionChangesAcrossALoad(t *testing.T) {
	f := fog.New(4, 1, 1, 1)
	f.AddSource(fog.Source{Team: blue, Col: 0, Row: 0, Radius: 0})
	f.Update()

	// A snapshot with the same blue layer as f, and a red team that has explored cells but no sources.
	src := fog.New(4, 1, 1, 1)
	src.AddSource(fog.Source{Team: blue, Col: 0, Row: 0, Radius: 0})
	src.Update()
	src.ExploreAll(red)
	store := save.NewFileStore(t.TempDir())
	if err := store.Save("slot1", savedGame{Fog: src}); err != nil {
		t.Fatal(err)
	}

	game := savedGame{Fog: f}
	blueBefore, redBefore := f.Revision(blue), f.Revision(red)
	if err := store.Load("slot1", &game); err != nil {
		t.Fatal(err)
	}
	g := game.Fog
	if g.ExploredCount(red) != 4 {
		t.Fatalf("red explored %d cells after load, want 4", g.ExploredCount(red))
	}
	if g.Revision(blue) == blueBefore || g.Revision(red) == redBefore {
		t.Fatalf("revisions after store.Load: blue %d (was %d), red %d (was %d)", g.Revision(blue), blueBefore, g.Revision(red), redBefore)
	}

	// Loading directly into the same Fog changes them again.
	data, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	blueBefore, redBefore = g.Revision(blue), g.Revision(red)
	if err := g.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	if g.Revision(blue) <= blueBefore || g.Revision(red) <= redBefore {
		t.Fatalf("revisions after UnmarshalJSON: blue %d (was %d), red %d (was %d)", g.Revision(blue), blueBefore, g.Revision(red), redBefore)
	}
}

// savedByValue holds the fog by value, as a game struct might.
type savedByValue struct {
	Turn int     `json:"turn"`
	Fog  fog.Fog `json:"fog"`
}

func TestFogSavedByValue(t *testing.T) {
	f := fog.New(3, 1, 16, 16)
	f.AddSource(fog.Source{Team: blue, Col: 1, Row: 0, Radius: 1})
	f.Update()
	store := save.NewFileStore(t.TempDir())
	// Passed by value, the struct and its Fog are not addressable, so only a value-receiver MarshalJSON writes the snapshot.
	if err := store.Save("slot1", savedByValue{Turn: 3, Fog: *f}); err != nil {
		t.Fatal(err)
	}
	var loaded savedByValue
	if err := store.Load("slot1", &loaded); err != nil {
		t.Fatal(err)
	}
	if got, want := picture(&loaded.Fog, blue), picture(f, blue); got != want {
		t.Fatalf("after load:\n%s\nwant:\n%s", got, want)
	}
}

func TestSnapshotText(t *testing.T) {
	f := fog.New(3, 2, 8, 8)
	f.SetOpaque(1, 0, true)
	f.AddSource(fog.Source{Team: red, Col: 2, Row: 1, Radius: 1})
	f.AddSource(fog.Source{Team: blue, Col: 0, Row: 0, Radius: 0})
	f.Update()
	got, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"version":1,"width":3,"height":2,"tileWidth":8,"tileHeight":8,"opaque":[".#.","..."],"nextSource":2,"sources":[{"id":1,"team":2,"col":2,"row":1,"radius":1},{"id":2,"team":1,"col":0,"row":0,"radius":0}],"teams":[{"team":1,"explored":["#..","..."]},{"team":2,"explored":[".##",".##"]}]}`
	if string(got) != want {
		t.Fatalf("snapshot:\n%s\nwant:\n%s", got, want)
	}
	var g fog.Fog
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(&g)
	if !bytes.Equal(got, again) {
		t.Fatalf("round trip changed the snapshot:\n%s", again)
	}
}

func TestUnmarshalRefusesBadSnapshots(t *testing.T) {
	const good = `{"version":1,"width":2,"height":1,"tileWidth":1,"tileHeight":1,"nextSource":1,"sources":[{"id":1,"team":0,"col":0,"row":0,"radius":1}],"teams":[{"team":0,"explored":["#."]}]}`
	var ok fog.Fog
	if err := json.Unmarshal([]byte(good), &ok); err != nil {
		t.Fatalf("good snapshot refused: %v", err)
	}
	cases := map[string]struct {
		text string
		want error
	}{
		"unknown field":      {strings.Replace(good, `"width"`, `"colour":1,"width"`, 1), fog.ErrInvalidSnapshot},
		"missing version":    {strings.Replace(good, `"version":1`, `"version":0`, 1), fog.ErrInvalidSnapshot},
		"future version":     {strings.Replace(good, `"version":1`, `"version":2`, 1), fog.ErrFutureVersion},
		"negative width":     {strings.Replace(good, `"width":2`, `"width":-2`, 1), fog.ErrInvalidSnapshot},
		"huge grid":          {strings.Replace(good, `"height":1`, `"height":99999999`, 1), fog.ErrInvalidSnapshot},
		"tall empty grid":    {`{"version":1,"width":0,"height":100000000000,"tileWidth":1,"tileHeight":1,"nextSource":0}`, fog.ErrInvalidSnapshot},
		"wide empty grid":    {`{"version":1,"width":5,"height":0,"tileWidth":1,"tileHeight":1,"nextSource":0}`, fog.ErrInvalidSnapshot},
		"short row":          {strings.Replace(good, `"#."`, `"#"`, 1), fog.ErrInvalidSnapshot},
		"bad cell":           {strings.Replace(good, `"#."`, `"#x"`, 1), fog.ErrInvalidSnapshot},
		"missing rows":       {strings.Replace(good, `"explored":["#."]`, `"explored":[]`, 1), fog.ErrInvalidSnapshot},
		"bad opaque":         {strings.Replace(good, `"nextSource"`, `"opaque":["##","##"],"nextSource"`, 1), fog.ErrInvalidSnapshot},
		"source above next":  {strings.Replace(good, `"nextSource":1`, `"nextSource":0`, 1), fog.ErrInvalidSnapshot},
		"source zero":        {strings.Replace(good, `"id":1`, `"id":0`, 1), fog.ErrInvalidSnapshot},
		"source no team":     {strings.Replace(good, `"team":0,"col"`, `"team":5,"col"`, 1), fog.ErrInvalidSnapshot},
		"teams out of order": {strings.Replace(good, `"teams":[{"team":0,"explored":["#."]}]`, `"teams":[{"team":1,"explored":[".."]},{"team":0,"explored":["#."]}]`, 1), fog.ErrInvalidSnapshot},
		"sources repeated":   {strings.Replace(good, `"nextSource":1,"sources":[{"id":1,"team":0,"col":0,"row":0,"radius":1}]`, `"nextSource":2,"sources":[{"id":1,"team":0,"col":0,"row":0,"radius":1},{"id":1,"team":0,"col":1,"row":0,"radius":1}]`, 1), fog.ErrInvalidSnapshot},
		"trailing data":      {good + `{}`, fog.ErrInvalidSnapshot},
		"not an object":      {`[]`, fog.ErrInvalidSnapshot},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := fog.New(1, 1, 1, 1)
			f.AddSource(fog.Source{})
			before, _ := json.Marshal(f)
			err := f.UnmarshalJSON([]byte(c.text))
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			after, _ := json.Marshal(f)
			if !bytes.Equal(before, after) {
				t.Fatal("a refused snapshot changed the fog")
			}
		})
	}
}

func TestStateString(t *testing.T) {
	for s, want := range map[fog.State]string{fog.Unseen: "unseen", fog.Explored: "explored", fog.Visible: "visible", 7: "fog.State(7)"} {
		if s.String() != want {
			t.Errorf("%d.String() = %q, want %q", s, s.String(), want)
		}
	}
}

func FuzzUnmarshalJSON(f *testing.F) {
	f.Add([]byte(`{"version":1,"width":3,"height":2,"tileWidth":8,"tileHeight":8,"opaque":[".#.","..."],"nextSource":2,"sources":[{"id":1,"team":2,"col":2,"row":1,"radius":1},{"id":2,"team":1,"col":0,"row":0,"radius":0}],"teams":[{"team":1,"explored":["#..","..."]},{"team":2,"explored":[".##","###"]}]}`))
	f.Add([]byte(`{"version":1,"width":0,"height":0,"tileWidth":0,"tileHeight":0,"nextSource":0}`))
	f.Add([]byte(`{"version":1,"width":4,"height":1,"tileWidth":1,"tileHeight":1,"nextSource":1,"sources":[{"id":1,"team":-3,"col":9,"row":-2,"radius":99999}],"teams":[{"team":-3,"explored":["...."]}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var g fog.Fog
		if err := json.Unmarshal(data, &g); err != nil {
			return
		}
		first, err := json.Marshal(&g)
		if err != nil {
			t.Fatalf("marshal of an accepted snapshot: %v", err)
		}
		var h fog.Fog
		if err := json.Unmarshal(first, &h); err != nil {
			t.Fatalf("re-reading our own snapshot: %v\n%s", err, first)
		}
		second, _ := json.Marshal(&h)
		if !bytes.Equal(first, second) {
			t.Fatalf("snapshot is not stable:\n%s\n%s", first, second)
		}
	})
}

func benchFog(withWalls bool) (*fog.Fog, []fog.SourceID) {
	const size = 128
	f := fog.New(size, size, 16, 16)
	if withWalls {
		for row := 4; row < size; row += 8 {
			for col := range size {
				if col%16 != 0 {
					f.SetOpaque(col, row, true)
				}
			}
		}
	}
	ids := make([]fog.SourceID, 0, 64)
	for i := range 64 {
		ids = append(ids, f.AddSource(fog.Source{Team: fog.Team(i % 2), Col: (i * 37) % size, Row: (i * 53) % size, Radius: 8}))
	}
	f.Update()
	return f, ids
}

func benchmarkUpdate(b *testing.B, withWalls bool) {
	f, ids := benchFog(withWalls)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		for j, id := range ids {
			s, _ := f.Source(id)
			s.Col = (s.Col + 1 + (i+j)%3) % f.Width()
			f.SetSource(id, s)
		}
		f.Update()
	}
}

// BenchmarkUpdateOpen moves 64 radius-8 units on a 128x128 open map and updates both teams each step.
func BenchmarkUpdateOpen(b *testing.B) { benchmarkUpdate(b, false) }

// BenchmarkUpdateWalls is BenchmarkUpdateOpen with rows of walls, so every source shadowcasts.
func BenchmarkUpdateWalls(b *testing.B) { benchmarkUpdate(b, true) }

func BenchmarkDraw(b *testing.B) {
	f, _ := benchFog(true)
	cam := &tilemap.Camera{X: 1024, Y: 1024, Width: 640, Height: 360}
	style := fog.DefaultStyle()
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 4096)}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		d.Ops = d.Ops[:0]
		f.Draw(&d, 0, cam, style)
	}
}
