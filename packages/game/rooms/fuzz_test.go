package rooms_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/rooms"
)

// FuzzParse checks that Parse never panics, that it only fails with ErrInvalidMap, and that every map it accepts survives Encode and Parse unchanged.
func FuzzParse(f *testing.F) {
	f.Add([]byte(sampleMap))
	f.Add([]byte("version: 1\nrooms:\n  - {id: a, x: 0, y: 0, width: 10, height: 10}\n"))
	f.Add([]byte("version: 1\ntransition: {kind: fade, color: red}\nrooms:\n  - {id: a, x: -5.5, y: 1e3, width: 1, height: 1}\n"))
	f.Add([]byte("version: 1\nrooms: [{id: a, x: .nan, y: 0, width: 1, height: 1}]\n"))
	f.Add([]byte("version: 1\nrooms: [{id: a, x: -0.0, y: -0, width: 1, height: 1}]\n"))
	f.Add([]byte("{}"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := rooms.Parse(data)
		if err != nil {
			if !errors.Is(err, rooms.ErrInvalidMap) {
				t.Fatalf("Parse error %v does not wrap ErrInvalidMap", err)
			}
			return
		}
		out, err := rooms.Encode(m)
		if err != nil {
			t.Fatalf("Encode of a parsed map failed: %v", err)
		}
		back, err := rooms.Parse(out)
		if err != nil {
			t.Fatalf("Parse of encoded map failed: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(m, back) {
			t.Fatalf("round trip changed the map:\n%s", out)
		}
		again, err := rooms.Encode(back)
		if err != nil || string(again) != string(out) {
			t.Fatalf("Encode is not stable: %v\n%s\n%s", err, out, again)
		}
	})
}
