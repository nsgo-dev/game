package accounts

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"golang.org/x/crypto/argon2"
)

// Argon2Params is the cost of an argon2id password hash.
type Argon2Params struct {
	// Time is the number of passes over memory, 1 to 16.
	Time uint32 `yaml:"time"`
	// MemoryKiB is the memory one hash uses, in KiB, from 8*Threads to 4 GiB.
	MemoryKiB uint32 `yaml:"memoryKiB"`
	// Threads is the parallelism (lanes), 1 to 16.
	Threads uint8 `yaml:"threads"`
}

const (
	maxArgon2Time      = 16
	maxArgon2MemoryKiB = 4 << 20
	maxArgon2Threads   = 16
	argon2SaltLen      = 16
	argon2KeyLen       = 32
)

// DefaultArgon2Params returns OWASP's recommended argon2id cost: 19 MiB of memory, 2 passes, 1 lane.
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{Time: 2, MemoryKiB: 19 * 1024, Threads: 1}
}

// Validate checks that p is within argon2id's limits and this package's bounds.
func (p Argon2Params) Validate() error {
	return p.validateAt("")
}

func (p Argon2Params) validateAt(prefix string) error {
	field := func(name string) string {
		if prefix == "" {
			return name
		}
		return prefix + "." + name
	}
	if p.Time < 1 || p.Time > maxArgon2Time {
		return loaderr.Errorf(field("time"), fmt.Sprintf("set %s to an integer from 1 to %d", field("time"), maxArgon2Time), "accounts: argon2id time %d must be from 1 to %d", p.Time, maxArgon2Time)
	}
	if p.Threads < 1 || p.Threads > maxArgon2Threads {
		return loaderr.Errorf(field("threads"), fmt.Sprintf("set %s to an integer from 1 to %d", field("threads"), maxArgon2Threads), "accounts: argon2id threads %d must be from 1 to %d", p.Threads, maxArgon2Threads)
	}
	if p.MemoryKiB < 8*uint32(p.Threads) || p.MemoryKiB > maxArgon2MemoryKiB {
		return loaderr.Errorf(field("memoryKiB"), fmt.Sprintf("set %s to an integer from %d to %d", field("memoryKiB"), 8*uint32(p.Threads), maxArgon2MemoryKiB), "accounts: argon2id memoryKiB %d must be from %d to %d", p.MemoryKiB, 8*uint32(p.Threads), maxArgon2MemoryKiB)
	}
	return nil
}

var errHashFormat = errors.New("accounts: malformed password hash")

// HashPassword returns the argon2id hash of password with a random salt from r, in the PHC string format "$argon2id$v=19$m=...,t=...,p=...$salt$hash".
func HashPassword(password string, p Argon2Params, r io.Reader) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	if len(password) > MaxPasswordBytes {
		return "", ErrWeakPassword
	}
	salt := make([]byte, argon2SaltLen)
	if _, err := io.ReadFull(r, salt); err != nil {
		return "", fmt.Errorf("accounts: salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, argon2KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.MemoryKiB, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches the PHC-format argon2id hash encoded, comparing in constant time, and returns the cost the hash was made with so a caller can rehash a password made with an outdated cost.
func VerifyPassword(password, encoded string) (bool, Argon2Params, error) {
	p, salt, want, err := parseHash(encoded)
	if err != nil {
		return false, Argon2Params{}, err
	}
	if len(password) > MaxPasswordBytes {
		return false, p, nil
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, p, nil
}

// parseHash splits a PHC-format argon2id hash, refusing costs outside Validate's bounds so a tampered record cannot make a check exhaust memory.
func parseHash(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return Argon2Params{}, nil, nil, errHashFormat
	}
	var p Argon2Params
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return Argon2Params{}, nil, nil, errHashFormat
	}
	for i, prefix := range []string{"m=", "t=", "p="} {
		v, ok := strings.CutPrefix(params[i], prefix)
		if !ok {
			return Argon2Params{}, nil, nil, errHashFormat
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil || strconv.FormatUint(n, 10) != v {
			return Argon2Params{}, nil, nil, errHashFormat
		}
		switch i {
		case 0:
			p.MemoryKiB = uint32(n)
		case 1:
			p.Time = uint32(n)
		case 2:
			if n > 255 {
				return Argon2Params{}, nil, nil, errHashFormat
			}
			p.Threads = uint8(n)
		}
	}
	if p.Validate() != nil {
		return Argon2Params{}, nil, nil, errHashFormat
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return Argon2Params{}, nil, nil, errHashFormat
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return Argon2Params{}, nil, nil, errHashFormat
	}
	return p, salt, key, nil
}
