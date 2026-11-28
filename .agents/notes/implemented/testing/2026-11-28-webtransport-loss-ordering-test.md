# Agent Note: Synchronize WebTransport loss-ordering tests with datagram transmission

Status: implemented

## Problem

`TestWebTransportUnreliableNotBlockedByLostPacket` intentionally drops the first reliable stream frame and checks that an unreliable datagram arrives before QUIC retransmits that frame. WebTransport server sends now pass through a dedicated datagram outbox, so the test's ordering window included an unbounded scheduler delay between `Send` and QUIC receiving the datagram. Under CPU load, QUIC's probe timeout could expire first and make the test fail even though the transport preserved its no-head-of-line-blocking behavior.

## Decision

The WebTransport server increments a session-lived `datagramsSent` counter after `SendDatagram` succeeds. The internal loss-ordering test records the counter before queuing its datagram and waits for it to advance before reading packets and comparing order. The counter is owned by the existing server outbox counters, so reconnects do not reset the observed transmission state, and the test still exercises the real asynchronous outbox path.

## Alternatives considered

- **Call `SendDatagram` directly from `WebTransportServer.Send` in this test path**: this would remove the scheduling window, but it would restore the caller-side blocking that the datagram outbox was introduced to prevent.
- **Delay the retransmitted reliable copy in the packet shaper**: this would make the test less sensitive to QUIC timers, but it would hide regressions by changing the transport timing rather than synchronizing the test with the behavior it measures.

## Consequences

- The test measures packet ordering only after QUIC has accepted the unreliable datagram, removing scheduler-vs-probe-timeout flakes while retaining the asynchronous production path.
- The counter is internal server state used by internal tests and adds one atomic increment per successfully sent datagram.
- If the datagram writer stops making progress, the test now fails with an explicit handoff timeout instead of reporting a misleading packet-ordering failure.

## Verification

- `go tool nsgo test ./pkg/online/netplay -run TestWebTransportUnreliableNotBlockedByLostPacket -count=5`
