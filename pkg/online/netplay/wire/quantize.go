package wire

import (
	"math"
	mbits "math/bits"
)

// QuantizeFloat quantizes v from [minVal, maxVal] into an unsigned integer in [0, (1<<bits)-1] using integer arithmetic only, so every CPU architecture (amd64, arm64, wasm) and cross-language implementation produces identical bytes.
//
// Values at or below minVal (and NaN) quantize to 0; values at or above maxVal quantize to (1<<bits)-1. Halfway ties round up.
func QuantizeFloat(v, minVal, maxVal float32, bits int) uint32 {
	if bits < 1 {
		return 0
	}
	if bits > 32 {
		bits = 32
	}
	umin := math.Float32bits(minVal)
	umax := math.Float32bits(maxVal)
	if !isFloat32Finite(umin) || !isFloat32Finite(umax) || float32Order(umin) >= float32Order(umax) {
		return 0
	}
	uv := math.Float32bits(v)
	if isFloat32NaN(uv) {
		return 0
	}
	maxQ := (uint64(1) << bits) - 1
	ov := float32Order(uv)
	omin := float32Order(umin)
	omax := float32Order(umax)
	if ov <= omin {
		return 0
	}
	if ov >= omax {
		return uint32(maxQ)
	}

	nSig, nExp := subFloat32Pos(uv, umin)
	dSig, dExp := subFloat32Pos(umax, umin)
	eDiff := dExp - nExp
	if eDiff < 0 {
		return uint32(maxQ)
	}
	if eDiff > 34 {
		return 0
	}

	hi, lo := mbits.Mul64(nSig, maxQ)
	q0, r0 := mbits.Div64(hi, lo, dSig)

	var q uint64
	if eDiff == 0 {
		q = q0
		if r0<<1 >= dSig {
			q++
		}
	} else {
		q = q0 >> eDiff
		lowBits := q0 & ((uint64(1) << eDiff) - 1)
		halfBit := uint64(1) << (eDiff - 1)
		if lowBits >= halfBit {
			q++
		}
	}
	if q > maxQ {
		q = maxQ
	}
	return uint32(q)
}

// DequantizeFloat reconstructs a float32 from an unsigned quantized integer q in [0, (1<<bits)-1] over [minVal, maxVal] using integer arithmetic only, guaranteeing bit-identical results across architectures.
//
// q == 0 returns minVal exactly and q >= (1<<bits)-1 returns maxVal exactly.
func DequantizeFloat(q uint32, minVal, maxVal float32, bits int) float32 {
	if bits < 1 {
		return minVal
	}
	if bits > 32 {
		bits = 32
	}
	umin := math.Float32bits(minVal)
	umax := math.Float32bits(maxVal)
	if !isFloat32Finite(umin) || !isFloat32Finite(umax) || float32Order(umin) >= float32Order(umax) {
		return minVal
	}
	maxQ := (uint64(1) << bits) - 1
	if q == 0 {
		return minVal
	}
	if uint64(q) >= maxQ {
		return maxVal
	}

	dSig, dExp := subFloat32Pos(umax, umin)
	pHi, pLo := mbits.Mul64(dSig, uint64(q))
	offSig, offRem := mbits.Div64(pHi, pLo, maxQ)
	offExp := dExp
	if lz := mbits.LeadingZeros64(offSig); lz > 1 {
		shift := lz - 1
		var extraSig uint64
		extraSig, offRem = mbits.Div64(offRem>>(64-shift), offRem<<shift, maxQ)
		offSig = (offSig << shift) | extraSig
		offExp -= shift
	}
	offSticky := offRem != 0

	mNeg, mSig, mExp := decomposeFloat32(umin)
	return addSignedToFloat32(mNeg, mSig, mExp, false, offSig, offExp, offSticky)
}

// QuantizeFixed converts v into a signed fixed-point integer round(v * scale), clamped to [minInt, maxInt], using integer arithmetic only. Halfway ties round away from zero.
func QuantizeFixed(v float64, scale, minInt, maxInt int64) int64 {
	if scale <= 0 || minInt > maxInt {
		return 0
	}
	uv := math.Float64bits(v)
	if isFloat64NaN(uv) {
		return clampInt64(0, minInt, maxInt)
	}
	neg := uv>>63 != 0
	expField := (uv >> 52) & 0x7ff
	if expField == 0x7ff {
		if neg {
			return minInt
		}
		return maxInt
	}
	mag, overflow := scaledFloat64Mag(uv, uint64(scale))
	if overflow {
		if neg {
			return minInt
		}
		return maxInt
	}
	if neg {
		if minInt >= 0 {
			return minInt
		}
		minMag := uint64(-(minInt + 1)) + 1
		if mag >= minMag {
			return minInt
		}
		val := -int64(mag)
		if val > maxInt {
			return maxInt
		}
		return val
	}
	if maxInt <= 0 {
		return maxInt
	}
	if mag >= uint64(maxInt) {
		return maxInt
	}
	val := int64(mag)
	if val < minInt {
		return minInt
	}
	return val
}

// QuantizeFixedUint converts v into an unsigned fixed-point integer round(v * scale), clamped to [0, maxUint], using integer arithmetic only. Halfway ties round up.
func QuantizeFixedUint(v float64, scale int64, maxUint uint64) uint64 {
	if scale <= 0 {
		return 0
	}
	uv := math.Float64bits(v)
	if isFloat64NaN(uv) {
		return 0
	}
	neg := uv>>63 != 0
	if neg {
		return 0
	}
	if (uv>>52)&0x7ff == 0x7ff {
		return maxUint
	}
	mag, overflow := scaledFloat64Mag(uv, uint64(scale))
	if overflow || mag >= maxUint {
		return maxUint
	}
	return mag
}

// DequantizeFixed converts a signed fixed-point integer q back to float64 (q / scale) using integer arithmetic only.
func DequantizeFixed(q, scale int64) float64 {
	if q == 0 || scale <= 0 {
		return 0
	}
	neg := q < 0
	var mag uint64
	if neg {
		mag = uint64(-(q + 1)) + 1
	} else {
		mag = uint64(q)
	}
	return divUint64ToFloat64(neg, mag, uint64(scale))
}

// DequantizeFixedUint converts an unsigned fixed-point integer q back to float64 (q / scale) using integer arithmetic only.
func DequantizeFixedUint(q uint64, scale int64) float64 {
	if q == 0 || scale <= 0 {
		return 0
	}
	return divUint64ToFloat64(false, q, uint64(scale))
}

func clampInt64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func isFloat32NaN(u uint32) bool {
	return (u&0x7f800000) == 0x7f800000 && (u&0x007fffff) != 0
}

func isFloat32Finite(u uint32) bool {
	return (u & 0x7f800000) != 0x7f800000
}

func float32Order(u uint32) int32 {
	if u == 0x80000000 {
		return 0
	}
	if u&0x80000000 != 0 {
		return -int32(u & 0x7fffffff)
	}
	return int32(u)
}

func decomposeFloat32(u uint32) (neg bool, sig uint64, exp int) {
	neg = u>>31 != 0
	e := int((u >> 23) & 0xff)
	f := uint64(u & 0x7fffff)
	if e == 0 {
		if f == 0 {
			return false, 0, 0
		}
		sig = f
		exp = -149
	} else {
		sig = (uint64(1) << 23) | f
		exp = e - 150
	}
	// Normalize sig so bit 23 is set (2^23 <= sig < 2^24).
	lz := mbits.LeadingZeros64(sig)
	if shift := lz - 40; shift > 0 {
		sig <<= shift
		exp -= shift
	}
	return neg, sig, exp
}

// subFloat32Pos computes a - b for finite float32 bit patterns with a > b, returning normalized (sig, exp) where 2^62 <= sig < 2^63.
func subFloat32Pos(ua, ub uint32) (uint64, int) {
	aNeg, aSig, aExp := decomposeFloat32(ua)
	bNeg, bSig, bExp := decomposeFloat32(ub)
	bNeg = !bNeg // compute a + (-b)

	var resSig uint64
	var resExp int
	switch {
	case bSig == 0:
		resSig, resExp = aSig, aExp
	case aSig == 0:
		resSig, resExp = bSig, bExp
	default:
		// Put the larger-magnitude term first.
		hiNeg, hiSig, hiExp := aNeg, aSig, aExp
		loNeg, loSig, loExp := bNeg, bSig, bExp
		if bExp > aExp || (bExp == aExp && bSig > aSig) {
			hiNeg, hiSig, hiExp = bNeg, bSig, bExp
			loNeg, loSig, loExp = aNeg, aSig, aExp
		}
		d := hiExp - loExp
		if d <= 38 {
			shifted := hiSig << d
			if hiNeg == loNeg {
				resSig = shifted + loSig
			} else {
				resSig = shifted - loSig
			}
			resExp = loExp
		} else {
			shiftedHi := hiSig << 38
			s := d - 38
			var shiftedLo uint64
			var sticky uint64
			if s >= 64 {
				shiftedLo = 0
				sticky = 1
			} else {
				shiftedLo = loSig >> s
				if loSig&((uint64(1)<<s)-1) != 0 {
					sticky = 1
				}
			}
			if hiNeg == loNeg {
				resSig = shiftedHi + shiftedLo
			} else {
				resSig = shiftedHi - shiftedLo - sticky
			}
			resExp = hiExp - 38
		}
	}

	lz := mbits.LeadingZeros64(resSig)
	if lz > 1 {
		shift := lz - 1
		resSig <<= shift
		resExp -= shift
	} else if lz == 0 {
		resSig >>= 1
		resExp++
	}
	return resSig, resExp
}

func addSignedToFloat32(aNeg bool, aSig uint64, aExp int, bNeg bool, bSig uint64, bExp int, bSticky bool) float32 {
	if aSig == 0 {
		return packFloat32(bNeg, bSig, bExp, bSticky)
	}
	if bSig == 0 {
		return packFloat32(aNeg, aSig, aExp, false)
	}
	// Normalize both aSig and bSig into [2^62, 2^63).
	if lz := mbits.LeadingZeros64(aSig); lz > 1 {
		shift := lz - 1
		aSig <<= shift
		aExp -= shift
	} else if lz == 0 {
		aSig >>= 1
		aExp++
	}
	if lz := mbits.LeadingZeros64(bSig); lz > 1 {
		shift := lz - 1
		bSig <<= shift
		bExp -= shift
	} else if lz == 0 {
		bSticky = bSticky || (bSig&1 != 0)
		bSig >>= 1
		bExp++
	}

	// Put larger magnitude in hi.
	hiNeg, hiSig, hiExp, hiSticky := aNeg, aSig, aExp, false
	loNeg, loSig, loExp, loSticky := bNeg, bSig, bExp, bSticky
	if bExp > aExp || (bExp == aExp && (bSig > aSig || (bSig == aSig && bSticky))) {
		hiNeg, hiSig, hiExp, hiSticky = bNeg, bSig, bExp, bSticky
		loNeg, loSig, loExp, loSticky = aNeg, aSig, aExp, false
	}

	d := hiExp - loExp
	var loShifted uint64
	var lostSticky bool
	switch {
	case d == 0:
		loShifted = loSig
		lostSticky = loSticky
	case d < 64:
		loShifted = loSig >> d
		lostSticky = loSticky || (loSig&((uint64(1)<<d)-1) != 0)
	default:
		loShifted = 0
		lostSticky = true
	}

	if hiNeg == loNeg {
		return packFloat32(hiNeg, hiSig+loShifted, hiExp, hiSticky || lostSticky)
	}
	// Subtraction: hi >= lo.
	if hiSig == loShifted && !hiSticky && !lostSticky {
		return 0
	}
	diff := hiSig - loShifted
	sticky := hiSticky
	if lostSticky {
		if !hiSticky {
			diff--
		}
		sticky = true
	}
	if diff == 0 && !sticky {
		return 0
	}
	if diff == 0 {
		// Remainder is strictly between 0 and 2^hiExp; represents a tiny non-zero value.
		return packFloat32(hiNeg, 1, hiExp-1, true)
	}
	return packFloat32(hiNeg, diff, hiExp, sticky)
}

// packFloat32 packs (-1)^neg * (sig + eps) * 2^exp into an IEEE-754 float32 using round-to-nearest-even.
func packFloat32(neg bool, sig uint64, exp int, sticky bool) float32 {
	if sig == 0 {
		return 0
	}
	var sign uint32
	if neg {
		sign = 1 << 31
	}
	top := 63 - mbits.LeadingZeros64(sig)
	unbiased := exp + top
	if unbiased > 127 {
		return math.Float32frombits(sign | 0x7f800000)
	}
	if unbiased >= -126 {
		var mant uint64
		var roundBit bool
		var st bool
		if top > 23 {
			shift := top - 23
			mant = sig >> shift
			roundBit = (sig>>(shift-1))&1 != 0
			st = sticky || (shift > 1 && sig&((uint64(1)<<(shift-1))-1) != 0)
		} else {
			mant = sig << (23 - top)
			st = sticky
		}
		if roundBit && (st || mant&1 != 0) {
			mant++
			if mant == 1<<24 {
				mant >>= 1
				unbiased++
				if unbiased > 127 {
					return math.Float32frombits(sign | 0x7f800000)
				}
			}
		}
		return math.Float32frombits(sign | uint32(unbiased+127)<<23 | uint32(mant&0x7fffff))
	}

	// Subnormal or underflow to zero.
	shift := -149 - exp
	if shift > 64 {
		return math.Float32frombits(sign)
	}
	var mant uint64
	var roundBit bool
	var st bool
	if shift == 64 {
		mant = 0
		roundBit = sig>>63 != 0
		st = sticky || (sig&((uint64(1)<<63)-1) != 0)
	} else if shift > 0 {
		mant = sig >> shift
		roundBit = (sig>>(shift-1))&1 != 0
		st = sticky || (shift > 1 && sig&((uint64(1)<<(shift-1))-1) != 0)
	} else {
		mant = sig << (-shift)
		st = sticky
	}
	if roundBit && (st || mant&1 != 0) {
		mant++
		if mant == 1<<23 {
			return math.Float32frombits(sign | (1 << 23))
		}
	}
	return math.Float32frombits(sign | uint32(mant&0x7fffff))
}

func isFloat64NaN(u uint64) bool {
	return (u&0x7ff0000000000000) == 0x7ff0000000000000 && (u&0x000fffffffffffff) != 0
}

// scaledFloat64Mag computes round(|v| * scale) for a finite float64 bit pattern u with halfway ties rounding up.
func scaledFloat64Mag(u uint64, scale uint64) (uint64, bool) {
	e := int((u >> 52) & 0x7ff)
	f := u & 0x000fffffffffffff
	var sig uint64
	var exp int
	if e == 0 {
		if f == 0 {
			return 0, false
		}
		sig = f
		exp = -1074
	} else {
		sig = (uint64(1) << 52) | f
		exp = e - 1075
	}
	hi, lo := mbits.Mul64(sig, scale)
	if exp >= 0 {
		if exp >= 64 {
			return 0, hi != 0 || lo != 0
		}
		if hi != 0 {
			return 0, true
		}
		if exp > 0 && lo>>(64-exp) != 0 {
			return 0, true
		}
		return lo << exp, false
	}

	s := -exp // s >= 1
	if s >= 129 {
		return 0, false
	}
	if s == 128 {
		// Halfway bit is bit 63 of hi.
		if hi>>63 != 0 {
			return 1, false
		}
		return 0, false
	}
	if s >= 64 {
		k := s - 64 // 0..63
		var q uint64
		var roundUp bool
		if k == 0 {
			q = hi
			roundUp = lo>>63 != 0
		} else {
			q = hi >> k
			roundUp = (hi>>(k-1))&1 != 0
		}
		if roundUp {
			if q == math.MaxUint64 {
				return 0, true
			}
			q++
		}
		return q, false
	}

	// 1 <= s < 64
	if hi>>s != 0 {
		return 0, true
	}
	q := (hi << (64 - s)) | (lo >> s)
	if (lo>>(s-1))&1 != 0 {
		if q == math.MaxUint64 {
			return 0, true
		}
		q++
	}
	return q, false
}

// divUint64ToFloat64 computes (-1)^neg * (mag / scale) as an exact round-to-nearest-even IEEE-754 float64 using integer arithmetic only.
func divUint64ToFloat64(neg bool, mag, scale uint64) float64 {
	// We want quotient q with MSB at bit 54 (55 significant bits: 53 mantissa + 1 round + 1 guard) plus sticky from remainder.
	// Find shift s so that 2^54 <= mag * 2^s / scale < 2^55.
	magTop := 63 - mbits.LeadingZeros64(mag)
	scaleTop := 63 - mbits.LeadingZeros64(scale)
	// Approximate log2(mag/scale) is magTop - scaleTop (within ±1).
	// Target MSB at bit 54 => s = 54 - (magTop - scaleTop).
	s := 54 - (magTop - scaleTop)

	var q, rem uint64
	switch {
	case s >= 0:
		// Compute (mag << s) / scale. Since magTop + s = 54 + scaleTop, the dividend fits in at most 128 bits with hi < scale when adjusted.
		for {
			var hi, lo uint64
			if s >= 64 {
				hi = mag << (s - 64)
				lo = 0
			} else if s > 0 {
				hi = mag >> (64 - s)
				lo = mag << s
			} else {
				hi = 0
				lo = mag
			}
			if hi >= scale {
				s--
				continue
			}
			q, rem = mbits.Div64(hi, lo, scale)
			if q < (uint64(1) << 54) {
				s++
				continue
			}
			break
		}
	default:
		// s < 0 means mag/scale >= 2^54 already; divide mag by scale first, then scale by 2^(-s) if needed.
		// Actually scale >= 1 and mag < 2^64, so mag/scale < 2^64; direct division works when s <= 0!
		k := -s
		q, rem = mag/scale, mag%scale
		// q may have up to 64 bits; packFloat64 handles any top bit position!
		_ = k
		return packFloat64(neg, q, 0, rem != 0)
	}
	return packFloat64(neg, q, -s, rem != 0)
}

// packFloat64 packs (-1)^neg * (sig + eps) * 2^exp into an IEEE-754 float64 using round-to-nearest-even.
func packFloat64(neg bool, sig uint64, exp int, sticky bool) float64 {
	if sig == 0 {
		return 0
	}
	var sign uint64
	if neg {
		sign = 1 << 63
	}
	top := 63 - mbits.LeadingZeros64(sig)
	unbiased := exp + top
	if unbiased > 1023 {
		return math.Float64frombits(sign | 0x7ff0000000000000)
	}
	if unbiased >= -1022 {
		var mant uint64
		var roundBit bool
		var st bool
		if top > 52 {
			shift := top - 52
			mant = sig >> shift
			roundBit = (sig>>(shift-1))&1 != 0
			st = sticky || (shift > 1 && sig&((uint64(1)<<(shift-1))-1) != 0)
		} else {
			mant = sig << (52 - top)
			st = sticky
		}
		if roundBit && (st || mant&1 != 0) {
			mant++
			if mant == 1<<53 {
				mant >>= 1
				unbiased++
				if unbiased > 1023 {
					return math.Float64frombits(sign | 0x7ff0000000000000)
				}
			}
		}
		return math.Float64frombits(sign | uint64(unbiased+1023)<<52 | (mant & 0x000fffffffffffff))
	}

	// Subnormal or underflow to zero.
	shift := -1074 - exp
	if shift > 64 {
		return math.Float64frombits(sign)
	}
	var mant uint64
	var roundBit bool
	var st bool
	if shift == 64 {
		mant = 0
		roundBit = sig>>63 != 0
		st = sticky || (sig&((uint64(1)<<63)-1) != 0)
	} else if shift > 0 {
		mant = sig >> shift
		roundBit = (sig>>(shift-1))&1 != 0
		st = sticky || (shift > 1 && sig&((uint64(1)<<(shift-1))-1) != 0)
	} else {
		mant = sig << (-shift)
		st = sticky
	}
	if roundBit && (st || mant&1 != 0) {
		mant++
		if mant == 1<<52 {
			return math.Float64frombits(sign | (1 << 52))
		}
	}
	return math.Float64frombits(sign | (mant & 0x000fffffffffffff))
}

// BitWriter appends MSB-first bit-packed values to a caller-owned byte slice without heap allocations.
type BitWriter struct {
	Dst  []byte
	acc  uint64
	bits uint8
}

// WriteBits appends the lowest n bits (1..64) of v in MSB-first order.
func (w *BitWriter) WriteBits(v uint64, n uint8) {
	if n == 0 {
		return
	}
	if n < 64 {
		v &= (uint64(1) << n) - 1
	}
	// If adding n bits would exceed 64 bits in acc, flush full bytes first.
	for w.bits >= 8 {
		w.bits -= 8
		w.Dst = append(w.Dst, byte(w.acc>>w.bits))
	}
	if w.bits+n <= 64 {
		w.acc = (w.acc << n) | v
		w.bits += n
	} else {
		// Split across 64-bit boundary (only when n > 56 and w.bits > 0).
		hiN := 64 - w.bits
		loN := n - hiN
		w.acc = (w.acc << hiN) | (v >> loN)
		w.bits = 64
		for w.bits >= 8 {
			w.bits -= 8
			w.Dst = append(w.Dst, byte(w.acc>>w.bits))
		}
		w.acc = v & ((uint64(1) << loN) - 1)
		w.bits = loN
	}
	for w.bits >= 8 {
		w.bits -= 8
		w.Dst = append(w.Dst, byte(w.acc>>w.bits))
	}
	if w.bits == 0 {
		w.acc = 0
	} else {
		w.acc &= (uint64(1) << w.bits) - 1
	}
}

// Flush pads any remaining partial byte with zero low bits and returns the updated destination slice.
func (w *BitWriter) Flush() []byte {
	if w.bits > 0 {
		w.Dst = append(w.Dst, byte(w.acc<<(8-w.bits)))
		w.acc = 0
		w.bits = 0
	}
	return w.Dst
}

// BitReader reads MSB-first bit-packed values from a byte slice without heap allocations.
type BitReader struct {
	Src  []byte
	Off  int
	acc  uint64
	bits uint8
}

// ReadBits reads n bits (1..64) in MSB-first order.
func (r *BitReader) ReadBits(n uint8) (uint64, error) {
	if n == 0 {
		return 0, nil
	}
	if n <= 56 {
		for r.bits < n {
			if r.Off >= len(r.Src) {
				return 0, ErrShortBuffer
			}
			r.acc = (r.acc << 8) | uint64(r.Src[r.Off])
			r.Off++
			r.bits += 8
		}
		r.bits -= n
		v := (r.acc >> r.bits) & ((uint64(1) << n) - 1)
		if r.bits == 0 {
			r.acc = 0
		} else {
			r.acc &= (uint64(1) << r.bits) - 1
		}
		return v, nil
	}
	// Split reads wider than 56 bits into two calls so r.acc never overflows 64 bits.
	hi, err := r.ReadBits(32)
	if err != nil {
		return 0, err
	}
	lo, err := r.ReadBits(n - 32)
	if err != nil {
		return 0, err
	}
	return (hi << (n - 32)) | lo, nil
}

// Align discards the remaining unread low bits of the current partial byte, returning ErrNonZeroPadding if any padding bit is non-zero.
func (r *BitReader) Align() error {
	if r.bits > 0 {
		if r.acc&((uint64(1)<<r.bits)-1) != 0 {
			return ErrNonZeroPadding
		}
		r.acc = 0
		r.bits = 0
	}
	return nil
}
