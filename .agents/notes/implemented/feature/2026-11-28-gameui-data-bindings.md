# Agent Note: Game UI binding metadata

Status: implemented

## Problem

Screen files described widget layout but could not declare which game values drive properties, so each game had to keep an unreviewable mapping in Go.

## Decision

`ScreenNodeDoc.Bind` stores a deterministic map from widget property names to dotted source paths. Parsing rejects empty, whitespace-padded and control-containing keys or paths. Clone makes a defensive map copy so document edits remain isolated.

## Alternatives considered

- **Add one typed field per node property immediately**: rejected because generated binding registration and future widgets would require repeated schema changes.
- **Accept arbitrary values directly in YAML**: rejected because runtime types belong to the game's registered sources and YAML should remain a path contract.

## Consequences

Screen files can review binding intent alongside layout and round-trip deterministically. Runtime signal registration, typed source checking and generated accessors remain follow-up work tied to #1345/#1344/#1346.

## Verification

`TestScreenBindingRoundTrip` parses and re-encodes label text and visibility bindings. Game UI package checks pass.
