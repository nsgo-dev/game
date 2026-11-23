# Cave Run — Game Design Document

**Cave Run** is the playable 2D sample game for `github.com/nsgo-dev/game` ([#8](https://github.com/nsgo-dev/game/issues/8)). It demonstrates the full 2D package stack — `game.Loop`, `input`, `sprite`, `tilemap`, `scene`, `text`, and `sound` — inside a compact, responsive retro platformer whose HUD and menus are ordinary native NSGo views layered over the game `Canvas`.

## 1. Core Loop

1. **Explore & Collect**: The player controls a cavern explorer starting in the Entrance Alcove (`spawn`) of a subterranean ruin (`level1.tmj`).
2. **Navigate Hazards**: Jump across stone ledges and one-way wooden bridges, avoid bubbling lava pits (`hazard`) and patrolling cave slimes (`slime`), and collect shimmering cyan gems (`gem`, +100 pts each).
3. **Damage & Recovery**: Touching a slime or lava costs 1 heart (out of 3), triggers brief invulnerability frames + knockback, and plays `sfx_hurt.wav`. Falling into lava respawns the explorer on the last safe ledge (or ends the run at 0 hearts).
4. **Clear the Cave**: Reaching the swirling amethyst exit portal (`goal`) triggers `sfx_win.wav`, adds a heart bonus, and opens the Victory overlay with score summary and Play Again / Menu actions.

## 2. Virtual Resolution & Camera

- **Virtual Canvas**: `320×180` logical pixels (16:9 aspect ratio, showing `20×11.25` tiles of `16×16` pixels at a time).
- **Scaling**: Integer or crisp nearest-neighbor letterboxed fit inside the NSGo window via `screen`, preserving pixel-art edges across macOS, iOS, Windows, Linux, Android, and Web.
- **Orientation**: the root scope requests landscape (`cx.RequestOrientation(core.OrientationLandscape)`), so an iPhone or Android phone turns sideways and stays there while the game runs. `build.targets` in `nsgo.yaml` does not list iOS, so a plain `nsgo build` makes no iPhone package; build one on a Mac with Xcode with `go tool nsgo build -target ios`, or `-target ios_simulator` for the simulator (CI checks both targets on its macOS runner).
- **World & Camera**: `level1.tmj` is `40×14` tiles (`640×224` virtual pixels). The tilemap camera smoothly tracks the explorer with horizontal look-ahead and clamps cleanly to the world bounds.
- **Fixed Timestep**: `game.Loop` runs physics, collision, enemy patrols, and `sprite.Animation` updates at `60 Hz`, interpolating render positions by `alpha`.

## 3. Controls

All gameplay inputs are mapped through named `input` actions (`bindControls` in `game.go`) so keyboard, gamepad, touch, and mouse work identically. `input.Actions.WatchKeys` hears held keys anywhere on the window and `WatchGamepads` reads every connected controller; one `Actions.Step` per fixed step samples them all:

| Action | Keyboard | Gamepad | Touch / Mouse |
| --- | --- | --- | --- |
| Move Left | `A`, `←` | D-Pad Left, Left Stick `< -0.3` | On-screen Left button (HUD overlay), lower-left canvas region |
| Move Right | `D`, `→` | D-Pad Right, Left Stick `> 0.3` | On-screen Right button (HUD overlay), lower-middle canvas region |
| Jump | `Space`, `W`, `↑`, `Z` | South (`A` / Cross) | On-screen Jump button (HUD overlay), lower-right canvas region |
| Start / Play Again | `Enter` (or Jump on the title) | `Start` / Menu | Play / Play Again buttons |
| Pause / Resume | `Esc`, `P` | `Start` / Menu | Native HUD Pause button |
| Quick Restart | `R` | `Select` / Back | Pause / End overlay Restart button |
| 1000-firefly stress swarm | `F` | North (`Y` / Triangle) | Pause menu Fireflies button |

The stick reads as an analog axis with an axial dead zone of `0.3`, so a half push walks at half speed. A pause, restart or scene change resets the actions, and a key or button held through it stays inactive until it is released.

Movement uses responsive platformer feel: short coyote time (`~100 ms`), jump input buffering (`~100 ms`), and variable jump height when releasing Jump early.

## 4. Tilemap Layout (`assets/level1.tmj`)

`level1.tmj` is a Tiled JSON map (`40×14` orthogonal, `16×16` tiles) using the embedded `cave_tiles` tileset (`assets/tileset.png`, `64×32`, 8 tiles):

- **Tileset (`assets/tileset.png`)**:
  - `0` (`stone_solid`, `solid=true`): Chiseled subterranean stone block.
  - `1` (`stone_moss`, `solid=true`): Stone block topped with lush cave moss.
  - `2` (`bridge`, `oneWay=true`): One-way wooden timber platform (pass-through from below, solid on landing).
  - `3` (`wall_bg`): Dark indigo-slate cave background wall.
  - `4` (`wall_arch`): Background wall with carved archway masonry.
  - `5` (`crystal_decor`): Glowing decorative cave crystals.
  - `6` (`lava_0`, `hazard=true`, animated `6 -> 7` every `250 ms`) & `7` (`lava_1`, `hazard=true`): Molten lava surface.
- **Layers**:
  1. `background` (tilelayer): Atmospheric cave masonry, arches, and crystal clusters.
  2. `terrain` (tilelayer): Three interconnected chambers designed so every gap is `≤ 3` tiles wide and every vertical rise is `≤ 2` tiles high:
     - **Chamber 1 — Entrance Alcove** (cols `1–11`): Safe mossy floor, starter one-way bridge with first gem, and entry ledge.
     - **Chamber 2 — Sunken Crystal Cavern** (cols `12–26`): Two lava pits spanned by wooden bridges, a central island patrolled by `slime_1`, and an upper timber bridge holding a gem pair.
     - **Chamber 3 — Upper Shrine** (cols `27–38`): Patrol floor guarded by `slime_2`, a bonus gem tucked in the lower-right alcove, and ascending bridges leading up to the exit portal ledge.
  3. `objects` (objectgroup):
     - `spawn` (1): Explorer start position in Chamber 1.
     - `gem` (8): Collectible cave gems (`value: 100` each; `800` max gem score).
     - `slime` (2): Patrolling enemies with `patrolMinX`, `patrolMaxX`, and `speed` custom properties.
     - `goal` (1): Exit portal at the Upper Shrine.

## 5. Sprites, Animations & Bitmap Font

### Sprite Atlas (`assets/sprites.png` + `assets/sprites.json`)

A `128×64` RGBA sprite sheet (`16×16` cells) paired with an Aseprite JSON-hash descriptor compatible with `sprite.LoadAtlas`:

| Tag / Frame | Frames | Duration | Playback | Purpose |
| --- | --- | --- | --- | --- |
| `idle` | `hero_idle_0..3` | `220 ms` | Loop | Explorer standing breath & headlamp bob |
| `run` | `hero_run_0..3` | `90 ms` | Loop | Explorer stride & fluttering scarf |
| `jump` | `hero_jump_0..1` | `140 ms` | Loop | Airborne pose |
| `gem` | `gem_0..3` | `120 ms` | Loop | Spinning faceted cyan cave gem |
| `slime` | `slime_0..3` | `150 ms` | Loop | Squishing/hopping purple cave slime |
| `portal` | `portal_0..3` | `120 ms` | Loop | Swirling amethyst & gold exit portal |
| `spark` | `spark_0..2` | `70 ms` | Once (`repeat: "1"`) | Gem pickup / jump dust burst |
| HUD Icons | `heart_full`, `heart_empty`, `gem_icon` | Static | — | Canvas/HUD status icons |

### Bitmap Font (`assets/font.png` + `assets/font.fnt`)

A `128×48` pixel bitmap font in AngelCode BMFont text format (`8×8` cells, printable ASCII `32–126`, fixed-width digits `0–9` for jitter-free counters) used for in-canvas floating popups (`+100`, `CLEAR!`) and lightweight canvas text rendering.

## 6. Audio (`assets/*.wav`)

All audio assets are uncompressed 16-bit mono PCM WAV files at `22050 Hz`, routed through `sound.Mixer` with separate `"effects"` and `"music"` volume groups:

| File | Group | Description |
| --- | --- | --- |
| `sfx_jump.wav` | `effects` | Crisp rising 25%-duty pulse pitch sweep (`0.18 s`) |
| `sfx_gem.wav` | `effects` | Sparkling 4-note ascending E-major chime arpeggio (`0.33 s`) |
| `sfx_hurt.wav` | `effects` | Low descending buzz + retro LFSR noise burst (`0.26 s`) |
| `sfx_win.wav` | `effects` | Upbeat 6-note chiptune victory fanfare (`0.94 s`) |
| `music_cave.wav` | `music` | Seamless 4-bar (`6.40 s`, 150 BPM) D-Dorian cavern loop (pulse lead, arpeggio shimmer, triangle bass) |

## 7. Native HUD & Menus

Keeping with NSGo's architecture, gameplay renders into a single `core.Canvas` while UI chrome stays native NSGo views overlaid on top:

- **In-Game HUD**: Top-left hearts + collected gem counter (`3/8`), top-center score, top-right Pause button; optional bottom touch controls rendered on touch/mobile viewports.
- **Pause Menu**: Semi-transparent native modal pausing `game.Loop` and ducking/pausing music, with **Resume**, **Restart**, **Fireflies** (the stress swarm), and **Music / SFX Volume** controls persisted by `sound.Mixer`. While the swarm is on, the score pill also shows the frame's sprite count (`SPRITES 1012`).
- **End Overlays**: **Cave Cleared** (score breakdown, gems collected, replay) and **Try Again** (instant restart on 0 hearts).

## 8. Performance Targets & Headless `nsgotest` Plan

- **Runtime Budget**: Hold the display's rate on desktop and the web, and `60 fps` on iOS and Android, with 1000 moving sprites on screen, measured with NSGo's `--nsgo-fps` monitor and recorded in `README.md`.
- **Stress Swarm**: `swarm.go` adds 1000 fireflies (`swarmSize`) that drift and twinkle over the camera's view, all at the same `Z` and below full opacity, so `sprite.Batch` draws the whole swarm with one `DrawAtlas`. Moving them allocates nothing; NSGo v0.3.0's `Drawing` still copies each image draw's source onto the heap, so the tilemap's tiles allocate on every frame, and the swarm adds the copies for its own `DrawAtlas` and for the second run it splits the level's sprites into (its `Z` sits between the gems and the slimes and explorer). `BenchmarkLevel1000Sprites` measures a step and a step plus the frame's drawing with the swarm on.
- **Headless Completion Test (`nsgotest`)**:
  1. Boot the `caverun` app in a headless `nsgotest` harness with deterministic fixed-step ticks (`1/60 s`).
  2. Drive the explorer from `spawn` across Chambers 1–3 using scripted holds/releases (jumping the starter bridge, both lava pits, both slimes, and ascending to the Upper Shrine), once each with the HUD buttons, held keyboard keys, a game controller, and held keys with the 1000-firefly swarm on.
  3. Assert at least one gem pickup increments score (`+100`) and triggers the `spark` animation, no unhandled panics occur, and reaching `goal` transitions the scene to the Victory state within a bounded step count.
  4. Verify pause/resume freezes simulation time and volume changes propagate cleanly through `sound.Mixer`.
