### Fixed

- `pkg/editor`: wire the Build and Release screen's default store uploader to `go tool nsgo publish` (itch.io via `BUTLER_API_KEY`, Google Play internal via `NSGO_PLAY_SERVICE_ACCOUNT`, and TestFlight via `NSGO_APP_STORE_KEY`), show exact `nsgo.yaml` / NSGo default itch.io channels per target, and list Steam-channel desktop builds as `"upload with SteamPipe"` instead of offering them for itch.io (#1762).
