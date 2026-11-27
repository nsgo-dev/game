### Fixed

- `game/online/server`: `Rooms.Create` records the room's initial empty timestamp so rooms nobody joins close after `EmptyTimeout` on `Tick` (releasing their private codes), and `Rooms.Join`/`JoinCode` refuse with `ErrRoomNotFound` if `Tick` closed the room concurrently (#1599).
