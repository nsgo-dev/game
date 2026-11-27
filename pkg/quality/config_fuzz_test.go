package quality_test

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/quality"
)

func FuzzParseConfig(f *testing.F) {
	f.Add([]byte(defaultYAML))
	if data, err := os.ReadFile("testdata/quality.yaml"); err == nil {
		f.Add(data)
	}
	f.Add([]byte("version: 1\npresets: {low: {resolution_scale: 0.1, particle_scale: 1e-3}}\n"))
	f.Add([]byte("version: 1\n---\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := quality.ParseConfig(data)
		if err != nil {
			return
		}
		out, err := quality.EncodeConfig(c)
		if err != nil {
			t.Fatalf("EncodeConfig of a parsed config: %v", err)
		}
		back, err := quality.ParseConfig(out)
		if err != nil {
			t.Fatalf("ParseConfig of encoded output: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(normalize(back), normalize(c)) {
			t.Fatalf("round trip changed the config:\n%+v\n%+v", c, back)
		}
		again, err := quality.EncodeConfig(back)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("second encode differs (%v):\n%s\n%s", err, out, again)
		}
	})
}

// normalize drops the difference between no device rules and an empty list, which encode the same.
func normalize(c quality.Config) quality.Config {
	if len(c.Devices) == 0 {
		c.Devices = nil
	}
	return c
}
