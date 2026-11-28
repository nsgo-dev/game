package accounts

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"
)

// PublicProfile is the safe subset of account data shown to other players.
type PublicProfile struct {
	AccountID   string
	Username    string
	DisplayName string
	AvatarURL   string
	Lang        string
	TimeZone    string
	Metadata    map[string]any
	Updated     time.Time
}

type ProfileUpdate struct {
	Username, DisplayName, AvatarURL, Lang, TimeZone string
	Metadata                                         map[string]any
}

var ErrInvalidUsername = errors.New("accounts: invalid username")
var ErrUsernameCooldown = errors.New("accounts: username change cooldown")

// PublicDirectory stores public profiles and supports atomic username claims.
type PublicDirectory struct {
	mu               sync.RWMutex
	byID             map[string]PublicProfile
	byName           map[string]string
	lastName         map[string]time.Time
	Now              func() time.Time
	UsernameCooldown time.Duration
	NameFilter       func(string) error
}

func NewPublicDirectory() *PublicDirectory {
	return &PublicDirectory{byID: map[string]PublicProfile{}, byName: map[string]string{}}
}
func normalizeUsername(v string) string {
	return strings.ToLower(norm.NFKC.String(strings.TrimSpace(v)))
}
func validUsername(v string) bool {
	n := []rune(v)
	if len(n) < 3 || len(n) > 20 {
		return false
	}
	for _, r := range n {
		if !(r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
func (d *PublicDirectory) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}
func (d *PublicDirectory) Set(ctx context.Context, id string, u ProfileUpdate) (PublicProfile, error) {
	if err := ctx.Err(); err != nil {
		return PublicProfile{}, err
	}
	if d == nil || id == "" {
		return PublicProfile{}, ErrNotFound
	}
	name := normalizeUsername(u.Username)
	if !validUsername(name) {
		return PublicProfile{}, ErrInvalidUsername
	}
	if d.NameFilter != nil {
		if err := d.NameFilter(name); err != nil {
			return PublicProfile{}, err
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.byID == nil {
		d.byID = map[string]PublicProfile{}
	}
	if d.byName == nil {
		d.byName = map[string]string{}
	}
	if d.lastName == nil {
		d.lastName = map[string]time.Time{}
	}
	now := d.now()
	old := d.byID[id]
	if old.Username != name && !old.Updated.IsZero() && d.UsernameCooldown > 0 && now.Sub(d.lastName[id]) < d.UsernameCooldown {
		return PublicProfile{}, ErrUsernameCooldown
	}
	if owner := d.byName[name]; owner != "" && owner != id {
		return PublicProfile{}, ErrTaken
	}
	if old.Username != "" {
		delete(d.byName, old.Username)
	}
	d.byName[name] = id
	cp := PublicProfile{AccountID: id, Username: name, DisplayName: u.DisplayName, AvatarURL: u.AvatarURL, Lang: u.Lang, TimeZone: u.TimeZone, Metadata: cloneMetadata(u.Metadata), Updated: now}
	d.byID[id] = cp
	d.lastName[id] = now
	return copyPublic(cp), nil
}
func (d *PublicDirectory) Users(ctx context.Context, ids []string, usernames []string) ([]PublicProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	for _, n := range usernames {
		if id := d.byName[normalizeUsername(n)]; id != "" {
			want[id] = true
		}
	}
	out := make([]PublicProfile, 0, len(want))
	for id := range want {
		if p, ok := d.byID[id]; ok {
			out = append(out, copyPublic(p))
		}
	}
	return out, nil
}
func cloneMetadata(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
func copyPublic(p PublicProfile) PublicProfile { p.Metadata = cloneMetadata(p.Metadata); return p }
