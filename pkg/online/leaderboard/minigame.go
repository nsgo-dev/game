package leaderboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
)

const miniGameBestKeyPrefix = "leaderboard.minigame.best."

// MiniGameEntry is one key-value pair written to a mini-game host's user cloud storage (such as wx.setUserCloudStorage on WeChat or tt.setUserCloudStorage on Douyin).
type MiniGameEntry struct {
	// Key is the catalog board's miniGame.key (at most MaxMiniGameKeyLength bytes).
	Key string
	// Value is the serialized score payload (WeChat's wxgame JSON object), bounded so len(Key)+len(Value) <= MaxMiniGameValueBytes.
	Value string
}

// MiniGameRanking describes a request to present the open data context friend ranking view over a rectangle of the game view.
type MiniGameRanking struct {
	// Key is the user cloud storage key whose friend entries to rank.
	Key string
	// Order is the sort order ("desc" or "asc") for ranking friend entries.
	Order Order
	// Bounds is the rectangle of the game view, in points, where the open data context draws the ranking.
	Bounds core.Bounds
}

// MiniGameHost bridges Service to a mini-game host's user cloud storage and open data context (WeChat wx.* / Douyin tt.*, or an NSGo test host).
//
// SetUserCloudStorage writes entries for the local player and invokes done with nil or an error from any goroutine; Service marshals callbacks onto the engine goroutine. ShowFriendRanking presents the open data context ranking view over req.Bounds, and HideFriendRanking removes it.
type MiniGameHost interface {
	SetUserCloudStorage(entries []MiniGameEntry, done func(error))
	ShowFriendRanking(req MiniGameRanking) error
	HideFriendRanking() error
}

type serviceConfig struct {
	miniHost MiniGameHost
}

// Option configures a Service created by New.
type Option func(*serviceConfig)

// WithMiniGameHost sets the MiniGameHost used when the service runs under ProviderMiniGame. On js/wasm mini-game targets, New uses the host's wx / tt bridge automatically when no custom host is supplied. Pass a test host here in nsgotest tests.
func WithMiniGameHost(host MiniGameHost) Option {
	return func(cfg *serviceConfig) {
		cfg.miniHost = host
	}
}

type miniGameScoreEnvelope struct {
	WXGame miniGameWXGame `json:"wxgame"`
}

type miniGameWXGame struct {
	Score      int64 `json:"score"`
	UpdateTime int64 `json:"update_time"`
}

// EncodeMiniGameScore encodes score and updateTime (Unix seconds) into the JSON object format WeChat and Douyin open data contexts expect ({"wxgame":{"score":...,"update_time":...}}), verifying that len(key)+len(value) stays within MaxMiniGameValueBytes (1 KiB).
func EncodeMiniGameScore(key string, score, updateTime int64) (string, error) {
	if err := validateMiniGameKey(key); err != nil {
		return "", fmt.Errorf("leaderboard: miniGame key: %w", err)
	}
	raw, err := json.Marshal(miniGameScoreEnvelope{
		WXGame: miniGameWXGame{
			Score:      score,
			UpdateTime: updateTime,
		},
	})
	if err != nil {
		return "", fmt.Errorf("leaderboard: encode miniGame score: %w", err)
	}
	if len(key)+len(raw) > MaxMiniGameValueBytes {
		return "", fmt.Errorf("leaderboard: miniGame KV entry for %q is %d bytes (at most %d)", key, len(key)+len(raw), MaxMiniGameValueBytes)
	}
	return string(raw), nil
}

// DecodeMiniGameScore decodes a JSON score payload produced by EncodeMiniGameScore.
func DecodeMiniGameScore(value string) (score, updateTime int64, err error) {
	var env miniGameScoreEnvelope
	if err := json.Unmarshal([]byte(value), &env); err != nil {
		return 0, 0, fmt.Errorf("leaderboard: decode miniGame score: %w", err)
	}
	return env.WXGame.Score, env.WXGame.UpdateTime, nil
}

func (s *Service) miniGameHost() MiniGameHost {
	if s.miniHost != nil {
		return s.miniHost
	}
	return defaultMiniGameHost()
}

func isBetterScore(candidate, current int64, order Order) bool {
	if order == OrderAsc {
		return candidate < current
	}
	return candidate > current
}

func (s *Service) loadMiniGameBest(key string) (int64, bool) {
	s.bestMu.Lock()
	if v, ok := s.miniBest[key]; ok {
		s.bestMu.Unlock()
		return v, true
	}
	s.bestMu.Unlock()

	raw, ok := s.cx.Storage().GetItem(miniGameBestKeyPrefix + key)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	s.bestMu.Lock()
	if cur, exists := s.miniBest[key]; exists {
		s.bestMu.Unlock()
		return cur, true
	}
	s.miniBest[key] = v
	s.bestMu.Unlock()
	return v, true
}

func (s *Service) recordMiniGameBest(key string, score int64, order Order) {
	s.bestMu.Lock()
	if cur, exists := s.miniBest[key]; exists && !isBetterScore(score, cur, order) {
		s.bestMu.Unlock()
		return
	}
	s.miniBest[key] = score
	s.bestMu.Unlock()
	_ = s.cx.Storage().SetItem(miniGameBestKeyPrefix+key, strconv.FormatInt(score, 10))
}

func (s *Service) submitMiniGame(b Board, score int64, done func(error)) {
	host := s.miniGameHost()
	if host == nil {
		s.fail(ErrUnsupported, done)
		return
	}
	key := b.MiniGame.Key
	order := b.MiniGame.EffectiveOrder()
	if best, ok := s.loadMiniGameBest(key); ok && !isBetterScore(score, best, order) {
		s.post(func() {
			if done != nil {
				done(nil)
			}
		})
		return
	}

	var updateTime int64
	if now := s.cx.WallTime(); !now.IsZero() {
		updateTime = now.Unix()
	}
	val, err := EncodeMiniGameScore(key, score, updateTime)
	if err != nil {
		s.fail(err, done)
		return
	}

	var once sync.Once
	host.SetUserCloudStorage([]MiniGameEntry{{Key: key, Value: val}}, func(err error) {
		once.Do(func() {
			s.post(func() {
				if err == nil {
					s.recordMiniGameBest(key, score, order)
				}
				if done != nil {
					done(err)
				}
			})
		})
	})
}

func (s *Service) showMiniGame(b Board, bounds core.Bounds, hasBounds bool) error {
	host := s.miniGameHost()
	if host == nil {
		return ErrUnsupported
	}
	if hasBounds && (bounds.Size.Width < 0 || bounds.Size.Height < 0) {
		return fmt.Errorf("leaderboard: negative ranking bounds %v", bounds)
	}
	if !hasBounds || (bounds.Size.Width == 0 && bounds.Size.Height == 0) {
		win := core.Untracked(s.cx, s.cx.Env().Window)
		bounds = core.Bounds{Size: win}
	}
	return host.ShowFriendRanking(MiniGameRanking{
		Key:    b.MiniGame.Key,
		Order:  b.MiniGame.EffectiveOrder(),
		Bounds: bounds,
	})
}

func (s *Service) signInMiniGame(done func(Player, error)) {
	var once sync.Once
	s.cx.SignInMiniProgram(core.MiniProgramSignInOptions{}, func(cred core.MiniProgramCredential, err error) {
		once.Do(func() {
			s.post(func() {
				var p Player
				var outErr error
				if err == nil {
					p = Player{
						ID:            cred.Code,
						DisplayName:   string(cred.Platform),
						Authenticated: true,
					}
					s.player.Set(p)
				} else {
					outErr = mapMiniProgramSignInError(err)
					if errors.Is(outErr, ErrUnauthenticated) || errors.Is(outErr, ErrCancelled) {
						s.player.Set(Player{})
					}
				}
				if done != nil {
					done(p, outErr)
				}
			})
		})
	})
}

func mapMiniProgramSignInError(err error) error {
	switch {
	case errors.Is(err, core.MiniProgramSignInCancelled):
		return ErrCancelled
	case errors.Is(err, core.MiniProgramSignInNotSignedIn):
		return ErrUnauthenticated
	case errors.Is(err, core.MiniProgramSignInUnavailable):
		return ErrUnavailable
	default:
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
}
