//go:build darwin && !ios

package editor

import (
	"bytes"
	"encoding/hex"
	"errors"
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
	service = strings.TrimSpace(service)
	account = strings.TrimSpace(account)
	if service == "" || account == "" {
		return errors.New("editor: keychain service and account must be non-empty")
	}
	if secret == "" {
		return errors.New("editor: keychain secret must be non-empty")
	}
	if strings.ContainsAny(service, "\r\n\"") || strings.ContainsAny(account, "\r\n\"") {
		return errors.New("editor: invalid characters in keychain service or account")
	}
	hexSecret := hex.EncodeToString([]byte(secret))
	script := fmt.Sprintf("add-generic-password -U -s %q -a %q -X %s\n", service, account, hexSecret)

	cmd := exec.Command("/usr/bin/security", "-i")
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
	service = strings.TrimSpace(service)
	account = strings.TrimSpace(account)
	if service == "" || account == "" {
		return "", ErrCredentialNotFound
	}
	cmd := exec.Command("/usr/bin/security", "find-generic-password", "-s", service, "-a", account, "-w")
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
	if decoded, err := hex.DecodeString(raw); err == nil && len(raw)%2 == 0 && len(raw) > 0 && isLikelyHexPayload(raw) {
		return string(decoded), nil
	}
	return raw, nil
}

// Delete removes a generic password from the macOS login keychain.
func (darwinKeychain) Delete(service, account string) error {
	service = strings.TrimSpace(service)
	account = strings.TrimSpace(account)
	if service == "" || account == "" {
		return nil
	}
	cmd := exec.Command("/usr/bin/security", "delete-generic-password", "-s", service, "-a", account)
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

func isLikelyHexPayload(s string) bool {
	// `security add-generic-password -X` stores raw bytes, and `find-generic-password -w`
	// returns plain UTF-8 when printable or hex when non-UTF-8; check only if the caller
	// stored binary or security emitted a 0x-prefixed hex dump.
	return strings.HasPrefix(s, "0x")
}
