package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOIDCVerify(t *testing.T) {
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	n := base64.RawURLEncoding.EncodeToString(k.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(bigIntBytes(k.E))
	issuer := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/jwks.json" {
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kid": "k", "kty": "RSA", "alg": "RS256", "n": n, "e": e}}})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	issuer = srv.URL
	head := b64(map[string]string{"alg": "RS256", "kid": "k"})
	pay := b64(map[string]any{"iss": issuer, "sub": "acct", "aud": "app", "exp": time.Now().Add(time.Hour).Unix()})
	sig, _ := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, func() []byte { h := sha256.Sum256([]byte(head + "." + pay)); return h[:] }())
	tok := fmt.Sprintf("%s.%s.%s", head, pay, base64.RawURLEncoding.EncodeToString(sig))
	v := OIDC{Issuer: issuer, ClientIDs: []string{"app"}}
	if got, err := v.VerifyIdentity(context.Background(), tok); err != nil || got != "acct" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}
func b64(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
func bigIntBytes(v int) []byte {
	if v < 256 {
		return []byte{byte(v)}
	}
	return []byte{1, 0, 1}
}
