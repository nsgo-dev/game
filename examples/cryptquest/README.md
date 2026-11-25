# CryptQuest

**CryptQuest** is a top-down tile-based dungeon adventure demonstrating the NSGo 2D Game SDK across every target (macOS, Windows, Linux, Android, iOS, and Web).

## What It Demonstrates

- **Obstacle-avoiding enemy pathfinding (`ai.AStar` + `flow.Machine`)**: Skeleton guardians patrol dungeon waypoints (`EnemyPatrol`), detect the player via proximity or unobstructed `tilemap.Raycast` line-of-sight (`EnemyChase`), and compute optimal paths around stone walls and pillars using a shared `ai.AStar` searcher with reused `FindInto` path buffers.
- **Baked navigation mesh overlay (`navbake.FromTilemap` + `navbake.Draw`)**: The crypt's walkable area is baked from the tilemap's solid cells for a skeleton's radius, rebaked when the sanctum gate opens or closes, and drawn with its convex polygons and portals when `N` is pressed.
- **Multi-page typewriter dialogue (`text.DrawPlatform` + native NSGo UI)**: Speaking with Elder Aldric opens a framed, word-wrapped, typewriter-revealed multi-page dialogue box drawn on the virtual canvas paired with native NSGo `Next` and `Close` buttons.
- **Versioned persistent saves (`save.NewStorageStore`)**: Saves and loads player coordinates, HP, collected Sun Relic, unlocked sanctum gate, visited rooms, and enemy alert/defeated states as a versioned v1 JSON envelope in `core.Storage` that survives app restarts.
- **Cross-platform controls & viewport (`game.Loop`, `tilemap.Camera`, `screen.Viewport`, `input.Actions`)**: Fixed-step 60 Hz simulation, smooth clamped camera following with hit-shake, virtual `320×200` pixel-snapped viewport, hardware keyboard (`WASD` / Arrows / `E` / `F` / `Esc`), on-screen touch/mouse D-pad regions, and a native NSGo HUD.
- **Zero external asset files**: All 16×16 pixel-art dungeon tiles, animated characters, props, and indicators are generated procedurally into an in-memory `*image.RGBA` sprite atlas (`core.ImageSource{Pixels: ...}`).

## Controls

| Action | Keyboard | Touch / Mouse | Native HUD |
| --- | --- | --- | --- |
| Move | `W A S D` or Arrow Keys | On-screen D-pad (bottom-left) | — |
| Interact / Advance Dialogue | `E`, `Space`, or `Enter` | `E` pad (bottom-right) | **Interact** / **Next** |
| Banish Nearby Skeletons | `F` or `J` | `F` pad (bottom-right) | **Banish** |
| Close Dialogue | `Escape` | — | **Close** |
| Show Navigation Mesh | `N` | — | — |
| Save / Load / Reset | — | — | **Save** / **Load** / **Reset** |

## Running and Testing

Run headless tests:

```bash
cd examples/cryptquest
go tool nsgo test ./...
```

Verify builds across all targets:

```bash
cd examples/cryptquest
go tool nsgo check -target macos,windows,linux,android,web
```

Run locally:

```bash
cd examples/cryptquest
go tool nsgo dev
```
