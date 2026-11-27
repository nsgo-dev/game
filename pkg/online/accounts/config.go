package accounts

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/pkg/online/internal/cors"
)

// ConfigVersion is the configuration file schema version this package reads and writes.
const ConfigVersion = 1

// Config is the account service's settings, kept in a YAML file:
//
//	version: 1
//	issuer: https://accounts.example.com
//	audience: my-game
//	tokenTTL: 15m
//	password:
//	  minLength: 10
//	  argon2id:
//	    time: 2
//	    memoryKiB: 19456
//	    threads: 1
//	allowedOrigins:
//	  - https://play.example.com
//
// Unknown fields are refused. Marshal writes the canonical form: the fields in this order, durations in their shortest text, disableGuests only when true and allowedOrigins only when it has entries. A file in that form, such as one Marshal wrote, reads back and writes again unchanged; a hand-written file in another order or spelling is normalized.
type Config struct {
	// Version is the schema version; it must be ConfigVersion.
	Version int `yaml:"version"`
	// Issuer names the service in its tokens, usually its URL.
	Issuer string `yaml:"issuer"`
	// Audience names the game the tokens are for.
	Audience string `yaml:"audience"`
	// TokenTTL is how long a token stays valid, in whole seconds from 1s to 24h.
	TokenTTL Duration `yaml:"tokenTTL"`
	// DisableGuests refuses CreateGuest, so every account starts with an email address or an identity.
	DisableGuests bool `yaml:"disableGuests,omitempty"`
	// Password sets the password rules and hash cost.
	Password PasswordConfig `yaml:"password"`
	// AllowedOrigins lists the web origins ("https://play.example.com") whose pages may call the service from a browser. The handler answers their CORS preflight requests; other origins get no CORS headers, so browsers refuse their requests. Native clients send no origin and need no entry.
	AllowedOrigins []string `yaml:"allowedOrigins,omitempty"`
}

// PasswordConfig is the password rules and the argon2id cost.
type PasswordConfig struct {
	// MinLength is the fewest characters a password may have, at least 8.
	MinLength int `yaml:"minLength"`
	// Argon2id is the hash cost of new and rehashed passwords.
	Argon2id Argon2Params `yaml:"argon2id"`
}

// MaxPasswordBytes is the longest password, in bytes, the service hashes.
const MaxPasswordBytes = 1024

// DefaultConfig returns a configuration for issuer and audience with a 15-minute token TTL, guests allowed, passwords of at least 10 characters and OWASP's recommended argon2id cost (19 MiB, 2 passes, 1 lane).
func DefaultConfig(issuer, audience string) Config {
	return Config{
		Version:  ConfigVersion,
		Issuer:   issuer,
		Audience: audience,
		TokenTTL: Duration(15 * time.Minute),
		Password: PasswordConfig{MinLength: 10, Argon2id: DefaultArgon2Params()},
	}
}

// Validate checks that c is a complete version 1 configuration.
func (c Config) Validate() error {
	if c.Version != ConfigVersion {
		return fmt.Errorf("accounts: unsupported config version %d (want %d)", c.Version, ConfigVersion)
	}
	if c.Issuer == "" || c.Audience == "" {
		return errors.New("accounts: config needs an issuer and an audience")
	}
	ttl := time.Duration(c.TokenTTL)
	if ttl < time.Second || ttl > 24*time.Hour || ttl%time.Second != 0 {
		return fmt.Errorf("accounts: tokenTTL %v must be whole seconds from 1s to 24h", ttl)
	}
	if c.Password.MinLength < 8 || c.Password.MinLength > MaxPasswordBytes {
		return fmt.Errorf("accounts: password minLength %d must be from 8 to %d", c.Password.MinLength, MaxPasswordBytes)
	}
	if err := c.Password.Argon2id.Validate(); err != nil {
		return err
	}
	if err := cors.ValidateOrigins(c.AllowedOrigins); err != nil {
		return fmt.Errorf("accounts: %w", err)
	}
	return nil
}

// ParseConfig decodes and validates a YAML configuration. Unknown fields and trailing documents are refused.
func ParseConfig(data []byte) (Config, error) {
	var c Config
	if len(bytes.TrimSpace(data)) == 0 {
		return Config{}, errors.New("accounts: empty config")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("accounts: config: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, errors.New("accounts: config: unexpected trailing YAML document")
		}
		return Config{}, fmt.Errorf("accounts: config: %w", err)
	}
	if len(c.AllowedOrigins) == 0 {
		c.AllowedOrigins = nil
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Marshal validates c and encodes it as deterministic 2-space YAML.
func (c Config) Marshal() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("accounts: config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("accounts: config: %w", err)
	}
	return buf.Bytes(), nil
}

// Duration is a time.Duration written in YAML as Go duration text ("15m", "1h30m").
type Duration time.Duration

// MarshalYAML writes d as its shortest Go duration text: "15m" rather than "15m0s".
func (d Duration) MarshalYAML() (any, error) {
	s := time.Duration(d).String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s, nil
}

// UnmarshalYAML reads Go duration text.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" {
		return fmt.Errorf("line %d: want a duration such as 15m", n.Line)
	}
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*d = Duration(v)
	return nil
}
