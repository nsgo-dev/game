# Building and Shipping

Verify your game across every target with `nsgo check`, configure metadata, icons and signing in `nsgo.yaml`, and build release packages for macOS, Windows, Linux, the web, iOS and Android with `nsgo build`.

## Configure nsgo.yaml

`nsgo build` reads app identity, icons, signing and target defaults from `nsgo.yaml` at your project root. Every flag on `nsgo build` (`-name`, `-id`, `-version`, `-build-number`, `-output`, `-target`, `-channel`) overrides the matching setting in `nsgo.yaml` for one build.

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

- **`id`**: reverse-DNS bundle identifier / application ID (for example `com.example.mygame`). Keep it stable across releases; Apple App Store, Google Play and Microsoft Store key your app on it.
- **`version`**: user-facing semantic version (`CFBundleShortVersionString`, Android `versionName`).
- **`build_number`**: monotonically increasing integer for every store upload (`CFBundleVersion` on Apple platforms, `versionCode` on Android). When omitted, it derives from `version`.
- **`resources/` vs `assets/`**: game files loaded at runtime (`"asset:..."`) live in `resources/` and ship inside every package; build-time inputs such as icons live in `assets/` and are compiled into platform icon formats. Use `mobile.exclude_resources` in `nsgo.yaml` to omit desktop-only files from iOS and Android packages.

## App icons

Place default icons in `assets/` and `nsgo build` picks them up automatically without extra configuration:

- `assets/AppIcon.icon` (Apple Icon Composer) or `assets/AppIcon.icns` (macOS only) for macOS and iOS.
- `assets/Icon.png` (a square PNG, 1024×1024 recommended) for Windows, Linux, Android, the web, and any Apple target without `icon.apple`.

To distinguish development builds from releases, add `-dev` variants (`assets/AppIcon-dev.icon`, `assets/Icon-dev.png`) or set `dev.icon`; `nsgo dev` uses them automatically.

For desktop installers, you can also set `macos.dmg_icon`, `macos.dmg_background` (PNG with optional `@2x` twin), `windows.installer_icon` and `windows.uninstaller_icon` in `nsgo.yaml`.

## Verify all targets before building

Check your host machine's toolchains first:

```sh
go tool nsgo doctor
```

Run `nsgo check` to vet your Go code (and any catalogs) across every target before building release packages:

```sh
go tool nsgo check -target all
```

Or check the 6 game platforms explicitly:

```sh
go tool nsgo check -target macos,windows,linux,web,ios,android
```

- Pass `-jobs N` to vet up to `N` targets in parallel.
- A bare OS name (`macos`, `windows`, `linux`) expands to both its `arm64` and `x64` architectures; `android` expands to `android_arm64` (`android_x64` requires an Android NDK).
- On Linux or Windows hosts, `-target all` skips Apple targets that require macOS and Xcode; naming `ios` explicitly fails if Xcode is absent.

Run headless tests with `go tool nsgo test`, or run tests on a real platform with `go tool nsgo test -target macos|windows|linux|web|ios_simulator|android`.

## Build release packages

`nsgo build` compiles stripped release binaries and writes one directory per target under `<output>/<name>-<version>-<target>`, plus distributable release files.

```sh
# Build every target configured in nsgo.yaml
go tool nsgo build

# Build all targets Go and Xcode can build on this machine
go tool nsgo build -target all

# Build store-formatted packages instead of direct installers
go tool nsgo build -channel store -target macos,windows,linux,ios,android
```

`-channel` chooses how packages are formatted and updated:

- **`direct`** (default): direct-download installers and archives (`.dmg`, NSIS `.exe`, `.deb`/Arch/`.AppImage`, `.apk`, `.ipa`, web `.zip`).
- **`store`**: store submission packages (Mac App Store `.pkg`, Microsoft Store `.msix`, Flathub `.tar.gz` + Flatpak manifest, Google Play `.aab`, iOS `.ipa`).
- **`none`**: skips auto-update feeds while keeping direct packaging.

Pass `-installers=false` if you only want the raw unpacked app directories on desktop targets. Pass `-run` to launch a built package immediately on this machine, a connected device, or a local web server.

## Targets

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
  `nsgo build` signs the app in the App Sandbox and packages a `.pkg` installer ready to upload via Transporter.

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
  - Upload `<name>.ipa` to App Store Connect / TestFlight with Apple's Transporter app or Xcode.
  - Increment `build_number` (`CFBundleVersion`) for every upload.
  - Configure iPad multitasking and screen orientations with `apple.requires_full_screen` and `apple.orientations` (`portrait`, `portrait_upside_down`, `landscape_left`, `landscape_right`), and add any required `Info.plist` entries under `apple.info`. `apple.orientations` declares the orientations a full-screen iPad allows, while the runtime request from `cx.RequestOrientation` applies on both the iPhone and the iPad (see [Orientation on phones](screen.md#orientation-on-phones)). On an iPad, UIKit allows only the orientations both lists share, so list every orientation the game requests, such as `landscape_left` and `landscape_right` for a landscape game; a request with nothing in common with `apple.orientations` leaves the iPad no valid orientation.

### Android

Build ARM64 (needs only Go — `CGO_ENABLED=0`, no Android SDK or NDK required):

```sh
go tool nsgo build -target android_arm64
```

To also build `android_x64` (for x86_64 emulators or Chromebooks), install an Android NDK and pass `-target android_arm64,android_x64`.

- **Signing**: configure a PKCS#12 or JKS keystore via `sign.keystore` (or `-keystore`) and `sign.key_alias` (or `-key-alias`). Never commit passwords to `nsgo.yaml`; pass them via environment variables:
  ```sh
  NSGO_KEYSTORE_PASSWORD="$STORE_PW" NSGO_KEY_PASSWORD="$KEY_PW" \
    go tool nsgo build -channel store -target android_arm64 \
    -keystore certs/upload.p12 -key-alias upload
  ```
  Without a keystore, `nsgo build` signs APKs with the project's debug key (for sideloading only, rejected by Google Play).
- **Google Play**: build with `-channel store` to produce a signed `.aab` (Android App Bundle) for Google Play Console. Increment `build_number` (`versionCode`) on every upload, set `android.target_sdk` to Google Play's current required API level (`minSdkVersion` is 28), and declare any extra permissions under `android.permissions`.
- **Orientation**: the manifest does not fix an orientation; the game requests the one it plays in at runtime with `cx.RequestOrientation` (see [Orientation on phones](screen.md#orientation-on-phones)).
