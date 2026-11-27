package idle

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// MaxExponent is the largest power of ten a Number holds. Results beyond it saturate to the largest Number of that sign, and results whose magnitude falls below 10^-MaxExponent become zero.
const MaxExponent int64 = 1_000_000_000_000_000_000

// ErrSyntax reports text that is not a decimal number, such as an empty string, "inf", "NaN", a hexadecimal float or stray characters.
var ErrSyntax = errors.New("invalid number syntax")

// ErrRange reports text whose power of ten lies beyond ±MaxExponent.
var ErrRange = errors.New("number out of range")

// Number is a signed number that reaches far beyond float64: its value is Mantissa() × 10^Exponent() with an int64 exponent up to MaxExponent. The zero value is 0.
//
// A magnitude from 1e-300 up to 1e300 is held as a plain float64, so arithmetic there is exactly float64 arithmetic (2^10 is 1024, integers up to 2^53 are exact) and only values past that range pay for a separate exponent. Every value has one representation, so two equal Numbers compare equal with ==. A Number is a small value type; its methods never modify it and return a new Number.
type Number struct {
	// m is the value itself when e is 0, or a mantissa with 1 ≤ |m| < 10 when e is ≥ 300 or ≤ -301.
	m float64
	e int64
}

const (
	// plainMax and plainMin bound the magnitudes held as a plain float64.
	plainMax = 1e300
	plainMin = 1e-300
	// plainTop and plainBottom are the decimal exponents of the largest and smallest plain magnitudes.
	plainTop    = 299
	plainBottom = -300
)

// pow10tab holds the powers of ten that float64 represents exactly.
var pow10tab = [...]float64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10,
	1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22,
}

// largestMantissa is the mantissa of a saturated Number, the float64 just below 10.
var largestMantissa = math.Nextafter(10, 0)

func pow10(k int64) float64 {
	if k >= 0 && k < int64(len(pow10tab)) {
		return pow10tab[k]
	}
	if k > 400 {
		return math.Inf(1)
	}
	if k < -400 {
		return 0
	}
	return math.Pow10(int(k))
}

// isPlain reports whether v lies in the range held as a plain float64.
func isPlain(v float64) bool {
	a := math.Abs(v)
	return a == 0 || a >= plainMin && a < plainMax
}

// New returns the Number nearest f: f itself for magnitudes from 1e-300 up to 1e300, and within one unit in the last place of f beyond, where the decimal mantissa's precision does not line up with float64's. It panics if f is NaN or infinite.
func New(f float64) Number {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		panic("idle: New of a non-finite float64")
	}
	if isPlain(f) {
		return Number{m: f}
	}
	return canon(f, 0)
}

// NewExp returns the Number equal to mantissa × 10^exp; the mantissa need not be normalized. It panics if mantissa is NaN or infinite.
func NewExp(mantissa float64, exp int64) Number {
	if math.IsNaN(mantissa) || math.IsInf(mantissa, 0) {
		panic("idle: NewExp of a non-finite mantissa")
	}
	return canon(mantissa, clampExp(exp))
}

// clampExp bounds an exponent so sums of two exponents cannot overflow int64; canon applies the real limits.
func clampExp(e int64) int64 {
	const limit = 2 * MaxExponent
	if e > limit {
		return limit
	}
	if e < -limit {
		return -limit
	}
	return e
}

// split returns finite m × 10^e as a mantissa in [1, 10) and a power of ten. The scale is found with exact comparisons against powers of ten and applied with one division or multiplication, so the result is the same on every platform.
func split(m float64, e int64) (float64, int64) {
	if m == 0 {
		return 0, 0
	}
	a := math.Abs(m)
	if a < 1e-290 {
		m *= 1e300
		a *= 1e300
		e -= 300
	}
	_, be := math.Frexp(a)
	// a lies in [2^(be-1), 2^be); 30103/100000 approximates log10(2), and the loops correct the estimate exactly.
	k := int64(be-1) * 30103 / 100000
	for a >= pow10(k+1) {
		k++
	}
	for a < pow10(k) {
		k--
	}
	switch {
	case k > 0:
		m /= pow10(k)
	case k < 0 && -k < int64(len(pow10tab)):
		m *= pow10tab[-k]
	case k < 0:
		m /= pow10(k)
	}
	if math.Abs(m) >= 10 {
		m /= 10
		k++
	} else if math.Abs(m) < 1 {
		m *= 10
		k--
	}
	return m, e + k
}

// canon returns the Number equal to finite m × 10^e in its one representation: a plain float64 inside the plain range, otherwise a normalized mantissa and exponent, saturated or flushed to zero past ±MaxExponent.
func canon(m float64, e int64) Number {
	if m == 0 {
		return Number{}
	}
	dm, de := split(m, e)
	if de > MaxExponent {
		return Number{m: math.Copysign(largestMantissa, dm), e: MaxExponent}
	}
	if de < -MaxExponent {
		return Number{}
	}
	if de >= plainBottom && de <= plainTop {
		// Reading the mantissa's shortest digits with the exponent rounds the decimal once, so NewExp(1.23, 45) is the float64 nearest 1.23e45, as Parse("1.23e45") is.
		v := decimalFloat(dm, de, -1)
		if isPlain(v) {
			return Number{m: v}
		}
		// Rounding carried v just across a boundary of the plain range.
		dm, de = split(v, 0)
	}
	return Number{m: dm, e: de}
}

// decimalFloat returns the float64 nearest m's digits × 10^e, for a mantissa with 1 ≤ |m| < 10, rounding the whole decimal once. prec is the number of digits after the point, as strconv.AppendFloat takes it: -1 reads the shortest digits that identify m, and exactDigits reads m's exact binary value. Past float64's range the result is ±Inf or a signed zero.
func decimalFloat(m float64, e int64, prec int) float64 {
	var buf [80]byte
	text := strconv.AppendFloat(buf[:0], m, 'e', prec, 64)
	text = text[:len(text)-len("e+00")]
	text = append(text, 'e')
	text = strconv.AppendInt(text, e, 10)
	v, _ := strconv.ParseFloat(string(text), 64)
	return v
}

// exactDigits is enough fraction digits to write any float64 in [1, 10) exactly, since its binary fraction has at most 52 bits.
const exactDigits = 52

// parts returns n's normalized mantissa and exponent.
func (n Number) parts() (float64, int64) {
	if n.e != 0 {
		return n.m, n.e
	}
	return split(n.m, 0)
}

// Mantissa returns n's decimal mantissa: 0 for zero, otherwise a value with 1 ≤ |m| < 10 carrying n's sign.
func (n Number) Mantissa() float64 {
	m, _ := n.parts()
	return m
}

// Exponent returns n's decimal exponent, the power of ten of its leading digit; it is 0 for zero.
func (n Number) Exponent() int64 {
	_, e := n.parts()
	return e
}

// IsZero reports whether n is 0.
func (n Number) IsZero() bool { return n.m == 0 }

// Sign returns -1, 0 or +1 as n is negative, zero or positive.
func (n Number) Sign() int {
	switch {
	case n.m > 0:
		return 1
	case n.m < 0:
		return -1
	}
	return 0
}

// Neg returns -n.
func (n Number) Neg() Number {
	if n.m == 0 {
		return Number{}
	}
	return Number{m: -n.m, e: n.e}
}

// Abs returns |n|.
func (n Number) Abs() Number { return Number{m: math.Abs(n.m), e: n.e} }

// Add returns n + o. Past the plain range, when the exponents differ by 17 or more the smaller operand lies below the larger's precision and the larger is returned unchanged.
func (n Number) Add(o Number) Number {
	if o.m == 0 {
		return n
	}
	if n.m == 0 {
		return o
	}
	if n.e == 0 && o.e == 0 {
		if r := n.m + o.m; isPlain(r) {
			return Number{m: r}
		}
	}
	am, ae := n.parts()
	bm, be := o.parts()
	if ae < be {
		n, am, ae, bm, be = o, bm, be, am, ae
	}
	d := ae - be
	if d >= 17 {
		return n
	}
	// A division by an exact power of ten keeps the shifted mantissa correctly rounded and leaves no product a fused multiply-add could absorb.
	return canon(am+bm/pow10tab[d], ae)
}

// Sub returns n - o.
func (n Number) Sub(o Number) Number { return n.Add(o.Neg()) }

// Mul returns n × o.
func (n Number) Mul(o Number) Number {
	if n.m == 0 || o.m == 0 {
		return Number{}
	}
	if n.e == 0 && o.e == 0 {
		if r := n.m * o.m; isPlain(r) {
			return Number{m: r}
		}
	}
	am, ae := n.parts()
	bm, be := o.parts()
	return canon(float64(am*bm), ae+be)
}

// Div returns n / o. It panics if o is zero.
func (n Number) Div(o Number) Number {
	if o.m == 0 {
		panic("idle: division by zero")
	}
	if n.m == 0 {
		return Number{}
	}
	if n.e == 0 && o.e == 0 {
		if r := n.m / o.m; isPlain(r) {
			return Number{m: r}
		}
	}
	am, ae := n.parts()
	bm, be := o.parts()
	return canon(am/bm, ae-be)
}

// Scale returns n × f. It panics if f is NaN or infinite.
func (n Number) Scale(f float64) Number { return n.Mul(New(f)) }

// Pow returns n raised to the integer power k by repeated squaring, so geometric costs such as base × growth^owned come out the same on every platform. Pow(0) is 1; a negative k returns 1 / n^-k and panics if n is zero.
func (n Number) Pow(k int64) Number {
	one := Number{m: 1}
	if k < 0 {
		if k == math.MinInt64 {
			return one.Div(n.Pow(math.MaxInt64).Mul(n))
		}
		return one.Div(n.Pow(-k))
	}
	result := one
	base := n
	for k > 0 {
		if k&1 == 1 {
			result = result.Mul(base)
		}
		k >>= 1
		if k > 0 {
			base = base.Mul(base)
		}
	}
	return result
}

// Cmp compares n and o and returns -1, 0 or +1 as n is less than, equal to or greater than o.
func (n Number) Cmp(o Number) int {
	sn, so := n.Sign(), o.Sign()
	if sn != so {
		if sn < so {
			return -1
		}
		return 1
	}
	if sn == 0 {
		return 0
	}
	if n.e == 0 && o.e == 0 {
		return cmpFloat(n.m, o.m)
	}
	nm, ne := n.parts()
	om, oe := o.parts()
	if ne != oe {
		// The plain and extended ranges do not overlap, so differing exponents decide.
		if (ne > oe) == (sn > 0) {
			return 1
		}
		return -1
	}
	return cmpFloat(nm, om)
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Min returns the smaller of n and o.
func (n Number) Min(o Number) Number {
	if o.Cmp(n) < 0 {
		return o
	}
	return n
}

// Max returns the larger of n and o.
func (n Number) Max(o Number) Number {
	if o.Cmp(n) > 0 {
		return o
	}
	return n
}

// Float64 returns the float64 nearest n: n itself for magnitudes from 1e-300 up to 1e300, ±Inf when |n| exceeds float64's range, and 0 or a subnormal when it is below the smallest normal float64.
func (n Number) Float64() float64 {
	switch {
	case n.e == 0:
		return n.m
	case n.e > 308:
		return math.Copysign(math.Inf(1), n.m)
	case n.e < -400:
		return math.Copysign(0, n.m)
	}
	return decimalFloat(n.m, n.e, exactDigits)
}

// Log10 returns log10(|n|), -Inf for zero. It is meant for display, such as progress bars over many orders of magnitude; its last bits may differ between platforms, so game state should not depend on it.
func (n Number) Log10() float64 {
	if n.m == 0 {
		return math.Inf(-1)
	}
	return math.Log10(math.Abs(n.m)) + float64(n.e)
}

// digits holds a Number's significant decimal digits and the power of ten of the first.
type digits struct {
	d  [maxDigits]byte
	nd int
	e  int64
}

// set fills d with the digits of |v| × 10^off that strconv writes for precision prec (-1 for the shortest that reads back exactly).
func (d *digits) set(v float64, off int64, prec int) {
	var tmp [32]byte
	b := strconv.AppendFloat(tmp[:0], math.Abs(v), 'e', prec, 64)
	d.nd = 0
	i := 0
	for ; i < len(b) && b[i] != 'e'; i++ {
		if b[i] != '.' && d.nd < maxDigits {
			d.d[d.nd] = b[i]
			d.nd++
		}
	}
	var exp int64
	neg := false
	for i++; i < len(b); i++ {
		switch c := b[i]; {
		case c == '-':
			neg = true
		case c >= '0' && c <= '9':
			exp = exp*10 + int64(c-'0')
		}
	}
	if neg {
		exp = -exp
	}
	d.e = off + exp
}

// shortest returns the fewest digits of |n| that read back as n.
func (n Number) shortest() digits {
	var d digits
	d.set(n.m, n.e, -1)
	return d
}

// String returns n's canonical text: positional for exponents from -6 to 20 ("1500", "0.25"), otherwise scientific with the shortest mantissa that reads back exactly ("1.23e45", "-4e-9"). Parse of the text returns n.
func (n Number) String() string {
	var buf [48]byte
	return string(n.AppendText(buf[:0]))
}

// AppendText appends n's canonical text, as String returns it, to dst.
func (n Number) AppendText(dst []byte) []byte {
	if n.m == 0 {
		return append(dst, '0')
	}
	if n.m < 0 {
		dst = append(dst, '-')
	}
	d := n.shortest()
	ds := d.d[:d.nd]
	if d.e >= -6 && d.e < 21 {
		intDigits := int(d.e) + 1
		if intDigits <= 0 {
			dst = append(dst, '0', '.')
			for i := intDigits; i < 0; i++ {
				dst = append(dst, '0')
			}
			return append(dst, ds...)
		}
		for i := range intDigits {
			if i < len(ds) {
				dst = append(dst, ds[i])
			} else {
				dst = append(dst, '0')
			}
		}
		if len(ds) > intDigits {
			dst = append(dst, '.')
			dst = append(dst, ds[intDigits:]...)
		}
		return dst
	}
	dst = append(dst, ds[0])
	if len(ds) > 1 {
		dst = append(dst, '.')
		dst = append(dst, ds[1:]...)
	}
	dst = append(dst, 'e')
	return strconv.AppendInt(dst, d.e, 10)
}

// MarshalText implements encoding.TextMarshaler with n's canonical text, so a Number is a string in JSON saves and YAML data.
func (n Number) MarshalText() ([]byte, error) { return n.AppendText(nil), nil }

// UnmarshalText implements encoding.TextUnmarshaler with Parse.
func (n *Number) UnmarshalText(text []byte) error {
	v, err := Parse(string(text))
	if err != nil {
		return err
	}
	*n = v
	return nil
}

// UnmarshalJSON implements json.Unmarshaler. It accepts the string MarshalText writes and also a JSON number, so a person can type 1500 for a Number field in a JSON data file; null leaves n unchanged, as encoding/json does for other types.
func (n *Number) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		return n.UnmarshalText([]byte(s))
	}
	return n.UnmarshalText(data)
}

// Parse reads a decimal number: an optional sign, digits with an optional point, and an optional exponent, such as "42", "-0.5", "1.23e45" or "7E+400". The exponent may exceed float64's range up to ±MaxExponent. Spaces, underscores, hexadecimal, "inf" and "NaN" are refused with ErrSyntax; a value whose power of ten lies beyond the range returns ErrRange, and zero is 0 whatever its exponent.
func Parse(s string) (Number, error) {
	n, err := parse(s)
	if err != nil {
		shown := s
		if len(shown) > 64 {
			shown = shown[:64] + "..."
		}
		return Number{}, fmt.Errorf("idle: parse number %q: %w", shown, err)
	}
	return n, nil
}

func parse(s string) (Number, error) {
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	// sig collects the significant digits; lead is the power of ten of the first one.
	var stack [40]byte
	sig := stack[:0]
	sawDigit, sawPoint := false, false
	var lead int64 = -1
scan:
	for ; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			sawDigit = true
			if len(sig) == 0 && c == '0' {
				if sawPoint {
					lead--
				}
				continue
			}
			sig = append(sig, c)
			if !sawPoint {
				lead++
			}
		case c == '.' && !sawPoint:
			sawPoint = true
		default:
			break scan
		}
	}
	if !sawDigit {
		return Number{}, ErrSyntax
	}
	var exp int64
	if i < len(s) {
		if s[i] != 'e' && s[i] != 'E' {
			return Number{}, ErrSyntax
		}
		i++
		start := i
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if i == len(s) {
			return Number{}, ErrSyntax
		}
		for j := i; j < len(s); j++ {
			if s[j] < '0' || s[j] > '9' {
				return Number{}, ErrSyntax
			}
		}
		// The text is all digits, so the only possible error is overflow, where ParseInt returns the int64 limit of that sign; clampExp keeps exp + lead from overflowing, and the check below refuses it.
		v, _ := strconv.ParseInt(s[start:], 10, 64)
		exp = clampExp(v)
	}
	if len(sig) == 0 {
		return Number{}, nil
	}
	e := exp + lead
	if e > MaxExponent || e < -MaxExponent {
		return Number{}, ErrRange
	}
	// Trailing zeros carry no information and only slow ParseFloat down.
	for len(sig) > 1 && sig[len(sig)-1] == '0' {
		sig = sig[:len(sig)-1]
	}
	// Build "d.ddd", with "e<exp>" when the value is in the plain range so ParseFloat rounds the whole decimal once.
	var mstack [64]byte
	text := append(mstack[:0], sig[0])
	if len(sig) > 1 {
		text = append(text, '.')
		text = append(text, sig[1:]...)
	}
	plain := e >= plainBottom && e <= plainTop
	if plain {
		text = append(text, 'e')
		text = strconv.AppendInt(text, e, 10)
	}
	m, err := strconv.ParseFloat(string(text), 64)
	if err != nil {
		return Number{}, ErrSyntax
	}
	if neg {
		m = -m
	}
	if plain {
		if isPlain(m) {
			return Number{m: m}, nil
		}
		return canon(m, 0), nil
	}
	return canon(m, e), nil
}
