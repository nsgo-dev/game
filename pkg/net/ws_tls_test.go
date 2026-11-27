//go:build !js

package net_test

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	gamenet "github.com/nsgo-dev/game/pkg/net"
)

func selfSignedTLS(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}

func upgradeStatus(t *testing.T, addr, origin string, cfg *tls.Config) int {
	t.Helper()
	var conn net.Conn
	var err error
	if cfg != nil {
		conn, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	} else {
		conn, err = net.Dial("tcp", addr)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	req := "GET /ws HTTP/1.1\r\nHost: " + addr + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n"
	if origin != "" {
		req += "Origin: " + origin + "\r\n"
	}
	req += "\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestWebSocketTLSUpgradesAndChecksOrigin(t *testing.T) {
	srv, err := gamenet.ListenWebSocketTLS("127.0.0.1:0", 1, gamenet.Auth{}, selfSignedTLS(t), []string{"https://allowed.example"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	addr := srv.Addr().String()
	if status := upgradeStatus(t, addr, "https://allowed.example", selfSignedTLS(t)); status != http.StatusSwitchingProtocols {
		t.Fatalf("TLS upgrade status = %d, want 101", status)
	}
	if status := upgradeStatus(t, addr, "https://evil.example", selfSignedTLS(t)); status != http.StatusForbidden {
		t.Fatalf("foreign Origin status = %d, want 403", status)
	}
}

func TestWebSocketOriginAllowsAnyAndMissing(t *testing.T) {
	srv, err := gamenet.ListenWebSocketTLS("127.0.0.1:0", 1, gamenet.Auth{}, selfSignedTLS(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if status := upgradeStatus(t, srv.Addr().String(), "https://any.example", selfSignedTLS(t)); status != http.StatusSwitchingProtocols {
		t.Fatalf("no-allowlist upgrade = %d, want 101", status)
	}

	restricted, err := gamenet.ListenWebSocketTLS("127.0.0.1:0", 1, gamenet.Auth{}, selfSignedTLS(t), []string{"https://allowed.example/"})
	if err != nil {
		t.Fatal(err)
	}
	defer restricted.Close()
	// A native client sends no Origin, and a trailing slash on the entry is not part of the Origin.
	if status := upgradeStatus(t, restricted.Addr().String(), "", selfSignedTLS(t)); status != http.StatusSwitchingProtocols {
		t.Fatalf("native client upgrade = %d, want 101", status)
	}
	if status := upgradeStatus(t, restricted.Addr().String(), "https://allowed.example", selfSignedTLS(t)); status != http.StatusSwitchingProtocols {
		t.Fatalf("trailing-slash entry upgrade = %d, want 101", status)
	}

	wildcard, err := gamenet.ListenWebSocketTLS("127.0.0.1:0", 1, gamenet.Auth{}, selfSignedTLS(t), []string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	defer wildcard.Close()
	if status := upgradeStatus(t, wildcard.Addr().String(), "https://any.example", selfSignedTLS(t)); status != http.StatusSwitchingProtocols {
		t.Fatalf("wildcard upgrade = %d, want 101", status)
	}
}

func TestWebSocketOriginRefusesEntriesThatAreNotOrigins(t *testing.T) {
	for _, origin := range []string{"https://allowed.example/play", "allowed.example", ""} {
		if _, err := gamenet.ListenWebSocketTLS("127.0.0.1:0", 1, gamenet.Auth{}, selfSignedTLS(t), []string{origin}); err == nil {
			t.Fatalf("allow-list entry %q accepted, want an error", origin)
		}
	}
}

func TestWebSocketHandshakeRejectsOversizedHeaders(t *testing.T) {
	srv, err := gamenet.ListenWebSocket("127.0.0.1:0", 1, gamenet.Auth{})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	var req strings.Builder
	req.WriteString("GET /ws HTTP/1.1\r\nHost: test\r\nX-Big: ")
	for range 2 << 20 {
		req.WriteByte('a')
	}
	req.WriteString("\r\n\r\n")
	// The server closes once the cap is reached, which may reset the write; either way no 101 comes back.
	_, _ = conn.Write([]byte(req.String()))
	line, _ := bufio.NewReader(conn).ReadString('\n')
	if strings.Contains(line, "101") {
		t.Fatalf("oversized headers upgraded: %q", line)
	}
}

func TestWebSocketTLSFilesRejectsMissingKey(t *testing.T) {
	if _, err := gamenet.ListenWebSocketTLSFiles("127.0.0.1:0", 1, gamenet.Auth{}, "missing-cert.pem", "missing-key.pem", nil); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("missing certificate error = %v", err)
	}
}
