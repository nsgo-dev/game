// Package remoteconfig evaluates signed-document values with deterministic
// account bucketing. Network fetching and signature transport are left to hosts.
package remoteconfig

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

type ValueType string

const (
	Bool   ValueType = "bool"
	Int    ValueType = "int"
	Float  ValueType = "float"
	String ValueType = "string"
)

type Value struct {
	Type      ValueType
	Default   any
	Overrides []Override
}
type Override struct {
	Salt    string
	Percent int
	Variant any
}
type Config struct{ Values map[string]Value }
type Resolver struct {
	active Config
	staged Config
}

func New(c Config) *Resolver       { return &Resolver{active: c, staged: c} }
func (r *Resolver) Activate()      { r.active = r.staged }
func (r *Resolver) Stage(c Config) { r.staged = c }
func (r *Resolver) GetBool(key, id string) bool {
	v := r.resolve(key, id)
	if x, ok := v.(bool); ok {
		return x
	}
	return false
}
func (r *Resolver) GetInt(key, id string) int64 {
	v := r.resolve(key, id)
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	}
	return 0
}
func (r *Resolver) GetFloat(key, id string) float64 {
	v := r.resolve(key, id)
	if x, ok := v.(float64); ok {
		return x
	}
	return 0
}
func (r *Resolver) GetString(key, id string) string {
	v := r.resolve(key, id)
	if x, ok := v.(string); ok {
		return x
	}
	return ""
}
func (r *Resolver) resolve(key, id string) any {
	v, ok := r.active.Values[key]
	if !ok {
		return nil
	}
	for _, o := range v.Overrides {
		if o.Percent < 0 || o.Percent > 10000 {
			continue
		}
		if Bucket(o.Salt, id) < o.Percent {
			return o.Variant
		}
	}
	return v.Default
}
func Bucket(salt, id string) int {
	h := sha256.Sum256([]byte(salt + "\x00" + id))
	return int(binary.BigEndian.Uint64(h[:8]) % 10000)
}
func Validate(c Config) error {
	for k, v := range c.Values {
		if strings.TrimSpace(k) == "" || v.Default == nil {
			return errors.New("remoteconfig: key has no default")
		}
		switch v.Type {
		case Bool:
			if _, ok := v.Default.(bool); !ok {
				return fmt.Errorf("remoteconfig: %s default is not bool", k)
			}
		case Int:
		case Float:
		case String:
			if v.Type == String {
				if _, ok := v.Default.(string); !ok {
					return fmt.Errorf("remoteconfig: %s default is not string", k)
				}
			}
		default:
			return fmt.Errorf("remoteconfig: %s has unknown type %q", k, v.Type)
		}
		for _, o := range v.Overrides {
			if o.Percent < 0 || o.Percent > 10000 {
				return fmt.Errorf("remoteconfig: %s override percent %d", k, o.Percent)
			}
		}
	}
	return nil
}
