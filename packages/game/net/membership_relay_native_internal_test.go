//go:build !js

package net

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

// awaitAny waits on several clients' change signals until cond holds.
func awaitAny(t testing.TB, clients []*relayClient, what string, cond func() bool) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	cases := []reflect.SelectCase{{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(timeout.C)}}
	for _, c := range clients {
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(c.changed)})
	}
	for !cond() {
		if chosen, _, _ := reflect.Select(cases); chosen == 0 {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// TestRelayLockstepPeerJoinsAndLeaves runs a lockstep match between clients of a relay: a third client joins the relay session mid-match and the host admits it as the relay announces it, then it closes its connection and the host removes it as the relay reports it gone. The two who stay end in the same state.
func TestRelayLockstepPeerJoinsAndLeaves(t *testing.T) {
	for _, proto := range []string{"websocket", "udp"} {
		t.Run(proto, func(t *testing.T) {
			_, dial := startRelay(t, proto, RelayConfig{}, Auth{})
			a := mustDialRelay(t, dial, "match/alice")
			b := mustDialRelay(t, dial, "match/bob")
			awaitPeers(t, a, b.LocalID())
			awaitPeers(t, b, a.LocalID())

			type player struct {
				c    *relayClient
				sim  *relaySim
				s    *LockstepSession
				sent uint32 // one past the last tick submitted
			}
			players := []*player{
				{c: a, sim: &relaySim{}},
				{c: b, sim: &relaySim{}},
			}
			for _, p := range players {
				p.s = NewLockstepSession(LockstepConfig{Transport: p.c, Sim: p.sim, ChecksumInterval: 5})
			}
			host := players[0]
			if host.s.host != a.LocalID() || players[1].s.host != a.LocalID() {
				t.Fatalf("hosts %d and %d, want %d", host.s.host, players[1].s.host, a.LocalID())
			}
			clients := func() []*relayClient {
				var out []*relayClient
				for _, p := range players {
					out = append(out, p.c)
				}
				return out
			}
			// playTo runs every player that takes part to tick end.
			playTo := func(end uint32) {
				awaitAny(t, clients(), "the match to reach its tick", func() bool {
					done := true
					for _, p := range players {
						if p.s.Active() && p.s.Tick() < end && p.sent <= p.s.Tick() {
							if err := p.s.SubmitInput(relayInput(p.c.LocalID(), p.s.Tick())); err != nil {
								t.Fatalf("submit: %v", err)
							}
							p.sent = p.s.Tick() + 1
						}
						for p.s.Tick() < end && p.s.Step() {
						}
						done = done && (!p.s.Active() || p.s.Tick() >= end)
					}
					return done
				})
			}
			playTo(10)

			c := mustDialRelay(t, dial, "match/carol")
			awaitPeers(t, a, b.LocalID(), c.LocalID())
			carol := &player{c: c, sim: &relaySim{}}
			carol.s = NewLockstepSession(LockstepConfig{Transport: c, Sim: carol.sim, Host: a.LocalID(), Join: true, ChecksumInterval: 5})
			players = append(players, carol)
			join, err := host.s.AddPeer(c.LocalID())
			if err != nil || join != 10 {
				t.Fatalf("AddPeer = %d, %v; want 10", join, err)
			}
			awaitAny(t, clients(), "the newcomer's join state", func() bool {
				carol.s.Step()
				return carol.s.Active()
			})
			if carol.s.Tick() != join || carol.sim.tick != join {
				t.Fatalf("newcomer at tick %d with a state at %d, want %d", carol.s.Tick(), carol.sim.tick, join)
			}
			playTo(25)
			if carol.sim.Checksum() != host.sim.Checksum() || players[1].sim.Checksum() != host.sim.Checksum() {
				t.Fatalf("states differ at tick 25: %+v %+v %+v", host.sim, players[1].sim, carol.sim)
			}

			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			players = players[:2]
			awaitPeers(t, a, b.LocalID())
			leave, err := host.s.RemovePeer(c.LocalID())
			if err != nil || leave != 25 {
				t.Fatalf("RemovePeer = %d, %v; want 25", leave, err)
			}
			playTo(40)
			if host.sim.tick != 40 || players[1].sim.Checksum() != host.sim.Checksum() {
				t.Fatalf("states differ at tick 40: %+v %+v", host.sim, players[1].sim)
			}
			if got := players[1].s.Members(); !slices.Equal(got, []PeerID{a.LocalID(), b.LocalID()}) {
				t.Fatalf("members after the leave = %v", got)
			}
			for _, p := range append(players, carol) {
				if d := p.s.Desyncs(); len(d) != 0 {
					t.Fatalf("desyncs: %+v", d)
				}
			}
		})
	}
}

// TestRelayRollbackPeerJoinsAndLeaves runs a rollback match between clients of a relay. The host admits a third client as soon as it dials, before the relay has told the host about it, so the join state waits until the host's transport knows the newcomer. Later the newcomer closes its connection and the host removes it. Every tick is confirmed alike on every peer taking part.
func TestRelayRollbackPeerJoinsAndLeaves(t *testing.T) {
	for _, proto := range []string{"websocket", "udp"} {
		t.Run(proto, func(t *testing.T) {
			_, dial := startRelay(t, proto, RelayConfig{}, Auth{})
			a := mustDialRelay(t, dial, "match/alice")
			b := mustDialRelay(t, dial, "match/bob")
			awaitPeers(t, a, b.LocalID())
			awaitPeers(t, b, a.LocalID())

			type player struct {
				c   *relayClient
				sim *relaySim
				s   *RollbackSession
			}
			players := []*player{{c: a, sim: &relaySim{}}, {c: b, sim: &relaySim{}}}
			for _, p := range players {
				p.s = NewRollbackSession(RollbackConfig{Transport: p.c, Sim: p.sim, MaxRollback: 8, ChecksumInterval: 5})
			}
			host := players[0]
			clients := func() []*relayClient {
				var out []*relayClient
				for _, p := range players {
					out = append(out, p.c)
				}
				return out
			}
			pollAll := func() {
				for _, p := range players {
					if err := p.s.Poll(); err != nil {
						t.Fatalf("poll: %v", err)
					}
				}
			}
			// playTo steps every player that takes part, one tick at a time, waiting each time until they all confirm it.
			playTo := func(end uint32) {
				for tick := host.s.Tick(); tick < end; tick++ {
					for _, p := range players {
						if p.s.Active() && p.s.Tick() == tick {
							if err := p.s.Step(relayInput(p.c.LocalID(), tick)); err != nil {
								t.Fatalf("step %d: %v", tick, err)
							}
						}
					}
					awaitAny(t, clients(), "every player to confirm the tick", func() bool {
						pollAll()
						for _, p := range players {
							if p.s.Active() && p.s.ConfirmedTick() < int64(tick) {
								return false
							}
						}
						return true
					})
				}
			}
			playTo(10)

			c := mustDialRelay(t, dial, "match/carol")
			carol := &player{c: c, sim: &relaySim{}}
			carol.s = NewRollbackSession(RollbackConfig{Transport: c, Sim: carol.sim, MaxRollback: 8, ChecksumInterval: 5, Host: a.LocalID(), Join: true})
			players = append(players, carol)
			join, err := host.s.AddPeer(c.LocalID())
			if err != nil || join != 10 {
				t.Fatalf("AddPeer = %d, %v; want 10", join, err)
			}
			awaitAny(t, clients(), "the newcomer's join state", func() bool {
				pollAll()
				return carol.s.Active()
			})
			if carol.s.Tick() != join || carol.sim.tick != join || carol.s.ConfirmedTick() != int64(join)-1 {
				t.Fatalf("newcomer at tick %d (confirmed %d) with a state at %d, want %d", carol.s.Tick(), carol.s.ConfirmedTick(), carol.sim.tick, join)
			}
			playTo(25)
			if carol.sim.Checksum() != host.sim.Checksum() || players[1].sim.Checksum() != host.sim.Checksum() {
				t.Fatalf("states differ at tick 25: %+v %+v %+v", host.sim, players[1].sim, carol.sim)
			}

			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			players = players[:2]
			awaitPeers(t, a, b.LocalID())
			leave, err := host.s.RemovePeer(c.LocalID())
			if err != nil || leave != 25 {
				t.Fatalf("RemovePeer = %d, %v; want 25", leave, err)
			}
			playTo(40)
			if host.sim.tick != 40 || players[1].sim.Checksum() != host.sim.Checksum() {
				t.Fatalf("states differ at tick 40: %+v %+v", host.sim, players[1].sim)
			}
			if got := players[1].s.Members(); !slices.Equal(got, []PeerID{a.LocalID(), b.LocalID()}) {
				t.Fatalf("members after the leave = %v", got)
			}
			for _, p := range append(players, carol) {
				if d := p.s.Desyncs(); len(d) != 0 {
					t.Fatalf("desyncs: %+v", d)
				}
			}
		})
	}
}
