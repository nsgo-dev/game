package resource_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/resource"
)

func TestNewUID(t *testing.T) {
	// 65 bits from nine bytes: the first byte's top five bits make the first character.
	id, err := resource.NewUID(bytes.NewReader([]byte{0xff, 0, 0, 0, 0, 0, 0, 0, 0x80}))
	if err != nil || id != "zw00000000001" {
		t.Fatalf("NewUID = %q, %v", id, err)
	}
	seen := map[string]bool{}
	for range 1000 {
		id, err := resource.NewUID(nil)
		if err != nil || !resource.ValidUID(id) || seen[id] {
			t.Fatalf("NewUID = %q, %v (seen %v)", id, err, seen[id])
		}
		seen[id] = true
	}
	if _, err := resource.NewUID(bytes.NewReader([]byte{1, 2})); err == nil {
		t.Fatal("NewUID from a short reader succeeded")
	}
}

func TestValidUID(t *testing.T) {
	for _, s := range []string{"0000000000000", "zzzzzzzzzzzzz", "mat0000000001", "0123456789abc"} {
		if !resource.ValidUID(s) {
			t.Errorf("ValidUID(%q) = false", s)
		}
	}
	for _, s := range []string{"", "000000000000", "00000000000000", "000000000000i", "000000000000l", "000000000000o", "000000000000u", "000000000000A", "000000000000-", "00000000000é"} {
		if resource.ValidUID(s) {
			t.Errorf("ValidUID(%q) = true", s)
		}
	}
}

func TestHeaderUID(t *testing.T) {
	const src = "type: curve\nversion: 1\nuid: crv0000000001\nkeys:\n  - time: 0\n    value: 1\n"
	c, err := resource.ParseAs[*resource.Curve]([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if c.UID != "crv0000000001" {
		t.Fatalf("UID = %q", c.UID)
	}
	out, err := resource.Encode(c)
	if err != nil || string(out) != src {
		t.Fatalf("Encode = %q, %v; want the uid after the version, unchanged", out, err)
	}
	if _, err := resource.Parse([]byte(strings.Replace(src, "crv0000000001", "crv000000000u", 1))); err == nil || !strings.Contains(err.Error(), `uid "crv000000000u" must be 13 characters`) {
		t.Fatalf("Parse with a bad uid: %v", err)
	}
	c.UID = "x"
	if _, err := resource.Encode(c); err == nil {
		t.Fatal("Encode with a bad uid succeeded")
	}
}
