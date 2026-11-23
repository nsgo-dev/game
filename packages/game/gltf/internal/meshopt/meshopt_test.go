package meshopt

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path"
	"testing"
)

// The streams and expected outputs below are meshoptimizer's own reference vectors (demo/tests.cpp, MIT), which its scalar and SIMD decoders must both reproduce.

var refVertices = []struct {
	px, py, pz uint16
	nu, nv     uint8
	tx, ty     uint16
}{
	{0, 0, 0, 0, 0, 0, 0},
	{300, 0, 0, 0, 0, 500, 0},
	{0, 300, 0, 0, 0, 0, 500},
	{300, 300, 0, 0, 0, 500, 500},
}

func refVertexBytes() []byte {
	var out []byte
	for _, v := range refVertices {
		out = binary.LittleEndian.AppendUint16(out, v.px)
		out = binary.LittleEndian.AppendUint16(out, v.py)
		out = binary.LittleEndian.AppendUint16(out, v.pz)
		out = append(out, v.nu, v.nv)
		out = binary.LittleEndian.AppendUint16(out, v.tx)
		out = binary.LittleEndian.AppendUint16(out, v.ty)
	}
	return out
}

var vertexDataV0 = []byte{
	0xa0, 0x01, 0x3f, 0x00, 0x00, 0x00, 0x58, 0x57, 0x58, 0x01, 0x26, 0x00, 0x00, 0x00, 0x01, 0x0c,
	0x00, 0x00, 0x00, 0x58, 0x01, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x3f, 0x00,
	0x00, 0x00, 0x17, 0x18, 0x17, 0x01, 0x26, 0x00, 0x00, 0x00, 0x01, 0x0c, 0x00, 0x00, 0x00, 0x17,
	0x01, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00,
}

var vertexDataV1 = []byte{
	0xa1, 0xee, 0xaa, 0xee, 0x00, 0x4b, 0x4b, 0x4b, 0x00, 0x00, 0x4b, 0x00, 0x00, 0x7d, 0x7d, 0x7d,
	0x00, 0x00, 0x7d, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x62, 0x00, 0x62,
}

var vertexDataV1Custom = []byte{
	0xa1, 0xd4, 0x94, 0xd4, 0x01, 0x0e, 0x00, 0x58, 0x57, 0x58, 0x02, 0x02, 0x12, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x58, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x02, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x0e, 0x00, 0x7d, 0x7d, 0x7d, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7d, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x62,
}

func TestDecodeVertexBufferReference(t *testing.T) {
	want := refVertexBytes()
	for name, stream := range map[string][]byte{"v0": vertexDataV0, "v1": vertexDataV1, "v1-custom": vertexDataV1Custom} {
		got := make([]byte, len(want))
		if err := DecodeVertexBuffer(got, 4, 12, stream); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: decoded %v, want %v", name, got, want)
		}
	}
}

func TestDecodeVertexBufferRejectsMalformed(t *testing.T) {
	dst := make([]byte, 64)
	for i := 0; i < len(vertexDataV0); i++ {
		if err := DecodeVertexBuffer(dst, 4, 12, vertexDataV0[:i]); err == nil {
			t.Fatalf("truncated to %d bytes: want error", i)
		}
	}
	extra := append(append([]byte{}, vertexDataV0...), 0)
	if err := DecodeVertexBuffer(dst, 4, 12, extra); !errors.Is(err, ErrData) {
		t.Fatalf("trailing byte: got %v, want ErrData", err)
	}
	bad := append([]byte{}, vertexDataV0...)
	bad[0] = 0xa2
	if err := DecodeVertexBuffer(dst, 4, 12, bad); !errors.Is(err, ErrHeader) {
		t.Fatalf("version 2: got %v, want ErrHeader", err)
	}
	if err := DecodeVertexBuffer(dst, 4, 6, vertexDataV0); !errors.Is(err, ErrLayout) {
		t.Fatalf("size 6: got %v, want ErrLayout", err)
	}
}

func indicesFromBytes(b []byte, size int) []uint32 {
	out := make([]uint32, len(b)/size)
	for i := range out {
		if size == 2 {
			out[i] = uint32(binary.LittleEndian.Uint16(b[2*i:]))
		} else {
			out[i] = binary.LittleEndian.Uint32(b[4*i:])
		}
	}
	return out
}

func TestDecodeIndexBufferReference(t *testing.T) {
	cases := []struct {
		name   string
		stream []byte
		want   []uint32
	}{
		{"v0", []byte{
			0xe0, 0xf0, 0x10, 0xfe, 0xff, 0xf0, 0x0c, 0xff, 0x02, 0x02, 0x02, 0x00, 0x76, 0x87, 0x56, 0x67,
			0x78, 0xa9, 0x86, 0x65, 0x89, 0x68, 0x98, 0x01, 0x69, 0x00, 0x00,
		}, []uint32{0, 1, 2, 2, 1, 3, 4, 6, 5, 7, 8, 9}},
		{"v1-tricky", []byte{
			0xe1, 0xf0, 0x10, 0xfe, 0x1f, 0x3d, 0x00, 0x0a, 0x00, 0x76, 0x87, 0x56, 0x67, 0x78, 0xa9, 0x86,
			0x65, 0x89, 0x68, 0x98, 0x01, 0x69, 0x00, 0x00,
		}, []uint32{0, 1, 2, 2, 1, 3, 0, 1, 2, 2, 1, 5, 2, 1, 4}},
		{"v1-more", []byte{
			0xe1, 0xf0, 0x10, 0xfe, 0xff, 0xf0, 0x0c, 0xff, 0x02, 0x02, 0x02, 0x00, 0x76, 0x87, 0x56, 0x67,
			0x78, 0xa9, 0x86, 0x65, 0x89, 0x68, 0x98, 0x01, 0x69, 0x00, 0x00,
		}, []uint32{0, 1, 2, 2, 1, 3, 4, 6, 5, 7, 8, 9}},
		{"v1-three-edges", []byte{
			0xe1, 0xf0, 0x20, 0x30, 0x40, 0x00, 0x76, 0x87, 0x56, 0x67, 0x78, 0xa9, 0x86, 0x65, 0x89, 0x68,
			0x98, 0x01, 0x69, 0x00, 0x00,
		}, []uint32{0, 1, 2, 1, 0, 3, 2, 1, 4, 0, 2, 5}},
	}
	for _, tc := range cases {
		for _, size := range []int{2, 4} {
			dst := make([]byte, len(tc.want)*size)
			if err := DecodeIndexBuffer(dst, len(tc.want), size, tc.stream); err != nil {
				t.Fatalf("%s/%d: %v", tc.name, size, err)
			}
			if got := indicesFromBytes(dst, size); !equalU32(got, tc.want) {
				t.Fatalf("%s/%d: got %v, want %v", tc.name, size, got, tc.want)
			}
			for i := 0; i < len(tc.stream); i++ {
				if err := DecodeIndexBuffer(dst, len(tc.want), size, tc.stream[:i]); err == nil {
					t.Fatalf("%s/%d truncated to %d: want error", tc.name, size, i)
				}
			}
		}
	}
}

func TestDecodeIndexSequenceReference(t *testing.T) {
	stream := []byte{0xd1, 0x00, 0x04, 0xcd, 0x01, 0x04, 0x07, 0x98, 0x1f, 0x00, 0x00, 0x00, 0x00}
	want := []uint32{0, 1, 51, 2, 49, 1000}
	dst := make([]byte, 4*len(want))
	if err := DecodeIndexSequence(dst, len(want), 4, stream); err != nil {
		t.Fatal(err)
	}
	if got := indicesFromBytes(dst, 4); !equalU32(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := DecodeIndexSequence(dst, len(want), 4, append(append([]byte{}, stream...), 0)); !errors.Is(err, ErrData) {
		t.Fatalf("trailing byte: got %v, want ErrData", err)
	}
	if err := DecodeIndexSequence(dst, len(want), 4, append([]byte{0xe1}, stream[1:]...)); !errors.Is(err, ErrHeader) {
		t.Fatalf("triangle header: got %v, want ErrHeader", err)
	}
}

func equalU32(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func u16s(v ...uint16) []byte {
	var out []byte
	for _, x := range v {
		out = binary.LittleEndian.AppendUint16(out, x)
	}
	return out
}

func TestDecodeFiltersReference(t *testing.T) {
	oct8 := []byte{0, 1, 127, 0, 0, 187, 127, 1, 255, 1, 127, 0, 14, 130, 127, 1}
	if err := DecodeFilterOct(oct8, 4, 4); err != nil {
		t.Fatal(err)
	}
	if want := []byte{0, 1, 127, 0, 0, 159, 82, 1, 255, 1, 127, 0, 1, 130, 241, 1}; !bytes.Equal(oct8, want) {
		t.Fatalf("oct8: got %v, want %v", oct8, want)
	}

	oct12 := u16s(0, 1, 2047, 0, 0, 1870, 2047, 1, 2017, 1, 2047, 0, 14, 1300, 2047, 1)
	if err := DecodeFilterOct(oct12, 4, 8); err != nil {
		t.Fatal(err)
	}
	if want := u16s(0, 16, 32767, 0, 0, 32621, 3088, 1, 32764, 16, 471, 0, 307, 28541, 16093, 1); !bytes.Equal(oct12, want) {
		t.Fatalf("oct12: got %v, want %v", oct12, want)
	}

	quat := u16s(0, 1, 0, 0x7fc, 0, 1870, 0, 0x7fd, 2017, 1, 0, 0x7fe, 14, 1300, 0, 0x7ff)
	if err := DecodeFilterQuat(quat, 4, 8); err != nil {
		t.Fatal(err)
	}
	if want := u16s(32767, 0, 11, 0, 0, 25013, 0, 21166, 11, 0, 23504, 22830, 158, 14715, 0, 29277); !bytes.Equal(quat, want) {
		t.Fatalf("quat: got %v, want %v", quat, want)
	}

	var exp []byte
	for _, v := range []uint32{0, 0xff000003, 0x02fffff7, 0xfe7fffff} {
		exp = binary.LittleEndian.AppendUint32(exp, v)
	}
	if err := DecodeFilterExp(exp, 4, 4); err != nil {
		t.Fatal(err)
	}
	for i, want := range []uint32{0, 0x3fc00000, 0xc2100000, 0x49fffffe} {
		if got := binary.LittleEndian.Uint32(exp[4*i:]); got != want {
			t.Fatalf("exp[%d]: got %#x, want %#x", i, got, want)
		}
	}
}

func TestDecodeFilterColor(t *testing.T) {
	// y=100, co=20, cg=-30 with a full-scale alpha: r = y+co-cg, g = y+cg, b = y-co-cg at unit scale.
	c8 := []byte{100, 20, 226, 255}
	if err := DecodeFilterColor(c8, 1, 4); err != nil {
		t.Fatal(err)
	}
	if want := []byte{150, 70, 110, 255}; !bytes.Equal(c8, want) {
		t.Fatalf("color8: got %v, want %v", c8, want)
	}
	c16 := u16s(1000, 200, 0xffff-299, 0xffff)
	if err := DecodeFilterColor(c16, 1, 8); err != nil {
		t.Fatal(err)
	}
	if want := u16s(1500, 700, 1100, 0xffff); !bytes.Equal(c16, want) {
		t.Fatalf("color16: got %v, want %v", c16, want)
	}
	if err := DecodeFilterColor(make([]byte, 3), 1, 4); !errors.Is(err, ErrLayout) {
		t.Fatalf("short color: got %v, want ErrLayout", err)
	}
}

func FuzzDecodeVertexBuffer(f *testing.F) {
	f.Add(vertexDataV0, uint8(4), uint8(12))
	f.Add(vertexDataV1, uint8(4), uint8(12))
	f.Add(vertexDataV1Custom, uint8(4), uint8(12))
	f.Fuzz(func(t *testing.T, data []byte, count, size uint8) {
		n, s := int(count), int(size)&^3
		if s == 0 {
			s = 4
		}
		dst := make([]byte, n*s)
		_ = DecodeVertexBuffer(dst, n, s, data)
		_ = DecodeFilterOct(dst, len(dst)/8, 8)
		_ = DecodeFilterQuat(dst, len(dst)/8, 8)
		_ = DecodeFilterExp(dst, n, s)
		_ = DecodeFilterColor(dst, len(dst)/4, 4)
	})
}

func FuzzDecodeIndexBuffer(f *testing.F) {
	f.Add([]byte{
		0xe1, 0xf0, 0x10, 0xfe, 0x1f, 0x3d, 0x00, 0x0a, 0x00, 0x76, 0x87, 0x56, 0x67, 0x78, 0xa9, 0x86,
		0x65, 0x89, 0x68, 0x98, 0x01, 0x69, 0x00, 0x00,
	}, uint8(5))
	f.Add([]byte{0xd1, 0x00, 0x04, 0xcd, 0x01, 0x04, 0x07, 0x98, 0x1f, 0x00, 0x00, 0x00, 0x00}, uint8(2))
	f.Fuzz(func(t *testing.T, data []byte, triangles uint8) {
		n := 3 * int(triangles)
		dst := make([]byte, 4*n)
		_ = DecodeIndexBuffer(dst, n, 4, data)
		_ = DecodeIndexBuffer(dst, n, 2, data)
		_ = DecodeIndexSequence(dst, n, 4, data)
		_ = DecodeIndexSequence(dst, n, 2, data)
	})
}

// khronosView is one compressed bufferView of the MeshoptCubeTest sample with its uncompressed fallback bytes.
type khronosView struct {
	mode, filter  string
	count, stride int
	src, fallback []byte
}

// khronosViews reads every compressed bufferView of MeshoptCubeTest from the gltf package's testdata. The glTF variant carries the uncompressed fallback buffer next to the compressed one.
func khronosViews(t testing.TB) []khronosView {
	t.Helper()
	dir := path.Join("..", "..", "testdata", "khronos", "MeshoptCubeTest", "glTF")
	raw, err := os.ReadFile(path.Join(dir, "MeshoptCubeTest.gltf"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Buffers []struct {
			URI string `json:"uri"`
		} `json:"buffers"`
		BufferViews []struct {
			Buffer     int `json:"buffer"`
			ByteOffset int `json:"byteOffset"`
			ByteLength int `json:"byteLength"`
			Extensions struct {
				Meshopt *struct {
					Buffer     int    `json:"buffer"`
					ByteOffset int    `json:"byteOffset"`
					ByteLength int    `json:"byteLength"`
					ByteStride int    `json:"byteStride"`
					Count      int    `json:"count"`
					Mode       string `json:"mode"`
					Filter     string `json:"filter"`
				} `json:"KHR_meshopt_compression"`
			} `json:"extensions"`
		} `json:"bufferViews"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	bufs := make([][]byte, len(doc.Buffers))
	for i, b := range doc.Buffers {
		if bufs[i], err = os.ReadFile(path.Join(dir, b.URI)); err != nil {
			t.Fatal(err)
		}
	}
	var out []khronosView
	for _, bv := range doc.BufferViews {
		m := bv.Extensions.Meshopt
		if m == nil {
			continue
		}
		out = append(out, khronosView{
			mode: m.Mode, filter: m.Filter, count: m.Count, stride: m.ByteStride,
			src:      bufs[m.Buffer][m.ByteOffset : m.ByteOffset+m.ByteLength],
			fallback: bufs[bv.Buffer][bv.ByteOffset : bv.ByteOffset+bv.ByteLength],
		})
	}
	return out
}

func decodeView(v khronosView, dst []byte) error {
	switch v.mode {
	case "TRIANGLES":
		return DecodeIndexBuffer(dst, v.count, v.stride, v.src)
	case "INDICES":
		return DecodeIndexSequence(dst, v.count, v.stride, v.src)
	}
	return DecodeVertexBuffer(dst, v.count, v.stride, v.src)
}

// TestDecodeKhronosViews checks every stream of the Khronos sample against its uncompressed fallback: attribute and index sequence streams byte for byte, triangle streams up to the rotation the codec may apply to each triangle. Filtered views are checked by the gltf package's tests, since filters are lossy.
func TestDecodeKhronosViews(t *testing.T) {
	views := khronosViews(t)
	if len(views) == 0 {
		t.Fatal("no compressed views found")
	}
	for i, v := range views {
		if v.filter != "" && v.filter != "NONE" {
			continue
		}
		dst := make([]byte, v.count*v.stride)
		if err := decodeView(v, dst); err != nil {
			t.Fatalf("view %d (%s): %v", i, v.mode, err)
		}
		if v.mode != "TRIANGLES" {
			if !bytes.Equal(dst, v.fallback) {
				t.Fatalf("view %d (%s) differs from the fallback", i, v.mode)
			}
			continue
		}
		got, want := indicesFromBytes(dst, v.stride), indicesFromBytes(v.fallback, v.stride)
		for f := 0; f < len(want); f += 3 {
			a, b := got[f:f+3], want[f:f+3]
			if !equalU32(a, b) && !equalU32(a, []uint32{b[1], b[2], b[0]}) && !equalU32(a, []uint32{b[2], b[0], b[1]}) {
				t.Fatalf("view %d triangle %d is %v, want a rotation of %v", i, f/3, a, b)
			}
		}
	}
}

func BenchmarkDecodeKhronosViews(b *testing.B) {
	views := khronosViews(b)
	dst := make([][]byte, len(views))
	var total int64
	for i, v := range views {
		dst[i] = make([]byte, v.count*v.stride)
		total += int64(len(dst[i]))
	}
	b.SetBytes(total)
	for b.Loop() {
		for i, v := range views {
			if err := decodeView(v, dst[i]); err != nil {
				b.Fatal(err)
			}
		}
	}
}
