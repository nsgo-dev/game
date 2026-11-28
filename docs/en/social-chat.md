# Social chat

`social.Chat` provides canonical room, group and DM channel IDs, bounded JSON messages, history paging, sender edits/deletes and a pre-send moderation hook.

`Chat.Join`, `Leave` and `Members` maintain deterministic online membership snapshots. `HistoryBefore` pages older messages by an exclusive message ID. `Retention` with `Sweep` removes expired history, and `Now` makes retention and message timestamps deterministic in tests. Chat state is protected for concurrent send, history and membership calls. Group membership and DM block checks remain service policies until the dependent social services land.
