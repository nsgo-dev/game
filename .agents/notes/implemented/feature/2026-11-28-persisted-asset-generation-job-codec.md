# Agent Note: Persisted asset-generation job codec

Status: implemented

## Problem

Asset generation needs a durable request record before the editor can recover paid work across restarts. The record must reject unknown fields and unsafe candidate paths while remaining stable for review and hashing.

## Decision

`pkg/tooling/genjob` defines version 1 job and candidate records. `Parse` uses strict YAML decoding, duplicate-key and trailing-document checks, and validates lifecycle fields, request IDs, fingerprints, hashes, and candidate file names. `Encode` sorts parameter keys and candidates before emitting two-space YAML. The package is independent of the editor runtime so job loading can be reused by recovery and checks.

## Alternatives considered

Keeping records as an editor-private struct would couple the on-disk format to UI code. Permissive decoding would allow typos and unknown fields to survive a restart. Both were rejected in favor of a small strict package.

## Consequences

The durable schema has a stable foundation for job persistence, candidate integrity checks, and restart recovery. This slice does not yet connect the codec to the editor's generation calls or recovery rail.
