### Security

- `pkg/modding`: mods without `CapStorage` now run with `tsx.Sandbox.DisableStorage` enabled instead of a one-byte partition quota, so they can no longer read, write, or delete entries through the `"nsgo"` module's `Storage` (#1267).
