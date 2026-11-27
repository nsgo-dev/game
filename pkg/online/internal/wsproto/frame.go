// Package wsproto implements the RFC 6455 WebSocket wire format shared by the netplay transports and pkg/online/wsclient: frame reading and writing with slow-loris-safe allocation, the opening handshake's accept key, and the native client upgrade. It is internal to pkg/online; games use netplay or wsclient.
package wsproto

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"strings"
)

const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	OpContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	OpClose        = 0x8
	OpPing         = 0x9
	OpPong         = 0xA
	// MaxControlPayload bounds a control frame's payload (RFC 6455 §5.5).
	MaxControlPayload = 125
	// MaxMessageSize is the largest frame payload ReadFrame reads and WriteFrame writes.
	MaxMessageSize = 4 << 20 // 4 MiB safety cap
	// EagerAlloc is the largest payload ReadFrameLimit allocates before its bytes arrive. A larger payload grows with the bytes actually received, so a peer that announces a 4 MiB frame and then trickles it (a slow-loris attack) holds memory only for what it has sent (#546).
	EagerAlloc = 64 << 10
)

var (
	// ErrPayloadTooLarge is returned when a frame's payload exceeds the reader's limit or MaxMessageSize on write.
	ErrPayloadTooLarge = errors.New("net: payload too large")
	// ErrInvalidPacket is returned when a frame breaks RFC 6455: a fragmented or oversized control frame, or a reserved control opcode.
	ErrInvalidPacket = errors.New("net: invalid packet")
)

// readPayload reads exactly n payload bytes. Up to EagerAlloc it allocates them at once; past that the buffer grows as bytes arrive instead of trusting the length the peer announced.
func readPayload(r io.Reader, n uint64) ([]byte, error) {
	if n <= EagerAlloc {
		buf := make([]byte, n)
		_, err := io.ReadFull(r, buf)
		return buf, err
	}
	var buf bytes.Buffer
	buf.Grow(EagerAlloc)
	if _, err := io.CopyN(&buf, r, int64(n)); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return buf.Bytes(), nil
}

// AcceptKey derives the Sec-WebSocket-Accept response value for a client's Sec-WebSocket-Key (RFC 6455 §4.2.2).
func AcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(strings.TrimSpace(key)))
	h.Write([]byte(magicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// WriteFrame writes one unfragmented frame of at most MaxMessageSize payload bytes.
func WriteFrame(w io.Writer, opcode byte, masked bool, payload []byte) error {
	return WriteFrameLimit(w, opcode, masked, payload, MaxMessageSize)
}

// WriteFrameLimit writes one unfragmented frame whose payload is at most limit bytes. Clients mask, servers do not (RFC 6455 §5.3).
func WriteFrameLimit(w io.Writer, opcode byte, masked bool, payload []byte, limit uint64) error {
	if uint64(len(payload)) > limit {
		return ErrPayloadTooLarge
	}
	var header [14]byte
	header[0] = 0x80 | (opcode & 0x0F) // FIN=1

	var maskBit byte
	if masked {
		maskBit = 0x80
	}

	n := 2
	plen := len(payload)
	switch {
	case plen < 126:
		header[1] = maskBit | byte(plen)
	case plen <= 0xFFFF:
		header[1] = maskBit | 126
		binary.BigEndian.PutUint16(header[2:4], uint16(plen))
		n = 4
	default:
		header[1] = maskBit | 127
		binary.BigEndian.PutUint64(header[2:10], uint64(plen))
		n = 10
	}

	if masked {
		var maskKey [4]byte
		if _, err := rand.Read(maskKey[:]); err != nil {
			return err
		}
		copy(header[n:n+4], maskKey[:])
		n += 4
		buf := make([]byte, n+plen)
		copy(buf[:n], header[:n])
		for i := range plen {
			buf[n+i] = payload[i] ^ maskKey[i&3]
		}
		_, err := w.Write(buf)
		return err
	}

	if _, err := w.Write(header[:n]); err != nil {
		return err
	}
	if plen > 0 {
		_, err := w.Write(payload)
		return err
	}
	return nil
}

// ReadFrame reads one frame of at most MaxMessageSize payload bytes.
func ReadFrame(r *bufio.Reader) (fin bool, opcode byte, payload []byte, masked bool, err error) {
	return ReadFrameLimit(r, MaxMessageSize)
}

// ReadFrameLimit reads one frame whose payload is at most limit bytes.
func ReadFrameLimit(r *bufio.Reader, limit uint64) (fin bool, opcode byte, payload []byte, masked bool, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return false, 0, nil, false, err
	}
	fin = (hdr[0] & 0x80) != 0
	opcode = hdr[0] & 0x0F
	masked = (hdr[1] & 0x80) != 0
	plen := uint64(hdr[1] & 0x7F)

	// RFC 6455 §5.5: control frames (opcodes 0x8-0xF) must not be fragmented,
	// must carry at most 125 payload bytes, and opcodes 0xB-0xF are reserved.
	if opcode >= OpClose {
		if opcode > OpPong || !fin || plen > MaxControlPayload {
			return false, 0, nil, false, ErrInvalidPacket
		}
	}

	switch plen {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, false, err
		}
		plen = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, false, err
		}
		plen = binary.BigEndian.Uint64(ext[:])
	}

	if plen > limit {
		return false, 0, nil, false, ErrPayloadTooLarge
	}

	var maskKey [4]byte
	if masked {
		if _, err = io.ReadFull(r, maskKey[:]); err != nil {
			return false, 0, nil, false, err
		}
	}

	if payload, err = readPayload(r, plen); err != nil {
		return false, 0, nil, false, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i&3]
		}
	}
	return fin, opcode, payload, masked, nil
}
