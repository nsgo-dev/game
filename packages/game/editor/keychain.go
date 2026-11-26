package editor

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// KeychainServiceName is the system keychain service identifier used to store release upload credentials
// outside any game project directory.
const KeychainServiceName = "dev.nsgo.game.publish"

// ErrCredentialNotFound is returned by Keychain.Get when no credential is stored for the requested account.
var ErrCredentialNotFound = errors.New("editor: credential not found in system keychain")

// ErrKeychainUnavailable is returned when the host platform does not expose a desktop system keychain.
var ErrKeychainUnavailable = errors.New("editor: system keychain unavailable on this platform")

// Keychain stores, retrieves, and removes store upload credentials in the host OS keychain
// (macOS Keychain, Linux Secret Service, Windows Credential Manager) or an in-memory test store.
// Credentials must never be written into project files.
type Keychain interface {
	Set(service, account, secret string) error
	Get(service, account string) (string, error)
	Delete(service, account string) error
}

// KeychainCredential holds a store upload credential retrieved from the system keychain just before upload.
// Its fields carry `yaml:"-" json:"-"` and redacted String/GoString methods so secrets are never serialized
// into project files, snapshots, or logs.
type KeychainCredential struct {
	Destination PublishDestination `yaml:"-" json:"-"`
	Account     string             `yaml:"-" json:"-"`
	Secret      string             `yaml:"-" json:"-"`
}

// String redacts the credential secret for safe logging.
func (c KeychainCredential) String() string {
	return fmt.Sprintf("KeychainCredential{Destination:%q, Account:%q, Secret:[REDACTED]}", c.Destination, c.Account)
}

// GoString redacts the credential secret for %#v formatting.
func (c KeychainCredential) GoString() string {
	return c.String()
}

// DefaultKeychain returns the platform's native system keychain implementation.
func DefaultKeychain() Keychain {
	return platformKeychain()
}

// MemoryKeychain is a thread-safe in-memory Keychain implementation for tests and sandboxed tooling.
type MemoryKeychain struct {
	mu      sync.RWMutex
	entries map[string]string
}

// NewMemoryKeychain constructs an empty in-memory Keychain.
func NewMemoryKeychain() *MemoryKeychain {
	return &MemoryKeychain{
		entries: make(map[string]string),
	}
}

func memoryKeychainKey(service, account string) string {
	return service + "\x00" + account
}

// Set stores secret under (service, account) in memory.
func (m *MemoryKeychain) Set(service, account, secret string) error {
	if m == nil {
		return ErrKeychainUnavailable
	}
	service = strings.TrimSpace(service)
	account = strings.TrimSpace(account)
	if service == "" || account == "" {
		return errors.New("editor: keychain service and account must be non-empty")
	}
	if secret == "" {
		return errors.New("editor: keychain secret must be non-empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = make(map[string]string)
	}
	m.entries[memoryKeychainKey(service, account)] = secret
	return nil
}

// Get retrieves the secret stored under (service, account) in memory.
func (m *MemoryKeychain) Get(service, account string) (string, error) {
	if m == nil {
		return "", ErrKeychainUnavailable
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.entries[memoryKeychainKey(strings.TrimSpace(service), strings.TrimSpace(account))]
	if !ok {
		return "", ErrCredentialNotFound
	}
	return v, nil
}

// Delete removes any secret stored under (service, account) in memory.
func (m *MemoryKeychain) Delete(service, account string) error {
	if m == nil {
		return ErrKeychainUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, memoryKeychainKey(strings.TrimSpace(service), strings.TrimSpace(account)))
	return nil
}

// Len returns the number of entries currently stored in memory.
func (m *MemoryKeychain) Len() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}
