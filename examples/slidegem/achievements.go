package main

import (
	_ "embed"

	"github.com/nsgo-dev/game/pkg/online/achievements"
	"github.com/nsgo-dev/nsgo/core"
)

// achievementsYAML declares SlideGem's achievements and their Game Center and Play Games IDs.
//
//go:embed achievements.yaml
var achievementsYAML []byte

const (
	achievementsKey = "slidegem.achievements"

	chainReactionCombo = 3
	highRollerScore    = 5000
)

// newAchievementTracker returns a Tracker for achievements.yaml whose progress lives in the app's storage. Stored progress that no longer parses is discarded rather than blocking the game.
func newAchievementTracker(cx *core.Scope) *achievements.Tracker {
	set, err := achievements.Parse(achievementsYAML)
	if err != nil {
		panic("slidegem: achievements.yaml: " + err.Error())
	}
	tracker, err := achievements.NewTracker(set)
	if err != nil {
		panic("slidegem: achievements.yaml: " + err.Error())
	}
	store := cx.Storage()
	if err := tracker.Persist(store, achievementsKey); err != nil {
		store.RemoveItem(achievementsKey)
		if err := tracker.Persist(store, achievementsKey); err != nil {
			panic("slidegem: achievements: " + err.Error())
		}
	}
	return tracker
}

// recordClear advances the achievements one clear step earns. Save failures leave the progress in memory, and the next change saves it.
func (g *slideGemGame) recordClear(cleared int) {
	_ = g.trophies.Unlock("first-match")
	_ = g.trophies.Add("gem-collector", cleared)
	if g.board.Combo >= chainReactionCombo {
		_ = g.trophies.Unlock("chain-reaction")
	}
	if g.board.Score >= highRollerScore {
		_ = g.trophies.Unlock("high-roller")
	}
}

// idleStatus returns the status line for a settled turn, the achievement unlocked during it or else the default prompt, and forgets that achievement so the next turn starts clean.
func (g *slideGemGame) idleStatus() string {
	note := g.unlockNote
	g.unlockNote = ""
	if note == "" {
		return "Tap or swipe adjacent gems"
	}
	return note
}
