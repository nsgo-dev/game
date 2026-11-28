# Agent Note: Localized font character-set acceptance

Status: implemented

## Problem

Font baking already collected localization strings, plural forms, placeholders and extra character files, while asset checking reported missing glyphs. The acceptance boundary needed an explicit record that both paths use the same sorted character set and rebuild only when that set changes.

## Decision

Keep the existing `charsFrom` and `charsFile` format. The asset builder sorts and deduplicates the effective runes for cache keys, and the checker reports missing runes with localization key and source line. Acceptance tests cover markup and placeholder stripping, plural forms, deterministic output, cache reuse and missing-character diagnostics.

## Alternatives considered

Maintaining a separate generated character manifest would introduce another file that can drift from localization tables. Baking the whole font would avoid collection but is too large for web and mini-game targets.

## Consequences

Localized font targets remain compact and update automatically from source tables. A string edit that changes only ordering or placeholder names does not rebuild the font, while new glyphs trigger a deterministic rebuild and checker diagnostics.

## Verification

Existing assetbuild and assetcheck tests cover the acceptance cases. Targeted tests and `go tool nsgo check` pass for both packages.
