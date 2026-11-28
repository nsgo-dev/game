# Agent Note: Pin MCP write targets

Status: implemented

## Problem

MCP write tools read the live editor selection once to acquire a hold and again to apply a write. A person clicking between those calls could redirect the write to another object.

## Decision

Mutating tools resolve an explicit target immediately. When a target is omitted during an agent turn, they use the turn's captured `SelectionIDs`, then pass the same IDs to both hold acquisition and the editor mutation. Omitted targets outside a turn are refused because live selection is context rather than a write address.

## Alternatives considered

Reading the live selection at mutation time preserves the old convenience behavior, but keeps the race and can silently edit the wrong object. A project-wide revision would over-refuse unrelated edits and belongs to a separate concurrency contract.

## Consequences

Human clicks no longer redirect object writes, and existing explicit-ID calls retain their behavior. Clients making selection-default writes outside a turn must provide IDs, which makes their target auditable.
