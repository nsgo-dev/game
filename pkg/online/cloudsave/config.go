package cloudsave

import (
	"bytes"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/online/accounts/authtoken"
	"github.com/nsgo-dev/game/pkg/online/internal/cors"
)

// ConfigVersion is the configuration file schema version this package reads and writes.
const ConfigVersion = 1

// DefaultAudience is the token audience the service accepts when Config.Audience is empty. The account service issues tokens for it on request (accounts.Service.RefreshFor), never at sign-in, so the token a player sends a game server does not reach the player's saves.
const DefaultAudience = "cloudsave"

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
//	audience: cloudsave
//	maxSlotBytes: 1048576
//	maxSlots: 32
//	keepVersions: 5
//	allowedOrigins:
//	  - https://play.example.com
//
// Unknown fields are refused. Marshal writes the canonical form: the fields in this order, audience only when set and allowedOrigins only when it has entries. A file in that form, such as one Marshal wrote, reads back and writes again unchanged.
type Config struct {
	// Version is the schema version; it must be ConfigVersion.
	Version int `yaml:"version"`
	// Audience is the token audience the service accepts, which the account service must list in its audiences; empty means DefaultAudience. It must differ from the audience of the key set, which names the game's servers, and must not be "accounts", the account service's own audience.
	Audience string `yaml:"audience,omitempty"`
	// MaxSlotBytes is the largest payload a slot may hold, in bytes, from 1 to MaxSlotBytesLimit. A liveops.CloudSyncer payload is the save inside a JSON envelope: about 250 bytes more, with the save's quotes and backslashes escaped, or base64, a third larger, for data that is not UTF-8.
	MaxSlotBytes int `yaml:"maxSlotBytes"`
	// MaxSlots is how many live slots an account may have, from 1 to MaxSlotsLimit.
	MaxSlots int `yaml:"maxSlots"`
	// KeepVersions is how many of each slot's newest versions are kept, the current one included, from 1 to MaxKeepVersions.
	KeepVersions int `yaml:"keepVersions"`
	// AllowedOrigins lists the web origins ("https://play.example.com") whose pages may call the service from a browser. The handler answers their CORS preflight requests; other origins get no CORS headers, so browsers refuse their requests. Native clients send no origin and need no entry.
	AllowedOrigins []string `yaml:"allowedOrigins,omitempty"`
}

// DefaultConfig returns a configuration for DefaultAudience with 1 MiB slots, 32 slots per account and 5 versions kept per slot, and no web origins.
func DefaultConfig() Config {
	return Config{Version: ConfigVersion, Audience: DefaultAudience, MaxSlotBytes: 1 << 20, MaxSlots: 32, KeepVersions: 5}
}

// TokenAudience is the token audience the service accepts: Audience, or DefaultAudience when it is empty.
func (c Config) TokenAudience() string {
	if c.Audience == "" {
		return DefaultAudience
	}
	return c.Audience
}

// Validate checks that c is a complete version 1 configuration.
func (c Config) Validate() error {
	if c.Version != ConfigVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the cloudsave config", ConfigVersion), "cloudsave: unsupported config version %d (want %d)", c.Version, ConfigVersion)
	}
	if c.Audience == authtoken.AccountServiceAudience {
		return loaderr.Errorf("audience", fmt.Sprintf("set audience to the name the account service lists in its audiences for this service, such as %q, not %q, which names the account service itself", DefaultAudience, authtoken.AccountServiceAudience), "cloudsave: audience %q is reserved for the account service", authtoken.AccountServiceAudience)
	}
	if c.MaxSlotBytes < 1 || c.MaxSlotBytes > MaxSlotBytesLimit {
		return loaderr.Errorf("maxSlotBytes", fmt.Sprintf("set maxSlotBytes to an integer from 1 to %d", MaxSlotBytesLimit), "cloudsave: maxSlotBytes %d must be from 1 to %d", c.MaxSlotBytes, MaxSlotBytesLimit)
	}
	if c.MaxSlots < 1 || c.MaxSlots > MaxSlotsLimit {
		return loaderr.Errorf("maxSlots", fmt.Sprintf("set maxSlots to an integer from 1 to %d", MaxSlotsLimit), "cloudsave: maxSlots %d must be from 1 to %d", c.MaxSlots, MaxSlotsLimit)
	}
	if c.KeepVersions < 1 || c.KeepVersions > MaxKeepVersions {
		return loaderr.Errorf("keepVersions", fmt.Sprintf("set keepVersions to an integer from 1 to %d", MaxKeepVersions), "cloudsave: keepVersions %d must be from 1 to %d", c.KeepVersions, MaxKeepVersions)
	}
	if err := cors.ValidateOrigins(c.AllowedOrigins); err != nil {
		return loaderr.Errorf("allowedOrigins", "set each allowedOrigins entry to an origin such as https://play.example.com with no path, query or fragment", "cloudsave: %w", err)
	}
	return nil
}

// ParseConfig decodes and validates a YAML configuration. Unknown fields and trailing documents are refused.
func ParseConfig(data []byte) (Config, error) {
	var c Config
	if len(bytes.TrimSpace(data)) == 0 {
		return Config{}, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("add a cloudsave config starting with version: %d", ConfigVersion), "cloudsave: empty config"), "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return Config{}, loaderr.Locate(fmt.Errorf("cloudsave: config: %w", err), "", data, "check the YAML syntax and field names in the cloudsave config")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the cloudsave config and remove any --- separator", "cloudsave: config: unexpected trailing YAML document"), "", data, "")
		}
		return Config{}, loaderr.Locate(fmt.Errorf("cloudsave: config: %w", err), "", data, "remove trailing YAML content after the cloudsave config")
	}
	if len(c.AllowedOrigins) == 0 {
		c.AllowedOrigins = nil
	}
	if err := c.Validate(); err != nil {
		return Config{}, loaderr.Locate(err, "", data, "")
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
