
### Match lifecycle handlers

A room may attach a `gameserver.MatchHandler` before admitting peers. `JoinWithMeta` calls `JoinAttempt` before taking a seat, then calls `Joined` after admission. Returning false leaves the room unchanged and returns the handler's reason. `LeaveWithReason` reports quit, dropped, kicked or replaced departures through `Left`. `Room.Signal` invokes the handler with a copied message, while `Room.Terminate` calls `Terminate(grace)` before ending the room. `Room.SetLabel` replaces searchable metadata copied into `RoomInfo.Label`.

The legacy `Join` and `Leave` methods remain valid and use empty metadata and the `quit` reason. Callbacks should finish quickly and coordinate any longer work through the server's match mailbox.
