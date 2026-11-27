package achievements_test

import (
	"os"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/achievements"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/achievements.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseFixture(t *testing.T) {
	set, err := achievements.Parse(readFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Achievements) != 4 {
		t.Fatalf("got %d achievements, want 4", len(set.Achievements))
	}
	collector, ok := set.Lookup("gem-collector")
	if !ok || collector.TotalSteps() != 500 || collector.Platforms.ID(achievements.PlayGames) != "CgkIabc123EAIQAg" {
		t.Fatalf("gem-collector = %+v, %v", collector, ok)
	}
	secret, _ := set.Lookup("secret")
	if !secret.Hidden || secret.TotalSteps() != 1 || secret.Platforms.ID(achievements.PlayGames) != "" {
		t.Fatalf("secret = %+v", secret)
	}
	if _, ok := set.Lookup("missing"); ok {
		t.Fatal("Lookup found an undeclared achievement")
	}
}

func TestEncodeRoundTripIsLossless(t *testing.T) {
	data := readFixture(t)
	set, err := achievements.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	out, err := achievements.Encode(set)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(data) {
		t.Fatalf("round trip changed the file:\n--- got\n%s--- want\n%s", out, data)
	}
}

func TestParseRefusesInvalidFiles(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"unknown field":    "version: 1\nachievements:\n  - id: a\n    points: 10\n",
		"unknown root":     "version: 1\nleaderboards: []\n",
		"missing version":  "achievements:\n  - id: a\n",
		"future version":   "version: 2\n",
		"missing id":       "version: 1\nachievements:\n  - title: A\n",
		"bad id":           "version: 1\nachievements:\n  - id: has space\n",
		"long id":          "version: 1\nachievements:\n  - id: " + strings.Repeat("a", 129) + "\n",
		"duplicate id":     "version: 1\nachievements:\n  - id: a\n  - id: a\n",
		"negative steps":   "version: 1\nachievements:\n  - id: a\n    steps: -1\n",
		"too many steps":   "version: 1\nachievements:\n  - id: a\n    steps: 2147483648\n",
		"unknown service":  "version: 1\nachievements:\n  - id: a\n    platforms:\n      steam: ACH_A\n",
		"spaced platform":  "version: 1\nachievements:\n  - id: a\n    platforms:\n      gameCenter: a b\n",
		"shared platform":  "version: 1\nachievements:\n  - id: a\n    platforms:\n      playGames: X\n  - id: b\n    platforms:\n      playGames: X\n",
		"trailing doc":     "version: 1\n---\nversion: 1\n",
		"wrong type":       "version: 1\nachievements: {}\n",
		"invalid utf8":     "version: 1\nachievements:\n  - id: a\n    title: \xff\n",
		"platform too big": "version: 1\nachievements:\n  - id: a\n    platforms:\n      gameCenter: " + strings.Repeat("x", 256) + "\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if set, err := achievements.Parse([]byte(src)); err == nil {
				t.Fatalf("Parse accepted %q as %+v", src, set)
			}
		})
	}
}

func TestSamePlatformIDOnDifferentServicesIsAllowed(t *testing.T) {
	src := "version: 1\nachievements:\n  - id: a\n    platforms:\n      gameCenter: win\n      playGames: win\n"
	if _, err := achievements.Parse([]byte(src)); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeValidates(t *testing.T) {
	if _, err := achievements.Encode(&achievements.Set{Version: 1, Achievements: []achievements.Definition{{ID: ""}}}); err == nil {
		t.Fatal("Encode accepted an achievement without an ID")
	}
	if _, err := achievements.Encode(nil); err == nil {
		t.Fatal("Encode accepted nil")
	}
}

func TestProgressRoundTripIsLosslessAndSorted(t *testing.T) {
	p := &achievements.Progress{Version: 1, Achievements: []achievements.Record{
		{ID: "gem-collector", Steps: 120, Reported: map[achievements.Service]int{achievements.PlayGames: 100, achievements.GameCenter: 120}},
		{ID: "first-match", Steps: 1},
	}}
	out, err := achievements.EncodeProgress(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 1\nachievements:\n  - id: first-match\n    steps: 1\n  - id: gem-collector\n    steps: 120\n    reported:\n      gameCenter: 120\n      playGames: 100\n"
	if string(out) != want {
		t.Fatalf("EncodeProgress:\n%s\nwant:\n%s", out, want)
	}
	if p.Achievements[0].ID != "gem-collector" {
		t.Fatal("EncodeProgress reordered the caller's records")
	}
	back, err := achievements.ParseProgress(out)
	if err != nil {
		t.Fatal(err)
	}
	again, err := achievements.EncodeProgress(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(out) {
		t.Fatalf("progress round trip changed:\n%s\nwant:\n%s", again, out)
	}
}

func TestParseProgressRefusesInvalidDocuments(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"future version":   "version: 2\n",
		"unknown field":    "version: 1\nachievements:\n  - id: a\n    steps: 1\n    unlockedAt: now\n",
		"duplicate":        "version: 1\nachievements:\n  - id: a\n    steps: 1\n  - id: a\n    steps: 2\n",
		"negative steps":   "version: 1\nachievements:\n  - id: a\n    steps: -3\n",
		"unknown service":  "version: 1\nachievements:\n  - id: a\n    steps: 1\n    reported:\n      steam: 1\n",
		"reported > steps": "version: 1\nachievements:\n  - id: a\n    steps: 1\n    reported:\n      gameCenter: 2\n",
		"bad id":           "version: 1\nachievements:\n  - id: \"a/b\"\n    steps: 1\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if p, err := achievements.ParseProgress([]byte(src)); err == nil {
				t.Fatalf("ParseProgress accepted %q as %+v", src, p)
			}
		})
	}
}

func TestServiceValid(t *testing.T) {
	if !achievements.GameCenter.Valid() || !achievements.PlayGames.Valid() {
		t.Fatal("known services reported invalid")
	}
	if achievements.Service("steam").Valid() || achievements.Service("").Valid() {
		t.Fatal("unknown service reported valid")
	}
}
