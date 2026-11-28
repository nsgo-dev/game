# Agent Note: Mini-game friend leaderboards and simulated OS provider gating

Status: implemented

## Problem

`pkg/online/leaderboard` previously knew only Game Center and Google Play Games Services (`ProviderGameCenter`, `ProviderPlayGames`). On WeChat and Douyin mini games, friend rankings cannot be implemented through the self-hosted leaderboard's friend-list query because friend lists and friend scores are isolated inside the host's open data context (`wx.getFriendCloudStorage` / `tt.getFriendCloudStorage`) and never return to the main game scope. Meanwhile, both `leaderboard.ProviderFor` and `liveops.CloudProviderFor` took only `core.OS`; because NSGo's mini-program backend reports the device's OS (such as `core.OSIOS`) with `Env().Simulated() == true`, a mini program running on an iPhone or a web build run with `--nsgo-os ios` reported `ProviderGameCenter` and `CloudProviderICloud` even though neither Apple service is reachable there (#1737).

## Decision

Extend the `version: 1` leaderboard catalog with an optional `miniGame` provider entry, route mini-program environments to `ProviderMiniGame` through a `MiniGameHost` bridge, and switch both `leaderboard.ProviderFor` and `liveops.CloudProviderFor` to inspect `*core.Env`:

- **Catalog schema (`pkg/online/leaderboard/catalog.go`)**:
  - `Board` gains `MiniGame *MiniGameBoard` (`yaml:"miniGame,omitempty"`), holding `Key` (`yaml:"key"`) and optional `Order` (`yaml:"order,omitempty"`, accepting `"desc"`, `"asc"`, or omitted for `OrderDesc`). Keeping `version: 1` preserves backwards compatibility with all existing catalogs while maintaining byte-for-byte deterministic YAML round-trips (`Catalog.Encode`).
  - `Catalog.Validate` requires at least one of `gameCenter`, `playGames`, or `miniGame` per board, enforces unique non-empty UTF-8 `miniGame.key` values without control characters or surrounding whitespace, caps key length at `MaxMiniGameKeyLength` (128 bytes), and caps total `miniGame` keys across the catalog at `MaxMiniGameKeys` (128) to match WeChat's user cloud storage quota.
  - `Catalog.Board`, `New`, and `Service.Catalog` deep-copy `*MiniGameBoard` pointers so caller mutations never leak into `Service`.
- **Provider selection and simulated OS guard (`pkg/online/leaderboard/leaderboard.go`, `pkg/online/liveops/cloud_platform.go`)**:
  - `leaderboard.ProviderFor(env *core.Env)` returns `ProviderMiniGame` when `env.CanSignInMiniProgram()` is true, `ProviderNone` when `env == nil` or `env.Simulated()` is true, and otherwise maps native `OSMacOS`/`OSIOS` to `ProviderGameCenter` and `OSAndroid` to `ProviderPlayGames`.
  - `liveops.CloudProviderFor(env *core.Env)` returns `CloudProviderNone` whenever `env == nil || env.Simulated()`, and `PlatformCloud.Available()` also guards on `!env.Simulated()` (see [Platform cloud saves in `liveops`](2026-11-24-liveops-platform-cloud-saves.md)).
- **Mini-game best-score writes and open data context (`pkg/online/leaderboard/minigame.go`, `minigame_js.go`, `minigame_other.go`)**:
  - `Service.Submit` under `ProviderMiniGame` compares the submitted score against the player's cached best (in memory and persisted in `cx.Storage()` under `leaderboard.minigame.best.<key>`) using `MiniGameBoard.EffectiveOrder()`. Non-improving scores succeed asynchronously without overwriting host cloud storage; improving scores encode WeChat's `{"wxgame":{"score":...,"update_time":...}}` JSON payload (enforcing `MaxMiniGameValueBytes` = 1024 bytes) and update the cached best only after the host write succeeds.
  - `Service.Show(name)` presents the friend ranking over the full window rectangle, `Service.ShowIn(name, bounds)` presents it over an explicit `core.Bounds` rectangle, and `Service.Hide()` removes it. `Service.Scores` (for both `Friends` and `Global`) reports `ErrUnsupported` under `ProviderMiniGame` because friend data never leaves the open data context.
  - `Service.SignIn` under `ProviderMiniGame` calls `cx.SignInMiniProgram` and signs in `Player{Authenticated: true}` with an empty `ID` and `DisplayName`. The login code NSGo returns is a short-lived credential for the game's server to exchange (`core.MiniProgramCredential`), so exposing it as `Player.ID` would hand games a value that changes on every sign-in and leaks a credential into anything that logs or persists player IDs.
  - `MiniGameHost` (`WithMiniGameHost`) lets `nsgotest` drive cloud storage writes, errors, and open data context `Show`/`Hide` headlessly today, while `minigame_js.go` (`//go:build js && wasm`) bridges `nsgoMP.api`, `wx`, and `tt` (`setUserCloudStorage` and `getOpenDataContext().postMessage`, preferring `nsgoMP.showOpenDataContext`/`hideOpenDataContext` when NSGo lands upstream support in `nsgo-dev/nsgo#2672`).

## Alternatives considered

### Bump catalog schema to `version: 2`

Bumping `CurrentVersion` to 2 would make the addition of `miniGame` explicit in the version number. Rejected because `miniGame` is strictly optional with `omitempty`; existing `version: 1` catalogs (including `examples/starburst/leaderboards.yaml`) stay valid and byte-identical on round-trip without migration churn.

### Keep `ProviderFor(os core.OS)` and add a separate `ProviderForEnv(env *core.Env)`

Preserving the old `core.OS` signature would avoid changing callers of `ProviderFor` and `CloudProviderFor`. Rejected because both `pkg/online/leaderboard` and `pkg/online/liveops` are experimental packages, `core.OS` cannot distinguish simulated iOS (mini programs or web `--nsgo-os ios`) from native iOS, and keeping a `core.OS`-only helper invites callers to re-introduce the exact Game Center / iCloud misreporting bug in mini programs.

### Wait for `nsgo-dev/nsgo#2672` before shipping any mini-game host bridge

Deferring `MiniGameHost` and the `js/wasm` bridge until NSGo adds a first-party `miniprogram` package would avoid a `syscall/js` bridge in `game`. Rejected because `pkg/online/wsclient` and `pkg/online/netplay` already bridge `js/wasm` host globals where NSGo lacks a wrapper, and isolating the bridge behind `MiniGameHost` lets `nsgo-dev/game` ship full mini-game friend ranking support and headless `nsgotest` coverage immediately while allowing a drop-in swap to NSGo's native bridge once `nsgo#2672` lands.

## Consequences

- **Benefits**: WeChat and Douyin mini games can declare friend leaderboards alongside Game Center and Play Games in one catalog, enforce WeChat's 128-key / 128-byte / 1 KiB limits at parse and submit time, retain best scores across restarts without overwriting host cloud storage on worse runs, present/hide open data context rankings in any rectangle, and never misreport Game Center or iCloud on simulated iOS/Android hosts.
- **Costs**: Callers of experimental `leaderboard.ProviderFor` and `liveops.CloudProviderFor` pass `*core.Env` instead of `core.OS`. Until `nsgo-dev/nsgo#2672` ships a built-in open data context view and renderer, WeChat/Douyin mini games must pair `Show`/`ShowIn`/`Hide` with an open data context listener handling `nsgo.leaderboard.show` and `nsgo.leaderboard.hide` messages.
