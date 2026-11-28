# Agent Note: durable analytics batch provider

Status: implemented

## Problem

Analytics providers received one event synchronously and in-memory queues lost events when mobile processes exited.

## Decision

Add `BatchProvider`, which appends events to a bounded JSON Lines file, flushes through a caller-supplied sink, leaves failed batches intact, and purges on consent revocation.

## Consequences

Apps can perform offline durable delivery without blocking `Track` on network I/O. Install/session context, retries with backoff and collector services remain follow-up integrations.

## Alternatives considered

An in-memory channel was rejected because process termination loses events. Clearing the queue before sink success was rejected because transient failures would cause silent data loss.
