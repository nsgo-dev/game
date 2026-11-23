# 线上运营（Live operations）

```go
import "github.com/nsgo-dev/game/packages/game/liveops"
```

`liveops` 为 NSGo 游戏提供与具体服务商无关的线上运营基础能力。它只依赖 Go 标准库、`packages/game` 和 NSGo 的公开 `core` API，不内置任何第三方崩溃上报、埋点分析、广告或支付 SDK。

## 范围决策（[#194](https://github.com/nsgo-dev/game/issues/194)）

| 能力 | 状态 | 说明 |
| --- | --- | --- |
| **崩溃报告**（[#195](https://github.com/nsgo-dev/game/issues/195)） | 纳入范围 | 与服务商无关的 panic 捕获，附带固定大小步长环形缓冲区、堆栈、元数据和可选状态快照。 |
| **统计分析**（[#198](https://github.com/nsgo-dev/game/issues/198)） | 纳入范围 | 与服务商无关、严格以显式同意为前提的事件 API，同意状态保存在 `core.Storage`。未显式同意前不会发送也不会缓存任何事件。 |
| **云存档**（[#196](https://github.com/nsgo-dev/game/issues/196)） | 纳入范围（接口） | 当前提供与平台无关的 `CloudSync` 接口和 `CloudSyncer` 冲突解决（[#539](https://github.com/nsgo-dev/game/issues/539)）；iCloud、Play Games、Steam Cloud 等内置适配器等待平台云存档绑定就绪。 |
| **应用内购买**（[#369](https://github.com/nsgo-dev/game/issues/369)） | 纳入范围 | 基于 NSGo 购买服务的 `Purchases`：消耗型商品恰好一次入账到存档，解锁项与订阅来自商店权益，支持恢复购买、“询问后购买”和可选的服务器校验。 |
| **广告**（[#199](https://github.com/nsgo-dev/game/issues/199)） | 不纳入核心层 | 广告网络（AdMob、AppLovin 等）依赖专有原生 SDK，并伴随繁重的商店政策与隐私合规义务，不放入核心 `game` 模块；需要广告的游戏可直接通过 NSGo 嵌入原生 SDK 视图。 |

## 崩溃报告

`CrashReporter` 用预分配的环形缓冲区保存最近若干次模拟步长（`DefaultHistorySize` = 120，稳态零堆分配），并为 `game.Loop` 回调或任意函数提供 panic 防护。

- `RecordStep(dt, summary)` 与 `RecordStepData(dt, summary, data)` 向环形缓冲区追加一条 `StepRecord` 并返回单调递增的步数。
- `WrapLoop(l)` 返回带防护的 `*game.Loop`；`GuardLoop(l)` 原地包装 `l.Update` 和 `l.Draw`。如果 `Update` 在单步内未自行调用 `RecordStep`，防护层会自动记录一条 `"update"` 步长（包含触发 panic 的那一步）。
- `RunSafe(fn)` 防护任意回调并报告是否发生 panic。
- 发生 panic 时，报告器构造可 JSON 序列化的 `CrashReport`（包含 `Timestamp`、`Panic`、`Message`、`Stack`、`Step`、按时间从旧到新排列的 `LastSteps`、来自 `CaptureState` 的可选 `State` 以及 `Metadata` 副本），并交给 `Reporter`。
- 需要上报后继续抛出 panic 时，可设置 `Rethrow: true`。

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

`CloudSync` 定义了与服务商无关的存档槽位接口（`Load`、`Save`、`Delete`、`List`，以及 `ErrSlotNotFound` 和 `ErrCloudUnavailable` 哨兵错误）。游戏目前可以针对自有后端或内存测试替身实现该接口；iCloud、Google Play Games 和 Steam Cloud 的平台适配器等待平台云存档绑定就绪（[#196](https://github.com/nsgo-dev/game/issues/196)）。

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
- 每次写云端前，同步器都会重新读取云端副本；如果其间另一台设备已上传，该槽位以 `ErrCloudChanged` 失败，不写入任何内容，下次同步会看到新副本。
- 失败的槽位（损坏的负载、不安全的槽位名、`Ask` 返回错误）会被跳过，错误合并进返回值，其余槽位照常同步；`ErrCloudUnavailable` 或已结束的 context 会中止同步。

每个 `Conflict` 为两端各带一个 `SaveVersion`：是否存在（某一端可能删除了该槽位）、修订号、哈希、修改时间、写入设备、游戏 `Summarize` 给出的摘要以及原始数据。各策略如下：

| 策略 | 行为 |
| --- | --- |
| `PolicyNewest`（默认） | 保留修改时间较晚的副本并覆盖另一份（`OutcomeKeptLocal` 或 `OutcomeKeptCloud`）。任一时间未知（例如未经 `Backend()` 写入的本地副本；记下的时间只对应记录时的那份副本）或两者相等时保留两份。一端删除、另一端修改时保留修改后的副本。 |
| `PolicyKeepBoth` | 槽位中保留云端副本，把本地副本在两端都移到 `<slot>.conflict-<device>`（依次再用 `-2`、`-3`……）（`OutcomeKeptBoth`，`SlotResult.Fork`）。该名称超过 200 字节的槽位名上限时，槽位部分会被截短，并在其后加上 `~` 和槽位名 SHA-256 的 8 位十六进制。在覆盖任何内容之前，先在本地写好分叉副本及其同步状态，因此上传失败时，下次同步会上传同一个分叉，而不是再建一个。删除对修改时保留修改。 |
| `PolicyAsk` | 用冲突调用 `Ask` 并执行其 `Choice`：`ChoiceLocal`、`ChoiceCloud`、`ChoiceBoth`（同 `PolicyKeepBoth`）或 `ChoiceDefer`；`ChoiceDefer` 不写入任何内容，下次同步再次询问。`Ask` 返回错误时两份副本都保持不变。 |

`Ask` 在同步过程中、在应用线程上运行，因此不能等待玩家作答。要通过对话框询问，就保存 `Conflict`、返回 `ChoiceDefer`、展示两份摘要，再用 `Resolve(ctx, conflict, choice)` 执行玩家的选择，如上例所示。`Resolve` 会重新读取两端，只有两端仍持有玩家看到的那两份副本（是否存在、哈希和云端修订号都相同）时才执行选择；否则不写入任何内容并返回 `ErrConflictChanged`。它在任何策略下都可用。

只要某次解决替换了一份副本，`SlotResult.Discarded` 都会返回它，方便游戏展示或归档。被替换的本地副本是有效的存档信封且云端副本存在时，本地后端也会把它保留为备份（`ReadBackup`）；云端副本是删除，或本地副本不是存档信封时，`Discarded` 是仅存的一份。

云端负载是键顺序固定的 JSON 文本：`format`（`CloudSaveFormat`）、`version`（`CloudSaveVersion`）、`revision`、`hash`、`modified`（RFC 3339，UTC）、`device` 和 `data`；`data` 是按原样保存存档的字符串（非 UTF-8 数据改用 `data_base64`）。`DecodeCloudSave` 以 `ErrCorruptCloudSave` 拒绝未知字段、更新的版本、尾随数据以及与数据不符的哈希；把 `EncodeCloudSave` 写出的负载解码后再编码，得到的字节完全相同。没有格式标记的云端负载（由直接使用 `CloudSync` 的游戏写入）按修订号 0 读取，并在下次上传时升级。云端槽位名在交给本地后端之前会先经 `save.ValidateSlot` 检查。

`Backend()` 通过 `Unwrap` 暴露本地后端，因此基于它创建的设置存储（`save.NewDefaultSettingsStore(syncer.Backend())`）会把设置保存在本地后端自己的设置键下，设置永远不会作为槽位同步。

`CloudSyncer` 不支持并发使用；请在应用代码运行的地方调用它，这是 `core.Storage` 的要求。`CloudSync` 没有条件写入，因此另一台设备的上传若恰好落在同步器重新读取与自身写入之间，仍可能被覆盖；这个窗口只隔一次 `Load` 和一次 `Save`。

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
