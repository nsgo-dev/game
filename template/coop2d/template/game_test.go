package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// testConfig is a small room with short timers: a grace period of three ticks, a two-tick revive and a three-tick respawn.
var testConfig = CoopConfig{Version: 1, TickRate: 20, MaxPlayers: 2, RejoinGrace: 150 * time.Millisecond, ReviveTicks: 2, RespawnTicks: 3}

func newTestRoom(t *testing.T, ids ...string) *Server {
	t.Helper()
	s, err := NewServer(testConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRoom("ROOM1"); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := s.Join("ROOM1", id); err != nil {
			t.Fatalf("join %s: %v", id, err)
		}
	}
	return s
}

func tick(t *testing.T, s *Server, n int) []string {
	t.Helper()
	var expired []string
	for range n {
		ids, err := s.Tick("ROOM1")
		if err != nil {
			t.Fatal(err)
		}
		expired = append(expired, ids...)
	}
	return expired
}

func player(t *testing.T, s *Server, id string) Player {
	t.Helper()
	st, ok := s.Room("ROOM1")
	if !ok {
		t.Fatal("room closed")
	}
	p, ok := st.Player(id)
	if !ok {
		t.Fatalf("no player %s", id)
	}
	return p
}

func TestCoopYAMLRoundTripsAndRefusesUnknownFields(t *testing.T) {
	out, err := EncodeCoopConfig(Coop)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, coopYAML) {
		t.Fatalf("encoding coop.yaml changed it:\n%s", out)
	}
	for name, doc := range map[string]string{
		"unknown field":  string(coopYAML) + "lives: 3\n",
		"second doc":     string(coopYAML) + "---\nversion: 1\n",
		"version":        strings.Replace(string(coopYAML), "version: 1", "version: 2", 1),
		"five players":   strings.Replace(string(coopYAML), "maxPlayers: 4", "maxPlayers: 5", 1),
		"no grace":       strings.Replace(string(coopYAML), "rejoinGrace: 30s", "rejoinGrace: 1ms", 1),
		"no revive time": strings.Replace(string(coopYAML), "reviveTicks: 40", "reviveTicks: 0", 1),
	} {
		if _, err := ParseCoopConfig([]byte(doc)); err == nil {
			t.Errorf("%s: parsed %q", name, doc)
		}
	}
}

func TestInviteLinkCarriesTheRoomCode(t *testing.T) {
	code := RoomCode("room-1")
	if len(code) != 6 || code != strings.ToUpper(code) || RoomCode("room-1") != code || RoomCode("room-2") == code {
		t.Fatalf("RoomCode = %q", code)
	}
	link, err := InviteLink("https://example.com/play?lang=en", code)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := RoomFromInvite(link); !ok || got != code || !strings.Contains(link, "lang=en") {
		t.Fatalf("InviteLink = %q, RoomFromInvite = %q, %v", link, got, ok)
	}
	if _, ok := RoomFromInvite("https://example.com/play"); ok {
		t.Fatal("a link with no room has a room")
	}
}

func TestTwoPlayersJoinByCode(t *testing.T) {
	s := newTestRoom(t, "a", "b")
	if err := s.Join("ROOM1", "c"); !errors.Is(err, ErrRoomFull) {
		t.Errorf("third join: %v, want ErrRoomFull", err)
	}
	if err := s.Join("ROOM1", "a"); !errors.Is(err, ErrInRoom) {
		t.Errorf("second join of a: %v, want ErrInRoom", err)
	}
	if err := s.Join("NOPE", "c"); !errors.Is(err, ErrUnknownRoom) {
		t.Errorf("join of an unknown code: %v, want ErrUnknownRoom", err)
	}
	if st, _ := s.Room("ROOM1"); len(st.Players) != 2 || !st.Players[0].Connected || !st.Players[1].Connected {
		t.Errorf("room = %+v", st)
	}
}

func TestRejoinWithinGraceKeepsState(t *testing.T) {
	s := newTestRoom(t, "a", "b")
	if err := s.Down("ROOM1", "b"); err != nil {
		t.Fatal(err)
	}
	if err := s.Leave("ROOM1", "b"); err != nil {
		t.Fatal(err)
	}
	if expired := tick(t, s, 3); len(expired) != 0 {
		t.Fatalf("expired %v within the grace period", expired)
	}
	if p := player(t, s, "b"); p.Connected || !p.Downed || p.ReviveProgress != 0 {
		t.Fatalf("a disconnected downed player is revived: %+v", p)
	}
	if err := s.Rejoin("ROOM1", "b"); err != nil {
		t.Fatal(err)
	}
	if p := player(t, s, "b"); !p.Connected || !p.Downed {
		t.Fatalf("rejoined player = %+v, want connected and still downed", p)
	}
}

func TestGraceExpiryRemovesThePlayer(t *testing.T) {
	s := newTestRoom(t, "a", "b")
	if err := s.Leave("ROOM1", "b"); err != nil {
		t.Fatal(err)
	}
	tick(t, s, 3)
	if expired := tick(t, s, 1); len(expired) != 1 || expired[0] != "b" {
		t.Fatalf("expired = %v, want [b]", expired)
	}
	if err := s.Rejoin("ROOM1", "b"); !errors.Is(err, ErrNotInRoom) {
		t.Fatalf("rejoin after the grace period: %v, want ErrNotInRoom", err)
	}
	if err := s.Join("ROOM1", "c"); err != nil {
		t.Fatalf("the expired player's place is not free: %v", err)
	}
}

func TestTeammateRevivesDownedPlayer(t *testing.T) {
	s := newTestRoom(t, "a", "b")
	if err := s.Down("ROOM1", "a"); err != nil {
		t.Fatal(err)
	}
	tick(t, s, 1)
	if p := player(t, s, "a"); !p.Downed || p.ReviveProgress != 1 {
		t.Fatalf("after one tick: %+v", p)
	}
	tick(t, s, 1)
	if p := player(t, s, "a"); p.Downed {
		t.Fatalf("teammate did not revive the player: %+v", p)
	}
}

func TestDownedPlayerRespawnsAlone(t *testing.T) {
	s := newTestRoom(t, "a", "b")
	if err := s.Leave("ROOM1", "b"); err != nil {
		t.Fatal(err)
	}
	if err := s.Down("ROOM1", "a"); err != nil {
		t.Fatal(err)
	}
	tick(t, s, 2)
	if p := player(t, s, "a"); !p.Downed || p.RespawnProgress != 2 || p.ReviveProgress != 0 {
		t.Fatalf("after two ticks alone: %+v", p)
	}
	tick(t, s, 1)
	if p := player(t, s, "a"); p.Downed {
		t.Fatalf("the player did not respawn alone: %+v", p)
	}
}

func TestBothDownedRespawnTogether(t *testing.T) {
	s := newTestRoom(t, "a", "b")
	for _, id := range []string{"a", "b"} {
		if err := s.Down("ROOM1", id); err != nil {
			t.Fatal(err)
		}
	}
	tick(t, s, 3)
	if a, b := player(t, s, "a"), player(t, s, "b"); a.Downed || b.Downed {
		t.Fatalf("players with no one standing did not respawn: %+v %+v", a, b)
	}
}
