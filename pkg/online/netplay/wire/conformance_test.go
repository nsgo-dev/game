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
