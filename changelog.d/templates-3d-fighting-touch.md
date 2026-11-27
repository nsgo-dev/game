### Changed

- `template/action3d`, `template/fps3d`, `template/racing3d`, `template/fighting2d`: moved all four templates onto `input.TouchControls` with versioned `controls.touch.yaml` layouts (plus `versus.touch.yaml` in `fighting2d`) anchored inside the safe area, visible from the first frame on native phones and phone browsers, hidden on desktop until touched and hidden again on keyboard or controller input, and requesting landscape orientation (`core.OrientationLandscape`).
- `template/fps3d`: added Weapon 1 (`W1`) and Weapon 2 (`W2`) touch buttons and unified touch sticks with keyboard and gamepads on up-positive `moveY` and `lookY`, removing `touchMoveY` and `touchLookY`.
- `template/fighting2d`: replaced the `game.yaml` touch/mouse regions with `TouchControls` buttons and added two-player touch versus on one phone, compacting player 1 onto the left half and mirroring player 2 onto the right half.
