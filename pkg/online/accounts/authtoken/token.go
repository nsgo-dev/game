// Package authtoken issues and verifies the short-lived player tokens of the accounts service.
//
// A token is a compact JWT (RFC 7519) signed with Ed25519 ("alg": "EdDSA"). The account service holds the private key and issues tokens with a Signer; a game server holds only the public keys, as a KeySet loaded from a YAML file, and checks tokens with a Verifier. Verifying makes no network call: it decodes the token, checks the signature against a known key, then the issuer, audience and expiry.
//
// The audience names the one consumer a token is for. A key set's audience is the game's servers, which a player sends a token to when joining a match; a service that acts on the player's behalf, such as cloud save or a leaderboard, verifies tokens for its own audience with NewVerifierFor, so a token handed to a game server cannot be replayed against it.
//
// The package depends on the standard library and go.yaml.in/yaml/v3 only, so a game server that verifies tokens links none of the account service's password hashing or database drivers.
package authtoken

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// MaxLength is the longest token Verify decodes; a longer string is refused before any work.
const MaxLength = 4096

var (
	// ErrMalformed reports a token that is not a well-formed token of this package.
	ErrMalformed = errors.New("authtoken: malformed")
	// ErrUnknownKey reports a token signed with a key ID the verifier does not hold.
	ErrUnknownKey = errors.New("authtoken: unknown signing key")
	// ErrSignature reports a token whose signature does not match its contents.
	ErrSignature = errors.New("authtoken: bad signature")
	// ErrIssuer reports a token from another issuer.
	ErrIssuer = errors.New("authtoken: wrong issuer")
	// ErrAudience reports a token meant for another audience.
	ErrAudience = errors.New("authtoken: wrong audience")
	// ErrExpired reports a token past its expiry.
	ErrExpired = errors.New("authtoken: expired")
	// ErrNotYetValid reports a token issued after the verifier's current time.
	ErrNotYetValid = errors.New("authtoken: not yet valid")
)

// b64 is the strict unpadded base64url encoding of JWTs; Strict refuses non-canonical trailing bits, so one token has one spelling.
var b64 = base64.RawURLEncoding.Strict()

// Claims are what a verified token says.
type Claims struct {
	// Issuer is the account service that issued the token.
	Issuer string
	// Subject is the account ID the token stands for.
	Subject string
	// Audience is the consumer the token is meant for: the game's servers (the key set's audience) or a service such as cloud save.
	Audience string
	// IssuedAt is when the token was issued, to the second.
	IssuedAt time.Time
	// Expires is when the token stops being valid, to the second.
	Expires time.Time
	// ID is the token's unique random ID.
	ID string
	// KeyID is the ID of the key that signed the token.
	KeyID string
}

// header is a token's JOSE header.
type header struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

// claims is a token's payload in JWT's registered claim names.
type claims struct {
	Iss string `json:"iss"`
	Sub string `json:"sub"`
	Aud string `json:"aud"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
	Jti string `json:"jti"`
}

// KeyID returns the ID of a public key: the first 8 bytes of its SHA-256 in hex. Signers and key sets derive IDs this way, so an ID always names one key.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// Signer issues tokens with one private key. Its fields other than Now are fixed by NewSigner.
type Signer struct {
	key      ed25519.PrivateKey
	keyID    string
	issuer   string
	audience string
	ttl      time.Duration
	// Now is the clock tokens are issued by; nil uses time.Now.
	Now func() time.Time
	// Rand fills token IDs; nil uses crypto/rand. Issue reads it without a lock, so a signer used from several goroutines needs a reader that is safe for concurrent use.
	Rand io.Reader
}

// NewSigner returns a signer that issues tokens for audience from issuer, each valid for ttl (at least one second, in whole seconds).
func NewSigner(key ed25519.PrivateKey, issuer, audience string, ttl time.Duration) (*Signer, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("authtoken: signer needs an Ed25519 private key")
	}
	if issuer == "" || audience == "" {
		return nil, errors.New("authtoken: signer needs an issuer and an audience")
	}
	if ttl < time.Second || ttl%time.Second != 0 {
		return nil, fmt.Errorf("authtoken: ttl %v must be a whole number of seconds, at least 1s", ttl)
	}
	pub := key.Public().(ed25519.PublicKey)
	return &Signer{key: key, keyID: KeyID(pub), issuer: issuer, audience: audience, ttl: ttl}, nil
}

// TTL is how long the signer's tokens stay valid.
func (s *Signer) TTL() time.Duration { return s.ttl }

// KeyID is the ID of the signer's key.
func (s *Signer) KeyID() string { return s.keyID }

// KeySet returns the public key set that verifies the signer's tokens.
func (s *Signer) KeySet() *KeySet {
	pub := s.key.Public().(ed25519.PublicKey)
	return &KeySet{
		Version:  KeySetVersion,
		Issuer:   s.issuer,
		Audience: s.audience,
		Keys:     []PublicKey{{ID: s.keyID, Key: base64.StdEncoding.EncodeToString(pub)}},
	}
}

// Issue returns a token for subject and the signer's audience, valid from now for the signer's TTL, and its claims.
func (s *Signer) Issue(subject string) (string, Claims, error) {
	return s.IssueFor(subject, s.audience)
}

// IssueFor returns a token for subject meant for audience instead of the signer's own, valid from now for the signer's TTL, and its claims. Only a verifier built for that audience (NewVerifierFor) accepts it.
func (s *Signer) IssueFor(subject, audience string) (string, Claims, error) {
	if subject == "" {
		return "", Claims{}, errors.New("authtoken: empty subject")
	}
	if audience == "" {
		return "", Claims{}, errors.New("token: empty audience")
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	r := s.Rand
	if r == nil {
		r = rand.Reader
	}
	var id [16]byte
	if _, err := io.ReadFull(r, id[:]); err != nil {
		return "", Claims{}, fmt.Errorf("authtoken: id: %w", err)
	}
	iat := now().Unix()
	c := claims{Iss: s.issuer, Sub: subject, Aud: audience, Iat: iat, Exp: iat + int64(s.ttl/time.Second), Jti: b64.EncodeToString(id[:])}
	h, err := json.Marshal(header{Alg: "EdDSA", Kid: s.keyID, Typ: "JWT"})
	if err != nil {
		return "", Claims{}, err
	}
	p, err := json.Marshal(c)
	if err != nil {
		return "", Claims{}, err
	}
	signed := b64.EncodeToString(h) + "." + b64.EncodeToString(p)
	sig := ed25519.Sign(s.key, []byte(signed))
	tok := signed + "." + b64.EncodeToString(sig)
	if len(tok) > MaxLength {
		return "", Claims{}, fmt.Errorf("authtoken: %d bytes is longer than %d", len(tok), MaxLength)
	}
	return tok, c.public(s.keyID), nil
}

func (c claims) public(kid string) Claims {
	return Claims{Issuer: c.Iss, Subject: c.Sub, Audience: c.Aud, IssuedAt: time.Unix(c.Iat, 0).UTC(), Expires: time.Unix(c.Exp, 0).UTC(), ID: c.Jti, KeyID: kid}
}

// Verifier checks tokens offline against a key set. Build one with NewVerifier; it is safe for concurrent use.
type Verifier struct {
	keys     map[string]ed25519.PublicKey
	issuer   string
	audience string
	// Leeway tolerates clock skew between the account service and the verifier: a token stays valid until Leeway past its expiry and is accepted up to Leeway before its issue time.
	Leeway time.Duration
	// Now is the clock tokens are checked against; nil uses time.Now.
	Now func() time.Time
}

// NewVerifier returns a verifier for the keys, issuer and audience of ks: the one a game server uses.
func NewVerifier(ks *KeySet) (*Verifier, error) {
	if err := ks.Validate(); err != nil {
		return nil, err
	}
	return NewVerifierFor(ks, ks.Audience)
}

// NewVerifierFor returns a verifier for the keys and issuer of ks that accepts only tokens for audience, such as the one a cloud save service verifies. It refuses tokens for the key set's own audience unless audience is that one.
func NewVerifierFor(ks *KeySet, audience string) (*Verifier, error) {
	if err := ks.Validate(); err != nil {
		return nil, err
	}
	if audience == "" {
		return nil, errors.New("token: verifier needs an audience")
	}
	v := &Verifier{keys: make(map[string]ed25519.PublicKey, len(ks.Keys)), issuer: ks.Issuer, audience: audience}
	for _, k := range ks.Keys {
		pub, _ := k.publicKey() // Validate checked it.
		v.keys[k.ID] = pub
	}
	return v, nil
}

// Audience is the audience the verifier accepts.
func (v *Verifier) Audience() string { return v.audience }

// Verify checks tok and returns its claims. Errors wrap ErrMalformed, ErrUnknownKey, ErrSignature, ErrIssuer, ErrAudience, ErrExpired or ErrNotYetValid.
func (v *Verifier) Verify(tok string) (Claims, error) {
	if len(tok) > MaxLength {
		return Claims{}, fmt.Errorf("%w: longer than %d bytes", ErrMalformed, MaxLength)
	}
	// base64 decoding skips '\r' and '\n', so without this check a line break in the signature would give one token a second spelling.
	for i := 0; i < len(tok); i++ {
		if c := tok[i]; !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return Claims{}, fmt.Errorf("%w: invalid character", ErrMalformed)
		}
	}
	first := strings.IndexByte(tok, '.')
	last := strings.LastIndexByte(tok, '.')
	if first < 0 || first == last || strings.IndexByte(tok[first+1:last], '.') >= 0 {
		return Claims{}, fmt.Errorf("%w: want three parts", ErrMalformed)
	}
	var h header
	if err := decodePart(tok[:first], &h); err != nil {
		return Claims{}, fmt.Errorf("%w: header: %v", ErrMalformed, err)
	}
	if h.Alg != "EdDSA" || h.Typ != "JWT" {
		return Claims{}, fmt.Errorf("%w: alg %q typ %q", ErrMalformed, h.Alg, h.Typ)
	}
	sig, err := b64.DecodeString(tok[last+1:])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return Claims{}, fmt.Errorf("%w: signature encoding", ErrMalformed)
	}
	pub, ok := v.keys[h.Kid]
	if !ok {
		return Claims{}, ErrUnknownKey
	}
	if !ed25519.Verify(pub, []byte(tok[:last]), sig) {
		return Claims{}, ErrSignature
	}
	var c claims
	if err := decodePart(tok[first+1:last], &c); err != nil {
		return Claims{}, fmt.Errorf("%w: claims: %v", ErrMalformed, err)
	}
	if c.Sub == "" || c.Jti == "" || c.Exp <= c.Iat {
		return Claims{}, fmt.Errorf("%w: incomplete claims", ErrMalformed)
	}
	if subtle.ConstantTimeCompare([]byte(c.Iss), []byte(v.issuer)) != 1 {
		return Claims{}, ErrIssuer
	}
	if subtle.ConstantTimeCompare([]byte(c.Aud), []byte(v.audience)) != 1 {
		return Claims{}, ErrAudience
	}
	now := time.Now
	if v.Now != nil {
		now = v.Now
	}
	t := now()
	if !t.Before(time.Unix(c.Exp, 0).Add(v.Leeway)) {
		return Claims{}, ErrExpired
	}
	if t.Add(v.Leeway).Before(time.Unix(c.Iat, 0)) {
		return Claims{}, ErrNotYetValid
	}
	return c.public(h.Kid), nil
}

// decodePart decodes one base64url JSON part into v, refusing unknown fields and trailing data.
func decodePart(part string, v any) error {
	raw, err := b64.DecodeString(part)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	// A second value, or a stray closing brace or bracket, is trailing data; More alone misses the stray one.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}
