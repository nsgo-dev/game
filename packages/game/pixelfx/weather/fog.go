// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/fx/weather/fog.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See ../LICENSE and ../NOTICE.md.
//
// pixelfx adapts the banded fog with lit crests from Sprite Fusion's open-source fog (https://www.spritefusion.com/pixel-art-vfx/fog), MIT License, Copyright (c) 2026 Hugo Duprez.

package weather

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

// Fog is slow banks of moonlit fog at three depths drifting past each other, crests catching the light, while thin streaks of mist slide by in front. It is registered as "weather/fog".
var Fog = &pixelfx.Effect{
	Meta: pixelfx.Meta{
		Name:     "Fog",
		Desc:     "Slow banks of moonlit fog at three depths drift past each other, crests catching the light, while thin streaks of mist slide by in front.",
		Tags:     []string{"weather", "fog", "mist", "haze", "night", "ambient", "loop"},
		Size:     [2]int{320, 180},
		Origin:   [2]int{160, 170},
		Duration: fogDuration,
		Loop:     true,
		FPS:      30,
		BG:       "sky",
		Options: []pixelfx.Option{
			{Name: "palette", Default: pixelfx.Palette("#14121e", "#24213a", "#363250", "#4f4a6c", "#6d688b", "#918cab", "#bdb8d4", "#ece8f6")},
			{Name: "seed", Default: pixelfx.Num(5)},
		},
	},
	New: newFog,
}

func init() { pixelfx.Register("weather/fog", Fog) }

const (
	fogDuration = 12
	fogW        = 320
	fogBW       = 0.06
)

var fogT = [3]float64{0.3, 0.5, 0.7}

type fogBankSpec struct {
	top, base, bot float64
	sx, sy, lap    float64
	alpha          float64
	tone           [3]int
	crest          int
	warp           float64
	crestFrom      int
}

var fogBanks = [3]fogBankSpec{
	{top: -84, base: -40, bot: -20, sx: 1.6, sy: 2.2, lap: 1, alpha: 0.24, tone: [3]int{2, 3, 4}, crest: 5, warp: 16, crestFrom: 3},
	{top: -62, base: -24, bot: -2, sx: 1.0667, sy: 2.5, lap: 1, alpha: 0.3, tone: [3]int{2, 3, 4}, crest: 5, warp: 22, crestFrom: 2},
	{top: -42, base: -8, bot: 10, sx: 0.8, sy: 2.8, lap: 1, alpha: 0.36, tone: [3]int{2, 3, 5}, crest: 6, warp: 28, crestFrom: 1},
}

type fogInst struct {
	S      float64
	C      []uint32
	F      [3]*noiseField
	G      *noiseField
	FS     *noiseField
	br     [fogW]float32
	p1, p2 [3][fogW]float32
	warp   [fogW]float32
	rb     rowBuf
}

func newFog(o *pixelfx.Options) pixelfx.Instance {
	S := o.Seed()
	return &fogInst{
		S: S,
		C: pixelfx.Fit(o.Palette(), 8),
		F: [3]*noiseField{
			field(S*7+0, 256, 128, nil),
			field(S*7+1, 256, 128, nil),
			field(S*7+2, 256, 128, nil),
		},
		G:  field(S*7+11, 256, 128, cells4_8_16),
		FS: field(S*7+21, 256, 128, nil),
	}
}

func (fg *fogInst) Update(float64) {}

func (fg *fogInst) DrawBack(p *pixelfx.Painter, t float64) {
	fg.bank(p, t, 0)
	fg.bank(p, t, 1)
}

func (fg *fogInst) Draw(p *pixelfx.Painter, t float64) {
	fg.bank(p, t, 2)
	fg.streaks(p, t)
}

func (fg *fogInst) bank(p *pixelfx.Painter, t float64, n int) {
	B := fogBanks[n]
	F, G, C := fg.F[n], fg.G, fg.C
	drift := (256 * B.lap * t) / fogDuration
	boil := (128 * t) / fogDuration
	prev1, prev2 := &fg.p1[n], &fg.p2[n]
	tones := [3]uint32{C[B.tone[0]], C[B.tone[1]], C[B.tone[2]]}
	crest := pixelfx.Mix(C[B.crest], pixelfx.White, 0.12+0.08*float64(n))
	clear(prev1[:])
	clear(prev2[:])
	nf := float64(n)
	for i := 0; i < fogW; i++ {
		fg.br[i] = float32(2 * math.Sin((tau*t)/fogDuration+float64(i-160)*0.006+nf*1.7))
	}
	botFade := 4.0
	if B.bot < 10 {
		botFade = 14
	}
	rows(p, &Fog.Meta, &fg.rb, B.top-2, B.bot, func(y float64, R *rowBuf) {
		prof := pixelfx.Smooth(B.top, B.base, y) * (1 - pixelfx.Smooth(B.bot-botFade, B.bot+1, y))
		for i := 0; i < fogW; i += 2 {
			v := float32(sample(G, float64(i)*0.35+nf*70, y*0.5+boil) - 0.5)
			fg.warp[i] = v
			fg.warp[i+1] = v
		}
		for i := 0; i < fogW; i++ {
			x := float64(i-160) + 0.5
			w := float64(fg.warp[i])
			nn := pixelfx.Clamp((sample(F, x*B.sx+drift+w*B.warp, (y+float64(fg.br[i]))*B.sy+w*8)-0.5)*2.6+0.5) - 0.5
			f := 0.0
			if prof > 0.01 {
				f = (prof*0.72 + nn*0.95) * (0.6 + 0.5*prof)
			}
			above := float64(prev2[i])
			prev2[i] = prev1[i]
			prev1[i] = float32(f)
			if f <= fogT[0] {
				continue
			}
			th := bayer(float64(i), y) * fogBW
			band := 0
			if f > fogT[2]+th {
				band = 2
			} else if f > fogT[1]+th {
				band = 1
			}
			solid := 0
			if f > fogT[2] {
				solid = 2
			} else if f > fogT[1] {
				solid = 1
			}
			if solid >= B.crestFrom && above < fogT[solid] {
				R.c[i] = crest
				R.a[i] = float32(B.alpha * 0.95)
				R.l[i] = C[6]
				R.k[i] = 0.07
			} else {
				R.c[i] = tones[band]
				R.a[i] = float32(B.alpha * (0.55 + 0.25*float64(band)))
				if band == 2 {
					R.l[i] = C[5]
					R.k[i] = float32(0.03 + 0.02*nf)
				}
			}
			R.b[i] = 1
		}
	})
}

func (fg *fogInst) streaks(p *pixelfx.Painter, t float64) {
	F, C := fg.FS, fg.C
	drift := (256 * t) / fogDuration
	const sx, y0, y1 = 0.4, -40.0, 6.0
	rows(p, &Fog.Meta, &fg.rb, y0, y1, func(y float64, R *rowBuf) {
		env := pixelfx.Smooth(y0, y0+10, y) * (1 - pixelfx.Smooth(y1-10, y1, y))
		for i := 0; i < fogW; i++ {
			x := float64(i-160) + 0.5
			s := (sample(F, x*sx+drift, y*6.5+30) - 0.64) * 3 * env
			if s <= 0.05 {
				continue
			}
			if s > 0.35 {
				R.c[i] = C[5]
			} else {
				R.c[i] = C[4]
			}
			R.a[i] = float32(math.Min(0.4, s))
			R.b[i] = 1
			R.l[i] = C[5]
			R.k[i] = float32(s * 0.05)
		}
	})
}
