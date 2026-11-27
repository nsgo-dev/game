package selfhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxRequestBytes bounds a request body; a Friends request with MaxFriends of the longest account IDs fits.
const maxRequestBytes = 64 << 10

// Query defaults the handler uses when a request leaves them out.
const (
	// DefaultLimit is the number of entries GET top returns without a limit.
	DefaultLimit = 25
	// DefaultRadius is the radius GET around uses without one.
	DefaultRadius = 5
)

// Handler serves the service over HTTP. Every request carries the player's account token as "Authorization: Bearer <token>", verified offline with Options.Tokens, whose subject is the account the request is for. Requests and responses are JSON; every response carries Cache-Control: no-store.
//
//	GET  /v1/boards                                              -> {"boards": [board]}
//	POST /v1/boards/{board}/scores   {"score", "proof"}          -> {"period", "entry", "total", "improved"}
//	GET  /v1/boards/{board}/top?period=&offset=&limit=           -> page
//	GET  /v1/boards/{board}/around?period=&radius=               -> page around the token's account
//	POST /v1/boards/{board}/friends?period=   {"accounts": [id]} -> page of those accounts
//
// A board is {"name", "order", "reset", "validation", "current", "previous"}; a period is {"id", "start", "end"}, start and end in RFC 3339 and absent for an all-time board; a page is {"board", "period", "total", "entries"}; an entry is {"rank", "account", "score", "achieved"}. period is "current" (the default), "previous" or an earlier period's ID; limit defaults to DefaultLimit and radius to DefaultRadius.
//
// Errors are {"error": code} with the status the code implies: invalid_token 401; proof_required, invalid_proof and proof_expired 403; unknown_board, unknown_period and not_ranked 404; bad_request, invalid_account and score_out_of_range 400; request_too_large 413; unavailable 503 when the request was cancelled or timed out; internal 500.
//
// The handler does not limit request rates; put it behind a rate limiter, and behind TLS. It panics when the service has no Options.Tokens.
func (s *Service) Handler() http.Handler {
	if s.tokens == nil {
		panic("selfhost: Handler needs Options.Tokens to verify account tokens")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/boards", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.bearer(w, r); !ok {
			return
		}
		resp := boardsResponse{Boards: []boardResponse{}}
		for _, info := range s.Boards(r.Context()) {
			br := boardResponse{Name: info.Board.Name, Order: string(info.Board.Order), Reset: string(info.Board.Reset), Validation: string(info.Board.Validation), Current: periodJSON(info.Current), Previous: []periodResponse{}}
			for _, p := range info.Previous {
				br.Previous = append(br.Previous, periodJSON(p))
			}
			resp.Boards = append(resp.Boards, br)
		}
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc("POST /v1/boards/{board}/scores", func(w http.ResponseWriter, r *http.Request) {
		account, ok := s.bearer(w, r)
		if !ok {
			return
		}
		var req scoreRequest
		if !readBody(w, r, &req) {
			return
		}
		if req.Score == nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{"bad_request"})
			return
		}
		res, err := s.Submit(r.Context(), Submission{Board: r.PathValue("board"), Account: account, Score: *req.Score, Proof: req.Proof})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resultResponse{Period: periodJSON(res.Period), Entry: entryJSON(res.Entry), Total: res.Total, Improved: res.Improved})
	})
	mux.HandleFunc("GET /v1/boards/{board}/top", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.bearer(w, r); !ok {
			return
		}
		q := r.URL.Query()
		offset, ok1 := intParam(q.Get("offset"), 0)
		limit, ok2 := intParam(q.Get("limit"), DefaultLimit)
		if !ok1 || !ok2 {
			writeJSON(w, http.StatusBadRequest, errorResponse{"bad_request"})
			return
		}
		writePage(w, func() (Page, error) { return s.Top(r.Context(), r.PathValue("board"), q.Get("period"), offset, limit) })
	})
	mux.HandleFunc("GET /v1/boards/{board}/around", func(w http.ResponseWriter, r *http.Request) {
		account, ok := s.bearer(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		radius, ok := intParam(q.Get("radius"), DefaultRadius)
		if !ok {
			writeJSON(w, http.StatusBadRequest, errorResponse{"bad_request"})
			return
		}
		writePage(w, func() (Page, error) {
			return s.Around(r.Context(), r.PathValue("board"), q.Get("period"), account, radius)
		})
	})
	mux.HandleFunc("POST /v1/boards/{board}/friends", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.bearer(w, r); !ok {
			return
		}
		var req friendsRequest
		if !readBody(w, r, &req) {
			return
		}
		writePage(w, func() (Page, error) {
			return s.Friends(r.Context(), r.PathValue("board"), r.URL.Query().Get("period"), req.Accounts)
		})
	})
	return mux
}

type scoreRequest struct {
	Score *int64 `json:"score"`
	Proof string `json:"proof,omitempty"`
}

type friendsRequest struct {
	Accounts []string `json:"accounts"`
}

type periodResponse struct {
	ID    string `json:"id"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type entryResponse struct {
	Rank     int    `json:"rank"`
	Account  string `json:"account"`
	Score    int64  `json:"score"`
	Achieved string `json:"achieved"`
}

type boardResponse struct {
	Name       string           `json:"name"`
	Order      string           `json:"order"`
	Reset      string           `json:"reset"`
	Validation string           `json:"validation,omitempty"`
	Current    periodResponse   `json:"current"`
	Previous   []periodResponse `json:"previous"`
}

type boardsResponse struct {
	Boards []boardResponse `json:"boards"`
}

type resultResponse struct {
	Period   periodResponse `json:"period"`
	Entry    entryResponse  `json:"entry"`
	Total    int            `json:"total"`
	Improved bool           `json:"improved"`
}

type pageResponse struct {
	Board   string          `json:"board"`
	Period  periodResponse  `json:"period"`
	Total   int             `json:"total"`
	Entries []entryResponse `json:"entries"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func periodJSON(p Period) periodResponse {
	out := periodResponse{ID: p.ID}
	if !p.Start.IsZero() {
		out.Start = p.Start.Format(time.RFC3339)
		out.End = p.End.Format(time.RFC3339)
	}
	return out
}

func entryJSON(e Entry) entryResponse {
	return entryResponse{Rank: e.Rank, Account: e.Account, Score: e.Score, Achieved: e.Achieved.Format(time.RFC3339Nano)}
}

func writePage(w http.ResponseWriter, f func() (Page, error)) {
	page, err := f()
	if err != nil {
		writeError(w, err)
		return
	}
	resp := pageResponse{Board: page.Board, Period: periodJSON(page.Period), Total: page.Total, Entries: make([]entryResponse, 0, len(page.Entries))}
	for _, e := range page.Entries {
		resp.Entries = append(resp.Entries, entryJSON(e))
	}
	writeJSON(w, http.StatusOK, resp)
}

// intParam parses a non-negative decimal query parameter, or returns def when it is empty.
func intParam(v string, def int) (int, bool) {
	if v == "" {
		return def, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || strconv.Itoa(n) != v {
		return 0, false
	}
	return n, true
}

// bearer verifies the request's bearer token and returns its account, writing a 401 and returning false when it is missing or invalid.
func (s *Service) bearer(w http.ResponseWriter, r *http.Request) (string, bool) {
	scheme, tok, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if ok && strings.EqualFold(scheme, "Bearer") {
		if claims, err := s.tokens.Verify(strings.TrimSpace(tok)); err == nil && validAccount(claims.Subject) {
			return claims.Subject, true
		}
	}
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	writeJSON(w, http.StatusUnauthorized, errorResponse{"invalid_token"})
	return "", false
}

// readBody decodes a JSON request body into v, refusing unknown fields, trailing data and bodies over maxRequestBytes. It writes the error response and returns false on failure.
func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{"request_too_large"})
		} else {
			writeJSON(w, http.StatusBadRequest, errorResponse{"bad_request"})
		}
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"bad_request"})
		return false
	}
	// A second value, or a stray closing brace or bracket, is trailing data; More alone misses the stray one.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, errorResponse{"bad_request"})
		return false
	}
	return true
}

// writeError writes the response for err; an error outside the service's contract becomes a bare 500.
func writeError(w http.ResponseWriter, err error) {
	codes := []struct {
		err    error
		status int
		code   string
	}{
		{ErrUnknownBoard, http.StatusNotFound, "unknown_board"},
		{ErrUnknownPeriod, http.StatusNotFound, "unknown_period"},
		{ErrNotRanked, http.StatusNotFound, "not_ranked"},
		{ErrInvalidAccount, http.StatusBadRequest, "invalid_account"},
		{ErrScoreOutOfRange, http.StatusBadRequest, "score_out_of_range"},
		{ErrInvalidQuery, http.StatusBadRequest, "bad_request"},
		{ErrProofRequired, http.StatusForbidden, "proof_required"},
		{ErrInvalidProof, http.StatusForbidden, "invalid_proof"},
		{ErrProofExpired, http.StatusForbidden, "proof_expired"},
		{context.DeadlineExceeded, http.StatusServiceUnavailable, "unavailable"},
		{context.Canceled, http.StatusServiceUnavailable, "unavailable"},
	}
	for _, c := range codes {
		if errors.Is(err, c.err) {
			writeJSON(w, c.status, errorResponse{c.code})
			return
		}
	}
	writeJSON(w, http.StatusInternalServerError, errorResponse{"internal"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
