// Package identity contains standards-based account identity verifiers.
package identity

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

type OIDC struct {
	Issuer    string
	ClientIDs []string
	Client    *http.Client
	Clock     func() time.Time
	mu        sync.Mutex
	keys      map[string]any
	fetched   time.Time
}

func (o *OIDC) VerifyIdentity(ctx context.Context, credential string) (string, error) {
	if o.Client == nil {
		o.Client = http.DefaultClient
	}
	parts := strings.Split(credential, ".")
	if len(parts) != 3 {
		return "", errors.New("identity: malformed OIDC token")
	}
	var h struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	var c struct {
		Iss, Sub string
		Aud      json.RawMessage
		Exp      int64 `json:"exp"`
		Nonce    string
	}
	if err := decode(parts[0], &h); err != nil {
		return "", err
	}
	if err := decode(parts[1], &c); err != nil {
		return "", err
	}
	if c.Iss != o.Issuer || c.Sub == "" || c.Exp <= o.now().Unix() {
		return "", errors.New("identity: invalid OIDC claims")
	}
	if !audOK(c.Aud, o.ClientIDs) {
		return "", errors.New("identity: invalid OIDC audience")
	}
	key, err := o.key(ctx, h.Kid)
	if err != nil {
		return "", err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	switch h.Alg {
	case "RS256":
		if rsa.VerifyPKCS1v15(key.(*rsa.PublicKey), crypto.SHA256, digest[:], sig) != nil {
			return "", errors.New("identity: invalid OIDC signature")
		}
	case "ES256":
		if !ecdsa.VerifyASN1(key.(*ecdsa.PublicKey), digest[:], sig) {
			return "", errors.New("identity: invalid OIDC signature")
		}
	default:
		return "", fmt.Errorf("identity: unsupported OIDC algorithm %q", h.Alg)
	}
	return c.Sub, nil
}
func (o *OIDC) now() time.Time {
	if o.Clock != nil {
		return o.Clock()
	}
	return time.Now()
}
func decode(s string, v any) error {
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func audOK(raw json.RawMessage, want []string) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		for _, x := range want {
			if x == s {
				return true
			}
		}
	}
	var a []string
	if json.Unmarshal(raw, &a) == nil {
		for _, x := range a {
			for _, w := range want {
				if x == w {
					return true
				}
			}
		}
	}
	return false
}
func (o *OIDC) key(ctx context.Context, kid string) (any, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.keys == nil || o.now().Sub(o.fetched) > time.Hour {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(o.Issuer, "/")+"/.well-known/jwks.json", nil)
		if e != nil {
			return nil, e
		}
		resp, e := o.Client.Do(req)
		if e != nil {
			return nil, e
		}
		defer resp.Body.Close()
		var doc struct {
			Keys []struct {
				Kid string `json:"kid"`
				Kty string `json:"kty"`
				Alg string `json:"alg"`
				N   string `json:"n"`
				E   string `json:"e"`
				Crv string `json:"crv"`
				X   string `json:"x"`
				Y   string `json:"y"`
			} `json:"keys"`
		}
		if e = json.NewDecoder(resp.Body).Decode(&doc); e != nil {
			return nil, e
		}
		o.keys = map[string]any{}
		for _, k := range doc.Keys {
			if k.Kty == "RSA" {
				n, _ := base64.RawURLEncoding.DecodeString(k.N)
				e, _ := base64.RawURLEncoding.DecodeString(k.E)
				o.keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
			} else if k.Kty == "EC" && k.Crv == "P-256" {
				xb, _ := base64.RawURLEncoding.DecodeString(k.X)
				yb, _ := base64.RawURLEncoding.DecodeString(k.Y)
				o.keys[k.Kid] = &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(xb), Y: new(big.Int).SetBytes(yb)}
			}
		}
		o.fetched = o.now()
	}
	k, ok := o.keys[kid]
	if !ok {
		return nil, errors.New("identity: unknown OIDC key")
	}
	return k, nil
}
