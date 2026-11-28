package accounts

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrSessionExpired = errors.New("accounts: session expired")
var ErrSessionReuse = errors.New("accounts: session secret reused")

type SessionPolicy struct {
	IdleTTL, AbsoluteTTL time.Duration
	Rotate               bool
	Overlap              time.Duration
}
type DeviceSession struct {
	ID, AccountID, Device      string
	Created, LastUsed, Expires time.Time
	Generation                 uint64
	SecretHash                 string
}

type SessionDirectory struct {
	mu       sync.Mutex
	policy   SessionPolicy
	now      func() time.Time
	sessions map[string]DeviceSession
	secrets  map[string]string
	overlap  map[string]oldSecret
}
type oldSecret struct {
	hash       string
	until      time.Time
	generation uint64
}

func NewSessionDirectory(policy SessionPolicy, now func() time.Time) *SessionDirectory {
	return &SessionDirectory{policy: policy, now: now, sessions: map[string]DeviceSession{}, secrets: map[string]string{}, overlap: map[string]oldSecret{}}
}
func (d *SessionDirectory) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}
func (d *SessionDirectory) Open(s DeviceSession, secretHash string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if s.Created.IsZero() {
		s.Created = d.clock()
	}
	s.LastUsed = s.Created
	s.Generation = 1
	s.SecretHash = secretHash
	if d.policy.AbsoluteTTL > 0 {
		s.Expires = s.Created.Add(d.policy.AbsoluteTTL)
	}
	d.sessions[s.ID] = s
	d.secrets[s.ID] = secretHash
}
func (d *SessionDirectory) Check(id, secretHash string) (DeviceSession, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.sessions[id]
	if !ok {
		return DeviceSession{}, ErrInvalidCredentials
	}
	now := d.clock()
	if (d.policy.AbsoluteTTL > 0 && now.Sub(s.Created) >= d.policy.AbsoluteTTL) || (d.policy.IdleTTL > 0 && now.Sub(s.LastUsed) >= d.policy.IdleTTL) {
		delete(d.sessions, id)
		delete(d.secrets, id)
		return DeviceSession{}, ErrSessionExpired
	}
	if d.secrets[id] != secretHash {
		if old, ok := d.overlap[id]; ok && now.Before(old.until) && old.hash == secretHash {
			return s, nil
		}
		delete(d.sessions, id)
		delete(d.secrets, id)
		return DeviceSession{}, ErrSessionReuse
	}
	s.LastUsed = now
	d.sessions[id] = s
	return s, nil
}
func (d *SessionDirectory) Refresh(id, secretHash, newSecretHash string) (DeviceSession, error) {
	s, e := d.Check(id, secretHash)
	if e != nil {
		return DeviceSession{}, e
	}
	if !d.policy.Rotate {
		return s, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.overlap[id] = oldSecret{hash: secretHash, until: d.clock().Add(d.policy.Overlap), generation: s.Generation}
	s.Generation++
	s.LastUsed = d.clock()
	s.SecretHash = newSecretHash
	d.sessions[id] = s
	d.secrets[id] = newSecretHash
	return s, nil
}
func (d *SessionDirectory) List(account string) []DeviceSession {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]DeviceSession, 0)
	for _, s := range d.sessions {
		if s.AccountID == account {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastUsed.After(out[j].LastUsed) })
	return out
}
func (d *SessionDirectory) RevokeAll(account, except string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for id, s := range d.sessions {
		if s.AccountID == account && id != except {
			delete(d.sessions, id)
			delete(d.secrets, id)
			n++
		}
	}
	return n
}
func (d *SessionDirectory) Sweep() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.clock()
	n := 0
	for id, s := range d.sessions {
		if (d.policy.AbsoluteTTL > 0 && now.Sub(s.Created) >= d.policy.AbsoluteTTL) || (d.policy.IdleTTL > 0 && now.Sub(s.LastUsed) >= d.policy.IdleTTL) {
			delete(d.sessions, id)
			delete(d.secrets, id)
			n++
		}
	}
	return n
}
