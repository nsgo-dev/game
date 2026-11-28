### Added

- `pkg/editor/editormcp`: reserves the live Play tool names (`play_start`, `play_stop`, `play_step`, `play_input`, `play_state`, `play_frame`, `output_read`) and exports their argument and result types with `ValidatePlayStep` and `ValidatePlayInput`; the server does not register the tools yet (#1743).
