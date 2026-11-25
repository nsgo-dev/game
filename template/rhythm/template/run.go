package main

import (
	"time"
)

// Grade is how a note was played: a hit within one of the judgement windows, or a miss.
type Grade uint8

const (
	// Perfect is a hit within the perfect window.
	Perfect Grade = iota
	// Great is a hit within the great window.
	Great
	// Good is a hit within the good window.
	Good
	// Miss is a note no hit landed on in time.
	Miss
)

// grades are the grades in order, for counting and showing them.
var grades = [...]Grade{Perfect, Great, Good, Miss}

// String returns the grade's name as the game shows it.
func (g Grade) String() string {
	switch g {
	case Perfect:
		return "Perfect"
	case Great:
		return "Great"
	case Good:
		return "Good"
	}
	return "Miss"
}

// MaxScore is the score of a chart played all perfect.
const MaxScore = 1_000_000

// gradeWeight is what each grade is worth toward the score, in thousandths of a perfect.
var gradeWeight = [...]int64{Perfect: 1000, Great: 700, Good: 400, Miss: 0}

// Status is where a run is: playing, or ended in a clear or a fail.
type Status uint8

const (
	// Playing is a run still going.
	Playing Status = iota
	// Cleared is a run that reached the song's end with energy left: a win.
	Cleared
	// Failed is a run whose energy ran out: a loss.
	Failed
)

// Judged is what one note's judgement was.
type Judged struct {
	// Note is the index of the note in the chart.
	Note int
	// Grade is its grade.
	Grade Grade
	// Offset is how early (negative) or late (positive) the hit was; 0 for a miss.
	Offset time.Duration
}

// Run is a chart being played: which notes are judged, the score, the combo and the energy. It knows nothing of clocks or input: the game passes it track positions, as rhythm.Clock.Position gives them, so a test can drive it exactly.
type Run struct {
	rules *SongFile
	chart *Chart
	// times are the notes' track times, in chart order, and end the song's.
	times []time.Duration
	end   time.Duration
	// lanes are the indexes of each lane's notes in time order, and next the first of them not yet judged.
	lanes [Lanes][]int
	next  [Lanes]int
	// judged counts the notes judged, and done holds whether each is.
	judged int
	done   []bool

	weight   int64
	Combo    int
	MaxCombo int
	Energy   int
	Counts   [len(grades)]int
	Status   Status
}

// NewRun starts chart, of the song played by track, under the rules of f.
func NewRun(f *SongFile, song *Song, track Track, chart *Chart) *Run {
	r := &Run{
		rules:  f,
		chart:  chart,
		times:  make([]time.Duration, len(chart.Notes)),
		end:    track.Timing.Time(song.LengthBeats),
		done:   make([]bool, len(chart.Notes)),
		Energy: f.Energy.Start,
	}
	for i, n := range chart.Notes {
		r.times[i] = track.Timing.Time(n.Beat)
		r.lanes[n.Lane] = append(r.lanes[n.Lane], i)
	}
	return r
}

// Chart returns the chart played.
func (r *Run) Chart() *Chart { return r.chart }

// NoteTime returns the track time of the chart's note i.
func (r *Run) NoteTime(i int) time.Duration { return r.times[i] }

// End returns the track time the song ends at.
func (r *Run) End() time.Duration { return r.end }

// Judged reports whether note i is judged.
func (r *Run) Judged(i int) bool { return r.done[i] }

// Score returns the score so far, out of MaxScore for the whole chart: each note's grade weighed against a perfect.
func (r *Run) Score() int {
	return int(r.weight * MaxScore / (int64(len(r.chart.Notes)) * gradeWeight[Perfect]))
}

// Hit judges a hit in lane at track position pos, the position the hit went down at: the lane's next note, if pos is within the good window of it. It first misses the lane's notes whose window pos is past. It reports false for a hit no note is near, which costs nothing, and once the run has ended.
func (r *Run) Hit(lane int, pos time.Duration) (Judged, bool) {
	if r.Status != Playing || lane < 0 || lane >= Lanes {
		return Judged{}, false
	}
	perfect, great, good := r.rules.Judgement.windows()
	notes := r.lanes[lane]
	for r.next[lane] < len(notes) {
		i := notes[r.next[lane]]
		off := pos - r.times[i]
		switch {
		case off > good:
			r.judge(lane, i, Miss, 0)
			if r.Status != Playing {
				return Judged{}, false
			}
			continue
		case off < -good:
			return Judged{}, false
		}
		g := Good
		switch abs := max(off, -off); {
		case abs <= perfect:
			g = Perfect
		case abs <= great:
			g = Great
		}
		r.judge(lane, i, g, off)
		return Judged{Note: i, Grade: g, Offset: off}, true
	}
	return Judged{}, false
}

// Advance moves the run to track position pos: every note whose good window pos is past is a miss, returned in time order, and a run past the song's end with every note judged is cleared.
func (r *Run) Advance(pos time.Duration) []Judged {
	if r.Status != Playing {
		return nil
	}
	_, _, good := r.rules.Judgement.windows()
	var missed []Judged
	for {
		// The lane whose next note is earliest goes first, so misses come in time order.
		lane, at := -1, time.Duration(0)
		for l := range Lanes {
			if r.next[l] < len(r.lanes[l]) {
				if t := r.times[r.lanes[l][r.next[l]]]; lane < 0 || t < at {
					lane, at = l, t
				}
			}
		}
		if lane < 0 || pos-at <= good {
			break
		}
		i := r.lanes[lane][r.next[lane]]
		r.judge(lane, i, Miss, 0)
		missed = append(missed, Judged{Note: i, Grade: Miss})
		if r.Status != Playing {
			return missed
		}
	}
	if r.judged == len(r.chart.Notes) && pos >= r.end {
		r.Status = Cleared
	}
	return missed
}

// judge records grade g for note i, the next of lane, with the combo and energy it brings, and fails the run when the energy runs out.
func (r *Run) judge(lane, i int, g Grade, off time.Duration) {
	r.next[lane]++
	r.done[i] = true
	r.judged++
	r.Counts[g]++
	r.weight += gradeWeight[g]
	e := r.rules.Energy
	switch g {
	case Perfect:
		r.Energy += e.Perfect
	case Great:
		r.Energy += e.Great
	case Good:
		r.Energy += e.Good
	case Miss:
		r.Energy += e.Miss
	}
	r.Energy = min(max(r.Energy, 0), MaxEnergy)
	if g == Miss {
		r.Combo = 0
	} else {
		r.Combo++
		r.MaxCombo = max(r.MaxCombo, r.Combo)
	}
	if r.Energy == 0 {
		r.Status = Failed
	}
}

// Rank returns the letter a score earns: S from 950,000, then A, B and C every 50,000 below, and D under 800,000.
func Rank(score int) string {
	switch {
	case score >= 950_000:
		return "S"
	case score >= 900_000:
		return "A"
	case score >= 850_000:
		return "B"
	case score >= 800_000:
		return "C"
	}
	return "D"
}
