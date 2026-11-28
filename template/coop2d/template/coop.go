package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

// coopYAML is the room rules and tuning: the tick rate, the room size, the rejoin grace period, and the revive and respawn times.
//
//go:embed coop.yaml
var coopYAML []byte

// Coop is the parsed coop.yaml.
var Coop = mustParseCoopConfig(coopYAML)

// coopConfigVersion is the only version of coop.yaml this template reads.
const coopConfigVersion = 1

// CoopConfig is the schema of coop.yaml.
type CoopConfig struct {
	// Version is the schema version, coopConfigVersion.
	Version int `yaml:"version"`
	// TickRate is the authoritative server's fixed steps per second.
	TickRate int `yaml:"tickRate"`
	// MaxPlayers is the room size, from one to four.
	MaxPlayers int `yaml:"maxPlayers"`
	// RejoinGrace is how long a player who left keeps their place and state in the room.
	RejoinGrace time.Duration `yaml:"rejoinGrace"`
	// ReviveTicks is how many consecutive ticks a standing, connected teammate takes to revive a downed player.
	ReviveTicks int `yaml:"reviveTicks"`
	// RespawnTicks is how many consecutive ticks a downed player with no standing, connected teammate waits to get up alone.
	RespawnTicks int `yaml:"respawnTicks"`
}

// Validate reports the first value of c that is out of range.
func (c CoopConfig) Validate() error {
	switch {
	case c.Version != coopConfigVersion:
		return fmt.Errorf("coop: version %d, want %d", c.Version, coopConfigVersion)
	case c.TickRate <= 0:
		return fmt.Errorf("coop: tickRate %d is not positive", c.TickRate)
	case c.MaxPlayers < 1 || c.MaxPlayers > 4:
		return fmt.Errorf("coop: maxPlayers %d is not from 1 to 4", c.MaxPlayers)
	case c.GraceTicks() < 1:
		return fmt.Errorf("coop: rejoinGrace %s is shorter than one tick", c.RejoinGrace)
	case c.ReviveTicks <= 0:
		return fmt.Errorf("coop: reviveTicks %d is not positive", c.ReviveTicks)
	case c.RespawnTicks <= 0:
		return fmt.Errorf("coop: respawnTicks %d is not positive", c.RespawnTicks)
	}
	return nil
}

// GraceTicks is RejoinGrace in whole ticks.
func (c CoopConfig) GraceTicks() uint64 {
	if c.RejoinGrace <= 0 || c.TickRate <= 0 {
		return 0
	}
	return uint64(c.RejoinGrace * time.Duration(c.TickRate) / time.Second)
}

// ParseCoopConfig reads a coop.yaml document, refusing unknown fields, a second document and out-of-range values.
func ParseCoopConfig(data []byte) (CoopConfig, error) {
	var c CoopConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return CoopConfig{}, fmt.Errorf("coop: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return CoopConfig{}, errors.New("coop: more than one YAML document")
	}
	if err := c.Validate(); err != nil {
		return CoopConfig{}, err
	}
	return c, nil
}

// EncodeCoopConfig writes c as coop.yaml, with its fields in schema order, so that parsing and encoding the file changes nothing.
func EncodeCoopConfig(c CoopConfig) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return yaml.Marshal(c)
}

func mustParseCoopConfig(data []byte) CoopConfig {
	c, err := ParseCoopConfig(data)
	if err != nil {
		panic(err)
	}
	return c
}

// RoomCode derives a short, upper-case room code from seed, such as a server's room counter.
func RoomCode(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return strings.ToUpper(hex.EncodeToString(sum[:])[:6])
}

// InviteLink adds the room code to base as its room query parameter, keeping base's other parameters.
func InviteLink(base, code string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("coop: invite base %q: %w", base, err)
	}
	q := u.Query()
	q.Set("room", code)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// RoomFromInvite returns the room code of an invite link, and whether it has one.
func RoomFromInvite(link string) (string, bool) {
	u, err := url.Parse(link)
	if err != nil {
		return "", false
	}
	code := u.Query().Get("room")
	return code, code != ""
}

// Errors the Server returns.
var (
	ErrRoomExists  = errors.New("coop: room exists")
	ErrUnknownRoom = errors.New("coop: no room with that code")
	ErrRoomFull    = errors.New("coop: room full")
	ErrInRoom      = errors.New("coop: player already in the room")
	ErrNotInRoom   = errors.New("coop: player not in the room")
	ErrConnected   = errors.New("coop: player is connected")
	ErrGraceOver   = errors.New("coop: rejoin grace period over")
)

// Player is one player's state in a room.
type Player struct {
	ID        string
	Connected bool
	Downed    bool
	// LeftAt is the room tick at which the player left, while not Connected.
	LeftAt          uint64
	ReviveProgress  int
	RespawnProgress int
}

// RoomState is a copy of a room's state, with its players sorted by ID.
type RoomState struct {
	Code    string
	Tick    uint64
	Players []Player
}

// Player returns the player id of the room, and whether it is in the room.
func (r RoomState) Player(id string) (Player, bool) {
	i := slices.IndexFunc(r.Players, func(p Player) bool { return p.ID == id })
	if i < 0 {
		return Player{}, false
	}
	return r.Players[i], true
}

type room struct {
	code    string
	tick    uint64
	players map[string]*Player
}

// Server is the authoritative co-op rules for any number of rooms, advanced one fixed tick at a time. It is safe for concurrent use.
type Server struct {
	mu    sync.Mutex
	cfg   CoopConfig
	rooms map[string]*room
}

// NewServer returns a Server with no rooms.
func NewServer(cfg CoopConfig) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, rooms: make(map[string]*room)}, nil
}

// CreateRoom opens an empty room with code.
func (s *Server) CreateRoom(code string) error {
	if code == "" {
		return errors.New("coop: empty room code")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rooms[code] != nil {
		return ErrRoomExists
	}
	s.rooms[code] = &room{code: code, players: map[string]*Player{}}
	return nil
}

// Join adds the new player id to the room with code. A player who left and is still in the grace period uses Rejoin instead.
func (s *Server) Join(code, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[code]
	switch {
	case r == nil:
		return ErrUnknownRoom
	case r.players[id] != nil:
		return ErrInRoom
	case len(r.players) >= s.cfg.MaxPlayers:
		return ErrRoomFull
	}
	r.players[id] = &Player{ID: id, Connected: true}
	return nil
}

// Leave disconnects player id, who keeps their place and state for the grace period.
func (s *Server) Leave(code, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.player(code, id)
	if err != nil {
		return err
	}
	if p.Connected {
		p.Connected, p.LeftAt = false, s.rooms[code].tick
		p.ReviveProgress, p.RespawnProgress = 0, 0
	}
	return nil
}

// Rejoin reconnects player id, who left within the grace period, with the state they left with.
func (s *Server) Rejoin(code, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.player(code, id)
	switch {
	case err != nil:
		return err
	case p.Connected:
		return ErrConnected
	case s.rooms[code].tick-p.LeftAt > s.cfg.GraceTicks():
		return ErrGraceOver
	}
	p.Connected = true
	return nil
}

// Down marks player id as downed, as the game's rules do when the player's health runs out.
func (s *Server) Down(code, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.player(code, id)
	if err != nil {
		return err
	}
	if !p.Downed {
		p.Downed, p.ReviveProgress, p.RespawnProgress = true, 0, 0
	}
	return nil
}

// Tick advances the room with code by one fixed step: it removes the players whose grace period ran out, returning their IDs in order, and advances the revive or solo respawn of every connected, downed player. A room with no players left is closed.
func (s *Server) Tick(code string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[code]
	if r == nil {
		return nil, ErrUnknownRoom
	}
	r.tick++
	var expired []string
	for id, p := range r.players {
		if !p.Connected && r.tick-p.LeftAt > s.cfg.GraceTicks() {
			expired = append(expired, id)
		}
	}
	slices.Sort(expired)
	for _, id := range expired {
		delete(r.players, id)
	}
	if len(r.players) == 0 {
		delete(s.rooms, code)
		return expired, nil
	}

	// Who is standing is decided once, before any player gets up this tick, so that the result does not depend on the order players are visited in.
	standing := 0
	for _, p := range r.players {
		if p.Connected && !p.Downed {
			standing++
		}
	}
	for _, p := range r.players {
		if !p.Connected || !p.Downed {
			continue
		}
		if standing > 0 {
			p.RespawnProgress = 0
			p.ReviveProgress++
			if p.ReviveProgress >= s.cfg.ReviveTicks {
				p.Downed, p.ReviveProgress = false, 0
			}
		} else {
			p.ReviveProgress = 0
			p.RespawnProgress++
			if p.RespawnProgress >= s.cfg.RespawnTicks {
				p.Downed, p.RespawnProgress = false, 0
			}
		}
	}
	return expired, nil
}

// Room returns a copy of the state of the room with code, and whether it is open.
func (s *Server) Room(code string) (RoomState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[code]
	if r == nil {
		return RoomState{}, false
	}
	st := RoomState{Code: r.code, Tick: r.tick, Players: make([]Player, 0, len(r.players))}
	for _, p := range r.players {
		st.Players = append(st.Players, *p)
	}
	slices.SortFunc(st.Players, func(a, b Player) int { return strings.Compare(a.ID, b.ID) })
	return st, true
}

// player returns player id of the room with code; s.mu is held.
func (s *Server) player(code, id string) (*Player, error) {
	r := s.rooms[code]
	if r == nil {
		return nil, ErrUnknownRoom
	}
	p := r.players[id]
	if p == nil {
		return nil, ErrNotInRoom
	}
	return p, nil
}
