package tilemap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// ChunkFormatVersion is the current schema version of the plain-text chunk YAML and JSON format.
const ChunkFormatVersion = 1

const fixChunkDoc = "correct the tilemap chunk document to match its version 1 format in docs/en/tilemap.md"

// ChunkDoc is the versioned plain-text representation of one sparse tilemap chunk,
// suitable for deterministic YAML or JSON persistence.
type ChunkDoc struct {
	// Version is the schema version; must be 1.
	Version int `yaml:"version" json:"version"`
	// X is the chunk's X coordinate in chunk-grid space (may be negative).
	X int `yaml:"x" json:"x"`
	// Y is the chunk's Y coordinate in chunk-grid space (may be negative).
	Y int `yaml:"y" json:"y"`
	// Size is the power-of-two chunk edge length in tiles.
	Size int `yaml:"size" json:"size"`
	// Runs holds row-major run-length encoded raw tile GIDs (including flip bits),
	// formatted as "gid" (count 1) or "gid*count" (count > 1), whose counts sum to Size*Size.
	Runs []string `yaml:"runs" json:"runs"`
}

// ToDoc converts c into its deterministic ChunkDoc representation.
// Returns nil if c is nil.
func (c *Chunk) ToDoc() *ChunkDoc {
	if c == nil {
		return nil
	}
	sz := c.Size
	if !ValidChunkSize(sz) {
		sz = normalizeChunkSize(sz)
	}
	total := sz * sz
	runs := make([]string, 0, min(total, 64))

	tileAt := func(i int) uint32 {
		if i < len(c.Tiles) {
			return c.Tiles[i]
		}
		return 0
	}

	var cur uint32
	count := 0
	flush := func() {
		if count <= 0 {
			return
		}
		if count == 1 {
			runs = append(runs, strconv.FormatUint(uint64(cur), 10))
		} else {
			runs = append(runs, strconv.FormatUint(uint64(cur), 10)+"*"+strconv.Itoa(count))
		}
	}
	for i := range total {
		gid := tileAt(i)
		if count == 0 {
			cur = gid
			count = 1
			continue
		}
		if gid == cur {
			count++
			continue
		}
		flush()
		cur = gid
		count = 1
	}
	flush()

	return &ChunkDoc{
		Version: ChunkFormatVersion,
		X:       c.Coord.X,
		Y:       c.Coord.Y,
		Size:    sz,
		Runs:    runs,
	}
}

// ChunkFromDoc validates doc and decodes it into a clean Chunk (Dirty=true, Modified=false).
func ChunkFromDoc(doc *ChunkDoc) (*Chunk, error) {
	if err := ValidateChunkDoc(doc); err != nil {
		return nil, err
	}
	total := doc.Size * doc.Size
	tiles := make([]uint32, total)
	pos := 0
	nonEmpty := 0
	for i, token := range doc.Runs {
		gid, count, err := parseChunkRun(token)
		if err != nil {
			return nil, loaderr.Errorf(loaderr.Path("runs", i), fixChunkDoc, "tilemap: chunk: %v", err)
		}
		if gid != 0 {
			nonEmpty += count
		}
		for range count {
			tiles[pos] = gid
			pos++
		}
	}
	return &Chunk{
		Coord:    ChunkCoord{X: doc.X, Y: doc.Y},
		Size:     doc.Size,
		Tiles:    tiles,
		Dirty:    true,
		Modified: false,
		nonEmpty: nonEmpty,
	}, nil
}

// ValidateChunkDoc checks that doc conforms to the version 1 chunk schema.
func ValidateChunkDoc(doc *ChunkDoc) error {
	if doc == nil {
		return &loaderr.Error{Problem: "tilemap: chunk: nil document", Fix: fixChunkDoc}
	}
	if doc.Version != ChunkFormatVersion {
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("tilemap: chunk: unsupported version %d", doc.Version),
			Expected: strconv.Itoa(ChunkFormatVersion),
			Fix:      fmt.Sprintf("set version to %d", ChunkFormatVersion),
		}
	}
	if !ValidChunkSize(doc.Size) {
		return &loaderr.Error{
			Field:    "size",
			Problem:  fmt.Sprintf("tilemap: chunk: invalid size %d", doc.Size),
			Expected: fmt.Sprintf("power of two in [%d, %d]", MinChunkSize, MaxChunkSize),
			Fix:      fmt.Sprintf("set size to a power of two between %d and %d (such as %d)", MinChunkSize, MaxChunkSize, DefaultChunkSize),
		}
	}
	if len(doc.Runs) == 0 {
		return &loaderr.Error{
			Field:    "runs",
			Problem:  "tilemap: chunk: runs must not be empty",
			Expected: fmt.Sprintf("run-length tokens summing to %d", doc.Size*doc.Size),
			Fix:      fixChunkDoc,
		}
	}
	want := doc.Size * doc.Size
	sum := 0
	for i, token := range doc.Runs {
		_, count, err := parseChunkRun(token)
		if err != nil {
			return loaderr.Errorf(loaderr.Path("runs", i), fixChunkDoc, "tilemap: chunk: %v", err)
		}
		if count > want-sum {
			return &loaderr.Error{
				Field:    loaderr.Path("runs", i),
				Problem:  fmt.Sprintf("tilemap: chunk: runs exceed %d tiles (%dx%d)", want, doc.Size, doc.Size),
				Expected: fmt.Sprintf("total run count %d", want),
				Fix:      fixChunkDoc,
			}
		}
		sum += count
	}
	if sum != want {
		return &loaderr.Error{
			Field:    "runs",
			Problem:  fmt.Sprintf("tilemap: chunk: runs cover %d tiles, want %d (%dx%d)", sum, want, doc.Size, doc.Size),
			Expected: fmt.Sprintf("%d", want),
			Fix:      fixChunkDoc,
		}
	}
	return nil
}

func parseChunkRun(token string) (gid uint32, count int, err error) {
	if token == "" {
		return 0, 0, fmt.Errorf("empty run token")
	}
	if strings.TrimSpace(token) != token {
		return 0, 0, fmt.Errorf("run token %q has surrounding whitespace", token)
	}
	gidPart, countPart, hasStar := strings.Cut(token, "*")
	g64, err := strconv.ParseUint(gidPart, 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid tile GID %q: %w", gidPart, err)
	}
	if !hasStar {
		return uint32(g64), 1, nil
	}
	if countPart == "" || strings.Contains(countPart, "*") {
		return 0, 0, fmt.Errorf("invalid run count in %q", token)
	}
	n, err := strconv.Atoi(countPart)
	if err != nil || n <= 0 || n > math.MaxInt32 {
		return 0, 0, fmt.Errorf("invalid run count %q in %q", countPart, token)
	}
	return uint32(g64), n, nil
}

// EncodeChunkYAML validates doc and encodes it as deterministic 2-space YAML.
func EncodeChunkYAML(doc *ChunkDoc) ([]byte, error) {
	if err := ValidateChunkDoc(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("tilemap: chunk: encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("tilemap: chunk: encode yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// DecodeChunkYAML decodes and validates a YAML ChunkDoc, refusing unknown fields and trailing documents.
// Errors are returned as *loaderr.Error values.
func DecodeChunkYAML(data []byte) (*ChunkDoc, error) {
	doc, err := decodeChunkYAML(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixChunkDoc)
	}
	return doc, nil
}

func decodeChunkYAML(data []byte) (*ChunkDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &loaderr.Error{Problem: "tilemap: chunk: empty document", Fix: fixChunkDoc}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc ChunkDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("tilemap: chunk: decode yaml: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, &loaderr.Error{
				Line:    trailingYAMLDocumentLine(data),
				Problem: "tilemap: chunk: unexpected trailing YAML document",
				Fix:     "keep one chunk document per file; remove the --- and anything after it",
			}
		}
		return nil, fmt.Errorf("tilemap: chunk: decode yaml: %w", err)
	}
	if err := ValidateChunkDoc(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func trailingYAMLDocumentLine(data []byte) int {
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

// EncodeChunkJSON validates doc and encodes it as deterministic indented JSON with a trailing newline.
func EncodeChunkJSON(doc *ChunkDoc) ([]byte, error) {
	if err := ValidateChunkDoc(doc); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("tilemap: chunk: encode json: %w", err)
	}
	return append(raw, '\n'), nil
}

// DecodeChunkJSON decodes and validates a JSON ChunkDoc, refusing unknown fields and trailing data.
// Errors are returned as *loaderr.Error values.
func DecodeChunkJSON(data []byte) (*ChunkDoc, error) {
	doc, err := decodeChunkJSON(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixChunkDoc)
	}
	return doc, nil
}

func decodeChunkJSON(data []byte) (*ChunkDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &loaderr.Error{Problem: "tilemap: chunk: empty document", Fix: fixChunkDoc}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var doc ChunkDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("tilemap: chunk: decode json: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, &loaderr.Error{
			Problem: "tilemap: chunk: unexpected trailing JSON data",
			Fix:     "keep one JSON chunk object per file",
		}
	}
	if err := ValidateChunkDoc(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}
