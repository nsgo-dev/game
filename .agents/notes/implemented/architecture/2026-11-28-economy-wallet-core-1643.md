# Agent Note: idempotent economy wallet core

Status: implemented

## Problem

Client-owned currency lacked an authoritative operation primitive for grants, spends, exchanges and retry handling.

## Decision

Add `online/economy.Wallet` with atomic grant, spend and exchange operations keyed by caller idempotency keys. A replay returns the original result and concurrent spends cannot overdraw.

## Consequences

A durable records transaction adapter can reuse these operation semantics. Currency definitions, HTTP authorization, purchase verification and persistence remain integration work dependent on server-owned records.

## Alternatives considered

Separate balance checks and writes were rejected because retries and concurrent spends race. Client-side ledgers were rejected because they cannot prevent tampering.
