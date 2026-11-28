package cloudsave_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts/authtoken"
	"github.com/nsgo-dev/game/pkg/online/cloudsave"
)

var t0 = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

type fixture struct {
	svc    *cloudsave.Service
	signer *authtoken.Signer
	now    time.Time
}

func newFixture(t *testing.T, mutate func(*cloudsave.Config)) *fixture {
	t.Helper()
	key, err := authtoken.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authtoken.NewSigner(key, "https://accounts.example.com", "test-game", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cfg := cloudsave.DefaultConfig()
	cfg.AllowedOrigins = []string{"https://play.example.com"}
	if mutate != nil {
		mutate(&cfg)
	}
	verifier, err := authtoken.NewVerifierFor(signer.KeySet(), cfg.TokenAudience())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{signer: signer, now: t0}
	f.svc, err = cloudsave.New(cfg, cloudsave.Options{Store: cloudsave.NewMemoryStore(), Tokens: verifier, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// token returns a token for the service's audience, as a device gets from the account service's refresh.
func (f *fixture) token(t *testing.T, account string) string {
	t.Helper()
	tok, _, err := f.signer.IssueFor(account, f.svc.Config().TokenAudience())
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

type request struct {
	method, path, bearer string
	body                 string
	header               http.Header
}

func (f *fixture) serve(t *testing.T, r request) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body))
	for k, vs := range r.header {
		req.Header[k] = vs
	}
	if r.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+r.bearer)
	}
	rec := httptest.NewRecorder()
	f.svc.Handler().ServeHTTP(rec, req)
	if rec.Header().Get("Cache-Control") != "no-store" && r.method != http.MethodOptions {
		t.Errorf("%s %s: Cache-Control %q", r.method, r.path, rec.Header().Get("Cache-Control"))
	}
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) (string, int) {
	t.Helper()
	var e struct {
		Error string `json:"error"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("error body %q: %v", rec.Body, err)
	}
	return e.Error, e.Limit
}

func quoted(data string) http.Header {
	return http.Header{"If-Match": {strconv.Quote(cloudsave.Hash([]byte(data)))}}
}

func TestHTTPSaveLoadAcrossDevicesOfOneAccount(t *testing.T) {
	f := newFixture(t, nil)
	web, android := f.token(t, "acct-1"), f.token(t, "acct-1")
	if rec := f.serve(t, request{method: "PUT", path: "/v1/saves/main", bearer: web, body: "level 3", header: http.Header{"If-None-Match": {"*"}}}); rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	} else if et := rec.Header().Get("ETag"); et != strconv.Quote(cloudsave.Hash([]byte("level 3"))) {
		t.Fatalf("PUT ETag = %q", et)
	}
	rec := f.serve(t, request{method: "GET", path: "/v1/saves/main", bearer: android})
	if rec.Code != http.StatusOK || rec.Body.String() != "level 3" || rec.Header().Get("Save-Version") != "1" || rec.Header().Get("ETag") != strconv.Quote(cloudsave.Hash([]byte("level 3"))) || rec.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("GET = %d %q %v", rec.Code, rec.Body, rec.Header())
	}
	// Another account sees nothing.
	if rec := f.serve(t, request{method: "GET", path: "/v1/saves/main", bearer: f.token(t, "acct-2")}); rec.Code != http.StatusNotFound {
		t.Fatalf("other account GET = %d", rec.Code)
	}
	rec = f.serve(t, request{method: "GET", path: "/v1/saves", bearer: android})
	if rec.Code != http.StatusOK || rec.Body.String() != `{"slots":[{"slot":"main","version":1,"hash":"`+cloudsave.Hash([]byte("level 3"))+`","size":7,"created":"2026-11-24T10:00:00.000Z"}]}`+"\n" {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
}

func TestHTTPConditionalWrites(t *testing.T) {
	f := newFixture(t, nil)
	tok := f.token(t, "acct")
	put := func(body string, h http.Header) *httptest.ResponseRecorder {
		return f.serve(t, request{method: "PUT", path: "/v1/saves/main", bearer: tok, body: body, header: h})
	}
	if rec := put("base", nil); rec.Code != http.StatusOK {
		t.Fatalf("unconditional PUT = %d", rec.Code)
	}
	// Two devices both read "base"; the first write wins and the second is refused, not applied.
	if rec := put("phone", quoted("base")); rec.Code != http.StatusOK {
		t.Fatalf("phone PUT = %d %s", rec.Code, rec.Body)
	}
	rec := put("tablet", quoted("base"))
	if code, _ := errorCode(t, rec); rec.Code != http.StatusPreconditionFailed || code != "changed" {
		t.Fatalf("tablet PUT = %d %s", rec.Code, rec.Body)
	}
	if rec := put("again", http.Header{"If-None-Match": {"*"}}); rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("create over existing = %d", rec.Code)
	}
	if rec := f.serve(t, request{method: "GET", path: "/v1/saves/main", bearer: tok}); rec.Body.String() != "phone" {
		t.Fatalf("slot = %q", rec.Body)
	}
	if rec := f.serve(t, request{method: "DELETE", path: "/v1/saves/main", bearer: tok, header: quoted("base")}); rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale DELETE = %d", rec.Code)
	}
	if rec := f.serve(t, request{method: "DELETE", path: "/v1/saves/main", bearer: tok, header: quoted("phone")}); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", rec.Code)
	}
	if rec := f.serve(t, request{method: "DELETE", path: "/v1/saves/main", bearer: tok, header: http.Header{"If-None-Match": {"*"}}}); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE of a deleted slot = %d", rec.Code)
	}
	if rec := f.serve(t, request{method: "GET", path: "/v1/saves/main", bearer: tok}); rec.Code != http.StatusNotFound {
		t.Fatalf("GET after delete = %d", rec.Code)
	}
	for _, h := range []http.Header{
		{"If-Match": {"*"}},
		{"If-Match": {"sha256:00"}},
		{"If-Match": {`"sha256:zz"`}},
		{"If-Match": {strconv.Quote(cloudsave.Hash(nil)), strconv.Quote(cloudsave.Hash(nil))}},
		{"If-None-Match": {`"x"`}},
		{"If-Match": {strconv.Quote(cloudsave.Hash(nil))}, "If-None-Match": {"*"}},
	} {
		if rec := put("x", h); rec.Code != http.StatusBadRequest {
			t.Errorf("PUT with %v = %d", h, rec.Code)
		}
	}
}

func TestHTTPRejectsSlotOverLimit(t *testing.T) {
	f := newFixture(t, func(c *cloudsave.Config) { c.MaxSlotBytes = 16 })
	tok := f.token(t, "acct")
	if rec := f.serve(t, request{method: "PUT", path: "/v1/saves/main", bearer: tok, body: strings.Repeat("a", 16)}); rec.Code != http.StatusOK {
		t.Fatalf("PUT at the limit = %d", rec.Code)
	}
	rec := f.serve(t, request{method: "PUT", path: "/v1/saves/main", bearer: tok, body: strings.Repeat("b", 17)})
	if code, limit := errorCode(t, rec); rec.Code != http.StatusRequestEntityTooLarge || code != "slot_too_large" || limit != 16 {
		t.Fatalf("PUT over the limit = %d %s", rec.Code, rec.Body)
	}
	// Without a Content-Length the body is cut off at the limit all the same.
	req := httptest.NewRequest("PUT", "/v1/saves/main", struct{ *strings.Reader }{strings.NewReader(strings.Repeat("c", 40))})
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	f.svc.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked PUT over the limit = %d", rec.Code)
	}
	if rec := f.serve(t, request{method: "GET", path: "/v1/saves/main", bearer: tok}); rec.Body.String() != strings.Repeat("a", 16) {
		t.Fatalf("slot changed by a refused write: %q", rec.Body)
	}
	if _, err := f.svc.Save(context.Background(), "acct", "main", make([]byte, 17), cloudsave.MatchAny); !errors.Is(err, cloudsave.ErrSlotTooLarge) || !strings.Contains(err.Error(), "limit 16") {
		t.Fatalf("Save over the limit = %v", err)
	}
}

func TestHTTPTooManySlots(t *testing.T) {
	f := newFixture(t, func(c *cloudsave.Config) { c.MaxSlots = 1 })
	tok := f.token(t, "acct")
	f.serve(t, request{method: "PUT", path: "/v1/saves/one", bearer: tok, body: "1"})
	rec := f.serve(t, request{method: "PUT", path: "/v1/saves/two", bearer: tok, body: "2"})
	if code, _ := errorCode(t, rec); rec.Code != http.StatusConflict || code != "too_many_slots" {
		t.Fatalf("second slot = %d %s", rec.Code, rec.Body)
	}
}

func TestHTTPVersionsAndRestore(t *testing.T) {
	f := newFixture(t, func(c *cloudsave.Config) { c.KeepVersions = 3 })
	tok := f.token(t, "acct")
	for i, body := range []string{"v1", "v2", "v3", "v4"} {
		f.now = t0.Add(time.Duration(i) * time.Minute)
		f.serve(t, request{method: "PUT", path: "/v1/saves/main", bearer: tok, body: body})
	}
	f.now = t0.Add(10 * time.Minute)
	f.serve(t, request{method: "DELETE", path: "/v1/saves/main", bearer: tok})
	rec := f.serve(t, request{method: "GET", path: "/v1/saves/main/versions", bearer: tok})
	var list struct {
		Versions []struct {
			Version int64  `json:"version"`
			Deleted bool   `json:"deleted"`
			Hash    string `json:"hash"`
			Size    int64  `json:"size"`
			Created string `json:"created"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("versions = %d %s", rec.Code, rec.Body)
	}
	if len(list.Versions) != 3 || list.Versions[0].Version != 5 || !list.Versions[0].Deleted || list.Versions[1].Version != 4 || list.Versions[1].Hash != cloudsave.Hash([]byte("v4")) || list.Versions[2].Created != "2026-11-24T10:02:00.000Z" {
		t.Fatalf("versions = %+v", list.Versions)
	}
	if rec := f.serve(t, request{method: "GET", path: "/v1/saves/main/versions/3", bearer: tok}); rec.Code != http.StatusOK || rec.Body.String() != "v3" || rec.Header().Get("Save-Version") != "3" {
		t.Fatalf("version 3 = %d %q", rec.Code, rec.Body)
	}
	if rec := f.serve(t, request{method: "GET", path: "/v1/saves/main/versions/5", bearer: tok}); rec.Code != http.StatusGone {
		t.Fatalf("deletion version = %d", rec.Code)
	}
	if rec := f.serve(t, request{method: "GET", path: "/v1/saves/main/versions/1", bearer: tok}); rec.Code != http.StatusNotFound {
		t.Fatalf("pruned version = %d", rec.Code)
	}
	for _, p := range []string{"/v1/saves/main/versions/0", "/v1/saves/main/versions/x"} {
		if rec := f.serve(t, request{method: "GET", path: p, bearer: tok}); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d", p, rec.Code)
		}
	}
	// The Go API restores a version byte for byte, and can restore the deletion too.
	ctx := context.Background()
	v, err := f.svc.Restore(ctx, "acct", "main", 3, cloudsave.MatchHash(""))
	if err != nil || v.Number != 6 || v.Hash != cloudsave.Hash([]byte("v3")) {
		t.Fatalf("Restore = %+v, %v", v, err)
	}
	if v, err := f.svc.Restore(ctx, "acct", "main", 5, cloudsave.MatchHash(cloudsave.Hash([]byte("v3")))); err != nil || !v.Deleted || v.Number != 7 {
		t.Fatalf("Restore of a deletion = %+v, %v", v, err)
	}
	if rec := f.serve(t, request{method: "DELETE", path: "/v1/saves/main/versions", bearer: tok}); rec.Code != http.StatusNoContent {
		t.Fatalf("purge = %d", rec.Code)
	}
	if rec := f.serve(t, request{method: "GET", path: "/v1/saves/main/versions", bearer: tok}); rec.Code != http.StatusNotFound {
		t.Fatalf("versions after purge = %d", rec.Code)
	}
}

func TestHTTPAuthAndNames(t *testing.T) {
	f := newFixture(t, nil)
	for _, bearer := range []string{"", "not-a-token"} {
		rec := f.serve(t, request{method: "GET", path: "/v1/saves", bearer: bearer})
		if code, _ := errorCode(t, rec); rec.Code != http.StatusUnauthorized || code != "invalid_token" || rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("bearer %q = %d %s", bearer, rec.Code, rec.Body)
		}
	}
	// A token from another key is refused.
	other := newFixture(t, nil)
	if rec := f.serve(t, request{method: "GET", path: "/v1/saves", bearer: other.token(t, "acct")}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("foreign token = %d", rec.Code)
	}
	tok := f.token(t, "acct")
	if rec := f.serve(t, request{method: "PUT", path: "/v1/saves/%01bad", bearer: tok, body: "x"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("control character slot = %d", rec.Code)
	}
	// Escaped names round trip: a space and a dot are fine, a slash is not.
	if rec := f.serve(t, request{method: "PUT", path: "/v1/saves/slot%201.conflict-web", bearer: tok, body: "x"}); rec.Code != http.StatusOK {
		t.Fatalf("escaped slot = %d %s", rec.Code, rec.Body)
	}
	if rec := f.serve(t, request{method: "PUT", path: "/v1/saves/a%2Fb", bearer: tok, body: "x"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("slash slot = %d", rec.Code)
	}
	if names, err := f.svc.List(context.Background(), "acct"); err != nil || len(names) != 1 || names[0].Slot != "slot 1.conflict-web" {
		t.Fatalf("List = %+v, %v", names, err)
	}
}

func TestHTTPCORS(t *testing.T) {
	f := newFixture(t, nil)
	pre := func(origin string) *httptest.ResponseRecorder {
		return f.serve(t, request{method: http.MethodOptions, path: "/v1/saves/main", header: http.Header{
			"Origin":                         {origin},
			"Access-Control-Request-Method":  {"PUT"},
			"Access-Control-Request-Headers": {"authorization, if-match"},
		}})
	}
	rec := pre("https://play.example.com")
	h := rec.Header()
	if rec.Code != http.StatusNoContent || h.Get("Access-Control-Allow-Origin") != "https://play.example.com" || !strings.Contains(h.Get("Access-Control-Allow-Headers"), "If-Match") || !strings.Contains(h.Get("Access-Control-Allow-Methods"), "PUT") {
		t.Fatalf("allowed preflight = %d %v", rec.Code, h)
	}
	if rec := pre("https://evil.example.com"); rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign preflight = %d %v", rec.Code, rec.Header())
	}
	tok := f.token(t, "acct")
	f.serve(t, request{method: "PUT", path: "/v1/saves/main", bearer: tok, body: "x"})
	rec = f.serve(t, request{method: "GET", path: "/v1/saves/main", bearer: tok, header: http.Header{"Origin": {"https://play.example.com"}}})
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://play.example.com" || !strings.Contains(rec.Header().Get("Access-Control-Expose-Headers"), "ETag") || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("CORS GET headers = %v", rec.Header())
	}
	rec = f.serve(t, request{method: "GET", path: "/v1/saves/main", bearer: tok, header: http.Header{"Origin": {"https://evil.example.com"}}})
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign origin allowed: %v", rec.Header())
	}
}

// TestHTTPRefusesGameServerToken checks that the token a player sends a game server, which that server can replay, reaches none of the player's slots: every route answers 401 and nothing is written or purged.
func TestHTTPRefusesGameServerToken(t *testing.T) {
	f := newFixture(t, nil)
	if rec := f.serve(t, request{method: "PUT", path: "/v1/saves/main", bearer: f.token(t, "acct"), body: "progress"}); rec.Code != http.StatusOK {
		t.Fatalf("PUT with a cloudsave token = %d %s", rec.Code, rec.Body)
	}
	gameTok, _, err := f.signer.Issue("acct")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []request{
		{method: "GET", path: "/v1/saves"},
		{method: "GET", path: "/v1/saves/main"},
		{method: "PUT", path: "/v1/saves/main", body: "overwritten"},
		{method: "DELETE", path: "/v1/saves/main"},
		{method: "GET", path: "/v1/saves/main/versions"},
		{method: "GET", path: "/v1/saves/main/versions/1"},
		{method: "DELETE", path: "/v1/saves/main/versions"},
	} {
		r.bearer = gameTok
		rec := f.serve(t, r)
		if code, _ := errorCode(t, rec); rec.Code != http.StatusUnauthorized || code != "invalid_token" {
			t.Errorf("%s %s with a game server token = %d %s, want 401 invalid_token", r.method, r.path, rec.Code, rec.Body)
		}
	}
	if v, err := f.svc.Load(context.Background(), "acct", "main"); err != nil || string(v.Data) != "progress" || v.Number != 1 {
		t.Fatalf("slot after game server token requests = %+v, %v", v, err)
	}
}

func TestNewRefusesVerifierForAnotherAudience(t *testing.T) {
	key, err := authtoken.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authtoken.NewSigner(key, "https://accounts.example.com", "test-game", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	game, err := authtoken.NewVerifier(signer.KeySet())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cloudsave.New(cloudsave.DefaultConfig(), cloudsave.Options{Store: cloudsave.NewMemoryStore(), Tokens: game}); err == nil {
		t.Fatal("New accepted a verifier for the game servers' audience")
	}
	if got := (cloudsave.Config{}).TokenAudience(); got != cloudsave.DefaultAudience {
		t.Fatalf("TokenAudience of a config without audience = %q", got)
	}
	cfg := cloudsave.DefaultConfig()
	cfg.Audience = "saves"
	saves, err := authtoken.NewVerifierFor(signer.KeySet(), "saves")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cloudsave.New(cfg, cloudsave.Options{Store: cloudsave.NewMemoryStore(), Tokens: saves}); err != nil {
		t.Fatalf("New with a configured audience = %v", err)
	}
}

func TestNewRefusesIncompleteOptions(t *testing.T) {
	if _, err := cloudsave.New(cloudsave.DefaultConfig(), cloudsave.Options{Store: cloudsave.NewMemoryStore()}); err == nil {
		t.Fatal("New without a token verifier succeeded")
	}
	f := newFixture(t, nil)
	cfg := f.svc.Config()
	cfg.KeepVersions = 0
	if _, err := cloudsave.New(cfg, cloudsave.Options{}); err == nil {
		t.Fatal("New with an invalid config succeeded")
	}
}

func TestValidateSlot(t *testing.T) {
	for _, ok := range []string{"main", "slot 1", "セーブ", "a.conflict-phone-2", "...", strings.Repeat("x", 200)} {
		if err := cloudsave.ValidateSlot(ok); err != nil {
			t.Errorf("ValidateSlot(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", "a\nb", "a\x7fb", "a\u0085b", strings.Repeat("x", 201), string([]byte{0xff})} {
		if err := cloudsave.ValidateSlot(bad); !errors.Is(err, cloudsave.ErrInvalidSlot) {
			t.Errorf("ValidateSlot(%q) = %v", bad, err)
		}
	}
}

func BenchmarkHTTPSaveLoad(b *testing.B) {
	key, _ := authtoken.GenerateKey(nil)
	signer, _ := authtoken.NewSigner(key, "https://accounts.example.com", "bench", time.Hour)
	verifier, _ := authtoken.NewVerifier(signer.KeySet())
	svc, err := cloudsave.New(cloudsave.DefaultConfig(), cloudsave.Options{Store: cloudsave.NewMemoryStore(), Tokens: verifier})
	if err != nil {
		b.Fatal(err)
	}
	h := svc.Handler()
	tok, _, _ := signer.Issue("acct")
	payload := bytes.Repeat([]byte("x"), 16<<10)
	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest("PUT", "/v1/saves/main", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		req = httptest.NewRequest("GET", "/v1/saves/main", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatal(rec.Code)
		}
	}
}
