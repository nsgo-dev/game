package gameserver

import (
	"context"
	"errors"
	"sync"
)

var ErrResultConflict = errors.New("gameserver: match result conflict")

type MatchResults struct {
	MatchID                 string
	Records, Wallet, Scores []byte
}
type ResultCommitter interface {
	Commit(context.Context, MatchResults) error
}
type MemoryResultCommitter struct {
	mu        sync.Mutex
	committed map[string]MatchResults
}

func NewMemoryResultCommitter() *MemoryResultCommitter {
	return &MemoryResultCommitter{committed: map[string]MatchResults{}}
}
func (c *MemoryResultCommitter) Commit(ctx context.Context, r MatchResults) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.MatchID == "" {
		return errors.New("gameserver: empty match ID")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.committed[r.MatchID]; ok {
		if string(old.Records) != string(r.Records) || string(old.Wallet) != string(r.Wallet) || string(old.Scores) != string(r.Scores) {
			return ErrResultConflict
		}
		return nil
	}
	c.committed[r.MatchID] = r
	return nil
}
func (c *MemoryResultCommitter) Committed(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.committed[id]
	return ok
}
