package cards_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/cards"
	"github.com/nsgo-dev/game/packages/game/math2d"
)

const solitaireYAML = `version: 1
card_size:
  w: 63
  h: 88
slots:
  - id: hand
    rect:
      x: 40
      "y": 400
      w: 560
      h: 120
    layout:
      fan:
        arc: 24
        sag: 14
  - id: discard
    rect:
      x: 360
      "y": 200
      w: 63
      h: 88
    layout:
      stack:
        offset:
          x: 0
          "y": -0.5
        depth: 6
    accepts:
      - red
      - black
    capacity: 52
    grab: top
  - id: market
    rect:
      x: 40
      "y": 40
      w: 300
      h: 200
    layout:
      grid:
        columns: 4
        gap:
          x: 8
          "y": 8
    grab: none
`

func TestBoardDocRoundTripIsLossless(t *testing.T) {
	doc, err := cards.ParseBoardDoc([]byte(solitaireYAML))
	if err != nil {
		t.Fatal(err)
	}
	out, err := cards.EncodeBoardDoc(doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != solitaireYAML {
		t.Fatalf("encode changed the document:\n%s", out)
	}
	again, err := cards.ParseBoardDoc(out)
	if err != nil || !reflect.DeepEqual(again, doc) {
		t.Fatalf("re-parse differs: %v", err)
	}
}

func TestLoadBoardAndDoc(t *testing.T) {
	doc, err := cards.ParseBoardDoc([]byte(solitaireYAML))
	if err != nil {
		t.Fatal(err)
	}
	b, err := cards.LoadBoard(doc)
	if err != nil {
		t.Fatal(err)
	}
	if b.Size() != (math2d.Vec2{X: 63, Y: 88}) {
		t.Fatalf("card size %v", b.Size())
	}
	discard := b.Slot("discard")
	want := cards.Stack{Offset: math2d.Vec2{Y: -0.5}, Depth: 6}
	if discard == nil || discard.Layout != want || discard.Grab != cards.GrabTop || discard.Capacity != 52 || len(discard.Accepts) != 2 {
		t.Fatalf("discard = %+v", discard)
	}
	if got := b.Slot("hand").Layout; got != (cards.Fan{Arc: 24, Sag: 14}) {
		t.Fatalf("hand layout %+v", got)
	}
	if got := b.Slot("market"); got.Layout != (cards.Grid{Columns: 4, Gap: math2d.Vec2{X: 8, Y: 8}}) || got.Grab != cards.GrabNone {
		t.Fatalf("market %+v", got)
	}
	back, err := b.Doc()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, doc) {
		t.Fatalf("Doc() = %+v, want %+v", back, doc)
	}

	// Pointer layouts and a nil layout write too; a custom layout does not.
	var code cards.Board
	_ = code.AddSlot(&cards.Slot{ID: "p", Rect: math2d.Rect{W: 10, H: 10}, Layout: &cards.Grid{Columns: 2}})
	_ = code.AddSlot(&cards.Slot{ID: "n", Rect: math2d.Rect{W: 10, H: 10}})
	cd, err := code.Doc()
	if err != nil || cd.Slots[0].Layout.Grid.Columns != 2 || cd.Slots[1].Layout.Stack == nil || cd.CardSize.W != cards.DefaultCardSize.X {
		t.Fatalf("code board doc = %+v, %v", cd, err)
	}
	_ = code.AddSlot(&cards.Slot{ID: "c", Rect: math2d.Rect{W: 10, H: 10}, Layout: spiral{}})
	if _, err := code.Doc(); !errors.Is(err, cards.ErrCustomLayout) {
		t.Fatalf("custom layout Doc() = %v", err)
	}
	var nilFan *cards.Fan
	bad := &cards.Board{}
	_ = bad.AddSlot(&cards.Slot{ID: "f", Rect: math2d.Rect{W: 10, H: 10}, Layout: nilFan})
	if _, err := bad.Doc(); !errors.Is(err, cards.ErrCustomLayout) {
		t.Fatalf("nil *Fan Doc() = %v", err)
	}
	if _, err := cards.LoadBoard(nil); err == nil {
		t.Fatal("LoadBoard(nil) succeeded")
	}
	if _, err := cards.EncodeBoardDoc(nil); err == nil {
		t.Fatal("EncodeBoardDoc(nil) succeeded")
	}
}

type spiral struct{}

func (spiral) Arrange(_ math2d.Rect, _ math2d.Vec2, n int, dst []cards.Pose) []cards.Pose {
	for range n {
		dst = append(dst, cards.Pose{})
	}
	return dst
}

func (spiral) InsertIndex(_ math2d.Rect, _ math2d.Vec2, n int, _ math2d.Vec2) int { return n }

func TestBoardPathSavesAsTheDocPathDoes(t *testing.T) {
	doc, err := cards.ParseBoardDoc([]byte(solitaireYAML))
	if err != nil {
		t.Fatal(err)
	}
	b, err := cards.LoadBoard(doc)
	if err != nil {
		t.Fatal(err)
	}
	back, err := b.Doc()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cards.EncodeBoardDoc(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != solitaireYAML {
		t.Fatalf("Parse, LoadBoard, Doc and Encode changed the document:\n%s", out)
	}
	// The default grab rule has one form: omitted. EncodeBoardDoc refuses the spelled-out one, as ParseBoardDoc does.
	back.Slots[0].Grab = "any"
	if _, err := cards.EncodeBoardDoc(back); err == nil {
		t.Fatal(`EncodeBoardDoc wrote grab "any"`)
	}
}

func TestParseBoardDocRefusesBadDocuments(t *testing.T) {
	base := "version: 1\ncard_size: {w: 63, h: 88}\n"
	slot := func(body string) string {
		return base + "slots:\n  - id: s\n    rect: {x: 0, y: 0, w: 10, h: 10}\n" + body
	}
	cases := map[string]string{
		"empty":            "  \n",
		"version":          "version: 2\ncard_size: {w: 63, h: 88}\n",
		"unknown field":    base + "colour: red\n",
		"card size":        "version: 1\ncard_size: {w: 0, h: 88}\n",
		"nan card size":    "version: 1\ncard_size: {w: .nan, h: 88}\n",
		"trailing":         base + "---\nversion: 1\n",
		"no id":            base + "slots:\n  - rect: {x: 0, y: 0, w: 10, h: 10}\n    layout: {fan: {}}\n",
		"duplicate id":     base + "slots:\n  - id: s\n    rect: {x: 0, y: 0, w: 10, h: 10}\n    layout: {fan: {}}\n  - id: s\n    rect: {x: 0, y: 0, w: 10, h: 10}\n    layout: {fan: {}}\n",
		"empty rect":       base + "slots:\n  - id: s\n    rect: {x: 0, y: 0, w: 0, h: 10}\n    layout: {fan: {}}\n",
		"inf rect":         base + "slots:\n  - id: s\n    rect: {x: .inf, y: 0, w: 10, h: 10}\n    layout: {fan: {}}\n",
		"no layout":        slot(""),
		"two layouts":      slot("    layout: {fan: {}, grid: {}}\n"),
		"unknown layout":   slot("    layout: {ring: {}}\n"),
		"unknown fan key":  slot("    layout: {fan: {radius: 3}}\n"),
		"negative spacing": slot("    layout: {fan: {spacing: -1}}\n"),
		"full turn arc":    slot("    layout: {fan: {arc: 360}}\n"),
		"nan sag":          slot("    layout: {fan: {sag: .nan}}\n"),
		"negative depth":   slot("    layout: {stack: {depth: -1}}\n"),
		"inf offset":       slot("    layout: {stack: {offset: {x: .inf, y: 0}}}\n"),
		"negative columns": slot("    layout: {grid: {columns: -2}}\n"),
		"negative gap":     slot("    layout: {grid: {gap: {x: -1, y: 0}}}\n"),
		"empty tag":        slot("    layout: {fan: {}}\n    accepts: ['']\n"),
		"repeated tag":     slot("    layout: {fan: {}}\n    accepts: [red, red]\n"),
		"negative cap":     slot("    layout: {fan: {}}\n    capacity: -1\n"),
		"grab":             slot("    layout: {fan: {}}\n    grab: bottom\n"),
		"grab any":         slot("    layout: {fan: {}}\n    grab: any\n"),
		"not yaml":         "version: [1\n",
	}
	for name, src := range cases {
		if _, err := cards.ParseBoardDoc([]byte(src)); err == nil {
			t.Errorf("%s: parsed without error", name)
		}
	}
	// The smallest valid documents parse.
	for _, src := range []string{base, slot("    layout: {stack: {}}\n")} {
		if _, err := cards.ParseBoardDoc([]byte(src)); err != nil {
			t.Errorf("valid document refused: %v\n%s", err, src)
		}
	}
}

func FuzzParseBoardDoc(f *testing.F) {
	f.Add([]byte(solitaireYAML))
	f.Add([]byte("version: 1\ncard_size: {w: 1, h: 1}\n"))
	f.Add([]byte("version: 1\ncard_size: {w: 1, h: 1}\nslots:\n  - id: a\n    rect: {x: 0, y: 0, w: 1, h: 1}\n    layout: {grid: {columns: 3}}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := cards.ParseBoardDoc(data)
		if err != nil {
			return
		}
		// Anything that parses encodes, loads and survives a second round trip unchanged.
		out, err := cards.EncodeBoardDoc(doc)
		if err != nil {
			t.Fatalf("encode of a parsed document: %v", err)
		}
		again, err := cards.ParseBoardDoc(out)
		if err != nil {
			t.Fatalf("re-parse: %v\n%s", err, out)
		}
		out2, err := cards.EncodeBoardDoc(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("second encode differs: %v\n%s\n%s", err, out, out2)
		}
		b, err := cards.LoadBoard(again)
		if err != nil {
			t.Fatalf("LoadBoard: %v", err)
		}
		if _, err := b.Doc(); err != nil {
			t.Fatalf("Doc: %v", err)
		}
	})
}
