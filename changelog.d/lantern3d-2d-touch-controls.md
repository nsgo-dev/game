### Fixed

- Lantern 3D: added on-screen touch controls (`input.TouchControls` from `controls.touch.yaml`) for the 2D Courtyard Map so touch-only devices can walk in all eight directions with a floating stick in the left half and enter the 3D shrine with a contextual `USE` button on the right whenever the traveler is at the portal. The controls stay hidden in 3D mode (leaving 3D drag-to-orbit and pinch-to-zoom unchanged) and on desktop until the screen is touched, and reset cleanly on scene switch or pause so no held direction leaks (#1292).
