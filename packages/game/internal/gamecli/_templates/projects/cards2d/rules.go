package main

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// rulesVersion is the schema version of rules.yaml that this game reads and writes.
const rulesVersion = 1

// Limits rules.yaml is checked against: every rank is drawn as one or two digits, the ladders are laid out in one row of the table, and a deck has to be dealt again a bounded number of times.
const (
	maxRanks   = 13
	maxSuits   = 4
	maxRedeals = 9
)

// rulesYAML is the deck the game deals: its suits, how high each ladder climbs and how many times the discard pile can be dealt again. Change the game's rules here; the table's layout is in board.yaml.
//
//go:embed rules.yaml
var rulesYAML []byte

// Rules is rules.yaml: the deck is one card of every rank from 1 to Ranks in every suit.
type Rules struct {
	// Version must equal rulesVersion.
	Version int `yaml:"version"`
	// Ranks is the highest rank, and so how many cards each ladder holds when it is complete.
	Ranks int `yaml:"ranks"`
	// Redeals is how many times a game may turn the discard pile over into a new draw pile.
	Redeals int `yaml:"redeals"`
	// Suits are the deck's suits, one ladder each, in the order the ladders sit on the table.
	Suits []Suit `yaml:"suits"`
}

// Suit is one suit of the deck.
type Suit struct {
	// Name is the suit's tag on its cards and the end of its ladder's slot ID in board.yaml, ladder-<name>: lower-case letters, digits and -, starting with a letter.
	Name string `yaml:"name"`
	// Color is the color the suit's cards are drawn in, as "#rrggbb".
	Color string `yaml:"color"`
}

var suitName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// parseRules decodes rules.yaml and validates it, refusing unknown fields and trailing documents.
func parseRules(data []byte) (*Rules, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var r Rules
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("rules.yaml: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("rules.yaml: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("rules.yaml: %w", err)
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// encodeRules validates r and writes it as rules.yaml is written: 2-space YAML with the fields in a fixed order, so a file in that form reads and writes back unchanged.
func encodeRules(r *Rules) ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(r); err != nil {
		return nil, fmt.Errorf("rules.yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("rules.yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// validate checks r against the version 1 schema.
func (r *Rules) validate() error {
	if r.Version != rulesVersion {
		return fmt.Errorf("rules.yaml: version %d, want %d", r.Version, rulesVersion)
	}
	if r.Ranks < 1 || r.Ranks > maxRanks {
		return fmt.Errorf("rules.yaml: ranks %d must be between 1 and %d", r.Ranks, maxRanks)
	}
	if r.Redeals < 0 || r.Redeals > maxRedeals {
		return fmt.Errorf("rules.yaml: redeals %d must be between 0 and %d", r.Redeals, maxRedeals)
	}
	if len(r.Suits) < 1 || len(r.Suits) > maxSuits {
		return fmt.Errorf("rules.yaml: %d suits, want 1 to %d", len(r.Suits), maxSuits)
	}
	seen := make(map[string]bool, len(r.Suits))
	for i, s := range r.Suits {
		if !suitName.MatchString(s.Name) {
			return fmt.Errorf("rules.yaml: suit %d name %q must be lower-case letters, digits and -, starting with a letter", i, s.Name)
		}
		if seen[s.Name] {
			return fmt.Errorf("rules.yaml: suit %q appears twice", s.Name)
		}
		seen[s.Name] = true
		if _, err := s.color(); err != nil {
			return fmt.Errorf("rules.yaml: suit %q: %w", s.Name, err)
		}
	}
	return nil
}

// color returns the suit's Color, which must be "#rrggbb".
func (s Suit) color() (core.Color, error) {
	if len(s.Color) != 7 || s.Color[0] != '#' {
		return 0, fmt.Errorf("color %q must be #rrggbb", s.Color)
	}
	return core.ParseColor(s.Color)
}

// cardCount returns how many cards the deck holds.
func (r *Rules) cardCount() int { return r.Ranks * len(r.Suits) }
