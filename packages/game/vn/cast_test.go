package vn_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/vn"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const castYAML = `version: 1
slots:
  - name: left
    x: 0.25
  - name: center
    x: 0.5
  - name: right
    x: 0.75
characters:
  - id: aiko
    name: Aiko
    color: '#e07a5f'
    slot: left
    defaultExpression: neutral
    expressions:
      - name: neutral
        frame: aiko_neutral
      - name: smile
        frame: aiko_smile
      - name: worry
        frame: aiko_worry
  - id: ren
    name: Ren
    color: '#81b29a'
    slot: right
    expressions:
      - name: neutral
        frame: ren_neutral
      - name: doubt
        frame: ren_doubt
      - name: smile
        frame: ren_smile
`

func mustCast(t testing.TB) *vn.Cast {
	t.Helper()
	c, err := vn.ParseCast([]byte(castYAML))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCastRoundTripIsLossless(t *testing.T) {
	c := mustCast(t)
	out, err := vn.EncodeCast(c)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != castYAML {
		t.Fatalf("encode changed the document:\n%s", out)
	}
	again, err := vn.ParseCast(out)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := vn.EncodeCast(again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, out2) {
		t.Fatal("second round trip differs")
	}
}

func TestCastLookups(t *testing.T) {
	c := mustCast(t)
	if ch, ok := c.Speaker("Aiko"); !ok || ch.ID != "aiko" {
		t.Fatalf("Speaker(Aiko) = %v, %v", ch, ok)
	}
	if ch, ok := c.Speaker("ren"); !ok || ch.DisplayName() != "Ren" {
		t.Fatalf("Speaker(ren) = %v, %v", ch, ok)
	}
	if _, ok := c.Speaker("Narrator"); ok {
		t.Fatal("Narrator is not in the cast")
	}
	ren, _ := c.Character("ren")
	if got := ren.EffectiveExpression(); got != "neutral" {
		t.Fatalf("ren's default expression = %q, want the first, neutral", got)
	}
	if s, ok := c.Slot("right"); !ok || s.X != 0.75 {
		t.Fatalf("Slot(right) = %v, %v", s, ok)
	}
	var empty vn.Cast
	if got := empty.EffectiveSlots(); len(got) != 3 || got[1].Name != "center" {
		t.Fatalf("default slots = %v", got)
	}
}

func TestParseCastRefusesBadDocuments(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"version":         "version: 2\ncharacters:\n  - id: a\n    expressions:\n      - name: n\n        frame: f\n",
		"unknown field":   "version: 1\nmood: happy\ncharacters:\n  - id: a\n    expressions:\n      - name: n\n        frame: f\n",
		"unknown nested":  "version: 1\ncharacters:\n  - id: a\n    pose: x\n    expressions:\n      - name: n\n        frame: f\n",
		"no characters":   "version: 1\n",
		"duplicate id":    "version: 1\ncharacters:\n  - id: a\n    expressions:\n      - name: n\n        frame: f\n  - id: a\n    expressions:\n      - name: n\n        frame: f\n",
		"space in id":     "version: 1\ncharacters:\n  - id: a b\n    expressions:\n      - name: n\n        frame: f\n",
		"no expressions":  "version: 1\ncharacters:\n  - id: a\n    expressions: []\n",
		"dup expression":  "version: 1\ncharacters:\n  - id: a\n    expressions:\n      - name: n\n        frame: f\n      - name: n\n        frame: g\n",
		"no frame":        "version: 1\ncharacters:\n  - id: a\n    expressions:\n      - name: n\n        frame: ''\n",
		"bad default":     "version: 1\ncharacters:\n  - id: a\n    defaultExpression: x\n    expressions:\n      - name: n\n        frame: f\n",
		"bad slot":        "version: 1\ncharacters:\n  - id: a\n    slot: far\n    expressions:\n      - name: n\n        frame: f\n",
		"bad color":       "version: 1\ncharacters:\n  - id: a\n    color: notacolor\n    expressions:\n      - name: n\n        frame: f\n",
		"slot x":          "version: 1\nslots:\n  - name: l\n    x: 1.5\ncharacters:\n  - id: a\n    expressions:\n      - name: n\n        frame: f\n",
		"duplicate slot":  "version: 1\nslots:\n  - name: l\n    x: 0\n  - name: l\n    x: 1\ncharacters:\n  - id: a\n    expressions:\n      - name: n\n        frame: f\n",
		"second document": castYAML + "---\nversion: 1\n",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := vn.ParseCast([]byte(doc)); err == nil {
				t.Fatal("ParseCast accepted the document")
			}
		})
	}
}

func TestCheckFrames(t *testing.T) {
	c := mustCast(t)
	atlas := &sprite.Atlas{Frames: map[string]sprite.Frame{}}
	for _, ch := range c.Characters {
		for _, e := range ch.Expressions {
			atlas.Frames[e.Frame] = sprite.Frame{Name: e.Frame, W: 1, H: 1, Width: 1, Height: 1}
		}
	}
	if err := c.CheckFrames(atlas); err != nil {
		t.Fatal(err)
	}
	delete(atlas.Frames, "ren_doubt")
	err := c.CheckFrames(atlas)
	if err == nil || !strings.Contains(err.Error(), "ren_doubt") {
		t.Fatalf("CheckFrames = %v, want the missing ren_doubt", err)
	}
}

func FuzzParseCast(f *testing.F) {
	f.Add([]byte(castYAML))
	f.Add([]byte("version: 1\ncharacters:\n  - id: a\n    expressions:\n      - name: n\n        frame: f\n"))
	f.Add([]byte("version: 1\nslots: []\ncharacters: [{id: x, expressions: [{name: a, frame: b}]}]\n"))
	f.Add([]byte("{"))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := vn.ParseCast(data)
		if err != nil {
			return
		}
		out, err := vn.EncodeCast(c)
		if err != nil {
			t.Fatalf("a parsed cast does not encode: %v", err)
		}
		again, err := vn.ParseCast(out)
		if err != nil {
			t.Fatalf("an encoded cast does not parse: %v\n%s", err, out)
		}
		out2, err := vn.EncodeCast(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("round trip is not stable: %v\n%s\n%s", err, out, out2)
		}
	})
}
