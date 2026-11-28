package sceneformat

import (
	"fmt"
	"strconv"
	"strings"
)

// RuleContext supplies the values a connection's When expression reads (ConnectionDoc.When), by name. A game hands it to Signals.SetRuleContext and keeps changing the maps it holds: Signals reads them each time an event fires, so a condition sees the values as they are then.
type RuleContext struct {
	// Numbers are the values a numeric comparison such as "score >= 10" reads.
	Numbers map[string]float64
	// Strings are the values a string comparison such as "mode == combat" reads.
	Strings map[string]string
	// Bools are the values a bare name such as "has_key" or a comparison such as "has_key == true" reads.
	Bools map[string]bool
}

// whenExpr is a parsed When expression: a literal, a bare name, or a name, an operator and a value.
type whenExpr struct {
	literal, isLiteral bool
	name, op, value    string
}

// parseWhen parses a When expression: "true", "false", a bare name, or `name op value` with op one of <, <=, >, >=, == and !=, separated by spaces. The value may be written in double quotes but holds no space. An empty expression parses to nil, a connection that always runs.
func parseWhen(expression string) (*whenExpr, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, nil
	}
	parts := strings.Fields(expression)
	switch len(parts) {
	case 1:
		switch parts[0] {
		case "true":
			return &whenExpr{isLiteral: true, literal: true}, nil
		case "false":
			return &whenExpr{isLiteral: true}, nil
		}
		return &whenExpr{name: parts[0]}, nil
	case 3:
		switch parts[1] {
		case "<", "<=", ">", ">=", "==", "!=":
		default:
			return nil, fmt.Errorf("sceneformat: when expression %q: unknown operator %q", expression, parts[1])
		}
		return &whenExpr{name: parts[0], op: parts[1], value: strings.Trim(parts[2], `"`)}, nil
	}
	return nil, fmt.Errorf("sceneformat: when expression %q must be true, false, a name, or name, operator and value separated by spaces", expression)
}

// eval evaluates e against ctx, reporting an error when ctx holds no value of e's name or the value does not fit the comparison.
func (e *whenExpr) eval(ctx RuleContext) (bool, error) {
	if e.isLiteral {
		return e.literal, nil
	}
	if e.op == "" {
		b, ok := ctx.Bools[e.name]
		if !ok {
			return false, fmt.Errorf("sceneformat: when %q: the rule context has no bool %q", e.name, e.name)
		}
		return b, nil
	}
	if n, ok := ctx.Numbers[e.name]; ok {
		want, err := strconv.ParseFloat(e.value, 64)
		if err != nil {
			return false, fmt.Errorf("sceneformat: when %q %s %q: %q is a number, and %q is not", e.name, e.op, e.value, e.name, e.value)
		}
		return compareNumber(n, want, e.op), nil
	}
	if s, ok := ctx.Strings[e.name]; ok {
		switch e.op {
		case "==":
			return s == e.value, nil
		case "!=":
			return s != e.value, nil
		}
		return false, fmt.Errorf("sceneformat: when %q %s %q: a string compares only with == and !=", e.name, e.op, e.value)
	}
	if b, ok := ctx.Bools[e.name]; ok {
		want, err := strconv.ParseBool(e.value)
		if err != nil {
			return false, fmt.Errorf("sceneformat: when %q %s %q: %q is a bool, and %q is not", e.name, e.op, e.value, e.name, e.value)
		}
		switch e.op {
		case "==":
			return b == want, nil
		case "!=":
			return b != want, nil
		}
		return false, fmt.Errorf("sceneformat: when %q %s %q: a bool compares only with == and !=", e.name, e.op, e.value)
	}
	return false, fmt.Errorf("sceneformat: when %q %s %q: the rule context has no value %q", e.name, e.op, e.value, e.name)
}

// EvaluateWhen evaluates a connection's When expression against ctx: an empty expression, or "true", is true and "false" is false; a bare name reads ctx.Bools; `name op value` compares the value of name in ctx.Numbers, ctx.Strings or ctx.Bools, looked up in that order, with value, where a number takes <, <=, >, >=, == and !=, and a string or bool takes == and !=. It returns an error for an expression it cannot parse, a name ctx holds no value of, and a value that does not fit the comparison.
func EvaluateWhen(expression string, ctx RuleContext) (bool, error) {
	e, err := parseWhen(expression)
	if err != nil || e == nil {
		return err == nil, err
	}
	return e.eval(ctx)
}

func compareNumber(a, b float64, op string) bool {
	switch op {
	case "<":
		return a < b
	case "<=":
		return a <= b
	case ">":
		return a > b
	case ">=":
		return a >= b
	case "==":
		return a == b
	default: // "!=", the only operator parseWhen leaves
		return a != b
	}
}
