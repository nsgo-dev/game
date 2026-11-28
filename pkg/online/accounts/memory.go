package accounts

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

// MemoryStore is a Store held in memory, for tests and single-process prototypes. Its records are lost when the process ends.
type MemoryStore struct {
	mu         sync.Mutex
	accounts   map[string]memoryAccount
	emails     map[string]string   // email -> account ID
	identities map[Identity]string // identity -> account ID
	sessions   map[string]Session
}

type memoryAccount struct {
	a            Account
	passwordHash string
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		accounts:   map[string]memoryAccount{},
		emails:     map[string]string{},
		identities: map[Identity]string{},
		sessions:   map[string]Session{},
	}
}

// CreateAccount implements Store.
func (m *MemoryStore) CreateAccount(_ context.Context, a Account, passwordHash string, identities ...Identity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[a.ID]; ok {
		return ErrTaken
	}
	if a.Email != "" {
		if _, ok := m.emails[a.Email]; ok {
			return ErrTaken
		}
	}
	providers := map[string]bool{}
	for _, id := range identities {
		if _, ok := m.identities[id]; ok {
			return ErrTaken
		}
		if providers[id.Provider] {
			return ErrTaken
		}
		providers[id.Provider] = true
	}
	m.accounts[a.ID] = memoryAccount{a: a, passwordHash: passwordHash}
	if a.Email != "" {
		m.emails[a.Email] = a.ID
	}
	for _, id := range identities {
		m.identities[id] = a.ID
	}
	return nil
}

// Account implements Store.
func (m *MemoryStore) Account(_ context.Context, id string) (Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.accounts[id]
	if !ok {
		return Account{}, ErrNotFound
	}
	return rec.a, nil
}

// AccountByEmail implements Store.
func (m *MemoryStore) AccountByEmail(_ context.Context, email string) (Account, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.emails[email]
	if !ok {
		return Account{}, "", ErrNotFound
	}
	rec := m.accounts[id]
	return rec.a, rec.passwordHash, nil
}

// SetEmail implements Store.
func (m *MemoryStore) SetEmail(_ context.Context, id, email, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.accounts[id]
	if !ok {
		return ErrNotFound
	}
	if email == "" {
		return ErrInvalidEmail
	}
	if rec.a.Email != "" {
		return ErrAlreadyLinked
	}
	if _, ok := m.emails[email]; ok {
		return ErrTaken
	}
	rec.a.Email = email
	rec.passwordHash = passwordHash
	m.accounts[id] = rec
	m.emails[email] = id
	return nil
}

// SetPasswordHash implements Store.
func (m *MemoryStore) SetPasswordHash(_ context.Context, id, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.accounts[id]
	if !ok || rec.a.Email == "" {
		return ErrNotFound
	}
	rec.passwordHash = passwordHash
	m.accounts[id] = rec
	return nil
}

// LinkIdentity implements Store.
func (m *MemoryStore) LinkIdentity(_ context.Context, id string, identity Identity, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[id]; !ok {
		return ErrNotFound
	}
	if owner, ok := m.identities[identity]; ok {
		if owner == id {
			return nil
		}
		return ErrTaken
	}
	for other, owner := range m.identities {
		if owner == id && other.Provider == identity.Provider {
			return ErrAlreadyLinked
		}
	}
	m.identities[identity] = id
	return nil
}

// UnlinkIdentity implements Store.
func (m *MemoryStore) UnlinkIdentity(_ context.Context, id, provider string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[id]; !ok {
		return ErrNotFound
	}
	for identity, owner := range m.identities {
		if owner == id && identity.Provider == provider {
			delete(m.identities, identity)
			return nil
		}
	}
	return ErrNotFound
}

// AccountByIdentity implements Store.
func (m *MemoryStore) AccountByIdentity(_ context.Context, identity Identity) (Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.identities[identity]
	if !ok {
		return Account{}, ErrNotFound
	}
	return m.accounts[id].a, nil
}

// Identities implements Store.
func (m *MemoryStore) Identities(_ context.Context, id string) ([]Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Identity
	for identity, owner := range m.identities {
		if owner == id {
			out = append(out, identity)
		}
	}
	slices.SortFunc(out, func(a, b Identity) int { return strings.Compare(a.Provider, b.Provider) })
	return out, nil
}

// CreateSession implements Store.
func (m *MemoryStore) CreateSession(_ context.Context, s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[s.AccountID]; !ok {
		return ErrNotFound
	}
	if _, ok := m.sessions[s.ID]; ok {
		return ErrTaken
	}
	m.sessions[s.ID] = s
	return nil
}

// Session implements Store.
func (m *MemoryStore) Session(_ context.Context, id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	return s, nil
}

// DeleteSession implements Store.
func (m *MemoryStore) DeleteSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; !ok {
		return ErrNotFound
	}
	delete(m.sessions, id)
	return nil
}
