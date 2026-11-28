# Account client sessions

The liveops account client stores the long-lived session through a caller-provided secure store, refreshes audience tokens with a single-flight lock, and exposes account registration, linking, profile and sign-out operations. `NewAccountForScope` binds it to NSGo's keychain API.
