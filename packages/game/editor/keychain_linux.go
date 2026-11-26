//go:build linux && !android

package editor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type linuxSecretServiceKeychain struct{}

func platformKeychain() Keychain {
	return linuxSecretServiceKeychain{}
}

// Set stores secret via Freedesktop Secret Service (`secret-tool store`), reading the secret from stdin.
func (linuxSecretServiceKeychain) Set(service, account, secret string) error {
	var err error
	service, account, err = validateKeychainKey(service, account)
	if err != nil {
		return err
	}
	if err := validateKeychainSecret(secret); err != nil {
		return err
	}
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return ErrKeychainUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), keychainCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "secret-tool", "store", "--label="+service+": "+account, "--", "service", service, "account", account)
	cmd.Stdin = strings.NewReader(secret)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("editor: secret-tool store failed: %s", msg)
		}
		return fmt.Errorf("editor: secret-tool store failed: %w", err)
	}
	return nil
}

// Get retrieves secret via Freedesktop Secret Service (`secret-tool lookup`).
func (linuxSecretServiceKeychain) Get(service, account string) (string, error) {
	var err error
	service, account, err = validateKeychainKey(service, account)
	if err != nil {
		return "", ErrCredentialNotFound
	}
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return "", ErrKeychainUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), keychainCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "secret-tool", "lookup", "--", "service", service, "account", account)
	cmd.Stdin = nil
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stdout.Len() == 0 && ctx.Err() == nil {
			return "", ErrCredentialNotFound
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("editor: secret-tool lookup failed: %s", msg)
		}
		return "", fmt.Errorf("editor: secret-tool lookup failed: %w", err)
	}
	val := strings.TrimSuffix(stdout.String(), "\n")
	if val == "" {
		return "", ErrCredentialNotFound
	}
	return val, nil
}

// Delete removes secret via Freedesktop Secret Service (`secret-tool clear`).
func (linuxSecretServiceKeychain) Delete(service, account string) error {
	var err error
	service, account, err = validateKeychainKey(service, account)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return ErrKeychainUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), keychainCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "secret-tool", "clear", "--", "service", service, "account", account)
	cmd.Stdin = nil
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("editor: secret-tool clear failed: %s", msg)
		}
		return fmt.Errorf("editor: secret-tool clear failed: %w", err)
	}
	return nil
}
