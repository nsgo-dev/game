package wire_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*wire.Schema]{
		Decode: wire.Parse,
		Encode: wire.Encode,
		Files:  []string{"*.wire.yaml", "testdata/*.wire.yaml"},
		Doc:    "online/netplay/wire",
	})
}

// TestGeneratedCodecsUpToDate fails when any committed *_wire.go file differs from what GenerateGo produces for its *.wire.yaml schema.
func TestGeneratedCodecsUpToDate(t *testing.T) {
	names, err := wire.BuiltinFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 7 {
		t.Fatalf("expected 7 builtin schemas, got %d (%v)", len(names), names)
	}
	schemas, err := wire.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		var extras []*wire.Schema
		for j, other := range schemas {
			if i != j {
				extras = append(extras, other)
			}
		}
		want, err := wire.GenerateGo(schemas[i], wire.GenerateOptions{
			SourceName:   name,
			Self:         true,
			ExtraSchemas: extras,
		})
		if err != nil {
			t.Fatalf("%s: GenerateGo: %v", name, err)
		}
		goFile := strings.TrimSuffix(name, ".wire.yaml") + "_wire.go"
		got, err := os.ReadFile(goFile)
		if err != nil {
			t.Fatalf("read %s: %v", goFile, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s is out of date; run go generate ./pkg/online/netplay/wire", goFile)
		}
	}
}

// TestGeneratedTSCodecsUpToDate fails when any committed TypeScript codec under ts/ or testdata test codec differs from the generator output.
func TestGeneratedTSCodecsUpToDate(t *testing.T) {
	names, err := wire.BuiltinFiles()
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := wire.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}

	checkFile := func(path string, want []byte) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s is out of date; run go generate ./pkg/online/netplay/wire", path)
		}
	}

	checkFile(filepath.Join("ts", "runtime.ts"), wire.GenerateTSRuntime())
	checkFile(filepath.Join("ts", "index.ts"), wire.GenerateTSIndex(names))

	for i, name := range names {
		var extras []wire.NamedSchema
		for j, other := range schemas {
			if i != j {
				extras = append(extras, wire.NamedSchema{Name: names[j], Schema: other})
			}
		}
		want, err := wire.GenerateTS(schemas[i], wire.TSGenerateOptions{
			SourceName:   name,
			ExtraSchemas: extras,
		})
		if err != nil {
			t.Fatalf("%s: GenerateTS: %v", name, err)
		}
		checkFile(filepath.Join("ts", wire.TSOutputFileName(name)), want)
	}

	// Also verify testdata schemas' Go and TS test codecs stay in sync.
	tdNames := []string{"custom_props.wire.yaml", "edge_cases.wire.yaml"}
	checkFile(filepath.Join("testdata", "ts", "schemas", "runtime.ts"), wire.GenerateTSRuntime())
	checkFile(filepath.Join("testdata", "ts", "schemas", "index.ts"), wire.GenerateTSIndex(tdNames))
	for _, tdName := range tdNames {
		s, err := wire.LoadFile(filepath.Join("testdata", tdName))
		if err != nil {
			t.Fatal(err)
		}
		wantTS, err := wire.GenerateTS(s, wire.TSGenerateOptions{SourceName: tdName})
		if err != nil {
			t.Fatalf("%s: GenerateTS: %v", tdName, err)
		}
		checkFile(filepath.Join("testdata", "ts", "schemas", wire.TSOutputFileName(tdName)), wantTS)

		wantGo, err := wire.GenerateGo(s, wire.GenerateOptions{Package: "wire_test", SourceName: tdName})
		if err != nil {
			t.Fatalf("%s: GenerateGo: %v", tdName, err)
		}
		goName := strings.TrimSuffix(tdName, ".wire.yaml") + "_wire_test.go"
		checkFile(goName, wantGo)
	}
}

func TestTSCodegenDeterministicAndCustomProps(t *testing.T) {
	s, err := wire.LoadFile(filepath.Join("testdata", "custom_props.wire.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := wire.GenerateTS(s, wire.TSGenerateOptions{SourceName: "custom_props.wire.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := wire.GenerateTS(s, wire.TSGenerateOptions{SourceName: "custom_props.wire.yaml"})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("GenerateTS output differed on run %d", i+1)
		}
	}
	src := string(first)
	for _, want := range []string{
		`from "./runtime.ts"`,
		"export interface FighterProps",
		"export function createFighterProps(): FighterProps",
		"export function encodeFighterProps(m: FighterProps, w: Writer): void",
		"export function decodeFighterProps(r: Reader, m: FighterProps): void",
		"export function diffFighterProps(base: FighterProps, cur: FighterProps): number",
		"export function applyFighterProps(base: FighterProps, delta: FighterProps): void",
		"quantizeFloat",
		"dequantizeFloat",
		"quantizeFixedUint",
		"dequantizeFixedUint",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated custom_props TS code missing %q", want)
		}
	}
}

func TestCodegenDeterministicAndCustomProps(t *testing.T) {
	s, err := wire.LoadFile(filepath.Join("testdata", "custom_props.wire.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := wire.GenerateGo(s, wire.GenerateOptions{SourceName: "custom_props.wire.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := wire.GenerateGo(s, wire.GenerateOptions{SourceName: "custom_props.wire.yaml"})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("GenerateGo output differed on run %d", i+1)
		}
	}
	src := string(first)
	for _, want := range []string{
		"package props",
		`"github.com/nsgo-dev/game/pkg/online/netplay/wire"`,
		"func AppendFighterProps(dst []byte, m *FighterProps) []byte",
		"func DecodeFighterProps(src []byte, m *FighterProps) (int, error)",
		"func DiffFighterProps(base, cur *FighterProps) uint8",
		"func ApplyFighterProps(dst, delta *FighterProps)",
		"wire.QuantizeFloat",
		"wire.DequantizeFloat",
		"wire.QuantizeFixedUint",
		"wire.DequantizeFixedUint",
		"wire.BitWriter",
		"wire.BitReader",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated custom_props code missing %q", want)
		}
	}
}

func TestParseValidationErrors(t *testing.T) {
	cases := map[string]string{
		"empty": "",
		"trailing_doc": `version: 1
package: demo
messages:
  - name: A
---
version: 1
`,
		"duplicate_key": `version: 1
version: 1
package: demo
messages:
  - name: A
`,
		"unknown_field": `version: 1
package: demo
bogus: true
messages:
  - name: A
`,
		"bad_package": `version: 1
package: 123bad
messages:
  - name: A
`,
		"bad_qfloat_bounds": `version: 1
package: demo
messages:
  - name: M
    fields:
      - name: x
        type: qfloat
        qfloat:
          min: 10
          max: 5
          bits: 8
`,
		"rest_not_last": `version: 1
package: demo
messages:
  - name: M
    fields:
      - name: a
        type: bytes
        length: rest
      - name: b
        type: u8
`,
		"unknown_type": `version: 1
package: demo
messages:
  - name: M
    fields:
      - name: a
        type: MissingType
`,
		"nibble_tag_overflow": `version: 1
package: demo
messages:
  - name: M
unions:
  - name: U
    discriminator:
      type: nibble
      highBits: 4
      highValue: 2
    cases:
      - tag: 16
        message: M
`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := wire.Parse([]byte(doc)); err == nil {
				t.Fatalf("Parse accepted invalid schema %s", name)
			}
		})
	}
}
