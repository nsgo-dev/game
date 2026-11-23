package accounts_test

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/accounts"
)

func TestConfigRoundTripIsLossless(t *testing.T) {
	cfg, err := accounts.ParseConfig([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	if time.Duration(cfg.TokenTTL) != 15*time.Minute || cfg.Password.MinLength != 10 || cfg.Password.Argon2id != fastArgon2 {
		t.Fatalf("config = %+v", cfg)
	}
	out, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != testConfig {
		t.Fatalf("Marshal =\n%s\nwant\n%s", out, testConfig)
	}
	def := accounts.DefaultConfig("https://accounts.example.com", "my-game")
	def.DisableGuests = true
	def.TokenTTL = accounts.Duration(90*time.Minute + 30*time.Second)
	out, err = def.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	want := `version: 1
issuer: https://accounts.example.com
audience: my-game
tokenTTL: 1h30m30s
disableGuests: true
password:
  minLength: 10
  argon2id:
    time: 2
    memoryKiB: 19456
    threads: 1
`
	if string(out) != want {
		t.Fatalf("Marshal =\n%s\nwant\n%s", out, want)
	}
	back, err := accounts.ParseConfig(out)
	if err != nil || back != def {
		t.Fatalf("ParseConfig(Marshal) = %+v, %v; want %+v", back, err, def)
	}
}

func TestConfigMarshalNormalizesHandWrittenFiles(t *testing.T) {
	// A hand-written file may order its fields differently, spell durations longhand and write defaults; Marshal writes the canonical form, which then reads back and writes again unchanged.
	hand := `audience: my-game
issuer: https://accounts.example.com
version: 1
disableGuests: false
password:
  argon2id:
    threads: 1
    memoryKiB: 64
    time: 1
  minLength: 10
tokenTTL: 15m0s
`
	cfg, err := accounts.ParseConfig([]byte(hand))
	if err != nil {
		t.Fatal(err)
	}
	out, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != testConfig {
		t.Fatalf("Marshal =\n%s\nwant\n%s", out, testConfig)
	}
	back, err := accounts.ParseConfig(out)
	if err != nil || back != cfg {
		t.Fatalf("ParseConfig(Marshal) = %+v, %v; want %+v", back, err, cfg)
	}
}

func TestDurationText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		time.Second:                     "1s",
		90 * time.Second:                "1m30s",
		15 * time.Minute:                "15m",
		time.Hour:                       "1h",
		24 * time.Hour:                  "24h",
		time.Hour + time.Second:         "1h0m1s",
		2*time.Hour + 5*time.Minute:     "2h5m",
		1500 * time.Millisecond:         "1.5s",
		time.Hour + 30*time.Millisecond: "1h0m0.03s",
	} {
		got, _ := accounts.Duration(d).MarshalYAML()
		if got != want {
			t.Errorf("%v = %q, want %q", d, got, want)
		}
		if back, err := time.ParseDuration(want); err != nil || back != d {
			t.Errorf("%q parses to %v, %v", want, back, err)
		}
	}
}

func TestParseConfigRefuses(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"unknown field":  testConfig + "extra: true\n",
		"nested unknown": strings.Replace(testConfig, "  minLength: 10\n", "  minLength: 10\n  maxLength: 99\n", 1),
		"version":        strings.Replace(testConfig, "version: 1", "version: 2", 1),
		"no version":     strings.Replace(testConfig, "version: 1\n", "", 1),
		"no issuer":      strings.Replace(testConfig, "issuer: https://accounts.example.com\n", "", 1),
		"no audience":    strings.Replace(testConfig, "audience: my-game\n", "", 1),
		"no ttl":         strings.Replace(testConfig, "tokenTTL: 15m\n", "", 1),
		"ttl number":     strings.Replace(testConfig, "tokenTTL: 15m", "tokenTTL: 900", 1),
		"ttl text":       strings.Replace(testConfig, "tokenTTL: 15m", "tokenTTL: soon", 1),
		"ttl too long":   strings.Replace(testConfig, "tokenTTL: 15m", "tokenTTL: 25h", 1),
		"ttl fraction":   strings.Replace(testConfig, "tokenTTL: 15m", "tokenTTL: 1.5s", 1),
		"short minimum":  strings.Replace(testConfig, "minLength: 10", "minLength: 4", 1),
		"no time":        strings.Replace(testConfig, "    time: 1\n", "", 1),
		"memory":         strings.Replace(testConfig, "memoryKiB: 64", "memoryKiB: 4", 1),
		"huge memory":    strings.Replace(testConfig, "memoryKiB: 64", "memoryKiB: 8388608", 1),
		"threads":        strings.Replace(testConfig, "threads: 1", "threads: 0", 1),
		"trailing doc":   testConfig + "---\nversion: 1\n",
		"not a map":      "- version\n",
	}
	for name, data := range cases {
		if _, err := accounts.ParseConfig([]byte(data)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func FuzzParseConfig(f *testing.F) {
	f.Add([]byte(testConfig))
	def, _ := accounts.DefaultConfig("i", "a").Marshal()
	f.Add(def)
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := accounts.ParseConfig(data)
		if err != nil {
			return
		}
		out, err := cfg.Marshal()
		if err != nil {
			t.Fatalf("Marshal of a parsed config: %v", err)
		}
		back, err := accounts.ParseConfig(out)
		if err != nil || back != cfg {
			t.Fatalf("round trip: %+v, %v; want %+v", back, err, cfg)
		}
		again, _ := back.Marshal()
		if !bytes.Equal(out, again) {
			t.Fatalf("Marshal not stable:\n%s\n%s", out, again)
		}
	})
}

func TestPasswordHash(t *testing.T) {
	hash, err := accounts.HashPassword("correct horse battery", fastArgon2, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ok, params, err := accounts.VerifyPassword("correct horse battery", hash)
	if !ok || err != nil || params != fastArgon2 {
		t.Fatalf("VerifyPassword = %v, %+v, %v", ok, params, err)
	}
	if ok, _, _ := accounts.VerifyPassword("correct horse batterY", hash); ok {
		t.Fatal("a wrong password matched")
	}
	if again, _ := accounts.HashPassword("correct horse battery", fastArgon2, rand.Reader); again == hash {
		t.Fatal("two hashes share a salt")
	}
	parts := strings.Split(hash, "$")
	for name, bad := range map[string]string{
		"empty":         "",
		"bcrypt":        "$2a$10$abcdefghijklmnopqrstuv",
		"argon2i":       strings.Replace(hash, "argon2id", "argon2i", 1),
		"version":       strings.Replace(hash, "v=19", "v=16", 1),
		"huge memory":   strings.Replace(hash, "m=64", "m=99999999", 1),
		"no lanes":      strings.Replace(hash, "p=1", "p=0", 1),
		"many passes":   strings.Replace(hash, "t=1", "t=1000", 1),
		"leading zero":  strings.Replace(hash, "m=64", "m=064", 1),
		"param order":   strings.Replace(hash, "m=64,t=1", "t=1,m=64", 1),
		"bad salt":      strings.Join([]string{"", parts[1], parts[2], parts[3], "!!", parts[5]}, "$"),
		"short key":     strings.Join([]string{"", parts[1], parts[2], parts[3], parts[4], "AAAA"}, "$"),
		"extra section": hash + "$x",
	} {
		if _, _, err := accounts.VerifyPassword("correct horse battery", bad); err == nil {
			t.Errorf("%s: accepted %q", name, bad)
		}
	}
	if _, err := accounts.HashPassword("x", accounts.Argon2Params{}, rand.Reader); err == nil {
		t.Fatal("hashed with zero params")
	}
}

func BenchmarkHashPasswordDefault(b *testing.B) {
	p := accounts.DefaultArgon2Params()
	for b.Loop() {
		if _, err := accounts.HashPassword("correct horse battery", p, rand.Reader); err != nil {
			b.Fatal(err)
		}
	}
}
