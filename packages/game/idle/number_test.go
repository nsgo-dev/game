package idle_test

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/idle"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"go.yaml.in/yaml/v3"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// near reports whether a and b agree to within rel of the larger magnitude.
func near(a, b idle.Number, rel float64) bool {
	if a.Cmp(b) == 0 {
		return true
	}
	diff := a.Sub(b).Abs()
	scale := a.Abs().Max(b.Abs())
	return diff.Cmp(scale.Scale(rel)) <= 0
}

func TestNewNormalizes(t *testing.T) {
	cases := []struct {
		in   float64
		m    float64
		e    int64
		text string
	}{
		{0, 0, 0, "0"},
		{1, 1, 0, "1"},
		{1500, 1.5, 3, "1500"},
		{-0.25, -2.5, -1, "-0.25"},
		{1e300, 1, 300, "1e300"},
		{5e-324, 4.9406564584124654, -324, ""},
		{math.MaxFloat64, 1.7976931348623157, 308, ""},
	}
	for _, c := range cases {
		n := idle.New(c.in)
		if n.Exponent() != c.e || math.Abs(n.Mantissa()-c.m) > 1e-14 {
			t.Errorf("New(%g) = %g×10^%d, want %g×10^%d", c.in, n.Mantissa(), n.Exponent(), c.m, c.e)
		}
		if got := n.String(); c.text != "" && got != c.text {
			t.Errorf("New(%g).String() = %q, want %q", c.in, got, c.text)
		}
	}
	if got := idle.NewExp(123, 4); got != idle.NewExp(1.23, 6) {
		t.Errorf("NewExp(123, 4) = %v, want 1.23e6", got)
	}
}

func TestNewPanicsOnNonFinite(t *testing.T) {
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("New(%g) did not panic", f)
				}
			}()
			idle.New(f)
		}()
	}
}

func TestArithmetic(t *testing.T) {
	if got := idle.New(1).Add(idle.New(2)); got != idle.New(3) {
		t.Errorf("1+2 = %v", got)
	}
	if got := idle.New(5).Sub(idle.New(5)); !got.IsZero() {
		t.Errorf("5-5 = %v", got)
	}
	if got := idle.New(5).Sub(idle.New(7)); got != idle.New(-2) {
		t.Errorf("5-7 = %v", got)
	}
	if got := idle.NewExp(1, 400).Mul(idle.NewExp(2.5, 400)); got != idle.NewExp(2.5, 800) {
		t.Errorf("1e400×2.5e400 = %v", got)
	}
	if got := idle.NewExp(1, 400).Div(idle.NewExp(4, 100)); got != idle.NewExp(2.5, 299) {
		t.Errorf("1e400/4e100 = %v", got)
	}
	if got := idle.NewExp(9.5, 1000).Add(idle.NewExp(5, 999)); got != idle.NewExp(1, 1001) {
		t.Errorf("9.5e1000+5e999 = %v", got)
	}
	big := idle.NewExp(1, 400)
	if got := big.Add(idle.NewExp(9.9, 383)); got != big {
		t.Errorf("an addend 17 orders smaller changed the sum: %v", got)
	}
	if got := big.Add(idle.NewExp(5, 384)); got == big {
		t.Errorf("an addend 16 orders smaller was dropped")
	}
	if got := idle.New(-3).Abs(); got != idle.New(3) {
		t.Errorf("|-3| = %v", got)
	}
	if got := idle.New(2).Scale(0.25); got != idle.New(0.5) {
		t.Errorf("2×0.25 = %v", got)
	}
	if got := (idle.Number{}).Neg(); got != (idle.Number{}) {
		t.Errorf("-0 = %#v", got)
	}
}

func TestPow(t *testing.T) {
	if got := idle.New(2).Pow(10); got != idle.New(1024) {
		t.Errorf("2^10 = %v", got)
	}
	if got := idle.New(7).Pow(0); got != idle.New(1) {
		t.Errorf("7^0 = %v", got)
	}
	if got := idle.New(10).Pow(-3); !near(got, idle.New(0.001), 1e-15) {
		t.Errorf("10^-3 = %v", got)
	}
	if got, want := idle.New(2).Pow(1000), idle.NewExp(1.0715086071862673, 301); !near(got, want, 1e-14) {
		t.Errorf("2^1000 = %v, want %v", got, want)
	}
	// A cost curve far past float64: 1.15^10000 = 10^(10000×log10 1.15) ≈ 1.24e607.
	if got := idle.New(1.15).Pow(10000); got.Exponent() != 606 && got.Exponent() != 607 {
		t.Errorf("1.15^10000 = %v", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("0^-1 did not panic")
		}
	}()
	idle.Number{}.Pow(-1)
}

func TestDivByZeroPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Div by zero did not panic")
		}
	}()
	idle.New(1).Div(idle.Number{})
}

func TestSaturation(t *testing.T) {
	top := idle.NewExp(9, idle.MaxExponent)
	got := top.Mul(idle.New(100))
	if got.Exponent() != idle.MaxExponent || got.Mantissa() >= 10 || got.Mantissa() < 9.99 {
		t.Errorf("overflow = %g×10^%d, want saturation", got.Mantissa(), got.Exponent())
	}
	if got := top.Neg().Mul(top); got.Sign() != -1 || got.Exponent() != idle.MaxExponent {
		t.Errorf("negative overflow = %v", got)
	}
	if got := idle.NewExp(1, -idle.MaxExponent).Div(idle.New(10)); !got.IsZero() {
		t.Errorf("underflow = %v, want 0", got)
	}
}

func TestCmp(t *testing.T) {
	ordered := []idle.Number{
		idle.NewExp(-1, 500), idle.New(-1000), idle.New(-1), idle.New(-0.5), {},
		idle.NewExp(1, -500), idle.New(0.5), idle.New(1), idle.New(9.99), idle.New(10), idle.NewExp(1, 500),
	}
	for i, a := range ordered {
		for j, b := range ordered {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := a.Cmp(b); got != want {
				t.Errorf("Cmp(%v, %v) = %d, want %d", a, b, got, want)
			}
		}
	}
	if got := idle.New(3).Min(idle.New(2)); got != idle.New(2) {
		t.Errorf("Min = %v", got)
	}
	if got := idle.New(3).Max(idle.New(2)); got != idle.New(3) {
		t.Errorf("Max = %v", got)
	}
}

func TestFloat64AndLog10(t *testing.T) {
	cases := []struct {
		n    idle.Number
		want float64
	}{
		{idle.Number{}, 0},
		{idle.NewExp(1.5, 3), 1500},
		{idle.NewExp(-2.5, -3), -0.0025},
		{idle.NewExp(1, 400), math.Inf(1)},
		{idle.NewExp(-1, 400), math.Inf(-1)},
		{idle.NewExp(1, -400), 0},
	}
	for _, c := range cases {
		if got := c.n.Float64(); got != c.want {
			t.Errorf("%v.Float64() = %g, want %g", c.n, got, c.want)
		}
	}
	if got := idle.NewExp(1, 1000).Log10(); got != 1000 {
		t.Errorf("Log10(1e1000) = %g", got)
	}
	if got := (idle.Number{}).Log10(); !math.IsInf(got, -1) {
		t.Errorf("Log10(0) = %g", got)
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		in, text string
	}{
		{"0", "0"},
		{"-0", "0"},
		{"000", "0"},
		{"0.000e999", "0"},
		{"42", "42"},
		{"+42", "42"},
		{"1500", "1500"},
		{"1.5e3", "1500"},
		{"0.25", "0.25"},
		{".5", "0.5"},
		{"5.", "5"},
		{"-0.5", "-0.5"},
		{"0.000001", "0.000001"},
		{"0.0000001", "1e-7"},
		{"1.23e45", "1.23e45"},
		{"1.23E+45", "1.23e45"},
		{"7E+400", "7e400"},
		{"-4e-9", "-4e-9"},
		{"1e21", "1e21"},
		{"99999999999999999999", "100000000000000000000"},
		{"123456789012345678901", "123456789012345680000"},
		{"0012.3400", "12.34"},
		{"1e1000000000000000000", "1e1000000000000000000"},
	}
	for _, c := range cases {
		n, err := idle.Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if got := n.String(); got != c.text {
			t.Errorf("Parse(%q).String() = %q, want %q", c.in, got, c.text)
		}
		back, err := idle.Parse(n.String())
		if err != nil || back != n {
			t.Errorf("Parse(%q) does not round trip: %v, %v", n.String(), back, err)
		}
	}

	bad := []struct {
		in   string
		want error
	}{
		{"", idle.ErrSyntax},
		{"abc", idle.ErrSyntax},
		{".", idle.ErrSyntax},
		{"+", idle.ErrSyntax},
		{"1e", idle.ErrSyntax},
		{"1e+", idle.ErrSyntax},
		{"1e1.5", idle.ErrSyntax},
		{"inf", idle.ErrSyntax},
		{"NaN", idle.ErrSyntax},
		{"0x10", idle.ErrSyntax},
		{"1_000", idle.ErrSyntax},
		{" 1", idle.ErrSyntax},
		{"1 ", idle.ErrSyntax},
		{"--1", idle.ErrSyntax},
		{"1.2.3", idle.ErrSyntax},
		{"1e99999999999999999999", idle.ErrRange},
		{"1e1000000000000000001", idle.ErrRange},
		{"10e1000000000000000000", idle.ErrRange},
		{"1e-1000000000000000001", idle.ErrRange},
		{"1e-99999999999999999999", idle.ErrRange},
		{"1e1000000000000000002", idle.ErrRange},
	}
	for _, c := range bad {
		if _, err := idle.Parse(c.in); !errors.Is(err, c.want) {
			t.Errorf("Parse(%q) error = %v, want %v", c.in, err, c.want)
		}
	}
}

// TestParseExponentAfterLead checks that Parse judges the range by the leading digit's power of ten, not the written exponent.
func TestParseExponentAfterLead(t *testing.T) {
	for _, c := range []struct {
		in   string
		want idle.Number
	}{
		{"0e9999999999999999999", idle.Number{}},
		{"-0.000e-99999999999999999999", idle.Number{}},
		{"0.001e1000000000000000002", idle.NewExp(1, idle.MaxExponent-1)},
		{"1000e-1000000000000000002", idle.NewExp(1, -idle.MaxExponent+1)},
	} {
		got, err := idle.Parse(c.in)
		if err != nil || got != c.want {
			t.Errorf("Parse(%q) = %v, %v, want %v", c.in, got, err, c.want)
		}
	}
}

// TestNewFloat64Precision pins how closely New and Float64 keep a float64: exactly inside the plain range, and within one unit in the last place beyond, where a decimal mantissa cannot hold every float64.
func TestNewFloat64Precision(t *testing.T) {
	ulps := func(a, b float64) uint64 {
		x, y := math.Float64bits(a), math.Float64bits(b)
		if x > y {
			return x - y
		}
		return y - x
	}
	// A fixed linear congruential sequence of bit patterns, so the test is deterministic.
	state := uint64(0x9e3779b97f4a7c15)
	for range 20000 {
		state = state*6364136223846793005 + 1442695040888963407
		f := math.Float64frombits(state >> 1)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		got := idle.New(f).Float64()
		a := math.Abs(f)
		if a == 0 || a >= 1e-300 && a < 1e300 {
			if got != f {
				t.Fatalf("New(%v).Float64() = %v, want it exactly", f, got)
			}
		} else if ulps(got, f) > 1 {
			t.Fatalf("New(%v).Float64() = %v, more than one ulp away", f, got)
		}
	}
}

func TestJSONNumbers(t *testing.T) {
	type wallet struct {
		Gold idle.Number `json:"gold"`
		Gems idle.Number `json:"gems"`
	}
	var w wallet
	if err := json.Unmarshal([]byte(`{"gold":1500,"gems":-2.5E+3}`), &w); err != nil {
		t.Fatal(err)
	}
	if w.Gold != idle.New(1500) || w.Gems != idle.New(-2500) {
		t.Errorf("JSON numbers = %+v", w)
	}
	w = wallet{Gold: idle.New(7)}
	if err := json.Unmarshal([]byte(`{"gold":null,"gems":"1.5e400"}`), &w); err != nil {
		t.Fatal(err)
	}
	if w.Gold != idle.New(7) || w.Gems != idle.NewExp(1.5, 400) {
		t.Errorf("JSON null and string = %+v", w)
	}
	for _, in := range []string{`{"gold":"lots"}`, `{"gold":true}`, `{"gold":[1]}`} {
		if err := json.Unmarshal([]byte(in), &w); err == nil {
			t.Errorf("json.Unmarshal(%s) accepted", in)
		}
	}
	if err := json.Unmarshal([]byte(`{"gold":"1e2000000000000000000"}`), &w); !errors.Is(err, idle.ErrRange) {
		t.Errorf("JSON out of range: %v", err)
	}
}

func TestTextEncodings(t *testing.T) {
	type wallet struct {
		Gold idle.Number `json:"gold" yaml:"gold"`
		Gems idle.Number `json:"gems" yaml:"gems"`
	}
	w := wallet{Gold: idle.NewExp(1.23, 45), Gems: idle.New(250)}

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"gold":"1.23e45","gems":"250"}`; string(data) != want {
		t.Errorf("JSON = %s, want %s", data, want)
	}
	var fromJSON wallet
	if err := json.Unmarshal(data, &fromJSON); err != nil || fromJSON != w {
		t.Errorf("JSON round trip = %+v, %v", fromJSON, err)
	}

	out, err := yaml.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	var fromYAML wallet
	if err := yaml.Unmarshal(out, &fromYAML); err != nil || fromYAML != w {
		t.Errorf("YAML round trip of %s = %+v, %v", out, fromYAML, err)
	}
	again, err := yaml.Marshal(fromYAML)
	if err != nil || string(again) != string(out) {
		t.Errorf("YAML is not stable: %s then %s", out, again)
	}

	// A person writes plain YAML numbers, including ones past float64's range.
	var authored wallet
	if err := yaml.Unmarshal([]byte("gold: 1.5e400\ngems: 12\n"), &authored); err != nil {
		t.Fatal(err)
	}
	if authored.Gold != idle.NewExp(1.5, 400) || authored.Gems != idle.New(12) {
		t.Errorf("authored YAML = %+v", authored)
	}
	if err := yaml.Unmarshal([]byte("gold: lots\n"), &authored); !errors.Is(err, idle.ErrSyntax) {
		t.Errorf("YAML with a bad number: %v", err)
	}
}

// TestDeterministicSequence pins the result of a long chain of operations, so a change in rounding or a platform that fuses multiply-add shows up.
func TestDeterministicSequence(t *testing.T) {
	total := idle.Number{}
	rate := idle.New(1.07)
	for i := range 12000 {
		total = total.Add(rate.Pow(int64(i)).Scale(0.3)).Sub(idle.New(float64(i)))
	}
	again := idle.Number{}
	for i := range 12000 {
		again = again.Add(rate.Pow(int64(i)).Scale(0.3)).Sub(idle.New(float64(i)))
	}
	if total != again {
		t.Fatalf("the same operations differ: %v then %v", total, again)
	}
	// Σ 0.3×1.07^i for i < 12000 = 0.3 × (1.07^12000 − 1) / 0.07, which crosses from plain float64 into the extended range; the subtracted integers are far below its precision.
	want := idle.New(1.07).Pow(12000).Sub(idle.New(1)).Scale(0.3).Div(idle.New(0.07))
	if !near(total, want, 1e-12) {
		t.Errorf("sum = %v, want about %v", total, want)
	}
	if got := total.String(); got != wantSequence {
		t.Errorf("sum = %s, want the pinned %s", got, wantSequence)
	}
}

func BenchmarkAdd(b *testing.B) {
	x, y := idle.NewExp(1.5, 300), idle.NewExp(2.25, 295)
	for b.Loop() {
		x = x.Add(y)
	}
	_ = x
}

func BenchmarkMul(b *testing.B) {
	x, y := idle.NewExp(1.5, 300), idle.New(1.0000001)
	for b.Loop() {
		x = x.Mul(y)
	}
	_ = x
}

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := idle.Parse("1.2345678e450"); err != nil {
			b.Fatal(err)
		}
	}
}

// wantSequence is the canonical text of TestDeterministicSequence's sum.
const wantSequence = "1.7272509380691996e353"

func FuzzParse(f *testing.F) {
	for _, s := range []string{"0", "-0.5", "1.23e45", "7E+400", "9.9999999999999999999e299", "1e-300", "99999999999999999999", ".5", "1e", "inf", "1e1000000000000000000"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := idle.Parse(s)
		if err != nil {
			return
		}
		text := n.String()
		back, err := idle.Parse(text)
		if err != nil {
			t.Fatalf("Parse(%q) = %v, whose text %q does not parse: %v", s, n, text, err)
		}
		if back != n || back.String() != text {
			t.Fatalf("Parse(%q) = %v does not round trip through %q: %v", s, n, text, back)
		}
		if n.Sign() != 0 && (n.Abs().Mantissa() < 1 || n.Abs().Mantissa() >= 10) {
			t.Fatalf("Parse(%q) mantissa %g is not normalized", s, n.Mantissa())
		}
	})
}
