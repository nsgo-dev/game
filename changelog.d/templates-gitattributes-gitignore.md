### Fixed

- `template`, `cmd/game`, `refs`: added `* text=auto eol=lf` and `binary` media rules to every template's `.gitattributes` (and `game merge -setup`), added `.game/` to every template's `.gitignore`, normalized CRLF to LF when hashing text files for `game.index.yaml`, and added a Windows CRLF-checkout template CI job (#1534).
