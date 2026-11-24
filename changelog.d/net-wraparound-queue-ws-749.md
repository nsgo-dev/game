### Fixed

- `game/net`: `LockstepSession` and `RollbackSession` now compare `uint32` ticks with RFC 1982 serial-number arithmetic across membership windows, joins, leaves, rollbacks, checksums, and history pruning; `RollbackSession` retains unconfirmed history until the first tick confirms; `sessionTable` drains peer lifecycle events in amortized O(1) without dropping events; and native WebSocket frame reading rejects RFC 6455 §5.5 control-frame violations (fragmented control frames, payloads over 125 bytes, and reserved opcodes `0xB`–`0xF`) (#749).
