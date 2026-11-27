package wire_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
)

func TestQFloatGolden(t *testing.T) {
	// Golden vectors verified on both amd64 and arm64 in CI. Every value is computed with pure integer arithmetic.
	cases := []struct {
		name     string
		v        float32
		min, max float32
		bits     int
		wantQ    uint32
		wantDeq  uint32 // exact IEEE-754 float32 bit pattern of DequantizeFloat(wantQ, min, max, bits)
	}{
		{name: "zero_in_sym_16", v: 0, min: -1024, max: 1024, bits: 16, wantQ: 32768, wantDeq: 0x3c800080},
		{name: "neg_zero_in_sym_16", v: float32(math.Copysign(0, -1)), min: -1024, max: 1024, bits: 16, wantQ: 32768, wantDeq: 0x3c800080},
		{name: "min_exact_16", v: -1024, min: -1024, max: 1024, bits: 16, wantQ: 0, wantDeq: math.Float32bits(-1024)},
		{name: "max_exact_16", v: 1024, min: -1024, max: 1024, bits: 16, wantQ: 65535, wantDeq: math.Float32bits(1024)},
		{name: "below_min_clamp", v: -9999, min: -1024, max: 1024, bits: 16, wantQ: 0, wantDeq: math.Float32bits(-1024)},
		{name: "above_max_clamp", v: 9999, min: -1024, max: 1024, bits: 16, wantQ: 65535, wantDeq: math.Float32bits(1024)},
		{name: "nan_clamps_to_zero", v: float32(math.NaN()), min: -1024, max: 1024, bits: 16, wantQ: 0, wantDeq: math.Float32bits(-1024)},
		{name: "neg_inf_clamps", v: float32(math.Inf(-1)), min: -1024, max: 1024, bits: 16, wantQ: 0, wantDeq: math.Float32bits(-1024)},
		{name: "pos_inf_clamps", v: float32(math.Inf(1)), min: -1024, max: 1024, bits: 16, wantQ: 65535, wantDeq: math.Float32bits(1024)},
		{name: "unit_1bit_low", v: 0.49, min: 0, max: 1, bits: 1, wantQ: 0, wantDeq: math.Float32bits(0)},
		{name: "unit_1bit_half_ties_up", v: 0.5, min: 0, max: 1, bits: 1, wantQ: 1, wantDeq: math.Float32bits(1)},
		{name: "unit_8bit_quarter", v: 0.25, min: 0, max: 1, bits: 8, wantQ: 64, wantDeq: 0x3e808081},
		{name: "angle_12bit_pi_quarter", v: 0.7853982, min: -3.1415927, max: 3.1415927, bits: 12, wantQ: 2559, wantDeq: 0x3f48ea26},
		{name: "asymmetric_10bit", v: 12.345, min: -10, max: 50, bits: 10, wantQ: 381, wantDeq: 0x41458962},
		{name: "high_prec_24bit", v: 123.45679, min: -500, max: 500, bits: 24, wantQ: 10459869, wantDeq: 0x42f6e9e3},
		{name: "full_32bit_mid", v: 0.5, min: 0, max: 1, bits: 32, wantQ: 2147483648, wantDeq: 0x3f000000},
		{name: "subnormal_in_tiny_range", v: math.SmallestNonzeroFloat32, min: 0, max: 1e-38, bits: 16, wantQ: 0, wantDeq: math.Float32bits(0)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotQ := wire.QuantizeFloat(tc.v, tc.min, tc.max, tc.bits)
			if gotQ != tc.wantQ {
				t.Errorf("QuantizeFloat(%v, [%v,%v], %d) = %d (0x%x), want %d (0x%x)", tc.v, tc.min, tc.max, tc.bits, gotQ, gotQ, tc.wantQ, tc.wantQ)
			}
			gotF := wire.DequantizeFloat(gotQ, tc.min, tc.max, tc.bits)
			gotBits := math.Float32bits(gotF)
			if gotBits != tc.wantDeq {
				t.Errorf("DequantizeFloat(%d, [%v,%v], %d) = %g (0x%08x), want %g (0x%08x)", gotQ, tc.min, tc.max, tc.bits, gotF, gotBits, math.Float32frombits(tc.wantDeq), tc.wantDeq)
			}
		})
	}
}

func TestQFloatProperties(t *testing.T) {
	ranges := [][2]float32{
		{0, 1},
		{-1, 1},
		{-1024, 1024},
		{-3.1415927, 3.1415927},
		{10, 500},
	}
	for _, r := range ranges {
		minV, maxV := r[0], r[1]
		for _, bits := range []int{1, 4, 8, 10, 12, 16, 20} {
			maxQ := uint32((uint64(1) << bits) - 1)
			step := float64(maxV-minV) / float64(maxQ)
			if got := wire.QuantizeFloat(minV, minV, maxV, bits); got != 0 {
				t.Fatalf("[%v,%v] bits=%d: min quantized to %d", minV, maxV, bits, got)
			}
			if got := wire.QuantizeFloat(maxV, minV, maxV, bits); got != maxQ {
				t.Fatalf("[%v,%v] bits=%d: max quantized to %d, want %d", minV, maxV, bits, got, maxQ)
			}
			if got := wire.DequantizeFloat(0, minV, maxV, bits); got != minV {
				t.Fatalf("[%v,%v] bits=%d: dequant(0) = %v, want %v", minV, maxV, bits, got, minV)
			}
			if got := wire.DequantizeFloat(maxQ, minV, maxV, bits); got != maxV {
				t.Fatalf("[%v,%v] bits=%d: dequant(maxQ) = %v, want %v", minV, maxV, bits, got, maxV)
			}

			var prevQ uint32
			for i := 0; i <= 256; i++ {
				frac := float64(i) / 256.0
				v := float32(float64(minV) + frac*float64(maxV-minV))
				q := wire.QuantizeFloat(v, minV, maxV, bits)
				if i > 0 && q < prevQ {
					t.Fatalf("[%v,%v] bits=%d: non-monotonic at step %d: %d < %d", minV, maxV, bits, i, q, prevQ)
				}
				prevQ = q
				deq := wire.DequantizeFloat(q, minV, maxV, bits)
				diff := math.Abs(float64(deq) - float64(v))
				tol := step*0.5001 + 1e-5*math.Abs(float64(maxV-minV))
				if diff > tol {
					t.Fatalf("[%v,%v] bits=%d: v=%v q=%d deq=%v err=%v > tol=%v", minV, maxV, bits, v, q, deq, diff, tol)
				}
			}
		}
	}
}

func TestFixedGolden(t *testing.T) {
	signedCases := []struct {
		name    string
		v       float64
		scale   int64
		lo, hi  int64
		wantQ   int64
		wantDeq uint64
	}{
		{name: "zero", v: 0, scale: 1000, lo: math.MinInt32, hi: math.MaxInt32, wantQ: 0, wantDeq: 0},
		{name: "pos_exact", v: 12.345, scale: 1000, lo: math.MinInt32, hi: math.MaxInt32, wantQ: 12345, wantDeq: math.Float64bits(12.345)},
		{name: "neg_exact", v: -98.75, scale: 100, lo: math.MinInt16, hi: math.MaxInt16, wantQ: -9875, wantDeq: math.Float64bits(-98.75)},
		{name: "half_pos_rounds_away", v: 1.5, scale: 1, lo: math.MinInt16, hi: math.MaxInt16, wantQ: 2, wantDeq: math.Float64bits(2)},
		{name: "half_neg_rounds_away", v: -1.5, scale: 1, lo: math.MinInt16, hi: math.MaxInt16, wantQ: -2, wantDeq: math.Float64bits(-2)},
		{name: "clamp_hi", v: 1e9, scale: 100, lo: math.MinInt16, hi: math.MaxInt16, wantQ: math.MaxInt16, wantDeq: math.Float64bits(327.67)},
		{name: "clamp_lo", v: -1e9, scale: 100, lo: math.MinInt16, hi: math.MaxInt16, wantQ: math.MinInt16, wantDeq: math.Float64bits(-327.68)},
		{name: "nan_to_zero", v: math.NaN(), scale: 100, lo: math.MinInt16, hi: math.MaxInt16, wantQ: 0, wantDeq: 0},
		{name: "binary_scale_65536", v: 3.25, scale: 65536, lo: math.MinInt32, hi: math.MaxInt32, wantQ: 212992, wantDeq: math.Float64bits(3.25)},
	}
	for _, tc := range signedCases {
		t.Run(tc.name, func(t *testing.T) {
			gotQ := wire.QuantizeFixed(tc.v, tc.scale, tc.lo, tc.hi)
			if gotQ != tc.wantQ {
				t.Errorf("QuantizeFixed(%v, %d) = %d, want %d", tc.v, tc.scale, gotQ, tc.wantQ)
			}
			gotF := wire.DequantizeFixed(gotQ, tc.scale)
			if math.Float64bits(gotF) != tc.wantDeq {
				t.Errorf("DequantizeFixed(%d, %d) = %g (0x%016x), want %g (0x%016x)", gotQ, tc.scale, gotF, math.Float64bits(gotF), math.Float64frombits(tc.wantDeq), tc.wantDeq)
			}
		})
	}

	if got := wire.QuantizeFixedUint(-5.0, 100, math.MaxUint16); got != 0 {
		t.Fatalf("QuantizeFixedUint(-5) = %d, want 0", got)
	}
	if got := wire.QuantizeFixedUint(12.34, 100, math.MaxUint16); got != 1234 {
		t.Fatalf("QuantizeFixedUint(12.34, 100) = %d, want 1234", got)
	}
	if got := wire.DequantizeFixedUint(1234, 100); math.Float64bits(got) != math.Float64bits(12.34) {
		t.Fatalf("DequantizeFixedUint(1234, 100) = %g (0x%016x), want 12.34", got, math.Float64bits(got))
	}
}

func TestBitWriterReader(t *testing.T) {
	// Nibble split: version=2, type=5 -> 0x25.
	bw := wire.BitWriter{Dst: make([]byte, 0, 16)}
	bw.WriteBits(2, 4)
	bw.WriteBits(5, 4)
	out := bw.Flush()
	if len(out) != 1 || out[0] != 0x25 {
		t.Fatalf("nibble write = %x, want 25", out)
	}

	// Mixed widths: 3 + 12 + 12 + 5 + 60 + 4 = 96 bits = 12 bytes.
	bw = wire.BitWriter{Dst: make([]byte, 0, 16)}
	bw.WriteBits(0b101, 3)
	bw.WriteBits(0xabc, 12)
	bw.WriteBits(0x321, 12)
	bw.WriteBits(0x1f, 5)
	bw.WriteBits(0x0fedcba987654321, 60)
	bw.WriteBits(0x9, 4)
	out = bw.Flush()
	if len(out) != 12 {
		t.Fatalf("len = %d, want 12", len(out))
	}

	br := wire.BitReader{Src: out}
	check := func(n uint8, want uint64) {
		t.Helper()
		got, err := br.ReadBits(n)
		if err != nil {
			t.Fatalf("ReadBits(%d): %v", n, err)
		}
		if got != want {
			t.Fatalf("ReadBits(%d) = 0x%x, want 0x%x", n, got, want)
		}
	}
	check(3, 0b101)
	check(12, 0xabc)
	check(12, 0x321)
	check(5, 0x1f)
	check(60, 0x0fedcba987654321)
	check(4, 0x9)
	if err := br.Align(); err != nil {
		t.Fatalf("Align: %v", err)
	}
	if br.Off != 12 {
		t.Fatalf("br.Off = %d, want 12", br.Off)
	}

	// Non-zero padding bits must be refused.
	brPad := wire.BitReader{Src: []byte{0b1010_0001}}
	v, err := brPad.ReadBits(4)
	if err != nil || v != 0b1010 {
		t.Fatalf("ReadBits(4) = %x, %v", v, err)
	}
	if err := brPad.Align(); err != wire.ErrNonZeroPadding {
		t.Fatalf("Align with non-zero padding = %v, want ErrNonZeroPadding", err)
	}
}

func TestQuantizeZeroAllocs(t *testing.T) {
	var sinkU32 uint32
	var sinkF32 float32
	var sinkI64 int64
	var sinkF64 float64
	gamedebug.AssertZeroAllocs(t, 100, func() {
		q := wire.QuantizeFloat(12.345, -1024, 1024, 16)
		sinkU32 = q
		sinkF32 = wire.DequantizeFloat(q, -1024, 1024, 16)
		fi := wire.QuantizeFixed(-98.75, 1000, math.MinInt32, math.MaxInt32)
		sinkI64 = fi
		sinkF64 = wire.DequantizeFixed(fi, 1000)
	})
	_, _, _, _ = sinkU32, sinkF32, sinkI64, sinkF64
}
