package flow

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Branching dialogue script runtime using the Yarn Spinner (.yarn) format.
//
// Format decision: Yarn Spinner (.yarn) over compiled ink JSON.
//
//   - Single-file human-authorable without an external compiler: Yarn scripts are
//     plain text authored directly in the repo and parsed by ParseYarn in pure Go,
//     whereas ink's runtime format requires compiling .ink sources through the
//     external C# inklecate compiler to JSON.
//   - First-class localization hooks: Yarn's native #line:<id> tags attach stable
//     string-table keys directly to lines and choices, pairing cleanly with
//     StringResolver without coupling flow to a specific l10n package.
//   - Direct gameplay commands: <<command arg1 arg2>> maps one-to-one onto typed
//     DialogueEvent messages published on flow.Bus (and/or DialogueOptions.OnCommand),
//     keeping narrative scripts decoupled from game systems.
//   - Deterministic and isolated: zero goroutines, zero wall-clock reads, zero
//     package-level state, and serializable DialogueState for save games.

// DefaultMaxStepsPerContinue is the default upper bound on internal statements
// (assignments, branches, jumps, commands) executed in a single Start, Continue,
// or Choose call before returning ErrStepLimitExceeded.
const DefaultMaxStepsPerContinue = 1024

var (
	// ErrNilScript is returned when starting a DialogueRunner with a nil script.
	ErrNilScript = errors.New("flow: nil dialogue script")
	// ErrUnknownNode is returned when starting or jumping to a node not present in the script.
	ErrUnknownNode = errors.New("flow: unknown dialogue node")
	// ErrInvalidChoice is returned when Choose receives an out-of-range option index.
	ErrInvalidChoice = errors.New("flow: invalid dialogue choice index")
	// ErrChoiceUnavailable is returned when Choose selects an option whose <<if>> guard evaluated false.
	ErrChoiceUnavailable = errors.New("flow: dialogue choice is unavailable")
	// ErrNoPendingChoice is returned when Choose is called while no choice menu is active.
	ErrNoPendingChoice = errors.New("flow: no pending dialogue choices")
	// ErrChoiceRequired is returned when Continue is called while a choice menu is awaiting Choose.
	ErrChoiceRequired = errors.New("flow: dialogue is waiting for Choose")
	// ErrStepLimitExceeded is returned when execution exceeds MaxStepsPerContinue without yielding a line, choice, or end.
	ErrStepLimitExceeded = errors.New("flow: dialogue step limit exceeded")
)

// DialogueScript is a parsed Yarn Spinner script containing one or more named nodes.
// A parsed DialogueScript is immutable and safe to share across multiple DialogueRunners.
type DialogueScript struct {
	nodes     map[string]*dialogueNode
	nodeOrder []string
}

// Nodes returns the script's node titles in declaration order.
func (s *DialogueScript) Nodes() []string {
	if s == nil {
		return nil
	}
	return slices.Clone(s.nodeOrder)
}

// HasNode reports whether the script contains a node with the given title.
func (s *DialogueScript) HasNode(title string) bool {
	if s == nil {
		return false
	}
	_, ok := s.nodes[title]
	return ok
}

// NodeHeaders returns a copy of the header metadata declared on the named node,
// or nil if the node does not exist.
func (s *DialogueScript) NodeHeaders(title string) map[string]string {
	if s == nil {
		return nil
	}
	n, ok := s.nodes[title]
	if !ok {
		return nil
	}
	return maps.Clone(n.headers)
}

// DialogueEvent is published onto flow.Bus (via Publish) and/or passed to
// DialogueOptions.OnCommand whenever a script executes a game command such as
// <<give_item key 1>> or <<play_sfx chime>>.
type DialogueEvent struct {
	Command string
	Args    []string
	Node    string
}

// StringResolver resolves localized text for a dialogue line or choice.
//
// It receives the #line:<id> identifier (empty if untagged), the raw source text
// from the Yarn script as fallback, and a snapshot of the runner's variables.
// Any remaining {expr} placeholders in the returned string are interpolated by
// the runner using its current variables.
type StringResolver func(lineID, fallback string, vars map[string]any) string

// StepKind identifies what a DialogueStep produced.
type StepKind int

const (
	// StepEnded indicates the dialogue reached the end of a node, hit <<stop>>, or has not started.
	StepEnded StepKind = iota
	// StepLine indicates a single spoken or narrated line is ready to display.
	StepLine
	// StepChoices indicates one or more player choices are awaiting Choose.
	StepChoices
)

// String returns a human-readable name for the StepKind.
func (k StepKind) String() string {
	switch k {
	case StepEnded:
		return "ended"
	case StepLine:
		return "line"
	case StepChoices:
		return "choices"
	default:
		return fmt.Sprintf("StepKind(%d)", int(k))
	}
}

// DialogueLine holds a resolved dialogue line produced by a DialogueRunner.
type DialogueLine struct {
	Speaker string
	Text    string
	LineID  string
	Tags    []string
}

// DialogueChoice holds one option presented at a StepChoices step.
type DialogueChoice struct {
	Index     int
	Text      string
	LineID    string
	Available bool
	Tags      []string
}

// DialogueStep is the result of Start, Continue, or Choose on a DialogueRunner.
type DialogueStep struct {
	Kind    StepKind
	Line    DialogueLine
	Choices []DialogueChoice
}

// DialogueOptions configures a DialogueRunner.
type DialogueOptions struct {
	// Bus, if non-nil, receives a DialogueEvent via Publish for every game command.
	Bus *Bus
	// OnCommand, if non-nil, is invoked for every game command after Bus delivery.
	OnCommand func(DialogueEvent)
	// StringResolver, if non-nil, resolves localized text for dialogue lines and choices.
	StringResolver StringResolver
	// MaxStepsPerContinue caps internal statements per Start, Continue, or Choose call
	// to guard against infinite <<jump>> loops. Defaults to DefaultMaxStepsPerContinue.
	MaxStepsPerContinue int
}

// DialogueState is a JSON-serializable snapshot of a DialogueRunner's variables
// and node visit counts for save/load persistence.
type DialogueState struct {
	Vars    map[string]any `json:"vars,omitempty"`
	Visited map[string]int `json:"visited,omitempty"`
}

// --- AST ---

type dialogueNode struct {
	title   string
	headers map[string]string
	body    []dialogueStmt
}

type dialogueStmt interface {
	isDialogueStmt()
}

type lineStmt struct {
	speaker string
	text    string
	lineID  string
	tags    []string
}

func (lineStmt) isDialogueStmt() {}

type choiceOption struct {
	text   string
	lineID string
	tags   []string
	cond   dialogueExpr
	body   []dialogueStmt
}

type choiceGroupStmt struct {
	choices []choiceOption
}

func (choiceGroupStmt) isDialogueStmt() {}

type ifBranch struct {
	cond dialogueExpr // nil for <<else>>
	body []dialogueStmt
}

type ifStmt struct {
	branches []ifBranch
}

func (ifStmt) isDialogueStmt() {}

type setStmt struct {
	varName     string
	op          string // "=", "+=", "-=", "*=", "/="
	expr        dialogueExpr
	onlyIfUnset bool
}

func (setStmt) isDialogueStmt() {}

type jumpStmt struct {
	target string
}

func (jumpStmt) isDialogueStmt() {}

type stopStmt struct{}

func (stopStmt) isDialogueStmt() {}

type commandArg struct {
	raw    string
	isVar  bool
	quoted bool
}

type commandStmt struct {
	name string
	args []commandArg
}

func (commandStmt) isDialogueStmt() {}

// --- Yarn Parser ---

type rawLine struct {
	num  int
	text string
}

type bodyLine struct {
	num    int
	indent int
	text   string
}

// ParseYarn parses Yarn Spinner (.yarn) source text into a reusable DialogueScript.
//
// Supported syntax:
//   - Node headers (title: ..., custom key: value) delimited by --- and ===
//   - Comments starting with //
//   - Dialogue lines: [Speaker: ]Text [#line:id] [#tag ...] with {expr} interpolation
//   - Branching choices: -> Option text [<<if cond>>] [#line:id] [#tag ...] with indented bodies
//   - Conditionals: <<if expr>> / <<elseif expr>> / <<else>> / <<endif>>
//   - Variables: <<set $var = expr>> (plus to, +=, -=, *=, /=) and <<declare $var = expr>>
//   - Control flow: <<jump NodeTitle>> and <<stop>>
//   - Game commands: <<command arg1 "quoted arg" $var {$expr}>>
func ParseYarn(src string) (*DialogueScript, error) {
	normalized := strings.ReplaceAll(src, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")

	script := &DialogueScript{
		nodes: make(map[string]*dialogueNode),
	}

	const (
		stateOutside = iota
		stateHeaders
		stateBody
	)

	state := stateOutside
	var curHeaders map[string]string
	var curTitle string
	var curStartLine int
	var curBody []rawLine

	for idx, raw := range lines {
		lineNum := idx + 1
		trimmed := strings.TrimSpace(raw)

		switch state {
		case stateOutside:
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if trimmed == "---" {
				return nil, fmt.Errorf("yarn line %d: unexpected '---' before node headers", lineNum)
			}
			if trimmed == "===" {
				return nil, fmt.Errorf("yarn line %d: unexpected '===' outside node", lineNum)
			}
			curHeaders = make(map[string]string)
			curTitle = ""
			curStartLine = lineNum
			curBody = nil
			state = stateHeaders
			k, v, err := parseHeaderLine(trimmed, lineNum)
			if err != nil {
				return nil, err
			}
			curHeaders[k] = v
			if k == "title" {
				curTitle = v
			}

		case stateHeaders:
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if trimmed == "---" {
				if curTitle == "" {
					return nil, fmt.Errorf("yarn line %d: node starting at line %d is missing a 'title' header", lineNum, curStartLine)
				}
				if _, exists := script.nodes[curTitle]; exists {
					return nil, fmt.Errorf("yarn line %d: duplicate node title %q", lineNum, curTitle)
				}
				state = stateBody
				continue
			}
			if trimmed == "===" {
				return nil, fmt.Errorf("yarn line %d: unexpected '===' before '---' in node %q", lineNum, curTitle)
			}
			k, v, err := parseHeaderLine(trimmed, lineNum)
			if err != nil {
				return nil, err
			}
			curHeaders[k] = v
			if k == "title" {
				curTitle = v
			}

		case stateBody:
			if trimmed == "===" {
				stmts, err := parseNodeBody(curBody)
				if err != nil {
					return nil, err
				}
				script.nodes[curTitle] = &dialogueNode{
					title:   curTitle,
					headers: curHeaders,
					body:    stmts,
				}
				script.nodeOrder = append(script.nodeOrder, curTitle)
				state = stateOutside
				continue
			}
			curBody = append(curBody, rawLine{num: lineNum, text: raw})
		}
	}

	if state == stateHeaders {
		return nil, fmt.Errorf("yarn line %d: unterminated node headers (missing '---')", curStartLine)
	}
	if state == stateBody {
		return nil, fmt.Errorf("yarn line %d: unterminated node %q (missing '===')", curStartLine, curTitle)
	}
	if len(script.nodeOrder) == 0 {
		return nil, fmt.Errorf("yarn: script contains no nodes")
	}
	return script, nil
}

func parseHeaderLine(trimmed string, lineNum int) (string, string, error) {
	colon := strings.IndexByte(trimmed, ':')
	if colon <= 0 {
		return "", "", fmt.Errorf("yarn line %d: invalid header %q (expected 'key: value')", lineNum, trimmed)
	}
	key := strings.ToLower(strings.TrimSpace(trimmed[:colon]))
	val := strings.TrimSpace(stripLineComment(trimmed[colon+1:]))
	if key == "" {
		return "", "", fmt.Errorf("yarn line %d: empty header key", lineNum)
	}
	if key == "title" && val == "" {
		return "", "", fmt.Errorf("yarn line %d: empty node title", lineNum)
	}
	return key, val, nil
}

func measureIndent(s string) int {
	indent := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ':
			indent++
		case '\t':
			indent += 4
		default:
			return indent
		}
	}
	return indent
}

// stripLineComment strips an unescaped // comment outside string literals.
// Patterns like http:// or https:// (where // immediately follows ':') are preserved.
func stripLineComment(s string) string {
	var inQuote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		if c == '\\' && i+1 < len(s) {
			i++
			continue
		}
		if c == '"' || c == '\'' {
			// Only enter quote mode inside commands <<...>> or expressions {...};
			// plain dialogue lines frequently contain apostrophes like "I'm" or "don't".
			if isInsideBrackets(s, i) {
				inQuote = c
			}
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			if i > 0 && s[i-1] == ':' {
				continue
			}
			return strings.TrimRightFunc(s[:i], unicode.IsSpace)
		}
	}
	return strings.TrimRightFunc(s, unicode.IsSpace)
}

func isInsideBrackets(s string, pos int) bool {
	inCmd := false
	braceDepth := 0
	for i := 0; i < pos; i++ {
		if s[i] == '\\' && i+1 < pos {
			i++
			continue
		}
		if !inCmd && s[i] == '<' && i+1 < len(s) && s[i+1] == '<' {
			inCmd = true
			i++
			continue
		}
		if inCmd && s[i] == '>' && i+1 < len(s) && s[i+1] == '>' {
			inCmd = false
			i++
			continue
		}
		if s[i] == '{' {
			braceDepth++
		} else if s[i] == '}' && braceDepth > 0 {
			braceDepth--
		}
	}
	return inCmd || braceDepth > 0
}

func parseNodeBody(rawLines []rawLine) ([]dialogueStmt, error) {
	var blines []bodyLine
	for _, rl := range rawLines {
		indent := measureIndent(rl.text)
		cleaned := strings.TrimSpace(stripLineComment(rl.text))
		if cleaned == "" {
			continue
		}
		blines = append(blines, bodyLine{
			num:    rl.num,
			indent: indent,
			text:   cleaned,
		})
	}

	idx := 0
	stmts, closeKw, err := parseBlock(blines, &idx, 0, false)
	if err != nil {
		return nil, err
	}
	if closeKw != "" {
		return nil, fmt.Errorf("yarn: unexpected <<%s>>", closeKw)
	}
	return stmts, nil
}

func parseBlock(lines []bodyLine, idx *int, minIndent int, stopOnIfControl bool) ([]dialogueStmt, string, error) {
	var stmts []dialogueStmt

	for *idx < len(lines) {
		ln := lines[*idx]
		if ln.indent < minIndent {
			break
		}

		// Command statement: << ... >>
		if strings.HasPrefix(ln.text, "<<") {
			if !strings.HasSuffix(ln.text, ">>") || len(ln.text) < 4 {
				return nil, "", fmt.Errorf("yarn line %d: unclosed command %q", ln.num, ln.text)
			}
			inner := strings.TrimSpace(ln.text[2 : len(ln.text)-2])
			if inner == "" {
				return nil, "", fmt.Errorf("yarn line %d: empty command", ln.num)
			}
			kw, rest := splitFirstWord(inner)
			switch kw {
			case "elseif", "else", "endif":
				if !stopOnIfControl {
					return nil, "", fmt.Errorf("yarn line %d: unexpected <<%s>> without matching <<if>>", ln.num, kw)
				}
				return stmts, kw, nil
			case "if":
				ifStmtNode, err := parseIfBlock(lines, idx, minIndent)
				if err != nil {
					return nil, "", err
				}
				stmts = append(stmts, ifStmtNode)
			case "set":
				st, err := parseSetCommand(rest, false, ln.num)
				if err != nil {
					return nil, "", err
				}
				*idx++
				stmts = append(stmts, st)
			case "declare":
				st, err := parseSetCommand(rest, true, ln.num)
				if err != nil {
					return nil, "", err
				}
				*idx++
				stmts = append(stmts, st)
			case "jump":
				target := strings.TrimSpace(rest)
				if target == "" || strings.ContainsAny(target, " \t") {
					return nil, "", fmt.Errorf("yarn line %d: <<jump>> expects a single target node name, got %q", ln.num, rest)
				}
				*idx++
				stmts = append(stmts, jumpStmt{target: target})
			case "stop":
				if strings.TrimSpace(rest) != "" {
					return nil, "", fmt.Errorf("yarn line %d: <<stop>> takes no arguments", ln.num)
				}
				*idx++
				stmts = append(stmts, stopStmt{})
			default:
				args, err := parseCommandArgs(rest, ln.num)
				if err != nil {
					return nil, "", err
				}
				*idx++
				stmts = append(stmts, commandStmt{name: kw, args: args})
			}
			continue
		}

		// Choice group: consecutive '->' lines at the same indent.
		if strings.HasPrefix(ln.text, "->") {
			choiceIndent := ln.indent
			var group choiceGroupStmt
			for *idx < len(lines) && lines[*idx].indent == choiceIndent && strings.HasPrefix(lines[*idx].text, "->") {
				cln := lines[*idx]
				opt, err := parseChoiceHeader(strings.TrimSpace(cln.text[2:]), cln.num)
				if err != nil {
					return nil, "", err
				}
				*idx++
				body, _, err := parseBlock(lines, idx, choiceIndent+1, false)
				if err != nil {
					return nil, "", err
				}
				opt.body = body
				group.choices = append(group.choices, opt)
			}
			stmts = append(stmts, group)
			continue
		}

		// Plain or speaker dialogue line.
		st, err := parseDialogueLine(ln.text, ln.num)
		if err != nil {
			return nil, "", err
		}
		*idx++
		stmts = append(stmts, st)
	}

	return stmts, "", nil
}

func parseIfBlock(lines []bodyLine, idx *int, minIndent int) (ifStmt, error) {
	openLine := lines[*idx]
	openInner := strings.TrimSpace(openLine.text[2 : len(openLine.text)-2])
	_, condSrc := splitFirstWord(openInner)
	if strings.TrimSpace(condSrc) == "" {
		return ifStmt{}, fmt.Errorf("yarn line %d: <<if>> requires a condition", openLine.num)
	}
	cond, err := parseDialogueExpr(condSrc)
	if err != nil {
		return ifStmt{}, fmt.Errorf("yarn line %d: invalid <<if>> condition: %w", openLine.num, err)
	}
	*idx++

	var res ifStmt
	seenElse := false

	body, closeKw, err := parseBlock(lines, idx, minIndent, true)
	if err != nil {
		return ifStmt{}, err
	}
	res.branches = append(res.branches, ifBranch{cond: cond, body: body})

	for {
		if closeKw == "" {
			return ifStmt{}, fmt.Errorf("yarn line %d: unclosed <<if>> (missing <<endif>>)", openLine.num)
		}
		ctrlLine := lines[*idx]
		ctrlInner := strings.TrimSpace(ctrlLine.text[2 : len(ctrlLine.text)-2])
		_, ctrlRest := splitFirstWord(ctrlInner)
		*idx++

		switch closeKw {
		case "endif":
			if strings.TrimSpace(ctrlRest) != "" {
				return ifStmt{}, fmt.Errorf("yarn line %d: <<endif>> takes no arguments", ctrlLine.num)
			}
			return res, nil
		case "elseif":
			if seenElse {
				return ifStmt{}, fmt.Errorf("yarn line %d: <<elseif>> cannot appear after <<else>>", ctrlLine.num)
			}
			if strings.TrimSpace(ctrlRest) == "" {
				return ifStmt{}, fmt.Errorf("yarn line %d: <<elseif>> requires a condition", ctrlLine.num)
			}
			ec, err := parseDialogueExpr(ctrlRest)
			if err != nil {
				return ifStmt{}, fmt.Errorf("yarn line %d: invalid <<elseif>> condition: %w", ctrlLine.num, err)
			}
			b, nextKw, err := parseBlock(lines, idx, minIndent, true)
			if err != nil {
				return ifStmt{}, err
			}
			res.branches = append(res.branches, ifBranch{cond: ec, body: b})
			closeKw = nextKw
		case "else":
			if seenElse {
				return ifStmt{}, fmt.Errorf("yarn line %d: duplicate <<else>>", ctrlLine.num)
			}
			if strings.TrimSpace(ctrlRest) != "" {
				return ifStmt{}, fmt.Errorf("yarn line %d: <<else>> takes no arguments", ctrlLine.num)
			}
			seenElse = true
			b, nextKw, err := parseBlock(lines, idx, minIndent, true)
			if err != nil {
				return ifStmt{}, err
			}
			res.branches = append(res.branches, ifBranch{cond: nil, body: b})
			closeKw = nextKw
		}
	}
}

func parseSetCommand(rest string, onlyIfUnset bool, lineNum int) (setStmt, error) {
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "$") {
		return setStmt{}, fmt.Errorf("yarn line %d: expected '$var' in assignment, got %q", lineNum, rest)
	}
	i := 1
	for i < len(rest) {
		r, size := utf8.DecodeRuneInString(rest[i:])
		if i == 1 {
			if !unicode.IsLetter(r) && r != '_' {
				break
			}
		} else if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		i += size
	}
	if i == 1 {
		return setStmt{}, fmt.Errorf("yarn line %d: invalid variable name in %q", lineNum, rest)
	}
	varName := rest[1:i]
	afterVar := strings.TrimSpace(rest[i:])

	var op string
	var exprSrc string
	switch {
	case strings.HasPrefix(afterVar, "+="):
		op, exprSrc = "+=", afterVar[2:]
	case strings.HasPrefix(afterVar, "-="):
		op, exprSrc = "-=", afterVar[2:]
	case strings.HasPrefix(afterVar, "*="):
		op, exprSrc = "*=", afterVar[2:]
	case strings.HasPrefix(afterVar, "/="):
		op, exprSrc = "/=", afterVar[2:]
	case strings.HasPrefix(afterVar, "="):
		op, exprSrc = "=", afterVar[1:]
	case strings.HasPrefix(afterVar, "to") && (len(afterVar) == 2 || unicode.IsSpace(rune(afterVar[2]))):
		op, exprSrc = "=", afterVar[2:]
	default:
		return setStmt{}, fmt.Errorf("yarn line %d: expected assignment operator after $%s", lineNum, varName)
	}

	if onlyIfUnset && op != "=" {
		return setStmt{}, fmt.Errorf("yarn line %d: <<declare>> only supports '=' assignment", lineNum)
	}

	exprSrc = strings.TrimSpace(exprSrc)
	if exprSrc == "" {
		return setStmt{}, fmt.Errorf("yarn line %d: missing value expression for $%s", lineNum, varName)
	}
	expr, err := parseDialogueExpr(exprSrc)
	if err != nil {
		return setStmt{}, fmt.Errorf("yarn line %d: invalid expression for $%s: %w", lineNum, varName, err)
	}
	return setStmt{
		varName:     varName,
		op:          op,
		expr:        expr,
		onlyIfUnset: onlyIfUnset,
	}, nil
}

func parseCommandArgs(src string, lineNum int) ([]commandArg, error) {
	var args []commandArg
	i := 0
	n := len(src)
	for i < n {
		for i < n && unicode.IsSpace(rune(src[i])) {
			i++
		}
		if i >= n {
			break
		}
		if src[i] == '"' || src[i] == '\'' {
			quote := src[i]
			i++
			var sb strings.Builder
			closed := false
			for i < n {
				c := src[i]
				if c == '\\' && i+1 < n {
					next := src[i+1]
					switch next {
					case 'n':
						sb.WriteByte('\n')
					case 't':
						sb.WriteByte('\t')
					case 'r':
						sb.WriteByte('\r')
					default:
						sb.WriteByte(next)
					}
					i += 2
					continue
				}
				if c == quote {
					i++
					closed = true
					break
				}
				sb.WriteByte(c)
				i++
			}
			if !closed {
				return nil, fmt.Errorf("yarn line %d: unterminated quoted argument in command", lineNum)
			}
			val := sb.String()
			if err := validateInterpolations(val); err != nil {
				return nil, fmt.Errorf("yarn line %d: %w", lineNum, err)
			}
			args = append(args, commandArg{raw: val, quoted: true})
			continue
		}

		// Unquoted token: may contain { ... } with spaces inside.
		start := i
		braceDepth := 0
		var inQuote byte
		for i < n {
			c := src[i]
			if braceDepth > 0 {
				if inQuote != 0 {
					if c == '\\' && i+1 < n {
						i += 2
						continue
					}
					if c == inQuote {
						inQuote = 0
					}
					i++
					continue
				}
				if c == '"' || c == '\'' {
					inQuote = c
					i++
					continue
				}
				if c == '{' {
					braceDepth++
				} else if c == '}' {
					braceDepth--
				}
				i++
				continue
			}
			if unicode.IsSpace(rune(c)) {
				break
			}
			if c == '{' {
				braceDepth++
			}
			i++
		}
		if braceDepth != 0 {
			return nil, fmt.Errorf("yarn line %d: unclosed '{' in command argument", lineNum)
		}
		tok := src[start:i]
		if isSimpleVarToken(tok) {
			args = append(args, commandArg{raw: tok[1:], isVar: true})
		} else {
			if err := validateInterpolations(tok); err != nil {
				return nil, fmt.Errorf("yarn line %d: %w", lineNum, err)
			}
			args = append(args, commandArg{raw: tok})
		}
	}
	return args, nil
}

func isSimpleVarToken(tok string) bool {
	if len(tok) < 2 || tok[0] != '$' {
		return false
	}
	for i := 1; i < len(tok); {
		r, size := utf8.DecodeRuneInString(tok[i:])
		if i == 1 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		} else if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
		i += size
	}
	return true
}

func findCommandClose(s string, start int) int {
	var inQuote byte
	for i := start; i < len(s); i++ {
		c := s[i]
		if inQuote != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = c
			continue
		}
		if c == '>' && i+1 < len(s) && s[i+1] == '>' {
			return i
		}
	}
	return -1
}

func parseChoiceHeader(s string, lineNum int) (choiceOption, error) {
	var cond dialogueExpr
	// Extract optional inline <<if ...>> anywhere on the choice line.
	if openIdx := strings.Index(s, "<<"); openIdx >= 0 {
		closeIdx := findCommandClose(s, openIdx+2)
		if closeIdx < 0 {
			return choiceOption{}, fmt.Errorf("yarn line %d: unclosed '<<' in choice option", lineNum)
		}
		inner := strings.TrimSpace(s[openIdx+2 : closeIdx])
		kw, condSrc := splitFirstWord(inner)
		if kw != "if" || strings.TrimSpace(condSrc) == "" {
			return choiceOption{}, fmt.Errorf("yarn line %d: choice option only supports <<if expr>> guard, got <<%s>>", lineNum, inner)
		}
		var err error
		cond, err = parseDialogueExpr(condSrc)
		if err != nil {
			return choiceOption{}, fmt.Errorf("yarn line %d: invalid choice <<if>> condition: %w", lineNum, err)
		}
		before := strings.TrimRightFunc(s[:openIdx], unicode.IsSpace)
		after := strings.TrimLeftFunc(s[closeIdx+2:], unicode.IsSpace)
		if before != "" && after != "" {
			s = before + " " + after
		} else {
			s = before + after
		}
		if strings.Contains(s, "<<") {
			return choiceOption{}, fmt.Errorf("yarn line %d: multiple inline commands on a single choice option", lineNum)
		}
	}

	text, lineID, tags := extractLineMetadata(s)
	if text == "" {
		return choiceOption{}, fmt.Errorf("yarn line %d: empty choice text", lineNum)
	}
	if err := validateInterpolations(text); err != nil {
		return choiceOption{}, fmt.Errorf("yarn line %d: %w", lineNum, err)
	}
	return choiceOption{
		text:   text,
		lineID: lineID,
		tags:   tags,
		cond:   cond,
	}, nil
}

func parseDialogueLine(s string, lineNum int) (lineStmt, error) {
	body, lineID, tags := extractLineMetadata(s)
	speaker, text := splitSpeaker(body)
	if err := validateInterpolations(text); err != nil {
		return lineStmt{}, fmt.Errorf("yarn line %d: %w", lineNum, err)
	}
	return lineStmt{
		speaker: speaker,
		text:    text,
		lineID:  lineID,
		tags:    tags,
	}, nil
}

// extractLineMetadata peels trailing #line:<id> and #tag metadata tokens off a line.
// Escaped \# sequences remain in the text as literal '#'.
func extractLineMetadata(s string) (text, lineID string, tags []string) {
	s = strings.TrimSpace(s)
	// Peel trailing whitespace-delimited #tags from the right.
	var revTags []string
	cur := s
	for {
		cur = strings.TrimRightFunc(cur, unicode.IsSpace)
		if cur == "" {
			break
		}
		// Find last whitespace boundary (or start of string).
		lastSpace := strings.LastIndexFunc(cur, unicode.IsSpace)
		token := cur[lastSpace+1:]
		if !strings.HasPrefix(token, "#") || len(token) == 1 {
			break
		}
		revTags = append(revTags, token[1:])
		if lastSpace < 0 {
			cur = ""
			break
		}
		cur = cur[:lastSpace]
	}

	// Restore left-to-right tag order.
	for i := len(revTags) - 1; i >= 0; i-- {
		tag := revTags[i]
		if strings.HasPrefix(tag, "line:") {
			lineID = tag[len("line:"):]
		} else {
			tags = append(tags, tag)
		}
	}

	text = strings.ReplaceAll(strings.TrimSpace(cur), `\#`, "#")
	return text, lineID, tags
}

// splitSpeaker separates an optional "Speaker: text" prefix.
// Escaped colons (\:) prevent speaker splitting.
func splitSpeaker(s string) (speaker, text string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			continue
		}
		if s[i] == '{' {
			break
		}
		if s[i] == ':' {
			// Require whitespace or end-of-line after ':' so "12:30" or "https://..." is not a speaker.
			if i+1 < len(s) && !unicode.IsSpace(rune(s[i+1])) {
				break
			}
			cand := strings.TrimSpace(s[:i])
			if isValidSpeakerName(cand) {
				rest := strings.TrimSpace(s[i+1:])
				return cand, strings.ReplaceAll(rest, `\:`, ":")
			}
			break
		}
	}
	return "", strings.ReplaceAll(s, `\:`, ":")
}

func isValidSpeakerName(s string) bool {
	if s == "" {
		return false
	}
	first := true
	for _, r := range s {
		if first {
			first = false
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != ' ' && r != '_' && r != '-' && r != '.' && r != '\'' {
			return false
		}
	}
	return true
}

func splitFirstWord(s string) (word, rest string) {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if unicode.IsSpace(r) {
			return s[:i], strings.TrimSpace(s[i:])
		}
	}
	return s, ""
}

// --- Runtime ---

type execFrame struct {
	stmts []dialogueStmt
	index int
}

// DialogueRunner executes a parsed DialogueScript step by step.
//
// It maintains variables, node visit counts, and the active execution stack
// without goroutines or global state. Game commands execute synchronously and
// publish typed DialogueEvent values on DialogueOptions.Bus when configured.
type DialogueRunner struct {
	script         *DialogueScript
	opts           DialogueOptions
	vars           map[string]any
	visited        map[string]int
	currentNode    string
	frames         []execFrame
	pendingChoices []DialogueChoice
	pendingOptions []choiceOption
}

// NewDialogueRunner creates a runner for script with the given options.
func NewDialogueRunner(script *DialogueScript, opts DialogueOptions) *DialogueRunner {
	if opts.MaxStepsPerContinue <= 0 {
		opts.MaxStepsPerContinue = DefaultMaxStepsPerContinue
	}
	return &DialogueRunner{
		script:  script,
		opts:    opts,
		vars:    make(map[string]any),
		visited: make(map[string]int),
	}
}

// Start begins executing the script at the named node and advances until the
// first line, choice menu, or end of dialogue.
func (r *DialogueRunner) Start(node string) (DialogueStep, error) {
	if r.script == nil {
		return DialogueStep{}, ErrNilScript
	}
	n, ok := r.script.nodes[node]
	if !ok {
		return DialogueStep{}, fmt.Errorf("%w: %q", ErrUnknownNode, node)
	}
	r.frames = []execFrame{{stmts: n.body}}
	r.pendingChoices = nil
	r.pendingOptions = nil
	r.currentNode = node
	r.visited[node]++
	return r.advance()
}

// Continue advances an active dialogue to the next line, choice menu, or end.
// Calling Continue while a choice menu is awaiting Choose returns ErrChoiceRequired.
func (r *DialogueRunner) Continue() (DialogueStep, error) {
	if len(r.pendingChoices) > 0 {
		return DialogueStep{}, ErrChoiceRequired
	}
	if len(r.frames) == 0 {
		return DialogueStep{Kind: StepEnded}, nil
	}
	return r.advance()
}

// Choose selects optionIndex from the active StepChoices step and advances to
// the next line, choice menu, or end of dialogue.
func (r *DialogueRunner) Choose(optionIndex int) (DialogueStep, error) {
	if len(r.pendingChoices) == 0 {
		return DialogueStep{}, ErrNoPendingChoice
	}
	if optionIndex < 0 || optionIndex >= len(r.pendingChoices) {
		return DialogueStep{}, fmt.Errorf("%w: %d (count %d)", ErrInvalidChoice, optionIndex, len(r.pendingChoices))
	}
	if !r.pendingChoices[optionIndex].Available {
		return DialogueStep{}, fmt.Errorf("%w: option %d", ErrChoiceUnavailable, optionIndex)
	}
	body := r.pendingOptions[optionIndex].body
	r.pendingChoices = nil
	r.pendingOptions = nil
	if len(body) > 0 {
		r.frames = append(r.frames, execFrame{stmts: body})
	}
	return r.advance()
}

// CurrentNode returns the title of the currently executing node, or "" when ended.
func (r *DialogueRunner) CurrentNode() string {
	return r.currentNode
}

// SetVar sets a dialogue variable. A leading '$' is optional and normalized away.
// Numeric Go types are normalized to float64 to match Yarn expression semantics.
func (r *DialogueRunner) SetVar(name string, val any) {
	if r.vars == nil {
		r.vars = make(map[string]any)
	}
	r.vars[normalizeVarName(name)] = normalizeDialogueValue(val)
}

// Var returns the current value of name (leading '$' optional) and whether it is set.
func (r *DialogueRunner) Var(name string) (any, bool) {
	if r.vars == nil {
		return nil, false
	}
	v, ok := r.vars[normalizeVarName(name)]
	return v, ok
}

// Vars returns a shallow copy of all currently set dialogue variables (keys without leading '$').
func (r *DialogueRunner) Vars() map[string]any {
	out := make(map[string]any, len(r.vars))
	maps.Copy(out, r.vars)
	return out
}

// Visited returns how many times the named node has been entered via Start or <<jump>>.
func (r *DialogueRunner) Visited(node string) int {
	if r.visited == nil {
		return 0
	}
	return r.visited[node]
}

// State returns a serializable snapshot of the runner's variables and visit counts.
func (r *DialogueRunner) State() DialogueState {
	visited := make(map[string]int, len(r.visited))
	maps.Copy(visited, r.visited)
	return DialogueState{
		Vars:    r.Vars(),
		Visited: visited,
	}
}

// LoadState restores variables and visit counts from a previously saved DialogueState.
func (r *DialogueRunner) LoadState(state DialogueState) {
	r.vars = make(map[string]any, len(state.Vars))
	for k, v := range state.Vars {
		r.vars[normalizeVarName(k)] = normalizeDialogueValue(v)
	}
	r.visited = make(map[string]int, len(state.Visited))
	maps.Copy(r.visited, state.Visited)
}

func normalizeVarName(name string) string {
	return strings.TrimPrefix(name, "$")
}

func (r *DialogueRunner) env() *exprEnv {
	return &exprEnv{
		vars:    r.vars,
		visited: r.visited,
	}
}

func (r *DialogueRunner) resolveAndInterpolate(lineID, rawText string) (string, error) {
	text := rawText
	if r.opts.StringResolver != nil {
		text = r.opts.StringResolver(lineID, rawText, r.Vars())
	}
	return interpolateText(text, r.env())
}

func (r *DialogueRunner) advance() (DialogueStep, error) {
	limit := r.opts.MaxStepsPerContinue
	if limit <= 0 {
		limit = DefaultMaxStepsPerContinue
	}
	steps := 0

	for len(r.frames) > 0 {
		top := &r.frames[len(r.frames)-1]
		if top.index >= len(top.stmts) {
			r.frames = r.frames[:len(r.frames)-1]
			continue
		}

		steps++
		if steps > limit {
			r.frames = nil
			r.pendingChoices = nil
			r.pendingOptions = nil
			node := r.currentNode
			r.currentNode = ""
			return DialogueStep{}, fmt.Errorf("%w: exceeded %d steps in node %q", ErrStepLimitExceeded, limit, node)
		}

		stmt := top.stmts[top.index]
		top.index++

		switch st := stmt.(type) {
		case lineStmt:
			text, err := r.resolveAndInterpolate(st.lineID, st.text)
			if err != nil {
				return DialogueStep{}, err
			}
			return DialogueStep{
				Kind: StepLine,
				Line: DialogueLine{
					Speaker: st.speaker,
					Text:    text,
					LineID:  st.lineID,
					Tags:    slices.Clone(st.tags),
				},
			}, nil

		case choiceGroupStmt:
			choices := make([]DialogueChoice, len(st.choices))
			anyAvailable := false
			env := r.env()
			for i, opt := range st.choices {
				avail := true
				if opt.cond != nil {
					v, err := opt.cond.eval(env)
					if err != nil {
						return DialogueStep{}, err
					}
					avail = toDialogueBool(v)
				}
				if avail {
					anyAvailable = true
				}
				text, err := r.resolveAndInterpolate(opt.lineID, opt.text)
				if err != nil {
					return DialogueStep{}, err
				}
				choices[i] = DialogueChoice{
					Index:     i,
					Text:      text,
					LineID:    opt.lineID,
					Available: avail,
					Tags:      slices.Clone(opt.tags),
				}
			}
			// If no choice is currently available, skip the menu so dialogue does not deadlock.
			if !anyAvailable {
				continue
			}
			r.pendingChoices = choices
			r.pendingOptions = st.choices
			return DialogueStep{
				Kind:    StepChoices,
				Choices: slices.Clone(choices),
			}, nil

		case ifStmt:
			env := r.env()
			for _, br := range st.branches {
				matched := true
				if br.cond != nil {
					v, err := br.cond.eval(env)
					if err != nil {
						return DialogueStep{}, err
					}
					matched = toDialogueBool(v)
				}
				if matched {
					if len(br.body) > 0 {
						r.frames = append(r.frames, execFrame{stmts: br.body})
					}
					break
				}
			}

		case setStmt:
			if st.onlyIfUnset {
				if _, exists := r.vars[st.varName]; exists {
					continue
				}
			}
			val, err := st.expr.eval(r.env())
			if err != nil {
				return DialogueStep{}, err
			}
			if st.op == "=" {
				r.SetVar(st.varName, val)
				continue
			}
			cur := r.vars[st.varName]
			switch st.op {
			case "+=":
				_, lStr := cur.(string)
				_, rStr := val.(string)
				if lStr || rStr {
					r.SetVar(st.varName, formatDialogueValue(cur)+formatDialogueValue(val))
				} else {
					ln, err := toDialogueNumber(cur)
					if err != nil {
						return DialogueStep{}, err
					}
					rn, err := toDialogueNumber(val)
					if err != nil {
						return DialogueStep{}, err
					}
					r.SetVar(st.varName, ln+rn)
				}
			case "-=":
				ln, err := toDialogueNumber(cur)
				if err != nil {
					return DialogueStep{}, err
				}
				rn, err := toDialogueNumber(val)
				if err != nil {
					return DialogueStep{}, err
				}
				r.SetVar(st.varName, ln-rn)
			case "*=":
				ln, err := toDialogueNumber(cur)
				if err != nil {
					return DialogueStep{}, err
				}
				rn, err := toDialogueNumber(val)
				if err != nil {
					return DialogueStep{}, err
				}
				r.SetVar(st.varName, ln*rn)
			case "/=":
				ln, err := toDialogueNumber(cur)
				if err != nil {
					return DialogueStep{}, err
				}
				rn, err := toDialogueNumber(val)
				if err != nil {
					return DialogueStep{}, err
				}
				if rn == 0 {
					return DialogueStep{}, fmt.Errorf("dialogue: division by zero in $%s /=", st.varName)
				}
				r.SetVar(st.varName, ln/rn)
			}

		case jumpStmt:
			target, ok := r.script.nodes[st.target]
			if !ok {
				return DialogueStep{}, fmt.Errorf("%w: %q", ErrUnknownNode, st.target)
			}
			r.frames = []execFrame{{stmts: target.body}}
			r.currentNode = st.target
			r.visited[st.target]++

		case stopStmt:
			r.frames = nil
			r.pendingChoices = nil
			r.pendingOptions = nil
			r.currentNode = ""
			return DialogueStep{Kind: StepEnded}, nil

		case commandStmt:
			env := r.env()
			args := make([]string, len(st.args))
			for i, a := range st.args {
				if a.isVar {
					args[i] = formatDialogueValue(env.lookup(a.raw))
				} else {
					s, err := interpolateText(a.raw, env)
					if err != nil {
						return DialogueStep{}, err
					}
					args[i] = s
				}
			}
			ev := DialogueEvent{
				Command: st.name,
				Args:    args,
				Node:    r.currentNode,
			}
			if r.opts.Bus != nil {
				Publish(r.opts.Bus, ev)
			}
			if r.opts.OnCommand != nil {
				r.opts.OnCommand(ev)
			}
		}
	}

	r.currentNode = ""
	return DialogueStep{Kind: StepEnded}, nil
}
