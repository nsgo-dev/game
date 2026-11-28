# Agent Note: Hold expiring room seats

Status: implemented

## Problem

Matched players had no way to hold a room seat before connecting, so a full room could admit a different peer while the matched player was still joining (#1691).

## Decision

`Rooms.Reserve` holds a named account seat until a TTL, expired claims are removed lazily, and `Rooms.Claim` converts a live reservation into a member. Reservations count against room capacity and are scoped to one in-memory Rooms manager.

## Alternatives considered

- Reserve by peer ID: rejected because matched clients may not have connected and therefore have no transport peer ID.
- Hold seats indefinitely: rejected because abandoned matches would leak capacity.

## Consequences

Matchmaking integrations can hold seats before transport connection and claim them on arrival. Cluster-wide signed reservations and directories remain extension points for the multi-process deployment layer.
