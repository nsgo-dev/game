# Coming from GameMaker

NSGo keeps the fast iteration and data-driven workflow familiar to GameMaker while using Go for the game code.

## Projects and rooms

Use `game.yaml` for the project identity, window, display, input map, assets and export presets. A room maps to a `scene` or `sceneformat` document. Keep authored scenes and resources in YAML so they can be reviewed and merged, then load them through the scene packages.

## Objects and events

Use `sceneformat` components and connections for object data and reusable behavior. Use `pkg/entities` when many objects share systems and queries. Input actions in `pkg/input` replace checking keyboard constants directly, and `game.Loop` replaces a room's step and draw events with fixed-step updates and interpolation.

## Drawing and assets

Use `screen.View` and `gameui` for menus and HUDs, `sprite` and `tilemap` for 2D art, and `pkg/assets` for atlases, audio and hot reload. The editor opens the same text scene files used at runtime.

## Saves and shipping

Use `pkg/save` for versioned JSON settings and save slots. Run `go tool nsgo check`, then build with the documented `nsgo` release commands and export presets.

