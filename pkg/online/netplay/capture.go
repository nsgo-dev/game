package netplay

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"sync"
	"time"
)

// CaptureVersion is the version of the JSON Lines packet capture format written by NewCaptureTransport and read by ReadCapture.
const CaptureVersion = 1

// CaptureDirection identifies whether a captured packet was sent or received by the wrapped Transport.
type CaptureDirection string

const (
	// CaptureSend marks a packet passed to Transport.Send.
	CaptureSend CaptureDirection = "send"
	// CaptureRecv marks a packet returned by Transport.Recv.
	CaptureRecv CaptureDirection = "recv"
)

// CaptureRecord is one line of a version 1 packet capture JSONL stream.
//
// Each line is a self-contained UTF-8 JSON object:
//
//	{"version":1,"t":0,"dir":"send","peer":2,"ch":0,"seq":1,"payload":"AQID"}
//
// Warning: captured payloads contain plaintext player inputs, entity coordinates, and game state. Treat capture files like player telemetry and never ship or publish them without consent.
type CaptureRecord struct {
	// Version is the capture format version; it must equal CaptureVersion (1).
	Version int `json:"version"`
	// T is the monotonic elapsed time in nanoseconds since NewCaptureTransport started capturing.
	T int64 `json:"t"`
	// Dir is "send" for outbound Transport.Send calls or "recv" for inbound Transport.Recv packets.
	Dir CaptureDirection `json:"dir"`
	// Peer is the destination peer ID (or PeerBroadcast) on send, or Packet.From on recv.
	Peer PeerID `json:"peer"`
	// Ch is the transport delivery channel (ChannelReliableOrdered or ChannelUnreliable).
	Ch Channel `json:"ch"`
	// Seq is the transport packet sequence number.
	Seq uint32 `json:"seq"`
	// Payload is the raw plaintext transport payload bytes, encoded as standard base64 in JSON.
	Payload []byte `json:"payload"`
}

// CaptureOptions configures filtering, sampling, timing, and error reporting for NewCaptureTransport.
type CaptureOptions struct {
	// Channels, when non-empty, restricts capture to packets on one of the listed channels.
	Channels []Channel
	// Peers, when non-empty, restricts capture to packets sent to or received from one of the listed peers.
	Peers []PeerID
	// SampleEvery records one out of every N matching packets (0 or 1 records every matching packet).
	SampleEvery int
	// SampleRate, when in (0, 1) and SampleEvery <= 1, probabilistically records each matching packet using SampleSeed.
	SampleRate float64
	// SampleSeed seeds the deterministic PRNG used with SampleRate.
	SampleSeed int64
	// Now returns the clock used to stamp elapsed nanoseconds T since NewCaptureTransport was called; time.Now when nil.
	Now func() time.Time
	// OnError, when non-nil, is called if encoding or writing a capture line fails. Capture write errors never fail Send or Recv.
	OnError func(error)
}

type captureSeqKey struct {
	peer PeerID
	ch   Channel
}

type captureTransport struct {
	inner Transport
	w     io.Writer

	channels    []Channel
	peers       []PeerID
	sampleEvery int
	sampleRate  float64
	now         func() time.Time
	start       time.Time
	onError     func(error)

	mu       sync.Mutex
	sendSeq  map[captureSeqKey]uint32
	matched  uint64
	rng      *rand.Rand
	writeBuf []byte
}

// NewCaptureTransport wraps inner and writes one JSON Lines CaptureRecord to w for every successful Send and every packet returned by Recv that matches opts.
//
// Because it sits at the Transport boundary, wrapping UDPClient or UDPServer captures plaintext packets above authentication and encryption, so captures never need session keys.
//
// The returned Transport also implements InboxReporter by forwarding SetInboxLimits and InboxStats to inner when inner implements InboxReporter.
//
// Warning: captures contain raw player inputs and game state.
func NewCaptureTransport(inner Transport, w io.Writer, opts CaptureOptions) Transport {
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	ct := &captureTransport{
		inner:       inner,
		w:           w,
		channels:    slices.Clone(opts.Channels),
		peers:       slices.Clone(opts.Peers),
		sampleEvery: opts.SampleEvery,
		sampleRate:  opts.SampleRate,
		now:         nowFn,
		start:       nowFn(),
		onError:     opts.OnError,
		sendSeq:     make(map[captureSeqKey]uint32),
	}
	if ct.sampleEvery <= 1 && ct.sampleRate > 0 && ct.sampleRate < 1 {
		seed := uint64(opts.SampleSeed)
		ct.rng = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	}
	return ct
}

func (c *captureTransport) Send(to PeerID, ch Channel, payload []byte) error {
	if c.inner == nil {
		return ErrClosed
	}
	if err := c.inner.Send(to, ch, payload); err != nil {
		return err
	}
	c.recordSend(to, ch, payload)
	return nil
}

func (c *captureTransport) Recv() (Packet, bool) {
	if c.inner == nil {
		return Packet{}, false
	}
	pkt, ok := c.inner.Recv()
	if !ok {
		return Packet{}, false
	}
	c.recordRecv(pkt)
	return pkt, true
}

func (c *captureTransport) LocalID() PeerID {
	if c.inner == nil {
		return 0
	}
	return c.inner.LocalID()
}

func (c *captureTransport) Peers() []PeerID {
	if c.inner == nil {
		return nil
	}
	return c.inner.Peers()
}

func (c *captureTransport) Close() error {
	if c.inner == nil {
		return nil
	}
	return c.inner.Close()
}

func (c *captureTransport) SetInboxLimits(l InboxLimits) {
	if ir, ok := c.inner.(InboxReporter); ok {
		ir.SetInboxLimits(l)
	}
}

func (c *captureTransport) InboxStats() InboxStats {
	if ir, ok := c.inner.(InboxReporter); ok {
		return ir.InboxStats()
	}
	return InboxStats{}
}

func (c *captureTransport) recordSend(to PeerID, ch Channel, payload []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := captureSeqKey{peer: to, ch: ch}
	c.sendSeq[key]++
	seq := c.sendSeq[key]
	c.writeLocked(CaptureSend, to, ch, seq, payload)
}

func (c *captureTransport) recordRecv(pkt Packet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeLocked(CaptureRecv, pkt.From, pkt.Channel, pkt.Seq, pkt.Payload)
}

func (c *captureTransport) writeLocked(dir CaptureDirection, peer PeerID, ch Channel, seq uint32, payload []byte) {
	if c.w == nil {
		return
	}
	if len(c.channels) > 0 && !slices.Contains(c.channels, ch) {
		return
	}
	if len(c.peers) > 0 && !slices.Contains(c.peers, peer) {
		return
	}
	if c.sampleEvery > 1 {
		idx := c.matched
		c.matched++
		if idx%uint64(c.sampleEvery) != 0 {
			return
		}
	} else if c.rng != nil {
		c.matched++
		if c.rng.Float64() >= c.sampleRate {
			return
		}
	} else {
		c.matched++
	}

	elapsed := c.now().Sub(c.start).Nanoseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	pl := payload
	if pl == nil {
		pl = []byte{}
	}
	rec := CaptureRecord{
		Version: CaptureVersion,
		T:       elapsed,
		Dir:     dir,
		Peer:    peer,
		Ch:      ch,
		Seq:     seq,
		Payload: pl,
	}
	line, err := EncodeCaptureRecord(rec)
	if err != nil {
		if c.onError != nil {
			c.onError(err)
		}
		return
	}
	c.writeBuf = append(c.writeBuf[:0], line...)
	c.writeBuf = append(c.writeBuf, '\n')
	if _, err := c.w.Write(c.writeBuf); err != nil && c.onError != nil {
		c.onError(err)
	}
}

// EncodeCaptureRecord validates and encodes one CaptureRecord as a single compact JSON line (without a trailing newline).
// If r.Version is 0, CaptureVersion is written automatically.
func EncodeCaptureRecord(r CaptureRecord) ([]byte, error) {
	if r.Version == 0 {
		r.Version = CaptureVersion
	}
	if err := validateCaptureRecord(&r); err != nil {
		return nil, err
	}
	if r.Payload == nil {
		r.Payload = []byte{}
	}
	return json.Marshal(r)
}

// ParseCaptureRecord strictly decodes and validates one JSON line into a CaptureRecord, refusing unknown fields, trailing data, missing or unsupported versions, invalid directions or channels, negative timestamps, and missing or null payloads.
func ParseCaptureRecord(line []byte) (CaptureRecord, error) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return CaptureRecord{}, errors.New("net: capture record is empty")
	}
	var r CaptureRecord
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return CaptureRecord{}, fmt.Errorf("net: decode capture record: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return CaptureRecord{}, errors.New("net: capture record has trailing data")
		}
		return CaptureRecord{}, fmt.Errorf("net: decode capture record: %w", err)
	}
	if err := validateCaptureRecord(&r); err != nil {
		return CaptureRecord{}, err
	}
	if r.Payload == nil {
		return CaptureRecord{}, errors.New("net: capture record missing payload")
	}
	return r, nil
}

func validateCaptureRecord(r *CaptureRecord) error {
	if r.Version == 0 {
		return fmt.Errorf("net: capture record missing version (want %d)", CaptureVersion)
	}
	if r.Version != CaptureVersion {
		return fmt.Errorf("net: unsupported capture version %d (want %d)", r.Version, CaptureVersion)
	}
	if r.T < 0 {
		return fmt.Errorf("net: capture timestamp %d must not be negative", r.T)
	}
	if r.Dir != CaptureSend && r.Dir != CaptureRecv {
		return fmt.Errorf("net: invalid capture direction %q (want %q or %q)", r.Dir, CaptureSend, CaptureRecv)
	}
	if r.Ch != ChannelReliableOrdered && r.Ch != ChannelUnreliable {
		return fmt.Errorf("net: invalid capture channel %d", r.Ch)
	}
	return nil
}

const maxCaptureLineBytes = 16 << 20 // 16 MiB per JSONL line

// ReadCapture reads every non-empty JSONL line from r and returns the parsed CaptureRecords in order.
func ReadCapture(r io.Reader) ([]CaptureRecord, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxCaptureLineBytes)
	var records []CaptureRecord
	lineNum := 0
	for sc.Scan() {
		lineNum++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		rec, err := ParseCaptureRecord(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		records = append(records, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("net: read capture: %w", err)
	}
	return records, nil
}
