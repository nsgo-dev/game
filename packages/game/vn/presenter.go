package vn

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/flow"
	"github.com/nsgo-dev/game/packages/game/text"
)

const (
	// DefaultCharsPerSecond is how fast a line's text appears when Options.CharsPerSecond is 0.
	DefaultCharsPerSecond = 40
	// DefaultAutoDelay is how long auto mode waits after a line's text and voice end when Options.AutoDelay is 0.
	DefaultAutoDelay = 1500 * time.Millisecond
	// DefaultBacklogSize is how many entries the backlog keeps when Options.BacklogSize is 0.
	DefaultBacklogSize = 200
)

// Line tags a Presenter reads off a dialogue line, written after the line in the Yarn script, such as "Aiko: Hello! #expr:smile #at:left #voice:aiko_001".
const (
	// TagExpression sets the speaker's expression: #expr:smile.
	TagExpression = "expr:"
	// TagSlot moves the speaker's portrait to a slot: #at:left.
	TagSlot = "at:"
	// TagVoice names the line's voice line: #voice:aiko_001.
	TagVoice = "voice:"
	// TagOffstage speaks the line without bringing the speaker's portrait on the stage: #offstage.
	TagOffstage = "offstage"
)

// Commands a Presenter runs itself rather than passing to DialogueOptions.OnCommand. Each still reaches DialogueOptions.Bus.
const (
	// CommandShow shows a character's portrait: <<show aiko>>, <<show aiko left>>, <<show aiko smile>> or <<show aiko left smile>>, the slot and expression in either order.
	CommandShow = "show"
	// CommandHide hides a character's portrait, <<hide aiko>>, or every portrait, <<hide>>.
	CommandHide = "hide"
	// CommandExpression changes a character's expression, showing the portrait if it is hidden: <<expr aiko smile>>.
	CommandExpression = "expr"
)

// Options configures a Presenter.
type Options struct {
	// Dialogue configures the presenter's flow.DialogueRunner. Its OnCommand receives every command but CommandShow, CommandHide and CommandExpression.
	Dialogue flow.DialogueOptions
	// Voices plays voice lines; nil plays none.
	Voices VoicePlayer
	// VoiceFromLineID plays a line's #line: id as its voice line when it has no #voice: tag.
	VoiceFromLineID bool
	// CharsPerSecond is how fast a line's text appears; 0 uses DefaultCharsPerSecond and a negative rate shows the whole line at once.
	CharsPerSecond float32
	// AutoDelay is how long auto mode waits after a line's text has appeared and its voice ended before it goes on; 0 uses DefaultAutoDelay and a negative delay waits none.
	AutoDelay time.Duration
	// AutoDelayPerRune adds this much to AutoDelay for each character of the line, so a long line stays longer.
	AutoDelayPerRune time.Duration
	// SkipInterval is how long skip mode shows each line; 0 shows one line per Update.
	SkipInterval time.Duration
	// SkipUnread lets skip mode go past lines not read before; when false, skip mode stops on the first line the player has not read.
	SkipUnread bool
	// BacklogSize is how many entries the backlog keeps, dropping the oldest; 0 uses DefaultBacklogSize and a negative size keeps none.
	BacklogSize int
}

// Portrait is a character's portrait on the stage.
type Portrait struct {
	// Character is the character's ID.
	Character string
	// Expression is the expression shown.
	Expression string
	// Frame is the expression's atlas frame.
	Frame string
	// Slot is the slot the portrait stands in.
	Slot string
	// X is the slot's X, the portrait's center as a fraction of the stage's width.
	X float32
	// Speaking reports whether the character speaks the line shown.
	Speaking bool
}

// Line is the dialogue line a Presenter shows.
type Line struct {
	// DialogueLine is the line as the runner produced it, tags included.
	flow.DialogueLine
	// Character is the speaking cast member's ID, empty for a speaker not in the cast and for narration.
	Character string
	// Name is the name plate's text: the cast member's display name, the speaker as the script writes it for a speaker not in the cast, or empty for narration.
	Name string
	// Color is the cast member's name plate color, empty for the style's.
	Color string
	// Voice is the line's voice line id, empty for none.
	Voice string
	// Read reports whether the player had read the line before it was shown this time.
	Read bool
}

// BacklogEntry is one line or chosen option in the backlog.
type BacklogEntry struct {
	// Character is the speaking cast member's ID, empty for none.
	Character string
	// Name is the name plate's text, empty for narration and for choices.
	Name string
	// Color is the cast member's name plate color, empty for the style's.
	Color string
	// Text is the line's text with its rich text markup.
	Text string
	// LineID is the line's #line: id, empty for none.
	LineID string
	// Voice is the line's voice line id, empty for none.
	Voice string
	// Choice reports that the entry is an option the player chose rather than a line.
	Choice bool
}

// Presenter plays a dialogue script as a visual novel: it runs the script with a flow.DialogueRunner and keeps the portraits on the stage, the line in the text box with its name plate, the typewriter revealing it, the choices, the backlog and the lines read, and plays voice lines. Auto mode goes on by itself after each line, and skip mode runs through lines quickly.
//
// It reads no clock: Update moves it on by the fixed step a game.Loop passes, so a scene plays alike every time and under nsgotest. Its methods are called on one goroutine.
type Presenter struct {
	cast   *Cast
	runner *flow.DialogueRunner
	opts   Options

	kind      flow.StepKind
	hasLine   bool
	line      Line
	tw        *text.Typewriter
	choices   []flow.DialogueChoice
	portraits []Portrait
	backlog   []BacklogEntry
	read      map[string]bool

	auto, skip bool
	autoWait   time.Duration
	skipWait   time.Duration
	voiceLeft  time.Duration
	// cmdErr is the first error a stage command raised during the runner call in progress.
	cmdErr error
}

// New returns a presenter that plays script with cast. Call Start to begin a node.
func New(script *flow.DialogueScript, cast *Cast, opts Options) *Presenter {
	p := &Presenter{cast: cast, opts: opts, read: map[string]bool{}}
	dopts := opts.Dialogue
	user := dopts.OnCommand
	dopts.OnCommand = func(ev flow.DialogueEvent) {
		switch ev.Command {
		case CommandShow, CommandHide, CommandExpression:
			if err := p.command(ev); err != nil && p.cmdErr == nil {
				p.cmdErr = err
			}
		default:
			if user != nil {
				user(ev)
			}
		}
	}
	p.runner = flow.NewDialogueRunner(script, dopts)
	return p
}

// Runner returns the presenter's dialogue runner, for its variables and save state. Advance it through the presenter, not directly.
func (p *Presenter) Runner() *flow.DialogueRunner { return p.runner }

// Cast returns the presenter's cast.
func (p *Presenter) Cast() *Cast { return p.cast }

// Start clears the stage and the line shown and begins the script at node. The backlog and the lines read stay.
func (p *Presenter) Start(node string) error {
	p.stopVoice()
	p.portraits = nil
	p.hasLine = false
	p.line = Line{}
	p.tw = nil
	p.choices = nil
	p.skipWait = 0
	p.cmdErr = nil
	step, err := p.runner.Start(node)
	return p.apply(step, err)
}

// Update moves the presenter on by dt: it reveals the line's text, counts down its voice, and in auto mode goes on once the line has stayed its delay, or in skip mode goes on to the next line. An error is the script's or a stage command's.
func (p *Presenter) Update(dt time.Duration) error {
	if dt <= 0 {
		return nil
	}
	// after is the part of dt after the voice line ended, which counts toward auto mode's delay.
	after := dt
	if p.voiceLeft > 0 {
		after = max(dt-p.voiceLeft, 0)
		p.voiceLeft = max(p.voiceLeft-dt, 0)
	}
	if p.skip {
		return p.skipStep(dt)
	}
	if p.kind != flow.StepLine || p.tw == nil {
		return nil
	}
	if !p.tw.Done() {
		p.tw.Update(float32(dt.Seconds()))
		return nil
	}
	if !p.auto || p.voiceLeft > 0 {
		return nil
	}
	p.autoWait += after
	if p.autoWait < p.autoDelay() {
		return nil
	}
	return p.next()
}

// Advance is the player's tap or key to go on: it stops skip mode, shows the rest of a line still appearing, or goes on to the next line. It fails with flow.ErrChoiceRequired while choices wait and does nothing once the script has ended.
func (p *Presenter) Advance() error {
	p.skip = false
	switch p.kind {
	case flow.StepChoices:
		return flow.ErrChoiceRequired
	case flow.StepEnded:
		return nil
	}
	if p.tw != nil && !p.tw.Done() {
		p.tw.Skip()
		return nil
	}
	return p.next()
}

// Choose picks the option whose Index is index, adds it to the backlog and goes on.
func (p *Presenter) Choose(index int) error {
	if p.kind != flow.StepChoices {
		return flow.ErrNoPendingChoice
	}
	var chosen *flow.DialogueChoice
	for i := range p.choices {
		if p.choices[i].Index == index {
			chosen = &p.choices[i]
		}
	}
	entry := BacklogEntry{Choice: true}
	if chosen != nil {
		entry.Text, entry.LineID = chosen.Text, chosen.LineID
	}
	p.cmdErr = nil
	step, err := p.runner.Choose(index)
	if err != nil {
		return err
	}
	p.addBacklog(entry)
	return p.apply(step, nil)
}

// SetAuto turns auto mode on or off. In auto mode the presenter goes on by itself once a line's text has appeared, its voice ended and Options.AutoDelay passed; it waits at choices.
func (p *Presenter) SetAuto(on bool) {
	p.auto = on
	p.autoWait = 0
}

// Auto reports whether auto mode is on.
func (p *Presenter) Auto() bool { return p.auto }

// SetSkip turns skip mode on or off. In skip mode each Update shows the next line at once, without its voice, one every Options.SkipInterval; it stops at choices, at the end, on Advance, and, unless Options.SkipUnread, on a line the player had not read before, which shows as usual, so skip mode never passes a line the player has not seen.
func (p *Presenter) SetSkip(on bool) {
	p.skip = on
	p.skipWait = 0
}

// Skipping reports whether skip mode is on.
func (p *Presenter) Skipping() bool { return p.skip }

// Line returns the line shown and whether there is one. The line stays shown while its choices wait.
func (p *Presenter) Line() (Line, bool) {
	if !p.hasLine {
		return Line{}, false
	}
	l := p.line
	l.Tags = slices.Clone(l.Tags)
	return l, true
}

// Typewriter returns the typewriter revealing the line shown, nil for none. Draw it, or read how much of the line has appeared.
func (p *Presenter) Typewriter() *text.Typewriter { return p.tw }

// Choices returns the options waiting for Choose, nil when none wait.
func (p *Presenter) Choices() []flow.DialogueChoice {
	if p.kind != flow.StepChoices {
		return nil
	}
	return slices.Clone(p.choices)
}

// Ended reports whether the script has ended or not started.
func (p *Presenter) Ended() bool { return p.kind == flow.StepEnded }

// Portraits returns the portraits on the stage in the order they entered, which is the order to draw them in.
func (p *Presenter) Portraits() []Portrait { return slices.Clone(p.portraits) }

// Backlog returns the lines shown and options chosen, oldest first.
func (p *Presenter) Backlog() []BacklogEntry { return slices.Clone(p.backlog) }

// ReplayVoice plays the voice line of backlog entry i again and reports whether it played.
func (p *Presenter) ReplayVoice(i int) bool {
	if i < 0 || i >= len(p.backlog) || p.backlog[i].Voice == "" || p.opts.Voices == nil {
		return false
	}
	_, ok := p.opts.Voices.PlayVoice(p.backlog[i].Voice)
	return ok
}

// ReadKeys returns the keys of the lines the player has read, sorted, for a save to keep and MarkRead to restore. A line's key is its #line: id, or for an untagged line its node, speaker and text.
func (p *Presenter) ReadKeys() []string {
	keys := make([]string, 0, len(p.read))
	for k := range p.read {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// MarkRead marks the lines with keys, from ReadKeys, as read.
func (p *Presenter) MarkRead(keys ...string) {
	for _, k := range keys {
		p.read[k] = true
	}
}

func (p *Presenter) autoDelay() time.Duration {
	d := p.opts.AutoDelay
	if d == 0 {
		d = DefaultAutoDelay
	}
	d = max(d, 0)
	if p.tw != nil {
		d += time.Duration(p.tw.TotalCount()) * p.opts.AutoDelayPerRune
	}
	return d
}

func (p *Presenter) skipStep(dt time.Duration) error {
	if p.kind != flow.StepLine || (!p.opts.SkipUnread && !p.line.Read) {
		p.skip = false
		return nil
	}
	p.skipWait += dt
	if p.skipWait < p.opts.SkipInterval {
		return nil
	}
	p.skipWait = 0
	return p.next()
}

// next stops the voice and goes on to the runner's next step.
func (p *Presenter) next() error {
	p.stopVoice()
	p.cmdErr = nil
	step, err := p.runner.Continue()
	return p.apply(step, err)
}

// apply takes the step a runner call produced, returning the call's error or the first stage command's.
func (p *Presenter) apply(step flow.DialogueStep, err error) error {
	if err != nil {
		return err
	}
	p.kind = step.Kind
	p.autoWait = 0
	switch step.Kind {
	case flow.StepLine:
		p.choices = nil
		if lerr := p.showLine(step.Line); lerr != nil && p.cmdErr == nil {
			p.cmdErr = lerr
		}
	case flow.StepChoices:
		p.choices = step.Choices
		p.skip = false
	default:
		p.choices = nil
		p.skip = false
		p.setSpeaking("")
	}
	cerr := p.cmdErr
	p.cmdErr = nil
	return cerr
}

// showLine shows dl in the text box, places its speaker's portrait and plays its voice line.
func (p *Presenter) showLine(dl flow.DialogueLine) error {
	var expr, slot, voice string
	offstage := false
	for _, tag := range dl.Tags {
		switch {
		case strings.HasPrefix(tag, TagExpression):
			expr = tag[len(TagExpression):]
		case strings.HasPrefix(tag, TagSlot):
			slot = tag[len(TagSlot):]
		case strings.HasPrefix(tag, TagVoice):
			voice = tag[len(TagVoice):]
		case tag == TagOffstage:
			offstage = true
		}
	}
	if voice == "" && p.opts.VoiceFromLineID {
		voice = dl.LineID
	}

	line := Line{DialogueLine: dl, Name: dl.Speaker, Voice: voice}
	var err error
	if ch, ok := p.cast.Speaker(dl.Speaker); ok {
		line.Character, line.Name, line.Color = ch.ID, ch.DisplayName(), ch.Color
		if !offstage || p.onStage(ch.ID) >= 0 {
			err = p.place(ch, slot, expr)
		}
	} else if expr != "" || slot != "" {
		err = fmt.Errorf("vn: line %q: speaker %q is not in the cast, so it takes no #%s or #%s tag", dl.Text, dl.Speaker, TagExpression, TagSlot)
	}
	p.setSpeaking(line.Character)

	key := p.readKey(dl)
	line.Read = p.read[key]
	p.read[key] = true
	if p.skip && !p.opts.SkipUnread && !line.Read {
		p.skip = false
	}
	fast := p.skip

	p.line, p.hasLine = line, true
	p.addBacklog(BacklogEntry{Character: line.Character, Name: line.Name, Color: line.Color, Text: dl.Text, LineID: dl.LineID, Voice: voice})

	cps := p.opts.CharsPerSecond
	if cps == 0 {
		cps = DefaultCharsPerSecond
	}
	p.tw = text.NewTypewriterString(dl.Text, max(cps, 0))
	if cps < 0 || fast {
		p.tw.Skip()
	}
	if !fast && voice != "" && p.opts.Voices != nil {
		if length, ok := p.opts.Voices.PlayVoice(voice); ok {
			p.voiceLeft = length
		}
	}
	return err
}

// readKey is the key the read set keeps dl under.
func (p *Presenter) readKey(dl flow.DialogueLine) string {
	if dl.LineID != "" {
		return dl.LineID
	}
	return p.runner.CurrentNode() + "\x00" + dl.Speaker + "\x00" + dl.Text
}

func (p *Presenter) addBacklog(e BacklogEntry) {
	size := p.opts.BacklogSize
	if size == 0 {
		size = DefaultBacklogSize
	}
	if size < 0 {
		return
	}
	if len(p.backlog) >= size {
		p.backlog = slices.Delete(p.backlog, 0, len(p.backlog)-size+1)
	}
	p.backlog = append(p.backlog, e)
}

func (p *Presenter) stopVoice() {
	p.voiceLeft = 0
	if p.opts.Voices != nil {
		p.opts.Voices.StopVoice()
	}
}

func (p *Presenter) setSpeaking(id string) {
	for i := range p.portraits {
		p.portraits[i].Speaking = id != "" && p.portraits[i].Character == id
	}
}

// onStage returns the index of id's portrait, or -1 when it is not on the stage.
func (p *Presenter) onStage(id string) int {
	return slices.IndexFunc(p.portraits, func(pt Portrait) bool { return pt.Character == id })
}

// place shows ch's portrait in slot with expr, either empty to keep the portrait's or use the character's defaults. A different portrait in that slot leaves the stage.
func (p *Presenter) place(ch *Character, slot, expr string) error {
	i := p.onStage(ch.ID)
	if slot == "" {
		switch {
		case i >= 0:
			slot = p.portraits[i].Slot
		case ch.Slot != "":
			slot = ch.Slot
		default:
			slot = p.cast.defaultSlot()
		}
	}
	s, ok := p.cast.Slot(slot)
	if !ok {
		return fmt.Errorf("vn: character %q: unknown slot %q", ch.ID, slot)
	}
	if expr == "" {
		if i >= 0 {
			expr = p.portraits[i].Expression
		} else {
			expr = ch.EffectiveExpression()
		}
	}
	e, ok := ch.Expression(expr)
	if !ok {
		return fmt.Errorf("vn: character %q: unknown expression %q", ch.ID, expr)
	}
	pt := Portrait{Character: ch.ID, Expression: e.Name, Frame: e.Frame, Slot: s.Name, X: s.X}
	if i >= 0 {
		pt.Speaking = p.portraits[i].Speaking
		p.portraits[i] = pt
	} else {
		p.portraits = append(p.portraits, pt)
	}
	p.portraits = slices.DeleteFunc(p.portraits, func(o Portrait) bool { return o.Slot == s.Name && o.Character != ch.ID })
	return nil
}

// command runs a stage command.
func (p *Presenter) command(ev flow.DialogueEvent) error {
	switch ev.Command {
	case CommandHide:
		switch len(ev.Args) {
		case 0:
			p.portraits = nil
		case 1:
			if _, ok := p.cast.Character(ev.Args[0]); !ok {
				return fmt.Errorf("vn: <<hide>>: unknown character %q", ev.Args[0])
			}
			if i := p.onStage(ev.Args[0]); i >= 0 {
				p.portraits = slices.Delete(p.portraits, i, i+1)
			}
		default:
			return errors.New("vn: <<hide>> takes at most one character")
		}
		return nil
	case CommandExpression:
		if len(ev.Args) != 2 {
			return errors.New("vn: <<expr>> takes a character and an expression")
		}
		ch, ok := p.cast.Character(ev.Args[0])
		if !ok {
			return fmt.Errorf("vn: <<expr>>: unknown character %q", ev.Args[0])
		}
		return p.place(ch, "", ev.Args[1])
	default: // CommandShow
		if len(ev.Args) == 0 || len(ev.Args) > 3 {
			return errors.New("vn: <<show>> takes a character and optionally a slot and an expression")
		}
		ch, ok := p.cast.Character(ev.Args[0])
		if !ok {
			return fmt.Errorf("vn: <<show>>: unknown character %q", ev.Args[0])
		}
		var slot, expr string
		for _, a := range ev.Args[1:] {
			if _, isSlot := p.cast.Slot(a); isSlot && slot == "" {
				slot = a
			} else if _, isExpr := ch.Expression(a); isExpr && expr == "" {
				expr = a
			} else {
				return fmt.Errorf("vn: <<show %s>>: %q is neither a slot nor an expression of the character", ch.ID, a)
			}
		}
		return p.place(ch, slot, expr)
	}
}
