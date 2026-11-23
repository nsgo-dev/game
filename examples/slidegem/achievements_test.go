package main

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/achievements"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gameservices"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestAchievementsFileIsCanonical(t *testing.T) {
	set, err := achievements.Parse(achievementsYAML)
	if err != nil {
		t.Fatal(err)
	}
	out, err := achievements.Encode(set)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(achievementsYAML) {
		t.Fatalf("achievements.yaml is not in canonical form:\n%s", out)
	}
	for _, d := range set.Achievements {
		if d.Title == "" || d.Platforms.GameCenter == "" || d.Platforms.PlayGames == "" {
			t.Errorf("%s needs a title and an ID on every service", d.ID)
		}
	}
}

// gameServicesEnv starts SlideGem on os with a signed-in game services player, keeping the base test environment's audio and safe area.
func gameServicesEnv(os core.OS) []nsgotest.Option {
	return []nsgotest.Option{
		nsgotest.Environment(core.Environment{
			Audio:              true,
			SafeArea:           core.Edges{Top: 24, Bottom: 16},
			OS:                 os,
			CanUseGameServices: true,
		}),
		func(a *nsgotest.App) {
			a.SetGameServicesPlayer(gameservices.Player{ID: "p1", DisplayName: "Ada"})
		},
	}
}

func playHintMove(t *testing.T, app *nsgotest.App, g *slideGemGame) {
	t.Helper()
	mv, ok := g.board.FindHint()
	if !ok {
		t.Fatal("seeded board has no valid move")
	}
	tapCell(app, g, mv.A)
	tapCell(app, g, mv.B)
	advanceUntilIdle(t, app, g, 240)
}

func TestFirstMatchUnlocksOnEachService(t *testing.T) {
	for _, tc := range []struct {
		os core.OS
		id string
	}{
		{core.OSMacOS, "dev.nsgo.examples.slidegem.first_match"},
		{core.OSAndroid, "CgkI8Nq1mKcBEAIQAQ"},
	} {
		t.Run(string(tc.os), func(t *testing.T) {
			app, g := startTestGame(t, defaultSeed, gameServicesEnv(tc.os)...)
			if !app.Exists("btn-trophies") {
				t.Fatal("Trophies button missing after sign-in")
			}
			playHintMove(t, app, g)
			app.Settle()

			got := app.GameServicesAchievements()
			i := slices.IndexFunc(got, func(a gameservices.Achievement) bool { return a.ID == tc.id })
			if i < 0 || !got[i].Unlocked {
				t.Fatalf("first-match not unlocked on %s: %+v", tc.os, got)
			}
			if !g.trophies.Unlocked("first-match") || g.trophies.Steps("gem-collector") == 0 {
				t.Fatalf("tracker: first-match=%v gems=%d", g.trophies.Unlocked("first-match"), g.trophies.Steps("gem-collector"))
			}
			if want := achievements.ServiceFor(tc.os); len(g.trophies.Pending(want)) != 0 {
				t.Fatalf("pending on %s: %v", want, g.trophies.Pending(want))
			}

			app.Press("btn-trophies")
			app.Settle()
			if app.GameServicesShownAchievements() != 1 {
				t.Fatal("Trophies did not open the achievements screen")
			}
		})
	}
}

func TestAchievementsWithoutGameServicesPersistAndReportLater(t *testing.T) {
	app, g := startTestGame(t, defaultSeed)
	if app.Exists("btn-trophies") {
		t.Fatal("Trophies button shown without a game service")
	}
	playHintMove(t, app, g)
	if !g.trophies.Unlocked("first-match") {
		t.Fatal("first-match not recorded locally")
	}
	if got := app.Find("hud-status").Text; got != "Achievement unlocked: First Match" {
		t.Fatalf("hud-status = %q", got)
	}

	kept := map[string]string{}
	st := app.Storage()
	for _, k := range st.Keys() {
		kept[k], _ = st.GetItem(k)
	}
	if _, ok := kept[achievementsKey]; !ok {
		t.Fatal("achievement progress was not stored")
	}

	// The next launch on a platform with Game Center reports the stored unlock at sign-in.
	opts := append(gameServicesEnv(core.OSIOS), nsgotest.Storage(kept))
	restarted, rg := startTestGame(t, defaultSeed+1, opts...)
	restarted.Advance(time.Millisecond)
	restarted.Settle()
	want := gameservices.Achievement{ID: "dev.nsgo.examples.slidegem.first_match", PercentComplete: 100, Unlocked: true}
	if got := restarted.GameServicesAchievements(); !slices.Contains(got, want) {
		t.Fatalf("stored unlock not reported: %+v", got)
	}
	if !rg.trophies.Unlocked("first-match") {
		t.Fatal("stored progress not restored")
	}
}

func TestCorruptStoredProgressIsDiscarded(t *testing.T) {
	_, g := startTestGame(t, defaultSeed, nsgotest.Storage(map[string]string{achievementsKey: "not: [valid"}))
	if g.trophies.Steps("gem-collector") != 0 {
		t.Fatal("corrupt progress was restored")
	}
}
