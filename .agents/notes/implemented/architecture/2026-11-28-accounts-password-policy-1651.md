# Agent Note: client password policy validation

Status: implemented

## Problem

Clients had to duplicate password length checks before sending account forms, and the package exposed only hashing APIs.

## Decision

Add `ValidatePassword(password, min)` to apply the configured minimum and package maximum without performing a costly Argon2 hash.

## Consequences

Forms can fail fast and consistently before network requests. Password reset, email verification, unlinking, and session revocation remain dependent on the separate mailer and session-management work in issue #1651.

## Alternatives considered

Hashing to validate a form was rejected because it wastes CPU and does not provide the service's policy error before a request is sent.
