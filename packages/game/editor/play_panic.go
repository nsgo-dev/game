package editor

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/replay"
)

// PanicStackFrame represents one parsed stack frame from a Go panic trace.
type PanicStackFrame struct {
	Function string `json:"function"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Runtime  bool   `json:"runtime"`
}

// PanicReport captures a panic detected in a game run or Play session, with separated game and runtime frames.
type PanicReport struct {
	RunNumber  int               `json:"runNumber"`
	Time       time.Time         `json:"time"`
	Frame      int               `json:"frame"`
	Message    string            `json:"message"`
	Frames     []PanicStackFrame `json:"frames"`
	ReplayPath string            `json:"replayPath,omitempty"`
	ReplayAbs  string            `json:"replayAbs,omitempty"`
}

// Clone returns an independent copy of p.
func (p *PanicReport) Clone() *PanicReport {
	if p == nil {
		return nil
	}
	cp := *p
	cp.Frames = slices.Clone(p.Frames)
	return &cp
}

// GameFrames returns the user/game stack frames (non-runtime) in call order.
func (p *PanicReport) GameFrames() []PanicStackFrame {
	if p == nil {
		return nil
	}
	var out []PanicStackFrame
	for _, f := range p.Frames {
		if !f.Runtime {
			out = append(out, f)
		}
	}
	return out
}

// RuntimeFrames returns the folded game-runtime and Go-runtime stack frames in call order.
func (p *PanicReport) RuntimeFrames() []PanicStackFrame {
	if p == nil {
		return nil
	}
	var out []PanicStackFrame
	for _, f := range p.Frames {
		if f.Runtime {
			out = append(out, f)
		}
	}
	return out
}

var panicFrameRe = regexp.MustCompile(`(?i)\b(?:frame|step)[\s:=#]+(\d[\d\s,]*)\b`)

// ParsePanicTrace detects a Go panic message and stack trace in lines, normalizing paths relative to projectDir
// and separating game frames from runtime/engine frames.
func ParsePanicTrace(lines []string, projectDir string) (*PanicReport, bool) {
	panicIdx := -1
	panicMsg := ""
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if idx := strings.Index(trimmed, "panic: "); idx >= 0 {
			panicIdx = i
			panicMsg = trimmed[idx:]
			break
		}
	}
	if panicIdx < 0 {
		return nil, false
	}

	report := &PanicReport{
		Message: panicMsg,
	}

	// Look for an optional frame/step annotation in the panic line or surrounding run lines.
	for i := max(0, panicIdx-5); i < len(lines); i++ {
		if m := panicFrameRe.FindStringSubmatch(lines[i]); len(m) == 2 {
			digits := strings.ReplaceAll(strings.ReplaceAll(m[1], " ", ""), ",", "")
			if n, err := strconv.Atoi(digits); err == nil && n > 0 {
				report.Frame = n
				break
			}
		}
	}

	// Parse Go stack trace pairs:
	//   pkg/path.(*Type).Method(args...)
	//       /abs/or/rel/file.go:83 +0x4c
	var pendingFunc string
	for i := panicIdx + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(report.Frames) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "goroutine ") {
			pendingFunc = ""
			continue
		}
		if strings.HasPrefix(trimmed, "created by ") {
			pendingFunc = ""
			continue
		}
		// Indented file:line line following a function line (or standalone file:line).
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ") || pendingFunc != "" {
			if file, lineNum, ok := parseStackFileLine(trimmed, projectDir); ok {
				fn := simplifyStackFunc(pendingFunc)
				if fn == "" {
					fn = filepath.Base(file)
				}
				isRT := isRuntimeStackFrame(pendingFunc, file, projectDir)
				report.Frames = append(report.Frames, PanicStackFrame{
					Function: fn,
					File:     file,
					Line:     lineNum,
					Runtime:  isRT,
				})
				pendingFunc = ""
				continue
			}
		}
		if looksLikeStackFuncLine(trimmed) {
			pendingFunc = trimmed
		} else {
			pendingFunc = ""
		}
	}

	return report, true
}

func looksLikeStackFuncLine(s string) bool {
	if s == "" || strings.HasPrefix(s, "goroutine ") || strings.HasPrefix(s, "panic:") {
		return false
	}
	if strings.Contains(s, "(") && strings.Contains(s, ")") {
		return true
	}
	// Accept package.Func without parens as well when a file:line follows.
	return strings.Contains(s, ".") && !strings.Contains(s, " ")
}

func simplifyStackFunc(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Strip trailing argument list "(...)" while preserving receiver "(*Type)".
	if closeIdx := strings.LastIndexByte(s, ')'); closeIdx == len(s)-1 {
		depth := 0
		openIdx := -1
		for i := closeIdx; i >= 0; i-- {
			switch s[i] {
			case ')':
				depth++
			case '(':
				depth--
				if depth == 0 {
					openIdx = i
				}
			}
			if openIdx >= 0 {
				break
			}
		}
		if openIdx > 0 {
			s = s[:openIdx]
		}
	}
	// Shorten long module paths to the last path segment (e.g. "example.com/dungeon/game/lever.(*System).toggle" -> "lever.(*System).toggle").
	if slash := strings.LastIndexByte(s, '/'); slash >= 0 && slash+1 < len(s) {
		s = s[slash+1:]
	}
	return s
}

func parseStackFileLine(trimmed, projectDir string) (string, int, bool) {
	// Go stack lines look like "/path/to/file.go:83 +0x4c" or "game/lever/system.go:83".
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", 0, false
	}
	return parseFileLineToken(fields[0], projectDir)
}

func isRuntimeStackFrame(rawFunc, normFile, projectDir string) bool {
	fn := strings.TrimSpace(rawFunc)
	f := strings.ToLower(strings.ReplaceAll(normFile, "\\", "/"))

	for _, prefix := range []string{
		"runtime.",
		"runtime/",
		"reflect.",
		"sync.",
		"syscall.",
		"testing.",
		"internal/",
		"github.com/nsgo-dev/nsgo/",
		"github.com/nsgo-dev/game/packages/game",
	} {
		if strings.HasPrefix(fn, prefix) {
			return true
		}
	}
	for _, marker := range []string{
		"/src/runtime/",
		"/runtime/panic.go",
		"/runtime/debug/",
		"packages/game/",
		"nsgo/core/",
		"nsgo/ui/",
		"nsgo/nsgotest/",
	} {
		if strings.Contains(f, marker) {
			return true
		}
	}
	// If a project directory is known and the normalized file is relative (inside the project), it's a game frame.
	if projectDir != "" && !filepath.IsAbs(normFile) && !strings.HasPrefix(normFile, "../") {
		return false
	}
	return false
}

// OutputPanic returns a copy of the latest detected panic report in the Output panel, or nil when none is present.
func (e *Editor) OutputPanic() *PanicReport {
	if e == nil {
		return nil
	}
	e.output.mu.Lock()
	defer e.output.mu.Unlock()
	return e.output.panicReport.Clone()
}

// SetOutputPanic sets or clears the Output panel's panic card report.
func (e *Editor) SetOutputPanic(report *PanicReport) {
	if e == nil {
		return
	}
	e.output.mu.Lock()
	e.output.panicReport = report.Clone()
	e.output.panicRuntimeOpen = false
	if report != nil {
		e.output.open = true
	}
	e.output.mu.Unlock()
	e.notify()
}

// OutputPanicRuntimeExpanded reports whether the panic card's folded runtime frames are expanded.
func (e *Editor) OutputPanicRuntimeExpanded() bool {
	if e == nil {
		return false
	}
	e.output.mu.Lock()
	defer e.output.mu.Unlock()
	return e.output.panicRuntimeOpen
}

// SetOutputPanicRuntimeExpanded shows or folds runtime stack frames inside the Output panel's panic card.
func (e *Editor) SetOutputPanicRuntimeExpanded(expanded bool) {
	if e == nil {
		return
	}
	e.output.mu.Lock()
	changed := e.output.panicRuntimeOpen != expanded
	e.output.panicRuntimeOpen = expanded
	e.output.mu.Unlock()
	if changed {
		e.notify()
	}
}

// AskAgentToFixPanic sends the detected panic report (message, stack frames, and attached replay) to the agent
// via Editor.OnAskPanicFix and displays the task in the Play mode Agent tab.
func (e *Editor) AskAgentToFixPanic() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	p := e.OutputPanic()
	if p == nil {
		return errors.New("editor: no panic to send to the agent")
	}
	e.playMarks.tab = PlayTabAgent
	e.playMarks.agentActive = true
	e.playMarks.sentQuestion = "Fix " + p.Message
	meta := fmt.Sprintf("Run %d", max(1, p.RunNumber))
	if p.Frame > 0 {
		meta += " · frame " + formatPlayFrameNumber(p.Frame)
	}
	if p.ReplayPath != "" {
		meta += " · " + p.ReplayPath
	}
	e.playMarks.sentMeta = meta
	e.playMarks.agentSteps = []PlayAgentStep{
		{Text: "Attached panic stack trace and replay", Status: "done"},
		{Text: "Investigating panic in game code", Status: "active"},
		{Text: "Propose a fix as a review", Status: "pending"},
	}
	if e.OnAskPanicFix != nil {
		e.OnAskPanicFix(*p)
	}
	e.Status = "Asked agent to fix panic"
	e.notify()
	return nil
}

const maxReplayUpToFrameHardLimit = 36000

// resolveContainedReplayFile resolves a replay path inside root, refusing path traversal,
// symlink escapes outside root, and non-.replay.yaml/.replay.yml files.
func resolveContainedReplayFile(root, relOrAbs string) (string, error) {
	if root == "" {
		return "", errors.New("editor: replay root directory is not configured")
	}
	realRoot, err := evalPlaytestPathSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("editor: resolve replay root: %w", err)
	}
	var candidate string
	if filepath.IsAbs(relOrAbs) {
		candidate = filepath.Clean(relOrAbs)
	} else {
		slashed := strings.ReplaceAll(strings.TrimSpace(relOrAbs), "\\", "/")
		if slashed == "" || strings.HasPrefix(slashed, "/") || filepath.VolumeName(relOrAbs) != "" {
			return "", fmt.Errorf("editor: invalid replay path %q", relOrAbs)
		}
		clean := filepath.Clean(filepath.FromSlash(slashed))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("editor: replay path %q escapes project root", relOrAbs)
		}
		candidate = filepath.Join(realRoot, clean)
	}
	lower := strings.ToLower(candidate)
	if !strings.HasSuffix(lower, ".replay.yaml") && !strings.HasSuffix(lower, ".replay.yml") {
		return "", fmt.Errorf("editor: replay path %q must end in .replay.yaml or .replay.yml", relOrAbs)
	}
	realTarget, err := evalPlaytestPathSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("editor: resolve replay path: %w", err)
	}
	realLower := strings.ToLower(realTarget)
	if !strings.HasSuffix(realLower, ".replay.yaml") && !strings.HasSuffix(realLower, ".replay.yml") {
		return "", fmt.Errorf("editor: resolved replay path %q must end in .replay.yaml or .replay.yml", relOrAbs)
	}
	rel, err := filepath.Rel(realRoot, realTarget)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("editor: replay path %q resolves outside root %q", relOrAbs, root)
	}
	return realTarget, nil
}

// ReplayUpToPanic replays the saved input recording for the latest panic in Play mode and stops and pauses
// one frame before the panic frame (panicFrame - 1) so the panic is not triggered.
func (e *Editor) ReplayUpToPanic() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	p := e.OutputPanic()
	if p == nil {
		return errors.New("editor: no panic report available to replay")
	}
	if p.Frame <= 0 {
		return errors.New("editor: panic report has no recorded frame number")
	}

	var rec *replay.Recording
	if p.ReplayPath != "" || p.ReplayAbs != "" {
		root, rerr := e.replayRootDir()
		if rerr != nil {
			return fmt.Errorf("editor: load panic replay: %w", rerr)
		}
		target := p.ReplayPath
		if target == "" {
			target = p.ReplayAbs
		}
		safeAbs, err := resolveContainedReplayFile(root, target)
		if err != nil {
			return fmt.Errorf("editor: load panic replay: %w", err)
		}
		loaded, err := replay.LoadFile(safeAbs)
		if err != nil {
			return fmt.Errorf("editor: load panic replay: %w", err)
		}
		rec = loaded
	}
	if rec == nil && e.playMarks.lastPanicRec != nil {
		rec = e.playMarks.lastPanicRec.Clone()
	}
	if rec == nil && e.playRolling != nil {
		rec = e.playRolling.Recording()
	}
	if rec == nil {
		return errors.New("editor: no replay recording attached to the panic")
	}

	targetFrame := max(0, p.Frame-1)
	if err := e.ReplayUpToFrame(rec, targetFrame); err != nil {
		return err
	}
	e.Status = fmt.Sprintf("Paused at frame %s (1 frame before panic at %s)", formatPlayFrameNumber(targetFrame), formatPlayFrameNumber(p.Frame))
	e.notify()
	return nil
}

// ReplayUpToFrame starts (or restarts) Play mode from the clean initial scene snapshot using rec as the input source,
// steps deterministically to stopFrame, and pauses Play mode there.
func (e *Editor) ReplayUpToFrame(rec *replay.Recording, stopFrame int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if rec == nil {
		return errors.New("editor: nil replay recording")
	}
	if err := rec.Validate(); err != nil {
		return fmt.Errorf("editor: invalid replay recording: %w", err)
	}
	if stopFrame < 0 {
		return fmt.Errorf("editor: stop frame %d must be non-negative", stopFrame)
	}
	rate := rec.Rate
	if rate <= 0 {
		rate = e.PlayRateHz()
	}
	maxStop := min(maxReplayUpToFrameHardLimit, max(rec.TotalSteps, rollingReplayWindowSeconds*rate))
	if stopFrame > maxStop {
		return fmt.Errorf("editor: stop frame %d exceeds maximum allowed replay length %d", stopFrame, maxStop)
	}
	if e.Doc == nil {
		return errors.New("editor: cannot replay nil scene document")
	}

	// Preserve panic report and replay references across restart.
	if e.playing {
		if err := e.stopInternal(false); err != nil {
			return err
		}
	}

	if err := e.startPlaySession(e.scope, rec.Clone(), true); err != nil {
		return err
	}
	if stopFrame > 0 {
		e.StepPlay(stopFrame)
	}
	if !e.playing {
		return fmt.Errorf("editor: replay stopped before reaching frame %d (stopped at frame %d)", stopFrame, e.playSteps)
	}
	e.playPaused = true
	if e.playLoop != nil {
		e.playLoop.SetPaused(true)
	}
	e.notify()
	return nil
}

func (e *Editor) detectPanicFromOutputLocked(projectDir string) {
	if len(e.output.runLines) == 0 {
		return
	}
	rep, ok := ParsePanicTrace(e.output.runLines, projectDir)
	if !ok {
		return
	}
	rep.RunNumber = max(1, e.output.runCount)
	rep.Time = e.now()
	if rep.ReplayPath == "" && e.output.lastReplayPath != "" {
		rep.ReplayPath = e.output.lastReplayPath
		rep.ReplayAbs = e.output.lastReplayAbs
	}
	e.output.panicReport = rep
	e.output.open = true
}

func (e *Editor) recordPlayPanic(recovered any, stack []byte) {
	panicFrame := e.playSteps
	projectDir := ""
	if e.Project != nil && e.ProjectPath != "" {
		projectDir = filepath.Dir(e.ProjectPath)
	}

	// Snapshot the rolling recording up to the panic frame and save under replays/ when possible.
	rec := e.PlayRollingRecording()
	if rec != nil {
		e.playMarks.lastPanicRec = rec.Clone()
	}
	relPath, absPath, _, _ := e.SavePlayRollingReplay()

	msg := fmt.Sprintf("panic: %v", recovered)
	lines := []string{msg}
	for _, l := range strings.Split(string(stack), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}

	rep, ok := ParsePanicTrace(lines, projectDir)
	if !ok {
		rep = &PanicReport{Message: msg}
	}
	rep.Frame = panicFrame
	rep.Time = e.now()
	rep.ReplayPath = relPath
	rep.ReplayAbs = absPath

	e.output.mu.Lock()
	rep.RunNumber = max(1, e.output.runCount)
	e.output.panicReport = rep
	e.output.panicRuntimeOpen = false
	e.output.open = true
	e.output.mu.Unlock()

	// Log summary rows without re-running detectPanicFromOutputLocked, which would overwrite
	// rep with a 1-line report lacking parsed stack frames.
	e.appendOutputEntryWithPanicDetect(OutputStderr, OutputError, fmt.Sprintf("%s (frame %d)", msg, panicFrame), projectDir, false)
	for _, f := range rep.GameFrames() {
		e.appendOutputEntryWithPanicDetect(OutputStderr, OutputError, fmt.Sprintf("%s %s:%d", f.Function, f.File, f.Line), projectDir, false)
	}

	// Stop Play cleanly and restore the pre-play snapshot so ReplayUpToPanic starts cleanly from frame 0.
	_ = e.stopInternal(false)
	e.setStatusWithoutLogging(fmt.Sprintf("Run %d stopped on a panic at frame %s", rep.RunNumber, formatPlayFrameNumber(panicFrame)))
	e.notify()
}
