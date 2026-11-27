### Fixed

- `game/tween`: `Tween.Reset` clears the started state so replaying camera tweens (`CameraPan`, `CameraZoom`, `CameraRotate`, `CameraMove`, `CameraFollowTransition`) re-captures the camera's current position, zoom, and rotation instead of snapping back to the first run's origin (#1589).
