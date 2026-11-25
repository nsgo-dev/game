package tilemap_test

import (
	"errors"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func TestImageCollectionTilesetTMJAndTMXEquivalence(t *testing.T) {
	tmj := []byte(`{
		"width": 3, "height": 2, "tilewidth": 16, "tileheight": 16,
		"orientation": "orthogonal",
		"tilesets": [
			{"firstgid": 1, "source": "tilesets/collection.tsj"}
		],
		"layers": [
			{
				"name": "ground", "type": "tilelayer", "width": 3, "height": 2,
				"opacity": 1, "visible": true, "tintcolor": "#80ff80",
				"data": [1, 3, 6, 0, 0, 0]
			}
		]
	}`)
	tsj := []byte(`{
		"name": "props_col",
		"tilewidth": 24, "tileheight": 32,
		"columns": 0, "tilecount": 3,
		"tileoffset": {"x": 2, "y": -4},
		"objectalignment": "center",
		"tiles": [
			{
				"id": 0,
				"image": "img/tree.png", "imagewidth": 24, "imageheight": 32,
				"properties": [{"name": "solid", "type": "bool", "value": true}],
				"objectgroup": {
					"objects": [{"id": 1, "x": 4, "y": 16, "width": 16, "height": 16}]
				}
			},
			{
				"id": 2,
				"image": "img/sheet.png", "imagewidth": 64, "imageheight": 32,
				"x": 16, "y": 4, "width": 12, "height": 20,
				"animation": [
					{"tileid": 2, "duration": 100},
					{"tileid": 5, "duration": 100}
				]
			},
			{
				"id": 5,
				"image": "img/rock.png", "imagewidth": 16, "imageheight": 12
			}
		]
	}`)

	tmx := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<map orientation="orthogonal" width="3" height="2" tilewidth="16" tileheight="16">
 <tileset firstgid="1" source="tilesets/collection.tsx"/>
 <layer name="ground" width="3" height="2" opacity="1" visible="1" tintcolor="#80ff80">
  <data encoding="csv">1,3,6,0,0,0</data>
 </layer>
</map>`)
	tsx := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<tileset name="props_col" tilewidth="24" tileheight="32" tilecount="3" columns="0" objectalignment="center">
 <tileoffset x="2" y="-4"/>
 <tile id="0">
  <image source="img/tree.png" width="24" height="32"/>
  <properties>
   <property name="solid" type="bool" value="true"/>
  </properties>
  <objectgroup>
   <object id="1" x="4" y="16" width="16" height="16"/>
  </objectgroup>
 </tile>
 <tile id="2" x="16" y="4" width="12" height="20">
  <image source="img/sheet.png" width="64" height="32"/>
  <animation>
   <frame tileid="2" duration="100"/>
   <frame tileid="5" duration="100"/>
  </animation>
 </tile>
 <tile id="5">
  <image source="img/rock.png" width="16" height="12"/>
 </tile>
</tileset>`)

	makeLoader := func(images *[]string) tilemap.TilesetLoader {
		return tilemap.TilesetLoader{
			LoadTileset: func(source string) ([]byte, error) {
				switch source {
				case "tilesets/collection.tsj":
					return tsj, nil
				case "tilesets/collection.tsx":
					return tsx, nil
				default:
					return nil, errors.New("not found: " + source)
				}
			},
			LoadImage: func(imagePath string) (core.ImageSource, error) {
				*images = append(*images, imagePath)
				return core.ImageSource{URI: "asset:" + imagePath}, nil
			},
		}
	}

	var imgsTMJ, imgsTMX []string
	mTMJ, err := tilemap.LoadTiled(tmj, makeLoader(&imgsTMJ))
	if err != nil {
		t.Fatalf("LoadTiled TMJ: %v", err)
	}
	mTMX, err := tilemap.LoadTiled(tmx, makeLoader(&imgsTMX))
	if err != nil {
		t.Fatalf("LoadTiled TMX: %v", err)
	}
	assertEqualTilemaps(t, mTMX, mTMJ)

	wantImgs := []string{
		"tilesets/img/tree.png",
		"tilesets/img/sheet.png",
		"tilesets/img/rock.png",
	}
	if len(imgsTMJ) != len(wantImgs) {
		t.Fatalf("loaded images = %v, want %v", imgsTMJ, wantImgs)
	}
	for i := range wantImgs {
		if imgsTMJ[i] != wantImgs[i] || imgsTMX[i] != wantImgs[i] {
			t.Fatalf("image[%d]: TMJ=%q TMX=%q want=%q", i, imgsTMJ[i], imgsTMX[i], wantImgs[i])
		}
	}

	ts := mTMJ.Tilesets[0]
	if ts.TileOffsetX != 2 || ts.TileOffsetY != -4 || ts.ObjectAlignment != tilemap.ObjectAlignmentCenter {
		t.Fatalf("tileset offset=(%g,%g) align=%v, want (2,-4) center", ts.TileOffsetX, ts.TileOffsetY, ts.ObjectAlignment)
	}
	if len(ts.TileImages) != 3 {
		t.Fatalf("TileImages len = %d, want 3", len(ts.TileImages))
	}
	sub := ts.TileImages[2]
	if sub.X != 16 || sub.Y != 4 || sub.Width != 12 || sub.Height != 20 {
		t.Fatalf("sub-rect tile = %+v, want x=16 y=4 w=12 h=20", sub)
	}

	// TileBox reports each collection tile's own size.
	if w, h := mTMJ.TileBox(1); w != 24 || h != 32 {
		t.Fatalf("TileBox(1) = (%g,%g), want (24,32)", w, h)
	}
	if w, h := mTMJ.TileBox(3); w != 12 || h != 20 {
		t.Fatalf("TileBox(3) = (%g,%g), want (12,20)", w, h)
	}
	if w, h := mTMJ.TileBox(6); w != 16 || h != 12 {
		t.Fatalf("TileBox(6) = (%g,%g), want (16,12)", w, h)
	}

	// Flipped TileShapes on collection tile uses its own 24x32 box.
	flipped := mTMJ.TileShapes(tilemap.FlipHorizontal | 1)
	if len(flipped) != 1 || len(flipped[0].Points) != 4 {
		t.Fatalf("flipped shapes = %+v", flipped)
	}
	// Original rect [4, 20] x [16, 32] flipped H in width 24 stays [4, 20] x [16, 32].
	if flipped[0].Points[0].X != 20 || flipped[0].Points[1].X != 4 {
		t.Fatalf("flipped shape X = (%v, %v), want (20, 4)", flipped[0].Points[0].X, flipped[0].Points[1].X)
	}

	// Drawing applies TileImages, sub-rect, tileoffset, layer tint, and animation.
	var d core.Drawing
	mTMJ.Draw(&d, nil)
	if len(d.Ops) != 3 {
		t.Fatalf("got %d draw ops, want 3", len(d.Ops))
	}
	wantTint := core.RGB(0x80ff80)
	// Tile 0 at col=0, row=0: 24x32, dstX=0+2=2, dstY=16-32-4=-20
	if !d.Ops[0].Tinted || d.Ops[0].Color != wantTint || d.Ops[0].Image.URI != "asset:tilesets/img/tree.png" ||
		d.Ops[0].X != 2 || d.Ops[0].Y != -20 || d.Ops[0].W != 24 || d.Ops[0].H != 32 {
		t.Fatalf("op[0] = %+v", d.Ops[0])
	}
	// Tile 2 at col=1, row=0: sub-rect (16,4,12,20), dstX=16+2=18, dstY=16-20-4=-8
	if d.Ops[1].Image.URI != "asset:tilesets/img/sheet.png" ||
		d.Ops[1].X1 != 16 || d.Ops[1].Y1 != 4 || d.Ops[1].X2 != 12 || d.Ops[1].Y2 != 20 ||
		d.Ops[1].X != 18 || d.Ops[1].Y != -8 {
		t.Fatalf("op[1] = %+v", d.Ops[1])
	}

	// Advance animation by 100ms -> tile 2 switches to tile 5 (rock.png, 16x12).
	mTMJ.Update(100 * time.Millisecond)
	d.Ops = d.Ops[:0]
	mTMJ.Draw(&d, nil)
	if d.Ops[1].Image.URI != "asset:tilesets/img/rock.png" || d.Ops[1].W != 16 || d.Ops[1].H != 12 {
		t.Fatalf("animated collection op[1] = %+v, want rock.png 16x12", d.Ops[1])
	}

	// Verify YSort also draws per-tile images & tint on collection tilesets.
	mTMJ.Layers[0].YSort = true
	d.Ops = d.Ops[:0]
	mTMJ.Draw(&d, nil)
	if len(d.Ops) != 3 || d.Ops[0].Image.URI != "asset:tilesets/img/tree.png" || !d.Ops[0].Tinted || d.Ops[0].Color != wantTint {
		t.Fatalf("ysort collection ops = %+v", d.Ops)
	}
}

func TestObjectTemplatesTXAndTJPropertyMergeAndOverrides(t *testing.T) {
	// Shared external tileset used by map AND referenced via relative path from template.
	sharedTSJ := []byte(`{"name":"shared","image":"shared.png","tilewidth":16,"tileheight":16,"columns":2,"tilecount":4}`)
	sharedTSX := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<tileset name="shared" tilewidth="16" tileheight="16" tilecount="4" columns="2">
 <image source="shared.png" width="32" height="32"/>
</tileset>`)

	// External tileset only referenced by a template (not declared in map).
	extraTSJ := []byte(`{"name":"extra","tilewidth":20,"tileheight":20,"columns":0,"tilecount":1,"tiles":[{"id":0,"image":"orb.png","imagewidth":20,"imageheight":20}]}`)
	extraTSX := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<tileset name="extra" tilewidth="20" tileheight="20" tilecount="1" columns="0">
 <tile id="0"><image source="orb.png" width="20" height="20"/></tile>
</tileset>`)

	// Template 1: references map's shared tileset via ../tilesets/shared.* + custom properties.
	chestTJ := []byte(`{
		"type": "template",
		"tileset": {"firstgid": 1, "source": "../tilesets/shared.tsj"},
		"object": {
			"gid": 2,
			"name": "Chest",
			"type": "container",
			"width": 16,
			"height": 16,
			"rotation": 10,
			"visible": true,
			"properties": [
				{"name": "loot", "type": "string", "value": "gold"},
				{"name": "value", "type": "int", "value": 100},
				{"name": "locked", "type": "bool", "value": true},
				{"name": "tag", "type": "string", "value": "base"}
			]
		}
	}`)
	chestTX := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<template>
 <tileset firstgid="1" source="../tilesets/shared.tsx"/>
 <object name="Chest" type="container" gid="2" width="16" height="16" rotation="10" visible="1">
  <properties>
   <property name="loot" value="gold"/>
   <property name="value" type="int" value="100"/>
   <property name="locked" type="bool" value="true"/>
   <property name="tag" value="base"/>
  </properties>
 </object>
</template>`)

	// Template 2: references an external tileset NOT in the map, with FlipHorizontal on GID.
	orbTJ := []byte(`{
		"type": "template",
		"tileset": {"firstgid": 1, "source": "extra.tsj"},
		"object": {
			"gid": 2147483649,
			"name": "Orb",
			"class": "pickup",
			"width": 20,
			"height": 20
		}
	}`)
	orbTX := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<template>
 <tileset firstgid="1" source="extra.tsx"/>
 <object name="Orb" class="pickup" gid="2147483649" width="20" height="20"/>
</template>`)

	// Template 3: embedded tileset + polygon template.
	embedTJ := []byte(`{
		"type": "template",
		"tileset": {"firstgid": 1, "name": "emb", "image": "emb.png", "tilewidth": 8, "tileheight": 8, "columns": 1, "tilecount": 1},
		"object": {"gid": 1, "name": "Gem", "width": 8, "height": 8}
	}`)
	embedTX := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<template>
 <tileset firstgid="1" name="emb" tilewidth="8" tileheight="8" tilecount="1" columns="1">
  <image source="emb.png" width="8" height="8"/>
 </tileset>
 <object name="Gem" gid="1" width="8" height="8"/>
</template>`)

	// Map declares terrain (firstgid=1, count=4) and shared (firstgid=5, count=4),
	// then places two instances of Chest (with different overrides), one Orb, and one Gem.
	mapTMJ := []byte(`{
		"width": 4, "height": 4, "tilewidth": 16, "tileheight": 16,
		"tilesets": [
			{"firstgid": 1, "name": "terrain", "image": "terrain.png", "tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 4},
			{"firstgid": 5, "source": "tilesets/shared.tsj"}
		],
		"layers": [{
			"name": "objs", "type": "objectgroup", "opacity": 1, "visible": true,
			"objects": [
				{
					"id": 10, "template": "templates/chest.tj", "x": 32, "y": 48,
					"name": "Boss Chest", "rotation": 25,
					"properties": [
						{"name": "value", "type": "int", "value": 250},
						{"name": "locked", "type": "bool", "value": false},
						{"name": "owner", "type": "string", "value": "hero"}
					]
				},
				{
					"id": 11, "template": "templates/chest.tj", "x": 64, "y": 48,
					"visible": false
				},
				{
					"id": 12, "template": "templates/orb.tj", "x": 16, "y": 32
				},
				{
					"id": 13, "template": "templates/embed.tj", "x": 8, "y": 16
				}
			]
		}]
	}`)

	mapTMX := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<map orientation="orthogonal" width="4" height="4" tilewidth="16" tileheight="16">
 <tileset firstgid="1" name="terrain" tilewidth="16" tileheight="16" tilecount="4" columns="2">
  <image source="terrain.png" width="32" height="32"/>
 </tileset>
 <tileset firstgid="5" source="tilesets/shared.tsx"/>
 <objectgroup name="objs" opacity="1" visible="1">
  <object id="10" template="templates/chest.tx" x="32" y="48" name="Boss Chest" rotation="25">
   <properties>
    <property name="value" type="int" value="250"/>
    <property name="locked" type="bool" value="false"/>
    <property name="owner" value="hero"/>
   </properties>
  </object>
  <object id="11" template="templates/chest.tx" x="64" y="48" visible="0"/>
  <object id="12" template="templates/orb.tx" x="16" y="32"/>
  <object id="13" template="templates/embed.tx" x="8" y="16"/>
 </objectgroup>
</map>`)

	templateReads := map[string]int{}
	loader := tilemap.TilesetLoader{
		LoadTileset: func(source string) ([]byte, error) {
			switch source {
			case "tilesets/shared.tsj":
				return sharedTSJ, nil
			case "tilesets/shared.tsx":
				return sharedTSX, nil
			case "templates/extra.tsj":
				return extraTSJ, nil
			case "templates/extra.tsx":
				return extraTSX, nil
			default:
				return nil, errors.New("unknown tileset: " + source)
			}
		},
		LoadTemplate: func(source string) ([]byte, error) {
			templateReads[source]++
			switch source {
			case "templates/chest.tj":
				return chestTJ, nil
			case "templates/chest.tx":
				return chestTX, nil
			case "templates/orb.tj":
				return orbTJ, nil
			case "templates/orb.tx":
				return orbTX, nil
			case "templates/embed.tj":
				return embedTJ, nil
			case "templates/embed.tx":
				return embedTX, nil
			default:
				return nil, errors.New("unknown template: " + source)
			}
		},
		LoadImage: func(imagePath string) (core.ImageSource, error) {
			return core.ImageSource{URI: "asset:" + imagePath}, nil
		},
	}

	mTMJ, err := tilemap.LoadTiled(mapTMJ, loader)
	if err != nil {
		t.Fatalf("LoadTiled TMJ: %v", err)
	}
	if templateReads["templates/chest.tj"] != 1 {
		t.Fatalf("chest.tj loaded %d times, want 1 (cached)", templateReads["templates/chest.tj"])
	}

	mTMX, err := tilemap.LoadTiled(mapTMX, loader)
	if err != nil {
		t.Fatalf("LoadTiled TMX: %v", err)
	}
	assertEqualTilemaps(t, mTMX, mTMJ)

	// Shared tileset was deduplicated: 4 tilesets total (terrain, shared, extra, emb).
	if len(mTMJ.Tilesets) != 4 {
		t.Fatalf("len(Tilesets) = %d, want 4", len(mTMJ.Tilesets))
	}

	objs := mTMJ.ObjectLayer("objs").Objects
	// Instance 0 (Boss Chest): merged properties + overrides + remapped GID (shared FirstGID=5 + local 1 = 6).
	boss := objs[0]
	if boss.ID != 10 || boss.Name != "Boss Chest" || boss.Type != "container" ||
		boss.Shape != tilemap.ObjectTile || boss.GID != 6 || boss.Width != 16 || boss.Height != 16 ||
		boss.Rotation != 25 || !boss.Visible {
		t.Fatalf("boss = %+v", boss)
	}
	if v, ok := boss.Properties.String("loot"); !ok || v != "gold" {
		t.Fatalf("boss.loot = (%q, %v), want (gold, true)", v, ok)
	}
	if v, ok := boss.Properties.String("tag"); !ok || v != "base" {
		t.Fatalf("boss.tag = (%q, %v), want (base, true)", v, ok)
	}
	if v, ok := boss.Properties.Int("value"); !ok || v != 250 {
		t.Fatalf("boss.value = (%d, %v), want (250, true)", v, ok)
	}
	if v, ok := boss.Properties.Bool("locked"); !ok || v {
		t.Fatalf("boss.locked = (%v, %v), want (false, true)", v, ok)
	}
	if v, ok := boss.Properties.String("owner"); !ok || v != "hero" {
		t.Fatalf("boss.owner = (%q, %v), want (hero, true)", v, ok)
	}

	// Instance 1 (plain Chest): untouched by Boss Chest overrides, Visible overridden to false.
	plain := objs[1]
	if plain.Name != "Chest" || plain.Rotation != 10 || plain.Visible || plain.GID != 6 {
		t.Fatalf("plain = %+v", plain)
	}
	if v, ok := plain.Properties.Int("value"); !ok || v != 100 {
		t.Fatalf("plain.value = (%d, %v), want (100, true)", v, ok)
	}
	if v, ok := plain.Properties.Bool("locked"); !ok || !v {
		t.Fatalf("plain.locked = (%v, %v), want (true, true)", v, ok)
	}
	if plain.Properties.Has("owner") {
		t.Fatal("plain chest should not inherit Boss Chest's owner override")
	}

	// Instance 2 (Orb): remapped onto extra tileset (FirstGID=9) while preserving FlipHorizontal!
	orb := objs[2]
	if orb.Shape != tilemap.ObjectTile || orb.TileID() != 9 || orb.FlipFlags() != tilemap.FlipHorizontal || orb.Type != "pickup" {
		t.Fatalf("orb = %+v (TileID=%d FlipFlags=%#x)", orb, orb.TileID(), orb.FlipFlags())
	}
	tsOrb, lidOrb := mTMJ.TilesetForGID(orb.GID)
	if tsOrb == nil || lidOrb != 0 || tsOrb.TileImages[0].Image.URI != "asset:templates/orb.png" {
		t.Fatalf("orb tileset = %+v localID=%d", tsOrb, lidOrb)
	}

	// Instance 3 (Gem): embedded tileset inside template resolved image relative to templates/.
	gem := objs[3]
	tsGem, lidGem := mTMJ.TilesetForGID(gem.GID)
	if tsGem == nil || lidGem != 0 || tsGem.Image.URI != "asset:templates/emb.png" {
		t.Fatalf("gem tileset = %+v localID=%d", tsGem, lidGem)
	}
}

func TestTextObjectsTMJAndTMX(t *testing.T) {
	tmj := []byte(`{
		"width": 2, "height": 2, "tilewidth": 16, "tileheight": 16,
		"layers": [{
			"name": "labels", "type": "objectgroup", "opacity": 1, "visible": true,
			"objects": [
				{
					"id": 1, "name": "title", "x": 4, "y": 8, "width": 120, "height": 40,
					"text": {
						"text": "Hello\nWorld",
						"fontfamily": "Georgia",
						"pixelsize": 20,
						"wrap": true,
						"color": "#80ff4422",
						"bold": true,
						"italic": true,
						"underline": true,
						"strikeout": true,
						"kerning": false,
						"halign": "center",
						"valign": "bottom"
					}
				},
				{
					"id": 2, "name": "default_text", "x": 0, "y": 0, "width": 80, "height": 20,
					"text": {"text": "Plain"}
				}
			]
		}]
	}`)

	tmx := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<map orientation="orthogonal" width="2" height="2" tilewidth="16" tileheight="16">
 <objectgroup name="labels" opacity="1" visible="1">
  <object id="1" name="title" x="4" y="8" width="120" height="40">
   <text fontfamily="Georgia" pixelsize="20" wrap="1" color="#80ff4422" bold="1" italic="1" underline="1" strikeout="1" kerning="0" halign="center" valign="bottom">Hello
World</text>
  </object>
  <object id="2" name="default_text" x="0" y="0" width="80" height="20">
   <text>Plain</text>
  </object>
 </objectgroup>
</map>`)

	mTMJ, err := tilemap.LoadTiled(tmj, tilemap.TilesetLoader{})
	if err != nil {
		t.Fatalf("LoadTiled TMJ: %v", err)
	}
	mTMX, err := tilemap.LoadTiled(tmx, tilemap.TilesetLoader{})
	if err != nil {
		t.Fatalf("LoadTiled TMX: %v", err)
	}
	assertEqualTilemaps(t, mTMX, mTMJ)

	objs := mTMJ.ObjectLayer("labels").Objects
	if objs[0].Shape != tilemap.ObjectText || objs[0].Text == nil {
		t.Fatalf("objs[0] = %+v", objs[0])
	}
	got := objs[0].Text
	want := tilemap.TextObject{
		Text:       "Hello\nWorld",
		FontFamily: "Georgia",
		PixelSize:  20,
		Wrap:       true,
		Color:      core.Color(0xff442280),
		Bold:       true,
		Italic:     true,
		Underline:  true,
		Strikeout:  true,
		Kerning:    false,
		HAlign:     "center",
		VAlign:     "bottom",
	}
	if *got != want {
		t.Fatalf("styled text = %+v, want %+v", *got, want)
	}
	def := objs[1].Text
	if def == nil || def.Text != "Plain" || def.FontFamily != "sans-serif" || def.PixelSize != 16 ||
		def.Color != core.RGB(0x000000) || !def.Kerning || def.HAlign != "left" || def.VAlign != "top" || def.Wrap {
		t.Fatalf("default text = %+v", def)
	}
	if _, ok := objs[0].PhysicsShape(); ok {
		t.Fatal("text object should not produce a physics shape")
	}
}

func TestTileObjectsLayerOrderTintParallaxAlignmentAndCulling(t *testing.T) {
	// Verify:
	// 1. Interleaved draw order: tilelayer("ground") -> objectgroup("props") -> imagelayer("fog") -> tilelayer("canopy")
	// 2. Group tintcolor multiplies with child layer tintcolor.
	// 3. ObjectLayer parallax & offset & opacity & visibility & camera culling.
	raw := []byte(`{
		"width": 2, "height": 2, "tilewidth": 16, "tileheight": 16,
		"tilesets": [
			{"firstgid": 1, "name": "t", "image": "t.png", "tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 4, "objectalignment": "topleft"}
		],
		"layers": [
			{"name": "ground", "type": "tilelayer", "width": 2, "height": 2, "data": [1, 0, 0, 0]},
			{
				"name": "grp", "type": "group", "tintcolor": "#ff8080", "offsetx": 4, "offsety": 2, "opacity": 0.5,
				"layers": [
					{
						"name": "props", "type": "objectgroup", "tintcolor": "#80ff80", "parallaxx": 0.5, "parallaxy": 1,
						"objects": [
							{"id": 1, "gid": 2, "x": 10, "y": 10, "width": 16, "height": 16, "visible": true},
							{"id": 2, "gid": 2, "x": 500, "y": 500, "width": 16, "height": 16, "visible": true},
							{"id": 3, "gid": 2, "x": 10, "y": 10, "width": 16, "height": 16, "visible": false}
						]
					}
				]
			},
			{"name": "fog", "type": "imagelayer", "image": "fog.png", "imagewidth": 32, "imageheight": 32, "tintcolor": "#4080ff"},
			{"name": "canopy", "type": "tilelayer", "width": 2, "height": 2, "data": [0, 1, 0, 0]}
		]
	}`)

	m, err := tilemap.LoadTiled(raw, tilemap.TilesetLoader{})
	if err != nil {
		t.Fatalf("LoadTiled: %v", err)
	}

	cam := &tilemap.Camera{X: 20, Y: 16, Zoom: 1, Width: 64, Height: 48}
	var d core.Drawing
	m.Draw(&d, cam)

	// Collect all DrawImage ops in order and verify interleaving + culling + combined tint + parallax.
	var imgs []core.DrawOp
	for _, op := range d.Ops {
		if op.Kind == core.DrawImage {
			imgs = append(imgs, op)
		}
	}
	// Expected 4 visible DrawImage ops: ground(0,0), props obj #1 (obj #2 culled, #3 hidden), fog, canopy(1,0).
	if len(imgs) != 4 {
		t.Fatalf("got %d DrawImage ops, want 4: %+v", len(imgs), imgs)
	}
	// Check order by URI and src/dst coordinates:
	if imgs[0].X != 0 || imgs[0].Y != 0 {
		t.Fatalf("ground op = %+v", imgs[0])
	}
	// props obj #1: offset=(4,2), parallaxX=0.5 with cam.X=20 -> +10 -> offX=14, offY=2; obj=(10,10) topleft -> (24,12).
	// combined tint: #ff8080 * #80ff80 -> R=(255*128+127)/255=128 (0x80), G=128 (0x80), B=(128*128+127)/255=64 (0x40), A=255.
	wantCombinedTint := core.Color(0x808040ff)
	if !imgs[1].Tinted || imgs[1].Color != wantCombinedTint || imgs[1].X != 24 || imgs[1].Y != 12 {
		t.Fatalf("props op = %+v, want tint %#08x at (24,12)", imgs[1], uint32(wantCombinedTint))
	}
	if imgs[2].Image.URI != "fog.png" || !imgs[2].Tinted || imgs[2].Color != core.RGB(0x4080ff) {
		t.Fatalf("fog op = %+v", imgs[2])
	}
	if imgs[3].X != 16 || imgs[3].Y != 0 {
		t.Fatalf("canopy op = %+v", imgs[3])
	}

	// Verify all 9 explicit alignments + isometric default (bottom) vs orthogonal default (bottomleft).
	alignCases := []struct {
		orient tilemap.Orientation
		align  tilemap.ObjectAlignment
		wantX  float32
		wantY  float32
	}{
		{tilemap.OrientationOrthogonal, tilemap.ObjectAlignmentUnspecified, 100, 84},
		{tilemap.OrientationIsometric, tilemap.ObjectAlignmentUnspecified, 92, 84},
		{tilemap.OrientationOrthogonal, tilemap.ObjectAlignmentTopLeft, 100, 100},
		{tilemap.OrientationOrthogonal, tilemap.ObjectAlignmentTop, 92, 100},
		{tilemap.OrientationOrthogonal, tilemap.ObjectAlignmentTopRight, 84, 100},
		{tilemap.OrientationOrthogonal, tilemap.ObjectAlignmentLeft, 100, 92},
		{tilemap.OrientationOrthogonal, tilemap.ObjectAlignmentCenter, 92, 92},
		{tilemap.OrientationOrthogonal, tilemap.ObjectAlignmentRight, 84, 92},
		{tilemap.OrientationOrthogonal, tilemap.ObjectAlignmentBottomLeft, 100, 84},
		{tilemap.OrientationOrthogonal, tilemap.ObjectAlignmentBottom, 92, 84},
		{tilemap.OrientationOrthogonal, tilemap.ObjectAlignmentBottomRight, 84, 84},
	}
	for _, tc := range alignCases {
		ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "a.png"}, 16, 16, 1, 1)
		ts.ObjectAlignment = tc.align
		tm := tilemap.New(1, 1, 16, 16, ts)
		tm.Orientation = tc.orient
		tm.Objects = []tilemap.ObjectLayer{{
			Name:    "o",
			Opacity: 1,
			Visible: true,
			Objects: []tilemap.Object{{ID: 1, Shape: tilemap.ObjectTile, X: 100, Y: 100, Width: 16, Height: 16, GID: 1, Visible: true}},
		}}
		d.Ops = d.Ops[:0]
		tm.Draw(&d, nil)
		if len(d.Ops) != 1 || math.Abs(float64(d.Ops[0].X-tc.wantX)) > 1e-4 || math.Abs(float64(d.Ops[0].Y-tc.wantY)) > 1e-4 {
			t.Fatalf("orient=%v align=%v -> op=%+v, want (%g,%g)", tc.orient, tc.align, d.Ops, tc.wantX, tc.wantY)
		}
	}
}

// buildGoldenCollectionAssets builds deterministic, asymmetric pixel-art images so every
// flip, rotation, sub-rect, tileoffset, alignment, and tint change is unmistakably visible.
func buildGoldenCollectionAssets() map[string]image.Image {
	// 16x16 ground tile: light gray checker with subtle border.
	terrain := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			c := color.RGBA{R: 180, G: 180, B: 180, A: 255}
			if (x/4+y/4)%2 == 0 {
				c = color.RGBA{R: 150, G: 150, B: 150, A: 255}
			}
			if x == 0 || y == 0 {
				c = color.RGBA{R: 120, G: 120, B: 120, A: 255}
			}
			terrain.SetRGBA(x, y, c)
		}
	}

	// 24x32 asymmetric tree: brown trunk at bottom-center, green canopy, bright RED 6x6 apple on TOP-RIGHT.
	tree := image.NewRGBA(image.Rect(0, 0, 24, 32))
	for y := 20; y < 32; y++ {
		for x := 9; x < 15; x++ {
			tree.SetRGBA(x, y, color.RGBA{R: 130, G: 78, B: 42, A: 255})
		}
	}
	for y := 2; y < 22; y++ {
		for x := 2; x < 22; x++ {
			tree.SetRGBA(x, y, color.RGBA{R: 46, G: 152, B: 68, A: 255})
		}
	}
	for y := 4; y < 10; y++ {
		for x := 14; x < 20; x++ {
			tree.SetRGBA(x, y, color.RGBA{R: 235, G: 48, B: 48, A: 255})
		}
	}

	// 32x16 sheet containing a 16x16 directional crate in sub-rect (16, 0, 16, 16):
	// warm wood body, cyan stripe along TOP edge, yellow 4x4 badge at TOP-LEFT.
	sheet := image.NewRGBA(image.Rect(0, 0, 32, 16))
	for y := range 16 {
		for x := 16; x < 32; x++ {
			c := color.RGBA{R: 196, G: 132, B: 64, A: 255}
			if x == 16 || x == 31 || y == 0 || y == 15 {
				c = color.RGBA{R: 92, G: 56, B: 24, A: 255}
			} else if y <= 3 {
				c = color.RGBA{R: 48, G: 204, B: 224, A: 255}
			}
			if x >= 18 && x < 22 && y >= 2 && y < 6 {
				c = color.RGBA{R: 250, G: 224, B: 56, A: 255}
			}
			sheet.SetRGBA(x, y, c)
		}
	}

	// 12x20 banner: silver pole on LEFT (x=0..2), magenta flag with white tip on TOP-RIGHT.
	banner := image.NewRGBA(image.Rect(0, 0, 12, 20))
	for y := range 20 {
		for x := range 3 {
			banner.SetRGBA(x, y, color.RGBA{R: 210, G: 215, B: 225, A: 255})
		}
	}
	for y := 1; y < 11; y++ {
		for x := 3; x < 12; x++ {
			c := color.RGBA{R: 212, G: 56, B: 172, A: 255}
			if x >= 9 && y <= 4 {
				c = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			}
			banner.SetRGBA(x, y, c)
		}
	}

	return map[string]image.Image{
		"asset:terrain.png":           terrain,
		"asset:collection/tree.png":   tree,
		"asset:collection/sheet.png":  sheet,
		"asset:collection/banner.png": banner,
	}
}

func TestCollectionTileObjectsAndTemplatesGolden(t *testing.T) {
	colTSJ := []byte(`{
		"name": "props",
		"tilewidth": 24, "tileheight": 32,
		"columns": 0, "tilecount": 3,
		"tileoffset": {"x": 0, "y": 0},
		"objectalignment": "bottomleft",
		"tiles": [
			{"id": 0, "image": "tree.png", "imagewidth": 24, "imageheight": 32},
			{"id": 1, "image": "sheet.png", "imagewidth": 32, "imageheight": 16, "x": 16, "y": 0, "width": 16, "height": 16},
			{"id": 2, "image": "banner.png", "imagewidth": 12, "imageheight": 20}
		]
	}`)
	colTSX := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<tileset name="props" tilewidth="24" tileheight="32" tilecount="3" columns="0" objectalignment="bottomleft">
 <tile id="0"><image source="tree.png" width="24" height="32"/></tile>
 <tile id="1" x="16" y="0" width="16" height="16"><image source="sheet.png" width="32" height="16"/></tile>
 <tile id="2"><image source="banner.png" width="12" height="20"/></tile>
</tileset>`)

	// Template placing banner (local tile 2 -> GID 3 inside template) with default properties.
	bannerTJ := []byte(`{
		"type": "template",
		"tileset": {"firstgid": 1, "source": "../collection/props.tsj"},
		"object": {
			"gid": 3,
			"name": "Banner",
			"type": "prop",
			"width": 12,
			"height": 20,
			"properties": [
				{"name": "faction", "type": "string", "value": "north"},
				{"name": "hp", "type": "int", "value": 10}
			]
		}
	}`)
	bannerTX := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<template>
 <tileset firstgid="1" source="../collection/props.tsx"/>
 <object name="Banner" type="prop" gid="3" width="12" height="20">
  <properties>
   <property name="faction" value="north"/>
   <property name="hp" type="int" value="10"/>
  </properties>
 </object>
</template>`)

	// 8x6 map (128x96):
	// - ground layer tinted soft green (#b8e6b8)
	// - props object layer with:
	//   1. normal tree at (6, 40) -> [6, 30) x [8, 40) (red apple at top-right: x in [20,26), y in [12,18))
	//   2. horizontally flipped tree at (36, 40) -> [36, 60) x [8, 40) (red apple mirrored to top-left: x in [40,46), y in [12,18))
	//   3. normal crate at (68, 32) -> [68, 84) x [16, 32)
	//   4. vertically flipped crate at (90, 32) -> [90, 106) x [16, 32) (cyan stripe moves to bottom)
	//   5. 90° CW flipped crate (H|D) at (110, 32) -> [110, 126) x [16, 32) (cyan stripe moves to right)
	//   6. 90° CW rotated crate around bottom-left pivot (24, 60): occupies [24, 40) x [60, 76)
	//   7. 45° rotated & scaled crate (20x16) at (66, 76)
	//   8. templated banner at (104, 86) with overridden faction="south"
	//   9. hidden tree at (0, 96) -> must NOT render
	mapTMJ := []byte(`{
		"width": 8, "height": 6, "tilewidth": 16, "tileheight": 16,
		"tilesets": [
			{"firstgid": 1, "name": "terrain", "image": "terrain.png", "tilewidth": 16, "tileheight": 16, "columns": 1, "tilecount": 1},
			{"firstgid": 2, "source": "collection/props.tsj"}
		],
		"layers": [
			{
				"name": "ground", "type": "tilelayer", "width": 8, "height": 6,
				"opacity": 1, "visible": true, "tintcolor": "#b8e6b8",
				"data": [
					1,1,1,1,1,1,1,1,
					1,1,1,1,1,1,1,1,
					1,1,1,1,1,1,1,1,
					1,1,1,1,1,1,1,1,
					1,1,1,1,1,1,1,1,
					1,1,1,1,1,1,1,1
				]
			},
			{
				"name": "props", "type": "objectgroup", "opacity": 1, "visible": true,
				"objects": [
					{"id": 1, "gid": 2, "x": 6, "y": 40, "width": 24, "height": 32},
					{"id": 2, "gid": 2147483650, "x": 36, "y": 40, "width": 24, "height": 32},
					{"id": 3, "gid": 3, "x": 68, "y": 32, "width": 16, "height": 16},
					{"id": 4, "gid": 1073741827, "x": 90, "y": 32, "width": 16, "height": 16},
					{"id": 5, "gid": 2684354563, "x": 110, "y": 32, "width": 16, "height": 16},
					{"id": 6, "gid": 3, "x": 24, "y": 60, "width": 16, "height": 16, "rotation": 90},
					{"id": 7, "gid": 3, "x": 66, "y": 76, "width": 20, "height": 16, "rotation": 45},
					{
						"id": 8, "template": "templates/banner.tj", "x": 104, "y": 86,
						"properties": [{"name": "faction", "type": "string", "value": "south"}]
					},
					{"id": 9, "gid": 2, "x": 0, "y": 96, "width": 24, "height": 32, "visible": false}
				]
			}
		]
	}`)

	mapTMX := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<map orientation="orthogonal" width="8" height="6" tilewidth="16" tileheight="16">
 <tileset firstgid="1" name="terrain" tilewidth="16" tileheight="16" tilecount="1" columns="1">
  <image source="terrain.png" width="16" height="16"/>
 </tileset>
 <tileset firstgid="2" source="collection/props.tsx"/>
 <layer name="ground" width="8" height="6" opacity="1" visible="1" tintcolor="#b8e6b8">
  <data encoding="csv">
1,1,1,1,1,1,1,1,
1,1,1,1,1,1,1,1,
1,1,1,1,1,1,1,1,
1,1,1,1,1,1,1,1,
1,1,1,1,1,1,1,1,
1,1,1,1,1,1,1,1
</data>
 </layer>
 <objectgroup name="props" opacity="1" visible="1">
  <object id="1" gid="2" x="6" y="40" width="24" height="32"/>
  <object id="2" gid="2147483650" x="36" y="40" width="24" height="32"/>
  <object id="3" gid="3" x="68" y="32" width="16" height="16"/>
  <object id="4" gid="1073741827" x="90" y="32" width="16" height="16"/>
  <object id="5" gid="2684354563" x="110" y="32" width="16" height="16"/>
  <object id="6" gid="3" x="24" y="60" width="16" height="16" rotation="90"/>
  <object id="7" gid="3" x="66" y="76" width="20" height="16" rotation="45"/>
  <object id="8" template="templates/banner.tx" x="104" y="86">
   <properties>
    <property name="faction" value="south"/>
   </properties>
  </object>
  <object id="9" gid="2" x="0" y="96" width="24" height="32" visible="0"/>
 </objectgroup>
</map>`)

	loader := tilemap.TilesetLoader{
		LoadTileset: func(source string) ([]byte, error) {
			switch source {
			case "collection/props.tsj":
				return colTSJ, nil
			case "collection/props.tsx":
				return colTSX, nil
			default:
				return nil, errors.New("not found: " + source)
			}
		},
		LoadTemplate: func(source string) ([]byte, error) {
			switch source {
			case "templates/banner.tj":
				return bannerTJ, nil
			case "templates/banner.tx":
				return bannerTX, nil
			default:
				return nil, errors.New("not found: " + source)
			}
		},
		LoadImage: func(imagePath string) (core.ImageSource, error) {
			return core.ImageSource{URI: "asset:" + imagePath}, nil
		},
	}

	mTMJ, err := tilemap.LoadTiled(mapTMJ, loader)
	if err != nil {
		t.Fatalf("LoadTiled TMJ: %v", err)
	}
	mTMX, err := tilemap.LoadTiled(mapTMX, loader)
	if err != nil {
		t.Fatalf("LoadTiled TMX: %v", err)
	}
	assertEqualTilemaps(t, mTMX, mTMJ)

	// Verify templated banner properties merged with override.
	banner := mTMJ.ObjectLayer("props").Objects[7]
	if v, ok := banner.Properties.String("faction"); !ok || v != "south" {
		t.Fatalf("banner.faction = (%q, %v), want (south, true)", v, ok)
	}
	if v, ok := banner.Properties.Int("hp"); !ok || v != 10 {
		t.Fatalf("banner.hp = (%d, %v), want (10, true)", v, ok)
	}

	var dTMJ, dTMX core.Drawing
	mTMJ.Draw(&dTMJ, nil)
	mTMX.Draw(&dTMX, nil)
	sameOps(t, dTMX.Ops, dTMJ.Ops)

	img := gametest.RenderOps(128, 96, dTMJ.Ops, gametest.RenderOptions{
		Background: color.RGBA{R: 20, G: 24, B: 32, A: 255},
		Images:     buildGoldenCollectionAssets(),
	})

	// Mathematical pixel probes proving geometry matches Tiled's coordinate conventions:
	redApple := color.RGBA{R: 235, G: 48, B: 48, A: 255}
	yellowBadge := color.RGBA{R: 250, G: 224, B: 56, A: 255}
	magentaFlag := color.RGBA{R: 212, G: 56, B: 172, A: 255}

	// 1. Normal tree apple at (6+16, 8+6) = (22, 14).
	if px := img.RGBAAt(22, 14); px != redApple {
		t.Fatalf("normal tree apple at (22,14) = %+v, want %+v", px, redApple)
	}
	// 2. H-flipped tree apple mirrored from x=[14,20) to x=[4,10) -> (36+6, 8+6) = (42, 14).
	if px := img.RGBAAt(42, 14); px != redApple {
		t.Fatalf("flipped tree apple at (42,14) = %+v, want %+v", px, redApple)
	}
	// 3. Normal crate yellow badge at (68+3, 16+3) = (71, 19).
	if px := img.RGBAAt(71, 19); px != yellowBadge {
		t.Fatalf("normal crate badge at (71,19) = %+v, want %+v", px, yellowBadge)
	}
	// 4. V-flipped crate yellow badge moves to bottom-left: (90+3, 16+12) = (93, 28).
	if px := img.RGBAAt(93, 28); px != yellowBadge {
		t.Fatalf("V-flipped crate badge at (93,28) = %+v, want %+v", px, yellowBadge)
	}
	// 5. H|D (90° CW) crate yellow badge moves to top-right: (110+12, 16+3) = (122, 19).
	if px := img.RGBAAt(122, 19); px != yellowBadge {
		t.Fatalf("90CW flipped crate badge at (122,19) = %+v, want %+v", px, yellowBadge)
	}
	// 6. 90° CW rotated crate around bottom-left (24, 60): local (3, 3) relative to bottom-left is (3, -13);
	// rotated 90° CW -> (+13, +3) -> world (37, 63)!
	if px := img.RGBAAt(37, 63); px != yellowBadge {
		t.Fatalf("90° rotated crate badge at (37,63) = %+v, want %+v", px, yellowBadge)
	}
	// 7. Templated banner flag at (104+5, 66+6) = (109, 72).
	if px := img.RGBAAt(109, 72); px != magentaFlag {
		t.Fatalf("templated banner flag at (109,72) = %+v, want %+v", px, magentaFlag)
	}
	// 8. Hidden tree at (0, 96) must not obscure the tinted ground at (12, 80).
	if px := img.RGBAAt(12, 80); px.G <= px.R || px.G <= px.B {
		t.Fatalf("expected green-tinted ground at (12,80) without hidden tree, got %+v", px)
	}

	gametest.AssertGolden(t, "collection_objects_templates", img)
}

func TestCollectionTemplatesTextAndTintErrors(t *testing.T) {
	base := `"tilewidth": 16, "tileheight": 16, "width": 2, "height": 1`

	jsonCases := []struct {
		name, json, field string
	}{
		{"missing LoadTemplate", `{` + base + `, "layers": [{"type": "objectgroup", "objects": [{"id": 1, "template": "t.tj"}]}]}`, "layers[0].objects[0].template"},
		{"bad objectalignment", `{` + base + `, "tilesets": [{"firstgid": 1, "objectalignment": "diagonal"}]}`, "tilesets[0].objectalignment"},
		{"bad layer tintcolor", `{` + base + `, "layers": [{"type": "tilelayer", "name": "g", "tintcolor": "nope", "data": [0, 0]}]}`, "layers[0].tintcolor"},
		{"bad text color", `{` + base + `, "layers": [{"type": "objectgroup", "objects": [{"id": 1, "text": {"text": "hi", "color": "notcolor"}}]}]}`, "layers[0].objects[0].text.color"},
		{"bad text halign", `{` + base + `, "layers": [{"type": "objectgroup", "objects": [{"id": 1, "text": {"text": "hi", "halign": "middle"}}]}]}`, "layers[0].objects[0].text.halign"},
		{"bad text valign", `{` + base + `, "layers": [{"type": "objectgroup", "objects": [{"id": 1, "text": {"text": "hi", "valign": "middle"}}]}]}`, "layers[0].objects[0].text.valign"},
	}
	for _, tc := range jsonCases {
		t.Run("TMJ/"+tc.name, func(t *testing.T) {
			data := loaderrtest.Indent(tc.json)
			_, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{})
			loaderrtest.ExpectField(t, loaderr.WithFile(err, "level.tmj"), "level.tmj", data, tc.field)
		})
	}

	t.Run("TX template property error names TX file and line", func(t *testing.T) {
		tx := []byte("<?xml version=\"1.0\"?>\n<template>\n <object name=\"a\">\n  <properties>\n   <property name=\"n\" type=\"int\" value=\"bad\"/>\n  </properties>\n </object>\n</template>\n")
		tmx := []byte("<?xml version=\"1.0\"?>\n<map width=\"1\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <objectgroup name=\"o\">\n  <object id=\"1\" template=\"tmpl/a.tx\" x=\"0\" y=\"0\"/>\n </objectgroup>\n</map>\n")
		_, err := tilemap.LoadTiled(tmx, tilemap.TilesetLoader{
			LoadTemplate: func(string) ([]byte, error) { return tx, nil },
		})
		e := loaderrtest.Expect(t, loaderr.WithFile(err, "level.tmx"), "tmpl/a.tx", 5)
		if e.Field != "object.properties[0].value" {
			t.Errorf("Field = %q, want object.properties[0].value", e.Field)
		}
	})

	t.Run("TJ template missing object names TJ file", func(t *testing.T) {
		tj := loaderrtest.Indent(`{"type": "template"}`)
		tmj := loaderrtest.Indent(`{` + base + `, "layers": [{"type": "objectgroup", "objects": [{"id": 1, "template": "tmpl/empty.tj"}]}]}`)
		_, err := tilemap.LoadTiled(tmj, tilemap.TilesetLoader{
			LoadTemplate: func(string) ([]byte, error) { return tj, nil },
		})
		e := loaderrtest.Expect(t, loaderr.WithFile(err, "level.tmj"), "tmpl/empty.tj", 1)
		if e.Field != "object" {
			t.Errorf("Field = %q, want object", e.Field)
		}
	})

	t.Run("external TSX collection tile image error names TSX file and line", func(t *testing.T) {
		tsx := []byte("<?xml version=\"1.0\"?>\n<tileset name=\"col\" columns=\"0\">\n <tile id=\"0\">\n  <image source=\"missing.png\" width=\"16\" height=\"16\"/>\n </tile>\n</tileset>\n")
		tmx := []byte("<?xml version=\"1.0\"?>\n<map width=\"1\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <tileset firstgid=\"1\" source=\"tiles/col.tsx\"/>\n</map>\n")
		_, err := tilemap.LoadTiled(tmx, tilemap.TilesetLoader{
			LoadTileset: func(string) ([]byte, error) { return tsx, nil },
			LoadImage:   func(string) (core.ImageSource, error) { return core.ImageSource{}, errors.New("missing") },
		})
		e := loaderrtest.Expect(t, loaderr.WithFile(err, "level.tmx"), "tiles/col.tsx", 4)
		if e.Field != "tiles[0].image" {
			t.Errorf("Field = %q, want tiles[0].image", e.Field)
		}
	})

	t.Run("TMX bad tintcolor and objectalignment report exact lines", func(t *testing.T) {
		tmxTint := []byte("<?xml version=\"1.0\"?>\n<map width=\"1\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <layer name=\"g\" width=\"1\" height=\"1\" tintcolor=\"bad\">\n  <data encoding=\"csv\">0</data>\n </layer>\n</map>\n")
		_, err := tilemap.LoadTiled(tmxTint, tilemap.TilesetLoader{})
		e := loaderrtest.Expect(t, loaderr.WithFile(err, "tint.tmx"), "tint.tmx", 3)
		if e.Field != "layers[0].tintcolor" {
			t.Errorf("Field = %q, want layers[0].tintcolor", e.Field)
		}

		tmxAlign := []byte("<?xml version=\"1.0\"?>\n<map width=\"1\" height=\"1\" tilewidth=\"16\" tileheight=\"16\">\n <tileset firstgid=\"1\" name=\"t\" objectalignment=\"bogus\"/>\n</map>\n")
		_, err = tilemap.LoadTiled(tmxAlign, tilemap.TilesetLoader{})
		e = loaderrtest.Expect(t, loaderr.WithFile(err, "align.tmx"), "align.tmx", 3)
		if e.Field != "tilesets[0].objectalignment" {
			t.Errorf("Field = %q, want tilesets[0].objectalignment", e.Field)
		}
	})
}
