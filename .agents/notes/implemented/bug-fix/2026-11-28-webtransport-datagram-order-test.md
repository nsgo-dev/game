# Agent Note: Synchronize WebTransport datagram ordering coverage

Status: implemented

## Problem

`TestWebTransportUnreliableNotBlockedByLostPacket` measured the first packet received immediately after queuing an unreliable datagram. The server now queues datagrams on a writer goroutine, so scheduler latency could let QUIC's reliable retransmission arrive first on a loaded runner even when the datagram writer had not yet handed its frame to QUIC. The race made the regression test flaky and obscured real head-of-line blocking failures (#1916).

## Decision

The WebTransport outbox counters record successful datagram handoffs to QUIC. The ordering test waits for that counter to advance before checking receive order. Its packet shaper delays the first large packet after the intentional drop, preserving a deterministic retransmission window while leaving small unreliable packets immediately schedulable. The counter remains internal to native transports and does not alter the public `OutboxStats` contract.

## Alternatives considered

- Expose a test-only callback from `writeDatagramLoop`: rejected because a package-global hook would add race-prone production state for diagnostics.
- Call `SendDatagram` synchronously from `Send`: rejected because it restores the blocking behavior the WebTransport outbox was introduced to prevent.
- Relax the packet-order assertion: rejected because the test must continue to fail when unreliable datagrams wait behind reliable retransmissions.

## Consequences

The test now separates writer scheduling from transport ordering and passes repeated race runs under concurrent CPU-heavy tests. Native server counters gain one atomic increment per successful datagram handoff, with no API or wire-format change. The shaper adds 100 ms to one intentionally delayed large packet in this test, increasing its runtime while avoiding timing-dependent retransmission races.
