# Releasing in Mainland China

Prepare a game built with NSGo Game for players in mainland China: plan regulatory registrations in order, implement first-launch privacy consent and required disclosures, handle Android devices without Google Play Services, and track HarmonyOS and mini program preview status.

> This page summarizes engineering checkpoints, store mechanics, and links to official portals. It is not legal advice; rules and store review checklists change frequently, so confirm current requirements with the official portals below, your publisher, or qualified counsel before submitting.

## Order of steps and lead times

Publishing in mainland China is gated by regulatory lead times rather than build time. The National Press and Publication Administration (NPPA) approval queue usually sets the release date, while software copyright and app filing must line up before store submission.

| Step | Purpose | Asked for by | Typical lead time |
| --- | --- | --- | --- |
| 1. Legal entity & ICP qualifications | Register a mainland China corporate entity (business license); commercial online game operation also involves an ICP license (value-added telecom permit) or a licensed operator/publisher. | Filing providers, publishers, store accounts | 2–8 weeks |
| 2. Software copyright registration (软著) | Register the game's software copyright with the Copyright Protection Center of China (CPCC). | Major Android OEM stores, TapTap, NPPA approval | 1–2 months |
| 3. App filing (APP 备案) | File the mobile app with the Ministry of Industry and Information Technology (MIIT) through your mainland hosting/cloud provider (or mini-program host platform), binding your bundle ID and signing certificate fingerprint. | Apple App Store (mainland China), Huawei AppGallery, Android OEM stores, TapTap, HarmonyOS, mini programs | 1–3 weeks |
| 4. Publishing approval & ISBN (版号) | Obtain NPPA publishing approval and an ISBN through a licensed publishing unit before paid downloads, in-app purchases, or commercial/monetized operation (separate domestic and imported queues). | Apple App Store (monetized games), Huawei AppGallery, Android OEM stores, TapTap, monetized mini-games | Several months to 1+ year |
| 5. Store accounts & channel builds | Verify developer accounts, complete privacy/SDK disclosures, and upload store-specific builds. | Each store separately | 1–2 weeks per store |

Lock your reverse-DNS bundle identifier (`id` in `game.yaml` and `nsgo.yaml`) and your Android / HarmonyOS release signing keystore before starting APP filing: the filing records both the package ID and the signing certificate's public-key / MD5 fingerprint, and stores verify that uploaded packages match the filed record.

## Required in-game screens and timing

Store reviewers check both static packages and runtime network traffic from the moment the app starts.

### First-launch privacy consent dialog

Mainland China app stores reject builds that collect personal information, read device identifiers, transmit telemetry, or initialize third-party SDKs before the player explicitly accepts the privacy policy on first launch.

1. Show a privacy consent dialog before entering gameplay, opening analytics/crash uploads, or initializing any third-party SDK.
2. Include a clear summary of data collection and permissions, a clickable link to the full privacy policy, and explicit **Agree** and **Disagree** buttons. Choosing **Disagree** must exit cleanly or remain in a non-collecting offline/browse mode without reading device identifiers or sending telemetry.
3. Persist the player's choice in `core.Storage` so subsequent launches skip the prompt (while keeping a way in settings to view the policy or withdraw consent).
4. Keep `android.permissions` in `nsgo.yaml` minimal—declare only what the game uses, and avoid unnecessary device-identifier permissions—and maintain the third-party SDK disclosure table (SDK name, provider, purpose, collected fields, privacy policy URL) requested during store submission.

`liveops.Analytics` already enforces opt-in consent: it starts in `liveops.ConsentUnknown` and drops every `Track` call (zero events sent or buffered for retroactive replay) until `SetConsent(liveops.ConsentGranted)` is called (see [Live operations](liveops.md#analytics)). Gate `liveops.CrashReporter` uploads and any network/SDK initialization behind the same persisted consent flag:

```go
store := cx.Storage()
analytics := liveops.NewAnalytics(store, "privacy.analytics", myAnalyticsProvider)
analytics.SetClock(cx.Now)

// Before the player taps Agree, analytics.Track drops events without buffering.
// Gate crash report uploads and any third-party SDK init on the same consent state:
reporter := liveops.NewCrashReporter(liveops.CrashOptions{
	Reporter: func(r liveops.CrashReport) {
		if analytics.Consent() == liveops.ConsentGranted {
			uploadCrashReport(r)
		}
	},
	Now: cx.Now,
})

// Called from the first-launch privacy dialog's Agree button:
onAgreePrivacy := func() {
	_ = analytics.SetConsent(liveops.ConsentGranted)
	initPostConsentServices()
}

// Called from the Disagree button:
onDisagreePrivacy := func() {
	_ = analytics.SetConsent(liveops.ConsentDenied)
	// Exit cleanly or stay in offline mode with zero data collection.
}
```

### Health advisory, age rating, and publishing disclosures

- **Health game advisory (健康游戏忠告)**: display the standard NPPA advisory text on the splash/login screen (*抵制不良游戏，拒绝盗版游戏。注意自我保护，谨防受骗上当。适度游戏益脑，沉迷游戏伤身。合理安排时间，享受健康生活。*).
- **Age-appropriateness notice (适龄提示)**: display the CADPA age badge (such as 8+, 12+, or 16+) on the start screen and store listing pages.
- **Publishing & filing information**: display your NPPA approval number (文号), ISBN, publisher (出版单位), copyright holder (著作权人), and APP/ICP filing number (备案号) on the start/login/about screen and enter the matching fields in every store console.

### Real-name verification and minors anti-addiction

Commercial online games published in mainland China must verify every player against the NPPA real-name verification system (网络游戏防沉迷实名认证系统), enforce minor play-time windows (under 18: 20:00–21:00 Beijing time on Fridays, Saturdays, Sundays, and statutory holidays), enforce age-tiered spending caps (under 8: no payments; 8–15: max 50 CNY per payment and 200 CNY per month; 16–17: max 100 CNY per payment and 400 CNY per month), and report login/logout behavior from the server rather than trusting the client clock.

> **Current status (planned)**: NSGo Game does not yet ship built-in NPPA real-name verification, anti-addiction play-time/spending enforcement, `Purchases` pre-check hooks, or a template advisory splash screen. Runtime compliance support is tracked in [#1649](https://github.com/nsgo-dev/game/issues/1649). Until it lands, implement server-side verification and enforcement yourself or integrate your publisher's / channel's compliance SDK.

## Online packages without Google Play Services

Android devices sold in mainland China ship without Google Play Services (GMS), Google Play Games Services, Google Play Billing, or Google Play Store, and Firebase Cloud Messaging (FCM) is unreachable. The table below lists every `pkg/online` package in NSGo Game and how it behaves on Google-free Android devices.

| Package | Without Google services | Behavior on Google-free Android & fallback |
| --- | --- | --- |
| `pkg/online/accounts` | Works | Self-hosted HTTPS account service (`cmd/accountsd`, plus `accounts/token`, `sqlitestore`, `pgstore`, `sqlstore`, `storetest`). Works with guest accounts, email/password, or custom `IdentityVerifier` backends (WeChat, Huawei, TapTap, Apple, mini programs) instead of Play Games. NPPA real-name extensions are planned in [#1649](https://github.com/nsgo-dev/game/issues/1649). |
| `pkg/online/achievements` | Partial | Local `Tracker` and `Persist` (`core.Storage`) work everywhere without Google services. `ServiceFor(core.OSAndroid)` selects `PlayGames`, so `SignIn` fails with `gameservices.ErrUnsupported` on Google-free Android (and on NSGo `v0.3.4` Android overall; see [Platform support](platforms.md)). Render your own in-game UI or attach a custom `Reporter` through `Tracker.Connect`. |
| `pkg/online/audit` | Works | Pure server-side append-only JSON Lines audit log (`pkg/online/audit`); no client or Google dependency. |
| `pkg/online/cloudsave` | Works | Self-hosted HTTPS cloud save service (`cmd/cloudsaved`, plus `sqlstore`, `storetest`), authenticated by `accounts/token`. Recommended fallback for Android without Google Play Services and HarmonyOS. |
| `pkg/online/leaderboard` | Unavailable | `ProviderFor(core.OSAndroid)` selects `ProviderPlayGames`; without Google Play Services `Available()` is `false` and `SignIn`/`Submit`/`Show`/`Scores` return `ErrUnsupported` (Game Center still works on iOS/macOS). Use [`pkg/online/leaderboard/selfhost`](leaderboard-selfhost.md) instead. |
| `pkg/online/leaderboard/selfhost` | Works | Self-hosted cross-platform leaderboards over HTTPS (`cmd/leaderboardd`), authenticated by `accounts/token`. Recommended fallback on Google-free Android, HarmonyOS, Windows, Linux, and the web. |
| `pkg/online/liveops` | Partial | `CrashReporter` (and `liveops/crashreport`), consent-gated `Analytics`, `CloudClient` (backed by `cloudsave`), `DirCloud`, and `Reminders` (OS-native local notifications) all work without Google services. `PlatformCloud` picks `CloudProviderPlayGames` on Android and fails with `ErrCloudUnavailable`—use `CloudClient` instead. `Purchases` is unavailable on Android (`core.PurchasesUnavailable`; [nsgo#1369](https://github.com/nsgo-dev/nsgo/issues/1369), [nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370); Apple StoreKit works on iOS mainland China). `ReviewPrompt` is a safe no-op where `CanRequestReview()` is `false`. Remote push is unavailable everywhere ([nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59), [nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)). |
| `pkg/online/netplay` | Works | Pure-Go UDP, WebSocket, WebTransport, and loopback transports (plus `netplay/wire`); no Google dependency. Host mainland servers on ICP-filed domains and mainland infrastructure for reliable latency. |
| `pkg/online/presence` | Unavailable | Desktop-only rich presence (Steam `ISteamFriends` and Discord local IPC); returns `ErrUnavailable` on Android, iOS, and the web. No Google dependency (Steam works on mainland desktop PCs; Discord is generally unreachable). |
| `pkg/online/server` | Works | Server-side headless fixed-step match runner, rooms, matchmaking, health, and metrics; no Google dependency. |
| `pkg/online/wsclient` | Works | Pure-Go RFC 6455 WebSocket client on native targets and browser `js/wasm`; no Google dependency (unavailable in mini programs until [nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)). |

### Self-hosted fallback stack

For cross-platform saves, accounts, and leaderboards that work identically across iOS, Google-free Android, desktop, and web:

1. Deploy [`pkg/online/accounts`](accounts.md) (`cmd/accountsd` or a custom server with your chosen `IdentityVerifier` providers) to issue short-lived Ed25519 tokens.
2. Deploy [`pkg/online/cloudsave`](cloudsave.md) (`cmd/cloudsaved`) and connect clients with `liveops.CloudClient` + `liveops.CloudSyncer` (`SyncAsync`) instead of `liveops.PlatformCloud`.
3. Deploy [`pkg/online/leaderboard/selfhost`](leaderboard-selfhost.md) (`cmd/leaderboardd`) instead of `pkg/online/leaderboard`.
4. Keep achievements locally in `achievements.Tracker` (with `Persist(cx.Storage(), ...)`), optionally syncing via a custom `achievements.Reporter`.
5. Serve every endpoint over HTTPS on an ICP-filed mainland China domain (required for reliable connectivity and store compliance).

## Store-by-store notes

### Apple App Store (mainland China)

- Enter your MIIT APP filing number in App Store Connect before enabling mainland China availability.
- Paid games and games with in-app purchases require an NPPA publishing approval number in App Store Connect.
- Apple's native services work without Google dependencies: StoreKit in-app purchases (`liveops.Purchases` with `apple.purchases: true`), Game Center (`achievements`, `leaderboard`), iCloud (`liveops.PlatformCloud`), and StoreKit review prompts (`liveops.ReviewPrompt`).

### Android OEM stores and TapTap

Huawei AppGallery (Android), Xiaomi GetApps, OPPO, vivo, Honor, and TapTap each maintain a separate developer console, review queue, and privacy compliance scanner.

- **Signed release `.apk`, not `.aab`**: mainland Android stores distribute signed 64-bit (`arm64-v8a`) `.apk` files rather than Google Play App Bundles. Build with `-channel direct` (never `-channel store`, which outputs `.aab`) and your release keystore:
  ```sh
  NSGO_KEYSTORE_PASSWORD="$STORE_PW" NSGO_KEY_PASSWORD="$KEY_PW" \
    go tool game release -channel direct -target android
  ```
  Or invoke `nsgo build` directly with `GOFLAGS=-tags=release`:
  ```sh
  NSGO_KEYSTORE_PASSWORD="$STORE_PW" NSGO_KEY_PASSWORD="$KEY_PW" \
    GOFLAGS=-tags=release go tool nsgo build -channel direct -target android_arm64 \
    -keystore certs/upload.p12 -key-alias upload
  ```
- **Channel SDKs and payments**: OEM channel distribution (联运) typically requires integrating each store's channel SDK (account login, channel billing, anti-addiction, update/exit dialogs) and shipping separate per-channel packages. NSGo does not bundle proprietary OEM SDKs, and Android billing is not yet implemented in NSGo ([nsgo#1369](https://github.com/nsgo-dev/nsgo/issues/1369), [nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370)). TapTap is commonly used for official-server (官服) APK distribution and testing.
- **Test on a Google-free device**: verify before submission that signing in, saving, viewing leaderboards/achievements, and checking purchases degrade cleanly without hanging or crashing when Google Play Services is absent.
- **Target SDK and permissions**: keep `android.target_sdk` aligned with each store's current requirement and declare only minimal `android.permissions` in `nsgo.yaml`.

### HarmonyOS (AppGallery on HarmonyOS NEXT)

HarmonyOS NEXT runs native HarmonyOS packages (`.hap` / `.app`) exclusively—Android APKs do not run on HarmonyOS NEXT.

- **Building and signing (preview)**: NSGo includes a preview `harmonyos` target (WebAssembly running in ArkWeb with an ArkTS/ArkUI host) that writes `<name>-<version>-harmonyos.hap`:
  ```sh
  NSGO_HARMONY_KEYSTORE_PASSWORD="$STORE_PW" NSGO_HARMONY_KEY_PASSWORD="$KEY_PW" \
    GOFLAGS=-tags=release go tool nsgo build -target harmonyos
  ```
  Signed release HAPs require the OpenHarmony SDK (`OHOS_BASE_SDK_HOME`, API 26), `hvigorw`, Java (`hap-sign-tool.jar`), and AppGallery Connect signing materials in `nsgo.yaml` (see [NSGo's Building guide — HarmonyOS](https://nsgo.dev/docs/building/#harmonyos)):
  ```yaml
  harmonyos:
    signing:
      keystore: signing/app.p12
      key_alias: app
      certificate: signing/app.cer
      profile: signing/app.p7b
  ```
  Run `go tool nsgo doctor` in your project directory to verify the signing config and profile bundle name.
- **Preview limitations in NSGo `v0.3.4`**: `harmonyos` is an experimental preview target and is not yet ready for production game releases. As documented in [Platform support](platforms.md), it currently lacks 3D GPU views, audio (`CanPlayAudio()` is `false`), gamepads, haptics, motion sensors, persistent `core.Storage` (currently in-memory only), native local notifications, UDP, game services, and HarmonyOS IAP Kit ([nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521), [nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320), [nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370)).
- **AppGallery submission**: HarmonyOS NEXT apps require HarmonyOS APP filing (bound to the HarmonyOS bundle name and signing public key), software copyright, privacy disclosure, and NPPA publishing approval for monetized games.

### Mini programs (WeChat, QQ, Douyin, Alipay)

NSGo provides experimental targets `miniprogram_wechat`, `miniprogram_qq`, `miniprogram_alipay`, and `miniprogram_douyin`, writing a project for each platform's DevTools under `dist/releases/miniprogram/<platform>` with AppIDs configured under `miniprogram.appid` in `nsgo.yaml`:

```sh
GOFLAGS=-tags=release go tool nsgo build -target miniprogram_wechat
```

- **Filing and review**: mini programs complete mini-program filing (小程序备案) through their host platform console. Commercial/monetized mini-games also require software copyright and NPPA approval (check each host platform's current policies for non-IAP or ad-supported mini-games).
- **Experimental limitations in NSGo `v0.3.4`**: as documented in [Platform support](platforms.md), mini programs enforce a 2 MB compressed package limit and currently lack 3D GPU views, audio, shaders (`nsgo_noshader`), gamepads, and standard browser `fetch`/`WebSocket` globals ([nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)). Local saves persist via `save.StorageBackend(cx.Storage(), ...)`, and NSGo exposes `core.SignInMiniProgram` for host sign-in.

## Official resources

- [National Press and Publication Administration (NPPA / 国家新闻出版署)](https://www.nppa.gov.cn/) — game publishing approval (版号) notices and lists
- [NPPA Online Game Anti-Addiction Real-Name System (网络游戏防沉迷实名认证系统)](https://wlc.nppa.gov.cn/) — real-name verification and play-behavior reporting portal
- [MIIT ICP / APP Filing Portal (工业和信息化部政务服务平台)](https://beian.miit.gov.cn/) — ICP and mobile APP filing queries and rules
- [Copyright Protection Center of China (中国版权保护中心)](https://www.ccopyright.com.cn/) — software copyright registration (软著)
- [China Audio-video and Digital Publishing Association (中国音像与数字出版协会 / CADPA)](https://www.cadpa.org.cn/) — age-appropriateness notice standards (适龄提示)
- [Apple App Store Connect China Support](https://developer.apple.com/cn/support/app-store-connect/) — mainland China APP filing and ISBN submission rules
- [Huawei Developer & AppGallery Connect](https://developer.huawei.com/consumer/cn/doc/) — Android AppGallery and HarmonyOS NEXT signing & release guides
- Android OEM & distribution consoles: [Xiaomi Developer](https://dev.mi.com/), [OPPO Open Platform](https://open.oppomobile.com/), [vivo Open Platform](https://dev.vivo.com.cn/), [Honor Developer](https://developer.honor.com/), [TapTap Developer](https://developer.taptap.cn/)
- Mini program developer docs: [WeChat](https://developers.weixin.qq.com/miniprogram/dev/framework/), [QQ](https://q.qq.com/wiki/), [Douyin](https://developer.open-douyin.com/), [Alipay](https://opendocs.alipay.com/mini)
- [NSGo Building guide](https://nsgo.dev/docs/building/) — HarmonyOS, mini programs, and Android keystore configuration details
