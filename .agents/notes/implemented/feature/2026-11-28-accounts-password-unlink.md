# Agent Note: Account password changes and identity unlinking

Status: implemented

## Problem

Accounts could link credentials but could not replace an email password or remove an incorrectly linked identity. Store implementations had no atomic identity removal operation, and HTTP clients had no authenticated endpoints for either action.

## Decision

`Store.UnlinkIdentity` removes one provider identity atomically in memory and SQL-backed stores. `Service.ChangePassword` verifies the existing argon2id hash before writing a new hash. `Service.UnlinkIdentity` loads the profile first and refuses to remove the last available sign-in method with `ErrLastCredential`. The authenticated HTTP routes are `POST /v1/password` and `POST /v1/unlink/identity`.

## Alternatives considered

- **Delete identity directly in the service after listing identities**: rejected because concurrent requests could both observe a credential and remove the last one. The store operation keeps deletion atomic, while the service protects the normal path.
- **Accept a new password without the old password**: rejected because this endpoint is bearer-authenticated and a leaked service token must not silently become an account takeover.

## Consequences

Players can recover from a password leak and correct a linked provider without changing the account ID. The last identity-only credential remains protected. Password reset, email verification, mail delivery and session-wide revocation remain deferred to the session-management and mailer work tracked by #1651 and #1683.

## Verification

Service and HTTP tests cover password replacement, old-password rejection, identity unlinking, last-credential refusal and the new routes. Memory, SQLite and Postgres-backed account packages compile and pass their existing tests.
