package liveops

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultMaxInvitePayloadBytes is the default maximum byte length of an encoded invite query string.
	// It fits comfortably within mini-game share query limits (such as WeChat's shareAppMessage query) as well as mobile deep-link and web URLs.
	DefaultMaxInvitePayloadBytes = 1024

	// MaxInvitePayloadKeys bounds the number of key-value pairs in a single invite payload (excluding the optional signature field).
	MaxInvitePayloadKeys = 32

	// MaxInviteKeyBytes bounds the length of an individual invite payload key in bytes.
	MaxInviteKeyBytes = 64

	// MaxInviteSignatureBytes bounds the length of an encoded signature field in bytes.
	MaxInviteSignatureBytes = 128

	// InviteIDKey is the standard payload key carrying the unique invite identifier used as a server reward-grant idempotency key.
	InviteIDKey = "id"

	// InviteSigKey is the reserved payload key carrying the optional base64url raw HMAC-SHA256 signature.
	InviteSigKey = "sig"
)

var (
	// ErrInvitePayloadTooLarge reports an invite payload whose encoded "key=value&..." representation exceeds the host or configured byte limit.
	ErrInvitePayloadTooLarge = errors.New("liveops: invite payload exceeds size limit")

	// ErrInvalidInvitePayload reports a malformed invite payload (invalid key, control character, duplicate key, bad percent-encoding, or too many keys).
	ErrInvalidInvitePayload = errors.New("liveops: invalid invite payload")

	// ErrInvalidInviteSignature reports an invite payload whose HMAC-SHA256 signature is missing (when required) or does not match.
	ErrInvalidInviteSignature = errors.New("liveops: invalid invite signature")

	// ErrMissingHMACKey reports InvitesOptions or InvitePayloadOptions with RequireSignature set but an empty HMACKey.
	ErrMissingHMACKey = errors.New("liveops: RequireSignature requires a non-empty HMACKey")

	// ErrInvalidInviteURL reports a malformed invite base URL or incoming link.
	ErrInvalidInviteURL = errors.New("liveops: invalid invite URL")

	// ErrInvalidCard reports a share card with invalid fields (such as conflicting InviteID/Payload["id"], control characters, or conflicting Signature/HMACKey).
	ErrInvalidCard = errors.New("liveops: invalid share card")

	// ErrShareUnavailable reports that the current platform or host does not support sharing.
	ErrShareUnavailable = errors.New("liveops: sharing is unavailable on this platform")
)

// InviteEncoding chooses where a link-based ShareHost places the encoded invite payload in a URL.
type InviteEncoding uint8

const (
	// InviteEncodeQuery places the encoded payload in the URL query component ("?k=v&...").
	InviteEncodeQuery InviteEncoding = iota
	// InviteEncodeFragment places the encoded payload in the URL fragment component ("#k=v&..."), avoiding server access logs and cache-key fragmentation on the web.
	InviteEncodeFragment
)

// String returns "query", "fragment" or "unknown".
func (e InviteEncoding) String() string {
	switch e {
	case InviteEncodeQuery:
		return "query"
	case InviteEncodeFragment:
		return "fragment"
	default:
		return "unknown"
	}
}

// InvitePayloadOptions configures EncodeInvitePayload and DecodeInvitePayload.
type InvitePayloadOptions struct {
	// MaxBytes caps the encoded "key=value&..." payload length in bytes.
	// Values <= 0 use DefaultMaxInvitePayloadBytes (1024).
	MaxBytes int
	// HMACKey, when non-empty, signs encoded payloads with HMAC-SHA256 under InviteSigKey ("sig")
	// and verifies decoded payloads in constant time.
	HMACKey []byte
	// RequireSignature, when true during decoding, rejects unsigned payloads with ErrInvalidInviteSignature.
	// Requires a non-empty HMACKey; otherwise DecodeInvitePayload returns ErrMissingHMACKey.
	RequireSignature bool
}

// DecodedInvitePayload is the validated result of DecodeInvitePayload or VerifyInvitePayload.
type DecodedInvitePayload struct {
	// ID is the value of InviteIDKey ("id") when present in Payload.
	ID string
	// Payload contains every decoded key-value pair except InviteSigKey ("sig"), preserving unknown keys.
	Payload map[string]string
	// Signature is the raw InviteSigKey ("sig") value if present in the input, empty otherwise.
	Signature string
	// Verified is true only when a non-empty HMACKey was provided and Signature matched Payload under HMAC-SHA256.
	Verified bool
}

// InviteLinkOptions configures how invite URLs are built and matched on native and web platforms.
type InviteLinkOptions struct {
	// BaseURL is the invite URL prefix (for example "mygame://invite" on mobile/desktop or "https://play.example.com/invite" on the web).
	// When non-empty, ExtractInviteQuery matches incoming URLs against BaseURL's scheme, host (case-insensitive) and path (ignoring trailing slashes).
	BaseURL string
	// Encoding selects query (default) or fragment encoding.
	Encoding InviteEncoding
	// AcceptFragment, when true with InviteEncodeQuery, falls back to reading the URL fragment when the query string is empty.
	AcceptFragment bool
}

// EncodeInvitePayload encodes payload as a deterministic, lexicographically sorted "key=value&..." string within opts.MaxBytes.
// When opts.HMACKey is non-empty, it appends a base64url raw HMAC-SHA256 signature under InviteSigKey ("sig") computed over the canonical unsigned query and checks the final signed length against opts.MaxBytes.
func EncodeInvitePayload(payload map[string]string, opts InvitePayloadOptions) (string, error) {
	return encodeInvitePayloadWithSig(payload, "", opts)
}

func encodeInvitePayloadWithSig(payload map[string]string, precomputedSig string, opts InvitePayloadOptions) (string, error) {
	maxBytes := effectivePayloadLimit(opts.MaxBytes)
	if precomputedSig != "" {
		if len(opts.HMACKey) > 0 {
			return "", fmt.Errorf("%w: precomputed signature cannot be combined with HMACKey", ErrInvalidCard)
		}
		if err := validateSignatureToken(precomputedSig); err != nil {
			return "", err
		}
	}

	keys := make([]string, 0, len(payload))
	for k, v := range payload {
		if k == InviteSigKey {
			if len(opts.HMACKey) > 0 {
				return "", fmt.Errorf("%w: reserved key %q cannot be set when HMACKey is configured", ErrInvalidInvitePayload, InviteSigKey)
			}
			if precomputedSig != "" {
				return "", fmt.Errorf("%w: conflicting signature in payload and Card.Signature", ErrInvalidCard)
			}
			if err := validateSignatureToken(v); err != nil {
				return "", err
			}
		} else {
			if err := validateInviteKey(k); err != nil {
				return "", err
			}
			if err := validateInviteValue(v); err != nil {
				return "", err
			}
		}
		keys = append(keys, k)
	}

	nonSigCount := len(keys)
	if _, hasSig := payload[InviteSigKey]; hasSig {
		nonSigCount--
	}
	if nonSigCount > MaxInvitePayloadKeys {
		return "", fmt.Errorf("%w: %d keys exceed limit %d", ErrInvalidInvitePayload, nonSigCount, MaxInvitePayloadKeys)
	}

	slices.Sort(keys)

	var sig string
	if len(opts.HMACKey) > 0 {
		canonical := formatSortedPairs(keys, payload)
		sig = computeInviteHMAC(canonical, opts.HMACKey)
	} else if precomputedSig != "" {
		sig = precomputedSig
	}

	var encoded string
	if sig != "" {
		allKeys := make([]string, len(keys)+1)
		copy(allKeys, keys)
		allKeys[len(keys)] = InviteSigKey
		slices.Sort(allKeys)
		withSig := make(map[string]string, len(payload)+1)
		maps.Copy(withSig, payload)
		withSig[InviteSigKey] = sig
		encoded = formatSortedPairs(allKeys, withSig)
	} else {
		encoded = formatSortedPairs(keys, payload)
	}

	if len(encoded) > maxBytes {
		return "", fmt.Errorf("%w: %d bytes exceed limit %d", ErrInvitePayloadTooLarge, len(encoded), maxBytes)
	}
	return encoded, nil
}

// DecodeInvitePayload parses, validates and optionally verifies an encoded "key=value&..." invite payload.
//
// Malformed percent-encoding, empty segments, missing "=" separators, duplicate keys, invalid key names, control characters, invalid UTF-8, excessive key counts and payloads exceeding opts.MaxBytes are rejected safely.
// Unknown keys that satisfy validation are preserved in DecodedInvitePayload.Payload.
//
// When opts.HMACKey is non-empty:
//   - a valid signature sets Verified = true;
//   - a present but mismatched signature is rejected with ErrInvalidInviteSignature;
//   - a missing signature is rejected with ErrInvalidInviteSignature when opts.RequireSignature is true, and otherwise returns Verified = false.
func DecodeInvitePayload(raw string, opts InvitePayloadOptions) (DecodedInvitePayload, error) {
	if opts.RequireSignature && len(opts.HMACKey) == 0 {
		return DecodedInvitePayload{}, ErrMissingHMACKey
	}
	maxBytes := effectivePayloadLimit(opts.MaxBytes)
	if len(raw) > maxBytes {
		return DecodedInvitePayload{}, fmt.Errorf("%w: %d bytes exceed limit %d", ErrInvitePayloadTooLarge, len(raw), maxBytes)
	}

	result := DecodedInvitePayload{
		Payload: make(map[string]string),
	}
	if raw == "" {
		if opts.RequireSignature {
			return DecodedInvitePayload{}, fmt.Errorf("%w: missing signature", ErrInvalidInviteSignature)
		}
		return result, nil
	}

	hasSig := false
	keys := make([]string, 0, 8)
	for part := range strings.SplitSeq(raw, "&") {
		if part == "" {
			return DecodedInvitePayload{}, fmt.Errorf("%w: empty query segment", ErrInvalidInvitePayload)
		}
		rawKey, rawVal, hasEq := strings.Cut(part, "=")
		if !hasEq {
			return DecodedInvitePayload{}, fmt.Errorf("%w: missing '=' in segment %q", ErrInvalidInvitePayload, part)
		}
		key, err := url.QueryUnescape(rawKey)
		if err != nil {
			return DecodedInvitePayload{}, fmt.Errorf("%w: bad key escape: %v", ErrInvalidInvitePayload, err)
		}
		val, err := url.QueryUnescape(rawVal)
		if err != nil {
			return DecodedInvitePayload{}, fmt.Errorf("%w: bad value escape for key %q: %v", ErrInvalidInvitePayload, key, err)
		}
		if key == InviteSigKey {
			if hasSig {
				return DecodedInvitePayload{}, fmt.Errorf("%w: duplicate key %q", ErrInvalidInvitePayload, InviteSigKey)
			}
			if err := validateSignatureToken(val); err != nil {
				return DecodedInvitePayload{}, err
			}
			hasSig = true
			result.Signature = val
			continue
		}
		if err := validateInviteKey(key); err != nil {
			return DecodedInvitePayload{}, err
		}
		if err := validateInviteValue(val); err != nil {
			return DecodedInvitePayload{}, err
		}
		if _, dup := result.Payload[key]; dup {
			return DecodedInvitePayload{}, fmt.Errorf("%w: duplicate key %q", ErrInvalidInvitePayload, key)
		}
		if len(result.Payload) >= MaxInvitePayloadKeys {
			return DecodedInvitePayload{}, fmt.Errorf("%w: more than %d keys", ErrInvalidInvitePayload, MaxInvitePayloadKeys)
		}
		result.Payload[key] = val
		keys = append(keys, key)
	}

	result.ID = result.Payload[InviteIDKey]

	if len(opts.HMACKey) > 0 {
		if !hasSig {
			if opts.RequireSignature {
				return DecodedInvitePayload{}, fmt.Errorf("%w: missing signature", ErrInvalidInviteSignature)
			}
			return result, nil
		}
		slices.Sort(keys)
		canonical := formatSortedPairs(keys, result.Payload)
		expected := computeInviteHMAC(canonical, opts.HMACKey)
		if !hmac.Equal([]byte(result.Signature), []byte(expected)) {
			return DecodedInvitePayload{}, fmt.Errorf("%w: signature mismatch", ErrInvalidInviteSignature)
		}
		result.Verified = true
	}

	return result, nil
}

// SignInvitePayload encodes payload and signs it with HMAC-SHA256 using hmacKey.
// It is intended for both game clients and game servers minting verifiable invite payloads.
func SignInvitePayload(payload map[string]string, hmacKey []byte, maxBytes int) (string, error) {
	if len(hmacKey) == 0 {
		return "", ErrMissingHMACKey
	}
	return EncodeInvitePayload(payload, InvitePayloadOptions{
		MaxBytes: maxBytes,
		HMACKey:  hmacKey,
	})
}

// VerifyInvitePayload decodes raw and requires a valid HMAC-SHA256 signature under hmacKey.
// A game server receiving Invite.RawQuery calls VerifyInvitePayload before granting invite rewards or honoring privileged invite payloads.
func VerifyInvitePayload(raw string, hmacKey []byte, maxBytes int) (DecodedInvitePayload, error) {
	return DecodeInvitePayload(raw, InvitePayloadOptions{
		MaxBytes:         maxBytes,
		HMACKey:          hmacKey,
		RequireSignature: true,
	})
}

// FormatInviteURL attaches rawQuery to baseURL according to encoding (InviteEncodeQuery or InviteEncodeFragment).
// baseURL must have a valid scheme and must not already carry a query (for InviteEncodeQuery) or fragment (for InviteEncodeFragment).
func FormatInviteURL(baseURL, rawQuery string, encoding InviteEncoding) (string, error) {
	u, err := parseInviteBaseURL(baseURL)
	if err != nil {
		return "", err
	}
	switch encoding {
	case InviteEncodeQuery:
		if u.RawQuery != "" {
			return "", fmt.Errorf("%w: base URL %q must not contain a query string", ErrInvalidInviteURL, baseURL)
		}
		u.RawQuery = rawQuery
	case InviteEncodeFragment:
		if u.Fragment != "" || u.RawFragment != "" {
			return "", fmt.Errorf("%w: base URL %q must not contain a fragment", ErrInvalidInviteURL, baseURL)
		}
		u.Fragment = rawQuery
	default:
		return "", fmt.Errorf("%w: unknown invite encoding %d", ErrInvalidInviteURL, encoding)
	}
	return u.String(), nil
}

// ExtractInviteQuery checks whether rawURL matches opts.BaseURL and extracts its raw invite payload string according to opts.Encoding.
// It returns matched == false with a nil error when rawURL is empty, uses a different scheme/host/path, or carries no payload.
func ExtractInviteQuery(rawURL string, opts InviteLinkOptions) (rawQuery string, matched bool, err error) {
	if rawURL == "" {
		return "", false, nil
	}
	if hasControlChars(rawURL) || !utf8.ValidString(rawURL) {
		return "", false, fmt.Errorf("%w: control character or invalid UTF-8 in URL", ErrInvalidInviteURL)
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" {
		return "", false, fmt.Errorf("%w: %q", ErrInvalidInviteURL, rawURL)
	}

	if opts.BaseURL != "" {
		base, err := parseInviteBaseURL(opts.BaseURL)
		if err != nil {
			return "", false, err
		}
		if !strings.EqualFold(u.Scheme, base.Scheme) {
			return "", false, nil
		}
		if !strings.EqualFold(u.Host, base.Host) {
			return "", false, nil
		}
		if normalizeURLPath(u.Path) != normalizeURLPath(base.Path) {
			return "", false, nil
		}
	}

	switch opts.Encoding {
	case InviteEncodeQuery:
		if u.RawQuery != "" {
			return u.RawQuery, true, nil
		}
		if opts.AcceptFragment {
			if frag := rawURLFragment(u); frag != "" {
				return frag, true, nil
			}
		}
		return "", false, nil
	case InviteEncodeFragment:
		if frag := rawURLFragment(u); frag != "" {
			return frag, true, nil
		}
		return "", false, nil
	default:
		return "", false, fmt.Errorf("%w: unknown invite encoding %d", ErrInvalidInviteURL, opts.Encoding)
	}
}

func rawURLFragment(u *url.URL) string {
	if u.RawFragment != "" {
		return u.RawFragment
	}
	return u.Fragment
}

func parseInviteBaseURL(raw string) (*url.URL, error) {
	if raw == "" || hasControlChars(raw) || strings.ContainsAny(raw, " \t\r\n") || !utf8.ValidString(raw) {
		return nil, fmt.Errorf("%w: invalid base URL %q", ErrInvalidInviteURL, raw)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return nil, fmt.Errorf("%w: base URL %q requires a scheme", ErrInvalidInviteURL, raw)
	}
	return u, nil
}

func normalizeURLPath(p string) string {
	return strings.TrimSuffix(p, "/")
}

func effectivePayloadLimit(n int) int {
	if n <= 0 {
		return DefaultMaxInvitePayloadBytes
	}
	return n
}

func formatSortedPairs(sortedKeys []string, values map[string]string) string {
	if len(sortedKeys) == 0 {
		return ""
	}
	var b strings.Builder
	for i, k := range sortedKeys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(values[k]))
	}
	return b.String()
}

func computeInviteHMAC(canonical string, key []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validateInviteKey(k string) error {
	if k == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidInvitePayload)
	}
	if len(k) > MaxInviteKeyBytes {
		return fmt.Errorf("%w: key %q exceeds %d bytes", ErrInvalidInvitePayload, k, MaxInviteKeyBytes)
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.'
		if !ok {
			return fmt.Errorf("%w: invalid character %q in key %q", ErrInvalidInvitePayload, c, k)
		}
	}
	return nil
}

func validateInviteValue(v string) error {
	if !utf8.ValidString(v) {
		return fmt.Errorf("%w: value is not valid UTF-8", ErrInvalidInvitePayload)
	}
	if hasControlChars(v) {
		return fmt.Errorf("%w: value contains control characters", ErrInvalidInvitePayload)
	}
	return nil
}

func validateSignatureToken(sig string) error {
	if sig == "" || len(sig) > MaxInviteSignatureBytes {
		return fmt.Errorf("%w: invalid signature length", ErrInvalidInviteSignature)
	}
	for i := 0; i < len(sig); i++ {
		c := sig[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			return fmt.Errorf("%w: invalid signature character %q", ErrInvalidInviteSignature, c)
		}
	}
	return nil
}

func hasControlChars(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}
