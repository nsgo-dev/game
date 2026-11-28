# 线上运营（Live operations）

## Web 门户广告间隔

`liveops.Portal` 为 Poki 和 CrazyGames Web SDK 提供与供应商无关的接口。首个可玩的场景准备好后调用 `LoadingFinished`，并使用 `GameplayStart` 与 `GameplayStop` 包围实际游玩。`Break` 和 `Rewarded` 会在调用 SDK 前暂停游戏、静音并重置输入，在成功、错误或超时后恢复音频与游玩状态。只有供应商返回 `true` 时才发放奖励；原生构建和测试中的 `PortalNone` 是空操作。

```go
import "github.com/nsgo-dev/game/pkg/online/liveops"
```

`liveops` 为 NSGo 游戏提供与具体服务商无关的线上运营基础能力。它只依赖 Go 标准库、`github.com/nsgo-dev/game` 和 NSGo 的公开 `core`、`cloud` API，不内置任何第三方崩溃上报、埋点分析、广告或支付 SDK。面向中国大陆发布时的首次启动隐私同意门控、无 Google Play 服务 Android 设备的自部署替代方案及规划中的防沉迷合规能力，详见[在中国大陆发布](shipping-china.md)。

## 范围决策（[#194](https://github.com/nsgo-dev/game/issues/194)）

| 能力 | 状态 | 说明 |
| --- | --- | --- |
| **崩溃报告**（[#195](https://github.com/nsgo-dev/game/issues/195)） | 纳入范围 | 与服务商无关的 panic 捕获，附带固定大小步长环形缓冲区、堆栈、元数据和可选状态快照。 |
| **统计分析**（[#198](https://github.com/nsgo-dev/game/issues/198)） | 纳入范围 | 与服务商无关、严格以显式同意为前提的事件 API，同意状态保存在 `core.Storage`。未显式同意前不会发送也不会缓存任何事件。 |
| **云存档**（[#196](https://github.com/nsgo-dev/game/issues/196)） | 纳入范围 | 与服务商无关的 `CloudSync` 接口、基于 NSGo `cloud` 包的 `PlatformCloud`（macOS/iOS 上的 iCloud、Android 上的 Google Play Games 存档服务以及 `nsgotest` 内存存储）、面向 Steam Auto-Cloud 及其他目录同步商店的 `DirCloud`、访问自部署 [cloudsave](cloudsave.md) 服务的 `CloudClient`（[#557](https://github.com/nsgo-dev/game/issues/557)），以及 `CloudSyncer` 冲突解决（[#539](https://github.com/nsgo-dev/game/issues/539)）。 |
| **应用内购买**（[#369](https://github.com/nsgo-dev/game/issues/369)） | 纳入范围 | 基于 NSGo 购买服务的 `Purchases`：消耗型商品恰好一次入账到存档，解锁项与订阅来自商店权益，支持恢复购买、“询问后购买”和可选的服务器校验。 |
| **本地提醒**（[#1310](https://github.com/nsgo-dev/game/issues/1310)） | 纳入范围 | 基于 NSGo `core.Scope.ShowNotification` 的 `Reminders`：带版本号的 YAML 声明（`RemindersConfig`）、仅在首次 `Schedule` 或显式 `RequestPermission` 时申请权限、持久化玩家开关、本地时间 `QuietHours` 免打扰时段，以及应用回到前台时的取消/重排/保留规则。 |
| **商店评分提示**（[#1767](https://github.com/nsgo-dev/game/issues/1767)） | 纳入范围 | 基于 `core.Storage` 与 `core.Scope.RequestReview` 的 `ReviewPrompt`：按会话数、距首次启动天数、正向时刻、冷却期和每个版本最多一次的策略控制商店评分请求，负向时刻后在本会话内抑制提示，状态损坏或平台不支持时默认关闭。 |
| **分享卡片与邀请**（[#1738](https://github.com/nsgo-dev/game/issues/1738)） | 纳入范围 | 基于 `ShareHost` 接口的 `Invites`：有界规范 `key=value&…` 负载、可选 HMAC-SHA256 签名校验、冷启动与运行期重入各至多一次投递、原生/Web 适配器 `PlatformShareHost`、小游戏适配器 `MiniGameShareHost`（等待 [nsgo#2672](https://github.com/nsgo-dev/nsgo/issues/2672)）以及 5:4 画面截图助手。 |
| **远程推送**（[#1310](https://github.com/nsgo-dev/game/issues/1310)） | 等待上游 | 等待 NSGo 落地远程推送（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)）并确定开放设计决策（[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)）。在此之前，远程推送在所有平台上均不可用。 |
| **广告**（[#199](https://github.com/nsgo-dev/game/issues/199)） | 不纳入核心层 | 广告网络（AdMob、AppLovin 等）依赖专有原生 SDK，并伴随繁重的商店政策与隐私合规义务，不放入核心 `game` 模块；需要广告的游戏可直接通过 NSGo 嵌入原生 SDK 视图。 |

## 崩溃报告

`CrashReporter` 用预分配的环形缓冲区保存最近若干次模拟步长（`DefaultHistorySize` = 120，稳态零堆分配），并为 `game.Loop` 回调或任意函数提供 panic 防护。

- `RecordStep(dt, summary)` 与 `RecordStepData(dt, summary, data)` 向环形缓冲区追加一条 `StepRecord` 并返回单调递增的步数。
- `WrapLoop(l)` 返回带防护的 `*game.Loop`；`GuardLoop(l)` 原地包装 `l.Update` 和 `l.Draw`。如果 `Update` 在单步内未自行调用 `RecordStep`，防护层会自动记录一条 `"update"` 步长（包含触发 panic 的那一步）。
- `RunSafe(fn)` 防护任意回调并报告是否发生 panic。
- 发生 panic 时，报告器构造可 JSON 序列化的 `CrashReport`（包含 `Timestamp`、`Panic`、`Message`、`Stack`、`Step`、按时间从旧到新排列的 `LastSteps`、来自 `CaptureState` 的可选 `State` 以及 `Metadata` 副本），并交给 `Reporter`。
- 需要上报后继续抛出 panic 时，可设置 `Rethrow: true`。
- `CrashReport` 和 `StepRecord` 是 `pkg/online/liveops/crashreport` 中类型的别名；该包不依赖任何 NSGo 或 game 包，因此无头专用服务器也能构造同样的报告结构（`gameserver.Panic.Report`，见[服务器指南](gameserver.md#停止)）。已有代码无需改动。

```go
reporter := liveops.NewCrashReporter(liveops.CrashOptions{
	HistorySize:  120,
	Reporter:     func(r liveops.CrashReport) { uploadOrSave(r) },
	CaptureState: func() any { return world.Snapshot() },
	Metadata:     map[string]string{"version": appVersion, "scene": "level-1"},
	Now:          cx.Now,
})

loop := reporter.WrapLoop(&game.Loop{
	Update: func(dt time.Duration) {
		reporter.RecordStep(dt, "world.step")
		world.Step(dt)
	},
	Draw: func(alpha float64) { world.Draw(alpha) },
})
loop.Run(cx)
```

## 统计分析

`Analytics` 要求玩家显式同意（`ConsentGranted`）后才分发任何事件，且不内置任何第三方埋点后端。

- 同意状态为 `ConsentUnknown`（默认）、`ConsentDenied` 和 `ConsentGranted`。
- `NewAnalytics(store, key, provider)` 把同意状态持久化到 `core.Storage` 的 `key` 下（为空时使用 `DefaultConsentKey`）；缺失或无法识别的值一律按默认关闭处理（`ConsentUnknown`）。`NewMemoryAnalytics(provider)` 仅在内存中保存状态。
- 处于 `ConsentUnknown` 或 `ConsentDenied` 时，`Track(name, props)` 直接丢弃事件并返回 `false`，绝不发送，也不缓存供后续补发。
- 处于 `ConsentGranted` 时，`Track` 对 `props` 做防御性拷贝，并通过 `AnalyticsProvider.Send` 分发 `Event`。
- 调用 `SetConsent(ConsentDenied)` 撤回同意后，所有后续 `Track` 和 `Flush` 调用会立即停止。

```go
analytics := liveops.NewAnalytics(cx.Storage(), "liveops.consent", myProvider)
analytics.SetClock(cx.Now)

// 未显式同意前直接丢弃：
analytics.Track("app_open", nil) // 返回 false，发送 0 条事件

// 玩家同意后：
_ = analytics.SetConsent(liveops.ConsentGranted)
analytics.Track("level_complete", map[string]any{"level": 1})
```

## 云存档

`CloudSync` 定义了与服务商无关的存档槽位接口（`Load`、`Save`、`Delete`、`List`，以及 `ErrSlotNotFound`、`ErrCloudUnavailable`、`ErrCloudConflict`、`ErrSlotTooLarge` 和 `ErrTooManySlots` 哨兵错误）。`ConditionalCloudSync` 增加了 `SaveIf` 和 `DeleteIf`，它们只在槽位仍保存着调用方读到的负载（以 `PayloadHash` 标识）时才写入。

三个内置适配器在同一个 `CloudSync` 接口背后工作：

| 适配器 | 服务 / 目标平台 | 说明 |
| --- | --- | --- |
| `PlatformCloud` | **iCloud**（macOS、iOS、iPadOS、tvOS）、**Google Play Games 存档服务**（Android）、`nsgotest` 隔离内存存储 | 基于 `github.com/nsgo-dev/nsgo/cloud`。在 Windows、Linux、Web 或未登录/缺少权限时返回 `ErrCloudUnavailable`，绝不悄悄丢弃写入。 |
| `DirCloud` | **Steam Auto-Cloud** 及其他按目录同步的桌面商店（Windows、Linux、macOS） | 实现 `ConditionalCloudSync`，将每个槽位原子写入同步目录中的 `<slot>.json` 文件（例如 `filepath.Join(cx.DataDir(), "cloud")`）。 |
| `CloudClient` | 自部署 [cloudsave](cloudsave.md) 服务（[#557](https://github.com/nsgo-dev/game/issues/557)）（含 Web 在内的全部平台） | 经 HTTPS 访问的 `ConditionalCloudSync`，通过[账号服务](accounts.md)登录，支持大小上限和历史版本（`Versions`、`LoadVersion`、`Restore`、`Purge`）。 |

游戏也可以针对自有后端或内存测试替身实现 `CloudSync`。

### 平台云存档：iCloud、Play Games 与 Steam Cloud（[#196](https://github.com/nsgo-dev/game/issues/196)）

`PlatformCloud` 通过 NSGo 的 `cloud` 文件/快照 API，把每个存档槽位存为 `<Prefix><slot>.json`（默认 `DefaultCloudPrefix` = `"save."`，例如 `save.main.json`）。扁平的点分隔文件名不含路径分隔符，因此 Apple iCloud 通用容器和 Google Play Games `SnapshotsClient` 都能接受，同时把存档槽位与容器中的其他文件隔离开来。

```go
pc, err := liveops.NewPlatformCloud(cx, liveops.PlatformCloudOptions{})
if err != nil {
	return err
}
syncer, err := liveops.NewCloudSyncer(liveops.CloudSyncOptions{
	Local:  save.StorageBackend(cx.Storage(), "mygame"),
	Cloud:  pc,
	State:  cx.Storage(),
	Device: deviceID,
	Policy: liveops.PolicyKeepBoth,
})
if err != nil {
	return err
}

// 另一台设备更新槽位时重新同步：
pc.WatchChanges(func(ev liveops.CloudChange) {
	syncer.SyncAsync(cx, ctx, func(res liveops.SyncResult, err error) { /* 更新界面 */ })
})

// 平台报告 iCloud / Play Games 原生文件冲突时予以解决：
pc.OnConflict(func(c liveops.PlatformConflict) {
	chosen := c.Remote
	if c.LocalTime.After(c.RemoteTime) {
		chosen = c.Local
	}
	_ = c.Resolve(chosen)
})
```

- `CloudProviderFor(env)` 与 `pc.Provider()` 报告目标平台内置的云存档服务（`CloudProviderICloud`、`CloudProviderPlayGames`、`CloudProviderNone`；`CloudProviderSteam` 表示 Steam Auto-Cloud）；当 `env` 为 nil 或 `env.Simulated()` 为 true（例如运行在 iOS/Android 上的小程序，或带 `--nsgo-os` 的 Web 构建）时返回 `CloudProviderNone`，不会报告 iCloud 或 Play Games。`pc.Available()` 以响应式方式读取 `cx.Env().CanUseCloudStorage()`（且在 `Simulated()` 为 true 时返回 false），便于界面随 iCloud 或 Play Games 可用性变化而更新。
- 在真实的 Apple 和 Android 设备上，NSGo `cloud` 会通过 `cx.Post` 在引擎 goroutine 上派发回调，因此请通过 `CloudSyncer.SyncAsync` 运行 `PlatformCloud` 同步（或在引擎 goroutine 之外调用 `PlatformCloud`），避免同步 `Sync` 阻塞引擎 goroutine。`SyncAsync`、`SyncSlotAsync` 和 `ResolveAsync` 在工作 goroutine 上执行云端 I/O，同时把 `Local`、`State`、`Summarize`、`Now` 和 `Ask` 调度回 `cx` 的引擎 goroutine，按顺序串行化并发调用，并在云端读取期间本地有新存档写入时拒绝覆盖（`ErrConflictChanged`）。
- 在 macOS 和 iOS 上，需在 `nsgo.yaml` 的 `cloud:` 下声明 iCloud 权限（`kv_store` / `containers: [iCloud.com.example.myapp]`）；Mac App Store 构建需要带对应权限的描述文件，否则 iCloud 会返回 `ErrCloudUnavailable`。
- 当 iCloud（`NSFileVersion`）或 Google Play Games（`SnapshotsClient`）对某槽位存在未解决的原生冲突时，`PlatformCloud.Load` 会返回包装了 `ErrCloudConflict`、`cloud.ErrConflict` 和 `*cloud.ConflictError` 的错误；`CloudSyncer` 会跳过该槽位并继续同步其他槽位，直到通过 `PlatformConflict.Resolve`（或重新写入）解决冲突。
- 在 **Steam**（Windows、Linux、macOS）上，配置 Steam Auto-Cloud 同步 `cx.DataDir()` 下的目录即可。不需要 `CloudSyncer` 冲突信封的游戏可直接让 Steam Auto-Cloud 同步 `save.FileBackend(cx.DataDir())`（`*.json`）；希望所有平台共用同一套 `CloudSync` + `CloudSyncer` 冲突解决流程的游戏，可将 `liveops.NewDirCloud(filepath.Join(cx.DataDir(), "cloud"))` 作为 `Cloud` 传入。`DirCloud` 实现了 `ConditionalCloudSync`，具备原子临时文件写入、目录同步和大小写折叠冲突检测。Windows、macOS 与 Linux / SteamOS 上对应的 Steamworks Auto-Cloud 根目录与覆盖规则见 [Steam 云存档](steam.md#steam-云存档)。
- 在 `nsgotest` 中，以 `nsgotest.CloudAvailable(true)` 启动应用，并通过 `app.SimulateCloudChange`、`app.SimulateCloudConflict` 和 `app.SetCloudAvailable` 驱动跨设备变更、原生冲突和离线回退；`liveops/cloud_platform_test.go` 与 `liveops/cloud_dir_test.go` 覆盖了上述每种场景。

### 解决冲突（[#539](https://github.com/nsgo-dev/game/issues/539)）

`CloudSyncer` 把设备上的 `save.Backend` 与 `CloudSync` 对齐；同一槽位在两台设备上都有改动时，绝不会悄悄丢掉进度。

```go
syncer, err := liveops.NewCloudSyncer(liveops.CloudSyncOptions{
	Local:  save.StorageBackend(cx.Storage(), "mygame"),
	Cloud:  myCloud,
	State:  cx.Storage(),
	Device: deviceID, // 1 到 64 个 ASCII 字母、数字、'-' 或 '_'
	Policy: liveops.PolicyAsk,
	Ask: func(ctx context.Context, c liveops.Conflict) (liveops.Choice, error) {
		pending = append(pending, c) // 交给游戏的冲突对话框展示
		return liveops.ChoiceDefer, nil
	},
	Summarize: func(slot string, data []byte) string { return describeSave(data) }, // "第 12 关，已玩 3 小时 40 分"
})
if err != nil {
	return err
}
store := save.NewStore(syncer.Backend()) // 为每次本地写入记下时间

res, err := syncer.Sync(ctx)
for _, r := range res.Conflicts() {
	log.Printf("%s: %s", r.Slot, r.Outcome)
}

// 稍后，玩家在对话框中选定一份副本时：
r, err := syncer.Resolve(ctx, c, liveops.ChoiceLocal) // 或 ChoiceCloud、ChoiceBoth
if errors.Is(err, liveops.ErrConflictChanged) {
	// 对话框打开期间，存档或同步改动了该槽位：重新同步，并就新的冲突再次询问。
}
```

冲突如何检测：

- 同步器为每个槽位记住基线：上次同步后两端共同持有的内容哈希（`sha256:…`）和云端修订号，以 JSON 文本保存在 `State` 的 `StateKey` 前缀下（为空时用 `DefaultSyncStateKey`）。
- `Sync` 按名称顺序处理本地、云端或同步状态中出现过的每个槽位。只有一端与基线不同时，把这一端复制到另一端（`OutcomeUploaded`、`OutcomeDownloaded`、`OutcomeDeletedCloud`、`OutcomeDeletedLocal`）。两端都与基线不同且彼此也不同时，该槽位就是冲突。从未同步过某槽位的设备若持有与云端不同的副本，同样视为冲突，绝不直接覆盖。
- 每次写云端前，同步器都会重新读取云端副本；如果其间另一台设备已上传，该槽位以 `ErrCloudChanged` 失败，不写入任何内容，下次同步会看到新副本。当 `Cloud` 实现了 `ConditionalCloudSync`（`CloudClient` 和 `DirCloud` 皆是）时，写入本身以该副本为条件，因此在重新读取之后才落地的上传也会以同样方式失败，而不会被覆盖。
- 失败的槽位（损坏的负载、不安全的槽位名、`Ask` 返回错误）会被跳过，错误合并进返回值，其余槽位照常同步；`ErrCloudUnavailable` 或已结束的 context 会中止同步。

每个 `Conflict` 为两端各带一个 `SaveVersion`：是否存在（某一端可能删除了该槽位）、修订号、哈希、修改时间、写入设备、存档 `EnvelopeVersion`、游戏 `Summarize` 给出的摘要以及原始数据。将 `CloudSyncOptions.SaveVersion` 设为当前构建写入的 `save.Store.Version`，可保证较新游戏版本写入的云端副本绝不会覆盖本地槽位（返回 `OutcomeNeedsUpdate`，附带 `SlotResult.LocalVersion`、`SlotResult.CloudVersion` 及 `SyncResult.NeedsUpdate()`），也不会被旧版本客户端覆盖。各策略如下：

| 策略 | 行为 |
| --- | --- |
| `PolicyNewest`（默认） | 保留修改时间较晚的副本并覆盖另一份（`OutcomeKeptLocal` 或 `OutcomeKeptCloud`）。任一时间未知（例如未经 `Backend()` 写入的本地副本；记下的时间只对应记录时的那份副本）或两者相等时保留两份。一端删除、另一端修改时保留修改后的副本。两端存档信封版本不同时，绝不偏向较旧的信封版本，而是回退为保留两份。 |
| `PolicyKeepBoth` | 槽位中保留云端副本，把本地副本在两端都移到 `<slot>.conflict-<device>`（依次再用 `-2`、`-3`……）（`OutcomeKeptBoth`，`SlotResult.Fork`）。云端副本高于 `SaveVersion` 时，本地副本会在两端分叉保留，但本地与云端主槽位均保持不动（`OutcomeNeedsUpdate`）。该名称超过 200 字节的槽位名上限时，槽位部分会被截短，并在其后加上 `~` 和槽位名 SHA-256 的 8 位十六进制。在覆盖任何内容之前，先在本地写好分叉副本及其同步状态，因此上传失败时，下次同步会上传同一个分叉，而不是再建一个。删除对修改时保留修改。 |
| `PolicyAsk` | 用冲突（含两端 `EnvelopeVersion`）调用 `Ask` 并执行其 `Choice`：`ChoiceLocal`、`ChoiceCloud`、`ChoiceBoth`（同 `PolicyKeepBoth`）或 `ChoiceDefer`；`ChoiceDefer` 不写入任何内容，下次同步再次询问。`Ask` 返回错误时两份副本都保持不变。偏向较旧信封版本的决议会回退为保留两份，应用高于 `SaveVersion` 的云端副本会返回 `OutcomeNeedsUpdate` 而不覆盖本地槽位。 |

`Ask` 在同步过程中、在应用线程上运行，因此不能等待玩家作答。要通过对话框询问，就保存 `Conflict`、返回 `ChoiceDefer`、展示两份摘要，再用 `Resolve(ctx, conflict, choice)` 执行玩家的选择，如上例所示。`Resolve` 会重新读取两端，只有两端仍持有玩家看到的那两份副本（是否存在、哈希和云端修订号都相同）时才执行选择；否则不写入任何内容并返回 `ErrConflictChanged`。它在任何策略下都可用。

只要某次解决替换了一份副本，`SlotResult.Discarded` 都会返回它，方便游戏展示或归档。被替换的本地副本是有效的存档信封且云端副本存在时，本地后端也会把它保留为备份（`ReadBackup`）；云端副本是删除，或本地副本不是存档信封时，`Discarded` 是仅存的一份。

云端负载是键顺序固定的 JSON 文本：`format`（`CloudSaveFormat`）、`version`（`CloudSaveVersion`）、`revision`、`hash`、`modified`（RFC 3339，UTC）、`device` 和 `data`；`data` 是按原样保存存档的字符串（非 UTF-8 数据改用 `data_base64`）。`DecodeCloudSave` 以 `ErrCorruptCloudSave` 拒绝未知字段、更新的版本、尾随数据以及与数据不符的哈希；把 `EncodeCloudSave` 写出的负载解码后再编码，得到的字节完全相同。没有格式标记的云端负载（由直接使用 `CloudSync` 的游戏写入）按修订号 0 读取，并在下次上传时升级。云端槽位名在交给本地后端之前会先经 `save.ValidateSlot` 检查。

`Backend()` 通过 `Unwrap` 暴露本地后端，因此基于它创建的设置存储（`save.NewDefaultSettingsStore(syncer.Backend())`）会把设置保存在本地后端自己的设置键下，设置永远不会作为槽位同步。

`CloudSyncer` 不支持并发同步调用；请在应用代码运行的地方调用 `Sync`、`SyncSlot` 和 `Resolve`（这是 `core.Storage` 的要求），或使用 `SyncAsync`、`SyncSlotAsync` 和 `ResolveAsync` 在引擎 goroutine 之外执行云端 I/O，同时把本地存储和 `Ask` 调度回 `cx`（在 Apple 和 Android 设备上搭配 `PlatformCloud` 时必须如此，搭配 `CloudClient` 时也推荐如此）。普通的 `CloudSync` 没有条件写入，因此另一台设备的上传若恰好落在同步器重新读取与自身写入之间，仍可能被覆盖；这个窗口只隔一次 `Load` 和一次 `Save`。`ConditionalCloudSync` 消除了这个窗口。

## 应用内购买

`Purchases` 基于 NSGo 的购买服务（`cx.Purchase`、`cx.OnPurchaseUpdate`、`cx.CurrentEntitlements`、`cx.SyncPurchases`）出售游戏的商品。它只在 `cx.Env().CanPurchase()` 为 true 时可用，在 NSGo v0.3.0 中即 macOS、iOS 和 tvOS 上 `nsgo.yaml` 设置了 `apple.purchases: true` 的构建所连接的 App Store；其他平台上 `Buy` 以 `core.PurchasesUnavailable` 失败，`Available` 返回 false。

- 消耗型商品只入账一次。把 `Ledger` 放进存档，与 `Credit` 修改的状态放在一起：`Purchases` 先调用 `Credit`，把交易 ID 记入账本，再调用 `Save`，之后才完成（finish）交易。在保存前被杀掉，入账和记录一起丢失，商店会在下次启动时再次交付该交易；在保存后被杀掉，下次启动会在账本中找到该 ID，只完成交易而不重复入账。`Save` 失败时交易保持未完成。
- 非消耗型商品和订阅是“拥有”而非“入账”：`Owned(product)` 读取商店的权益（`NewPurchases` 启动时加载，`Refresh` 重新加载），并用 `cx.WallTime()` 判断订阅是否过期。在响应式计算中读取 `Owned` 和 `Pending`，购买、退款和恢复购买会自动更新视图；应用运行期间到期的订阅要到下次读取或 `Refresh` 时才会反映出来，因此请在应用回到前台时调用 `Refresh`。同一商品较旧的交易（例如启动时作为未完成交易再次交付的已过期续订）不会取代较新的交易。
- `Restore` 执行商店同步（只能由玩家按下“恢复购买”按钮时调用，因为它可能要求登录），然后重新加载权益，使重装后存档已被清空的解锁项恢复。
- `Buy` 的结果为 `Bought`、`BuyPending`（“询问后购买”等待审批；`Pending(product)` 会一直为 true，直到批准后的交易通过商店更新到达，并像 `Buy` 一样被交付；请求被拒绝时不会产生交易，`Pending` 会保持为 true，直到该商品之后的购买到达或应用重启）、`BuyCancelled`，或带错误的 `BuyFailed`。
- 退款或撤销会移除解锁项；对已入账的消耗型商品，若游戏设置了 `Revoke`，会调用它一次。账本只保留最新的 `LedgerLimit`（1024）个 ID，因此在其后又有这么多笔消耗型商品入账后，再到达的旧交易退款只会被完成，不会调用 `Revoke`。
- `Verify` 在交付前把每笔交易（其 `Signed` 为商店的 JWS）交给游戏服务器校验，可以在任意 goroutine 中回答；被拒绝的交易既不交付也不完成。未设置 `Verify` 时，只交付设备已校验的交易。
- `OnError` 接收没有 `Buy` 或 `Restore` 回调可报告的失败，例如启动时某笔未完成交易的保存失败。

```go
type Progress struct {
	Coins     int            `json:"coins"`
	Purchases liveops.Ledger `json:"purchases"`
}

saves := save.NewStorageStore(cx.Storage(), "game")
var p Progress
_ = saves.Load("main", &p)

shop := liveops.NewPurchases(cx, liveops.PurchasesOptions{
	Ledger: &p.Purchases,
	Credit: func(t core.Transaction) { p.Coins += 100 * t.Quantity },
	Revoke: func(t core.Transaction) { p.Coins -= 100 * t.Quantity },
	Save:   func() error { return saves.Save("main", &p) },
	Verify: func(t core.Transaction, done func(error)) { go done(server.Verify(t.Signed)) },
})

shop.Buy("com.example.coins.100", 1, func(r liveops.BuyResult, err error) { /* 已购买、等待审批、已取消或失败 */ })
showNoAds := !shop.Owned("com.example.noads")
```

测试通过 `nsgotest` 驱动购买：用 `nsgotest.Environment(core.Environment{Purchases: true})` 启动应用并设置 `app.Products`；用 `app.PurchaseAnswer` 模拟取消或“询问后购买”，用 `app.UpdatePurchase` 模拟批准或退款，用 `nsgotest.UnfinishedTransactions` 加 `nsgotest.Storage` 模拟完成交易前被杀掉的一次运行，用 `app.Entitlements` 加 `Restore` 模拟重装。`liveops/purchases_test.go` 覆盖了以上每种情况。

要在 StoreKit Testing 下手动验证恢复购买：在 iOS 应用的 Xcode scheme 中加入包含一个非消耗型商品的 StoreKit 配置文件，以 `apple.purchases: true` 构建，在模拟器中购买该商品，删除并重新安装应用，然后按下游戏的“恢复购买”按钮，`Owned` 会再次报告拥有该商品。NSGo v0.3.0 在 Android 上没有商店（Google Play Billing 等待 [nsgo#1369](https://github.com/nsgo-dev/nsgo/issues/1369)）。

## 本地提醒

`Reminders` 基于 NSGo 的 `core.Scope.ShowNotification` 调度、取消和重排本地召回通知。只要 `cx.Env().CanScheduleNotifications()` 为 true，它就能工作：在 macOS、iOS、watchOS、Android 和 Windows 上由系统原生调度（应用关闭后依然生效），在 Linux 和 Web 上于应用或页面运行期间生效。

- **带版本号的 YAML 声明**：在 `reminders.yaml`（Schema `https://game.nsgo.dev/schemas/reminders.v1.json`）中声明提醒，用 `ParseReminders` 或 `LoadRemindersFS` 加载，用 `EncodeReminders` 确定性写出。每条 `ReminderDef` 包含唯一 `id`、本地化键 `title` 和可选 `body`、正数延迟 `delay` 或 24 小时制本地挂钟时间 `at`（二者互斥；按周触发时可搭配 `weekday`）、可选 `repeat`（`daily` 或 `weekly`）、可选 `onOpen`（`cancel`、`reschedule` 或 `keep`，省略时默认为 `cancel`）以及可选 `silent`。未知键、重复键、YAML 锚点/别名和不支持的版本号都会被拒绝。
- **权限申请时机**：`NewReminders` 绝不在启动时查询或弹窗申请通知权限。只有在游戏首次调用 `Schedule` / `ScheduleWith`（且提醒处于开启状态并受平台支持）或显式调用 `RequestPermission` 时才会申请权限。并发调用会合并为一次系统请求，`Permission()` 以响应式方式暴露最近观察到的 `core.NotificationPermission`。
- **玩家开关与免打扰时段**：`Enabled()` / `SetEnabled(bool)` 将玩家开关持久化到 `core.Storage`；关闭提醒会立即取消所有已调度的提醒，并阻止后续 `Schedule` 弹窗申请权限。`QuietHours` 定义每日本地时间窗口 `[Start, End)`（支持跨午夜，如 `"22:00"` 到 `"08:00"`），落在窗口内的提醒会自动顺延到 `End` 触发。`SetQuietHours` 会持久化玩家自定义设置（传入 `nil` 会保存 `"off"`，避免重启后回退到配置文件默认值），并在不弹窗申请权限的前提下顺延已落入新窗口的待触发提醒。
- **回到前台时自动对账**：`Reminders` 监听 `cx.Env().Lifecycle()`，在启动和每次应用回到前台时对已调度提醒进行对账——取消 `onOpen: cancel`、按当前 `WallTime()` 无感重排 `onOpen: reschedule`，并保留 `onOpen: keep`。
- **平台能力与点击事件**：`CanScheduleNotifications()` 响应式转发 `cx.Env().CanScheduleNotifications()`，方便游戏在不支持定时通知的平台（如 tvOS）隐藏相关设置。玩家点击已送达的提醒通知时，`RemindersOptions.OnPress` 会收到 `ReminderPress{ID}`；内部使用专用通知 ID 前缀（`DefaultReminderPrefix`），确保 `CancelAll` 和点击路由不会干扰应用的其他通知。

```go
cfg, err := liveops.LoadRemindersFS(assetsFS, "reminders.yaml")
if err != nil {
	return err
}
reminders, err := liveops.NewReminders(cx, liveops.RemindersOptions{
	Config:   cfg,
	Localize: func(key string) string { return loc.T(key) },
	OnPress: func(ev liveops.ReminderPress) {
		openReminderDestination(ev.ID)
	},
})
if err != nil {
	return err
}

// 在不支持定时通知的平台上隐藏开关：
showReminderToggle := reminders.CanScheduleNotifications()

// 仅在合适时机申请权限并调度（绝不在启动时弹窗）：
_ = reminders.Schedule("daily_reward", func(scheduled bool) { /* 更新界面 */ })
```

测试通过 `nsgotest` 无头驱动提醒：用 `nsgotest.Environment(core.Environment{ScheduledNotifications: true})` 和 `nsgotest.WallTime(...)` 启动应用，用 `app.FastForward(...)` 推进时间，用 `app.SetEnvironment(...)` 模拟切后台与回前台，用 `app.PressNotification(...)` 模拟点击通知，并用 `nsgotest.Storage(...)` 验证重启后的持久化行为。`liveops/reminders_test.go` 覆盖了上述每条路径。

### 远程推送（等待上游）

远程推送通知（`liveops.Push`）目前在所有平台上均不可用，且有意暂不提供实现。游戏层封装——包括向游戏服务器上报设备令牌、把推送负载路由到线上活动处理器，以及将远程通知点击统一转为 `ReminderPress`——需等待 NSGo 实现远程推送（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)）并敲定其开放 API 设计决策（[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)）。

## 商店评分提示

`ReviewPrompt` 负责决定何时调用 NSGo 的 `core.Scope.RequestReview`，使游戏只在正向时刻之后请求商店评分、每个应用版本最多询问一次，且绝不在令玩家受挫的时机打扰玩家。

### 商店规范摘要

- **Apple App Store（StoreKit）**：每个应用在任意 365 天内最多展示三次系统评分弹窗（跨版本累计）。不要由按钮或玩家直接点击触发 `RequestReview`，也不要打断对局或弹窗流程；应在任务完成或正向里程碑后的自然停顿处请求。
- **Google Play（In-App Review API）**：由商店自行施加配额且不会告知应用弹窗是否实际显示。应等玩家充分体验游戏、能够给出有效反馈后再请求，并且不要在调用商店 API 之前先弹出诱导性前置问卷（如“你喜欢这款游戏吗？”）。

### 策略与持久化

- **阈值与默认值**：`NewReviewPrompt(store, opts)` 在启动时递增会话次数，并在 `MaybeAsk(cx)` 中依次检查：`MinSessions`（默认 `DefaultReviewMinSessions` = 3，绝不在首次会话询问）、距首次启动的 `MinDays`（默认 `DefaultReviewMinDays` = 3 天）、当前 `AppVersion` 下的 `MinPositiveMoments`（默认 `DefaultReviewMinPositiveMoments` = 3）、跨版本的请求冷却期 `Cooldown`（默认 `DefaultReviewCooldown` = 122 天，确保任意 365 天窗口内不超过 Apple 的 3 次上限），以及每个 `AppVersion` 最多一次。
- **正向/负向时刻与抑制**：在通关、刷新纪录等正向里程碑后调用 `Moment(MomentPositive)`，在失败或出错后调用 `Moment(MomentNegative)`。只要当前会话记录过 `MomentNegative`，本会话剩余时间将不再弹出评分请求；下次启动或调用 `RecordSession()` 会开启干净的新会话。可通过 `Suppress` 在购买页、广告流程或模态框打开期间临时抑制提示。
- **带版本号的 JSON 状态与失败即关闭**：状态以确定性的带版本号 JSON 文本（`ReviewPromptStateVersion` = 1）持久化到 `core.Storage` 的 `StorageKey`（为空时使用 `DefaultReviewPromptKey`）。遇到未知字段、尾随数据、非法时间戳、负数计数或未知/更高版本号时一律默认关闭（`Eligible()` 与 `MaybeAsk(cx)` 返回 `false`，且不覆写已存数据）。`AppVersion` 变更时会重置当前版本的 `positiveMoments`，同时保留 `firstLaunch`、`sessions` 与 `lastPromptedAt`，确保热修版本仍受冷却期约束。`MaybeAsk` 会先持久化本次请求记录，写入成功后再调用 `cx.RequestReview()`，避免存储写入失败导致重复弹窗。
- **不支持平台上的空操作**：在 `cx.Env().CanRequestReview()` 为 `false` 的平台（Windows、Linux、Web）上，`MaybeAsk(cx)` 为安全空操作，直接返回 `false` 且不记录已提示状态，游戏可无条件调用。

```go
review := liveops.NewReviewPrompt(cx.Storage(), liveops.ReviewPromptOptions{
	AppVersion: Project.AppVersion,
	Now:        cx.WallTime,
	Suppress:   func() bool { return purchaseOpen },
})

// 通关后在自然停顿处（例如结算卡片出现时）：
review.Moment(liveops.MomentPositive)
review.MaybeAsk(cx)

// 失败或出现受挫体验后：
review.Moment(liveops.MomentNegative)
```

测试可通过 `nsgotest` 无头驱动评分策略：搭配 `nsgotest.Environment(core.Environment{OS: core.OSIOS, CanRequestReview: true})`、`nsgotest.WallTime(...)`、`app.FastForward(...)`、`nsgotest.Storage(...)` 与 `app.ReviewRequests`；`liveops/review_test.go` 覆盖了全部规则。

## 分享卡片与邀请

`Invites` 基于精简的 `ShareHost` 接口发送携带负载的分享卡片与邀请链接，并在其他玩家通过卡片冷启动或热切回游戏时读回邀请负载。

### 负载格式与 HMAC 校验

- **规范编码格式**：负载按字典序编码为 `key=value&…` 查询字符串（`EncodeInvitePayload` / `DecodeInvitePayload`），使用 URL 查询转义。键名限 1–64 字节 ASCII `[A-Za-z0-9._-]`，值须为不含 ASCII 控制字符的合法 UTF-8，单个负载最多 `MaxInvitePayloadKeys`（32）个键，且总字节数不超过宿主上限（默认 `DefaultMaxInvitePayloadBytes` = 1024）。满足校验规则的未知键会被保留。
- **分享前拦截与畸形负载安全拒绝**：`Invites.Share` 在调用 `ShareHost.Share` 之前完成校验与编码；超长（`ErrInvitePayloadTooLarge`）或非法（`ErrInvalidInvitePayload`、`ErrInvalidCard`）卡片会直接拒绝，不会唤起平台分享界面。畸形、重复键、超长或被篡改的入站负载会被安全拒绝并通过 `InvitesOptions.OnError` 报告，绝不交付无效邀请。
- **可选 HMAC-SHA256 签名**：配置 `HMACKey` 后，`EncodeInvitePayload` 对按字典序规范化的无签名串计算 base64url raw HMAC-SHA256 签名并附加到 `InviteSigKey`（`"sig"`）。解码端在常量时间比对（`hmac.Equal`）前会重新规范化键顺序与空格转义（`+` 与 `%20`），确保签名在聊天软件和浏览器改写参数顺序后仍能通过校验。设置 `RequireSignature: true` 可拒绝所有未签名邀请。
- **服务端验签与奖励幂等**：生产环境中建议将邀请奖励签名密钥留在游戏服务器——由服务端调用 `SignInvitePayload` 生成签名（客户端通过 `Card.Signature` 携带），被邀请客户端将收到的 `Invite.RawQuery` 上报给服务端调用 `VerifyInvitePayload` 验签；用 `Invite.ID`（对应 `InviteIDKey` = `"id"`，由 `Card.InviteID` 设置）作为邀请奖励发放的幂等键（[#1643](https://github.com/nsgo-dev/game/issues/1643)），并将 `Invite.Payload` 中的房间/队伍参数交给组队加入流程（[#1688](https://github.com/nsgo-dev/game/issues/1688)）。

### 启动读回、运行期重入与菜单转发

- **冷启动读回（`Opened`）**：`Opened() (Invite, bool)` 返回冷启动携带的邀请并立即消费，后续调用一律返回 `false`。
- **运行期重入（`Consume`、`Incoming`、`OnInvite`）**：游戏运行期间到达的邀请（移动端/桌面端深度链接、Web 地址或片段变更、小游戏 `onShow`）通过 `Consume() (Invite, bool)`（可在 `core.Effect` 中响应式消费）与 `InvitesOptions.OnInvite` / `OnInvite(...)` 监听各投递一次；`Incoming()` 与 `PendingCount()` 响应式查看待消费队列。
- **菜单转发卡片（`SetMenuCard`）**：为带系统转发菜单的宿主（如微信小游戏右上角菜单的“转发”）注册动态 `func() Card` 回调；返回的卡片同样经过长度、合法性与签名检查后再交给宿主，传入 `nil` 可关闭菜单转发。

### 平台宿主与 5:4 截图助手

| 宿主适配器 | 目标平台 | 实现方式 |
| --- | --- | --- |
| `PlatformShareHost`（`InvitesOptions.Host` 为空时的默认宿主） | 移动端（iOS、Android）、桌面端（macOS、Windows、Linux） | 基于 `BaseURL`（如 `mygame://invite`）拼接邀请链接，经 `core.Scope.Share` 分享文案与附加图片字节，并通过 `core.Scope.AppURL` 与 `core.Scope.OnOpenLinks` 接收冷启动和运行期链接。 |
| `PlatformShareHost`（`core.OSWeb`） | Web | 未显式设置 `BaseURL` 时自动从 `cx.AppURL()` 推导（去掉查询串与片段），支持查询串（`InviteEncodeQuery`）或片段（`InviteEncodeFragment`）编码，经 `core.Scope.Share` 分享（不支持 `navigator.share` 时可选 `FallbackToClipboard` 复制到剪贴板），并通过 `cx.AppURL()` 与 `cx.OnAppURLChange` 接收邀请。 |
| `MiniGameShareHost` | 微信等小游戏 | 包装 `MiniGameShareDriver`（`ShareAppMessage`、带 `WithShareTicket` 的 `ShowShareMenu`、`OnShareAppMessage`、`LaunchOptions`、`OnShow`），完整保留 `Invite.Scene` 与 `Invite.ShareTicket`。在 NSGo 落地小游戏原生分享卡片与启动参数 API（[nsgo#2672](https://github.com/nsgo-dev/nsgo/issues/2672)）前，传入 `nil` 驱动会安全退化为不可用宿主（返回 `ErrShareUnavailable`）。 |

使用 `CaptureCardSnapshot(cx, target, opts, done)`（或对任意 `image.Image` 调用 `CropCardSnapshot` / `EncodeCardSnapshotPNG`）可将当前游戏画面居中裁剪并缩放为 5:4 PNG（默认 `DefaultCardSnapshotWidth` = 500、`DefaultCardSnapshotHeight` = 400），直接返回可用于 `Card.Image` 的 `core.ImageSource`。

```go
invites, err := liveops.NewInvites(cx, liveops.InvitesOptions{
	Platform: liveops.PlatformShareHostOptions{
		BaseURL:             "mygame://invite",
		FallbackToClipboard: true,
	},
})
if err != nil {
	return err
}

// 冷启动邀请（消费一次）：
if inv, ok := invites.Opened(); ok {
	handleInvite(inv) // 将 inv.RawQuery 交服务端验签，用 inv.ID 作为发奖幂等键
}

// 运行期重入邀请（响应式，每条消费一次）：
cx.Effect(func() {
	for {
		inv, ok := invites.Consume()
		if !ok {
			break
		}
		handleInvite(inv)
	}
})

// 截取 5:4 画面并分享邀请卡片：
liveops.CaptureCardSnapshot(cx, gameCanvasRef, liveops.CardSnapshotOptions{}, func(img core.ImageSource, err error) {
	invites.Share(liveops.Card{
		Title:    "来和我一起组队！",
		Image:    img,
		InviteID: inviteID,
		Payload:  map[string]string{"room": roomCode},
	}, func(shown bool, err error) { /* 处理分享结果 */ })
})
```

`liveops/invites_test.go` 覆盖了假 `ShareHost` 往返编解码、字节上限、畸形负载拒绝、HMAC 校验、冷启动与重入各一次投递、`MiniGameShareHost`、基于 `nsgotest` 的原生与 Web `PlatformShareHost` 以及 5:4 截图裁剪测试。
