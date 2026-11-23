package procgen_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/procgen"
	"github.com/nsgo-dev/game/packages/game/random"
)

func BenchmarkBSP80x50(b *testing.B) {
	cfg := procgen.BSPConfig{Width: 80, Height: 50, Loops: 3}
	b.ReportAllocs()
	for i := range b.N {
		if _, err := procgen.BSP(random.New(uint64(i)), cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCave80x50(b *testing.B) {
	cfg := procgen.CaveConfig{Width: 80, Height: 50, Join: true}
	b.ReportAllocs()
	for i := range b.N {
		if _, err := procgen.Cave(random.New(uint64(i)), cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWFC32x24(b *testing.B) {
	cfg := procgen.WFCConfig{Width: 32, Height: 24, Rules: loadRules(b)}
	b.ReportAllocs()
	for i := range b.N {
		if _, err := procgen.WFC(random.New(uint64(i)), cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRegions80x50(b *testing.B) {
	g, err := procgen.Cave(random.New(1), procgen.CaveConfig{Width: 80, Height: 50, Join: true})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for range b.N {
		if !g.Connected(procgen.IsFloor) {
			b.Fatal("disconnected")
		}
	}
}

func BenchmarkWFC64x64(b *testing.B) {
	cfg := procgen.WFCConfig{Width: 64, Height: 64, Rules: loadRules(b)}
	b.ReportAllocs()
	for i := range b.N {
		if _, err := procgen.WFC(random.New(uint64(i)), cfg); err != nil {
			b.Fatal(err)
		}
	}
}
