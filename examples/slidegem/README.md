# SlideGem

A touch-first 6×6 match-3 sliding-gem puzzle game built with the NSGo 2D Game SDK — with **zero external asset files**.

## Features

- **Deterministic 6×6 Match-3 Grid (`board.go`)**: Seeded with `random.New(seed)`, guaranteed to start with no immediate matches and at least one valid move, with automatic deadlock detection & reshuffle, cascade gravity drops, combo multipliers, and move budgeting.
- **Smooth Tweens & Visual Juice (`game.go`)**:
  - 60 Hz fixed-step `game.Loop` driving `tween.Group` (`core.EaseOut` swap slides, scale-pop clears, gravity drops, floating score popups) and `tween.Timeline` (invalid swap bounce-back).
  - `particles.Emitter` sparkle bursts on cleared gems.
  - Procedural RGBA faceted gem spritesheet atlas (`sprite.Atlas` + `sprite.Batch`) and procedural 16-bit PCM WAV sound effects (`sound.Mixer` + combo pitch-shifting chimes).
- **Touch-First Input & Cross-Platform Scaling**:
  - `screen.Viewport` (`ModeFit`) scaling with `ViewToGame` coordinate mapping and `input.Actions` supporting both tap-tap selection and direct touch/mouse swipe swaps.
  - Native NSGo HUD overlay (`Score`, `Best`, `Moves`, `Hint`, `New Game`, and `Play Again`) respecting `screen.SafeRectForEnv`.
  - Versioned high-score persistence via `save.NewStorageStore`.
- **Achievements (`achievements.go`, `achievements.yaml`)**: four achievements declared with their Game Center and Play Games IDs and tracked by `achievements.Tracker` in app storage. The game signs in with `achievements.SignIn`, reports progress made before or without sign-in once signed in, shows the unlocked achievement in the status line, and opens the platform's achievements screen from the **Trophies** button. The IDs in `achievements.yaml` stand for the ones you create in App Store Connect and Play Console for your own app.

## Running & Testing

```bash
# Run headless tests
cd examples/slidegem
go tool nsgo test ./...

# Cross-platform target check
go tool nsgo check -target macos,windows,linux,android,web

# Run interactively
go tool nsgo dev
```
