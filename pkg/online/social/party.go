package social

import "errors"

var ErrPartyFull = errors.New("social: party full")
var ErrPartyClosed = errors.New("social: party closed")

type Member struct {
	ID     string
	Joined uint64
}
type Party struct {
	ID, Leader string
	Open       bool
	MaxSize    int
	Members    []Member
	Label      string
	Pending    bool
}
type Parties struct {
	next    uint64
	parties map[string]*Party
}

func NewParties() *Parties { return &Parties{parties: map[string]*Party{}} }
func (p *Parties) Create(leader string, max int, open bool) *Party {
	if max < 1 {
		max = 4
	}
	p.next++
	id := stringID(p.next)
	x := &Party{ID: id, Leader: leader, Open: open, MaxSize: max, Members: []Member{{ID: leader, Joined: p.next}}}
	p.parties[id] = x
	z := clone(x)
	return &z
}
func (p *Parties) Join(id, member string) (*Party, error) {
	x, ok := p.parties[id]
	if !ok {
		return nil, errors.New("social: unknown party")
	}
	if !x.Open {
		return nil, ErrPartyClosed
	}
	if len(x.Members) >= x.MaxSize {
		return nil, ErrPartyFull
	}
	for _, m := range x.Members {
		if m.ID == member {
			z := clone(x)
			return &z, nil
		}
	}
	p.next++
	x.Members = append(x.Members, Member{ID: member, Joined: p.next})
	z := clone(x)
	return &z, nil
}
func (p *Parties) Leave(id, member string) *Party {
	x := p.parties[id]
	if x == nil {
		return nil
	}
	for i, m := range x.Members {
		if m.ID == member {
			x.Members = append(x.Members[:i], x.Members[i+1:]...)
			break
		}
	}
	if len(x.Members) == 0 {
		delete(p.parties, id)
		return nil
	}
	if x.Leader == member {
		x.Leader = x.Members[0].ID
	}
	z := clone(x)
	return &z
}
func (p *Parties) SetPending(id string, pending bool) bool {
	if x := p.parties[id]; x != nil {
		x.Pending = pending
		return true
	}
	return false
}
func clone(x *Party) Party { y := *x; y.Members = append([]Member(nil), x.Members...); return y }
func stringID(n uint64) string {
	const d = "0123456789abcdefghijklmnopqrstuvwxyz"
	s := ""
	for n > 0 {
		s = string(d[n%36]) + s
		n /= 36
	}
	return s
}
