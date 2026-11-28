# Agent Note: Template brief provenance

Status: implemented

## Problem

Templates had playtest goals sourced from `brief.yaml` but did not ship a brief, leaving new projects without design intent or provenance for target values. Reports could therefore omit the fact that a brief target had never been measured.

## Decision

Every registered template ships a canonical version 1 `brief.yaml` in both its module and embedded `game new` copy. Brief sections and feel targets accept the additive `basis` values `locked`, `target`, and `assumption`, while explicit edits may record deterministic `changes`. Playtest reports can include `briefTargets`, and `ApplyBriefTargetStatuses` preserves `pending` for targets without measured metrics.

## Alternatives considered

Creating a second verification file would duplicate the playtest report and make status drift likely. Keeping provenance in comments would make it unavailable to the editor and agent context, so it is typed in the existing brief format.

## Consequences

The brief schema and report payload gain optional fields, so old files remain valid. Template repositories carry one additional text file per template and the embedded copies must remain byte-identical. Reports can now distinguish missing evidence from a missed goal.
