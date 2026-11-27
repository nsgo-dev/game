package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/jsonschema"
	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
)

func TestWireSchemaAcceptsBuiltinsAndCustomProps(t *testing.T) {
	s := schemaAt(t, wire.SchemaURL)

	names, err := wire.BuiltinFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		data, err := fs.ReadFile(wire.BuiltinFS, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ValidateYAML(data); err != nil {
			t.Errorf("%s failed wire.v1.json validation: %v", name, err)
		}
	}

	custom, err := os.ReadFile(filepath.Join(root, "pkg", "online", "netplay", "wire", "testdata", "custom_props.wire.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateYAML(custom); err != nil {
		t.Errorf("custom_props.wire.yaml failed wire.v1.json validation: %v", err)
	}
}

func TestWireSchemaRefusesMistakes(t *testing.T) {
	s := schemaAt(t, wire.SchemaURL)
	for name, tc := range map[string]struct{ yaml, issue string }{
		"missing version": {"package: demo\nmessages: [{name: A}]\n", `/: missing required key "version"`},
		"future version":  {"version: 2\npackage: demo\nmessages: [{name: A}]\n", "/version: 2 is not the allowed value 1"},
		"unknown top key": {"version: 1\npackage: demo\nextra: 1\n", `/extra: unknown key "extra"`},
		"bad byte order":  {"version: 1\npackage: demo\norder: middle\n", `/order: "middle" is not one of "be", "le"`},
		"qfloat bits 0":   {"version: 1\npackage: demo\nmessages: [{name: M, fields: [{name: x, type: qfloat, qfloat: {min: 0, max: 1, bits: 0}}]}]\n", "/messages/0/fields/0/qfloat/bits: 0 is less than the minimum 1"},
		"fixed scale 0":   {"version: 1\npackage: demo\nmessages: [{name: M, fields: [{name: x, type: fixed, fixed: {scale: 0, type: i16}}]}]\n", "/messages/0/fields/0/fixed/scale: 0 is less than the minimum 1"},
		"bad discrim":     {"version: 1\npackage: demo\nunions: [{name: U, discriminator: {type: u32}, cases: [{tag: 1, message: M}]}]\n", `/unions/0/discriminator/type: "u32" is not one of`},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := wire.Parse([]byte(tc.yaml)); err == nil {
				t.Fatalf("wire.Parse accepted %q", tc.yaml)
			}
			err := s.ValidateYAML([]byte(tc.yaml))
			var verr *jsonschema.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("ValidateYAML = %v, want *ValidationError", err)
			}
			if !strings.Contains(verr.Error(), tc.issue) {
				t.Fatalf("issues %q do not include %q", verr.Issues, tc.issue)
			}
		})
	}
}
