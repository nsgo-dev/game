// Package templatemeta reads the versioned metadata shared by game templates,
// the editor start screen, website tooling, and editor agents.
package templatemeta

import (
	"bytes"
	"fmt"
	"go.yaml.in/yaml/v3"
	"strings"
)

const Version = 1
const SchemaURL = "https://game.nsgo.dev/schemas/template.v1.json"

type Localized struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description,omitempty"`
	Controls    string `yaml:"controls,omitempty"`
}
type Players struct {
	Min    int  `yaml:"min"`
	Max    int  `yaml:"max"`
	Local  bool `yaml:"local,omitempty"`
	Online bool `yaml:"online,omitempty"`
}
type Manifest struct {
	Version     int               `yaml:"version"`
	ID          string            `yaml:"id"`
	Title       map[string]string `yaml:"title"`
	Summary     map[string]string `yaml:"summary"`
	Genre       string            `yaml:"genre"`
	Dimension   string            `yaml:"dimension"`
	Orientation string            `yaml:"orientation"`
	Players     Players           `yaml:"players"`
	Inputs      []string          `yaml:"inputs,omitempty,flow"`
	Platforms   []string          `yaml:"platforms,omitempty,flow"`
	Controls    map[string]string `yaml:"controls,omitempty"`
	Tags        []string          `yaml:"tags,omitempty,flow"`
	Cover       string            `yaml:"cover,omitempty"`
	Playtest    string            `yaml:"playtest,omitempty"`
}

var genres = map[string]bool{"action": true, "card": true, "co-op": true, "fighting": true, "fps": true, "idle": true, "platformer": true, "puzzle": true, "racing": true, "rhythm": true, "roguelike": true, "rpg": true, "rts": true, "shmup": true, "tower-defense": true, "visual-novel": true, "starter": true}
var inputs = map[string]bool{"keyboard": true, "gamepad": true, "touch": true, "mouse": true}

func Parse(data []byte) (Manifest, error) {
	var m Manifest
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&m); err != nil {
		return m, err
	}
	if m.Version != Version {
		return m, fmt.Errorf("template: version must be %d", Version)
	}
	if strings.TrimSpace(m.ID) == "" {
		return m, fmt.Errorf("template: id is required")
	}
	if strings.TrimSpace(m.Title["en"]) == "" {
		return m, fmt.Errorf("template: title.en is required")
	}
	if !genres[m.Genre] {
		return m, fmt.Errorf("template: unknown genre %q", m.Genre)
	}
	if m.Dimension != "2d" && m.Dimension != "3d" {
		return m, fmt.Errorf("template: dimension must be 2d or 3d")
	}
	if m.Orientation != "landscape" && m.Orientation != "portrait" && m.Orientation != "any" {
		return m, fmt.Errorf("template: invalid orientation %q", m.Orientation)
	}
	if m.Players.Min < 1 || m.Players.Max < m.Players.Min {
		return m, fmt.Errorf("template: invalid players")
	}
	for _, in := range m.Inputs {
		if !inputs[in] {
			return m, fmt.Errorf("template: unknown input %q", in)
		}
	}
	return m, nil
}
func Encode(m Manifest) ([]byte, error) {
	if _, err := Parse(mustYAML(m)); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("# yaml-language-server: $schema=" + SchemaURL + "\n")
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if err := e.Encode(m); err != nil {
		return nil, err
	}
	_ = e.Close()
	return b.Bytes(), nil
}
func mustYAML(m Manifest) []byte { b, _ := yaml.Marshal(m); return b }
func (m Manifest) TitleFor(lang string) string {
	if v := m.Title[lang]; v != "" {
		return v
	}
	return m.Title["en"]
}
func (m Manifest) SummaryFor(lang string) string {
	if v := m.Summary[lang]; v != "" {
		return v
	}
	return m.Summary["en"]
}
