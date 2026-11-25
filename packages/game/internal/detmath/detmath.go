// Package detmath provides FMA-free deterministic math helpers and canonical
// state hashing shared by physics, physics3d, and vehicle.
//
// Go's arm64 compiler automatically contracts x*y+z into single-rounding FMADD
// instructions unless an explicit float32/float64 conversion separates the
// multiply from the add, whereas amd64 uses separate multiply and add
// instructions. Go's standard library math.Sin, math.Cos, math.Atan2,
// math.Acos, math.Tan, and math.Hypot also compile with FMADD/FMSUB on arm64.
// Every function in this package inserts explicit conversions around products
// so arm64, amd64, and wasm execute identical IEEE-754 rounding bit-for-bit.
package detmath

import "math"

// MulAdd32 computes float32(a*b) + c without FMA contraction.
func MulAdd32(a, b, c float32) float32 {
	return float32(a*b) + c
}

// MulSub32 computes float32(a*b) - c without FMA contraction.
func MulSub32(a, b, c float32) float32 {
	return float32(a*b) - c
}

// MulAdd64 computes float64(a*b) + c without FMA contraction.
func MulAdd64(a, b, c float64) float64 {
	return float64(a*b) + c
}

// MulSub64 computes float64(a*b) - c without FMA contraction.
func MulSub64(a, b, c float64) float64 {
	return float64(a*b) - c
}

// Hypot returns sqrt(p*p + q*q) without overflow, underflow, or FMA contraction.
func Hypot(p, q float64) float64 {
	p = math.Abs(p)
	q = math.Abs(q)
	if math.IsInf(p, 1) || math.IsInf(q, 1) {
		return math.Inf(1)
	}
	if math.IsNaN(p) || math.IsNaN(q) {
		return math.NaN()
	}
	if p < q {
		p, q = q, p
	}
	if p == 0 {
		return 0
	}
	r := q / p
	return p * math.Sqrt(1.0+float64(r*r))
}

// Cody-Waite Pi/4 constants and minimax coefficients matching Cephes / Go math/sin.go.
const (
	pi4A   = 7.85398125648498535156e-1  // 0x3fe921fb40000000
	pi4B   = 3.77489470793079817668e-8  // 0x3e64442d00000000
	pi4C   = 2.69515142907905952645e-15 // 0x3ce8469898cc5170
	m4PiO2 = 1.2732395447351626861510701069801148

	sinC0 = -1.66666666666666324348e-01
	sinC1 = 8.33333333332248946124e-03
	sinC2 = -1.98412698298579493134e-04
	sinC3 = 2.75573137070700676789e-06
	sinC4 = -2.50507602534068634195e-08
	sinC5 = 1.58969099521155010221e-10

	cosC0 = 4.16666666666666019037e-02
	cosC1 = -1.38888888888741095749e-03
	cosC2 = 2.48015872894767294178e-05
	cosC3 = -2.75573143513906633035e-07
	cosC4 = 2.08757232129817482790e-09
	cosC5 = -1.13596475577881948265e-11
)

// reduceTrig reduces |x| into [0, pi/4] and returns the octant j and reduced z.
func reduceTrig(x float64) (j uint64, z float64) {
	// For very large arguments where y*pi4A would lose exact bits, fold into [-pi, pi] first.
	if x >= 1<<25 {
		x = math.Remainder(x, 2*math.Pi)
		if x < 0 {
			x += 2 * math.Pi
		}
	}
	j = uint64(x * m4PiO2)
	y := float64(j)
	if j&1 == 1 {
		j++
		y++
	}
	j &= 7
	z = float64(float64(x-float64(y*pi4A))-float64(y*pi4B)) - float64(y*pi4C)
	return j, z
}

func sinPoly(z float64) float64 {
	zz := float64(z * z)
	p := float64(zz*sinC5) + sinC4
	p = float64(zz*p) + sinC3
	p = float64(zz*p) + sinC2
	p = float64(zz*p) + sinC1
	p = float64(zz*p) + sinC0
	return z + float64(float64(z*zz)*p)
}

func cosPoly(z float64) float64 {
	zz := float64(z * z)
	p := float64(zz*cosC5) + cosC4
	p = float64(zz*p) + cosC3
	p = float64(zz*p) + cosC2
	p = float64(zz*p) + cosC1
	p = float64(zz*p) + cosC0
	return float64(1.0-float64(0.5*zz)) + float64(float64(zz*zz)*p)
}

// Sin returns the sine of x (in radians) without FMA contraction.
func Sin(x float64) float64 {
	if x == 0 {
		return x
	}
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return math.NaN()
	}
	sign := false
	if x < 0 {
		x = -x
		sign = true
	}
	j, z := reduceTrig(x)
	if j > 3 {
		sign = !sign
		j -= 4
	}
	var y float64
	if j == 1 || j == 2 {
		y = cosPoly(z)
	} else {
		y = sinPoly(z)
	}
	if sign {
		y = -y
	}
	return y
}

// Cos returns the cosine of x (in radians) without FMA contraction.
func Cos(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return math.NaN()
	}
	if x < 0 {
		x = -x
	}
	j, z := reduceTrig(x)
	sign := false
	if j > 3 {
		sign = !sign
		j -= 4
	}
	if j > 1 {
		sign = !sign
	}
	var y float64
	if j == 1 || j == 2 {
		y = sinPoly(z)
	} else {
		y = cosPoly(z)
	}
	if sign {
		y = -y
	}
	return y
}

// Sincos returns Sin(x), Cos(x) without FMA contraction.
func Sincos(x float64) (sin, cos float64) {
	if x == 0 {
		return x, 1
	}
	if math.IsNaN(x) || math.IsInf(x, 0) {
		n := math.NaN()
		return n, n
	}
	sinSign := false
	cosSign := false
	if x < 0 {
		x = -x
		sinSign = true
	}
	j, z := reduceTrig(x)
	if j > 3 {
		j -= 4
		sinSign, cosSign = !sinSign, !cosSign
	}
	if j > 1 {
		cosSign = !cosSign
	}
	ss := sinPoly(z)
	cc := cosPoly(z)
	if j == 1 || j == 2 {
		sin, cos = cc, ss
	} else {
		sin, cos = ss, cc
	}
	if sinSign {
		sin = -sin
	}
	if cosSign {
		cos = -cos
	}
	return sin, cos
}

// Tan returns the tangent of x (in radians) without FMA contraction.
func Tan(x float64) float64 {
	if x == 0 {
		return x
	}
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return math.NaN()
	}
	s, c := Sincos(x)
	return s / c
}

// Cephes / Go math/atan.go minimax rational approximation constants.
const (
	atanP0 = -8.750608600031904122785e-01
	atanP1 = -1.615753718733365076637e+01
	atanP2 = -7.500855792314704667340e+01
	atanP3 = -1.228866684490136173410e+02
	atanP4 = -6.485021904942025371773e+01

	atanQ0 = 2.485846490142306297962e+01
	atanQ1 = 1.650270098316988542046e+02
	atanQ2 = 4.328810604912902668951e+02
	atanQ3 = 4.853903996359136964868e+02
	atanQ4 = 1.945506571482613964425e+02

	atanMorebits = 6.123233995736765886130e-17 // pi/2 = math.Pi/2 + atanMorebits
	atanTan3PiO8 = 2.41421356237309504880      // tan(3*pi/8)
)

func satan(x float64) float64 {
	z := float64(x * x)
	p := float64(atanP0*z) + atanP1
	p = float64(p*z) + atanP2
	p = float64(p*z) + atanP3
	p = float64(p*z) + atanP4

	q := z + atanQ0
	q = float64(q*z) + atanQ1
	q = float64(q*z) + atanQ2
	q = float64(q*z) + atanQ3
	q = float64(q*z) + atanQ4

	t := float64(z*p) / q
	return x + float64(x*t)
}

func xatan(x float64) float64 {
	var y, z float64
	switch {
	case x > atanTan3PiO8:
		y = math.Pi/2 + atanMorebits
		z = satan(-1.0 / x)
	case x > 0.66:
		y = math.Pi/4 + 0.5*atanMorebits
		z = satan((x - 1.0) / (x + 1.0))
	default:
		z = satan(x)
	}
	return y + z
}

// Atan returns the arctangent of x in [-pi/2, pi/2] without FMA contraction.
func Atan(x float64) float64 {
	if x == 0 {
		return x
	}
	if math.IsNaN(x) {
		return x
	}
	if math.IsInf(x, 1) {
		return math.Pi / 2
	}
	if math.IsInf(x, -1) {
		return -math.Pi / 2
	}
	if x < 0 {
		return -xatan(-x)
	}
	return xatan(x)
}

// Atan2 returns the arc tangent of y/x using the signs of both to determine the
// quadrant, without FMA contraction.
func Atan2(y, x float64) float64 {
	switch {
	case math.IsNaN(y) || math.IsNaN(x):
		return math.NaN()
	case y == 0:
		if x >= 0 && !math.Signbit(x) {
			return math.Copysign(0, y)
		}
		return math.Copysign(math.Pi, y)
	case x == 0:
		return math.Copysign(math.Pi/2, y)
	case math.IsInf(x, 0):
		if math.IsInf(x, 1) {
			if math.IsInf(y, 0) {
				return math.Copysign(math.Pi/4, y)
			}
			return math.Copysign(0, y)
		}
		if math.IsInf(y, 0) {
			return math.Copysign(3*math.Pi/4, y)
		}
		return math.Copysign(math.Pi, y)
	case math.IsInf(y, 0):
		return math.Copysign(math.Pi/2, y)
	}

	q := Atan(y / x)
	if x < 0 {
		if q <= 0 {
			return q + math.Pi
		}
		return q - math.Pi
	}
	return q
}

// Acos returns the arccosine of x in [0, pi] without FMA contraction.
func Acos(x float64) float64 {
	if math.IsNaN(x) || x < -1 || x > 1 {
		return math.NaN()
	}
	if x == 1 {
		return 0
	}
	if x == -1 {
		return math.Pi
	}
	if x == 0 {
		return math.Pi / 2
	}
	// (1-x)*(1+x) avoids catastrophic cancellation near |x| == 1.
	sinVal := math.Sqrt(float64(1.0-x) * float64(1.0+x))
	return Atan2(sinVal, x)
}

// Float32Bits returns canonical IEEE-754 bits for hashing: -0.0 maps to +0.0
// and any NaN maps to a single quiet NaN bit pattern.
func Float32Bits(v float32) uint32 {
	if v == 0 {
		return 0
	}
	if v != v {
		return 0x7fc00000
	}
	return math.Float32bits(v)
}

// Float64Bits returns canonical IEEE-754 bits for hashing: -0.0 maps to +0.0
// and any NaN maps to a single quiet NaN bit pattern.
func Float64Bits(v float64) uint64 {
	if v == 0 {
		return 0
	}
	if v != v {
		return 0x7ff8000000000000
	}
	return math.Float64bits(v)
}

// Hasher is a zero-allocation streaming 64-bit FNV-1a hasher for deterministic
// physics world checksums.
type Hasher struct {
	h uint64
}

const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

// NewHasher returns a new FNV-1a Hasher initialized with the standard 64-bit offset basis.
func NewHasher() Hasher {
	return Hasher{h: fnvOffset64}
}

// Sum64 returns the current 64-bit FNV-1a digest.
func (h *Hasher) Sum64() uint64 {
	return h.h
}

// WriteUint8 mixes one byte into the hash.
func (h *Hasher) WriteUint8(v uint8) {
	h.h ^= uint64(v)
	h.h *= fnvPrime64
}

// WriteBool mixes one boolean into the hash.
func (h *Hasher) WriteBool(v bool) {
	if v {
		h.WriteUint8(1)
	} else {
		h.WriteUint8(0)
	}
}

// WriteUint32 mixes a little-endian uint32 into the hash.
func (h *Hasher) WriteUint32(v uint32) {
	h.WriteUint8(uint8(v))
	h.WriteUint8(uint8(v >> 8))
	h.WriteUint8(uint8(v >> 16))
	h.WriteUint8(uint8(v >> 24))
}

// WriteUint64 mixes a little-endian uint64 into the hash.
func (h *Hasher) WriteUint64(v uint64) {
	h.WriteUint8(uint8(v))
	h.WriteUint8(uint8(v >> 8))
	h.WriteUint8(uint8(v >> 16))
	h.WriteUint8(uint8(v >> 24))
	h.WriteUint8(uint8(v >> 32))
	h.WriteUint8(uint8(v >> 40))
	h.WriteUint8(uint8(v >> 48))
	h.WriteUint8(uint8(v >> 56))
}

// WriteInt mixes an int as a fixed-width int64 so 32-bit and 64-bit targets agree.
func (h *Hasher) WriteInt(v int) {
	h.WriteUint64(uint64(int64(v)))
}

// WriteFloat32 mixes canonical IEEE-754 float32 bits into the hash.
func (h *Hasher) WriteFloat32(v float32) {
	h.WriteUint32(Float32Bits(v))
}

// WriteFloat64 mixes canonical IEEE-754 float64 bits into the hash.
func (h *Hasher) WriteFloat64(v float64) {
	h.WriteUint64(Float64Bits(v))
}

// WriteString mixes the length and bytes of s into the hash.
func (h *Hasher) WriteString(s string) {
	h.WriteInt(len(s))
	for i := 0; i < len(s); i++ {
		h.WriteUint8(s[i])
	}
}
