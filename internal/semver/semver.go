// Package semver parses and compares Semantic Versioning 2.0.0 versions and evaluates version constraints shared by mods, library packs and plugins.
//
// Versions follow MAJOR.MINOR.PATCH[-prerelease][+build] with an optional leading "v" (such as Go module versions and pseudo-versions). Build metadata is validated on parse and ignored in precedence and formatting; [Version.String] omits both "v" and build metadata.
//
// Constraints are space- or comma-separated conjunctions of clauses using *, =, >=, >, <=, <, ^, and ~, with optional partial versions (MAJOR or MAJOR.MINOR) and optional leading "v". Every comparison uses pure SemVer 2.0.0 precedence via [Version.Compare]: pre-releases (including Go pseudo-versions) satisfy any range whose bounds bracket them (for example, >=1.0.0 matches 1.1.0-beta and v1.0.1-0.20261120093000-0123456789ab, and <2.0.0 matches 2.0.0-alpha unless written <2.0.0-0).
package semver

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Version is a parsed semantic version (MAJOR.MINOR.PATCH[-prerelease]).
type Version struct {
	Major int
	Minor int
	Patch int
	Pre   string
}

// Parse parses a SemVer 2.0.0 version string with an optional leading "v", such as "1.2.3", "v0.4.0", "0.4.0-beta.1", "v0.0.0-20261120093000-0123456789ab", or "v2.0.0+incompatible". Build metadata is validated and discarded.
func Parse(s string) (Version, error) {
	v, _, err := parseVersion(s, false)
	return v, err
}

// String formats v as MAJOR.MINOR.PATCH[-prerelease] without a leading "v".
func (v Version) String() string {
	base := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		return base + "-" + v.Pre
	}
	return base
}

// Compare compares v and o according to SemVer 2.0.0 precedence. It returns -1 if v < o, 0 if v == o, and +1 if v > o.
func (v Version) Compare(o Version) int {
	if c := cmp.Compare(v.Major, o.Major); c != 0 {
		return c
	}
	if c := cmp.Compare(v.Minor, o.Minor); c != 0 {
		return c
	}
	if c := cmp.Compare(v.Patch, o.Patch); c != 0 {
		return c
	}
	if v.Pre == o.Pre {
		return 0
	}
	if v.Pre == "" {
		return 1
	}
	if o.Pre == "" {
		return -1
	}
	a := strings.Split(v.Pre, ".")
	b := strings.Split(o.Pre, ".")
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		aNum := isAllDigits(a[i])
		bNum := isAllDigits(b[i])
		switch {
		case aNum && bNum:
			if c := compareNumericStrings(a[i], b[i]); c != 0 {
				return c
			}
		case aNum:
			return -1
		case bNum:
			return 1
		default:
			return cmp.Compare(a[i], b[i])
		}
	}
	return cmp.Compare(len(a), len(b))
}

// Constraint is a parsed version constraint composed of zero or more AND-ed clauses. The zero value matches every version.
type Constraint struct {
	raw     string
	clauses []clause
}

type opKind uint8

const (
	opEq opKind = iota
	opGE
	opGT
	opLE
	opLT
	opNever
)

type clause struct {
	op  opKind
	ver Version
}

// ParseConstraint parses a version constraint. Supported operators are *, =, >=, >, <=, <, ^, and ~ (or a bare version, equivalent to =), joined by spaces or commas. Clauses may use partial versions such as ^1.2 (>=1.2.0 <2.0.0), ~1.2 (>=1.2.0 <1.3.0), or >=1.2 (>=1.2.0), and an optional leading "v". An empty string or "*" matches every version.
func ParseConstraint(s string) (Constraint, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || trimmed == "*" {
		return Constraint{raw: trimmed}, nil
	}
	var clauses []clause
	for group := range strings.SplitSeq(trimmed, ",") {
		group = strings.TrimSpace(group)
		if group == "" {
			return Constraint{}, fmt.Errorf("invalid constraint %q: empty clause", s)
		}
		for _, tok := range strings.Fields(group) {
			if tok == "*" {
				continue
			}
			op := "="
			rest := tok
			for _, prefix := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
				if strings.HasPrefix(tok, prefix) {
					op = prefix
					rest = strings.TrimPrefix(tok, prefix)
					break
				}
			}
			ver, parts, err := parseVersion(rest, true)
			if err != nil {
				return Constraint{}, fmt.Errorf("invalid constraint clause %q: %w", tok, err)
			}
			clauses = appendClause(clauses, op, ver, parts)
		}
	}
	return Constraint{raw: trimmed, clauses: clauses}, nil
}

// Check reports whether v satisfies every clause of c using SemVer 2.0.0 precedence.
func (c Constraint) Check(v Version) bool {
	for _, cl := range c.clauses {
		if !cl.matches(v) {
			return false
		}
	}
	return true
}

// String returns the trimmed constraint text, or "*" for an unconstrained zero value.
func (c Constraint) String() string {
	if c.raw == "" {
		return "*"
	}
	return c.raw
}

// Check parses version and constraint and reports whether version satisfies constraint.
func Check(version, constraint string) (bool, error) {
	v, err := Parse(version)
	if err != nil {
		return false, err
	}
	c, err := ParseConstraint(constraint)
	if err != nil {
		return false, err
	}
	return c.Check(v), nil
}

func (c clause) matches(v Version) bool {
	switch c.op {
	case opEq:
		return v.Compare(c.ver) == 0
	case opGE:
		return v.Compare(c.ver) >= 0
	case opGT:
		return v.Compare(c.ver) > 0
	case opLE:
		return v.Compare(c.ver) <= 0
	case opLT:
		return v.Compare(c.ver) < 0
	case opNever:
		return false
	default:
		return false
	}
}

func appendClause(dst []clause, op string, ver Version, parts int) []clause {
	switch op {
	case "=":
		switch parts {
		case 3:
			return append(dst, clause{op: opEq, ver: ver})
		case 2:
			dst = append(dst, clause{op: opGE, ver: ver})
			if upper, ok := nextMinor(ver.Major, ver.Minor); ok {
				dst = append(dst, clause{op: opLT, ver: upper})
			}
			return dst
		default:
			dst = append(dst, clause{op: opGE, ver: ver})
			if upper, ok := nextMajor(ver.Major); ok {
				dst = append(dst, clause{op: opLT, ver: upper})
			}
			return dst
		}
	case ">=":
		return append(dst, clause{op: opGE, ver: ver})
	case "<":
		return append(dst, clause{op: opLT, ver: ver})
	case "<=":
		switch parts {
		case 3:
			return append(dst, clause{op: opLE, ver: ver})
		case 2:
			if upper, ok := nextMinor(ver.Major, ver.Minor); ok {
				return append(dst, clause{op: opLT, ver: upper})
			}
			return dst
		default:
			if upper, ok := nextMajor(ver.Major); ok {
				return append(dst, clause{op: opLT, ver: upper})
			}
			return dst
		}
	case ">":
		switch parts {
		case 3:
			return append(dst, clause{op: opGT, ver: ver})
		case 2:
			if lower, ok := nextMinor(ver.Major, ver.Minor); ok {
				return append(dst, clause{op: opGE, ver: lower})
			}
			return append(dst, clause{op: opNever})
		default:
			if lower, ok := nextMajor(ver.Major); ok {
				return append(dst, clause{op: opGE, ver: lower})
			}
			return append(dst, clause{op: opNever})
		}
	case "^":
		dst = append(dst, clause{op: opGE, ver: ver})
		var (
			upper Version
			ok    bool
		)
		switch {
		case ver.Major > 0 || parts == 1:
			upper, ok = nextMajor(ver.Major)
		case ver.Minor > 0 || parts == 2:
			upper, ok = nextMinor(0, ver.Minor)
		default:
			upper, ok = nextPatch(0, 0, ver.Patch)
		}
		if ok {
			dst = append(dst, clause{op: opLT, ver: upper})
		}
		return dst
	case "~":
		dst = append(dst, clause{op: opGE, ver: ver})
		var (
			upper Version
			ok    bool
		)
		if parts == 1 {
			upper, ok = nextMajor(ver.Major)
		} else {
			upper, ok = nextMinor(ver.Major, ver.Minor)
		}
		if ok {
			dst = append(dst, clause{op: opLT, ver: upper})
		}
		return dst
	default:
		return append(dst, clause{op: opNever})
	}
}

func nextMajor(major int) (Version, bool) {
	if major == math.MaxInt {
		return Version{}, false
	}
	return Version{Major: major + 1}, true
}

func nextMinor(major, minor int) (Version, bool) {
	if minor == math.MaxInt {
		return nextMajor(major)
	}
	return Version{Major: major, Minor: minor + 1}, true
}

func nextPatch(major, minor, patch int) (Version, bool) {
	if patch == math.MaxInt {
		return nextMinor(major, minor)
	}
	return Version{Major: major, Minor: minor, Patch: patch + 1}, true
}

func parseVersion(raw string, allowPartial bool) (Version, int, error) {
	if raw == "" {
		return Version{}, 0, errors.New("empty version")
	}
	s := strings.TrimPrefix(raw, "v")
	if s == "" {
		return Version{}, 0, fmt.Errorf("empty version in %q", raw)
	}
	hasMeta := false
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		hasMeta = true
		if err := validateBuild(s[idx+1:]); err != nil {
			return Version{}, 0, fmt.Errorf("invalid build metadata in %q: %w", raw, err)
		}
		s = s[:idx]
	}
	var pre string
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		hasMeta = true
		pre = s[idx+1:]
		if err := validatePrerelease(pre); err != nil {
			return Version{}, 0, fmt.Errorf("invalid prerelease in %q: %w", raw, err)
		}
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	if allowPartial {
		if len(parts) < 1 || len(parts) > 3 {
			return Version{}, 0, fmt.Errorf("expected MAJOR[.MINOR[.PATCH]], got %q", raw)
		}
		if hasMeta && len(parts) != 3 {
			return Version{}, 0, fmt.Errorf("prerelease or build metadata requires MAJOR.MINOR.PATCH in %q", raw)
		}
	} else if len(parts) != 3 {
		return Version{}, 0, fmt.Errorf("expected MAJOR.MINOR.PATCH, got %q", raw)
	}
	var nums [3]int
	for i, p := range parts {
		n, err := parseUintSegment(p)
		if err != nil {
			return Version{}, 0, fmt.Errorf("invalid numeric segment %q in %q", p, raw)
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2], Pre: pre}, len(parts), nil
}

func parseUintSegment(p string) (int, error) {
	if p == "" || (len(p) > 1 && p[0] == '0') {
		return 0, errors.New("invalid numeric segment")
	}
	for i := range len(p) {
		if p[i] < '0' || p[i] > '9' {
			return 0, errors.New("invalid numeric segment")
		}
	}
	return strconv.Atoi(p)
}

func validatePrerelease(pre string) error {
	if pre == "" {
		return errors.New("empty prerelease")
	}
	for id := range strings.SplitSeq(pre, ".") {
		if id == "" {
			return errors.New("empty prerelease identifier")
		}
		allDigits := true
		for i := range len(id) {
			c := id[i]
			if c >= '0' && c <= '9' {
				continue
			}
			allDigits = false
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-' {
				continue
			}
			return fmt.Errorf("invalid character %q in prerelease", c)
		}
		if allDigits && len(id) > 1 && id[0] == '0' {
			return fmt.Errorf("leading zero in numeric prerelease identifier %q", id)
		}
	}
	return nil
}

func validateBuild(build string) error {
	if build == "" {
		return errors.New("empty build metadata")
	}
	for id := range strings.SplitSeq(build, ".") {
		if id == "" {
			return errors.New("empty build metadata identifier")
		}
		for i := range len(id) {
			c := id[i]
			if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-' {
				continue
			}
			return fmt.Errorf("invalid character %q in build metadata", c)
		}
	}
	return nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func compareNumericStrings(a, b string) int {
	a = trimLeadingZeros(a)
	b = trimLeadingZeros(b)
	if c := cmp.Compare(len(a), len(b)); c != 0 {
		return c
	}
	return cmp.Compare(a, b)
}

func trimLeadingZeros(s string) string {
	i := 0
	for i < len(s)-1 && s[i] == '0' {
		i++
	}
	return s[i:]
}
