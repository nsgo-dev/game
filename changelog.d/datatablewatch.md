### Changed

- `game/datatable`: `Table.Watch` and `Table.WatchWithError` moved to `game/datatable/datatablewatch` as `datatablewatch.Watch` and `datatablewatch.WatchWithError`, so importing the data-table package no longer pulls in NSGo and a dedicated server loads tables headlessly (#1315).
