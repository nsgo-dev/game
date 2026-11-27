package voxel

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// FormatVersion is the current voxel world YAML schema version.
const FormatVersion = 1

// SchemaURL is where the JSON Schema of version 1 voxel world documents is published.
const SchemaURL = "https://game.nsgo.dev/schemas/voxel.v1.json"

// SchemaComment is the first line [Encode] writes so YAML language servers validate
// and complete voxel world files automatically.
const SchemaComment = "# yaml-language-server: $schema=" + SchemaURL

const fixVoxelDoc = "correct the voxel world document to match its version 1 schema in docs/en/voxel.md"

// WorldDoc is the root of a versioned YAML voxel world document.
type WorldDoc struct {
	// Version is the schema version; must be 1.
	Version int `yaml:"version" schema:"required"`
	// Name is an optional human-readable name for the world.
	Name string `yaml:"name,omitempty"`
	// ChunkSize is the power-of-two cubic chunk edge length, from 4 to 64.
	ChunkSize int `yaml:"chunkSize" schema:"required,minimum=4,maximum=64"`
	// VoxelSize is the world-space edge length of one voxel; omitted or 0 defaults to 1.
	VoxelSize float32 `yaml:"voxelSize,omitempty" schema:"exclusiveMinimum=0"`
	// Palette lists the non-air block definitions used by the world, sorted by ID.
	Palette []BlockDoc `yaml:"palette,omitempty"`
	// Chunks lists the non-empty chunks in the world, sorted by (x, y, z).
	Chunks []ChunkDoc `yaml:"chunks,omitempty"`
}

// EffectiveVoxelSize returns doc.VoxelSize, defaulting to [DefaultVoxelSize] (1) when zero.
func (d *WorldDoc) EffectiveVoxelSize() float32 {
	if d == nil || d.VoxelSize == 0 {
		return DefaultVoxelSize
	}
	return d.VoxelSize
}

// BlockDoc describes one non-air block type in a [WorldDoc] palette.
type BlockDoc struct {
	// ID is the non-zero numeric block ID referenced by chunk runs.
	ID BlockID `yaml:"id" schema:"required,minimum=1"`
	// Name is the unique human-readable name of the block type.
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Transparent marks blocks whose neighbors remain visible behind them (such as glass or water).
	Transparent bool `yaml:"transparent,omitempty"`
	// NonSolid marks blocks excluded from solid collision and default raycasts.
	NonSolid bool `yaml:"nonSolid,omitempty"`
	// MaterialID groups emitted faces into SubMesh ranges when meshing.
	MaterialID uint32 `yaml:"materialId,omitempty"`
	// Emissive is the block light level (0..15) emitted into the block-light channel.
	Emissive uint8 `yaml:"emissive,omitempty" schema:"minimum=0,maximum=15"`
	// LightOpacity is the light attenuation (0..15) for transparent blocks; 0 lets direct sky light pass and attenuates propagated light by 1.
	LightOpacity uint8 `yaml:"lightOpacity,omitempty" schema:"minimum=0,maximum=15"`
}

// ChunkDoc stores one non-empty chunk using run-length encoded block runs in (y, z, x) order.
type ChunkDoc struct {
	// X is the chunk's X index in chunk-grid coordinates.
	X int `yaml:"x"`
	// Y is the chunk's Y index in chunk-grid coordinates.
	Y int `yaml:"y"`
	// Z is the chunk's Z index in chunk-grid coordinates.
	Z int `yaml:"z"`
	// Runs holds run-length encoded block tokens ("id*count" or "id" for count 1) in (y, z, x) order whose counts sum to chunkSize^3.
	Runs []string `yaml:"runs" schema:"required,minItems=1"`
}

// Parse decodes and validates a YAML voxel world document. Unknown fields and
// trailing YAML documents are rejected. Returned errors are [*loaderr.Error]
// values carrying line, column, field path, and a fix hint.
func Parse(data []byte) (*WorldDoc, error) {
	return ParseWith(data, "")
}

// ParseWith is [Parse] with an optional source file name attached to errors.
func ParseWith(data []byte, file string) (*WorldDoc, error) {
	doc, err := parseDoc(data)
	if err != nil {
		return nil, loaderr.Locate(err, file, data, fixVoxelDoc)
	}
	return doc, nil
}

func parseDoc(data []byte) (*WorldDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &loaderr.Error{
			Problem: "voxel: empty world document",
			Fix:     fixVoxelDoc,
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc WorldDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("voxel: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, &loaderr.Error{
				Line:    trailingDocumentLine(data),
				Problem: "voxel: unexpected trailing YAML document",
				Fix:     "keep one voxel world per file; remove the --- and the document after it",
			}
		}
		return nil, fmt.Errorf("voxel: decode: %w", err)
	}
	if err := Validate(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func trailingDocumentLine(data []byte) int {
	sawContent := false
	for i, line := range bytes.Split(data, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("---")); ok && (len(rest) == 0 || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\r') {
			if sawContent {
				return i + 1
			}
			continue
		}
		if t := bytes.TrimSpace(line); len(t) > 0 && t[0] != '#' {
			sawContent = true
		}
	}
	return 0
}

// Validate checks that doc conforms to the version 1 voxel world schema.
func Validate(doc *WorldDoc) error {
	if doc == nil {
		return &loaderr.Error{Problem: "voxel: nil world document", Fix: fixVoxelDoc}
	}
	if doc.Version != FormatVersion {
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("voxel: unsupported version %d", doc.Version),
			Expected: strconv.Itoa(FormatVersion),
			Fix:      fmt.Sprintf("set version to %d", FormatVersion),
		}
	}
	if !ValidChunkSize(doc.ChunkSize) {
		return &loaderr.Error{
			Field:    "chunkSize",
			Problem:  fmt.Sprintf("voxel: invalid chunkSize %d", doc.ChunkSize),
			Expected: fmt.Sprintf("a power of two between %d and %d", MinChunkSize, MaxChunkSize),
			Fix:      "use 4, 8, 16, 32, or 64 for chunkSize",
		}
	}
	if doc.VoxelSize != 0 {
		if math.IsNaN(float64(doc.VoxelSize)) || math.IsInf(float64(doc.VoxelSize), 0) || doc.VoxelSize <= 0 {
			return &loaderr.Error{
				Field:    "voxelSize",
				Problem:  fmt.Sprintf("voxel: invalid voxelSize %v", doc.VoxelSize),
				Expected: "a finite positive number",
				Fix:      "set voxelSize to a positive number or omit it to use 1",
			}
		}
	}

	seenID := make(map[BlockID]int, len(doc.Palette))
	seenName := make(map[string]int, len(doc.Palette))
	for i, b := range doc.Palette {
		if b.ID == Air {
			return &loaderr.Error{
				Field:    loaderr.Path("palette", i, "id"),
				Problem:  "voxel: palette block id must be non-zero (0 is reserved for air)",
				Expected: "integer >= 1",
				Fix:      "assign a positive integer ID >= 1",
			}
		}
		if b.Name == "" {
			return &loaderr.Error{
				Field:    loaderr.Path("palette", i, "name"),
				Problem:  "voxel: palette block name must not be empty",
				Expected: "non-empty string",
				Fix:      "give the palette block a unique non-empty name",
			}
		}
		if b.Name == "air" {
			return &loaderr.Error{
				Field:   loaderr.Path("palette", i, "name"),
				Problem: `voxel: palette block name "air" is reserved for block ID 0`,
				Fix:     `rename the block to something other than "air"`,
			}
		}
		if prev, dup := seenID[b.ID]; dup {
			return &loaderr.Error{
				Field:   loaderr.Path("palette", i, "id"),
				Problem: fmt.Sprintf("voxel: duplicate palette block id %d (first defined at palette[%d])", b.ID, prev),
				Fix:     "give every palette entry a distinct id",
			}
		}
		seenID[b.ID] = i
		if prev, dup := seenName[b.Name]; dup {
			return &loaderr.Error{
				Field:   loaderr.Path("palette", i, "name"),
				Problem: fmt.Sprintf("voxel: duplicate palette block name %q (first defined at palette[%d])", b.Name, prev),
				Fix:     "give every palette entry a distinct name",
			}
		}
		seenName[b.Name] = i
		if b.Emissive > MaxLightLevel {
			return &loaderr.Error{
				Field:    loaderr.Path("palette", i, "emissive"),
				Problem:  fmt.Sprintf("voxel: palette block emissive %d exceeds max %d", b.Emissive, MaxLightLevel),
				Expected: fmt.Sprintf("integer in 0..%d", MaxLightLevel),
				Fix:      fmt.Sprintf("set emissive to a value between 0 and %d", MaxLightLevel),
			}
		}
		if b.LightOpacity > MaxLightLevel {
			return &loaderr.Error{
				Field:    loaderr.Path("palette", i, "lightOpacity"),
				Problem:  fmt.Sprintf("voxel: palette block lightOpacity %d exceeds max %d", b.LightOpacity, MaxLightLevel),
				Expected: fmt.Sprintf("integer in 0..%d", MaxLightLevel),
				Fix:      fmt.Sprintf("set lightOpacity to a value between 0 and %d", MaxLightLevel),
			}
		}
	}

	expectedVoxels := doc.ChunkSize * doc.ChunkSize * doc.ChunkSize
	seenChunk := make(map[ChunkCoord]int, len(doc.Chunks))
	for i, ch := range doc.Chunks {
		cc := ChunkCoord{X: ch.X, Y: ch.Y, Z: ch.Z}
		if prev, dup := seenChunk[cc]; dup {
			return &loaderr.Error{
				Field:   loaderr.Path("chunks", i),
				Problem: fmt.Sprintf("voxel: duplicate chunk coordinate (%d, %d, %d) (first defined at chunks[%d])", cc.X, cc.Y, cc.Z, prev),
				Fix:     "remove or merge the duplicate chunk entry",
			}
		}
		seenChunk[cc] = i
		if len(ch.Runs) == 0 {
			return &loaderr.Error{
				Field:    loaderr.Path("chunks", i, "runs"),
				Problem:  "voxel: chunk runs must not be empty",
				Expected: fmt.Sprintf("RLE tokens summing to %d voxels", expectedVoxels),
				Fix:      fmt.Sprintf("provide run tokens whose counts sum to %d", expectedVoxels),
			}
		}
		total := 0
		for j, tok := range ch.Runs {
			_, count, err := parseRunToken(tok)
			if err != nil {
				return &loaderr.Error{
					Field:    loaderr.Path("chunks", i, "runs", j),
					Problem:  fmt.Sprintf("voxel: invalid run token %q: %v", tok, err),
					Expected: `"id" or "id*count" with id in 0..65535 and count >= 1`,
					Fix:      `write each run as "id" or "id*count", such as "1*256"`,
				}
			}
			total += count
			if total > expectedVoxels {
				return &loaderr.Error{
					Field:    loaderr.Path("chunks", i, "runs", j),
					Problem:  fmt.Sprintf("voxel: chunk runs exceed chunk volume (%d > %d)", total, expectedVoxels),
					Expected: fmt.Sprintf("exact total of %d voxels", expectedVoxels),
					Fix:      fmt.Sprintf("adjust run counts so they sum to %d", expectedVoxels),
				}
			}
		}
		if total != expectedVoxels {
			return &loaderr.Error{
				Field:    loaderr.Path("chunks", i, "runs"),
				Problem:  fmt.Sprintf("voxel: chunk runs cover %d voxels, want %d", total, expectedVoxels),
				Expected: fmt.Sprintf("exact total of %d voxels", expectedVoxels),
				Fix:      fmt.Sprintf("adjust run counts so they sum to %d", expectedVoxels),
			}
		}
	}
	return nil
}

// Encode validates doc and serializes it as deterministic 2-space YAML prefixed
// with [SchemaComment].
func Encode(doc *WorldDoc) ([]byte, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(SchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("voxel: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("voxel: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// ToDoc exports w as a canonical deterministic [WorldDoc]: palette entries
// sorted by ID, non-empty chunks sorted by [CompareChunkCoords], and block
// grids compressed with canonical merged run-length encoding.
func (w *World) ToDoc(name string) *WorldDoc {
	if w == nil {
		return &WorldDoc{
			Version:   FormatVersion,
			Name:      name,
			ChunkSize: DefaultChunkSize,
		}
	}
	doc := &WorldDoc{
		Version:   FormatVersion,
		Name:      name,
		ChunkSize: w.chunkSize,
	}
	if w.voxelSize != DefaultVoxelSize {
		doc.VoxelSize = w.voxelSize
	}

	defs := w.registry.All()
	if len(defs) > 0 {
		doc.Palette = make([]BlockDoc, len(defs))
		for i, d := range defs {
			doc.Palette[i] = BlockDoc{
				ID:           d.ID,
				Name:         d.Name,
				Transparent:  d.Transparent,
				NonSolid:     d.NonSolid,
				MaterialID:   d.MaterialID,
				Emissive:     d.Emissive,
				LightOpacity: d.LightOpacity,
			}
		}
	}

	coords := w.ChunkCoords()
	for _, cc := range coords {
		ch := w.chunks[cc]
		if ch == nil || ch.nonAir == 0 {
			continue
		}
		doc.Chunks = append(doc.Chunks, ChunkDoc{
			X:    cc.X,
			Y:    cc.Y,
			Z:    cc.Z,
			Runs: encodeRuns(ch.blocks),
		})
	}
	return doc
}

// FromDoc validates doc and constructs a [World] populated with its palette and
// chunks. Every loaded chunk is marked dirty so [Mesher.RemeshDirty] builds
// initial meshes for the world.
func FromDoc(doc *WorldDoc) (*World, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	reg := NewRegistry()
	for _, b := range doc.Palette {
		if _, err := reg.Register(BlockDef{
			ID:           b.ID,
			Name:         b.Name,
			Transparent:  b.Transparent,
			NonSolid:     b.NonSolid,
			MaterialID:   b.MaterialID,
			Emissive:     b.Emissive,
			LightOpacity: b.LightOpacity,
		}); err != nil {
			return nil, err
		}
	}
	w, err := NewWorld(WorldConfig{
		ChunkSize: doc.ChunkSize,
		VoxelSize: doc.EffectiveVoxelSize(),
		Registry:  reg,
	})
	if err != nil {
		return nil, err
	}

	for _, cd := range doc.Chunks {
		cc := ChunkCoord{X: cd.X, Y: cd.Y, Z: cd.Z}
		ch := newChunk(cc, w.chunkSize)
		pos := 0
		for _, tok := range cd.Runs {
			id, count, err := parseRunToken(tok)
			if err != nil {
				return nil, err
			}
			if id != Air {
				for k := range count {
					ch.blocks[pos+k] = id
				}
				ch.nonAir += count
			}
			pos += count
		}
		if ch.nonAir > 0 {
			w.chunks[cc] = ch
			w.markDirtyChunk(ch)
		}
	}
	w.recomputeLightInternal(false)
	return w, nil
}

// SaveYAML encodes w as deterministic versioned YAML text.
func SaveYAML(w *World, name string) ([]byte, error) {
	return Encode(w.ToDoc(name))
}

// LoadYAML parses YAML bytes and returns the reconstructed [World] and document Name.
func LoadYAML(data []byte) (*World, string, error) {
	return LoadYAMLWith(data, "")
}

// LoadYAMLWith is [LoadYAML] with an optional source file name attached to errors.
func LoadYAMLWith(data []byte, file string) (*World, string, error) {
	doc, err := ParseWith(data, file)
	if err != nil {
		return nil, "", err
	}
	w, err := FromDoc(doc)
	if err != nil {
		return nil, "", loaderr.Locate(err, file, data, fixVoxelDoc)
	}
	return w, doc.Name, nil
}

func encodeRuns(blocks []BlockID) []string {
	if len(blocks) == 0 {
		return nil
	}
	runs := make([]string, 0, 16)
	cur := blocks[0]
	count := 1
	for i := 1; i < len(blocks); i++ {
		if blocks[i] == cur {
			count++
			continue
		}
		runs = append(runs, formatRunToken(cur, count))
		cur = blocks[i]
		count = 1
	}
	runs = append(runs, formatRunToken(cur, count))
	return runs
}

func formatRunToken(id BlockID, count int) string {
	if count == 1 {
		return strconv.FormatUint(uint64(id), 10)
	}
	return strconv.FormatUint(uint64(id), 10) + "*" + strconv.Itoa(count)
}

func parseRunToken(tok string) (BlockID, int, error) {
	idStr, countStr, hasStar := strings.Cut(tok, "*")
	if idStr == "" {
		return 0, 0, fmt.Errorf("missing block id")
	}
	idVal, err := strconv.ParseUint(idStr, 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid block id %q", idStr)
	}
	if !hasStar {
		return BlockID(idVal), 1, nil
	}
	if countStr == "" {
		return 0, 0, fmt.Errorf("missing run count")
	}
	count, err := strconv.Atoi(countStr)
	if err != nil || count < 1 {
		return 0, 0, fmt.Errorf("invalid run count %q", countStr)
	}
	return BlockID(idVal), count, nil
}
