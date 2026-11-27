package importsettings

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

func (c *Credit) validate() error {
	if err := checkLine("author", c.Author, true); err != nil {
		return err
	}
	if err := checkLine("license", c.License, true); err != nil {
		return err
	}
	if err := ValidateLicense(c.License); err != nil {
		return err
	}
	if err := checkLine("source", c.Source, false); err != nil {
		return err
	}
	if c.Notice != "" {
		if strings.TrimSpace(c.Notice) != c.Notice {
			return errors.New("notice must not start or end with spaces or blank lines")
		}
		if i := strings.IndexFunc(c.Notice, func(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) }); i >= 0 {
			return fmt.Errorf("notice holds the control character %q; only line breaks and tabs are allowed", c.Notice[i:i+1])
		}
	}
	return nil
}

// checkLine refuses a one-line field holding control characters, line breaks included, or surrounding spaces, and an empty one when required.
func checkLine(field, s string, required bool) error {
	switch {
	case s == "":
		if required {
			return fmt.Errorf("%s is required", field)
		}
	case strings.TrimSpace(s) != s:
		return fmt.Errorf("%s %q must not start or end with spaces", field, s)
	case strings.IndexFunc(s, unicode.IsControl) >= 0:
		return fmt.Errorf("%s %q must be one line without control characters", field, s)
	}
	return nil
}

// ValidateLicense checks that expr is an SPDX license expression (SPDX specification 2.3, annex D): license identifiers such as CC-BY-4.0, each optionally followed by "+" for "or any later version" and by WITH and an exception identifier, joined by AND and OR, which are upper case, and grouped by parentheses. A license with no SPDX identifier is written LicenseRef- followed by letters, digits, dots and hyphens, optionally after DocumentRef-name: as SPDX allows. NONE and NOASSERTION are refused, since a credit names the license an asset is used under. Identifiers are checked for their form, not looked up in the SPDX license list, so a new license needs no update of this package.
func ValidateLicense(expr string) error {
	toks, err := licenseTokens(expr)
	if err != nil {
		return err
	}
	if len(toks) == 0 {
		return errors.New("license is required")
	}
	p := &licenseParser{toks: toks, expr: expr}
	if err := p.or(); err != nil {
		return err
	}
	if p.pos < len(p.toks) {
		if tok := p.toks[p.pos]; isOperator(tok) {
			return fmt.Errorf("license %q: the operator %q must be written %s", expr, tok, strings.ToUpper(tok))
		}
		return fmt.Errorf("license %q: unexpected %q", expr, p.toks[p.pos])
	}
	return nil
}

// licenseTokens splits expr into parentheses and the words between them.
func licenseTokens(expr string) ([]string, error) {
	var toks []string
	word := -1
	for i, r := range expr {
		switch {
		case r == '(' || r == ')' || r == ' ':
			if word >= 0 {
				toks = append(toks, expr[word:i])
				word = -1
			}
			if r != ' ' {
				toks = append(toks, string(r))
			}
		case r == '.' || r == '-' || r == '+' || r == ':' || r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			if word < 0 {
				word = i
			}
		default:
			return nil, fmt.Errorf("license %q holds %q; an SPDX license expression has only letters, digits, dots, hyphens, \"+\", \":\", spaces and parentheses", expr, string(r))
		}
	}
	if word >= 0 {
		toks = append(toks, expr[word:])
	}
	return toks, nil
}

// licenseParser parses an SPDX license expression by recursive descent: or is and-expressions joined by OR, and is terms joined by AND, AND binding tighter, and a term is a parenthesized expression or a license with an optional exception.
type licenseParser struct {
	toks []string
	pos  int
	expr string
}

func (p *licenseParser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *licenseParser) or() error {
	if err := p.and(); err != nil {
		return err
	}
	for p.peek() == "OR" {
		p.pos++
		if err := p.and(); err != nil {
			return err
		}
	}
	return nil
}

func (p *licenseParser) and() error {
	if err := p.term(); err != nil {
		return err
	}
	for p.peek() == "AND" {
		p.pos++
		if err := p.term(); err != nil {
			return err
		}
	}
	return nil
}

func (p *licenseParser) term() error {
	tok := p.peek()
	switch tok {
	case "":
		return fmt.Errorf("license %q ends where a license identifier is expected", p.expr)
	case "(":
		p.pos++
		if err := p.or(); err != nil {
			return err
		}
		if p.peek() != ")" {
			return fmt.Errorf("license %q has a \"(\" without its \")\"", p.expr)
		}
		p.pos++
		return nil
	}
	p.pos++
	if err := checkLicenseID(p.expr, tok); err != nil {
		return err
	}
	if p.peek() != "WITH" {
		return nil
	}
	p.pos++
	exc := p.peek()
	if exc == "" || exc == "(" || exc == ")" || isOperator(exc) {
		return fmt.Errorf("license %q: WITH must be followed by an exception identifier", p.expr)
	}
	p.pos++
	if !isIDString(exc) {
		return fmt.Errorf("license %q: exception %q must be letters, digits, dots and hyphens", p.expr, exc)
	}
	return nil
}

// checkLicenseID checks one license identifier of the expression expr: an idstring with an optional "+", or a LicenseRef, optionally after a DocumentRef.
func checkLicenseID(expr, id string) error {
	if id == ")" {
		return fmt.Errorf("license %q has a \")\" without its \"(\"", expr)
	}
	if isOperator(id) {
		if id == strings.ToUpper(id) {
			return fmt.Errorf("license %q: %s must come between two licenses", expr, id)
		}
		return fmt.Errorf("license %q: the operator %q must be written %s", expr, id, strings.ToUpper(id))
	}
	switch id {
	case "NONE", "NOASSERTION":
		return fmt.Errorf("license %s records no license; name the license the asset is used under", id)
	}
	if doc, ref, ok := strings.Cut(id, ":"); ok {
		name, isDoc := strings.CutPrefix(doc, "DocumentRef-")
		if !isDoc || !isIDString(name) || !strings.HasPrefix(ref, "LicenseRef-") {
			return fmt.Errorf("license %q: %q must be DocumentRef-<name>:LicenseRef-<name>", expr, id)
		}
		id = ref
	}
	if name, ok := strings.CutPrefix(id, "LicenseRef-"); ok {
		if !isIDString(name) {
			return fmt.Errorf("license %q: %q must be LicenseRef- followed by letters, digits, dots and hyphens", expr, id)
		}
		return nil
	}
	if !isIDString(strings.TrimSuffix(id, "+")) {
		return fmt.Errorf("license %q: %q is not a license identifier such as CC-BY-4.0", expr, id)
	}
	return nil
}

// isOperator reports whether s is AND, OR or WITH in any letter case.
func isOperator(s string) bool {
	switch strings.ToUpper(s) {
	case "AND", "OR", "WITH":
		return true
	}
	return false
}

// isIDString reports whether s is a non-empty SPDX idstring: ASCII letters, digits, dots and hyphens.
func isIDString(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}
