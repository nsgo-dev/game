package ai

import (
	"math"
	"sort"
)

type SmartSlot struct {
	ID       string
	X, Y     float64
	Tags     []string
	Behavior string
}
type SmartObject struct {
	ID      string
	X, Y    float64
	Enabled bool
	Slots   []SmartSlot
}
type SmartClaim struct{ ObjectID, SlotID, AgentID string }
type SmartQuery struct{ Tags []string }

type SmartObjects struct {
	objects    map[string]SmartObject
	claims     map[string]SmartClaim
	OnReleased func(SmartClaim)
}

func NewSmartObjects() *SmartObjects {
	return &SmartObjects{objects: map[string]SmartObject{}, claims: map[string]SmartClaim{}}
}
func (s *SmartObjects) Register(o SmartObject) {
	if s == nil || o.ID == "" {
		return
	}
	o.Enabled = true
	s.objects[o.ID] = o
}
func (s *SmartObjects) SetEnabled(id string, enabled bool) {
	if s == nil {
		return
	}
	o, ok := s.objects[id]
	if !ok {
		return
	}
	o.Enabled = enabled
	s.objects[id] = o
	if !enabled {
		for _, c := range s.claims {
			if c.ObjectID == id {
				s.Release(c)
			}
		}
	}
}
func (s *SmartObjects) FindInRadius(x, y, r float64, q SmartQuery) []SmartSlot {
	if s == nil || r < 0 {
		return nil
	}
	out := []SmartSlot{}
	for _, o := range s.objects {
		if !o.Enabled || math.Hypot(o.X-x, o.Y-y) > r {
			continue
		}
		for _, slot := range o.Slots {
			if _, held := s.claims[o.ID+"/"+slot.ID]; held {
				continue
			}
			if !tagsMatch(slot.Tags, q.Tags) {
				continue
			}
			cp := slot
			cp.ID = o.ID + "/" + slot.ID
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (s *SmartObjects) Claim(agent, slotID string) (SmartClaim, bool) {
	if s == nil {
		return SmartClaim{}, false
	}
	for id, o := range s.objects {
		for _, slot := range o.Slots {
			key := id + "/" + slot.ID
			if key == slotID && o.Enabled {
				if _, ok := s.claims[key]; ok {
					return SmartClaim{}, false
				}
				c := SmartClaim{id, slot.ID, agent}
				s.claims[key] = c
				return c, true
			}
		}
	}
	return SmartClaim{}, false
}
func (s *SmartObjects) Use(c SmartClaim) bool {
	if s == nil {
		return false
	}
	got, ok := s.claims[c.ObjectID+"/"+c.SlotID]
	return ok && got.AgentID == c.AgentID
}
func (s *SmartObjects) Release(c SmartClaim) {
	if s == nil {
		return
	}
	key := c.ObjectID + "/" + c.SlotID
	if got, ok := s.claims[key]; ok && got.AgentID == c.AgentID {
		delete(s.claims, key)
		if s.OnReleased != nil {
			s.OnReleased(got)
		}
	}
}
func tagsMatch(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

type SmartObjectsSnapshot struct{ Claims []SmartClaim }

func (s *SmartObjects) Snapshot() SmartObjectsSnapshot {
	out := SmartObjectsSnapshot{}
	for _, c := range s.claims {
		out.Claims = append(out.Claims, c)
	}
	sort.Slice(out.Claims, func(i, j int) bool {
		return out.Claims[i].ObjectID+out.Claims[i].SlotID < out.Claims[j].ObjectID+out.Claims[j].SlotID
	})
	return out
}
func (s *SmartObjects) Restore(st SmartObjectsSnapshot) {
	s.claims = map[string]SmartClaim{}
	for _, c := range st.Claims {
		s.claims[c.ObjectID+"/"+c.SlotID] = c
	}
}
