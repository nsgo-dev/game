package dialogue

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/flow"
)

type exprTokKind int

const (
	tokEOF exprTokKind = iota
	tokNumber
	tokString
	tokBool
	tokVar
	tokIdent
	tokLParen
	tokRParen
	tokComma
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokPercent
	tokEq
	tokNeq
	tokLt
	tokLte
	tokGt
	tokGte
	tokAnd
	tokOr
	tokNot
)

type exprToken struct {
	kind exprTokKind
	text string
	num  float64
	b    bool
	pos  int
}

type dialogueExpr interface {
	eval(env *exprEnv) (any, error)
}

type exprEnv struct {
	store   *flow.Vars
	vars    map[string]any
	visited map[string]int
}

func (e *exprEnv) lookup(name string) any {
	if e.store != nil {
		if v, ok := e.store.Get(name); ok {
			return normalizeDialogueValue(v)
		}
		return nil
	}
	if e.vars == nil {
		return nil
	}
	return e.vars[name]
}

type literalExpr struct {
	val any
}

func (l literalExpr) eval(*exprEnv) (any, error) { return l.val, nil }

type varExpr struct {
	name string
}

func (v varExpr) eval(env *exprEnv) (any, error) { return env.lookup(v.name), nil }

type identExpr struct {
	name string
}

func (id identExpr) eval(env *exprEnv) (any, error) { return env.lookup(id.name), nil }

type unaryExpr struct {
	op exprTokKind
	x  dialogueExpr
}

func (u unaryExpr) eval(env *exprEnv) (any, error) {
	v, err := u.x.eval(env)
	if err != nil {
		return nil, err
	}
	switch u.op {
	case tokNot:
		return !toDialogueBool(v), nil
	case tokMinus:
		n, err := toDialogueNumber(v)
		if err != nil {
			return nil, err
		}
		return -n, nil
	case tokPlus:
		return toDialogueNumber(v)
	default:
		return nil, fmt.Errorf("dialogue: unsupported unary operator %d", u.op)
	}
}

type binaryExpr struct {
	op          exprTokKind
	left, right dialogueExpr
}

func (b binaryExpr) eval(env *exprEnv) (any, error) {
	if b.op == tokAnd {
		lv, err := b.left.eval(env)
		if err != nil {
			return nil, err
		}
		if !toDialogueBool(lv) {
			return false, nil
		}
		rv, err := b.right.eval(env)
		if err != nil {
			return nil, err
		}
		return toDialogueBool(rv), nil
	}
	if b.op == tokOr {
		lv, err := b.left.eval(env)
		if err != nil {
			return nil, err
		}
		if toDialogueBool(lv) {
			return true, nil
		}
		rv, err := b.right.eval(env)
		if err != nil {
			return nil, err
		}
		return toDialogueBool(rv), nil
	}

	lv, err := b.left.eval(env)
	if err != nil {
		return nil, err
	}
	rv, err := b.right.eval(env)
	if err != nil {
		return nil, err
	}

	switch b.op {
	case tokEq:
		return dialogueValuesEqual(lv, rv), nil
	case tokNeq:
		return !dialogueValuesEqual(lv, rv), nil
	case tokLt, tokLte, tokGt, tokGte:
		if ls, lok := lv.(string); lok {
			if rs, rok := rv.(string); rok {
				switch b.op {
				case tokLt:
					return ls < rs, nil
				case tokLte:
					return ls <= rs, nil
				case tokGt:
					return ls > rs, nil
				case tokGte:
					return ls >= rs, nil
				}
			}
		}
		ln, err := toDialogueNumber(lv)
		if err != nil {
			return nil, err
		}
		rn, err := toDialogueNumber(rv)
		if err != nil {
			return nil, err
		}
		switch b.op {
		case tokLt:
			return ln < rn, nil
		case tokLte:
			return ln <= rn, nil
		case tokGt:
			return ln > rn, nil
		case tokGte:
			return ln >= rn, nil
		}
	case tokPlus:
		_, lStr := lv.(string)
		_, rStr := rv.(string)
		if lStr || rStr {
			return formatDialogueValue(lv) + formatDialogueValue(rv), nil
		}
		ln, err := toDialogueNumber(lv)
		if err != nil {
			return nil, err
		}
		rn, err := toDialogueNumber(rv)
		if err != nil {
			return nil, err
		}
		return ln + rn, nil
	case tokMinus:
		ln, err := toDialogueNumber(lv)
		if err != nil {
			return nil, err
		}
		rn, err := toDialogueNumber(rv)
		if err != nil {
			return nil, err
		}
		return ln - rn, nil
	case tokStar:
		ln, err := toDialogueNumber(lv)
		if err != nil {
			return nil, err
		}
		rn, err := toDialogueNumber(rv)
		if err != nil {
			return nil, err
		}
		return ln * rn, nil
	case tokSlash:
		ln, err := toDialogueNumber(lv)
		if err != nil {
			return nil, err
		}
		rn, err := toDialogueNumber(rv)
		if err != nil {
			return nil, err
		}
		if rn == 0 {
			return nil, fmt.Errorf("dialogue: division by zero")
		}
		return ln / rn, nil
	case tokPercent:
		ln, err := toDialogueNumber(lv)
		if err != nil {
			return nil, err
		}
		rn, err := toDialogueNumber(rv)
		if err != nil {
			return nil, err
		}
		if rn == 0 {
			return nil, fmt.Errorf("dialogue: modulo by zero")
		}
		return math.Mod(ln, rn), nil
	}
	return nil, fmt.Errorf("dialogue: unsupported binary operator %d", b.op)
}

type callExpr struct {
	name string
	args []dialogueExpr
}

func (c callExpr) eval(env *exprEnv) (any, error) {
	switch c.name {
	case "visited", "visited_count":
		if len(c.args) != 1 {
			return nil, fmt.Errorf("dialogue: %s expects 1 argument, got %d", c.name, len(c.args))
		}
		var target string
		if id, ok := c.args[0].(identExpr); ok {
			target = id.name
		} else {
			v, err := c.args[0].eval(env)
			if err != nil {
				return nil, err
			}
			target = formatDialogueValue(v)
		}
		count := 0
		if env.visited != nil {
			count = env.visited[target]
		}
		if c.name == "visited" {
			return count > 0, nil
		}
		return float64(count), nil
	default:
		return nil, fmt.Errorf("dialogue: unknown function %q", c.name)
	}
}

func lexExpr(src string) ([]exprToken, error) {
	var toks []exprToken
	i := 0
	n := len(src)
	for i < n {
		r, size := utf8.DecodeRuneInString(src[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		start := i
		switch {
		case r == '(':
			toks = append(toks, exprToken{kind: tokLParen, text: "(", pos: start})
			i++
		case r == ')':
			toks = append(toks, exprToken{kind: tokRParen, text: ")", pos: start})
			i++
		case r == ',':
			toks = append(toks, exprToken{kind: tokComma, text: ",", pos: start})
			i++
		case r == '+':
			toks = append(toks, exprToken{kind: tokPlus, text: "+", pos: start})
			i++
		case r == '-':
			toks = append(toks, exprToken{kind: tokMinus, text: "-", pos: start})
			i++
		case r == '*':
			toks = append(toks, exprToken{kind: tokStar, text: "*", pos: start})
			i++
		case r == '/':
			toks = append(toks, exprToken{kind: tokSlash, text: "/", pos: start})
			i++
		case r == '%':
			toks = append(toks, exprToken{kind: tokPercent, text: "%", pos: start})
			i++
		case r == '=' && i+1 < n && src[i+1] == '=':
			toks = append(toks, exprToken{kind: tokEq, text: "==", pos: start})
			i += 2
		case r == '!' && i+1 < n && src[i+1] == '=':
			toks = append(toks, exprToken{kind: tokNeq, text: "!=", pos: start})
			i += 2
		case r == '!':
			toks = append(toks, exprToken{kind: tokNot, text: "!", pos: start})
			i++
		case r == '<' && i+1 < n && src[i+1] == '=':
			toks = append(toks, exprToken{kind: tokLte, text: "<=", pos: start})
			i += 2
		case r == '<':
			toks = append(toks, exprToken{kind: tokLt, text: "<", pos: start})
			i++
		case r == '>' && i+1 < n && src[i+1] == '=':
			toks = append(toks, exprToken{kind: tokGte, text: ">=", pos: start})
			i += 2
		case r == '>':
			toks = append(toks, exprToken{kind: tokGt, text: ">", pos: start})
			i++
		case r == '&' && i+1 < n && src[i+1] == '&':
			toks = append(toks, exprToken{kind: tokAnd, text: "&&", pos: start})
			i += 2
		case r == '|' && i+1 < n && src[i+1] == '|':
			toks = append(toks, exprToken{kind: tokOr, text: "||", pos: start})
			i += 2
		case r == '"' || r == '\'':
			quote := byte(r)
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
					case '"', '\'', '\\':
						sb.WriteByte(next)
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
				return nil, fmt.Errorf("unterminated string literal at offset %d", start)
			}
			toks = append(toks, exprToken{kind: tokString, text: sb.String(), pos: start})
		case r == '$':
			i++
			nameStart := i
			for i < n {
				nr, nsize := utf8.DecodeRuneInString(src[i:])
				if i == nameStart {
					if !unicode.IsLetter(nr) && nr != '_' {
						break
					}
				} else if !unicode.IsLetter(nr) && !unicode.IsDigit(nr) && nr != '_' {
					break
				}
				i += nsize
			}
			if i == nameStart {
				return nil, fmt.Errorf("expected variable name after '$' at offset %d", start)
			}
			toks = append(toks, exprToken{kind: tokVar, text: src[nameStart:i], pos: start})
		case unicode.IsDigit(r) || (r == '.' && i+1 < n && src[i+1] >= '0' && src[i+1] <= '9'):
			hasDot := false
			for i < n {
				c := src[i]
				if c == '.' {
					if hasDot {
						break
					}
					hasDot = true
					i++
					continue
				}
				if c < '0' || c > '9' {
					break
				}
				i++
			}
			raw := src[start:i]
			val, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid number %q at offset %d: %w", raw, start, err)
			}
			toks = append(toks, exprToken{kind: tokNumber, text: raw, num: val, pos: start})
		case unicode.IsLetter(r) || r == '_':
			for i < n {
				nr, nsize := utf8.DecodeRuneInString(src[i:])
				if !unicode.IsLetter(nr) && !unicode.IsDigit(nr) && nr != '_' {
					break
				}
				i += nsize
			}
			word := src[start:i]
			switch word {
			case "true":
				toks = append(toks, exprToken{kind: tokBool, text: word, b: true, pos: start})
			case "false":
				toks = append(toks, exprToken{kind: tokBool, text: word, b: false, pos: start})
			case "and":
				toks = append(toks, exprToken{kind: tokAnd, text: word, pos: start})
			case "or":
				toks = append(toks, exprToken{kind: tokOr, text: word, pos: start})
			case "not":
				toks = append(toks, exprToken{kind: tokNot, text: word, pos: start})
			case "is":
				toks = append(toks, exprToken{kind: tokEq, text: word, pos: start})
			default:
				toks = append(toks, exprToken{kind: tokIdent, text: word, pos: start})
			}
		default:
			return nil, fmt.Errorf("unexpected character %q at offset %d", r, start)
		}
	}
	toks = append(toks, exprToken{kind: tokEOF, pos: n})
	return toks, nil
}

type exprParser struct {
	toks []exprToken
	pos  int
}

func parseDialogueExpr(src string) (dialogueExpr, error) {
	toks, err := lexExpr(src)
	if err != nil {
		return nil, err
	}
	p := &exprParser{toks: toks}
	e, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, fmt.Errorf("unexpected token %q in expression", p.peek().text)
	}
	return e, nil
}

func (p *exprParser) peek() exprToken {
	if p.pos >= len(p.toks) {
		return exprToken{kind: tokEOF}
	}
	return p.toks[p.pos]
}

func (p *exprParser) next() exprToken {
	t := p.peek()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}

func (p *exprParser) parseOr() (dialogueExpr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokOr {
		op := p.next().kind
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = binaryExpr{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseAnd() (dialogueExpr, error) {
	left, err := p.parseEquality()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokAnd {
		op := p.next().kind
		right, err := p.parseEquality()
		if err != nil {
			return nil, err
		}
		left = binaryExpr{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseEquality() (dialogueExpr, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokEq || p.peek().kind == tokNeq {
		op := p.next().kind
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = binaryExpr{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseComparison() (dialogueExpr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for {
		k := p.peek().kind
		if k != tokLt && k != tokLte && k != tokGt && k != tokGte {
			break
		}
		op := p.next().kind
		right, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		left = binaryExpr{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseAdditive() (dialogueExpr, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokPlus || p.peek().kind == tokMinus {
		op := p.next().kind
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = binaryExpr{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseMultiplicative() (dialogueExpr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokStar || p.peek().kind == tokSlash || p.peek().kind == tokPercent {
		op := p.next().kind
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = binaryExpr{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseUnary() (dialogueExpr, error) {
	k := p.peek().kind
	if k == tokNot || k == tokMinus || k == tokPlus {
		op := p.next().kind
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return unaryExpr{op: op, x: x}, nil
	}
	return p.parsePrimary()
}

func (p *exprParser) parsePrimary() (dialogueExpr, error) {
	t := p.next()
	switch t.kind {
	case tokNumber:
		return literalExpr{val: t.num}, nil
	case tokString:
		return literalExpr{val: t.text}, nil
	case tokBool:
		return literalExpr{val: t.b}, nil
	case tokVar:
		return varExpr{name: t.text}, nil
	case tokIdent:
		if p.peek().kind == tokLParen {
			p.next() // consume '('
			var args []dialogueExpr
			if p.peek().kind != tokRParen {
				for {
					arg, err := p.parseOr()
					if err != nil {
						return nil, err
					}
					args = append(args, arg)
					if p.peek().kind != tokComma {
						break
					}
					p.next()
				}
			}
			if p.peek().kind != tokRParen {
				return nil, fmt.Errorf("expected ')' after arguments to %q", t.text)
			}
			p.next()
			return callExpr{name: t.text, args: args}, nil
		}
		return identExpr{name: t.text}, nil
	case tokLParen:
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			return nil, fmt.Errorf("expected ')' to close '('")
		}
		p.next()
		return e, nil
	case tokEOF:
		return nil, fmt.Errorf("unexpected end of expression")
	default:
		return nil, fmt.Errorf("unexpected token %q in expression", t.text)
	}
}

func normalizeDialogueValue(val any) any {
	switch v := val.(type) {
	case nil:
		return nil
	case bool:
		return v
	case string:
		return v
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint8:
		return float64(v)
	case uint16:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	default:
		return v
	}
}

func toDialogueBool(val any) bool {
	switch v := normalizeDialogueValue(val).(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0 && !math.IsNaN(v)
	case string:
		return v != ""
	default:
		return true
	}
}

func toDialogueNumber(val any) (float64, error) {
	switch v := normalizeDialogueValue(val).(type) {
	case nil:
		return 0, nil
	case float64:
		return v, nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("dialogue: cannot convert %q to number", v)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("dialogue: unsupported numeric value %T", val)
	}
}

func dialogueValuesEqual(a, b any) bool {
	a = normalizeDialogueValue(a)
	b = normalizeDialogueValue(b)
	if a == nil && b == nil {
		return true
	}
	if a == nil {
		return dialogueIsZero(b)
	}
	if b == nil {
		return dialogueIsZero(a)
	}
	switch av := a.(type) {
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	default:
		return a == b
	}
}

func dialogueIsZero(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case bool:
		return !x
	case float64:
		return x == 0
	case string:
		return x == ""
	default:
		return false
	}
}

func formatDialogueValue(val any) string {
	switch v := normalizeDialogueValue(val).(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return strconv.FormatFloat(v, 'g', -1, 64)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// validateInterpolations verifies that every unescaped {expr} placeholder in raw parses cleanly.
func validateInterpolations(raw string) error {
	_, err := interpolateText(raw, nil)
	return err
}

// interpolateText evaluates unescaped {expr} placeholders in raw. When env is nil, expressions are parsed and validated without evaluating.
func interpolateText(raw string, env *exprEnv) (string, error) {
	var sb strings.Builder
	sb.Grow(len(raw))
	i := 0
	n := len(raw)
	for i < n {
		if raw[i] == '\\' && i+1 < n {
			next := raw[i+1]
			if next == '{' || next == '}' || next == '\\' {
				sb.WriteByte(next)
				i += 2
				continue
			}
		}
		if raw[i] != '{' {
			sb.WriteByte(raw[i])
			i++
			continue
		}
		// Find matching '}' respecting quoted strings inside the expression.
		j := i + 1
		inQuote := byte(0)
		found := false
		for j < n {
			c := raw[j]
			if inQuote != 0 {
				if c == '\\' && j+1 < n {
					j += 2
					continue
				}
				if c == inQuote {
					inQuote = 0
				}
				j++
				continue
			}
			if c == '"' || c == '\'' {
				inQuote = c
				j++
				continue
			}
			if c == '}' {
				found = true
				break
			}
			j++
		}
		if !found {
			return "", fmt.Errorf("unclosed '{' interpolation")
		}
		exprSrc := strings.TrimSpace(raw[i+1 : j])
		if exprSrc == "" {
			return "", fmt.Errorf("empty '{}' interpolation")
		}
		expr, err := parseDialogueExpr(exprSrc)
		if err != nil {
			return "", fmt.Errorf("invalid interpolation {%s}: %w", exprSrc, err)
		}
		if env != nil {
			val, err := expr.eval(env)
			if err != nil {
				return "", err
			}
			sb.WriteString(formatDialogueValue(val))
		}
		i = j + 1
	}
	return sb.String(), nil
}

// collectExprVars appends every variable name referenced by e to out.
// Bare identifiers inside visited(...) and visited_count(...) are node titles, not variables.
func collectExprVars(e dialogueExpr, out *[]string) {
	if e == nil {
		return
	}
	switch x := e.(type) {
	case varExpr:
		*out = append(*out, x.name)
	case identExpr:
		*out = append(*out, x.name)
	case unaryExpr:
		collectExprVars(x.x, out)
	case binaryExpr:
		collectExprVars(x.left, out)
		collectExprVars(x.right, out)
	case callExpr:
		for _, arg := range x.args {
			if (x.name == "visited" || x.name == "visited_count") && isIdentExpr(arg) {
				continue
			}
			collectExprVars(arg, out)
		}
	}
}

func isIdentExpr(e dialogueExpr) bool {
	_, ok := e.(identExpr)
	return ok
}

// collectInterpolationVars appends every variable name referenced inside unescaped {expr} placeholders in raw.
func collectInterpolationVars(raw string, out *[]string) {
	i := 0
	n := len(raw)
	for i < n {
		if raw[i] == '\\' && i+1 < n {
			next := raw[i+1]
			if next == '{' || next == '}' || next == '\\' {
				i += 2
				continue
			}
		}
		if raw[i] != '{' {
			i++
			continue
		}
		j := i + 1
		inQuote := byte(0)
		found := false
		for j < n {
			c := raw[j]
			if inQuote != 0 {
				if c == '\\' && j+1 < n {
					j += 2
					continue
				}
				if c == inQuote {
					inQuote = 0
				}
				j++
				continue
			}
			if c == '"' || c == '\'' {
				inQuote = c
				j++
				continue
			}
			if c == '}' {
				found = true
				break
			}
			j++
		}
		if !found {
			return
		}
		exprSrc := strings.TrimSpace(raw[i+1 : j])
		if expr, err := parseDialogueExpr(exprSrc); err == nil {
			collectExprVars(expr, out)
		}
		i = j + 1
	}
}
