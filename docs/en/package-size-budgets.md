# Package Size Budgets

`sizecheck` measures and enforces package size budgets for WebAssembly (`.wasm`), Android App Bundles (`.aab`), Android APKs (`.apk`), and iOS App Store Packages (`.ipa`) using Go's standard library (`archive/zip`), with no external tools or SDK dependencies.

## Why size budgets matter

Mobile store download ceilings and cellular install thresholds directly affect whether players finish installing a game:

- **Google Play (AAB)**: enforces a 200 MiB base-module compressed download limit per device configuration; every extra ABI in a universal APK multiplies native code footprint.
- **Apple App Store (IPA)**: applies a 200 MiB cellular download threshold before App Thinning; keeping binaries, frameworks, and asset catalogs lean keeps installs fast on mobile networks.
- **Web (Wasm)**: every megabyte of gzipped `.wasm` delays first paint and first playable frame.

`sizecheck` inspects each archive's exact compressed and uncompressed sizes, categorizes every entry, splits Android native libraries by ABI, and surfaces the largest files so regressions are immediately actionable.

## Default mobile budgets

Built-in defaults in `cmd/sizecheck` target typical 2D NSGo games and can be overridden with `-budget`, `-budget-mb`, or `-abi-budget-mb`:

| Target | Function | Total archive | Per-ABI native libs | Category defaults (compressed) |
| --- | --- | ---: | ---: | --- |
| Android AAB (`.aab`) | `DefaultAndroidAABBudget` | 50 MiB | 20 MiB | `native_libs` 35 MiB, `dex` 5 MiB, `assets` 25 MiB, `resources` 15 MiB, `manifest_meta` 2 MiB |
| Android APK (`.apk`) | `DefaultAndroidAPKBudget` | 40 MiB | 20 MiB | `native_libs` 30 MiB, `dex` 5 MiB, `assets` 20 MiB, `resources` 15 MiB, `manifest_meta` 2 MiB |
| iOS IPA (`.ipa`) | `DefaultIOSIPABudget` | 50 MiB | - | `executable` 25 MiB, `frameworks` 15 MiB, `assets_car` 15 MiB, `resources` 25 MiB, `lproj` 5 MiB, `code_signature` 2 MiB |

For this repository's minimal `./internal/sizeprobe` CI probe, tighter baseline files live in [`benchmarks/`](../../benchmarks/):

- [`benchmarks/web-budget.json`](../../benchmarks/web-budget.json): 8 MiB raw / 2.25 MiB gzipped `.wasm`
- [`benchmarks/android-aab-budget.json`](../../benchmarks/android-aab-budget.json): 6 MiB total `.aab` / 4 MiB per ABI
- [`benchmarks/ios-ipa-budget.json`](../../benchmarks/ios-ipa-budget.json): 6 MiB total `.ipa` / 4 MiB executable

## Package categories

### Android (`.aab` and `.apk`)

| Category | Matched archive entries |
| --- | --- |
| `native_libs` | `base/lib/<abi>/*` (or `<module>/lib/<abi>/*`) in AAB; `lib/<abi>/*` in APK. Also aggregated per ABI (`arm64-v8a`, `armeabi-v7a`, `x86_64`, etc.). |
| `dex` | `base/dex/*.dex` in AAB; `*.dex` in APK. |
| `assets` | `base/assets/**` in AAB; `assets/**` in APK. |
| `resources` | `base/res/**`, `base/resources.pb` in AAB; `res/**`, `resources.arsc` in APK. |
| `manifest_meta` | `AndroidManifest.xml`, `BundleConfig.pb`, `BUNDLE-METADATA/**`, `META-INF/**`, `native.pb`, `assets.pb`. |
| `other` | All remaining entries (such as `base/root/**` or Kotlin metadata). |

### iOS (`.ipa`)

| Category | Matched archive entries |
| --- | --- |
| `executable` | Main Mach-O binary inside `Payload/<App>.app/` (matched via `CFBundleExecutable` in `Info.plist`, falling back to `<App>`). |
| `frameworks` | `Frameworks/**`, `PlugIns/**`, `*.dylib`, and top-level `SwiftSupport/**`. |
| `assets_car` | Compiled asset catalogs (`Assets.car`, `*.car`). |
| `resources` | `Info.plist`, shaders/metallibs, images, audio, fonts, tilemaps, atlases, `*.storyboardc/**`, `*.bundle/**`, and `assets/**`. |
| `lproj` | Localization bundles (`*.lproj/**`). |
| `code_signature` | `_CodeSignature/**`, `embedded.mobileprovision`, `SC_Info/**`. |
| `other` | `PkgInfo`, files outside `Payload/<App>.app/` (`iTunesMetadata.plist`, `Symbols/**`), and unrecognized files. |

## CLI usage

Run `sizecheck` from the repository root:

```sh
# Inspect an Android App Bundle using built-in defaults
go run ./cmd/sizecheck -kind aab path/to/app.aab

# Enforce a tighter total and per-ABI budget on a multi-ABI APK and emit Markdown
go run ./cmd/sizecheck -kind apk -budget-mb 35 -abi-budget-mb 15 -markdown path/to/app.apk

# Inspect an iOS IPA and emit structured JSON
go run ./cmd/sizecheck -kind ipa -json path/to/app.ipa

# Check the WebAssembly probe against benchmarks/web-budget.json
go run ./cmd/sizecheck -kind wasm -budget ../../benchmarks/web-budget.json -markdown
```

### Flags

| Flag | Description |
| --- | --- |
| `-kind` | Package format: `aab`, `apk`, `ipa`, or `wasm`. Inferred from the file extension when omitted; defaults to `wasm`. |
| `-file` | Path to the package file (`.aab`, `.apk`, `.ipa`, or `.wasm`). May also be passed as a positional argument. |
| `-budget` | Path to a JSON budget file. Defaults to `benchmarks/web-budget.json` for `wasm` and built-in platform defaults for mobile. |
| `-budget-mb` | Override the raw total size limit in MiB (`> 0`). In wasm mode this replaces only the raw limit and leaves the compressed limit from the budget file. |
| `-abi-budget-mb` | Override the per-ABI native library compressed size limit in MiB (`> 0`, Android only). |
| `-wasm` | Existing `.wasm` path in `wasm` mode (skips building `target`). |
| `-json` | Write the structured report as JSON to stdout. |
| `-markdown` | Write a GitHub-flavored Markdown report to stdout. |

### Exit codes

- `0`: package inspected and all budgets passed.
- `1`: one or more total, per-ABI, or per-category limits were exceeded (the full breakdown is still written to stdout).
- `2`: invalid arguments, unreadable budget file, missing package, or corrupt ZIP archive.

### Custom mobile budget JSON

You can check a custom JSON file with `-budget`:

```json
{
  "max_total_bytes": 52428800,
  "max_per_abi_bytes": 20971520,
  "max_category_bytes": {
    "native_libs": 36700160,
    "dex": 5242880,
    "assets": 26214400
  },
  "note": "Release budget for MyGame Android AAB"
}
```

Any omitted or `<= 0` field is left unbounded.

## CI enforcement

[`.github/workflows/quality.yml`](../../.github/workflows/quality.yml) runs three dedicated size budget jobs on every push to `main` and every pull request:

1. **`web-size-budget`** (`depot-ubuntu-24.04-4`): builds `./internal/sizeprobe` for `GOOS=js GOARCH=wasm` and checks `benchmarks/web-budget.json`.
2. **`android-size-budget`** (`depot-ubuntu-24.04-4`): runs `go tool nsgo build -target android -channel store ./internal/sizeprobe`, inspects the generated `.aab` against `benchmarks/android-aab-budget.json`, and appends the Markdown report to `$GITHUB_STEP_SUMMARY`.
3. **`ios-size-budget`** (`depot-macos-15`): runs `go tool nsgo build -target ios ./internal/sizeprobe`, inspects the generated `.ipa` against `benchmarks/ios-ipa-budget.json`, and appends the Markdown report to `$GITHUB_STEP_SUMMARY`.

When a budget fails, `sizecheck` still renders the full violation list, category table, per-ABI table, and top-10 largest files into the job summary before exiting with code `1`.

## Keeping packages lean

- Strip Go symbol and DWARF tables in release builds (`-ldflags="-s -w"` and `-trimpath`).
- Ship `.aab` bundles to Google Play so users download only their device's ABI instead of every architecture.
- Prefer compressed audio (`.m4a` / `.ogg`) for music and packed sprite atlases over loose uncompressed PNGs or WAVs.
- Watch the `Largest Files` table in CI summaries whenever adding new dependencies or assets.
