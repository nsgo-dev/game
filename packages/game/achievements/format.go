package achievements

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// CurrentVersion is the schema version of both the achievements file (Set) and the progress document (Progress) this package reads and writes.
const CurrentVersion = 1

// MaxSteps is the largest step count a Definition may declare.
const MaxSteps = math.MaxInt32

const (
	maxIDLength         = 128
	maxPlatformIDLength = 255
)

// Service names a platform game service an achievement is reported to.
type Service string

const (
	// GameCenter is Apple's Game Center, reached through GameKit on macOS, iOS and tvOS.
	GameCenter Service = "gameCenter"
	// PlayGames is Google Play Games Services on Android.
	PlayGames Service = "playGames"
)

// services lists every known Service in a fixed order.
var services = [...]Service{GameCenter, PlayGames}

// Valid reports whether s is a known Service.
func (s Service) Valid() bool { return slices.Contains(services[:], s) }

// Set is the root of a versioned YAML achievements file.
type Set struct {
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version"`
	// Achievements lists the game's achievements in the order the game presents them.
	Achievements []Definition `yaml:"achievements,omitempty"`
}

// Definition declares one achievement.
type Definition struct {
	// ID is the game-wide identifier game code passes to Tracker: 1 to 128 ASCII letters, digits, '.', '_' or '-'.
	ID string `yaml:"id"`
	// Title is an optional display name for the game's own UI; the platform services show the name configured in their consoles.
	Title string `yaml:"title,omitempty"`
	// Description is an optional explanation for the game's own UI.
	Description string `yaml:"description,omitempty"`
	// Hidden marks an achievement the game's own UI should not reveal before it is unlocked.
	Hidden bool `yaml:"hidden,omitempty"`
	// Steps is the number of steps that complete the achievement, 1 to MaxSteps; 0 (omitted) means 1, a one-shot achievement.
	Steps int `yaml:"steps,omitempty"`
	// Platforms holds the ID each platform service knows the achievement by.
	Platforms Platforms `yaml:"platforms,omitempty"`
}

// TotalSteps returns the steps that complete d: Steps, or 1 when Steps is 0.
func (d Definition) TotalSteps() int {
	if d.Steps <= 0 {
		return 1
	}
	return d.Steps
}

// Platforms holds an achievement's ID on each platform service. An empty ID means the achievement is not reported to that service.
type Platforms struct {
	// GameCenter is the achievement ID configured in App Store Connect.
	GameCenter string `yaml:"gameCenter,omitempty"`
	// PlayGames is the achievement ID Play Console generated.
	PlayGames string `yaml:"playGames,omitempty"`
}

// ID returns the achievement's ID on service s, or "" when it has none there or s is unknown.
func (p Platforms) ID(s Service) string {
	switch s {
	case GameCenter:
		return p.GameCenter
	case PlayGames:
		return p.PlayGames
	default:
		return ""
	}
}

// Lookup returns the definition whose ID is id.
func (s *Set) Lookup(id string) (Definition, bool) {
	if s == nil {
		return Definition{}, false
	}
	for _, d := range s.Achievements {
		if d.ID == id {
			return d, true
		}
	}
	return Definition{}, false
}

// Clone returns a deep copy of s.
func (s *Set) Clone() *Set {
	if s == nil {
		return nil
	}
	return &Set{Version: s.Version, Achievements: slices.Clone(s.Achievements)}
}

// Parse decodes a YAML achievements file and validates it. Unknown fields and trailing YAML documents are refused.
func Parse(data []byte) (*Set, error) {
	var set Set
	if err := decodeStrict(data, &set, "achievements file"); err != nil {
		return nil, err
	}
	if err := Validate(&set); err != nil {
		return nil, err
	}
	return &set, nil
}

// Encode validates set and writes it as deterministic 2-space YAML that Parse reads back unchanged.
func Encode(set *Set) ([]byte, error) {
	if err := Validate(set); err != nil {
		return nil, err
	}
	return encode(set)
}

// Validate checks that set follows the version 1 schema: known version, valid unique IDs, step counts in range, and well-formed platform IDs that no two achievements share on the same service.
func Validate(set *Set) error {
	if set == nil {
		return errors.New("achievements: nil achievements file")
	}
	if set.Version != CurrentVersion {
		return fmt.Errorf("achievements: unsupported version %d (want %d)", set.Version, CurrentVersion)
	}
	ids := make(map[string]struct{}, len(set.Achievements))
	platformIDs := make(map[Service]map[string]string, len(services))
	for i, d := range set.Achievements {
		where := fmt.Sprintf("achievements[%d]", i)
		if err := validateID(d.ID); err != nil {
			return fmt.Errorf("achievements: %s: %w", where, err)
		}
		where = fmt.Sprintf("achievement %q", d.ID)
		if _, dup := ids[d.ID]; dup {
			return fmt.Errorf("achievements: %s is declared more than once", where)
		}
		ids[d.ID] = struct{}{}
		if d.Steps < 0 || d.Steps > MaxSteps {
			return fmt.Errorf("achievements: %s: steps %d must be between 1 and %d", where, d.Steps, MaxSteps)
		}
		if !utf8.ValidString(d.Title) || !utf8.ValidString(d.Description) {
			return fmt.Errorf("achievements: %s: title and description must be valid UTF-8", where)
		}
		for _, s := range services {
			pid := d.Platforms.ID(s)
			if pid == "" {
				continue
			}
			if err := validatePlatformID(pid); err != nil {
				return fmt.Errorf("achievements: %s: %s ID: %w", where, s, err)
			}
			seen := platformIDs[s]
			if seen == nil {
				seen = map[string]string{}
				platformIDs[s] = seen
			}
			if other, dup := seen[pid]; dup {
				return fmt.Errorf("achievements: %s: %s ID %q is also used by %q", where, s, pid, other)
			}
			seen[pid] = d.ID
		}
	}
	return nil
}

// Progress is the root of a versioned YAML progress document: the steps a player has made and what each service has acknowledged.
type Progress struct {
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version"`
	// Achievements holds one record per achievement with progress, sorted by ID.
	Achievements []Record `yaml:"achievements,omitempty"`
}

// Record is one achievement's progress.
type Record struct {
	// ID is the achievement's Definition.ID.
	ID string `yaml:"id"`
	// Steps is the number of steps the player has made, 0 to the definition's TotalSteps.
	Steps int `yaml:"steps"`
	// Reported holds, per service, the steps that service last acknowledged; never more than Steps.
	Reported map[Service]int `yaml:"reported,omitempty"`
}

// ParseProgress decodes a YAML progress document and validates it. Unknown fields and trailing YAML documents are refused.
func ParseProgress(data []byte) (*Progress, error) {
	var p Progress
	if err := decodeStrict(data, &p, "progress document"); err != nil {
		return nil, err
	}
	if err := ValidateProgress(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// EncodeProgress validates p and writes it as deterministic 2-space YAML with records sorted by ID and services in name order, so ParseProgress reads it back unchanged.
func EncodeProgress(p *Progress) ([]byte, error) {
	if err := ValidateProgress(p); err != nil {
		return nil, err
	}
	sorted := Progress{Version: p.Version, Achievements: slices.Clone(p.Achievements)}
	sortRecords(sorted.Achievements)
	return encode(&sorted)
}

// ValidateProgress checks that p follows the version 1 schema: known version, valid unique IDs, non-negative steps, and acknowledged steps for known services between 0 and the record's steps.
func ValidateProgress(p *Progress) error {
	if p == nil {
		return errors.New("achievements: nil progress document")
	}
	if p.Version != CurrentVersion {
		return fmt.Errorf("achievements: unsupported progress version %d (want %d)", p.Version, CurrentVersion)
	}
	ids := make(map[string]struct{}, len(p.Achievements))
	for i, r := range p.Achievements {
		if err := validateID(r.ID); err != nil {
			return fmt.Errorf("achievements: progress record %d: %w", i, err)
		}
		if _, dup := ids[r.ID]; dup {
			return fmt.Errorf("achievements: progress for %q is recorded more than once", r.ID)
		}
		ids[r.ID] = struct{}{}
		if r.Steps < 0 || r.Steps > MaxSteps {
			return fmt.Errorf("achievements: progress for %q: steps %d must be between 0 and %d", r.ID, r.Steps, MaxSteps)
		}
		for s, n := range r.Reported {
			if !s.Valid() {
				return fmt.Errorf("achievements: progress for %q: unknown service %q", r.ID, s)
			}
			if n < 0 || n > r.Steps {
				return fmt.Errorf("achievements: progress for %q: %s acknowledged %d steps, want 0 to %d", r.ID, s, n, r.Steps)
			}
		}
	}
	return nil
}

func validateID(id string) error {
	if id == "" {
		return errors.New("id is required")
	}
	if len(id) > maxIDLength {
		return fmt.Errorf("id %q is longer than %d bytes", id, maxIDLength)
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' {
			continue
		}
		return fmt.Errorf("id %q may only hold ASCII letters, digits, '.', '_' and '-'", id)
	}
	return nil
}

func validatePlatformID(id string) error {
	if len(id) > maxPlatformIDLength {
		return fmt.Errorf("%q is longer than %d bytes", id, maxPlatformIDLength)
	}
	if !utf8.ValidString(id) {
		return fmt.Errorf("%q is not valid UTF-8", id)
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("%q holds whitespace or control characters", id)
		}
	}
	return nil
}

// decodeStrict decodes exactly one YAML document into out, refusing unknown fields.
func decodeStrict(data []byte, out any, what string) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("achievements: empty %s", what)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("achievements: decode %s: %w", what, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("achievements: unexpected trailing YAML document in %s", what)
		}
		return fmt.Errorf("achievements: decode %s: %w", what, err)
	}
	return nil
}

func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("achievements: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("achievements: encode: %w", err)
	}
	return buf.Bytes(), nil
}
