# Agent Note: Mini-program publishing destinations

Status: implemented

## Problem

Release review supported desktop, Google Play, and TestFlight destinations, while mini-program builds were left as manual artifacts. There was no shared validation for provider identity, upload-key presence, source-IP allowlists, or phone preview links.

## Decision

The editor treats WeChat and Douyin as explicit `PublishDestination` values selected from `miniprogram_wechat` and `miniprogram_douyin` presets. It builds versioned `nsgo publish miniprogram` arguments and passes credentials through a dedicated environment variable. `ValidateMiniProgramPreflight` checks provider, AppID, version, key length, and an optional parsed `netip` allowlist. Preview QR generation reuses the deterministic local QR encoder and carries only provider, AppID, and version.

## Alternatives considered

Keeping mini-programs manual avoids provider-specific code, but leaves release review unable to detect missing credentials or unsafe source addresses. Sending credentials in project YAML would make them portable and leakable, so the system keychain and process environment remain the boundary.

## Consequences

The repository can prepare and validate uploads without requiring WeChat or Douyin developer tools on CI. Actual provider upload behavior remains delegated to the installed NSGo/platform tool and requires a person-confirmed review session. Tests cover deterministic preview QR output and all preflight rejection paths.
