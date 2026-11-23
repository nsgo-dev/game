package leaderboard_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/leaderboard"
)

const canonical = `version: 1
leaderboards:
  - name: high-score
    gameCenter: dev.nsgo.game.example.high_score
    playGames: CgkI6v7z8vQREAIQAQ
  - name: fastest-run
    gameCenter: dev.nsgo.game.example.fastest_run
  - name: daily
    playGames: CgkI6v7z8vQREAIQAw
`

func TestParseReadsBoardsInOrder(t *testing.T) {
	c, err := leaderboard.Parse([]byte(canonical))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := &leaderboard.Catalog{
		Version: 1,
		Leaderboards: []leaderboard.Board{
			{Name: "high-score", GameCenter: "dev.nsgo.game.example.high_score", PlayGames: "CgkI6v7z8vQREAIQAQ"},
			{Name: "fastest-run", GameCenter: "dev.nsgo.game.example.fastest_run"},
			{Name: "daily", PlayGames: "CgkI6v7z8vQREAIQAw"},
		},
	}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("Parse = %+v, want %+v", c, want)
	}

	b, ok := c.Board("fastest-run")
	if !ok || b.ID(leaderboard.ProviderGameCenter) != "dev.nsgo.game.example.fastest_run" || b.ID(leaderboard.ProviderPlayGames) != "" || b.ID(leaderboard.ProviderNone) != "" {
		t.Fatalf("Board(fastest-run) = %+v, %v", b, ok)
	}
	if _, ok := c.Board("missing"); ok {
		t.Fatal("Board(missing) found a board")
	}
	var nilCatalog *leaderboard.Catalog
	if _, ok := nilCatalog.Board("high-score"); ok {
		t.Fatal("nil catalog found a board")
	}
}

func TestEncodeRoundTripIsLossless(t *testing.T) {
	c, err := leaderboard.Parse([]byte(canonical))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := c.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(out) != canonical {
		t.Fatalf("Encode changed the file:\n%s\nwant:\n%s", out, canonical)
	}
	again, err := c.Encode()
	if err != nil || !bytes.Equal(again, out) {
		t.Fatalf("second Encode differs: %v\n%s", err, again)
	}
}

func TestEncodeQuotesNamesThatLookLikeOtherTypes(t *testing.T) {
	c := &leaderboard.Catalog{Version: 1, Leaderboards: []leaderboard.Board{
		{Name: "yes", GameCenter: "a"},
		{Name: "123", GameCenter: "b"},
		{Name: "null", PlayGames: "c"},
		{Name: "best: 速度 #1", PlayGames: "d"},
	}}
	out, err := c.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := leaderboard.Parse(out)
	if err != nil {
		t.Fatalf("Parse(Encode): %v\n%s", err, out)
	}
	if !reflect.DeepEqual(back, c) {
		t.Fatalf("round trip = %+v, want %+v", back, c)
	}
}

func TestParseRefusesInvalidCatalogs(t *testing.T) {
	long := strings.Repeat("a", leaderboard.MaxIDLength+1)
	cases := map[string]struct {
		yaml string
		want string
	}{
		"empty":           {"  \n", "empty catalog"},
		"unknown field":   {"version: 1\nleaderboards:\n  - name: a\n    gameCenter: x\n    steam: y\n", "field steam not found"},
		"unknown top":     {"version: 1\nboards: []\n", "field boards not found"},
		"missing version": {"leaderboards:\n  - name: a\n    gameCenter: x\n", "unsupported version 0"},
		"future version":  {"version: 2\nleaderboards:\n  - name: a\n    gameCenter: x\n", "unsupported version 2"},
		"no boards":       {"version: 1\nleaderboards: []\n", "no leaderboards"},
		"empty name":      {"version: 1\nleaderboards:\n  - gameCenter: x\n", "empty name"},
		"padded name":     {"version: 1\nleaderboards:\n  - name: \" a\"\n    gameCenter: x\n", "whitespace"},
		"control in name": {"version: 1\nleaderboards:\n  - name: \"a\\tb\"\n    gameCenter: x\n", "control character"},
		"duplicate name":  {"version: 1\nleaderboards:\n  - name: a\n    gameCenter: x\n  - name: a\n    playGames: y\n", "duplicate board name"},
		"no ids":          {"version: 1\nleaderboards:\n  - name: a\n", "no platform ID"},
		"bad id":          {"version: 1\nleaderboards:\n  - name: a\n    gameCenter: \"has space\"\n", "gameCenter"},
		"long id":         {"version: 1\nleaderboards:\n  - name: a\n    playGames: " + long + "\n", "at most 100"},
		"duplicate key":   {"version: 1\nversion: 1\nleaderboards:\n  - name: a\n    gameCenter: x\n", "already defined"},
		"trailing doc":    {"version: 1\nleaderboards:\n  - name: a\n    gameCenter: x\n---\nversion: 1\n", "trailing"},
		"not a mapping":   {"- 1\n", "decode"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, err := leaderboard.Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("Parse accepted %q as %+v", tc.yaml, c)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestEncodeValidatesFirst(t *testing.T) {
	var nilCatalog *leaderboard.Catalog
	if _, err := nilCatalog.Encode(); err == nil {
		t.Fatal("Encode(nil) succeeded")
	}
	bad := &leaderboard.Catalog{Version: 1, Leaderboards: []leaderboard.Board{{Name: "a"}}}
	if _, err := bad.Encode(); err == nil {
		t.Fatal("Encode wrote a board without IDs")
	}
}

func FuzzParseRoundTrip(f *testing.F) {
	f.Add([]byte(canonical))
	f.Add([]byte("version: 1\nleaderboards:\n  - name: \"yes\"\n    gameCenter: a.b_c-d\n"))
	f.Add([]byte("version: 1\nleaderboards: [{name: x, playGames: Q}]\n"))
	f.Add([]byte("{}"))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := leaderboard.Parse(data)
		if err != nil {
			return
		}
		out, err := c.Encode()
		if err != nil {
			t.Fatalf("Encode of a parsed catalog failed: %v", err)
		}
		back, err := leaderboard.Parse(out)
		if err != nil {
			t.Fatalf("Parse(Encode) failed: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(back, c) {
			t.Fatalf("round trip changed the catalog: %+v != %+v", back, c)
		}
		again, err := back.Encode()
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("Encode is not stable: %v\n%s\n%s", err, out, again)
		}
	})
}
