# Agent Note: OIDC account identity verifier

Status: implemented

## Problem

Games had to hand-roll issuer, audience, expiry and JWKS signature checks for OpenID Connect identity tokens.

## Decision

Add `accounts/identity.OIDC`, which fetches and caches an issuer's JWKS, validates issuer, configured audiences, expiry and subject, and verifies RS256 or ES256 signatures. Unknown keys trigger a cache refresh after the hourly cache expires.

## Consequences

Account services can register one standards-based verifier without accepting tokens issued for another application. Apple, Steam and mini-program exchanges remain provider-specific follow-up integrations.

## Alternatives considered

Accepting opaque credentials and delegating all checks to each game was rejected because it duplicates security-sensitive JWT validation. A third-party JWT dependency was avoided to keep verification explicit and the package small.
