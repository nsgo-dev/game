# leaderboard/selfhost

```go
import "github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost"
```

`leaderboard/selfhost` is a self-hostable leaderboard service for games that run on several platforms or on platforms without a game service (the web, Windows, Linux). Where [`leaderboard`](leaderboard.md) shows Game Center's and Play Games' own boards, this service keeps one board for every platform, keyed by the account the [account service](accounts.md) signs players in to, so a score from a browser and a score from a phone land on the same board.

It is server-side code. It needs only the standard library, the YAML package and `accounts/token`, so it links none of the account service's password hashing or database drivers.

| Package | What it holds |
| --- | --- |
| `leaderboard/selfhost` | `Service`, `Config`, the `Store` interface with `MemoryStore` and `FileStore`, `ProofSigner` and the HTTP `Handler` |
| `cmd/leaderboardd` | A standalone HTTP server with its scores in a directory |

## Declaring boards

The service reads its boards from a versioned YAML file:

```yaml
version: 1
audience: leaderboard
proofTTL: 10m
boards:
  - name: high-score
    order: descending
    reset: allTime
  - name: weekly-sprint
    order: ascending
    reset: weekly
    weekStart: monday
    keep: 4
    validation: signed
    min: 1
    max: 3600000
serverKeys:
  - id: 2317febd373fcbcd
    key: hze+0cYruVRddgjkbHzgCeWWJbEsBkn6K8JNHRDf6Wg=
allowedOrigins:
  - https://play.example.com
```

| Field | Meaning |
| --- | --- |
| `version` | Schema version, `1` |
| `audience` | The token audience the handler accepts, `leaderboard` when absent; the account service must list it in its `audiences`, and it must differ from the key set's audience |
| `proofTTL` | How old a game server's score proof may be, from `1s` to `24h`; `10m` when absent |
| `boards` | The boards, in the order the service lists them |
| `name` | The board's key in URLs and in the data directory: `a-z`, `0-9`, `.`, `_` and `-`, up to 64 characters, starting with a letter or digit |
| `order` | `descending` ranks the highest score first (points), `ascending` the lowest (times) |
| `reset` | `allTime`, `daily` (midnight UTC) or `weekly` (midnight UTC on `weekStart`) |
| `weekStart` | The day a weekly board resets on, a lowercase English weekday; `monday` when absent |
| `keep` | How many earlier periods of a daily or weekly board stay readable after a reset, 1 to 1000; 1 when absent |
| `validation` | `signed` accepts a score only with a game server's proof; absent trusts the player's client |
| `min`, `max` | The scores the board accepts; at most 2^53-1 either way, so every score survives a JSON number in JavaScript |
| `serverKeys` | The Ed25519 public keys game servers sign proofs with; a `signed` board needs at least one |
| `allowedOrigins` | Web origins whose pages may call the service; omitted when empty |

`ParseConfig` refuses unknown fields, duplicate keys, other versions, a second YAML document and an origin that is duplicated or has a path, query, fragment, user, wildcard, port outside 1 to 65535 or scheme other than `http` and `https`. Optional fields keep what the file says, and `Config.Marshal` writes the fields in the order above, omitting `allowedOrigins` when empty, so a file in that form parses and marshals back byte for byte.

## Ranking

A board ranks each account's best score in its current period. A score only counts when it beats the account's best, by the board's order; an equal score keeps the earlier one. Equal scores rank by who reached them first, then by account ID, so every entry has its own rank.

Periods are in UTC. A daily or weekly board starts a new, empty period at its reset, and the period before stays readable, with as many earlier ones as `keep` says. Each period has an ID: `all` for an all-time board, otherwise the UTC date it starts on, such as `2026-11-23`. A query names its period as `current` (the default), `previous`, or an earlier period's ID.

```go
svc, err := selfhost.New(ctx, cfg, selfhost.Options{
	Store:  store,      // selfhost.OpenFileStore("scores", selfhost.FileStoreOptions{})
	Tokens: verifier,   // token.NewVerifierFor(keySet, cfg.TokenAudience()), not token.NewVerifier
	Logger: slog.Default(),
})

res, err := svc.Submit(ctx, selfhost.Submission{Board: "high-score", Account: claims.Subject, Score: 4200})
// res.Entry.Rank, res.Improved, res.Period.ID

page, err := svc.Top(ctx, "high-score", "current", 0, 25)               // ranks 1-25
near, err := svc.Around(ctx, "weekly-sprint", "previous", account, 5)   // 5 above and 5 below
pals, err := svc.Friends(ctx, "high-score", "", friendIDs)              // these accounts, ranked
```

| Method | What it does |
| --- | --- |
| `Submit` | Records a score in the board's current period; `Result.Improved` reports whether it became the account's best |
| `Top` | Up to `limit` entries (at most 100) from rank `offset+1` |
| `Around` | The account's entry with up to `radius` entries (at most 50) each side, shifted at either end so the window keeps its size |
| `Friends` | The entries of up to 200 accounts the game supplies (a platform friend list, its own social graph), each with its rank on the whole board |
| `Rank` | One account's entry |
| `Boards` | Every board with its current period and the earlier periods still readable |

Errors are `ErrUnknownBoard`, `ErrUnknownPeriod` (no such period, or no longer kept), `ErrNotRanked`, `ErrInvalidAccount`, `ErrScoreOutOfRange`, `ErrProofRequired`, `ErrInvalidProof`, `ErrProofExpired` and `ErrInvalidQuery`.

Each period is an order-statistic skip list in memory, so every query costs O(log n) plus the entries it returns. On a board of a million accounts the benchmarks take about 7 µs for the top 100, 60 µs for 100 entries deep in the board, 40 µs around an account and 10 µs to submit (`go tool nsgo test ./pkg/online/leaderboard/selfhost -run '^$' -bench .`); a benchmark fails when a query takes more than 50 ms, and CI runs the query benchmarks on every pull request.

## Server-validated scores

A client can send any score it likes. A board with `validation: signed` accepts a score only with a proof from the game server: after the server decides a match or checks a replay, it signs the board, the account and the score, and hands the proof to that player's client, which submits it beside the score.

```go
key, err := token.ParsePrivateKey(pemData) // from leaderboardd genkey
prover, err := selfhost.NewProofSigner(key)
proof, err := prover.Sign("weekly-sprint", claims.Subject, result.TimeMillis)
// send proof to the player with the match result
```

A proof is `v1.<key id>.<unix seconds>.<signature>`, an Ed25519 signature over the board, account, score and time. The service refuses a missing proof (`ErrProofRequired`), one signed by a key not in `serverKeys`, for another board, account or score, or issued in the future (`ErrInvalidProof`), and one older than `proofTTL` or, on a daily or weekly board, issued before the current period began (`ErrProofExpired`), so a score signed before a reset does not count after it. `Options.Leeway` allows for clock skew between the machines. Because a proof names the account, a player cannot submit someone else's.

## HTTP

`Service.Handler` serves the service as JSON. Every request except `GET /v1/openapi.yaml` carries the player's account token as `Authorization: Bearer <token>`; the handler verifies it offline with `Options.Tokens` and accepts only a token for the configuration's audience (see [Token audiences](accounts.md#token-audiences)), so the token a player sends a game server cannot submit scores for them; `New` refuses a verifier for another audience or for the game servers' audience of its key set, and the configuration cannot name `accounts`, the account service's own audience. The token's subject is the account a score is submitted for, so a player can only submit their own scores. The full machine-readable contract is published as an [OpenAPI 3.1 description](https://game.nsgo.dev/schemas/leaderboard-selfhost-openapi.v1.yaml) and served live at `GET /v1/openapi.yaml`.

| Request | Response |
| --- | --- |
| `GET /v1/openapi.yaml` | the OpenAPI 3.1 description in YAML (unauthenticated) |
| `GET /v1/boards` | `{"boards": [{"name", "order", "reset", "validation", "current", "previous"}]}` |
| `POST /v1/boards/{board}/scores` `{"score", "proof"}` | `{"period", "entry", "total", "improved"}` |
| `GET /v1/boards/{board}/top?period=&offset=&limit=` | a page; `limit` 25 when absent |
| `GET /v1/boards/{board}/around?period=&radius=` | a page around the token's account; `radius` 5 when absent |
| `POST /v1/boards/{board}/friends?period=` `{"accounts": [id]}` | a page of those accounts |

A page is `{"board", "period", "total", "entries"}`, a period `{"id", "start", "end"}` (no `start` or `end` for an all-time board) and an entry `{"rank", "account", "score", "achieved"}`. Errors are `{"error": code}`: `invalid_token` 401; `proof_required`, `invalid_proof` and `proof_expired` 403; `unknown_board`, `unknown_period` and `not_ranked` 404; `bad_request`, `invalid_account` and `score_out_of_range` 400; `request_too_large` 413; `unavailable` 503; `internal` 500.

Entries carry account IDs; a game that shows names looks them up in its own profiles. For `allowedOrigins` the handler answers CORS preflight requests and allows `Authorization` and `Content-Type`; other origins receive no CORS headers. Put the handler behind TLS and a rate limiter.

## Storage

The service keeps its records in a `Store`. `NewMemoryStore` is for tests. `OpenFileStore` keeps one JSON Lines file per board period at `<dir>/<board>/<period>.jsonl`, a line per new best score:

```json
{"account":"3f9a...","score":4200,"achieved":"2026-11-24T09:15:02.5Z"}
```

`New` loads the files. It cuts off a last line a crash left half written, rewrites a file whose lines are mostly superseded scores with one line per account, and deletes the periods that are older than the ones a board keeps; a reset deletes them as it happens. A failed write is taken back, so the file never holds a score the client was told was lost. `New` deletes only what it can place: scores of a board removed from the configuration, and of periods stored under a board's earlier `reset` or `weekStart`, stay on disk, are not served, and are logged as a warning. A period later than the current one, as a machine whose clock is behind at boot sees it, is kept and served once the clock reaches it. `FileStoreOptions.Sync` flushes every write to the disk, for a machine that may lose power. A `Store` of your own implements `Load`, `Save` and `Drop`.

## leaderboardd

```sh
leaderboardd init   -config leaderboards.yaml
leaderboardd genkey -key server.pem
leaderboardd serve  -config leaderboards.yaml -keyset keys.yaml -data scores -addr :8081
```

`init` writes a configuration with one all-time `high-score` board and no `allowedOrigins` to edit; if a web build calls the service from another origin, add the game page's origin to `allowedOrigins` in `leaderboards.yaml` before `serve`. `genkey` writes a game server signing key and prints the `serverKeys` entry to paste into the configuration. `serve` verifies player tokens with the account service's key set file (`accountsd genkey` writes it), accepts only those for the configuration's `audience`, refuses to start when that is the key set's audience, which names the game's servers, keeps scores under `-data`, and stops on SIGINT or SIGTERM. `-sync` flushes every score to the disk before acknowledging it, for a machine that may lose power.
