//go:build !js

package wsproto

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// HostPort joins the URL's host and port, defaulting by scheme and keeping IPv6 literals bracketed; checking for a colon cannot tell a port from an IPv6 address (#546).
func HostPort(u *url.URL) (string, error) {
	host := u.Hostname()
	if host == "" {
		return "", errors.New("net: websocket url has no host")
	}
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "ws":
			port = "80"
		case "wss":
			port = "443"
		default:
			return "", fmt.Errorf("net: unsupported websocket scheme %q", u.Scheme)
		}
	}
	return net.JoinHostPort(host, port), nil
}

// reservedHeader reports whether ClientUpgrade writes the named request header itself, so an entry of header that names it is skipped: the handshake's own headers and every Sec-WebSocket-* key.
func reservedHeader(lower string) bool {
	return lower == "host" || lower == "upgrade" || lower == "connection" || strings.HasPrefix(lower, "sec-websocket-")
}

// ClientUpgrade writes the RFC 6455 opening handshake for u to w and validates the server's response read from br, returning the subprotocol the server selected, or "" for none. header carries extra request headers, such as Authorization; entries naming a header the handshake writes itself are skipped. A server that answers a subprotocol the client did not offer is a protocol error.
func ClientUpgrade(w io.Writer, br *bufio.Reader, u *url.URL, header http.Header, protocols []string) (string, error) {
	var keyBytes [16]byte
	_, _ = rand.Read(keyBytes[:]) // crypto/rand.Read never fails since Go 1.24
	secKey := base64.StdEncoding.EncodeToString(keyBytes[:])

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}

	var reqBuf strings.Builder
	fmt.Fprintf(&reqBuf, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&reqBuf, "Host: %s\r\n", u.Host)
	reqBuf.WriteString("Upgrade: websocket\r\n")
	reqBuf.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&reqBuf, "Sec-WebSocket-Key: %s\r\n", secKey)
	reqBuf.WriteString("Sec-WebSocket-Version: 13\r\n")
	if len(protocols) > 0 {
		for _, p := range protocols {
			if p == "" || strings.ContainsAny(p, ",\r\n") {
				return "", fmt.Errorf("net: websocket subprotocol %q is not a valid token", p)
			}
		}
		fmt.Fprintf(&reqBuf, "Sec-WebSocket-Protocol: %s\r\n", strings.Join(protocols, ", "))
	}
	for _, name := range slices.Sorted(maps.Keys(header)) {
		if reservedHeader(strings.ToLower(name)) {
			continue
		}
		if strings.ContainsAny(name, "\r\n") {
			return "", fmt.Errorf("net: websocket handshake header %q holds a newline", name)
		}
		for _, value := range header[name] {
			if strings.ContainsAny(value, "\r\n") {
				return "", fmt.Errorf("net: websocket handshake header %q holds a newline", name)
			}
			fmt.Fprintf(&reqBuf, "%s: %s\r\n", name, value)
		}
	}
	reqBuf.WriteString("\r\n")

	if _, err := io.WriteString(w, reqBuf.String()); err != nil {
		return "", err
	}

	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		return "", fmt.Errorf("net: websocket handshake read: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return "", fmt.Errorf("net: websocket upgrade rejected with status %d", resp.StatusCode)
	}
	if strings.TrimSpace(resp.Header.Get("Sec-WebSocket-Accept")) != AcceptKey(secKey) {
		return "", errors.New("net: websocket invalid Sec-WebSocket-Accept")
	}
	proto := strings.TrimSpace(resp.Header.Get("Sec-WebSocket-Protocol"))
	if proto != "" && !slices.Contains(protocols, proto) {
		return "", fmt.Errorf("net: websocket server selected unrequested subprotocol %q", proto)
	}
	return proto, nil
}
