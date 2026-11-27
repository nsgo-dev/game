package worldmap_test

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/worldmap"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func readCastle(t testing.TB) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/castle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseEncodeRoundTripIsLossless(t *testing.T) {
	data := readCastle(t)
	doc, err := worldmap.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Rooms) != 4 || len(doc.Markers) != 2 {
		t.Fatalf("got %d rooms and %d markers, want 4 and 2", len(doc.Rooms), len(doc.Markers))
	}
	out, err := worldmap.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("Encode(Parse(file)) differs from the file:\n--- got\n%s\n--- want\n%s", out, data)
	}
	again, err := worldmap.Encode(doc)
	if err != nil || !bytes.Equal(again, out) {
		t.Fatalf("second Encode differs (err %v)", err)
	}
}

func TestParseRefusesInvalidDocuments(t *testing.T) {
	room := func(extra string) string {
		return "version: 1\nrooms:\n  - id: a\n    x: 0\n    y: 0\n    width: 10\n    height: 10\n" + extra
	}
	cases := map[string]struct {
		yaml string
		want string
	}{
		"empty":           {"", "empty"},
		"version":         {"version: 2\nrooms:\n  - {id: a, x: 0, y: 0, width: 1, height: 1}\n", "unsupported version"},
		"missing version": {"rooms:\n  - {id: a, x: 0, y: 0, width: 1, height: 1}\n", "unsupported version"},
		"no rooms":        {"version: 1\nrooms: []\n", "no rooms"},
		"unknown field":   {room("    color: red\n"), "field color not found"},
		"unknown root":    {room("extra: 1\n"), "field extra not found"},
		"trailing doc":    {room("---\nversion: 1\n"), "trailing"},
		"empty id":        {"version: 1\nrooms:\n  - {id: '', x: 0, y: 0, width: 1, height: 1}\n", "empty ID"},
		"spaced id":       {"version: 1\nrooms:\n  - {id: ' a', x: 0, y: 0, width: 1, height: 1}\n", "leading or trailing"},
		"duplicate":       {room("  - {id: a, x: 20, y: 0, width: 1, height: 1}\n"), "duplicate room"},
		"zero size":       {"version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: 0, height: 1}\n", "positive width"},
		"nan":             {"version: 1\nrooms:\n  - {id: a, x: .nan, y: 0, width: 1, height: 1}\n", "non-finite"},
		"inf":             {"version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: .inf, height: 1}\n", "non-finite"},
		"overflow":        {"version: 1\nrooms:\n  - {id: a, x: 3e38, y: 0, width: 3e38, height: 1}\n", "float32 range"},
		"rounded away":    {"version: 1\nrooms:\n  - {id: a, x: 1e10, y: 0, width: 1, height: 1}\n", "too small for float32"},
		"rounded away y":  {"version: 1\nrooms:\n  - {id: a, x: 0, y: -1e10, width: 1, height: 1}\n", "too small for float32"},
		"area span": {
			"version: 1\nrooms:\n  - {id: a, x: -3e38, y: 0, width: 1e32, height: 1}\n  - {id: b, x: 3e38, y: 0, width: 1e32, height: 1}\n",
			"area \"\" spans past the float32 range",
		},
		"area span y": {
			"version: 1\nrooms:\n  - {id: a, area: z, x: 0, y: -3e38, width: 1, height: 1e32}\n  - {id: b, area: z, x: 0, y: 3e38, width: 1, height: 1e32}\n",
			"area \"z\" spans past the float32 range",
		},
		"self link":    {"version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: 1, height: 1, links: [a]}\n", "links to itself"},
		"unknown link": {"version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: 1, height: 1, links: [b]}\n", "unknown room \"b\""},
		"twice link": {
			"version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: 1, height: 1, links: [b]}\n  - {id: b, x: 1, y: 0, width: 1, height: 1, links: [a]}\n",
			"listed twice",
		},
		"overlap": {room("  - {id: b, x: 5, y: 5, width: 10, height: 10}\n"), "overlap"},
		"marker room": {
			room("markers:\n  - {id: m, kind: save, room: z, x: 1, y: 1}\n"),
			"unknown room \"z\"",
		},
		"marker outside": {
			room("markers:\n  - {id: m, kind: save, room: a, x: 11, y: 1}\n"),
			"outside room",
		},
		"marker kind": {
			room("markers:\n  - {id: m, kind: '', room: a, x: 1, y: 1}\n"),
			"no kind",
		},
		"marker duplicate": {
			room("markers:\n  - {id: m, kind: k, room: a, x: 1, y: 1}\n  - {id: m, kind: k, room: a, x: 2, y: 2}\n"),
			"duplicate marker",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := worldmap.Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("Parse succeeded, want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestValidateAllowsTouchingRoomsAndOverlapAcrossAreas(t *testing.T) {
	doc := &worldmap.Doc{Version: worldmap.Version, Rooms: []worldmap.RoomDoc{
		{ID: "a", Width: 10, Height: 10},
		{ID: "b", X: 10, Width: 10, Height: 10},
		{ID: "c", Y: 10, Width: 10, Height: 10},
		{ID: "d", Area: "basement", X: 5, Y: 5, Width: 10, Height: 10},
	}}
	if err := worldmap.Validate(doc); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := worldmap.Validate(nil); err == nil {
		t.Fatal("Validate(nil) succeeded")
	}
	if _, err := worldmap.Encode(&worldmap.Doc{Version: 1}); err == nil {
		t.Fatal("Encode of a map without rooms succeeded")
	}
}

func TestValidateFindsOverlapAmongManyRooms(t *testing.T) {
	doc := &worldmap.Doc{Version: worldmap.Version}
	for i := range 200 {
		doc.Rooms = append(doc.Rooms, worldmap.RoomDoc{ID: "r" + strconv.Itoa(i), X: float32(i%20) * 10, Y: float32(i/20) * 10, Width: 10, Height: 10})
	}
	if err := worldmap.Validate(doc); err != nil {
		t.Fatalf("Validate grid: %v", err)
	}
	doc.Rooms = append(doc.Rooms, worldmap.RoomDoc{ID: "late", X: 95, Y: 95, Width: 1, Height: 1})
	if err := worldmap.Validate(doc); err == nil || !strings.Contains(err.Error(), `"late"`) {
		t.Fatalf("Validate = %v, want an overlap naming \"late\"", err)
	}
}
