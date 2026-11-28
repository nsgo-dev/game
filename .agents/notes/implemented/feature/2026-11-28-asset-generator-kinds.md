# Agent Note: Generated asset kinds and provenance

Status: implemented

## Problem

Generated Assets accepted only `images` and `sound` generator servers, and a `.source.yaml` sidecar (`importsettings.AssetSource`) could neither say which media family it describes nor cover related files, such as a model and its material draft (`nsgo-dev/game#1748`).

## Decision

`AssetGeneratorKind` recognizes `images`, `sound`, `textures`, `models`, `animation` and `tiles`, and the `editor.yaml` generator schema enumerates all six, so Settings › Tools can declare every planned generator family now.

Recognizing a kind is separate from generating it. `AssetGeneratorKind.Generatable` is true only for `images` and `sound`, the kinds with a strict in-memory decoder. `GenerateAsset` (through `prepareAssetGenCall`) refuses any other kind before it calls the generator server. Without that gate, a `models` server's output would fall through the decode switch in `executeAssetGenCall`, show as candidates with no validated bytes, and break the rule that untrusted output is decoded and checked before a person sees or picks it.

The sidecar keeps `version: 1` and gains two optional fields, `kind` and `files`, encoded right after `version` in fixed order. `ValidateAssetSource` checks `kind` as a single line and each `files` entry as a non-empty single-line path. `PickGeneratedCandidate` writes the request's kind into the sidecar.

## Alternatives considered

- Bumping the sidecar to version 2: rejected, because both fields are optional and additive, and every existing generated asset must stay releasable without a migration.
- Letting the new kinds generate with no decoder: rejected, because it would surface unvalidated payloads as candidates.
- Keeping the new kinds out of settings until their decoders land: rejected, because the settings schema and sidecar are the format change the later decoders build on, and refusing at generation time gives a clear error instead of a schema failure.
- Encoding related files inside an opaque prompt or note field: rejected, because release and review tooling needs deterministic paths.

## Consequences

Settings and provenance can represent the new generator families and multi-file outputs. Not done yet, and tracked by `nsgo-dev/game#1748`: in-memory validators and previews for textures, models (`pkg/gltf`), animation and tiles; Pick writing more than one file; the credits license scan reading `files`; and the agent-facing MCP tools (`asset_generate`, `asset_candidates`, `asset_vary`).
