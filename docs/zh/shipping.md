# 构建与发布

用 `nsgo check` 验证全部目标平台，在 `nsgo.yaml` 中配置应用元数据、图标和签名，并用 `nsgo build` 为 macOS、Windows、Linux、Web、iOS 和 Android 构建发布包。

## 配置 nsgo.yaml

`nsgo build` 从项目根目录的 `nsgo.yaml` 读取应用标识、图标、签名和默认构建目标。命令行上的参数（`-name`、`-id`、`-version`、`-build-number`、`-output`、`-target`、`-channel`）会覆盖本次构建对应的 `nsgo.yaml` 字段。

```yaml
name: My Game
id: com.example.mygame
version: 1.0.0
build_number: 1
copyright: "© 2026 Example Studio"

icon:
  apple: assets/AppIcon.icon   # Icon Composer .icon（macOS/iOS）或 .icns（仅 macOS）
  png: assets/Icon.png         # 正方形 PNG（推荐 1024x1024）

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

- **`id`**：反向域名格式的 Bundle ID / Application ID（例如 `com.example.mygame`）。各版本间请保持不变，Apple App Store、Google Play 和 Microsoft Store 都以此识别你的应用。
- **`version`**：面向用户的语义化版本号（Apple 的 `CFBundleShortVersionString`、Android 的 `versionName`）。
- **`build_number`**：每次提交商店都必须递增的整数构建号（Apple 的 `CFBundleVersion`、Android 的 `versionCode`）。省略时会从 `version` 推导。
- **`resources/` 与 `assets/`**：运行时加载的游戏资源（`"asset:..."`）放在 `resources/` 中，会原样打入每个安装包；图标等构建期输入放在 `assets/` 中，由工具转换成各平台图标格式。可以在 `nsgo.yaml` 里用 `mobile.exclude_resources` 从 iOS 和 Android 包中排除仅桌面端使用的资源。

## 应用图标

把默认图标放到 `assets/` 目录下，`nsgo build` 无需额外配置即可自动识别：

- `assets/AppIcon.icon`（Apple Icon Composer 格式）或 `assets/AppIcon.icns`（仅 macOS），用于 macOS 和 iOS。
- `assets/Icon.png`（正方形 PNG，建议 1024×1024），用于 Windows、Linux、Android、Web，以及未配置 `icon.apple` 的 Apple 目标。

如果需要区分开发版和正式版图标，可以放置带 `-dev` 后缀的文件（`assets/AppIcon-dev.icon`、`assets/Icon-dev.png`）或在 `dev.icon` 中指定，`nsgo dev` 会自动使用它们。

对于桌面端安装程序，还可以在 `nsgo.yaml` 中设置 `macos.dmg_icon`、`macos.dmg_background`（PNG，可选同名 `@2x` 图）、`windows.installer_icon` 和 `windows.uninstaller_icon`。

## 构建前检查所有目标平台

先检查本机已安装的工具链：

```sh
go tool nsgo doctor
```

在构建发布包前，用 `nsgo check` 对所有目标平台执行 `go vet`（以及目录检查）：

```sh
go tool nsgo check -target all
```

或者显式检查全部 6 个游戏平台：

```sh
go tool nsgo check -target macos,windows,linux,web,ios,android
```

- 传入 `-jobs N` 可并行检查最多 `N` 个目标。
- 仅写操作系统名（`macos`、`windows`、`linux`）会自动展开为 `arm64` 和 `x64` 两个架构；`android` 展开为 `android_arm64`（`android_x64` 需要 Android NDK）。
- 在 Linux 或 Windows 主机上，`-target all` 会跳过需要 macOS 与 Xcode 的 Apple 目标；如果显式指定 `ios` 而缺少 Xcode，则会报错。

无头测试运行 `go tool nsgo test`；在真实平台上运行测试则使用 `go tool nsgo test -target macos|windows|linux|web|ios_simulator|android`。

## 构建发布包

`nsgo build` 会构建剥离调试符号的正式版二进制，在 `<output>/<name>-<version>-<target>` 下为每个目标生成目录，并输出可分发的发布文件。

```sh
# 按 nsgo.yaml 中配置的目标构建
go tool nsgo build

# 构建当前机器上 Go 与 Xcode 支持的全部目标
go tool nsgo build -target all

# 构建商店提交格式的包，而非直接下载的安装器
go tool nsgo build -channel store -target macos,windows,linux,ios,android
```

`-channel` 控制打包格式与更新通道：

- **`direct`**（默认）：直接分发安装器与压缩包（`.dmg`、NSIS `.exe`、`.deb`/Arch/`.AppImage`、`.apk`、`.ipa`、Web `.zip`）。
- **`store`**：应用商店提交包（Mac App Store `.pkg`、Microsoft Store `.msix`、Flathub `.tar.gz` + Flatpak 清单、Google Play `.aab`、iOS `.ipa`）。
- **`none`**：跳过自动更新源配置，保留直接分发格式。

如果桌面端只需要未打包的应用目录，传入 `-installers=false`。传入 `-run` 可在构建完成后立刻在本机、连接的设备或本地 Web 服务器上运行产物。

## 各目标平台说明

| 平台 | 目标名 | 直接分发（`-channel direct`） | 商店发布（`-channel store`） | 构建主机要求 |
| --- | --- | --- | --- | --- |
| macOS | `macos_arm64`、`macos_x64`（`macos`） | `.app` + `.dmg` | 沙盒 `.pkg` | 签名与公证需 macOS |
| Windows | `windows_x64`、`windows_arm64`（`windows`） | `.exe` 目录 + NSIS 安装程序 | `.msix` | 任意平台（Go 交叉编译） |
| Linux | `linux_x64`、`linux_arm64`（`linux`） | 二进制目录 + `.deb`、Arch 包、`.AppImage` | `.tar.gz` + AppStream + Flatpak 清单 | 任意平台（Go 交叉编译） |
| Web | `web` | 静态 WebAssembly 站点 + `.zip` | 同静态站点 | 任意平台 |
| iOS | `ios`、`ios_simulator` | `.ipa` | `.ipa` | macOS + Xcode |
| Android | `android_arm64`（`android`）、`android_x64` | `.apk` | `.aab` | 任意平台（`android_x64` 需 NDK） |

游戏主机（Nintendo Switch、PlayStation 和 Xbox）不是目标平台：它们的 SDK 受保密协议（NDA）约束，且需要 C 工具链，而 NSGo 只用 Go。

### macOS

构建 Apple Silicon、Intel 或同时构建两者：

```sh
go tool nsgo build -target macos
```

- **代码签名**：未设置签名身份时使用临时签名（ad hoc，仅适合本机测试，下载后会被 Gatekeeper 拦截）。直接分发请通过 `sign.identity` 或 `-sign` 指定 Developer ID Application 证书：
  ```sh
  go tool nsgo build -target macos -sign "Developer ID Application: Example Studio (TEAMID)"
  ```
- **公证（Notarization）**：为了让其他 Mac 无警告打开，请在 `sign.notarize` 或 `-notarize <profile>` 指定通过 `xcrun notarytool store-credentials` 保存的钥匙串配置。在 CI 环境中，可改为设置 App Store Connect API 密钥环境变量：`NSGO_NOTARY_KEY`（`.p8` 路径）、`NSGO_NOTARY_KEY_ID` 和 `NSGO_NOTARY_ISSUER`。`nsgo build` 会自动提交 `.dmg`（或打包的 `.app`）、等待审核并将票据装订（staple）到 `.app` 和 `.dmg` 上。
- **Mac App Store**：设置 `macos.category`（例如 `public.app-category.games`）、`sign.macos_profile`（Mac App Store 描述文件）以及可选的 `sign.installer`（Mac Installer Distribution 证书），然后以 `-channel store` 构建：
  ```sh
  go tool nsgo build -channel store -target macos
  ```
  `nsgo build` 会启用 App Sandbox 签名并生成可直接通过 Transporter 上传的 `.pkg` 安装包。

### Windows

构建 x64、ARM64 或两者：

```sh
go tool nsgo build -target windows
```

- **直接分发**：生成应用目录和 NSIS 安装程序（默认 `-installers=true`）。在 `nsgo.yaml` 中填写 `windows.publisher` 和 `copyright`，用于填充可执行文件元数据和卸载项信息。
- **代码签名**：为可执行文件和安装程序签名以避免 SmartScreen 拦截警告。通过 `-certificate`（或 `sign.certificate`）指定 PKCS#12 `.pfx` 证书，用环境变量 `NSGO_CERTIFICATE_PASSWORD` 提供密码，并可设置 RFC 3161 时间戳服务器（`-timestamp-url` / `sign.timestamp_url`，默认为 `http://timestamp.digicert.com`）：
  ```sh
  NSGO_CERTIFICATE_PASSWORD="$PFX_PASSWORD" \
    go tool nsgo build -target windows -certificate certs/win.pfx
  ```
  如果证书在硬件令牌或云签名服务中，请使用 `-sign-command`（或 `sign.sign_command`），其中 `%1` 代表待签名文件路径。
- **Microsoft Store**：在 `nsgo.yaml` 中填写 Partner Center 分配的 `windows.store.identity`、`windows.store.publisher`（`CN=...`）和 `windows.store.publisher_name`，再运行 `-channel store` 生成 `.msix` 包（包本身不签名，由商店发布时签名）：
  ```sh
  go tool nsgo build -channel store -target windows
  ```

### Linux

构建 x64、ARM64 或两者：

```sh
go tool nsgo build -target linux
```

- **直接分发**：生成 `.deb`（Debian/Ubuntu）、Arch 软件包和 `.AppImage`。在 `nsgo.yaml` 中配置桌面元数据：
  ```yaml
  linux:
    categories: [Game]
    maintainer: "Example Studio <games@example.com>"
    summary: "A 2D adventure game"
    license: MIT
  ```
  若游戏附带了外部辅助程序，可通过 `linux.depends` 和 `linux.recommends` 声明额外依赖。
- **Flathub / Flatpak**：使用 `-channel store` 会生成包含 `<id>.metainfo.xml` AppStream 元数据的 `.tar.gz` 归档，以及面向 GNOME 运行时的 `flatpak/<id>.yml` 清单（可用 `linux.flatpak_runtime` 覆盖默认运行时版本）。可将清单提交到 Flathub，或本地用 `flatpak-builder` 构建。

### Web

构建静态 WebAssembly 发布包：

```sh
go tool nsgo build -target web
```

部署前可直接在本地启动静态服务验证正式构建：

```sh
go tool nsgo build -target web -run
```

`dist/<name>-<version>-web/` 中包含 HTML、JS、WebAssembly 二进制和 `resources/` 文件，同时 releases 目录会生成一份 `.zip` 压缩包。将目录内容上传到任意静态托管服务（GitHub Pages、Cloudflare Pages、Netlify、itch.io 或 S3）即可上线。注意浏览器要求在首次用户手势之后才能播放音频（详见 [sound](sound.md)）。

### iOS

需要在安装了 Xcode 的 macOS 上构建。可构建真机 `.ipa` 或模拟器包：

```sh
go tool nsgo build -target ios
go tool nsgo build -target ios_simulator
```

- **签名**：通过 `sign.profile`（或 `-profile`）提供与 `id` 匹配的 App Store、Ad Hoc 或 Development 描述文件。`nsgo build` 会自动从钥匙串选择匹配的签名证书，也可以用 `sign.identity`（或 `-sign`）显式指定。
- **商店提交注意事项**：
  - 使用 Apple 的 Transporter 应用或 Xcode 将 `<name>.ipa` 上传至 App Store Connect / TestFlight。
  - 每次上传都必须递增 `build_number`（`CFBundleVersion`）。
  - 使用 `apple.requires_full_screen` 和 `apple.orientations`（`portrait`、`portrait_upside_down`、`landscape_left`、`landscape_right`）配置 iPad 分屏与屏幕方向，并在 `apple.info` 下添加所需的 `Info.plist` 字段。`apple.orientations` 只作用于 iPad；iPhone 采用游戏在运行时通过 `cx.RequestOrientation` 请求的方向（见[手机上的屏幕方向](screen.md#手机上的屏幕方向)）。

### Android

构建 ARM64（只需要 Go，使用 `CGO_ENABLED=0`，无需 Android SDK 或 NDK）：

```sh
go tool nsgo build -target android_arm64
```

如需同时构建 `android_x64`（用于 x86_64 模拟器或 Chromebook），请安装 Android NDK 后传入 `-target android_arm64,android_x64`。

- **签名**：通过 `sign.keystore`（或 `-keystore`）指定 PKCS#12 或 JKS 密钥库，通过 `sign.key_alias`（或 `-key-alias`）指定密钥别名。切勿把密码写入 `nsgo.yaml`，请使用环境变量传递：
  ```sh
  NSGO_KEYSTORE_PASSWORD="$STORE_PW" NSGO_KEY_PASSWORD="$KEY_PW" \
    go tool nsgo build -channel store -target android_arm64 \
    -keystore certs/upload.p12 -key-alias upload
  ```
  未配置密钥库时，`nsgo build` 会使用项目自带的调试密钥签名 APK（仅限侧载测试，无法上传 Google Play）。
- **Google Play**：使用 `-channel store` 生成已签名的 `.aab`（Android App Bundle）上传至 Google Play Console。每次上传需递增 `build_number`（`versionCode`），按 Google Play 当前要求设置 `android.target_sdk`（`minSdkVersion` 为 28），并在 `android.permissions` 下声明额外权限。
- **屏幕方向**：清单不固定屏幕方向；游戏在运行时通过 `cx.RequestOrientation` 请求游玩时的方向（见[手机上的屏幕方向](screen.md#手机上的屏幕方向)）。
