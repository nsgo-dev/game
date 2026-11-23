package rhythm

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

// CalibrationText is the words a CalibrationScreen shows, for a game to translate. Progress, Result and Unsteady are fmt formats.
type CalibrationText struct {
	// AudioPrompt and VideoPrompt ask the player to tap along to the clicks or the flashes.
	AudioPrompt, VideoPrompt string
	// Progress shows the taps counted and needed, as "%d / %d".
	Progress string
	// Result shows a steady result's offset in milliseconds, as "%+d ms".
	Result string
	// Unsteady shows a result too spread out to keep, given its spread in milliseconds.
	Unsteady string
	// Retry and Save label the buttons that start over and keep the result.
	Retry, Save string
}

// DefaultCalibrationText returns the English words.
func DefaultCalibrationText() CalibrationText {
	return CalibrationText{
		AudioPrompt: "Tap along to the clicks",
		VideoPrompt: "Tap along to the flashes",
		Progress:    "%d / %d",
		Result:      "Offset %+d ms",
		Unsteady:    "Taps spread %d ms apart: try again",
		Retry:       "Retry",
		Save:        "Save",
	}
}

// CalibrationScreen is the screen a player calibrates a device on, drawn into a core.Canvas with gameui: a prompt, the taps counted, the result, a Retry button and a Save button that keeps the result in an OffsetStore for the device. A video calibration flashes a disc at each cue; an audio one shows nothing that moves, so the player taps to what they hear.
//
// Draw it every frame from the canvas, which also drives its Calibration. A tap is taken on the edge it goes down, as gameplay judges a hit, never on a completed press, which comes only as the finger lifts: wrap the canvas in a core.View and pass its OnPressDown, OnPress and OnKeyDown here. A press going down off the buttons is a tap and a press on a button presses it; a key going down is a tap unless it moves the focus or presses a focused button, and once the calibration is done the focus moves to Save, or to Retry for an unsteady result, so a player on a keyboard or a switch reaches them.
type CalibrationScreen struct {
	// Text is the words shown; DefaultCalibrationText until set.
	Text CalibrationText
	// FlashColor is the video calibration's flash; the gameui theme's accent when 0.
	FlashColor core.Color
	// OnSave runs after Save keeps the offsets, with the device's offsets as kept.
	OnSave func(Offsets)
	cal    *Calibration
	store  *OffsetStore
	device string
	root   *gameui.Root
	prompt *gameui.Label
	result *gameui.Label
	bar    *gameui.ProgressBar
	retry  *gameui.Button
	save   *gameui.Button
	// flash is where the video calibration's disc is drawn, and saveErr why the last Save failed.
	flash   *gameui.Panel
	saveErr error
	// level is how bright the flash is this frame.
	level float64
	// done is whether the calibration was done at the last sync, so the focus moves to a button once as it finishes.
	done bool
}

// NewCalibrationScreen returns a screen running cal and keeping its result for device in store. It starts cal.
func NewCalibrationScreen(cal *Calibration, store *OffsetStore, device string) *CalibrationScreen {
	s := &CalibrationScreen{Text: DefaultCalibrationText(), cal: cal, store: store, device: device}
	s.prompt = &gameui.Label{TextAlign: core.TextAlignCenter}
	s.flash = &gameui.Panel{Flex: 1, DrawBackground: s.drawFlash}
	s.bar = &gameui.ProgressBar{Height: 20}
	s.result = &gameui.Label{TextAlign: core.TextAlignCenter}
	s.retry = &gameui.Button{Flex: 1, OnClick: s.Retry}
	s.save = &gameui.Button{Flex: 1, OnClick: func() { _ = s.Save() }}
	s.root = gameui.NewRoot(&gameui.VBox{
		Padding: core.Edges{Top: 24, Right: 24, Bottom: 24, Left: 24},
		Spacing: 16,
		Children: []gameui.Widget{
			s.prompt, s.flash, s.bar, s.result,
			&gameui.HBox{Height: 44, Spacing: 12, Children: []gameui.Widget{s.retry, s.save}},
		},
	})
	cal.Start()
	return s
}

// Root returns the screen's gameui tree, to set its Theme or Viewport or route keys through it.
func (s *CalibrationScreen) Root() *gameui.Root { return s.root }

// Calibration returns the calibration the screen runs.
func (s *CalibrationScreen) Calibration() *Calibration { return s.cal }

// Err returns why the last Save failed, nil after one that kept the offsets.
func (s *CalibrationScreen) Err() error { return s.saveErr }

// Stop stops the calibration's clicks and taps, for when the screen goes away mid-run; Retry starts it again.
func (s *CalibrationScreen) Stop() { s.cal.Stop() }

// Retry starts the calibration over.
func (s *CalibrationScreen) Retry() {
	s.saveErr = nil
	s.root.ClearFocus()
	s.cal.Start()
}

// Save keeps the device's offsets with the calibration's result in the store and runs OnSave. It does nothing until the result is steady.
func (s *CalibrationScreen) Save() error {
	cur, _ := s.store.Get(s.device)
	o, ok := s.cal.Apply(cur)
	if !ok {
		return nil
	}
	if s.saveErr = s.store.Set(s.device, o); s.saveErr != nil {
		return s.saveErr
	}
	if s.OnSave != nil {
		s.OnSave(o)
	}
	return nil
}

// OnPressDown handles a press going down on the canvas: a tap, unless it lands on a button, which OnPress then presses. It reports whether the tap counted.
func (s *CalibrationScreen) OnPressDown(ev core.PressEvent) bool {
	if ev.Keyboard || s.root.HitTest(ev.Position) != nil {
		return false
	}
	_, counted := s.cal.Tap()
	return counted
}

// OnPress handles a completed press on the canvas: one on a button presses it, and Return or Space presses the focused button. Taps are taken by OnPressDown and OnKeyDown, not here. It reports whether it used the press.
func (s *CalibrationScreen) OnPress(ev core.PressEvent) bool {
	return s.root.OnPress(ev)
}

// OnKeyDown handles a key going down: Tab and the arrows move the focus among the buttons and Return or Space presses the focused one, as gameui.Root does; any other key going down, not a held key's repeat, is a tap. It reports whether it used the key.
func (s *CalibrationScreen) OnKeyDown(ev core.KeyEvent) bool {
	if s.root.OnKeyDown(ev) {
		return true
	}
	if ev.Repeat || s.cal.Done() {
		return false
	}
	_, counted := s.cal.Tap()
	return counted
}

// Draw updates the calibration, the words and the buttons, and draws the screen into bounds of d. A video calibration records the frame that first flashes each cue as when the cue was presented.
func (s *CalibrationScreen) Draw(d *core.Drawing, bounds core.Bounds) {
	s.cal.Update()
	s.level = 0
	if s.cal.Kind == CalibrateVideo {
		now := audio.Now(s.cal.cx)
		if k, level := s.cal.Pulse(now); k >= 0 && level > 0 {
			s.cal.Present(k, now)
			s.level = level
		}
	}
	s.sync()
	s.root.Draw(d, bounds)
}

// drawFlash draws the video calibration's disc, as bright as this frame's pulse, centered in b.
func (s *CalibrationScreen) drawFlash(d *core.Drawing, b core.Bounds) {
	r := float32(min(b.Size.Width, b.Size.Height)) / 2
	if s.level <= 0 || r <= 0 {
		return
	}
	color := s.FlashColor
	if color == 0 {
		th := gameui.DefaultTheme()
		if s.root.Theme != nil {
			th = *s.root.Theme
		}
		color = th.Accent
	}
	disc := new(core.Path).Ellipse(float32(b.Origin.X)+float32(b.Size.Width)/2, float32(b.Origin.Y)+float32(b.Size.Height)/2, r, r)
	d.SetAlpha(float32(s.level)).SetFillColor(color).FillPath(disc, core.NonZero)
}

// sync sets the widgets from the calibration.
func (s *CalibrationScreen) sync() {
	t := s.Text
	if s.cal.Kind == CalibrateVideo {
		s.prompt.Text = t.VideoPrompt
	} else {
		s.prompt.Text = t.AudioPrompt
	}
	need := s.cal.taps()
	s.bar.Min, s.bar.Max, s.bar.Value = 0, float32(need), float32(s.cal.Count())
	s.bar.Label = fmt.Sprintf(t.Progress, s.cal.Count(), need)
	s.retry.Text, s.save.Text = t.Retry, t.Save
	r, done := s.cal.Result()
	switch {
	case !done:
		s.result.Text = ""
	case r.Steady():
		s.result.Text = fmt.Sprintf(t.Result, int64(r.Offset.Round(time.Millisecond)/time.Millisecond))
	default:
		s.result.Text = fmt.Sprintf(t.Unsteady, int64(r.Spread.Round(time.Millisecond)/time.Millisecond))
	}
	s.save.Disabled = !done || !r.Steady()
	if done && !s.done {
		if r.Steady() {
			s.root.SetFocus(s.save)
		} else {
			s.root.SetFocus(s.retry)
		}
	}
	s.done = done
}
