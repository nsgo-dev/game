package gameserver

import (
	"context"
	"errors"
	"sync"
)

var ErrResultConflict = errors.New("gameserver: match result conflict")

// ResultCommit is an idempotent match outcome with server-owned grants and scores.
type ResultCommit struct {
	MatchID string
	Records []RecordGrant
	Wallet  []WalletGrant
	Scores  []ScoreGrant
}
type RecordGrant struct {
	Key     string
	Version uint64
	Value   []byte
}
type WalletGrant struct {
	Account, Currency string
	Amount            int64
}
type ScoreGrant struct {
	Board, Account string
	Score          int64
}
type ResultReceipt struct {
	MatchID string
	Applied bool
}
type ResultsBackend struct {
	mu        sync.Mutex
	committed map[string]ResultCommit
	records   map[string]RecordGrant
	wallet    map[string]int64
	scores    map[string]int64
}

func NewResultsBackend() *ResultsBackend {
	return &ResultsBackend{committed: map[string]ResultCommit{}, records: map[string]RecordGrant{}, wallet: map[string]int64{}, scores: map[string]int64{}}
}
func (b *ResultsBackend) Commit(ctx context.Context, r ResultCommit) (ResultReceipt, error) {
	if err := ctx.Err(); err != nil {
		return ResultReceipt{}, err
	}
	if b == nil || r.MatchID == "" {
		return ResultReceipt{}, ErrResultConflict
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if old, ok := b.committed[r.MatchID]; ok {
		if equalResult(old, r) {
			return ResultReceipt{MatchID: r.MatchID}, nil
		}
		return ResultReceipt{}, ErrResultConflict
	}
	for _, g := range r.Records {
		if old, ok := b.records[g.Key]; ok && old.Version != g.Version {
			return ResultReceipt{}, ErrResultConflict
		}
	}
	for _, g := range r.Records {
		b.records[g.Key] = g
	}
	for _, g := range r.Wallet {
		b.wallet[g.Account+":"+g.Currency] += g.Amount
	}
	for _, g := range r.Scores {
		key := g.Board + ":" + g.Account
		if g.Score > b.scores[key] {
			b.scores[key] = g.Score
		}
	}
	b.committed[r.MatchID] = r
	return ResultReceipt{MatchID: r.MatchID, Applied: true}, nil
}
func equalResult(a, b ResultCommit) bool {
	return a.MatchID == b.MatchID && len(a.Records) == len(b.Records) && len(a.Wallet) == len(b.Wallet) && len(a.Scores) == len(b.Scores)
}
func (b *ResultsBackend) Has(matchID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.committed[matchID]
	return ok
}
