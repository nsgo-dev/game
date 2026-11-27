package editor

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// SetLayerParallax sets the horizontal and vertical parallax scroll factors of layerName (or ActiveLayer when empty) as an undoable command. Both factors must be finite. Setting a non-default parallax factor on a version 1 or version 2 scene raises the scene to sceneformat.CurrentVersion, which Undo restores. In a scene that inherits another, editing an inherited layer records the change under SceneDoc.LayerOverrides.
func (e *Editor) SetLayerParallax(layerName string, parallaxX, parallaxY float32) error {
	if e.playing {
		return ErrPlaying
	}
	if !isFiniteFloat32(parallaxX) || !isFiniteFloat32(parallaxY) {
		return errors.New("editor: layer parallax must be finite")
	}
	if layerName == "" {
		layerName = e.ActiveLayer
	}
	if e.inheritedLayers()[layerName] {
		e.endTileStrokeForChange()
		return e.editLayerOverride(layerName, "Set parallax of "+layerName, 3, func(lo *sceneformat.LayerOverride, eff, _ *sceneformat.LayerDoc) error {
			if parallaxX != eff.EffectiveParallaxX() {
				lo.ParallaxX = sceneformat.Float32(parallaxX)
			}
			if parallaxY != eff.EffectiveParallaxY() {
				lo.ParallaxY = sceneformat.Float32(parallaxY)
			}
			return nil
		})
	}
	li, err := e.resolveLayerIndex(layerName)
	if err != nil {
		return err
	}
	l := &e.Doc.Layers[li]
	var newPX, newPY *float32
	if l.ParallaxX != nil || parallaxX != 1 {
		newPX = sceneformat.Float32(parallaxX)
	}
	if l.ParallaxY != nil || parallaxY != 1 {
		newPY = sceneformat.Float32(parallaxY)
	}
	if equalFloat32Ptr(l.ParallaxX, newPX) && equalFloat32Ptr(l.ParallaxY, newPY) {
		return nil
	}
	e.endTileStrokeForChange()
	e.History.Execute(&layerParallaxCmd{
		ed:          e,
		layerName:   l.Name,
		oldPX:       cloneFloat32Ptr(l.ParallaxX),
		oldPY:       cloneFloat32Ptr(l.ParallaxY),
		newPX:       newPX,
		newPY:       newPY,
		prevVersion: e.Doc.Version,
	})
	return nil
}

// SetLayerRepeat sets the repeat flags and periods of layerName (or ActiveLayer when empty) as an undoable command. RepeatWidth and RepeatHeight must be finite and non-negative (0 uses the layer's tilemap pixel size or the scene's virtual size). Enabling repeat or setting a positive period on a version 1 or version 2 scene raises the scene to sceneformat.CurrentVersion, which Undo restores. In a scene that inherits another, editing an inherited layer records the change under SceneDoc.LayerOverrides.
func (e *Editor) SetLayerRepeat(layerName string, repeatX, repeatY bool, repeatWidth, repeatHeight float32) error {
	if e.playing {
		return ErrPlaying
	}
	if !isFiniteFloat32(repeatWidth) || repeatWidth < 0 {
		return fmt.Errorf("editor: repeatWidth %v must be finite and non-negative", repeatWidth)
	}
	if !isFiniteFloat32(repeatHeight) || repeatHeight < 0 {
		return fmt.Errorf("editor: repeatHeight %v must be finite and non-negative", repeatHeight)
	}
	if layerName == "" {
		layerName = e.ActiveLayer
	}
	if e.inheritedLayers()[layerName] {
		e.endTileStrokeForChange()
		return e.editLayerOverride(layerName, "Set repeat of "+layerName, 3, func(lo *sceneformat.LayerOverride, eff, _ *sceneformat.LayerDoc) error {
			if repeatX != eff.RepeatX {
				lo.RepeatX = sceneformat.Bool(repeatX)
			}
			if repeatY != eff.RepeatY {
				lo.RepeatY = sceneformat.Bool(repeatY)
			}
			if repeatWidth != eff.RepeatWidth {
				lo.RepeatWidth = sceneformat.Float32(repeatWidth)
			}
			if repeatHeight != eff.RepeatHeight {
				lo.RepeatHeight = sceneformat.Float32(repeatHeight)
			}
			return nil
		})
	}
	li, err := e.resolveLayerIndex(layerName)
	if err != nil {
		return err
	}
	l := &e.Doc.Layers[li]
	if l.RepeatX == repeatX && l.RepeatY == repeatY && l.RepeatWidth == repeatWidth && l.RepeatHeight == repeatHeight {
		return nil
	}
	e.endTileStrokeForChange()
	e.History.Execute(&layerRepeatCmd{
		ed:          e,
		layerName:   l.Name,
		oldRX:       l.RepeatX,
		oldRY:       l.RepeatY,
		oldRW:       l.RepeatWidth,
		oldRH:       l.RepeatHeight,
		newRX:       repeatX,
		newRY:       repeatY,
		newRW:       repeatWidth,
		newRH:       repeatHeight,
		prevVersion: e.Doc.Version,
	})
	return nil
}

type layerParallaxCmd struct {
	ed           *Editor
	layerName    string
	oldPX, oldPY *float32
	newPX, newPY *float32
	prevVersion  int
}

func (c *layerParallaxCmd) Label() string { return "Set parallax of " + c.layerName }

func (c *layerParallaxCmd) Do() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	if c.ed.Doc.Version < sceneformat.CurrentVersion && (c.newPX != nil || c.newPY != nil) {
		c.ed.Doc.Version = sceneformat.CurrentVersion
	}
	l := &c.ed.Doc.Layers[li]
	l.ParallaxX = cloneFloat32Ptr(c.newPX)
	l.ParallaxY = cloneFloat32Ptr(c.newPY)
	c.ed.Status = "Updated parallax of " + c.layerName
}

func (c *layerParallaxCmd) Undo() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	l := &c.ed.Doc.Layers[li]
	l.ParallaxX = cloneFloat32Ptr(c.oldPX)
	l.ParallaxY = cloneFloat32Ptr(c.oldPY)
	c.ed.Doc.Version = c.prevVersion
}

type layerRepeatCmd struct {
	ed           *Editor
	layerName    string
	oldRX, oldRY bool
	oldRW, oldRH float32
	newRX, newRY bool
	newRW, newRH float32
	prevVersion  int
}

func (c *layerRepeatCmd) Label() string { return "Set repeat of " + c.layerName }

func (c *layerRepeatCmd) Do() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	if c.ed.Doc.Version < sceneformat.CurrentVersion && (c.newRX || c.newRY || c.newRW > 0 || c.newRH > 0) {
		c.ed.Doc.Version = sceneformat.CurrentVersion
	}
	l := &c.ed.Doc.Layers[li]
	l.RepeatX = c.newRX
	l.RepeatY = c.newRY
	l.RepeatWidth = c.newRW
	l.RepeatHeight = c.newRH
	c.ed.Status = "Updated repeat of " + c.layerName
}

func (c *layerRepeatCmd) Undo() {
	li := c.ed.layerIndex(c.layerName)
	if li < 0 {
		return
	}
	l := &c.ed.Doc.Layers[li]
	l.RepeatX = c.oldRX
	l.RepeatY = c.oldRY
	l.RepeatWidth = c.oldRW
	l.RepeatHeight = c.oldRH
	c.ed.Doc.Version = c.prevVersion
}
