# Troubleshooting

Use this page when setup, build or first-run output stops before the game starts. Run `go tool nsgo doctor` first, then compare the exact message with the entries below. File-format failures are covered by [Errors](errors.md).

## Setup and module errors

### `verifying go.mod: ... sum.golang.org/lookup/...: 404 Not Found`

The module proxy cannot find the requested game-layer version or the network is serving a stale module path. Check the module path and version in `go.mod`, run `go mod tidy`, then retry with network access. A project created with `go run github.com/nsgo-dev/game/cmd/game@latest new` records a published version. A checkout build may need `-version` to select one.

### `go: no such tool "game"`

The project came from `nsgo init -template`, which installs the NSGo tool but not the game tool. Add the game tool with `go get -tool github.com/nsgo-dev/game/cmd/game@latest`, or run the repository's `game new` command and use `go tool game` from that module.

### `project: nsgo.yaml disagrees with game.yaml: id is ...`

The project metadata was edited in one file only. Edit `game.yaml`, then run `go run github.com/nsgo-dev/game/cmd/game project sync` so the mirrored `nsgo.yaml` fields match.

## Platform and release errors

### `error ios_simulator: the iOS SDK needs Xcode: xcrun ... executable file not found`

The iOS simulator target needs macOS and Xcode. Build desktop or web on Linux and Windows, or run the iOS target on a Mac with Xcode and its command-line tools selected.

### `signed with the debug key: set sign.keystore for store releases`

An Android store build has no release keystore. Configure `sign.keystore` and its passwords in the release settings. Debug builds can use the generated debug key, but do not upload them to a store.

### `game credits: game.yaml has no assets section`

The project has no declared asset manifest. Add an `assets` section before generating release credits, or treat the message as informational during development. A release pipeline that requires credits should fail the build until every shipped asset has an owner and license.

### `needs adb` or `needs a Mac with Xcode`

`nsgo doctor` reports missing tools for targets you selected. Install Android SDK Platform-Tools for `adb`. Install Xcode and its command-line tools on macOS for iOS builds. The Android APK compiler itself can run without `adb`, but device deployment cannot.

## Runtime, audio and logs

### Browser audio is silent until the first tap

Browsers block autoplay before a user gesture. Start or unlock the mixer from a pointer or key event, then play the requested sound. This is expected browser policy, not a missing asset.

Logs from `go tool nsgo dev` appear in the terminal. Packaged Apple apps write to the unified log under the bundle ID, Android output is available through `adb logcat`, and browser builds use DevTools Console. Panics should include the stack and the last game state in the crash reporter when the app wires one. For the debugger and runtime profiles, follow NSGo's [running and debugging guide](https://nsgo.dev/docs/running-and-debugging/).

See [gamedebug](gamedebug.md) for the in-game overlay, console and inspector, and [Errors](errors.md) for strict file-loader diagnostics.
