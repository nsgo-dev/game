package gameserver

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics is the server state exposed to health and metrics endpoints.
type Metrics struct {
	Peers     int
	Rooms     int
	TickNanos int64
	BytesIn   uint64
	BytesOut  uint64
}

// Health serves a game server's deployment endpoints:
//
//	GET /healthz  200 while the process is alive
//	GET /readyz   200 when Ready reports true, else 503
//	GET /metrics  Prometheus text metrics from Metrics
//
// Ready and Metrics are optional; a nil Ready is always ready and a nil
// Metrics reports zeros. Alongside the Metrics series, /metrics exports a
// per-match tick histogram and overrun counters (ObserveMatchTick, OnOverrun
// and Runner.Health) and whatever AddCollector appends, such as
// GuardCollector and InboxCollector.
type Health struct {
	Ready   func() bool
	Metrics func() Metrics

	tickNanos atomic.Int64

	mu         sync.Mutex
	matches    map[string]*matchMetrics
	collectors []func(io.Writer)
}

// NewHealth returns deployment endpoints.
func NewHealth(ready func() bool, metrics func() Metrics) *Health {
	return &Health{Ready: ready, Metrics: metrics}
}

// ObserveTick records the duration of the last server tick for metrics.
func (h *Health) ObserveTick(d time.Duration) {
	if h == nil {
		return
	}
	h.tickNanos.Store(int64(d))
}

// ServeHTTP handles GET and HEAD requests for /healthz, /readyz, and /metrics, rejecting other methods and paths.
func (h *Health) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case "/healthz":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	case "/readyz":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if h == nil || h.Ready == nil || h.Ready() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready\n"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready\n"))
	case "/metrics":
		var m Metrics
		if h != nil && h.Metrics != nil {
			m = h.Metrics()
		}
		tick := int64(0)
		if h != nil {
			tick = h.tickNanos.Load()
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprintf(w, "# HELP nsgo_server_peers Peers connected to this server.\n")
		fmt.Fprintf(w, "# TYPE nsgo_server_peers gauge\nnsgo_server_peers %d\n", m.Peers)
		fmt.Fprintf(w, "# HELP nsgo_server_rooms Open rooms.\n")
		fmt.Fprintf(w, "# TYPE nsgo_server_rooms gauge\nnsgo_server_rooms %d\n", m.Rooms)
		fmt.Fprintf(w, "# HELP nsgo_server_tick_duration_seconds Last fixed-step duration.\n")
		fmt.Fprintf(w, "# TYPE nsgo_server_tick_duration_seconds gauge\nnsgo_server_tick_duration_seconds %g\n", float64(tick)/1e9)
		fmt.Fprintf(w, "# HELP nsgo_server_bytes_in_total Bytes received.\n")
		fmt.Fprintf(w, "# TYPE nsgo_server_bytes_in_total counter\nnsgo_server_bytes_in_total %d\n", m.BytesIn)
		fmt.Fprintf(w, "# HELP nsgo_server_bytes_out_total Bytes sent.\n")
		fmt.Fprintf(w, "# TYPE nsgo_server_bytes_out_total counter\nnsgo_server_bytes_out_total %d\n", m.BytesOut)
		h.writeMatchMetrics(w)
		h.writeCollectors(w)
	default:
		http.NotFound(w, r)
	}
}
