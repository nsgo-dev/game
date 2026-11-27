// Package selfhost is a self-hostable leaderboard service, for cross-platform games and games on platforms without a game service: one board ranks the scores a player's devices submit from every platform, keyed by the account the accounts package signs in.
//
// # Boards
//
// A versioned YAML Config declares the boards. Each has a sort order (descending for points, ascending for times), a reset period (allTime, daily or weekly, in UTC) and, for a resetting board, how many earlier periods stay readable after a reset. A board ranks each account's best score in a period; equal scores rank by who got there first.
//
// # Submitting
//
// Service.Submit records a score for an account. Over HTTP (Service.Handler) the account is the subject of the player's account token, which the handler verifies offline with an accounts/token Verifier, so a player can only submit their own scores.
//
// A board with validation: signed accepts a score only with a proof from the game server: after deciding a match or checking a replay, the server signs the board, account and score with a ProofSigner, and the client submits the proof beside the score. The configuration lists the servers' public keys and how long a proof stays valid.
//
// # Queries
//
// Top pages through a period from the first rank, Around returns the entries around an account, and Friends ranks a set of accounts the game supplies. Each reads the current period, the previous one, or an earlier period the board keeps by its ID. Every board period is an order-statistic skip list in memory, so each query costs O(log n + entries returned): a few microseconds on a board of a million accounts.
//
// # Storage
//
// The service keeps its records in a Store: NewMemoryStore for tests, or OpenFileStore, which writes one JSON Lines file per board period. New loads them and drops the periods boards no longer keep; a reset drops them as it happens.
package selfhost
