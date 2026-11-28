# dialogue

`game/dialogue` runs branching conversations written as [Yarn Spinner](https://www.yarnspinner.dev/) scripts. `ParseYarn` reads a script of titled nodes into a `Script`, and a `Runner` steps through it one line or choice menu at a time, keeps its variables and node visit counts, and hands each `<<command>>` to the game. [vn](vn.md) presents a script as a visual novel on top of a `Runner`.

```go
import "github.com/nsgo-dev/game/pkg/dialogue"
```

Nothing in `dialogue` reads the wall clock, spawns goroutines or keeps package-level state: a `Runner` moves only when the game calls it, so a conversation plays the same every time and under `nsgotest`.

## Scripts

A script is plain text, usually a `.yarn` file next to the game's other assets. Each node has a `title:` header (and any other `key: value` headers) above `---`, and its body ends at `===`:

```yarn
title: Shop
---
<<declare $gold = 15>>
Merchant: You have {$gold} gold. #line:shop_01
-> Buy a potion (10 gold) <<if $gold >= 10>>
    <<set $gold -= 10>>
    <<give_item potion 1>>
    Merchant: Here you go.
-> Leave
    <<jump Farewell>>
===
title: Farewell
---
Merchant: Come back soon.
===
```

`ParseYarn` reads:

- Comments starting with `//`.
- Lines, `Speaker: Text` or narration without a speaker, with `{expr}` interpolation, an optional `#line:<id>` and any other `#tags`.
- Choices, `-> Option text`, each with an optional `<<if cond>>` guard, `#line:<id>` and tags, and an indented body.
- Conditionals: `<<if expr>>`, `<<elseif expr>>`, `<<else>>` and `<<endif>>`.
- Variables: `<<set $var = expr>>` (or `to`, `+=`, `-=`, `*=`, `/=`) and `<<declare $var = expr>>`, which sets a variable only when it is unset.
- `<<jump Node>>` and `<<stop>>`.
- Game commands, `<<command arg "quoted arg" $var {$expr}>>`, for any other name.

Expressions take numbers, strings, `true` and `false`, `$variables`, the arithmetic operators, comparisons (`==` or `is`, `!=`, `<`, `<=`, `>`, `>=`), `and`/`&&`, `or`/`||`, `not`/`!`, and `visited("Node")` and `visited_count("Node")`.

Its errors are `*loaderr.Error` values with the line at fault and how to fix it; name the file with `loaderr.WithFile` when the script came from one (see [Errors](errors.md)). A parsed `Script` never changes, so many runners can share it; `Nodes`, `HasNode` and `NodeHeaders` list what it holds.

## Running a script

```go
script, err := dialogue.ParseYarn(src)
if err != nil {
	return err
}
r := dialogue.NewRunner(script, dialogue.Options{
	OnCommand: func(e dialogue.Event) { runCommand(e.Command, e.Args) },
})

step, err := r.Start("Shop")
for err == nil && step.Kind != dialogue.StepEnded {
	switch step.Kind {
	case dialogue.StepLine:
		show(step.Line.Speaker, step.Line.Text)
		step, err = r.Continue()
	case dialogue.StepChoices:
		step, err = r.Choose(pick(step.Choices))
	}
}
```

- `Start`, `Continue` and `Choose` each run the script up to the next line (`StepLine`), choice menu (`StepChoices`) or end (`StepEnded`) and return it as a `Step`.
- A `Choice` has `Available` false when its `<<if>>` guard failed: show it greyed out or hide it. `Choose` on it returns `ErrChoiceUnavailable`; `Continue` while a menu waits returns `ErrChoiceRequired`.
- `Start` on a node the script lacks, or a `<<jump>>` to one, returns `ErrUnknownNode`. `Options.MaxStepsPerContinue` (default `DefaultMaxStepsPerContinue`, 1024) bounds the statements one call runs, so a `<<jump>>` loop returns `ErrStepLimitExceeded` instead of hanging.
- `SetVar`, `Var` and `Vars` read and write variables from Go, with or without the leading `$`; Go numbers become `float64`, as in Yarn. `Visited` gives how often a node was entered and `CurrentNode` the node running now.

## Commands

Every `<<command>>` that is not part of Yarn's syntax becomes an `Event` with the command's name, its arguments (variables and `{expr}` already evaluated) and the node it ran in. A runner passes it to `Options.OnCommand`, and publishes it on `Options.Bus`, a [flow](flow.md) `Bus`, so other systems can subscribe to it with `flow.Subscribe`:

```go
var bus flow.Bus
flow.Subscribe(&bus, func(e dialogue.Event) {
	if e.Command == "give_item" {
		inventory.Add(e.Args[0], 1)
	}
})
r := dialogue.NewRunner(script, dialogue.Options{Bus: &bus})
```

## Localization

A line's or choice's `#line:<id>` tag is its key in a string table. With `Options.StringResolver` set, a runner calls it with the id, the script's own text as the fallback and a copy of the variables, then interpolates any `{expr}` left in the returned text, so a translation can move a placeholder. The resolver usually looks the id up in an [l10n](l10n.md) string table and returns the fallback when the table has no entry.

## Shared variables

With `Options.Vars` set to a [flow](flow.md) `Vars` store loaded from `vars.yaml`, a runner reads and writes that store instead of a variable map of its own, so other runners and the rest of the game see every `<<set>>` at once. `<<declare>>` or `<<set>>` of a name the store does not declare returns an error wrapping `flow.ErrUndeclaredVar`, a `<<declare>>` of a declared name keeps the store's value, and assignments keep the declared kind and range. `Script.VarRefs` lists every variable a script reads or writes with its line, which `go tool game check` compares against `vars.yaml`.

## Saving

`State` returns a runner's variables and node visit counts as a `State`, which marshals to JSON for a [save](save.md) slot; `LoadState` restores it into a runner made from the same script. Start the runner at the node to resume from after loading.
