# 构建与发布

用 `nsgo check` 验证全部目标平台，在 `game.yaml` 中配置应用身份和导出预设，在 `nsgo.yaml` 中保留图标和签名设置，并用 `nsgo build` 为 macOS、Windows、Linux、Web、iOS 和 Android 构建发布包。

## 名称、标识符和版本

对于用 `game new` 创建的项目，`game.yaml` 拥有名称、标识符、玩家看到的版本、外观和导出设置。首次上传商店前，把生成的 `dev.nsgo.<name>` 标识符换成你拥有的域名的反向 DNS 名称，并在之后的版本中保持不变：App Store、Google Play 和 Microsoft Store 都靠它识别应用。

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

编辑 `game.yaml` 后，同步 NSGo 读取的字段，不要直接修改 `nsgo.yaml` 中的这些字段：

```sh
go tool game project sync
go tool game project check
```

`sync` 会保留 `nsgo.yaml` 的其他设置和注释。`build_number` 不属于 `game.yaml`：把它留在 `nsgo.yaml` 中，并在每次上传商店时递增。`nsgo build` 读取同步后的文件；它的 `-name`、`-id`、`-version`、`-build-number`、`-output`、`-target` 和 `-channel` 参数只覆盖本次构建的对应值，不会改写这两个文件。

<!-- shipping-project: nsgo.yaml -->
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

- **`appVersion` / `version`**：面向用户的语义化版本号（Apple 的 `CFBundleShortVersionString`、Android 的 `versionName`）。
- **`build_number`**：每次提交商店都必须递增的整数构建号（Apple 的 `CFBundleVersion`、Android 的 `versionCode`）。省略时会从 `version` 推导。
- **`resources/` 与 `assets/`**：所有在运行时通过 `"asset:..."` 加载的文件——包括图片、声音、图集 JSON、瓦片地图、模型、数据表以及由 `assets.Manager` 读取的原始字节——统一放在 `resources/` 中，会原样打入每个安装包（在 Web 上通过 `core.Scope.ReadAsset` 按需获取）。应用图标等构建期输入，以及少量通过 `//go:embed` 嵌入的首屏文件放在 `assets/` 中。可以在 `nsgo.yaml` 里用 `mobile.exclude_resources` 从 iOS 和 Android 包中排除仅桌面端使用的资源。

发布构建前，`game release` 和编辑器就绪检查会拒绝 `resources/` 中的符号链接、点文件与点目录，以及 `.env`、`.npmrc`、`.ssh`、`*.p12`、`*.pem` 等凭据名称、不超过 1 MiB 的文件中的私钥或服务账号内容，以及命名管道等非普通文件。确实需要发布的点文件可在 `game.yaml` 的 `export.allowResources` 中列出，但仍会作为警告要求人员复核，并且不会输出文件内容。

## 应用图标

把默认图标放到 `assets/` 目录下，`nsgo build` 无需额外配置即可自动识别：

- `assets/AppIcon.icon`（Apple Icon Composer 格式）或 `assets/AppIcon.icns`（仅 macOS），用于 macOS 和 iOS。
- `assets/Icon.png`（正方形 PNG，建议 1024×1024），用于 Windows、Linux、Android、Web，以及未配置 `icon.apple` 的 Apple 目标。

如果需要区分开发版和正式版图标，可以放置带 `-dev` 后缀的文件（`assets/AppIcon-dev.icon`、`assets/Icon-dev.png`）或在 `dev.icon` 中指定，`nsgo dev` 会自动使用它们。

对于桌面端安装程序，还可以在 `nsgo.yaml` 中设置 `macos.dmg_icon`、`macos.dmg_background`（PNG，可选同名 `@2x` 图）、`windows.installer_icon` 和 `windows.uninstaller_icon`。

Android 启动器可以按自己的形状裁剪并移动自适应图标，而不是把共享 PNG 缩放到自己的底板上。配置前景和背景，并为 Android 13 的主题图标添加单色形状：

```yaml
icon:
  android:
    foreground: assets/IconForeground.png
    background: "#1F1F1F"  # 颜色或正方形 PNG
    monochrome: assets/IconMonochrome.png
```

每个图层都是 108 dp 的正方形，PNG 会缩放到 432 像素。把标志放在中央 66 dp（264 像素）以内，并让背景铺满整个方形。单色形状只看透明度。`icon.android.png` 仍可单独指定普通 Android 位图；未指定时，NSGo 使用 `icon.png` 或用自适应图层绘制。完整的回退规则见 [NSGo 构建指南](https://nsgo.dev/zh/docs/building/)。

## 启动画面

在 `nsgo.yaml` 中设置 `launch_screen`，即可在 iOS 和 Android 12 及以后的版本上显示不透明背景、居中 PNG 或两者：

```yaml
launch_screen:
  background: "#F5F1EA"
  image: assets/Launch.png
  dark:
    background: "#201C26"
    image: assets/LaunchDark.png
```

图片路径相对于 `nsgo.yaml`。图片按比例放进 128 × 128 点或 dp 的区域；使用最长边至少 512 像素的透明 PNG。除非 `game.yaml` 的 `window.appearance` 固定外观，否则由系统选择浅色或深色。省略某个深色字段时会复用对应的浅色值；完全省略图片或背景则保留平台默认值。

## Apple 隐私清单与出口合规

NSGo 构建的每个 Apple 包都包含 `PrivacyInfo.xcprivacy`，声明 NSGo 和 Go 运行时使用的需要理由的 API。用 `apple.privacy` 添加游戏自己的声明；`resources/` 中的 `PrivacyInfo.xcprivacy` 会与这些声明合并，而不是被替换。在 `apple.info` 下记录应用对 Apple 加密出口合规问卷的回答：

```yaml
apple:
  privacy:
    accessed_apis:
      NSPrivacyAccessedAPICategoryDiskSpace: [E174.1]  # 仅为示例：声明游戏实际使用的 API 和理由
    tracking: false
  info:
    ITSAppUsesNonExemptEncryption: false  # 示例回答；请使用应用的实际情况
    # ITSEncryptionExportComplianceCode: "Apple 提供的批准代码（如果有）"
```

`accessed_apis` 也支持 Apple 的其他需要理由的 API 类别，`privacy` 还接受 `tracking_domains` 和 `collected_data`。出口合规值必须是 YAML 布尔值，批准代码必须是字符串。也可以省略这两个键，在上传时回答问卷。完整声明见 [NSGo 构建指南](https://nsgo.dev/zh/docs/building/)和 Apple 当前的 App Store Connect 指南。

## 构建前检查所有目标平台

先检查本机已安装的工具链：

```sh
go tool nsgo doctor
```

在构建发布包前，用 `nsgo check` 对所有目标平台执行 `go vet`（以及目录检查）：

```sh
go tool nsgo check -target all
```

或者显式检查全部 6 个主要构建目标：

```sh
go tool nsgo check -target macos,windows,linux,web,ios,android
```

- 传入 `-jobs N` 可并行检查最多 `N` 个目标。
- 仅写操作系统名（`macos`、`windows`、`linux`）会自动展开为 `arm64` 和 `x64` 两个架构；`android` 展开为 `android_arm64`（`android_x64` 需要 Android NDK）。
- 在 Linux 或 Windows 主机上，`-target all` 会跳过需要 macOS 与 Xcode 的 Apple 目标；如果显式指定 `ios` 而缺少 Xcode，则会报错。

无头测试运行 `go tool nsgo test`；在真实平台上运行测试则使用 `go tool nsgo test -target macos|windows|linux|web|ios_simulator|android`。

## 构建发布包

`go tool game release`（以及场景编辑器的导出面板 / 构建与发布界面）会自动以 `GOFLAGS=-tags=release` 调用 `nsgo build`，从而把仅限开发期的包（`pkg/tooling/gamedebug`、`pkg/tooling/hotreload`、`pkg/tooling/devstate` 以及物理与 AI 调试绘制）替换为零开销空实现，并在构建完成后校验桌面端二进制未链接调试符号，同时在产物旁写出 `CREDITS.md`：

```sh
# 按 game.yaml 的导出预设构建（未配置时默认构建全部六个主要平台）
go tool game release

# 构建商店提交格式的包，而非直接下载的安装器
go tool game release -channel store -target macos,windows,linux,ios,android
```

`nsgo build` 会构建剥离调试符号的正式版二进制，在 `<output>/<name>-<version>-<target>` 下为每个目标生成目录，并输出可分发的发布文件。在 NSGo 支持 `nsgo.yaml` 的发布专用标签之前（见 [nsgo-dev/nsgo#2695](https://github.com/nsgo-dev/nsgo/issues/2695)），直接调用 `nsgo build` 时请传入 `GOFLAGS=-tags=release`（不要把 `release` 加入 `nsgo.yaml` 的 `build.tags`，因为该字段也会作用于 `nsgo dev` 和 `nsgo test`）：

```sh
# 按 nsgo.yaml 中配置的目标构建
GOFLAGS=-tags=release go tool nsgo build

# 构建当前机器上 Go 与 Xcode 支持的全部目标
GOFLAGS=-tags=release go tool nsgo build -target all

# 构建商店提交格式的包，而非直接下载的安装器
GOFLAGS=-tags=release go tool nsgo build -channel store -target macos,windows,linux,ios,android
```

`-channel` 控制打包格式与更新通道：

- **`direct`**（默认）：直接分发安装器与压缩包（`.dmg`、NSIS `.exe`、`.deb`/Arch/`.AppImage`、`.apk`、`.ipa`、Web `.zip`）。
- **`store`**：应用商店提交包（Mac App Store `.pkg`、Microsoft Store `.msix`、Flathub `.tar.gz` + Flatpak 清单、Google Play `.aab`、iOS `.ipa`）。
- **`steam`**：用于 macOS、Windows 和 Linux 的未打包桌面端 Depot 目录（`linux_x64` 附带 `steam-launch.sh` 与内置 GTK 4），不含安装器、自更新器或 `steam_appid.txt`。Steamworks 配置、`nsgo-natives.yaml`、SteamPipe 上传、Steam 云存档路径与 Steam Deck 验证清单见[发布到 Steam](steam.md)。
- **`none`**：跳过自动更新源配置，保留直接分发格式。

如果桌面端只需要未打包的应用目录，传入 `-installers=false`。传入 `-run` 可在构建完成后立刻在本机、连接的设备或本地 Web 服务器上运行产物。

## 为资源署名

资源采用的多数许可证（包括知识共享署名许可和 SIL 开放字体许可证）都要求随游戏附上作者和许可证。把它们记录在每个资源旁边导入设置附属文件（`hero.png.import.yaml`）的 `credit` 部分；为每个平台运行 `nsgo build` 的 `go tool game release` 随后会在各个包旁边写出 `CREDITS.md`，供商店页面或下载页面使用，并且只要 `game.yaml` 的 `assets` 部分里有资源缺少署名就拒绝构建。`go tool game credits` 不构建，只检查署名并打印该文件；在发布之前运行 `go tool game credits -out resources/CREDITS.md` 还能让每个包内都带上它，因为 `nsgo build` 会把 `resources` 文件夹复制进每个包（见[命令行](cli.md#credits)）。游戏从 `assets` 部分之外加载的资源不会被列出，需要自行署名。

## 各目标平台说明

下表说明六个主要构建目标的打包与商店分发格式；各平台具体的运行时功能支持情况、实验性目标（鸿蒙、tvOS、watchOS 与小程序）状态及上游缺口见[平台支持](platforms.md)。

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
  `nsgo build` 会启用 App Sandbox 签名并生成可供 `go tool nsgo publish macos` 上传的 `.pkg` 安装包，见下文[发布到商店](#发布到商店)。

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
  - 用 `go tool nsgo publish ios` 把已签名的商店 `.ipa` 上传至 App Store Connect / TestFlight，见下文[发布到商店](#发布到商店)。
  - 每次上传都必须递增 `build_number`（`CFBundleVersion`）。
  - 使用 `apple.requires_full_screen` 和 `apple.orientations`（`portrait`、`portrait_upside_down`、`landscape_left`、`landscape_right`）配置 iPad 分屏与屏幕方向，并在 `apple.info` 下添加所需的 `Info.plist` 字段。`apple.orientations` 声明全屏 iPad 允许的方向，而游戏在运行时通过 `cx.RequestOrientation` 发出的请求在 iPhone 和 iPad 上都会生效（见[手机上的屏幕方向](screen.md#手机上的屏幕方向)）。在 iPad 上，UIKit 只允许两者共有的方向，因此要列出游戏请求的每个方向，例如横屏游戏列出 `landscape_left` 和 `landscape_right`；请求与 `apple.orientations` 没有共同方向时，iPad 将没有可用的方向。

### Android

构建 ARM64（只需要 Go，使用 `CGO_ENABLED=0`，无需 Android SDK 或 NDK）：

```sh
go tool nsgo build -target android_arm64
```

如需同时构建 `android_x64`（用于 x86_64 模拟器或 Chromebook），请安装 Android NDK 后传入 `-target android_arm64,android_x64`。

- **创建上传密钥**：用 JDK 的 `keytool` 创建 PKCS#12 上传密钥库，按提示填写身份信息并设置强密码。密钥文件和密码不要提交到版本库，并妥善备份：
  ```sh
  mkdir -p certs
  keytool -genkeypair -v -keystore certs/upload.p12 -storetype PKCS12 -alias upload -keyalg RSA -keysize 2048 -validity 10000
  ```
- **签名**：通过 `sign.keystore`（或 `-keystore`）指定这个 PKCS#12 或 JKS 密钥库，通过 `sign.key_alias`（或 `-key-alias`）指定密钥别名。切勿把密码写入 `nsgo.yaml`，请使用环境变量传递：
  ```sh
  NSGO_KEYSTORE_PASSWORD="$STORE_PW" NSGO_KEY_PASSWORD="$KEY_PW" \
    go tool nsgo build -channel store -target android_arm64 \
    -keystore certs/upload.p12 -key-alias upload
  ```
  未配置密钥库时，`nsgo build` 会使用项目自带的调试密钥签名 APK（仅限侧载测试，无法上传 Google Play）。
- **Google Play**：使用 `-channel store` 生成已签名的 `.aab`（Android App Bundle），再按下文[发布到商店](#发布到商店)用 `go tool nsgo publish -target android` 上传。每次上传需递增 `build_number`（`versionCode`），按 Google Play 当前要求设置 `android.target_sdk`（`minSdkVersion` 为 28），并在 `android.permissions` 下声明额外权限。
- **中国大陆商店（无 Google Play 服务）与鸿蒙**：关于版号与 ISBN、工信部 APP 备案、首次启动隐私弹窗、华为/小米/OPPO/vivo/荣耀/TapTap 等渠道的已签名 `.apk` 分发、无 Google 服务时各在线包的替代方案，以及鸿蒙与小程序预览状态，详见[在中国大陆发布](shipping-china.md)。
- **屏幕方向**：清单不固定屏幕方向；游戏在运行时通过 `cx.RequestOrientation` 请求游玩时的方向（见[手机上的屏幕方向](screen.md#手机上的屏幕方向)）。

## 商店列表

把商店元数据以 YAML 和 Markdown 纯文本纳入版本库，默认放在 `store/`，也可在 `nsgo.yaml` 中用 `store.dir` 修改目录：

```text
store/
  listing.yaml              # version: 1、default_locale、app_store 与 google_play 设置
  en-US/
    listing.yaml            # version: 1、标题、副标题、简短描述、关键词与 URL
    description.md          # 完整描述；也可改用 YAML 的 description 字段
    release_notes.md        # 更新说明；也可改用 YAML 的 release_notes 字段
    promotional_text.md     # 可选的 App Store 宣传文本
    screenshots/
  zh-Hans/
    listing.yaml
    description.md
    release_notes.md
    screenshots/
```

两层 `listing.yaml` 都必须包含 `version: 1`，并拒绝未知字段。规范的语言目录名会自动映射到各商店的语言代码；也可用 `app_store.locale` 或 `google_play.locale` 单独覆盖。`app_store.bundle_id`、`app_store.version` 和 `google_play.package_name` 默认使用从 `game.yaml` 同步的身份信息。全部字段、媒体类型和各商店覆盖规则见 [NSGo 构建指南](https://nsgo.dev/zh/docs/building/#商店列表)。

每次发布前验证所有语言：

```sh
go tool nsgo publish -store all -dry-run
```

它会检查必填项、语言代码、URL 协议、媒体文件及数量，以及各商店的字符上限，不读取凭据，也不发起 HTTP 请求。这些上限包括：两个商店的标题均为 30 字符，Google Play 简短描述为 80，两个商店的完整描述均为 4000，Google Play 更新说明为 500。App Store Connect 还限制副标题 30、关键词 100、宣传文本 170、更新说明 4000 字符。

要上传验证通过的元数据，运行 `go tool nsgo publish -store appstore,play`；在 `-store play` 后加 `-track production` 还会更新该轨道的发布说明。App Store Connect 读取 `NSGO_APP_STORE_KEY`、`NSGO_APP_STORE_KEY_ID` 和 `NSGO_APP_STORE_ISSUER`，未设置时回退到对应的 `NSGO_NOTARY_*` 变量。Google Play 读取 `NSGO_PLAY_SERVICE_ACCOUNT`。凭据只从环境变量读取，不写进列表文件。

### 商店截图计划

`store/capture.yaml` 记录每张商店截图和封面由哪个场景、哪段输入录像和哪一帧生成，以及各商店需要的尺寸，因此美术更新后可以重建整套图片，差异也能直接看出改了什么：

```yaml
version: 1
shots:
  - name: title
    scene: scenes/title.scene.yaml
    frame: 30
    output: title
    sizes:
      - preset: itch_cover
      - preset: minigame_share
  - name: boss
    scene: scenes/boss.scene.yaml
    recording: recordings/boss.replay.yaml
    time: 2.5
    output: play/boss
    sizes:
      - preset: google_play
      - width: 2796
        height: 1290
```

每个镜头声明一个场景和可选的 `.replay.yaml` 输入录像（都相对项目目录）、要截取的固定步 `frame` 或以秒为单位的 `time`（至多其一，都不写表示第一帧）、`output` 名称，以及 `sizes`，每项是一个 `preset` 或显式的 `width` 和 `height`。镜头在每个尺寸下的图片是媒体目录（默认 `store/media`）下的 `<output>-<width>x<height>.png`。预设有 `app_store_iphone`（1290x2796）、`app_store_ipad`（2064x2752）、`google_play`（1920x1080）、`google_play_feature`（1024x500）、`itch_cover`（630x500）和 `minigame_share`（1280x720）；横屏的 App Store 截图写显式尺寸。计划必须包含 `version: 1`，拒绝未知字段、项目之外的路径以及写到同一文件的两张图片，并且重新编码后字节不变。

`go tool game capture` 检查计划：每个场景和录像都存在，每张图片都存在且是目标尺寸的 PNG；缺失或尺寸不对的文件会逐个报告，并以 1 退出；`-shot name` 只检查一个镜头。它目前还不渲染图片：渲染需要 [#1438](https://github.com/nsgo-dev/game/issues/1438) 的固定步电影模式，在那之前请用运行时 MCP 桥的 `game_screenshot` 或手动截取。

## 发布到商店

上传前先在 App Store Connect 和 Google Play Console 创建应用记录。NSGo 负责上传构建和商店元数据，但处理完成的构建仍需你在各商店中单独提交审核。

对于 App Store Connect 和 TestFlight，iOS `.ipa` 要用 App Store 分发描述文件签名，macOS 商店 `.pkg` 要用团队的安装器身份签名。在安装了 Xcode 的 Mac 上，把 `NSGO_APP_STORE_KEY` 设为 App Store Connect `.p8` 密钥的路径或内容，并设置 `NSGO_APP_STORE_KEY_ID` 和 `NSGO_APP_STORE_ISSUER`；该密钥要有权访问此应用并上传构建。然后把已签名的包传给对应命令：

```sh
go tool nsgo publish ios path/to/MyGame.ipa
go tool nsgo publish macos path/to/MyGame.pkg
```

命令会检查包内构建号、上传包并报告 Apple 的处理结果。下次上传前递增 `build_number`。macOS 的构建号要跨所有应用版本递增；iOS 可以在新版本重新安排构建号序列。

对于 Google Play，启用 Google Play Developer API，并授予服务账号向目标轨道发布此应用的权限。把 `NSGO_PLAY_SERVICE_ACCOUNT` 设为服务账号 JSON 密钥的内容或文件路径，构建已签名的 App Bundle，然后发布：

```sh
GOFLAGS=-tags=release go tool nsgo build -channel store -target android
go tool nsgo publish -target android -track internal
```

`publish` 会在 `build.output/releases` 中查找当前项目版本的 `.aab`。有多个匹配文件时，在参数后传入要上传的 `.aab` 路径。轨道可以填写 `internal`、`production` 或封闭测试轨道的 ID。NSGo 从包中读取包名和版本代码，确认版本代码高于该轨道上的所有版本后，上传包并提交发布。

## 商店合规

用 `nsgo.yaml` 跟踪包外各项发布问卷的进度：

```yaml
compliance:
  app_store: { age_rating: false }
  google_play: { data_safety: false }
  steam: { content_survey: false }
```

完成问卷后将对应条目标记为 `true`。匹配的 `-channel store` 或 `-channel steam` 构建会提醒你处理仍为 `false` 的条目；不想在 YAML 中跟踪的条目可以省略。

- 在 App Store Connect 填写[年龄分级问卷](https://developer.apple.com/help/app-store-connect/manage-app-information/set-an-app-age-rating)和[应用隐私信息](https://developer.apple.com/help/app-store-connect/manage-app-information/manage-app-privacy)。加密出口合规问题按上文 [Apple 隐私清单与出口合规](#apple-隐私清单与出口合规)填写。
- 在 Play Console 填写[数据安全表单](https://support.google.com/googleplay/android-developer/answer/10787469)。仅通过内部测试轨道分发的应用可免填。
- 在提交游戏审核前，在 Steamworks 填写[内容调查](https://partner.steamgames.com/doc/gettingstarted/contentsurvey)。
