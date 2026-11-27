package voxel

import (
	"github.com/nsgo-dev/game/pkg/math3d"
)

const (
	// defaultLightByte encodes unobstructed sky light (15) and zero block light (0).
	defaultLightByte uint8 = MaxLightLevel << 4
)

type lightRemNode struct {
	pos BlockCoord
	val uint8
}

type airLightChunk struct {
	light      []uint8
	nonDefault int
}

type chunkColumn struct {
	x, z int
}

func newLightSlice(n int) []uint8 {
	s := make([]uint8, n*n*n)
	for i := range s {
		s[i] = defaultLightByte
	}
	return s
}

func countNonDefaultLight(s []uint8) int {
	n := 0
	for _, b := range s {
		if b != defaultLightByte {
			n++
		}
	}
	return n
}

func packLight(sky, block uint8) uint8 {
	return (sky << 4) | (block & 0x0F)
}

func unpackSky(b uint8) uint8   { return b >> 4 }
func unpackBlock(b uint8) uint8 { return b & 0x0F }

// LightFactor converts a discrete sky or block light level (0..15) into a
// linear-space brightness multiplier in [0, 1]. Using a quadratic curve in
// linear space produces a perceptually even gradient after sRGB encoding in
// [render3d].
func LightFactor(level uint8) float32 {
	if level == 0 {
		return 0
	}
	if level >= MaxLightLevel {
		return 1
	}
	t := float32(level) / float32(MaxLightLevel)
	return t * t
}

// LightStyle controls how per-vertex sky light, block light, and ambient
// occlusion combine into linear RGB vertex colors in [ChunkMesh.Colors].
type LightStyle struct {
	// SkyColor is the linear RGB tint at maximum sky light (level 15).
	// When both SkyColor and BlockColor are zero, [DefaultLightStyle] is used.
	SkyColor math3d.Vec3
	// BlockColor is the linear RGB tint at maximum block light (level 15).
	BlockColor math3d.Vec3
	// Ambient is the minimum linear brightness floor when both sky and block
	// light are 0 (for example 0.03 so unlit caves remain faintly visible).
	Ambient float32
}

// DefaultLightStyle returns a neutral linear lighting style with white daylight
// (1, 1, 1), white block light (1, 1, 1), and zero ambient floor.
func DefaultLightStyle() LightStyle {
	return LightStyle{
		SkyColor:   math3d.Vec3{X: 1, Y: 1, Z: 1},
		BlockColor: math3d.Vec3{X: 1, Y: 1, Z: 1},
	}
}

func resolveLightStyle(s LightStyle) LightStyle {
	if s.SkyColor == (math3d.Vec3{}) && s.BlockColor == (math3d.Vec3{}) {
		def := DefaultLightStyle()
		def.Ambient = s.Ambient
		return def
	}
	return s
}

// CombineLightColor evaluates the linear RGBA vertex color for a vertex with
// sky light (0..15), block light (0..15), and optional ambient occlusion scale
// (pass 1 when AO is disabled, or [AOFactor](ao) when enabled).
func CombineLightColor(sky, block uint8, aoScale float32, style LightStyle) math3d.Vec4 {
	style = resolveLightStyle(style)
	sf := LightFactor(sky)
	bf := LightFactor(block)
	amb := style.Ambient
	if amb < 0 {
		amb = 0
	}
	r := max(sf*style.SkyColor.X, bf*style.BlockColor.X, amb) * aoScale
	g := max(sf*style.SkyColor.Y, bf*style.BlockColor.Y, amb) * aoScale
	b := max(sf*style.SkyColor.Z, bf*style.BlockColor.Z, amb) * aoScale
	return math3d.Vec4{X: r, Y: g, Z: b, W: 1}
}

// SkyLight returns the sky light level (0..15) at world voxel coordinate pos.
// Voxels above the world return [MaxLightLevel] (15). Voxels below the lowest
// non-empty chunk return the sky light at the bottom of that column.
func (w *World) SkyLight(pos BlockCoord) uint8 {
	if w == nil || !w.hasMinChunkY {
		return MaxLightLevel
	}
	minY := w.minChunkY << w.shift
	if pos.Y < minY {
		pos.Y = minY
	}
	return unpackSky(w.lightByteRaw(pos))
}

// BlockLight returns the block light level (0..15) at world voxel coordinate pos.
func (w *World) BlockLight(pos BlockCoord) uint8 {
	if w == nil {
		return 0
	}
	return unpackBlock(w.lightByteRaw(pos))
}

// Light returns both the sky light and block light levels (0..15 each) at pos.
func (w *World) Light(pos BlockCoord) (sky, block uint8) {
	if w == nil {
		return MaxLightLevel, 0
	}
	if !w.hasMinChunkY {
		return MaxLightLevel, unpackBlock(w.lightByteRaw(pos))
	}
	minY := w.minChunkY << w.shift
	raw := w.lightByteRaw(pos)
	block = unpackBlock(raw)
	if pos.Y < minY {
		sky = unpackSky(w.lightByteRaw(BlockCoord{X: pos.X, Y: minY, Z: pos.Z}))
	} else {
		sky = unpackSky(raw)
	}
	return sky, block
}

func (w *World) lightByteAt(pos BlockCoord) uint8 {
	sky, blk := w.Light(pos)
	return packLight(sky, blk)
}

// lightByteRaw reads the stored packed light byte at pos without minY clamping.
func (w *World) lightByteRaw(pos BlockCoord) uint8 {
	cc, lx, ly, lz := w.BlockToChunk(pos)
	idx := (ly*w.chunkSize+lz)*w.chunkSize + lx
	if ch := w.chunks[cc]; ch != nil {
		return ch.light[idx]
	}
	if al := w.airLight[cc]; al != nil {
		return al.light[idx]
	}
	return defaultLightByte
}

func (w *World) skyLightRaw(pos BlockCoord) uint8 {
	return unpackSky(w.lightByteRaw(pos))
}

func (w *World) blockLightRaw(pos BlockCoord) uint8 {
	return unpackBlock(w.lightByteRaw(pos))
}

func (w *World) setLightByteRaw(pos BlockCoord, val uint8) {
	cc, lx, ly, lz := w.BlockToChunk(pos)
	idx := (ly*w.chunkSize+lz)*w.chunkSize + lx
	if ch := w.chunks[cc]; ch != nil {
		ch.light[idx] = val
		return
	}
	al := w.airLight[cc]
	if al == nil {
		if val == defaultLightByte {
			return
		}
		al = &airLightChunk{
			light: newLightSlice(w.chunkSize),
		}
		w.airLight[cc] = al
	}
	old := al.light[idx]
	if old == val {
		return
	}
	if old == defaultLightByte {
		al.nonDefault++
	} else if val == defaultLightByte {
		al.nonDefault--
	}
	al.light[idx] = val
	if al.nonDefault == 0 {
		delete(w.airLight, cc)
	}
}

func (w *World) setSkyTracked(pos BlockCoord, sky uint8) {
	old := w.lightByteRaw(pos)
	nw := (sky << 4) | (old & 0x0F)
	if old == nw {
		return
	}
	if _, seen := w.origLight[pos]; !seen {
		w.origLight[pos] = old
	}
	w.setLightByteRaw(pos, nw)
}

func (w *World) setBlockLightTracked(pos BlockCoord, blk uint8) {
	old := w.lightByteRaw(pos)
	nw := (old & 0xF0) | (blk & 0x0F)
	if old == nw {
		return
	}
	if _, seen := w.origLight[pos]; !seen {
		w.origLight[pos] = old
	}
	w.setLightByteRaw(pos, nw)
}

func (w *World) blockOpacityAt(pos BlockCoord) uint8 {
	id := w.Block(pos)
	return w.registry.LightOpacity(id)
}

func (w *World) blockEmissiveAt(pos BlockCoord) uint8 {
	id := w.Block(pos)
	return w.registry.Emissive(id)
}

// RecomputeLight recomputes sky light and block light across the entire world
// from scratch and marks any existing non-empty chunks whose light (or border
// neighbor light) changed as dirty.
func (w *World) RecomputeLight() {
	if w == nil {
		return
	}
	w.recomputeLightInternal(true)
}

func (w *World) recomputeLightInternal(markDirty bool) {
	// Snapshot existing light bytes if we need to mark only changed chunks dirty.
	var prevChunkLight map[ChunkCoord][]uint8
	var prevAirLight map[ChunkCoord][]uint8
	if markDirty {
		prevChunkLight = make(map[ChunkCoord][]uint8, len(w.chunks))
		for cc, ch := range w.chunks {
			snap := make([]uint8, len(ch.light))
			copy(snap, ch.light)
			prevChunkLight[cc] = snap
		}
		prevAirLight = make(map[ChunkCoord][]uint8, len(w.airLight))
		for cc, al := range w.airLight {
			snap := make([]uint8, len(al.light))
			copy(snap, al.light)
			prevAirLight[cc] = snap
		}
	}

	w.recomputeMinChunkY()
	w.recomputeSkyLightCore()
	w.recomputeBlockLightCore()

	if markDirty {
		w.markChunksChangedFromSnapshot(prevChunkLight, prevAirLight)
	}
}

func (w *World) recomputeMinChunkY() {
	w.hasMinChunkY = false
	w.minChunkY = 0
	for cc, ch := range w.chunks {
		if ch.nonAir == 0 {
			continue
		}
		if !w.hasMinChunkY || cc.Y < w.minChunkY {
			w.hasMinChunkY = true
			w.minChunkY = cc.Y
		}
	}
}

func (w *World) recomputeSkyLightCore() {
	// Reset sky nibble to 15 everywhere (preserving block nibbles).
	for _, ch := range w.chunks {
		for i, b := range ch.light {
			ch.light[i] = (MaxLightLevel << 4) | (b & 0x0F)
		}
	}
	for cc, al := range w.airLight {
		nonDef := 0
		for i, b := range al.light {
			nb := (MaxLightLevel << 4) | (b & 0x0F)
			al.light[i] = nb
			if nb != defaultLightByte {
				nonDef++
			}
		}
		al.nonDefault = nonDef
		if nonDef == 0 {
			delete(w.airLight, cc)
		}
	}

	if !w.hasMinChunkY {
		return
	}

	// Find the highest non-empty chunk Y per (cx, cz) column.
	colMaxY := make(map[chunkColumn]int)
	for cc, ch := range w.chunks {
		if ch.nonAir == 0 {
			continue
		}
		col := chunkColumn{x: cc.X, z: cc.Z}
		if cur, ok := colMaxY[col]; !ok || cc.Y > cur {
			colMaxY[col] = cc.Y
		}
	}

	n := w.chunkSize
	minY := w.minChunkY << w.shift
	w.lightAddQ = w.lightAddQ[:0]

	// Pass 1: trace direct sunlight straight down each active column; mark every
	// voxel at or below the first light-attenuating block as sky=0.
	type shadowedSpan struct {
		x, z      int
		topBlockY int
	}
	var shadowed []shadowedSpan

	for col, maxCY := range colMaxY {
		baseX := col.x << w.shift
		baseZ := col.z << w.shift
		topY := ((maxCY + 1) << w.shift) - 1
		for lz := range n {
			wz := baseZ + lz
			for lx := range n {
				wx := baseX + lx
				blocked := false
				topBlockY := 0
				for y := topY; y >= minY; y-- {
					pos := BlockCoord{X: wx, Y: y, Z: wz}
					if !blocked {
						if w.blockOpacityAt(pos) > 0 {
							blocked = true
							topBlockY = y
							old := w.lightByteRaw(pos)
							w.setLightByteRaw(pos, old&0x0F)
						}
					} else {
						old := w.lightByteRaw(pos)
						w.setLightByteRaw(pos, old&0x0F)
					}
				}
				if blocked {
					shadowed = append(shadowed, shadowedSpan{x: wx, z: wz, topBlockY: topBlockY})
				}
			}
		}
	}

	// Pass 2: seed BFS with every shadowed voxel that is permeable (op < 15) and
	// touches any neighbor (with y >= minY) that has direct sky light 15.
	for _, span := range shadowed {
		for y := span.topBlockY; y >= minY; y-- {
			pos := BlockCoord{X: span.x, Y: y, Z: span.z}
			op := w.blockOpacityAt(pos)
			if op >= MaxLightLevel {
				continue
			}
			c := op
			if c < 1 {
				c = 1
			}
			if MaxLightLevel <= c {
				continue
			}
			hasDirect := false
			for _, step := range faceSteps {
				nb := pos.Add(step)
				if nb.Y < minY {
					continue
				}
				if w.skyLightRaw(nb) == MaxLightLevel {
					hasDirect = true
					break
				}
			}
			if hasDirect {
				cand := MaxLightLevel - c
				old := w.lightByteRaw(pos)
				w.setLightByteRaw(pos, packLight(cand, unpackBlock(old)))
				w.lightAddQ = append(w.lightAddQ, pos)
			}
		}
	}

	// Pass 3: flood fill sky light across all shadowed regions.
	w.runSkyAddBFS(minY, false)
}

func (w *World) recomputeBlockLightCore() {
	// Reset block nibble to 0 everywhere (preserving sky nibbles).
	for _, ch := range w.chunks {
		for i, b := range ch.light {
			ch.light[i] = b & 0xF0
		}
	}
	for cc, al := range w.airLight {
		nonDef := 0
		for i, b := range al.light {
			nb := b & 0xF0
			al.light[i] = nb
			if nb != defaultLightByte {
				nonDef++
			}
		}
		al.nonDefault = nonDef
		if nonDef == 0 {
			delete(w.airLight, cc)
		}
	}

	n := w.chunkSize
	w.lightAddQ = w.lightAddQ[:0]

	for cc, ch := range w.chunks {
		if ch.nonAir == 0 {
			continue
		}
		origin := w.ChunkOrigin(cc)
		for ly := range n {
			for lz := range n {
				rowOff := (ly*n + lz) * n
				for lx := range n {
					id := ch.blocks[rowOff+lx]
					if id == Air {
						continue
					}
					em := w.registry.Emissive(id)
					if em == 0 {
						continue
					}
					idx := rowOff + lx
					ch.light[idx] = (ch.light[idx] & 0xF0) | em
					w.lightAddQ = append(w.lightAddQ, BlockCoord{
						X: origin.X + lx,
						Y: origin.Y + ly,
						Z: origin.Z + lz,
					})
				}
			}
		}
	}

	w.runBlockAddBFS(false)
}

func (w *World) markChunksChangedFromSnapshot(prevChunkLight, prevAirLight map[ChunkCoord][]uint8) {
	n := w.chunkSize
	checkChunk := func(cc ChunkCoord, cur, prev []uint8) {
		origin := w.ChunkOrigin(cc)
		for ly := range n {
			for lz := range n {
				rowOff := (ly*n + lz) * n
				for lx := range n {
					idx := rowOff + lx
					var a, b uint8 = defaultLightByte, defaultLightByte
					if cur != nil {
						a = cur[idx]
					}
					if prev != nil {
						b = prev[idx]
					}
					if a != b {
						w.markLightAffectedChunks(BlockCoord{
							X: origin.X + lx,
							Y: origin.Y + ly,
							Z: origin.Z + lz,
						})
					}
				}
			}
		}
	}

	visited := make(map[ChunkCoord]struct{}, len(w.chunks)+len(w.airLight)+len(prevChunkLight)+len(prevAirLight))
	for cc, ch := range w.chunks {
		visited[cc] = struct{}{}
		prev := prevChunkLight[cc]
		if prev == nil {
			prev = prevAirLight[cc]
		}
		checkChunk(cc, ch.light, prev)
	}
	for cc, al := range w.airLight {
		if _, done := visited[cc]; done {
			continue
		}
		visited[cc] = struct{}{}
		prev := prevAirLight[cc]
		if prev == nil {
			prev = prevChunkLight[cc]
		}
		checkChunk(cc, al.light, prev)
	}
	for cc, prev := range prevChunkLight {
		if _, done := visited[cc]; done {
			continue
		}
		visited[cc] = struct{}{}
		checkChunk(cc, nil, prev)
	}
	for cc, prev := range prevAirLight {
		if _, done := visited[cc]; done {
			continue
		}
		checkChunk(cc, nil, prev)
	}
}

// updateLightOnEdit incrementally updates sky and block light after pos changed
// from oldID to newID, marking only chunks whose mesh lighting changed dirty.
func (w *World) updateLightOnEdit(pos BlockCoord, oldID, newID BlockID, skyMinChanged bool) {
	oldOp := w.registry.LightOpacity(oldID)
	newOp := w.registry.LightOpacity(newID)
	oldEm := w.registry.Emissive(oldID)
	newEm := w.registry.Emissive(newID)

	if !skyMinChanged && oldOp == newOp && oldEm == newEm {
		return
	}

	if w.origLight == nil {
		w.origLight = make(map[BlockCoord]uint8, 64)
	}

	// 1. Incremental block light update.
	if oldOp != newOp || oldEm != newEm {
		w.updateBlockLightOnEdit(pos, oldOp, newOp, oldEm, newEm)
	}

	// 2. Sky light update (full sky recompute only if the world's global minChunkY shifted).
	if skyMinChanged {
		// Flush tracked block light dirties first, then recompute sky light with diffing.
		w.flushTrackedLightDirty()

		prevChunkLight := make(map[ChunkCoord][]uint8, len(w.chunks))
		for cc, ch := range w.chunks {
			snap := make([]uint8, len(ch.light))
			copy(snap, ch.light)
			prevChunkLight[cc] = snap
		}
		prevAirLight := make(map[ChunkCoord][]uint8, len(w.airLight))
		for cc, al := range w.airLight {
			snap := make([]uint8, len(al.light))
			copy(snap, al.light)
			prevAirLight[cc] = snap
		}
		w.recomputeSkyLightCore()
		w.markChunksChangedFromSnapshot(prevChunkLight, prevAirLight)
		return
	}

	if oldOp != newOp && w.hasMinChunkY {
		w.updateSkyLightOnEdit(pos, oldOp, newOp)
	}

	w.flushTrackedLightDirty()
}

func (w *World) flushTrackedLightDirty() {
	if len(w.origLight) == 0 {
		return
	}
	for pos, orig := range w.origLight {
		if w.lightByteRaw(pos) != orig {
			w.markLightAffectedChunks(pos)
		}
	}
	clear(w.origLight)
}

func (w *World) updateBlockLightOnEdit(pos BlockCoord, oldOp, newOp, oldEm, newEm uint8) {
	w.lightAddQ = w.lightAddQ[:0]
	w.lightRemQ = w.lightRemQ[:0]
	w.lightSeed = w.lightSeed[:0]

	oldBlk := w.blockLightRaw(pos)

	if newOp > oldOp || newEm < oldEm {
		// Light may decrease: remove dependent light first, then re-seed and flood.
		if oldBlk > 0 {
			w.setBlockLightTracked(pos, 0)
			w.lightRemQ = append(w.lightRemQ, lightRemNode{pos: pos, val: oldBlk})
			w.runBlockRemBFS()
		}
		if newEm > 0 {
			w.lightSeed = append(w.lightSeed, pos)
		}
		if newOp < MaxLightLevel {
			for _, step := range faceSteps {
				nb := pos.Add(step)
				if w.blockLightRaw(nb) > 1 {
					w.lightAddQ = append(w.lightAddQ, nb)
				}
			}
		}
		for _, s := range w.lightSeed {
			em := w.blockEmissiveAt(s)
			if em > w.blockLightRaw(s) {
				w.setBlockLightTracked(s, em)
				w.lightAddQ = append(w.lightAddQ, s)
			}
		}
		if len(w.lightAddQ) > 0 {
			w.runBlockAddBFS(true)
		}
		return
	}

	// Monotonic increase case (newOp <= oldOp && newEm >= oldEm).
	if newEm > oldBlk {
		w.setBlockLightTracked(pos, newEm)
		w.lightAddQ = append(w.lightAddQ, pos)
	}
	if newOp < oldOp && newOp < MaxLightLevel {
		for _, step := range faceSteps {
			nb := pos.Add(step)
			if w.blockLightRaw(nb) > 1 {
				w.lightAddQ = append(w.lightAddQ, nb)
			}
		}
	}
	if len(w.lightAddQ) > 0 {
		w.runBlockAddBFS(true)
	}
}

func (w *World) runBlockRemBFS() {
	for head := 0; head < len(w.lightRemQ); head++ {
		node := w.lightRemQ[head]
		uCanReceive := w.blockOpacityAt(node.pos) < MaxLightLevel
		for _, step := range faceSteps {
			nb := node.pos.Add(step)
			nbVal := w.blockLightRaw(nb)
			if nbVal == 0 {
				continue
			}
			if nbVal < node.val {
				w.setBlockLightTracked(nb, 0)
				w.lightRemQ = append(w.lightRemQ, lightRemNode{pos: nb, val: nbVal})
				if w.blockEmissiveAt(nb) > 0 {
					w.lightSeed = append(w.lightSeed, nb)
				}
			} else if uCanReceive {
				w.lightAddQ = append(w.lightAddQ, nb)
			}
		}
	}
	w.lightRemQ = w.lightRemQ[:0]
}

func (w *World) runBlockAddBFS(tracked bool) {
	for head := 0; head < len(w.lightAddQ); head++ {
		u := w.lightAddQ[head]
		lu := w.blockLightRaw(u)
		if lu <= 1 {
			continue
		}
		for _, step := range faceSteps {
			v := u.Add(step)
			op := w.blockOpacityAt(v)
			if op >= MaxLightLevel {
				continue
			}
			c := op
			if c < 1 {
				c = 1
			}
			if lu <= c {
				continue
			}
			cand := lu - c
			if cand > w.blockLightRaw(v) {
				if tracked {
					w.setBlockLightTracked(v, cand)
				} else {
					old := w.lightByteRaw(v)
					w.setLightByteRaw(v, (old&0xF0)|cand)
				}
				w.lightAddQ = append(w.lightAddQ, v)
			}
		}
	}
	w.lightAddQ = w.lightAddQ[:0]
}

func (w *World) updateSkyLightOnEdit(pos BlockCoord, oldOp, newOp uint8) {
	minY := w.minChunkY << w.shift
	if pos.Y < minY {
		return
	}

	w.lightAddQ = w.lightAddQ[:0]
	w.lightRemQ = w.lightRemQ[:0]

	if newOp > oldOp {
		oldSky := w.skyLightRaw(pos)
		if oldSky > 0 {
			w.setSkyTracked(pos, 0)
			w.lightRemQ = append(w.lightRemQ, lightRemNode{pos: pos, val: oldSky})
			w.runSkyRemBFS(minY)
		}
		if newOp < MaxLightLevel {
			for _, step := range faceSteps {
				nb := pos.Add(step)
				if nb.Y < minY {
					continue
				}
				if w.skyLightRaw(nb) > 0 {
					w.lightAddQ = append(w.lightAddQ, nb)
				}
			}
		}
		if len(w.lightAddQ) > 0 {
			w.runSkyAddBFS(minY, true)
		}
		return
	}

	// newOp < oldOp: sky light can only increase.
	for _, step := range faceSteps {
		nb := pos.Add(step)
		if nb.Y < minY {
			continue
		}
		if w.skyLightRaw(nb) > 0 {
			w.lightAddQ = append(w.lightAddQ, nb)
		}
	}
	if len(w.lightAddQ) > 0 {
		w.runSkyAddBFS(minY, true)
	}
}

func (w *World) runSkyRemBFS(minY int) {
	for head := 0; head < len(w.lightRemQ); head++ {
		node := w.lightRemQ[head]
		uCanReceive := w.blockOpacityAt(node.pos) < MaxLightLevel
		for dir, step := range faceSteps {
			nb := node.pos.Add(step)
			if nb.Y < minY {
				continue
			}
			nbSky := w.skyLightRaw(nb)
			if nbSky == 0 {
				continue
			}
			if Face(dir) == FaceNegY && node.val == MaxLightLevel && nbSky == MaxLightLevel {
				// Direct sunlight column going straight down loses sky=15.
				w.setSkyTracked(nb, 0)
				w.lightRemQ = append(w.lightRemQ, lightRemNode{pos: nb, val: MaxLightLevel})
			} else if nbSky < node.val {
				w.setSkyTracked(nb, 0)
				w.lightRemQ = append(w.lightRemQ, lightRemNode{pos: nb, val: nbSky})
			} else if uCanReceive {
				w.lightAddQ = append(w.lightAddQ, nb)
			}
		}
	}
	w.lightRemQ = w.lightRemQ[:0]
}

func (w *World) runSkyAddBFS(minY int, tracked bool) {
	for head := 0; head < len(w.lightAddQ); head++ {
		u := w.lightAddQ[head]
		lu := w.skyLightRaw(u)
		if lu <= 1 {
			continue
		}
		for dir, step := range faceSteps {
			v := u.Add(step)
			if v.Y < minY {
				continue
			}
			op := w.blockOpacityAt(v)
			if op >= MaxLightLevel {
				continue
			}
			var cand uint8
			if Face(dir) == FaceNegY && lu == MaxLightLevel && op == 0 {
				cand = MaxLightLevel
			} else {
				c := op
				if c < 1 {
					c = 1
				}
				if lu <= c {
					continue
				}
				cand = lu - c
			}
			if cand > w.skyLightRaw(v) {
				if tracked {
					w.setSkyTracked(v, cand)
				} else {
					old := w.lightByteRaw(v)
					w.setLightByteRaw(v, packLight(cand, unpackBlock(old)))
				}
				w.lightAddQ = append(w.lightAddQ, v)
			}
		}
	}
	w.lightAddQ = w.lightAddQ[:0]
}

// markLightAffectedChunks marks any existing non-empty chunk whose mesh vertex
// lighting (flat or smooth) can sample pos.
func (w *World) markLightAffectedChunks(pos BlockCoord) {
	cc, lx, ly, lz := w.BlockToChunk(pos)
	if ch := w.chunks[cc]; ch != nil && ch.nonAir > 0 {
		w.markDirtyChunk(ch)
	}

	n := w.chunkSize
	var dx, dy, dz int
	if lx == 0 {
		dx = -1
	} else if lx == n-1 {
		dx = 1
	}
	if ly == 0 {
		dy = -1
	} else if ly == n-1 {
		dy = 1
	}
	if lz == 0 {
		dz = -1
	} else if lz == n-1 {
		dz = 1
	}
	if dx == 0 && dy == 0 && dz == 0 {
		return
	}

	// Mark face-, edge-, and corner-adjacent existing non-empty chunks so both
	// flat and smooth cross-chunk vertex lighting rebuild cleanly.
	oxs := [2]int{0, dx}
	oys := [2]int{0, dy}
	ozs := [2]int{0, dz}
	nx := 1
	if dx != 0 {
		nx = 2
	}
	ny := 1
	if dy != 0 {
		ny = 2
	}
	nz := 1
	if dz != 0 {
		nz = 2
	}
	for ix := range nx {
		for iy := range ny {
			for iz := range nz {
				if ix == 0 && iy == 0 && iz == 0 {
					continue
				}
				ncc := ChunkCoord{X: cc.X + oxs[ix], Y: cc.Y + oys[iy], Z: cc.Z + ozs[iz]}
				if nch := w.chunks[ncc]; nch != nil && nch.nonAir > 0 {
					w.markDirtyChunk(nch)
				}
			}
		}
	}
}
