package gameserver_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/gameserver"
)

func TestHealthEndpoints(t *testing.T) {
	ready := false
	health := gameserver.NewHealth(func() bool { return ready }, func() gameserver.Metrics {
		return gameserver.Metrics{Peers: 3, Rooms: 1, BytesIn: 10, BytesOut: 20}
	})
	health.ObserveTick(5 * time.Millisecond)
	for _, tc := range []struct {
		path, method string
		want         int
	}{
		{"/healthz", http.MethodGet, http.StatusOK},
		{"/readyz", http.MethodGet, http.StatusServiceUnavailable},
		{"/metrics", http.MethodGet, http.StatusOK},
		{"/missing", http.MethodGet, http.StatusNotFound},
		{"/healthz", http.MethodPost, http.StatusMethodNotAllowed},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		health.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
		if tc.path == "/metrics" && !strings.Contains(rec.Body.String(), "nsgo_server_peers 3") {
			t.Fatalf("metrics = %s", rec.Body.String())
		}
	}
	ready = true
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	health.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz = %d, want 200", rec.Code)
	}
}
