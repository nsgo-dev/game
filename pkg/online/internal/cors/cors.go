// Package cors validates web origins and wraps an HTTP handler with Cross-Origin Resource Sharing headers for the self-hosted online services.
package cors

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Policy configures CORS handling for an HTTP handler.
type Policy struct {
	// Origins are the web origins (such as "https://play.example.com") allowed to call the handler from a browser. When empty, preflight requests are refused and no Access-Control-Allow-Origin header is sent.
	Origins []string
	// Methods are the HTTP methods advertised in Access-Control-Allow-Methods on preflight responses.
	Methods []string
	// Headers are the request headers advertised in Access-Control-Allow-Headers on preflight responses.
	Headers []string
	// ExposeHeaders are the response headers advertised in Access-Control-Expose-Headers on responses to allowed origins.
	ExposeHeaders []string
}

// Wrap returns an http.Handler that answers CORS preflight requests from p.Origins and sets the CORS response headers on requests from those origins.
func Wrap(next http.Handler, p Policy) http.Handler {
	allowed := make(map[string]bool, len(p.Origins))
	for _, o := range p.Origins {
		allowed[o] = true
	}
	methods := strings.Join(p.Methods, ", ")
	headers := strings.Join(p.Headers, ", ")
	expose := strings.Join(p.ExposeHeaders, ", ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Origin")
		ok := allowed[origin]
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			w.Header().Set("Cache-Control", "no-store")
			if !ok {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			if methods != "" {
				w.Header().Set("Access-Control-Allow-Methods", methods)
			}
			if headers != "" {
				w.Header().Set("Access-Control-Allow-Headers", headers)
			}
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			if expose != "" {
				w.Header().Set("Access-Control-Expose-Headers", expose)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ValidateOrigins checks that every entry in origins is a unique web origin as a browser sends it: an http or https scheme and a host, with no path, query, fragment, user or wildcard.
func ValidateOrigins(origins []string) error {
	seen := make(map[string]bool, len(origins))
	for _, o := range origins {
		if err := validateOrigin(o); err != nil {
			return err
		}
		if seen[o] {
			return fmt.Errorf("allowedOrigins lists %q twice", o)
		}
		seen[o] = true
	}
	return nil
}

// validateOrigin checks that o is a web origin as a browser sends it: an http or https scheme and a host, with no path, query, fragment, user or wildcard.
func validateOrigin(o string) error {
	u, err := url.Parse(o)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Hostname() == "" || strings.HasSuffix(u.Host, ":") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.String() != o || strings.Contains(u.Host, "*") || !validPort(u.Port()) {
		return fmt.Errorf("allowedOrigins entry %q is not an origin such as https://play.example.com", o)
	}
	return nil
}

// validPort reports whether port, from url.URL.Port, is empty or a number from 1 to 65535.
func validPort(port string) bool {
	if port == "" {
		return true
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535 && strconv.Itoa(n) == port
}
