### Added

- `game/cutscene`: added version 2 YAML timeline cutscenes (`kind: timeline`) with camera (`hold`, `pan`, `follow`), actor (`walk`, `state`, `drop`, `patrol`), dialogue (l10n line IDs only), sound and music (with `fadeIn`/`fadeOut`) tracks, deterministic fixed-loop `TimelinePlayer` with `Seek`, `Player.PlayTimeline`/`Step`/`Dialogue`/`TimelineScene`, and published `cutscene.v1.json`/`cutscene.v2.json` schemas (#1055).
- `game/refs` and `game/tooling/assetcheck`: tracked `*.cutscene.yaml` scene and audio paths in `refs` and checked cutscene object IDs, dialogue line IDs (against project l10n tables) and audio paths in `game check` with file and line diagnostics (#1055).
