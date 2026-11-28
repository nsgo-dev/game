# 包体积预算

`game size` 以 `tooling/sizecheck` 包为基础，基于 Go 标准库（`archive/zip`）测量并强制检查 WebAssembly（`.wasm`）、Android App Bundle（`.aab`）、Android APK（`.apk`）和 iOS App Store Package（`.ipa`）的包体积预算，无需任何外部工具或 SDK 依赖。

## 为什么需要包体积预算

移动端应用商店的下载上限与蜂窝网络阈值会直接影响玩家的安装转化率：

- **Google Play（AAB）**：对单台设备的基础模块压缩下载大小限制为 200 MiB；在通用 APK 中每多打包一个 ABI，原生库体积都会成倍增长。
- **Apple App Store（IPA）**：蜂窝网络下载阈值为 200 MiB（应用瘦身前）；保持主程序、框架和资源目录精简能让玩家在移动网络下更快完成安装。
- **Web（Wasm）**：gzip 后的 `.wasm` 每增加 1 MiB，都会直接拖慢首屏加载和可玩时间。

`game size` 会统计归档中每个条目的压缩与未压缩体积，按内容类别归类，将 Android 原生库按 ABI 拆分，并列出体积最大的文件，帮助开发者在体积回归时立刻定位原因。

## 默认移动端预算

`tooling/sizecheck` 内置了面向典型 2D NSGo 游戏的默认预算，也可通过 `-budget`、`-budget-mb` 或 `-abi-budget-mb` 覆盖：

| 目标格式 | 默认函数 | 总归档上限 | 单 ABI 原生库上限 | 默认分类上限（压缩后） |
| --- | --- | ---: | ---: | --- |
| Android AAB（`.aab`） | `DefaultAndroidAABBudget` | 50 MiB | 20 MiB | `native_libs` 35 MiB、`dex` 5 MiB、`assets` 25 MiB、`resources` 15 MiB、`manifest_meta` 2 MiB |
| Android APK（`.apk`） | `DefaultAndroidAPKBudget` | 40 MiB | 20 MiB | `native_libs` 30 MiB、`dex` 5 MiB、`assets` 20 MiB、`resources` 15 MiB、`manifest_meta` 2 MiB |
| iOS IPA（`.ipa`） | `DefaultIOSIPABudget` | 50 MiB | - | `executable` 25 MiB、`frameworks` 15 MiB、`assets_car` 15 MiB、`resources` 25 MiB、`lproj` 5 MiB、`code_signature` 2 MiB |

针对本仓库最小的 `./internal/sizeprobe` CI 探针，[`benchmarks/`](../../benchmarks/) 目录维护了更严格的基线预算文件：

- [`benchmarks/web-budget.json`](../../benchmarks/web-budget.json)：原始 `.wasm` 8 MiB / gzip 后 2.25 MiB
- [`benchmarks/android-aab-budget.json`](../../benchmarks/android-aab-budget.json)：`.aab` 总计 6 MiB / 单 ABI 4 MiB
- [`benchmarks/ios-ipa-budget.json`](../../benchmarks/ios-ipa-budget.json)：`.ipa` 总计 6 MiB / 主程序 4 MiB

## 包内容分类

### Android（`.aab` 与 `.apk`）

| 分类 | 匹配的归档路径 |
| --- | --- |
| `native_libs` | AAB 中的 `base/lib/<abi>/*`（或 `<module>/lib/<abi>/*`），APK 中的 `lib/<abi>/*`；同时按 ABI（`arm64-v8a`、`armeabi-v7a`、`x86_64` 等）分别统计。 |
| `dex` | AAB 中的 `base/dex/*.dex`，APK 中的 `*.dex`。 |
| `assets` | AAB 中的 `base/assets/**`，APK 中的 `assets/**`。 |
| `resources` | AAB 中的 `base/res/**`、`base/resources.pb`，APK 中的 `res/**`、`resources.arsc`。 |
| `manifest_meta` | `AndroidManifest.xml`、`BundleConfig.pb`、`BUNDLE-METADATA/**`、`META-INF/**`、`native.pb`、`assets.pb`。 |
| `other` | 其余条目（例如 `base/root/**` 或 Kotlin 元数据）。 |

### iOS（`.ipa`）

| 分类 | 匹配的归档路径 |
| --- | --- |
| `executable` | `Payload/<App>.app/` 下的主 Mach-O 可执行文件（优先读取 `Info.plist` 中的 `CFBundleExecutable`，缺省回退为 `<App>`）。 |
| `frameworks` | `Frameworks/**`、`PlugIns/**`、`*.dylib` 以及顶层 `SwiftSupport/**`。 |
| `assets_car` | 编译后的资源目录（`Assets.car`、`*.car`）。 |
| `resources` | `Info.plist`、着色器/metallib、图片、音频、字体、瓦片地图、图集、`*.storyboardc/**`、`*.bundle/**` 与 `assets/**`。 |
| `lproj` | 本地化资源目录（`*.lproj/**`）。 |
| `code_signature` | `_CodeSignature/**`、`embedded.mobileprovision`、`SC_Info/**`。 |
| `other` | `PkgInfo`、`Payload/<App>.app/` 之外的文件（如 `iTunesMetadata.plist`、`Symbols/**`）以及未识别文件。 |

## CLI 用法

在仓库根目录运行 `go tool game size`：

```sh
# 使用内置默认预算检查 Android App Bundle
go tool game size -kind aab path/to/app.aab

# 为多 ABI APK 指定更严格的总预算和单 ABI 预算，并输出 Markdown
go tool game size -kind apk -budget-mb 35 -abi-budget-mb 15 -markdown path/to/app.apk

# 检查 iOS IPA 并输出结构化 JSON
go tool game size -kind ipa -json path/to/app.ipa

# 对照 benchmarks/web-budget.json 检查 WebAssembly 探针
go tool game size -kind wasm -budget benchmarks/web-budget.json -markdown
```

### 参数说明

| 参数 | 说明 |
| --- | --- |
| `-kind` | 包类型：`aab`、`apk`、`ipa` 或 `wasm`。省略时根据文件后缀推断，未提供文件时默认为 `wasm`。 |
| `-file` | 包文件路径（`.aab`、`.apk`、`.ipa` 或 `.wasm`），也可作为位置参数传入。 |
| `-budget` | JSON 预算文件路径。`wasm` 模式默认读取相对于工作目录的 `benchmarks/web-budget.json`，移动端模式默认使用内置平台预算。 |
| `-budget-mb` | 以 MiB 为单位覆盖总包体积上限（需 `> 0`）。 |
| `-abi-budget-mb` | 以 MiB 为单位覆盖 Android 单 ABI 原生库压缩体积上限（需 `> 0`）。 |
| `-wasm` | 在 `wasm` 模式下指定已构建的 `.wasm` 文件（跳过构建 `target`）。 |
| `-json` | 将结构化报告以 JSON 输出到标准输出。 |
| `-markdown` | 将 GitHub 风格 Markdown 报告输出到标准输出。 |

### 退出码

- `0`：检查完成且所有预算均通过。
- `1`：超出总体积、单 ABI 或任一分类预算（标准输出仍会输出完整空间分解报告），或者预算文件、包或其 ZIP 归档无法读取。
- `2`：参数无效，例如未知的包类型、互相冲突的参数、负数上限或缺少包路径。

### 自定义移动端预算 JSON

可通过 `-budget` 传入自定义预算文件：

```json
{
  "max_total_bytes": 52428800,
  "max_per_abi_bytes": 20971520,
  "max_category_bytes": {
    "native_libs": 36700160,
    "dex": 5242880,
    "assets": 26214400
  },
  "note": "MyGame Android AAB 发布预算"
}
```

省略或 `<= 0` 的字段表示不限制。

## CI 强制检查

[`.github/workflows/quality.yml`](../../.github/workflows/quality.yml) 在每次推送到 `main` 和每次 Pull Request 上运行三个独立的包体积预算任务：

1. **`web-size-budget`**（`depot-ubuntu-24.04-4`）：构建 `GOOS=js GOARCH=wasm` 的 `./internal/sizeprobe`，对照 `benchmarks/web-budget.json` 检查体积。
2. **`android-size-budget`**（`depot-ubuntu-24.04-4`）：执行 `go tool nsgo build -target android -channel store ./internal/sizeprobe`，对照 `benchmarks/android-aab-budget.json` 检查生成的 `.aab`，并将 Markdown 报告写入 `$GITHUB_STEP_SUMMARY`。
3. **`ios-size-budget`**（`depot-macos-15`）：执行 `go tool nsgo build -target ios ./internal/sizeprobe`，对照 `benchmarks/ios-ipa-budget.json` 检查生成的 `.ipa`，并将 Markdown 报告写入 `$GITHUB_STEP_SUMMARY`。

当预算超限时，`game size` 依然会把完整的违规列表、分类表、单 ABI 表和 Top 10 最大文件写入 CI 摘要，然后以退出码 `1` 终止任务。

## 瘦身建议

- 发布构建中剥离符号表和调试信息（`-ldflags="-s -w"` 与 `-trimpath`）。
- 面向 Google Play 发布 `.aab`，让商店只下发目标设备对应的 ABI，避免用户下载多余架构的原生库。
- 背景音乐优先使用压缩格式（`.m4a` / `.ogg`），精灵图片尽量打包为图集，避免散落大量未压缩资源。
- 引入新依赖或新资源后，留意 CI 摘要中的 `Largest Files` 表格变化。

### Cave Run 预算

CI 还会使用 `benchmarks/caverun-web-budget.json` 检查完整的 Cave Run Web 构建。当前上限为原始 26 MiB、gzip 7 MiB，基于约 23.43 MiB 原始大小和 5.86 MiB gzip 大小的最新构建。Android 发布流程使用 `benchmarks/caverun-android-aab-budget.json`，总大小上限为 20 MiB，每个 ABI 上限为 12 MiB。两项检查都在 Depot 的 `depot-ubuntu-24.04-4` runner 上运行。
