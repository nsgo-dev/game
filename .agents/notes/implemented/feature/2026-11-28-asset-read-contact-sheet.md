# Agent Note: Asset read contact sheets

Status: implemented

## Problem

Agents could inspect the viewport but could not compare generated candidates or project images in one bounded response.

## Decision

The read-only `asset_read` MCP tool accepts up to twelve project-relative images, hashes each source, and returns a labeled PNG contact sheet bounded by the requested edge. Paths stay inside the open project and no files are written.

## Alternatives considered

Returning one image per call would make candidate comparison depend on agent bookkeeping. Reusing viewport rendering would require opening a scene and would not read arbitrary project assets, so a dedicated contact-sheet encoder is used.

## Consequences

Image reads are deterministic for unchanged files and provide source hashes for callers to detect changes. Audio WAV preview remains a follow-up extension because the current Result protocol carries PNG media.
