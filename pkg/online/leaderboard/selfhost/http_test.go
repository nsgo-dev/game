package selfhost_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts"
	"github.com/nsgo-dev/game/pkg/online/accounts/token"
	"github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost"
)

// fixture is an account service and a leaderboard service that trusts its tokens through the published key set, as two separate deployments would.
type fixture struct {
	clock    *clock
	accounts *accounts.Service
	boards   *selfhost.Service
	h        http.Handler
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	c := &clock{t0}
	// A platform identity verifier that trusts the credential as the subject, standing in for Play Games' server auth code exchange.
	playGames := accounts.IdentityVerifierFunc(func(_ context.Context, credential string) (string, error) { return credential, nil })
	acc, err := accounts.New(accounts.DefaultConfig("https://accounts.example.com", "my-game"), accounts.Options{
		Store:      accounts.NewMemoryStore(),
		Key:        seedKey(1),
		Identities: map[string]accounts.IdentityVerifier{"playgames": playGames},
		Now:        c.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	ksYAML, err := acc.KeySet().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	ks, err := token.ParseKeySet(ksYAML)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := selfhost.ParseConfig([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	v, err := token.NewVerifierFor(ks, cfg.TokenAudience())
	if err != nil {
		t.Fatal(err)
	}
	v.Now = c.Now
	boards, err := selfhost.New(context.Background(), cfg, selfhost.Options{Store: selfhost.NewMemoryStore(), Tokens: v, Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{clock: c, accounts: acc, boards: boards, h: boards.Handler()}
}

// token returns a leaderboard token for creds' session, as a device gets from the account service's refresh.
func (f *fixture) token(t *testing.T, creds accounts.Credentials) string {
	t.Helper()
	c, err := f.accounts.RefreshFor(context.Background(), creds.Session, f.boards.Config().TokenAudience())
	if err != nil {
		t.Fatal(err)
	}
	return c.Token.Reveal()
}

type pageJSON struct {
	Board  string `json:"board"`
	Period struct {
		ID    string `json:"id"`
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"period"`
	Total   int `json:"total"`
	Entries []struct {
		Rank     int    `json:"rank"`
		Account  string `json:"account"`
		Score    int64  `json:"score"`
		Achieved string `json:"achieved"`
	} `json:"entries"`
}

func (p pageJSON) ranking() []string {
	out := []string{}
	for _, e := range p.Entries {
		out = append(out, fmt.Sprintf("%d:%s:%d", e.Rank, e.Account, e.Score))
	}
	return out
}

type resultJSON struct {
	Period struct {
		ID string `json:"id"`
	} `json:"period"`
	Entry struct {
		Rank    int    `json:"rank"`
		Account string `json:"account"`
		Score   int64  `json:"score"`
	} `json:"entry"`
	Total    int  `json:"total"`
	Improved bool `json:"improved"`
}

type errorJSON struct {
	Error string `json:"error"`
}

// call serves one request on h without a network and decodes a JSON answer into out.
func call(t *testing.T, h http.Handler, method, path, bearer, body string, out any) int {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%s %s: Cache-Control = %q", method, path, rec.Header().Get("Cache-Control"))
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: %v in %q", method, path, err, rec.Body)
		}
	}
	return rec.Code
}

func TestScoresFromTwoPlatformsAppearOnOneBoardInOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Ada plays in a browser as a guest, Bob on Android with Play Games.
	adaWeb, err := f.accounts.CreateGuest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bobAndroid, err := f.accounts.SignInIdentity(ctx, "playgames", "g-bob")
	if err != nil {
		t.Fatal(err)
	}
	post := func(tok string, score int64) resultJSON {
		t.Helper()
		var res resultJSON
		if code := call(t, f.h, "POST", "/v1/boards/high-score/scores", tok, fmt.Sprintf(`{"score":%d}`, score), &res); code != http.StatusOK {
			t.Fatalf("submit %d = %d", score, code)
		}
		return res
	}
	post(f.token(t, adaWeb), 300)
	f.clock.advance(1)
	if res := post(f.token(t, bobAndroid), 500); res.Entry.Rank != 1 || res.Total != 2 || !res.Improved || res.Entry.Account != bobAndroid.AccountID {
		t.Fatalf("bob = %+v", res)
	}

	// Ada links Play Games on the web and continues on Android: the same account, so her better score replaces her web one.
	if err := f.accounts.LinkIdentity(ctx, adaWeb.AccountID, "playgames", "g-ada"); err != nil {
		t.Fatal(err)
	}
	adaAndroid, err := f.accounts.SignInIdentity(ctx, "playgames", "g-ada")
	if err != nil || adaAndroid.AccountID != adaWeb.AccountID {
		t.Fatalf("ada on Android = %+v, %v", adaAndroid, err)
	}
	f.clock.advance(1)
	if res := post(f.token(t, adaAndroid), 800); res.Entry.Rank != 1 || res.Total != 2 {
		t.Fatalf("ada on Android = %+v", res)
	}
	if res := post(f.token(t, adaWeb), 100); res.Improved || res.Entry.Score != 800 {
		t.Fatalf("ada's worse web score = %+v", res)
	}

	var page pageJSON
	if code := call(t, f.h, "GET", "/v1/boards/high-score/top", f.token(t, bobAndroid), "", &page); code != http.StatusOK {
		t.Fatalf("top = %d", code)
	}
	want := []string{"1:" + adaWeb.AccountID + ":800", "2:" + bobAndroid.AccountID + ":500"}
	if got := page.ranking(); !slices.Equal(got, want) || page.Total != 2 || page.Period.ID != "all" || page.Period.Start != "" {
		t.Fatalf("top = %+v, want %v", page, want)
	}
	page = pageJSON{}
	if code := call(t, f.h, "GET", "/v1/boards/high-score/around?radius=0", f.token(t, bobAndroid), "", &page); code != http.StatusOK || !slices.Equal(page.ranking(), want[1:]) {
		t.Fatalf("around = %d %+v", code, page)
	}
	page = pageJSON{}
	body := fmt.Sprintf(`{"accounts":[%q,%q]}`, bobAndroid.AccountID, "someone-else")
	if code := call(t, f.h, "POST", "/v1/boards/high-score/friends", f.token(t, adaWeb), body, &page); code != http.StatusOK || !slices.Equal(page.ranking(), want[1:]) {
		t.Fatalf("friends = %d %+v", code, page)
	}
}

func TestHTTPWeeklyPeriodsAndSignedScores(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ada, err := f.accounts.CreateGuest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tok := f.token(t, ada)
	var res resultJSON
	if code := call(t, f.h, "POST", "/v1/boards/weekly/scores", tok, `{"score":42}`, &res); code != http.StatusOK || res.Period.ID != "2026-11-23" {
		t.Fatalf("weekly = %d %+v", code, res)
	}
	var boards struct {
		Boards []struct {
			Name       string `json:"name"`
			Reset      string `json:"reset"`
			Validation string `json:"validation"`
			Current    struct {
				ID    string `json:"id"`
				Start string `json:"start"`
				End   string `json:"end"`
			} `json:"current"`
			Previous []struct {
				ID string `json:"id"`
			} `json:"previous"`
		} `json:"boards"`
	}
	if code := call(t, f.h, "GET", "/v1/boards", tok, "", &boards); code != http.StatusOK || len(boards.Boards) != 4 {
		t.Fatalf("boards = %d %+v", code, boards)
	}
	w := boards.Boards[1]
	if w.Name != "weekly" || w.Current.Start != "2026-11-23T00:00:00Z" || w.Current.End != "2026-11-30T00:00:00Z" || len(w.Previous) != 1 || w.Previous[0].ID != "2026-11-16" || boards.Boards[3].Validation != "signed" {
		t.Fatalf("weekly board = %+v", boards.Boards)
	}

	f.clock.advance(5 * 24 * time.Hour) // Monday 2026-11-30, 15:30
	// The token expired with the clock; sign in again.
	again, err := f.accounts.Refresh(ctx, ada.Session)
	if err != nil {
		t.Fatal(err)
	}
	tok = f.token(t, again)
	var page pageJSON
	if code := call(t, f.h, "GET", "/v1/boards/weekly/top?period=previous", tok, "", &page); code != http.StatusOK || page.Period.ID != "2026-11-23" || page.Total != 1 {
		t.Fatalf("previous week = %d %+v", code, page)
	}
	page = pageJSON{}
	if code := call(t, f.h, "GET", "/v1/boards/weekly/top", tok, "", &page); code != http.StatusOK || page.Period.ID != "2026-11-30" || page.Total != 0 || page.Entries == nil {
		t.Fatalf("this week = %d %+v", code, page)
	}

	var e errorJSON
	if code := call(t, f.h, "POST", "/v1/boards/ranked/scores", tok, `{"score":10}`, &e); code != http.StatusForbidden || e.Error != "proof_required" {
		t.Fatalf("unsigned = %d %+v", code, e)
	}
	server := mustSigner(9)
	server.Now = f.clock.Now
	proof, err := server.Sign("ranked", ada.AccountID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if code := call(t, f.h, "POST", "/v1/boards/ranked/scores", tok, fmt.Sprintf(`{"score":11,"proof":%q}`, proof), &e); code != http.StatusForbidden || e.Error != "invalid_proof" {
		t.Fatalf("proof for another score = %d %+v", code, e)
	}
	res = resultJSON{}
	if code := call(t, f.h, "POST", "/v1/boards/ranked/scores", tok, fmt.Sprintf(`{"score":10,"proof":%q}`, proof), &res); code != http.StatusOK || !res.Improved {
		t.Fatalf("signed = %d %+v", code, res)
	}
	// Bob cannot submit Ada's proof as his own.
	bob, err := f.accounts.CreateGuest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if code := call(t, f.h, "POST", "/v1/boards/ranked/scores", f.token(t, bob), fmt.Sprintf(`{"score":10,"proof":%q}`, proof), &e); code != http.StatusForbidden || e.Error != "invalid_proof" {
		t.Fatalf("someone else's proof = %d %+v", code, e)
	}
	f.clock.advance(5 * time.Minute)
	if code := call(t, f.h, "POST", "/v1/boards/ranked/scores", tok, fmt.Sprintf(`{"score":10,"proof":%q}`, proof), &e); code != http.StatusForbidden || e.Error != "proof_expired" {
		t.Fatalf("expired = %d %+v", code, e)
	}
}

func TestHTTPErrors(t *testing.T) {
	f := newFixture(t)
	ada, err := f.accounts.CreateGuest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tok := f.token(t, ada)
	cases := []struct {
		method, path, bearer, body string
		status                     int
		code                       string
	}{
		{"GET", "/v1/boards", "", "", 401, "invalid_token"},
		{"GET", "/v1/boards/high-score/top", "not-a-token", "", 401, "invalid_token"},
		{"POST", "/v1/boards/high-score/scores", "", `{"score":1}`, 401, "invalid_token"},
		{"POST", "/v1/boards/nope/scores", tok, `{"score":1}`, 404, "unknown_board"},
		{"GET", "/v1/boards/nope/top", tok, "", 404, "unknown_board"},
		{"GET", "/v1/boards/weekly/top?period=1999-01-04", tok, "", 404, "unknown_period"},
		{"GET", "/v1/boards/high-score/around", tok, "", 404, "not_ranked"},
		{"POST", "/v1/boards/high-score/scores", tok, `{}`, 400, "bad_request"},
		{"POST", "/v1/boards/high-score/scores", tok, `{"score":1.5}`, 400, "bad_request"},
		{"POST", "/v1/boards/high-score/scores", tok, `{"score":"1"}`, 400, "bad_request"},
		{"POST", "/v1/boards/high-score/scores", tok, `{"score":1,"name":"x"}`, 400, "bad_request"},
		{"POST", "/v1/boards/high-score/scores", tok, `{"score":1}}`, 400, "bad_request"},
		{"POST", "/v1/boards/high-score/scores", tok, `{"score":9007199254740992}`, 400, "score_out_of_range"},
		{"POST", "/v1/boards/speedrun/scores", tok, `{"score":0}`, 400, "score_out_of_range"},
		{"GET", "/v1/boards/high-score/top?limit=0", tok, "", 400, "bad_request"},
		{"GET", "/v1/boards/high-score/top?limit=101", tok, "", 400, "bad_request"},
		{"GET", "/v1/boards/high-score/top?offset=-1", tok, "", 400, "bad_request"},
		{"GET", "/v1/boards/high-score/top?limit=+5", tok, "", 400, "bad_request"},
		{"GET", "/v1/boards/high-score/around?radius=51", tok, "", 400, "bad_request"},
		{"POST", "/v1/boards/high-score/friends", tok, `{"accounts":[""]}`, 400, "invalid_account"},
		{"POST", "/v1/boards/high-score/friends", tok, `{"accounts":` + strings.Repeat(" ", 70<<10) + `[]}`, 413, "request_too_large"},
	}
	for _, tc := range cases {
		var e errorJSON
		if code := call(t, f.h, tc.method, tc.path, tc.bearer, tc.body, &e); code != tc.status || e.Error != tc.code {
			t.Errorf("%s %s %s = %d %q, want %d %q", tc.method, tc.path, tc.body, code, e.Error, tc.status, tc.code)
		}
	}
}

func TestHandlerNeedsATokenVerifier(t *testing.T) {
	svc := newService(t, selfhost.NewMemoryStore(), &clock{t0})
	defer func() {
		if recover() == nil {
			t.Fatal("Handler without Tokens did not panic")
		}
	}()
	svc.Handler()
}

// TestHTTPRefusesGameServerToken checks that the token a player sends a game server, which that server can replay, cannot submit scores or read boards for the player.
func TestHTTPRefusesGameServerToken(t *testing.T) {
	f := newFixture(t)
	ada, err := f.accounts.CreateGuest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gameTok := ada.Token.Reveal()
	for _, r := range []struct{ method, path, body string }{
		{"GET", "/v1/boards", ""},
		{"POST", "/v1/boards/high-score/scores", `{"score":4200}`},
		{"GET", "/v1/boards/high-score/top", ""},
		{"GET", "/v1/boards/high-score/around", ""},
		{"POST", "/v1/boards/high-score/friends", `{"accounts":[]}`},
	} {
		var e errorJSON
		if code := call(t, f.h, r.method, r.path, gameTok, r.body, &e); code != http.StatusUnauthorized || e.Error != "invalid_token" {
			t.Errorf("%s %s with a game server token = %d %+v, want 401 invalid_token", r.method, r.path, code, e)
		}
	}
	var page pageJSON
	if code := call(t, f.h, "GET", "/v1/boards/high-score/top", f.token(t, ada), "", &page); code != http.StatusOK || page.Total != 0 {
		t.Fatalf("top after game server token requests = %d %+v", code, page)
	}
	if got := (selfhost.Config{}).TokenAudience(); got != selfhost.DefaultAudience {
		t.Fatalf("TokenAudience of a config without audience = %q", got)
	}
	game, err := token.NewVerifier(f.accounts.KeySet())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := selfhost.New(context.Background(), f.boards.Config(), selfhost.Options{Store: selfhost.NewMemoryStore(), Tokens: game}); err == nil {
		t.Fatal("New accepted a verifier for the game servers' audience")
	}
}

func TestHTTPCORS(t *testing.T) {
	newWithOrigins := func(origins []string) (*fixture, string) {
		t.Helper()
		f := newFixture(t)
		cfg := f.boards.Config()
		cfg.AllowedOrigins = origins
		ksYAML, err := f.accounts.KeySet().Marshal()
		if err != nil {
			t.Fatal(err)
		}
		ks, err := token.ParseKeySet(ksYAML)
		if err != nil {
			t.Fatal(err)
		}
		v, err := token.NewVerifierFor(ks, cfg.TokenAudience())
		if err != nil {
			t.Fatal(err)
		}
		v.Now = f.clock.Now
		boards, err := selfhost.New(context.Background(), cfg, selfhost.Options{Store: selfhost.NewMemoryStore(), Tokens: v, Now: f.clock.Now})
		if err != nil {
			t.Fatal(err)
		}
		f.boards = boards
		f.h = boards.Handler()
		ada, err := f.accounts.CreateGuest(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return f, f.token(t, ada)
	}

	f, tok := newWithOrigins([]string{"https://play.example.com", "http://localhost:8080"})

	preflight := func(h http.Handler, path, origin, method, headers string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, path, nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", method)
		if headers != "" {
			req.Header.Set("Access-Control-Request-Headers", headers)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// Preflight from listed origins succeeds on both POST and GET endpoints.
	for _, tc := range []struct{ path, origin, method string }{
		{"/v1/boards/high-score/scores", "https://play.example.com", http.MethodPost},
		{"/v1/boards/high-score/top", "http://localhost:8080", http.MethodGet},
	} {
		rec := preflight(f.h, tc.path, tc.origin, tc.method, "authorization, content-type")
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
	if rec := preflight(f.h, "/v1/boards/high-score/scores", "https://evil.example.com", http.MethodPost, "authorization, content-type"); rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("foreign preflight = %d %v", rec.Code, rec.Header())
	}

	// Empty allowedOrigins refuses cross-origin preflights.
	noCORS, _ := newWithOrigins(nil)
	if rec := preflight(noCORS.h, "/v1/boards/high-score/scores", "https://play.example.com", http.MethodPost, "authorization"); rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("empty allowedOrigins preflight = %d %v", rec.Code, rec.Header())
	}

	// Real POST score submission and GET top from a listed origin carry Access-Control-Allow-Origin and Vary: Origin.
	req := httptest.NewRequest(http.MethodPost, "/v1/boards/high-score/scores", strings.NewReader(`{"score":420}`))
	req.Header.Set("Origin", "https://play.example.com")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "https://play.example.com" || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("POST score = %d %v", rec.Code, rec.Header())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/boards/high-score/top", nil)
	req.Header.Set("Origin", "https://play.example.com")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "https://play.example.com" || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("GET top = %d %v", rec.Code, rec.Header())
	}

	// Error response to a listed origin still carries Access-Control-Allow-Origin.
	req = httptest.NewRequest(http.MethodGet, "/v1/boards/high-score/top", nil)
	req.Header.Set("Origin", "https://play.example.com")
	rec = httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("Access-Control-Allow-Origin") != "https://play.example.com" || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("unauthed GET top = %d %v", rec.Code, rec.Header())
	}

	// Real request from an unlisted origin gets Vary: Origin and no Access-Control-Allow-Origin.
	req = httptest.NewRequest(http.MethodGet, "/v1/boards/high-score/top", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("foreign GET top = %d %v", rec.Code, rec.Header())
	}

	// Native request without Origin gets no CORS or Vary headers.
	req = httptest.NewRequest(http.MethodGet, "/v1/boards/high-score/top", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Vary") != "" {
		t.Fatalf("native GET top = %d %v", rec.Code, rec.Header())
	}
}
