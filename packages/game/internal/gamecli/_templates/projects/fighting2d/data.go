package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/fighting"
	"github.com/nsgo-dev/game/packages/game/framedata"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// dataFS holds the game's YAML data: data/match.yaml, the rules, the CPU opponent and the roster, and for each fighter a move set (package fighting) and frame data (package framedata). It is embedded so the game reads the same files on every target, the web and phones included.
//
//go:embed data
var dataFS embed.FS

// matchPath is the match file inside dataFS.
const matchPath = "data/match.yaml"

// matchVersion is the version of the match file format that parseMatch reads and encodeMatch writes.
const matchVersion = 1

// matchFile is data/match.yaml: how a match is played, how the CPU opponent plays and who can be picked.
type matchFile struct {
	// Version is the format version; it must be matchVersion.
	Version int `yaml:"version"`
	// Rules are the match rules and the stage.
	Rules rules `yaml:"rules"`
	// CPU tunes the computer opponent.
	CPU cpuConfig `yaml:"cpu"`
	// Fighters are the characters to pick from, in the order the title screen cycles through them.
	Fighters []fighterDef `yaml:"fighters"`
}

// rules are the rules of a match and the size of the stage, in steps of the 60 Hz loop and virtual points.
type rules struct {
	// RoundsToWin is how many rounds a fighter must take to win the match.
	RoundsToWin int `yaml:"roundsToWin"`
	// RoundSeconds is the round timer.
	RoundSeconds int `yaml:"roundSeconds"`
	// IntroSteps is how long the round announcement lasts before the fighters may act.
	IntroSteps int `yaml:"introSteps"`
	// OutroSteps is how long a finished round stays on screen.
	OutroSteps int `yaml:"outroSteps"`
	// Floor is the height of the ground the fighters stand on.
	Floor float32 `yaml:"floor"`
	// StageLeft and StageRight are the walls the fighters' origins stay between.
	StageLeft  float32 `yaml:"stageLeft"`
	StageRight float32 `yaml:"stageRight"`
	// StartGap is how far apart the fighters start each round.
	StartGap float32 `yaml:"startGap"`
	// PushWidth is the closest two grounded fighters' origins may come.
	PushWidth float32 `yaml:"pushWidth"`
	// Gravity is the downward acceleration of an airborne fighter, in points per step per step.
	Gravity float32 `yaml:"gravity"`
	// Hitstop freezes both fighters for this many steps when a hit or a block lands.
	Hitstop int `yaml:"hitstop"`
	// ChipDivisor divides the damage of a blocked special move into the damage the defender still takes.
	ChipDivisor int `yaml:"chipDivisor"`
	// ComboScaling is the percentage each further hit of a combo loses, down to MinScaling.
	ComboScaling int `yaml:"comboScaling"`
	// MinScaling is the least percentage of its damage a hit late in a combo deals.
	MinScaling int `yaml:"minScaling"`
}

// cpuConfig tunes the computer opponent. Chances are percentages rolled at each decision.
type cpuConfig struct {
	// Seed seeds the opponent's random choices, so a match against it replays the same way from the same inputs.
	Seed uint64 `yaml:"seed"`
	// Reaction is how many steps pass between its decisions.
	Reaction int `yaml:"reaction"`
	// Block is the chance it guards an attack it sees coming.
	Block int `yaml:"block"`
	// AntiAir is the chance it answers a jump with its anti-air script.
	AntiAir int `yaml:"antiAir"`
	// Projectile is the chance it throws its projectile from afar.
	Projectile int `yaml:"projectile"`
	// JumpIn is the chance it jumps in from mid range.
	JumpIn int `yaml:"jumpIn"`
}

// fighterDef is one character of the roster.
type fighterDef struct {
	// ID names the fighter in code and tests; it is unique and also names its data files.
	ID string `yaml:"id"`
	// Name is the name the HUD shows.
	Name string `yaml:"name"`
	// Color and Accent are the body and the strike colors, as "#rrggbb".
	Color  string `yaml:"color"`
	Accent string `yaml:"accent"`
	// Health is the fighter's health at the start of each round.
	Health int `yaml:"health"`
	// WalkSpeed and BackSpeed are how fast it walks forward and back, in points per step.
	WalkSpeed float32 `yaml:"walkSpeed"`
	BackSpeed float32 `yaml:"backSpeed"`
	// JumpSpeed is its upward speed as it leaves the ground and JumpDrift its sideways speed in a jump toward or away from the opponent.
	JumpSpeed float32 `yaml:"jumpSpeed"`
	JumpDrift float32 `yaml:"jumpDrift"`
	// MoveSet is the fighter's move set, a fighting.MoveSet file in data/.
	MoveSet string `yaml:"moveSet"`
	// FrameData is the fighter's frame data, a framedata.Doc file in data/.
	FrameData string `yaml:"frameData"`
	// Moves add what frame data does not say to some of its moves.
	Moves []moveExtra `yaml:"moves,omitempty"`
	// Scripts are the input sequences the CPU opponent plays this fighter with.
	Scripts scripts `yaml:"scripts"`
}

// Hit levels: a low must be guarded crouching and an overhead standing; any other hit, a mid, either way.
const (
	levelMid      = ""
	levelLow      = "low"
	levelOverhead = "overhead"
)

// moveExtra is what the game adds to a move of the frame data: how high it hits, whether it is special, how it moves the fighter and the projectile it throws.
type moveExtra struct {
	// Name is the frame data move it describes.
	Name string `yaml:"name"`
	// Level is levelLow or levelOverhead, or empty for a mid.
	Level string `yaml:"level,omitempty"`
	// Special moves deal chip damage when guarded.
	Special bool `yaml:"special,omitempty"`
	// Motion moves the fighter on ranges of the move's frames.
	Motion []motion `yaml:"motion,omitempty,flow"`
	// Projectile is thrown on one frame of the move.
	Projectile *projectileDef `yaml:"projectile,omitempty,flow"`
}

// motion moves a fighter by DX points toward the side it faces and DY points down on each of the frames Start to End; a negative DY lifts it into the air, where gravity takes it once the range ends.
type motion struct {
	Start int     `yaml:"start"`
	End   int     `yaml:"end"`
	DX    float32 `yaml:"dx,omitempty"`
	DY    float32 `yaml:"dy,omitempty"`
}

// projectileDef is a projectile a move throws: a looping frame data move of the same document that flies straight ahead until it strikes, leaves the stage or runs out of life. A fighter has one projectile out at a time.
type projectileDef struct {
	// Frame is the frame of the throwing move it appears on.
	Frame int `yaml:"frame"`
	// Move is the looping frame data move the projectile plays.
	Move string `yaml:"move"`
	// X and Y place it from the thrower's origin, X toward the side it faces.
	X float32 `yaml:"x,omitempty"`
	Y float32 `yaml:"y,omitempty"`
	// Speed is how far it flies each step.
	Speed float32 `yaml:"speed"`
	// Life is how many steps it lasts at most.
	Life int `yaml:"life"`
}

// scripts are the CPU opponent's input sequences for a fighter. Each step is a numpad direction relative to the side the fighter faces, optionally followed by "+button" for each button held, and "*n" to hold it for n steps instead of one, such as "2*2", "3*2", "6+light*2" for a quarter circle forward and a light press.
type scripts struct {
	// Projectile throws the fighter's projectile.
	Projectile []string `yaml:"projectile,flow"`
	// AntiAir answers an opponent jumping in.
	AntiAir []string `yaml:"antiAir,flow"`
	// Combo is a close-range string of attacks.
	Combo []string `yaml:"combo,flow"`
}

// parseMatch decodes and validates a match file, refusing unknown fields and trailing documents.
func parseMatch(data []byte) (*matchFile, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("match: empty document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var m matchFile
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("match: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("match: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("match: decode: %w", err)
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// encodeMatch validates m and writes it as deterministic 2-space YAML in field order, which parseMatch reads back unchanged.
func encodeMatch(m *matchFile) ([]byte, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("match: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("match: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// validate checks the parts of the match file that need nothing else: the version, positive rules, percentages, unique fighters with parseable colors and well-formed moves and scripts. loadRoster checks the references to the move sets and frame data.
func (m *matchFile) validate() error {
	if m.Version != matchVersion {
		return fmt.Errorf("match: unsupported version %d (want %d)", m.Version, matchVersion)
	}
	r := &m.Rules
	for _, v := range []struct {
		name string
		n    int
	}{{"roundsToWin", r.RoundsToWin}, {"roundSeconds", r.RoundSeconds}, {"introSteps", r.IntroSteps}, {"outroSteps", r.OutroSteps}, {"chipDivisor", r.ChipDivisor}} {
		if v.n < 1 {
			return fmt.Errorf("match: rules %s %d must be at least 1", v.name, v.n)
		}
	}
	if r.Hitstop < 0 || r.ComboScaling < 0 || r.ComboScaling > 100 || r.MinScaling < 1 || r.MinScaling > 100 {
		return fmt.Errorf("match: rules hitstop %d, comboScaling %d and minScaling %d must be at least 0, from 0 to 100 and from 1 to 100", r.Hitstop, r.ComboScaling, r.MinScaling)
	}
	if !positive(r.Floor, r.PushWidth, r.Gravity, r.StartGap) || !finite(r.StageLeft) || !finite(r.StageRight) || !(r.StageRight-r.StageLeft > r.StartGap) {
		return errors.New("match: rules floor, pushWidth, gravity and startGap must be positive, stageLeft and stageRight finite and the stage wider than startGap")
	}
	c := &m.CPU
	if c.Reaction < 1 {
		return fmt.Errorf("match: cpu reaction %d must be at least 1", c.Reaction)
	}
	for _, v := range []struct {
		name string
		n    int
	}{{"block", c.Block}, {"antiAir", c.AntiAir}, {"projectile", c.Projectile}, {"jumpIn", c.JumpIn}} {
		if v.n < 0 || v.n > 100 {
			return fmt.Errorf("match: cpu %s %d is not a percentage", v.name, v.n)
		}
	}
	if len(m.Fighters) < 1 {
		return errors.New("match: no fighters")
	}
	ids := map[string]bool{}
	for i := range m.Fighters {
		f := &m.Fighters[i]
		if f.ID == "" || ids[f.ID] {
			return fmt.Errorf("match: fighter %d has an empty or duplicate id %q", i, f.ID)
		}
		ids[f.ID] = true
		if err := f.validate(); err != nil {
			return fmt.Errorf("match: fighter %q: %w", f.ID, err)
		}
	}
	return nil
}

func (f *fighterDef) validate() error {
	if f.Name == "" {
		return errors.New("no name")
	}
	for _, c := range []string{f.Color, f.Accent} {
		if _, err := core.ParseColor(c); err != nil || !strings.HasPrefix(c, "#") {
			return fmt.Errorf("color %q is not #rrggbb", c)
		}
	}
	if f.Health < 1 {
		return fmt.Errorf("health %d must be at least 1", f.Health)
	}
	if !positive(f.WalkSpeed, f.BackSpeed, f.JumpSpeed) || !finite(f.JumpDrift) || f.JumpDrift < 0 {
		return errors.New("walkSpeed, backSpeed and jumpSpeed must be positive and jumpDrift at least 0")
	}
	if f.MoveSet == "" || f.FrameData == "" {
		return errors.New("no moveSet or frameData file")
	}
	names := map[string]bool{}
	for _, e := range f.Moves {
		if e.Name == "" || names[e.Name] {
			return fmt.Errorf("move extra with an empty or duplicate name %q", e.Name)
		}
		names[e.Name] = true
		switch e.Level {
		case levelMid, levelLow, levelOverhead:
		default:
			return fmt.Errorf("move %q has level %q, want %q or %q", e.Name, e.Level, levelLow, levelOverhead)
		}
		for _, mo := range e.Motion {
			if mo.Start < 1 || mo.End < mo.Start || !finite(mo.DX) || !finite(mo.DY) {
				return fmt.Errorf("move %q has a motion over frames %d to %d that is not 1 <= start <= end with finite dx and dy", e.Name, mo.Start, mo.End)
			}
		}
		if p := e.Projectile; p != nil {
			if p.Frame < 1 || p.Move == "" || !positive(p.Speed) || p.Life < 1 || !finite(p.X) || !finite(p.Y) {
				return fmt.Errorf("move %q has a projectile without a frame, a move, a positive speed and a life", e.Name)
			}
		}
	}
	for name, s := range map[string][]string{"projectile": f.Scripts.Projectile, "antiAir": f.Scripts.AntiAir, "combo": f.Scripts.Combo} {
		if len(s) == 0 {
			return fmt.Errorf("no %s script", name)
		}
		for _, step := range s {
			if _, err := parseScriptStep(step); err != nil {
				return fmt.Errorf("%s script: %w", name, err)
			}
		}
	}
	return nil
}

// scriptStep is one parsed step of a CPU script.
type scriptStep struct {
	dir     fighting.Direction
	buttons []string
	steps   int
}

// parseScriptStep parses a script step such as "6+light*2".
func parseScriptStep(s string) (scriptStep, error) {
	step := scriptStep{steps: 1}
	body, count, ok := strings.Cut(s, "*")
	if ok {
		n, err := strconv.Atoi(count)
		if err != nil || n < 1 || n > fighting.MaxSteps {
			return step, fmt.Errorf("step %q holds for %q steps, want 1 to %d", s, count, fighting.MaxSteps)
		}
		step.steps = n
	}
	parts := strings.Split(body, "+")
	if len(parts[0]) != 1 || parts[0][0] < '1' || parts[0][0] > '9' {
		return step, fmt.Errorf("step %q does not start with a numpad direction", s)
	}
	step.dir = fighting.Direction(parts[0][0] - '0')
	for _, b := range parts[1:] {
		if b == "" {
			return step, fmt.Errorf("step %q names an empty button", s)
		}
		step.buttons = append(step.buttons, b)
	}
	return step, nil
}

func positive(vs ...float32) bool {
	for _, v := range vs {
		if !finite(v) || v <= 0 {
			return false
		}
	}
	return true
}

func finite(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// fighter is a roster entry with its data loaded and checked against each other.
type fighter struct {
	def    *fighterDef
	moves  *fighting.MoveSet
	frames *framedata.Doc
	extras map[string]*moveExtra
	color  core.Color
	accent core.Color
	// buttons are the move set's buttons in bit order, as Recognizer.Buttons returns them.
	buttons []string
	// scripts are def.Scripts parsed, by name.
	scripts map[string][]scriptStep
}

// roster is the parsed match file with every fighter's data.
type roster struct {
	file     *matchFile
	fighters []*fighter
}

// stanceMoves are the frame data moves the game plays on its own, which every fighter must have.
var stanceMoves = []string{"idle", "walk", "crouch", "jump", "guard", "crouch_guard", "hurt", "ko"}

// loadRoster reads the match file and every fighter's move set and frame data from fsys and checks that they fit together: each recognized move has a frame data move to play standing, crouching or in the air, the stance moves are there, extras and projectiles name moves of the frame data, and scripts press buttons of the move set.
func loadRoster(fsys fs.FS) (*roster, error) {
	data, err := fs.ReadFile(fsys, matchPath)
	if err != nil {
		return nil, err
	}
	m, err := parseMatch(data)
	if err != nil {
		return nil, err
	}
	r := &roster{file: m}
	for i := range m.Fighters {
		f, err := loadFighter(fsys, &m.Fighters[i])
		if err != nil {
			return nil, fmt.Errorf("fighter %q: %w", m.Fighters[i].ID, err)
		}
		r.fighters = append(r.fighters, f)
	}
	return r, nil
}

func loadFighter(fsys fs.FS, def *fighterDef) (*fighter, error) {
	movesData, err := fs.ReadFile(fsys, "data/"+def.MoveSet)
	if err != nil {
		return nil, err
	}
	moves, err := fighting.Parse(movesData)
	if err != nil {
		return nil, err
	}
	framesData, err := fs.ReadFile(fsys, "data/"+def.FrameData)
	if err != nil {
		return nil, err
	}
	frames, err := framedata.Parse(framesData)
	if err != nil {
		return nil, err
	}
	f := &fighter{def: def, moves: moves, frames: frames, extras: map[string]*moveExtra{}, scripts: map[string][]scriptStep{}}
	f.color, _ = core.ParseColor(def.Color)
	f.accent, _ = core.ParseColor(def.Accent)
	for _, name := range stanceMoves {
		m, ok := frames.Move(name)
		if !ok {
			return nil, fmt.Errorf("frame data has no %q move", name)
		}
		if !m.Loop {
			return nil, fmt.Errorf("frame data move %q must loop", name)
		}
	}
	for _, m := range moves.Moves {
		if _, ok := f.variant(m.Name, false, false); !ok {
			if _, ok := f.variant(m.Name, false, true); !ok {
				return nil, fmt.Errorf("move set move %q has no frame data move", m.Name)
			}
		}
	}
	for i := range def.Moves {
		e := &def.Moves[i]
		m, ok := frames.Move(e.Name)
		if !ok {
			return nil, fmt.Errorf("extra for unknown move %q", e.Name)
		}
		for _, mo := range e.Motion {
			if mo.End > m.Length {
				return nil, fmt.Errorf("move %q motion ends on frame %d of %d", e.Name, mo.End, m.Length)
			}
		}
		if p := e.Projectile; p != nil {
			pm, ok := frames.Move(p.Move)
			if !ok || !pm.Loop || p.Frame > m.Length {
				return nil, fmt.Errorf("move %q throws %q, which is not a looping move, or on frame %d past its end", e.Name, p.Move, p.Frame)
			}
		}
		f.extras[e.Name] = e
	}
	f.buttons = moves.Buttons()
	for name, s := range map[string][]string{"projectile": def.Scripts.Projectile, "antiAir": def.Scripts.AntiAir, "combo": def.Scripts.Combo} {
		for _, text := range s {
			step, _ := parseScriptStep(text)
			for _, b := range step.buttons {
				if !slices.Contains(f.buttons, b) {
					return nil, fmt.Errorf("%s script presses %q, which the move set has no move for", name, b)
				}
			}
			f.scripts[name] = append(f.scripts[name], step)
		}
	}
	return f, nil
}

// variant returns the frame data move to play for the move set move name: air_<name> in the air, crouch_<name> or else name while crouching, and name standing.
func (f *fighter) variant(name string, airborne, crouching bool) (string, bool) {
	switch {
	case airborne:
		name = "air_" + name
	case crouching:
		if _, ok := f.frames.Move("crouch_" + name); ok {
			return "crouch_" + name, true
		}
	}
	_, ok := f.frames.Move(name)
	return name, ok
}

// extra returns the extra of move name, or an empty one.
func (f *fighter) extra(name string) *moveExtra {
	if e, ok := f.extras[name]; ok {
		return e
	}
	return &moveExtra{}
}
