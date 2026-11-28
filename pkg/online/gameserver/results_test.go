package gameserver

import (
	"context"
	"errors"
	"testing"
)

func TestResultsCommitIsIdempotentAndAtomic(t *testing.T) {
	b := NewResultsBackend()
	r := ResultCommit{MatchID: "room:1", Records: []RecordGrant{{Key: "xp:a", Version: 1}}, Wallet: []WalletGrant{{Account: "a", Currency: "coin", Amount: 5}}, Scores: []ScoreGrant{{Board: "b", Account: "a", Score: 10}}}
	got, e := b.Commit(context.Background(), r)
	if e != nil || !got.Applied {
		t.Fatalf("commit=%+v %v", got, e)
	}
	got, e = b.Commit(context.Background(), r)
	if e != nil || got.Applied {
		t.Fatalf("retry=%+v %v", got, e)
	}
	r.MatchID = "other"
	r.Records[0].Version = 2
	if _, e = b.Commit(context.Background(), r); !errors.Is(e, ErrResultConflict) {
		t.Fatalf("conflict=%v", e)
	}
}
