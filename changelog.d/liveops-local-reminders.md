### Added

- `pkg/online/liveops`: add local re-engagement reminders (`Reminders`, `RemindersConfig`, `QuietHours`, `ParseReminders`, `LoadRemindersFS`, `EncodeReminders`) over NSGo's `core.Scope.ShowNotification`, with versioned YAML definitions and JSON Schema (`reminders.v1.json`), lazy notification permission on first `Schedule` or explicit `RequestPermission`, persisted player toggle and quiet hours, foreground cancel/reschedule/keep reconciliation, and `CanScheduleNotifications` reporting (#1310).
