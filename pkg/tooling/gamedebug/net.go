//go:build !release

package gamedebug

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	defaultNetRecentLimit = 12
	netRateWindowSlots    = 60
	initialNetPeerCap     = 16
	maxOverlayNetRecent   = 8
)

type netRateBucket struct {
	dt        time.Duration
	sendBytes uint64
	recvBytes uint64
	sendPkts  uint64
	recvPkts  uint64
}

type netRateWindow struct {
	slots      [netRateWindowSlots]netRateBucket
	head       int
	count      int
	windowDur  time.Duration
	cur        netRateBucket
	totalSendB uint64
	totalRecvB uint64
	totalSendP uint64
	totalRecvP uint64
}

func (w *netRateWindow) addSend(n int) {
	w.cur.sendBytes += uint64(n)
	w.cur.sendPkts++
}

func (w *netRateWindow) addRecv(n int) {
	w.cur.recvBytes += uint64(n)
	w.cur.recvPkts++
}

func (w *netRateWindow) commit(dt time.Duration) {
	if dt <= 0 {
		dt = time.Second / 60
	}
	w.cur.dt = dt
	if w.count == netRateWindowSlots {
		old := w.slots[w.head]
		w.windowDur -= old.dt
		w.totalSendB -= old.sendBytes
		w.totalRecvB -= old.recvBytes
		w.totalSendP -= old.sendPkts
		w.totalRecvP -= old.recvPkts
	} else {
		w.count++
	}
	w.slots[w.head] = w.cur
	w.windowDur += w.cur.dt
	w.totalSendB += w.cur.sendBytes
	w.totalRecvB += w.cur.recvBytes
	w.totalSendP += w.cur.sendPkts
	w.totalRecvP += w.cur.recvPkts
	w.head = (w.head + 1) % netRateWindowSlots
	w.cur = netRateBucket{}
}

func (w *netRateWindow) rates() (totalB, totalP, sendB, recvB, sendP, recvP float64) {
	secs := w.windowDur.Seconds()
	if secs <= 0 {
		return 0, 0, 0, 0, 0, 0
	}
	sendB = float64(w.totalSendB) / secs
	recvB = float64(w.totalRecvB) / secs
	sendP = float64(w.totalSendP) / secs
	recvP = float64(w.totalRecvP) / secs
	return sendB + recvB, sendP + recvP, sendB, recvB, sendP, recvP
}

func (w *netRateWindow) reset() {
	*w = netRateWindow{}
}

type netPeerTracker struct {
	peer   netplay.PeerID
	window netRateWindow
}

// wireClassifier decodes packets using the generated #1301 wire codecs into caller-owned reused buffers without steady-state heap allocations.
type wireClassifier struct {
	relayFrame   wire.RelayFrame
	relayMembers wire.RelayMembers
	relayBody    wire.RelayBody
	replMsg      wire.ReplicationMessage
	rollbackMsg  wire.RollbackMessage
	memberMsg    wire.MembershipMessage
	handshakeMsg wire.HandshakeMessage
}

func (c *wireClassifier) classify(payload []byte) string {
	if len(payload) == 0 {
		return "empty"
	}
	if payload[0] == wire.RelayFrameVersion {
		if n, err := wire.DecodeRelayFrame(payload, &c.relayFrame); err == nil && n == len(payload) {
			if c.relayFrame.Peer == 0 {
				if mn, merr := wire.DecodeRelayMembers(c.relayFrame.Body, &c.relayMembers); merr == nil && mn == len(c.relayFrame.Body) {
					return "relay.members"
				}
			} else if un, uerr := wire.DecodeRelayBody(c.relayFrame.Body, &c.relayBody); uerr == nil && un == len(c.relayFrame.Body) {
				switch c.relayBody.Tag {
				case wire.RelayBodyTagAck:
					return "relay.ack"
				case wire.RelayBodyTagUnreliable:
					return relayUnreliableLabel(c.classifyApp(c.relayBody.Unreliable.Payload))
				case wire.RelayBodyTagReliable:
					return relayReliableLabel(c.classifyApp(c.relayBody.Reliable.Payload))
				}
			}
		}
	}
	return c.classifyApp(payload)
}

func (c *wireClassifier) classifyApp(payload []byte) string {
	if len(payload) == 0 {
		return "empty"
	}
	if n, err := wire.DecodeReplicationMessage(payload, &c.replMsg); err == nil && n == len(payload) {
		switch c.replMsg.Tag {
		case wire.ReplicationMessageTagFull:
			return "replication.full"
		case wire.ReplicationMessageTagDelta:
			return "replication.delta"
		case wire.ReplicationMessageTagAck:
			return "replication.ack"
		case wire.ReplicationMessageTagResync:
			return "replication.resync"
		case wire.ReplicationMessageTagFullPart:
			return "replication.full_part"
		}
	}
	if n, err := wire.DecodeRollbackMessage(payload, &c.rollbackMsg); err == nil && n == len(payload) {
		switch c.rollbackMsg.Tag {
		case wire.RollbackMessageTagSingleInput:
			return "rollback.single_input"
		case wire.RollbackMessageTagInputBundle:
			return "rollback.input_bundle"
		case wire.RollbackMessageTagChecksum:
			return "rollback.checksum"
		}
	}
	if n, err := wire.DecodeMembershipMessage(payload, &c.memberMsg); err == nil && n == len(payload) {
		switch c.memberMsg.Tag {
		case wire.MembershipMessageTagChange:
			return "membership.change"
		case wire.MembershipMessageTagJoinStatePart:
			return "membership.join_state_part"
		}
	}
	if n, err := wire.DecodeHandshakeMessage(payload, &c.handshakeMsg); err == nil && n == len(payload) {
		switch c.handshakeMsg.Tag {
		case wire.HandshakeMessageTagHello:
			return "handshake.hello"
		case wire.HandshakeMessageTagWelcome:
			return "handshake.welcome"
		case wire.HandshakeMessageTagReject:
			return "handshake.reject"
		}
	}
	return "raw"
}

func relayUnreliableLabel(inner string) string {
	switch inner {
	case "replication.full":
		return "relay.unreliable/replication.full"
	case "replication.delta":
		return "relay.unreliable/replication.delta"
	case "replication.ack":
		return "relay.unreliable/replication.ack"
	case "replication.resync":
		return "relay.unreliable/replication.resync"
	case "replication.full_part":
		return "relay.unreliable/replication.full_part"
	case "rollback.single_input":
		return "relay.unreliable/rollback.single_input"
	case "rollback.input_bundle":
		return "relay.unreliable/rollback.input_bundle"
	case "rollback.checksum":
		return "relay.unreliable/rollback.checksum"
	case "membership.change":
		return "relay.unreliable/membership.change"
	case "membership.join_state_part":
		return "relay.unreliable/membership.join_state_part"
	case "empty":
		return "relay.unreliable"
	default:
		return "relay.unreliable/raw"
	}
}

func relayReliableLabel(inner string) string {
	switch inner {
	case "replication.full":
		return "relay.reliable/replication.full"
	case "replication.delta":
		return "relay.reliable/replication.delta"
	case "replication.ack":
		return "relay.reliable/replication.ack"
	case "replication.resync":
		return "relay.reliable/replication.resync"
	case "replication.full_part":
		return "relay.reliable/replication.full_part"
	case "rollback.single_input":
		return "relay.reliable/rollback.single_input"
	case "rollback.input_bundle":
		return "relay.reliable/rollback.input_bundle"
	case "rollback.checksum":
		return "relay.reliable/rollback.checksum"
	case "membership.change":
		return "relay.reliable/membership.change"
	case "membership.join_state_part":
		return "relay.reliable/membership.join_state_part"
	case "empty":
		return "relay.reliable"
	default:
		return "relay.reliable/raw"
	}
}

// NetMonitor tracks per-channel and per-peer throughput rates, Transport InboxStats, and recent decoded wire message types using reused buffers so per-frame updates perform zero heap allocations in steady state.
type NetMonitor struct {
	mu         sync.Mutex
	inboxSrc   netplay.InboxReporter
	classifier wireClassifier

	channels [2]netRateWindow
	peers    []netPeerTracker

	recentBuf   []NetMessageEvent
	recentHead  int
	recentCount int

	snapChannels []NetChannelStats
	snapPeers    []NetPeerStats
	snapRecent   []NetMessageEvent
	snap         NetSnapshot
}

// NewNetMonitor creates a NetMonitor retaining up to recentLimit recent message types (defaulting to 12 when <= 0).
func NewNetMonitor(recentLimit int) *NetMonitor {
	if recentLimit <= 0 {
		recentLimit = defaultNetRecentLimit
	}
	return &NetMonitor{
		peers:        make([]netPeerTracker, 0, initialNetPeerCap),
		recentBuf:    make([]NetMessageEvent, recentLimit),
		snapChannels: make([]NetChannelStats, 0, 2),
		snapPeers:    make([]NetPeerStats, 0, initialNetPeerCap),
		snapRecent:   make([]NetMessageEvent, 0, recentLimit),
	}
}

type netMonitorTransport struct {
	inner netplay.Transport
	mon   *NetMonitor
}

// Wrap wraps inner so every successful Send and every Recv packet is observed by m with zero steady-state heap allocations.
// If inner implements netplay.InboxReporter, SampleFrame polls its InboxStats automatically and the wrapper forwards InboxReporter calls.
func (m *NetMonitor) Wrap(inner netplay.Transport) netplay.Transport {
	if m == nil || inner == nil {
		return inner
	}
	if ir, ok := inner.(netplay.InboxReporter); ok {
		m.mu.Lock()
		m.inboxSrc = ir
		m.mu.Unlock()
	}
	return &netMonitorTransport{inner: inner, mon: m}
}

func (t *netMonitorTransport) Send(to netplay.PeerID, ch netplay.Channel, payload []byte) error {
	if err := t.inner.Send(to, ch, payload); err != nil {
		return err
	}
	t.mon.ObserveSend(to, ch, payload)
	return nil
}

func (t *netMonitorTransport) Recv() (netplay.Packet, bool) {
	pkt, ok := t.inner.Recv()
	if !ok {
		return netplay.Packet{}, false
	}
	t.mon.ObserveRecv(pkt)
	return pkt, true
}

func (t *netMonitorTransport) LocalID() netplay.PeerID { return t.inner.LocalID() }
func (t *netMonitorTransport) Peers() []netplay.PeerID { return t.inner.Peers() }
func (t *netMonitorTransport) Close() error            { return t.inner.Close() }

func (t *netMonitorTransport) SetInboxLimits(l netplay.InboxLimits) {
	if ir, ok := t.inner.(netplay.InboxReporter); ok {
		ir.SetInboxLimits(l)
	}
}

func (t *netMonitorTransport) InboxStats() netplay.InboxStats {
	if ir, ok := t.inner.(netplay.InboxReporter); ok {
		return ir.InboxStats()
	}
	return netplay.InboxStats{}
}

// ObserveSend records one outbound packet and decodes its wire message type into reused buffers without steady-state allocations.
func (m *NetMonitor) ObserveSend(to netplay.PeerID, ch netplay.Channel, payload []byte) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if int(ch) < len(m.channels) {
		m.channels[ch].addSend(len(payload))
	}
	m.peerTrackerLocked(to).window.addSend(len(payload))
	msgType := m.classifier.classify(payload)
	m.pushRecentLocked(NetMessageEvent{
		Dir:     netplay.CaptureSend,
		Peer:    to,
		Channel: ch,
		Type:    msgType,
		Bytes:   len(payload),
	})
}

// ObserveRecv records one inbound packet and decodes its wire message type into reused buffers without steady-state allocations.
func (m *NetMonitor) ObserveRecv(pkt netplay.Packet) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if int(pkt.Channel) < len(m.channels) {
		m.channels[pkt.Channel].addRecv(len(pkt.Payload))
	}
	m.peerTrackerLocked(pkt.From).window.addRecv(len(pkt.Payload))
	msgType := m.classifier.classify(pkt.Payload)
	m.pushRecentLocked(NetMessageEvent{
		Dir:     netplay.CaptureRecv,
		Peer:    pkt.From,
		Channel: pkt.Channel,
		Type:    msgType,
		Bytes:   len(pkt.Payload),
	})
}

func (m *NetMonitor) peerTrackerLocked(peer netplay.PeerID) *netPeerTracker {
	for i := range m.peers {
		if m.peers[i].peer == peer {
			return &m.peers[i]
		}
		if m.peers[i].peer > peer {
			m.peers = append(m.peers, netPeerTracker{})
			copy(m.peers[i+1:], m.peers[i:])
			m.peers[i] = netPeerTracker{peer: peer}
			return &m.peers[i]
		}
	}
	m.peers = append(m.peers, netPeerTracker{peer: peer})
	return &m.peers[len(m.peers)-1]
}

func (m *NetMonitor) pushRecentLocked(ev NetMessageEvent) {
	if len(m.recentBuf) == 0 {
		return
	}
	m.recentBuf[m.recentHead] = ev
	m.recentHead = (m.recentHead + 1) % len(m.recentBuf)
	if m.recentCount < len(m.recentBuf) {
		m.recentCount++
	}
}

// SampleFrame commits the current frame's packet/byte counts into the sliding rate window, polls InboxStats from any wrapped InboxReporter, and refreshes the cached NetSnapshot without steady-state heap allocations.
func (m *NetMonitor) SampleFrame(dt time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.channels {
		m.channels[i].commit(dt)
	}
	for i := range m.peers {
		m.peers[i].window.commit(dt)
	}

	var inbox netplay.InboxStats
	if m.inboxSrc != nil {
		inbox = m.inboxSrc.InboxStats()
	}

	m.snapChannels = m.snapChannels[:0]
	for i := range m.channels {
		tb, tp, sb, rb, sp, rp := m.channels[i].rates()
		m.snapChannels = append(m.snapChannels, NetChannelStats{
			Channel:           netplay.Channel(i),
			BytesPerSec:       tb,
			PacketsPerSec:     tp,
			SendBytesPerSec:   sb,
			RecvBytesPerSec:   rb,
			SendPacketsPerSec: sp,
			RecvPacketsPerSec: rp,
		})
	}

	m.snapPeers = m.snapPeers[:0]
	for i := range m.peers {
		tb, tp, sb, rb, sp, rp := m.peers[i].window.rates()
		m.snapPeers = append(m.snapPeers, NetPeerStats{
			Peer:              m.peers[i].peer,
			BytesPerSec:       tb,
			PacketsPerSec:     tp,
			SendBytesPerSec:   sb,
			RecvBytesPerSec:   rb,
			SendPacketsPerSec: sp,
			RecvPacketsPerSec: rp,
		})
	}

	m.snapRecent = m.snapRecent[:0]
	if m.recentCount > 0 {
		start := (m.recentHead - m.recentCount + len(m.recentBuf)) % len(m.recentBuf)
		for i := 0; i < m.recentCount; i++ {
			idx := (start + i) % len(m.recentBuf)
			m.snapRecent = append(m.snapRecent, m.recentBuf[idx])
		}
	}

	m.snap = NetSnapshot{
		Inbox:    inbox,
		Channels: m.snapChannels,
		Peers:    m.snapPeers,
		Recent:   m.snapRecent,
	}
}

// Snapshot returns the most recent NetSnapshot built by SampleFrame, backed by m's reused buffers so calling it in Options.Net allocates zero bytes.
func (m *NetMonitor) Snapshot() NetSnapshot {
	if m == nil {
		return NetSnapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snap
}

// Reset clears all accumulated traffic counters, peer trackers, and recent message history while keeping allocated slice capacities.
func (m *NetMonitor) Reset() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.channels {
		m.channels[i].reset()
	}
	m.peers = m.peers[:0]
	m.recentHead = 0
	m.recentCount = 0
	m.snapChannels = m.snapChannels[:0]
	m.snapPeers = m.snapPeers[:0]
	m.snapRecent = m.snapRecent[:0]
	m.snap = NetSnapshot{}
}

// SetNet updates the network inspector panel's pushed snapshot and immediately refreshes the panel. It is used when Options.Net is nil.
func (o *Overlay) SetNet(snap NetSnapshot) {
	if o == nil {
		return
	}
	copyNetSnapshot(&o.netSnap, snap)
	o.hasNet = true
	o.netTick++
	if o.netSig != nil {
		o.netSig.Set(o.netTick)
	}
}

// ClearNet clears the pushed network snapshot and hides the network inspector panel when Options.Net is nil.
func (o *Overlay) ClearNet() {
	if o == nil {
		return
	}
	o.netSnap.Inbox = netplay.InboxStats{}
	o.netSnap.Channels = o.netSnap.Channels[:0]
	o.netSnap.Peers = o.netSnap.Peers[:0]
	o.netSnap.Recent = o.netSnap.Recent[:0]
	o.hasNet = false
	o.netTick++
	if o.netSig != nil {
		o.netSig.Set(o.netTick)
	}
}

// RefreshNet tells the network inspector panel to re-read Options.Net immediately.
func (o *Overlay) RefreshNet() {
	if o == nil {
		return
	}
	if o.opts.Net != nil {
		copyNetSnapshot(&o.lastNet, o.opts.Net())
	}
	o.netTick++
	if o.netSig != nil {
		o.netSig.Set(o.netTick)
	}
}

// NetSnapshot returns the currently displayed network snapshot from Options.Net if configured, or the most recent SetNet value.
func (o *Overlay) NetSnapshot() NetSnapshot {
	if o == nil {
		return NetSnapshot{}
	}
	if o.opts.Net != nil {
		return o.opts.Net()
	}
	return o.netSnap
}

func (o *Overlay) currentNet() (NetSnapshot, bool) {
	if o.netSig != nil {
		_ = o.netSig.Get()
	}
	if o.opts.Net != nil {
		return o.opts.Net(), true
	}
	if o.hasNet {
		return o.netSnap, true
	}
	return NetSnapshot{}, false
}

func (o *Overlay) netInspectorView() core.Node {
	return core.Show{
		When: func() bool {
			_, ok := o.currentNet()
			return ok
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "debug-net-inspector",
				Style:  sectionDividerStyle,
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						snap, ok := o.currentNet()
						if !ok {
							return nil
						}
						nodes := []core.Node{
							core.Text{Content: "Net", Style: sectionHeaderStyle},
						}

						inboxStyle := layerLabelStyle
						if snap.Inbox.UnreliableDropped > 0 || snap.Inbox.ReliableRefused > 0 || snap.Inbox.ReliableBacklog {
							inboxStyle = alertTextStyle
						}
						inboxLine := fmt.Sprintf("inbox: rel %d / unrel %d · drop %d · ref %d · warn %d",
							snap.Inbox.Reliable,
							snap.Inbox.Unreliable,
							snap.Inbox.UnreliableDropped,
							snap.Inbox.ReliableRefused,
							snap.Inbox.ReliableWarnings,
						)
						nodes = append(nodes, core.Text{Content: inboxLine, Style: inboxStyle})

						for _, ch := range snap.Channels {
							val := fmt.Sprintf("%s/s · %.1f pkt/s", formatBytes(int64(ch.BytesPerSec)), ch.PacketsPerSec)
							nodes = append(nodes, core.View{
								Style: core.Style{
									Direction:      core.Row,
									JustifyContent: core.JustifySpaceBetween,
									Gap:            8,
								},
								Children: []core.Node{
									core.Text{Content: ch.Channel.String(), Style: layerLabelStyle},
									core.Text{Content: val, Style: layerCountStyle},
								},
							})
						}

						for _, p := range snap.Peers {
							label := fmt.Sprintf("peer %d", p.Peer)
							if p.Peer == netplay.PeerBroadcast {
								label = "broadcast"
							}
							val := fmt.Sprintf("%s/s · %.1f pkt/s", formatBytes(int64(p.BytesPerSec)), p.PacketsPerSec)
							nodes = append(nodes, core.View{
								Style: core.Style{
									Direction:      core.Row,
									JustifyContent: core.JustifySpaceBetween,
									Gap:            8,
								},
								Children: []core.Node{
									core.Text{Content: label, Style: layerLabelStyle},
									core.Text{Content: val, Style: layerCountStyle},
								},
							})
						}

						if len(snap.Recent) > 0 {
							start := 0
							if len(snap.Recent) > maxOverlayNetRecent {
								start = len(snap.Recent) - maxOverlayNetRecent
							}
							recentRows := make([]core.Node, 0, len(snap.Recent)-start+1)
							recentRows = append(recentRows, core.Text{
								Content: fmt.Sprintf("Recent (%d)", len(snap.Recent)),
								Style:   sectionHeaderStyle,
							})
							for i := len(snap.Recent) - 1; i >= start; i-- {
								ev := snap.Recent[i]
								line := fmt.Sprintf("%s #%d %s (%d B)", ev.Dir, ev.Peer, ev.Type, ev.Bytes)
								recentRows = append(recentRows, core.Text{Content: line, Style: layerLabelStyle})
							}
							nodes = append(nodes, core.View{
								TestID:   "debug-net-recent",
								Style:    core.Style{Gap: 2},
								Children: recentRows,
							})
						}

						return core.Group(nodes)
					}),
				},
			}
		},
	}
}

func copyNetSnapshot(dst *NetSnapshot, src NetSnapshot) {
	dst.Inbox = src.Inbox
	dst.Channels = append(dst.Channels[:0], src.Channels...)
	dst.Peers = append(dst.Peers[:0], src.Peers...)
	dst.Recent = append(dst.Recent[:0], src.Recent...)
}

func netSnapshotsEqual(a, b NetSnapshot) bool {
	return a.Inbox == b.Inbox &&
		slices.Equal(a.Channels, b.Channels) &&
		slices.Equal(a.Peers, b.Peers) &&
		slices.Equal(a.Recent, b.Recent)
}
