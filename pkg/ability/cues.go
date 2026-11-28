package ability

import (
	"bytes"
	"fmt"
	"go.yaml.in/yaml/v3"
	"io"
)

// CuesDoc is a strict version-1 mapping from cosmetic cue tags to handlers.
type CuesDoc struct {
	Version int            `yaml:"version"`
	Cues    map[string]Cue `yaml:"cues"`
}

// Cue describes client-side cosmetic handlers. Simulation code remains unaware of assets.
type Cue struct {
	Particles   string    `yaml:"particles,omitempty"`
	Sound       string    `yaml:"sound,omitempty"`
	Shake       *ShakeCue `yaml:"shake,omitempty"`
	Once        bool      `yaml:"once,omitempty"`
	WhileActive bool      `yaml:"whileActive,omitempty"`
}

// ShakeCue describes a camera shake response.
type ShakeCue struct {
	Amplitude float64 `yaml:"amplitude"`
	Duration  string  `yaml:"duration"`
}

func ParseCues(data []byte) (*CuesDoc, error) {
	var c CuesDoc
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil {
		return nil, fmt.Errorf("ability: decode cues: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err == nil {
		return nil, fmt.Errorf("ability: cues: trailing document")
	}
	if err := ValidateCues(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
func ValidateCues(c *CuesDoc) error {
	if c == nil || c.Version != 1 {
		return fmt.Errorf("ability: cues version must be 1")
	}
	for tag, cu := range c.Cues {
		if tag == "" {
			return fmt.Errorf("ability: cue tag is required")
		}
		if cu.Particles == "" && cu.Sound == "" && cu.Shake == nil {
			return fmt.Errorf("ability: cue %q has no handler", tag)
		}
		if cu.Shake != nil && cu.Shake.Amplitude < 0 {
			return fmt.Errorf("ability: cue %q shake amplitude must be non-negative", tag)
		}
	}
	return nil
}
func EncodeCues(c *CuesDoc) ([]byte, error) {
	if err := ValidateCues(c); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if err := e.Encode(c); err != nil {
		return nil, err
	}
	if err := e.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

var _ = io.EOF
