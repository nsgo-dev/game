package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/nsgo-dev/game/pkg/vehicle"
	"go.yaml.in/yaml/v3"
)

// TrackVersion is the version of the track file format, the only one ParseTrack reads.
const TrackVersion = 1

// trackYAML is the track the game races on: its centerline, width, laps, checkpoints, starting grid and rivals. Edit it to change the circuit; TestTrackFileIsCanonical keeps it in the form EncodeTrack writes.
//
//go:embed data/track.yaml
var trackYAML []byte

// carYAML is the tuning every car in the race drives with, a vehicle.Config file.
//
//go:embed data/car.yaml
var carYAML []byte

// Track is the circuit a race runs on, written by people as versioned YAML in data/track.yaml. Lengths are meters.
type Track struct {
	// Version is the file format version; it must equal TrackVersion.
	Version int `yaml:"version"`
	// Name is the circuit's name, shown on the results screen.
	Name string `yaml:"name"`
	// Laps is how many laps finish the race (1 to 20).
	Laps int `yaml:"laps"`
	// Width is the road's width (4 to 40).
	Width float32 `yaml:"width"`
	// Points are the centerline's control points on the ground, in driving order; the road is the closed Catmull-Rom loop through them (4 to 64 points, each at most 300 from the origin, so the road stays on the ground).
	Points []Point `yaml:"points"`
	// Start is how far along the loop from the first point the start/finish line is.
	Start float32 `yaml:"start"`
	// Checkpoints is how many checkpoints split a lap, the start/finish line included (2 to 32); they are spaced evenly along the loop, and a lap counts only when a car passes them all in order.
	Checkpoints int `yaml:"checkpoints"`
	// Grid is the gap along the road between starting slots, behind the start line, two abreast (> 0).
	Grid float32 `yaml:"grid"`
	// Rivals are the computer-driven cars, in grid order from the front; the player starts behind them (1 to 7).
	Rivals []Rival `yaml:"rivals"`
}

// Rival is one computer-driven car.
type Rival struct {
	// Name is the driver's name on the results screen; names are unique and not "You".
	Name string `yaml:"name"`
	// Color is the car's paint as linear [r, g, b], each in [0, 1].
	Color [3]float32 `yaml:"color,flow"`
	// Pace scales the speed the driver takes each part of the racing line at, in (0, 1]; 1 is the fastest the line allows.
	Pace float32 `yaml:"pace"`
	// Lane is how far right of the centerline the driver keeps, negative for left, within a quarter of the road's width.
	Lane float32 `yaml:"lane"`
}

// Point is a centerline control point on the ground plane, written on a line of its own as {x: …, z: …}.
type Point struct {
	// X is the point's position along the world's X axis (east).
	X float32 `yaml:"x"`
	// Z is the point's position along the world's Z axis (south); the cars start driving toward -Z.
	Z float32 `yaml:"z"`
}

// MarshalYAML writes p in flow style, so each point of a track takes one line.
func (p Point) MarshalYAML() (any, error) {
	type plain Point
	var n yaml.Node
	if err := n.Encode(plain(p)); err != nil {
		return nil, err
	}
	n.Style = yaml.FlowStyle
	return &n, nil
}

// playerName is the player's name in the standings.
const playerName = "You"

// maxTrackRadius is how far from the origin a centerline point may be, in meters. The ground, both the physics box the wheels ride on and the mesh drawn under the road, reaches groundHalf from the origin, which leaves room for the road's half width and the curve swinging out between points.
const (
	maxTrackRadius = 300
	groundHalf     = maxTrackRadius + 100
)

// ParseTrack decodes a YAML track and validates it. Unknown fields and trailing YAML documents are refused.
func ParseTrack(data []byte) (*Track, error) {
	var t Track
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("track: empty document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("track: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("track: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("track: %w", err)
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

// EncodeTrack validates t and writes it as 2-space YAML in a fixed key order, which ParseTrack reads back unchanged.
func EncodeTrack(t *Track) ([]byte, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(t); err != nil {
		return nil, fmt.Errorf("track: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("track: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks that t uses TrackVersion and that every value is finite and in the range its field documents.
func (t *Track) Validate() error {
	switch {
	case t.Version != TrackVersion:
		return fmt.Errorf("track: version %d, want %d", t.Version, TrackVersion)
	case t.Name == "":
		return errors.New("track: name is empty")
	case t.Laps < 1 || t.Laps > 20:
		return fmt.Errorf("track: laps %d not in [1, 20]", t.Laps)
	case !finite(t.Width) || t.Width < 4 || t.Width > 40:
		return fmt.Errorf("track: width %v not in [4, 40]", t.Width)
	case len(t.Points) < 4 || len(t.Points) > 64:
		return fmt.Errorf("track: %d points, want 4 to 64", len(t.Points))
	case !finite(t.Start) || t.Start < 0:
		return fmt.Errorf("track: start %v is not a finite, non-negative distance", t.Start)
	case t.Checkpoints < 2 || t.Checkpoints > 32:
		return fmt.Errorf("track: checkpoints %d not in [2, 32]", t.Checkpoints)
	case !finite(t.Grid) || t.Grid <= 0 || t.Grid > 50:
		return fmt.Errorf("track: grid %v not in (0, 50]", t.Grid)
	case len(t.Rivals) < 1 || len(t.Rivals) > 7:
		return fmt.Errorf("track: %d rivals, want 1 to 7", len(t.Rivals))
	}
	for i, p := range t.Points {
		if !finite(p.X) || !finite(p.Z) || math.Hypot(float64(p.X), float64(p.Z)) > maxTrackRadius {
			return fmt.Errorf("track: point %d %v is not within %d of the origin", i, p, maxTrackRadius)
		}
		if next := t.Points[(i+1)%len(t.Points)]; next == p {
			return fmt.Errorf("track: point %d repeats the point before it", (i+1)%len(t.Points))
		}
	}
	names := map[string]bool{playerName: true}
	for i, r := range t.Rivals {
		if r.Name == "" || names[r.Name] {
			return fmt.Errorf("track: rival %d name %q is empty, %q or used twice", i, r.Name, playerName)
		}
		names[r.Name] = true
		for _, c := range r.Color {
			if !finite(c) || c < 0 || c > 1 {
				return fmt.Errorf("track: rival %q color %v not in [0, 1]", r.Name, r.Color)
			}
		}
		if !finite(r.Pace) || r.Pace <= 0 || r.Pace > 1 {
			return fmt.Errorf("track: rival %q pace %v not in (0, 1]", r.Name, r.Pace)
		}
		if !finite(r.Lane) || math.Abs(float64(r.Lane)) > float64(t.Width)/4 {
			return fmt.Errorf("track: rival %q lane %v is more than a quarter of the width from the centerline", r.Name, r.Lane)
		}
	}
	return nil
}

// LoadRace parses the embedded track and car files.
func LoadRace() (*Track, vehicle.Config, error) {
	t, err := ParseTrack(trackYAML)
	if err != nil {
		return nil, vehicle.Config{}, err
	}
	car, err := vehicle.ParseConfig(carYAML)
	if err != nil {
		return nil, vehicle.Config{}, err
	}
	return t, car, nil
}

// trackID names t's exact layout for its saved ghost: the name and a hash of the canonical file, so editing any field of data/track.yaml, not only the name, retires a ghost recorded on the old course.
func trackID(t *Track) string {
	data, err := EncodeTrack(t)
	if err != nil {
		// t comes from ParseTrack, which validated it; an invalid track has no layout to name.
		return ""
	}
	return fmt.Sprintf("%s %x", t.Name, sha256.Sum256(data))
}

func finite(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
