package navbake3d

import "math"

// notConnected marks a missing neighbour link of a compact span.
const notConnected int32 = -1

// cspan is one walkable span of a compact heightfield: the floor at y cell heights with h cells of clearance above it, its links to the walkable spans an agent can step to in each grid direction, and the region it belongs to.
type cspan struct {
	y, h int32
	con  [4]int32 // index of the neighbour span in each direction, or notConnected
	reg  int32
}

// compactHeightfield holds only the walkable tops of a heightfield, column by column, with links between neighbours an agent can move between.
type compactHeightfield struct {
	width, depth int
	bmin         [3]float32
	cs, ch       float32
	cellIndex    []int32 // first span of each column
	cellCount    []int32 // number of spans in each column
	spans        []cspan
	areas        []uint8
	maxRegions   int32
}

// buildCompact keeps hf's walkable spans and links each to the neighbours an agent of height cells fits between and climb cells steps to.
func buildCompact(hf *heightfield, height, climb int32) *compactHeightfield {
	chf := &compactHeightfield{
		width: hf.width, depth: hf.depth,
		bmin: [3]float32{hf.bmin.X, hf.bmin.Y, hf.bmin.Z},
		cs:   hf.cs, ch: hf.ch,
		cellIndex: make([]int32, hf.width*hf.depth),
		cellCount: make([]int32, hf.width*hf.depth),
	}
	for col, head := range hf.heads {
		chf.cellIndex[col] = int32(len(chf.spans))
		for i := head; i >= 0; i = hf.pool[i].next {
			s := hf.pool[i]
			if s.area == areaNull {
				continue
			}
			top := int32(spanMaxHeight)
			if s.next >= 0 {
				top = hf.pool[s.next].smin
			}
			chf.spans = append(chf.spans, cspan{y: s.smax, h: top - s.smax, con: [4]int32{notConnected, notConnected, notConnected, notConnected}})
			chf.areas = append(chf.areas, s.area)
			chf.cellCount[col]++
		}
	}
	for z := 0; z < chf.depth; z++ {
		for x := 0; x < chf.width; x++ {
			col := x + z*chf.width
			for i := chf.cellIndex[col]; i < chf.cellIndex[col]+chf.cellCount[col]; i++ {
				s := &chf.spans[i]
				for dir := range 4 {
					nx := x + dirOffsetX[dir]
					nz := z + dirOffsetZ[dir]
					if nx < 0 || nz < 0 || nx >= chf.width || nz >= chf.depth {
						continue
					}
					ncol := nx + nz*chf.width
					for k := chf.cellIndex[ncol]; k < chf.cellIndex[ncol]+chf.cellCount[ncol]; k++ {
						ns := &chf.spans[k]
						bot := max(s.y, ns.y)
						top := min(s.y+s.h, ns.y+ns.h)
						if top-bot >= height && abs32(ns.y-s.y) <= climb {
							s.con[dir] = k
							break
						}
					}
				}
			}
		}
	}
	return chf
}

// erode removes the walkable spans within radius cells of a wall, a ledge or the edge of the walkable area, using a chamfer distance field (2 per straight step, 3 per diagonal one).
func (chf *compactHeightfield) erode(radius int32) {
	n := len(chf.spans)
	dist := make([]int32, n)
	for i := range dist {
		dist[i] = math.MaxInt32 / 2
	}
	for i := range chf.spans {
		if chf.areas[i] == areaNull {
			dist[i] = 0
			continue
		}
		nc := 0
		for dir := range 4 {
			if a := chf.spans[i].con[dir]; a != notConnected && chf.areas[a] != areaNull {
				nc++
			}
		}
		if nc != 4 {
			dist[i] = 0
		}
	}
	relax := func(i int32, dir, diag int) {
		a := chf.spans[i].con[dir]
		if a == notConnected {
			return
		}
		dist[i] = min(dist[i], dist[a]+2)
		if aa := chf.spans[a].con[diag]; aa != notConnected {
			dist[i] = min(dist[i], dist[aa]+3)
		}
	}
	for z := 0; z < chf.depth; z++ {
		for x := 0; x < chf.width; x++ {
			col := x + z*chf.width
			for i := chf.cellIndex[col]; i < chf.cellIndex[col]+chf.cellCount[col]; i++ {
				relax(i, 0, 3)
				relax(i, 3, 2)
			}
		}
	}
	for z := chf.depth - 1; z >= 0; z-- {
		for x := chf.width - 1; x >= 0; x-- {
			col := x + z*chf.width
			for i := chf.cellIndex[col]; i < chf.cellIndex[col]+chf.cellCount[col]; i++ {
				relax(i, 2, 1)
				relax(i, 1, 0)
			}
		}
	}
	thr := radius * 2
	for i := range chf.spans {
		if dist[i] < thr {
			chf.areas[i] = areaNull
		}
	}
}

// buildRegions partitions the walkable spans into monotone regions: each grid row is swept into runs, and a run continues the region of the run above it only when the two are each other's only link, so every region is a stack of runs without holes. Islands of connected regions smaller than minArea cells are removed.
func (chf *compactHeightfield) buildRegions(minArea int) {
	type sweep struct {
		rid, id, ns, nei int32
	}
	const nullNei int32 = -1
	for i := range chf.spans {
		chf.spans[i].reg = 0
	}
	id := int32(1)
	var prev []int32
	sweeps := make([]sweep, chf.width+1)
	for z := 0; z < chf.depth; z++ {
		if need := int(id) + chf.width + 1; len(prev) < need {
			prev = append(prev, make([]int32, need-len(prev))...)
		}
		for i := range prev[:id] {
			prev[i] = 0
		}
		rid := int32(1)
		for x := 0; x < chf.width; x++ {
			col := x + z*chf.width
			for i := chf.cellIndex[col]; i < chf.cellIndex[col]+chf.cellCount[col]; i++ {
				if chf.areas[i] == areaNull {
					continue
				}
				s := &chf.spans[i]
				previd := int32(0)
				if a := s.con[0]; a != notConnected && chf.spans[a].reg != 0 && chf.areas[a] == chf.areas[i] {
					previd = chf.spans[a].reg
				}
				if previd == 0 {
					previd = rid
					rid++
					if int(rid) >= len(sweeps) {
						sweeps = append(sweeps, make([]sweep, len(sweeps))...)
					}
					sweeps[previd] = sweep{rid: previd}
				}
				if a := s.con[3]; a != notConnected && chf.spans[a].reg != 0 && chf.areas[a] == chf.areas[i] {
					nr := chf.spans[a].reg
					sw := &sweeps[previd]
					if sw.nei == 0 || sw.nei == nr {
						sw.nei = nr
						sw.ns++
						prev[nr]++
					} else {
						sw.nei = nullNei
					}
				}
				s.reg = previd
			}
		}
		for i := int32(1); i < rid; i++ {
			sw := &sweeps[i]
			if sw.nei != nullNei && sw.nei != 0 && prev[sw.nei] == sw.ns {
				sw.id = sw.nei
			} else {
				sw.id = id
				id++
			}
		}
		for x := 0; x < chf.width; x++ {
			col := x + z*chf.width
			for i := chf.cellIndex[col]; i < chf.cellIndex[col]+chf.cellCount[col]; i++ {
				if r := chf.spans[i].reg; r > 0 && r < rid {
					chf.spans[i].reg = sweeps[r].id
				}
			}
		}
	}
	chf.maxRegions = id
	chf.removeSmallIslands(minArea)
}

// removeSmallIslands clears the regions of every island of linked regions with fewer than minArea spans in all, then renumbers the regions left from 1 in order of first appearance.
func (chf *compactHeightfield) removeSmallIslands(minArea int) {
	n := int(chf.maxRegions)
	area := make([]int, n)
	parent := make([]int32, n)
	for i := range parent {
		parent[i] = int32(i)
	}
	var find func(int32) int32
	find = func(r int32) int32 {
		for parent[r] != r {
			parent[r] = parent[parent[r]]
			r = parent[r]
		}
		return r
	}
	for i := range chf.spans {
		r := chf.spans[i].reg
		if r == 0 {
			continue
		}
		area[r]++
		for dir := range 4 {
			a := chf.spans[i].con[dir]
			if a == notConnected {
				continue
			}
			if ar := chf.spans[a].reg; ar != 0 && ar != r {
				ra, rb := find(r), find(ar)
				if ra != rb {
					if ra < rb {
						parent[rb] = ra
					} else {
						parent[ra] = rb
					}
				}
			}
		}
	}
	island := make([]int, n)
	for r := 1; r < n; r++ {
		island[find(int32(r))] += area[r]
	}
	remap := make([]int32, n)
	next := int32(1)
	for i := range chf.spans {
		r := chf.spans[i].reg
		if r == 0 {
			continue
		}
		if island[find(r)] < minArea {
			chf.spans[i].reg = 0
			continue
		}
		if remap[r] == 0 {
			remap[r] = next
			next++
		}
		chf.spans[i].reg = remap[r]
	}
	chf.maxRegions = next
}
