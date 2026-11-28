# Agent Note: WeChat mini-game payment boundary

Status: implemented

## Problem

WeChat mini-game virtual payment requires server-created signatures and retried delivery notifications. Trusting client success callbacks or delivering each retry can grant paid goods incorrectly.

## Decision

`storeverify/wechatpay` signs orders with HMAC-SHA256 and provides a `DeliveryStore` keyed by `OutTradeNo`. A handler verifies environment and signature before calling `Deliver`, then records the result so retries are idempotent.

## Alternatives considered

Signing orders in the client was rejected because the AppKey must never leave the server. Using an in-memory boolean without a store interface was rejected because process restarts and concurrent retries need durable idempotency.

## Consequences

Games can plug the boundary into memory, SQLite or Postgres stores without coupling the payment trust rules to NSGo core. HTTP AES safe-mode transport and platform-specific query APIs remain follow-up work.
