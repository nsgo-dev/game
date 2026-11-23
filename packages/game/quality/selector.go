package quality

import (
	"time"
)

// Frames the Selector skips after it starts or changes level, while the change's own work (rebuilt buffers, fewer or more particles) settles.
const warmupFrames = 30

// Frames longer than this are hitches, such as loading, a debugger or the first frame back from the background, and do not count.
const hitchLimit = 500 * time.Millisecond

// Selector chooses the quality level a game runs at: the level its Config gives the Device to start, moved by measured frame times while the setting is auto, or the level the player sets.
//
// Feed it each frame's length with Observe, for example from game.Loop.FrameTime. While auto, it judges frames a window at a time: when more than a quarter of a window's frames run slower than the target frame rate it steps down a level, and after Adapt.UpgradeWindows windows in a row with nine in ten frames inside 60% of the target frame time it steps up one. It never steps back up to a level it stepped down from, so it settles rather than swinging between two levels.
//
// A Selector is not safe for concurrent use; use it from the scope that runs the loop.
type Selector struct {
	// OnChange, when set, is called with the new level and its preset whenever the level changes, from Observe or SetSetting.
	OnChange func(Level, Preset)

	cfg    Config
	start  Level
	level  Level
	auto   bool
	failed [levelCount]bool

	target, fast       time.Duration
	window, upgrades   int
	warm               int
	frames, slow, good int
	quick              int
}

// NewSelector returns a Selector for d that starts at cfg.StartLevel(d) with the setting auto. It returns an error when cfg is not valid.
func NewSelector(cfg Config, d Device) (*Selector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	start := cfg.StartLevel(d)
	target := time.Second / time.Duration(cfg.Adapt.targetFPS())
	s := &Selector{
		cfg:      cfg,
		start:    start,
		level:    start,
		auto:     true,
		target:   target,
		fast:     target * 6 / 10,
		window:   cfg.Adapt.windowFrames(),
		upgrades: cfg.Adapt.upgradeWindows(),
	}
	s.restart()
	return s, nil
}

// Level returns the current level.
func (s *Selector) Level() Level { return s.level }

// Preset returns the current level's preset.
func (s *Selector) Preset() Preset { return s.cfg.Presets.For(s.level) }

// StartLevel returns the level the device started at, the one auto returns to.
func (s *Selector) StartLevel() Level { return s.start }

// Auto reports whether the setting is auto, so frame times move the level.
func (s *Selector) Auto() bool { return s.auto }

// Setting returns the setting to store in the player's settings: "auto" or the fixed level's name.
func (s *Selector) Setting() string {
	if s.auto {
		return SettingAuto
	}
	return s.level.String()
}

// SetSetting applies a player's setting as NormalizeSetting reads it: "auto" (or empty) returns to the starting level and lets frame times move it again, and a level's name holds that level. It returns an error, changing nothing, for anything else.
func (s *Selector) SetSetting(setting string) error {
	norm, err := NormalizeSetting(setting)
	if err != nil {
		return err
	}
	next := s.start
	auto := norm == SettingAuto
	if !auto {
		next, _ = ParseLevel(norm)
	}
	s.auto = auto
	s.failed = [levelCount]bool{}
	s.restart()
	s.set(next)
	return nil
}

// Observe records one frame's length and reports whether it changed the level. It does nothing while the setting holds a fixed level or Config.Adapt.Off is set, and ignores lengths that are not positive or longer than half a second.
func (s *Selector) Observe(dt time.Duration) bool {
	if !s.auto || s.cfg.Adapt.Off || dt <= 0 || dt > hitchLimit {
		return false
	}
	if s.warm > 0 {
		s.warm--
		return false
	}
	s.frames++
	// A twentieth of slack keeps a display paced exactly at the target, whose frames jitter around it, from counting as slow.
	if dt > s.target+s.target/20 {
		s.slow++
	}
	if dt <= s.fast {
		s.quick++
	}
	if s.frames < s.window {
		return false
	}
	frames, slow, quick := s.frames, s.slow, s.quick
	s.frames, s.slow, s.quick = 0, 0, 0
	switch {
	case slow*4 > frames:
		s.good = 0
		if s.level > Low {
			s.failed[s.level] = true
			s.restart()
			return s.set(s.level - 1)
		}
	case quick*10 >= frames*9:
		s.good++
		if s.good >= s.upgrades && s.level < High && !s.failed[s.level+1] {
			s.restart()
			return s.set(s.level + 1)
		}
	default:
		s.good = 0
	}
	return false
}

// restart clears the window and skips the next frames while a change settles.
func (s *Selector) restart() {
	s.frames, s.slow, s.quick, s.good = 0, 0, 0, 0
	s.warm = warmupFrames
}

// set moves to l, calling OnChange, and reports whether the level changed.
func (s *Selector) set(l Level) bool {
	if l == s.level {
		return false
	}
	s.level = l
	if s.OnChange != nil {
		s.OnChange(l, s.cfg.Presets.For(l))
	}
	return true
}
