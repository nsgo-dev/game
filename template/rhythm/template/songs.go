package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/placeholder"
	"github.com/nsgo-dev/game/packages/game/rhythm"
	"github.com/nsgo-dev/nsgo/audio"
	"go.yaml.in/yaml/v3"
)

// SongsVersion is the version of the songs file format this game reads and writes.
const SongsVersion = 1

// Lanes is how many note lanes the playfield has; a note's lane is 0 to Lanes-1, left to right.
const Lanes = 4

// Limits a songs file is checked against.
const (
	// MaxEnergy is the most energy a player can hold, and the most a file may start them with.
	MaxEnergy = 100
	// MaxWindowMs is the widest judgement window a file may set, either side of a note.
	MaxWindowMs = 500
	// MaxLevel is the highest difficulty level a chart may show.
	MaxLevel = 20
	// MaxNotes is the most notes one chart may hold.
	MaxNotes = 10_000
	// placeholderScheme starts a song's music when it names a sound of the placeholder pack rather than a file in songs/.
	placeholderScheme = "placeholder:"
)

// songsYAML is the game's song list and its scoring rules.
//
//go:embed songs.yaml
var songsYAML []byte

// songFiles holds what a song names besides songs.yaml: its beat map and, for music of its own, the audio file.
//
//go:embed songs
var songFiles embed.FS

// Songs are the game's songs, read from songs.yaml and the files under songs/ it names.
var Songs = MustLoadSongs(songsYAML, mustSub(songFiles, "songs"))

// SongFile is songs.yaml: the judgement windows, how energy moves, and the songs with their charts.
type SongFile struct {
	// Version is the format version, SongsVersion.
	Version int `yaml:"version"`
	// Judgement is how close to its note a hit has to land for each judgement.
	Judgement Judgement `yaml:"judgement"`
	// Energy is how much energy a player starts with and each judgement adds or takes; a player whose energy reaches 0 fails the song.
	Energy Energy `yaml:"energy"`
	// Songs are the songs in the order the menu lists them.
	Songs []Song `yaml:"songs"`
}

// Judgement holds the windows of the three hit judgements, each the most a hit may be early or late, in milliseconds; a note no hit lands within GoodMs of is a miss.
type Judgement struct {
	// PerfectMs is the perfect window; more than 0 and less than GreatMs.
	PerfectMs int `yaml:"perfectMs"`
	// GreatMs is the great window; less than GoodMs.
	GreatMs int `yaml:"greatMs"`
	// GoodMs is the good window; at most MaxWindowMs.
	GoodMs int `yaml:"goodMs"`
}

// Energy is the player's life bar: where it starts, and what each judgement adds to it, up to MaxEnergy.
type Energy struct {
	// Start is the energy a song starts with, 1 to MaxEnergy.
	Start int `yaml:"start"`
	// Perfect, Great and Good are what those hits add, -MaxEnergy to MaxEnergy.
	Perfect int `yaml:"perfect"`
	Great   int `yaml:"great"`
	Good    int `yaml:"good"`
	// Miss is what a miss takes, a negative number down to -MaxEnergy, so a player who stops playing fails.
	Miss int `yaml:"miss"`
}

// Song is one track and the charts played to it.
type Song struct {
	// ID names the song in the saved records, so renaming or reordering songs keeps them; lower-case letters, digits and -.
	ID string `yaml:"id"`
	// Title is the name the game shows.
	Title string `yaml:"title"`
	// Music is the track: a file in songs/, such as night-drive.m4a, or placeholder:<name> for a sound of the placeholder pack.
	Music string `yaml:"music"`
	// Loop repeats the track until the song ends, for music shorter than the song, such as a loop.
	Loop bool `yaml:"loop,omitempty"`
	// BeatMap is the track's tempo map, a rhythm beat map file in songs/ whose name ends in .beatmap.yaml.
	BeatMap string `yaml:"beatMap"`
	// LengthBeats is where the song ends, in beats of its beat map; every note comes before it.
	LengthBeats float64 `yaml:"lengthBeats"`
	// Charts are the song's difficulties, easiest first.
	Charts []Chart `yaml:"charts"`
}

// Chart is one difficulty of a song: the notes to hit.
type Chart struct {
	// ID names the chart within its song in the saved records; lower-case letters, digits and -.
	ID string `yaml:"id"`
	// Name is the difficulty's name the game shows, such as Easy.
	Name string `yaml:"name"`
	// Level is how hard the chart is, 1 to MaxLevel, for the menu.
	Level int `yaml:"level"`
	// Notes are the notes in time order, and in lane order at one beat.
	Notes []Note `yaml:"notes"`
}

// Note is a note to hit: in Lane, at Beat of the song's beat map.
type Note struct {
	// Beat is when the note is hit, in beats, 0 or later and before the song's LengthBeats.
	Beat float64 `yaml:"beat"`
	// Lane is the lane it falls in, 0 to Lanes-1.
	Lane int `yaml:"lane"`
}

// noteFields is Note without its MarshalYAML, which encodes through it.
type noteFields Note

// MarshalYAML writes a note on one line, {beat: 4, lane: 1}, so a chart reads as a list of hits.
func (n Note) MarshalYAML() (any, error) {
	var node yaml.Node
	if err := node.Encode(noteFields(n)); err != nil {
		return nil, err
	}
	node.Style = yaml.FlowStyle
	return &node, nil
}

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ParseSongs decodes and validates a songs file, refusing unknown fields, a second document, anchors and aliases, and any version but SongsVersion.
func ParseSongs(data []byte) (*SongFile, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("songs: %w", err)
	}
	if err := checkNodes(&doc); err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f SongFile
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("songs: the file is empty")
		}
		return nil, fmt.Errorf("songs: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("songs: the file holds more than one document")
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// intKeys are the keys of the format whose values are whole numbers; no other field shares their names.
var intKeys = map[string]bool{
	"version": true, "perfectMs": true, "greatMs": true, "goodMs": true,
	"start": true, "perfect": true, "great": true, "good": true, "miss": true,
	"level": true, "lane": true,
}

// checkNodes reports the first anchor or alias in n or below it, which EncodeSongs never writes and which would let one note stand for many, and the first whole-number field holding something else, such as lane: 1.5, which yaml.v3 would cut to 1 so that the file would neither mean what it says nor round-trip.
func checkNodes(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return fmt.Errorf("songs: line %d: anchors and aliases are not allowed", n.Line)
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if intKeys[k.Value] && v.Kind == yaml.ScalarNode && v.Tag != "!!int" {
				return fmt.Errorf("songs: line %d: %s %q is not a whole number", v.Line, k.Value, v.Value)
			}
		}
	}
	for _, c := range n.Content {
		if err := checkNodes(c); err != nil {
			return err
		}
	}
	return nil
}

// EncodeSongs writes f in the canonical form songs.yaml is kept in: fields in declaration order, two-space indents, a note to a line and no comments. ParseSongs of the result gives f back.
func EncodeSongs(f *SongFile) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Validate reports the first problem with f: a wrong version, windows or energy out of range, no songs, two songs with one ID, or a song Song.Validate refuses.
func (f *SongFile) Validate() error {
	if f.Version != SongsVersion {
		return fmt.Errorf("songs: version %d, want %d", f.Version, SongsVersion)
	}
	j := f.Judgement
	if j.PerfectMs <= 0 || j.PerfectMs >= j.GreatMs || j.GreatMs >= j.GoodMs || j.GoodMs > MaxWindowMs {
		return fmt.Errorf("songs: judgement windows %d, %d and %d ms must grow from more than 0 to at most %d", j.PerfectMs, j.GreatMs, j.GoodMs, MaxWindowMs)
	}
	e := f.Energy
	if e.Start < 1 || e.Start > MaxEnergy {
		return fmt.Errorf("songs: energy start %d is not within 1 to %d", e.Start, MaxEnergy)
	}
	for _, v := range []int{e.Perfect, e.Great, e.Good} {
		if v < -MaxEnergy || v > MaxEnergy {
			return fmt.Errorf("songs: energy for a hit %d is not within ±%d", v, MaxEnergy)
		}
	}
	if e.Miss >= 0 || e.Miss < -MaxEnergy {
		return fmt.Errorf("songs: energy for a miss %d is not within -%d to -1", e.Miss, MaxEnergy)
	}
	if len(f.Songs) == 0 {
		return errors.New("songs: no songs")
	}
	seen := map[string]bool{}
	for i := range f.Songs {
		s := &f.Songs[i]
		if err := s.Validate(); err != nil {
			return fmt.Errorf("songs: song %d: %w", i+1, err)
		}
		if seen[s.ID] {
			return fmt.Errorf("songs: two songs have the id %q", s.ID)
		}
		seen[s.ID] = true
	}
	return nil
}

// Validate reports the first problem with s: an ID, title, music or beat map missing or malformed, a length out of range, no charts, two charts with one ID, or a chart Chart.Validate refuses.
func (s *Song) Validate() error {
	switch {
	case !idPattern.MatchString(s.ID):
		return fmt.Errorf("id %q is not lower-case letters, digits and -, starting with a letter", s.ID)
	case strings.TrimSpace(s.Title) == "":
		return fmt.Errorf("%s: no title", s.ID)
	case !validMusic(s.Music):
		return fmt.Errorf("%s: music %q is neither a file name nor placeholder:<name>", s.ID, s.Music)
	case !validFileName(s.BeatMap) || !strings.HasSuffix(s.BeatMap, ".beatmap.yaml"):
		return fmt.Errorf("%s: beatMap %q is not a file name ending in .beatmap.yaml", s.ID, s.BeatMap)
	case math.IsNaN(s.LengthBeats) || s.LengthBeats <= 0 || s.LengthBeats > rhythm.MaxBeat:
		return fmt.Errorf("%s: lengthBeats %v is not more than 0 and at most %d", s.ID, s.LengthBeats, rhythm.MaxBeat)
	case len(s.Charts) == 0:
		return fmt.Errorf("%s: no charts", s.ID)
	}
	seen := map[string]bool{}
	for i := range s.Charts {
		c := &s.Charts[i]
		if err := c.Validate(s.LengthBeats); err != nil {
			return fmt.Errorf("%s: chart %d: %w", s.ID, i+1, err)
		}
		if seen[c.ID] {
			return fmt.Errorf("%s: two charts have the id %q", s.ID, c.ID)
		}
		seen[c.ID] = true
	}
	return nil
}

// Validate reports the first problem with c in a song lengthBeats long: an ID or name missing or malformed, a level out of range, no notes or too many, or a note out of range or out of order.
func (c *Chart) Validate(lengthBeats float64) error {
	switch {
	case !idPattern.MatchString(c.ID):
		return fmt.Errorf("id %q is not lower-case letters, digits and -, starting with a letter", c.ID)
	case strings.TrimSpace(c.Name) == "":
		return fmt.Errorf("%s: no name", c.ID)
	case c.Level < 1 || c.Level > MaxLevel:
		return fmt.Errorf("%s: level %d is not within 1 to %d", c.ID, c.Level, MaxLevel)
	case len(c.Notes) == 0:
		return fmt.Errorf("%s: no notes", c.ID)
	case len(c.Notes) > MaxNotes:
		return fmt.Errorf("%s: %d notes, at most %d", c.ID, len(c.Notes), MaxNotes)
	}
	for i, n := range c.Notes {
		switch {
		case math.IsNaN(n.Beat) || n.Beat < 0 || n.Beat >= lengthBeats:
			return fmt.Errorf("%s: note %d: beat %v is not from 0 to before the song's end at %v", c.ID, i+1, n.Beat, lengthBeats)
		case n.Lane < 0 || n.Lane >= Lanes:
			return fmt.Errorf("%s: note %d: lane %d is not within 0 to %d", c.ID, i+1, n.Lane, Lanes-1)
		case i > 0 && !noteBefore(c.Notes[i-1], n):
			return fmt.Errorf("%s: note %d at beat %v in lane %d does not come after the note before it", c.ID, i+1, n.Beat, n.Lane)
		}
	}
	return nil
}

// noteBefore reports whether a comes before b: at an earlier beat, or at the same beat in a lane to its left.
func noteBefore(a, b Note) bool {
	return a.Beat < b.Beat || a.Beat == b.Beat && a.Lane < b.Lane
}

// validMusic reports whether music names a file in songs/ or a placeholder sound.
func validMusic(music string) bool {
	if name, ok := strings.CutPrefix(music, placeholderScheme); ok {
		return idPattern.MatchString(strings.ReplaceAll(name, "_", "-"))
	}
	return validFileName(music)
}

// validFileName reports whether name is a plain file name in songs/: no directory, not hidden.
func validFileName(name string) bool {
	return name != "" && !strings.ContainsAny(name, `/\`) && !strings.HasPrefix(name, ".") && path.Clean(name) == name
}

// Library is the songs ready to play: the songs file and each song's timing and music.
type Library struct {
	*SongFile
	// Tracks are the songs' tracks, in the order of SongFile.Songs.
	Tracks []Track
}

// Track is what a song plays by: its beat map's timing and its music.
type Track struct {
	// Timing converts between the song's time and its beats.
	Timing *rhythm.Timing
	// Music is the audio the song plays.
	Music audio.Source
}

// LoadSongs parses a songs file and reads the beat map and music each song names from fsys, the songs/ directory.
func LoadSongs(data []byte, fsys fs.FS) (*Library, error) {
	f, err := ParseSongs(data)
	if err != nil {
		return nil, err
	}
	lib := &Library{SongFile: f, Tracks: make([]Track, len(f.Songs))}
	for i, s := range f.Songs {
		text, err := fs.ReadFile(fsys, s.BeatMap)
		if err != nil {
			return nil, fmt.Errorf("songs: %s: %w", s.ID, err)
		}
		m, err := rhythm.ParseBeatMap(text)
		if err != nil {
			return nil, fmt.Errorf("songs: %s: %s: %w", s.ID, s.BeatMap, err)
		}
		timing, err := m.Timing()
		if err != nil {
			return nil, fmt.Errorf("songs: %s: %s: %w", s.ID, s.BeatMap, err)
		}
		src, err := loadMusic(fsys, s.Music)
		if err != nil {
			return nil, fmt.Errorf("songs: %s: %w", s.ID, err)
		}
		lib.Tracks[i] = Track{Timing: timing, Music: src}
	}
	return lib, nil
}

// loadMusic returns the audio music names: a placeholder pack sound or the bytes of a file in fsys.
func loadMusic(fsys fs.FS, music string) (audio.Source, error) {
	if name, ok := strings.CutPrefix(music, placeholderScheme); ok {
		return placeholder.Sound(name)
	}
	data, err := fs.ReadFile(fsys, music)
	if err != nil {
		return audio.Source{}, err
	}
	return audio.Source{Data: data}, nil
}

// MustLoadSongs is LoadSongs for the songs the game embeds, which its tests check, so it panics on an error.
func MustLoadSongs(data []byte, fsys fs.FS) *Library {
	lib, err := LoadSongs(data, fsys)
	if err != nil {
		panic(err)
	}
	return lib
}

// mustSub returns the subtree of fsys at dir, which is embedded and so always there.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// windows returns the judgement windows as durations: perfect, great and good.
func (j Judgement) windows() (perfect, great, good time.Duration) {
	return time.Duration(j.PerfectMs) * time.Millisecond, time.Duration(j.GreatMs) * time.Millisecond, time.Duration(j.GoodMs) * time.Millisecond
}
