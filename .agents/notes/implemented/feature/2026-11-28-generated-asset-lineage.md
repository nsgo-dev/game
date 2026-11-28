# Agent Note: Generated asset hash and lineage

Status: implemented

## Problem

Generated asset sidecars recorded prompts and licenses but could not prove which bytes were picked, preserve request parameters, or identify a parent asset used for variation and refinement.

## Decision

`importsettings.AssetSource` accepts optional `sha256`, `parameters`, `derivedFrom`, and `referenceHashes` fields while retaining version 1 compatibility. The editor computes the picked byte digest and records stable request parameters and a reference asset when writing the sidecar.

## Alternatives considered

Bumping the sidecar version immediately would force migration of existing user files and conflict with the parallel schema work. Optional fields preserve old sidecars while allowing new picks to carry lineage.

## Consequences

Existing source files remain valid and round-trip unchanged. New picks expose enough data for later edited-after-generation and disclosure checks, while old generated files have no hash until picked again.
