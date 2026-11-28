package gameserver

import (
	"encoding/json"
	"net/http"
	"runtime"
	"runtime/pprof"
	"strings"
)

// Inspector serves authenticated, read-only runtime inspection on an admin
// listener. It is intentionally a separate handler so a player-facing server
// never exposes profiling endpoints by accident.
type Inspector struct {
	// Authorize decides whether an admin request may proceed. A nil authorizer
	// denies every request.
	Authorize func(*http.Request) bool
}

// RuntimeInfo is a point-in-time process health snapshot.
type RuntimeInfo struct {
	Goroutines int    `json:"goroutines"`
	HeapBytes  uint64 `json:"heapBytes"`
	HeapInUse  uint64 `json:"heapInUse"`
	NumGC      uint32 `json:"numGC"`
}

// NewInspector returns an authenticated admin inspection handler.
func NewInspector(authorize func(*http.Request) bool) *Inspector {
	return &Inspector{Authorize: authorize}
}

func (i *Inspector) allowed(r *http.Request) bool {
	return i != nil && i.Authorize != nil && i.Authorize(r)
}

// ServeHTTP exposes GET /admin/v1/runtime and /admin/v1/pprof/*.
func (i *Inspector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !i.allowed(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/admin/v1/runtime":
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(RuntimeInfo{Goroutines: runtime.NumGoroutine(), HeapBytes: m.HeapAlloc, HeapInUse: m.HeapInuse, NumGC: m.NumGC})
	case strings.HasPrefix(r.URL.Path, "/admin/v1/pprof/"):
		name := strings.TrimPrefix(r.URL.Path, "/admin/v1/pprof/")
		if name == "" || strings.ContainsAny(name, "/\\") {
			http.NotFound(w, r)
			return
		}
		if profile := pprof.Lookup(name); profile != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_ = profile.WriteTo(w, 0)
			return
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}
