//go:build release

package debug

import (
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
)

var releaseNetMonitor NetMonitor

// NetMonitor is a zero-cost stub in release builds.
type NetMonitor struct{}

// NewNetMonitor returns a no-op NetMonitor in release builds.
func NewNetMonitor(int) *NetMonitor { return &releaseNetMonitor }

// Wrap returns inner unchanged in release builds.
func (*NetMonitor) Wrap(inner netplay.Transport) netplay.Transport { return inner }

// ObserveSend is a no-op in release builds.
func (*NetMonitor) ObserveSend(netplay.PeerID, netplay.Channel, []byte) {}

// ObserveRecv is a no-op in release builds.
func (*NetMonitor) ObserveRecv(netplay.Packet) {}

// SampleFrame is a no-op in release builds.
func (*NetMonitor) SampleFrame(time.Duration) {}

// Snapshot always returns the zero NetSnapshot in release builds.
func (*NetMonitor) Snapshot() NetSnapshot { return NetSnapshot{} }

// Reset is a no-op in release builds.
func (*NetMonitor) Reset() {}

// SetNet is a no-op in release builds.
func (*Overlay) SetNet(NetSnapshot) {}

// ClearNet is a no-op in release builds.
func (*Overlay) ClearNet() {}

// RefreshNet is a no-op in release builds.
func (*Overlay) RefreshNet() {}

// NetSnapshot always returns the zero NetSnapshot in release builds.
func (*Overlay) NetSnapshot() NetSnapshot { return NetSnapshot{} }
