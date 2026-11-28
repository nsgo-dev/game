# Agent Note: Social chat channels

Status: implemented

## Problem

Social messages had no channel IDs, bounded content, history, edit/delete policy or pre-send moderation hook.

## Decision

`social.Chat` stores channel-scoped messages with canonical room/group/DM IDs, a configurable content byte limit, history paging, sender-authorized edits/deletes and a `BeforeSend` hook.

## Alternatives considered

Reusing match packets would lose history and make moderation impossible outside a match. A transport-specific implementation would prevent HTTP history and SQL adapters.

## Consequences

The in-memory core provides message semantics for realtime and persistence adapters. Retention, membership authorization and block checks remain service policies.
