### Added

- `pkg/tooling/storecapture`, `game capture`: a versioned YAML store screenshot plan (`store/capture.yaml`) naming each shot's scene, input recording, frame or time and store sizes (presets for the App Store, Google Play, itch.io and mini game share images, or explicit sizes), and a command that checks every scene and recording exists and every image is a PNG of its target size. Rendering the images from the game is not included yet (#1933).
