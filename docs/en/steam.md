# Shipping on Steam

Configure Steamworks app and depot IDs, bundle the Steamworks redistributable through `nsgo-natives.yaml`, build desktop depots with `go tool game release -channel steam`, upload builds via SteamPipe to a staging branch, map NSGo's save directories to Steam Auto-Cloud, and verify your game against Steam Deck and Steam review requirements.

## Steamworks setup

Create your application in the [Steamworks Partner Portal](https://partner.steamgames.com/) and note its **App ID**. Under **SteamPipe → Depots**, create one depot per desktop operating system you ship:

| Platform | NSGo target | Typical Steamworks OS setting |
| --- | --- | --- |
| macOS (Apple Silicon + Intel) | `macos_universal` | macOS (64-bit) |
| Windows (64-bit) | `windows_x64` | Windows (64-bit) |
| Linux / SteamOS (64-bit) | `linux_x64` | Linux (64-bit) |

Steamworks assigns each depot its own **Depot ID** (commonly sequential after the App ID, such as `AppID + 1`, `AppID + 2`, `AppID + 3`; always copy the exact IDs shown in the portal).

### Local testing with `steam.app_id`

When launching a game outside the Steam client during development, Steamworks looks for `steam_appid.txt` in the working directory. Set `steam.app_id` in `nsgo.yaml`:

```yaml
steam:
  app_id: 480 # replace with your Steamworks App ID (480 is Spacewar, Valve's test app)
```

- `go tool nsgo dev` and `go tool game dev` write `steam_appid.txt` into the native build directory and start the app with that directory as its working directory so `SteamAPI_Init` succeeds while the Steam client is open.
- `go tool game release -channel steam` (and `nsgo build -channel steam`) never writes `steam_appid.txt` into shipped depots and ignores any `steam_appid.txt` inside `resources/`. Never commit or upload `steam_appid.txt` to a Steam depot.

### Bundling the Steamworks redistributable (`nsgo-natives.yaml`)

NSGo games bind Steamworks dynamically via `purego` / `syscall` with zero cgo. Download the Steamworks SDK from the Steamworks Partner Portal and copy the three desktop redistributables (`libsteam_api.dylib`, `steam_api64.dll`, `libsteam_api.so`) and Valve's SDK license file into a Go package inside your project (for example `internal/steamlib/`), then declare them in `internal/steamlib/nsgo-natives.yaml`:

```yaml
version: 2
libraries:
  macos_arm64:
    - name: libsteam_api.dylib
      path: lib/macos/libsteam_api.dylib
      sha256: "<64-hex-sha256-of-libsteam_api.dylib>"
      licenses: [licenses/STEAMWORKS_LICENSE.txt]
  macos_x64:
    - name: libsteam_api.dylib
      path: lib/macos/libsteam_api.dylib
      sha256: "<64-hex-sha256-of-libsteam_api.dylib>"
      licenses: [licenses/STEAMWORKS_LICENSE.txt]
  windows_x64:
    - name: steam_api64.dll
      path: lib/windows-x64/steam_api64.dll
      sha256: "<64-hex-sha256-of-steam_api64.dll>"
      licenses: [licenses/STEAMWORKS_LICENSE.txt]
  windows_arm64: []
  linux_x64:
    - name: libsteam_api.so
      path: lib/linux-x64/libsteam_api.so
      sha256: "<64-hex-sha256-of-libsteam_api.so>"
      licenses: [licenses/STEAMWORKS_LICENSE.txt]
  linux_arm64: []
  ios: []
  ios_simulator: []
  tvos: []
  tvos_simulator: []
```

- Compute each digest with `shasum -a 256 <file>` (macOS/Linux) or `Get-FileHash -Algorithm SHA256 <file>` (Windows).
- Valve ships `libsteam_api.dylib` as a universal macOS binary, so the same file can serve both `macos_arm64` and `macos_x64` when building `macos_universal`.
- Import the package (for example `_ "example.com/mygame/internal/steamlib"`) in your desktop build so NSGo copies each library into the right location before signing:
  - **macOS**: `<Name>.app/Contents/Frameworks/libsteam_api.dylib`
  - **Windows**: `steam_api64.dll` beside `<Name>.exe`
  - **Linux**: `libsteam_api.so` beside `<Name>`
- Because Steamworks redistributables are proprietary, keep them out of public repositories or fetch them in CI from a private artifact store rather than a public URL.
- Today, [`pkg/online/presence`](presence.md#steam-rich-presence) automatically probes `<exeDir>/../Frameworks/libsteam_api.dylib` on macOS and `<exeDir>` on Windows and Linux, while [`pkg/modding`](modding.md#steam-workshop-workshop) loads by library name or `SteamWorkshopOptions.LibraryPath`. A single shared Steamworks client package that owns `SteamAPI_Init`, the callback pump, overlay pause events and join events for every package is planned in [#1763](https://github.com/nsgo-dev/game/issues/1763).

## Building depots

Build all three desktop depots in one run with `go tool game release`:

```sh
go tool game release -channel steam -target macos_universal,windows_x64,linux_x64
```

Why pass `-target macos_universal,windows_x64,linux_x64` explicitly:

- If `game.yaml` declares `export.presets` that include mobile or web platforms, `game release` uses those presets unless `-target` overrides them, and `-channel steam` refuses non-desktop targets.
- Without `export.presets`, `-channel steam` defaults to `macos,windows,linux`, which builds separate `macos_arm64` and `macos_x64` directories; passing `macos_universal` joins both macOS architectures into one universal `.app` bundle, which matches a single macOS Steam depot.
- `macos_universal` requires a macOS host. On Linux or Windows hosts, build `-target windows_x64,linux_x64` there and build `macos_universal` on macOS.
- If Steam is the project's only distribution channel, you can also set `updates: { channel: steam }` in `nsgo.yaml` so `-channel steam` becomes the default.

`go tool game release` runs `nsgo build` with `GOFLAGS=-tags=release` (compiling out `pkg/debug`, `pkg/tooling/hotreload`, `pkg/tooling/devstate`, and physics/AI debug drawing), verifies the built desktop binaries against leaked debug symbols, checks asset license credits (writing `CREDITS.md` beside the depots; run `go tool game credits -out resources/CREDITS.md` before building to include it inside every depot), and reminds you if `compliance.steam.content_survey` in `nsgo.yaml` is still `false`.

Each depot is written to `<output>/<name>-<version>-<target>/` with no installers, no self-updater and no `steam_appid.txt`:

| Depot directory | Contents |
| --- | --- |
| `dist/<name>-<version>-macos_universal/` | `<name>.app` (signed and notarized when `sign.identity` and `sign.notarize` are configured; holds `Contents/Resources/` and `Contents/Frameworks/`). |
| `dist/<name>-<version>-windows_x64/` | `<name>.exe`, runtime files from `resources/`, `THIRD_PARTY_NOTICES.txt`, and declared Windows DLLs (such as `steam_api64.dll`). |
| `dist/<name>-<version>-linux_x64/` | `<name>`, `steam-launch.sh`, `nsgo-gtk/` (GTK 4 runtime and `nsgo-gtk/licenses/`), `<id>.desktop`, runtime files from `resources/`, `THIRD_PARTY_NOTICES.txt`, and declared Linux `.so` libraries (such as `libsteam_api.so`). |

Sign and notarize your macOS `.app` (`sign.identity`, `sign.notarize`, or `NSGO_NOTARY_*` in CI) and Authenticode-sign your Windows `.exe` and DLLs (`sign.certificate` + `NSGO_CERTIFICATE_PASSWORD` or `sign.sign_command`) so players do not hit Gatekeeper or SmartScreen warnings when launching through Steam. See [Building and Shipping](shipping.md) for signing configuration.

## Launch options and Linux runtime

Configure one launch option per operating system in Steamworks under **Installation → General Installation**:

| OS | Executable | Working directory | Notes |
| --- | --- | --- | --- |
| macOS | `<Name>.app` | (empty / depot root) | Universal `.app` bundle from `macos_universal`. |
| Windows (64-bit) | `<Name>.exe` | (empty / depot root) | Executable from `windows_x64`. |
| Linux (64-bit) | `steam-launch.sh` | (empty / depot root) | Wrapper script generated by `nsgo build -channel steam` for `linux_x64`. |
| Linux (arm64, optional) | `<Name>` | (empty / depot root) | Requires GTK 4 in the host runtime (no `nsgo-gtk/` bundle). |

Under **Installation → Linux Runtime**, select **Steam Linux Runtime 3.0 (sniper)** (or 4.0 when available). Steam Linux Runtime supplies the display stack but does not ship GTK 4.14+. On `linux_x64`, `nsgo build -channel steam` bundles GTK 4 and its dependencies in `nsgo-gtk/` alongside `steam-launch.sh`:

- `steam-launch.sh` checks `ldconfig` for a system `libgtk-4.so.1` of version 4.14 or newer.
- Only when the system lacks GTK 4.14+ (as inside Steam Linux Runtime 3.0/4.0) does it prepend `nsgo-gtk/lib` to `LD_LIBRARY_PATH` and `nsgo-gtk/share` to `XDG_DATA_DIRS`, then `exec`s your game binary with all launch arguments intact.
- Keep `nsgo-gtk/`—including `nsgo-gtk/licenses/` (`NOTICE`, `build.sh`, and upstream license texts)—in your Linux depot. It provides the LGPL license notices and corresponding source/build instructions for the bundled GLib, Pango and GTK libraries.

## Uploading with SteamPipe

`nsgo publish` does not yet upload builds to Steam ([nsgo-dev/nsgo#2691](https://github.com/nsgo-dev/nsgo/issues/2691)). Upload depots with Valve's `steamcmd` and SteamPipe VDF build scripts.

Create a `steam/` directory at your project root with one app build script and three depot build scripts. Replace `1000` with your App ID, `1001`–`1003` with your Depot IDs, and `My Game-1.0.0` with your `<name>-<version>` prefix (or copy the three depot directories to fixed paths such as `dist/steam/macos`, `dist/steam/windows`, `dist/steam/linux` in your release script so the VDFs stay unchanged across versions):

```text
// steam/app_build.vdf
"AppBuild"
{
	"AppID" "1000"
	"Desc" "1.0.0 release candidate"
	"BuildOutput" "../build/steam-output"
	"ContentRoot" "../dist"
	"SetLive" "staging"
	"Depots"
	{
		"1001" "depot_macos.vdf"
		"1002" "depot_windows.vdf"
		"1003" "depot_linux.vdf"
	}
}
```

```text
// steam/depot_macos.vdf
"DepotBuild"
{
	"DepotID" "1001"
	"FileMapping"
	{
		"LocalPath" "My Game-1.0.0-macos_universal/*"
		"DepotPath" "."
		"recursive" "1"
	}
	"FileExclusion" "*.pdb"
	"FileExclusion" ".DS_Store"
	"FileExclusion" "steam_appid.txt"
}
```

```text
// steam/depot_windows.vdf
"DepotBuild"
{
	"DepotID" "1002"
	"FileMapping"
	{
		"LocalPath" "My Game-1.0.0-windows_x64/*"
		"DepotPath" "."
		"recursive" "1"
	}
	"FileExclusion" "*.pdb"
	"FileExclusion" "steam_appid.txt"
}
```

```text
// steam/depot_linux.vdf
"DepotBuild"
{
	"DepotID" "1003"
	"FileMapping"
	{
		"LocalPath" "My Game-1.0.0-linux_x64/*"
		"DepotPath" "."
		"recursive" "1"
	}
	"FileExclusion" "*.debug"
	"FileExclusion" "steam_appid.txt"
}
```

Run `steamcmd` from the project root (create `build/steam-output` first if it does not exist):

```sh
mkdir -p build/steam-output
steamcmd +login <builder_account> +run_app_build "$(pwd)/steam/app_build.vdf" +quit
```

### Branches and promoting builds safely

- **Never point `"SetLive"` at `"default"` in a VDF script or CI job.** Always upload to a non-default branch such as `staging` (password-protected in Steamworks under **SteamPipe → Builds**).
- Opt a test Steam client into `staging` on macOS, Windows, and Linux / Steam Deck. Verify cold launch, audio, controller input, Steam overlay, and Steam Cloud sync.
- Once verified, open **SteamPipe → Builds** in the Steamworks Partner Portal, select the tested BuildID, set it live on `default`, and click **Preview Change → Set Build Live Now**.
- Keep a `previous` branch (or note the prior live BuildID) so you can roll back immediately if a regression reaches players.

### Automating uploads in CI

1. **Create a dedicated builder account**: in Steamworks (**Users & Permissions**), add a separate Steam account with only **Edit App Metadata** and **Publish App Changes to Steam** permissions for this app. Never store a personal admin account in CI.
2. **Cache Steam Guard credentials once**: run `steamcmd +login <builder_account> +quit` interactively on a trusted machine to approve Steam Guard, then base64-encode `steamcmd`'s cached `config/config.vdf` (and `ssfn*` if present) into a CI secret such as `STEAM_CONFIG_VDF` alongside `STEAM_USERNAME`.
3. **Build and upload to `staging` only**: build `macos_universal` on a macOS runner (and `windows_x64,linux_x64` on macOS or Linux), assemble all three depots in one `dist/` directory, restore `config.vdf`, and run `steamcmd +login "$STEAM_USERNAME" +run_app_build "$PWD/steam/app_build.vdf" +quit`. Promote the uploaded BuildID to `default` manually in Steamworks after smoke-testing `staging`.

## Steam Cloud

NSGo's `cx.DataDir()` resolves to a per-app directory keyed by `id` in `nsgo.yaml` (for example `com.example.mygame`):

| OS | NSGo `cx.DataDir()` | Steamworks Auto-Cloud Root | Subdirectory for `DirCloud` | Pattern |
| --- | --- | --- | --- | --- |
| Windows | `%APPDATA%\<id>` | `WinAppDataRoaming` | `<id>/cloud` | `*.json` |
| macOS | `~/Library/Application Support/<id>` | `MacAppSupport` | `<id>/cloud` | `*.json` |
| Linux / SteamOS | `$XDG_DATA_HOME/<id>` (default `~/.local/share/<id>`) | `LinuxXdgDataHome` | `<id>/cloud` | `*.json` |

Configure Steam Auto-Cloud in Steamworks under **Application → Steam Cloud**:

1. Enable Steam Cloud and set a byte quota and file-count quota per user (for example 100 MB and 100 files).
2. Under **Auto-Cloud Configuration**, add one rule with Root `WinAppDataRoaming`, Subdirectory `<id>/cloud` (for example `com.example.mygame/cloud`), Pattern `*.json`, and **Include Subdirectories** unchecked.
3. Under **Root Overrides**, map `WinAppDataRoaming` to:
   - **macOS**: Root `MacAppSupport`, Add/Replace Path `<id>/cloud`
   - **Linux + SteamOS**: Root `LinuxXdgDataHome`, Add/Replace Path `<id>/cloud`

In Go, point [`liveops.NewDirCloud`](liveops.md#cloud-saves) at `filepath.Join(cx.DataDir(), "cloud")` and pair it with `liveops.NewCloudSyncer`:

```go
dirCloud, err := liveops.NewDirCloud(filepath.Join(cx.DataDir(), "cloud"))
if err != nil {
	return err
}
syncer, err := liveops.NewCloudSyncer(liveops.CloudSyncOptions{
	Local:  save.StorageBackend(cx.Storage(), "mygame"),
	Cloud:  dirCloud,
	State:  cx.Storage(),
	Device: deviceID,
	Policy: liveops.PolicyKeepBoth,
})
if err != nil {
	return err
}
```

Why sync `cx.DataDir()/cloud` with `DirCloud` instead of syncing all of `cx.DataDir()`:

- **Machine-local settings stay local**: display resolution, fullscreen mode, frame cap, and audio settings kept in `cx.Storage()` (or `save.NewDefaultSettingsStore(syncer.Backend())`) are never synced, so playing on a 1280×800 Steam Deck does not overwrite a desktop PC's display settings.
- **Conflict safety**: `DirCloud` implements `ConditionalCloudSync` with atomic temp-file writes and directory sync, and `CloudSyncer` resolves concurrent offline edits across machines without silently dropping progress (see [Live operations](liveops.md#resolving-conflicts-539)).
- **Direct file alternative**: games that do not use `CloudSyncer` can point Auto-Cloud at `<id>` (`save.FileBackend(cx.DataDir())`, pattern `*.json`), keeping settings in a separate non-`.json` key or `cx.Storage()` so they are not synced.
- **Native Linux vs Proton**: if you ship a native `linux_x64` depot, Steam Deck uses the `LinuxXdgDataHome` override. If you ship only a `windows_x64` depot and players run it under Proton on Steam Deck, Steam automatically maps `WinAppDataRoaming` inside the game's Proton prefix (`compatdata/<AppID>/pfx/drive_c/users/steamuser/AppData/Roaming`).

## Steamworks features

### Rich presence localization

[`pkg/online/presence`](presence.md#steam-rich-presence) writes `status`, `level`, `mode`, `steam_player_group`, `steam_player_group_size`, `connect`, and `steam_display` through `ISteamFriends::SetRichPresence`. Steam only shows localized friend-list strings when `steam_display` names a token uploaded to Steamworks under **Community → Rich Presence Localization**.

Save a localization VDF matching the `DisplayToken` passed to `presence.NewSteam` (such as `"#Status_InLevel"`) and upload it in Steamworks:

```text
// steam/rich_presence_loc.vdf
"lang"
{
	"english"
	{
		"tokens"
		{
			"#Status_InMenu" "In Menus"
			"#Status_InLevel" "%mode%: %level%"
		}
	}
	"schinese"
	{
		"tokens"
		{
			"#Status_InMenu" "在菜单中"
			"#Status_InLevel" "%mode%：%level%"
		}
	}
}
```

`presence.NewSteam` automatically populates `%mode%` from `Activity.Mode` and `%level%` from `Activity.Level` (see [presence](presence.md#activity)).

### Steam Workshop

[`pkg/modding`](modding.md#steam-workshop-workshop) binds `ISteamUGC` via `purego` (`modding.NewSteamWorkshop` and `modding.NewWorkshop`) to publish mods with deterministic `workshop.yaml` metadata and sync subscribed items into `modding.Manager`.

- Enable Workshop in Steamworks under **Application → Workshop → General**, set the Workshop visibility state, and configure the allowed tags that your game passes in `workshop.yaml` / `PublishOptions.Tags`.
- When bundling `libsteam_api.dylib` with `nsgo-natives.yaml` on macOS, NSGo places it in `<Name>.app/Contents/Frameworks/libsteam_api.dylib`. Pass that path in `modding.SteamWorkshopOptions.LibraryPath` (resolved from `os.Executable()`) until the shared Steamworks client ([#1763](https://github.com/nsgo-dev/game/issues/1763)) unifies library discovery.

### Achievements, stats and leaderboards (planned)

Today, [`pkg/online/achievements`](achievements.md) and [`pkg/online/leaderboard`](leaderboard.md) report to Apple Game Center and Google Play Games Services through NSGo's `gameservices` package. Built-in Steam `ISteamUserStats` reporting for achievements, stats and Steam leaderboards is **not implemented yet** and is tracked in [#1764](https://github.com/nsgo-dev/game/issues/1764) (building on [#1763](https://github.com/nsgo-dev/game/issues/1763)).

What works on Steam today:

- `achievements.Tracker` tracks and persists local unlocks and step progress in `core.Storage` on desktop platforms so your in-game achievement UI works immediately.
- Games that want cross-platform leaderboards today can deploy [`pkg/online/leaderboard/selfhost`](leaderboard-selfhost.md).

## Steam Deck checklist

Use Valve's four **Steam Deck Verified** categories when testing your `linux_x64` depot (or `windows_x64` under Proton) on Steam Deck before submitting for review. Built-in Steam Deck glyphs, automatic Steam on-screen keyboard invocation, a `1280×800` fullscreen default on Deck, and Steam Input action sets from `game.yaml` are planned in [#1765](https://github.com/nsgo-dev/game/issues/1765); the checklist below notes what you can satisfy today and what waits on that issue.

### Input

- [ ] Every menu, pause screen, dialog, and gameplay action is playable end-to-end with a gamepad alone using [`input.Actions`](input.md#game-controllers) (`GamepadButton`, `BindGamepadAxis`, `WatchGamepads`) and spatial focus navigation in [`gameui`](gameui.md).
- [ ] Gameplay pauses cleanly or alerts the player if the active controller disconnects (a built-in disconnect dialog is tracked in [#1568](https://github.com/nsgo-dev/game/issues/1568)).
- [ ] Controller prompts match gamepad buttons rather than keyboard/mouse icons when playing on a controller (native Deck glyph detection and `game.yaml` Steam Input action sets are planned in [#1765](https://github.com/nsgo-dev/game/issues/1765)).
- [ ] No mandatory text entry blocks normal play without a controller-friendly fallback (automatic Steam on-screen keyboard summoning via `ShowFloatingGamepadTextInput` is planned in [#1765](https://github.com/nsgo-dev/game/issues/1765)).

### Display

- [ ] The game renders cleanly at `1280×800` (16:10 native Steam Deck) and `1280×720` (16:9) without clipped HUD elements, using [`screen.Viewport`](screen.md) (`ModeFit` or `ModeExpand`) and [`screen.Video`](screen.md#video-settings) (`Fullscreen: true`). Automatic `1280×800` fullscreen detection on Deck is tracked in [#1765](https://github.com/nsgo-dev/game/issues/1765).
- [ ] All UI text, prompts, and subtitles remain legible on a 7-inch 1280×800 screen (avoid tiny sub-9px text).
- [ ] Frame rate is steady and battery-friendly: wire `screen.Video`'s `FrameCap` (player-configurable or capped at 60/90 fps) and `AutoDropLowPower`.

### Seamlessness

- [ ] Clicking **Play** launches directly into the game with no external launcher, setup wizard, or mouse-only warning dialogs.
- [ ] Single-player play works offline without requiring a browser login or persistent network connection.
- [ ] Steam Cloud syncs save slots between desktop PC and Steam Deck without overwriting per-device video or audio settings (use `DirCloud` in `cx.DataDir()/cloud` and keep settings in `cx.Storage()`).

### System support

- [ ] Ship the `linux_x64` depot built with `-channel steam` under **Steam Linux Runtime 3.0 (sniper)** with `steam-launch.sh` and `nsgo-gtk/` intact, or verify your `windows_x64` depot under Proton.
- [ ] Every asset path in `resources/` matches exact filename casing (Linux filesystems are case-sensitive even if your macOS or Windows machine is not); run `go tool game check` before releasing.

## Store review and release

1. **Content survey**: complete the [Steamworks Content Survey](https://partner.steamgames.com/doc/gettingstarted/contentsurvey) for age ratings and mature content disclosures. Track it in `nsgo.yaml`:
   ```yaml
   compliance:
     steam:
       content_survey: false # change to true after submitting in Steamworks
   ```
   While set to `false`, `go tool game release -channel steam` (and `nsgo build -channel steam`) prints a reminder during the build.
2. **Store page review and "Coming Soon"**: fill out store copy, capsule art, screenshots, trailer, system requirements, and controller support tags, then submit the store page for Valve's review (typically 3–5 business days). Valve requires your store page to be publicly visible as **Coming Soon** for **at least two weeks** before you can release the game.
3. **Build review**: upload your release candidate depots via SteamPipe, configure depots, launch options, Linux runtime, and Steam Cloud paths, then submit the build for Valve's review (typically 3–5 business days; provide test credentials in the review notes if your game uses online accounts).
4. **Final pre-launch check**:
   - Confirm no `steam_appid.txt` is in any depot.
   - Confirm `THIRD_PARTY_NOTICES.txt`, `CREDITS.md` (if shipped in `resources/`), and `nsgo-gtk/licenses/` (Linux x64) are present.
   - Install the `staging` branch cleanly from the Steam client on each supported OS and verify launch, saves, Cloud sync, and Rich Presence / Workshop.
   - Set the approved BuildID live on `default` before clicking **Release App** in Steamworks.
