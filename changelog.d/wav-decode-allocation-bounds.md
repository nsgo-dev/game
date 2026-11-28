### Security

- `game/pkg/tooling/audioconv`: `DecodeWAV` and `ReadWAVFile` no longer allocate the size a WAV file's `fmt ` or `data` chunk header declares before reading it. The decoder reads only the 40 bytes of `fmt ` it uses and skips the rest, rejects a `fmt ` chunk over 256 MiB as it already rejected such a `data` chunk, and grows the `data` buffer with the bytes that arrive, so a corrupt or hostile WAV file in a project, asset pack or mod fails with an error instead of allocating up to 4 GiB. Valid files decode as before; update to pick up the fix (GHSA-hj3c-wjj6-66f6).
