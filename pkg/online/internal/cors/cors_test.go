package cors_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/internal/cors"
)

func TestValidateOrigins(t *testing.T) {
	for _, valid := range [][]string{
		nil,
		{},
		{"https://play.example.com"},
		{"https://play.example.com", "http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"},
	} {
		if err := cors.ValidateOrigins(valid); err != nil {
			t.Errorf("ValidateOrigins(%v) = %v", valid, err)
		}
	}
	bad := map[string][]string{
		"empty entry":        {""},
		"wildcard":           {"*"},
		"host wildcard":      {"https://*.example.com"},
		"bare wildcard host": {"https://*"},
		"trailing slash":     {"https://a.example/"},
		"path":               {"https://a.example/game"},
		"query":              {"https://a.example?x=1"},
		"fragment":           {"https://a.example#top"},
		"userinfo":           {"https://user@a.example"},
		"ftp scheme":         {"ftp://a.example"},
		"uppercase scheme":   {"HTTPS://a.example"},
		"port zero":          {"http://a.example:0"},
		"leading zero port":  {"http://a.example:080"},
		"port out of range":  {"http://a.example:99999"},
		"trailing colon":     {"http://a.example:"},
		"empty hostname":     {"http://:8080"},
		"duplicate":          {"https://a.example", "https://a.example"},
	}
	for name, origins := range bad {
		if err := cors.ValidateOrigins(origins); err == nil {
			t.Errorf("%s: ValidateOrigins(%v) succeeded", name, origins)
		}
	}
}

func TestWrapPreflightAndRequests(t *testing.T) {
	called := 0
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})
	h := cors.Wrap(inner, cors.Policy{
		Origins:       []string{"https://play.example.com"},
		Methods:       []string{http.MethodGet, http.MethodPost},
		Headers:       []string{"Authorization", "Content-Type"},
		ExposeHeaders: []string{"ETag"},
	})

	// Preflight from an allowed origin.
	req := httptest.NewRequest(http.MethodOptions, "/v1/test", nil)
	req.Header.Set("Origin", "https://play.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || called != 0 {
		t.Fatalf("allowed preflight = %d, called = %d", rec.Code, called)
	}
	hdr := rec.Header()
	if hdr.Get("Access-Control-Allow-Origin") != "https://play.example.com" ||
		hdr.Get("Access-Control-Allow-Methods") != "GET, POST" ||
		hdr.Get("Access-Control-Allow-Headers") != "Authorization, Content-Type" ||
		hdr.Get("Access-Control-Max-Age") != "600" ||
		hdr.Get("Cache-Control") != "no-store" ||
		!slices.Equal(hdr.Values("Vary"), []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"}) {
		t.Fatalf("allowed preflight headers = %v", hdr)
	}

	// Preflight from an unlisted origin.
	req = httptest.NewRequest(http.MethodOptions, "/v1/test", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || called != 0 || rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("disallowed preflight = %d, called = %d, headers = %v", rec.Code, called, rec.Header())
	}

	// Non-preflight OPTIONS falls through to inner handler.
	req = httptest.NewRequest(http.MethodOptions, "/v1/test", nil)
	req.Header.Set("Origin", "https://play.example.com")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || called != 1 {
		t.Fatalf("non-preflight OPTIONS = %d, called = %d", rec.Code, called)
	}

	// Real request from allowed origin.
	req = httptest.NewRequest(http.MethodPost, "/v1/test", nil)
	req.Header.Set("Origin", "https://play.example.com")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || called != 2 ||
		rec.Header().Get("Access-Control-Allow-Origin") != "https://play.example.com" ||
		rec.Header().Get("Access-Control-Expose-Headers") != "ETag" ||
		rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("allowed POST = %d, headers = %v", rec.Code, rec.Header())
	}

	// Real request from disallowed origin.
	req = httptest.NewRequest(http.MethodPost, "/v1/test", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || called != 3 ||
		rec.Header().Get("Access-Control-Allow-Origin") != "" ||
		rec.Header().Get("Access-Control-Expose-Headers") != "" ||
		rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("disallowed POST = %d, headers = %v", rec.Code, rec.Header())
	}

	// Request without Origin (native client).
	req = httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || called != 4 ||
		rec.Header().Get("Access-Control-Allow-Origin") != "" ||
		rec.Header().Get("Vary") != "" {
		t.Fatalf("no-origin GET = %d, headers = %v", rec.Code, rec.Header())
	}

	// Empty policy refuses preflights and omits Expose-Headers.
	empty := cors.Wrap(inner, cors.Policy{Methods: []string{http.MethodGet}})
	req = httptest.NewRequest(http.MethodOptions, "/v1/test", nil)
	req.Header.Set("Origin", "https://play.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec = httptest.NewRecorder()
	empty.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("empty-policy preflight = %d, headers = %v", rec.Code, rec.Header())
	}
}
