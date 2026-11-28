# Steamworks

`pkg/online/steamworks` owns one process-wide lifecycle for Steam features. Open it once with the game's App ID and provide the platform driver from the desktop binding. Call `Pump` once per engine frame and `Close` when the app shuts down. Presence and Workshop integrations can share this client, so only its owner initializes and shuts down Steamworks.

The client parses Steam's `+connect <steam-id>` launch pair and queues a join event until a listener subscribes. Overlay and shutdown events use the same typed event channel. Native library files are game-owned and must be declared in the game's `nsgo-natives.yaml` according to the SDK redistribution terms.
