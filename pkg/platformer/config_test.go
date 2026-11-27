package platformer_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/platformer"
)

const defaultYAML = `version: 1
coyote_time: 100ms
jump_buffer: 100ms
jump_speed: 284
jump_cut: 0.55
jump_cut_min_speed: 110
gravity: 720
max_fall_speed: 320
run_speed: 98
ground_accel: 960
ground_decel: 1150
air_accel: 680
wall_slide_speed: 60
wall_coyote_time: 80ms
wall_jump_speed_x: 150
wall_jump_speed_y: 260
wall_jump_lock: 150ms
`

func TestEncodeDefaultConfig(t *testing.T) {
	data, err := platformer.EncodeConfig(platformer.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != defaultYAML {
		t.Fatalf("EncodeConfig(DefaultConfig()) =\n%s\nwant\n%s", data, defaultYAML)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	c, err := platformer.ParseConfig([]byte(defaultYAML))
	if err != nil {
		t.Fatal(err)
	}
	if c != platformer.DefaultConfig() {
		t.Fatalf("ParseConfig = %+v, want DefaultConfig", c)
	}
	data, err := platformer.EncodeConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != defaultYAML {
		t.Fatalf("round trip changed the file:\n%s", data)
	}
}

func TestParseConfigRefuses(t *testing.T) {
	for name, doc := range map[string]string{
		"empty":            "  \n",
		"unknown field":    defaultYAML + "double_jump: true\n",
		"missing version":  strings.Replace(defaultYAML, "version: 1\n", "", 1),
		"future version":   strings.Replace(defaultYAML, "version: 1", "version: 2", 1),
		"integer duration": strings.Replace(defaultYAML, "coyote_time: 100ms", "coyote_time: 100", 1),
		"bad duration":     strings.Replace(defaultYAML, "coyote_time: 100ms", "coyote_time: soon", 1),
		"negative time":    strings.Replace(defaultYAML, "jump_buffer: 100ms", "jump_buffer: -1ms", 1),
		"negative speed":   strings.Replace(defaultYAML, "gravity: 720", "gravity: -720", 1),
		"nan":              strings.Replace(defaultYAML, "jump_speed: 284", "jump_speed: .nan", 1),
		"infinity":         strings.Replace(defaultYAML, "max_fall_speed: 320", "max_fall_speed: .inf", 1),
		"cut above one":    strings.Replace(defaultYAML, "jump_cut: 0.55", "jump_cut: 1.5", 1),
		"trailing doc":     defaultYAML + "---\nversion: 1\n",
		"not a mapping":    "- 1\n",
	} {
		if _, err := platformer.ParseConfig([]byte(doc)); err == nil {
			t.Errorf("%s: ParseConfig accepted\n%s", name, doc)
		}
	}
}

func TestEncodeConfigRefusesInvalid(t *testing.T) {
	c := platformer.DefaultConfig()
	c.WallJumpLock = -1
	if _, err := platformer.EncodeConfig(c); err == nil {
		t.Fatal("EncodeConfig accepted a negative wall_jump_lock")
	}
}

func FuzzParseConfig(f *testing.F) {
	f.Add([]byte(defaultYAML))
	f.Add([]byte("version: 1\ncoyote_time: 1.5s\njump_cut: 1\n"))
	f.Add([]byte("version: 1\njump_speed: 1e-7\nwall_jump_lock: 0s\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := platformer.ParseConfig(data)
		if err != nil {
			return
		}
		out, err := platformer.EncodeConfig(c)
		if err != nil {
			t.Fatalf("EncodeConfig of a parsed config: %v", err)
		}
		back, err := platformer.ParseConfig(out)
		if err != nil {
			t.Fatalf("ParseConfig of encoded config: %v\n%s", err, out)
		}
		if back != c {
			t.Fatalf("round trip changed the config: %+v != %+v", back, c)
		}
		again, err := platformer.EncodeConfig(back)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("encoding is not deterministic:\n%s\n%s", out, again)
		}
	})
}
