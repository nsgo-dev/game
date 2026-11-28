# Building and Shipping

## Web portal uploads

Build one self-contained web folder and connect its page adapter to `liveops.Portal`. Test loading, gameplay start and stop, commercial breaks, rewarded success and rewarded errors with each portal's local SDK mode. Keep the game paused and muted until the SDK callback, and grant opt-in rewards only after a successful result.

Verify your game across every target with `nsgo check`, configure its identity and export presets in `game.yaml`, keep icons and signing in `nsgo.yaml`, and build release packages for macOS, Windows, Linux, the web, iOS and Android with `nsgo build`.

## Name, identifier and version

In a project made with `game new`, `game.yaml` owns the name, identifier, player-facing version, appearance and export settings. Replace the generated `dev.nsgo.<name>` identifier with a reverse-DNS name of a domain you own before the first store upload, and keep it stable across releases: the App Store, Google Play and Microsoft Store identify the app by it.

<!-- shipping-project: game.yaml -->
```yaml
version: 1
name: My Game
id: com.example.mygame
appVersion: 1.0.0
window:
  width: 1280
  height: 720
export:
  output: dist
  presets:
    - platform: macos
      targets: [macos_arm64, macos_x64]
    - platform: windows
      targets: [windows_x64, windows_arm64]
    - platform: linux
      targets: [linux_x64, linux_arm64]
    - platform: web
    - platform: ios
    - platform: android
      targets: [android_arm64]
```

After editing `game.yaml`, synchronize the fields NSGo reads instead of editing them in `nsgo.yaml` yourself:

```sh
go tool game project sync
go tool game project check
```

`sync` keeps the other `nsgo.yaml` settings and comments. `build_number` is not part of `game.yaml`: leave it in `nsgo.yaml` and increment it for every store upload. `nsgo build` reads the synchronized files; its `-name`, `-id`, `-version`, `-build-number`, `-output`, `-target` and `-channel` flags override the corresponding values for one build without rewriting either file.

<!-- shipping-project: nsgo.yaml -->
```yaml
name: My Game
id: com.example.mygame
version: 1.0.0
build_number: 1
copyright: "© 2026 Example Studio"

icon:
  apple: assets/AppIcon.icon   # Icon Composer .icon (macOS/iOS) or .icns (macOS)
  png: assets/Icon.png         # square PNG (1024x1024 recommended)

build:
  output: dist
  installers: true
  targets:
    - macos_arm64
    - macos_x64
    - windows_x64
    - windows_arm64
    - linux_x64
    - linux_arm64
    - web
    - ios
    - android_arm64
```

- **`appVersion` / `version`**: the user-facing semantic version (`CFBundleShortVersionString`, Android `versionName`).
- **`build_number`**: a monotonically increasing integer for every store upload (`CFBundleVersion` on Apple platforms, `versionCode` on Android). When omitted, it derives from `version`.
- **`resources/` vs `assets/`**: every runtime file loaded as `"asset:..."` — images, sounds, atlas JSON, tilemaps, models, data tables and raw bytes read by `assets.Manager` — lives in `resources/` and ships inside every package (fetched on demand on the web through `core.Scope.ReadAsset`). Build-time inputs such as app icons, or small startup files embedded with `//go:embed`, live in `assets/`. Use `mobile.exclude_resources` in `nsgo.yaml` to omit desktop-only files from iOS and Android packages.

## App icons

Place default icons in `assets/` and `nsgo build` picks them up automatically without extra configuration:

- `assets/AppIcon.icon` (Apple Icon Composer) or `assets/AppIcon.icns` (macOS only) for macOS and iOS.
- `assets/Icon.png` (a square PNG, 1024×1024 recommended) for Windows, Linux, Android, the web, and any Apple target without `icon.apple`.

To distinguish development builds from releases, add `-dev` variants (`assets/AppIcon-dev.icon`, `assets/Icon-dev.png`) or set `dev.icon`; `nsgo dev` uses them automatically.

For desktop installers, you can also set `macos.dmg_icon`, `macos.dmg_background` (PNG with optional `@2x` twin), `windows.installer_icon` and `windows.uninstaller_icon` in `nsgo.yaml`.

Android launchers can mask and move an adaptive icon instead of putting the shared PNG on their own background. Configure its foreground and background, and add a monochrome shape for Android 13's themed icons:

```yaml
icon:
  android:
    foreground: assets/IconForeground.png
    background: "#1F1F1F"  # a color or a square PNG
    monochrome: assets/IconMonochrome.png
```

Each layer is a 108 dp square, with PNGs scaled to 432 pixels. Keep the logo inside the center 66 dp (264 pixels) and let the background fill the whole square. Only the monochrome shape's alpha matters. `icon.android.png` can still select a plain Android bitmap; without one, NSGo uses `icon.png` or draws it from the adaptive layers. See [NSGo's building guide](https://nsgo.dev/docs/building/) for the fallback rules.

## Launch screen

Set `launch_screen` in `nsgo.yaml` to show an opaque background, a centered PNG, or both on iOS and Android 12 or later:

```yaml
launch_screen:
  background: "#F5F1EA"
  image: assets/Launch.png
  dark:
    background: "#201C26"
    image: assets/LaunchDark.png
```

Image paths are relative to `nsgo.yaml`. The image fits proportionally inside 128 × 128 points or dp; use a transparent PNG whose longest side is at least 512 pixels. The system selects light or dark unless `window.appearance` in `game.yaml` pins the appearance. An omitted dark field reuses its light value; omit the image or background entirely to keep the platform default.

## Apple privacy manifest and export compliance

Every Apple bundle NSGo builds contains `PrivacyInfo.xcprivacy`, declaring the required-reason APIs used by NSGo and the Go runtime. Add your game's own declarations with `apple.privacy`; a `PrivacyInfo.xcprivacy` in `resources/` is merged with them rather than replaced. Record the app's answer to Apple's encryption export-compliance question under `apple.info`:

```yaml
apple:
  privacy:
    accessed_apis:
      NSPrivacyAccessedAPICategoryDiskSpace: [E174.1]  # example only: declare the APIs and reasons your game actually uses
    tracking: false
  info:
    ITSAppUsesNonExemptEncryption: false  # example answer; use your app's actual answer
    # ITSEncryptionExportComplianceCode: "Apple's approval code, if you have one"
```

`accessed_apis` also supports the other Apple required-reason categories, and `privacy` accepts `tracking_domains` and `collected_data`. The export-compliance value must be a YAML boolean and the approval code a string. You can omit both keys and answer the questionnaire at upload instead. See [NSGo's building guide](https://nsgo.dev/docs/building/) and Apple's current App Store Connect guidance for the complete declarations.

## Verify all targets before building

Check your host machine's toolchains first:

```sh
go tool nsgo doctor
```

Run `nsgo check` to vet your Go code (and any catalogs) across every target before building release packages:

```sh
go tool nsgo check -target all
```

Or check the six primary build targets explicitly:

```sh
go tool nsgo check -target macos,windows,linux,web,ios,android
```

- Pass `-jobs N` to vet up to `N` targets in parallel.
- A bare OS name (`macos`, `windows`, `linux`) expands to both its `arm64` and `x64` architectures; `android` expands to `android_arm64` (`android_x64` requires an Android NDK).
- On Linux or Windows hosts, `-target all` skips Apple targets that require macOS and Xcode; naming `ios` explicitly fails if Xcode is absent.

Run headless tests with `go tool nsgo test`, or run tests on a real platform with `go tool nsgo test -target macos|windows|linux|web|ios_simulator|android`.

## Build release packages

`go tool game release` (and the scene editor's Export / Build and Release screens) runs `nsgo build` with `GOFLAGS=-tags=release`, compiling out development-only packages (`pkg/tooling/gamedebug`, `pkg/tooling/hotreload`, `pkg/tooling/devstate`, and physics/AI debug drawing), verifying built desktop binaries against leaked debug symbols, and writing `CREDITS.md` beside the packages:

```sh
# Build every export preset in game.yaml (or all six primary targets by default)
go tool game release

# Build store-formatted packages instead of direct installers
go tool game release -channel store -target macos,windows,linux,ios,android
```

`nsgo build` compiles stripped release binaries and writes one directory per target under `<output>/<name>-<version>-<target>`, plus distributable release files. Until NSGo supports release-only tags in `nsgo.yaml` ([nsgo-dev/nsgo#2695](https://github.com/nsgo-dev/nsgo/issues/2695)), pass `GOFLAGS=-tags=release` whenever you invoke `nsgo build` directly (do not add `release` to `nsgo.yaml`'s `build.tags`, which also applies to `nsgo dev` and `nsgo test`):

```sh
# Build every target configured in nsgo.yaml
GOFLAGS=-tags=release go tool nsgo build

# Build all targets Go and Xcode can build on this machine
GOFLAGS=-tags=release go tool nsgo build -target all

# Build store-formatted packages instead of direct installers
GOFLAGS=-tags=release go tool nsgo build -channel store -target macos,windows,linux,ios,android
```

`-channel` chooses how packages are formatted and updated:

- **`direct`** (default): direct-download installers and archives (`.dmg`, NSIS `.exe`, `.deb`/Arch/`.AppImage`, `.apk`, `.ipa`, web `.zip`).
- **`store`**: store submission packages (Mac App Store `.pkg`, Microsoft Store `.msix`, Flathub `.tar.gz` + Flatpak manifest, Google Play `.aab`, iOS `.ipa`).
- **`steam`**: unpacked desktop depot directories for macOS, Windows and Linux (with `steam-launch.sh` and bundled GTK 4 on `linux_x64`), without installers, self-updaters or `steam_appid.txt`. See [Shipping on Steam](steam.md) for Steamworks setup, `nsgo-natives.yaml`, SteamPipe uploads, Steam Cloud paths and the Steam Deck checklist.
- **`none`**: skips auto-update feeds while keeping direct packaging.

Pass `-installers=false` if you only want the raw unpacked app directories on desktop targets. Pass `-run` to launch a built package immediately on this machine, a connected device, or a local web server.

## Credit your assets

Most licenses an asset comes under, Creative Commons Attribution and the SIL Open Font License among them, ask for its author and license to be shipped with the game. Record them in the `credit` section of the import settings sidecar beside each asset (`hero.png.import.yaml`); `go tool game release`, which runs `nsgo build` for every platform, then writes `CREDITS.md` beside the packages, for the store or download page, and refuses to build while an asset in `game.yaml`'s `assets` section has no credit. `go tool game credits` checks the credits and prints the file without building; `go tool game credits -out resources/CREDITS.md` before the release puts it in every package too, since `nsgo build` copies the `resources` folder into each one ([Command Line](cli.md#credits)). Assets the game loads from outside the `assets` section are not listed, so credit those yourself.

## Targets

This table covers build packaging and store formats for the six primary build targets; see [Platform support](platforms.md) for which runtime features work on each target, the status of experimental targets (HarmonyOS, tvOS, watchOS and mini programs), and links to upstream gaps.

| Platform | Target names | Direct build (`-channel direct`) | Store build (`-channel store`) | Host requirements |
| --- | --- | --- | --- | --- |
| macOS | `macos_arm64`, `macos_x64` (`macos`) | `.app` + `.dmg` | Sandboxed `.pkg` | macOS for signing/notarization |
| Windows | `windows_x64`, `windows_arm64` (`windows`) | `.exe` bundle + NSIS installer | `.msix` | Any host (Go cross-compiles) |
| Linux | `linux_x64`, `linux_arm64` (`linux`) | Binary bundle + `.deb`, Arch pkg, `.AppImage` | `.tar.gz` + AppStream + Flatpak manifest | Any host (Go cross-compiles) |
| Web | `web` | Static WebAssembly site + `.zip` | Same static site | Any host |
| iOS | `ios`, `ios_simulator` | `.ipa` | `.ipa` | macOS + Xcode |
| Android | `android_arm64` (`android`), `android_x64` | `.apk` | `.aab` | Any host (`android_x64` needs NDK) |

Consoles (Nintendo Switch, PlayStation and Xbox) are not targets: their SDKs are under NDA and need C toolchains, and NSGo is Go only.

### macOS

Build Apple Silicon, Intel, or both:

```sh
go tool nsgo build -target macos
```

- **Code signing**: without an identity, macOS apps are signed ad hoc (fine for local testing, rejected by Gatekeeper when downloaded). For direct distribution, pass a Developer ID Application certificate via `sign.identity` or `-sign`:
  ```sh
  go tool nsgo build -target macos -sign "Developer ID Application: Example Studio (TEAMID)"
  ```
- **Notarization**: to open on other Macs without Gatekeeper warnings, configure a keychain profile (`xcrun notarytool store-credentials`) in `sign.notarize` or pass `-notarize <profile>`. In CI, set the App Store Connect API key environment variables `NSGO_NOTARY_KEY` (path to `.p8`), `NSGO_NOTARY_KEY_ID` and `NSGO_NOTARY_ISSUER`. `nsgo build` submits the `.dmg` (or zipped `.app`), waits for approval, and staples the ticket to both the `.app` and `.dmg`.
- **Mac App Store**: set `macos.category` (for example `public.app-category.games`), `sign.macos_profile` (Mac App Store provisioning profile) and optionally `sign.installer` (Mac Installer Distribution identity), then build with `-channel store`:
  ```sh
  go tool nsgo build -channel store -target macos
  ```
  `nsgo build` signs the app in the App Sandbox and packages a `.pkg` installer ready for `go tool nsgo publish macos`, as [Publish to the stores](#publish-to-the-stores) describes.

### Windows

Build x64, ARM64, or both:

```sh
go tool nsgo build -target windows
```

- **Direct distribution**: produces the application directory and an NSIS setup installer (`-installers=true`, default). Set `windows.publisher` and `copyright` in `nsgo.yaml` to populate executable metadata and uninstall entries.
- **Code signing**: sign executables and the installer to prevent SmartScreen warnings. Pass a PKCS#12 `.pfx` certificate with `-certificate` (or `sign.certificate`) and provide its password in `NSGO_CERTIFICATE_PASSWORD`, plus an RFC 3161 timestamp server (`-timestamp-url` / `sign.timestamp_url`, defaults to `http://timestamp.digicert.com`):
  ```sh
  NSGO_CERTIFICATE_PASSWORD="$PFX_PASSWORD" \
    go tool nsgo build -target windows -certificate certs/win.pfx
  ```
  For hardware tokens or cloud signing services, use `-sign-command` (or `sign.sign_command`) where `%1` stands for each file path.
- **Microsoft Store**: set `windows.store.identity`, `windows.store.publisher` (`CN=...`) and `windows.store.publisher_name` from Partner Center, then build with `-channel store` to produce an `.msix` package (left unsigned so the Store signs it on publication):
  ```sh
  go tool nsgo build -channel store -target windows
  ```

### Linux

Build x64, ARM64, or both:

```sh
go tool nsgo build -target linux
```

- **Direct distribution**: builds `.deb` (Debian/Ubuntu), Arch packages and `.AppImage` bundles. Configure desktop metadata in `nsgo.yaml`:
  ```yaml
  linux:
    categories: [Game]
    maintainer: "Example Studio <games@example.com>"
    summary: "A 2D adventure game"
    license: MIT
  ```
  Add extra runtime dependencies with `linux.depends` and `linux.recommends` if your game ships helper binaries.
- **Flathub / Flatpak**: build with `-channel store` to emit `.tar.gz` archives containing `<id>.metainfo.xml` AppStream metadata and a `flatpak/<id>.yml` manifest targeting the GNOME runtime (`linux.flatpak_runtime` overrides the default runtime version). Submit the manifest to Flathub or build locally with `flatpak-builder`.

### The web

Build the static WebAssembly bundle:

```sh
go tool nsgo build -target web
```

Test the production build locally before deploying:

```sh
go tool nsgo build -target web -run
```

`dist/<name>-<version>-web/` contains the HTML, JS, WebAssembly binary and `resources/` files, and `nsgo build` also writes a `.zip` archive in the releases directory. Upload the directory contents to any static host (GitHub Pages, Cloudflare Pages, Netlify, itch.io, or S3). Remember that browsers gate audio behind the first user gesture (see [sound](sound.md)).

### iOS

Requires macOS with Xcode installed. Build for physical devices (`.ipa`) or the simulator:

```sh
go tool nsgo build -target ios
go tool nsgo build -target ios_simulator
```

- **Signing**: provide an App Store, Ad Hoc or Development provisioning profile matching `id` via `sign.profile` (or `-profile`). `nsgo build` automatically selects the matching signing identity from your keychain, or you can specify it explicitly with `sign.identity` (or `-sign`).
- **Store submission**:
  - Upload the signed store `.ipa` to App Store Connect / TestFlight with `go tool nsgo publish ios`, as [Publish to the stores](#publish-to-the-stores) describes.
  - Increment `build_number` (`CFBundleVersion`) for every upload.
  - Configure iPad multitasking and screen orientations with `apple.requires_full_screen` and `apple.orientations` (`portrait`, `portrait_upside_down`, `landscape_left`, `landscape_right`), and add any required `Info.plist` entries under `apple.info`. `apple.orientations` declares the orientations a full-screen iPad allows, while the runtime request from `cx.RequestOrientation` applies on both the iPhone and the iPad (see [Orientation on phones](screen.md#orientation-on-phones)). On an iPad, UIKit allows only the orientations both lists share, so list every orientation the game requests, such as `landscape_left` and `landscape_right` for a landscape game; a request with nothing in common with `apple.orientations` leaves the iPad no valid orientation.

### Android

Build ARM64 (needs only Go — `CGO_ENABLED=0`, no Android SDK or NDK required):

```sh
go tool nsgo build -target android_arm64
```

To also build `android_x64` (for x86_64 emulators or Chromebooks), install an Android NDK and pass `-target android_arm64,android_x64`.

- **Create an upload key**: use the JDK's `keytool` to create a PKCS#12 upload keystore, answer its identity prompts, and choose strong passwords when prompted. Keep the file and passwords out of version control and back them up securely:
  ```sh
  mkdir -p certs
  keytool -genkeypair -v -keystore certs/upload.p12 -storetype PKCS12 -alias upload -keyalg RSA -keysize 2048 -validity 10000
  ```
- **Signing**: configure the PKCS#12 or JKS keystore via `sign.keystore` (or `-keystore`) and `sign.key_alias` (or `-key-alias`). Never commit passwords to `nsgo.yaml`; pass them via environment variables:
  ```sh
  NSGO_KEYSTORE_PASSWORD="$STORE_PW" NSGO_KEY_PASSWORD="$KEY_PW" \
    go tool nsgo build -channel store -target android_arm64 \
    -keystore certs/upload.p12 -key-alias upload
  ```
  Without a keystore, `nsgo build` signs APKs with the project's debug key (for sideloading only, rejected by Google Play).
- **Google Play**: build with `-channel store` to produce a signed `.aab` (Android App Bundle), then upload it with `go tool nsgo publish -target android`, as [Publish to the stores](#publish-to-the-stores) describes. Increment `build_number` (`versionCode`) on every upload, set `android.target_sdk` to Google Play's current required API level (`minSdkVersion` is 28), and declare any extra permissions under `android.permissions`.
- **Mainland China stores (no Google Play Services) & HarmonyOS**: for publishing approval (版号/ISBN), MIIT APP filing, first-launch privacy consent, signed `.apk` distribution across Huawei AppGallery, Xiaomi, OPPO, vivo, Honor and TapTap, online-package fallbacks without Google services, and HarmonyOS / mini program preview status, see [Releasing in Mainland China](shipping-china.md).
- **Orientation**: the manifest does not fix an orientation; the game requests the one it plays in at runtime with `cx.RequestOrientation` (see [Orientation on phones](screen.md#orientation-on-phones)).

## Store listings

Keep store metadata in version control as YAML and Markdown under `store/`, or change the directory with `store.dir` in `nsgo.yaml`:

```text
store/
  listing.yaml              # version: 1, default_locale, app_store and google_play settings
  en-US/
    listing.yaml            # version: 1, title, subtitle, short_description, keywords and URLs
    description.md          # full description; can instead be the YAML description field
    release_notes.md        # what's new; can instead be the YAML release_notes field
    promotional_text.md     # optional App Store promotional text
    screenshots/
  zh-Hans/
    listing.yaml
    description.md
    release_notes.md
    screenshots/
```

Both levels of `listing.yaml` require `version: 1` and refuse unknown fields. Canonical locale directories map to each store's locale automatically; `app_store.locale` and `google_play.locale` can override the mapping. `app_store.bundle_id`, `app_store.version` and `google_play.package_name` default to the identity synchronized from `game.yaml`. See [NSGo's building guide](https://nsgo.dev/docs/building/#store-listings) for every field, media type and store-specific override.

Validate every locale before a release:

```sh
go tool nsgo publish -store all -dry-run
```

This checks required fields, locale codes, URL schemes, media files and counts, and each store's character limits without reading credentials or making HTTP requests. The limits include 30 characters for either store's title, 80 for a Google Play short description, 4000 for either full description, and 500 for Google Play release notes. App Store Connect also limits subtitles to 30, keywords to 100, promotional text to 170 and release notes to 4000 characters.

To upload the validated metadata, run `go tool nsgo publish -store appstore,play`; add `-track production` with `-store play` to update that track's release notes. App Store Connect reads `NSGO_APP_STORE_KEY`, `NSGO_APP_STORE_KEY_ID` and `NSGO_APP_STORE_ISSUER`, falling back to the corresponding `NSGO_NOTARY_*` variables. Google Play reads `NSGO_PLAY_SERVICE_ACCOUNT`. Credentials come from the environment, never the listing files.

## Publish to the stores

Create the app records in App Store Connect and Google Play Console before uploading. NSGo uploads builds and store metadata, but you still submit the processed build for review in each store.

For App Store Connect and TestFlight, sign an iOS `.ipa` with an App Store distribution profile or a macOS store `.pkg` with the team's installer identity. On a Mac with Xcode, set `NSGO_APP_STORE_KEY` to an App Store Connect `.p8` key's path or contents, and set `NSGO_APP_STORE_KEY_ID` and `NSGO_APP_STORE_ISSUER`; give the key access to the app and permission to upload builds. Then pass the signed package to the matching command:

```sh
go tool nsgo publish ios path/to/MyGame.ipa
go tool nsgo publish macos path/to/MyGame.pkg
```

The command checks the bundle's build number, uploads the package and reports Apple's processing result. Increment `build_number` before the next upload. macOS build numbers increase across all app versions; iOS can restart the sequence for a new version.

For Google Play, enable the Google Play Developer API and give a service account permission to release the app to the chosen track. Set `NSGO_PLAY_SERVICE_ACCOUNT` to the service-account JSON key's contents or file path, build a signed App Bundle, and publish it:

```sh
GOFLAGS=-tags=release go tool nsgo build -channel store -target android
go tool nsgo publish -target android -track internal
```

`publish` finds the current project version's `.aab` in `build.output/releases`. If more than one matches, pass the desired `.aab` path after the flags. The track can be `internal`, `production` or a closed track's ID. NSGo reads the package name and version code from the bundle, requires a version code higher than every release on that track, uploads the bundle and commits the release.

## Store compliance

Use `nsgo.yaml` to track the release questionnaires that remain outside the package:

```yaml
compliance:
  app_store: { age_rating: false }
  google_play: { data_safety: false }
  steam: { content_survey: false }
```

Set an entry to `true` after completing its questionnaire. A matching `-channel store` or `-channel steam` build reminds you about entries still set to `false`; omit entries you do not want to track.

- Complete the [age rating questionnaire](https://developer.apple.com/help/app-store-connect/manage-app-information/set-an-app-age-rating) and [App Privacy details](https://developer.apple.com/help/app-store-connect/manage-app-information/manage-app-privacy) in App Store Connect. Answer the encryption export-compliance question as [Apple privacy manifest and export compliance](#apple-privacy-manifest-and-export-compliance) describes.
- Complete [Data safety](https://support.google.com/googleplay/android-developer/answer/10787469) in Play Console. Apps distributed exclusively on the internal testing track are exempt.
- Complete the [Content Survey](https://partner.steamgames.com/doc/gettingstarted/contentsurvey) in Steamworks before submitting the game for review.
