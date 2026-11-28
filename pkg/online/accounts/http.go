package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts/authtoken"
	"github.com/nsgo-dev/game/pkg/online/internal/cors"
)

// maxRequestBytes bounds a request body.
const maxRequestBytes = 32 << 10

// Handler serves the service over HTTP. Requests and responses are JSON; every response carries Cache-Control: no-store.
//
//	POST /v1/guest                                         -> credentials
//	POST /v1/register          {"email", "password"}       -> credentials
//	POST /v1/signin/email      {"email", "password"}       -> credentials
//	POST /v1/signin/identity   {"provider", "credential"}  -> credentials
//	POST /v1/refresh           {"session"}                 -> credentials
//	POST /v1/signout           {"session"}                 -> 204
//	POST /v1/link/email        {"email", "password"}       -> 204, with a bearer token
//	POST /v1/link/identity     {"provider", "credential"}  -> 204, with a bearer token
//	GET  /v1/me                                            -> profile, with a bearer token
//	GET  /v1/keys                                          -> the key set as YAML
//
// Credentials are {"account", "session", "token", "expires", "created"}, expires in RFC 3339. A bearer token goes in "Authorization: Bearer <token>". Errors are {"error": code} with the status the code implies: invalid_credentials and invalid_token 401, guests_disabled 403, not_found 404, taken and already_linked 409, request_too_large 413, bad_request, invalid_email, weak_password and unknown_provider 400, unavailable 503 when the request was cancelled or timed out (for example waiting for a password hash slot under load), and internal 500.
//
// Browsers may call the handler from Config.AllowedOrigins: it answers their CORS preflight requests and allows Authorization and Content-Type.
//
// The handler does not limit request rates; put it behind a rate limiter, and behind TLS.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/guest", func(w http.ResponseWriter, r *http.Request) {
		if !readBody(w, r, &struct{}{}) {
			return
		}
		s.writeCredentials(w, r, func(ctx context.Context) (Credentials, error) { return s.CreateGuest(ctx) })
	})
	mux.HandleFunc("POST /v1/register", func(w http.ResponseWriter, r *http.Request) {
		var req emailRequest
		if !readBody(w, r, &req) {
			return
		}
		s.writeCredentials(w, r, func(ctx context.Context) (Credentials, error) { return s.Register(ctx, req.Email, req.Password) })
	})
	mux.HandleFunc("POST /v1/signin/email", func(w http.ResponseWriter, r *http.Request) {
		var req emailRequest
		if !readBody(w, r, &req) {
			return
		}
		s.writeCredentials(w, r, func(ctx context.Context) (Credentials, error) { return s.SignInEmail(ctx, req.Email, req.Password) })
	})
	mux.HandleFunc("POST /v1/signin/identity", func(w http.ResponseWriter, r *http.Request) {
		var req identityRequest
		if !readBody(w, r, &req) {
			return
		}
		s.writeCredentials(w, r, func(ctx context.Context) (Credentials, error) {
			return s.SignInIdentity(ctx, req.Provider, req.Credential)
		})
	})
	mux.HandleFunc("POST /v1/refresh", func(w http.ResponseWriter, r *http.Request) {
		var req sessionRequest
		if !readBody(w, r, &req) {
			return
		}
		s.writeCredentials(w, r, func(ctx context.Context) (Credentials, error) { return s.Refresh(ctx, NewSecret(req.Session)) })
	})
	mux.HandleFunc("POST /v1/signout", func(w http.ResponseWriter, r *http.Request) {
		var req sessionRequest
		if !readBody(w, r, &req) {
			return
		}
		writeDone(w, s.SignOut(r.Context(), NewSecret(req.Session)))
	})
	mux.HandleFunc("POST /v1/link/email", func(w http.ResponseWriter, r *http.Request) {
		claims, ok := s.bearer(w, r)
		if !ok {
			return
		}
		var req emailRequest
		if !readBody(w, r, &req) {
			return
		}
		writeDone(w, s.LinkEmail(r.Context(), claims.Subject, req.Email, req.Password))
	})
	mux.HandleFunc("POST /v1/link/identity", func(w http.ResponseWriter, r *http.Request) {
		claims, ok := s.bearer(w, r)
		if !ok {
			return
		}
		var req identityRequest
		if !readBody(w, r, &req) {
			return
		}
		writeDone(w, s.LinkIdentity(r.Context(), claims.Subject, req.Provider, req.Credential))
	})
	mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		claims, ok := s.bearer(w, r)
		if !ok {
			return
		}
		p, err := s.Profile(r.Context(), claims.Subject)
		if err != nil {
			writeError(w, err)
			return
		}
		resp := profileResponse{Account: p.ID, Email: p.Email, Created: p.Created.Format(time.RFC3339Nano), Guest: p.Guest(), Identities: []identityResponse{}}
		for _, id := range p.Identities {
			resp.Identities = append(resp.Identities, identityResponse{Provider: id.Provider, Subject: id.Subject})
		}
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc("GET /v1/keys", func(w http.ResponseWriter, r *http.Request) {
		data, err := s.KeySet().Marshal()
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
	return cors.Wrap(mux, cors.Policy{
		Origins: s.cfg.AllowedOrigins,
		Methods: []string{http.MethodGet, http.MethodPost},
		Headers: []string{"Authorization", "Content-Type"},
	})
}

type emailRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type identityRequest struct {
	Provider   string `json:"provider"`
	Credential string `json:"credential"`
}

type sessionRequest struct {
	Session string `json:"session"`
}

type credentialsResponse struct {
	Account string `json:"account"`
	Session string `json:"session"`
	Token   string `json:"token"`
	Expires string `json:"expires"`
	Created bool   `json:"created"`
}

type identityResponse struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
}

type profileResponse struct {
	Account    string             `json:"account"`
	Email      string             `json:"email,omitempty"`
	Created    string             `json:"created"`
	Guest      bool               `json:"guest"`
	Identities []identityResponse `json:"identities"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// readBody decodes a JSON request body into v, refusing unknown fields, trailing data and bodies over maxRequestBytes. An empty body decodes as {}. It writes the error response and returns false on failure.
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
	if len(bytes.TrimSpace(body)) == 0 {
		body = []byte("{}")
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

// bearer verifies the request's bearer token, writing a 401 and returning false when it is missing or invalid.
func (s *Service) bearer(w http.ResponseWriter, r *http.Request) (authtoken.Claims, bool) {
	auth := r.Header.Get("Authorization")
	scheme, tok, ok := strings.Cut(auth, " ")
	if ok && strings.EqualFold(scheme, "Bearer") {
		if claims, err := s.Authenticate(strings.TrimSpace(tok)); err == nil {
			return claims, true
		}
	}
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	writeJSON(w, http.StatusUnauthorized, errorResponse{"invalid_token"})
	return authtoken.Claims{}, false
}

func (s *Service) writeCredentials(w http.ResponseWriter, r *http.Request, f func(context.Context) (Credentials, error)) {
	c, err := f(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if c.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, credentialsResponse{
		Account: c.AccountID,
		Session: c.Session.Reveal(),
		Token:   c.Token.Reveal(),
		Expires: c.Expires.Format(time.RFC3339),
		Created: c.Created,
	})
}

func writeDone(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// writeError writes the response for err. A cancelled or timed-out request becomes 503 unavailable, which the service does not log; any other error outside the service's contract becomes a bare 500, and the service has logged it.
func writeError(w http.ResponseWriter, err error) {
	codes := []struct {
		err    error
		status int
		code   string
	}{
		{ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials"},
		{ErrGuestsDisabled, http.StatusForbidden, "guests_disabled"},
		{ErrNotFound, http.StatusNotFound, "not_found"},
		{ErrTaken, http.StatusConflict, "taken"},
		{ErrAlreadyLinked, http.StatusConflict, "already_linked"},
		{ErrInvalidEmail, http.StatusBadRequest, "invalid_email"},
		{ErrWeakPassword, http.StatusBadRequest, "weak_password"},
		{ErrUnknownProvider, http.StatusBadRequest, "unknown_provider"},
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
