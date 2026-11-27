package cloudsave_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/cloudsave"
)

const canonicalConfig = `version: 1
maxSlotBytes: 1048576
maxSlots: 32
keepVersions: 5
allowedOrigins:
  - https://play.example.com
  - http://localhost:8080
`

func TestConfigRoundTrip(t *testing.T) {
	c, err := cloudsave.ParseConfig([]byte(canonicalConfig))
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxSlotBytes != 1<<20 || c.MaxSlots != 32 || c.KeepVersions != 5 || len(c.AllowedOrigins) != 2 {
		t.Fatalf("parsed %+v", c)
	}
	out, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != canonicalConfig {
		t.Fatalf("Marshal changed the canonical file:\n%s", out)
	}
	def, err := cloudsave.DefaultConfig().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if want := "version: 1\nmaxSlotBytes: 1048576\nmaxSlots: 32\nkeepVersions: 5\n"; string(def) != want {
		t.Fatalf("default config:\n%s", def)
	}
}

func TestConfigRefusesInvalid(t *testing.T) {
	cases := map[string]string{
		"empty":                     "",
		"unknown field":             "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\ncolour: red\n",
		"newer version":             "version: 2\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\n",
		"no size":                   "version: 1\nmaxSlots: 1\nkeepVersions: 1\n",
		"size too big":              "version: 1\nmaxSlotBytes: 67108865\nmaxSlots: 1\nkeepVersions: 1\n",
		"no slots":                  "version: 1\nmaxSlotBytes: 1\nmaxSlots: 0\nkeepVersions: 1\n",
		"too many slots":            "version: 1\nmaxSlotBytes: 1\nmaxSlots: 10001\nkeepVersions: 1\n",
		"keep none":                 "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 0\n",
		"keep too many":             "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 101\n",
		"origin path":               "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\nallowedOrigins: [https://a.example/]\n",
		"origin scheme":             "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\nallowedOrigins: [ftp://a.example]\n",
		"origin wildcard":           "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\nallowedOrigins: ['*']\n",
		"origin host wildcard":      "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\nallowedOrigins: ['https://*.example.com']\n",
		"origin bare wildcard host": "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\nallowedOrigins: ['https://*']\n",
		"origin port out of range":  "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\nallowedOrigins: ['http://a.example:99999']\n",
		"origin port zero":          "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\nallowedOrigins: ['http://a.example:0']\n",
		"origin trailing colon":     "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\nallowedOrigins: ['http://a.example:']\n",
		"origin empty host":         "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\nallowedOrigins: ['http://:8080']\n",
		"origin twice":              "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\nallowedOrigins: [https://a.example, https://a.example]\n",
		"trailing doc":              "version: 1\nmaxSlotBytes: 1\nmaxSlots: 1\nkeepVersions: 1\n---\nversion: 1\n",
	}
	for name, src := range cases {
		if _, err := cloudsave.ParseConfig([]byte(src)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func FuzzParseConfig(f *testing.F) {
	f.Add([]byte(canonicalConfig))
	f.Add([]byte("version: 1\nmaxSlotBytes: 10\nmaxSlots: 2\nkeepVersions: 3\n"))
	f.Add([]byte("keepVersions: 3\nversion: 1\nmaxSlots: 2\nmaxSlotBytes: 0x10\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := cloudsave.ParseConfig(data)
		if err != nil {
			return
		}
		out, err := c.Marshal()
		if err != nil {
			t.Fatalf("Marshal of a parsed config: %v", err)
		}
		c2, err := cloudsave.ParseConfig(out)
		if err != nil {
			t.Fatalf("reparse of %q: %v", out, err)
		}
		out2, err := c2.Marshal()
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("not stable: %q then %q (%v)", out, out2, err)
		}
		if strings.Contains(string(out), "allowedOrigins: []") {
			t.Fatalf("empty origins written: %q", out)
		}
	})
}
