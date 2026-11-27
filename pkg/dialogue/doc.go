// Package dialogue runs branching conversations written as Yarn Spinner (.yarn) scripts: [ParseYarn] reads a [Script] of titled nodes, and a [Runner] steps through it line by line and choice by choice, evaluates its variables and conditions, and hands each <<command>> to [Options.OnCommand] or publishes it as an [Event] on a [flow.Bus].
//
// Nothing in dialogue reads the wall clock, spawns goroutines or keeps package-level state, and a Runner's variables and visit counts save and load as a [State], so a conversation replays the same under nsgotest and survives a save game.
package dialogue
