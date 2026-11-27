package leaderboard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// CurrentVersion is the leaderboard catalog schema version this package reads and writes.
const CurrentVersion = 1

// MaxIDLength is the longest platform leaderboard ID a catalog accepts, matching Game Center's limit of 100 characters.
const MaxIDLength = 100

// MaxMiniGameKeys is the most mini-game cloud storage keys a catalog may declare, matching WeChat's limit of 128 KV pairs per player.
const MaxMiniGameKeys = 128

// MaxMiniGameKeyLength is the longest mini-game cloud storage key in bytes, matching WeChat's 128-byte key limit.
const MaxMiniGameKeyLength = 128

// MaxMiniGameValueBytes is the maximum byte length of one serialized key-value pair (len(key) + len(value)) in mini-game user cloud storage (1 KiB).
const MaxMiniGameValueBytes = 1024

// Order chooses whether higher or lower scores rank first on a mini-game board.
type Order string

const (
	// OrderDesc ranks higher scores first (for example, points). It is the default when MiniGameBoard.Order is omitted.
	OrderDesc Order = "desc"
	// OrderAsc ranks lower scores first (for example, race times).
	OrderAsc Order = "asc"
)

// MiniGameBoard configures a leaderboard on WeChat and Douyin mini games through the host's user cloud storage and open data context.
type MiniGameBoard struct {
	// Key is the host user cloud storage key (at most MaxMiniGameKeyLength bytes, unique across boards in the catalog).
	Key string `yaml:"key"`
	// Order is the sort order ("desc" or "asc"); empty defaults to OrderDesc.
	Order Order `yaml:"order,omitempty"`
}

// EffectiveOrder returns m.Order, defaulting to OrderDesc when omitted.
func (m MiniGameBoard) EffectiveOrder() Order {
	if m.Order == "" {
		return OrderDesc
	}
	return m.Order
}

// Catalog is the YAML file that declares a game's leaderboards once: each board's name in the game and its ID or storage entry in every platform game service it is registered with.
//
//	version: 1
//	leaderboards:
//	  - name: high-score
//	    gameCenter: com.example.game.high_score
//	    playGames: CgkIxxxxxxxxxxEAIQAQ
//	    miniGame:
//	      key: best
//	      order: desc
type Catalog struct {
	// Version is the schema version; it must equal CurrentVersion.
	Version int `yaml:"version"`
	// Leaderboards lists the boards in the order the file declares them.
	Leaderboards []Board `yaml:"leaderboards"`
}

// Board declares one leaderboard: the name the game's code uses and its ID or storage config in each platform game service. A board needs at least one platform entry; on a platform whose entry it lacks, Service reports ErrNoPlatformID.
type Board struct {
	// Name is the game's own key for the board, such as "high-score". It is unique within the catalog.
	Name string `yaml:"name"`
	// GameCenter is the leaderboard ID registered in App Store Connect, used on iOS, iPadOS, tvOS and macOS.
	GameCenter string `yaml:"gameCenter,omitempty"`
	// PlayGames is the leaderboard ID the Google Play Console generated, used on Android.
	PlayGames string `yaml:"playGames,omitempty"`
	// MiniGame configures friend rankings on WeChat and Douyin mini games through the host's cloud storage and open data context.
	MiniGame *MiniGameBoard `yaml:"miniGame,omitempty"`
}

// ID returns the board's platform ID (or mini-game cloud storage key) for provider, or "" when the board declares none for it.
func (b Board) ID(provider Provider) string {
	switch provider {
	case ProviderGameCenter:
		return b.GameCenter
	case ProviderPlayGames:
		return b.PlayGames
	case ProviderMiniGame:
		if b.MiniGame != nil {
			return b.MiniGame.Key
		}
	}
	return ""
}

func cloneBoard(b Board) Board {
	if b.MiniGame != nil {
		mg := *b.MiniGame
		b.MiniGame = &mg
	}
	return b
}

func cloneBoards(src []Board) []Board {
	if src == nil {
		return nil
	}
	out := make([]Board, len(src))
	for i, b := range src {
		out[i] = cloneBoard(b)
	}
	return out
}

// Board returns a copy of the board declared under name and whether it exists.
func (c *Catalog) Board(name string) (Board, bool) {
	if c == nil {
		return Board{}, false
	}
	for _, b := range c.Leaderboards {
		if b.Name == name {
			return cloneBoard(b), true
		}
	}
	return Board{}, false
}

// Parse decodes a YAML leaderboard catalog and validates it. Unknown fields, duplicate keys and trailing YAML documents are refused.
func Parse(data []byte) (*Catalog, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("add a leaderboard catalog starting with version: %d and a leaderboards list", CurrentVersion), "leaderboard: empty catalog"), "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Catalog
	if err := dec.Decode(&c); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("leaderboard: decode: %w", err), "", data, "check the YAML syntax and field names in the leaderboard catalog")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the leaderboard catalog and remove any --- separator", "leaderboard: unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("leaderboard: decode: %w", err), "", data, "remove trailing YAML content after the leaderboard catalog")
	}
	if err := c.Validate(); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	return &c, nil
}

// Encode validates c and writes it as deterministic two-space YAML: fields in schema order, boards in declaration order, and absent entries left out, so Parse followed by Encode reproduces a file Encode wrote byte for byte.
func (c *Catalog) Encode() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("leaderboard: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("leaderboard: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks c against the version 1 schema: a supported version, at least one board, unique non-empty names without control characters or surrounding whitespace, at least one well-formed platform entry per board, no two boards sharing a Game Center, Play Games or mini-game key, and at most MaxMiniGameKeys (128) mini-game keys of at most MaxMiniGameKeyLength (128) bytes each.
func (c *Catalog) Validate() error {
	if c == nil {
		return loaderr.Errorf("", "pass a non-nil *leaderboard.Catalog to Validate", "leaderboard: nil catalog")
	}
	if c.Version != CurrentVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the leaderboard catalog", CurrentVersion), "leaderboard: unsupported version %d (want %d)", c.Version, CurrentVersion)
	}
	if len(c.Leaderboards) == 0 {
		return loaderr.Errorf("leaderboards", "declare at least one board entry under leaderboards", "leaderboard: catalog declares no leaderboards")
	}
	seen := make(map[string]struct{}, len(c.Leaderboards))
	seenGC := make(map[string]string, len(c.Leaderboards))
	seenPG := make(map[string]string, len(c.Leaderboards))
	seenMG := make(map[string]string, len(c.Leaderboards))
	for i, b := range c.Leaderboards {
		bPath := fmt.Sprintf("leaderboards[%d]", i)
		if err := validateName(b.Name); err != nil {
			return loaderr.Errorf(bPath+".name", "set name to a non-empty UTF-8 string without control characters or surrounding spaces", "leaderboard: board %d: %w", i, err)
		}
		if _, dup := seen[b.Name]; dup {
			return loaderr.Errorf(bPath+".name", "give each leaderboard a unique name", "leaderboard: duplicate board name %q", b.Name)
		}
		seen[b.Name] = struct{}{}
		if b.GameCenter == "" && b.PlayGames == "" && b.MiniGame == nil {
			return loaderr.Errorf(bPath, "set gameCenter, playGames, miniGame, or a combination on the leaderboard entry", "leaderboard: board %q declares no platform ID (set gameCenter, playGames or miniGame)", b.Name)
		}
		if err := validateID(b.GameCenter); err != nil {
			return loaderr.Errorf(bPath+".gameCenter", fmt.Sprintf("set gameCenter to up to %d ASCII letters, digits, '.', '_' or '-'", MaxIDLength), "leaderboard: board %q gameCenter: %w", b.Name, err)
		}
		if err := validateID(b.PlayGames); err != nil {
			return loaderr.Errorf(bPath+".playGames", fmt.Sprintf("set playGames to up to %d ASCII letters, digits, '.', '_' or '-'", MaxIDLength), "leaderboard: board %q playGames: %w", b.Name, err)
		}
		checkID := func(provider, fieldPath string, ids map[string]string, id string) error {
			if id == "" {
				return nil
			}
			if other, dup := ids[id]; dup {
				return loaderr.Errorf(fieldPath, fmt.Sprintf("assign a unique %s ID to each leaderboard", provider), "leaderboard: board %q reuses the %s ID %q of board %q", b.Name, provider, id, other)
			}
			ids[id] = b.Name
			return nil
		}
		if err := checkID("gameCenter", bPath+".gameCenter", seenGC, b.GameCenter); err != nil {
			return err
		}
		if err := checkID("playGames", bPath+".playGames", seenPG, b.PlayGames); err != nil {
			return err
		}
		if b.MiniGame != nil {
			if err := validateMiniGameKey(b.MiniGame.Key); err != nil {
				return loaderr.Errorf(bPath+".miniGame.key", fmt.Sprintf("set miniGame.key to a non-empty UTF-8 string of at most %d bytes without control characters or surrounding spaces", MaxMiniGameKeyLength), "leaderboard: board %q miniGame.key: %w", b.Name, err)
			}
			switch b.MiniGame.Order {
			case "", OrderDesc, OrderAsc:
			default:
				return loaderr.Errorf(bPath+".miniGame.order", `set miniGame.order to "desc" or "asc" (or omit it for "desc")`, "leaderboard: board %q miniGame.order %q is invalid (want %q or %q)", b.Name, b.MiniGame.Order, OrderDesc, OrderAsc)
			}
			if other, dup := seenMG[b.MiniGame.Key]; dup {
				return loaderr.Errorf(bPath+".miniGame.key", "assign a unique miniGame.key to each leaderboard", "leaderboard: board %q reuses the miniGame key %q of board %q", b.Name, b.MiniGame.Key, other)
			}
			seenMG[b.MiniGame.Key] = b.Name
			if len(seenMG) > MaxMiniGameKeys {
				return loaderr.Errorf(bPath+".miniGame", fmt.Sprintf("declare at most %d miniGame keys across the catalog", MaxMiniGameKeys), "leaderboard: catalog declares more than %d miniGame keys", MaxMiniGameKeys)
			}
		}
	}
	return nil
}

// validateName accepts any non-empty UTF-8 name without control characters or leading or trailing spaces.
func validateName(name string) error {
	if name == "" {
		return errors.New("empty name")
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("name %q is not valid UTF-8", name)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("name %q contains a control character", name)
		}
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("name %q has leading or trailing whitespace", name)
	}
	return nil
}

// validateMiniGameKey accepts a non-empty UTF-8 cloud storage key of at most MaxMiniGameKeyLength bytes without control characters or surrounding whitespace.
func validateMiniGameKey(key string) error {
	if key == "" {
		return errors.New("empty key")
	}
	if !utf8.ValidString(key) {
		return fmt.Errorf("key %q is not valid UTF-8", key)
	}
	for _, r := range key {
		if unicode.IsControl(r) {
			return fmt.Errorf("key %q contains a control character", key)
		}
	}
	if strings.TrimSpace(key) != key {
		return fmt.Errorf("key %q has leading or trailing whitespace", key)
	}
	if len(key) > MaxMiniGameKeyLength {
		return fmt.Errorf("key is %d bytes (at most %d)", len(key), MaxMiniGameKeyLength)
	}
	return nil
}

// validateID accepts "" (no ID for that platform) or up to MaxIDLength ASCII letters, digits, '.', '_' and '-', the characters Game Center and Play Games use in leaderboard IDs.
func validateID(id string) error {
	if id == "" {
		return nil
	}
	if len(id) > MaxIDLength {
		return fmt.Errorf("ID is %d characters (at most %d)", len(id), MaxIDLength)
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return fmt.Errorf("ID %q contains %q (want letters, digits, '.', '_' or '-')", id, c)
		}
	}
	return nil
}
