package leaderboard

import (
	"errors"
	"fmt"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gameservices"
)

// Types shared with NSGo's package gameservices, so a game needs only this import.
type (
	// Player is the local player signed into the platform game service.
	Player = gameservices.Player
	// Entry is one ranked row of a leaderboard returned by Service.Scores. Its LeaderboardID is the platform ID, not the catalog name.
	Entry = gameservices.ScoreEntry
	// PlayerScope selects which players Service.Scores includes: Global or Friends.
	PlayerScope = gameservices.PlayerScope
	// TimeScope selects the time window Service.Scores covers: AllTime, Week or Today.
	TimeScope = gameservices.TimeScope
)

// Player scopes for Query.Players.
const (
	Global  = gameservices.Global
	Friends = gameservices.Friends
)

// Time scopes for Query.Time.
const (
	AllTime = gameservices.AllTime
	Week    = gameservices.Week
	Today   = gameservices.Today
)

// Errors a Service reports. The platform's own errors are NSGo's gameservices sentinels, repeated here so callers can test them with errors.Is without a second import.
var (
	// ErrUnknownLeaderboard means the name is not declared in the catalog. It is reported on every platform, so a misspelt name shows up while developing on a desktop without a game service.
	ErrUnknownLeaderboard = errors.New("leaderboard: unknown leaderboard")
	// ErrNoPlatformID means the board is declared but has no ID for the game service of the platform the game runs on.
	ErrNoPlatformID = errors.New("leaderboard: no ID for this platform's game service")
	// ErrUnsupported means the platform has no game service the game can reach (Windows, Linux, the web, watchOS, mini programs, or a device without Game Center or Play Games).
	ErrUnsupported = gameservices.ErrUnsupported
	// ErrUnavailable means the platform's game service could not be reached or is not enabled.
	ErrUnavailable = gameservices.ErrUnavailable
	// ErrUnauthenticated means no player is signed in; call Service.SignIn first.
	ErrUnauthenticated = gameservices.ErrUnauthenticated
	// ErrCancelled means the player dismissed the platform's sign-in.
	ErrCancelled = gameservices.ErrCancelled
)

// Provider names a platform game service a Board can carry an ID for.
type Provider uint8

const (
	// ProviderNone means the platform has no game service this package can use.
	ProviderNone Provider = iota
	// ProviderGameCenter is Apple's Game Center on iOS, iPadOS, tvOS and macOS.
	ProviderGameCenter
	// ProviderPlayGames is Google Play Games Services on Android.
	ProviderPlayGames
)

// String returns the provider's catalog key: "gameCenter", "playGames" or "none".
func (p Provider) String() string {
	switch p {
	case ProviderGameCenter:
		return "gameCenter"
	case ProviderPlayGames:
		return "playGames"
	}
	return "none"
}

// ProviderFor returns the game service NSGo's gameservices package talks to on os: Game Center on macOS and iOS (which covers iPadOS and tvOS), Play Games on Android, and ProviderNone elsewhere.
func ProviderFor(os core.OS) Provider {
	switch os {
	case core.OSMacOS, core.OSIOS:
		return ProviderGameCenter
	case core.OSAndroid:
		return ProviderPlayGames
	}
	return ProviderNone
}

// Query selects the rows Service.Scores loads. The zero Query asks for the global all-time board with the platform's default of 25 rows.
type Query struct {
	// Players chooses everyone (Global, the default) or the local player's friends (Friends).
	Players PlayerScope
	// Time chooses all-time scores (AllTime, the default), this week's (Week) or today's (Today).
	Time TimeScope
	// Limit is the most rows to load; 0 or less means 25, and more than 100 is clamped to 100.
	Limit int
}

// Service submits scores to and shows the leaderboards a Catalog declares, through NSGo's gameservices package and so through Game Center or Play Games. Every callback runs on the engine goroutine, after the call that started it has returned, so it may update signals and views directly.
type Service struct {
	cx      *core.Scope
	catalog Catalog
	player  *core.Signal[Player]
}

// New validates c and returns a Service bound to cx, which keeps its own copy of the catalog. The service starts signed out; call SignIn before submitting scores.
func New(cx *core.Scope, c *Catalog) (*Service, error) {
	if cx == nil {
		return nil, errors.New("leaderboard: nil scope")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &Service{
		cx:      cx,
		catalog: Catalog{Version: c.Version, Leaderboards: slices.Clone(c.Leaderboards)},
		player:  core.NewSignal(cx, Player{}),
	}, nil
}

// Catalog returns a copy of the catalog the service was built from.
func (s *Service) Catalog() *Catalog {
	return &Catalog{Version: s.catalog.Version, Leaderboards: slices.Clone(s.catalog.Leaderboards)}
}

// Provider returns the game service of the platform the game runs on, whether or not it is reachable.
func (s *Service) Provider() Provider {
	return ProviderFor(core.Untracked(s.cx, s.cx.Env().OS))
}

// Available reports whether the platform's game service can be used here. It reads the environment reactively, so a view that calls it updates when that changes.
func (s *Service) Available() bool {
	return s.cx.Env().CanUseGameServices() && s.Provider() != ProviderNone
}

// Supports reports whether the board name can be submitted to and shown here: the service is Available and the board has an ID for this platform. Like Available it reads reactively.
func (s *Service) Supports(name string) bool {
	_, err := s.resolve(name, s.Available())
	return err == nil
}

// ID returns the platform ID of the board name, or ErrUnknownLeaderboard, ErrUnsupported or ErrNoPlatformID (checked in that order).
func (s *Service) ID(name string) (string, error) {
	return s.resolveNow(name)
}

// Player returns the signed-in player, or the zero Player while signed out. It reads a signal, so a view that calls it updates on sign-in and sign-out.
func (s *Service) Player() Player {
	return s.player.Get()
}

// SignedIn reports whether a player is signed in, reactively like Player.
func (s *Service) SignedIn() bool {
	return s.player.Get().Authenticated
}

// SignIn signs the local player into the platform game service, which may show the platform's own sign-in UI, and calls done with the player or an error such as ErrUnsupported or ErrCancelled. done may be nil.
func (s *Service) SignIn(done func(Player, error)) {
	if !s.availableNow() {
		s.post(func() {
			if done != nil {
				done(Player{}, ErrUnsupported)
			}
		})
		return
	}
	gameservices.Authenticate(s.cx, func(p Player, err error) {
		switch {
		case err == nil:
			s.player.Set(p)
		case errors.Is(err, ErrUnauthenticated), errors.Is(err, ErrCancelled):
			s.player.Set(Player{})
		}
		if done != nil {
			done(p, err)
		}
	})
}

// Submit sends score to the board name for the signed-in player and calls done with the result; done may be nil. The platform keeps each player's best score by the board's own sort order, so a game can submit every finished run. A board the catalog does not declare, a platform without a game service or a board without an ID here fail through done without reaching the platform.
func (s *Service) Submit(name string, score int64, done func(error)) {
	id, err := s.resolveNow(name)
	if err != nil {
		s.fail(err, done)
		return
	}
	gameservices.SubmitScore(s.cx, id, score, func(err error) {
		s.noteSignedOut(err)
		if done != nil {
			done(err)
		}
	})
}

// Show presents the platform's own leaderboard UI for the board name. It reports ErrUnknownLeaderboard, ErrUnsupported or ErrNoPlatformID instead of silently doing nothing; once it returns nil, what the platform shows (including its sign-in prompt) is up to the platform.
func (s *Service) Show(name string) error {
	id, err := s.resolveNow(name)
	if err != nil {
		return err
	}
	gameservices.ShowLeaderboard(s.cx, id)
	return nil
}

// Scores loads ranked rows of the board name selected by q, for a game that draws its own board, and calls done with them or an error. done must not be nil.
func (s *Service) Scores(name string, q Query, done func([]Entry, error)) {
	if done == nil {
		panic("leaderboard: Scores needs a done callback")
	}
	id, err := s.resolveNow(name)
	if err != nil {
		s.post(func() { done(nil, err) })
		return
	}
	gameservices.LoadScores(s.cx, id, q.Players, q.Time, q.Limit, func(entries []Entry, err error) {
		s.noteSignedOut(err)
		done(entries, err)
	})
}

// availableNow is Available without subscribing the running computation, for actions rather than views.
func (s *Service) availableNow() bool {
	return core.Untracked(s.cx, s.Available)
}

// resolveNow maps a catalog name to this platform's ID without subscribing the running computation.
func (s *Service) resolveNow(name string) (string, error) {
	return s.resolve(name, s.availableNow())
}

// resolve maps a catalog name to this platform's ID, given whether the game service is available.
func (s *Service) resolve(name string, available bool) (string, error) {
	b, ok := s.catalog.Board(name)
	if !ok {
		return "", fmt.Errorf("%w %q", ErrUnknownLeaderboard, name)
	}
	if !available {
		return "", ErrUnsupported
	}
	id := b.ID(s.Provider())
	if id == "" {
		return "", fmt.Errorf("%w: board %q has no %s ID", ErrNoPlatformID, name, s.Provider())
	}
	return id, nil
}

// noteSignedOut clears the player when the platform reports that nobody is signed in any more.
func (s *Service) noteSignedOut(err error) {
	if errors.Is(err, ErrUnauthenticated) {
		s.player.Set(Player{})
	}
}

// fail reports err through done on the engine goroutine, like the platform's own replies.
func (s *Service) fail(err error, done func(error)) {
	if done == nil {
		return
	}
	s.post(func() { done(err) })
}

// post runs fn on the engine goroutine after the current call returns.
func (s *Service) post(fn func()) {
	s.cx.Post(fn)
}
