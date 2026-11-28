# accounts

`github.com/nsgo-dev/game/pkg/online/accounts` is a self-hostable player account service. A player starts as a guest, upgrades to an email address or links an OAuth or platform identity (Game Center, Play Games, Steam) without losing their data, and every device gets short-lived tokens a game server verifies offline.

The service is server-side code in its own packages. A game client or game server that does not import them links none of their dependencies (argon2id from `golang.org/x/crypto`, the SQLite and Postgres drivers); a game server that verifies tokens imports only `accounts/authtoken`, which needs the standard library and the YAML package.

| Package | What it holds |
| --- | --- |
| `accounts` | `Service`, `Config`, the `Store` interface, `MemoryStore`, argon2id password hashing and the HTTP `Handler` |
| `accounts/authtoken` | Ed25519 token `Signer` and offline `Verifier`, the `KeySet` YAML file and PEM signing keys |
| `accounts/sqlitestore` | A `Store` in a SQLite file (pure Go, no cgo); every target but the web |
| `accounts/pgstore` | A `Store` in Postgres, shared by several service processes |
| `accounts/sqlstore` | The SQL schema and queries both stores share |
| `accounts/storetest` | The `Store` contract tests, for a store of your own |
| `cmd/accountsd` | A standalone HTTP server for guests and email accounts |

## Accounts, sessions and tokens

An account is a permanent random ID. Key everything the game keeps for a player (saves, progress, purchases) by that ID: linking an email address or an identity never changes it, so a guest who upgrades keeps their data.

Every sign-in returns `Credentials`:

- `Session` is a long-lived secret the device stores. It gets new tokens with `Refresh` and is revoked by `SignOut`. For a guest it is the only way back into the account.
- `Token` is a short-lived signed token the game sends its server. It expires after the configured `tokenTTL`, and it is good only for game servers: see [Token audiences](#token-audiences).
- `Created` reports whether the sign-in created the account.

```go
svc, err := accounts.New(cfg, accounts.Options{
	Store:      store,
	Key:        key, // from authtoken.ParsePrivateKey
	Identities: map[string]accounts.IdentityVerifier{"gamecenter": gameCenter, "steam": steam},
	Logger:     slog.Default(),
})

guest, _ := svc.CreateGuest(ctx)                                  // play at once
_ = svc.LinkEmail(ctx, guest.AccountID, "ada@example.com", pass)  // upgrade, same ID
again, _ := svc.SignInEmail(ctx, "ada@example.com", pass)         // a second device
fresh, _ := svc.Refresh(ctx, again.Session)                       // a new token
saves, _ := svc.RefreshFor(ctx, again.Session, "cloudsave")       // a token for cloud save
```

| Method | What it does |
| --- | --- |
| `CreateGuest` | A new account with neither an email address nor an identity (`ErrGuestsDisabled` with `disableGuests`) |
| `Register` | A new account with an email address and password |
| `LinkEmail` | Gives an account without one an email address and password |
| `SignInEmail` | Signs in with an email address and password |
| `SignInIdentity` | Signs in with a provider credential, creating the account when the identity is new |
| `LinkIdentity` | Links a provider identity to an account |
| `Refresh` / `SignOut` | A new game server token for a session / revokes the session |
| `RefreshFor` | A new token for a session meant for another audience: `ServiceAudience` or one of `audiences` |
| `Profile` | The account, its email address and its identities; `Profile.Guest` reports a guest |

Errors are `ErrNotFound`, `ErrTaken` (the address or identity belongs to another account), `ErrAlreadyLinked`, `ErrInvalidCredentials` (never saying whether the address, the password or the session was wrong), `ErrInvalidEmail`, `ErrWeakPassword`, `ErrUnknownProvider` and `ErrUnknownAudience` (a `RefreshFor` audience the configuration does not offer).

## Identities

An `IdentityVerifier` turns a credential from the device into the provider's stable subject for the player:

```go
steam := accounts.IdentityVerifierFunc(func(ctx context.Context, ticket string) (string, error) {
	return steamWebAPI.AuthenticateUserTicket(ctx, ticket) // the SteamID
})
```

A Game Center verifier checks the signed identity against Apple's certificate, a Play Games verifier exchanges the server auth code, a Steam verifier authenticates the session ticket with the Steam Web API, and an OAuth provider's verifier checks the OpenID Connect ID token. Those need the platforms' credentials, so the game supplies its verifiers under provider names of its choosing (`a-z`, `0-9`, `.`, `_`, `-`, up to 32 characters).

Linking an identity lets a second device sign in to the same account with `SignInIdentity`. An account has at most one identity per provider, and an identity belongs to one account: linking one that another account has returns `ErrTaken`. Two devices signing in with a new identity at the same time get one account between them.

The service never shows a verifier's error to the caller or logs it, since it may quote the credential; the caller gets `ErrInvalidCredentials`.

## Token audiences

A token names the one consumer it is for in its audience, and each consumer accepts only its own:

| Audience | Who accepts it | How a device gets one |
| --- | --- | --- |
| `audience` of the configuration (`my-game`) | the game's servers, with `authtoken.NewVerifier` | every sign-in, `Refresh` |
| `accounts` (`ServiceAudience`) | the service's own `GET /v1/me` and `POST /v1/link/*` | `RefreshFor(session, "accounts")` |
| an entry of `audiences` (`cloudsave`, `leaderboard`) | that service, with `authtoken.NewVerifierFor` | `RefreshFor(session, "cloudsave")` |

A player sends the game server token to every server it joins, and whoever runs that server can replay it until it expires. Because cloud save, the leaderboard and the account service refuse it, a game server learns who the player is and nothing more: it cannot read, overwrite or purge the player's saves, submit scores for them, or link an email address or identity to their account. A device keeps its session to itself and sends each service only a token for that service.

## Verifying tokens on a game server

A token is a compact JWT signed with Ed25519 (`"alg": "EdDSA"`) holding the issuer, the account ID as subject, the audience, the issue and expiry times and a random ID. The game server loads the service's public keys from a key set file and verifies each token without a network call:

```go
data, _ := os.ReadFile("keys.yaml")
ks, err := authtoken.ParseKeySet(data)
v, err := authtoken.NewVerifier(ks)
v.Leeway = 5 * time.Second // clock skew between the two machines

claims, err := v.Verify(tok)
if err != nil { /* refuse the player */ }
player := claims.Subject // the account ID
```

`NewVerifier` accepts the key set's audience, the game server one. A service that acts for the player verifies its own audience instead, with `authtoken.NewVerifierFor(ks, "cloudsave")`. `Verify` checks the signature, the issuer, the audience and the expiry, and its errors wrap `ErrMalformed`, `ErrUnknownKey`, `ErrSignature`, `ErrIssuer`, `ErrAudience`, `ErrExpired` or `ErrNotYetValid`. Tokens longer than `MaxLength` are refused before any work. A revoked session stops getting new tokens, but tokens already issued stay valid until they expire, so keep `tokenTTL` short.

The key set file:

```yaml
version: 1
issuer: https://accounts.example.com
audience: my-game
keys:
  - id: 2317febd373fcbcd
    key: hze+0cYruVRddgjkbHzgCeWWJbEsBkn6K8JNHRDf6Wg=
```

Each `id` is the first 8 bytes of the key's SHA-256 in hex and must match its key. To rotate the signing key, add the new key beside the old one, give game servers the new file, switch the service to the new key, and drop the old key once its last tokens have expired. Give the service the old keys too, as `Options.KeySet` (or `accountsd serve -keyset keys.yaml`): it then accepts bearer tokens signed with any key in the set, and `GET /v1/keys` serves the signing key followed by those keys, so tokens issued before the restart keep working on the service and on game servers until the old key is removed from the file.

## Configuration

The service reads a versioned YAML file:

```yaml
version: 1
issuer: https://accounts.example.com
audience: my-game
audiences:
  - cloudsave
  - leaderboard
tokenTTL: 15m
password:
  minLength: 10
  argon2id:
    time: 2
    memoryKiB: 19456
    threads: 1
allowedOrigins:
  - https://play.example.com
```

| Field | Meaning |
| --- | --- |
| `version` | Schema version, `1` |
| `issuer` / `audience` | Written into every token and checked by verifiers; `audience` names the game's servers and the key set, and may not be `accounts` |
| `audiences` | The other services a device may get tokens for with `RefreshFor`, such as `cloudsave` and `leaderboard`; never the game's audience or `accounts`, which is always available; omitted when empty |
| `tokenTTL` | Token lifetime, whole seconds from `1s` to `24h` |
| `disableGuests` | Refuses `CreateGuest`, so every account starts with an email address or an identity |
| `password.minLength` | Fewest characters in a password, at least 8; passwords are at most 1024 bytes |
| `password.argon2id` | The argon2id cost: passes, memory in KiB and lanes |
| `allowedOrigins` | Web origins whose pages may call the service; omitted when empty |

`ParseConfig` refuses unknown fields, trailing documents, out-of-range values and an origin that is duplicated or has a path, query, fragment, user, wildcard, port outside 1 to 65535 or scheme other than `http` and `https`. `Config.Marshal` writes the canonical form: the fields in the order above, durations in their shortest text (`15m`, not `15m0s`), `disableGuests` only when it is true and `audiences` and `allowedOrigins` only when they have entries. A file in that form, such as one `Marshal` or `accountsd init` wrote, reads back and writes again unchanged; a hand-written file in another order or spelling is normalized. `DefaultConfig` returns the settings shown, without origins. A version 1 file without `audiences` still reads, but its service then issues tokens only for game servers and for itself, so add the audiences of the cloud save and leaderboard services it serves.

## Passwords and secrets

- Passwords are stored only as argon2id hashes in the PHC format (`$argon2id$v=19$m=19456,t=2,p=1$salt$hash`), OWASP's recommended cost by default. A sign-in with a password hashed at an older cost rehashes it at the configured one. `Options.MaxConcurrentHashes` bounds how many hashes run at once, so a burst of sign-ins cannot take all the server's memory.
- An unknown email address takes as long to refuse as a wrong password, so sign-in does not reveal whether an address has an account. `Register` and `LinkEmail` do: they answer `ErrTaken` (`409 taken`) for an address that belongs to an account, so anyone can test an address there without its password. Rate-limit those requests per client.
- Session secrets are 256 random bits; the store keeps only their SHA-256.
- `Credentials.Session` and `Credentials.Token` are `Secret` values, which print, log and marshal as `[redacted]`; `Reveal` returns the value for the one place that sends it. The service logs account events (created, signed in, linked) with account IDs and provider names, never a password, session secret, token or identity credential.
- Signing keys are PKCS #8 PEM files written with mode `0600`; a Postgres DSN is read from the environment, not the command line.

## Storage

`Store` is the interface the service keeps its records in. Implementations enforce uniqueness atomically, so two devices racing to claim one email address or identity cannot both succeed.

```go
store, err := sqlitestore.Open(ctx, "accounts.db")                  // one service process
store, err := pgstore.Open(ctx, os.Getenv("ACCOUNTS_POSTGRES_DSN")) // several processes
```

`sqlitestore` uses the pure-Go `modernc.org/sqlite` driver with write-ahead logging and one connection; `pgstore` uses `github.com/jackc/pgx/v5` and takes an advisory lock while it creates the schema. Both create their tables when missing and refuse a database whose schema is newer than they know. `NewMemoryStore` keeps everything in memory for tests and prototypes.

A store of your own passes `storetest.Run`:

```go
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) accounts.Store { return openEmptyStore(t) })
}
```

## HTTP

`Service.Handler` serves the service with JSON bodies; every response carries `Cache-Control: no-store`:

| Request | Body | Answer |
| --- | --- | --- |
| `POST /v1/guest` | | credentials |
| `POST /v1/register` | `{"email", "password"}` | credentials |
| `POST /v1/signin/email` | `{"email", "password"}` | credentials |
| `POST /v1/signin/identity` | `{"provider", "credential"}` | credentials |
| `POST /v1/refresh` | `{"session", "audience"}`, `audience` optional | credentials |
| `POST /v1/signout` | `{"session"}` | 204 |
| `POST /v1/link/email` | `{"email", "password"}`, with an `accounts` bearer token | 204 |
| `POST /v1/link/identity` | `{"provider", "credential"}`, with an `accounts` bearer token | 204 |
| `GET /v1/me` | `accounts` bearer token | profile |
| `GET /v1/keys` | | the key set as YAML |

Credentials are `{"account", "session", "token", "expires", "created"}`, with status 201 when the account is new. A sign-in's token is for game servers; `refresh` with `audience` returns one for that audience instead. The bearer endpoints accept only a token for `accounts` and answer `invalid_token` to any other. Errors are `{"error": code}`: `invalid_credentials` and `invalid_token` 401, `guests_disabled` 403, `not_found` 404, `taken` and `already_linked` 409, `request_too_large` 413, `bad_request`, `invalid_email`, `weak_password`, `unknown_provider` and `unknown_audience` 400, `unavailable` 503 when the request was cancelled or timed out (for example while waiting for a password hash slot under load), and `internal` 500. Request bodies are limited to 32 KiB and unknown fields are refused. For `allowedOrigins` the handler answers CORS preflight requests and allows `Authorization` and `Content-Type`; other origins receive no CORS headers. The handler does not limit request rates: run it behind TLS and a rate limiter.

## accountsd

`cmd/accountsd` runs the service for guests and email accounts:

```sh
accountsd init   -config accounts.yaml -issuer https://accounts.example.com -audience my-game
accountsd genkey -config accounts.yaml -key key.pem -keyset keys.yaml
accountsd serve  -config accounts.yaml -key key.pem -keyset keys.yaml -sqlite accounts.db -addr :8080
ACCOUNTS_POSTGRES_DSN=postgres://... accountsd serve -config accounts.yaml -key key.pem -keyset keys.yaml -postgres
```

`init` never overwrites a configuration and writes no `allowedOrigins`; if a web build calls the service from another origin, add the game page's origin to `allowedOrigins` in `accounts.yaml` before `serve`. `genkey` never overwrites a key and adds the new public key to the key set file, the first step of a rotation; it replaces the file through a temporary file and a rename, so a game server reading it never sees half of it, and it removes the new key again when the key set cannot be written. `serve -keyset` accepts and serves every key in the key set, so tokens signed with an old key keep working on the service after a rotation. `serve` logs JSON to stderr and stops on SIGINT or SIGTERM. A game that links platform identities builds its own command around `accounts.New` with its verifiers.

`accounts.AppAccountToken(accountID)` derives a stable UUID-shaped App Store account token from an account ID. It is suitable for purchase requests and contains no account secret.
