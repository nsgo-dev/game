package editor

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// MiniProgramPreflight contains the non-secret inputs checked before a
// developer-tool upload starts. The upload key itself is never returned in a
// readiness item or error.
type MiniProgramPreflight struct {
	Provider    string
	AppID       string
	Version     string
	UploadKey   string
	RemoteIP    string
	IPAllowlist []string
}

// ValidateMiniProgramPreflight validates app identity, version, upload key,
// and optional source-IP restrictions before an upload reaches a provider.
func ValidateMiniProgramPreflight(p MiniProgramPreflight) error {
	provider := strings.ToLower(strings.TrimSpace(p.Provider))
	if provider != "wechat" && provider != "douyin" {
		return fmt.Errorf("editor: unsupported mini-program provider %q", p.Provider)
	}
	if strings.TrimSpace(p.AppID) == "" || strings.ContainsAny(p.AppID, "\r\n \t") {
		return errors.New("editor: mini-program app id is required")
	}
	version := strings.TrimSpace(p.Version)
	if version == "" || strings.HasPrefix(version, "-") || strings.ContainsAny(version, "\r\n \t") {
		return errors.New("editor: mini-program version is required")
	}
	if len([]byte(p.UploadKey)) < 16 {
		return errors.New("editor: mini-program upload key is too short")
	}
	if strings.TrimSpace(p.RemoteIP) == "" || len(p.IPAllowlist) == 0 {
		return nil
	}
	remote, err := netip.ParseAddr(strings.TrimSpace(p.RemoteIP))
	if err != nil {
		return fmt.Errorf("editor: invalid mini-program source IP: %w", err)
	}
	for _, raw := range p.IPAllowlist {
		prefix, parseErr := netip.ParsePrefix(strings.TrimSpace(raw))
		if parseErr == nil && prefix.Contains(remote) {
			return nil
		}
	}
	return errors.New("editor: mini-program source IP is outside the configured allowlist")
}
