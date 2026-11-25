# Agent Note: The rpg2d template, a top-down RPG

Status: implemented

## Problem

Issue #789, under the per-genre template plan (#786), asks for a `rpg2d` template: a top-down RPG with a town and a field, people with branching dialogue, an inventory with equipment, one quest with objectives and rewards, simple combat and save slots, playable with keyboard, gamepad and touch, with an nsgotest playthrough that reaches a win and a loss. The template infrastructure (#787) fixes where a template lives and how it is checked, but not how a genre template is built from this repository's packages, and several packages (tilemap, vn, quest) leave choices to the game that the template has to make.

## Decision

The template is the module `github.com/nsgo-dev/game/template/rpg2d`, its project in `template/rpg2d/template/`, copied to `packages/game/cmd/game/_templates/projects/rpg2d` and registered in `templates.go`. Its game, Hollowbrook, is an original design: a town and a meadow, three people, a glowcaps quest gated by a boss that the starting weapon cannot beat.

- **Data is text, one file per package that reads it.** `data/items.yaml` (inventory catalog), `data/stats.yaml` (stats and one effect per piece of equipment, named after the item), `data/quests.yaml` (quest book), `data/cast.yaml` (vn cast) and `data/dialogue.yarn` (Yarn Spinner, read by flow) are in their packages' own formats. The maps use the template's own `data/world.yaml`, defined in `world.go`: a version field, unknown fields and second documents refused, maps drawn as rows of one-character tile keys, `[column, row]` cells in flow style, and `EncodeWorld` writing back exactly what `ParseWorld` read. `TestDataFilesAreCanonical` checks that every YAML data file is in its encoder's form, and `FuzzParseWorld` that the round trip holds for any accepted input. `loadContent` checks every cross-file reference (items, foes, people, dialogue nodes, cast entries, effects, art frames, quest targets) and the app shows a load error in the window instead of crashing.
- **Rules apart from the app.** `Game` (game.go, menu.go, save.go) holds the state and rules with no drawing or devices, and takes one `intent` per fixed step; the app (ui.go) reads the input map into that intent. Unit tests drive `Game` directly, and the playthroughs drive the app.
- **One input map for every device.** game.yaml binds `up`/`down`/`left`/`right`/`confirm`/`cancel`/`menu` to keys and key codes, gamepad buttons and touch and mouse regions, with `moveX`/`moveY` axes over the direction actions and over the left stick. The touch pad is drawn from `Project.Region`, so moving a region in game.yaml moves its button. This needs the key and gamepad binding kinds of #908, so `go.mod.tmpl` pins `packages/game` at `v0.0.0-20261125051754-af952ae5732f`, a commit on main that has them.
- **Menus are native buttons, conversations are drawn.** Title, pack, battle, defeat and victory menus are one generic `menuView` rendered as non-focusable NSGo buttons, outlined at the selection that up/down move, so a tap, a click, Enter and South all pick the same entry without a focused button taking Space or Enter a second time. Conversations are a `vn.Presenter` drawn on the canvas, advanced by confirm or a tap and choosing by `ChoiceAt`.
- **Grid movement and bump-to-fight battles.** The player and patrolling foes move a tile at a time; walking into a foe, or a foe walking into the player, starts a turn-based battle (Attack, Potion, Flee; bosses cannot be fled). Damage is attack less defense, at least 1, with no randomness, so the balance is a test: `TestThornhideNeedsTheIronSword` loses with the starting sword and wins with the iron one.
- **The quest is unordered, gated by the dialogue.** Gathering and defeating may happen in any order; the report objective is a talk objective the script only reaches once the others are done (`$ready`). Starting the quest counts what came before it with `quest.Log.SetProgress`, since a `Log` ignores events for inactive quests.
- **Saves are JSON in a versioned `save.Store`** under the app's storage, three slots, holding the bag, equipment, quest snapshot, dialogue state (variables and visited nodes) and the defeated foes and taken items; `restore` validates against the data before replacing anything.

## Alternatives considered

- **Tiled maps (`.tmj`) read with `tilemap.LoadTiled`**: the editor path a real game takes later, and no new format. Hand-written Tiled JSON is unreadable in review and needs Tiled to edit, while a template should be read and changed in an afternoon; rows of characters keep the maps diffable and editable in any editor, and replacing `buildTilemap` with `LoadTiled` stays a local change.
- **Menus drawn on the canvas like the conversation**: one rendering path and pixel-perfect styling, but it needs hand-written hit testing, gives no accessibility roles, and would make the touch and mouse paths differ from what nsgotest presses. Native buttons get all three for free.
- **Ordered quest objectives**: the quest package's `ordered` would make "report" wait for the others without script logic, but it locks later objectives, so defeating the boss before the last glowcap would be lost and the quest could no longer complete.
- **Real-time combat**: closer to an action RPG, but needs hitboxes, timing and a far longer test to reach a deterministic loss; a turn-based battle shows the genre's stats and items more clearly and keeps the playthroughs short.
- **Keeping the starter's pin (`a93b5ed9593b`)**: no new pseudo-version, but that version's game.yaml has no key or gamepad bindings, so the controls would have to be bound in code apart from the input map.

## Consequences

- The template plays to a win with the keyboard and to a loss with a gamepad and with the touch pad in `play_test.go`, which walks the maps by shortest path over `Game.blocked`, so map edits that keep the places the test visits keep it passing.
- Gaps found in the packages, worked around in the template rather than changed here: `vn.Presenter.Draw` draws options but not a selected one, so keyboard and gamepad players' selection is ringed by the template over `Style.Layout`; `quest.Log` drops events for quests not yet started, so the template syncs progress on start; tilemap reads only Tiled and LDtk maps, so the template defines its own map rows.
- The pin has to move forward when the template starts using a package or binding newer than `af952ae5732f`; CI's templates job, which builds against the pin with `GOWORK=off`, fails until it does.
