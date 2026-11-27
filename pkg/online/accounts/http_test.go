package accounts_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/accounts"
	"github.com/nsgo-dev/game/pkg/online/accounts/token"
)

type httpCreds struct {
	Account string `json:"account"`
	Session string `json:"session"`
	Token   string `json:"token"`
	Expires string `json:"expires"`
	Created bool   `json:"created"`
}

// call serves one request on h without a network and decodes a JSON answer into out.
func call(t *testing.T, h http.Handler, method, path, bearer, body string, out any) (int, http.Header) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: %v in %q", method, path, err, rec.Body)
		}
	}
	return rec.Code, rec.Header()
}

func TestHTTPGuestUpgradeAndSecondDevice(t *testing.T) {
	f := newFixture(t, nil)
	h := f.svc.Handler()

	var guest httpCreds
	code, hdr := call(t, h, "POST", "/v1/guest", "", "", &guest)
	if code != http.StatusCreated || !guest.Created || guest.Session == "" || guest.Token == "" || guest.Expires != "2026-11-23T12:15:00Z" {
		t.Fatalf("guest = %d %+v", code, guest)
	}
	if hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", hdr.Get("Cache-Control"))
	}
	if code, _ := call(t, h, "POST", "/v1/link/email", guest.Token, `{"email":"ada@example.com","password":"correct horse battery"}`, nil); code != http.StatusNoContent {
		t.Fatalf("link email = %d", code)
	}
	if code, _ := call(t, h, "POST", "/v1/link/identity", guest.Token, `{"provider":"gamecenter","credential":"gc-signature-device-1"}`, nil); code != http.StatusNoContent {
		t.Fatalf("link identity = %d", code)
	}
	var byEmail, byIdentity httpCreds
	if code, _ := call(t, h, "POST", "/v1/signin/email", "", `{"email":"ada@example.com","password":"correct horse battery"}`, &byEmail); code != http.StatusOK || byEmail.Account != guest.Account {
		t.Fatalf("sign in by email = %d %+v", code, byEmail)
	}
	if code, _ := call(t, h, "POST", "/v1/signin/identity", "", `{"provider":"gamecenter","credential":"gc-signature-device-2"}`, &byIdentity); code != http.StatusOK || byIdentity.Account != guest.Account {
		t.Fatalf("sign in by identity = %d %+v", code, byIdentity)
	}
	var me struct {
		Account    string `json:"account"`
		Email      string `json:"email"`
		Guest      bool   `json:"guest"`
		Identities []struct {
			Provider string `json:"provider"`
			Subject  string `json:"subject"`
		} `json:"identities"`
	}
	if code, _ := call(t, h, "GET", "/v1/me", byIdentity.Token, "", &me); code != http.StatusOK || me.Account != guest.Account || me.Email != "ada@example.com" || me.Guest || len(me.Identities) != 1 || me.Identities[0].Subject != "G:1234" {
		t.Fatalf("me = %d %+v", code, me)
	}
	var refreshed httpCreds
	if code, _ := call(t, h, "POST", "/v1/refresh", "", `{"session":"`+byEmail.Session+`"}`, &refreshed); code != http.StatusOK || refreshed.Account != guest.Account || refreshed.Session != byEmail.Session {
		t.Fatalf("refresh = %d %+v", code, refreshed)
	}
	if code, _ := call(t, h, "POST", "/v1/signout", "", `{"session":"`+byEmail.Session+`"}`, nil); code != http.StatusNoContent {
		t.Fatalf("sign out = %d", code)
	}
	var e struct{ Error string }
	if code, _ := call(t, h, "POST", "/v1/refresh", "", `{"session":"`+byEmail.Session+`"}`, &e); code != http.StatusUnauthorized || e.Error != "invalid_credentials" {
		t.Fatalf("refresh after sign out = %d %+v", code, e)
	}

	// The key set the service publishes verifies its tokens.
	req := httptest.NewRequest("GET", "/v1/keys", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	ks, err := token.ParseKeySet(rec.Body.Bytes())
	if err != nil || rec.Header().Get("Content-Type") != "application/yaml" {
		t.Fatalf("keys = %v, %q", err, rec.Header().Get("Content-Type"))
	}
	v, _ := token.NewVerifier(ks)
	v.Now = f.clock.Now
	if c, err := v.Verify(guest.Token); err != nil || c.Subject != guest.Account {
		t.Fatalf("verify with published keys = %+v, %v", c, err)
	}
}

func TestHTTPErrors(t *testing.T) {
	f := newFixture(t, nil)
	h := f.svc.Handler()
	var guest httpCreds
	call(t, h, "POST", "/v1/guest", "", "{}", &guest)
	call(t, h, "POST", "/v1/register", "", `{"email":"ada@example.com","password":"correct horse battery"}`, nil)
	cases := []struct {
		name, method, path, bearer, body string
		status                           int
		code                             string
	}{
		{"unknown field", "POST", "/v1/register", "", `{"email":"x@example.com","password":"correct horse battery","admin":true}`, 400, "bad_request"},
		{"not json", "POST", "/v1/signin/email", "", `email=x`, 400, "bad_request"},
		{"trailing data", "POST", "/v1/signin/email", "", `{"email":"x@example.com","password":"y"} {}`, 400, "bad_request"},
		{"trailing brace", "POST", "/v1/signin/email", "", `{"email":"x@example.com","password":"y"}}`, 400, "bad_request"},
		{"too large", "POST", "/v1/register", "", `{"email":"` + strings.Repeat("a", 40<<10) + `"}`, 413, "request_too_large"},
		{"invalid email", "POST", "/v1/register", "", `{"email":"nope","password":"correct horse battery"}`, 400, "invalid_email"},
		{"weak password", "POST", "/v1/register", "", `{"email":"x@example.com","password":"short"}`, 400, "weak_password"},
		{"taken", "POST", "/v1/register", "", `{"email":"ada@example.com","password":"correct horse battery"}`, 409, "taken"},
		{"wrong password", "POST", "/v1/signin/email", "", `{"email":"ada@example.com","password":"wrong password!"}`, 401, "invalid_credentials"},
		{"unknown provider", "POST", "/v1/signin/identity", "", `{"provider":"steam","credential":"t"}`, 400, "unknown_provider"},
		{"no bearer", "GET", "/v1/me", "", "", 401, "invalid_token"},
		{"bad bearer", "POST", "/v1/link/email", "not-a-token", `{}`, 401, "invalid_token"},
		{"link identity", "POST", "/v1/link/identity", guest.Token, `{"provider":"gamecenter","credential":"gc-signature-device-1"}`, 204, ""},
		{"forged identity", "POST", "/v1/signin/identity", "", `{"provider":"gamecenter","credential":"forged"}`, 401, "invalid_credentials"},
	}
	for _, tc := range cases {
		var e struct{ Error string }
		code, hdr := call(t, h, tc.method, tc.path, tc.bearer, tc.body, &e)
		if code != tc.status || e.Error != tc.code {
			t.Errorf("%s: %d %q, want %d %q", tc.name, code, e.Error, tc.status, tc.code)
		}
		if tc.code == "invalid_token" && !strings.Contains(hdr.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s: no WWW-Authenticate", tc.name)
		}
	}
	if code, _ := call(t, h, "GET", "/v1/guest", "", "", nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /v1/guest = %d", code)
	}
}

func TestHTTPCORS(t *testing.T) {
	f := newFixture(t, func(c *accounts.Config, _ *accounts.Options) {
		c.AllowedOrigins = []string{"https://play.example.com", "http://localhost:8080"}
	})
	h := f.svc.Handler()

	preflight := func(handler http.Handler, path, origin, method, headers string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, path, nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", method)
		if headers != "" {
			req.Header.Set("Access-Control-Request-Headers", headers)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Preflight from listed origins succeeds on both POST and GET endpoints.
	for _, tc := range []struct{ path, origin, method string }{
		{"/v1/guest", "https://play.example.com", http.MethodPost},
		{"/v1/me", "http://localhost:8080", http.MethodGet},
	} {
		rec := preflight(h, tc.path, tc.origin, tc.method, "authorization, content-type")
		hdr := rec.Header()
		if rec.Code != http.StatusNoContent ||
			hdr.Get("Access-Control-Allow-Origin") != tc.origin ||
			hdr.Get("Access-Control-Allow-Methods") != "GET, POST" ||
			hdr.Get("Access-Control-Allow-Headers") != "Authorization, Content-Type" ||
			hdr.Get("Access-Control-Max-Age") != "600" ||
			hdr.Get("Cache-Control") != "no-store" ||
			!slices.Equal(hdr.Values("Vary"), []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"}) {
			t.Fatalf("preflight %s from %s = %d %v", tc.path, tc.origin, rec.Code, hdr)
		}
	}

	// Preflight from an unlisted origin is refused with no CORS allow headers.
	if rec := preflight(h, "/v1/guest", "https://evil.example.com", http.MethodPost, "content-type"); rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("foreign preflight = %d %v", rec.Code, rec.Header())
	}

	// Default configuration (no AllowedOrigins) refuses cross-origin preflights.
	noCORS := newFixture(t, nil).svc.Handler()
	if rec := preflight(noCORS, "/v1/guest", "https://play.example.com", http.MethodPost, "content-type"); rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("empty allowedOrigins preflight = %d %v", rec.Code, rec.Header())
	}

	// Real POST /v1/guest and GET /v1/me from a listed origin carry Access-Control-Allow-Origin and Vary: Origin.
	req := httptest.NewRequest(http.MethodPost, "/v1/guest", strings.NewReader("{}"))
	req.Header.Set("Origin", "https://play.example.com")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || rec.Header().Get("Access-Control-Allow-Origin") != "https://play.example.com" || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("POST /v1/guest = %d %v", rec.Code, rec.Header())
	}
	var guest httpCreds
	if err := json.Unmarshal(rec.Body.Bytes(), &guest); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Origin", "https://play.example.com")
	req.Header.Set("Authorization", "Bearer "+guest.Token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "https://play.example.com" || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("GET /v1/me = %d %v", rec.Code, rec.Header())
	}

	// Error responses to a listed origin still carry Access-Control-Allow-Origin so the browser can read the error code.
	req = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Origin", "https://play.example.com")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("Access-Control-Allow-Origin") != "https://play.example.com" || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("unauthed GET /v1/me = %d %v", rec.Code, rec.Header())
	}

	// Real request from an unlisted origin gets Vary: Origin and no Access-Control-Allow-Origin.
	req = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Authorization", "Bearer "+guest.Token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("foreign GET /v1/me = %d %v", rec.Code, rec.Header())
	}

	// Native request without Origin gets no CORS or Vary headers.
	req = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+guest.Token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Vary") != "" {
		t.Fatalf("native GET /v1/me = %d %v", rec.Code, rec.Header())
	}
}
