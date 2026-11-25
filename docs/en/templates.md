# Templates

A new project starts from a template: a small, complete game that builds, runs and passes its tests before you change a line. `game new` writes one into a new directory, and NSGo's `nsgo init -template` writes the same files from the template's Go module.

## The templates

| Template | What it is | `nsgo init -template` module |
| --- | --- | --- |
| `starter` | A tap-to-score 2D game: an animated sprite on a fixed-step loop, a native HUD and headless tests, the smallest project to grow from. The default. | `github.com/nsgo-dev/game/template` |
| `action3d` | A third-person 3D action adventure: an orbit camera, run, jump and attack animations blended by a state machine, melee enemies, and a key that opens the gate to the goal ([guide](template-action3d.md)). | `github.com/nsgo-dev/game/template/action3d` |
| `cards2d` | [Suit Ladders](template-cards2d.md), a single-player card game: cards dragged between a draw pile, a hand, a discard pile and four ladders, rules in code, the deck and the table in YAML, numbered deals and saves. | `github.com/nsgo-dev/game/template/cards2d` |
| `fighting2d` | A one-on-one 2D fighting game: two original fighters with special moves from motion inputs, combos, guards high and low, rounds and a timer, against a CPU opponent or a second player, played with the keyboard, game controllers or touch. [The fighting game template](template-fighting2d.md) describes it. | `github.com/nsgo-dev/game/template/fighting2d` |
| `fps3d` | A first-person shooter arena in 3D: hitscan and projectile weapons, enemies that patrol, hunt and shoot with `ai` and `physics3d`, health and ammo pickups and three waves to clear, played with the keyboard and mouse, a game controller or touch. [The fps3d template](template-fps3d.md) describes it. | `github.com/nsgo-dev/game/template/fps3d` |
| `platformer2d` | A side-scrolling platformer ([guide](template-platformer2d.md)): a controller with coyote time and jump buffering, three rooms with camera bounds, spikes, pits, sparks to collect, a checkpoint and a goal, played with the keyboard, a controller or touch. | `github.com/nsgo-dev/game/template/platformer2d` |
| `puzzle2d` | Spark Slide, a 2D grid puzzle: slide a spark across the board to light every lamp, with levels in YAML, undo and redo, a move limit, stars per level and a level select that saves progress; plays with the keyboard, a game controller, touch and the mouse. | `github.com/nsgo-dev/game/template/puzzle2d` |
| `racing3d` | A 3D racer: two laps of a closed circuit against racing-line rivals, with raycast cars, checkpoints, a countdown, a best-lap ghost and a results screen, played with keyboard, gamepad or touch. [Racing 3D template](template-racing3d.md) | `github.com/nsgo-dev/game/template/racing3d` |
| `rpg2d` | A top-down RPG: a town and a meadow to walk, people with branching dialogue, an inventory with equipment, one quest, turn-based battles and three save slots. [Guide](template-rpg2d.md). | `github.com/nsgo-dev/game/template/rpg2d` |
| `rts2d` | A 2D real-time strategy skirmish: gather crystals, build barracks, train three unit types and destroy a computer opponent's headquarters, with fog of war, box selection, control groups and keyboard, gamepad and touch controls ([guide](template-rts2d.md)). | `github.com/nsgo-dev/game/template/rts2d` |
| `towerdefense2d` | A 2D tower defense game: towers with upgrades beside a road enemies follow, waves defined in YAML, money and lives, played with keyboard, gamepad or touch; [Tower defense template](template-towerdefense2d.md) describes it. | `github.com/nsgo-dev/game/template/towerdefense2d` |
| `visualnovel` | A short branching visual novel: two characters, drawn backgrounds, choices that change the ending, a backlog, auto and skip modes, save slots and two languages. | `github.com/nsgo-dev/game/template/visualnovel` |

`go tool game new -list` prints the same list from the command itself, the default first.

### puzzle2d

A spark slides until a wall stops it and lights every lamp it passes; lighting all of a level's lamps within its moves wins the level, and a slide into a void or running out of moves loses it. The template shows how a puzzle game grows: the rules in `board.go`, where each move is an `undo.History` move that undoes and redoes exactly; the levels in `levels.yaml`, a versioned YAML file with unknown fields refused and a canonical form, which `levels.go` reads and writes; the input map in `game.yaml`, bound into `input.Actions` with swipes from `input.GestureRecognizer`; moves animated with `tween` on a fixed-step `game.Loop`; and the stars of each level kept with `save`. To change the game, add or edit levels in `levels.yaml`, whose tests check that every level can be won in its par; rebind the controls in `game.yaml`; add a tile in `board.go` and draw it in `draw.go`. The template's README lists the controls, the level format and every file.

### visualnovel

*Lantern Crossing*: a cartographer has to get her map across a foggy river before dawn, and the ferryman decides how far to trust her. Two choices earn or lose his trust and the third, at a fork in the river, reaches the harbor or the fog, so the story has a good ending and a lost one.

It is built on [`vn`](vn.md), which presents the script: portraits with expressions, a text box with name plates and a typewriter, choices, the backlog, and auto and skip modes; [`flow`](flow.md), whose dialogue runner plays the [Yarn Spinner](https://www.yarnspinner.dev/) script and keeps its variables; [`save`](save.md), for three save slots and the player's settings; and [`l10n`](l10n.md), for English and Simplified Chinese string tables. The backgrounds and portraits are drawn in code, so it needs no image files. The keyboard, a controller and touch each play all of it, from the input map in `game.yaml`.

| File | What it holds |
| --- | --- |
| `story/story.yarn` | The story. Speakers are cast ids, every line and option has a `#line:` id, `<<bg name>>` changes the background, `<<ending name>>` names the ending, and each node's `chapter:` header names what a save slot shows. |
| `story/cast.yaml` | The characters: name plate colors, stage slots and expressions, in `vn`'s cast format. |
| `strings/en.yaml`, `strings/zh-Hans.yaml` | The interface's strings and the names, chapters and endings in each language. English lines are the script's own text; another language translates each line as `line.<id>`. |
| `story.go` | Loads the story files and shows lines and name plates in the player's language. |
| `novel.go` | The game: the title screen, the story and its menus, input, save slots and settings. |
| `ui.go`, `stage.go` | The buttons and menus over the stage, and the drawn backgrounds and portraits. |
| `story_test.go`, `game_test.go` | Checks that the story files agree (every line translated, every background and ending known, every path ending in an ending), and plays both endings with the keyboard, a controller and touch, saving and loading on the way. |

A save slot keeps the options chosen since the start, the id of the line shown and how many times that line had shown since the last choice, and loading replays that path, because the dialogue runner's saved state holds its variables and visited nodes but not where in a node it stopped. Saving over a used slot asks first. The lines read and the endings found are player settings, kept apart from the slots and saved again when the app leaves the front or quits, so skip mode knows the lines read in every playthrough. The text box keeps clear of a phone's notch and home indicator, and the backlog's Older and Newer buttons scroll it for touch and the mouse.

To make the template your own, write a story in `story/story.yarn` and run its tests, which name every line without a translation and every background, ending or chapter the game does not know; add characters to `story/cast.yaml` with a look in `stage.go`, backgrounds to `backgrounds` in `stage.go`, endings to `endings` in `story.go`, and a language as a string table listed in `story.go`. The template's `README.md` lists the controls.
## Create a project from a template

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -list
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -template starter mygame
```

Without `-template`, `game new` uses `starter`. A name that is not in the list is refused with the names that are. Shell completion for `go tool game` ([Command Line](cli.md#completion)) completes `-template` with the same names. [Command Line](cli.md#new) describes the other flags.

NSGo's own command writes a template from its module without the `game` tool, with an NSGo command that supports [Go module templates](https://nsgo.dev/docs/getting-started/#templates-from-go-modules), such as `go install github.com/nsgo-dev/nsgo/cmd/nsgo@v0.3.0`:

```sh
nsgo init -template github.com/nsgo-dev/game/template -module example.com/mygame mygame
```

Use the module the table names for each template, and add `@<version>` to pin one. The project's `game.yaml` and `nsgo.yaml` keep the template's own game name and identifier until you edit them, where `game new` sets them from the directory or `-name`.

## How the templates are laid out

Each template is a Go module of its own in this repository, because `nsgo init -template` writes the `template` directory at the root of the module it is given. `starter` is the `template/` module itself; every other template is the module `github.com/nsgo-dev/game/template/<name>` in `template/<name>/`:

```text
template/
├─ go.mod                     module github.com/nsgo-dev/game/template
├─ template/                  starter
└─ <name>/
   ├─ go.mod                  module github.com/nsgo-dev/game/template/<name>
   └─ template/               the <name> template
      ├─ go.mod.tmpl
      ├─ go.sum
      ├─ game.yaml
      ├─ nsgo.yaml
      ├─ main.go
      ├─ …_test.go
      ├─ README.md
      └─ .gitignore
packages/game/cmd/game/
├─ templates.go               the registry: one entry per template
└─ _templates/projects/
   ├─ starter/                a copy of template/template
   └─ <name>/                 a copy of template/<name>/template
```

`game new` embeds the copies under `_templates/projects`, so it works from the module cache without this repository. The repository's `go.work` uses every template module, so from a checkout `go tool nsgo dev ./template/<name>/template` runs a template and `go tool nsgo test ./template/<name>/template` tests it against the local game layer.

## Add a template

A template is added in one pull request of its own:

1. Create the module: `template/<name>/go.mod` declaring `module github.com/nsgo-dev/game/template/<name>` with the `go` line of `template/go.mod`, and the project in `template/<name>/template/`. Its name is lower-case letters, digits and `-`, starting with a letter, and not `template`.
2. Add `./template/<name>` to the `use` list of `go.work`.
3. Copy the project into the `game` command, from `packages/game/cmd/game`: `cp -R ../../../../template/<name>/template _templates/projects/<name>`.
4. Add one entry to `projectTemplates` in `packages/game/cmd/game/templates.go`: `newProjectTemplate("<name>", templateModuleRoot+"/<name>", "<one line saying what the game is>")`.
5. Add a row for the template to the table above, in `docs/en/templates.md` and `docs/zh/templates.md`, and a fragment under `changelog.d/`.

The project in `template/<name>/template/` follows these rules, which the tests check where they can:

- `go.mod.tmpl` starts with `module {{.Module}}`, requires a published version of `github.com/nsgo-dev/game/packages/game` that has every package the template uses, and has `tool github.com/nsgo-dev/nsgo/cmd/nsgo`. It is the only file with a template action: `game new` and `nsgo init` render `nsgo.yaml`, `main.go` and any other `.tmpl` file too, but CI renders only `go.mod.tmpl` and the template must also build as it is in the workspace, so each of those must render to itself, and `main.go` cannot hold `{{`, as a composite literal such as `[]T{{...}}` does; put such code in another Go file. Other files are copied as they are and may hold any bytes. A file named `go.mod` would make the directory a module of its own, which neither `go:embed` nor the workspace can include.
- `go.sum` holds the sums of the pinned modules. Write it by turning `go.mod.tmpl` into a `go.mod` in a copy of the template outside the repository and running `GOWORK=off go mod tidy` there.
- `game.yaml` is canonical, as `go run github.com/nsgo-dev/game/packages/game/cmd/gameproject check` reports, and `nsgo.yaml` agrees with it; `game new` replaces the `name` and `id` of both.
- `main.go`, `README.md`, `.gitignore` and tests are there, and `go run ./cmd/game index ../../template` from `packages/game` has nothing left to do.
- Every file the template's author writes is text, as for any project in this repository.

The tests of `packages/game/cmd/game` fail when any of these steps is missing: when the copy under `_templates/projects` differs from the module's `template` directory by a byte, when a `template/*/template` directory has no registry entry, when a registry entry has no copy, module or `go.work` entry, and when this page or its Chinese counterpart has no row naming a template and its module. They also create a project from every template, build it against the local game layer and run its tests.

CI's `templates` job needs no edit for a new template: it lists `template/template` and every `template/*/template` directory and tests each template the way a developer's project gets it: it copies the template out of the workspace, renders `go.mod` for `example.com/<name>game`, and runs `go tool nsgo test ./...` with `GOWORK=off`, so the project builds against the game layer version `go.mod.tmpl` pins rather than this checkout. A template that needs a package not yet published bumps that pin once the package is on `main`.
