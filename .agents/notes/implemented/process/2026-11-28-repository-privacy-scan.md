# Agent Note: Repository privacy scan

Status: implemented

## Problem

Tracked fixtures and release inputs had no repository-level guard against private paths, private-key files, or secret-like text.

## Decision

`internal/repocheck` scans text trees using deterministic filename and content rules. Synthetic examples use a versioned YAML exemption file where every entry has an exact path or pattern and a human reason. CI runs the focused scanner on a Depot runner before the broader checks.

## Alternatives considered

An external secret scanner alone would miss private filenames and personal paths. A broad ignore list would hide new secrets beside an approved fixture, so exemptions remain exact and reasoned.

## Consequences

The scanner is intentionally conservative and may require a narrowly scoped exemption for documentation examples. Release archive scanning remains a follow-up integration point because the package formats are assembled by the release workflow.
