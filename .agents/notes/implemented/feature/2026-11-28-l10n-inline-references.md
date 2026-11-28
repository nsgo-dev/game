# Agent Note: Inline localization key references

Status: implemented

## Problem

Localized sentences could interpolate arguments and select one plural entry, but could not reuse a translated item or adjective inside another message. Splitting sentences into fragments loses language-specific word order.

## Decision

`Catalog.T` and `Catalog.Plural` expand `{@key}` after named interpolation. References use the locale that supplied the enclosing entry, follow normal fallback lookup, preserve missing and cyclic references literally, and share a 128-expansion budget. Escaped braces remain ordinary literal text.

## Alternatives considered

- **Copy translated fragments at every call site**: preserves the old API but duplicates translations and breaks word order for languages that reorder phrases.
- **Evaluate references while holding the catalog lock**: rejected because nested lookup and interpolation would retain a read lock recursively and complicate future provider-backed catalogs.

## Consequences

String tables can reuse localized entries while existing `T` and `Plural` callers remain source-compatible. The current syntax handles references and cycles, while ICU inline plural/select parsing and editor preview remain follow-up work tied to #1424 and the broader message-format design.

## Verification

`pkg/l10n/references_test.go` covers nested references, locale fallback, escaped braces, missing references, cycles and the expansion limit. Focused l10n tests and `go tool nsgo check ./pkg/l10n` pass.
