package selfhost

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrTournamentClosed = errors.New("selfhost: tournament closed")
var ErrTournamentFull = errors.New("selfhost: tournament full")
var ErrAttemptsExceeded = errors.New("selfhost: tournament attempts exceeded")

type TournamentConfig struct {
	Name, Category           string
	Start, End               time.Time
	Duration                 time.Duration
	MaxEntrants, MaxAttempts int
	JoinRequired             bool
}
type TournamentRound struct {
	Number     int
	Start, End time.Time
}
type Tournament struct {
	mu         sync.Mutex
	cfg        TournamentConfig
	entrants   map[string]bool
	attempts   map[string]int
	scores     map[string]int64
	round      int
	ended      map[int]bool
	OnRoundEnd func(TournamentRound, []Entry)
}

func NewTournament(cfg TournamentConfig) *Tournament {
	return &Tournament{cfg: cfg, entrants: map[string]bool{}, attempts: map[string]int{}, scores: map[string]int64{}, ended: map[int]bool{}}
}
func (t *Tournament) Config() TournamentConfig { return t.cfg }
func (t *Tournament) Round(now time.Time) (TournamentRound, bool) {
	if now.Before(t.cfg.Start) || (!t.cfg.End.IsZero() && !now.Before(t.cfg.End)) {
		return TournamentRound{}, false
	}
	if t.cfg.Duration <= 0 {
		return TournamentRound{Number: 1, Start: t.cfg.Start, End: t.cfg.End}, true
	}
	n := int(now.Sub(t.cfg.Start) / t.cfg.Duration)
	st := t.cfg.Start.Add(time.Duration(n) * t.cfg.Duration)
	return TournamentRound{Number: n + 1, Start: st, End: st.Add(t.cfg.Duration)}, true
}
func (t *Tournament) Join(account string, now time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.Round(now); !ok {
		return ErrTournamentClosed
	}
	if t.entrants[account] {
		return nil
	}
	if t.cfg.MaxEntrants > 0 && len(t.entrants) >= t.cfg.MaxEntrants {
		return ErrTournamentFull
	}
	t.entrants[account] = true
	return nil
}
func (t *Tournament) Submit(account string, score int64, now time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.Round(now)
	if !ok {
		return ErrTournamentClosed
	}
	if t.cfg.JoinRequired && !t.entrants[account] {
		return ErrTournamentClosed
	}
	if t.cfg.MaxAttempts > 0 && t.attempts[account] >= t.cfg.MaxAttempts {
		return ErrAttemptsExceeded
	}
	t.attempts[account]++
	if old, ok := t.scores[account]; !ok || score > old {
		t.scores[account] = score
	}
	_ = r
	return nil
}
func (t *Tournament) EndRound(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.Round(now.Add(-time.Nanosecond))
	if !ok || t.ended[r.Number] {
		return
	}
	if now.Before(r.End) {
		return
	}
	t.ended[r.Number] = true
	rows := make([]Entry, 0, len(t.scores))
	for a, s := range t.scores {
		rows = append(rows, Entry{Account: a, Score: s})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		return rows[i].Account < rows[j].Account
	})
	for i := range rows {
		rows[i].Rank = i + 1
	}
	if t.OnRoundEnd != nil {
		t.OnRoundEnd(r, rows)
	}
}
func (t *Tournament) Entrants() int { t.mu.Lock(); defer t.mu.Unlock(); return len(t.entrants) }
