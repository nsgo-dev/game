### Added

- The `puzzle2d` template, Spark Slide: a 2D grid puzzle in which a spark slides until a wall stops it and lights every lamp it passes, with levels in a versioned YAML file, undo and redo through `undo.History`, a move limit, three stars per level, and a level select that unlocks each level after the one before and saves the stars with `save`; it plays with the keyboard, a game controller, touch and the mouse, and its tests play every level to a win and to a loss. `game new -template puzzle2d` or `nsgo init -template github.com/nsgo-dev/game/template/puzzle2d` creates it (#795).
