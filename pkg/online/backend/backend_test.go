package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRPCAuthAndDispatch(t *testing.T) {
	b := New(func(r *http.Request) (Caller, error) {
		if r.Header.Get("Authorization") != "Bearer ok" {
			return Caller{}, errors.New("bad")
		}
		return Caller{AccountID: "a"}, nil
	})
	if err := b.RPC("hello", func(_ context.Context, c Caller, raw json.RawMessage) (any, error) {
		return map[string]any{"account": c.AccountID, "body": string(raw)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/rpc/hello", strings.NewReader(`{"x":1}`))
	req.Header.Set("Authorization", "Bearer ok")
	rec := httptest.NewRecorder()
	b.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
