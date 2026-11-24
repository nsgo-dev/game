# Field Medic

**Field Medic** is a small game that ships its own scene editor with an editor plugin built in ([#492](https://github.com/nsgo-dev/game/issues/492)). Four patients lose a hit point every second and a half; pressing a patient heals it by three, up to its max, and the game ends when every patient is lost. Its ward is a scene file, `scenes/ward.scene.yaml`, whose objects carry a `health` component, and the game reads its main scene from `game.yaml`.

It is a sample of the editor's plugin API rather than a showcase game, so it is not listed on the website's showcase.

| Package | What it holds |
| --- | --- |
| `.` (`fieldmedic`) | the game: the ward (`ward.go`) on a fixed-step `game.Loop` drawing one `Canvas`, with a native HUD and game-over screen (`game.go`) |
| `health` | the `health` component type (`hp`, `max`), which the game and the editor both register, and `Read`, which fills in the defaults |
| `healthplugin` | the editor plugin: a **Ward** panel listing every patient's hit points with a **Heal All** button, an inspector for the `health` component with a bar of the hit points left, and an **Admit** tool that places a patient where the viewport is pressed |
| `cmd/editor` | the game's own editor command, which registers the component and the plugin |

The game binary does not import the editor: only `healthplugin` and `cmd/editor` do, so the plugin's code stays out of the shipped game.

## Play

From `examples/fieldmedic/`:

```sh
go tool nsgo dev                 # run it on this computer
go tool nsgo dev -target web     # run it in a browser
```

Press or click a patient to heal it; `R`, or **Restart** once every patient is lost, starts the ward again.

## Edit the ward with the plugin

```sh
go run ./cmd/editor              # the project in this directory
go run ./cmd/editor path/to/fieldmedic
```

`game.yaml` turns the plugin on:

```yaml
editor:
  plugins: [health]
```

The editor opens the project's main scene. Select a patient to edit its hit points in the plugin's inspector, press **Heal All** in the Ward panel (one step that one Undo takes back), or choose **Admit** in the tool bar and press the viewport to place a patient. Save writes the scene file in its canonical form.

`go tool game editor .` opens the same project without the plugin: it names `health` in the status bar as a plugin it was built without, and the inspector shows the component's generic fields. `cmd/editor` instead fails to start when `game.yaml` lists a plugin it lacks.

## Test

```sh
go tool nsgo test ./...                                   # the ward, the plugin in the editor, the editor command
go tool nsgo check -target macos,windows,linux,android,web
```
