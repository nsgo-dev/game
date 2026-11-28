package netplay

import (
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"time"
)

// CongestionConfig bounds a deterministic byte-rate controller.
type CongestionConfig struct {
	RateMin     int64
	RateMax     int64
	InitialRate int64
	MTU         int
}

// DefaultCongestionConfig is a conservative 2 Mbps controller with 16 KiB datagrams.
var DefaultCongestionConfig = CongestionConfig{RateMin: 32 << 10, RateMax: 8 << 20, InitialRate: 2 << 20, MTU: 1200}

// AckHeader carries the next missing reliable sequence and a 64-bit selective ack mask.
type AckHeader struct {
	Next uint32
	Mask uint64
}

const AckHeaderSize = 12

func (h AckHeader) Encode(dst []byte) bool {
	if len(dst) < AckHeaderSize {
		return false
	}
	binary.BigEndian.PutUint32(dst[:4], h.Next)
	binary.BigEndian.PutUint64(dst[4:12], h.Mask)
	return true
}
func DecodeAckHeader(src []byte) (AckHeader, bool) {
	if len(src) < AckHeaderSize {
		return AckHeader{}, false
	}
	return AckHeader{Next: binary.BigEndian.Uint32(src[:4]), Mask: binary.BigEndian.Uint64(src[4:12])}, true
}

// AckTracker tracks received reliable sequence numbers and produces a compact piggyback header.
type AckTracker struct {
	next uint32
	mask uint64
}

func (a *AckTracker) Receive(seq uint32) {
	if seq < a.next {
		return
	}
	d := seq - a.next
	if d == 0 {
		a.next++
		for a.mask&1 != 0 {
			a.next++
			a.mask >>= 1
		}
		return
	}
	if d <= 64 {
		a.mask |= uint64(1) << (d - 1)
	}
}
func (a AckTracker) Header() AckHeader { return AckHeader{Next: a.next, Mask: a.mask} }

// Outbound is one application message awaiting pacing. Reliable messages may be coalesced with following messages up to MTU.
type Outbound struct {
	Payload  []byte
	Reliable bool
}

// Pacer spreads datagrams over time and coalesces queued messages under MTU.
type Pacer struct {
	cfg         CongestionConfig
	rate        float64
	next        time.Time
	queue       []Outbound
	queuedBytes int64
	lossStreak  int
}

func NewPacer(cfg CongestionConfig) *Pacer {
	d := DefaultCongestionConfig
	if cfg.RateMin > 0 {
		d.RateMin = cfg.RateMin
	}
	if cfg.RateMax > 0 {
		d.RateMax = cfg.RateMax
	}
	if cfg.InitialRate > 0 {
		d.InitialRate = cfg.InitialRate
	}
	if cfg.MTU > 0 {
		d.MTU = cfg.MTU
	}
	if d.RateMin < 1 {
		d.RateMin = 1
	}
	if d.RateMax < d.RateMin {
		d.RateMax = d.RateMin
	}
	if d.InitialRate < d.RateMin {
		d.InitialRate = d.RateMin
	}
	if d.InitialRate > d.RateMax {
		d.InitialRate = d.RateMax
	}
	return &Pacer{cfg: d, rate: float64(d.InitialRate)}
}
func (p *Pacer) Rate() int64 {
	if p == nil {
		return 0
	}
	return int64(p.rate)
}
func (p *Pacer) QueueBytes() int64 {
	if p == nil {
		return 0
	}
	return p.queuedBytes
}
func (p *Pacer) DrainTime() time.Duration {
	if p == nil || p.queuedBytes <= 0 {
		return 0
	}
	return time.Duration(float64(time.Second) * float64(p.queuedBytes) / p.rate)
}
func (p *Pacer) Enqueue(msg Outbound) error {
	if p == nil {
		return errors.New("net: nil pacer")
	}
	if len(msg.Payload) == 0 {
		return errors.New("net: empty payload")
	}
	if len(msg.Payload) > p.cfg.MTU {
		return errors.New("net: payload exceeds MTU")
	}
	msg.Payload = slices.Clone(msg.Payload)
	p.queue = append(p.queue, msg)
	p.queuedBytes += int64(len(msg.Payload))
	return nil
}

// Poll returns datagrams whose pacing deadlines have elapsed at now.
func (p *Pacer) Poll(now time.Time) [][]byte {
	if p == nil || len(p.queue) == 0 {
		return nil
	}
	if p.next.IsZero() {
		p.next = now
	}
	if now.Before(p.next) {
		return nil
	}
	var out [][]byte
	for len(p.queue) > 0 && !now.Before(p.next) {
		first := p.queue[0]
		packet := slices.Clone(first.Payload)
		used := len(packet)
		n := 1
		if first.Reliable {
			for n < len(p.queue) && p.queue[n].Reliable && used+len(p.queue[n].Payload) <= p.cfg.MTU {
				packet = append(packet, p.queue[n].Payload...)
				used += len(p.queue[n].Payload)
				n++
			}
		}
		out = append(out, packet)
		p.queue = p.queue[n:]
		p.queuedBytes -= int64(used)
		p.next = p.next.Add(time.Duration(float64(time.Second) * float64(used) / p.rate))
	}
	return out
}

// Acked applies additive increase after acknowledged bytes.
func (p *Pacer) Acked(bytes int64) {
	if p == nil || bytes <= 0 {
		return
	}
	p.lossStreak = 0
	p.rate += float64(bytes) * 0.125
	if p.rate > float64(p.cfg.RateMax) {
		p.rate = float64(p.cfg.RateMax)
	}
}

// Lost applies multiplicative decrease, bounded by RateMin.
func (p *Pacer) Lost() {
	if p == nil {
		return
	}
	p.lossStreak++
	p.rate *= 0.7
	if p.rate < float64(p.cfg.RateMin) {
		p.rate = float64(p.cfg.RateMin)
	}
}
func (p *Pacer) LossStreak() int {
	if p == nil {
		return 0
	}
	return p.lossStreak
}
func (p *Pacer) SetRate(rate int64) {
	if p == nil || rate <= 0 || math.IsNaN(float64(rate)) {
		return
	}
	if rate < p.cfg.RateMin {
		rate = p.cfg.RateMin
	}
	if rate > p.cfg.RateMax {
		rate = p.cfg.RateMax
	}
	p.rate = float64(rate)
}
