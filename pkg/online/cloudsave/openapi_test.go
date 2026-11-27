package cloudsave

import (
	"bytes"
	"context"
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

func newContractService(t *testing.T, store Store, mutate func(*Config)) (*Service, *acctoken.Signer) {
	t.Helper()
	key, err := acctoken.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := acctoken.NewSigner(key, "https://accounts.example.com", "test-game", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := acctoken.NewVerifier(signer.KeySet())
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.AllowedOrigins = []string{"https://play.example.com"}
	if mutate != nil {
		mutate(&cfg)
	}
	if store == nil {
		store = NewMemoryStore()
	}
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	svc, err := New(cfg, Options{Store: store, Tokens: verifier, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return svc, signer
}

func issueToken(t *testing.T, signer *acctoken.Signer, account string) string {
	t.Helper()
	tok, _, err := signer.Issue(account)
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
	svc, _ := newContractService(t, nil, nil)
	_, handlerRoutes := svc.newMux()
	slices.Sort(handlerRoutes)
	wantRoutes := specRoutes(model)
	if !slices.Equal(handlerRoutes, wantRoutes) {
		t.Fatalf("handler routes != openapi.yaml routes\nhandler: %v\nspec:    %v", handlerRoutes, wantRoutes)
	}

	// Ensure http.go registers routes only through the recording handle helper.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "http.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	muxHandleCalls := 0
	ast.Inspect(f, func(n ast.Node) bool {
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

	// Every error in writeError's mapping table plus the 500 fallback must be documented under its status.
	handlerCodes := map[int]map[string]bool{
		http.StatusInternalServerError: {"internal": true},
	}
	for _, m := range errorMappings {
		if handlerCodes[m.status] == nil {
			handlerCodes[m.status] = map[string]bool{}
		}
		handlerCodes[m.status][m.code] = true
	}

	// Also extract every errorResponse literal written anywhere in http.go so direct writes cannot drift.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "http.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	literalCodes := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
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
	svc, _ := newContractService(t, nil, nil)
	h := svc.Handler()

	// Unauthenticated fetch succeeds.
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

	// Allowed CORS origin can fetch it and pass preflight.
	corsReq := httptest.NewRequest(http.MethodGet, "/v1/openapi.yaml", nil)
	corsReq.Header.Set("Origin", "https://play.example.com")
	corsRec := httptest.NewRecorder()
	h.ServeHTTP(corsRec, corsReq)
	if corsRec.Code != http.StatusOK || corsRec.Header().Get("Access-Control-Allow-Origin") != "https://play.example.com" {
		t.Fatalf("CORS GET /v1/openapi.yaml = %d %v", corsRec.Code, corsRec.Header())
	}

	preReq := httptest.NewRequest(http.MethodOptions, "/v1/openapi.yaml", nil)
	preReq.Header.Set("Origin", "https://play.example.com")
	preReq.Header.Set("Access-Control-Request-Method", "GET")
	preRec := httptest.NewRecorder()
	h.ServeHTTP(preRec, preReq)
	if preRec.Code != http.StatusNoContent || preRec.Header().Get("Access-Control-Allow-Origin") != "https://play.example.com" {
		t.Fatalf("CORS OPTIONS /v1/openapi.yaml = %d %v", preRec.Code, preRec.Header())
	}

	// Disallowed origin gets no CORS allow header.
	evilReq := httptest.NewRequest(http.MethodGet, "/v1/openapi.yaml", nil)
	evilReq.Header.Set("Origin", "https://evil.example.com")
	evilRec := httptest.NewRecorder()
	h.ServeHTTP(evilRec, evilReq)
	if evilRec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed origin got CORS header: %v", evilRec.Header())
	}
}

type failingStore struct {
	err error
}

func (f failingStore) Head(context.Context, string, string) (Version, error) {
	return Version{}, f.err
}
func (f failingStore) Version(context.Context, string, string, int64) (Version, error) {
	return Version{}, f.err
}
func (f failingStore) Versions(context.Context, string, string) ([]Version, error) {
	return nil, f.err
}
func (f failingStore) Slots(context.Context, string) ([]Version, error) {
	return nil, f.err
}
func (f failingStore) Append(context.Context, string, string, Write) (Version, error) {
	return Version{}, f.err
}
func (f failingStore) Purge(context.Context, string, string) error {
	return f.err
}

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
	// Every response header other than Content-Type and Content-Length must be documented.
	for k := range resp.Header {
		ck := http.CanonicalHeaderKey(k)
		if ck == "Content-Type" || ck == "Content-Length" {
			continue
		}
		if _, ok := declared[ck]; !ok {
			t.Errorf("%s %d: undocumented response header %q = %q", opKey, resp.StatusCode, ck, resp.Header.Get(ck))
		}
	}
	// Every required header in the spec must be present and conform to its schema.
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
		if slices.Contains(schema.Type, "integer") {
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				t.Errorf("%s %d: header %q = %q is not an integer", opKey, resp.StatusCode, ck, val)
			} else if schema.Minimum != nil && float64(n) < *schema.Minimum {
				t.Errorf("%s %d: header %q = %d below minimum %v", opKey, resp.StatusCode, ck, n, *schema.Minimum)
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
		body           []byte
		contentType    string
		headers        map[string]string
		noAuth         bool
		badToken       bool
		cancelCtx      bool
		storeErr       error
		mutateCfg      func(*Config)
		setup          func(t *testing.T, svc *Service, acct string)
		skipReqCheck   bool // true when request omits auth or uses a Go-only invalid path segment
		wantReqInvalid bool // true when the OpenAPI spec itself must reject the malformed request
		wantStatus     int
		wantErrorCode  string
		validateCustom func(t *testing.T, rec *httptest.ResponseRecorder)
	}

	hashV1 := Hash([]byte("v1"))
	hashOther := Hash([]byte("other"))

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

		// GET /v1/saves
		{
			name:       "list/empty",
			opKey:      "GET /v1/saves",
			method:     http.MethodGet,
			target:     "/v1/saves",
			wantStatus: http.StatusOK,
		},
		{
			name:   "list/populated",
			opKey:  "GET /v1/saves",
			method: http.MethodGet,
			target: "/v1/saves",
			setup: func(t *testing.T, svc *Service, acct string) {
				if _, err := svc.Save(context.Background(), acct, "main", []byte("v1"), MatchAny); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:          "list/unauthorized",
			opKey:         "GET /v1/saves",
			method:        http.MethodGet,
			target:        "/v1/saves",
			noAuth:        true,
			skipReqCheck:  true,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "invalid_token",
		},
		{
			name:          "list/unavailable",
			opKey:         "GET /v1/saves",
			method:        http.MethodGet,
			target:        "/v1/saves",
			cancelCtx:     true,
			storeErr:      context.Canceled,
			wantStatus:    http.StatusServiceUnavailable,
			wantErrorCode: "unavailable",
		},
		{
			name:          "list/internal",
			opKey:         "GET /v1/saves",
			method:        http.MethodGet,
			target:        "/v1/saves",
			storeErr:      errors.New("db down"),
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "internal",
		},

		// PUT /v1/saves/{slot}
		{
			name:        "save/create-if-none-match",
			opKey:       "PUT /v1/saves/{slot}",
			method:      http.MethodPut,
			target:      "/v1/saves/main",
			body:        []byte("v1"),
			contentType: "application/octet-stream",
			headers:     map[string]string{"If-None-Match": "*"},
			wantStatus:  http.StatusOK,
		},
		{
			name:        "save/update-if-match",
			opKey:       "PUT /v1/saves/{slot}",
			method:      http.MethodPut,
			target:      "/v1/saves/main",
			body:        []byte("v2"),
			contentType: "application/octet-stream",
			headers:     map[string]string{"If-Match": strconv.Quote(hashV1)},
			setup: func(t *testing.T, svc *Service, acct string) {
				if _, err := svc.Save(context.Background(), acct, "main", []byte("v1"), MatchAny); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:           "save/bad-precondition",
			opKey:          "PUT /v1/saves/{slot}",
			method:         http.MethodPut,
			target:         "/v1/saves/main",
			body:           []byte("v1"),
			contentType:    "application/octet-stream",
			headers:        map[string]string{"If-Match": "not-a-hash"},
			wantReqInvalid: true,
			wantStatus:     http.StatusBadRequest,
			wantErrorCode:  "bad_request",
		},
		{
			name:          "save/bad-slot",
			opKey:         "PUT /v1/saves/{slot}",
			method:        http.MethodPut,
			target:        "/v1/saves/%01bad",
			body:          []byte("v1"),
			contentType:   "application/octet-stream",
			skipReqCheck:  true,
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "bad_request",
		},
		{
			name:          "save/unauthorized",
			opKey:         "PUT /v1/saves/{slot}",
			method:        http.MethodPut,
			target:        "/v1/saves/main",
			body:          []byte("v1"),
			contentType:   "application/octet-stream",
			badToken:      true,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "invalid_token",
		},
		{
			name:        "save/too-many-slots",
			opKey:       "PUT /v1/saves/{slot}",
			method:      http.MethodPut,
			target:      "/v1/saves/two",
			body:        []byte("v2"),
			contentType: "application/octet-stream",
			mutateCfg:   func(c *Config) { c.MaxSlots = 1 },
			setup: func(t *testing.T, svc *Service, acct string) {
				if _, err := svc.Save(context.Background(), acct, "one", []byte("v1"), MatchAny); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus:    http.StatusConflict,
			wantErrorCode: "too_many_slots",
		},
		{
			name:        "save/changed",
			opKey:       "PUT /v1/saves/{slot}",
			method:      http.MethodPut,
			target:      "/v1/saves/main",
			body:        []byte("v2"),
			contentType: "application/octet-stream",
			headers:     map[string]string{"If-Match": strconv.Quote(hashOther)},
			setup: func(t *testing.T, svc *Service, acct string) {
				if _, err := svc.Save(context.Background(), acct, "main", []byte("v1"), MatchAny); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus:    http.StatusPreconditionFailed,
			wantErrorCode: "changed",
		},
		{
			name:          "save/too-large",
			opKey:         "PUT /v1/saves/{slot}",
			method:        http.MethodPut,
			target:        "/v1/saves/main",
			body:          bytes.Repeat([]byte("a"), 32),
			contentType:   "application/octet-stream",
			mutateCfg:     func(c *Config) { c.MaxSlotBytes = 16 },
			wantStatus:    http.StatusRequestEntityTooLarge,
			wantErrorCode: "slot_too_large",
		},
		{
			name:          "save/unavailable",
			opKey:         "PUT /v1/saves/{slot}",
			method:        http.MethodPut,
			target:        "/v1/saves/main",
			body:          []byte("v1"),
			contentType:   "application/octet-stream",
			storeErr:      context.DeadlineExceeded,
			wantStatus:    http.StatusServiceUnavailable,
			wantErrorCode: "unavailable",
		},
		{
			name:          "save/internal",
			opKey:         "PUT /v1/saves/{slot}",
			method:        http.MethodPut,
			target:        "/v1/saves/main",
			body:          []byte("v1"),
			contentType:   "application/octet-stream",
			storeErr:      errors.New("disk full"),
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "internal",
		},

		// GET /v1/saves/{slot}
		{
			name:   "load/ok",
			opKey:  "GET /v1/saves/{slot}",
			method: http.MethodGet,
			target: "/v1/saves/main",
			setup: func(t *testing.T, svc *Service, acct string) {
				if _, err := svc.Save(context.Background(), acct, "main", []byte("v1"), MatchAny); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:          "load/bad-slot",
			opKey:         "GET /v1/saves/{slot}",
			method:        http.MethodGet,
			target:        "/v1/saves/%01bad",
			skipReqCheck:  true,
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "bad_request",
		},
		{
			name:          "load/unauthorized",
			opKey:         "GET /v1/saves/{slot}",
			method:        http.MethodGet,
			target:        "/v1/saves/main",
			noAuth:        true,
			skipReqCheck:  true,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "invalid_token",
		},
		{
			name:          "load/not-found",
			opKey:         "GET /v1/saves/{slot}",
			method:        http.MethodGet,
			target:        "/v1/saves/missing",
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "not_found",
		},
		{
			name:          "load/unavailable",
			opKey:         "GET /v1/saves/{slot}",
			method:        http.MethodGet,
			target:        "/v1/saves/main",
			storeErr:      context.Canceled,
			wantStatus:    http.StatusServiceUnavailable,
			wantErrorCode: "unavailable",
		},
		{
			name:          "load/internal",
			opKey:         "GET /v1/saves/{slot}",
			method:        http.MethodGet,
			target:        "/v1/saves/main",
			storeErr:      errors.New("boom"),
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "internal",
		},

		// DELETE /v1/saves/{slot}
		{
			name:    "delete/ok",
			opKey:   "DELETE /v1/saves/{slot}",
			method:  http.MethodDelete,
			target:  "/v1/saves/main",
			headers: map[string]string{"If-Match": strconv.Quote(hashV1)},
			setup: func(t *testing.T, svc *Service, acct string) {
				if _, err := svc.Save(context.Background(), acct, "main", []byte("v1"), MatchAny); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name:           "delete/bad-precondition",
			opKey:          "DELETE /v1/saves/{slot}",
			method:         http.MethodDelete,
			target:         "/v1/saves/main",
			headers:        map[string]string{"If-None-Match": "nope"},
			wantReqInvalid: true,
			wantStatus:     http.StatusBadRequest,
			wantErrorCode:  "bad_request",
		},
		{
			name:          "delete/unauthorized",
			opKey:         "DELETE /v1/saves/{slot}",
			method:        http.MethodDelete,
			target:        "/v1/saves/main",
			noAuth:        true,
			skipReqCheck:  true,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "invalid_token",
		},
		{
			name:    "delete/changed",
			opKey:   "DELETE /v1/saves/{slot}",
			method:  http.MethodDelete,
			target:  "/v1/saves/main",
			headers: map[string]string{"If-Match": strconv.Quote(hashOther)},
			setup: func(t *testing.T, svc *Service, acct string) {
				if _, err := svc.Save(context.Background(), acct, "main", []byte("v1"), MatchAny); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus:    http.StatusPreconditionFailed,
			wantErrorCode: "changed",
		},
		{
			name:          "delete/unavailable",
			opKey:         "DELETE /v1/saves/{slot}",
			method:        http.MethodDelete,
			target:        "/v1/saves/main",
			storeErr:      context.Canceled,
			wantStatus:    http.StatusServiceUnavailable,
			wantErrorCode: "unavailable",
		},
		{
			name:          "delete/internal",
			opKey:         "DELETE /v1/saves/{slot}",
			method:        http.MethodDelete,
			target:        "/v1/saves/main",
			storeErr:      errors.New("boom"),
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "internal",
		},

		// GET /v1/saves/{slot}/versions
		{
			name:   "versions/ok",
			opKey:  "GET /v1/saves/{slot}/versions",
			method: http.MethodGet,
			target: "/v1/saves/main/versions",
			setup: func(t *testing.T, svc *Service, acct string) {
				if _, err := svc.Save(context.Background(), acct, "main", []byte("v1"), MatchAny); err != nil {
					t.Fatal(err)
				}
				if err := svc.Delete(context.Background(), acct, "main", MatchAny); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:          "versions/bad-slot",
			opKey:         "GET /v1/saves/{slot}/versions",
			method:        http.MethodGet,
			target:        "/v1/saves/%01bad/versions",
			skipReqCheck:  true,
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "bad_request",
		},
		{
			name:          "versions/unauthorized",
			opKey:         "GET /v1/saves/{slot}/versions",
			method:        http.MethodGet,
			target:        "/v1/saves/main/versions",
			noAuth:        true,
			skipReqCheck:  true,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "invalid_token",
		},
		{
			name:          "versions/not-found",
			opKey:         "GET /v1/saves/{slot}/versions",
			method:        http.MethodGet,
			target:        "/v1/saves/missing/versions",
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "not_found",
		},
		{
			name:          "versions/unavailable",
			opKey:         "GET /v1/saves/{slot}/versions",
			method:        http.MethodGet,
			target:        "/v1/saves/main/versions",
			storeErr:      context.DeadlineExceeded,
			wantStatus:    http.StatusServiceUnavailable,
			wantErrorCode: "unavailable",
		},
		{
			name:          "versions/internal",
			opKey:         "GET /v1/saves/{slot}/versions",
			method:        http.MethodGet,
			target:        "/v1/saves/main/versions",
			storeErr:      errors.New("boom"),
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "internal",
		},

		// GET /v1/saves/{slot}/versions/{n}
		{
			name:   "version/ok",
			opKey:  "GET /v1/saves/{slot}/versions/{n}",
			method: http.MethodGet,
			target: "/v1/saves/main/versions/1",
			setup: func(t *testing.T, svc *Service, acct string) {
				if _, err := svc.Save(context.Background(), acct, "main", []byte("v1"), MatchAny); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:           "version/bad-n",
			opKey:          "GET /v1/saves/{slot}/versions/{n}",
			method:         http.MethodGet,
			target:         "/v1/saves/main/versions/0",
			wantReqInvalid: true,
			wantStatus:     http.StatusBadRequest,
			wantErrorCode:  "bad_request",
		},
		{
			name:          "version/unauthorized",
			opKey:         "GET /v1/saves/{slot}/versions/{n}",
			method:        http.MethodGet,
			target:        "/v1/saves/main/versions/1",
			noAuth:        true,
			skipReqCheck:  true,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "invalid_token",
		},
		{
			name:          "version/not-found",
			opKey:         "GET /v1/saves/{slot}/versions/{n}",
			method:        http.MethodGet,
			target:        "/v1/saves/main/versions/9",
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "not_found",
		},
		{
			name:   "version/deleted",
			opKey:  "GET /v1/saves/{slot}/versions/{n}",
			method: http.MethodGet,
			target: "/v1/saves/main/versions/2",
			setup: func(t *testing.T, svc *Service, acct string) {
				if _, err := svc.Save(context.Background(), acct, "main", []byte("v1"), MatchAny); err != nil {
					t.Fatal(err)
				}
				if err := svc.Delete(context.Background(), acct, "main", MatchAny); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus:    http.StatusGone,
			wantErrorCode: "deleted",
		},
		{
			name:          "version/unavailable",
			opKey:         "GET /v1/saves/{slot}/versions/{n}",
			method:        http.MethodGet,
			target:        "/v1/saves/main/versions/1",
			storeErr:      context.Canceled,
			wantStatus:    http.StatusServiceUnavailable,
			wantErrorCode: "unavailable",
		},
		{
			name:          "version/internal",
			opKey:         "GET /v1/saves/{slot}/versions/{n}",
			method:        http.MethodGet,
			target:        "/v1/saves/main/versions/1",
			storeErr:      errors.New("boom"),
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "internal",
		},

		// DELETE /v1/saves/{slot}/versions
		{
			name:   "purge/ok",
			opKey:  "DELETE /v1/saves/{slot}/versions",
			method: http.MethodDelete,
			target: "/v1/saves/main/versions",
			setup: func(t *testing.T, svc *Service, acct string) {
				if _, err := svc.Save(context.Background(), acct, "main", []byte("v1"), MatchAny); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name:          "purge/bad-slot",
			opKey:         "DELETE /v1/saves/{slot}/versions",
			method:        http.MethodDelete,
			target:        "/v1/saves/%01bad/versions",
			skipReqCheck:  true,
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "bad_request",
		},
		{
			name:          "purge/unauthorized",
			opKey:         "DELETE /v1/saves/{slot}/versions",
			method:        http.MethodDelete,
			target:        "/v1/saves/main/versions",
			noAuth:        true,
			skipReqCheck:  true,
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "invalid_token",
		},
		{
			name:          "purge/unavailable",
			opKey:         "DELETE /v1/saves/{slot}/versions",
			method:        http.MethodDelete,
			target:        "/v1/saves/main/versions",
			storeErr:      context.DeadlineExceeded,
			wantStatus:    http.StatusServiceUnavailable,
			wantErrorCode: "unavailable",
		},
		{
			name:          "purge/internal",
			opKey:         "DELETE /v1/saves/{slot}/versions",
			method:        http.MethodDelete,
			target:        "/v1/saves/main/versions",
			storeErr:      errors.New("boom"),
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "internal",
		},
	}

	coveredOps := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var store Store
			if tc.storeErr != nil {
				store = failingStore{err: tc.storeErr}
			}
			svc, signer := newContractService(t, store, tc.mutateCfg)
			if tc.setup != nil {
				tc.setup(t, svc, "acct-1")
			}

			var bodyReader io.Reader
			if tc.body != nil {
				bodyReader = bytes.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.target, bodyReader)
			if tc.cancelCtx {
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
			}
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			for k, val := range tc.headers {
				req.Header.Set(k, val)
			}
			if !tc.noAuth {
				tok := "not-a-valid-token"
				if !tc.badToken {
					tok = issueToken(t, signer, "acct-1")
				}
				req.Header.Set("Authorization", "Bearer "+tok)
			}

			if !tc.skipReqCheck {
				reqCopy := req.Clone(req.Context())
				if tc.body != nil {
					reqCopy.Body = io.NopCloser(bytes.NewReader(tc.body))
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
			svc.Handler().ServeHTTP(rec, req)
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
			if tc.validateCustom != nil {
				tc.validateCustom(t, rec)
			}
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
	req := httptest.NewRequest(http.MethodGet, "/v1/saves", nil)
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.Header().Set("Cache-Control", "no-store")
	rec.WriteHeader(http.StatusOK)
	_, _ = rec.WriteString(`{"slots":[],"extra":1}`)
	if ok, _ := v.ValidateHttpResponse(req, rec.Result()); ok {
		t.Error("expected response with undeclared field to fail validation")
	}

	// Undocumented error code must be rejected.
	rec = httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.Header().Set("Cache-Control", "no-store")
	rec.WriteHeader(http.StatusNotFound)
	_, _ = rec.WriteString(`{"error":"brand_new_code"}`)
	loadReq := httptest.NewRequest(http.MethodGet, "/v1/saves/main", nil)
	if ok, _ := v.ValidateHttpResponse(loadReq, rec.Result()); ok {
		t.Error("expected response with undocumented error code to fail validation")
	}

	// Undocumented response header must be rejected in strict mode.
	rec = httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.Header().Set("Cache-Control", "no-store")
	rec.Header().Set("X-Undocumented", "1")
	rec.WriteHeader(http.StatusOK)
	_, _ = rec.WriteString(`{"slots":[]}`)
	if ok, _ := v.ValidateHttpResponse(req, rec.Result()); ok {
		t.Error("expected response with undocumented header to fail validation")
	}
}
