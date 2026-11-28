# Agent Note: Publish immutable datatable views

Status: implemented

## Problem

`Table.Rows` copied rows on every call and exposed its version separately. A reload between those calls could make a reader pair rows from one generation with a version from another, while a match had no zero-copy way to pin the data it used (#1700).

## Decision

`Table.View` captures the current row slice and version while holding the table read lock. `View.Rows` returns the shared slice as read-only and `View.Version` identifies the same publication. Reload swaps the slice and increments the version while holding the write lock, so an existing view remains stable.

## Alternatives considered

- Remove copying from `Table.Rows`: rejected because it would silently change the existing mutable-slice contract.
- Return rows and version as separate values: rejected because callers could still pair values from different reads.

## Consequences

Match-like readers can retain a stable generation without per-read allocation. Callers must not modify `View.Rows`, and existing `Rows` callers retain copy semantics.
