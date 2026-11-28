# Agent Note: Broadcast to deterministic peer groups

Status: implemented

## Problem

Games repeatedly looped over room members to send one payload, losing join order and the identity of failed peers while multiplying transport lookups (#1697).

## Decision

`PeerGroup` maintains an ordered set with Add, Remove, Members, and Broadcast. `SendMany` provides a transport-independent fallback that reports every failed peer in input order, while transports may implement `Multicaster` for a batched path.

## Alternatives considered

- Use a map keyed by peer ID: rejected because map iteration would make delivery and failure order nondeterministic.
- Return only the first error: rejected because callers need to remove every peer that failed from room or team membership.

## Consequences

Room and team code can maintain lifecycle-aware groups and exclude a sender without rebuilding membership logic. The fallback preserves compatibility with existing transports, while native transports can add a batched implementation later.
