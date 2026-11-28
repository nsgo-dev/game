# Agent Note: Steam catalog identifiers

Status: implemented

## Problem

The shared achievement and leaderboard catalogs supported Game Center and Play Games but had no stable Steam identifier fields.

## Decision

Steam is a first-class service/provider in both catalog formats. IDs use the existing validation and deterministic YAML encoding rules, while the runtime Steamworks adapter remains responsible for delivery.

## Alternatives considered

Keeping Steam IDs in `Custom` fields would avoid changing the schema, but would prevent shared validation and catalog tooling from discovering them.

## Consequences

Catalog authors can declare Steam names once and share them with the Steamworks adapter. Existing catalogs remain compatible because the fields are optional.
