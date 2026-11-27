import { describe, expect, test } from "bun:test";
import {
  Reader,
  WireError,
  Writer,
  bytesEqual,
  dequantizeFixed,
  dequantizeFixedUint,
  dequantizeFloat,
  float32Bits,
  float32FromBits,
  float64Bits,
  float64FromBits,
  quantizeFixed,
  quantizeFixedUint,
  quantizeFloat,
  utf8ByteLength,
} from "../../ts/runtime.ts";

describe("wire runtime", () => {
  test("float32Bits preserves -0.0 and canonical NaN", () => {
    expect(float32Bits(0)).toBe(0x00000000);
    expect(float32Bits(-0)).toBe(0x80000000);
    expect(Object.is(float32FromBits(0x80000000), -0)).toBe(true);
    expect(float32Bits(Number.NaN)).toBe(0x7fc00000);
    expect(Number.isNaN(float32FromBits(0x7fc00000))).toBe(true);
  });

  test("float64Bits round-trips special values", () => {
    expect(float64Bits(-0)).toBe(0x8000000000000000n);
    expect(Object.is(float64FromBits(0x8000000000000000n), -0)).toBe(true);
    expect(float64FromBits(float64Bits(12.345))).toBe(12.345);
  });

  test("Writer and Reader reuse caller-owned Uint8Array across frames", () => {
    const buf = new Uint8Array(64);
    const w = new Writer(buf);
    const r = new Reader(buf);

    for (let i = 0; i < 10; i++) {
      w.reset();
      w.writeUvarint(i * 1000);
      w.writeSvarint(-i * 17);
      w.writeU64(0x0020000000000001n + BigInt(i), false);
      const written = w.bytes();

      r.reset(written);
      const expectedSigned = i === 0 ? 0 : -i * 17;
      expect(r.readUvarint(32)).toBe(i * 1000);
      expect(r.readSvarint(32)).toBe(expectedSigned);
      expect(r.readU64(false)).toBe(0x0020000000000001n + BigInt(i));
      expect(r.remaining()).toBe(0);
    }
  });

  test("bit packing matches Go MSB-first layout and rejects non-zero padding", () => {
    const buf = new Uint8Array(16);
    const w = new Writer(buf);
    w.writeBits(0b101, 3);
    w.writeBits(0xabc, 12);
    w.writeBits(0x321, 12);
    w.writeBits(0x1f, 5);
    w.writeBitsBig(0x0fedcba987654321n, 60);
    w.writeBits(0x9, 4);
    w.flushBits();

    expect(w.bytes().byteLength).toBe(12);

    const r = new Reader(w.bytes());
    expect(r.readBits(3)).toBe(0b101);
    expect(r.readBits(12)).toBe(0xabc);
    expect(r.readBits(12)).toBe(0x321);
    expect(r.readBits(5)).toBe(0x1f);
    expect(r.readBitsBig(60)).toBe(0x0fedcba987654321n);
    expect(r.readBits(4)).toBe(0x9);
    r.alignBits();
    expect(r.remaining()).toBe(0);

    const badPad = new Reader(new Uint8Array([0b1010_0001]));
    expect(badPad.readBits(4)).toBe(0b1010);
    expect(() => badPad.alignBits()).toThrow(WireError);
    try {
      const badPad2 = new Reader(new Uint8Array([0b1010_0001]));
      badPad2.readBits(4);
      badPad2.alignBits();
    } catch (e) {
      expect((e as WireError).code).toBe("non_zero_padding");
    }
  });

  test("Writer throws buffer_full when caller buffer is too small", () => {
    const w = new Writer(new Uint8Array(2));
    expect(() => w.writeU32(1, true)).toThrow(WireError);
    try {
      w.writeU32(1, true);
    } catch (e) {
      expect((e as WireError).code).toBe("buffer_full");
    }
  });

  test("utf8ByteLength matches TextEncoder for ASCII, CJK, and surrogate pairs", () => {
    const enc = new TextEncoder();
    for (const s of ["", "hello", "αβγ", "服务器已满!", "冠军🏆🎮"]) {
      expect(utf8ByteLength(s)).toBe(enc.encode(s).byteLength);
    }
  });

  test("bytesEqual compares lengths and contents", () => {
    expect(bytesEqual(new Uint8Array([1, 2]), new Uint8Array([1, 2]))).toBe(true);
    expect(bytesEqual(new Uint8Array([1, 2]), new Uint8Array([1, 3]))).toBe(false);
    expect(bytesEqual(new Uint8Array([1]), new Uint8Array([1, 2]))).toBe(false);
  });

  test("quantize helpers handle invalid parameters safely", () => {
    expect(quantizeFloat(0.5, 0, 1, 0)).toBe(0);
    expect(dequantizeFloat(0, 0, 1, 0)).toBe(0);
    expect(quantizeFixed(1.5, 0n, -10n, 10n)).toBe(0n);
    expect(dequantizeFixed(0n, 100n)).toBe(0);
    expect(quantizeFixedUint(-1, 100n, 65535n)).toBe(0n);
    expect(dequantizeFixedUint(0n, 100n)).toBe(0);
  });
});
