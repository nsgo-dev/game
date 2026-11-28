package gameserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInspectorRequiresAuthorizationAndServesRuntime(t *testing.T) {
	i := NewInspector(func(r *http.Request) bool { return r.Header.Get("X-Admin") == "yes" })
	unauth := httptest.NewRecorder()
	i.ServeHTTP(unauth, httptest.NewRequest("GET", "/admin/v1/runtime", nil))
	if unauth.Code != 401 {
		t.Fatalf("unauthorized status = %d", unauth.Code)
	}
	auth := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/v1/runtime", nil)
	r.Header.Set("X-Admin", "yes")
	i.ServeHTTP(auth, r)
	if auth.Code != 200 || auth.Body.Len() == 0 {
		t.Fatalf("runtime response = %d %q", auth.Code, auth.Body.String())
	}
}

func TestInspectorPprofRequiresAdminHandler(t *testing.T) {
	i := NewInspector(func(*http.Request) bool { return true })
	rec := httptest.NewRecorder()
	i.ServeHTTP(rec, httptest.NewRequest("GET", "/admin/v1/pprof/goroutine", nil))
	if rec.Code != 200 {
		t.Fatalf("pprof status = %d", rec.Code)
	}
}
