# Agent Note: Asset delivery placement

Status: implemented

## Problem

Export presets could declare delivery groups, but package layout consumers had no shared deterministic resolver for main, subpackage, and remote destinations.

## Decision

`AssetDelivery.PlaceGroups` resolves named groups to stable placement records, keeps unlisted groups in the main package, and sorts results by group name. Existing strict validation continues to reject duplicate placements and negative caps.

## Alternatives considered

Leaving layout resolution to each platform exporter would duplicate placement rules and produce different package layouts. Returning map iteration order would make generated output unstable.

## Consequences

Exporters can share one deterministic group-to-package mapping and report cap failures consistently. Platform-specific package writing and remote-origin uploading remain follow-up integrations.
