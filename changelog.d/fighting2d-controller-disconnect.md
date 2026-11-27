### Fixed

- `template/fighting2d`: keep each fighter's assigned game controller ID via `input.Players` instead of reassigning by sorted position every step, and pause versus matches with a reconnect panel (same-pad reconnect, free-pad press-to-reassign, or continue on keyboard) when an assigned controller disconnects (#1567).
