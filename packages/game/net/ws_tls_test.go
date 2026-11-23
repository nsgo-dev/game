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

	gamenet "github.com/nsgo-dev/game/packages/game/net"
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
	srv, err := gamenet.ListenWebSocketTLS("127.0.0.1:0", 1, selfSignedTLS(t), []string{"https://allowed.example"})
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
	srv, err := gamenet.ListenWebSocketTLS("127.0.0.1:0", 1, selfSignedTLS(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if status := upgradeStatus(t, srv.Addr().String(), "https://any.example", selfSignedTLS(t)); status != http.StatusSwitchingProtocols {
		t.Fatalf("no-allowlist upgrade = %d, want 101", status)
	}
}

func TestWebSocketTLSFilesRejectsMissingKey(t *testing.T) {
	if _, err := gamenet.ListenWebSocketTLSFiles("127.0.0.1:0", 1, "missing-cert.pem", "missing-key.pem", nil); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("missing certificate error = %v", err)
	}
}
