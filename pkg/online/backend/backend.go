// Package backend composes online service handlers and compiled game RPCs in
// one embeddable HTTP handler.
package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Caller identifies the authenticated account invoking an RPC.
type Caller struct{ AccountID string }

// RPCFunc handles one game-defined JSON RPC.
type RPCFunc func(context.Context, Caller, json.RawMessage) (any, error)

// Backend is an embeddable service mux. Hooks and service-specific auth remain
// owned by the mounted handlers, while RPC authorization is supplied by Auth.
type Backend struct {
	mux  *http.ServeMux
	auth func(*http.Request) (Caller, error)
}

// New returns an empty backend. Auth is required for RPC calls and may inspect
// the request's bearer token using the application's verifier.
func New(auth func(*http.Request) (Caller, error)) *Backend {
	return &Backend{mux: http.NewServeMux(), auth: auth}
}

// Mount mounts an existing service handler below prefix, such as /accounts/.
func (b *Backend) Mount(prefix string, h http.Handler) error {
	if b == nil || b.mux == nil || h == nil || !strings.HasPrefix(prefix, "/") {
		return errors.New("backend: invalid mount")
	}
	b.mux.Handle(prefix, http.StripPrefix(strings.TrimSuffix(prefix, "/"), h))
	return nil
}

// RPC registers POST /v1/rpc/name.
func (b *Backend) RPC(name string, fn RPCFunc) error {
	if b == nil || b.mux == nil || fn == nil || name == "" || strings.Contains(name, "/") {
		return errors.New("backend: invalid rpc")
	}
	b.mux.HandleFunc("POST /v1/rpc/"+name, func(w http.ResponseWriter, r *http.Request) {
		if b.auth == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		caller, err := b.auth(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var raw json.RawMessage
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&raw); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		result, err := fn(r.Context(), caller, raw)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
	return nil
}

// ServeHTTP serves mounted services and RPCs.
func (b *Backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if b == nil || b.mux == nil {
		http.NotFound(w, r)
		return
	}
	b.mux.ServeHTTP(w, r)
}
