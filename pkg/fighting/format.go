package fighting

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

const (
	fixMoveSet     = "write the move set as version: 1 YAML with input bindings and valid moves"
	fixReadMoveSet = "check that the move set file exists and is readable"
)

// FormatVersion is the move set schema version written by Encode and required by Parse.
const FormatVersion = 1

// MaxSteps bounds every step count in a move set (windows, buffers, charge times and combo gaps) to ten seconds at 60 steps per second, so a document cannot make a Recognizer allocate an unbounded history.
const MaxSteps = 600

// MaxButtons is the largest number of distinct button actions a move set may name.
const MaxButtons = 32

// DefaultDeadzone is the axis magnitude at or above which an axis counts as pushed when Input.Deadzone is zero.
const DefaultDeadzone = 0.5

// MoveSet is the root of a versioned YAML move set: how directions are read, the moves to recognize and the combos built from them.
type MoveSet struct {
	// Version is the schema version; it must equal FormatVersion.
	Version int `yaml:"version"`
	// Input names the actions and axes that make up the stick or pad.
	Input Input `yaml:"input"`
	// Moves are the motion inputs and button presses to recognize, in definition order.
	Moves []Move `yaml:"moves"`
	// Combos are sequences of moves performed with bounded gaps.
	Combos []Combo `yaml:"combos,omitempty"`
}

// Input names the actions and axes a Recognizer reads to build a direction each step. Actions and axes may be mixed; their contributions are summed per axis before the deadzone applies.
type Input struct {
	// Up, Down, Left and Right are digital action names. Pressing both opposite actions cancels that axis (neutral SOCD).
	Up    string `yaml:"up,omitempty"`
	Down  string `yaml:"down,omitempty"`
	Left  string `yaml:"left,omitempty"`
	Right string `yaml:"right,omitempty"`
	// AxisX is an analog axis name whose positive values point right.
	AxisX string `yaml:"axisX,omitempty"`
	// AxisY is an analog axis name whose positive values point down, as in screen coordinates and most gamepads; set YUp when positive values point up.
	AxisY string `yaml:"axisY,omitempty"`
	// YUp makes positive AxisY values point up.
	YUp bool `yaml:"yUp,omitempty"`
	// Deadzone is the magnitude in (0, 1] at or above which an axis counts as pushed; zero means DefaultDeadzone.
	Deadzone float64 `yaml:"deadzone,omitempty"`
}

// Move is one recognizable input: a motion, a button press, or a motion finished by a button press.
//
// Motion steps are matched on distinct, increasing steps of the facing-relative direction history, so ["6", "5", "6"] is a forward double tap and ["2", "3", "6"] a quarter circle forward. All step counts are inclusive. Inputs that completed a move cannot complete it again: its next completion needs a motion that starts after that step.
type Move struct {
	// Name identifies the move in results and combos; it must be unique across moves and combos.
	Name string `yaml:"name"`
	// Motion is the ordered direction sequence. Each entry is a string of one or more distinct numpad digits, any of which satisfies that step, so "23" accepts down or down-forward.
	Motion []string `yaml:"motion,omitempty"`
	// Window is the most steps allowed from the first to the last motion entry. It must be at least len(Motion)-1, and is refused on a move with fewer than 2 motion entries.
	Window int `yaml:"window,omitempty"`
	// Charge requires a direction to be held before the motion starts.
	Charge *Charge `yaml:"charge,omitempty"`
	// Press lists button action names that finish the move. Without PressAll, pressing any one of them on the current step finishes it.
	Press []string `yaml:"press,omitempty"`
	// PressAll requires every Press button to be pressed within PressWindow steps, one of them on the current step.
	PressAll bool `yaml:"pressAll,omitempty"`
	// PressWindow is the most steps between the first and last button of a PressAll chord.
	PressWindow int `yaml:"pressWindow,omitempty"`
	// Buffer is the most steps the finishing press may come after the last motion entry. It needs both Motion and Press; a move without Press finishes on the first step its motion fits.
	Buffer int `yaml:"buffer,omitempty"`
	// Priority orders moves finished on the same step, highest first; ties keep definition order.
	Priority int `yaml:"priority,omitempty"`
}

// Charge is a direction held for a number of consecutive steps before a motion starts.
type Charge struct {
	// Dirs is a string of numpad digits any of which counts as holding the charge, such as "147" for any back direction.
	Dirs string `yaml:"dirs"`
	// Steps is the least number of consecutive steps the charge must be held.
	Steps int `yaml:"steps"`
	// Grace is the most steps allowed between the end of the charge and the first motion entry, beyond the step that immediately follows it.
	Grace int `yaml:"grace,omitempty"`
}

// Combo is a sequence of moves where each move follows the previous one within Gap steps.
type Combo struct {
	// Name identifies the combo in results; it must be unique across moves and combos.
	Name string `yaml:"name"`
	// Sequence lists at least two move names in order.
	Sequence []string `yaml:"sequence"`
	// Gap is the most steps between consecutive moves of the sequence, at least 1.
	Gap int `yaml:"gap"`
}

// Parse decodes a YAML move set and validates it. Unknown fields and trailing YAML documents are refused.
func Parse(data []byte) (*MoveSet, error) {
	set, err := parseMoveSet(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixMoveSet)
	}
	return set, nil
}

func parseMoveSet(data []byte) (*MoveSet, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixMoveSet, "fighting: empty move set document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var set MoveSet
	if err := dec.Decode(&set); err != nil {
		return nil, fmt.Errorf("fighting: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document in the move set; remove the trailing --- document", "fighting: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("fighting: decode: %w", err)
	}
	if err := set.Validate(); err != nil {
		return nil, err
	}
	return &set, nil
}

// LoadFile reads and parses the YAML move set at path.
func LoadFile(path string) (*MoveSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, loaderr.Locate(fmt.Errorf("fighting: %w", err), path, nil, fixReadMoveSet)
	}
	set, err := Parse(data)
	if err != nil {
		return nil, loaderr.WithFile(err, path)
	}
	return set, nil
}

// Encode validates set and serializes it as deterministic 2-space YAML with fields in schema order, so Parse followed by Encode reproduces Encode's output byte for byte.
func Encode(set *MoveSet) ([]byte, error) {
	if err := set.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(set); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("fighting: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("fighting: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks set against the schema: the version, input names, at least one move, unique non-empty names, well-formed motions and charges, step counts within [0, MaxSteps], at most MaxButtons buttons and combos that reference existing moves.
func (set *MoveSet) Validate() error {
	if set == nil {
		return loaderr.Errorf("", fixMoveSet, "fighting: nil move set")
	}
	if set.Version != FormatVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version to %d", FormatVersion), "fighting: unsupported version %d (want %d)", set.Version, FormatVersion)
	}
	in := set.Input
	if in.Up == "" && in.Down == "" && in.Left == "" && in.Right == "" && in.AxisX == "" && in.AxisY == "" {
		return loaderr.Errorf("input", "set at least one direction action (up, down, left, right) or axis (axisX, axisY)", "fighting: input names no actions or axes")
	}
	if math.IsNaN(in.Deadzone) || in.Deadzone < 0 || in.Deadzone > 1 {
		return loaderr.Errorf(loaderr.Path("input", "deadzone"), "set deadzone to a finite number in [0, 1]", "fighting: input deadzone %v outside [0, 1]", in.Deadzone)
	}
	if len(set.Moves) == 0 {
		return loaderr.Errorf("moves", "add at least one move under moves", "fighting: move set has no moves")
	}
	names := make(map[string]bool, len(set.Moves)+len(set.Combos))
	for i := range set.Moves {
		m := &set.Moves[i]
		mPath := loaderr.Path("moves", i)
		if m.Name == "" {
			return loaderr.Errorf(loaderr.Join(mPath, "name"), "give the move a non-empty name", "fighting: move %d has no name", i)
		}
		if names[m.Name] {
			return loaderr.Errorf(loaderr.Join(mPath, "name"), "give each move and combo a unique name", "fighting: duplicate name %q", m.Name)
		}
		names[m.Name] = true
		if err := validateMove(mPath, m); err != nil {
			return err
		}
	}
	if n := len(set.Buttons()); n > MaxButtons {
		return loaderr.Errorf("moves", fmt.Sprintf("use at most %d distinct press button actions across all moves", MaxButtons), "fighting: %d buttons (max %d)", n, MaxButtons)
	}
	for i := range set.Combos {
		c := &set.Combos[i]
		cPath := loaderr.Path("combos", i)
		if c.Name == "" {
			return loaderr.Errorf(loaderr.Join(cPath, "name"), "give the combo a non-empty name", "fighting: combo %d has no name", i)
		}
		if names[c.Name] {
			return loaderr.Errorf(loaderr.Join(cPath, "name"), "give each move and combo a unique name", "fighting: duplicate name %q", c.Name)
		}
		names[c.Name] = true
		if len(c.Sequence) < 2 {
			return loaderr.Errorf(loaderr.Join(cPath, "sequence"), "list at least 2 move names in sequence", "fighting: combo %q needs at least 2 moves", c.Name)
		}
		for j, step := range c.Sequence {
			if !set.hasMove(step) {
				return loaderr.Errorf(loaderr.Join(cPath, "sequence", j), "name a move declared under moves", "fighting: combo %q references unknown move %q", c.Name, step)
			}
		}
		if c.Gap < 1 || c.Gap > MaxSteps {
			return loaderr.Errorf(loaderr.Join(cPath, "gap"), fmt.Sprintf("set gap to an integer in [1, %d]", MaxSteps), "fighting: combo %q gap %d outside [1, %d]", c.Name, c.Gap, MaxSteps)
		}
	}
	return nil
}

func validateMove(mPath string, m *Move) error {
	if len(m.Motion) == 0 && len(m.Press) == 0 {
		return loaderr.Errorf(mPath, "set motion, press or both on the move", "fighting: move %q has neither motion nor press", m.Name)
	}
	for i, entry := range m.Motion {
		if _, ok := parseDirSet(entry); !ok {
			return loaderr.Errorf(loaderr.Join(mPath, "motion", i), `write a string of distinct numpad digits 1-9 such as "2", "3", "6" or "23"`, "fighting: move %q motion entry %q is not a set of distinct digits 1-9", m.Name, entry)
		}
	}
	if err := checkSteps(loaderr.Join(mPath, "window"), m.Name, "window", m.Window); err != nil {
		return err
	}
	if len(m.Motion) < 2 && m.Window > 0 {
		return loaderr.Errorf(loaderr.Join(mPath, "window"), "remove window or add at least 2 motion entries", "fighting: move %q window needs at least 2 motion entries", m.Name)
	}
	if len(m.Motion) > 0 && m.Window < len(m.Motion)-1 {
		return loaderr.Errorf(loaderr.Join(mPath, "window"), fmt.Sprintf("set window to at least %d", len(m.Motion)-1), "fighting: move %q window %d is shorter than its %d motion entries need", m.Name, m.Window, len(m.Motion))
	}
	if err := checkSteps(loaderr.Join(mPath, "buffer"), m.Name, "buffer", m.Buffer); err != nil {
		return err
	}
	if err := checkSteps(loaderr.Join(mPath, "pressWindow"), m.Name, "pressWindow", m.PressWindow); err != nil {
		return err
	}
	if m.Charge != nil {
		cPath := loaderr.Join(mPath, "charge")
		if len(m.Motion) == 0 {
			return loaderr.Errorf(cPath, "add a motion sequence or remove charge", "fighting: move %q has a charge but no motion", m.Name)
		}
		if _, ok := parseDirSet(m.Charge.Dirs); !ok {
			return loaderr.Errorf(loaderr.Join(cPath, "dirs"), `write distinct numpad digits 1-9 such as "147"`, "fighting: move %q charge dirs %q is not a set of distinct digits 1-9", m.Name, m.Charge.Dirs)
		}
		if m.Charge.Steps < 1 || m.Charge.Steps > MaxSteps {
			return loaderr.Errorf(loaderr.Join(cPath, "steps"), fmt.Sprintf("set charge steps to an integer in [1, %d]", MaxSteps), "fighting: move %q charge steps %d outside [1, %d]", m.Name, m.Charge.Steps, MaxSteps)
		}
		if err := checkSteps(loaderr.Join(cPath, "grace"), m.Name, "charge grace", m.Charge.Grace); err != nil {
			return err
		}
	}
	if (m.PressAll || m.PressWindow > 0) && len(m.Press) < 2 {
		field := "pressAll"
		if !m.PressAll {
			field = "pressWindow"
		}
		return loaderr.Errorf(loaderr.Join(mPath, field), "list at least 2 buttons in press, or remove pressAll and pressWindow", "fighting: move %q pressAll and pressWindow need at least 2 press buttons", m.Name)
	}
	if m.PressWindow > 0 && !m.PressAll {
		return loaderr.Errorf(loaderr.Join(mPath, "pressWindow"), "set pressAll: true alongside pressWindow, or remove pressWindow", "fighting: move %q pressWindow needs pressAll", m.Name)
	}
	if m.Buffer > 0 && len(m.Press) == 0 {
		return loaderr.Errorf(loaderr.Join(mPath, "buffer"), "add press buttons alongside buffer, or remove buffer", "fighting: move %q buffer needs press buttons", m.Name)
	}
	if m.Buffer > 0 && len(m.Motion) == 0 {
		return loaderr.Errorf(loaderr.Join(mPath, "buffer"), "add a motion sequence alongside buffer, or remove buffer", "fighting: move %q buffer needs a motion", m.Name)
	}
	seen := make(map[string]bool, len(m.Press))
	for i, b := range m.Press {
		if b == "" {
			return loaderr.Errorf(loaderr.Join(mPath, "press", i), "name a non-empty button action or remove the entry", "fighting: move %q has an empty press button", m.Name)
		}
		if seen[b] {
			return loaderr.Errorf(loaderr.Join(mPath, "press", i), "remove the duplicate button from press", "fighting: move %q presses %q twice", m.Name, b)
		}
		seen[b] = true
	}
	return nil
}

func checkSteps(path, move, field string, v int) error {
	if v < 0 || v > MaxSteps {
		return loaderr.Errorf(path, fmt.Sprintf("set %s to an integer in [0, %d]", field, MaxSteps), "fighting: move %q %s %d outside [0, %d]", move, field, v, MaxSteps)
	}
	return nil
}

func (set *MoveSet) hasMove(name string) bool {
	for i := range set.Moves {
		if set.Moves[i].Name == name {
			return true
		}
	}
	return false
}

// Buttons returns the distinct button action names the moves press, in order of first appearance. Bit i of Frame.Held and of the held mask passed to Recognizer.Feed is Buttons()[i].
func (set *MoveSet) Buttons() []string {
	var out []string
	seen := make(map[string]bool)
	for i := range set.Moves {
		for _, b := range set.Moves[i].Press {
			if !seen[b] {
				seen[b] = true
				out = append(out, b)
			}
		}
	}
	return out
}

// Lookback returns how many past steps, including the current one, a Recognizer must keep to evaluate every move in set.
func (set *MoveSet) Lookback() int {
	n := 2
	for i := range set.Moves {
		m := &set.Moves[i]
		need := m.Window + m.Buffer + 1
		if m.Charge != nil {
			need += m.Charge.Steps + m.Charge.Grace + 1
		}
		need = max(need, m.PressWindow+1) + 1
		n = max(n, need)
	}
	return n
}
