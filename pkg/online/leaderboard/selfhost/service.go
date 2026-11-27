package selfhost

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/online/accounts/token"
)

// Query limits.
const (
	// MaxPage is the most entries Top returns at once.
	MaxPage = 100
	// MaxRadius is the most entries Around returns on each side of the account.
	MaxRadius = 50
	// MaxFriends is the most accounts one Friends query looks up.
	MaxFriends = 200
	// MaxAccountLength is the longest account ID, in bytes, the service ranks.
	MaxAccountLength = 128
)

// Errors the service returns; the HTTP handler maps each to a status and an error code.
var (
	// ErrUnknownBoard means the configuration has no board of that name.
	ErrUnknownBoard = errors.New("selfhost: unknown board")
	// ErrUnknownPeriod means the board has no such period, or no longer keeps it.
	ErrUnknownPeriod = errors.New("selfhost: unknown period")
	// ErrNotRanked means the account has no score in the period.
	ErrNotRanked = errors.New("selfhost: account has no score in the period")
	// ErrInvalidAccount means an account ID is empty, too long, not UTF-8 or holds a control character.
	ErrInvalidAccount = errors.New("selfhost: invalid account ID")
	// ErrScoreOutOfRange means the score is outside the board's min and max.
	ErrScoreOutOfRange = errors.New("selfhost: score out of range")
	// ErrProofRequired means the board validates scores and the submission has no proof.
	ErrProofRequired = errors.New("selfhost: the board needs a game server's proof")
	// ErrInvalidProof means the proof is malformed, signed by a key the configuration does not list, issued in the future, or not for this board, account and score.
	ErrInvalidProof = errors.New("selfhost: invalid score proof")
	// ErrProofExpired means the proof is older than the configuration's proofTTL, or was issued before the current period of a daily or weekly board began.
	ErrProofExpired = errors.New("selfhost: score proof expired")
	// ErrInvalidQuery means a limit, offset or radius is out of range, or a Friends query names too many accounts.
	ErrInvalidQuery = errors.New("selfhost: invalid query")
)

// Options are what a Service needs besides its Config.
type Options struct {
	// Store keeps the records; required.
	Store Store
	// Tokens verifies the account service's tokens for Handler, which needs it; a Service used only from Go may leave it nil.
	Tokens *token.Verifier
	// Logger receives resets and store errors, never tokens or proofs; nil discards them.
	Logger *slog.Logger
	// Now is the service's clock; nil uses time.Now.
	Now func() time.Time
	// Leeway is the clock skew allowed between a game server and the service: a proof issued up to Leeway in the future is accepted, and an expired one up to Leeway late.
	Leeway time.Duration
}

// Service is the leaderboard service: the configuration's boards, each ranking every account's best score in its current period and keeping its earlier periods readable. It is safe for concurrent use.
type Service struct {
	cfg    Config
	store  Store
	tokens *token.Verifier
	keys   map[string]ed25519.PublicKey
	boards map[string]*board
	log    *slog.Logger
	now    func() time.Time
	leeway time.Duration
}

// board is one board's state. mu guards current and periods.
type board struct {
	def     Board
	mu      sync.RWMutex
	current Period
	periods map[string]*index
}

// Entry is an account's place on a board.
type Entry struct {
	// Rank is the 1-based rank: the better score first, then the earlier achievement, then the smaller account ID, so no two entries share a rank.
	Rank int
	// Account is the account ID.
	Account string
	// Score is the account's best score in the period.
	Score int64
	// Achieved is when that score was submitted.
	Achieved time.Time
}

func (e entry) public(rank int) Entry {
	return Entry{Rank: rank, Account: e.account, Score: e.score, Achieved: time.Unix(0, e.achieved).UTC()}
}

// Page is the result of a query: some entries of one period of a board.
type Page struct {
	// Board is the board's name.
	Board string
	// Period is the period the entries are in.
	Period Period
	// Total is how many accounts have a score in the period.
	Total int
	// Entries are the entries asked for, in rank order.
	Entries []Entry
}

// Submission is a score for a board.
type Submission struct {
	// Board is the board's name.
	Board string
	// Account is the account that scored, the subject of the player's verified token.
	Account string
	// Score is the score.
	Score int64
	// Proof is a game server's ProofSigner proof of the score; a board with validation: signed needs it, other boards ignore it.
	Proof string
}

// Result is what a submission did.
type Result struct {
	// Period is the period the score counted in.
	Period Period
	// Entry is the account's entry after the submission: the new score when it improved on the account's best, the earlier best otherwise.
	Entry Entry
	// Total is how many accounts have a score in the period.
	Total int
	// Improved reports whether the score became the account's best in the period.
	Improved bool
}

// BoardInfo describes a board and its periods now.
type BoardInfo struct {
	// Board is the board's declaration.
	Board Board
	// Current is the period scores count in now.
	Current Period
	// Previous are the earlier periods still readable, the latest first.
	Previous []Period
}

// New returns a service with the configuration and options. It loads the store's records, serves those of each board's current period and the earlier periods the board keeps, and drops from the store only the periods that align with the board's reset and weekStart and are older than the ones it keeps. Records it cannot place are left in the store, not served, and logged as a warning: those of a board the configuration no longer declares, and those of a period that does not align, such as one stored before the board's reset or weekStart changed. A period later than the current one, which a clock that is behind sees, is loaded but not served until the clock reaches it.
func New(ctx context.Context, cfg Config, opts Options) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if opts.Store == nil {
		return nil, errors.New("selfhost: no store")
	}
	if opts.Leeway < 0 {
		return nil, errors.New("selfhost: negative leeway")
	}
	cfg.AllowedOrigins = slices.Clone(cfg.AllowedOrigins)
	s := &Service{cfg: cfg, store: opts.Store, tokens: opts.Tokens, keys: map[string]ed25519.PublicKey{}, boards: map[string]*board{}, log: opts.Logger, now: opts.Now, leeway: opts.Leeway}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	for _, k := range cfg.ServerKeys {
		pub, err := parsePublicKey(k)
		if err != nil {
			return nil, err
		}
		s.keys[k.ID] = pub
	}
	now := s.now()
	for _, def := range cfg.Boards {
		s.boards[def.Name] = &board{def: def, current: def.periodAt(now), periods: map[string]*index{}}
	}
	stale := map[[2]string]bool{}
	misaligned := map[string]bool{}
	ahead := map[string]bool{}
	unknown := map[string]bool{}
	err := s.store.Load(ctx, func(rec Record) error {
		b := s.boards[rec.Board]
		if b == nil {
			unknown[rec.Board] = true
			return nil
		}
		p, ok := b.def.aligned(rec.Period)
		if !ok {
			misaligned[rec.Board+"/"+rec.Period] = true
			return nil
		}
		if b.def.expired(rec.Period, b.current) {
			stale[[2]string{rec.Board, rec.Period}] = true
			return nil
		}
		if p.Start.After(b.current.Start) {
			ahead[rec.Board+"/"+rec.Period] = true
		}
		if !validAccount(rec.Account) || rec.Score < -MaxSafeScore || rec.Score > MaxSafeScore || !inNanoRange(rec.Achieved) {
			return fmt.Errorf("selfhost: store holds an invalid record for board %q period %q", rec.Board, rec.Period)
		}
		idx := b.periods[rec.Period]
		if idx == nil {
			idx = newIndex(b.def.Order)
			b.periods[rec.Period] = idx
		}
		idx.set(entry{account: rec.Account, score: rec.Score, achieved: rec.Achieved.UnixNano()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("selfhost: load: %w", err)
	}
	for _, name := range sortedKeys(unknown) {
		s.log.Warn("selfhost: the store holds scores of a board the configuration does not declare; they are kept but not served", "board", name)
	}
	for _, k := range sortedKeys(misaligned) {
		s.log.Warn("selfhost: the store holds a period that does not align with the board's reset and weekStart; it is kept but not served", "period", k)
	}
	for _, k := range sortedKeys(ahead) {
		s.log.Warn("selfhost: the store holds a period later than the current one; the clock may be behind, and the period is served once the clock reaches it", "period", k)
	}
	drops := make([][2]string, 0, len(stale))
	for k := range stale {
		drops = append(drops, k)
	}
	sort.Slice(drops, func(i, j int) bool { return drops[i][0]+"/"+drops[i][1] < drops[j][0]+"/"+drops[j][1] })
	for _, k := range drops {
		if err := s.store.Drop(ctx, k[0], k[1]); err != nil {
			return nil, fmt.Errorf("selfhost: drop %s/%s: %w", k[0], k[1], err)
		}
		s.log.Info("selfhost: dropped a period the board no longer keeps", "board", k[0], "period", k[1])
	}
	return s, nil
}

// Config returns the service's configuration.
func (s *Service) Config() Config {
	c := s.cfg
	c.AllowedOrigins = slices.Clone(s.cfg.AllowedOrigins)
	return c
}

// board returns the named board, first moving it to the period the clock is in when that period has started since.
func (s *Service) board(ctx context.Context, name string) (*board, error) {
	b := s.boards[name]
	if b == nil {
		return nil, ErrUnknownBoard
	}
	if b.def.Reset == AllTime {
		return b, nil
	}
	cur := b.def.periodAt(s.now())
	b.mu.RLock()
	stale := cur.Start.After(b.current.Start)
	b.mu.RUnlock()
	if stale {
		b.mu.Lock()
		s.rotate(ctx, b, cur)
		b.mu.Unlock()
	}
	return b, nil
}

// rotate makes cur the board's current period when it is later than the one it has, and drops the periods that have expired. b.mu must be held for writing.
func (s *Service) rotate(ctx context.Context, b *board, cur Period) {
	if !cur.Start.After(b.current.Start) {
		return
	}
	s.log.Info("selfhost: board reset", "board", b.def.Name, "period", cur.ID, "previous", b.current.ID)
	b.current = cur
	for _, id := range sortedKeys(b.periods) {
		if !b.def.expired(id, cur) {
			continue
		}
		delete(b.periods, id)
		// Dropping is clean-up: the period is gone from memory either way, and New drops it again on the next start if this fails.
		if err := s.store.Drop(context.WithoutCancel(ctx), b.def.Name, id); err != nil {
			s.log.Error("selfhost: drop an expired period", "board", b.def.Name, "period", id, "err", err)
		} else {
			s.log.Info("selfhost: dropped a period the board no longer keeps", "board", b.def.Name, "period", id)
		}
	}
}

// Submit records a score. The score counts in the board's current period and replaces the account's best there only when it is better; an equal or worse score leaves the entry as it was and reports Improved false.
func (s *Service) Submit(ctx context.Context, sub Submission) (Result, error) {
	b := s.boards[sub.Board]
	if b == nil {
		return Result{}, ErrUnknownBoard
	}
	if !validAccount(sub.Account) {
		return Result{}, ErrInvalidAccount
	}
	if lo, hi := b.def.bounds(); sub.Score < lo || sub.Score > hi {
		return Result{}, ErrScoreOutOfRange
	}
	// One clock reading decides the period, the achievement time and the proof's age, so a submission at a reset cannot land in one period with the time of another.
	now := s.now()
	var issued time.Time
	if b.def.Validation == SignedByServer {
		if sub.Proof == "" {
			return Result{}, ErrProofRequired
		}
		var err error
		issued, err = verifyProof(s.keys, sub.Proof, sub.Board, sub.Account, sub.Score)
		if err != nil {
			return Result{}, err
		}
		if issued.After(now.Add(s.leeway)) {
			return Result{}, ErrInvalidProof
		}
		if !now.Before(issued.Add(s.cfg.proofTTL() + s.leeway)) {
			return Result{}, ErrProofExpired
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if !inNanoRange(now) {
		return Result{}, fmt.Errorf("selfhost: clock %v out of range", now)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	s.rotate(ctx, b, b.def.periodAt(now))
	cur := b.current
	if b.def.Validation == SignedByServer && b.def.Reset != AllTime && issued.Before(cur.Start) {
		// A proof signed before the reset is for a score of the previous period, which is closed.
		return Result{}, ErrProofExpired
	}
	if now.Before(cur.Start) {
		// Another submission read the clock later and already moved the board on; this one arrived at the reset, so it counts at the reset's instant rather than before its own period.
		now = cur.Start
	}
	idx := b.periods[cur.ID]
	if idx == nil {
		idx = newIndex(b.def.Order)
	}
	if old, ok := idx.get(sub.Account); ok && !idx.better(sub.Score, old) {
		return Result{Period: cur, Entry: old.public(idx.rank(sub.Account)), Total: idx.n}, nil
	}
	e := entry{account: sub.Account, score: sub.Score, achieved: now.UnixNano()}
	if err := s.store.Save(ctx, Record{Board: sub.Board, Period: cur.ID, Account: sub.Account, Score: sub.Score, Achieved: time.Unix(0, e.achieved).UTC()}); err != nil {
		s.log.Error("selfhost: save a score", "board", sub.Board, "period", cur.ID, "err", err)
		return Result{}, fmt.Errorf("selfhost: save: %w", err)
	}
	b.periods[cur.ID] = idx
	idx.set(e)
	return Result{Period: cur, Entry: e.public(idx.rank(sub.Account)), Total: idx.n, Improved: true}, nil
}

// read resolves the board and period a query names and calls fn with the period's index, nil when it has no scores, under the board's read lock.
func (s *Service) read(ctx context.Context, name, period string, fn func(p Period, idx *index) error) error {
	b, err := s.board(ctx, name)
	if err != nil {
		return err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	p, ok := b.def.readable(period, b.current)
	if !ok {
		return ErrUnknownPeriod
	}
	return fn(p, b.periods[p.ID])
}

// Top returns up to limit entries (1 to MaxPage) of a period from rank offset+1 on. The period is "" or "current" for the current period, "previous" for the one before, or the ID of an earlier period the board still keeps.
func (s *Service) Top(ctx context.Context, board, period string, offset, limit int) (Page, error) {
	if offset < 0 || limit < 1 || limit > MaxPage {
		return Page{}, ErrInvalidQuery
	}
	var page Page
	err := s.read(ctx, board, period, func(p Period, idx *index) error {
		page = Page{Board: board, Period: p, Entries: []Entry{}}
		if idx != nil {
			page.Total = idx.n
			// An offset at or past the end is an empty page; checking first also keeps offset+1 from overflowing.
			if offset < idx.n {
				page.Entries = idx.window(offset+1, limit)
			}
		}
		return nil
	})
	return page, err
}

// Around returns the account's entry with up to radius entries (0 to MaxRadius) on each side of it. Near either end of the board the window shifts so it still holds 2*radius+1 entries when the board has that many. It returns ErrNotRanked when the account has no score in the period.
func (s *Service) Around(ctx context.Context, board, period, account string, radius int) (Page, error) {
	if radius < 0 || radius > MaxRadius {
		return Page{}, ErrInvalidQuery
	}
	if !validAccount(account) {
		return Page{}, ErrInvalidAccount
	}
	var page Page
	err := s.read(ctx, board, period, func(p Period, idx *index) error {
		if idx == nil {
			return ErrNotRanked
		}
		r := idx.rank(account)
		if r == 0 {
			return ErrNotRanked
		}
		first := max(1, min(r-radius, idx.n-2*radius))
		page = Page{Board: board, Period: p, Total: idx.n, Entries: idx.window(first, 2*radius+1)}
		return nil
	})
	return page, err
}

// Friends returns the entries of the given accounts that have a score in the period, in rank order, each with its rank on the whole board. A game passes the player's friends from wherever it keeps them (a platform's friend list, its own social graph), usually with the player's own account; duplicates are ignored. At most MaxFriends accounts are looked up at once.
func (s *Service) Friends(ctx context.Context, board, period string, accounts []string) (Page, error) {
	if len(accounts) > MaxFriends {
		return Page{}, ErrInvalidQuery
	}
	for _, a := range accounts {
		if !validAccount(a) {
			return Page{}, ErrInvalidAccount
		}
	}
	var page Page
	err := s.read(ctx, board, period, func(p Period, idx *index) error {
		page = Page{Board: board, Period: p, Entries: []Entry{}}
		if idx == nil {
			return nil
		}
		page.Total = idx.n
		seen := make(map[string]bool, len(accounts))
		for _, a := range accounts {
			if seen[a] {
				continue
			}
			seen[a] = true
			if e, ok := idx.get(a); ok {
				page.Entries = append(page.Entries, e.public(idx.rank(a)))
			}
		}
		sort.Slice(page.Entries, func(i, j int) bool { return page.Entries[i].Rank < page.Entries[j].Rank })
		return nil
	})
	return page, err
}

// Rank returns the account's entry in a period, or ErrNotRanked when it has none.
func (s *Service) Rank(ctx context.Context, board, period, account string) (Entry, error) {
	page, err := s.Around(ctx, board, period, account, 0)
	if err != nil {
		return Entry{}, err
	}
	return page.Entries[0], nil
}

// Boards describes every board in the configuration's order, with its current period and the earlier periods still readable.
func (s *Service) Boards(ctx context.Context) []BoardInfo {
	out := make([]BoardInfo, 0, len(s.cfg.Boards))
	for _, def := range s.cfg.Boards {
		b, _ := s.board(ctx, def.Name)
		b.mu.RLock()
		info := BoardInfo{Board: def, Current: b.current, Previous: []Period{}}
		b.mu.RUnlock()
		for k := 1; k <= def.keep(); k++ {
			info.Previous = append(info.Previous, def.previous(info.Current, k))
		}
		out = append(out, info)
	}
	return out
}

// validAccount reports whether id can be ranked: 1 to MaxAccountLength bytes of UTF-8 with no control characters.
func validAccount(id string) bool {
	if id == "" || len(id) > MaxAccountLength || !utf8.ValidString(id) {
		return false
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return false
		}
	}
	return true
}

// inNanoRange reports whether t fits in int64 Unix nanoseconds, the form entries keep it in.
func inNanoRange(t time.Time) bool {
	return t.Year() >= 1678 && t.Year() <= 2261
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
