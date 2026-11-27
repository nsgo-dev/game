package wire

import (
	"cmp"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// InspectOptions configures schema-driven packet inspection.
type InspectOptions struct {
	// ResolveDeltas tracks per-peer replication baselines across a capture so SnapshotDelta layers include full resolved entity states.
	ResolveDeltas bool
}

// DecodedLayer is one protocol layer decoded from a packet using the wire schemas.
type DecodedLayer struct {
	// Layer names the protocol layer ("transport", "udp_sealed", "relay", "replication", "rollback", "membership", "handshake", a custom schema package, or "raw").
	Layer string `yaml:"layer" json:"layer"`
	// Union names the discriminated wire union when this layer was selected by a union discriminator.
	Union string `yaml:"union,omitempty" json:"union,omitempty"`
	// Version is the nibble discriminator high value when a nibble union matched (for example 2 for ReplicationMessage).
	Version *uint8 `yaml:"version,omitempty" json:"version,omitempty"`
	// Case is the matched union case name (for example "full", "delta", "single_input", "reliable").
	Case string `yaml:"case,omitempty" json:"case,omitempty"`
	// Tag is the union discriminator tag value when Union is set.
	Tag *uint64 `yaml:"tag,omitempty" json:"tag,omitempty"`
	// Message is the wire message name decoded at this layer.
	Message string `yaml:"message,omitempty" json:"message,omitempty"`
	// Fields holds the decoded fields of Message in exact wire order. Trailing payload bytes peeled into the next layer are omitted here; ReencodeLayers restores them from the inner layer.
	Fields DecodedObject `yaml:"fields,omitempty" json:"fields,omitempty"`
	// Raw holds raw undecoded bytes when Layer is "raw".
	Raw WireBytes `yaml:"raw,omitempty" json:"raw,omitempty"`
	// Resolved holds the full resulting entity states after applying a SnapshotDelta to its tracked peer baseline when ResolveDeltas is enabled.
	Resolved []DecodedObject `yaml:"resolved,omitempty" json:"resolved,omitempty"`
	// BaselineMissing is true when ResolveDeltas was enabled on a SnapshotDelta whose baseline_tick had not been captured yet.
	BaselineMissing bool `yaml:"baseline_missing,omitempty" json:"baseline_missing,omitempty"`

	// PayloadField records which trailing bytes field of Message was peeled off into the next layer so ReencodeLayers can reconstruct exact wire bytes.
	PayloadField string `yaml:"-" json:"-"`
}

// DecodedFrame is one fully decoded packet from a capture stream or raw frame input.
type DecodedFrame struct {
	// Index is the 0-based frame index in the input stream.
	Index int `yaml:"index" json:"index"`
	// T is the capture timestamp in nanoseconds (nil for raw stdin frames).
	T *int64 `yaml:"t,omitempty" json:"t,omitempty"`
	// Dir is "send" or "recv" (empty for raw stdin frames).
	Dir string `yaml:"dir,omitempty" json:"dir,omitempty"`
	// Peer is the remote peer ID (nil for raw stdin frames).
	Peer *uint32 `yaml:"peer,omitempty" json:"peer,omitempty"`
	// Size is the payload size in bytes.
	Size int `yaml:"size" json:"size"`
	// Layers lists the decoded protocol layers from outermost to innermost.
	Layers []DecodedLayer `yaml:"layers" json:"layers"`
}

type baselineKey struct {
	dir   string
	peer  uint32
	epoch uint32
}

type partialFullKey struct {
	dir   string
	peer  uint32
	epoch uint32
	tick  uint32
}

type partialFullState struct {
	total    uint32
	received map[uint32]bool
	entities map[uint32]EntityRecord
}

// Inspector decodes captured packets and raw wire frames layer by layer using a Registry of built-in and optional custom wire schemas, and re-encodes decoded frames back to exact wire bytes.
type Inspector struct {
	reg  *Registry
	opts InspectOptions

	baselines map[baselineKey]map[uint32]map[uint32]EntityRecord
	partials  map[partialFullKey]*partialFullState
}

// NewInspector returns a schema-driven Inspector. When reg is nil, NewRegistry() is called to load the built-in pkg/online/netplay schemas.
func NewInspector(reg *Registry, opts InspectOptions) (*Inspector, error) {
	if reg == nil {
		var err error
		reg, err = NewRegistry()
		if err != nil {
			return nil, err
		}
	}
	return &Inspector{
		reg:       reg,
		opts:      opts,
		baselines: make(map[baselineKey]map[uint32]map[uint32]EntityRecord),
		partials:  make(map[partialFullKey]*partialFullState),
	}, nil
}

// Registry returns the schema registry backing this inspector.
func (ins *Inspector) Registry() *Registry { return ins.reg }

// InspectCaptureRecord decodes one captured transport packet into a layered DecodedFrame.
// The transport layer is populated from ch and seq using transport.wire.yaml, and payload is decoded layer by layer using the wire discriminators.
func (ins *Inspector) InspectCaptureRecord(index int, t int64, dir string, peer uint32, ch uint8, seq uint32, payload []byte) DecodedFrame {
	tCopy := t
	pCopy := peer
	chEnum := ins.channelEnum(ch)
	transportLayer := DecodedLayer{
		Layer:   "transport",
		Message: "TransportFrame",
		Fields: DecodedObject{
			{Name: "channel", Value: chEnum},
			{Name: "seq", Value: uint64(seq)},
		},
		PayloadField: "payload",
	}
	inner := ins.decodePayloadLayers(payload)
	layers := make([]DecodedLayer, 0, 1+len(inner))
	layers = append(layers, transportLayer)
	layers = append(layers, inner...)

	ins.postProcessLayers(dir, peer, layers)

	return DecodedFrame{
		Index:  index,
		T:      &tCopy,
		Dir:    dir,
		Peer:   &pCopy,
		Size:   len(payload),
		Layers: layers,
	}
}

// InspectRawFrame decodes a standalone raw wire frame (such as hex or base64 read from stdin) into a layered DecodedFrame.
func (ins *Inspector) InspectRawFrame(index int, raw []byte) DecodedFrame {
	layers := ins.decodeRawLayers(raw)
	ins.postProcessLayers("raw", 0, layers)
	return DecodedFrame{
		Index:  index,
		Size:   len(raw),
		Layers: layers,
	}
}

func (ins *Inspector) channelEnum(ch uint8) DecodedEnum {
	if se, ok := ins.reg.enums["Channel"]; ok {
		for _, ev := range se.def.Values {
			if ev.Value == uint64(ch) {
				return DecodedEnum{Name: ev.Name, Value: ev.Value}
			}
		}
	}
	return DecodedEnum{Name: fmt.Sprintf("%d", ch), Value: uint64(ch)}
}

// decodeRawLayers handles standalone wire frames that may begin with UDPSealedDatagram, TransportFrame, RelayFrame, or an application union.
func (ins *Inspector) decodeRawLayers(raw []byte) []DecodedLayer {
	if len(raw) == 0 {
		return []DecodedLayer{{Layer: "raw", Raw: WireBytes{}}}
	}
	// 1. UDPSealedDatagram (leading byte 0xB1..0xB6); ciphertext stays opaque without session keys.
	if raw[0] >= 0xB1 && raw[0] <= 0xB6 {
		if fields, n, err := ins.reg.DecodeMessage("UDPSealedDatagram", raw); err == nil && n == len(raw) {
			return []DecodedLayer{{
				Layer:   "udp_sealed",
				Message: "UDPSealedDatagram",
				Fields:  fields,
			}}
		}
	}

	// 2. Disambiguate RelayFrame (leading byte 0x01) vs TransportFrame (leading byte 0x00 or 0x01).
	if raw[0] == 1 {
		if relayLayers, ok := ins.tryDecodeRelay(raw, true); ok {
			return relayLayers
		}
	}
	if (raw[0] == 0 || raw[0] == 1) && len(raw) >= 5 {
		if fields, n, err := ins.reg.DecodeMessage("TransportFrame", raw); err == nil && n == len(raw) {
			pl, _ := fields.Get("payload")
			plBytes, _ := pl.(WireBytes)
			inner := ins.decodePayloadLayers(plBytes)
			if len(inner) > 0 && inner[0].Layer != "raw" {
				layer := DecodedLayer{
					Layer:        "transport",
					Message:      "TransportFrame",
					Fields:       fields.Without("payload"),
					PayloadField: "payload",
				}
				return append([]DecodedLayer{layer}, inner...)
			}
		}
	}
	if raw[0] == 1 {
		if relayLayers, ok := ins.tryDecodeRelay(raw, false); ok {
			return relayLayers
		}
	}

	// 3. Direct application / handshake / custom schema payload.
	return ins.decodePayloadLayers(raw)
}

// decodePayloadLayers decodes a transport payload through relay -> application -> custom schema layers.
func (ins *Inspector) decodePayloadLayers(payload []byte) []DecodedLayer {
	if len(payload) == 0 {
		return []DecodedLayer{{Layer: "raw", Raw: WireBytes{}}}
	}
	// Relay frame (leading byte 0x01).
	if payload[0] == 1 {
		if layers, ok := ins.tryDecodeRelay(payload, false); ok {
			return layers
		}
	}
	// Application / handshake / custom layer.
	if layer, ok := ins.tryDecodeAppLayer(payload); ok {
		return []DecodedLayer{layer}
	}
	return []DecodedLayer{{Layer: "raw", Raw: WireBytes(slices.Clone(payload))}}
}

func (ins *Inspector) tryDecodeRelay(payload []byte, requireKnownInner bool) ([]DecodedLayer, bool) {
	fields, n, err := ins.reg.DecodeMessage("RelayFrame", payload)
	if err != nil || n != len(payload) {
		return nil, false
	}
	peerVal, _ := fields.Get("peer")
	peer, _ := asUint64(peerVal)
	bodyVal, _ := fields.Get("body")
	bodyBytes, _ := bodyVal.(WireBytes)

	frameLayer := DecodedLayer{
		Layer:        "relay",
		Message:      "RelayFrame",
		Fields:       fields.Without("body"),
		PayloadField: "body",
	}

	if peer == 0 {
		members, mn, merr := ins.reg.DecodeMessage("RelayMembers", bodyBytes)
		if merr != nil || mn != len(bodyBytes) {
			return nil, false
		}
		return []DecodedLayer{
			frameLayer,
			{
				Layer:   "relay",
				Message: "RelayMembers",
				Fields:  members,
			},
		}, true
	}

	du, un, uerr := ins.reg.DecodeUnion("RelayBody", bodyBytes)
	if uerr != nil || un != len(bodyBytes) {
		return nil, false
	}
	tag := du.Tag
	bodyLayer := DecodedLayer{
		Layer:   "relay",
		Union:   du.Union,
		Case:    du.Case,
		Tag:     &tag,
		Message: du.Message,
		Fields:  du.Fields,
	}
	if du.Message == "RelayUnreliable" || du.Message == "RelayReliable" {
		plVal, _ := du.Fields.Get("payload")
		plBytes, _ := plVal.(WireBytes)
		if len(plBytes) > 0 {
			inner, ok := ins.tryDecodeAppLayer(plBytes)
			if requireKnownInner && !ok {
				return nil, false
			}
			bodyLayer.Fields = du.Fields.Without("payload")
			bodyLayer.PayloadField = "payload"
			if ok {
				return []DecodedLayer{frameLayer, bodyLayer, inner}, true
			}
			return []DecodedLayer{
				frameLayer,
				bodyLayer,
				{Layer: "raw", Raw: WireBytes(slices.Clone(plBytes))},
			}, true
		}
	}
	return []DecodedLayer{frameLayer, bodyLayer}, true
}

type builtinUnionCandidate struct {
	layer string
	union string
}

var builtinAppUnions = []builtinUnionCandidate{
	{layer: "replication", union: "ReplicationMessage"},
	{layer: "rollback", union: "RollbackMessage"},
	{layer: "membership", union: "MembershipMessage"},
	{layer: "handshake", union: "HandshakeMessage"},
}

func (ins *Inspector) tryDecodeAppLayer(payload []byte) (DecodedLayer, bool) {
	for _, cand := range builtinAppUnions {
		du, n, err := ins.reg.DecodeUnion(cand.union, payload)
		if err == nil && n == len(payload) {
			return unionToLayer(cand.layer, du), true
		}
	}
	// Try custom unions first.
	for _, name := range ins.reg.customUnions {
		du, n, err := ins.reg.DecodeUnion(name, payload)
		if err == nil && n == len(payload) {
			return unionToLayer(du.Schema, du), true
		}
	}
	// Try custom messages when no union matched.
	if dp, ok := ins.tryDecodeCustomPayload(payload); ok {
		return DecodedLayer{
			Layer:   dp.Schema,
			Union:   dp.Union,
			Version: dp.Version,
			Case:    dp.Case,
			Tag:     dp.Tag,
			Message: dp.Message,
			Fields:  dp.Fields,
		}, true
	}
	return DecodedLayer{}, false
}

func unionToLayer(layerName string, du DecodedUnion) DecodedLayer {
	tag := du.Tag
	l := DecodedLayer{
		Layer:   layerName,
		Union:   du.Union,
		Case:    du.Case,
		Tag:     &tag,
		Message: du.Message,
		Fields:  du.Fields,
	}
	if du.HasHigh {
		v := du.HighValue
		l.Version = &v
	}
	return l
}

func (ins *Inspector) tryDecodeCustomPayload(raw []byte) (DecodedPayload, bool) {
	if len(raw) == 0 || (len(ins.reg.customUnions) == 0 && len(ins.reg.customMessages) == 0) {
		return DecodedPayload{}, false
	}
	for _, name := range ins.reg.customUnions {
		du, n, err := ins.reg.DecodeUnion(name, raw)
		if err == nil && n == len(raw) {
			tag := du.Tag
			dp := DecodedPayload{
				Schema:  du.Schema,
				Union:   du.Union,
				Case:    du.Case,
				Tag:     &tag,
				Message: du.Message,
				Fields:  du.Fields,
			}
			if du.HasHigh {
				v := du.HighValue
				dp.Version = &v
			}
			return dp, true
		}
	}
	// Prefer a custom message literally named "Props" when present, then fall back to remaining custom messages in sorted order.
	candidates := ins.reg.customMessages
	ordered := make([]string, 0, len(candidates))
	if slices.Contains(candidates, "Props") {
		ordered = append(ordered, "Props")
	}
	for _, name := range candidates {
		if name != "Props" {
			ordered = append(ordered, name)
		}
	}
	for _, name := range ordered {
		fields, n, err := ins.reg.DecodeMessage(name, raw)
		if err == nil && n == len(raw) {
			sm := ins.reg.messages[name]
			return DecodedPayload{
				Schema:  sm.schema.Package,
				Message: name,
				Fields:  fields,
			}, true
		}
	}
	return DecodedPayload{}, false
}

func (ins *Inspector) postProcessLayers(dir string, peer uint32, layers []DecodedLayer) {
	effectivePeer := peer
	for i := range layers {
		if layers[i].Layer == "relay" && layers[i].Message == "RelayFrame" {
			if pv, ok := layers[i].Fields.Get("peer"); ok {
				if u, err := asUint64(pv); err == nil && u != 0 {
					effectivePeer = uint32(u)
				}
			}
		}
		if layers[i].Layer == "replication" {
			if ins.opts.ResolveDeltas {
				ins.trackReplication(dir, effectivePeer, &layers[i])
			}
			if len(ins.reg.customUnions) > 0 || len(ins.reg.customMessages) > 0 {
				ins.decodeReplicationProps(&layers[i])
			}
		}
	}
}

func (ins *Inspector) decodeReplicationProps(layer *DecodedLayer) {
	decodeEntitySlice := func(fieldName string) {
		raw, ok := layer.Fields.Get(fieldName)
		if !ok {
			return
		}
		ents, ok := raw.([]any)
		if !ok {
			return
		}
		for i := range ents {
			entObj, ok := ents[i].(DecodedObject)
			if !ok {
				continue
			}
			fv, ok := entObj.Get("fields")
			if !ok {
				continue
			}
			dm, ok := fv.(DecodedMask)
			if !ok {
				continue
			}
			pv, ok := dm.Fields.Get("props")
			if !ok {
				continue
			}
			wb, ok := pv.(WireBytes)
			if !ok || len(wb) == 0 {
				continue
			}
			if dp, ok := ins.tryDecodeCustomPayload(wb); ok {
				dm.Fields.Set("props", dp)
				entObj.Set("fields", dm)
				ents[i] = entObj
			}
		}
	}
	switch layer.Message {
	case "SnapshotFull", "SnapshotFullPart":
		decodeEntitySlice("entities")
	case "SnapshotDelta":
		decodeEntitySlice("updates")
	}
}

func (ins *Inspector) trackReplication(dir string, peer uint32, layer *DecodedLayer) {
	epochVal, ok := layer.Fields.Get("epoch")
	if !ok {
		return
	}
	epochU, err := asUint64(epochVal)
	if err != nil {
		return
	}
	epoch := uint32(epochU)
	bkey := baselineKey{dir: dir, peer: peer, epoch: epoch}

	switch layer.Message {
	case "SnapshotFull":
		tick := mustUint32Field(layer.Fields, "tick")
		ents := extractEntityRecords(layer.Fields, "entities")
		world := make(map[uint32]EntityRecord, len(ents))
		for _, e := range ents {
			world[e.ID] = cloneEntityRecord(e)
		}
		ins.storeBaseline(bkey, tick, world)

	case "SnapshotFullPart":
		tick := mustUint32Field(layer.Fields, "tick")
		idx := mustUint32Field(layer.Fields, "index")
		total := mustUint32Field(layer.Fields, "total")
		if total == 0 {
			return
		}
		pkey := partialFullKey{dir: dir, peer: peer, epoch: epoch, tick: tick}
		ps := ins.partials[pkey]
		if ps == nil || ps.total != total {
			ps = &partialFullState{
				total:    total,
				received: make(map[uint32]bool),
				entities: make(map[uint32]EntityRecord),
			}
			ins.partials[pkey] = ps
		}
		ps.received[idx] = true
		for _, e := range extractEntityRecords(layer.Fields, "entities") {
			ps.entities[e.ID] = cloneEntityRecord(e)
		}
		if uint32(len(ps.received)) == total {
			ins.storeBaseline(bkey, tick, ps.entities)
			delete(ins.partials, pkey)
		}

	case "SnapshotDelta":
		tick := mustUint32Field(layer.Fields, "tick")
		baseTick := mustUint32Field(layer.Fields, "baseline_tick")
		perTick := ins.baselines[bkey]
		base, ok := perTick[baseTick]
		if !ok {
			layer.BaselineMissing = true
			return
		}
		cur := make(map[uint32]EntityRecord, len(base))
		for id, e := range base {
			cur[id] = cloneEntityRecord(e)
		}
		if dv, ok := layer.Fields.Get("despawns"); ok {
			if ds, ok := dv.([]any); ok {
				for _, item := range ds {
					if id, err := asUint64(item); err == nil {
						delete(cur, uint32(id))
					}
				}
			}
		}
		for _, upd := range extractEntityRecords(layer.Fields, "updates") {
			ent := cur[upd.ID]
			ent.ID = upd.ID
			ApplyEntityRecord(&ent, &upd)
			ent.FieldsMask = EntityRecordMaskValid
			cur[upd.ID] = ent
		}
		ins.storeBaseline(bkey, tick, cur)
		layer.Resolved = ins.formatResolvedEntities(cur)
	}
}

func (ins *Inspector) storeBaseline(key baselineKey, tick uint32, world map[uint32]EntityRecord) {
	if ins.baselines[key] == nil {
		ins.baselines[key] = make(map[uint32]map[uint32]EntityRecord)
	}
	ins.baselines[key][tick] = world
}

func (ins *Inspector) formatResolvedEntities(world map[uint32]EntityRecord) []DecodedObject {
	ids := make([]uint32, 0, len(world))
	for id := range world {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]DecodedObject, 0, len(ids))
	for _, id := range ids {
		e := world[id]
		var propsVal any = WireBytes(slices.Clone(e.Props))
		if len(e.Props) > 0 {
			if dp, ok := ins.tryDecodeCustomPayload(e.Props); ok {
				propsVal = dp
			}
		}
		obj := DecodedObject{
			{Name: "id", Value: uint64(e.ID)},
			{Name: "kind", Value: uint64(e.Kind)},
			{Name: "x", Value: e.X},
			{Name: "y", Value: e.Y},
			{Name: "vx", Value: e.VX},
			{Name: "vy", Value: e.VY},
			{Name: "rotation", Value: e.Rotation},
			{Name: "flags", Value: uint64(e.Flags)},
			{Name: "props", Value: propsVal},
		}
		out = append(out, obj)
	}
	return out
}

func mustUint32Field(obj DecodedObject, name string) uint32 {
	if v, ok := obj.Get(name); ok {
		if u, err := asUint64(v); err == nil {
			return uint32(u)
		}
	}
	return 0
}

func extractEntityRecords(obj DecodedObject, fieldName string) []EntityRecord {
	raw, ok := obj.Get(fieldName)
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]EntityRecord, 0, len(list))
	for _, item := range list {
		eo, ok := item.(DecodedObject)
		if !ok {
			continue
		}
		rec := EntityRecord{
			ID: mustUint32Field(eo, "id"),
		}
		if fv, ok := eo.Get("fields"); ok {
			if dm, ok := fv.(DecodedMask); ok {
				rec.FieldsMask = uint8(dm.Mask)
				if v, ok := dm.Fields.Get("kind"); ok {
					if u, err := asUint64(v); err == nil {
						rec.Kind = uint16(u)
					}
				}
				if v, ok := dm.Fields.Get("x"); ok {
					if f, err := asFloat32(v); err == nil {
						rec.X = f
					}
				}
				if v, ok := dm.Fields.Get("y"); ok {
					if f, err := asFloat32(v); err == nil {
						rec.Y = f
					}
				}
				if v, ok := dm.Fields.Get("vx"); ok {
					if f, err := asFloat32(v); err == nil {
						rec.VX = f
					}
				}
				if v, ok := dm.Fields.Get("vy"); ok {
					if f, err := asFloat32(v); err == nil {
						rec.VY = f
					}
				}
				if v, ok := dm.Fields.Get("rotation"); ok {
					if f, err := asFloat32(v); err == nil {
						rec.Rotation = f
					}
				}
				if v, ok := dm.Fields.Get("flags"); ok {
					if u, err := asUint64(v); err == nil {
						rec.Flags = uint32(u)
					}
				}
				if v, ok := dm.Fields.Get("props"); ok {
					if wb, ok := v.(WireBytes); ok {
						rec.Props = slices.Clone([]byte(wb))
					}
				}
			}
		}
		out = append(out, rec)
	}
	return out
}

func cloneEntityRecord(e EntityRecord) EntityRecord {
	cp := e
	if len(e.Props) > 0 {
		cp.Props = slices.Clone(e.Props)
	} else {
		cp.Props = nil
	}
	return cp
}

// ReencodeLayers re-encodes a stack of decoded layers from innermost to outermost back into wire bytes using the wire schemas.
func (ins *Inspector) ReencodeLayers(layers []DecodedLayer) ([]byte, error) {
	if len(layers) == 0 {
		return nil, errors.New("wire: no layers to re-encode")
	}
	var inner []byte
	for i := len(layers) - 1; i >= 0; i-- {
		l := layers[i]
		if l.Layer == "raw" {
			if i != len(layers)-1 {
				return nil, errors.New("wire: raw layer must be innermost")
			}
			inner = slices.Clone([]byte(l.Raw))
			continue
		}
		fields := slices.Clone(l.Fields)
		if l.PayloadField != "" {
			fields.Set(l.PayloadField, WireBytes(inner))
		}
		var encoded []byte
		var err error
		if l.Union != "" {
			du := DecodedUnion{
				Schema:  l.Layer,
				Union:   l.Union,
				Case:    l.Case,
				Message: l.Message,
				Fields:  fields,
			}
			if l.Tag != nil {
				du.Tag = *l.Tag
			}
			if l.Version != nil {
				du.HasHigh = true
				du.HighValue = *l.Version
			}
			encoded, err = ins.reg.EncodeUnion(du)
		} else {
			encoded, err = ins.reg.EncodeMessage(l.Message, fields)
		}
		if err != nil {
			return nil, fmt.Errorf("wire: reencode layer %d (%s.%s): %w", i, l.Layer, l.Message, err)
		}
		inner = encoded
	}
	return inner, nil
}

// ReencodePayload re-encodes the transport payload of a capture-decoded DecodedFrame (skipping the synthetic outer transport layer when present).
// For frames decoded via InspectRawFrame, it re-encodes all layers from outermost to innermost.
func (ins *Inspector) ReencodePayload(frame DecodedFrame) ([]byte, error) {
	if len(frame.Layers) == 0 {
		return nil, errors.New("wire: frame has no layers")
	}
	if frame.Dir != "" && frame.Layers[0].Layer == "transport" && len(frame.Layers) > 1 {
		return ins.ReencodeLayers(frame.Layers[1:])
	}
	return ins.ReencodeLayers(frame.Layers)
}

// FrameMessageType returns a stable, human-readable message type name for frame (such as "replication.delta", "rollback.single_input", "relay.members", or "raw").
func FrameMessageType(frame DecodedFrame) string {
	if len(frame.Layers) == 0 {
		return "raw"
	}
	leaf := frame.Layers[len(frame.Layers)-1]
	if leaf.Layer == "raw" {
		if len(frame.Layers) >= 2 && frame.Layers[len(frame.Layers)-2].Layer == "relay" {
			prev := frame.Layers[len(frame.Layers)-2]
			if prev.Case != "" {
				return "relay." + prev.Case + "/raw"
			}
		}
		return "raw"
	}
	if leaf.Case != "" {
		return leaf.Layer + "." + leaf.Case
	}
	if leaf.Message != "" {
		return leaf.Layer + "." + leaf.Message
	}
	return leaf.Layer
}

// TypeStats aggregates packet and byte totals for one decoded message type.
type TypeStats struct {
	Type    string `yaml:"type" json:"type"`
	Packets int    `yaml:"packets" json:"packets"`
	Bytes   int64  `yaml:"bytes" json:"bytes"`
}

// PeerStats aggregates packet and byte totals for one peer ID.
type PeerStats struct {
	Peer        uint32 `yaml:"peer" json:"peer"`
	Packets     int    `yaml:"packets" json:"packets"`
	Bytes       int64  `yaml:"bytes" json:"bytes"`
	SendPackets int    `yaml:"send_packets" json:"send_packets"`
	SendBytes   int64  `yaml:"send_bytes" json:"send_bytes"`
	RecvPackets int    `yaml:"recv_packets" json:"recv_packets"`
	RecvBytes   int64  `yaml:"recv_bytes" json:"recv_bytes"`
}

// CaptureStats summarizes traffic decoded from a capture stream.
type CaptureStats struct {
	TotalPackets   int         `yaml:"total_packets" json:"total_packets"`
	TotalBytes     int64       `yaml:"total_bytes" json:"total_bytes"`
	SendPackets    int         `yaml:"send_packets" json:"send_packets"`
	SendBytes      int64       `yaml:"send_bytes" json:"send_bytes"`
	RecvPackets    int         `yaml:"recv_packets" json:"recv_packets"`
	RecvBytes      int64       `yaml:"recv_bytes" json:"recv_bytes"`
	DurationNS     int64       `yaml:"duration_ns" json:"duration_ns"`
	DeltaCount     int         `yaml:"delta_count" json:"delta_count"`
	DeltaBytes     int64       `yaml:"delta_bytes" json:"delta_bytes"`
	MeanDeltaBytes float64     `yaml:"mean_delta_bytes" json:"mean_delta_bytes"`
	ByType         []TypeStats `yaml:"by_type" json:"by_type"`
	ByPeer         []PeerStats `yaml:"by_peer" json:"by_peer"`
}

// ComputeStats aggregates message-type counts, per-peer counts, and mean delta size across frames.
func (ins *Inspector) ComputeStats(frames []DecodedFrame) CaptureStats {
	var st CaptureStats
	byType := make(map[string]*TypeStats)
	byPeer := make(map[uint32]*PeerStats)

	var minT, maxT int64
	hasT := false

	for _, fr := range frames {
		st.TotalPackets++
		st.TotalBytes += int64(fr.Size)
		if fr.Dir == "send" {
			st.SendPackets++
			st.SendBytes += int64(fr.Size)
		} else if fr.Dir == "recv" {
			st.RecvPackets++
			st.RecvBytes += int64(fr.Size)
		}
		if fr.T != nil {
			if !hasT {
				minT, maxT, hasT = *fr.T, *fr.T, true
			} else {
				if *fr.T < minT {
					minT = *fr.T
				}
				if *fr.T > maxT {
					maxT = *fr.T
				}
			}
		}

		msgType := FrameMessageType(fr)
		ts := byType[msgType]
		if ts == nil {
			ts = &TypeStats{Type: msgType}
			byType[msgType] = ts
		}
		ts.Packets++
		ts.Bytes += int64(fr.Size)

		var peer uint32
		if fr.Peer != nil {
			peer = *fr.Peer
		}
		ps := byPeer[peer]
		if ps == nil {
			ps = &PeerStats{Peer: peer}
			byPeer[peer] = ps
		}
		ps.Packets++
		ps.Bytes += int64(fr.Size)
		if fr.Dir == "send" {
			ps.SendPackets++
			ps.SendBytes += int64(fr.Size)
		} else if fr.Dir == "recv" {
			ps.RecvPackets++
			ps.RecvBytes += int64(fr.Size)
		}

		for i := range fr.Layers {
			if fr.Layers[i].Layer == "replication" && fr.Layers[i].Message == "SnapshotDelta" {
				st.DeltaCount++
				if b, err := ins.ReencodeLayers(fr.Layers[i:]); err == nil {
					st.DeltaBytes += int64(len(b))
				} else {
					st.DeltaBytes += int64(fr.Size)
				}
			}
		}
	}

	if hasT && maxT >= minT {
		st.DurationNS = maxT - minT
	}
	if st.DeltaCount > 0 {
		st.MeanDeltaBytes = float64(st.DeltaBytes) / float64(st.DeltaCount)
	}

	st.ByType = make([]TypeStats, 0, len(byType))
	for _, ts := range byType {
		st.ByType = append(st.ByType, *ts)
	}
	slices.SortFunc(st.ByType, func(a, b TypeStats) int {
		if c := cmp.Compare(b.Bytes, a.Bytes); c != 0 {
			return c
		}
		return cmp.Compare(a.Type, b.Type)
	})

	st.ByPeer = make([]PeerStats, 0, len(byPeer))
	for _, ps := range byPeer {
		st.ByPeer = append(st.ByPeer, *ps)
	}
	slices.SortFunc(st.ByPeer, func(a, b PeerStats) int {
		return cmp.Compare(a.Peer, b.Peer)
	})
	return st
}

// DecodeRawFrameText parses a standalone hex (optional 0x prefix, optional spaces) or base64 (standard or URL, padded or unpadded) frame string into raw bytes.
func (ins *Inspector) DecodeRawFrameText(text string) ([]byte, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, errors.New("wire: empty raw frame input")
	}
	explicitHex := strings.HasPrefix(trimmed, "0x") || strings.HasPrefix(trimmed, "0X") || strings.ContainsAny(trimmed, " \t")
	hexClean := strings.TrimPrefix(strings.TrimPrefix(trimmed, "0x"), "0X")
	hexCompact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, hexClean)

	isHex := len(hexCompact) > 0 && len(hexCompact)%2 == 0
	if isHex {
		for i := 0; i < len(hexCompact); i++ {
			c := hexCompact[i]
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				isHex = false
				break
			}
		}
	}

	if explicitHex {
		if !isHex {
			return nil, errors.New("wire: invalid hex frame")
		}
		return hex.DecodeString(hexCompact)
	}

	b64Bytes, b64Err := decodeAnyBase64(trimmed)
	if isHex {
		hexBytes, _ := hex.DecodeString(hexCompact)
		if b64Err != nil {
			return hexBytes, nil
		}
		// Ambiguous all-hex string that is also valid base64: prefer whichever decodes into a recognized protocol layer.
		if ins != nil {
			hexKnown := ins.decodesAsKnown(hexBytes)
			b64Known := ins.decodesAsKnown(b64Bytes)
			if b64Known && !hexKnown {
				return b64Bytes, nil
			}
		}
		return hexBytes, nil
	}
	if b64Err == nil {
		return b64Bytes, nil
	}
	return nil, errors.New("wire: input is neither valid JSONL capture, hex, nor base64")
}

func (ins *Inspector) decodesAsKnown(raw []byte) bool {
	layers := ins.decodeRawLayers(raw)
	return len(layers) > 0 && layers[0].Layer != "raw"
}

func decodeAnyBase64(s string) ([]byte, error) {
	compact := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, s)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(compact); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("invalid base64")
}
