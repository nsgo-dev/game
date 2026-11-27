package netplay

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"time"
)

const (
	// wtCloseGoodbye is the WebTransport session error code sent on a clean Close(), telling the server to report PeerLeft at once.
	wtCloseGoodbye = 0
	// wtCloseReconnect is the WebTransport session error code sent when closing a session to reconnect, telling the server to keep the session for Auth.GracePeriod.
	wtCloseReconnect = 4000

	wtStreamHeaderSize     = 4
	wtMaxStreamMessageSize = 4 << 20 // 4 MiB safety cap on reliable stream frames
	wtMaxHelloFrame        = hsHelloHeader + MaxTokenSize
	wtMaxReplyFrame        = 1 << 10
	wtHandshakeTimeout     = 10 * time.Second
	wtWriteTimeout         = 10 * time.Second
	wtClientPing           = 2 * time.Second
	wtClientIdle           = 10 * time.Second
)

// ErrWebTransportUnavailable is returned by DialWebTransport in browsers that do not expose the WebTransport API.
var ErrWebTransportUnavailable = errors.New("net: browser WebTransport API unavailable")

// WebTransportOptions configures DialWebTransport.
type WebTransportOptions struct {
	// ServerCertificateHashes pins expected SHA-256 digests (32 bytes each) of the server's leaf X.509 DER certificate.
	// In browsers, these are passed as serverCertificateHashes to the WebTransport constructor so self-signed development certificates (at most 14 days validity per the WebTransport specification) work without installing a root CA.
	// On native targets, when non-empty, certificate verification accepts a server whose leaf certificate SHA-256 digest matches any entry instead of verifying against system roots.
	ServerCertificateHashes [][]byte
}

func validateWebTransportURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("net: parse webtransport url: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("net: unsupported webtransport scheme %q (want https)", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, errors.New("net: webtransport url has no host")
	}
	return u, nil
}

func validateCertificateHashes(hashes [][]byte) ([][]byte, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	out := make([][]byte, len(hashes))
	for i, h := range hashes {
		if len(h) != sha256.Size {
			return nil, fmt.Errorf("net: webtransport certificate hash %d has length %d, want %d (SHA-256)", i, len(h), sha256.Size)
		}
		out[i] = slices.Clone(h)
	}
	return out, nil
}

// encodeStreamFrame prefixes payload with a 4-byte big-endian length.
func encodeStreamFrame(payload []byte) ([]byte, error) {
	if len(payload) > wtMaxStreamMessageSize {
		return nil, ErrPayloadTooLarge
	}
	frame := make([]byte, wtStreamHeaderSize+len(payload))
	binary.BigEndian.PutUint32(frame[:wtStreamHeaderSize], uint32(len(payload)))
	copy(frame[wtStreamHeaderSize:], payload)
	return frame, nil
}

// wtStreamDecoder reassembles length-prefixed frames from arbitrary byte chunks received on a WebTransport bidirectional stream.
type wtStreamDecoder struct {
	buf []byte
}

func (d *wtStreamDecoder) write(chunk []byte) {
	d.buf = append(d.buf, chunk...)
}

// next extracts the next complete frame whose length is at most limit bytes.
// It returns (nil, false, nil) when more bytes are needed, or ErrPayloadTooLarge as soon as the 4-byte length header exceeds limit.
func (d *wtStreamDecoder) next(limit uint32) ([]byte, bool, error) {
	if len(d.buf) < wtStreamHeaderSize {
		return nil, false, nil
	}
	n := binary.BigEndian.Uint32(d.buf[:wtStreamHeaderSize])
	if n > limit {
		return nil, false, ErrPayloadTooLarge
	}
	total := wtStreamHeaderSize + int(n)
	if len(d.buf) < total {
		return nil, false, nil
	}
	frame := slices.Clone(d.buf[wtStreamHeaderSize:total])
	rem := copy(d.buf, d.buf[total:])
	clear(d.buf[rem:])
	d.buf = d.buf[:rem]
	if len(d.buf) == 0 && cap(d.buf) > wsEagerAllocCap {
		d.buf = nil
	}
	return frame, true, nil
}

// wsEagerAllocCap is the buffer capacity threshold above which an empty wtStreamDecoder releases its backing array.
const wsEagerAllocCap = 64 << 10
