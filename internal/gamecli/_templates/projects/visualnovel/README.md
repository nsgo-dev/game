# Lantern Crossing: a visual novel template

A short branching visual novel built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game`](https://github.com/nsgo-dev/game). A cartographer, Mira, has to get her map across a foggy river before dawn, and the ferryman, Tobin, decides how far to trust her. Three choices decide whether they reach the harbor or lose themselves in the fog.

It shows:

- A [Yarn Spinner](https://www.yarnspinner.dev/) script played by `vn.Presenter` on `flow`'s dialogue runner: portraits with expressions in stage slots, a text box with name plates and a typewriter, choices, variables that change the ending, a backlog, and auto and skip modes that skip only the lines already read.
- Backgrounds and portraits drawn in code, so the project needs no image files.
- Three save slots that keep the path to the line shown and replay it on load, and player settings (language, lines read, endings found) kept apart from them with `save` and saved again when the app leaves the front or quits.
- English and Simplified Chinese through YAML string tables (`l10n`): the lines, the options, the name plates and the menus.
- Keyboard, controller and touch input from the input map in `game.yaml`, with every menu reachable by each of them.
- Headless tests that play the story to both endings with each kind of input, saving and loading through the menus on the way, and check that the story files agree with each other.

## Running and testing

```sh
go tool nsgo dev                       # this machine, reloading on save
go tool nsgo dev -target web           # the browser
go tool nsgo dev -target ios_simulator # or ios, android
go tool nsgo test ./...
go tool nsgo check -target macos,windows,linux,android,web
```

Build release artifacts into `dist/` with `go tool nsgo build -target macos_arm64,windows_x64,linux_x64,web`.

## Controls

| Action | Keyboard | Controller | Touch and mouse |
| --- | --- | --- | --- |
| Go on, pick | Space, Return | A (South) | Tap the stage or an option |
| Move between options and menu items | Up, Down | D-pad | |
| Auto mode | A | X (West) | Auto |
| Skip mode | S | Right shoulder | Skip |
| Backlog | L, then Up and Down to scroll | Left shoulder, then the D-pad to scroll | Log, then Older and Newer to scroll |
| Menu: save, load, title screen | Escape | Start | Menu |
| Back | Backspace | B (East) | Back |

The keys and buttons are the input map in `game.yaml`; change them there. Saving to a used slot asks first: pick it again to overwrite it.

## Layout

- `story/story.yarn` is the story. Lines name their speaker by cast id (`tobin: ...`), carry a `#line:` id, and steer portraits with `#expr:` and `#at:` tags and `<<show>>`, `<<hide>>` and `<<expr>>` commands. `<<bg name>>` changes the background and `<<ending name>>` names the ending. Each node has a `chapter:` header that names the chapter a save slot shows.
- `story/cast.yaml` is the cast: the stage slots and each character's name plate color, slot and expressions (`vn`'s cast format).
- `strings/en.yaml` and `strings/zh-Hans.yaml` are the string tables. English lines are the script's own text; another language translates each line and option as `line.<id>`, and every language has the menu, chapter, ending and name strings.
- `story.go` loads those files and resolves lines and name plates in the player's language.
- `novel.go` is the game: the title screen, the story and its menus, input, save slots and settings.
- `ui.go` is the interface over the stage: the Auto, Skip, Log and Menu buttons, the menus, and the stage canvas's drawing.
- `stage.go` draws the backgrounds and paints the portraits.
- `game.yaml` is the project file: the game's name, id, version, window, input map and export presets. After editing it, run `go run github.com/nsgo-dev/game/cmd/gameproject sync` to update `nsgo.yaml`, and `gameproject check` to check it.
- `story_test.go` checks the story files: canonical YAML, a translation for every line, a background and strings for everything the script names, and an ending at the end of every path. `game_test.go` plays the game, and `project_test.go` checks `game.yaml`.

## Where to change it

- Write your own story in `story/story.yarn`, and run `go tool nsgo test ./...`: `story_test.go` names every line that lacks a translation, every background or ending the game does not know, and every path that ends without an ending.
- Add a character to `story/cast.yaml`, give them a look in `looks` in `stage.go` (or load real portraits with `sprite.LoadAtlas`), and a `name.<id>` string in each string table.
- Add a background to `backgrounds` in `stage.go`, or replace the drawn ones with images.
- Add an ending to `endings` in `story.go`, with `ending.<name>` and `ending.<name>.text` strings.
- Add a language: a string table under `strings/`, its embed in `story.go`, and its locale in `languages`.
- Add voice lines with `vn.EffectVoices` and `#voice:` tags, as the `vn` package's documentation describes.

## Git setup

Each clone enables the object-by-object scene merge driver once (`.gitattributes` names it, while Git keeps driver config local to `.git/config`):

```sh
go tool game merge -setup
```
