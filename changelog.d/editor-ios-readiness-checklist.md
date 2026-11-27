### Fixed

- `pkg/editor`: read real `nsgo.yaml` keys (`id`, `icon.apple` / `icon.png`, `sign.identity`, `sign.profile`, `apple.privacy`), `resources/PrivacyInfo.xcprivacy`, and `*.touch.yaml` layouts in the iOS release readiness checklist via shared `internal/releaseplan` checks, write agent privacy manifests under `resources/PrivacyInfo.xcprivacy` with NSGo's required-reason APIs, and generate `controls.touch.yaml` from existing `game.yaml` actions instead of adding an unused `touch_action` (#1667).
