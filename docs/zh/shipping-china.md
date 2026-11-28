# 在中国大陆发布

为使用 NSGo Game 开发的游戏准备面向中国大陆玩家的发布工作：按顺序规划资质与备案流程、实现首次启动隐私同意弹窗与合规展示、处理缺少 Google Play 服务的 Android 设备，以及了解鸿蒙和小程序预览目标的现状。

> 本页整理工程检查项、应用商店机制和官方入口链接，不构成法律意见。相关法规与各商店审核细则可能随时更新，提交前请以官方平台公告、出版单位或专业法律顾问的最新要求为准。

## 办理顺序与常见周期

在中国大陆发布游戏，排期通常由合规审批周期而非打包构建时间决定。国家新闻出版署（NPPA）的版号审批往往决定上线日期，而软著登记与 APP 备案需要提前与商店提交对齐。

| 步骤 | 目的 | 要求方 | 常见周期 |
| --- | --- | --- | --- |
| 1. 企业主体与 ICP 资质 | 注册中国大陆企业主体（营业执照）；商业化网络游戏自行运营通常还涉及 ICP 许可证（增值电信业务经营许可证），或委托具备资质的出版/运营单位。 | 备案接入商、出版单位、商店开发者账号 | 2–8 周 |
| 2. 计算机软件著作权登记（软著） | 向中国版权保护中心（CPCC）申请游戏软件著作权登记证书。 | 主流 Android 厂商商店、TapTap、版号申报 | 1–2 个月 |
| 3. APP 备案（工信部备案） | 按工业和信息化部（MIIT）要求，通过境内云服务/接入商（或小程序宿主平台）完成移动互联网应用程序备案，绑定应用包名（Bundle ID）及签名证书指纹。 | Apple App Store（中国大陆）、华为应用市场、各 Android 厂商商店、TapTap、鸿蒙、小程序 | 1–3 周 |
| 4. 网络游戏出版审批与 ISBN（版号） | 在付费下载、应用内购买或商业化运营前，经具备资质的出版单位向国家新闻出版署申请版号及 ISBN（国产与进口分队列审批）。 | Apple App Store（付费/内购游戏）、华为应用市场、各 Android 厂商商店、TapTap、商业化小游戏 | 数月至一年以上 |
| 5. 商店开发者账号与渠道包 | 完成各商店企业开发者认证、隐私与 SDK 清单填报，并按商店要求提交对应构建包。 | 各应用商店分别审核 | 每个商店 1–2 周 |

请在启动 APP 备案前确定最终的反向域名标识（`game.yaml` 与 `nsgo.yaml` 中的 `id`）以及 Android / 鸿蒙正式签名密钥库：APP 备案会同时绑定包名和签名证书的公钥 / MD5 指纹，各商店在审核时会核对上传包与备案信息是否一致。

## 游戏内必须展示的内容与时机

商店审核既会静态扫描安装包，也会在应用首次启动时动态监测网络请求和敏感接口调用。

### 首次启动隐私政策弹窗

中国大陆应用商店会拒审在玩家首次同意隐私政策之前就收集个人信息、读取设备标识符、上报统计/崩溃数据或初始化第三方 SDK 的应用。

1. 在进入主玩法、上报埋点或崩溃日志、初始化任何第三方 SDK 之前，先展示首次启动隐私政策弹窗。
2. 弹窗需包含简洁明了的数据收集与权限说明、可点击查看的完整隐私政策链接，以及明确的**同意**与**不同意**按钮。玩家选择**不同意**时，必须干净退出或停留在不收集任何数据的离线/浏览模式，不得读取设备标识或发送遥测数据。
3. 将玩家选择持久化到 `core.Storage`，后续启动不再重复弹窗（同时在设置页保留查看隐私政策和撤回同意的入口）。
4. 保持 `nsgo.yaml` 中的 `android.permissions` 最小化——仅声明游戏必需的权限，避免申请无关的设备识别类权限——并维护商店提审所需的第三方 SDK 共享清单（SDK 名称、提供方、使用目的、收集字段与隐私政策链接）。

`liveops.Analytics` 内置了显式同意门槛：默认处于 `liveops.ConsentUnknown`，在调用 `SetConsent(liveops.ConsentGranted)` 之前会直接丢弃所有 `Track` 调用（不发送，也不缓存供后续补发；详见[线上运营](liveops.md#统计分析)）。将 `liveops.CrashReporter` 上报与任何网络/SDK 初始化也置于同一份持久化同意状态之后：

```go
store := cx.Storage()
analytics := liveops.NewAnalytics(store, "privacy.analytics", myAnalyticsProvider)
analytics.SetClock(cx.Now)

// 玩家点击同意前，analytics.Track 会直接丢弃事件且不缓存。
// 将崩溃上报和任何第三方 SDK 初始化也绑定到同一同意状态：
reporter := liveops.NewCrashReporter(liveops.CrashOptions{
	Reporter: func(r liveops.CrashReport) {
		if analytics.Consent() == liveops.ConsentGranted {
			uploadCrashReport(r)
		}
	},
	Now: cx.Now,
})

// 首次启动隐私弹窗点击“同意”时调用：
onAgreePrivacy := func() {
	_ = analytics.SetConsent(liveops.ConsentGranted)
	initPostConsentServices()
}

// 点击“不同意”时调用：
onDisagreePrivacy := func() {
	_ = analytics.SetConsent(liveops.ConsentDenied)
	// 干净退出，或停留在零数据收集的离线模式。
}
```

### 健康游戏忠告、适龄提示与资质公示

- **健康游戏忠告**：在启动屏或登录界面醒目展示国家新闻出版署规定的 48 字《健康游戏忠告》（“抵制不良游戏，拒绝盗版游戏。注意自我保护，谨防受骗上当。适度游戏益脑，沉迷游戏伤身。合理安排时间，享受健康生活。”）。
- **适龄提示**：按中国音像与数字出版协会（CADPA）标准在起始画面与商店详情页展示适龄提示标识（如 8+、12+、16+）。
- **版号与备案号公示**：按要求在启动屏、登录页或“关于”界面公示版号审批文号、ISBN、出版单位、著作权人以及 APP / ICP 备案号，并在各商店后台填报一致的信息。

### 实名认证与未成年人防沉迷

在中国大陆商业化发行的网络游戏必须接入国家新闻出版署网络游戏防沉迷实名认证系统，强制执行未成年人游戏时长限制（未满 18 周岁玩家仅可在周五、周六、周日及法定节假日每日 20:00–21:00 北京时间游玩）、按年龄分级执行充值限额（未满 8 周岁禁止付费；8 至 15 周岁单次不超过 50 元、每月累计不超过 200 元；16 至 17 周岁单次不超过 100 元、每月累计不超过 400 元），并由服务端上报登录/登出行为，绝不能依赖客户端本地时钟。

> **当前状态（规划中）**：NSGo Game 目前尚未内置国家新闻出版署实名认证接口、防沉迷时长/充值校验、`Purchases` 购买前拦截钩子或忠告启动屏模板。运行时合规能力正在 [#1649](https://github.com/nsgo-dev/game/issues/1649) 中规划与追踪。落地前，商业化发行团队需自行在服务端实现实名与防沉迷校验，或接入出版方/渠道方的合规 SDK。

## 缺少 Google Play 服务时各在线包的行为

中国大陆销售的 Android 手机不预装 Google Play 服务（GMS）、Google Play 游戏服务、Google Play 结算服务和 Google Play 商店，且 Firebase Cloud Messaging（FCM）无法直接访问。下表列出 NSGo Game 中每一个 `pkg/online` 包在无 Google 服务 Android 设备上的表现及替代方案。

| 包 | 无 Google 服务状态 | 无 Google 服务 Android 上的行为与替代方案 |
| --- | --- | --- |
| `pkg/online/accounts` | 可用 | 自部署 HTTPS 账号服务（`cmd/accountsd`，含 `accounts/authtoken`、`sqlitestore`、`pgstore`、`sqlstore`、`storetest`）。支持游客、邮箱密码，以及通过自定义 `IdentityVerifier` 对接微信、华为、TapTap、Apple 或小程序身份，不依赖 Play 游戏服务。实名认证扩展规划见 [#1649](https://github.com/nsgo-dev/game/issues/1649)。 |
| `pkg/online/achievements` | 部分可用 | 本地 `Tracker` 与 `Persist`（`core.Storage`）在所有平台均无 Google 依赖。`ServiceFor(core.OSAndroid)` 会选择 `PlayGames`，在无 Google Play 服务的 Android 设备（以及当前 NSGo `v0.3.4` 的 Android 整体；见[平台支持](platforms.md)）上 `SignIn` 会返回 `gameservices.ErrUnsupported`。可自行绘制游戏内成就界面，或通过 `Tracker.Connect` 挂载自定义 `Reporter`。 |
| `pkg/online/audit` | 可用 | 纯服务端只追加 JSON Lines 审计日志（`pkg/online/audit`），无客户端或 Google 依赖。 |
| `pkg/online/cloudsave` | 可用 | 自部署 HTTPS 云存档服务（`cmd/cloudsaved`，含 `sqlitestore`、`pgstore`、`sqlstore`、`storetest`），由 `accounts/authtoken` 鉴权。推荐作为无 Google 服务 Android 与鸿蒙的云存档替代方案。 |
| `pkg/online/gameserver` | 可用 | 纯服务端无头固定步长对局运行器、房间、匹配、健康检查与指标，无 Google 依赖。 |
| `pkg/online/leaderboard` | 不可用 | `ProviderFor(core.OSAndroid)` 选择 `ProviderPlayGames`；缺少 Google Play 服务时 `Available()` 返回 `false`，`SignIn`/`Submit`/`Show`/`Scores` 返回 `ErrUnsupported`（iOS/macOS 上的 Game Center 仍正常）。请改用 [`pkg/online/leaderboard/selfhost`](leaderboard-selfhost.md)。 |
| `pkg/online/leaderboard/selfhost` | 可用 | 自部署跨平台 HTTPS 排行榜服务（`cmd/leaderboardd`），由 `accounts/authtoken` 鉴权。推荐用于无 Google 服务 Android、鸿蒙、桌面端与 Web。 |
| `pkg/online/liveops` | 部分可用 | `CrashReporter`（及 `liveops/crashreport`）、显式同意门控的 `Analytics`、`CloudClient`（对接 `cloudsave`）、`DirCloud` 和 `Reminders`（系统原生本地通知）均不依赖 Google 服务。`PlatformCloud` 在 Android 上选择 `CloudProviderPlayGames` 并返回 `ErrCloudUnavailable`，请改用 `CloudClient`。`Purchases` 在 Android 上不可用（返回 `core.PurchasesUnavailable`；[nsgo#1369](https://github.com/nsgo-dev/nsgo/issues/1369)、[nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370)；中国大陆 iOS 上 Apple StoreKit 可用）。`ReviewPrompt` 在 `CanRequestReview()` 为 `false` 时安全空操作。远程推送目前在所有平台均不可用（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)、[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)）。 |
| `pkg/online/netplay` | 可用 | 纯 Go 实现的 UDP、WebSocket、WebTransport 与回环传输（含 `netplay/wire`），无 Google 依赖。建议部署在已完成 ICP 备案的中国大陆服务器与域名上以保证连通性与延迟。 |
| `pkg/online/presence` | 不可用 | 仅限桌面端的丰富状态集成（Steam `ISteamFriends` 与 Discord 本地 IPC）；在 Android、iOS 与 Web 上返回 `ErrUnavailable`。无 Google 依赖（中国大陆桌面端 Steam 可用，Discord 通常不可达）。 |
| `pkg/online/storeverify/wechatpay` | 可用 | 纯服务端的微信小游戏虚拟支付订单签名，以及按 `OutTradeNo` 只发货一次的道具发货推送处理（[liveops](liveops.md)），无客户端或 Google 依赖。 |
| `pkg/online/wsclient` | 可用 | 原生与浏览器 `js/wasm` 通用的纯 Go RFC 6455 WebSocket 客户端，无 Google 依赖（小程序暂不可用，见 [nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)）。 |

### 自部署替代组合

如需在 iOS、无 Google 服务 Android、桌面端与 Web 上提供一致的账号、云存档与排行榜体验：

1. 部署 [`pkg/online/accounts`](accounts.md)（`cmd/accountsd` 或集成自定义 `IdentityVerifier` 的自有服务）签发短期 Ed25519 令牌。
2. 部署 [`pkg/online/cloudsave`](cloudsave.md)（`cmd/cloudsaved`），客户端使用 `liveops.CloudClient` + `liveops.CloudSyncer`（`SyncAsync`）替代 `liveops.PlatformCloud`。
3. 部署 [`pkg/online/leaderboard/selfhost`](leaderboard-selfhost.md)（`cmd/leaderboardd`）替代 `pkg/online/leaderboard`。
4. 成就进度通过 `achievements.Tracker` 与 `Persist(cx.Storage(), ...)` 留在本地，必要时通过自定义 `achievements.Reporter` 同步到自有后端。
5. 所有接口通过已完成 ICP 备案的中国大陆域名和 HTTPS 证书对外提供，以满足网络稳定性与商店合规要求。

## 各商店与平台说明

### Apple App Store（中国大陆）

- 在 App Store Connect 启用中国大陆地区上架前，必须填写工信部 APP 备案号。
- 付费下载或含应用内购买的游戏还必须在 App Store Connect 填报国家新闻出版署版号。
- Apple 原生服务不依赖 Google：StoreKit 应用内购买（`liveops.Purchases` 搭配 `apple.purchases: true`）、Game Center（`achievements`、`leaderboard`）、iCloud（`liveops.PlatformCloud`）以及 StoreKit 评分提示（`liveops.ReviewPrompt`）在中国大陆 iOS 设备上均可正常工作。

### Android 厂商商店与 TapTap

华为应用市场（Android）、小米应用商店、OPPO、vivo、荣耀与 TapTap 各自独立运作，均有独立的开发者后台、提审队列与隐私合规扫描器。

- **提交已签名的正式 `.apk`，而非 `.aab`**：中国大陆 Android 商店普遍分发已签名的 64 位（`arm64-v8a`）`.apk`，而非 Google Play 的 `.aab`。构建时请使用 `-channel direct`（不要使用会输出 `.aab` 的 `-channel store`）并配置正式签名密钥库：
  ```sh
  NSGO_KEYSTORE_PASSWORD="$STORE_PW" NSGO_KEY_PASSWORD="$KEY_PW" \
    go tool game release -channel direct -target android
  ```
  或以 `GOFLAGS=-tags=release` 直接调用 `nsgo build`：
  ```sh
  NSGO_KEYSTORE_PASSWORD="$STORE_PW" NSGO_KEY_PASSWORD="$KEY_PW" \
    GOFLAGS=-tags=release go tool nsgo build -channel direct -target android_arm64 \
    -keystore certs/upload.p12 -key-alias upload
  ```
- **渠道 SDK 与支付**：厂商联运（渠道服）通常要求接入各商店的渠道 SDK（登录、渠道支付、防沉迷、更新/退出弹窗）并分别打渠道包。NSGo 不内置各厂商专有渠道 SDK，且 Android 结算服务尚未在 NSGo 中实现（[nsgo#1369](https://github.com/nsgo-dev/nsgo/issues/1369)、[nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370)）。TapTap 常用于官服 APK 分发与测试。
- **在无 Google 服务的真机上验证**：提审前务必确认登录、存档、排行榜/成就及购买入口在缺少 Google Play 服务时能优雅降级，不会卡死或崩溃。
- **目标 SDK 与权限**：按各商店当前规范设置 `android.target_sdk`，并在 `nsgo.yaml` 的 `android.permissions` 中仅声明最小权限集合。

### 鸿蒙（HarmonyOS NEXT 华为应用市场）

HarmonyOS NEXT 仅运行原生鸿蒙安装包（`.hap` / `.app`），无法运行 Android APK。

- **构建与签名（预览阶段）**：NSGo 提供预览版 `harmonyos` 目标（在 ArkWeb 中运行 WebAssembly，并由 ArkTS/ArkUI 宿主绘制界面），输出 `<name>-<version>-harmonyos.hap`：
  ```sh
  NSGO_HARMONY_KEYSTORE_PASSWORD="$STORE_PW" NSGO_HARMONY_KEY_PASSWORD="$KEY_PW" \
    GOFLAGS=-tags=release go tool nsgo build -target harmonyos
  ```
  签署正式发布 HAP 需要 OpenHarmony SDK（`OHOS_BASE_SDK_HOME`，API 26）、`hvigorw`、Java（`hap-sign-tool.jar`）以及在 `nsgo.yaml` 中配置 AppGallery Connect 正式签名材料（详见 [NSGo 构建指南——HarmonyOS](https://nsgo.dev/docs/building/#harmonyos)）：
  ```yaml
  harmonyos:
    signing:
      keystore: signing/app.p12
      key_alias: app
      certificate: signing/app.cer
      profile: signing/app.p7b
  ```
  在项目目录中运行 `go tool nsgo doctor` 可校验签名配置和描述文件中的包名。
- **NSGo `v0.3.4` 预览阶段限制**：`harmonyos` 属于实验性预览目标，目前尚不满足正式游戏发布条件。如[平台支持](platforms.md)所述，当前仍缺少 3D GPU 视图、音频（`CanPlayAudio()` 为 `false`）、手柄、触觉反馈、运动传感器、持久化 `core.Storage`（目前仅内存存储）、原生本地通知、UDP、游戏服务以及鸿蒙 IAP Kit（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)、[nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370)）。
- **应用市场上架要求**：HarmonyOS NEXT 应用上架同样需要鸿蒙 APP 备案（绑定鸿蒙包名与签名公钥）、软件著作权、隐私声明，商业化游戏还需国家新闻出版署版号。

### 小程序（微信、QQ、抖音、支付宝）

NSGo 提供实验性目标 `miniprogram_wechat`、`miniprogram_qq`、`miniprogram_alipay` 与 `miniprogram_douyin`，在 `dist/releases/miniprogram/<platform>` 生成对应开发者工具工程，并在 `nsgo.yaml` 的 `miniprogram.appid` 中填写各平台 AppID：

```sh
GOFLAGS=-tags=release go tool nsgo build -target miniprogram_wechat
```

- **备案与审核**：小程序通过各宿主平台后台直接办理小程序备案。商业化/含内购的小游戏同样需要软著与版号（仅广告变现或非内购小游戏的最新规则请查阅各宿主平台官方文档）。
- **NSGo `v0.3.4` 实验阶段限制**：如[平台支持](platforms.md)所述，小程序受 2 MB 压缩包体上限约束，且当前缺少 3D GPU 视图、音频、着色器（`nsgo_noshader`）、手柄，以及浏览器标准 `fetch`/`WebSocket` 全局对象（[nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)）。本地存档可通过 `save.StorageBackend(cx.Storage(), ...)` 写入宿主存储，NSGo 亦提供 `core.SignInMiniProgram` 用于宿主登录。

## 官方参考链接

- [国家新闻出版署（NPPA）](https://www.nppa.gov.cn/)——网络游戏审批信息与政策公告
- [国家新闻出版署网络游戏防沉迷实名认证系统](https://wlc.nppa.gov.cn/)——实名认证与行为数据上报接口文档及入口
- [工业和信息化部政务服务平台（ICP / APP 备案管理系统）](https://beian.miit.gov.cn/)——ICP 与移动应用备案查询与规定
- [中国版权保护中心（CPCC）](https://www.ccopyright.com.cn/)——计算机软件著作权登记
- [中国音像与数字出版协会（CADPA）](https://www.cadpa.org.cn/)——网络游戏适龄提示团体标准
- [Apple App Store Connect 中国大陆支持](https://developer.apple.com/cn/support/app-store-connect/)——中国大陆 APP 备案号与版号提交流程
- [华为开发者联盟与 AppGallery Connect](https://developer.huawei.com/consumer/cn/doc/)——Android 应用市场及 HarmonyOS NEXT 签名与发布文档
- Android 厂商与分发后台：[小米开放平台](https://dev.mi.com/)、[OPPO 开放平台](https://open.oppomobile.com/)、[vivo 开放平台](https://dev.vivo.com.cn/)、[荣耀开发者服务平台](https://developer.honor.com/)、[TapTap 开发者中心](https://developer.taptap.cn/)
- 小程序开发文档：[微信](https://developers.weixin.qq.com/miniprogram/dev/framework/)、[QQ](https://q.qq.com/wiki/)、[抖音](https://developer.open-douyin.com/)、[支付宝](https://opendocs.alipay.com/mini)
- [NSGo 构建指南](https://nsgo.dev/docs/building/)——鸿蒙、小程序与 Android 密钥库配置详情
