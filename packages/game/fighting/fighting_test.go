package fighting_test

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/fighting"
	"github.com/nsgo-dev/game/packages/game/replay"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

var (
	recActions = []string{"left", "right", "up", "down", "lp", "hp", "mk", "lk"}
	recAxes    = []string{"move_x", "move_y"}
)

func loadSet(t testing.TB) *fighting.MoveSet {
	t.Helper()
	set, err := fighting.LoadFile("testdata/moves.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// scriptSource is a replay.ActionSource whose state a test sets before each step.
type scriptSource struct {
	prev, curr map[string]bool
	next       map[string]bool
	axes       map[string]float64
}

func newScriptSource() *scriptSource {
	return &scriptSource{prev: map[string]bool{}, curr: map[string]bool{}, next: map[string]bool{}, axes: map[string]float64{}}
}

func (s *scriptSource) Step() {
	s.prev, s.curr = s.curr, s.prev
	clear(s.curr)
	for k, v := range s.next {
		s.curr[k] = v
	}
}
func (s *scriptSource) Pressed(name string) bool      { return s.curr[name] }
func (s *scriptSource) JustPressed(name string) bool  { return s.curr[name] && !s.prev[name] }
func (s *scriptSource) JustReleased(name string) bool { return !s.curr[name] && s.prev[name] }
func (s *scriptSource) Axis(name string) float64      { return s.axes[name] }

// seg holds an absolute direction and buttons for n steps.
type seg struct {
	n       int
	dir     fighting.Direction
	buttons []string
}

func hold(n int, dir fighting.Direction, buttons ...string) seg {
	return seg{n: n, dir: dir, buttons: buttons}
}

// record drives a scripted session through replay.Recorder and round-trips the recording through its file encoding, so the tests read inputs exactly as a recorded run stores them.
func record(t *testing.T, segs ...seg) *replay.Recording {
	t.Helper()
	src := newScriptSource()
	rec := replay.NewRecorder(src, 525, 60, recActions, recAxes)
	for _, s := range segs {
		for range s.n {
			clear(src.next)
			if s.dir.X() < 0 {
				src.next["left"] = true
			} else if s.dir.X() > 0 {
				src.next["right"] = true
			}
			if s.dir.Y() > 0 {
				src.next["up"] = true
			} else if s.dir.Y() < 0 {
				src.next["down"] = true
			}
			for _, b := range s.buttons {
				src.next[b] = true
			}
			rec.Step()
		}
	}
	data, err := rec.Recording().Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := replay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type hit struct {
	step  int
	moves string
}

// play feeds rec through a Recognizer for set and returns, for every step that completed anything, the move names in result order followed by any combo names after a "+".
func play(t *testing.T, set *fighting.MoveSet, rec *replay.Recording, facing func(step int) fighting.Facing) []hit {
	t.Helper()
	r, err := fighting.NewRecognizer(set)
	if err != nil {
		t.Fatal(err)
	}
	p := replay.NewPlayer(rec)
	var hits []hit
	for step := 0; !p.Done(); step++ {
		p.Step()
		f := fighting.FaceRight
		if facing != nil {
			f = facing(step)
		}
		res := r.Update(p, f)
		if res.Step != step {
			t.Fatalf("result step %d, want %d", res.Step, step)
		}
		if len(res.Moves) == 0 && len(res.Combos) == 0 {
			continue
		}
		var names []string
		for _, m := range res.Moves {
			names = append(names, m.Name)
		}
		for _, c := range res.Combos {
			names = append(names, "+"+c.Name)
		}
		hits = append(hits, hit{step: step, moves: strings.Join(names, ",")})
	}
	return hits
}

// specials returns the hits whose best move is not a plain normal, which is what most window tests care about.
func specials(hits []hit) []hit {
	var out []hit
	for _, h := range hits {
		if h.moves == "lp" || h.moves == "hp" || h.moves == "lk" {
			continue
		}
		out = append(out, h)
	}
	return out
}

func TestGoldenRecordedSession(t *testing.T) {
	set := loadSet(t)
	rec, err := replay.LoadFile("testdata/session.replay.yaml")
	if err != nil {
		t.Fatal(err)
	}
	got := play(t, set, rec, nil)
	want := []hit{
		{18, "hadoken,lp"},
		{78, "sonic_boom,lp"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
	data, err := os.ReadFile("testdata/session.replay.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := rec.Encode(); err != nil || !bytes.Equal(out, data) {
		t.Fatalf("recording is not in Encode's form (%v):\n%s", err, out)
	}
}

const (
	d1 = fighting.DownBack
	d2 = fighting.Down
	d3 = fighting.DownForward
	d4 = fighting.Back
	d5 = fighting.Neutral
	d6 = fighting.Forward
)

func TestMotionWindows(t *testing.T) {
	set := loadSet(t)
	tests := []struct {
		name string
		segs []seg
		want []hit
	}{
		{
			// The quarter circle spans exactly its 12-step window: 2 at step 0, 3 until step 11, 6 at step 12.
			name: "qcf at window edge",
			segs: []seg{hold(1, d2), hold(11, d3), hold(1, d6, "lp")},
			want: []hit{{12, "hadoken,lp"}},
		},
		{
			name: "qcf one step past window",
			segs: []seg{hold(1, d2), hold(12, d3), hold(1, d6, "lp")},
			want: nil,
		},
		{
			// The button comes exactly 8 steps after the last motion entry, the hadoken's buffer.
			name: "button at buffer edge",
			segs: []seg{hold(5, d5), hold(2, d2), hold(2, d3), hold(1, d6), hold(7, d5), hold(1, d5, "hp")},
			want: []hit{{17, "hadoken,hp"}},
		},
		{
			name: "button one step past buffer",
			segs: []seg{hold(5, d5), hold(2, d2), hold(2, d3), hold(1, d6), hold(8, d5), hold(1, d5, "hp")},
			want: nil,
		},
		{
			name: "dragon punch",
			segs: []seg{hold(2, d6), hold(2, d2), hold(1, d3, "hp")},
			want: []hit{{4, "shoryuken,hp"}},
		},
		{
			// 6 2 3 6 completes both the dragon punch and the quarter circle; priority puts the dragon punch first.
			name: "priority orders overlapping motions",
			segs: []seg{hold(2, d6), hold(2, d2), hold(1, d3), hold(1, d6, "lp")},
			want: []hit{{5, "shoryuken,hadoken,lp"}},
		},
		{
			name: "missing middle entry",
			segs: []seg{hold(3, d2), hold(1, d6, "lp")},
			want: nil,
		},
		{
			name: "charge held long enough",
			segs: []seg{hold(40, d1), hold(1, d6, "lp")},
			want: []hit{{40, "sonic_boom,lp"}},
		},
		{
			name: "charge one step short",
			segs: []seg{hold(39, d4), hold(1, d6, "lp")},
			want: nil,
		},
		{
			// The charge ends 7 steps before the forward input: the step right after it plus the 6-step grace.
			name: "charge released within grace",
			segs: []seg{hold(40, d4), hold(6, d5), hold(1, d6, "lp")},
			want: []hit{{46, "sonic_boom,lp"}},
		},
		{
			name: "charge released past grace",
			segs: []seg{hold(40, d4), hold(7, d5), hold(1, d6, "lp")},
			want: nil,
		},
		{
			name: "double tap",
			segs: []seg{hold(3, d6), hold(3, d5), hold(5, d6)},
			want: []hit{{6, "forward_dash"}},
		},
		{
			// The second 6 comes exactly the dash's 12-step window after the first.
			name: "double tap at window edge",
			segs: []seg{hold(1, d6), hold(11, d5), hold(1, d6)},
			want: []hit{{12, "forward_dash"}},
		},
		{
			name: "double tap too slow",
			segs: []seg{hold(1, d6), hold(12, d5), hold(1, d6)},
			want: nil,
		},
		{
			// lk comes exactly pressWindow (2) steps after lp.
			name: "chord within press window",
			segs: []seg{hold(2, d5, "lp"), hold(1, d5, "lp", "lk")},
			want: []hit{{0, "lp"}, {2, "throw,lk"}},
		},
		{
			name: "chord too spread",
			segs: []seg{hold(3, d5, "lp"), hold(1, d5, "lp", "lk")},
			want: []hit{{0, "lp"}, {3, "lk"}},
		},
	}
	set.Moves = append(set.Moves, fighting.Move{Name: "lk", Press: []string{"lk"}})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := play(t, set, record(t, tt.segs...), nil)
			if !strings.HasPrefix(tt.name, "chord") {
				got = specials(got)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("hits = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFacingMirrorsMotions(t *testing.T) {
	set := loadSet(t)
	// Absolute 2 1 4 is a quarter circle toward the left.
	rec := record(t, hold(2, d2), hold(2, d1), hold(1, d4, "lp"))
	left := func(int) fighting.Facing { return fighting.FaceLeft }
	if got, want := specials(play(t, set, rec, left)), []hit{{4, "hadoken,lp"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("facing left: hits = %v, want %v", got, want)
	}
	if got := specials(play(t, set, rec, nil)); got != nil {
		t.Fatalf("facing right: hits = %v, want none", got)
	}
}

func TestCombos(t *testing.T) {
	set := loadSet(t)
	tests := []struct {
		name string
		segs []seg
		want []hit
	}{
		{
			name: "linked within gap",
			segs: []seg{hold(1, d2, "mk"), hold(9, d2), hold(2, d3), hold(1, d6, "hp")},
			want: []hit{{0, "cr_mk"}, {12, "hadoken,hp,+low_fireball"}},
		},
		{
			// The hadoken finishes exactly the combo's 20-step gap after cr_mk.
			name: "linked at gap edge",
			segs: []seg{hold(1, d2, "mk"), hold(17, d2), hold(2, d3), hold(1, d6, "hp")},
			want: []hit{{0, "cr_mk"}, {20, "hadoken,hp,+low_fireball"}},
		},
		{
			name: "gap exceeded",
			segs: []seg{hold(1, d2, "mk"), hold(18, d2), hold(2, d3), hold(1, d6, "hp")},
			want: []hit{{0, "cr_mk"}, {21, "hadoken,hp"}},
		},
		{
			name: "another move breaks the chain",
			segs: []seg{hold(1, d2, "mk"), hold(1, d5), hold(1, d5, "lp"), hold(1, d2), hold(2, d3), hold(1, d6, "hp")},
			want: []hit{{0, "cr_mk"}, {2, "lp"}, {6, "hadoken,hp"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := play(t, set, record(t, tt.segs...), nil); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("hits = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMotionCompletesOnce(t *testing.T) {
	set := &fighting.MoveSet{
		Version: 1,
		Input:   fighting.Input{Left: "left", Right: "right", Up: "up", Down: "down"},
		Moves: []fighting.Move{
			{Name: "lenient", Motion: []string{"2", "23", "36"}, Window: 10},
			{Name: "tap", Motion: []string{"4"}},
		},
	}
	r, err := fighting.NewRecognizer(set)
	if err != nil {
		t.Fatal(err)
	}
	// The lenient entries overlap, so the step before the final 6 already satisfies the last entry; the move still completes once, and holding 6 or 4 does not repeat either move.
	dirs := []fighting.Direction{d5, d2, d3, d6, d6, d6, d5, d4, d4, d4, d5, d4}
	var got []hit
	for _, d := range dirs {
		res := r.Feed(d, 0, fighting.FaceRight)
		for _, m := range res.Moves {
			got = append(got, hit{res.Step, m.Name})
		}
	}
	want := []hit{{3, "lenient"}, {7, "tap"}, {11, "tap"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

func TestMotionIsConsumed(t *testing.T) {
	set := loadSet(t)
	set.Combos = append(set.Combos, fighting.Combo{Name: "double", Sequence: []string{"hadoken", "hadoken"}, Gap: 30})
	// A button pressed within the buffer after a finished quarter circle is a plain normal, not a second hadoken; a fresh quarter circle is.
	rec := record(t, hold(1, d2), hold(1, d3), hold(1, d6, "lp"), hold(1, d5), hold(1, d5, "hp"))
	want := []hit{{2, "hadoken,lp"}, {4, "hp"}}
	if got := play(t, set, rec, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("button after a finished motion: hits = %v, want %v", got, want)
	}
	rec = record(t, hold(1, d2), hold(1, d3), hold(1, d6, "lp"), hold(15, d5), hold(1, d2), hold(1, d3), hold(1, d6, "hp"))
	want = []hit{{2, "hadoken,lp"}, {20, "hadoken,hp,+double"}}
	if got := play(t, set, rec, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("two motions: hits = %v, want %v", got, want)
	}
}

// rollbackInputs performs cr_mk into a hadoken (the low_fireball combo), a forward dash and a dragon punch.
var rollbackInputs = []struct {
	dir  fighting.Direction
	held uint32
}{
	{d5, 0}, {d5, 0}, {d2, 4}, {d2, 0}, {d2, 0}, {d3, 0}, {d3, 0}, {d6, 2}, {d5, 0}, {d5, 0},
	{d6, 0}, {d5, 0}, {d6, 0}, {d5, 0}, {d2, 0}, {d3, 2}, {d5, 0}, {d5, 0}, {d1, 0}, {d4, 1},
}

func describe(res fighting.Result) string {
	return fmt.Sprintf("%d %v %v", res.Step, res.Moves, res.Combos)
}

func TestRecognizerStateRollback(t *testing.T) {
	set := loadSet(t)
	ref, err := fighting.NewRecognizer(set)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	sawCombo := false
	for _, in := range rollbackInputs {
		res := ref.Feed(in.dir, in.held, fighting.FaceRight)
		sawCombo = sawCombo || len(res.Combos) > 0
		want = append(want, describe(res))
	}
	if !sawCombo {
		t.Fatal("inputs complete no combo")
	}
	for k := range len(rollbackInputs) + 1 {
		r, err := fighting.NewRecognizer(set)
		if err != nil {
			t.Fatal(err)
		}
		for _, in := range rollbackInputs[:k] {
			r.Feed(in.dir, in.held, fighting.FaceRight)
		}
		state := r.AppendState(nil)
		// Mispredicted steps: the remote side was assumed to mash a different direction and button.
		for i := range 5 {
			r.Feed(fighting.Direction(1+i%9), uint32(i%4), fighting.FaceLeft)
		}
		if err := r.LoadState(state); err != nil {
			t.Fatalf("rollback to step %d: %v", k, err)
		}
		fresh, err := fighting.NewRecognizer(set)
		if err != nil {
			t.Fatal(err)
		}
		if err := fresh.LoadState(state); err != nil {
			t.Fatalf("load into fresh recognizer at step %d: %v", k, err)
		}
		for i, in := range rollbackInputs[k:] {
			for _, rr := range []*fighting.Recognizer{r, fresh} {
				if got := describe(rr.Feed(in.dir, in.held, fighting.FaceRight)); got != want[k+i] {
					t.Fatalf("rolled back to step %d: step %d gave %s, want %s", k, k+i, got, want[k+i])
				}
			}
		}
		if !bytes.Equal(r.AppendState(nil), ref.AppendState(nil)) || !bytes.Equal(fresh.AppendState(nil), ref.AppendState(nil)) {
			t.Fatalf("rolled back to step %d: final state differs", k)
		}
	}
}

func TestRecognizerStateErrors(t *testing.T) {
	set := loadSet(t)
	r, err := fighting.NewRecognizer(set)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range rollbackInputs[:6] {
		r.Feed(in.dir, in.held, fighting.FaceRight)
	}
	state := r.AppendState(nil)
	other, err := fighting.NewRecognizer(&fighting.MoveSet{Version: 1, Input: fighting.Input{Left: "left"}, Moves: []fighting.Move{{Name: "a", Press: []string{"a"}}}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		state []byte
		want  string
	}{
		{"empty", nil, "version 0"},
		{"version", append([]byte{9}, state[1:]...), "version 9"},
		{"truncated", state[:len(state)-1], "truncated"},
		{"trailing", append(slices.Clone(state), 0), "trailing"},
		{"other move set", other.AppendState(nil), "moves"},
		{"bad direction", append([]byte{1, 1, 0, 0, 0, 0}, state[6:]...), "bad frame"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := r.AppendState(nil)
			err := r.LoadState(tt.state)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadState error = %v, want it to contain %q", err, tt.want)
			}
			if !bytes.Equal(r.AppendState(nil), before) {
				t.Fatal("a refused state changed the recognizer")
			}
		})
	}
}

func TestComboStartsAndRepeats(t *testing.T) {
	set := &fighting.MoveSet{
		Version: 1,
		Input:   fighting.Input{Left: "left", Right: "right"},
		Moves:   []fighting.Move{{Name: "a", Press: []string{"a"}}, {Name: "b", Press: []string{"b"}}},
		Combos:  []fighting.Combo{{Name: "aab", Sequence: []string{"a", "a", "b"}, Gap: 5}},
	}
	r, err := fighting.NewRecognizer(set)
	if err != nil {
		t.Fatal(err)
	}
	// a a a b: the combo completes from the second a, the latest start that fits.
	inputs := []uint32{1, 0, 1, 0, 1, 0, 2}
	var combos []fighting.Match
	for _, held := range inputs {
		combos = append(combos, r.Feed(fighting.Neutral, held, fighting.FaceRight).Combos...)
	}
	want := []fighting.Match{{Name: "aab", Index: 0, Start: 2, Step: 6}}
	if !reflect.DeepEqual(combos, want) {
		t.Fatalf("combos = %+v, want %+v", combos, want)
	}
	r.Reset()
	if res := r.Feed(fighting.Neutral, 2, fighting.FaceRight); res.Step != 0 || len(res.Combos) != 0 {
		t.Fatalf("after reset: %+v", res)
	}
}

func TestMatchStart(t *testing.T) {
	set := loadSet(t)
	r, err := fighting.NewRecognizer(set)
	if err != nil {
		t.Fatal(err)
	}
	dirs := []fighting.Direction{d5, d2, d2, d3, d6}
	var res fighting.Result
	for i, d := range dirs {
		var held uint32
		if i == len(dirs)-1 {
			held = 1 // lp
		}
		res = r.Feed(d, held, fighting.FaceRight)
	}
	want := fighting.Match{Name: "hadoken", Index: 3, Start: 2, Step: 4}
	if len(res.Moves) == 0 || res.Moves[0] != want {
		t.Fatalf("moves = %+v, want first %+v", res.Moves, want)
	}
}

func TestDirection(t *testing.T) {
	for d := fighting.Direction(1); d <= 9; d++ {
		if got := fighting.DirectionOf(d.X(), d.Y()); got != d {
			t.Fatalf("DirectionOf(%d, %d) = %v, want %v", d.X(), d.Y(), got, d)
		}
		if d.Mirror().Mirror() != d || d.Mirror().X() != -d.X() || d.Mirror().Y() != d.Y() {
			t.Fatalf("Mirror(%v) = %v", d, d.Mirror())
		}
	}
	if fighting.Direction(0).Valid() || fighting.Direction(10).Valid() || fighting.Direction(0).String() != "?" {
		t.Fatal("invalid directions report valid")
	}
	if fighting.FaceLeft.Relative(fighting.DownBack) != fighting.DownForward || fighting.FaceRight.Relative(fighting.Back) != fighting.Back {
		t.Fatal("Relative does not mirror only when facing left")
	}

	set := loadSet(t)
	r, err := fighting.NewRecognizer(set)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		pressed []string
		axes    map[string]float64
		want    fighting.Direction
	}{
		{"neutral", nil, nil, d5},
		{"left and right cancel", []string{"left", "right"}, nil, d5},
		{"up and down cancel", []string{"up", "down", "right"}, nil, d6},
		{"down left", []string{"down", "left"}, nil, d1},
		{"axis past deadzone", nil, map[string]float64{"move_x": 0.5, "move_y": 0.7}, d3},
		{"axis inside deadzone", nil, map[string]float64{"move_x": 0.49, "move_y": -0.2}, d5},
		{"axis up", nil, map[string]float64{"move_y": -1}, fighting.Up},
		{"non-finite axis ignored", []string{"left"}, map[string]float64{"move_x": math.NaN(), "move_y": math.Inf(1)}, d4},
	}
	for _, tt := range tests {
		src := newScriptSource()
		for _, p := range tt.pressed {
			src.next[p] = true
		}
		for k, v := range tt.axes {
			src.axes[k] = v
		}
		src.Step()
		if got := r.Direction(src); got != tt.want {
			t.Errorf("%s: Direction = %v, want %v", tt.name, got, tt.want)
		}
	}

	set.Input = fighting.Input{AxisY: "stick_y", YUp: true, Deadzone: 0.25}
	r, err = fighting.NewRecognizer(set)
	if err != nil {
		t.Fatal(err)
	}
	src := newScriptSource()
	src.axes["stick_y"] = 0.3
	if got := r.Direction(src); got != fighting.Up {
		t.Fatalf("yUp axis: Direction = %v, want up", got)
	}
}

func TestBuffer(t *testing.T) {
	b := fighting.NewBuffer(3)
	if _, ok := b.Latest(); ok || b.Len() != 0 || b.Oldest() != 0 {
		t.Fatal("new buffer is not empty")
	}
	b.Push(fighting.Back, 0b01, fighting.FaceLeft)
	b.Push(0, 0b11, fighting.Facing(7))
	b.Push(fighting.Down, 0b10, fighting.FaceRight)
	f := b.Push(fighting.Up, 0b10, fighting.FaceRight)
	if b.Len() != 3 || b.Cap() != 3 || b.Steps() != 4 || b.Oldest() != 1 {
		t.Fatalf("len %d cap %d steps %d oldest %d", b.Len(), b.Cap(), b.Steps(), b.Oldest())
	}
	if _, ok := b.At(0); ok {
		t.Fatal("step 0 should have been overwritten")
	}
	if got, _ := b.At(1); got.Raw != fighting.Neutral || got.Facing != fighting.FaceRight || got.Pressed != 0b10 {
		t.Fatalf("step 1 = %+v", got)
	}
	if f.Step != 3 || f.Pressed != 0 || f.Held != 0b10 {
		t.Fatalf("latest = %+v", f)
	}
	b.Reset()
	if b.Len() != 0 || b.Steps() != 0 {
		t.Fatal("reset buffer is not empty")
	}
	if fighting.NewBuffer(0).Cap() != 1 {
		t.Fatal("capacity below 1 should keep one frame")
	}

	r := fighting.NewBuffer(2)
	if got := r.Push(fighting.Back, 0, fighting.FaceLeft); got.Dir != fighting.Forward {
		t.Fatalf("facing left Dir = %v, want forward", got.Dir)
	}
}

func TestFormatRoundTrip(t *testing.T) {
	data, err := os.ReadFile("testdata/moves.yaml")
	if err != nil {
		t.Fatal(err)
	}
	set, err := fighting.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	out, err := fighting.Encode(set)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("Encode(Parse(file)) differs from the file:\n%s", out)
	}
	again, err := fighting.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, set) {
		t.Fatal("Parse(Encode(set)) differs from set")
	}
	if got := set.Buttons(); !reflect.DeepEqual(got, []string{"lp", "hp", "mk", "lk"}) {
		t.Fatalf("Buttons = %v", got)
	}

	// Integer motion entries are accepted and re-encoded as canonical strings.
	plain := strings.Replace(string(data), "      - \"2\"\n      - \"3\"\n      - \"6\"", "      - 2\n      - 3\n      - 6", 1)
	set2, err := fighting.Parse([]byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set2, set) {
		t.Fatal("integer motion entries decode differently")
	}
}

func TestParseErrors(t *testing.T) {
	data, err := os.ReadFile("testdata/moves.yaml")
	if err != nil {
		t.Fatal(err)
	}
	base := string(data)
	tests := []struct {
		name, doc, want string
	}{
		{"empty", "  \n", "empty"},
		{"unknown field", strings.Replace(base, "    priority: 5\n", "    priority: 5\n    speed: 3\n", 1), "field speed not found"},
		{"version", strings.Replace(base, "version: 1", "version: 2", 1), "unsupported version 2"},
		{"trailing document", base + "---\nversion: 1\n", "trailing"},
		{"no input", strings.Replace(base, "input:\n  up: up\n  down: down\n  left: left\n  right: right\n  axisX: move_x\n  axisY: move_y\n", "input: {}\n", 1), "input names no actions"},
		{"bad deadzone", strings.Replace(base, "  axisY: move_y\n", "  axisY: move_y\n  deadzone: 1.5\n", 1), "deadzone"},
		{"bad digit", strings.Replace(base, "\"123\"", "\"120\"", 1), "distinct digits"},
		{"repeated digit", strings.Replace(base, "\"123\"", "\"122\"", 1), "distinct digits"},
		{"short window", strings.Replace(base, "window: 12\n    press", "window: 1\n    press", 1), "window 1 is shorter"},
		{"huge window", strings.Replace(base, "window: 14", "window: 601", 1), "outside [0, 600]"},
		{"duplicate name", strings.Replace(base, "name: hp", "name: lp", 1), "duplicate name \"lp\""},
		{"charge steps", strings.Replace(base, "steps: 40", "steps: 0", 1), "charge steps 0"},
		{"chord needs two", strings.Replace(base, "      - lp\n      - lk\n    pressAll", "      - lp\n    pressAll", 1), "at least 2 press"},
		{"window on one entry", strings.Replace(base, "    charge:", "    window: 3\n    charge:", 1), "window needs at least 2 motion entries"},
		{"window without motion", strings.Replace(base, "  - name: lp\n    press:", "  - name: lp\n    window: 600\n    press:", 1), "window needs at least 2 motion entries"},
		{"buffer without motion", strings.Replace(base, "  - name: lp\n    press:\n      - lp\n", "  - name: lp\n    press:\n      - lp\n    buffer: 4\n", 1), "buffer needs a motion"},
		{"buffer without press", strings.Replace(base, "    window: 12\n    priority: 10", "    window: 12\n    buffer: 3\n    priority: 10", 1), "buffer needs press"},
		{"combo unknown move", strings.Replace(base, "      - cr_mk\n", "      - cr_hk\n", 1), "unknown move \"cr_hk\""},
		{"combo gap", strings.Replace(base, "gap: 20", "gap: 0", 1), "gap 0"},
		{"combo name clash", strings.Replace(base, "name: low_fireball", "name: hadoken", 1), "duplicate name \"hadoken\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fighting.Parse([]byte(tt.doc))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse error = %v, want it to contain %q", err, tt.want)
			}
		})
	}

	var nilSet *fighting.MoveSet
	if err := nilSet.Validate(); err == nil {
		t.Fatal("nil set validated")
	}
	set := loadSet(t)
	for i := range fighting.MaxButtons + 1 {
		set.Moves = append(set.Moves, fighting.Move{Name: fmt.Sprintf("b%d", i), Press: []string{fmt.Sprintf("b%d", i)}})
	}
	if _, err := fighting.Encode(set); err == nil || !strings.Contains(err.Error(), "buttons") {
		t.Fatalf("too many buttons: %v", err)
	}
	if _, err := fighting.NewRecognizer(set); err == nil {
		t.Fatal("NewRecognizer accepted an invalid set")
	}
}

func TestFeedDoesNotAllocate(t *testing.T) {
	r, err := fighting.NewRecognizer(loadSet(t))
	if err != nil {
		t.Fatal(err)
	}
	dirs := []fighting.Direction{d2, d3, d6, d5, d6, d2, d3}
	i := 0
	allocs := testing.AllocsPerRun(200, func() {
		r.Feed(dirs[i%len(dirs)], uint32(i%3), fighting.FaceRight)
		i++
	})
	if allocs != 0 {
		t.Fatalf("Feed allocates %v times per step", allocs)
	}
}

func BenchmarkRecognizerFeed(b *testing.B) {
	r, err := fighting.NewRecognizer(loadSet(b))
	if err != nil {
		b.Fatal(err)
	}
	dirs := []fighting.Direction{d2, d3, d6, d5, d4, d1, d2, d3, d6}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		r.Feed(dirs[i%len(dirs)], uint32(i%5), fighting.FaceRight)
		i++
	}
}

func FuzzParse(f *testing.F) {
	data, err := os.ReadFile("testdata/moves.yaml")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte("version: 1\ninput: {left: l}\nmoves: [{name: a, press: [a]}]\n"))
	f.Fuzz(func(t *testing.T, doc []byte) {
		set, err := fighting.Parse(doc)
		if err != nil {
			return
		}
		out, err := fighting.Encode(set)
		if err != nil {
			t.Fatalf("Encode of a parsed set: %v", err)
		}
		again, err := fighting.Parse(out)
		if err != nil {
			t.Fatalf("Parse of encoded set: %v", err)
		}
		out2, err := fighting.Encode(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("encoding is not a fixed point: %v", err)
		}
		if _, err := fighting.NewRecognizer(set); err != nil {
			t.Fatalf("NewRecognizer: %v", err)
		}
	})
}

func FuzzFeed(f *testing.F) {
	f.Add([]byte{2, 0, 3, 0, 6, 1, 5, 0}, true)
	f.Add([]byte{4, 0, 4, 0, 6, 1}, false)
	set, err := fighting.LoadFile("testdata/moves.yaml")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, input []byte, left bool) {
		r, err := fighting.NewRecognizer(set)
		if err != nil {
			t.Fatal(err)
		}
		facing := fighting.FaceRight
		if left {
			facing = fighting.FaceLeft
		}
		for i := 0; i+1 < len(input); i += 2 {
			res := r.Feed(fighting.Direction(input[i]%11), uint32(input[i+1]), facing)
			for _, m := range res.Moves {
				if m.Start > m.Step || m.Step != res.Step || m.Step-m.Start >= r.Buffer().Cap() {
					t.Fatalf("bad match %+v at step %d", m, res.Step)
				}
			}
		}
		state := r.AppendState(nil)
		r2, err := fighting.NewRecognizer(set)
		if err != nil {
			t.Fatal(err)
		}
		if err := r2.LoadState(state); err != nil || !bytes.Equal(r2.AppendState(nil), state) {
			t.Fatalf("state round trip: %v", err)
		}
	})
}
