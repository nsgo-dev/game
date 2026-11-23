package cloudsave

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"

	"go.yaml.in/yaml/v3"
)

// ConfigVersion is the configuration file schema version this package reads and writes.
const ConfigVersion = 1

// Bounds of the configuration's limits.
const (
	// MaxSlotBytesLimit is the largest Config.MaxSlotBytes: 64 MiB.
	MaxSlotBytesLimit = 64 << 20
	// MaxSlotsLimit is the largest Config.MaxSlots.
	MaxSlotsLimit = 10000
	// MaxKeepVersions is the largest Config.KeepVersions.
	MaxKeepVersions = 100
)

// Config is the cloud save service's settings, kept in a YAML file:
//
//	version: 1
//	maxSlotBytes: 1048576
//	maxSlots: 32
//	keepVersions: 5
//	allowedOrigins:
//	  - https://play.example.com
//
// Unknown fields are refused. Marshal writes the canonical form: the fields in this order and allowedOrigins only when it has entries. A file in that form, such as one Marshal wrote, reads back and writes again unchanged.
type Config struct {
	// Version is the schema version; it must be ConfigVersion.
	Version int `yaml:"version"`
	// MaxSlotBytes is the largest payload a slot may hold, in bytes, from 1 to MaxSlotBytesLimit. A liveops.CloudSyncer payload is the save inside a JSON envelope: about 250 bytes more, with the save's quotes and backslashes escaped, or base64, a third larger, for data that is not UTF-8.
	MaxSlotBytes int `yaml:"maxSlotBytes"`
	// MaxSlots is how many live slots an account may have, from 1 to MaxSlotsLimit.
	MaxSlots int `yaml:"maxSlots"`
	// KeepVersions is how many of each slot's newest versions are kept, the current one included, from 1 to MaxKeepVersions.
	KeepVersions int `yaml:"keepVersions"`
	// AllowedOrigins lists the web origins ("https://play.example.com") whose pages may call the service from a browser. The handler answers their CORS preflight requests; other origins get no CORS headers, so browsers refuse their requests. Native clients send no origin and need no entry.
	AllowedOrigins []string `yaml:"allowedOrigins,omitempty"`
}

// DefaultConfig returns a configuration with 1 MiB slots, 32 slots per account and 5 versions kept per slot, and no web origins.
func DefaultConfig() Config {
	return Config{Version: ConfigVersion, MaxSlotBytes: 1 << 20, MaxSlots: 32, KeepVersions: 5}
}

// Validate checks that c is a complete version 1 configuration.
func (c Config) Validate() error {
	if c.Version != ConfigVersion {
		return fmt.Errorf("cloudsave: unsupported config version %d (want %d)", c.Version, ConfigVersion)
	}
	if c.MaxSlotBytes < 1 || c.MaxSlotBytes > MaxSlotBytesLimit {
		return fmt.Errorf("cloudsave: maxSlotBytes %d must be from 1 to %d", c.MaxSlotBytes, MaxSlotBytesLimit)
	}
	if c.MaxSlots < 1 || c.MaxSlots > MaxSlotsLimit {
		return fmt.Errorf("cloudsave: maxSlots %d must be from 1 to %d", c.MaxSlots, MaxSlotsLimit)
	}
	if c.KeepVersions < 1 || c.KeepVersions > MaxKeepVersions {
		return fmt.Errorf("cloudsave: keepVersions %d must be from 1 to %d", c.KeepVersions, MaxKeepVersions)
	}
	seen := map[string]bool{}
	for _, o := range c.AllowedOrigins {
		if err := validateOrigin(o); err != nil {
			return err
		}
		if seen[o] {
			return fmt.Errorf("cloudsave: allowedOrigins lists %q twice", o)
		}
		seen[o] = true
	}
	return nil
}

// validateOrigin checks that o is a web origin as a browser sends it: an http or https scheme and a host, with no path, query, fragment or user.
func validateOrigin(o string) error {
	u, err := url.Parse(o)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.String() != o {
		return fmt.Errorf("cloudsave: allowedOrigins entry %q is not an origin such as https://play.example.com", o)
	}
	return nil
}

// ParseConfig decodes and validates a YAML configuration. Unknown fields and trailing documents are refused.
func ParseConfig(data []byte) (Config, error) {
	var c Config
	if len(bytes.TrimSpace(data)) == 0 {
		return Config{}, errors.New("cloudsave: empty config")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("cloudsave: config: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, errors.New("cloudsave: config: unexpected trailing YAML document")
		}
		return Config{}, fmt.Errorf("cloudsave: config: %w", err)
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
		return nil, fmt.Errorf("cloudsave: config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("cloudsave: config: %w", err)
	}
	return buf.Bytes(), nil
}
