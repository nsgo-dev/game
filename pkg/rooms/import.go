package rooms

import (
	"encoding/json"
	"fmt"

	"github.com/nsgo-dev/game/pkg/tilemap"
)

type ldtkRoomsProject struct {
	WorldLayout *string          `json:"worldLayout"`
	Levels      []ldtkRoomsLevel `json:"levels"`
	Worlds      []ldtkRoomsWorld `json:"worlds"`
}

type ldtkRoomsWorld struct {
	Identifier  string           `json:"identifier"`
	WorldLayout *string          `json:"worldLayout"`
	Levels      []ldtkRoomsLevel `json:"levels"`
}

type ldtkRoomsLevel struct {
	Identifier string `json:"identifier"`
	WorldX     int    `json:"worldX"`
	WorldY     int    `json:"worldY"`
	PxWid      int    `json:"pxWid"`
	PxHei      int    `json:"pxHei"`
}

// FromLDtk returns one room per level of an LDtk project (.ldtk JSON), with the level's identifier as the room's ID and its world position and pixel size as the room's rectangle.
//
// An empty world reads the project's top-level levels; a project with LDtk's multi-worlds enabled names the world to read by its identifier. The world's layout must be Free or GridVania, since Linear layouts give levels no world positions. The rooms carry no transitions; put them in a Map, whose Transition applies to every room, and run Validate.
func FromLDtk(data []byte, world string) ([]Room, error) {
	var proj ldtkRoomsProject
	if err := json.Unmarshal(data, &proj); err != nil {
		return nil, fmt.Errorf("rooms: parse LDtk project: %w", err)
	}
	layout, levels := proj.WorldLayout, proj.Levels
	if world != "" || (len(levels) == 0 && len(proj.Worlds) > 0) {
		if world == "" {
			return nil, fmt.Errorf("rooms: LDtk project has %d worlds; name one", len(proj.Worlds))
		}
		found := false
		for i := range proj.Worlds {
			if proj.Worlds[i].Identifier == world {
				layout, levels = proj.Worlds[i].WorldLayout, proj.Worlds[i].Levels
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("rooms: LDtk project has no world %q", world)
		}
	}
	if layout != nil && *layout != "Free" && *layout != "GridVania" {
		return nil, fmt.Errorf("rooms: LDtk world layout %q gives levels no world positions (want Free or GridVania)", *layout)
	}
	if len(levels) == 0 {
		return nil, fmt.Errorf("rooms: LDtk project has no levels")
	}
	out := make([]Room, len(levels))
	for i, lvl := range levels {
		r := Room{
			ID:     lvl.Identifier,
			X:      float32(lvl.WorldX),
			Y:      float32(lvl.WorldY),
			Width:  float32(lvl.PxWid),
			Height: float32(lvl.PxHei),
		}
		if err := validID(r.ID); err != nil {
			return nil, fmt.Errorf("rooms: LDtk level %d identifier: %v", i, err)
		}
		if err := validRect(r.X, r.Y, r.Width, r.Height); err != nil {
			return nil, fmt.Errorf("rooms: LDtk level %q: %v", r.ID, err)
		}
		out[i] = r
	}
	return out, nil
}

// FromTiledLayer returns one room per object of a Tiled object layer, in layer order, with the object's name as the room's ID and its rectangle, moved by the layer's offset (which includes the offsets of the groups holding it), as the room's.
//
// Every object must be an unrotated rectangle (not a tile, point, ellipse, polygon or polyline) with a name and a positive size, so a misdrawn room is reported instead of skipped. The rooms carry no transitions; put them in a Map, whose Transition applies to every room, and run Validate.
func FromTiledLayer(layer *tilemap.ObjectLayer) ([]Room, error) {
	if layer == nil {
		return nil, fmt.Errorf("rooms: nil Tiled object layer")
	}
	out := make([]Room, 0, len(layer.Objects))
	for _, o := range layer.Objects {
		o.X += layer.OffsetX
		o.Y += layer.OffsetY
		if o.Shape != tilemap.ObjectRect || o.GID != 0 {
			return nil, fmt.Errorf("rooms: Tiled object %d (%q) is not a rectangle", o.ID, o.Name)
		}
		if o.Rotation != 0 {
			return nil, fmt.Errorf("rooms: Tiled object %d (%q) is rotated", o.ID, o.Name)
		}
		if err := validID(o.Name); err != nil {
			return nil, fmt.Errorf("rooms: Tiled object %d name: %v", o.ID, err)
		}
		if err := validRect(o.X, o.Y, o.Width, o.Height); err != nil {
			return nil, fmt.Errorf("rooms: Tiled object %d (%q): %v", o.ID, o.Name, err)
		}
		out = append(out, Room{ID: o.Name, X: o.X, Y: o.Y, Width: o.Width, Height: o.Height})
	}
	return out, nil
}
