### Added

- `game/pkg/modding`: hosts can register custom TSX modules (`ModModule`, `ManagerOptions.Modules`, `RegisterModule`) gated by dotted host capabilities (`HostCapability`, `ManagerOptions.Capabilities`), resolve the calling mod with `Caller`, enforce per-call checks with `ModAPI.RequireHost`, narrow grants with `ManagerOptions.GrantHost` and `Manager.SetHostGrant`, emit TypeScript declarations with `WriteModAPIDTS`, and declare dotted host capability names in `mod.yaml` (`ModManifest.HostCapabilities`, `mod.v1.json`) (#1284).
