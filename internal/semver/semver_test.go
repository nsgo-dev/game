package semver_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/internal/semver"
)

func TestParseAndString(t *testing.T) {
	valid := []struct {
		in   string
		want semver.Version
		str  string
	}{
		{"0.0.0", semver.Version{Major: 0, Minor: 0, Patch: 0}, "0.0.0"},
		{"1.2.3", semver.Version{Major: 1, Minor: 2, Patch: 3}, "1.2.3"},
		{"v0.4.0", semver.Version{Major: 0, Minor: 4, Patch: 0}, "0.4.0"},
		{"v1.2.3-beta.1", semver.Version{Major: 1, Minor: 2, Patch: 3, Pre: "beta.1"}, "1.2.3-beta.1"},
		{"1.2.3-0", semver.Version{Major: 1, Minor: 2, Patch: 3, Pre: "0"}, "1.2.3-0"},
		{"1.2.3-01a", semver.Version{Major: 1, Minor: 2, Patch: 3, Pre: "01a"}, "1.2.3-01a"},
		{"1.0.0-x.-1", semver.Version{Major: 1, Minor: 0, Patch: 0, Pre: "x.-1"}, "1.0.0-x.-1"},
		{"1.0.0--1", semver.Version{Major: 1, Minor: 0, Patch: 0, Pre: "-1"}, "1.0.0--1"},
		{"v0.0.0-20261120093000-0123456789ab", semver.Version{Major: 0, Minor: 0, Patch: 0, Pre: "20261120093000-0123456789ab"}, "0.0.0-20261120093000-0123456789ab"},
		{"v1.2.3-0.20261120093000-0123456789ab", semver.Version{Major: 1, Minor: 2, Patch: 3, Pre: "0.20261120093000-0123456789ab"}, "1.2.3-0.20261120093000-0123456789ab"},
		{"v1.2.3-pre.0.20261120093000-0123456789ab", semver.Version{Major: 1, Minor: 2, Patch: 3, Pre: "pre.0.20261120093000-0123456789ab"}, "1.2.3-pre.0.20261120093000-0123456789ab"},
		{"v2.0.0+incompatible", semver.Version{Major: 2, Minor: 0, Patch: 0}, "2.0.0"},
		{"1.2.3+20130313144700", semver.Version{Major: 1, Minor: 2, Patch: 3}, "1.2.3"},
		{"1.2.3-beta.1+exp.sha.5114f85", semver.Version{Major: 1, Minor: 2, Patch: 3, Pre: "beta.1"}, "1.2.3-beta.1"},
		{"1.2.3+build-with-hyphen.001", semver.Version{Major: 1, Minor: 2, Patch: 3}, "1.2.3"},
	}
	for _, tc := range valid {
		t.Run("valid/"+tc.in, func(t *testing.T) {
			got, err := semver.Parse(tc.in)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
			if s := got.String(); s != tc.str {
				t.Fatalf("String() = %q, want %q", s, tc.str)
			}
			back, err := semver.Parse(got.String())
			if err != nil || back != got {
				t.Fatalf("round trip Parse(%q) = %+v, %v; want %+v", got.String(), back, err, got)
			}
		})
	}

	invalid := []string{
		"",
		" ",
		"v",
		"vv1.2.3",
		"V1.2.3",
		"1",
		"1.2",
		"v1.2",
		"1.2.3.4",
		"01.2.3",
		"1.02.3",
		"1.2.03",
		"-1.2.3",
		"1.-2.3",
		"1.+2.3",
		" 1.2.3",
		"1.2.3 ",
		"1.2.3-",
		"1.2.3-.",
		"1.2.3-alpha..1",
		"1.0.0-a..b",
		"1.0.0-.a",
		"1.2.3-alpha.",
		"1.0.0-01",
		"1.0.0-x.01",
		"1.2.3-01",
		"1.2.3-a_b",
		"1.0.0-x.+1",
		"1.2.3+",
		"1.2.3+.",
		"1.2.3+a..b",
		"1.2.3+a b",
		"1.2.3+a+b",
		"1.2.3-alpha+",
		"latest",
		"999999999999999999999999999.0.0",
	}
	for _, in := range invalid {
		t.Run("invalid/"+in, func(t *testing.T) {
			if got, err := semver.Parse(in); err == nil {
				t.Fatalf("Parse(%q) = %+v, want error", in, got)
			}
		})
	}
}

func TestComparePrecedence(t *testing.T) {
	// SemVer 2.0.0 §11 ordering plus Go pseudo-versions and large numeric pre-release identifiers.
	ordered := []string{
		"0.0.0-20261120093000-0123456789ab",
		"v0.0.0-20261121093000-0123456789ab",
		"0.0.0",
		"0.4.0",
		"v0.4.1-0.20261120093000-0123456789ab",
		"0.4.1",
		"1.0.0-1",
		"1.0.0-2",
		"1.0.0-10",
		"1.0.0-99999999999999999999999",
		"1.0.0-100000000000000000000000",
		"1.0.0--1",
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0-x.0",
		"1.0.0-x.5",
		"1.0.0-x.-1",
		"1.0.0-x.-2",
		"1.0.0-x.a",
		"1.0.0",
		"1.1.0-beta",
		"1.1.0",
		"2.0.0-alpha",
		"2.0.0",
	}
	parsed := make([]semver.Version, len(ordered))
	for i, s := range ordered {
		v, err := semver.Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		parsed[i] = v
	}
	for i := range parsed {
		if c := parsed[i].Compare(parsed[i]); c != 0 {
			t.Errorf("Compare(%s, %s) = %d, want 0", parsed[i], parsed[i], c)
		}
		for j := i + 1; j < len(parsed); j++ {
			if c := parsed[i].Compare(parsed[j]); c != -1 {
				t.Errorf("Compare(%s, %s) = %d, want -1", parsed[i], parsed[j], c)
			}
			if c := parsed[j].Compare(parsed[i]); c != 1 {
				t.Errorf("Compare(%s, %s) = %d, want 1", parsed[j], parsed[i], c)
			}
		}
	}

	// Build metadata does not affect precedence.
	a, err := semver.Parse("1.2.3+build.1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := semver.Parse("v1.2.3+build.2")
	if err != nil {
		t.Fatal(err)
	}
	if a.Compare(b) != 0 {
		t.Fatalf("Compare(%q, %q) = %d, want 0", a, b, a.Compare(b))
	}
}

func TestConstraints(t *testing.T) {
	cases := []struct {
		constraint string
		version    string
		want       bool
	}{
		// Wildcard and empty.
		{"", "0.0.0", true},
		{"", "v0.0.0-20261120093000-0123456789ab", true},
		{"*", "1.2.3", true},
		{"*", "v0.0.0-20261120093000-0123456789ab", true},

		// Exact and bare versions.
		{"1.2.3", "1.2.3", true},
		{"1.2.3", "v1.2.3+build.1", true},
		{"1.2.3", "1.2.4", false},
		{"=v1.2.3", "1.2.3", true},
		{"1.2", "1.2.0", true},
		{"1.2", "1.2.9", true},
		{"1.2", "1.3.0", false},
		{"1.2", "1.1.9", false},
		{"=1.2", "1.2.4", true},
		{"=1.2", "1.3.0", false},
		{"1", "1.0.0", true},
		{"1", "1.9.9", true},
		{"1", "2.0.0", false},
		{"1", "0.9.9", false},

		// Comparison operators (full and partial).
		{">=1.2.0", "1.2.0", true},
		{">=1.2.0", "1.1.9", false},
		{">=1.2", "1.2.0", true},
		{">=1.2", "1.1.9", false},
		{">=1", "1.0.0", true},
		{">=1", "0.9.9", false},
		{">1.2.0", "1.2.0", false},
		{">1.2.0", "1.2.1", true},
		{">1.2", "1.2.9", false},
		{">1.2", "1.3.0", true},
		{">1", "1.9.9", false},
		{">1", "2.0.0", true},
		{"<=1.2.3", "1.2.3", true},
		{"<=1.2.3", "1.2.4", false},
		{"<=1.2", "1.2.9", true},
		{"<=1.2", "1.3.0", false},
		{"<=1", "1.9.9", true},
		{"<=1", "2.0.0", false},
		{"<1.2.3", "1.2.2", true},
		{"<1.2.3", "1.2.3", false},
		{"<1.2", "1.1.9", true},
		{"<1.2", "1.2.0", false},
		{"<1", "0.9.9", true},
		{"<1", "1.0.0", false},

		// Caret (^).
		{"^1.2.3", "1.2.3", true},
		{"^1.2.3", "1.9.9", true},
		{"^1.2.3", "1.2.2", false},
		{"^1.2.3", "2.0.0", false},
		{"^1.2", "1.2.0", true},
		{"^1.2", "1.9.0", true},
		{"^1.2", "1.1.9", false},
		{"^1.2", "2.0.0", false},
		{"^v1.2", "v1.4.3", true},
		{"^1", "1.0.0", true},
		{"^1", "1.9.9", true},
		{"^1", "2.0.0", false},
		{"^0.2.3", "0.2.3", true},
		{"^0.2.3", "0.2.9", true},
		{"^0.2.3", "0.3.0", false},
		{"^0.2", "0.2.0", true},
		{"^0.2", "0.2.9", true},
		{"^0.2", "0.3.0", false},
		{"^0.0.3", "0.0.3", true},
		{"^0.0.3", "0.0.4", false},
		{"^0.0", "0.0.0", true},
		{"^0.0", "0.0.9", true},
		{"^0.0", "0.1.0", false},
		{"^0", "0.0.0", true},
		{"^0", "0.9.9", true},
		{"^0", "1.0.0", false},

		// Tilde (~).
		{"~1.2.3", "1.2.3", true},
		{"~1.2.3", "1.2.9", true},
		{"~1.2.3", "1.3.0", false},
		{"~1.2.3", "1.2.2", false},
		{"~1.2", "1.2.0", true},
		{"~1.2", "1.2.9", true},
		{"~1.2", "1.3.0", false},
		{"~1", "1.0.0", true},
		{"~1", "1.9.9", true},
		{"~1", "2.0.0", false},
		{"~0.2", "0.2.5", true},
		{"~0.2", "0.3.0", false},
		{"~0", "0.5.0", true},
		{"~0", "1.0.0", false},

		// Conjunctions (space and comma).
		{">=1.0.0 <2.0.0", "1.5.0", true},
		{">=1.0.0 <2.0.0", "2.0.0", false},
		{">=1.0.0, <2.0.0", "1.5.0", true},
		{">=1.0.0,<2.0.0", "1.5.0", true},
		{">=v0.4.0, <v0.5.0", "v0.4.2", true},
		{">=1.0.0 * <2.0.0", "1.2.0", true},

		// Pre-release and Go pseudo-version semantics (pure Compare).
		{">=1.0.0", "1.1.0-beta", true},
		{">=1.0.0", "1.0.0-beta", false},
		{">=0.4.0 <0.5.0", "v0.4.1-0.20261120093000-0123456789ab", true},
		{">=0.4.0 <0.5.0", "v0.4.0-0.20261120093000-0123456789ab", false},
		{"^0.4.0", "v0.4.1-0.20261120093000-0123456789ab", true},
		{">=0.4.0 <0.5.0", "0.5.0-alpha.1", true},
		{">=0.4.0 <0.5.0-0", "0.5.0-alpha.1", false},
		{">=0.4.0 <0.5.0-0", "v0.4.9", true},
		{">=1.0.0-x.5", "1.0.0-x.-1", true},
		{">=1.0.0-x.-1", "1.0.0-x.5", false},
	}
	for _, tc := range cases {
		t.Run(tc.constraint+"_vs_"+tc.version, func(t *testing.T) {
			got, err := semver.Check(tc.version, tc.constraint)
			if err != nil {
				t.Fatalf("Check(%q, %q) unexpected error: %v", tc.version, tc.constraint, err)
			}
			if got != tc.want {
				t.Fatalf("Check(%q, %q) = %v, want %v", tc.version, tc.constraint, got, tc.want)
			}
		})
	}
}

func TestConstraintZeroValueAndString(t *testing.T) {
	var zero semver.Constraint
	if zero.String() != "*" {
		t.Fatalf("zero Constraint.String() = %q, want \"*\"", zero.String())
	}
	v, err := semver.Parse("v0.0.0-20261120093000-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	if !zero.Check(v) {
		t.Fatalf("zero Constraint.Check(%s) = false, want true", v)
	}
	c, err := semver.ParseConstraint("  ^1.2  ")
	if err != nil {
		t.Fatal(err)
	}
	if c.String() != "^1.2" {
		t.Fatalf("Constraint.String() = %q, want \"^1.2\"", c.String())
	}
}

func TestConstraintMaxIntBounds(t *testing.T) {
	maxStr := fmt.Sprintf("%d", math.MaxInt)
	maxVer := semver.Version{Major: math.MaxInt}
	oneMaxMinor := semver.Version{Major: 1, Minor: math.MaxInt, Patch: 5}
	two := semver.Version{Major: 2}

	cases := []struct {
		constraint string
		ver        semver.Version
		want       bool
	}{
		{"^" + maxStr + ".0.0", maxVer, true},
		{"<=1." + maxStr, oneMaxMinor, true},
		{"<=1." + maxStr, two, false},
		{">1." + maxStr, oneMaxMinor, false},
		{">1." + maxStr, two, true},
		{">" + maxStr, maxVer, false},
		{">" + maxStr + "." + maxStr, semver.Version{Major: math.MaxInt, Minor: math.MaxInt, Patch: math.MaxInt}, false},
		{"^0.0." + maxStr, semver.Version{Major: 0, Minor: 0, Patch: math.MaxInt}, true},
		{"^0.0." + maxStr, semver.Version{Major: 0, Minor: 1, Patch: 0}, false},
	}
	for _, tc := range cases {
		c, err := semver.ParseConstraint(tc.constraint)
		if err != nil {
			t.Fatalf("ParseConstraint(%q): %v", tc.constraint, err)
		}
		if got := c.Check(tc.ver); got != tc.want {
			t.Errorf("(%q).Check(%s) = %v, want %v", tc.constraint, tc.ver, got, tc.want)
		}
	}
}

func TestParseConstraintRejectsInvalid(t *testing.T) {
	bad := []string{
		"not-a-version",
		">>1",
		"==1.0.0",
		"!=1.0.0",
		"~=1.0.0",
		"^=1.0.0",
		">=",
		"^",
		"^v",
		">= 1.0.0",
		",",
		", ,",
		">=1.0.0,",
		", >=1.0.0",
		">=1.0.0,,<2.0.0",
		"^1.2-alpha",
		"^1-alpha",
		"^1.2+build",
		">=1.2.",
		"^.1.2",
		"^01.2",
		"^1.02",
		"^1.2.3.4",
		">=1.0.0-01",
		">=1.0.0-a..b",
		">=1.0.0 || <0.5.0",
	}
	for _, s := range bad {
		t.Run(s, func(t *testing.T) {
			if c, err := semver.ParseConstraint(s); err == nil {
				t.Fatalf("ParseConstraint(%q) = %+v, want error", s, c)
			}
		})
	}
	if _, err := semver.Check("not-a-version", ">=1.0.0"); err == nil {
		t.Fatal("Check accepted invalid version")
	}
}

func FuzzParseAndCompare(f *testing.F) {
	seeds := []string{
		"0.0.0",
		"1.2.3",
		"v0.4.0",
		"v0.0.0-20261120093000-0123456789ab",
		"v1.2.3-0.20261120093000-0123456789ab",
		"1.2.3-alpha.1+build.42",
		"v2.0.0+incompatible",
		"1.0.0-999999999999999999999999",
		"1.2",
		"01.2.3",
		"1.2.3-",
		"1.2.3+",
	}
	for i, s := range seeds {
		f.Add(s, seeds[(i+1)%len(seeds)])
	}
	f.Fuzz(func(t *testing.T, rawA, rawB string) {
		a, errA := semver.Parse(rawA)
		if errA == nil {
			s := a.String()
			if strings.HasPrefix(s, "v") || strings.Contains(s, "+") {
				t.Fatalf("Version.String() = %q must not have leading v or build metadata", s)
			}
			back, err := semver.Parse(s)
			if err != nil {
				t.Fatalf("Parse(%q) after Parse(%q) failed: %v", s, rawA, err)
			}
			if back != a {
				t.Fatalf("round trip Parse(%q) = %+v, want %+v", s, back, a)
			}
			withV, err := semver.Parse("v" + s)
			if err != nil || withV != a {
				t.Fatalf("Parse(%q) = %+v, %v; want %+v", "v"+s, withV, err, a)
			}
			if a.Compare(a) != 0 {
				t.Fatalf("Compare(%s, %s) != 0", a, a)
			}
		}
		b, errB := semver.Parse(rawB)
		if errA == nil && errB == nil {
			ab := a.Compare(b)
			ba := b.Compare(a)
			if ab != -ba {
				t.Fatalf("Compare antisymmetry violated: Compare(%s, %s) = %d, Compare(%s, %s) = %d", a, b, ab, b, a, ba)
			}
			if (ab == 0) != (a == b) {
				t.Fatalf("Compare(%s, %s) = %d inconsistent with equality (%v)", a, b, ab, a == b)
			}
		}
	})
}

func FuzzParseConstraint(f *testing.F) {
	f.Add("^1.2", "1.4.3")
	f.Add(">=0.4.0 <0.5.0", "v0.4.1-0.20261120093000-0123456789ab")
	f.Add("~0.2, >=0.2.1", "0.2.5")
	f.Add("*", "v2.0.0+incompatible")
	f.Add("<=1", "1.9.9")
	f.Fuzz(func(t *testing.T, rawConstraint, rawVersion string) {
		c, errC := semver.ParseConstraint(rawConstraint)
		v, errV := semver.Parse(rawVersion)
		if errC != nil || errV != nil {
			return
		}
		got := c.Check(v)
		got2, err := semver.Check(rawVersion, rawConstraint)
		if err != nil {
			t.Fatalf("Check(%q, %q) failed after Parse/ParseConstraint succeeded: %v", rawVersion, rawConstraint, err)
		}
		if got != got2 {
			t.Fatalf("Check(%q, %q) = %v, want %v", rawVersion, rawConstraint, got2, got)
		}
	})
}
