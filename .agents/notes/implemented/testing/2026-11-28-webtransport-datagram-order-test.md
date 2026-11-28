# Agent Note: Hold back the retransmission in the WebTransport loss-ordering test

Status: implemented

## Problem

`TestWebTransportUnreliableNotBlockedByLostPacket` drops the first transmission of a reliable stream frame, sends one unreliable datagram, and requires the datagram to reach the client before QUIC's retransmission of the reliable frame (#1916). Since 0.2.1 `WebTransportServer.Send` queues datagrams in the client's outbox and `writeDatagramLoop` hands them to QUIC, which adds a goroutine wake-up between `Send` and QUIC. On loopback the client's acknowledgement of the datagram's packet makes QUIC declare the reliable packet lost and resend it within a fraction of a millisecond, and the client reads datagrams and its stream on goroutines of their own, so a retransmission let through could be delivered first even when QUIC had sent the datagram first. The test raced scheduling latency against QUIC's loss detection and failed on a loaded runner.

## Decision

`shapingPacketConn.armDrop` takes a `hold` flag. With it set, after the shaper drops the reliable packet it also holds back every later packet of at least the drop size, which is what carries QUIC's retransmission, until the test calls `release`, which writes the held packets in the order QUIC sent them. The test waits for the unreliable datagram while the reliable data is still held back, then releases and waits for the reliable packet as before. The shaper's state moves under a mutex because drop, hold and release now change together. The server's datagram outbox and every production path are unchanged.

## Alternatives considered

- **Count successful `SendDatagram` handoffs in the server's outbox counters and wait for the counter before asserting (#1917)**: rejected because it only removes the server-side scheduling window; the client still reads datagrams and the stream on separate goroutines, so a retransmission that arrives right after the datagram can still be delivered first. It also adds production state used only by a test.
- **Call `SendDatagram` on the caller's goroutine**: rejected because it restores the blocking that GHSA-gw6m-3wq3-4782 removed.
- **Relax the order assertion**: rejected because the test must still fail when unreliable datagrams wait behind the reliable stream.

## Consequences

- The order the test checks no longer depends on scheduler latency or QUIC's timers: the retransmission cannot reach the client until the datagram has.
- A regression that makes datagrams wait behind the stream still fails the test: the datagram never arrives while the reliable data is held back, and the wait times out.
- If QUIC bundled the small datagram into a large packet together with a retransmission, that packet would be held too and the test would time out; this only happens if the datagram writer is delayed past QUIC's probe timeout, a far wider window than the race this note removes.

## Verification

- `go tool nsgo test ./pkg/online/netplay -run TestWebTransportUnreliableNotBlockedByLostPacket -count=200 -race`
- `go tool nsgo check ./pkg/online/netplay`
