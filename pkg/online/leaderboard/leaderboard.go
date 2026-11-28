package leaderboard

import (
	"errors"
	"fmt"
	"sync"

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
	// ErrNoPlatformID means the board is declared but has no ID or storage entry for the game service of the platform the game runs on.
	ErrNoPlatformID = errors.New("leaderboard: no ID for this platform's game service")
	// ErrUnsupported means the platform has no game service the game can reach (Windows, Linux, the web, watchOS, simulated OS builds without a mini-game host, or a device without Game Center or Play Games), or the operation is unsupported by the current provider (such as Scores on ProviderMiniGame).
	ErrUnsupported = gameservices.ErrUnsupported
	// ErrUnavailable means the platform's game service could not be reached or is not enabled.
	ErrUnavailable = gameservices.ErrUnavailable
	// ErrUnauthenticated means no player is signed in; call Service.SignIn first.
	ErrUnauthenticated = gameservices.ErrUnauthenticated
	// ErrCancelled means the player dismissed the platform's sign-in.
	ErrCancelled = gameservices.ErrCancelled
)

// Provider names a platform game service a Board can carry an ID or storage entry for.
type Provider uint8

const (
	// ProviderNone means the platform has no game service this package can use.
	ProviderNone Provider = iota
	// ProviderGameCenter is Apple's Game Center on iOS, iPadOS, tvOS and macOS.
	ProviderGameCenter
	// ProviderPlayGames is Google Play Games Services on Android.
	ProviderPlayGames
	// ProviderSteam is Valve Steamworks on desktop.
	ProviderSteam
	// ProviderMiniGame is WeChat or Douyin mini-game friend rankings backed by host user cloud storage and the open data context.
	ProviderMiniGame
)

// String returns the provider's catalog key: "gameCenter", "playGames", "miniGame" or "none".
func (p Provider) String() string {
	switch p {
	case ProviderGameCenter:
		return "gameCenter"
	case ProviderPlayGames:
		return "playGames"
	case ProviderSteam:
		return "steam"
	case ProviderMiniGame:
		return "miniGame"
	}
	return "none"
}

// ProviderFor returns the leaderboard provider for env: ProviderMiniGame inside a mini program host (CanSignInMiniProgram), ProviderNone when env is nil or Simulated (such as a web build run with --nsgo-os ios), ProviderGameCenter on native macOS and iOS (which covers iPadOS and tvOS), ProviderPlayGames on native Android, and ProviderNone elsewhere.
func ProviderFor(env *core.Env) Provider {
	if env == nil {
		return ProviderNone
	}
	if env.CanSignInMiniProgram() {
		return ProviderMiniGame
	}
	if env.Simulated() {
		return ProviderNone
	}
	switch env.OS() {
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

// Service submits scores to and shows the leaderboards a Catalog declares, through NSGo's gameservices package (Game Center or Play Games) or a mini-game host's friend cloud storage and open data context (WeChat and Douyin). Every callback runs on the engine goroutine, after the call that started it has returned, so it may update signals and views directly.
type Service struct {
	cx       *core.Scope
	catalog  Catalog
	player   *core.Signal[Player]
	miniHost MiniGameHost

	bestMu   sync.Mutex
	miniBest map[string]int64
}

// New validates c and returns a Service bound to cx, which keeps its own copy of the catalog. The service starts signed out; on Game Center and Play Games call SignIn before submitting scores. Pass Option values such as WithMiniGameHost to override the mini-game host in tests.
func New(cx *core.Scope, c *Catalog, opts ...Option) (*Service, error) {
	if cx == nil {
		return nil, errors.New("leaderboard: nil scope")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var cfg serviceConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &Service{
		cx:       cx,
		catalog:  Catalog{Version: c.Version, Leaderboards: cloneBoards(c.Leaderboards)},
		player:   core.NewSignal(cx, Player{}),
		miniHost: cfg.miniHost,
		miniBest: make(map[string]int64),
	}, nil
}

// Catalog returns a deep copy of the catalog the service was built from.
func (s *Service) Catalog() *Catalog {
	return &Catalog{Version: s.catalog.Version, Leaderboards: cloneBoards(s.catalog.Leaderboards)}
}

// Provider returns the game service of the platform the game runs on, whether or not it is reachable.
func (s *Service) Provider() Provider {
	return core.Untracked(s.cx, func() Provider { return ProviderFor(s.cx.Env()) })
}

// Available reports whether the platform's game service can be used here. It reads the environment reactively, so a view that calls it updates when that changes.
func (s *Service) Available() bool {
	env := s.cx.Env()
	switch ProviderFor(env) {
	case ProviderGameCenter, ProviderPlayGames:
		return env.CanUseGameServices()
	case ProviderMiniGame:
		return s.miniGameHost() != nil
	default:
		return false
	}
}

// Supports reports whether the board name can be submitted to and shown here: the service is Available and the board has an entry for this platform. Like Available it reads reactively.
func (s *Service) Supports(name string) bool {
	_, _, err := s.resolveBoard(name, s.Available())
	return err == nil
}

// ID returns the platform ID (or mini-game cloud storage key) of the board name, or ErrUnknownLeaderboard, ErrUnsupported or ErrNoPlatformID (checked in that order).
func (s *Service) ID(name string) (string, error) {
	_, id, err := s.resolveBoardNow(name)
	return id, err
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
	if s.Provider() == ProviderMiniGame {
		s.signInMiniGame(done)
		return
	}
	gameservices.Authenticate(s.cx, func(p Player, err error) {
		if err == nil && !p.Authenticated {
			// A backend that answers nil without a player has not signed anyone in.
			err = ErrUnauthenticated
		}
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

// Submit sends score to the board name for the signed-in player and calls done with the result; done may be nil. On Game Center and Play Games the platform keeps each player's best score by the board's own sort order; on ProviderMiniGame Service writes through host user cloud storage only when score improves on the player's previous best for the board's Order. A board the catalog does not declare, a platform without a game service or a board without an entry here fail through done without reaching the platform.
func (s *Service) Submit(name string, score int64, done func(error)) {
	b, id, err := s.resolveBoardNow(name)
	if err != nil {
		s.fail(err, done)
		return
	}
	if s.Provider() == ProviderMiniGame {
		s.submitMiniGame(b, score, done)
		return
	}
	gameservices.SubmitScore(s.cx, id, score, func(err error) {
		s.noteSignedOut(err)
		if done != nil {
			done(err)
		}
	})
}

// Show presents the platform's own leaderboard UI for the board name. On ProviderMiniGame it presents the open data context friend ranking over the full game view; call ShowIn to place it inside a specific rectangle and Hide to remove it. It reports ErrUnknownLeaderboard, ErrUnsupported or ErrNoPlatformID instead of silently doing nothing; once it returns nil on Game Center or Play Games, what the platform shows (including its sign-in prompt) is up to the platform.
func (s *Service) Show(name string) error {
	return s.show(name, core.Bounds{}, false)
}

// ShowIn presents the leaderboard UI for name over bounds (in points relative to the game view). On ProviderMiniGame it sizes and positions the open data context friend ranking at bounds (or the full window when bounds.Size is zero). On Game Center and Play Games native sheets are full-screen, so ShowIn behaves like Show.
func (s *Service) ShowIn(name string, bounds core.Bounds) error {
	return s.show(name, bounds, true)
}

func (s *Service) show(name string, bounds core.Bounds, hasBounds bool) error {
	b, id, err := s.resolveBoardNow(name)
	if err != nil {
		return err
	}
	if s.Provider() == ProviderMiniGame {
		return s.showMiniGame(b, bounds, hasBounds)
	}
	gameservices.ShowLeaderboard(s.cx, id)
	return nil
}

// Hide removes the open data context friend ranking presented by Show or ShowIn on ProviderMiniGame. On platforms without an available game service it returns ErrUnsupported; on Game Center and Play Games native overlays are dismissed by the player and Hide returns nil.
func (s *Service) Hide() error {
	if !s.availableNow() {
		return ErrUnsupported
	}
	if s.Provider() == ProviderMiniGame {
		return s.miniGameHost().HideFriendRanking()
	}
	return nil
}

// Scores loads ranked rows of the board name selected by q, for a game that draws its own board, and calls done with them or an error. done must not be nil. On ProviderMiniGame, Scores (including Friends) always answers ErrUnsupported because friend cloud storage is isolated inside the open data context and never returned to the game.
func (s *Service) Scores(name string, q Query, done func([]Entry, error)) {
	if done == nil {
		panic("leaderboard: Scores needs a done callback")
	}
	b, ok := s.catalog.Board(name)
	if !ok {
		s.post(func() { done(nil, fmt.Errorf("%w %q", ErrUnknownLeaderboard, name)) })
		return
	}
	if !s.availableNow() {
		s.post(func() { done(nil, ErrUnsupported) })
		return
	}
	provider := s.Provider()
	if provider == ProviderMiniGame {
		s.post(func() { done(nil, ErrUnsupported) })
		return
	}
	id := b.ID(provider)
	if id == "" {
		s.post(func() { done(nil, fmt.Errorf("%w: board %q has no %s ID", ErrNoPlatformID, name, provider)) })
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

// resolveBoardNow maps a catalog name to its Board and platform ID without subscribing the running computation.
func (s *Service) resolveBoardNow(name string) (Board, string, error) {
	return s.resolveBoard(name, s.availableNow())
}

// resolveBoard maps a catalog name to its Board and platform ID, given whether the game service is available.
func (s *Service) resolveBoard(name string, available bool) (Board, string, error) {
	b, ok := s.catalog.Board(name)
	if !ok {
		return Board{}, "", fmt.Errorf("%w %q", ErrUnknownLeaderboard, name)
	}
	if !available {
		return Board{}, "", ErrUnsupported
	}
	provider := s.Provider()
	id := b.ID(provider)
	if id == "" {
		return Board{}, "", fmt.Errorf("%w: board %q has no %s ID", ErrNoPlatformID, name, provider)
	}
	return b, id, nil
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
