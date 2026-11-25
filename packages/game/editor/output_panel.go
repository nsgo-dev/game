package editor

import (
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

const maxOutputEntries = 2000

// OutputSource identifies where a line in the Output panel originated.
type OutputSource int

const (
	// OutputEditor is a status or error message produced by the editor itself.
	OutputEditor OutputSource = iota
	// OutputStdout is a line streamed from the running game's standard output.
	OutputStdout
	// OutputStderr is a line streamed from the running game's standard error.
	OutputStderr
)

// String returns a short label for the output source ("editor", "stdout", or "stderr").
func (s OutputSource) String() string {
	switch s {
	case OutputEditor:
		return "editor"
	case OutputStdout:
		return "stdout"
	case OutputStderr:
		return "stderr"
	default:
		return "unknown"
	}
}

// OutputLevel classifies the severity of an Output panel entry.
type OutputLevel int

const (
	// OutputInfo is a normal status message or game stdout line.
	OutputInfo OutputLevel = iota
	// OutputError is an error reported by the editor or streamed from game stderr.
	OutputError
)

// String returns a short label for the output level ("info" or "error").
func (l OutputLevel) String() string {
	switch l {
	case OutputInfo:
		return "info"
	case OutputError:
		return "error"
	default:
		return "unknown"
	}
}

// OutputEntry is one timestamped line in the editor's Output panel history.
type OutputEntry struct {
	Time   time.Time
	Source OutputSource
	Level  OutputLevel
	Text   string
	// File is the parsed file path when Text contains a file:line location, or empty.
	File string
	// Line is the 1-based line number paired with File, or 0 when none was parsed.
	Line int
}

type outputPanelState struct {
	mu               sync.Mutex
	open             bool
	errorsOnly       bool
	entries          []OutputEntry
	lastStatus       string
	lastBuildErr     string
	runCount         int
	runLines         []string
	panicReport      *PanicReport
	panicRuntimeOpen bool
	lastReplayPath   string
	lastReplayAbs    string
}

// OutputPanelOpen reports whether the Output panel is visible below the viewport.
func (e *Editor) OutputPanelOpen() bool {
	if e == nil {
		return false
	}
	e.output.mu.Lock()
	defer e.output.mu.Unlock()
	return e.output.open
}

// SetOutputPanelOpen shows or hides the Output panel below the viewport.
func (e *Editor) SetOutputPanelOpen(open bool) {
	if e == nil {
		return
	}
	e.output.mu.Lock()
	changed := e.output.open != open
	e.output.open = open
	e.output.mu.Unlock()
	if changed {
		e.notify()
	}
}

// OutputErrorsOnly reports whether the Output panel is filtered to error entries only.
func (e *Editor) OutputErrorsOnly() bool {
	if e == nil {
		return false
	}
	e.output.mu.Lock()
	defer e.output.mu.Unlock()
	return e.output.errorsOnly
}

// SetOutputErrorsOnly toggles the Output panel's Errors-only filter.
func (e *Editor) SetOutputErrorsOnly(errorsOnly bool) {
	if e == nil {
		return
	}
	e.output.mu.Lock()
	changed := e.output.errorsOnly != errorsOnly
	e.output.errorsOnly = errorsOnly
	e.output.mu.Unlock()
	if changed {
		e.notify()
	}
}

// OutputEntries returns a snapshot of every entry recorded in the Output panel in chronological order. It is safe to call from any goroutine.
func (e *Editor) OutputEntries() []OutputEntry {
	if e == nil {
		return nil
	}
	e.output.mu.Lock()
	defer e.output.mu.Unlock()
	if len(e.output.entries) == 0 {
		return nil
	}
	return slices.Clone(e.output.entries)
}

// FilteredOutputEntries returns the Output panel entries visible under the current Errors-only filter. It is safe to call from any goroutine.
func (e *Editor) FilteredOutputEntries() []OutputEntry {
	if e == nil {
		return nil
	}
	e.output.mu.Lock()
	defer e.output.mu.Unlock()
	if len(e.output.entries) == 0 {
		return nil
	}
	if !e.output.errorsOnly {
		return slices.Clone(e.output.entries)
	}
	out := make([]OutputEntry, 0, len(e.output.entries))
	for _, entry := range e.output.entries {
		if entry.Level == OutputError {
			out = append(out, entry)
		}
	}
	return out
}

// ClearOutput removes all recorded entries and any panic card from the Output panel.
func (e *Editor) ClearOutput() {
	if e == nil {
		return
	}
	e.output.mu.Lock()
	e.output.entries = nil
	e.output.runLines = nil
	e.output.panicReport = nil
	e.output.panicRuntimeOpen = false
	e.output.mu.Unlock()
	e.notify()
}

// OpenOutputEntry navigates to the file:line location carried by entry: if it names a scene file (*.scene.yaml, *.scene.yml, or a scene listed under Project.Autoload.Scenes), it opens that scene in the editor (and updates ScenePath and SelectedFile when a project is open); otherwise it selects the file in the file browser when a project is open. Entries without a file:line location are a no-op.
func (e *Editor) OpenOutputEntry(entry OutputEntry) error {
	if e == nil {
		return nil
	}
	projectDir := ""
	if e.Project != nil && e.ProjectPath != "" {
		projectDir = filepath.Dir(e.ProjectPath)
	}
	file, line := entry.File, entry.Line
	if file == "" {
		var ok bool
		file, line, ok = parseFileLine(entry.Text, projectDir)
		if !ok {
			return nil
		}
	}
	file = normalizeOutputFile(file, projectDir)
	if file == "" {
		return nil
	}

	if isProjectScene(e.Project, file) {
		abs := filepath.FromSlash(file)
		inProject := false
		if projectDir != "" && !filepath.IsAbs(abs) {
			abs = filepath.Join(projectDir, abs)
			inProject = true
		} else if projectDir != "" {
			if rel, err := filepath.Rel(projectDir, abs); err == nil {
				slash := filepath.ToSlash(rel)
				if slash != ".." && !strings.HasPrefix(slash, "../") {
					file = slash
					inProject = true
				}
			}
		}
		if err := e.OpenFile(abs); err != nil {
			if inProject {
				e.selectFile(file)
				e.notify()
			}
			return err
		}
		if inProject {
			e.ScenePath = file
			e.selectFile(file)
		}
		if line > 0 {
			e.Status = fmt.Sprintf("Opened %s:%d", file, line)
		} else {
			e.Status = "Opened " + file
		}
		e.notify()
		return nil
	}

	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	e.selectFile(file)
	if line > 0 {
		e.Status = fmt.Sprintf("Selected %s:%d", file, line)
	} else {
		e.Status = "Selected " + file
	}
	e.notify()
	return nil
}

func (e *Editor) appendOutputEntry(source OutputSource, level OutputLevel, text, projectDir string) {
	e.appendOutputEntryWithPanicDetect(source, level, text, projectDir, true)
}

func (e *Editor) appendOutputEntryWithPanicDetect(source OutputSource, level OutputLevel, text, projectDir string, detectPanic bool) {
	if e == nil {
		return
	}
	file, line, _ := parseFileLine(text, projectDir)
	entry := OutputEntry{
		Time:   time.Now(),
		Source: source,
		Level:  level,
		Text:   text,
		File:   file,
		Line:   line,
	}
	e.output.mu.Lock()
	e.output.entries = append(e.output.entries, entry)
	if len(e.output.entries) > maxOutputEntries {
		drop := len(e.output.entries) - maxOutputEntries
		e.output.entries = slices.Clone(e.output.entries[drop:])
	}
	if detectPanic && (source == OutputStdout || source == OutputStderr) {
		e.output.runLines = append(e.output.runLines, text)
		if len(e.output.runLines) > 512 {
			e.output.runLines = slices.Clone(e.output.runLines[len(e.output.runLines)-512:])
		}
		e.detectPanicFromOutputLocked(projectDir)
	}
	e.output.mu.Unlock()
}

func (e *Editor) reportStatusError(msg string) {
	if e == nil || msg == "" {
		return
	}
	projectDir := ""
	if e.Project != nil && e.ProjectPath != "" {
		projectDir = filepath.Dir(e.ProjectPath)
	}
	e.appendOutputEntry(OutputEditor, OutputError, msg, projectDir)
	e.setStatusWithoutLogging(msg)
	e.notify()
}

func (e *Editor) setStatusWithoutLogging(msg string) {
	if e == nil {
		return
	}
	e.Status = msg
	e.output.mu.Lock()
	e.output.lastStatus = msg
	e.output.mu.Unlock()
}

func (e *Editor) captureStatusForOutput() {
	if e == nil {
		return
	}
	projectDir := ""
	if e.Project != nil && e.ProjectPath != "" {
		projectDir = filepath.Dir(e.ProjectPath)
	}

	status := e.Status
	var buildMsg string
	if e.BuildErr != nil {
		buildMsg = "Build error: " + e.BuildErr.Error()
	}

	e.output.mu.Lock()
	logStatus := status != "" && status != e.output.lastStatus
	if logStatus {
		e.output.lastStatus = status
	}
	logBuild := buildMsg != "" && buildMsg != e.output.lastBuildErr
	e.output.lastBuildErr = buildMsg
	e.output.mu.Unlock()

	if logStatus {
		level := OutputInfo
		if isErrorStatus(status) {
			level = OutputError
		}
		e.appendOutputEntry(OutputEditor, level, status, projectDir)
	}
	if logBuild {
		e.appendOutputEntry(OutputEditor, OutputError, buildMsg, projectDir)
	}
}

func isErrorStatus(status string) bool {
	lower := strings.ToLower(strings.TrimSpace(status))
	for _, prefix := range []string{
		"error:",
		"save error:",
		"build error:",
		"run error:",
		"undo failed:",
		"redo failed:",
		"export failed:",
		"export blocked:",
		"assets build failed:",
		"assets reload failed:",
		"reimport failed:",
		"game exited with error:",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// outputStreamWriter splits concurrent stdout/stderr writes into complete lines and delivers each line to onLine.
type outputStreamWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	onLine func(string)
}

func newOutputStreamWriter(onLine func(string)) *outputStreamWriter {
	return &outputStreamWriter{onLine: onLine}
}

func (w *outputStreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	var lines []string
	for _, b := range p {
		if b == '\r' {
			continue
		}
		if b == '\n' {
			lines = append(lines, w.buf.String())
			w.buf.Reset()
			continue
		}
		w.buf.WriteByte(b)
	}
	w.mu.Unlock()
	for _, line := range lines {
		w.onLine(line)
	}
	return len(p), nil
}

func (w *outputStreamWriter) Flush() {
	w.mu.Lock()
	rem := w.buf.String()
	w.buf.Reset()
	w.mu.Unlock()
	if rem != "" {
		w.onLine(rem)
	}
}

// parseFileLine extracts the first file:line reference (optionally file:line:col) from text, normalizing project-relative paths when projectDir is non-empty.
func parseFileLine(text, projectDir string) (file string, line int, ok bool) {
	for _, field := range strings.Fields(text) {
		if f, l, matched := parseFileLineToken(field, projectDir); matched {
			return f, l, true
		}
	}
	return "", 0, false
}

func parseFileLineToken(tok, projectDir string) (string, int, bool) {
	tok = strings.TrimLeft(tok, "([{'\"")
	tok = strings.TrimRight(tok, ")]}'\",;:")
	if tok == "" {
		return "", 0, false
	}
	// Skip URLs and asset URIs.
	if strings.Contains(tok, "://") || strings.HasPrefix(tok, "asset:") {
		return "", 0, false
	}

	// Preserve a Windows drive letter prefix such as C:\ or C:/ while scanning for :line[:col].
	start := 0
	if len(tok) >= 3 && isASCIIAlpha(tok[0]) && tok[1] == ':' && (tok[2] == '\\' || tok[2] == '/') {
		start = 2
	}

	rest := tok[start:]
	firstColon := strings.IndexByte(rest, ':')
	if firstColon <= 0 {
		return "", 0, false
	}
	rawPath := tok[:start+firstColon]
	after := rest[firstColon+1:]
	if after == "" {
		return "", 0, false
	}

	// after is "<line>" or "<line>:<col>" (ignoring any trailing extra colon segments).
	linePart := after
	if nextColon := strings.IndexByte(after, ':'); nextColon >= 0 {
		linePart = after[:nextColon]
	}
	lineNum, err := strconv.Atoi(linePart)
	if err != nil || lineNum <= 0 {
		return "", 0, false
	}

	if !looksLikeFilePath(rawPath) {
		return "", 0, false
	}
	norm := normalizeOutputFile(rawPath, projectDir)
	if norm == "" {
		return "", 0, false
	}
	return norm, lineNum, true
}

func looksLikeFilePath(p string) bool {
	if p == "" {
		return false
	}
	// Require a path separator or file extension so timestamps (14:07:28) and labels (Error: 1) are ignored.
	return strings.ContainsAny(p, "/\\.")
}

func normalizeOutputFile(raw, projectDir string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if projectDir != "" {
		native := filepath.FromSlash(strings.ReplaceAll(raw, "\\", "/"))
		if filepath.IsAbs(native) {
			if rel, err := filepath.Rel(projectDir, native); err == nil {
				slash := filepath.ToSlash(rel)
				if slash != ".." && !strings.HasPrefix(slash, "../") && slash != "." {
					return slash
				}
			}
		}
	}
	// Keep Windows drive-rooted paths clean without stripping the drive.
	if len(raw) >= 3 && isASCIIAlpha(raw[0]) && raw[1] == ':' && (raw[2] == '\\' || raw[2] == '/') {
		return strings.ReplaceAll(raw, "\\", "/")
	}
	slash := strings.ReplaceAll(raw, "\\", "/")
	cleaned := path.Clean(slash)
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// OutputPanelView renders the docked Output / Log panel below the viewport when OutputPanelOpen reports true. It displays the editor's timestamped status and error history alongside live stdout and stderr lines streamed from the running game, with Clear and Errors-only filter controls and clickable file:line navigation.
func OutputPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	open := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.OutputPanelOpen()
	})
	type snapshot struct {
		entries          []OutputEntry
		totalCount       int
		errorCount       int
		errorsOnly       bool
		running          bool
		assetsBuilding   bool
		panicReport      *PanicReport
		panicRuntimeOpen bool
	}
	state := core.NewMemoFunc(cx, func() snapshot {
		_ = rev.Get()
		all := ed.OutputEntries()
		errCount := 0
		for _, entry := range all {
			if entry.Level == OutputError {
				errCount++
			}
		}
		return snapshot{
			entries:          ed.FilteredOutputEntries(),
			totalCount:       len(all),
			errorCount:       errCount,
			errorsOnly:       ed.OutputErrorsOnly(),
			running:          ed.GameRunning(),
			assetsBuilding:   ed.AssetsBuilding(),
			panicReport:      ed.OutputPanic(),
			panicRuntimeOpen: ed.OutputPanicRuntimeExpanded(),
		}
	}, func(a, b snapshot) bool {
		if a.totalCount != b.totalCount ||
			a.errorCount != b.errorCount ||
			a.errorsOnly != b.errorsOnly ||
			a.running != b.running ||
			a.assetsBuilding != b.assetsBuilding ||
			a.panicRuntimeOpen != b.panicRuntimeOpen ||
			!slices.Equal(a.entries, b.entries) {
			return false
		}
		if (a.panicReport == nil) != (b.panicReport == nil) {
			return false
		}
		if a.panicReport != nil {
			if a.panicReport.RunNumber != b.panicReport.RunNumber ||
				a.panicReport.Frame != b.panicReport.Frame ||
				a.panicReport.Message != b.panicReport.Message ||
				a.panicReport.ReplayPath != b.panicReport.ReplayPath ||
				!slices.Equal(a.panicReport.Frames, b.panicReport.Frames) {
				return false
			}
		}
		return true
	})

	list := core.Dynamic(func(cx *core.Scope) core.Node {
		s := state.Get()
		var children []core.Node
		if s.panicReport != nil {
			children = append(children, outputPanicCardView(cx, ed, s.panicReport, s.panicRuntimeOpen))
		}
		if len(s.entries) == 0 && s.panicReport == nil {
			msg := ed.T("output.empty")
			if s.errorsOnly {
				msg = ed.T("output.emptyErrors")
			}
			return core.Text{
				TestID:  "output-empty",
				Content: msg,
				Style:   core.Style{FontSize: 11, Color: colors.Muted()},
			}
		}
		for i, item := range s.entries {
			entry := item
			textColor := colors.Text()
			if entry.Level == OutputError {
				textColor = colors.Danger()
			}
			var onPress func(core.PressEvent)
			if entry.File != "" {
				onPress = func(core.PressEvent) {
					ed.report(ed.OpenOutputEntry(entry))
				}
			}
			children = append(children, core.View{
				TestID: fmt.Sprintf("output-row-%d", i),
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(2, 6),
					CornerRadius: 4,
					Background:   colors.Surface(),
				},
				OnPress: onPress,
				Children: []core.Node{
					core.Text{
						Content: entry.Time.Format("15:04:05"),
						Style:   core.Style{FontSize: 11, Color: colors.Muted()},
					},
					core.Text{
						Content: "[" + entry.Source.String() + "]",
						Style:   core.Style{FontSize: 11, FontWeight: 600, Color: textColor},
					},
					core.Text{
						Content: entry.Text,
						Style:   core.Style{FontSize: 11, Color: textColor, FlexGrow: 1},
					},
				},
			})
		}
		return core.View{Style: core.Style{Gap: 4}, Children: children}
	})

	return core.Show{
		When: open.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "output-panel",
				Style: dockedStyle(cx, core.Style{
					Background:  colors.Inset(),
					Borders:     core.Edges{Top: 1},
					BorderColor: colors.Line(),
					Padding:     core.All(8),
					Gap:         6,
				}),
				StyleFunc: func() core.Style {
					if isDockedPanel(cx) {
						return core.Style{}
					}
					if state.Get().panicReport != nil {
						return core.Style{Height: core.Pt(236)}
					}
					return core.Style{Height: core.Pt(140)}
				},
				Children: []core.Node{
					ui.HStack{
						Gap: 8,
						Children: []core.Node{
							core.Text{
								TestID: "output-title",
								ContentFunc: func() string {
									s := state.Get()
									title := ed.T("output.title", l10n.Param("count", s.totalCount))
									switch {
									case s.running:
										title = ed.T("output.titleRunning", l10n.Param("count", s.totalCount))
									case s.assetsBuilding:
										title = ed.T("output.titleBuilding", l10n.Param("count", s.totalCount))
									}
									if s.errorCount > 0 {
										title += ed.Plural("output.errorCount", s.errorCount)
									}
									return title
								},
								Style: core.Style{FontSize: 12, FontWeight: 700, Color: colors.Text(), FlexGrow: 1},
							},
							ui.ToggleButton{
								TestID: "output-filter-errors",
								Text:   ed.T("output.errors"),
								Label:  ed.T("output.errorsLabel"),
								On: func() bool {
									return state.Get().errorsOnly
								},
								OnChange: func(on bool) {
									ed.SetOutputErrorsOnly(on)
								},
							},
							ui.Button{
								TestID:  "output-clear",
								Text:    ed.T("output.clear"),
								Label:   ed.T("output.clearLabel"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									return state.Get().totalCount == 0
								},
								OnPress: func() {
									ed.ClearOutput()
								},
							},
						},
					},
					ui.ScrollView{
						Style:    core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), MinHeight: core.Pt(0)},
						Children: []core.Node{list},
					},
				},
			}
		},
	}
}
