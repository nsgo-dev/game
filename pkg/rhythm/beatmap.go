package rhythm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

const (
	fixBeatMap      = "write the beat map as version: 1 YAML with tempos starting at beat 0"
	fixCalibrations = "write calibrations as version: 1 YAML with valid device offsets"
)

// BeatMapVersion is the beat map schema version this package reads and writes.
const BeatMapVersion = 1

// Limits a beat map is validated against.
const (
	// MinBPM is the slowest tempo a beat map may set, so every time a valid map gives, up to MaxBeat, fits a time.Duration many times over.
	MinBPM = 1
	// MaxBPM is the fastest tempo a beat map may set.
	MaxBPM = 2000
	// MaxBeat is the latest beat a tempo or meter change may start at.
	MaxBeat = 1_000_000
	// MaxOffsetMs is the largest offset, either way, of beat 0 from the start of the track: one hour.
	MaxOffsetMs = 3_600_000
	// MaxChanges is the most tempo changes, and separately meter changes, a beat map may hold.
	MaxChanges = 10_000
	// MaxBeatsPerBar is the longest bar a meter may set.
	MaxBeatsPerBar = 64
)

// BeatMap is a track's tempo map, as written in a versioned YAML file next to the track: where beat 0 falls in the audio, the tempo from each beat on, and optionally the meter from each bar on.
//
//	version: 1
//	title: Opening
//	offsetMs: 120
//	tempos:
//	  - beat: 0
//	    bpm: 120
//	  - beat: 64
//	    bpm: 140
//	meters:
//	  - beat: 0
//	    beatsPerBar: 4
//
// A beat is the unit the tempo counts, so a tempo change can start between beats (beat: 62.5) while a meter change starts on a whole bar of the meter before it. Parse and Encode keep the fields in this order and round-trip a valid map unchanged; Timing turns it into times.
type BeatMap struct {
	// Version is the schema version; it must be BeatMapVersion.
	Version int `yaml:"version"`
	// Title names the track for people reading the file; nothing uses it.
	Title string `yaml:"title,omitempty"`
	// OffsetMs is where beat 0 plays in the track, in milliseconds from its start; negative when the track starts after beat 0.
	OffsetMs float64 `yaml:"offsetMs,omitempty"`
	// Tempos are the tempo changes in beat order, the first at beat 0.
	Tempos []Tempo `yaml:"tempos"`
	// Meters are the meter changes in beat order, the first at beat 0; none means 4 beats to a bar.
	Meters []Meter `yaml:"meters,omitempty"`
}

// Tempo is a tempo change: from Beat on, BPM beats a minute.
type Tempo struct {
	// Beat is where the tempo starts, 0 or later and after the change before it.
	Beat float64 `yaml:"beat"`
	// BPM is the beats a minute, from MinBPM to MaxBPM.
	BPM float64 `yaml:"bpm"`
}

// Meter is a meter change: from Beat on, bars of BeatsPerBar beats.
type Meter struct {
	// Beat is where the first bar of the meter starts: a whole number of the previous meter's bars after the previous change.
	Beat int `yaml:"beat"`
	// BeatsPerBar is the beats in each bar, 1 to MaxBeatsPerBar.
	BeatsPerBar int `yaml:"beatsPerBar"`
}

// ParseBeatMap decodes a YAML beat map and validates it. Unknown fields, a second YAML document, anchors and aliases, and a fraction where a whole number belongs are refused.
func ParseBeatMap(data []byte) (*BeatMap, error) {
	m, err := parseBeatMap(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixBeatMap)
	}
	return m, nil
}

func parseBeatMap(data []byte) (*BeatMap, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixBeatMap, "rhythm: empty beat map")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var m BeatMap
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("rhythm: decode beat map: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document in the beat map; remove the trailing --- document", "rhythm: beat map has a second YAML document")
		}
		return nil, fmt.Errorf("rhythm: decode beat map: %w", err)
	}
	if err := requireIntegers(data, []string{"version"}, map[string][]string{"meters": {"beat", "beatsPerBar"}}); err != nil {
		return nil, err
	}
	// An empty list is written as none, so it reads back as none.
	if len(m.Meters) == 0 {
		m.Meters = nil
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	yamlzero.Normalize(&m)
	return &m, nil
}

// requireIntegers refuses an anchor or alias anywhere in data, which Encode never writes and which would carry a value past the checks below, and then reports the first value that decoded into an int field without being a YAML integer: keys of the top mapping in top, and keys of the mappings listed under a top key in nested. yaml.v3 truncates 1.5 into an int field, which would neither mean what the file says nor round-trip.
func requireIntegers(data []byte, top []string, nested map[string][]string) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	if err := refuseAliases(&doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	check := func(field, key string, n *yaml.Node) error {
		if n.Kind == yaml.ScalarNode && n.Tag != "!!int" {
			return &loaderr.Error{
				Line:    n.Line,
				Column:  n.Column,
				Field:   field,
				Problem: fmt.Sprintf("rhythm: line %d: %s %q is not a whole number", n.Line, key, n.Value),
				Fix:     fmt.Sprintf("write %s as a whole number", key),
			}
		}
		return nil
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i].Value, root.Content[i+1]
		if slices.Contains(top, key) {
			if err := check(key, key, val); err != nil {
				return err
			}
		}
		fields, ok := nested[key]
		if !ok || val.Kind != yaml.SequenceNode {
			continue
		}
		for idx, item := range val.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(item.Content); j += 2 {
				if slices.Contains(fields, item.Content[j].Value) {
					k := item.Content[j].Value
					if err := check(loaderr.Path(key, idx, k), k, item.Content[j+1]); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// refuseAliases reports the first anchor or alias in n and below it.
func refuseAliases(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return &loaderr.Error{
			Line:    n.Line,
			Column:  n.Column,
			Problem: fmt.Sprintf("rhythm: line %d: anchors and aliases are not allowed", n.Line),
			Fix:     "write values directly without YAML anchors or aliases",
		}
	}
	for _, c := range n.Content {
		if err := refuseAliases(c); err != nil {
			return err
		}
	}
	return nil
}

// EncodeBeatMap validates m and writes it as deterministic 2-space YAML that ParseBeatMap reads back to an equal map.
func EncodeBeatMap(m *BeatMap) ([]byte, error) {
	if m == nil {
		return nil, errors.New("rhythm: nil beat map")
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return encodeYAML(m)
}

// encodeYAML writes v as 2-space YAML.
func encodeYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(v)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("rhythm: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("rhythm: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate reports the first way m breaks the version 1 schema: a wrong version, no tempo, a change out of order or range, or a meter change off its bar line.
func (m *BeatMap) Validate() error {
	if m == nil {
		return loaderr.Errorf("", fixBeatMap, "rhythm: nil beat map")
	}
	if m.Version != BeatMapVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version to %d", BeatMapVersion), "rhythm: beat map version %d, want %d", m.Version, BeatMapVersion)
	}
	if !finite(m.OffsetMs) || math.Abs(m.OffsetMs) > MaxOffsetMs {
		return loaderr.Errorf("offsetMs", fmt.Sprintf("set offsetMs to a finite number within ±%d", MaxOffsetMs), "rhythm: offsetMs %v is not within ±%d", m.OffsetMs, MaxOffsetMs)
	}
	if len(m.Tempos) == 0 {
		return loaderr.Errorf("tempos", "add at least one tempo entry starting at beat 0", "rhythm: a beat map needs a tempo at beat 0")
	}
	if len(m.Tempos) > MaxChanges {
		return loaderr.Errorf("tempos", fmt.Sprintf("keep at most %d tempo changes", MaxChanges), "rhythm: more than %d tempo or meter changes", MaxChanges)
	}
	if len(m.Meters) > MaxChanges {
		return loaderr.Errorf("meters", fmt.Sprintf("keep at most %d meter changes", MaxChanges), "rhythm: more than %d tempo or meter changes", MaxChanges)
	}
	for i, t := range m.Tempos {
		switch {
		case !finite(t.Beat) || t.Beat < 0 || t.Beat > MaxBeat:
			return loaderr.Errorf(loaderr.Path("tempos", i, "beat"), fmt.Sprintf("set beat to a finite number in [0, %d]", MaxBeat), "rhythm: tempos[%d]: beat %v is not within 0 to %d", i, t.Beat, MaxBeat)
		case i == 0 && t.Beat != 0:
			return loaderr.Errorf(loaderr.Path("tempos", 0, "beat"), "set the first tempo entry's beat to 0", "rhythm: tempos[0] starts at beat %v, want 0", t.Beat)
		case i > 0 && t.Beat <= m.Tempos[i-1].Beat:
			return loaderr.Errorf(loaderr.Path("tempos", i, "beat"), "order tempo changes in strictly increasing beat order", "rhythm: tempos[%d]: beat %v is not after beat %v", i, t.Beat, m.Tempos[i-1].Beat)
		case !finite(t.BPM) || t.BPM < MinBPM || t.BPM > MaxBPM:
			return loaderr.Errorf(loaderr.Path("tempos", i, "bpm"), fmt.Sprintf("set bpm to a finite number in [%d, %d]", MinBPM, MaxBPM), "rhythm: tempos[%d]: bpm %v is not within %d to %d", i, t.BPM, MinBPM, MaxBPM)
		}
	}
	for i, me := range m.Meters {
		switch {
		case me.Beat < 0 || me.Beat > MaxBeat:
			return loaderr.Errorf(loaderr.Path("meters", i, "beat"), fmt.Sprintf("set beat to an integer in [0, %d]", MaxBeat), "rhythm: meters[%d]: beat %d is not within 0 to %d", i, me.Beat, MaxBeat)
		case i == 0 && me.Beat != 0:
			return loaderr.Errorf(loaderr.Path("meters", 0, "beat"), "set the first meter entry's beat to 0", "rhythm: meters[0] starts at beat %d, want 0", me.Beat)
		case me.BeatsPerBar < 1 || me.BeatsPerBar > MaxBeatsPerBar:
			return loaderr.Errorf(loaderr.Path("meters", i, "beatsPerBar"), fmt.Sprintf("set beatsPerBar to an integer in [1, %d]", MaxBeatsPerBar), "rhythm: meters[%d]: beatsPerBar %d is not within 1 to %d", i, me.BeatsPerBar, MaxBeatsPerBar)
		case i > 0 && me.Beat <= m.Meters[i-1].Beat:
			return loaderr.Errorf(loaderr.Path("meters", i, "beat"), "order meter changes in strictly increasing beat order", "rhythm: meters[%d]: beat %d is not after beat %d", i, me.Beat, m.Meters[i-1].Beat)
		case i > 0 && (me.Beat-m.Meters[i-1].Beat)%m.Meters[i-1].BeatsPerBar != 0:
			return loaderr.Errorf(loaderr.Path("meters", i, "beat"), "align each meter change to a whole bar boundary of the previous meter", "rhythm: meters[%d]: beat %d does not start a bar of %d beats from beat %d", i, me.Beat, m.Meters[i-1].BeatsPerBar, m.Meters[i-1].Beat)
		}
	}
	return nil
}

// Timing validates m and returns its times, which later changes to m do not affect.
func (m *BeatMap) Timing() (*Timing, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	t := &Timing{segs: make([]tempoSeg, len(m.Tempos))}
	sec := m.OffsetMs / 1000
	for i, tp := range m.Tempos {
		if i > 0 {
			prev := t.segs[i-1]
			sec = prev.sec + (tp.Beat-prev.beat)*prev.spb
		}
		t.segs[i] = tempoSeg{beat: tp.Beat, sec: sec, spb: 60 / tp.BPM, bpm: tp.BPM}
	}
	meters := m.Meters
	if len(meters) == 0 {
		meters = []Meter{{Beat: 0, BeatsPerBar: 4}}
	}
	t.bars = make([]meterSeg, len(meters))
	bar := 0
	for i, me := range meters {
		if i > 0 {
			prev := meters[i-1]
			bar += (me.Beat - prev.Beat) / prev.BeatsPerBar
		}
		t.bars[i] = meterSeg{beat: me.Beat, bar: bar, per: me.BeatsPerBar}
	}
	return t, nil
}

// Timing converts between a track's time and its beats and bars, by a BeatMap. Times are from the start of the audio, the position a player reports; a beat before 0 or after the last change keeps the tempo nearest it. A Timing never changes, so goroutines may share one.
type Timing struct {
	segs []tempoSeg
	bars []meterSeg
}

// tempoSeg is a tempo change with the track time, in seconds, its beat plays at, and its seconds a beat.
type tempoSeg struct {
	beat, sec, spb, bpm float64
}

// meterSeg is a meter change with the bar number its beat starts.
type meterSeg struct {
	beat, bar, per int
}

// Seconds returns the track time beat plays at, in seconds, at full float64 precision.
func (t *Timing) Seconds(beat float64) float64 {
	s := t.segs[t.tempoAt(beat)]
	return s.sec + (beat-s.beat)*s.spb
}

// Time returns the track time beat plays at, rounded to the nanosecond.
func (t *Timing) Time(beat float64) time.Duration {
	return secondsDuration(t.Seconds(beat))
}

// BeatAtSeconds returns the beat that plays sec seconds into the track.
func (t *Timing) BeatAtSeconds(sec float64) float64 {
	i := sort.Search(len(t.segs), func(i int) bool { return t.segs[i].sec > sec }) - 1
	s := t.segs[max(i, 0)]
	return s.beat + (sec-s.sec)/s.spb
}

// Beat returns the beat that plays at track time d.
func (t *Timing) Beat(d time.Duration) float64 { return t.BeatAtSeconds(d.Seconds()) }

// BPM returns the tempo at beat.
func (t *Timing) BPM(beat float64) float64 { return t.segs[t.tempoAt(beat)].bpm }

// Bar returns the bar beat falls in, counted from 0 at beat 0 and negative before it, and how many beats into that bar it is.
func (t *Timing) Bar(beat float64) (bar int, inBar float64) {
	i := sort.Search(len(t.bars), func(i int) bool { return float64(t.bars[i].beat) > beat }) - 1
	m := t.bars[max(i, 0)]
	rel := beat - float64(m.beat)
	n := math.Floor(rel / float64(m.per))
	return m.bar + int(n), rel - n*float64(m.per)
}

// BeatsPerBar returns the meter at beat.
func (t *Timing) BeatsPerBar(beat float64) int {
	i := sort.Search(len(t.bars), func(i int) bool { return float64(t.bars[i].beat) > beat }) - 1
	return t.bars[max(i, 0)].per
}

// tempoAt is the index of the tempo change that holds at beat.
func (t *Timing) tempoAt(beat float64) int {
	return max(sort.Search(len(t.segs), func(i int) bool { return t.segs[i].beat > beat })-1, 0)
}

// secondsDuration rounds sec to the nanosecond, saturating at the longest Duration either way, as a beat far past any valid map's can reach.
func secondsDuration(sec float64) time.Duration {
	ns := math.Round(sec * 1e9)
	switch {
	case ns >= math.MaxInt64:
		return math.MaxInt64
	case ns <= math.MinInt64:
		return math.MinInt64
	case math.IsNaN(ns):
		return 0
	}
	return time.Duration(ns)
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
