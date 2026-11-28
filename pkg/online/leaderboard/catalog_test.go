package leaderboard_test

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/leaderboard"
)

const canonical = `version: 1
leaderboards:
  - name: high-score
    gameCenter: dev.nsgo.game.example.high_score
    playGames: CgkI6v7z8vQREAIQAQ
    miniGame:
      key: best
      order: desc
  - name: fastest-run
    gameCenter: dev.nsgo.game.example.fastest_run
    miniGame:
      key: fastest
      order: asc
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
			{
				Name:       "high-score",
				GameCenter: "dev.nsgo.game.example.high_score",
				PlayGames:  "CgkI6v7z8vQREAIQAQ",
				MiniGame:   &leaderboard.MiniGameBoard{Key: "best", Order: leaderboard.OrderDesc},
			},
			{
				Name:       "fastest-run",
				GameCenter: "dev.nsgo.game.example.fastest_run",
				MiniGame:   &leaderboard.MiniGameBoard{Key: "fastest", Order: leaderboard.OrderAsc},
			},
			{
				Name:      "daily",
				PlayGames: "CgkI6v7z8vQREAIQAw",
			},
		},
	}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("Parse = %+v, want %+v", c, want)
	}

	b, ok := c.Board("fastest-run")
	if !ok || b.ID(leaderboard.ProviderGameCenter) != "dev.nsgo.game.example.fastest_run" || b.ID(leaderboard.ProviderPlayGames) != "" || b.ID(leaderboard.ProviderMiniGame) != "fastest" || b.ID(leaderboard.ProviderNone) != "" {
		t.Fatalf("Board(fastest-run) = %+v, %v", b, ok)
	}
	// Mutating the returned board's MiniGame must not mutate the catalog.
	b.MiniGame.Key = "mutated"
	if again, _ := c.Board("fastest-run"); again.MiniGame.Key != "fastest" {
		t.Fatalf("Board returned a shared MiniGame pointer: %+v", again.MiniGame)
	}
	if _, ok := c.Board("missing"); ok {
		t.Fatal("Board(missing) found a board")
	}
	var nilCatalog *leaderboard.Catalog
	if _, ok := nilCatalog.Board("high-score"); ok {
		t.Fatal("nil catalog found a board")
	}
}

func TestParseFlowMiniGameAndDefaultOrder(t *testing.T) {
	src := "version: 1\nleaderboards:\n  - name: friends-only\n    miniGame: { key: best, order: desc }\n  - name: default-order\n    miniGame: { key: points }\n"
	c, err := leaderboard.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	b1, ok := c.Board("friends-only")
	if !ok || b1.MiniGame == nil || b1.MiniGame.Key != "best" || b1.MiniGame.Order != leaderboard.OrderDesc || b1.MiniGame.EffectiveOrder() != leaderboard.OrderDesc {
		t.Fatalf("friends-only = %+v, %v", b1, ok)
	}
	b2, ok := c.Board("default-order")
	if !ok || b2.MiniGame == nil || b2.MiniGame.Key != "points" || b2.MiniGame.Order != "" || b2.MiniGame.EffectiveOrder() != leaderboard.OrderDesc {
		t.Fatalf("default-order = %+v, %v", b2, ok)
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
		{Name: "best: 速度 #1", PlayGames: "d", MiniGame: &leaderboard.MiniGameBoard{Key: "score_1", Order: leaderboard.OrderAsc}},
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
	longMGKey := strings.Repeat("k", leaderboard.MaxMiniGameKeyLength+1)
	exactMGKey := strings.Repeat("k", leaderboard.MaxMiniGameKeyLength)
	if _, err := leaderboard.Parse([]byte("version: 1\nleaderboards:\n  - name: ok\n    miniGame:\n      key: " + exactMGKey + "\n")); err != nil {
		t.Fatalf("exact 128-byte miniGame key refused: %v", err)
	}

	cases := map[string]struct {
		yaml string
		want string
	}{
		"empty":                  {"  \n", "empty catalog"},
		"unknown field":          {"version: 1\nleaderboards:\n  - name: a\n    gameCenter: x\n    xbox: y\n", `unknown field "xbox"`},
		"unknown miniGame field": {"version: 1\nleaderboards:\n  - name: a\n    miniGame:\n      key: best\n      extra: 1\n", `unknown field "extra"`},
		"unknown top":            {"version: 1\nboards: []\n", `unknown field "boards"`},
		"missing version":        {"leaderboards:\n  - name: a\n    gameCenter: x\n", "unsupported version 0"},
		"future version":         {"version: 2\nleaderboards:\n  - name: a\n    gameCenter: x\n", "unsupported version 2"},
		"no boards":              {"version: 1\nleaderboards: []\n", "no leaderboards"},
		"empty name":             {"version: 1\nleaderboards:\n  - gameCenter: x\n", "empty name"},
		"padded name":            {"version: 1\nleaderboards:\n  - name: \" a\"\n    gameCenter: x\n", "whitespace"},
		"control in name":        {"version: 1\nleaderboards:\n  - name: \"a\\tb\"\n    gameCenter: x\n", "control character"},
		"duplicate name":         {"version: 1\nleaderboards:\n  - name: a\n    gameCenter: x\n  - name: a\n    playGames: y\n", "duplicate board name"},
		"duplicate gc id":        {"version: 1\nleaderboards:\n  - name: a\n    gameCenter: x\n  - name: b\n    gameCenter: x\n", "reuses the gameCenter ID"},
		"duplicate pg id":        {"version: 1\nleaderboards:\n  - name: a\n    playGames: x\n  - name: b\n    playGames: x\n", "reuses the playGames ID"},
		"duplicate mg key":       {"version: 1\nleaderboards:\n  - name: a\n    miniGame: { key: best }\n  - name: b\n    miniGame: { key: best }\n", "reuses the miniGame key"},
		"no ids":                 {"version: 1\nleaderboards:\n  - name: a\n", "no platform ID"},
		"bad id":                 {"version: 1\nleaderboards:\n  - name: a\n    gameCenter: \"has space\"\n", "gameCenter"},
		"long id":                {"version: 1\nleaderboards:\n  - name: a\n    playGames: " + long + "\n", "at most 100"},
		"empty mg key":           {"version: 1\nleaderboards:\n  - name: a\n    miniGame: { order: desc }\n", "empty key"},
		"padded mg key":          {"version: 1\nleaderboards:\n  - name: a\n    miniGame: { key: \" best\" }\n", "whitespace"},
		"control in mg key":      {"version: 1\nleaderboards:\n  - name: a\n    miniGame: { key: \"a\\nb\" }\n", "control character"},
		"long mg key":            {"version: 1\nleaderboards:\n  - name: a\n    miniGame: { key: " + longMGKey + " }\n", "at most 128"},
		"long multibyte mg key":  {"version: 1\nleaderboards:\n  - name: a\n    miniGame: { key: " + strings.Repeat("分", 43) + " }\n", "at most 128"},
		"bad mg order":           {"version: 1\nleaderboards:\n  - name: a\n    miniGame: { key: best, order: descending }\n", "miniGame.order"},
		"duplicate key":          {"version: 1\nversion: 1\nleaderboards:\n  - name: a\n    gameCenter: x\n", "already defined"},
		"trailing doc":           {"version: 1\nleaderboards:\n  - name: a\n    gameCenter: x\n---\nversion: 1\n", "trailing"},
		"not a mapping":          {"- 1\n", "cannot use a !!seq"},
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

func TestCatalogMiniGameKeyLimit(t *testing.T) {
	makeCatalog := func(n int) *leaderboard.Catalog {
		boards := make([]leaderboard.Board, n)
		for i := range boards {
			boards[i] = leaderboard.Board{
				Name:     fmt.Sprintf("board-%03d", i),
				MiniGame: &leaderboard.MiniGameBoard{Key: fmt.Sprintf("key_%03d", i), Order: leaderboard.OrderDesc},
			}
		}
		return &leaderboard.Catalog{Version: 1, Leaderboards: boards}
	}
	if err := makeCatalog(leaderboard.MaxMiniGameKeys).Validate(); err != nil {
		t.Fatalf("Validate(%d keys) failed: %v", leaderboard.MaxMiniGameKeys, err)
	}
	if err := makeCatalog(leaderboard.MaxMiniGameKeys + 1).Validate(); err == nil || !strings.Contains(err.Error(), "more than 128 miniGame keys") {
		t.Fatalf("Validate(%d keys) = %v, want >128 error", leaderboard.MaxMiniGameKeys+1, err)
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
	f.Add([]byte("version: 1\nleaderboards: [{name: f, miniGame: {key: best, order: asc}}]\n"))
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

func TestSteamLeaderboardRoundTrip(t *testing.T) {
	data := []byte("version: 1\nleaderboards:\n  - name: high-score\n    steam: global_high\n")
	c, err := leaderboard.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Leaderboards[0].ID(leaderboard.ProviderSteam); got != "global_high" {
		t.Fatalf("steam ID = %q", got)
	}
	out, err := c.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("steam: global_high")) {
		t.Fatalf("encoded steam ID missing: %s", out)
	}
}
