package main

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/idle"
	"go.yaml.in/yaml/v3"
)

// hearthYAML is the game's balance: its fuel, generators, upgrades, prestige, goal and offline rules. Change the numbers there, not in code; TestHearthDataIsCanonical keeps the file in the form encodeData writes.
//
//go:embed hearth.yaml
var hearthYAML []byte

// dataVersion is the hearth.yaml schema version parseData reads and encodeData writes.
const dataVersion = 1

// Upgrade effects: what buying an upgrade changes.
const (
	// effectGenerator multiplies one generator's output by Value.
	effectGenerator = "generator"
	// effectAll multiplies every generator's output by Value.
	effectAll = "all"
	// effectStoke multiplies the embers a stoke gathers by Value.
	effectStoke = "stoke"
	// effectFuel adds Value fuel per second, so the fire feeds itself.
	effectFuel = "fuel"
)

// hearthData is the parsed hearth.yaml.
type hearthData struct {
	// Version is the schema version; it must be dataVersion.
	Version int `yaml:"version"`
	// Fuel is how the fire burns.
	Fuel fuelData `yaml:"fuel"`
	// Generators produce embers every second, cheapest first.
	Generators []generatorData `yaml:"generators"`
	// Upgrades are one-off purchases that change production, in the order the shop offers them.
	Upgrades []upgradeData `yaml:"upgrades"`
	// Prestige is the reset that trades a run's embers for ash.
	Prestige prestigeData `yaml:"prestige"`
	// Goal is the purchase that wins the game.
	Goal goalData `yaml:"goal"`
	// Offline is how time away from the game is credited.
	Offline offlineData `yaml:"offline"`
}

// fuelData describes the fuel gauge, which empties at Burn per second; the fire goes out, losing the run, when it reaches zero.
type fuelData struct {
	// Capacity is the most fuel the hearth holds, and what a new run starts with.
	Capacity float64 `yaml:"capacity"`
	// Burn is the fuel the fire uses per second.
	Burn float64 `yaml:"burn"`
	// Stoke is the fuel one stoke adds.
	Stoke float64 `yaml:"stoke"`
	// Embers is what one stoke gathers before upgrades.
	Embers idle.Number `yaml:"embers"`
}

// generatorData is one generator: buying the n-th costs Cost × Growth^(n-1), and each owned one makes Rate embers per second.
type generatorData struct {
	// ID names the generator in saves and upgrades; it never changes once players have saves.
	ID string `yaml:"id"`
	// Name is what the shop shows.
	Name string `yaml:"name"`
	// Cost is the price of the first one.
	Cost idle.Number `yaml:"cost"`
	// Growth multiplies the price after every purchase; it is above 1.
	Growth float64 `yaml:"growth"`
	// Rate is the embers per second each one makes.
	Rate idle.Number `yaml:"rate"`
}

// upgradeData is a one-off purchase with one Effect.
type upgradeData struct {
	// ID names the upgrade in saves; it never changes once players have saves.
	ID string `yaml:"id"`
	// Name is what the shop shows.
	Name string `yaml:"name"`
	// Cost is its price.
	Cost idle.Number `yaml:"cost"`
	// Effect is effectGenerator, effectAll, effectStoke or effectFuel.
	Effect string `yaml:"effect"`
	// Target is the ID of the generator an effectGenerator upgrade multiplies; empty for the other effects.
	Target string `yaml:"target,omitempty"`
	// Value is the multiplier, above 1, or for effectFuel the fuel per second it adds.
	Value float64 `yaml:"value"`
}

// prestigeData describes scattering the ashes: a run that has earned Threshold × n² embers may restart for n ash, and each ash adds Bonus to every generator's multiplier for good.
type prestigeData struct {
	// Threshold is the embers a run must earn for its first ash.
	Threshold idle.Number `yaml:"threshold"`
	// Bonus is the production each ash adds, such as 0.1 for +10%.
	Bonus float64 `yaml:"bonus"`
}

// goalData is the purchase that wins the game.
type goalData struct {
	// Name is what the shop shows.
	Name string `yaml:"name"`
	// Cost is its price.
	Cost idle.Number `yaml:"cost"`
}

// offlineData describes how time away is credited, as idle.Offline does, and the fixed step the away time is replayed in.
type offlineData struct {
	// MinSeconds is the shortest absence credited; a quicker app switch credits nothing.
	MinSeconds int64 `yaml:"minSeconds"`
	// CapMinutes is the longest absence credited.
	CapMinutes int64 `yaml:"capMinutes"`
	// Efficiency is the fraction of production earned while away, from above 0 to 1.
	Efficiency float64 `yaml:"efficiency"`
	// StepSeconds is the length of each step the absence is replayed in.
	StepSeconds int64 `yaml:"stepSeconds"`
}

// offline returns the idle.Offline that credits an absence under these rules.
func (o offlineData) offline() idle.Offline {
	return idle.Offline{
		Min:        time.Duration(o.MinSeconds) * time.Second,
		Cap:        time.Duration(o.CapMinutes) * time.Minute,
		Efficiency: o.Efficiency,
	}
}

// step returns the length of one replayed step.
func (o offlineData) step() time.Duration { return time.Duration(o.StepSeconds) * time.Second }

// parseData decodes and validates a hearth.yaml document, refusing unknown fields and trailing documents.
func parseData(src []byte) (*hearthData, error) {
	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)
	var d hearthData
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("hearth.yaml: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("hearth.yaml: more than one document")
	}
	if err := d.validate(); err != nil {
		return nil, fmt.Errorf("hearth.yaml: %w", err)
	}
	return &d, nil
}

// mustParseData parses hearth.yaml, which is part of the program, panicking if it is invalid.
func mustParseData(src []byte) *hearthData {
	d, err := parseData(src)
	if err != nil {
		panic(err)
	}
	return d
}

// encodeData validates d and writes it as 2-space YAML with fields in declaration order, so parseData followed by encodeData returns the same bytes.
func encodeData(d *hearthData) ([]byte, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// validate reports the first rule d breaks.
func (d *hearthData) validate() error {
	if d.Version != dataVersion {
		return fmt.Errorf("version %d, want %d", d.Version, dataVersion)
	}
	f := d.Fuel
	if !(f.Capacity > 0) || math.IsInf(f.Capacity, 1) || !(f.Burn > 0) || math.IsInf(f.Burn, 1) || !(f.Stoke > 0) || f.Stoke > f.Capacity {
		return fmt.Errorf("fuel: capacity, burn and stoke must be positive and finite, and stoke at most capacity")
	}
	if f.Embers.Sign() <= 0 {
		return errors.New("fuel: embers must be positive")
	}
	if len(d.Generators) == 0 {
		return errors.New("no generators")
	}
	ids := map[string]bool{}
	for i, g := range d.Generators {
		switch {
		case g.ID == "" || g.Name == "":
			return fmt.Errorf("generator %d: id and name are required", i)
		case ids[g.ID]:
			return fmt.Errorf("generator %s: duplicate id", g.ID)
		case g.Cost.Sign() <= 0 || g.Rate.Sign() <= 0:
			return fmt.Errorf("generator %s: cost and rate must be positive", g.ID)
		case !(g.Growth > 1) || g.Growth > 10:
			return fmt.Errorf("generator %s: growth %v is not above 1 and at most 10", g.ID, g.Growth)
		}
		ids[g.ID] = true
	}
	for i, u := range d.Upgrades {
		switch {
		case u.ID == "" || u.Name == "":
			return fmt.Errorf("upgrade %d: id and name are required", i)
		case ids[u.ID]:
			return fmt.Errorf("upgrade %s: duplicate id", u.ID)
		case u.Cost.Sign() <= 0:
			return fmt.Errorf("upgrade %s: cost must be positive", u.ID)
		}
		switch u.Effect {
		case effectGenerator:
			if d.generator(u.Target) < 0 {
				return fmt.Errorf("upgrade %s: target %q is not a generator", u.ID, u.Target)
			}
		case effectAll, effectStoke, effectFuel:
			if u.Target != "" {
				return fmt.Errorf("upgrade %s: a %s upgrade has no target", u.ID, u.Effect)
			}
		default:
			return fmt.Errorf("upgrade %s: effect %q is not generator, all, stoke or fuel", u.ID, u.Effect)
		}
		if u.Effect == effectFuel && !(u.Value > 0) || u.Effect != effectFuel && !(u.Value > 1) || u.Value > 1e6 {
			return fmt.Errorf("upgrade %s: value %v is out of range", u.ID, u.Value)
		}
		ids[u.ID] = true
	}
	if d.Prestige.Threshold.Sign() <= 0 || !(d.Prestige.Bonus > 0) || d.Prestige.Bonus > 100 {
		return errors.New("prestige: threshold must be positive and bonus above 0 and at most 100")
	}
	if d.Goal.Name == "" || d.Goal.Cost.Sign() <= 0 {
		return errors.New("goal: name and a positive cost are required")
	}
	o := d.Offline
	if o.MinSeconds < 0 || o.CapMinutes <= 0 || o.CapMinutes > 7*24*60 || o.StepSeconds <= 0 || o.StepSeconds > 3600 || o.MinSeconds > o.CapMinutes*60 || !(o.Efficiency > 0) || o.Efficiency > 1 {
		return errors.New("offline: needs 0 <= minSeconds <= capMinutes*60, capMinutes from 1 to a week, stepSeconds from 1 to 3600 and efficiency above 0 and at most 1")
	}
	return nil
}

// generator returns the index of the generator called id, or -1.
func (d *hearthData) generator(id string) int {
	for i, g := range d.Generators {
		if g.ID == id {
			return i
		}
	}
	return -1
}
