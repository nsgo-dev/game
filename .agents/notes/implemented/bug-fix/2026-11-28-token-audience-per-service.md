# Agent Note: One token audience per consumer of account tokens

Status: implemented

## Problem

The account service issued one kind of bearer token, and every consumer accepted it with the same key set, issuer and audience: dedicated game servers (to learn who a player is), the cloud save service (to pick the account whose slots a request reads, writes and purges), the self-hosted leaderboard (to pick the account a score is for) and the account service's own `GET /v1/me` and `POST /v1/link/*`. A player sends that token to every game server it joins, so whoever ran a server held, for the token's lifetime, a credential the other services accepted as the player. `authtoken.Verifier` checked the audience, but there was only one.

## Decision

Every consumer verifies its own audience, and the account service issues a token for a named audience only on request with the session, which only the device holds.

### Token contract (`accounts/authtoken`)

- `Signer.IssueFor(subject, audience)` issues for an audience other than the signer's own; `Issue` keeps issuing for the key set's audience.
- `NewVerifierFor(ks, audience)` builds a verifier for the key set's keys and issuer that accepts only `audience`; `NewVerifier(ks)` is `NewVerifierFor(ks, ks.Audience)`. `Verifier.Audience` reports it.
- The key set file format is unchanged: its `audience` is the game servers' one.

### Account service (`accounts`)

- `Config.Audiences` (`audiences:` in `accounts.yaml`, omitted when empty) lists the other audiences a device may request. An entry may not be empty, repeated, the game's `audience`, or `accounts`. `Config.Audience` may not be `accounts`. `DefaultConfig` lists `cloudsave` and `leaderboard`.
- `ServiceAudience` (`"accounts"`) is the audience of the service's own bearer endpoints and is always available. `GET /v1/me`, `POST /v1/link/email` and `POST /v1/link/identity` accept only it; `Authenticate` keeps verifying the game servers' audience.
- `RefreshFor(ctx, session, audience)` and `POST /v1/refresh {"session", "audience"}` issue for `Config.Audience` (or `""`), `ServiceAudience` or an entry of `Config.Audiences`; any other is `ErrUnknownAudience` (`400 unknown_audience`). Sign-ins and `Refresh` keep returning a game server token.

### Services that act for the player

- `cloudsave.Config.Audience` and `selfhost.Config.Audience` (`audience:`, omitted when empty) name the audience the service accepts; `TokenAudience` resolves empty to `DefaultAudience` (`cloudsave` / `leaderboard`). `cloudsaved init` and `leaderboardd init` write it explicitly.
- `cloudsave.New` and `selfhost.New` refuse an `Options.Tokens` whose `Audience` is not `TokenAudience`, so a verifier built with `NewVerifier` from the key set fails at construction rather than accepting game server tokens.
- `cloudsaved serve` and `leaderboardd serve` build the verifier with `NewVerifierFor(ks, cfg.TokenAudience())` and refuse to start when that audience equals the key set's.

## Alternatives considered

1. **A scope claim on one audience**: add `scope` to the JWT and have each service check it. It keeps one verifier per key set. Rejected because `aud` is the registered claim for exactly this, every consumer already checks it, and a new claim would need its own refusal rules while an old verifier would ignore it and keep accepting any token.
2. **Require the session in the body of destructive cloud save requests and of the link endpoints**: the session never leaves the device, so this also stops a replayed token. Rejected as the main fix because it leaks the long-lived session to every service that asks for it, and it leaves reads and score submissions open; per-audience tokens close every route with one rule.
3. **Derive service audiences from the game audience (`my-game/cloudsave`)**: no configuration needed. Rejected because the services do not know the game audience until they read the key set, a fixed default per service is simpler to document, and the accounts side still has to decide which audiences it hands out.
4. **Make `audience` required in the cloud save and leaderboard configurations**: explicit in every file. Rejected because existing version 1 files would stop parsing; an empty field defaulting to a name that differs from any game audience is backward compatible at parse time and still refuses game server tokens.

## Consequences

- **Benefits**: a game server, or anyone holding a token sent to one, cannot read, overwrite or purge the player's saves, submit or read scores as the player, or read or change the player's account. Every consumer refuses tokens of any other audience with the same `ErrAudience` path.
- **Costs**: clients must request a token per service: `POST /v1/refresh` with `"audience": "cloudsave"` for cloud save, `"leaderboard"` for the leaderboard, and `"accounts"` before `GET /v1/me` and the link endpoints. An existing `accounts.yaml` without `audiences` must add the audiences of the services it serves. Library callers of `cloudsave.New` and `selfhost.New` must build their verifier with `NewVerifierFor`.
