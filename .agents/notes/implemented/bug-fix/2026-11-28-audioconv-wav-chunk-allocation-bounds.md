# Agent Note: Bound DecodeWAV allocations to the bytes a WAV stream delivers

Status: implemented

## Problem

`audioconv.DecodeWAV` in `pkg/tooling/audioconv/wav.go` treated chunk sizes as untrusted (`maxWAVChunkSize`) but still allocated from them. The `fmt ` case ran `make([]byte, size)` with only a lower bound, so a 20-byte stream (`RIFF`, `WAVE`, a `fmt ` header declaring `1<<30`, no body) allocated 1 GiB before failing with `fmt chunk: EOF`, and a declared `0xFFFFFFFF` allocated 4 GiB. The `data` case checked `maxWAVChunkSize` first, but then allocated the full declared size, so a 48-byte stream declaring a 256 MiB `data` chunk still allocated 256 MiB. Asset import, the editor's generated-audio check (`decodeUntrustedAudio` in `pkg/editor/asset_gen.go`) and mod content all run this decoder on files nobody vetted, and an allocation that size fails outright on 32-bit and web targets and can get a mobile process killed.

## Decision

Memory in `DecodeWAV` follows the bytes the stream delivers, never the size a header claims:

- **`fmt `**: sizes over `maxWAVChunkSize` are rejected with a size error, like `data`. The decoder reads `min(size, wavExtensibleFmtSize)` bytes (40, the `WAVE_FORMAT_EXTENSIBLE` layout, the longest it reads fields from) into a stack array and discards the remainder with `io.CopyN(io.Discard, ...)`, so a longer `fmt ` chunk with trailing codec bytes still decodes and the chunk walk stays aligned.
- **`data`**: still capped at `maxWAVChunkSize`, then read by `readWAVChunk`, which starts with at most `wavChunkPrealloc` (64 KiB) and doubles its buffer, never past the declared size, only after the stream has filled it. A truncated chunk returns `io.ErrUnexpectedEOF`.
- **`smpl`** was already bounded (at most `smplHeaderSize+64*smplLoopSize` bytes buffered, larger chunks skipped) and unknown chunks are skipped with `io.CopyN`, so neither changes.
- **Testing**: `TestDecodeWAVHostileChunkSizesAllocateLittle` in `pkg/tooling/audioconv/wav_alloc_test.go` measures `runtime.MemStats.TotalAlloc` around header-only streams and requires under 1 MiB; `FuzzDecodeWAV` asserts allocation within 64 times the input plus 4 MiB and that every decoded buffer passes `Validate`, and runs in the fuzz job of `.github/workflows/quality.yml`.

## Alternatives considered

### Only apply `maxWAVChunkSize` to `fmt ` before allocating

This is the one-line change and mirrors the existing `data` check. It still lets a 36-byte file allocate 256 MiB, which is the same failure on a phone or a wasm heap, and it leaves `data` with the same gap, so the cap alone does not close the weakness.

### Reject any `fmt ` chunk longer than 40 bytes (or a small slack such as 256)

Simpler than skipping, and no supported format needs more. Other tools do write longer `fmt ` chunks (codec extra bytes after `cbSize`, padding), and refusing them would break files that decode today, while reading a fixed prefix and skipping the rest costs no memory either way.

### Pre-check the remaining stream length (`Len()`, `Seek`) before allocating `data`

Zero extra copies for `*bytes.Reader` and files, but it does nothing for opaque readers (pipes, archives, HTTP bodies), which then still allocate the declared size. Incremental growth covers every `io.Reader`; its cost is described below.

## Consequences

- **Benefits**: a hostile or truncated header costs at most about 64 KiB of buffer before the decoder fails, on every reader type; valid files decode exactly as before, including `fmt ` chunks longer than 40 bytes.
- **Costs**: a large valid `data` chunk is copied a few times while the buffer doubles (transiently up to about twice its size, and never past the declared size in capacity), where the old code made one exact allocation. A `fmt ` chunk over 256 MiB, which no real file has, is now an error instead of a read.
- **Not changed**: `decodePCMSamples` still allocates one `float64` per sample, up to 8 bytes per input byte for 8-bit PCM; that is proportional to bytes actually read and bounded by `maxWAVChunkSize`, so it is not header-driven.
