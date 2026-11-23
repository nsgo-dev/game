package achievements_test

import (
	"bytes"
	"testing"

	"github.com/nsgo-dev/game/packages/game/achievements"
)

// FuzzParse checks that Parse never panics and that whatever it accepts encodes to a canonical form that parses and encodes back unchanged.
func FuzzParse(f *testing.F) {
	f.Add([]byte("version: 1\nachievements:\n  - id: a\n    steps: 3\n    platforms:\n      gameCenter: x.y\n      playGames: Cgk\n"))
	f.Add([]byte("version: 1\n"))
	f.Add([]byte(""))
	f.Add([]byte("a: &a [*a]"))
	f.Add([]byte("version: 1\nachievements:\n  - id: a\n    title: \"\\u00e9\"\n    hidden: true\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		set, err := achievements.Parse(data)
		if err != nil {
			return
		}
		first, err := achievements.Encode(set)
		if err != nil {
			t.Fatalf("Encode of a parsed file failed: %v", err)
		}
		again, err := achievements.Parse(first)
		if err != nil {
			t.Fatalf("Parse of encoded file failed: %v\n%s", err, first)
		}
		second, err := achievements.Encode(again)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("encoding is not stable:\n%s\n---\n%s", first, second)
		}
	})
}

// FuzzParseProgress checks the same for progress documents.
func FuzzParseProgress(f *testing.F) {
	f.Add([]byte("version: 1\nachievements:\n  - id: a\n    steps: 2\n    reported:\n      gameCenter: 1\n"))
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("version: 1\nachievements:\n  - id: b\n    steps: 1\n  - id: a\n    steps: 0\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := achievements.ParseProgress(data)
		if err != nil {
			return
		}
		first, err := achievements.EncodeProgress(p)
		if err != nil {
			t.Fatalf("EncodeProgress of a parsed document failed: %v", err)
		}
		again, err := achievements.ParseProgress(first)
		if err != nil {
			t.Fatalf("ParseProgress of encoded document failed: %v\n%s", err, first)
		}
		second, err := achievements.EncodeProgress(again)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("encoding is not stable:\n%s\n---\n%s", first, second)
		}
	})
}
