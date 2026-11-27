package cloudsave

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/online/internal/cors"
)

//go:embed openapi.yaml
var openapiSpec []byte

// Headers of the HTTP protocol besides the standard ones.
const (
	// HeaderVersion carries a slot version's number on GET responses.
	HeaderVersion = "Save-Version"
)

// Handler serves the service over HTTP. Every request except GET /v1/openapi.yaml needs "Authorization: Bearer <token>" with a token from the account service; slots are those of the token's subject. Slot names are path segments, escaped as url.PathEscape does. The machine-readable OpenAPI 3.1 description lives in openapi.yaml and is served at GET /v1/openapi.yaml.
//
//	GET    /v1/openapi.yaml                -> the OpenAPI 3.1 description (unauthenticated)
//	GET    /v1/saves                       -> {"slots": [version]}
//	GET    /v1/saves/{slot}                -> the payload, with ETag and Save-Version
//	PUT    /v1/saves/{slot}    payload     -> version; If-Match: "<hash>" or If-None-Match: * make it conditional
//	DELETE /v1/saves/{slot}                -> 204; If-Match: "<hash>" or If-None-Match: * make it conditional
//	GET    /v1/saves/{slot}/versions       -> {"versions": [version]}, newest first
//	GET    /v1/saves/{slot}/versions/{n}   -> the payload of version n, with ETag and Save-Version
//	DELETE /v1/saves/{slot}/versions       -> 204, the slot and all its versions purged
//
// A version is {"slot", "version", "deleted", "hash", "size", "created"}, created in RFC 3339 with milliseconds; deleted and hash appear only when set. An ETag is the payload's Hash in double quotes. Payloads are application/octet-stream.
//
// Errors are {"error": code} with the status the code implies: bad_request 400 (an invalid slot name, version number or precondition header), invalid_token 401, not_found 404, too_many_slots 409, deleted 410 (a version that is a deletion has no payload), changed 412, slot_too_large 413 (with "limit", Config.MaxSlotBytes), unavailable 503 when the request was cancelled or timed out, and internal 500. Every response carries Cache-Control: no-store.
//
// Browsers may call the handler from Config.AllowedOrigins: it answers their CORS preflight requests and exposes ETag and Save-Version to them.
//
// The handler does not limit request rates; put it behind a rate limiter, and behind TLS.
func (s *Service) Handler() http.Handler {
	mux, _ := s.newMux()
	return cors.Wrap(mux, cors.Policy{
		Origins:       s.cfg.AllowedOrigins,
		Methods:       []string{http.MethodGet, http.MethodPut, http.MethodDelete},
		Headers:       []string{"Authorization", "Content-Type", "If-Match", "If-None-Match"},
		ExposeHeaders: []string{"ETag", HeaderVersion},
	})
}

func (s *Service) newMux() (*http.ServeMux, []string) {
	mux := http.NewServeMux()
	var routes []string
	handle := func(pattern string, h http.HandlerFunc) {
		routes = append(routes, pattern)
		mux.HandleFunc(pattern, h)
	}
	handle("GET /v1/openapi.yaml", writeOpenAPI)
	handle("GET /v1/saves", s.authed(func(w http.ResponseWriter, r *http.Request, account string) {
		heads, err := s.List(r.Context(), account)
		if err != nil {
			writeError(w, err, s.cfg.MaxSlotBytes)
			return
		}
		resp := slotsResponse{Slots: make([]versionResponse, 0, len(heads))}
		for _, h := range heads {
			resp.Slots = append(resp.Slots, versionJSON(h))
		}
		writeJSON(w, http.StatusOK, resp)
	}))
	handle("GET /v1/saves/{slot}", s.authed(func(w http.ResponseWriter, r *http.Request, account string) {
		v, err := s.Load(r.Context(), account, r.PathValue("slot"))
		if err != nil {
			writeError(w, err, s.cfg.MaxSlotBytes)
			return
		}
		writePayload(w, v)
	}))
	handle("PUT /v1/saves/{slot}", s.authed(func(w http.ResponseWriter, r *http.Request, account string) {
		m, ok := precondition(r)
		if !ok {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "bad_request"})
			return
		}
		if r.ContentLength > int64(s.cfg.MaxSlotBytes) {
			writeError(w, ErrSlotTooLarge, s.cfg.MaxSlotBytes)
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(s.cfg.MaxSlotBytes)))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, ErrSlotTooLarge, s.cfg.MaxSlotBytes)
			} else {
				writeJSON(w, http.StatusBadRequest, errorResponse{Error: "bad_request"})
			}
			return
		}
		v, err := s.Save(r.Context(), account, r.PathValue("slot"), data, m)
		if err != nil {
			writeError(w, err, s.cfg.MaxSlotBytes)
			return
		}
		w.Header().Set("ETag", strconv.Quote(v.Hash))
		writeJSON(w, http.StatusOK, versionJSON(v))
	}))
	handle("DELETE /v1/saves/{slot}", s.authed(func(w http.ResponseWriter, r *http.Request, account string) {
		m, ok := precondition(r)
		if !ok {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "bad_request"})
			return
		}
		writeDone(w, s.Delete(r.Context(), account, r.PathValue("slot"), m), s.cfg.MaxSlotBytes)
	}))
	handle("GET /v1/saves/{slot}/versions", s.authed(func(w http.ResponseWriter, r *http.Request, account string) {
		vs, err := s.Versions(r.Context(), account, r.PathValue("slot"))
		if err != nil {
			writeError(w, err, s.cfg.MaxSlotBytes)
			return
		}
		resp := versionsResponse{Versions: make([]versionResponse, 0, len(vs))}
		for _, v := range vs {
			resp.Versions = append(resp.Versions, versionJSON(v))
		}
		writeJSON(w, http.StatusOK, resp)
	}))
	handle("GET /v1/saves/{slot}/versions/{n}", s.authed(func(w http.ResponseWriter, r *http.Request, account string) {
		n, err := strconv.ParseInt(r.PathValue("n"), 10, 64)
		if err != nil || n < 1 {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "bad_request"})
			return
		}
		v, err := s.Version(r.Context(), account, r.PathValue("slot"), n)
		if err != nil {
			writeError(w, err, s.cfg.MaxSlotBytes)
			return
		}
		if v.Deleted {
			w.Header().Set(HeaderVersion, strconv.FormatInt(v.Number, 10))
			writeJSON(w, http.StatusGone, errorResponse{Error: "deleted"})
			return
		}
		writePayload(w, v)
	}))
	handle("DELETE /v1/saves/{slot}/versions", s.authed(func(w http.ResponseWriter, r *http.Request, account string) {
		writeDone(w, s.Purge(r.Context(), account, r.PathValue("slot")), s.cfg.MaxSlotBytes)
	}))
	return mux, routes
}

func writeOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapiSpec)
}

type versionResponse struct {
	Slot    string `json:"slot"`
	Version int64  `json:"version"`
	Deleted bool   `json:"deleted,omitempty"`
	Hash    string `json:"hash,omitempty"`
	Size    int64  `json:"size"`
	Created string `json:"created"`
}

type slotsResponse struct {
	Slots []versionResponse `json:"slots"`
}

type versionsResponse struct {
	Versions []versionResponse `json:"versions"`
}

type errorResponse struct {
	Error string `json:"error"`
	Limit int    `json:"limit,omitempty"`
}

// createdLayout is RFC 3339 with exactly three fractional digits, the precision versions keep.
const createdLayout = "2006-01-02T15:04:05.000Z07:00"

func versionJSON(v Version) versionResponse {
	return versionResponse{Slot: v.Slot, Version: v.Number, Deleted: v.Deleted, Hash: v.Hash, Size: v.Size, Created: v.Created.UTC().Format(createdLayout)}
}

// precondition reads If-Match or If-None-Match. If-Match takes exactly one quoted hash; If-None-Match takes only "*". A request with neither is unconditional; one with both, or with any other value, is malformed.
func precondition(r *http.Request) (Match, bool) {
	im, inm := r.Header.Values("If-Match"), r.Header.Values("If-None-Match")
	switch {
	case len(im) == 0 && len(inm) == 0:
		return MatchAny, true
	case len(im) == 1 && len(inm) == 0:
		tag := strings.TrimSpace(im[0])
		hash, err := strconv.Unquote(tag)
		if err != nil || !strings.HasPrefix(tag, `"`) || !validHash(hash) {
			return Match{}, false
		}
		return MatchHash(hash), true
	case len(im) == 0 && len(inm) == 1 && strings.TrimSpace(inm[0]) == "*":
		return MatchHash(""), true
	}
	return Match{}, false
}

// validHash reports whether h has the form Hash returns.
func validHash(h string) bool {
	hex, ok := strings.CutPrefix(h, "sha256:")
	if !ok || len(hex) != 64 {
		return false
	}
	for i := 0; i < len(hex); i++ {
		if c := hex[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// authed verifies the request's bearer token and passes its subject to h, or writes a 401.
func (s *Service) authed(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme, tok, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			if claims, err := s.tokens.Verify(strings.TrimSpace(tok)); err == nil && validateAccount(claims.Subject) == nil {
				h(w, r, claims.Subject)
				return
			}
		}
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "invalid_token"})
	}
}

func writePayload(w http.ResponseWriter, v Version) {
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Length", strconv.Itoa(len(v.Data)))
	h.Set("ETag", strconv.Quote(v.Hash))
	h.Set(HeaderVersion, strconv.FormatInt(v.Number, 10))
	h.Set("Last-Modified", v.Created.UTC().Format(http.TimeFormat))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(v.Data)
}

func writeDone(w http.ResponseWriter, err error, limit int) {
	if err != nil {
		writeError(w, err, limit)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

type errorMapping struct {
	err    error
	status int
	code   string
}

var errorMappings = []errorMapping{
	{ErrInvalidSlot, http.StatusBadRequest, "bad_request"},
	{ErrInvalidAccount, http.StatusUnauthorized, "invalid_token"},
	{ErrNotFound, http.StatusNotFound, "not_found"},
	{ErrTooManySlots, http.StatusConflict, "too_many_slots"},
	{ErrChanged, http.StatusPreconditionFailed, "changed"},
	{ErrSlotTooLarge, http.StatusRequestEntityTooLarge, "slot_too_large"},
	{context.DeadlineExceeded, http.StatusServiceUnavailable, "unavailable"},
	{context.Canceled, http.StatusServiceUnavailable, "unavailable"},
}

// writeError writes the response for err. A cancelled or timed-out request becomes 503 unavailable; any other error outside the service's contract becomes a bare 500, and the service has logged it.
func writeError(w http.ResponseWriter, err error, limit int) {
	for _, c := range errorMappings {
		if errors.Is(err, c.err) {
			resp := errorResponse{Error: c.code}
			if c.err == ErrSlotTooLarge {
				resp.Limit = limit
			}
			writeJSON(w, c.status, resp)
			return
		}
	}
	writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
