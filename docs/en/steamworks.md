# Steamworks client

`game/online/steamworks` owns one process-wide Steamworks client while any game scope is attached. It loads the platform library through purego, pumps manual-dispatch callbacks once per frame, exposes overlay and rich-presence helpers, handles `+connect` and `+connect_lobby` launch joins, and provides Web API auth tickets and Steam Deck text input. Use `NewFakeDriver` in tests and handle `ErrUnavailable` on unsupported targets.
