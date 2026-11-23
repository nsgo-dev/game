package replay_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/replay"
)

func readGolden(t testing.TB) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "level_clear.replay.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func editText(t *testing.T, text, old, new string) string {
	t.Helper()
	if strings.Count(text, old) != 1 {
		t.Fatalf("edit target %q found %d times, want 1", old, strings.Count(text, old))
	}
	return strings.Replace(text, old, new, 1)
}

type inputState struct {
	jump, dash, left, right bool
	moveX                   float64
}

func sample(p *replay.Player) inputState {
	return inputState{
		jump:  p.Pressed("jump"),
		dash:  p.Pressed("dash"),
		left:  p.Pressed("left"),
		right: p.Pressed("right"),
		moveX: p.Axis("move_x"),
	}
}

// TestEditedRecordingReplaysAsEdited edits the golden recording the way a person would in a text editor and checks that the Player follows every edit and nothing else.
func TestEditedRecordingReplaysAsEdited(t *testing.T) {
	golden := readGolden(t)
	orig, err := replay.Decode([]byte(golden))
	if err != nil {
		t.Fatalf("Decode golden: %v", err)
	}

	edited := golden
	// Move the jump-and-dash press two steps later.
	edited = editText(t, edited, "  - {step: 85, press: [jump, dash]}\n", "  - {step: 87, press: [jump, dash]}\n")
	// Halve the run speed from step 105.
	edited = editText(t, edited, "  - {step: 105, press: [right], axes: {move_x: 1}}\n", "  - {step: 105, press: [right], axes: {move_x: 0.5}}\n")
	// Add a one-step dash tap that was never recorded, with a comment, written in block style.
	edited = editText(t, edited, "  - {step: 195, release: [jump]}\n", "  - {step: 195, release: [jump]}\n  # extra dash to test the gap\n  - step: 200\n    press: [dash]\n  - {step: 201, release: [dash]}\n")

	rec, err := replay.Decode([]byte(edited))
	if err != nil {
		t.Fatalf("Decode edited: %v", err)
	}

	want := func(step int, s inputState) inputState {
		if step == 85 || step == 86 {
			s.jump, s.dash = false, false
		}
		if step >= 105 && step < 159 {
			s.moveX = 0.5
		}
		if step == 200 {
			s.dash = true
		}
		return s
	}

	op, ep := replay.NewPlayer(orig), replay.NewPlayer(rec)
	for step := range orig.TotalSteps {
		op.Step()
		ep.Step()
		if got, w := sample(ep), want(step, sample(op)); got != w {
			t.Fatalf("step %d: edited playback = %+v, want %+v", step, got, w)
		}
		if got := ep.JustPressed("jump"); got != (step == 87 || step == 125 || step == 179) {
			t.Fatalf("step %d: JustPressed(jump) = %v", step, got)
		}
		if got := ep.JustPressed("dash"); got != (step == 87 || step == 101 || step == 200) {
			t.Fatalf("step %d: JustPressed(dash) = %v", step, got)
		}
		if got := ep.JustReleased("dash"); got != (step == 95 || step == 102 || step == 201) {
			t.Fatalf("step %d: JustReleased(dash) = %v", step, got)
		}
	}
	if !ep.Done() {
		t.Fatal("edited playback not done")
	}

	// Saving the edited recording writes it back in the canonical one-line form, which decodes to the same input.
	canon, err := rec.Encode()
	if err != nil {
		t.Fatalf("Encode edited: %v", err)
	}
	if !strings.Contains(string(canon), "\n  - {step: 200, press: [dash]}\n") {
		t.Fatalf("canonical text lacks the added step:\n%s", canon)
	}
	again, err := replay.Decode(canon)
	if err != nil {
		t.Fatalf("Decode canonical: %v", err)
	}
	againText, err := again.Encode()
	if err != nil {
		t.Fatalf("Encode canonical: %v", err)
	}
	if string(againText) != string(canon) {
		t.Fatalf("canonical round trip changed:\n got:\n%s\nwant:\n%s", againText, canon)
	}
}

// TestEditedRecordingChangesTheRun checks that an edit reaches the simulation: dropping the last jump keeps the mini level from finishing the way the golden run does.
func TestEditedRecordingChangesTheRun(t *testing.T) {
	golden := readGolden(t)
	orig, err := replay.Decode([]byte(golden))
	if err != nil {
		t.Fatalf("Decode golden: %v", err)
	}
	edited := editText(t, golden, "  - {step: 179, press: [jump]}\n  - {step: 195, release: [jump]}\n", "")
	rec, err := replay.Decode([]byte(edited))
	if err != nil {
		t.Fatalf("Decode edited: %v", err)
	}
	before := replay.Run(t, orig, newMiniLevelSim)
	after := replay.Run(t, rec, newMiniLevelSim)
	if after.JumpCount != before.JumpCount-1 {
		t.Fatalf("jumps after edit = %d, want %d", after.JumpCount, before.JumpCount-1)
	}
	if approxLevelStateEqual(after, before) {
		t.Fatalf("edited run ended in the same state as the golden run: %+v", after)
	}
}

// TestDecodeKeepsNonCanonicalOrder checks that names and axis keys written in an order other than the header's survive a load and save unchanged.
func TestDecodeKeepsNonCanonicalOrder(t *testing.T) {
	text := `version: 2
seed: 18446744073709551615
rate: 30
actions: [a, b]
axes: [x, z]
total_steps: 3
steps:
  - {step: 0, press: [b, a], axes: {z: -0.125, x: 1e+21}}
  - {step: 2, release: [a, b], axes: {x: 0, z: 0}}
`
	rec, err := replay.Decode([]byte(text))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if rec.Seed != 1<<64-1 {
		t.Fatalf("seed = %d", rec.Seed)
	}
	out, err := rec.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(out) != text {
		t.Fatalf("round trip changed the text:\n got:\n%s\nwant:\n%s", out, text)
	}
}

// TestEncodeQuotesAwkwardNames checks that names YAML would read as other types or as syntax are quoted and survive the round trip.
func TestEncodeQuotesAwkwardNames(t *testing.T) {
	rec := &replay.Recording{
		Version:    replay.FormatVersion,
		Rate:       60,
		Actions:    []string{"yes", "1", "a: b", "[x]", "#c", "跳"},
		Axes:       []string{"null", "move x"},
		TotalSteps: 1,
		Events: []replay.StepEvent{{
			Step:  0,
			Press: []int{0, 1, 2, 3, 4, 5},
			Axes:  []replay.AxisChange{{Index: 0, Value: 1}, {Index: 1, Value: -1}},
		}},
	}
	text, err := rec.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Count(string(text), "\n") != 8 {
		t.Fatalf("want one line per step:\n%s", text)
	}
	got, err := replay.Decode(text)
	if err != nil {
		t.Fatalf("Decode:\n%s\n%v", text, err)
	}
	again, err := got.Encode()
	if err != nil {
		t.Fatalf("Encode again: %v", err)
	}
	if string(again) != string(text) {
		t.Fatalf("round trip changed:\n got:\n%s\nwant:\n%s", again, text)
	}
	for i, name := range rec.Actions {
		if got.Actions[i] != name {
			t.Fatalf("action %d = %q, want %q", i, got.Actions[i], name)
		}
	}
	for i, name := range rec.Axes {
		if got.Axes[i] != name {
			t.Fatalf("axis %d = %q, want %q", i, got.Axes[i], name)
		}
	}
}

// FuzzDecode checks that Decode never panics and that anything it accepts encodes to text that decodes and encodes to the same bytes again.
func FuzzDecode(f *testing.F) {
	f.Add([]byte(readGolden(f)))
	f.Add([]byte("version: 2\nseed: 1\nrate: 60\nactions: [a]\naxes: [x]\ntotal_steps: 3\nsteps:\n  - {step: 0, press: [a], axes: {x: -0}}\n  - {step: 2, release: [a]}\n"))
	f.Add([]byte("version: 2\nseed: 1\nrate: 60\naxes: [x]\ntotal_steps: 1\nsteps:\n  - {step: 0, axes: {x: -0.0}}\n"))
	f.Add([]byte("version: 2\nseed: 0\nrate: 1\ntotal_steps: 0\n"))
	f.Add([]byte("version: 2\nseed: 1\nrate: 60\nactions: []\ntotal_steps: 2\nsteps:\n  - {step: 1}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		rec, err := replay.Decode(data)
		if err != nil {
			return
		}
		first, err := rec.Encode()
		if err != nil {
			t.Fatalf("Encode of a decoded recording failed: %v", err)
		}
		again, err := replay.Decode(first)
		if err != nil {
			t.Fatalf("Decode of encoded text failed: %v\n%s", err, first)
		}
		second, err := again.Encode()
		if err != nil {
			t.Fatalf("second Encode failed: %v", err)
		}
		if string(first) != string(second) {
			t.Fatalf("encoding is not stable:\nfirst:\n%s\nsecond:\n%s", first, second)
		}
	})
}

// BenchmarkDecode measures reading the golden recording's text.
func BenchmarkDecode(b *testing.B) {
	data := []byte(readGolden(b))
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := replay.Decode(data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncode measures writing the golden recording as text.
func BenchmarkEncode(b *testing.B) {
	rec, err := replay.Decode([]byte(readGolden(b)))
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if _, err := rec.Encode(); err != nil {
			b.Fatal(err)
		}
	}
}

// TestNegativeZeroAxisIsWrittenAsZero checks that -0, whether written by hand or set in code, encodes as 0 so a second load and save does not change the text.
func TestNegativeZeroAxisIsWrittenAsZero(t *testing.T) {
	const want = "version: 2\nseed: 1\nrate: 60\naxes: [x]\ntotal_steps: 1\nsteps:\n  - {step: 0, axes: {x: 0}}\n"
	rec, err := replay.Decode([]byte("version: 2\nseed: 1\nrate: 60\naxes: [x]\ntotal_steps: 1\nsteps:\n  - {step: 0, axes: {x: -0.0}}\n"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if v := rec.Events[0].Axes[0].Value; math.Signbit(v) {
		t.Fatalf("decoded axis value = %v, want +0", v)
	}
	out, err := rec.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(out) != want {
		t.Fatalf("Encode of decoded -0.0:\n got:\n%s\nwant:\n%s", out, want)
	}

	built := &replay.Recording{
		Version:    replay.FormatVersion,
		Seed:       1,
		Rate:       60,
		Axes:       []string{"x"},
		TotalSteps: 1,
		Events:     []replay.StepEvent{{Step: 0, Axes: []replay.AxisChange{{Index: 0, Value: math.Copysign(0, -1)}}}},
	}
	out, err = built.Encode()
	if err != nil {
		t.Fatalf("Encode built: %v", err)
	}
	if string(out) != want {
		t.Fatalf("Encode of code-built -0:\n got:\n%s\nwant:\n%s", out, want)
	}
}
