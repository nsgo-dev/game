package meshopt

import (
	"encoding/binary"
	"math"
)

// Filters reconstruct values in place after the attribute codec, following meshoptimizer's scalar decoders. Explicit float32 conversions keep each product rounded on its own so that no platform fuses it into a multiply-add.

// roundSigned is int(v + (ref >= 0 ? 0.5 : -0.5)), rounding half away from zero by the sign of ref.
func roundSigned(v, ref float32) int32 {
	if ref >= 0 {
		return int32(float32(v + 0.5))
	}
	return int32(float32(v - 0.5))
}

func abs32(v float32) float32 { return math.Float32frombits(math.Float32bits(v) &^ (1 << 31)) }

// DecodeFilterOct decodes count octahedral-encoded unit vectors stored as 4 signed bytes (stride 4) or 4 signed 16-bit integers (stride 8).
func DecodeFilterOct(data []byte, count, stride int) error {
	if (stride != 4 && stride != 8) || count < 0 || len(data) < count*stride {
		return ErrLayout
	}
	for i := 0; i < count; i++ {
		e := data[i*stride:]
		var x, y, z, maxV float32
		if stride == 4 {
			maxV = 127
			x, y, z = float32(int8(e[0])), float32(int8(e[1])), float32(int8(e[2]))
		} else {
			maxV = 32767
			x = float32(int16(binary.LittleEndian.Uint16(e[0:])))
			y = float32(int16(binary.LittleEndian.Uint16(e[2:])))
			z = float32(int16(binary.LittleEndian.Uint16(e[4:])))
		}
		z = z - abs32(x) - abs32(y)
		t := float32(0)
		if z < 0 {
			t = z
		}
		if x >= 0 {
			x += t
		} else {
			x -= t
		}
		if y >= 0 {
			y += t
		} else {
			y -= t
		}
		l := float32(math.Sqrt(float64(float32(x*x) + float32(y*y) + float32(z*z))))
		s := maxV / l
		xf := roundSigned(float32(x*s), x)
		yf := roundSigned(float32(y*s), y)
		zf := roundSigned(float32(z*s), z)
		if stride == 4 {
			e[0], e[1], e[2] = byte(int8(xf)), byte(int8(yf)), byte(int8(zf))
		} else {
			binary.LittleEndian.PutUint16(e[0:], uint16(int16(xf)))
			binary.LittleEndian.PutUint16(e[2:], uint16(int16(yf)))
			binary.LittleEndian.PutUint16(e[4:], uint16(int16(zf)))
		}
	}
	return nil
}

// DecodeFilterQuat decodes count quaternions stored as 4 signed 16-bit integers with the largest component reconstructed (stride 8).
func DecodeFilterQuat(data []byte, count, stride int) error {
	if stride != 8 || count < 0 || len(data) < count*stride {
		return ErrLayout
	}
	scale := float32(1 / math.Sqrt(2))
	for i := 0; i < count; i++ {
		e := data[i*8:]
		var q [4]int16
		for c := range q {
			q[c] = int16(binary.LittleEndian.Uint16(e[2*c:]))
		}
		sf := int32(q[3]) | 3
		ss := scale / float32(sf)
		x := float32(float32(q[0]) * ss)
		y := float32(float32(q[1]) * ss)
		z := float32(float32(q[2]) * ss)
		ww := 1 - float32(x*x) - float32(y*y) - float32(z*z)
		if ww < 0 {
			ww = 0
		}
		w := float32(math.Sqrt(float64(ww)))
		xf := roundSigned(float32(x*32767), x)
		yf := roundSigned(float32(y*32767), y)
		zf := roundSigned(float32(z*32767), z)
		wf := int32(float32(float32(w*32767) + 0.5))
		qc := int(q[3] & 3)
		put := func(slot int, v int32) {
			binary.LittleEndian.PutUint16(e[2*(slot&3):], uint16(int16(v)))
		}
		put(qc+1, xf)
		put(qc+2, yf)
		put(qc+3, zf)
		put(qc, wf)
	}
	return nil
}

// DecodeFilterExp decodes count elements of stride bytes holding 32-bit values with an 8-bit exponent and a 24-bit signed mantissa.
func DecodeFilterExp(data []byte, count, stride int) error {
	if stride <= 0 || stride%4 != 0 || count < 0 || len(data) < count*stride {
		return ErrLayout
	}
	n := count * stride / 4
	for i := 0; i < n; i++ {
		v := binary.LittleEndian.Uint32(data[4*i:])
		m := int32(v<<8) >> 8
		e := int32(v) >> 24
		f := math.Float32frombits(uint32(e+127) << 23)
		binary.LittleEndian.PutUint32(data[4*i:], math.Float32bits(float32(f*float32(m))))
	}
	return nil
}

// DecodeFilterColor decodes count YCoCg-encoded colors stored as 4 unsigned bytes (stride 4) or 4 unsigned 16-bit integers (stride 8).
func DecodeFilterColor(data []byte, count, stride int) error {
	if (stride != 4 && stride != 8) || count < 0 || len(data) < count*stride {
		return ErrLayout
	}
	for i := 0; i < count; i++ {
		e := data[i*stride:]
		var yv, a int32
		var co, cg int32
		var maxV float32
		if stride == 4 {
			maxV = 255
			yv, co, cg, a = int32(e[0]), int32(int8(e[1])), int32(int8(e[2])), int32(e[3])
		} else {
			maxV = 65535
			yv = int32(binary.LittleEndian.Uint16(e[0:]))
			co = int32(int16(binary.LittleEndian.Uint16(e[2:])))
			cg = int32(int16(binary.LittleEndian.Uint16(e[4:])))
			a = int32(binary.LittleEndian.Uint16(e[6:]))
		}
		as := a
		as |= as >> 1
		as |= as >> 2
		as |= as >> 4
		as |= as >> 8
		r := yv + co - cg
		g := yv + cg
		b := yv - co - cg
		a = ((a << 1) & as) | (a & 1)
		ss := maxV / float32(as)
		rf := int32(float32(float32(float32(r)*ss) + 0.5))
		gf := int32(float32(float32(float32(g)*ss) + 0.5))
		bf := int32(float32(float32(float32(b)*ss) + 0.5))
		af := int32(float32(float32(float32(a)*ss) + 0.5))
		if stride == 4 {
			e[0], e[1], e[2], e[3] = byte(rf), byte(gf), byte(bf), byte(af)
		} else {
			binary.LittleEndian.PutUint16(e[0:], uint16(rf))
			binary.LittleEndian.PutUint16(e[2:], uint16(gf))
			binary.LittleEndian.PutUint16(e[4:], uint16(bf))
			binary.LittleEndian.PutUint16(e[6:], uint16(af))
		}
	}
	return nil
}
