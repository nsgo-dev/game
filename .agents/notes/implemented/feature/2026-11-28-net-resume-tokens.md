# Agent Note: Persist opaque WebSocket resume tokens

Status: implemented

## Problem

Session resume state lived only inside a client transport object, so a reloaded browser or restarted app could not present it to the server (#1692).

## Decision

`WebSocketTransport.ResumeToken` encodes the peer ID and resume secret as unpadded base64url. `DialWebSocketResume` decodes the token and presents it through the existing handshake. Invalid tokens fail closed, while an expired valid token follows the server's existing fresh-join behavior.

## Alternatives considered

- Expose the resume secret directly: rejected because callers should treat the value as an opaque credential.
- Change the handshake wire format: rejected because the existing resume fields already carry the required state.

## Consequences

Games can persist the token in secure storage and resume after a new client object is created. The token has the same lifetime and security requirements as the server's grace period. UDP and per-peer grace extension remain follow-up work.
