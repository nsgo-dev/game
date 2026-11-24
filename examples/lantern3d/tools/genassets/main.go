// Command genassets deterministically generates the original glTF 2.0 binary scene (.glb) and its embedded textures for the Lantern 3D example under examples/lantern3d/assets/.
//
// All generated assets are original works dedicated to the public domain under CC0-1.0 (see examples/lantern3d/CREDITS.md). This generator uses only the Go standard library and produces byte-for-byte identical output on every run and every architecture: products that are added are converted to their type first, because Go may fuse a multiply and an add into one instruction (it does on arm64, not on amd64), which rounds differently.
//
// Usage:
//
//	go run ./examples/lantern3d/tools/genassets/main.go
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
)

const (
	glbMagic     uint32 = 0x46546C67 // "glTF"
	glbVersion   uint32 = 2
	glbChunkJSON uint32 = 0x4E4F534A // "JSON"
	glbChunkBIN  uint32 = 0x004E4942 // "BIN\0"

	compUnsignedShort = 5123
	compFloat         = 5126

	targetArrayBuffer        = 34962
	targetElementArrayBuffer = 34963

	modeTriangles = 4

	filterLinear             = 9729
	filterLinearMipmapLinear = 9987
	wrapRepeat               = 10497
)

type vec2 [2]float32
type vec3 [3]float32

func (a vec3) add(b vec3) vec3      { return vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func (a vec3) sub(b vec3) vec3      { return vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func (a vec3) scale(s float32) vec3 { return vec3{a[0] * s, a[1] * s, a[2] * s} }
func (a vec3) cross(b vec3) vec3 {
	return vec3{
		float32(a[1]*b[2]) - float32(a[2]*b[1]),
		float32(a[2]*b[0]) - float32(a[0]*b[2]),
		float32(a[0]*b[1]) - float32(a[1]*b[0]),
	}
}
func (a vec3) norm() vec3 {
	l := float32(math.Sqrt(float64(float32(a[0]*a[0]) + float32(a[1]*a[1]) + float32(a[2]*a[2]))))
	if l < 1e-8 {
		return vec3{0, 1, 0}
	}
	return a.scale(1 / l)
}

type primData struct {
	material  int
	positions []vec3
	normals   []vec3
	uvs       []vec2
	indices   []uint16
}

func (p *primData) addQuad(a, b, c, d vec3, uvA, uvB, uvC, uvD vec2) {
	n := b.sub(a).cross(c.sub(a)).norm()
	base := uint16(len(p.positions))
	p.positions = append(p.positions, a, b, c, d)
	p.normals = append(p.normals, n, n, n, n)
	p.uvs = append(p.uvs, uvA, uvB, uvC, uvD)
	p.indices = append(p.indices, base, base+1, base+2, base, base+2, base+3)
}

func (p *primData) addTri(a, b, c vec3, uvA, uvB, uvC vec2) {
	n := b.sub(a).cross(c.sub(a)).norm()
	base := uint16(len(p.positions))
	p.positions = append(p.positions, a, b, c)
	p.normals = append(p.normals, n, n, n)
	p.uvs = append(p.uvs, uvA, uvB, uvC)
	p.indices = append(p.indices, base, base+1, base+2)
}

// addBox appends an axis-aligned CCW box from (minX, minY, minZ) to (maxX, maxY, maxZ).
func (p *primData) addBox(minX, minY, minZ, maxX, maxY, maxZ, uvScale float32) {
	dx, dy, dz := (maxX-minX)*uvScale, (maxY-minY)*uvScale, (maxZ-minZ)*uvScale
	// +Z front
	p.addQuad(
		vec3{minX, minY, maxZ}, vec3{maxX, minY, maxZ}, vec3{maxX, maxY, maxZ}, vec3{minX, maxY, maxZ},
		vec2{0, dy}, vec2{dx, dy}, vec2{dx, 0}, vec2{0, 0},
	)
	// -Z back
	p.addQuad(
		vec3{maxX, minY, minZ}, vec3{minX, minY, minZ}, vec3{minX, maxY, minZ}, vec3{maxX, maxY, minZ},
		vec2{0, dy}, vec2{dx, dy}, vec2{dx, 0}, vec2{0, 0},
	)
	// +X right
	p.addQuad(
		vec3{maxX, minY, maxZ}, vec3{maxX, minY, minZ}, vec3{maxX, maxY, minZ}, vec3{maxX, maxY, maxZ},
		vec2{0, dy}, vec2{dz, dy}, vec2{dz, 0}, vec2{0, 0},
	)
	// -X left
	p.addQuad(
		vec3{minX, minY, minZ}, vec3{minX, minY, maxZ}, vec3{minX, maxY, maxZ}, vec3{minX, maxY, minZ},
		vec2{0, dy}, vec2{dz, dy}, vec2{dz, 0}, vec2{0, 0},
	)
	// +Y top
	p.addQuad(
		vec3{minX, maxY, maxZ}, vec3{maxX, maxY, maxZ}, vec3{maxX, maxY, minZ}, vec3{minX, maxY, minZ},
		vec2{0, dz}, vec2{dx, dz}, vec2{dx, 0}, vec2{0, 0},
	)
	// -Y bottom
	p.addQuad(
		vec3{minX, minY, minZ}, vec3{maxX, minY, minZ}, vec3{maxX, minY, maxZ}, vec3{minX, minY, maxZ},
		vec2{0, dz}, vec2{dx, dz}, vec2{dx, 0}, vec2{0, 0},
	)
}

// addFrustum appends a tapered N-sided prism from y0 (radius r0) to y1 (radius r1).
func (p *primData) addFrustum(cx, cz, y0, y1, r0, r1 float32, sides int, phase, uvScale float32, capTop, capBottom bool) {
	ring0 := make([]vec3, sides)
	ring1 := make([]vec3, sides)
	for i := range sides {
		ang := float64(phase) + float64(i)*2*math.Pi/float64(sides)
		c, s := float32(math.Cos(ang)), float32(math.Sin(ang))
		ring0[i] = vec3{cx + float32(r0*c), y0, cz + float32(r0*s)}
		ring1[i] = vec3{cx + float32(r1*c), y1, cz + float32(r1*s)}
	}
	h := (y1 - y0) * uvScale
	for i := range sides {
		j := (i + 1) % sides
		u0 := float32(i) / float32(sides) * uvScale * 2
		u1 := float32(i+1) / float32(sides) * uvScale * 2
		// Outward CCW winding when viewed from outside:
		// As angle increases from +X toward +Z (Wait: cos(0)=1,sin(0)=0 -> cos(90)=0,sin(90)=1 is clockwise from +Y!). So ring[j] -> ring[i] runs CCW when viewed from +Y, making (ring0[j], ring0[i], ring1[i], ring1[j]) outward CCW.
		p.addQuad(ring0[j], ring0[i], ring1[i], ring1[j],
			vec2{u0, h}, vec2{u1, h}, vec2{u1, 0}, vec2{u0, 0})
	}
	if capTop && r1 > 1e-5 {
		center := vec3{cx, y1, cz}
		for i := range sides {
			j := (i + 1) % sides
			p.addTri(center, ring1[j], ring1[i], vec2{0.5, 0.5}, vec2{1, 0.5}, vec2{0.5, 1})
		}
	}
	if capBottom && r0 > 1e-5 {
		center := vec3{cx, y0, cz}
		for i := range sides {
			j := (i + 1) % sides
			p.addTri(center, ring0[i], ring0[j], vec2{0.5, 0.5}, vec2{1, 0.5}, vec2{0.5, 1})
		}
	}
}

// addBipyramid appends a faceted diamond/crystal with N equatorial sides.
func (p *primData) addBipyramid(cx, cy, cz, radius, halfH float32, sides int, phase float32) {
	top := vec3{cx, cy + halfH, cz}
	bot := vec3{cx, cy - halfH, cz}
	ring := make([]vec3, sides)
	for i := range sides {
		ang := float64(phase) + float64(i)*2*math.Pi/float64(sides)
		ring[i] = vec3{cx + float32(radius*float32(math.Cos(ang))), cy, cz + float32(radius*float32(math.Sin(ang)))}
	}
	for i := range sides {
		j := (i + 1) % sides
		u0 := float32(i) / float32(sides)
		u1 := float32(i+1) / float32(sides)
		p.addTri(ring[j], ring[i], top, vec2{u0, 0.5}, vec2{u1, 0.5}, vec2{(u0 + u1) * 0.5, 0})
		p.addTri(ring[i], ring[j], bot, vec2{u1, 0.5}, vec2{u0, 0.5}, vec2{(u0 + u1) * 0.5, 1})
	}
}

// addPyramid appends a pointed spire from base (y0, radius r0) to apex (tip).
func (p *primData) addPyramid(cx, cz, y0, r0 float32, tip vec3, sides int, phase float32) {
	ring := make([]vec3, sides)
	for i := range sides {
		ang := float64(phase) + float64(i)*2*math.Pi/float64(sides)
		ring[i] = vec3{cx + float32(r0*float32(math.Cos(ang))), y0, cz + float32(r0*float32(math.Sin(ang)))}
	}
	for i := range sides {
		j := (i + 1) % sides
		u0 := float32(i) / float32(sides)
		u1 := float32(i+1) / float32(sides)
		p.addTri(ring[j], ring[i], tip, vec2{u0, 1}, vec2{u1, 1}, vec2{(u0 + u1) * 0.5, 0})
	}
	center := vec3{cx, y0, cz}
	for i := range sides {
		j := (i + 1) % sides
		p.addTri(center, ring[i], ring[j], vec2{0.5, 0.5}, vec2{1, 0.5}, vec2{0.5, 1})
	}
}

// glTF 2.0 JSON structs.
type gltfDoc struct {
	Asset       gltfAsset        `json:"asset"`
	Scene       int              `json:"scene"`
	Scenes      []gltfScene      `json:"scenes"`
	Nodes       []gltfNode       `json:"nodes"`
	Meshes      []gltfMesh       `json:"meshes"`
	Materials   []gltfMaterial   `json:"materials"`
	Textures    []gltfTexture    `json:"textures,omitempty"`
	Images      []gltfImage      `json:"images,omitempty"`
	Samplers    []gltfSampler    `json:"samplers,omitempty"`
	Accessors   []gltfAccessor   `json:"accessors"`
	BufferViews []gltfBufferView `json:"bufferViews"`
	Buffers     []gltfBuffer     `json:"buffers"`
}

type gltfAsset struct {
	Version   string `json:"version"`
	Generator string `json:"generator"`
}

type gltfScene struct {
	Name  string `json:"name"`
	Nodes []int  `json:"nodes"`
}

type gltfNode struct {
	Name        string    `json:"name,omitempty"`
	Mesh        *int      `json:"mesh,omitempty"`
	Children    []int     `json:"children,omitempty"`
	Translation []float32 `json:"translation,omitempty"`
	Rotation    []float32 `json:"rotation,omitempty"`
	Scale       []float32 `json:"scale,omitempty"`
}

type gltfMesh struct {
	Name       string          `json:"name"`
	Primitives []gltfPrimitive `json:"primitives"`
}

type gltfPrimitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    int            `json:"indices"`
	Material   int            `json:"material"`
	Mode       int            `json:"mode"`
}

type gltfMaterial struct {
	Name                 string    `json:"name"`
	PBRMetallicRoughness gltfPBR   `json:"pbrMetallicRoughness"`
	EmissiveFactor       []float32 `json:"emissiveFactor,omitempty"`
	DoubleSided          bool      `json:"doubleSided,omitempty"`
}

type gltfPBR struct {
	BaseColorFactor  []float32        `json:"baseColorFactor"`
	BaseColorTexture *gltfTextureInfo `json:"baseColorTexture,omitempty"`
	MetallicFactor   float32          `json:"metallicFactor"`
	RoughnessFactor  float32          `json:"roughnessFactor"`
}

type gltfTextureInfo struct {
	Index int `json:"index"`
}

type gltfTexture struct {
	Sampler int `json:"sampler"`
	Source  int `json:"source"`
}

type gltfImage struct {
	Name       string `json:"name"`
	BufferView int    `json:"bufferView"`
	MimeType   string `json:"mimeType"`
}

type gltfSampler struct {
	MagFilter int `json:"magFilter"`
	MinFilter int `json:"minFilter"`
	WrapS     int `json:"wrapS"`
	WrapT     int `json:"wrapT"`
}

type gltfAccessor struct {
	BufferView    int       `json:"bufferView"`
	ComponentType int       `json:"componentType"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Min           []float32 `json:"min,omitempty"`
	Max           []float32 `json:"max,omitempty"`
}

type gltfBufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	Target     int `json:"target,omitempty"`
}

type gltfBuffer struct {
	ByteLength int `json:"byteLength"`
}

type glbBuilder struct {
	doc gltfDoc
	bin bytes.Buffer
}

func (b *glbBuilder) padBin4() {
	for b.bin.Len()%4 != 0 {
		b.bin.WriteByte(0)
	}
}

func (b *glbBuilder) addBufferView(data []byte, target int) int {
	b.padBin4()
	off := b.bin.Len()
	b.bin.Write(data)
	idx := len(b.doc.BufferViews)
	b.doc.BufferViews = append(b.doc.BufferViews, gltfBufferView{
		Buffer:     0,
		ByteOffset: off,
		ByteLength: len(data),
		Target:     target,
	})
	return idx
}

func (b *glbBuilder) addPNGImage(name string, img image.Image) (int, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return 0, err
	}
	bv := b.addBufferView(buf.Bytes(), 0)
	imgIdx := len(b.doc.Images)
	b.doc.Images = append(b.doc.Images, gltfImage{
		Name:       name,
		BufferView: bv,
		MimeType:   "image/png",
	})
	texIdx := len(b.doc.Textures)
	b.doc.Textures = append(b.doc.Textures, gltfTexture{
		Sampler: 0,
		Source:  imgIdx,
	})
	return texIdx, nil
}

func (b *glbBuilder) addVec3Accessor(vs []vec3, withBounds bool) int {
	raw := make([]byte, len(vs)*12)
	minV := vec3{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	maxV := vec3{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
	for i, v := range vs {
		binary.LittleEndian.PutUint32(raw[i*12+0:], math.Float32bits(v[0]))
		binary.LittleEndian.PutUint32(raw[i*12+4:], math.Float32bits(v[1]))
		binary.LittleEndian.PutUint32(raw[i*12+8:], math.Float32bits(v[2]))
		for c := range 3 {
			if v[c] < minV[c] {
				minV[c] = v[c]
			}
			if v[c] > maxV[c] {
				maxV[c] = v[c]
			}
		}
	}
	bv := b.addBufferView(raw, targetArrayBuffer)
	acc := gltfAccessor{
		BufferView:    bv,
		ComponentType: compFloat,
		Count:         len(vs),
		Type:          "VEC3",
	}
	if withBounds && len(vs) > 0 {
		acc.Min = []float32{minV[0], minV[1], minV[2]}
		acc.Max = []float32{maxV[0], maxV[1], maxV[2]}
	}
	idx := len(b.doc.Accessors)
	b.doc.Accessors = append(b.doc.Accessors, acc)
	return idx
}

func (b *glbBuilder) addVec2Accessor(vs []vec2) int {
	raw := make([]byte, len(vs)*8)
	for i, v := range vs {
		binary.LittleEndian.PutUint32(raw[i*8+0:], math.Float32bits(v[0]))
		binary.LittleEndian.PutUint32(raw[i*8+4:], math.Float32bits(v[1]))
	}
	bv := b.addBufferView(raw, targetArrayBuffer)
	idx := len(b.doc.Accessors)
	b.doc.Accessors = append(b.doc.Accessors, gltfAccessor{
		BufferView:    bv,
		ComponentType: compFloat,
		Count:         len(vs),
		Type:          "VEC2",
	})
	return idx
}

func (b *glbBuilder) addIndexAccessor(indices []uint16) int {
	raw := make([]byte, len(indices)*2)
	for i, v := range indices {
		binary.LittleEndian.PutUint16(raw[i*2:], v)
	}
	bv := b.addBufferView(raw, targetElementArrayBuffer)
	idx := len(b.doc.Accessors)
	b.doc.Accessors = append(b.doc.Accessors, gltfAccessor{
		BufferView:    bv,
		ComponentType: compUnsignedShort,
		Count:         len(indices),
		Type:          "SCALAR",
	})
	return idx
}

func (b *glbBuilder) addMesh(name string, prims ...primData) int {
	gPrims := make([]gltfPrimitive, 0, len(prims))
	for _, p := range prims {
		posAcc := b.addVec3Accessor(p.positions, true)
		normAcc := b.addVec3Accessor(p.normals, false)
		uvAcc := b.addVec2Accessor(p.uvs)
		idxAcc := b.addIndexAccessor(p.indices)
		gPrims = append(gPrims, gltfPrimitive{
			Attributes: map[string]int{
				"POSITION":   posAcc,
				"NORMAL":     normAcc,
				"TEXCOORD_0": uvAcc,
			},
			Indices:  idxAcc,
			Material: p.material,
			Mode:     modeTriangles,
		})
	}
	idx := len(b.doc.Meshes)
	b.doc.Meshes = append(b.doc.Meshes, gltfMesh{
		Name:       name,
		Primitives: gPrims,
	})
	return idx
}

func (b *glbBuilder) pack() ([]byte, error) {
	b.padBin4()
	binBytes := b.bin.Bytes()
	b.doc.Buffers = []gltfBuffer{{ByteLength: len(binBytes)}}

	jsonBytes, err := json.Marshal(b.doc)
	if err != nil {
		return nil, err
	}
	for len(jsonBytes)%4 != 0 {
		jsonBytes = append(jsonBytes, ' ')
	}

	const maxGLBBytes = 64 << 20 // 64 MiB safety cap for generated example asset
	totalLen64 := uint64(12) + 8 + uint64(len(jsonBytes)) + 8 + uint64(len(binBytes))
	if totalLen64 > maxGLBBytes {
		return nil, fmt.Errorf("glb size %d exceeds limit %d", totalLen64, maxGLBBytes)
	}
	var out bytes.Buffer
	var hdr [20]byte
	binary.LittleEndian.PutUint32(hdr[0:4], glbMagic)
	binary.LittleEndian.PutUint32(hdr[4:8], glbVersion)
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(totalLen64))
	binary.LittleEndian.PutUint32(hdr[12:16], uint32(len(jsonBytes)))
	binary.LittleEndian.PutUint32(hdr[16:20], glbChunkJSON)
	out.Write(hdr[:])
	out.Write(jsonBytes)
	var binHdr [8]byte
	binary.LittleEndian.PutUint32(binHdr[0:4], uint32(len(binBytes)))
	binary.LittleEndian.PutUint32(binHdr[4:8], glbChunkBIN)
	out.Write(binHdr[:])
	out.Write(binBytes)
	return out.Bytes(), nil
}

func hash2D(x, y, seed uint32) uint32 {
	h := x*374761393 + y*668265263 + seed*1442695041
	h = (h ^ (h >> 13)) * 1274126177
	return h ^ (h >> 16)
}

func clampU8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// makeStoneTexture builds a 64x64 procedural flagstone tile texture with mortar joints, subtle bevel highlights, and carved granite grain.
func makeStoneTexture() *image.NRGBA {
	const size = 64
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			row := y / 32
			offX := (x + row*16) % size
			tx, ty := offX%32, y%32
			tileID := uint32((offX/32)*7 + row*13)
			tileTint := int(hash2D(tileID, rowUint(row), 11)%19) - 9
			grain := int(hash2D(uint32(x), uint32(y), 29)%15) - 7

			r := 134 + tileTint + grain
			g := 140 + tileTint + grain
			b := 152 + tileTint + grain

			// Mortar seams and soft bevel edges.
			if tx <= 1 || ty <= 1 {
				r -= 46
				g -= 46
				b -= 42
			} else if tx == 2 || ty == 2 {
				r += 16
				g += 16
				b += 18
			} else if tx == 31 || ty == 31 {
				r -= 18
				g -= 18
				b -= 16
			}
			// Subtle carved inner border on alternating stones.
			if (tx == 6 || tx == 26) && ty >= 6 && ty <= 26 || (ty == 6 || ty == 26) && tx >= 6 && tx <= 26 {
				r -= 10
				g -= 10
				b -= 8
			}
			img.SetNRGBA(x, y, color.NRGBA{R: clampU8(r), G: clampU8(g), B: clampU8(b), A: 255})
		}
	}
	return img
}

func rowUint(v int) uint32 {
	if v < 0 {
		return 0
	}
	return uint32(v)
}

// makeCedarTexture builds a 64x64 warm cedar timber plank texture with wood grain and corner bronze bracket trim.
func makeCedarTexture() *image.NRGBA {
	const size = 64
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			plank := y / 16
			py := y % 16
			wave := int(math.Round(4.5 * math.Sin(float64(float64(x)*0.22)+float64(float64(plank)*1.7))))
			grain := int(hash2D(uint32(x/2), uint32(y), 73)%13) - 6
			plankShift := int(hash2D(uint32(plank), 3, 91)%15) - 7

			r := 148 + plankShift + wave + grain
			g := 96 + plankShift*2/3 + wave/2 + grain/2
			b := 62 + plankShift/2 + grain/3

			// Plank seam
			if py == 0 {
				r -= 48
				g -= 36
				b -= 24
			} else if py == 1 {
				r += 12
				g += 9
				b += 6
			}
			// Frame border trim
			if x < 4 || x >= size-4 || y < 4 || y >= size-4 {
				r = 104 + grain/2
				g = 72 + grain/2
				b = 48 + grain/3
			}
			// Corner bronze rivets
			if (x >= 2 && x <= 5 || x >= size-6 && x <= size-3) && (y >= 2 && y <= 5 || y >= size-6 && y <= size-3) {
				r = 196
				g = 158
				b = 92
			}
			img.SetNRGBA(x, y, color.NRGBA{R: clampU8(r), G: clampU8(g), B: clampU8(b), A: 255})
		}
	}
	return img
}

func quatY(deg float64) []float32 {
	rad := deg * math.Pi / 180.0
	return []float32{0, float32(math.Sin(rad / 2)), 0, float32(math.Cos(rad / 2))}
}

func intPtr(v int) *int { return &v }

func buildLanternShrineGLB() ([]byte, error) {
	b := &glbBuilder{
		doc: gltfDoc{
			Asset: gltfAsset{
				Version:   "2.0",
				Generator: "nsgo-dev/game examples/lantern3d/tools/genassets",
			},
			Scene: 0,
			Samplers: []gltfSampler{{
				MagFilter: filterLinear,
				MinFilter: filterLinearMipmapLinear,
				WrapS:     wrapRepeat,
				WrapT:     wrapRepeat,
			}},
		},
	}

	stoneTex, err := b.addPNGImage("stone_tiles", makeStoneTexture())
	if err != nil {
		return nil, err
	}
	cedarTex, err := b.addPNGImage("cedar_wood", makeCedarTexture())
	if err != nil {
		return nil, err
	}

	const (
		matCourtyard = iota
		matCarvedStone
		matCedar
		matBronzeRoof
		matLanternCore
		matCrystal
		matWispOrb
	)

	b.doc.Materials = []gltfMaterial{
		{
			Name: "CourtyardStone",
			PBRMetallicRoughness: gltfPBR{
				BaseColorFactor:  []float32{0.82, 0.86, 0.92, 1},
				BaseColorTexture: &gltfTextureInfo{Index: stoneTex},
				MetallicFactor:   0.05,
				RoughnessFactor:  0.86,
			},
		},
		{
			Name: "CarvedStone",
			PBRMetallicRoughness: gltfPBR{
				BaseColorFactor:  []float32{0.96, 0.92, 0.85, 1},
				BaseColorTexture: &gltfTextureInfo{Index: stoneTex},
				MetallicFactor:   0.05,
				RoughnessFactor:  0.72,
			},
		},
		{
			Name: "CedarTimber",
			PBRMetallicRoughness: gltfPBR{
				BaseColorFactor:  []float32{1.0, 0.92, 0.82, 1},
				BaseColorTexture: &gltfTextureInfo{Index: cedarTex},
				MetallicFactor:   0.08,
				RoughnessFactor:  0.64,
			},
		},
		{
			Name: "PatinaBronze",
			PBRMetallicRoughness: gltfPBR{
				BaseColorFactor: []float32{0.22, 0.39, 0.37, 1},
				MetallicFactor:  0.55,
				RoughnessFactor: 0.40,
			},
		},
		{
			Name: "LanternFlame",
			PBRMetallicRoughness: gltfPBR{
				BaseColorFactor: []float32{1.0, 0.88, 0.48, 1},
				MetallicFactor:  0.0,
				RoughnessFactor: 0.20,
			},
			EmissiveFactor: []float32{1.0, 0.66, 0.20},
		},
		{
			Name: "AzureCrystal",
			PBRMetallicRoughness: gltfPBR{
				BaseColorFactor: []float32{0.26, 0.86, 0.94, 1},
				MetallicFactor:  0.22,
				RoughnessFactor: 0.18,
			},
			EmissiveFactor: []float32{0.08, 0.28, 0.34},
		},
		{
			Name: "WispFlame",
			PBRMetallicRoughness: gltfPBR{
				BaseColorFactor: []float32{1.0, 0.94, 0.70, 1},
				MetallicFactor:  0.0,
				RoughnessFactor: 0.12,
			},
			EmissiveFactor: []float32{1.0, 0.82, 0.36},
		},
	}

	// Mesh 0: Courtyard ground slab (9m x 9m, slightly thick so underside is solid).
	var ground primData
	ground.material = matCourtyard
	ground.addBox(-4.6, -0.18, -4.6, 4.6, 0.0, 4.6, 0.55)
	meshGround := b.addMesh("CourtyardFloor", ground)

	// Mesh 1: Two-tier octagonal stone dais centered at origin (height 0.32m).
	var dais primData
	dais.material = matCarvedStone
	dais.addFrustum(0, 0, 0.0, 0.16, 1.85, 1.75, 8, math.Pi/8, 1.0, true, true)
	dais.addFrustum(0, 0, 0.16, 0.32, 1.28, 1.20, 8, math.Pi/8, 1.0, true, true)
	meshDais := b.addMesh("CenterDais", dais)

	// Mesh 2: Shrine carved stone pedestal (y = 0..1.10 in shrine local space).
	var pedestal primData
	pedestal.material = matCarvedStone
	pedestal.addBox(-0.56, 0.0, -0.56, 0.56, 0.18, 0.56, 1.0)
	pedestal.addFrustum(0, 0, 0.18, 0.94, 0.40, 0.32, 8, math.Pi/8, 1.2, true, true)
	pedestal.addBox(-0.54, 0.94, -0.54, 0.54, 1.10, 0.54, 1.0)
	meshPedestal := b.addMesh("ShrinePedestal", pedestal)

	// Mesh 3: Lantern timber frame (4 corner posts + upper sill, y = 1.10..1.92).
	var housing primData
	housing.material = matCedar
	const postOff = 0.38
	const postHalf = 0.065
	for _, sx := range []float32{-1, 1} {
		for _, sz := range []float32{-1, 1} {
			cx, cz := sx*postOff, sz*postOff
			housing.addBox(cx-postHalf, 1.10, cz-postHalf, cx+postHalf, 1.86, cz+postHalf, 1.4)
		}
	}
	// Upper frame beam collar
	housing.addBox(-0.50, 1.84, -0.50, 0.50, 1.94, 0.50, 1.2)
	meshHousing := b.addMesh("ShrineHousing", housing)

	// Mesh 4: Pagoda bronze roof + finial (y = 1.94..2.64).
	var roof primData
	roof.material = matBronzeRoof
	// Wide eaves lower tier + sloped upper tier + spire finial
	roof.addFrustum(0, 0, 1.94, 2.18, 0.96, 0.54, 4, math.Pi/4, 1.0, true, true)
	roof.addFrustum(0, 0, 2.18, 2.44, 0.58, 0.14, 4, math.Pi/4, 1.0, true, true)
	roof.addBipyramid(0, 2.54, 0, 0.10, 0.16, 6, 0)
	meshRoof := b.addMesh("ShrineRoof", roof)

	// Mesh 5: Glowing golden-amber lantern crystal core suspended inside housing.
	var core primData
	core.material = matLanternCore
	core.addBipyramid(0, 1.48, 0, 0.25, 0.34, 8, 0)
	meshCore := b.addMesh("ShrineCore", core)

	// Mesh 6: Corner courtyard lantern bollard (multi-primitive: stone base + cedar post + bronze cap).
	var postStone primData
	postStone.material = matCarvedStone
	postStone.addBox(-0.28, 0.0, -0.28, 0.28, 0.26, 0.28, 1.2)
	var postWood primData
	postWood.material = matCedar
	postWood.addBox(-0.16, 0.26, -0.16, 0.16, 0.98, 0.16, 1.4)
	var postCap primData
	postCap.material = matBronzeRoof
	postCap.addFrustum(0, 0, 0.98, 1.18, 0.34, 0.06, 4, math.Pi/4, 1.0, true, true)
	meshCornerPost := b.addMesh("CornerPost", postStone, postWood, postCap)

	// Mesh 7: Offering wooden crate (0.72m cube resting on courtyard).
	var crate primData
	crate.material = matCedar
	crate.addBox(-0.36, 0.0, -0.36, 0.36, 0.72, 0.36, 1.38)
	meshCrate := b.addMesh("OfferingCrate", crate)

	// Mesh 8: Azure crystal cluster on carved stone plinth (multi-primitive).
	var plinth primData
	plinth.material = matCarvedStone
	plinth.addFrustum(0, 0, 0.0, 0.22, 0.44, 0.38, 6, 0, 1.0, true, true)
	var crystals primData
	crystals.material = matCrystal
	crystals.addPyramid(0.0, 0.0, 0.22, 0.17, vec3{0.03, 1.05, -0.02}, 6, 0)
	crystals.addPyramid(-0.18, 0.10, 0.22, 0.12, vec3{-0.30, 0.74, 0.18}, 5, 0.3)
	crystals.addPyramid(0.18, -0.08, 0.22, 0.11, vec3{0.28, 0.68, -0.16}, 5, 1.1)
	meshCrystal := b.addMesh("CrystalCluster", plinth, crystals)

	// Mesh 9: Glowing orbiting light wisp orb.
	var orb primData
	orb.material = matWispOrb
	orb.addBipyramid(0, 0, 0, 0.11, 0.14, 8, 0)
	meshWisp := b.addMesh("LightWisp", orb)

	// Build hierarchical glTF scene graph:
	// Node 0: CourtyardRoot
	//   1: CourtyardFloor
	//   2: CenterDais -> child 10 (LanternShrine)
	//      10: LanternShrine (y=+0.32) -> children 11, 12, 13, 14
	//   3..6: CornerPostNE/NW/SE/SW
	//   7: OfferingCrate
	//   8: CrystalCluster
	//   9: LightWisp (animated in Go to follow the orbiting warm point light)
	b.doc.Nodes = []gltfNode{
		{Name: "CourtyardRoot", Children: []int{1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{Name: "CourtyardFloor", Mesh: intPtr(meshGround)},
		{Name: "CenterDais", Mesh: intPtr(meshDais), Children: []int{10}},
		{Name: "CornerPostNE", Mesh: intPtr(meshCornerPost), Translation: []float32{2.35, 0, -2.35}},
		{Name: "CornerPostNW", Mesh: intPtr(meshCornerPost), Translation: []float32{-2.35, 0, -2.35}},
		{Name: "CornerPostSE", Mesh: intPtr(meshCornerPost), Translation: []float32{2.35, 0, 2.35}},
		{Name: "CornerPostSW", Mesh: intPtr(meshCornerPost), Translation: []float32{-2.35, 0, 2.35}},
		{Name: "OfferingCrate", Mesh: intPtr(meshCrate), Translation: []float32{-1.55, 0, 1.35}, Rotation: quatY(24)},
		{Name: "CrystalCluster", Mesh: intPtr(meshCrystal), Translation: []float32{1.58, 0, 1.12}, Rotation: quatY(-18)},
		{Name: "LightWisp", Mesh: intPtr(meshWisp), Translation: []float32{1.95, 1.90, 0}},
		{Name: "LanternShrine", Translation: []float32{0, 0.32, 0}, Children: []int{11, 12, 13, 14}},
		{Name: "ShrinePedestal", Mesh: intPtr(meshPedestal)},
		{Name: "ShrineHousing", Mesh: intPtr(meshHousing)},
		{Name: "ShrineRoof", Mesh: intPtr(meshRoof)},
		{Name: "ShrineCore", Mesh: intPtr(meshCore)},
	}
	b.doc.Scenes = []gltfScene{{Name: "LanternCourtyard", Nodes: []int{0}}}

	return b.pack()
}

func defaultAssetsDir() string {
	if _, file, _, ok := runtime.Caller(0); ok {
		return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "assets"))
	}
	return filepath.Join("examples", "lantern3d", "assets")
}

func main() {
	outDir := flag.String("out", defaultAssetsDir(), "output directory for generated assets")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "genassets: create dir: %v\n", err)
		os.Exit(1)
	}
	glb, err := buildLanternShrineGLB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "genassets: build glb: %v\n", err)
		os.Exit(1)
	}
	dst := filepath.Join(*outDir, "lantern_shrine.glb")
	if err := os.WriteFile(dst, glb, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "genassets: write %s: %v\n", dst, err)
		os.Exit(1)
	}
	fmt.Printf("generated %s (%d bytes)\n", dst, len(glb))
}
