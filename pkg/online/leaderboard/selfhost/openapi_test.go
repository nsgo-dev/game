package selfhost

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi-validator/config"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	acctoken "github.com/nsgo-dev/game/pkg/online/accounts/token"
)

func loadOpenAPI(t *testing.T) (libopenapi.Document, *v3.Document, validator.Validator) {
	t.Helper()
	doc, err := libopenapi.NewDocument(openapiSpec)
	if err != nil {
		t.Fatalf("libopenapi.NewDocument: %v", err)
	}
	if !strings.HasPrefix(doc.GetVersion(), "3.1.") {
		t.Fatalf("openapi version = %q, want 3.1.x", doc.GetVersion())
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("BuildV3Model: %v", err)
	}
	v, errs := validator.NewValidator(
		doc,
		config.WithStrictMode(),
		config.WithFormatAssertions(),
		config.WithStrictIgnoredHeaders("Content-Type", "Content-Length", "Accept", "Accept-Encoding", "Host", "User-Agent"),
	)
	if len(errs) > 0 {
		t.Fatalf("NewValidator: %v", errs)
	}
	t.Cleanup(v.Release)
	return doc, &model.Model, v
}

func TestOpenAPIDocumentValid(t *testing.T) {
	_, model, v := loadOpenAPI(t)
	if model.Info == nil || model.Info.Version == "" {
		t.Fatal("openapi.yaml must declare info.version")
	}
	if ok, errs := v.ValidateDocument(); !ok {
		for _, e := range errs {
			t.Errorf("document validation error: %s", e.Error())
		}
	}
}

type contractFixture struct {
	svc       *Service
	tokenSign *acctoken.Signer
	proofSign *ProofSigner
	now       time.Time
}

func newContractFixture(t *testing.T, store Store) *contractFixture {
	t.Helper()
	accKey, err := acctoken.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokenSign, err := acctoken.NewSigner(accKey, "https://accounts.example.com", "test-game", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := acctoken.NewVerifier(tokenSign.KeySet())
	if err != nil {
		t.Fatal(err)
	}
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 42
	serverKey := ed25519.NewKeyFromSeed(seed)
	proofSign, err := NewProofSigner(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 25, 15, 30, 0, 0, time.UTC)
	f := &contractFixture{tokenSign: tokenSign, proofSign: proofSign, now: now}
	verifier.Now = func() time.Time { return f.now }
	proofSign.Now = func() time.Time { return f.now }
	tokenSign.Now = func() time.Time { return f.now }

	minScore := int64(1)
	maxScore := int64(100000)
	cfg := Config{
		Version:  ConfigVersion,
		ProofTTL: Duration(5 * time.Minute),
		Boards: []Board{
			{Name: "high-score", Order: Descending, Reset: AllTime},
			{Name: "weekly", Order: Descending, Reset: Weekly, WeekStart: "monday", Keep: 2},
			{Name: "speedrun", Order: Ascending, Reset: Daily, Keep: 2, Min: &minScore, Max: &maxScore},
			{Name: "ranked", Order: Descending, Reset: AllTime, Validation: SignedByServer},
		},
		ServerKeys: []acctoken.PublicKey{proofSign.PublicKey()},
	}
	if store == nil {
		store = NewMemoryStore()
	}
	svc, err := New(context.Background(), cfg, Options{Store: store, Tokens: verifier, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

func (f *contractFixture) token(t *testing.T, account string) string {
	t.Helper()
	tok, _, err := f.tokenSign.Issue(account)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func specRoutes(model *v3.Document) []string {
	var routes []string
	if model.Paths == nil || model.Paths.PathItems == nil {
		return routes
	}
	for pathStr, item := range model.Paths.PathItems.FromOldest() {
		for method := range item.GetOperations().FromOldest() {
			routes = append(routes, strings.ToUpper(method)+" "+pathStr)
		}
	}
	slices.Sort(routes)
	return routes
}

func TestOpenAPIRoutesMatchHandler(t *testing.T) {
	_, model, _ := loadOpenAPI(t)
	f := newContractFixture(t, nil)
	_, handlerRoutes := f.svc.newMux()
	slices.Sort(handlerRoutes)
	wantRoutes := specRoutes(model)
	if !slices.Equal(handlerRoutes, wantRoutes) {
		t.Fatalf("handler routes != openapi.yaml routes\nhandler: %v\nspec:    %v", handlerRoutes, wantRoutes)
	}

	// Ensure http.go registers routes only through the recording handle helper.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "http.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	muxHandleCalls := 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "mux" && (sel.Sel.Name == "Handle" || sel.Sel.Name == "HandleFunc") {
			muxHandleCalls++
		}
		return true
	})
	if muxHandleCalls != 1 {
		t.Fatalf("http.go has %d direct mux.Handle/HandleFunc calls, want 1 (inside the recording handle helper)", muxHandleCalls)
	}
}

func specErrorCodes(model *v3.Document) map[int]map[string]bool {
	out := map[int]map[string]bool{}
	if model.Paths == nil || model.Paths.PathItems == nil {
		return out
	}
	for _, item := range model.Paths.PathItems.FromOldest() {
		for _, op := range item.GetOperations().FromOldest() {
			if op.Responses == nil || op.Responses.Codes == nil {
				continue
			}
			for statusStr, resp := range op.Responses.Codes.FromOldest() {
				status, err := strconv.Atoi(statusStr)
				if err != nil || status < 400 || resp.Content == nil {
					continue
				}
				mt, ok := resp.Content.Get("application/json")
				if !ok || mt.Schema == nil {
					continue
				}
				schema := mt.Schema.Schema()
				if schema == nil || schema.Properties == nil {
					continue
				}
				errProp, ok := schema.Properties.Get("error")
				if !ok {
					continue
				}
				errSchema := errProp.Schema()
				if errSchema == nil {
					continue
				}
				for _, ev := range errSchema.Enum {
					if out[status] == nil {
						out[status] = map[string]bool{}
					}
					out[status][ev.Value] = true
				}
			}
		}
	}
	return out
}

func TestOpenAPIErrorCodesMatchHandler(t *testing.T) {
	_, model, _ := loadOpenAPI(t)
	documented := specErrorCodes(model)

	handlerCodes := map[int]map[string]bool{
		http.StatusInternalServerError: {"internal": true},
	}
	for _, m := range errorMappings {
		if handlerCodes[m.status] == nil {
			handlerCodes[m.status] = map[string]bool{}
		}
		handlerCodes[m.status][m.code] = true
	}

	// Extract every errorResponse literal written in http.go so direct writes cannot drift.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "http.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	literalCodes := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		id, ok := lit.Type.(*ast.Ident)
		if !ok || id.Name != "errorResponse" {
			return true
		}
		for _, elt := range lit.Elts {
			switch e := elt.(type) {
			case *ast.KeyValueExpr:
				if k, ok := e.Key.(*ast.Ident); ok && k.Name == "Error" {
					if bl, ok := e.Value.(*ast.BasicLit); ok && bl.Kind == token.STRING {
						if s, err := strconv.Unquote(bl.Value); err == nil {
							literalCodes[s] = true
						}
					}
				}
			case *ast.BasicLit:
				if e.Kind == token.STRING {
					if s, err := strconv.Unquote(e.Value); err == nil {
						literalCodes[s] = true
					}
				}
			}
		}
		return true
	})

	for status, codes := range handlerCodes {
		for code := range codes {
			if !documented[status][code] {
				t.Errorf("writeError maps %d %q, which is missing from openapi.yaml", status, code)
			}
		}
	}
	allDocumented := map[string]bool{}
	for _, codes := range documented {
		for code := range codes {
			allDocumented[code] = true
		}
	}
	for code := range literalCodes {
		if !allDocumented[code] {
			t.Errorf("http.go writes errorResponse %q, which is missing from openapi.yaml", code)
		}
	}
	allHandler := map[string]bool{}
	for _, codes := range handlerCodes {
		for code := range codes {
			allHandler[code] = true
		}
	}
	for code := range literalCodes {
		allHandler[code] = true
	}
	for status, codes := range documented {
		for code := range codes {
			if !allHandler[code] {
				t.Errorf("openapi.yaml documents %d %q, which http.go never writes", status, code)
			}
		}
	}
}

func TestOpenAPIServed(t *testing.T) {
	f := newContractFixture(t, nil)
	h := f.svc.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/openapi.yaml = %d %s", rec.Code, rec.Body)
	}
	mediaType, _, err := mime.ParseMediaType(rec.Header().Get("Content-Type"))
	if err != nil || mediaType != "application/yaml" {
		t.Fatalf("Content-Type = %q (parsed %q, err %v)", rec.Header().Get("Content-Type"), mediaType, err)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	if !bytes.Equal(rec.Body.Bytes(), openapiSpec) {
		t.Fatal("GET /v1/openapi.yaml body does not match embedded openapi.yaml")
	}
}

type failingSaveStore struct {
	err error
}

func (f failingSaveStore) Load(context.Context, func(Record) error) error { return nil }
func (f failingSaveStore) Save(context.Context, Record) error             { return f.err }
func (f failingSaveStore) Drop(context.Context, string, string) error     { return nil }

func findSpecResponse(model *v3.Document, opKey string, status int) (*v3.Response, error) {
	method, pathStr, ok := strings.Cut(opKey, " ")
	if !ok {
		return nil, fmt.Errorf("bad op key %q", opKey)
	}
	item, ok := model.Paths.PathItems.Get(pathStr)
	if !ok {
		return nil, fmt.Errorf("path %q not in spec", pathStr)
	}
	op, ok := item.GetOperations().Get(strings.ToLower(method))
	if !ok {
		return nil, fmt.Errorf("operation %s %s not in spec", method, pathStr)
	}
	resp, ok := op.Responses.Codes.Get(strconv.Itoa(status))
	if !ok {
		return nil, fmt.Errorf("status %d not documented for %s", status, opKey)
	}
	return resp, nil
}

func validateResponseHeaders(t *testing.T, model *v3.Document, opKey string, resp *http.Response) {
	t.Helper()
	specResp, err := findSpecResponse(model, opKey, resp.StatusCode)
	if err != nil {
		t.Fatalf("%v", err)
	}
	declared := map[string]*v3.Header{}
	if specResp.Headers != nil {
		for k, h := range specResp.Headers.FromOldest() {
			declared[http.CanonicalHeaderKey(k)] = h
		}
	}
	for k := range resp.Header {
		ck := http.CanonicalHeaderKey(k)
		if ck == "Content-Type" || ck == "Content-Length" {
			continue
		}
		if _, ok := declared[ck]; !ok {
			t.Errorf("%s %d: undocumented response header %q = %q", opKey, resp.StatusCode, ck, resp.Header.Get(ck))
		}
	}
	for ck, h := range declared {
		val := resp.Header.Get(ck)
		if h.Required && val == "" {
			t.Errorf("%s %d: missing required response header %q", opKey, resp.StatusCode, ck)
			continue
		}
		if val == "" || h.Schema == nil {
			continue
		}
		schema := h.Schema.Schema()
		if schema == nil {
			continue
		}
		if len(schema.Enum) > 0 {
			matched := false
			for _, ev := range schema.Enum {
				if ev.Value == val {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("%s %d: header %q = %q not in spec enum", opKey, resp.StatusCode, ck, val)
			}
		}
		if schema.Pattern != "" {
			re := regexp.MustCompile(schema.Pattern)
			if !re.MatchString(val) {
				t.Errorf("%s %d: header %q = %q does not match pattern %q", opKey, resp.StatusCode, ck, val, schema.Pattern)
			}
		}
	}
}

func TestOpenAPIContract(t *testing.T) {
	_, model, v := loadOpenAPI(t)

	type contractCase struct {
		name           string
		opKey          string
		method         string
		target         string
		body           func(t *testing.T, f *contractFixture) string
		noAuth         bool
		badToken       bool
		cancelCtx      bool
		storeErr       error
		setup          func(t *testing.T, f *contractFixture)
		skipReqCheck   bool
		wantReqInvalid bool
		wantStatus     int
		wantErrorCode  string
	}

	strBody := func(s string) func(t *testing.T, f *contractFixture) string {
		return func(*testing.T, *contractFixture) string { return s }
	}

	cases := []contractCase{
		// GET /v1/openapi.yaml
		{
			name:       "openapi/ok",
			opKey:      "GET /v1/openapi.yaml",
			method:     http.MethodGet,
			target:     "/v1/openapi.yaml",
			noAuth:     true,
			wantStatus: http.StatusOK,
		},

		// GET /v1/boards
		{
			name:       "boards/ok",
			opKey:      "GET /v1/boards",
			method:     http.MethodGet,
			target:     "/v1/boards",
			wantStatus: http.StatusOK,
		},
		{
			name:          "boards/unauthorized",
			opKey:         "GET /v1/boards",
			method:        http.MethodGet,
			target:        "/v1/boards",
			noAuth:        true,
			skipReqCheck:  true,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "invalid_token",
		},

		// POST /v1/boards/{board}/scores
		{
			name:       "scores/ok-unsigned",
			opKey:      "POST /v1/boards/{board}/scores",
			method:     http.MethodPost,
			target:     "/v1/boards/high-score/scores",
			body:       strBody(`{"score":4200}`),
			wantStatus: http.StatusOK,
		},
		{
			name:   "scores/ok-signed",
			opKey:  "POST /v1/boards/{board}/scores",
			method: http.MethodPost,
			target: "/v1/boards/ranked/scores",
			body: func(t *testing.T, f *contractFixture) string {
				proof, err := f.proofSign.Sign("ranked", "acct-1", 100)
				if err != nil {
					t.Fatal(err)
				}
				return fmt.Sprintf(`{"score":100,"proof":%q}`, proof)
			},
			wantStatus: http.StatusOK,
		},
		{
			name:           "scores/bad-request-missing-score",
			opKey:          "POST /v1/boards/{board}/scores",
			method:         http.MethodPost,
			target:         "/v1/boards/high-score/scores",
			body:           strBody(`{}`),
			wantReqInvalid: true,
			wantStatus:     http.StatusBadRequest,
			wantErrorCode:  "bad_request",
		},
		{
			name:           "scores/bad-request-unknown-field",
			opKey:          "POST /v1/boards/{board}/scores",
			method:         http.MethodPost,
			target:         "/v1/boards/high-score/scores",
			body:           strBody(`{"score":10,"extra":true}`),
			wantReqInvalid: true,
			wantStatus:     http.StatusBadRequest,
			wantErrorCode:  "bad_request",
		},
		{
			name:          "scores/out-of-range",
			opKey:         "POST /v1/boards/{board}/scores",
			method:        http.MethodPost,
			target:        "/v1/boards/speedrun/scores",
			body:          strBody(`{"score":0}`),
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "score_out_of_range",
		},
		{
			name:          "scores/unauthorized",
			opKey:         "POST /v1/boards/{board}/scores",
			method:        http.MethodPost,
			target:        "/v1/boards/high-score/scores",
			body:          strBody(`{"score":10}`),
			badToken:      true,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "invalid_token",
		},
		{
			name:          "scores/proof-required",
			opKey:         "POST /v1/boards/{board}/scores",
			method:        http.MethodPost,
			target:        "/v1/boards/ranked/scores",
			body:          strBody(`{"score":10}`),
			wantStatus:    http.StatusForbidden,
			wantErrorCode: "proof_required",
		},
		{
			name:          "scores/invalid-proof",
			opKey:         "POST /v1/boards/{board}/scores",
			method:        http.MethodPost,
			target:        "/v1/boards/ranked/scores",
			body:          strBody(`{"score":10,"proof":"v1.bad.0.sig"}`),
			wantStatus:    http.StatusForbidden,
			wantErrorCode: "invalid_proof",
		},
		{
			name:   "scores/proof-expired",
			opKey:  "POST /v1/boards/{board}/scores",
			method: http.MethodPost,
			target: "/v1/boards/ranked/scores",
			body: func(t *testing.T, f *contractFixture) string {
				proof, err := f.proofSign.Sign("ranked", "acct-1", 10)
				if err != nil {
					t.Fatal(err)
				}
				f.now = f.now.Add(10 * time.Minute)
				return fmt.Sprintf(`{"score":10,"proof":%q}`, proof)
			},
			wantStatus:    http.StatusForbidden,
			wantErrorCode: "proof_expired",
		},
		{
			name:          "scores/unknown-board",
			opKey:         "POST /v1/boards/{board}/scores",
			method:        http.MethodPost,
			target:        "/v1/boards/missing/scores",
			body:          strBody(`{"score":10}`),
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "unknown_board",
		},
		{
			name:          "scores/too-large",
			opKey:         "POST /v1/boards/{board}/scores",
			method:        http.MethodPost,
			target:        "/v1/boards/high-score/scores",
			body:          strBody(`{"score":10,"proof":"` + strings.Repeat("a", maxRequestBytes) + `"}`),
			skipReqCheck:  true,
			wantStatus:    http.StatusRequestEntityTooLarge,
			wantErrorCode: "request_too_large",
		},
		{
			name:          "scores/unavailable",
			opKey:         "POST /v1/boards/{board}/scores",
			method:        http.MethodPost,
			target:        "/v1/boards/high-score/scores",
			body:          strBody(`{"score":10}`),
			cancelCtx:     true,
			wantStatus:    http.StatusServiceUnavailable,
			wantErrorCode: "unavailable",
		},
		{
			name:          "scores/internal",
			opKey:         "POST /v1/boards/{board}/scores",
			method:        http.MethodPost,
			target:        "/v1/boards/high-score/scores",
			body:          strBody(`{"score":10}`),
			storeErr:      errors.New("disk full"),
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "internal",
		},

		// GET /v1/boards/{board}/top
		{
			name:   "top/ok",
			opKey:  "GET /v1/boards/{board}/top",
			method: http.MethodGet,
			target: "/v1/boards/weekly/top?period=current&offset=0&limit=10",
			setup: func(t *testing.T, f *contractFixture) {
				if _, err := f.svc.Submit(context.Background(), Submission{Board: "weekly", Account: "acct-1", Score: 500}); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:           "top/bad-limit",
			opKey:          "GET /v1/boards/{board}/top",
			method:         http.MethodGet,
			target:         "/v1/boards/high-score/top?limit=0",
			wantReqInvalid: true,
			wantStatus:     http.StatusBadRequest,
			wantErrorCode:  "bad_request",
		},
		{
			name:          "top/unauthorized",
			opKey:         "GET /v1/boards/{board}/top",
			method:        http.MethodGet,
			target:        "/v1/boards/high-score/top",
			noAuth:        true,
			skipReqCheck:  true,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "invalid_token",
		},
		{
			name:          "top/unknown-board",
			opKey:         "GET /v1/boards/{board}/top",
			method:        http.MethodGet,
			target:        "/v1/boards/missing/top",
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "unknown_board",
		},
		{
			name:          "top/unknown-period",
			opKey:         "GET /v1/boards/{board}/top",
			method:        http.MethodGet,
			target:        "/v1/boards/weekly/top?period=1999-01-04",
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "unknown_period",
		},

		// GET /v1/boards/{board}/around
		{
			name:   "around/ok",
			opKey:  "GET /v1/boards/{board}/around",
			method: http.MethodGet,
			target: "/v1/boards/high-score/around?period=current&radius=3",
			setup: func(t *testing.T, f *contractFixture) {
				if _, err := f.svc.Submit(context.Background(), Submission{Board: "high-score", Account: "acct-1", Score: 500}); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:           "around/bad-radius",
			opKey:          "GET /v1/boards/{board}/around",
			method:         http.MethodGet,
			target:         "/v1/boards/high-score/around?radius=51",
			wantReqInvalid: true,
			wantStatus:     http.StatusBadRequest,
			wantErrorCode:  "bad_request",
		},
		{
			name:          "around/unauthorized",
			opKey:         "GET /v1/boards/{board}/around",
			method:        http.MethodGet,
			target:        "/v1/boards/high-score/around",
			noAuth:        true,
			skipReqCheck:  true,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "invalid_token",
		},
		{
			name:          "around/not-ranked",
			opKey:         "GET /v1/boards/{board}/around",
			method:        http.MethodGet,
			target:        "/v1/boards/high-score/around",
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "not_ranked",
		},
		{
			name:          "around/unknown-board",
			opKey:         "GET /v1/boards/{board}/around",
			method:        http.MethodGet,
			target:        "/v1/boards/missing/around",
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "unknown_board",
		},
		{
			name:          "around/unknown-period",
			opKey:         "GET /v1/boards/{board}/around",
			method:        http.MethodGet,
			target:        "/v1/boards/weekly/around?period=1999-01-04",
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "unknown_period",
		},

		// POST /v1/boards/{board}/friends
		{
			name:   "friends/ok",
			opKey:  "POST /v1/boards/{board}/friends",
			method: http.MethodPost,
			target: "/v1/boards/high-score/friends?period=current",
			body:   strBody(`{"accounts":["acct-1","acct-2"]}`),
			setup: func(t *testing.T, f *contractFixture) {
				if _, err := f.svc.Submit(context.Background(), Submission{Board: "high-score", Account: "acct-1", Score: 500}); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:           "friends/bad-request-unknown-field",
			opKey:          "POST /v1/boards/{board}/friends",
			method:         http.MethodPost,
			target:         "/v1/boards/high-score/friends",
			body:           strBody(`{"accounts":["acct-1"],"extra":1}`),
			wantReqInvalid: true,
			wantStatus:     http.StatusBadRequest,
			wantErrorCode:  "bad_request",
		},
		{
			name:           "friends/invalid-account",
			opKey:          "POST /v1/boards/{board}/friends",
			method:         http.MethodPost,
			target:         "/v1/boards/high-score/friends",
			body:           strBody(`{"accounts":[""]}`),
			wantReqInvalid: true,
			wantStatus:     http.StatusBadRequest,
			wantErrorCode:  "invalid_account",
		},
		{
			name:          "friends/unauthorized",
			opKey:         "POST /v1/boards/{board}/friends",
			method:        http.MethodPost,
			target:        "/v1/boards/high-score/friends",
			body:          strBody(`{"accounts":["acct-1"]}`),
			noAuth:        true,
			skipReqCheck:  true,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "invalid_token",
		},
		{
			name:          "friends/unknown-board",
			opKey:         "POST /v1/boards/{board}/friends",
			method:        http.MethodPost,
			target:        "/v1/boards/missing/friends",
			body:          strBody(`{"accounts":["acct-1"]}`),
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "unknown_board",
		},
		{
			name:          "friends/unknown-period",
			opKey:         "POST /v1/boards/{board}/friends",
			method:        http.MethodPost,
			target:        "/v1/boards/weekly/friends?period=1999-01-04",
			body:          strBody(`{"accounts":["acct-1"]}`),
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "unknown_period",
		},
		{
			name:          "friends/too-large",
			opKey:         "POST /v1/boards/{board}/friends",
			method:        http.MethodPost,
			target:        "/v1/boards/high-score/friends",
			body:          strBody(`{"accounts":` + strings.Repeat(" ", maxRequestBytes+1024) + `[]}`),
			skipReqCheck:  true,
			wantStatus:    http.StatusRequestEntityTooLarge,
			wantErrorCode: "request_too_large",
		},
	}

	coveredOps := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var store Store
			if tc.storeErr != nil {
				store = failingSaveStore{err: tc.storeErr}
			}
			f := newContractFixture(t, store)
			if tc.setup != nil {
				tc.setup(t, f)
			}

			var bodyStr string
			if tc.body != nil {
				bodyStr = tc.body(t, f)
			}
			var bodyReader io.Reader
			if tc.body != nil {
				bodyReader = strings.NewReader(bodyStr)
			}
			req := httptest.NewRequest(tc.method, tc.target, bodyReader)
			if tc.body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			if tc.cancelCtx {
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
			}
			if !tc.noAuth {
				tok := "not-a-valid-token"
				if !tc.badToken {
					tok = f.token(t, "acct-1")
				}
				req.Header.Set("Authorization", "Bearer "+tok)
			}

			if !tc.skipReqCheck {
				reqCopy := req.Clone(req.Context())
				if tc.body != nil {
					reqCopy.Body = io.NopCloser(strings.NewReader(bodyStr))
				}
				ok, errs := v.ValidateHttpRequestSync(reqCopy)
				if tc.wantReqInvalid {
					if ok {
						t.Errorf("expected malformed request to fail OpenAPI validation, but it passed")
					}
				} else if !ok {
					for _, e := range errs {
						t.Errorf("request failed OpenAPI validation: %s", e.Error())
					}
				}
			}

			rec := httptest.NewRecorder()
			f.svc.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantErrorCode != "" {
				var er errorResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &er); err != nil {
					t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
				}
				if er.Error != tc.wantErrorCode {
					t.Fatalf("error code = %q, want %q", er.Error, tc.wantErrorCode)
				}
			}

			resp := rec.Result()
			defer resp.Body.Close()
			if ok, errs := v.ValidateHttpResponse(req, resp); !ok {
				for _, e := range errs {
					t.Errorf("response failed OpenAPI validation: %s", e.Error())
				}
			}
			validateResponseHeaders(t, model, tc.opKey, resp)
			coveredOps[tc.opKey] = true
		})
	}

	for _, op := range specRoutes(model) {
		if !coveredOps[op] {
			t.Errorf("openapi.yaml operation %q was not exercised by TestOpenAPIContract", op)
		}
	}
}

func TestOpenAPIDriftDetection(t *testing.T) {
	_, _, v := loadOpenAPI(t)

	// Extra undeclared JSON field in a response must be rejected.
	req := httptest.NewRequest(http.MethodGet, "/v1/boards", nil)
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.Header().Set("Cache-Control", "no-store")
	rec.WriteHeader(http.StatusOK)
	_, _ = rec.WriteString(`{"boards":[],"extra":1}`)
	if ok, _ := v.ValidateHttpResponse(req, rec.Result()); ok {
		t.Error("expected response with undeclared field to fail validation")
	}

	// Undocumented error code must be rejected.
	scoreReq := httptest.NewRequest(http.MethodPost, "/v1/boards/high-score/scores", strings.NewReader(`{"score":1}`))
	scoreReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.Header().Set("Cache-Control", "no-store")
	rec.WriteHeader(http.StatusForbidden)
	_, _ = rec.WriteString(`{"error":"brand_new_code"}`)
	if ok, _ := v.ValidateHttpResponse(scoreReq, rec.Result()); ok {
		t.Error("expected response with undocumented error code to fail validation")
	}

	// Undocumented response header must be rejected in strict mode.
	rec = httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.Header().Set("Cache-Control", "no-store")
	rec.Header().Set("X-Undocumented", "1")
	rec.WriteHeader(http.StatusOK)
	_, _ = rec.WriteString(`{"boards":[]}`)
	if ok, _ := v.ValidateHttpResponse(req, rec.Result()); ok {
		t.Error("expected response with undocumented header to fail validation")
	}
}
