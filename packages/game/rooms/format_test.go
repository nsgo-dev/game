package rooms_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/packages/game/rooms"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const sampleMap = `version: 1
transition:
  kind: slide
  duration: 300ms
rooms:
  - id: entrance
    name: room.entrance
    x: 0
    "y": 0
    width: 320
    height: 180
  - id: hall
    x: 320
    "y": 0
    width: 640
    height: 180
  - id: crypt
    x: 320
    "y": 180
    width: 320
    height: 360
    transition:
      kind: fade
      duration: 500ms
      easing: easeIn
      color: '#102030'
  - id: closet
    x: 0
    "y": 180
    width: 320
    height: 180
    transition:
      kind: cut
`

func TestParseReadsSampleMap(t *testing.T) {
	m, err := rooms.Parse([]byte(sampleMap))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Rooms) != 4 || m.Rooms[0].Name != "room.entrance" {
		t.Fatalf("rooms = %+v", m.Rooms)
	}
	hall := m.Room("hall")
	if hall == nil || hall.Bounds() != (core.Bounds{Origin: core.Point{X: 320}, Size: core.Size{Width: 640, Height: 180}}) {
		t.Fatalf("hall = %+v", hall)
	}
	if got := m.TransitionInto(hall); got.Kind != rooms.KindSlide || got.Length() != 300*time.Millisecond || got.Curve() != core.EaseInOut {
		t.Fatalf("hall transition = %+v", got)
	}
	crypt := m.TransitionInto(m.Room("crypt"))
	if crypt.Kind != rooms.KindFade || crypt.Length() != 500*time.Millisecond || crypt.Curve() != core.EaseIn {
		t.Fatalf("crypt transition = %+v", crypt)
	}
	want, _ := core.ParseColor("#102030")
	if crypt.FadeColor() != want {
		t.Fatalf("crypt color = %v, want %v", crypt.FadeColor(), want)
	}
	if got := m.TransitionInto(m.Room("closet")); got.Kind != rooms.KindCut || got.Length() != 0 {
		t.Fatalf("closet transition = %+v", got)
	}
	if m.Room("missing") != nil {
		t.Fatal("Room(missing) != nil")
	}
}

func TestEncodeRoundTripsLosslessly(t *testing.T) {
	m, err := rooms.Parse([]byte(sampleMap))
	if err != nil {
		t.Fatal(err)
	}
	out, err := rooms.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != sampleMap {
		t.Fatalf("Encode changed the canonical file:\n%s", out)
	}
	// Hand-written files may leave the y key unquoted; Encode quotes it because YAML 1.1 reads a bare y as true.
	plain, err := rooms.Parse([]byte(strings.ReplaceAll(sampleMap, `"y":`, "y:")))
	if err != nil || !reflect.DeepEqual(plain, m) {
		t.Fatalf("unquoted y keys: %v, %+v", err, plain)
	}
	again, err := rooms.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m, again) {
		t.Fatalf("round trip changed the map:\n%+v\n%+v", m, again)
	}
	out2, err := rooms.Encode(again)
	if err != nil {
		t.Fatal(err)
	}
	if string(out2) != string(out) {
		t.Fatal("Encode is not deterministic")
	}
}

func TestEncodeKeepsFractionalCoordinates(t *testing.T) {
	m := &rooms.Map{Version: 1, Rooms: []rooms.Room{{ID: "a", X: -12.25, Y: 0.1, Width: 100.5, Height: 3e6}}}
	out, err := rooms.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	back, err := rooms.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m, back) {
		t.Fatalf("round trip changed the map:\n%s", out)
	}
}

func TestEncodeWritesNegativeZeroAsZero(t *testing.T) {
	m, err := rooms.Parse([]byte("version: 1\nrooms: [{id: a, x: -0.0, y: -0.0, width: 1, height: 1}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if math.Signbit(float64(m.Rooms[0].X)) || math.Signbit(float64(m.Rooms[0].Y)) {
		t.Fatalf("Parse kept a negative zero: %+v", m.Rooms[0])
	}
	negZero := float32(math.Copysign(0, -1))
	out, err := rooms.Encode(&rooms.Map{Version: 1, Rooms: []rooms.Room{{ID: "a", X: negZero, Y: negZero, Width: 1, Height: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "-0") {
		t.Fatalf("Encode wrote a negative zero:\n%s", out)
	}
	back, err := rooms.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	again, err := rooms.Encode(back)
	if err != nil || string(again) != string(out) {
		t.Fatalf("Encode is not stable: %v\n%s\n%s", err, out, again)
	}
}

func TestParseRefusesInvalidMaps(t *testing.T) {
	const room = "rooms:\n  - {id: a, x: 0, y: 0, width: 10, height: 10}\n"
	cases := map[string]string{
		"empty":            "",
		"version":          "version: 2\n" + room,
		"no rooms":         "version: 1\nrooms: []\n",
		"unknown field":    "version: 1\nlayers: 3\n" + room,
		"unknown room key": "version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: 10, height: 10, depth: 1}\n",
		"trailing doc":     "version: 1\n" + room + "---\nversion: 1\n",
		"missing id":       "version: 1\nrooms:\n  - {x: 0, y: 0, width: 10, height: 10}\n",
		"spaced id":        "version: 1\nrooms:\n  - {id: ' a', x: 0, y: 0, width: 10, height: 10}\n",
		"duplicate id":     "version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: 10, height: 10}\n  - {id: a, x: 10, y: 0, width: 10, height: 10}\n",
		"zero width":       "version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: 0, height: 10}\n",
		"negative height":  "version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: 10, height: -1}\n",
		"nan":              "version: 1\nrooms:\n  - {id: a, x: .nan, y: 0, width: 10, height: 10}\n",
		"inf":              "version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: .inf, height: 10}\n",
		"overflow":         "version: 1\nrooms:\n  - {id: a, x: 3e38, y: 0, width: 3e38, height: 10}\n",
		"no kind":          "version: 1\ntransition: {duration: 1s}\n" + room,
		"bad kind":         "version: 1\ntransition: {kind: wipe}\n" + room,
		"bare duration":    "version: 1\ntransition: {kind: slide, duration: 400}\n" + room,
		"bad duration":     "version: 1\ntransition: {kind: slide, duration: soon}\n" + room,
		"negative":         "version: 1\ntransition: {kind: slide, duration: -1s}\n" + room,
		"bad easing":       "version: 1\ntransition: {kind: slide, easing: bouncy}\n" + room,
		"cut duration":     "version: 1\ntransition: {kind: cut, duration: 1s}\n" + room,
		"cut easing":       "version: 1\ntransition: {kind: cut, easing: linear}\n" + room,
		"slide color":      "version: 1\ntransition: {kind: slide, color: '#000'}\n" + room,
		"bad color":        "version: 1\ntransition: {kind: fade, color: 'not a color'}\n" + room,
		"room transition":  "version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: 10, height: 10, transition: {kind: zoom}}\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := rooms.Parse([]byte(src))
			if !errors.Is(err, rooms.ErrInvalidMap) {
				t.Fatalf("Parse error = %v, want ErrInvalidMap", err)
			}
		})
	}
}

func TestEncodeRefusesInvalidMap(t *testing.T) {
	if _, err := rooms.Encode(&rooms.Map{Version: 1}); !errors.Is(err, rooms.ErrInvalidMap) {
		t.Fatalf("Encode error = %v", err)
	}
	if err := rooms.Validate(nil); !errors.Is(err, rooms.ErrInvalidMap) {
		t.Fatalf("Validate(nil) = %v", err)
	}
}

func TestLoadFS(t *testing.T) {
	fsys := fstest.MapFS{"world/rooms.yaml": {Data: []byte(sampleMap)}, "bad.yaml": {Data: []byte("version: 9\n")}}
	m, err := rooms.LoadFS(fsys, "world/rooms.yaml")
	if err != nil || len(m.Rooms) != 4 {
		t.Fatalf("LoadFS = %v, %v", m, err)
	}
	if _, err := rooms.LoadFS(fsys, "bad.yaml"); !errors.Is(err, rooms.ErrInvalidMap) || !strings.Contains(err.Error(), "bad.yaml") {
		t.Fatalf("LoadFS(bad) = %v", err)
	}
	if _, err := rooms.LoadFS(fsys, "missing.yaml"); err == nil {
		t.Fatal("LoadFS(missing) succeeded")
	}
}

func TestTransitionDefaults(t *testing.T) {
	cases := []struct {
		tr     rooms.Transition
		length time.Duration
		curve  core.Curve
	}{
		{rooms.Transition{}, 0, core.Linear},
		{rooms.Transition{Kind: rooms.KindCut}, 0, core.Linear},
		{rooms.Transition{Kind: rooms.KindSlide}, rooms.DefaultSlideDuration, core.EaseInOut},
		{rooms.Transition{Kind: rooms.KindFade}, rooms.DefaultFadeDuration, core.Linear},
		{rooms.Transition{Kind: rooms.KindSlide, Duration: rooms.Duration(time.Second), Easing: rooms.EasingLinear}, time.Second, core.Linear},
		{rooms.Transition{Kind: rooms.KindFade, Easing: rooms.EasingEaseOut}, rooms.DefaultFadeDuration, core.EaseOut},
		{rooms.Transition{Kind: rooms.KindFade, Easing: rooms.EasingEase}, rooms.DefaultFadeDuration, core.Ease},
	}
	for _, c := range cases {
		if got := c.tr.Length(); got != c.length {
			t.Errorf("%+v Length = %v, want %v", c.tr, got, c.length)
		}
		if got := c.tr.Curve(); got != c.curve {
			t.Errorf("%+v Curve = %v, want %v", c.tr, got, c.curve)
		}
	}
	if got := (rooms.Transition{Kind: rooms.KindFade}).FadeColor(); got != core.RGB(0) {
		t.Fatalf("default fade color = %v", got)
	}
	var m *rooms.Map
	if m.TransitionInto(nil).Kind != rooms.KindCut || m.RoomAt(core.Point{}) != nil || m.Room("a") != nil {
		t.Fatal("nil map lookups")
	}
}

func TestRoomContainsIsHalfOpen(t *testing.T) {
	m := &rooms.Map{Version: 1, Rooms: []rooms.Room{
		{ID: "left", X: 0, Y: 0, Width: 100, Height: 100},
		{ID: "right", X: 100, Y: 0, Width: 100, Height: 100},
	}}
	for _, c := range []struct {
		p    core.Point
		want string
	}{
		{core.Point{X: 0, Y: 0}, "left"},
		{core.Point{X: 99.5, Y: 50}, "left"},
		{core.Point{X: 100, Y: 50}, "right"},
		{core.Point{X: 199.9, Y: 99.9}, "right"},
		{core.Point{X: 200, Y: 50}, ""},
		{core.Point{X: 50, Y: 100}, ""},
		{core.Point{X: -0.1, Y: 0}, ""},
	} {
		got := ""
		if r := m.RoomAt(c.p); r != nil {
			got = r.ID
		}
		if got != c.want {
			t.Errorf("RoomAt(%v) = %q, want %q", c.p, got, c.want)
		}
	}
}
