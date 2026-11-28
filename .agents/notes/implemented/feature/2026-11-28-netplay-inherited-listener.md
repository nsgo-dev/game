# Agent Note: Serve WebSocket sessions on caller-owned listeners

Status: implemented

## Problem

Dedicated servers could only bind WebSockets by address, so a replacement process could not inherit a systemd or parent-owned listener while the old process drained existing sessions (#1698).

## Decision

`ServeWebSocket` accepts a caller-owned `net.Listener` and starts the same session machinery as address-based listeners. `StopAccepting` closes only the listener, leaving connected sessions and their outboxes alive for a handoff. UDP remains address-owned because two readers cannot safely share one datagram socket.

## Alternatives considered

- Add `SO_REUSEPORT` and let both processes bind: rejected because UDP delivery would split datagrams and TCP behavior differs by platform.
- Close the whole server on handoff: rejected because it drops existing sessions instead of allowing a drain.

## Consequences

Systemd socket activation and parent/child file-descriptor handoff can now use the WebSocket transport without a rebinding gap. The caller owns listener lifetime and must coordinate readiness and eventual server close.
