package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

const coopConfigVersion = 1

type CoopConfig struct {
	Version      int           `yaml:"version"`
	TickRate     int           `yaml:"tickRate"`
	MaxPlayers   int           `yaml:"maxPlayers"`
	RejoinGrace  time.Duration `yaml:"rejoinGrace"`
	ReviveTicks  int           `yaml:"reviveTicks"`
	RespawnTicks int           `yaml:"respawnTicks"`
}

func (c CoopConfig) Validate() error {
	if c.Version != coopConfigVersion || c.TickRate <= 0 || c.MaxPlayers < 1 || c.MaxPlayers > 4 || c.RejoinGrace <= 0 || c.ReviveTicks <= 0 || c.RespawnTicks <= 0 {
		return errors.New("coop: invalid version or tuning values")
	}
	return nil
}
func ParseCoopConfig(data []byte) (CoopConfig, error) {
	var c CoopConfig
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return c, err
	}
	return c, c.Validate()
}
func EncodeCoopConfig(c CoopConfig) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return yaml.Marshal(c)
}

func RoomCode(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return strings.ToUpper(hex.EncodeToString(sum[:])[:6])
}
func InviteLink(base, room string) string {
	return fmt.Sprintf("%s?room=%s", strings.TrimRight(base, "?&"), room)
}

type Player struct {
	ID              string
	Connected       bool
	Downed          bool
	ReviveProgress  int
	RespawnProgress int
}
type Room struct {
	Code    string
	Tick    uint64
	Players map[string]*Player
}
type Server struct {
	mu    sync.Mutex
	cfg   CoopConfig
	rooms map[string]*Room
}

func NewServer(cfg CoopConfig) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, rooms: make(map[string]*Room)}, nil
}
func (s *Server) Join(code, id string) (*Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[code]
	if r == nil {
		r = &Room{Code: code, Players: map[string]*Player{}}
		s.rooms[code] = r
	}
	if p := r.Players[id]; p != nil {
		p.Connected = true
		return r, nil
	}
	if len(r.Players) >= s.cfg.MaxPlayers {
		return nil, errors.New("coop: room full")
	}
	r.Players[id] = &Player{ID: id, Connected: true}
	return r, nil
}
func (s *Server) Leave(code, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.rooms[code]; r != nil {
		if p := r.Players[id]; p != nil {
			p.Connected = false
			p.RespawnProgress = 0
		}
	}
}
func (s *Server) Rejoin(code, id string) (*Room, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[code]
	if r == nil {
		return nil, false
	}
	p := r.Players[id]
	if p == nil || r.Tick > uint64(s.cfg.RejoinGrace.Seconds()*float64(s.cfg.TickRate)) && !p.Connected {
		return nil, false
	}
	p.Connected = true
	return r, true
}
func (s *Server) Tick(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[code]
	if r == nil {
		return
	}
	r.Tick++
	for _, p := range r.Players {
		if p.Downed {
			nearby := false
			for _, q := range r.Players {
				if q != p && q.Connected && !q.Downed {
					nearby = true
					break
				}
			}
			if nearby {
				p.ReviveProgress++
			} else {
				p.ReviveProgress = 0
			}
			if p.ReviveProgress >= s.cfg.ReviveTicks {
				p.Downed, p.ReviveProgress = false, 0
			}
		} else if !p.Connected {
			p.RespawnProgress++
			if p.RespawnProgress >= s.cfg.RespawnTicks {
				p.Connected, p.RespawnProgress = true, 0
			}
		}
	}
}
