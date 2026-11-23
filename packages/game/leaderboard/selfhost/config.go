package selfhost

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/packages/game/accounts/token"
)

// ConfigVersion is the configuration file schema version this package reads and writes.
const ConfigVersion = 1

// MaxSafeScore is the largest score magnitude a board accepts, 2^53-1, so a score survives a JSON number in every client, JavaScript's included.
const MaxSafeScore = 1<<53 - 1

// DefaultProofTTL is how old a game server's score proof may be when the configuration sets no proofTTL.
const DefaultProofTTL = 10 * time.Minute

// MaxKeep is the most earlier periods a resetting board keeps readable.
const MaxKeep = 1000

// Order is the direction a board ranks scores in.
type Order string

// The orders a board ranks in.
const (
	// Descending ranks the highest score first, as for points.
	Descending Order = "descending"
	// Ascending ranks the lowest score first, as for a race time.
	Ascending Order = "ascending"
)

// Reset is how often a board starts over.
type Reset string

// The reset periods. Periods are in UTC: a daily board starts over at midnight UTC, a weekly one at midnight UTC on its weekStart day.
const (
	// AllTime never resets.
	AllTime Reset = "allTime"
	// Daily starts over every day at midnight UTC.
	Daily Reset = "daily"
	// Weekly starts over every week at midnight UTC on the board's weekStart day.
	Weekly Reset = "weekly"
)

// Validation is how a board checks where a score came from.
type Validation string

// SignedByServer accepts a score only with a proof signed by one of the configuration's serverKeys, which the game server issues for the match result or replay it checked.
const SignedByServer Validation = "signed"

// Config is the leaderboard service's settings, kept in a YAML file:
//
//	version: 1
//	proofTTL: 10m
//	boards:
//	  - name: high-score
//	    order: descending
//	    reset: allTime
//	  - name: weekly-sprint
//	    order: ascending
//	    reset: weekly
//	    weekStart: monday
//	    keep: 4
//	    validation: signed
//	    min: 1
//	    max: 3600000
//	serverKeys:
//	  - id: 2317febd373fcbcd
//	    key: hze+0cYruVRddgjkbHzgCeWWJbEsBkn6K8JNHRDf6Wg=
//
// Unknown fields, duplicate keys and trailing documents are refused. Optional fields keep what the file says, so a parsed file marshals back to the same fields; Marshal writes them in the order above with 2-space indentation, and a file in that form reads back and writes again byte for byte.
type Config struct {
	// Version is the schema version; it must be ConfigVersion.
	Version int `yaml:"version"`
	// ProofTTL is how old a game server's score proof may be, from 1s to 24h; zero means DefaultProofTTL.
	ProofTTL Duration `yaml:"proofTTL,omitempty"`
	// Boards are the leaderboards, in the order the service lists them.
	Boards []Board `yaml:"boards"`
	// ServerKeys are the game servers' Ed25519 public keys that sign score proofs. A board with validation: signed needs at least one.
	ServerKeys []token.PublicKey `yaml:"serverKeys,omitempty"`
}

// Board declares one leaderboard.
type Board struct {
	// Name is the board's key in URLs and the data directory: 1 to 64 characters of a-z, 0-9, '.', '_' and '-', starting with a letter or digit.
	Name string `yaml:"name"`
	// Order is the direction scores rank in.
	Order Order `yaml:"order"`
	// Reset is how often the board starts over.
	Reset Reset `yaml:"reset"`
	// WeekStart is the day a weekly board resets on, such as "monday" (the default when empty); only a weekly board may set it.
	WeekStart string `yaml:"weekStart,omitempty"`
	// Keep is how many earlier periods of a daily or weekly board stay readable after it resets, from 1 to MaxKeep; zero means 1. An all-time board may not set it.
	Keep int `yaml:"keep,omitempty"`
	// Validation is "signed" to accept only scores with a game server's proof, or empty to trust the player's client.
	Validation Validation `yaml:"validation,omitempty"`
	// Min is the lowest score accepted; nil means -MaxSafeScore.
	Min *int64 `yaml:"min,omitempty"`
	// Max is the highest score accepted; nil means MaxSafeScore.
	Max *int64 `yaml:"max,omitempty"`
}

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
}

// weekStart returns the day a weekly board resets on.
func (b Board) weekStart() time.Weekday {
	if b.WeekStart == "" {
		return time.Monday
	}
	return weekdays[b.WeekStart]
}

// keep returns how many earlier periods stay readable.
func (b Board) keep() int {
	if b.Reset == AllTime {
		return 0
	}
	if b.Keep == 0 {
		return 1
	}
	return b.Keep
}

// bounds returns the lowest and highest score the board accepts.
func (b Board) bounds() (lo, hi int64) {
	lo, hi = -MaxSafeScore, MaxSafeScore
	if b.Min != nil {
		lo = *b.Min
	}
	if b.Max != nil {
		hi = *b.Max
	}
	return lo, hi
}

// proofTTL returns how old a proof may be.
func (c Config) proofTTL() time.Duration {
	if c.ProofTTL == 0 {
		return DefaultProofTTL
	}
	return time.Duration(c.ProofTTL)
}

// Board returns the board named name and whether it exists.
func (c Config) Board(name string) (Board, bool) {
	for _, b := range c.Boards {
		if b.Name == name {
			return b, true
		}
	}
	return Board{}, false
}

// Validate checks that c is a complete version 1 configuration.
func (c Config) Validate() error {
	if c.Version != ConfigVersion {
		return fmt.Errorf("selfhost: unsupported config version %d (want %d)", c.Version, ConfigVersion)
	}
	if ttl := time.Duration(c.ProofTTL); ttl != 0 && (ttl < time.Second || ttl > 24*time.Hour || ttl%time.Second != 0) {
		return fmt.Errorf("selfhost: proofTTL %v must be whole seconds from 1s to 24h", ttl)
	}
	if len(c.Boards) == 0 {
		return errors.New("selfhost: config has no boards")
	}
	names := make(map[string]bool, len(c.Boards))
	signed := false
	for _, b := range c.Boards {
		if err := b.validate(); err != nil {
			return err
		}
		if names[b.Name] {
			return fmt.Errorf("selfhost: board %q declared twice", b.Name)
		}
		names[b.Name] = true
		signed = signed || b.Validation == SignedByServer
	}
	ids := make(map[string]bool, len(c.ServerKeys))
	for _, k := range c.ServerKeys {
		pub, err := parsePublicKey(k)
		if err != nil {
			return err
		}
		if id := token.KeyID(pub); k.ID != id {
			return fmt.Errorf("selfhost: server key id %q does not match its key (want %q)", k.ID, id)
		}
		if ids[k.ID] {
			return fmt.Errorf("selfhost: server key %q listed twice", k.ID)
		}
		ids[k.ID] = true
	}
	if signed && len(c.ServerKeys) == 0 {
		return errors.New("selfhost: a board validates signed scores but the config has no serverKeys")
	}
	return nil
}

func (b Board) validate() error {
	if !validName(b.Name) {
		return fmt.Errorf("selfhost: invalid board name %q: want 1 to 64 characters of a-z, 0-9, '.', '_' and '-', starting with a letter or digit", b.Name)
	}
	switch b.Order {
	case Descending, Ascending:
	default:
		return fmt.Errorf("selfhost: board %q: order %q must be descending or ascending", b.Name, b.Order)
	}
	switch b.Reset {
	case AllTime:
		if b.Keep != 0 {
			return fmt.Errorf("selfhost: board %q: an allTime board has no earlier periods to keep", b.Name)
		}
	case Daily, Weekly:
		if b.Keep < 0 || b.Keep > MaxKeep {
			return fmt.Errorf("selfhost: board %q: keep %d must be from 1 to %d", b.Name, b.Keep, MaxKeep)
		}
	default:
		return fmt.Errorf("selfhost: board %q: reset %q must be allTime, daily or weekly", b.Name, b.Reset)
	}
	if b.WeekStart != "" {
		if b.Reset != Weekly {
			return fmt.Errorf("selfhost: board %q: only a weekly board has a weekStart", b.Name)
		}
		if _, ok := weekdays[b.WeekStart]; !ok {
			return fmt.Errorf("selfhost: board %q: weekStart %q must be a lowercase English weekday such as monday", b.Name, b.WeekStart)
		}
	}
	if b.Validation != "" && b.Validation != SignedByServer {
		return fmt.Errorf("selfhost: board %q: validation %q must be signed or absent", b.Name, b.Validation)
	}
	for _, v := range []*int64{b.Min, b.Max} {
		if v != nil && (*v < -MaxSafeScore || *v > MaxSafeScore) {
			return fmt.Errorf("selfhost: board %q: score bound %d is beyond ±%d", b.Name, *v, int64(MaxSafeScore))
		}
	}
	if lo, hi := b.bounds(); lo > hi {
		return fmt.Errorf("selfhost: board %q: min %d is above max %d", b.Name, lo, hi)
	}
	return nil
}

// validName reports whether s is a valid board name: 1 to 64 characters of a-z, 0-9, '.', '_' and '-', starting with a letter or digit.
func validName(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '.' || c == '_' || c == '-') && i > 0:
		default:
			return false
		}
	}
	return true
}

func parsePublicKey(k token.PublicKey) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.Strict().DecodeString(k.Key)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("selfhost: server key %q is not a base64 Ed25519 public key", k.ID)
	}
	return ed25519.PublicKey(raw), nil
}

// ParseConfig decodes and validates a YAML configuration. Unknown fields, duplicate keys and trailing documents are refused.
func ParseConfig(data []byte) (Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Config{}, errors.New("selfhost: empty config")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("selfhost: config: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, errors.New("selfhost: config: unexpected trailing YAML document")
		}
		return Config{}, fmt.Errorf("selfhost: config: %w", err)
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
		return nil, fmt.Errorf("selfhost: config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("selfhost: config: %w", err)
	}
	return buf.Bytes(), nil
}

// Duration is a time.Duration written in YAML as Go duration text ("10m", "1h30m").
type Duration time.Duration

// IsZero reports whether d is zero, so an omitted duration stays omitted.
func (d Duration) IsZero() bool { return d == 0 }

// MarshalYAML writes d as its shortest Go duration text: "10m" rather than "10m0s".
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
		return fmt.Errorf("line %d: want a duration such as 10m", n.Line)
	}
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*d = Duration(v)
	return nil
}
