# Agent Note: Social party lifecycle

Status: implemented

## Problem

Matchmaking tickets carried party members but games had no shared lifecycle for groups before and between matches.

## Decision

`social.Parties` owns ephemeral party state with open joins, size limits, deterministic oldest-member leader handover, leave cleanup and pending queue state.

## Alternatives considered

Embedding party fields into matchmaking tickets loses pre-match invites and makes leader handover inconsistent. Persistent storage would outlive the realtime session, so this core keeps parties process-local.

## Consequences

Realtime transports can route party operations and queue one ticket for all members. Cross-process routing and friend notifications remain adapter integrations.
