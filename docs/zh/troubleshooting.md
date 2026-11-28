# 排障

当设置、构建或首次运行在游戏启动前停止时使用本页。先运行 `go tool nsgo doctor`，再将完整错误与下面的条目对照。文件格式错误见[错误](errors.md)。

## 设置与模块错误

### `verifying go.mod: ... sum.golang.org/lookup/...: 404 Not Found`

模块代理找不到请求的游戏层版本，或网络返回了过期模块路径。检查 `go.mod` 中的模块路径与版本，运行 `go mod tidy` 后在网络正常时重试。`go run github.com/nsgo-dev/game/cmd/game@latest new` 创建的项目会记录已发布版本，使用 checkout 构建时可能需要 `-version`。

### `go: no such tool "game"`

项目来自 `nsgo init -template`，它只安装 NSGo 工具，没有安装 game 工具。运行 `go get -tool github.com/nsgo-dev/game/cmd/game@latest`，或使用本仓库的 `game new`，再在该模块中运行 `go tool game`。

### `project: nsgo.yaml disagrees with game.yaml: id is ...`

只编辑了其中一个项目文件。编辑 `game.yaml` 后运行 `go run github.com/nsgo-dev/game/cmd/game project sync`，让镜像字段同步到 `nsgo.yaml`。

## 平台与发布错误

### `error ios_simulator: the iOS SDK needs Xcode: xcrun ... executable file not found`

iOS 模拟器目标需要 macOS 和 Xcode。Linux 与 Windows 可构建桌面或网页，iOS 目标请在安装并选定命令行工具的 Mac 上运行。

### `signed with the debug key: set sign.keystore for store releases`

Android 商店构建没有发布密钥库。请在发布设置中配置 `sign.keystore` 及其密码。调试构建可使用生成的 debug key，但不能上传到商店。

### `game credits: game.yaml has no assets section`

项目没有声明资源清单。生成发布署名之前加入 `assets` 节，或在开发阶段将该消息视为提示。要求署名的发布流水线应在每个资源都有作者与许可证之前停止。

### `needs adb` 或 `needs a Mac with Xcode`

`nsgo doctor` 报告所选目标缺少工具。为 `adb` 安装 Android SDK Platform-Tools，为 iOS 构建在 macOS 安装 Xcode 和命令行工具。Android APK 编译本身不需要 `adb`，但部署到设备需要。

## 运行、音频与日志

### 浏览器首次点击前没有声音

浏览器会在用户手势前阻止自动播放。请在指针或按键事件中启动或解锁 mixer，再播放声音。这是浏览器策略，不是资源缺失。

`go tool nsgo dev` 的日志出现在终端。打包后的 Apple 应用写入 bundle ID 对应的 unified log，Android 输出可用 `adb logcat` 查看，网页构建使用 DevTools Console。应用接入崩溃报告器后，panic 应包含堆栈和最近游戏状态。调试器与运行时 profile 见 NSGo 的[运行与调试指南](https://nsgo.dev/docs/running-and-debugging/)。

游戏内覆盖层、控制台和检查器见 [gamedebug](gamedebug.md)，严格的文件加载器诊断见[错误](errors.md)。
