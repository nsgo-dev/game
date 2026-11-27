package main

import (
	"bytes"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/rhythm"
)

// TestSongsFileIsCanonical keeps songs.yaml in the form EncodeSongs writes, so an edit by hand and one by a tool read the same in review.
func TestSongsFileIsCanonical(t *testing.T) {
	canonical, err := EncodeSongs(Songs.SongFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, songsYAML) {
		t.Fatalf("songs.yaml is not in its canonical form; replace it with:\n%s", canonical)
	}
	back, err := ParseSongs(canonical)
	if err != nil || !reflect.DeepEqual(back, Songs.SongFile) {
		t.Fatalf("the canonical form reads back as %+v, %v", back, err)
	}
}

// TestBeatMapsAreCanonical keeps each song's beat map in the form rhythm.EncodeBeatMap writes.
func TestBeatMapsAreCanonical(t *testing.T) {
	files := mustSub(songFiles, "songs")
	for _, s := range Songs.Songs {
		data, err := fs.ReadFile(files, s.BeatMap)
		if err != nil {
			t.Fatal(err)
		}
		m, err := rhythm.ParseBeatMap(data)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := rhythm.EncodeBeatMap(m)
		if err != nil || !bytes.Equal(canonical, data) {
			t.Fatalf("%s is not in its canonical form (%v); replace it with:\n%s", s.BeatMap, err, canonical)
		}
	}
}

// TestChartsAreFair checks what a chart's format does not: every note starts after the lead-in has shown it, and two notes of a lane are far enough apart that a hit judged good for one is never in reach of the other.
func TestChartsAreFair(t *testing.T) {
	_, _, good := Songs.Judgement.windows()
	for si, s := range Songs.Songs {
		timing := Songs.Tracks[si].Timing
		for _, c := range s.Charts {
			last := [Lanes]float64{}
			seen := [Lanes]bool{}
			for i, n := range c.Notes {
				if at := timing.Time(n.Beat); at < approach-leadIn {
					t.Errorf("%s/%s: note %d at %v falls in before the playfield shows it", s.ID, c.ID, i+1, at)
				}
				if seen[n.Lane] {
					if gap := timing.Time(n.Beat) - timing.Time(last[n.Lane]); gap <= 2*good {
						t.Errorf("%s/%s: note %d is %v after the last note of lane %d, at most twice the good window", s.ID, c.ID, i+1, gap, n.Lane)
					}
				}
				last[n.Lane], seen[n.Lane] = n.Beat, true
			}
		}
	}
}

// validSongs is a small songs file every case of TestParseSongsRefuses breaks one way.
const validSongs = `version: 1
judgement:
  perfectMs: 45
  greatMs: 90
  goodMs: 135
energy:
  start: 60
  perfect: 2
  great: 1
  good: 0
  miss: -15
songs:
  - id: one
    title: One
    music: placeholder:music_loop
    beatMap: one.beatmap.yaml
    lengthBeats: 16
    charts:
      - id: easy
        name: Easy
        level: 1
        notes:
          - {beat: 4, lane: 0}
          - {beat: 4, lane: 3}
          - {beat: 5.5, lane: 1}
`

func TestParseSongsReadsAndWritesAFile(t *testing.T) {
	f, err := ParseSongs([]byte(validSongs))
	if err != nil {
		t.Fatal(err)
	}
	want := []Note{{Beat: 4, Lane: 0}, {Beat: 4, Lane: 3}, {Beat: 5.5, Lane: 1}}
	if got := f.Songs[0].Charts[0].Notes; !reflect.DeepEqual(got, want) {
		t.Fatalf("notes %+v, want %+v", got, want)
	}
	out, err := EncodeSongs(f)
	if err != nil || string(out) != validSongs {
		t.Fatalf("encoded (%v):\n%s\nwant:\n%s", err, out, validSongs)
	}
}

func TestParseSongsRefuses(t *testing.T) {
	cases := []struct {
		name, old, new, want string
	}{
		{"an unknown field", "  title: One\n", "  title: One\n    tempo: 120\n", "field tempo not found"},
		{"another version", "version: 1", "version: 2", "version 2, want 1"},
		{"a fractional version", "version: 1", "version: 1.5", "version \"1.5\" is not a whole number"},
		{"a fractional lane", "lane: 3}", "lane: 2.5}", "lane \"2.5\" is not a whole number"},
		{"a lane past the last", "lane: 3}", "lane: 4}", "lane 4 is not within 0 to 3"},
		{"a negative lane", "lane: 3}", "lane: -1}", "lane -1 is not within 0 to 3"},
		{"notes out of order", "{beat: 5.5, lane: 1}", "{beat: 3, lane: 1}", "does not come after"},
		{"two notes in one place", "{beat: 5.5, lane: 1}", "{beat: 4, lane: 3}", "does not come after"},
		{"a note at the end", "{beat: 5.5, lane: 1}", "{beat: 16, lane: 1}", "before the song's end"},
		{"a note before the start", "{beat: 4, lane: 0}", "{beat: -1, lane: 0}", "is not from 0"},
		{"a note at no number", "{beat: 5.5, lane: 1}", "{beat: .nan, lane: 1}", "is not from 0"},
		{"no notes", "        notes:\n          - {beat: 4, lane: 0}\n          - {beat: 4, lane: 3}\n          - {beat: 5.5, lane: 1}\n", "        notes: []\n", "no notes"},
		{"windows out of order", "greatMs: 90", "greatMs: 30", "must grow"},
		{"a window too wide", "goodMs: 135", "goodMs: 900", "must grow"},
		{"a miss that costs nothing", "miss: -15", "miss: 0", "energy for a miss 0"},
		{"no energy to start", "start: 60", "start: 0", "energy start 0"},
		{"a bad song id", "id: one", "id: One", "id \"One\""},
		{"a bad chart id", "id: easy", "id: Easy", "id \"Easy\""},
		{"a song with no title", "title: One", "title: \"\"", "no title"},
		{"music in a directory", "music: placeholder:music_loop", "music: ../music.wav", "music \"../music.wav\""},
		{"a beat map by another name", "beatMap: one.beatmap.yaml", "beatMap: one.yaml", "beatMap \"one.yaml\""},
		{"no length", "lengthBeats: 16", "lengthBeats: 0", "lengthBeats 0"},
		{"a level too high", "level: 1", "level: 21", "level 21"},
		{"an alias", "  - id: one\n", "  - &song\n    id: one\n", "anchors and aliases"},
		{"a second document", "", "---\nversion: 1\n", "more than one document"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text := validSongs + c.new
			if c.old != "" {
				if !strings.Contains(validSongs, c.old) {
					t.Fatalf("the file has no %q", c.old)
				}
				text = strings.Replace(validSongs, c.old, c.new, 1)
			}
			_, err := ParseSongs([]byte(text))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one saying %q", err, c.want)
			}
		})
	}
	if _, err := ParseSongs(nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("an empty file: %v", err)
	}
}

func TestLoadSongsReadsTheFilesASongNames(t *testing.T) {
	beatMap := []byte("version: 1\ntempos:\n  - beat: 0\n    bpm: 120\n")
	files := fstest.MapFS{"one.beatmap.yaml": {Data: beatMap}, "one.wav": {Data: []byte("RIFF")}}
	lib, err := LoadSongs([]byte(strings.Replace(validSongs, "placeholder:music_loop", "one.wav", 1)), files)
	if err != nil {
		t.Fatal(err)
	}
	if got := lib.Tracks[0].Timing.Time(4).String(); got != "2s" || string(lib.Tracks[0].Music.Data) != "RIFF" {
		t.Fatalf("beat 4 at %s, music %q", got, lib.Tracks[0].Music.Data)
	}
	for name, files := range map[string]fstest.MapFS{
		"no beat map":    {"one.wav": {Data: []byte("RIFF")}},
		"no music":       {"one.beatmap.yaml": {Data: beatMap}},
		"a bad beat map": {"one.beatmap.yaml": {Data: []byte("version: 1\ntempos: []\n")}, "one.wav": {Data: []byte("RIFF")}},
	} {
		if _, err := LoadSongs([]byte(strings.Replace(validSongs, "placeholder:music_loop", "one.wav", 1)), files); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
	if _, err := LoadSongs([]byte(strings.Replace(validSongs, "music_loop", "no_such_sound", 1)), files); err == nil {
		t.Error("a placeholder sound the pack lacks loaded")
	}
}

// FuzzParseSongs checks that whatever ParseSongs accepts encodes to text that reads back the same and encodes the same again.
func FuzzParseSongs(f *testing.F) {
	f.Add([]byte(validSongs))
	f.Add(songsYAML)
	f.Add([]byte("version: 1\nsongs: []\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := ParseSongs(data)
		if err != nil {
			return
		}
		out, err := EncodeSongs(s)
		if err != nil {
			t.Fatalf("a parsed file does not encode: %v", err)
		}
		back, err := ParseSongs(out)
		if err != nil {
			t.Fatalf("the encoded file does not parse: %v\n%s", err, out)
		}
		again, err := EncodeSongs(back)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("encoding is not stable (%v):\n%s\nthen:\n%s", err, out, again)
		}
		if !reflect.DeepEqual(back, s) {
			t.Fatalf("round trip changed the file: %+v, then %+v", s, back)
		}
	})
}
