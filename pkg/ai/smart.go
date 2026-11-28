package ai

import (
	"errors"
	"sort"
)

var ErrSmartSlotBusy = errors.New("ai: smart-object slot is busy")

type SmartSlot struct {
	ID       string
	X, Y     float64
	Tags     []string
	Behavior string
	Agent    string
}
type SmartObject struct {
	ID      string
	X, Y    float64
	Enabled bool
	Slots   []SmartSlot
}
type SmartQuery struct{ Tags []string }
type SmartClaim struct{ ObjectID, SlotID, Agent string }
type SmartSnapshot struct{ Objects []SmartObject }
type SmartObjects struct {
	objects    map[string]SmartObject
	onReleased func(SmartClaim)
}

func NewSmartObjects() *SmartObjects { return &SmartObjects{objects: map[string]SmartObject{}} }
func (s *SmartObjects) SetOnReleased(fn func(SmartClaim)) {
	if s != nil {
		s.onReleased = fn
	}
}
func (s *SmartObjects) Register(o SmartObject) {
	if s == nil {
		return
	}
	if !o.Enabled {
		o.Enabled = true
	}
	sort.SliceStable(o.Slots, func(i, j int) bool { return o.Slots[i].ID < o.Slots[j].ID })
	s.objects[o.ID] = o
}
func (s *SmartObjects) FindInRadius(x, y, r float64, q SmartQuery) []SmartClaim {
	if s == nil || r < 0 {
		return nil
	}
	var out []SmartClaim
	for _, o := range s.objects {
		if !o.Enabled || (o.X-x)*(o.X-x)+(o.Y-y)*(o.Y-y) > r*r {
			continue
		}
		for _, sl := range o.Slots {
			if sl.Agent == "" && tagsMatch(sl.Tags, q.Tags) {
				out = append(out, SmartClaim{ObjectID: o.ID, SlotID: sl.ID})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ObjectID != out[j].ObjectID {
			return out[i].ObjectID < out[j].ObjectID
		}
		return out[i].SlotID < out[j].SlotID
	})
	return out
}
func tagsMatch(have, need []string) bool {
	for _, n := range need {
		ok := false
		for _, h := range have {
			if h == n {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
func (s *SmartObjects) Claim(agent, obj, slot string) (SmartClaim, bool) {
	if s == nil || agent == "" {
		return SmartClaim{}, false
	}
	o, ok := s.objects[obj]
	if !ok || !o.Enabled {
		return SmartClaim{}, false
	}
	for i := range o.Slots {
		if o.Slots[i].ID == slot {
			if o.Slots[i].Agent != "" {
				return SmartClaim{}, false
			}
			o.Slots[i].Agent = agent
			s.objects[obj] = o
			return SmartClaim{obj, slot, agent}, true
		}
	}
	return SmartClaim{}, false
}
func (s *SmartObjects) Release(c SmartClaim) bool {
	if s == nil {
		return false
	}
	o, ok := s.objects[c.ObjectID]
	if !ok {
		return false
	}
	for i := range o.Slots {
		if o.Slots[i].ID == c.SlotID && o.Slots[i].Agent == c.Agent {
			o.Slots[i].Agent = ""
			s.objects[o.ID] = o
			if s.onReleased != nil {
				s.onReleased(c)
			}
			return true
		}
	}
	return false
}
func (s *SmartObjects) Disable(id string) {
	if s == nil {
		return
	}
	o, ok := s.objects[id]
	if !ok {
		return
	}
	o.Enabled = false
	for i := range o.Slots {
		if o.Slots[i].Agent != "" {
			c := SmartClaim{id, o.Slots[i].ID, o.Slots[i].Agent}
			o.Slots[i].Agent = ""
			if s.onReleased != nil {
				s.onReleased(c)
			}
		}
	}
	s.objects[id] = o
}
func (s *SmartObjects) Snapshot() SmartSnapshot {
	if s == nil {
		return SmartSnapshot{}
	}
	out := SmartSnapshot{}
	for _, o := range s.objects {
		slots := make([]SmartSlot, len(o.Slots))
		copy(slots, o.Slots)
		for i := range slots {
			slots[i].Tags = append([]string(nil), slots[i].Tags...)
		}
		o.Slots = slots
		out.Objects = append(out.Objects, o)
	}
	sort.Slice(out.Objects, func(i, j int) bool { return out.Objects[i].ID < out.Objects[j].ID })
	return out
}
func (s *SmartObjects) Restore(snap SmartSnapshot) {
	if s == nil {
		return
	}
	s.objects = map[string]SmartObject{}
	for _, o := range snap.Objects {
		s.objects[o.ID] = o
	}
}
