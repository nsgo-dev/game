# Agent Note: Steam catalog keys

Status: implemented

## Problem

Steamworks achievements and leaderboards could not be declared in the shared YAML catalogs. Games had to keep store-specific IDs outside the online packages even though the runtime already had Steam presence support.

## Decision

Achievements add a `steam` service and platform ID. Leaderboards add a `steam` provider and board ID with the same strict identifier validation and duplicate checks as existing stores. Existing version-1 files remain valid and unchanged when the fields are omitted.

## Alternatives considered

Keeping Steam IDs in a parallel file would split catalog validation and make migration harder. Bumping the file version would reject existing catalogs unnecessarily because the new field is optional. Both were rejected in favor of optional version-1 keys.

## Consequences

Catalogs can declare Steam IDs now, and future Steamworks adapters can resolve them through the same APIs. Reporting, stats ingestion, and native driver wiring remain separate follow-up work.
