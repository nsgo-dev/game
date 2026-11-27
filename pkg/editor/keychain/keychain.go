// Package keychain stores secrets in the host OS keychain: macOS Keychain, the Freedesktop Secret Service on Linux, and Windows Credential Manager, with an in-memory Store for tests. It imports only the standard library.
package keychain

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// maxKeychainSecretBytes bounds the size of any credential secret stored via Store.
	maxKeychainSecretBytes = 64 << 10
	// keychainCommandTimeout bounds external desktop keychain CLI invocations so a locked keychain prompt cannot freeze the editor indefinitely.
	keychainCommandTimeout = 5 * time.Second
)

func validateKeychainKey(service, account string) (string, string, error) {
	for _, raw := range []string{service, account} {
		for _, r := range raw {
			if r < 0x20 || r == 0x7f || r == '"' || r == '\'' || r == '\\' {
				return "", "", errors.New("keychain: invalid characters in keychain service or account")
			}
		}
	}
	service = strings.TrimSpace(service)
	account = strings.TrimSpace(account)
	if service == "" || account == "" {
		return "", "", errors.New("keychain: keychain service and account must be non-empty")
	}
	if strings.HasPrefix(service, "-") || strings.HasPrefix(account, "-") {
		return "", "", errors.New("keychain: keychain service and account must not start with '-'")
	}
	return service, account, nil
}

func validateKeychainSecret(secret string) error {
	if secret == "" {
		return errors.New("keychain: keychain secret must be non-empty")
	}
	if strings.IndexByte(secret, 0) >= 0 {
		return errors.New("keychain: keychain secret must not contain NUL bytes")
	}
	if len(secret) > maxKeychainSecretBytes {
		return fmt.Errorf("keychain: keychain secret exceeds maximum size (%d bytes)", maxKeychainSecretBytes)
	}
	return nil
}

// ErrNotFound is returned by Store.Get when no secret is stored for the requested service and account.
var ErrNotFound = errors.New("keychain: credential not found in system keychain")

// ErrUnavailable is returned when the host platform does not expose a desktop system keychain.
var ErrUnavailable = errors.New("keychain: system keychain unavailable on this platform")

// Store stores, retrieves, and removes secrets in the host OS keychain (macOS Keychain, Linux Secret Service, Windows Credential Manager) or an in-memory test store. Secrets must never be written into project files.
type Store interface {
	Set(service, account, secret string) error
	Get(service, account string) (string, error)
	Delete(service, account string) error
}

// Default returns the platform's native system keychain implementation.
func Default() Store {
	return platformKeychain()
}

// Memory is a thread-safe in-memory Store implementation for tests and sandboxed tooling.
type Memory struct {
	mu      sync.RWMutex
	entries map[string]string
}

// NewMemory constructs an empty in-memory Store.
func NewMemory() *Memory {
	return &Memory{
		entries: make(map[string]string),
	}
}

func memoryKeychainKey(service, account string) string {
	return service + "\x00" + account
}

// Set stores secret under (service, account) in memory.
func (m *Memory) Set(service, account, secret string) error {
	if m == nil {
		return ErrUnavailable
	}
	var err error
	service, account, err = validateKeychainKey(service, account)
	if err != nil {
		return err
	}
	if err := validateKeychainSecret(secret); err != nil {
		return err
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
func (m *Memory) Get(service, account string) (string, error) {
	if m == nil {
		return "", ErrUnavailable
	}
	service, account, err := validateKeychainKey(service, account)
	if err != nil {
		return "", ErrNotFound
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.entries[memoryKeychainKey(service, account)]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// Delete removes any secret stored under (service, account) in memory.
func (m *Memory) Delete(service, account string) error {
	if m == nil {
		return ErrUnavailable
	}
	service, account, err := validateKeychainKey(service, account)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, memoryKeychainKey(service, account))
	return nil
}

// Len returns the number of entries currently stored in memory.
func (m *Memory) Len() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}
