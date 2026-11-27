package editormcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/tilemap"
)

// The names of the tileset tools added to the editor's MCP server.
const (
	// ToolTilesetRead reads a *.tileset.yaml tileset document (or the tileset open in the editor) with its tile collision, animation, terrain, variant weight, and custom data fields.
	ToolTilesetRead = "tileset_read"
	// ToolTilesetEdit edits a *.tileset.yaml tileset through the editor's undoable tileset commands (flags, collision shapes, animation, custom data, name, weight, and terrain rules).
	ToolTilesetEdit = "tileset_edit"
)

const tilesetReadSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "file": {"type": "string", "description": "Slash-separated path from the project root ending in .tileset.yaml; empty reads the tileset currently open in the editor"}
  }
}`

const tilesetEditSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["ops"],
  "properties": {
    "file": {"type": "string", "description": "Slash-separated path of a *.tileset.yaml file inside the open project; empty edits the tileset open in the editor"},
    "save": {"type": "boolean", "description": "Save the tileset to disk after applying all edits (default false, leaving edits unsaved and undoable in the editor)"},
    "ops": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["op"],
        "properties": {
          "op": {"type": "string", "enum": ["set_tile", "set_property", "remove_property", "add_terrain", "remove_terrain"]},
          "id": {"type": "integer", "minimum": 0},
          "name": {"type": "string"},
          "solid": {"type": "boolean"},
          "oneWay": {"type": "boolean"},
          "flags": {"type": "integer", "minimum": 0},
          "weight": {"type": "number", "minimum": 0},
          "clearWeight": {"type": "boolean"},
          "shapes": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["kind"],
              "properties": {
                "kind": {"type": "string", "enum": ["rect", "ellipse", "polygon", "polyline"]},
                "name": {"type": "string"},
                "type": {"type": "string"},
                "x": {"type": "number"},
                "y": {"type": "number"},
                "width": {"type": "number", "minimum": 0},
                "height": {"type": "number", "minimum": 0},
                "points": {"type": "array", "items": {"type": "array", "minItems": 2, "maxItems": 2, "items": {"type": "number"}}},
                "properties": {"type": "object"}
              }
            }
          },
          "animation": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["tile", "duration"],
              "properties": {
                "tile": {"type": "integer", "minimum": 0},
                "duration": {"type": "string"}
              }
            }
          },
          "terrain": {"type": "string"},
          "neighbors": {"type": "array", "items": {"type": "string"}},
          "mode": {"type": "string", "enum": ["cardinal", "blob"]},
          "key": {"type": "string"},
          "value": {}
        }
      }
    }
  }
}`

// TilesetReadResult is the structured result of tileset_read.
type TilesetReadResult struct {
	File       string               `json:"file"`
	Source     string               `json:"source"`
	Unsaved    bool                 `json:"unsaved"`
	Name       string               `json:"name,omitempty"`
	TileWidth  int                  `json:"tileWidth"`
	TileHeight int                  `json:"tileHeight"`
	Image      string               `json:"image,omitempty"`
	Atlas      string               `json:"atlas,omitempty"`
	TileCount  int                  `json:"tileCount"`
	Terrains   []tilemap.TerrainDoc `json:"terrains,omitempty"`
	Tiles      []TilesetTileView    `json:"tiles,omitempty"`
	YAML       string               `json:"yaml"`
}

// TilesetTileView is one tile's fields returned by tileset_read.
type TilesetTileView struct {
	ID         int                    `json:"id"`
	Name       string                 `json:"name,omitempty"`
	Solid      bool                   `json:"solid,omitempty"`
	OneWay     bool                   `json:"oneWay,omitempty"`
	Flags      tilemap.TileFlags      `json:"flags,omitempty"`
	Weight     *float64               `json:"weight,omitempty"`
	Properties map[string]any         `json:"properties,omitempty"`
	Shapes     []tilemap.TileShapeDoc `json:"shapes,omitempty"`
	Animation  []TilesetAnimFrameView `json:"animation,omitempty"`
	Terrain    string                 `json:"terrain,omitempty"`
	Neighbors  []string               `json:"neighbors,omitempty"`
}

// TilesetAnimFrameView is one animation frame in JSON form.
type TilesetAnimFrameView struct {
	Tile     int    `json:"tile"`
	Duration string `json:"duration"`
}

// TilesetEditResult is the structured result of tileset_edit.
type TilesetEditResult struct {
	File    string `json:"file"`
	Applied int    `json:"applied"`
	Saved   bool   `json:"saved"`
	Unsaved bool   `json:"unsaved"`
	Tiles   int    `json:"tiles"`
}

type tilesetReadArgs struct {
	File string `json:"file"`
}

type tilesetEditArgs struct {
	File string          `json:"file"`
	Save bool            `json:"save"`
	Ops  []tilesetEditOp `json:"ops"`
}

type tilesetEditOp struct {
	Op          string                  `json:"op"`
	ID          *int                    `json:"id"`
	Name        *string                 `json:"name"`
	Solid       *bool                   `json:"solid"`
	OneWay      *bool                   `json:"oneWay"`
	Flags       *uint32                 `json:"flags"`
	Weight      *float64                `json:"weight"`
	ClearWeight bool                    `json:"clearWeight"`
	Shapes      *[]tilesetShapeArg      `json:"shapes"`
	Animation   *[]TilesetAnimFrameView `json:"animation"`
	Terrain     *string                 `json:"terrain"`
	Neighbors   *[]string               `json:"neighbors"`
	Mode        string                  `json:"mode"`
	Key         string                  `json:"key"`
	Value       json.RawMessage         `json:"value"`
}

type tilesetShapeArg struct {
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	X          float32         `json:"x"`
	Y          float32         `json:"y"`
	Width      float32         `json:"width"`
	Height     float32         `json:"height"`
	Points     [][]float32     `json:"points"`
	Properties json.RawMessage `json:"properties"`
}

func tilesetRead(ctx context.Context, c *Call) (*Result, error) {
	var args tilesetReadArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("tileset_read: %w", err)
	}
	var (
		root   string
		rel    string
		doc    *tilemap.TilesetDoc
		raw    []byte
		source string
		dirty  bool
	)
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		root = projectRoot(ed)
		if args.File == "" {
			if ed.TilesetOpen() {
				doc = ed.TilesetDoc()
				rel = ed.TilesetPath()
				dirty = ed.TilesetDirty()
				source = "editor"
				var err error
				raw, err = tilemap.EncodeTileset(doc)
				return err
			}
			activePath := ed.ActiveLayerTilesetPath()
			if activePath == "" {
				return errors.New("tileset_read: no tileset is open in the editor and the active layer has no tilesetFile; pass file or open a *.tileset.yaml first")
			}
			clean, err := cleanProjectPath(activePath)
			if err != nil {
				return fmt.Errorf("tileset_read: %w", err)
			}
			rel = clean
			if root == "" {
				return fmt.Errorf("tileset_read: %s: no project is open in the editor", clean)
			}
			return nil
		}
		clean, err := cleanProjectPath(args.File)
		if err != nil {
			return fmt.Errorf("tileset_read: %w", err)
		}
		rel = clean
		if ed.TilesetOpen() && ed.TilesetPath() == clean && ed.TilesetDirty() {
			doc = ed.TilesetDoc()
			dirty = true
			source = "editor"
			raw, err = tilemap.EncodeTileset(doc)
			return err
		}
		if root == "" {
			return fmt.Errorf("tileset_read: %s: no project is open in the editor", clean)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if doc == nil {
		data, err := readProjectFile(root, rel)
		if err != nil {
			return nil, fmt.Errorf("tileset_read: %w", err)
		}
		parsed, err := tilemap.ParseTileset(data)
		if err != nil {
			return nil, fmt.Errorf("tileset_read: %s: %w", rel, err)
		}
		doc = parsed
		raw = data
		source = "disk"
	}

	tiles := make([]TilesetTileView, 0, len(doc.Tiles))
	for _, t := range doc.Tiles {
		var anim []TilesetAnimFrameView
		for _, f := range t.Animation {
			anim = append(anim, TilesetAnimFrameView{Tile: f.Tile, Duration: f.Duration.String()})
		}
		tiles = append(tiles, TilesetTileView{
			ID:         t.ID,
			Name:       t.Name,
			Solid:      t.Solid,
			OneWay:     t.OneWay,
			Flags:      t.Flags,
			Weight:     t.Weight,
			Properties: t.Properties,
			Shapes:     t.Shapes,
			Animation:  anim,
			Terrain:    t.Terrain,
			Neighbors:  t.Neighbors,
		})
	}
	res := TilesetReadResult{
		File:       rel,
		Source:     source,
		Unsaved:    dirty,
		Name:       doc.Name,
		TileWidth:  doc.TileWidth,
		TileHeight: doc.TileHeight,
		Image:      doc.Image,
		Atlas:      doc.Atlas,
		TileCount:  doc.Count(),
		Terrains:   doc.Terrains,
		Tiles:      tiles,
		YAML:       string(raw),
	}
	text := fmt.Sprintf("%s (%d tiles, %dx%d, %d configured tiles, %d terrains).\n%s",
		res.File, res.TileCount, res.TileWidth, res.TileHeight, len(res.Tiles), len(res.Terrains), res.YAML)
	return &Result{Text: text, Structured: res}, nil
}

func tilesetEdit(ctx context.Context, c *Call) (*Result, error) {
	var args tilesetEditArgs
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("tileset_edit: %w", err)
	}
	if len(args.Ops) == 0 {
		return nil, errors.New("tileset_edit: ops must not be empty")
	}
	var clean string
	if args.File != "" {
		var err error
		clean, err = cleanProjectPath(args.File)
		if err != nil {
			return nil, fmt.Errorf("tileset_edit: %w", err)
		}
		lower := strings.ToLower(clean)
		if !strings.HasSuffix(lower, ".tileset.yaml") && !strings.HasSuffix(lower, ".tileset.yml") {
			return nil, fmt.Errorf("tileset_edit: %s: file name must end with .tileset.yaml", clean)
		}
	}

	var res TilesetEditResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.Playing() {
			return fmt.Errorf("tileset_edit: %w", editor.ErrPlaying)
		}
		if holdErr := sketchHoldEditError(ed, sceneName(ed, projectRoot(ed))); holdErr != nil {
			return holdErr
		}
		if clean != "" {
			if !ed.TilesetOpen() || ed.TilesetPath() != clean {
				if err := ed.OpenTilesetProjectFile(clean); err != nil {
					return fmt.Errorf("tileset_edit: open %s: %w", clean, err)
				}
			}
		} else if !ed.TilesetOpen() {
			return errors.New("tileset_edit: no tileset is open in the editor; pass file or open a *.tileset.yaml first")
		}

		label := fmt.Sprintf("MCP tileset edit (%d ops)", len(args.Ops))
		if err := ed.History.Group(label, func() error {
			for i, op := range args.Ops {
				if err := applyTilesetOp(ed, op); err != nil {
					return fmt.Errorf("tileset_edit: ops[%d] (%s): %w", i, op.Op, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}

		if args.Save {
			if err := ed.SaveTileset(); err != nil {
				return fmt.Errorf("tileset_edit: save: %w", err)
			}
		}

		doc := ed.TilesetDoc()
		res = TilesetEditResult{
			File:    ed.TilesetPath(),
			Applied: len(args.Ops),
			Saved:   args.Save,
			Unsaved: ed.TilesetDirty(),
			Tiles:   len(doc.Tiles),
		}
		return nil
	}); err != nil {
		return nil, err
	}

	text := fmt.Sprintf("Applied %d edits to %s (%d configured tiles).", res.Applied, res.File, res.Tiles)
	if res.Saved {
		text += " Saved to disk."
	}
	return &Result{Text: text, Structured: res}, nil
}

func applyTilesetOp(ed *editor.Editor, op tilesetEditOp) error {
	switch op.Op {
	case "set_tile":
		if op.ID == nil {
			return errors.New("set_tile requires id")
		}
		id := *op.ID
		if op.Name != nil {
			if err := ed.SetTilesetTileName(id, *op.Name); err != nil {
				return err
			}
		}
		if op.Solid != nil || op.OneWay != nil || op.Flags != nil {
			cur, _ := ed.TilesetTileInfo(id)
			solid, oneWay, flags := cur.Solid, cur.OneWay, cur.Flags
			if op.Solid != nil {
				solid = *op.Solid
			}
			if op.OneWay != nil {
				oneWay = *op.OneWay
			}
			if op.Flags != nil {
				flags = tilemap.TileFlags(*op.Flags)
			}
			if err := ed.SetTilesetTileFlags(id, solid, oneWay, flags); err != nil {
				return err
			}
		}
		if op.ClearWeight {
			if err := ed.SetTilesetTileWeight(id, nil); err != nil {
				return err
			}
		} else if op.Weight != nil {
			if err := ed.SetTilesetTileWeight(id, op.Weight); err != nil {
				return err
			}
		}
		if op.Shapes != nil {
			cur, _ := ed.TilesetTileInfo(id)
			for i := len(cur.Shapes) - 1; i >= 0; i-- {
				if err := ed.RemoveTilesetTileShape(id, i); err != nil {
					return err
				}
			}
			for _, sa := range *op.Shapes {
				props, err := decodeTilesetProps(sa.Properties)
				if err != nil {
					return err
				}
				if err := ed.AddTilesetTileShape(id, tilemap.TileShapeDoc{
					Kind:       sa.Kind,
					Name:       sa.Name,
					Type:       sa.Type,
					X:          sa.X,
					Y:          sa.Y,
					Width:      sa.Width,
					Height:     sa.Height,
					Points:     sa.Points,
					Properties: props,
				}); err != nil {
					return err
				}
			}
		}
		if op.Animation != nil {
			frames := make([]tilemap.TileFrameDoc, len(*op.Animation))
			for i, f := range *op.Animation {
				d, err := time.ParseDuration(strings.TrimSpace(f.Duration))
				if err != nil || d <= 0 {
					return fmt.Errorf("animation[%d].duration %q must be a positive Go duration", i, f.Duration)
				}
				frames[i] = tilemap.TileFrameDoc{Tile: f.Tile, Duration: d}
			}
			if err := ed.SetTilesetTileAnimation(id, frames); err != nil {
				return err
			}
		}
		if op.Terrain != nil {
			if *op.Terrain == "" {
				if err := ed.ClearTilesetTileTerrain(id); err != nil {
					return err
				}
			} else {
				var mask uint8
				if op.Neighbors != nil {
					var err error
					mask, err = tilemap.TerrainNeighborsMask(*op.Neighbors)
					if err != nil {
						return err
					}
				}
				if err := ed.SetTilesetTileMask(id, *op.Terrain, mask, true); err != nil {
					return err
				}
			}
		}
		return nil

	case "set_property":
		if op.ID == nil {
			return errors.New("set_property requires id")
		}
		val, err := decodeTilesetScalar(op.Value)
		if err != nil {
			return err
		}
		return ed.SetTilesetTileProperty(*op.ID, op.Key, val)

	case "remove_property":
		if op.ID == nil {
			return errors.New("remove_property requires id")
		}
		return ed.DeleteTilesetTileProperty(*op.ID, op.Key)

	case "add_terrain":
		if op.Name == nil {
			return errors.New("add_terrain requires name")
		}
		return ed.AddTilesetTerrain(*op.Name, op.Mode)

	case "remove_terrain":
		if op.Name == nil {
			return errors.New("remove_terrain requires name")
		}
		return ed.RemoveTilesetTerrain(*op.Name)

	default:
		return fmt.Errorf("unknown op %q", op.Op)
	}
}

func decodeTilesetScalar(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, errors.New("value is required")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	switch x := v.(type) {
	case bool, string:
		return x, nil
	case json.Number:
		s := x.String()
		if !strings.ContainsAny(s, ".eE") {
			if n, err := x.Int64(); err == nil {
				return int(n), nil
			}
		}
		f, err := x.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("invalid number %q", s)
		}
		return f, nil
	default:
		return nil, fmt.Errorf("property value must be a bool, integer, number, or string, got %T", v)
	}
}

func decodeTilesetProps(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return nil, err
	}
	out := make(map[string]any, len(rawMap))
	for k, v := range rawMap {
		val, err := decodeTilesetScalar(v)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", k, err)
		}
		out[k] = val
	}
	return out, nil
}
