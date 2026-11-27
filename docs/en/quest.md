# quest

`game/quest` runs quests with objectives (collect, reach, defeat, talk), quest states and rewards. Quests are defined in a versioned YAML book, advanced by game events published on a `flow.Bus`, and saved as a small snapshot that round-trips through `save.Store`.

```go
import "github.com/nsgo-dev/game/pkg/quest"
```

## The quest book

A designer writes every quest in one YAML file:

```yaml
version: 1
quests:
  - id: welcome
    title: quest.welcome.title
    autoStart: true
    objectives:
      - id: meet-elder
        kind: talk
        target: elder
  - id: rat-problem
    title: quest.rats.title
    requires: [welcome]
    ordered: true
    objectives:
      - id: talk-innkeeper
        kind: talk
        target: innkeeper
      - id: find-cellar
        kind: reach
        target: cellar
      - id: kill-rats
        kind: defeat
        target: rat
        count: 5
      - id: tails
        kind: collect
        target: rat-tail
        count: 3
        optional: true
      - id: report
        kind: talk
        target: innkeeper
    rewards:
      - kind: gold
        id: coin
        amount: 50
      - kind: item
        id: cellar-key
```

| Field | Meaning |
| --- | --- |
| `version` | Schema version, `1` (`quest.FormatVersion`) |
| `id` | Quest ID, unique in the book and used in saves |
| `title`, `description` | Display text or localization keys; the package does not read them |
| `requires` | Quests that must be completed before this one can start |
| `autoStart` | Start as soon as the prerequisites are complete |
| `repeatable` | Allow `Start` again after the quest completed |
| `ordered` | Objectives advance one at a time, in list order |
| `objectives` | At least one; the quest completes when every non-optional objective is complete |
| `rewards` | Reported on completion for the game to grant |

Each objective has an `id` (unique in its quest), a `kind` (`collect`, `reach`, `defeat` or `talk`), a `target` (the item, place, enemy or character ID events must name), an optional `count` (default 1), `optional` and `text`. A reward has an optional `kind` (such as `item`, `xp` or `gold`), an `id` and an `amount` (default 1). The package only reports rewards, so they can name anything a game defines, including inventory items or stats.

`quest.Parse` and `quest.LoadFS` refuse unknown fields, a wrong version, duplicate or empty IDs, unknown kinds, negative counts, prerequisites that name a missing quest, and prerequisite cycles; every error wraps `quest.ErrInvalidBook`. `quest.Encode` writes the book back in canonical form: 2-space YAML with fields in a fixed order, `requires` in flow style and everything else in block style, empty lists and default values left out, and no generated values. Loading and saving a book in that form, like the example above, changes nothing, and `quest.Parse(quest.Encode(book))` always equals the parsed book. A file in another form comes back canonical, and YAML comments are not kept, so put designer notes in `description` or `text`.

## Running quests

`quest.NewLog` tracks one player's progress through a book. Attach it to the game's event bus and publish events from wherever they happen:

```go
book, err := quest.LoadFS(assets, "quests.yaml")
if err != nil {
	return err
}
log, err := quest.NewLog(book)
if err != nil {
	return err
}
log.OnUpdate = func(u quest.Update) {
	if u.Kind == quest.QuestCompleted {
		for _, r := range u.Rewards {
			grant(r.Kind, r.ID, r.Quantity())
		}
	}
}
detach := log.Attach(&bus)
defer detach()
log.StartReady() // starts autoStart quests with no prerequisites

flow.Publish(&bus, quest.Talked{Character: "elder"})
flow.Publish(&bus, quest.Defeated{Enemy: "rat", Amount: 2})
flow.Publish(&bus, quest.Collected{Item: "rat-tail"})
flow.Publish(&bus, quest.Reached{Place: "cellar"})
```

`Collected`, `Reached`, `Defeated` and `Talked` are shorthands for `quest.Event{Kind, Target, Amount}`; an `Amount` below 1 counts as 1. Without a bus, call `log.Handle(event)` directly.

Quests move through five states:

| State | Meaning |
| --- | --- |
| `Inactive` | Not started |
| `Active` | Objectives accept events |
| `Completed` | Every required objective is complete |
| `Failed` | The game called `Fail`; `Start` retries it |
| `Abandoned` | The player called `Abandon`; `Start` takes it up again |

- `Start(id)` begins a quest with no progress. It returns `ErrNotStartable` while a prerequisite is incomplete, while the quest is active, or after it completed unless it is `repeatable`.
- `StartReady()` starts every `autoStart` quest that has never started and whose prerequisites are complete. A log calls it after each completion, so quest chains follow on by themselves.
- `Fail(id)` and `Abandon(id)` end an active quest; abandoning drops its progress. Neither state is restarted by `StartReady`, so an `autoStart` quest the player abandoned stays abandoned, across saves too, until the game calls `Start`.
- `SetProgress(quest, objective, n)` mirrors a value the game already tracks, such as how many of an item the inventory holds.
- `State`, `Quests(state)`, `Objectives(id)` and `Objective(quest, objective)` feed a journal or HUD. An objective is `ObjectiveLocked`, `ObjectiveActive` or `ObjectiveDone`.

In an `ordered` quest only the first incomplete required objective (and any optional ones before it) accepts events, and the event that completes one objective does not count toward the next, so talking to the innkeeper once does not also finish the final report. The same holds for a quest that an event's completion auto-starts.

Every change is reported as an `Update` (`QuestStarted`, `ObjectiveProgressed`, `ObjectiveCompleted`, `QuestCompleted`, `QuestFailed`, `QuestAbandoned`), delivered to `OnUpdate` and, while attached, published on the bus. Handlers may call back into the log, for example to start a follow-up quest; updates raised that way arrive after the current ones, in order.

## Saving progress

`log.Snapshot()` returns the progress as plain data: every quest that is not inactive, in book order, with its state and the objectives that have progress. Store it in a save slot next to the rest of the game's state:

```go
type gameSave struct {
	Gold   int            `json:"gold"`
	Quests quest.Snapshot `json:"quests"`
}

err := store.Save("slot1", gameSave{Gold: gold, Quests: log.Snapshot()})
```

which writes stable JSON such as:

```json
{"version":1,"quests":[{"id":"welcome","state":"completed","progress":{"meet-elder":1}},{"id":"herbs","state":"active","progress":{"moonleaf":3,"sunroot":1}}]}
```

To load, build a log from the same book and restore:

```go
var s gameSave
if err := store.Load("slot1", &s); err != nil {
	return err
}
if err := log.Restore(s.Quests); err != nil {
	return err
}
log.StartReady()
```

`Restore` checks the whole snapshot before changing anything and delivers no updates. It refuses (with `ErrInvalidSnapshot`) a wrong version, quests or objectives the book no longer has, progress outside an objective's range, and progress a log cannot reach: a quest whose prerequisite is missing from the snapshot (or, unless the prerequisite is `repeatable`, not completed), an abandoned quest with progress, progress in an `ordered` quest past an incomplete required objective, and an active quest whose required objectives are already complete. When a content update removes or renames quests or objectives, lowers a count or adds a prerequisite, register a `save.Migration` that rewrites the old snapshot.

A save slot written before the game tracked quests decodes its missing field to the zero `quest.Snapshot`; `Restore` takes that as no progress and resets every quest to `Inactive`.
