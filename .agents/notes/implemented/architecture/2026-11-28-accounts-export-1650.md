# Agent Note: account data export

Status: implemented

## Problem

Players had no endpoint to obtain a portable copy of their account profile and linked identities.

## Decision

Add `Service.DataExport` and authenticated `GET /v1/me/export`. The JSON document contains account metadata and provider identities while excluding password hashes, session secrets and tokens.

## Consequences

Clients can present a self-service export action without exposing credentials. Full account deletion and erasure fan-out remain dependent on session-list and eraser contracts from issue #1683.

## Alternatives considered

Returning the internal account record was rejected because it would couple the wire format to storage fields and risk exposing secrets. A server-side archive was rejected because clients need an immediate portable JSON document.
