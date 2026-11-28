package sceneformat

import (
	"fmt"
	"strconv"
	"strings"
)

// RuleContext supplies the values used by a connection's When expression.
// Expressions intentionally stay small and deterministic for scene files.
type RuleContext struct {
	Numbers map[string]float64
	Strings map[string]string
	Bools   map[string]bool
}

// EvaluateWhen evaluates an empty expression as true and supports name truth,
// name ==/!= value and numeric comparisons (<, <=, >, >=, ==, !=).
func EvaluateWhen(expression string, ctx RuleContext) (bool, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return true, nil
	}
	if expression == "true" {
		return true, nil
	}
	if expression == "false" {
		return false, nil
	}
	parts := strings.Fields(expression)
	if len(parts) != 3 {
		return false, fmt.Errorf("sceneformat: invalid when expression %q", expression)
	}
	name, op, raw := parts[0], parts[1], strings.Trim(parts[2], "\"")
	if n, ok := ctx.Numbers[name]; ok {
		want, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return false, fmt.Errorf("sceneformat: %q: %w", expression, err)
		}
		return compareNumber(n, want, op)
	}
	if s, ok := ctx.Strings[name]; ok {
		return compareString(s, raw, op)
	}
	if b, ok := ctx.Bools[name]; ok {
		want, err := strconv.ParseBool(raw)
		if err != nil {
			return false, err
		}
		if op == "==" {
			return b == want, nil
		}
		if op == "!=" {
			return b != want, nil
		}
	}
	return false, fmt.Errorf("sceneformat: unknown when value %q", name)
}
func compareNumber(a, b float64, op string) (bool, error) {
	switch op {
	case "<":
		return a < b, nil
	case "<=":
		return a <= b, nil
	case ">":
		return a > b, nil
	case ">=":
		return a >= b, nil
	case "==":
		return a == b, nil
	case "!=":
		return a != b, nil
	}
	return false, fmt.Errorf("sceneformat: invalid comparison %q", op)
}
func compareString(a, b, op string) (bool, error) {
	switch op {
	case "==":
		return a == b, nil
	case "!=":
		return a != b, nil
	}
	return false, fmt.Errorf("sceneformat: invalid string comparison %q", op)
}
