package editor

import (
	"errors"
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// Playing reports whether Play mode is currently active.
func (e *Editor) Playing() bool {
	return e != nil && e.playing
}

// PlayScene returns the active runtime BuiltScene while playing, or nil when stopped.
func (e *Editor) PlayScene() *sceneformat.BuiltScene {
	if e == nil || !e.playing {
		return nil
	}
	return e.playScene
}

// PlaySteps returns the number of fixed simulation updates executed in the current or last Play session.
func (e *Editor) PlaySteps() int {
	if e == nil {
		return 0
	}
	return e.playSteps
}

// PlayFrames returns the number of draw frames executed in the current or last Play session.
func (e *Editor) PlayFrames() int {
	if e == nil {
		return 0
	}
	return e.playFrames
}

// Play snapshots the current SceneDoc and starts a live game.Loop on the editor's attached core.Scope.
// Call EditorView(cx, ed) or AttachScope(cx) beforehand, or call PlayIn(cx) with an explicit scope.
func (e *Editor) Play() error {
	return e.PlayIn(e.scope)
}

// PlayIn snapshots the current SceneDoc (as deterministic YAML), builds an isolated runtime copy,
// and starts a live game.Loop on cx calling OnPlayUpdate and OnPlayDraw.
// Calling Stop stops the loop and restores the exact pre-play SceneDoc snapshot.
func (e *Editor) PlayIn(cx *core.Scope) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return nil
	}
	if cx == nil {
		return errors.New("editor: play requires a core.Scope (mount EditorView or pass cx to PlayIn)")
	}
	if e.Doc == nil {
		return errors.New("editor: cannot play nil scene document")
	}

	snap, err := sceneformat.Encode(e.Doc)
	if err != nil {
		return fmt.Errorf("editor: snapshot scene: %w", err)
	}
	playDoc, err := sceneformat.Parse(snap)
	if err != nil {
		return fmt.Errorf("editor: clone play scene: %w", err)
	}
	playBuilt, err := sceneformat.Build(playDoc, e.Resolver)
	if err != nil {
		return fmt.Errorf("editor: build play scene: %w", err)
	}

	e.playSnapshot = snap
	e.Doc = playDoc
	e.Built = playBuilt
	e.playScene = playBuilt
	e.playSteps = 0
	e.playFrames = 0
	e.playing = true
	e.Status = "Playing"

	loop := &game.Loop{
		Rate: e.PlayRate,
		Update: func(dt time.Duration) {
			if !e.playing {
				return
			}
			e.playSteps++
			if e.OnPlayUpdate != nil {
				e.OnPlayUpdate(e.playScene, dt)
			}
			e.notify()
		},
		Draw: func(alpha float64) {
			if !e.playing {
				return
			}
			e.playFrames++
			if e.OnPlayDraw != nil {
				e.OnPlayDraw(e.playScene, alpha)
			}
			e.notify()
		},
	}
	cx.Run(func() {
		e.playStop = loop.Run(cx)
	})
	e.notify()
	return nil
}

// Stop halts the running Play loop and restores the exact pre-play SceneDoc snapshot,
// discarding any runtime mutations made during Play mode.
func (e *Editor) Stop() error {
	if e == nil || !e.playing {
		return nil
	}
	if e.playStop != nil {
		e.playStop()
		e.playStop = nil
	}
	e.playing = false
	e.playScene = nil

	snap := e.playSnapshot
	e.playSnapshot = nil

	if len(snap) > 0 {
		restored, err := sceneformat.Parse(snap)
		if err != nil {
			return fmt.Errorf("editor: restore pre-play snapshot: %w", err)
		}
		e.Doc = restored
	}
	e.Rebuild()
	e.Status = "Stopped"
	e.notify()
	return nil
}
