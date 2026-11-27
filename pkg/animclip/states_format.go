package animclip

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/resource"
	"go.yaml.in/yaml/v3"
)

// StateMachineVersion is the schema version of *.states.yaml files this package reads and writes.
const StateMachineVersion = 1

// AnyState is the special TransitionDoc.From value matching any active state other than TransitionDoc.To.
const AnyState = "*"

// StateMachineSchemaURL is where the JSON Schema of version 1 sprite state machine documents is published.
const StateMachineSchemaURL = "https://game.nsgo.dev/schemas/states.v1.json"

// StateMachineSchemaComment is the first line EncodeStateMachine writes, pointing YAML language servers at StateMachineSchemaURL.
const StateMachineSchemaComment = "# yaml-language-server: $schema=" + StateMachineSchemaURL

// ParamType is the value kind of a state machine parameter.
type ParamType string

const (
	// ParamFloat holds a finite float64 value (default 0).
	ParamFloat ParamType = "float"
	// ParamBool holds a boolean value (default false).
	ParamBool ParamType = "bool"
	// ParamTrigger is a one-shot boolean pulse set via Trigger and cleared at the end of Update.
	ParamTrigger ParamType = "trigger"
)

// StateMachineDoc is the plain-text YAML schema of a *.states.yaml sprite animation state machine.
type StateMachineDoc struct {
	// Version is the schema version; it must equal StateMachineVersion (1).
	Version int `yaml:"version" schema:"required"`
	// UID is an optional stable file ID assigned by game index or the editor for safe renames.
	UID string `yaml:"uid,omitempty"`
	// Name is an optional human-readable display name for the state machine.
	Name string `yaml:"name,omitempty"`
	// Entry is the name of the state the machine enters when playback starts or resets.
	Entry string `yaml:"entry" schema:"required"`
	// Parameters are the named inputs that transition conditions read.
	Parameters []ParamDoc `yaml:"parameters,omitempty"`
	// States are the named animation states, each playing one animation clip.
	States []StateDoc `yaml:"states" schema:"required"`
	// Transitions are the directed edges evaluated in declaration order on every fixed Update step.
	Transitions []TransitionDoc `yaml:"transitions,omitempty"`
}

// ParamDoc declares one named parameter of a StateMachineDoc.
type ParamDoc struct {
	// Name is the parameter identifier used in transition expressions, such as "move.x", "velocity.y", "on_floor" or "jump".
	Name string `yaml:"name" schema:"required"`
	// Type is the parameter kind: "float", "bool" or "trigger".
	Type ParamType `yaml:"type" schema:"required,enum=float|bool|trigger"`
	// Default is the optional starting value: a number for "float", a boolean for "bool", and omitted for "trigger".
	Default any `yaml:"default,omitempty"`
}

// StateDoc declares one state of a StateMachineDoc.
type StateDoc struct {
	// Name is the unique state name within the state machine, such as "idle" or "run".
	Name string `yaml:"name" schema:"required"`
	// Clip is the project-relative path or resolver key of the animation clip resource played while this state is active.
	Clip string `yaml:"clip" schema:"required"`
	// Speed is an optional playback speed multiplier (> 0); 0 or omitted plays at 1x speed.
	Speed float32 `yaml:"speed,omitempty" schema:"minimum=0"`
	// Loop optionally overrides the clip's own loop setting while played in this state.
	Loop *bool `yaml:"loop,omitempty"`
	// X is an optional horizontal coordinate of this state node in the editor graph.
	X *float32 `yaml:"x,omitempty"`
	// Y is an optional vertical coordinate of this state node in the editor graph.
	Y *float32 `yaml:"y,omitempty"`
}

// TransitionDoc declares one directed transition from From (or "*" for any state) to To.
type TransitionDoc struct {
	// From is the source state name, or "*" (AnyState) to match any state other than To.
	From string `yaml:"from" schema:"required"`
	// To is the target state name; it must name a state in States.
	To string `yaml:"to" schema:"required"`
	// Label is an optional human-readable summary shown on the editor graph edge and LEAVING list.
	Label string `yaml:"label,omitempty"`
	// When is an optional boolean expression over declared parameters, such as "|move.x| > 0.1" or "jump && on_floor".
	When string `yaml:"when,omitempty"`
	// OnEnd fires this transition when the source state's clip finishes playing (and When, if set, is also true).
	OnEnd bool `yaml:"onEnd,omitempty"`
}

// StateMachineError reports a validation error in a StateMachineDoc together with the YAML field path that caused it, so callers such as assetcheck can locate the exact line.
type StateMachineError struct {
	// Path is the sequence of YAML mapping keys and decimal slice indices leading to the offending field, such as ["transitions", "2", "to"].
	Path []string
	// Message describes what is wrong and how to fix it.
	Message string
}

// Error implements error.
func (e *StateMachineError) Error() string {
	if len(e.Path) == 0 {
		return "animclip: " + e.Message
	}
	return fmt.Sprintf("animclip: %s: %s", strings.Join(e.Path, "."), e.Message)
}

func smErr(msg string, path ...string) error {
	return &StateMachineError{Path: path, Message: msg}
}

// ParseStateMachine decodes data as a *.states.yaml document, refusing unknown fields, trailing documents, unsupported versions and invalid states, parameters or transition expressions.
func ParseStateMachine(data []byte) (*StateMachineDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, smErr("empty state machine document")
	}
	var doc StateMachineDoc
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("animclip: decode states: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, smErr("unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("animclip: decode states: %w", err)
	}
	if err := ValidateStateMachine(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// ValidateStateMachine checks doc's version, header, parameters, states, entry state and every transition's endpoints and condition expression.
func ValidateStateMachine(doc *StateMachineDoc) error {
	if doc == nil {
		return smErr("nil state machine")
	}
	if doc.Version != StateMachineVersion {
		return smErr(fmt.Sprintf("unsupported version %d (want %d)", doc.Version, StateMachineVersion), "version")
	}
	if doc.UID != "" && !resource.ValidUID(doc.UID) {
		return smErr(fmt.Sprintf("uid %q must be %d characters of 0-9 and a-z without i, l, o and u", doc.UID, resource.UIDLength), "uid")
	}

	params := make(map[string]ParamType, len(doc.Parameters))
	for i := range doc.Parameters {
		p := &doc.Parameters[i]
		idx := strconv.Itoa(i)
		if !validParamIdent(p.Name) {
			return smErr(fmt.Sprintf("invalid parameter name %q (use letters, digits, '_' and '.', starting with a letter or '_')", p.Name), "parameters", idx, "name")
		}
		if _, dup := params[p.Name]; dup {
			return smErr(fmt.Sprintf("duplicate parameter %q", p.Name), "parameters", idx, "name")
		}
		switch p.Type {
		case ParamFloat:
			if p.Default != nil {
				v, ok := toFloat64(p.Default)
				if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
					return smErr(fmt.Sprintf("float parameter %q default must be a finite number", p.Name), "parameters", idx, "default")
				}
				p.Default = v
			}
		case ParamBool:
			if p.Default != nil {
				if _, ok := p.Default.(bool); !ok {
					return smErr(fmt.Sprintf("bool parameter %q default must be a boolean", p.Name), "parameters", idx, "default")
				}
			}
		case ParamTrigger:
			if p.Default != nil {
				return smErr(fmt.Sprintf("trigger parameter %q cannot have a default value", p.Name), "parameters", idx, "default")
			}
		default:
			return smErr(fmt.Sprintf("parameter %q has unknown type %q (want %q, %q or %q)", p.Name, p.Type, ParamFloat, ParamBool, ParamTrigger), "parameters", idx, "type")
		}
		params[p.Name] = p.Type
	}

	if len(doc.States) == 0 {
		return smErr("state machine must define at least one state", "states")
	}
	states := make(map[string]struct{}, len(doc.States))
	for i := range doc.States {
		st := &doc.States[i]
		idx := strconv.Itoa(i)
		if strings.TrimSpace(st.Name) == "" || st.Name != strings.TrimSpace(st.Name) || st.Name == AnyState {
			return smErr(fmt.Sprintf("invalid state name %q", st.Name), "states", idx, "name")
		}
		if _, dup := states[st.Name]; dup {
			return smErr(fmt.Sprintf("duplicate state %q", st.Name), "states", idx, "name")
		}
		if strings.TrimSpace(st.Clip) == "" {
			return smErr(fmt.Sprintf("state %q must name a clip", st.Name), "states", idx, "clip")
		}
		if math.IsNaN(float64(st.Speed)) || math.IsInf(float64(st.Speed), 0) || st.Speed < 0 {
			return smErr(fmt.Sprintf("state %q speed %v must be finite and non-negative", st.Name, st.Speed), "states", idx, "speed")
		}
		if st.X != nil && (math.IsNaN(float64(*st.X)) || math.IsInf(float64(*st.X), 0)) {
			return smErr(fmt.Sprintf("state %q x must be finite", st.Name), "states", idx, "x")
		}
		if st.Y != nil && (math.IsNaN(float64(*st.Y)) || math.IsInf(float64(*st.Y), 0)) {
			return smErr(fmt.Sprintf("state %q y must be finite", st.Name), "states", idx, "y")
		}
		states[st.Name] = struct{}{}
	}

	if doc.Entry == "" {
		return smErr("missing entry state", "entry")
	}
	if _, ok := states[doc.Entry]; !ok {
		return smErr(fmt.Sprintf("entry state %q is not defined", doc.Entry), "entry")
	}

	for i := range doc.Transitions {
		tr := &doc.Transitions[i]
		idx := strconv.Itoa(i)
		if tr.From == "" {
			return smErr("transition is missing 'from' state", "transitions", idx, "from")
		}
		if tr.From != AnyState {
			if _, ok := states[tr.From]; !ok {
				return smErr(fmt.Sprintf("transition from missing state %q", tr.From), "transitions", idx, "from")
			}
		}
		if tr.To == "" {
			return smErr("transition is missing 'to' state", "transitions", idx, "to")
		}
		if _, ok := states[tr.To]; !ok {
			return smErr(fmt.Sprintf("transition to missing state %q", tr.To), "transitions", idx, "to")
		}
		if tr.From == tr.To {
			return smErr(fmt.Sprintf("transition from %q to itself is not allowed", tr.From), "transitions", idx, "to")
		}
		when := strings.TrimSpace(tr.When)
		if tr.When != "" && when == "" {
			return smErr(fmt.Sprintf("transition %s -> %s condition must not be whitespace-only", tr.From, tr.To), "transitions", idx, "when")
		}
		if when == "" && !tr.OnEnd {
			return smErr(fmt.Sprintf("transition %s -> %s must set 'when' or 'onEnd: true'", tr.From, tr.To), "transitions", idx)
		}
		if when != "" {
			if _, err := CompileCondition(when, params); err != nil {
				return smErr(fmt.Sprintf("transition %s -> %s condition %q: %v", tr.From, tr.To, tr.When, err), "transitions", idx, "when")
			}
		}
	}
	return nil
}

// EncodeStateMachine validates doc and writes deterministic 2-space YAML preceded by StateMachineSchemaComment.
func EncodeStateMachine(doc *StateMachineDoc) ([]byte, error) {
	if err := ValidateStateMachine(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(StateMachineSchemaComment)
	buf.WriteByte('\n')
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("animclip: encode states: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("animclip: encode states: %w", err)
	}
	return buf.Bytes(), nil
}

// Clone returns a deep copy of doc.
func (doc *StateMachineDoc) Clone() *StateMachineDoc {
	if doc == nil {
		return nil
	}
	out := *doc
	out.Parameters = slices.Clone(doc.Parameters)
	if doc.States != nil {
		out.States = make([]StateDoc, len(doc.States))
		for i, st := range doc.States {
			out.States[i] = st
			if st.Loop != nil {
				v := *st.Loop
				out.States[i].Loop = &v
			}
			if st.X != nil {
				v := *st.X
				out.States[i].X = &v
			}
			if st.Y != nil {
				v := *st.Y
				out.States[i].Y = &v
			}
		}
	}
	out.Transitions = slices.Clone(doc.Transitions)
	return &out
}

// State returns a pointer to the named state in doc, or nil when absent.
func (doc *StateMachineDoc) State(name string) *StateDoc {
	if doc == nil {
		return nil
	}
	for i := range doc.States {
		if doc.States[i].Name == name {
			return &doc.States[i]
		}
	}
	return nil
}

// UsedParameters returns the set of parameter names referenced by at least one transition's When condition.
func (doc *StateMachineDoc) UsedParameters() map[string]bool {
	used := make(map[string]bool)
	if doc == nil {
		return used
	}
	params := doc.paramTypeMap()
	for _, tr := range doc.Transitions {
		when := strings.TrimSpace(tr.When)
		if when == "" {
			continue
		}
		cond, err := CompileCondition(when, params)
		if err != nil {
			continue
		}
		for _, p := range cond.Params() {
			used[p] = true
		}
	}
	return used
}

// UnusedParameters returns the indices into doc.Parameters of parameters that no transition condition reads.
func (doc *StateMachineDoc) UnusedParameters() []int {
	if doc == nil {
		return nil
	}
	used := doc.UsedParameters()
	var unused []int
	for i, p := range doc.Parameters {
		if !used[p.Name] {
			unused = append(unused, i)
		}
	}
	return unused
}

func (doc *StateMachineDoc) paramTypeMap() map[string]ParamType {
	m := make(map[string]ParamType, len(doc.Parameters))
	for _, p := range doc.Parameters {
		m[p.Name] = p.Type
	}
	return m
}

func validParamIdent(s string) bool {
	if s == "" || s == "true" || s == "false" || s == "and" || s == "or" || s == "not" || s == "abs" {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if i == 0 {
			if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_') {
				return false
			}
			continue
		}
		if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '.') {
			return false
		}
		if b == '.' && (i == len(s)-1 || s[i-1] == '.') {
			return false
		}
	}
	return true
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// Condition is a compiled boolean transition expression over named state machine parameters.
type Condition struct {
	root   condNode
	params []string
}

// Params returns the sorted unique parameter names referenced by c.
func (c *Condition) Params() []string {
	if c == nil {
		return nil
	}
	return slices.Clone(c.params)
}

// Eval evaluates c against the given float and bool/trigger parameter lookup functions.
func (c *Condition) Eval(getFloat func(string) float64, getBool func(string) bool) bool {
	if c == nil || c.root == nil {
		return true
	}
	return c.root.evalBool(getFloat, getBool)
}

// CompileCondition parses and type-checks expr against params (mapping parameter name to ParamType).
// Supported syntax:
//   - Logical: || (or), && (and), ! (not), ( ... )
//   - Comparisons: ==, !=, <, <=, >, >= (and Unicode ≤, ≥)
//   - Numeric atoms: float literals, float parameter names, |param|, abs(expr), unary -
//   - Boolean atoms: true, false, bool/trigger parameter names, bool == true/false
func CompileCondition(expr string, params map[string]ParamType) (*Condition, error) {
	tokens, err := lexCond(expr)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, errors.New("empty condition expression")
	}
	p := &condParser{
		tokens: tokens,
		params: params,
		used:   make(map[string]struct{}),
	}
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.tokens) {
		return nil, fmt.Errorf("unexpected token %q", p.tokens[p.pos].text)
	}
	used := make([]string, 0, len(p.used))
	for name := range p.used {
		used = append(used, name)
	}
	slices.Sort(used)
	return &Condition{root: node, params: used}, nil
}

type condTokKind uint8

const (
	tokEOF condTokKind = iota
	tokIdent
	tokNumber
	tokAnd
	tokOr
	tokNot
	tokLParen
	tokRParen
	tokBar
	tokEQ
	tokNE
	tokLT
	tokLE
	tokGT
	tokGE
	tokMinus
)

type condTok struct {
	kind condTokKind
	text string
	num  float64
}

func lexCond(s string) ([]condTok, error) {
	var out []condTok
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if strings.HasPrefix(s[i:], "≤") {
			out = append(out, condTok{kind: tokLE, text: "<="})
			i += len("≤")
			continue
		}
		if strings.HasPrefix(s[i:], "≥") {
			out = append(out, condTok{kind: tokGE, text: ">="})
			i += len("≥")
			continue
		}
		switch c {
		case '(':
			out = append(out, condTok{kind: tokLParen, text: "("})
			i++
		case ')':
			out = append(out, condTok{kind: tokRParen, text: ")"})
			i++
		case '-':
			out = append(out, condTok{kind: tokMinus, text: "-"})
			i++
		case '&':
			if i+1 < len(s) && s[i+1] == '&' {
				out = append(out, condTok{kind: tokAnd, text: "&&"})
				i += 2
			} else {
				return nil, fmt.Errorf("unexpected '&' (use '&&')")
			}
		case '|':
			if i+1 < len(s) && s[i+1] == '|' {
				out = append(out, condTok{kind: tokOr, text: "||"})
				i += 2
			} else {
				out = append(out, condTok{kind: tokBar, text: "|"})
				i++
			}
		case '!':
			if i+1 < len(s) && s[i+1] == '=' {
				out = append(out, condTok{kind: tokNE, text: "!="})
				i += 2
			} else {
				out = append(out, condTok{kind: tokNot, text: "!"})
				i++
			}
		case '=':
			if i+1 < len(s) && s[i+1] == '=' {
				out = append(out, condTok{kind: tokEQ, text: "=="})
				i += 2
			} else {
				return nil, fmt.Errorf("unexpected '=' (use '==')")
			}
		case '<':
			if i+1 < len(s) && s[i+1] == '=' {
				out = append(out, condTok{kind: tokLE, text: "<="})
				i += 2
			} else {
				out = append(out, condTok{kind: tokLT, text: "<"})
				i++
			}
		case '>':
			if i+1 < len(s) && s[i+1] == '=' {
				out = append(out, condTok{kind: tokGE, text: ">="})
				i += 2
			} else {
				out = append(out, condTok{kind: tokGT, text: ">"})
				i++
			}
		default:
			if (c >= '0' && c <= '9') || (c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9') {
				start := i
				dot := false
				for i < len(s) {
					if s[i] >= '0' && s[i] <= '9' {
						i++
						continue
					}
					if s[i] == '.' && !dot {
						dot = true
						i++
						continue
					}
					break
				}
				txt := s[start:i]
				val, err := strconv.ParseFloat(txt, 64)
				if err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
					return nil, fmt.Errorf("invalid number %q", txt)
				}
				out = append(out, condTok{kind: tokNumber, text: txt, num: val})
				continue
			}
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
				start := i
				for i < len(s) {
					b := s[i]
					if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '.' {
						i++
					} else {
						break
					}
				}
				txt := s[start:i]
				switch txt {
				case "and":
					out = append(out, condTok{kind: tokAnd, text: txt})
				case "or":
					out = append(out, condTok{kind: tokOr, text: txt})
				case "not":
					out = append(out, condTok{kind: tokNot, text: txt})
				default:
					out = append(out, condTok{kind: tokIdent, text: txt})
				}
				continue
			}
			return nil, fmt.Errorf("unexpected character %q", c)
		}
	}
	return out, nil
}

type condNode interface {
	evalBool(getFloat func(string) float64, getBool func(string) bool) bool
}

type numNode interface {
	evalNum(getFloat func(string) float64) float64
}

type condOrNode struct{ a, b condNode }

func (n condOrNode) evalBool(gf func(string) float64, gb func(string) bool) bool {
	return n.a.evalBool(gf, gb) || n.b.evalBool(gf, gb)
}

type condAndNode struct{ a, b condNode }

func (n condAndNode) evalBool(gf func(string) float64, gb func(string) bool) bool {
	return n.a.evalBool(gf, gb) && n.b.evalBool(gf, gb)
}

type condNotNode struct{ x condNode }

func (n condNotNode) evalBool(gf func(string) float64, gb func(string) bool) bool {
	return !n.x.evalBool(gf, gb)
}

type condLitBool struct{ v bool }

func (n condLitBool) evalBool(func(string) float64, func(string) bool) bool { return n.v }

type condParamBool struct{ name string }

func (n condParamBool) evalBool(_ func(string) float64, gb func(string) bool) bool {
	return gb(n.name)
}

type condBoolEq struct {
	a, b condNode
	neg  bool
}

func (n condBoolEq) evalBool(gf func(string) float64, gb func(string) bool) bool {
	eq := n.a.evalBool(gf, gb) == n.b.evalBool(gf, gb)
	if n.neg {
		return !eq
	}
	return eq
}

type condCmpNum struct {
	op   condTokKind
	a, b numNode
}

func (n condCmpNum) evalBool(gf func(string) float64, _ func(string) bool) bool {
	av, bv := n.a.evalNum(gf), n.b.evalNum(gf)
	switch n.op {
	case tokEQ:
		return av == bv
	case tokNE:
		return av != bv
	case tokLT:
		return av < bv
	case tokLE:
		return av <= bv
	case tokGT:
		return av > bv
	case tokGE:
		return av >= bv
	default:
		return false
	}
}

type numLit struct{ v float64 }

func (n numLit) evalNum(func(string) float64) float64 { return n.v }

type numParam struct{ name string }

func (n numParam) evalNum(gf func(string) float64) float64 { return gf(n.name) }

type numAbs struct{ x numNode }

func (n numAbs) evalNum(gf func(string) float64) float64 { return math.Abs(n.x.evalNum(gf)) }

type numNeg struct{ x numNode }

func (n numNeg) evalNum(gf func(string) float64) float64 { return -n.x.evalNum(gf) }

type condParser struct {
	tokens []condTok
	pos    int
	params map[string]ParamType
	used   map[string]struct{}
}

func (p *condParser) peek() condTok {
	if p.pos >= len(p.tokens) {
		return condTok{kind: tokEOF}
	}
	return p.tokens[p.pos]
}

func (p *condParser) next() condTok {
	t := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return t
}

func (p *condParser) parseOr() (condNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokOr {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = condOrNode{left, right}
	}
	return left, nil
}

func (p *condParser) parseAnd() (condNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokAnd {
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = condAndNode{left, right}
	}
	return left, nil
}

func (p *condParser) parseUnary() (condNode, error) {
	if p.peek().kind == tokNot {
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return condNotNode{x}, nil
	}
	return p.parsePrimary()
}

func (p *condParser) parsePrimary() (condNode, error) {
	t := p.peek()
	switch t.kind {
	case tokLParen:
		// Could be a grouped boolean expression "(a || b)" or a parenthesized numeric comparison "(x) > 0".
		saved := p.pos
		p.next()
		if sub, err := p.parseOr(); err == nil && p.peek().kind == tokRParen {
			p.next()
			if p.peek().kind == tokEQ || p.peek().kind == tokNE {
				op := p.next().kind
				rhs, err := p.parseBoolAtom()
				if err != nil {
					return nil, err
				}
				return condBoolEq{a: sub, b: rhs, neg: op == tokNE}, nil
			}
			return sub, nil
		}
		p.pos = saved
		return p.parseNumericCmp()
	case tokBar, tokNumber, tokMinus:
		return p.parseNumericCmp()
	case tokIdent:
		if t.text == "abs" {
			return p.parseNumericCmp()
		}
		if t.text == "true" || t.text == "false" {
			lhs := condLitBool{v: t.text == "true"}
			p.next()
			if p.peek().kind == tokEQ || p.peek().kind == tokNE {
				op := p.next().kind
				rhs, err := p.parseBoolAtom()
				if err != nil {
					return nil, err
				}
				return condBoolEq{a: lhs, b: rhs, neg: op == tokNE}, nil
			}
			return lhs, nil
		}
		pt, ok := p.params[t.text]
		if !ok {
			return nil, fmt.Errorf("unknown parameter %q", t.text)
		}
		if pt == ParamFloat {
			return p.parseNumericCmp()
		}
		p.used[t.text] = struct{}{}
		p.next()
		lhs := condNode(condParamBool{name: t.text})
		switch p.peek().kind {
		case tokEQ, tokNE:
			op := p.next().kind
			rhs, err := p.parseBoolAtom()
			if err != nil {
				return nil, err
			}
			return condBoolEq{a: lhs, b: rhs, neg: op == tokNE}, nil
		case tokLT, tokLE, tokGT, tokGE:
			return nil, fmt.Errorf("parameter %q is %s and cannot be compared with %q", t.text, pt, p.peek().text)
		}
		return lhs, nil
	default:
		return nil, fmt.Errorf("expected condition expression, got %q", t.text)
	}
}

func (p *condParser) parseBoolAtom() (condNode, error) {
	t := p.next()
	if t.kind != tokIdent {
		return nil, fmt.Errorf("expected boolean value, got %q", t.text)
	}
	if t.text == "true" {
		return condLitBool{v: true}, nil
	}
	if t.text == "false" {
		return condLitBool{v: false}, nil
	}
	pt, ok := p.params[t.text]
	if !ok {
		return nil, fmt.Errorf("unknown parameter %q", t.text)
	}
	if pt == ParamFloat {
		return nil, fmt.Errorf("float parameter %q cannot be compared to a boolean", t.text)
	}
	p.used[t.text] = struct{}{}
	return condParamBool{name: t.text}, nil
}

func (p *condParser) parseNumericCmp() (condNode, error) {
	lhs, err := p.parseNumExpr()
	if err != nil {
		return nil, err
	}
	op := p.peek()
	switch op.kind {
	case tokEQ, tokNE, tokLT, tokLE, tokGT, tokGE:
		p.next()
	default:
		return nil, fmt.Errorf("numeric expression requires a comparison operator (==, !=, <, <=, >, >=), got %q", op.text)
	}
	rhs, err := p.parseNumExpr()
	if err != nil {
		return nil, err
	}
	return condCmpNum{op: op.kind, a: lhs, b: rhs}, nil
}

func (p *condParser) parseNumExpr() (numNode, error) {
	t := p.peek()
	switch t.kind {
	case tokMinus:
		p.next()
		x, err := p.parseNumExpr()
		if err != nil {
			return nil, err
		}
		return numNeg{x}, nil
	case tokBar:
		p.next()
		x, err := p.parseNumExpr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokBar {
			return nil, fmt.Errorf("missing closing '|' in absolute value expression")
		}
		p.next()
		return numAbs{x}, nil
	case tokLParen:
		p.next()
		x, err := p.parseNumExpr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			return nil, fmt.Errorf("missing closing ')'")
		}
		p.next()
		return x, nil
	case tokNumber:
		p.next()
		return numLit{v: t.num}, nil
	case tokIdent:
		if t.text == "abs" {
			p.next()
			if p.peek().kind != tokLParen {
				return nil, fmt.Errorf("expected '(' after abs")
			}
			p.next()
			x, err := p.parseNumExpr()
			if err != nil {
				return nil, err
			}
			if p.peek().kind != tokRParen {
				return nil, fmt.Errorf("missing closing ')' after abs(...)")
			}
			p.next()
			return numAbs{x}, nil
		}
		pt, ok := p.params[t.text]
		if !ok {
			return nil, fmt.Errorf("unknown parameter %q", t.text)
		}
		if pt != ParamFloat {
			return nil, fmt.Errorf("parameter %q is %s, want float in numeric expression", t.text, pt)
		}
		p.used[t.text] = struct{}{}
		p.next()
		return numParam{name: t.text}, nil
	default:
		return nil, fmt.Errorf("expected numeric value, got %q", t.text)
	}
}
