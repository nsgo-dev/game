package physics

// LayerMask is a 32-bit bitmask identifying collision layers or collision filter masks.
// A zero LayerMask defaults to LayerAll so bodies, triggers and queries with unset
// layer fields remain compatible with unfiltered collision checks.
type LayerMask uint32

// LayerAll matches every collision layer.
const LayerAll LayerMask = ^LayerMask(0)

// LayerBit returns the LayerMask with bit index set, or 0 when index >= 32.
func LayerBit(index uint) LayerMask {
	if index >= 32 {
		return 0
	}
	return LayerMask(1) << index
}

func effectiveLayer(m LayerMask) LayerMask {
	if m == 0 {
		return LayerAll
	}
	return m
}

func layersInteract(layerA, maskA, layerB, maskB LayerMask) bool {
	la := effectiveLayer(layerA)
	ma := effectiveLayer(maskA)
	lb := effectiveLayer(layerB)
	mb := effectiveLayer(maskB)
	return (la&mb) != 0 && (lb&ma) != 0
}

func (b *Body) layer() LayerMask {
	return effectiveLayer(b.Layer)
}

func (b *Body) mask() LayerMask {
	return effectiveLayer(b.Mask)
}

// CanCollide reports whether bodies a and b are allowed to collide based on their Layer and Mask fields.
// Zero Layer or Mask values default to LayerAll.
func CanCollide(a, b *Body) bool {
	if a == nil || b == nil {
		return false
	}
	return (a.layer()&b.mask()) != 0 && (b.layer()&a.mask()) != 0
}
