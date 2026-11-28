# Agent Note: Input glyph-set format

Status: implemented

## Problem

Button prompts had fixed built-in labels and icon keys, but no reviewable format for platform-specific glyph metadata.

## Decision

`GlyphSet` defines strict version-1 YAML mapping device families and input tokens to `Prompt` labels, glyphs, and icons. Unknown fields, missing version, trailing documents, and empty entries are refused. Runtime `PromptTable` remains the override and fallback mechanism.

## Alternatives considered

Keeping every glyph in Go requires code changes for art metadata. Permissive maps allow misspelled keys to silently produce blank prompts.

## Consequences

Projects can version and review prompt glyph metadata independently from code. Binding-aware prompt resolution and screen prompt nodes remain follow-up integrations.
