# Agent Note: Deterministic netplay pacing boundary

Status: implemented

## Problem

Reliable UDP and relay endpoints can emit every message immediately and send acknowledgements as standalone datagrams. Bursts fill mobile uplink queues, retransmissions add pressure, and reliable traffic delays the unreliable stream.

## Decision

`pkg/online/netplay` exposes `Pacer`, a bounded-MTU queue that coalesces adjacent reliable payloads and schedules datagrams from a caller-provided virtual time. `CongestionConfig` bounds the byte rate. `Acked` applies additive increase and `Lost` applies multiplicative decrease, keeping the rate between the configured minimum and maximum. `AckTracker` emits a 32-bit next sequence and 64-bit selective mask for piggyback headers.

The boundary is independent of `RelayTransport` and UDP framing because #1440's shared reliable endpoint extraction is not merged yet. Transport implementations can wrap `Poll` output and attach `AckHeader` without changing gameplay code. No goroutines or wall-clock reads occur in the component, so replay tests can drive exact timestamps.

## Alternatives considered

- Leave pacing to each transport implementation. That avoids a new shared API, but duplicates coalescing and AIMD behavior and makes relay and UDP diverge.
- Use a timer goroutine to pace sends. It can smooth sends in production, but introduces races and wall-clock dependence into tests and rollback.

## Consequences

Reliable endpoint implementations gain a reusable queue, rate telemetry and piggyback-ready ack state. The current change does not alter wire framing or LoopbackHub loss behavior, so existing transports remain compatible until the shared endpoint work lands.
