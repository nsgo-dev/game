// Package wire defines the YAML wire message schema format for pkg/online/netplay and game protocols, integer-only quantization helpers, bit-packing primitives, and deterministic zero-allocation Go and TypeScript codec generators.
//
// Schema files declare fixed-width integers (with explicit big- or little-endian byte order), varints, zigzag signed varints, booleans, raw float32 values, integer-quantized qfloat and fixed-point numbers, length-prefixed or trailing bytes and strings, fixed and variable arrays, enums, MSB-first bit-packed fields, presence-bitmask groups with optional delta diff/apply generation, and tag- or nibble-discriminated unions.
//
// Run game wire (directly or through go generate) to produce deterministic Go codecs (AppendX / DecodeX) and dependency-free TypeScript codecs (encodeX / decodeX, plus diffX / applyX for delta masks) that fill caller-owned storage without per-packet heap allocations.
package wire

//go:generate go run ../../../../cmd/game wire -in . -out . -self
//go:generate go run ../../../../cmd/game wire -lang ts -in . -out ts

import (
	"embed"
	"io/fs"
	"slices"
	"strings"
)

// BuiltinFS embeds the canonical YAML wire schemas describing every pkg/net wire format (transport, UDP sealed datagrams, handshake, relay, replication v2, lockstep/rollback, and membership).
//
//go:embed *.wire.yaml
var BuiltinFS embed.FS

// BuiltinFiles returns the sorted file names of the embedded pkg/net wire schemas.
func BuiltinFiles() ([]string, error) {
	entries, err := fs.ReadDir(BuiltinFS, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".wire.yaml") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

// LoadBuiltins parses and validates all embedded pkg/net wire schemas in sorted file order.
func LoadBuiltins() ([]*Schema, error) {
	names, err := BuiltinFiles()
	if err != nil {
		return nil, err
	}
	out := make([]*Schema, 0, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(BuiltinFS, name)
		if err != nil {
			return nil, err
		}
		s, err := Parse(data)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := ValidateAll(out); err != nil {
		return nil, err
	}
	return out, nil
}
