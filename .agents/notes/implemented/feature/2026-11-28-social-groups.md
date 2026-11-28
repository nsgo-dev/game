# Agent Note: Enforce social group roles in one state machine

Status: implemented

## Problem

Online services had no persistent group model with centralized role, ban, owner, and capacity rules (#1685).

## Decision

`social.Groups` provides an in-memory group store with owner/admin/member/requested/banned roles, open and approval-only joins, member caps, last-owner protection, and deterministic case-insensitive prefix search.

## Alternatives considered

- Put role checks in HTTP handlers: rejected because server-side callers and handlers would diverge.
- Use unordered member maps for search: rejected because result order must be deterministic for paging and tests.

## Consequences

The role model is available to server code before SQL and HTTP adapters land. Group state is process-local and should be replaced by a transactional store for multi-process deployments.
