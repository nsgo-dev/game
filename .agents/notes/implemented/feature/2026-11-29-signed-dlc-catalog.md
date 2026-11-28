# Agent Note: Signed DLC catalog metadata

Status: implemented

## Problem

Pack signatures covered only archive digests, leaving a downloaded manifest free to redirect a valid archive to another ID or version.

## Decision

`assets.Catalog` signs canonical JSON with `Signature` cleared. `VerifyCatalog` checks Ed25519 over serial and every pack field, so metadata and digest are authenticated together.

## Alternatives considered

- **Keep signing each pack digest only**: preserves the current format but leaves catalog identity and version mutable.
- **Sign non-canonical JSON bytes from callers**: rejected because whitespace and key order would make equivalent catalogs verify differently.

## Consequences

Catalog metadata cannot be altered without the private key. Fetching, serial rollback storage, update-size reporting and cache pruning remain follow-up work.

## Verification

`TestSignedCatalogCoversPackMetadata` verifies a catalog and rejects a changed pack version. Assets package checks pass.
