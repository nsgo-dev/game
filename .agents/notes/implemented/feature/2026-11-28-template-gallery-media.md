# Agent Note: Template gallery media metadata

Status: implemented

## Problem

Template cards on the editor Start Screen and website showcase had no shared place for deterministic cover or preview media metadata, so gallery visuals could not be generated or lazy-loaded consistently.

## Decision

`editor.ProjectTemplate` carries optional `Cover` and `Preview` URLs. The game CLI registry supplies the deterministic gallery cover path, the Start Screen renders the cover, and website `ShowcaseEntry` accepts cover and preview fields. Website previews remain lazy media hooks with `preload="none"` and only become visible on hover or focus.

## Alternatives considered

Embedding binary screenshots in the Go package would increase the editor binary and bypass the capture plan. Generating a second website-only metadata format would allow the two galleries to drift, so both surfaces use the same field shape even when a fallback cover is used.

## Consequences

Existing templates remain compatible because the fields are optional and the registry supplies a deterministic fallback cover. Actual gameplay capture and video encoding remain owned by the capture-plan tooling, while the UI contract is ready to consume generated assets.
