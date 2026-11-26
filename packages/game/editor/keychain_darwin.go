//go:build darwin && !ios

package editor

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
)

type darwinKeychain struct{}

func platformKeychain() Keychain {
	return darwinKeychain{}
}

// Set stores a generic password in the macOS login keychain using `security -i` over stdin
// with hex-encoded secret (`-X`) so the secret never appears in command-line arguments.
func (darwinKeychain) Set(service, account, secret string) error {
	var err error
	service, account, err = validateKeychainKey(service, account)
	if err != nil {
		return err
	}
	if err := validateKeychainSecret(secret); err != nil {
		return err
	}
	hexSecret := hex.EncodeToString([]byte(secret))
	script := fmt.Sprintf("add-generic-password -U -s %q -a %q -X %s\n", service, account, hexSecret)

	ctx, cancel := context.WithTimeout(context.Background(), keychainCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "-i")
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("editor: macOS keychain set failed: %s", msg)
		}
		return fmt.Errorf("editor: macOS keychain set failed: %w", err)
	}
	return nil
}

// Get reads a generic password from the macOS login keychain.
func (darwinKeychain) Get(service, account string) (string, error) {
	var err error
	service, account, err = validateKeychainKey(service, account)
	if err != nil {
		return "", ErrCredentialNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), keychainCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", service, "-a", account, "-w")
	cmd.Stdin = nil
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(msg, "could not be found") || strings.Contains(msg, "SecKeychainSearchCopyNext") {
			return "", ErrCredentialNotFound
		}
		if msg != "" {
			return "", fmt.Errorf("editor: macOS keychain get failed: %s", msg)
		}
		return "", fmt.Errorf("editor: macOS keychain get failed: %w", err)
	}
	raw := strings.TrimSuffix(stdout.String(), "\n")
	return decodeDarwinKeychainPayload(raw), nil
}

// Delete removes a generic password from the macOS login keychain.
func (darwinKeychain) Delete(service, account string) error {
	var err error
	service, account, err = validateKeychainKey(service, account)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), keychainCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "delete-generic-password", "-s", service, "-a", account)
	cmd.Stdin = nil
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(msg, "could not be found") {
			return nil
		}
		if msg != "" {
			return fmt.Errorf("editor: macOS keychain delete failed: %s", msg)
		}
		return fmt.Errorf("editor: macOS keychain delete failed: %w", err)
	}
	return nil
}

// decodeDarwinKeychainPayload decodes 0x-prefixed hex dumps emitted by `security find-generic-password -w`
// when a stored generic password contains non-printable or multiline bytes, returning plain payloads unchanged.
func decodeDarwinKeychainPayload(raw string) string {
	var hexPart string
	switch {
	case strings.HasPrefix(raw, "0x"):
		hexPart = raw[2:]
	case strings.HasPrefix(raw, "0X"):
		hexPart = raw[2:]
	default:
		return raw
	}
	if len(hexPart) == 0 || len(hexPart)%2 != 0 {
		return raw
	}
	decoded, err := hex.DecodeString(hexPart)
	if err != nil {
		return raw
	}
	return string(decoded)
}
