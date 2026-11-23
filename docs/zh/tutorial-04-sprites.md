# 教程 4：精灵

把篮子和星星画成精灵：绘制一张精灵表，用 `sprite.Atlas` 描述它的帧，用 `sprite.Animation` 播放星星的闪烁，再用一个 `sprite.Batch` 画出全部精灵。

完成后的代码在 [`examples/tutorial/ch04-sprites`](../../examples/tutorial/ch04-sprites/)。

## 图集

图集是一张打包了许多图片的图像，加上说明每张图片位置的帧。游戏通常用 TexturePacker 或 Aseprite 这样的打包工具导出图集，再用 `sprite.LoadAtlas` 加载，见 [sprite 指南](sprite.md)。为了让教程不需要任何图片文件，Star Catcher 改在 `art.go` 里用 Go 绘制精灵表，并手写帧表：

```go
func makeAtlas() *sprite.Atlas {
	sheet := image.NewRGBA(image.Rect(0, 0, sheetWidth, sheetHeight))
	paintBasket(sheet, 0)
	paintStar(sheet, basketWidth, 1, color.RGBA{R: 0xfd, G: 0xe0, B: 0x47, A: 0xff})
	paintStar(sheet, basketWidth+itemSize, 0.8, color.RGBA{R: 0xfa, G: 0xcc, B: 0x15, A: 0xff})

	basket := frame("basket", 0, basketWidth, basketHeight)
	star0 := frame("star_0", basketWidth, itemSize, itemSize)
	star1 := frame("star_1", basketWidth+itemSize, itemSize, itemSize)
	return &sprite.Atlas{
		Image:  core.ImageSource{Pixels: sheet},
		Frames: map[string]sprite.Frame{basket.Name: basket, star0.Name: star0, star1.Name: star1},
		Animations: map[string]sprite.Animation{
			"twinkle": {
				Loop: true,
				Frames: []sprite.AnimationFrame{
					{Frame: star0, Duration: twinkleFrame},
					{Frame: star1, Duration: twinkleFrame},
				},
			},
		},
	}
}
```

- 精灵表只有一行：篮子、一颗亮星，以及一颗更暗更小的星。`paintStar` 填充五角星内部的像素，`paintBasket` 绘制一只编织的篮子。
- `core.ImageSource{Pixels: sheet}` 把像素交给平台，平台只上传一次。
- `frame` 生成一个未裁剪的帧，轴心在中间，所以用它的精灵绕中心旋转。

## 精灵和批次

`sprite.Sprite` 摆放一个帧：原点放在哪里，如何旋转、缩放、翻转和淡出。`sprite.Batch` 按顺序绘制同一图集里的精灵：

```go
atlas := makeAtlas()
s.twinkle = atlas.Animations["twinkle"]
s.basket = sprite.NewSprite(atlas.Frames["basket"])
s.basket.OriginY = 0
s.star = sprite.NewSprite(s.twinkle.Frame())
s.batch = sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&s.basket, &s.star}}
```

`NewSprite` 把原点放在帧的轴心。篮子把原点移到上边缘，所以它的 `Y` 就是篮子的顶部，与世界里一致。

现在绘制时摆好两个精灵，再画出批次：

```go
s.basket.X, s.basket.Y = w.basketAt(a), basketY
s.star.X, s.star.Y = laneX(w.starLane), w.starAt(a)
// The star turns a little as it falls.
s.star.Rotation = s.star.Y / 60
s.batch.Draw(d)
```

## 动画

`Animation` 是一个值，自己记录播放到哪一帧。从图集里复制一份，在 `update` 里用循环的 `dt` 推进它，并显示它的当前帧：

```go
s.world.step(dt, move)
s.twinkle.Update(dt)
s.star.Frame = s.twinkle.Frame()
```

因为它随固定步长推进，闪烁和游戏节奏一致，到了[第 6 章](tutorial-06-screens.md)游戏暂停时它也会停下。

运行这一章：篮子现在像个篮子了，星星边下落边闪烁旋转。

## 测试美术

测试检查图集本身：每一帧中间有颜色、角落是透明的；还检查星星闪烁一次后换帧、两次后换回来：

```go
func TestStarTwinkles(t *testing.T) {
	var s *starCatcher
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx)
		return s.view()
	}, nsgotest.ManualFrames())

	first := s.star.Frame.Name
	s.update(twinkleFrame)
	if s.star.Frame.Name == first {
		t.Fatalf("star still shows %s after a twinkle frame", first)
	}
	s.update(twinkleFrame)
	if s.star.Frame.Name != first {
		t.Fatalf("star shows %s after two twinkle frames, want %s again", s.star.Frame.Name, first)
	}
}
```

`nsgotest.ManualFrames` 只在测试推进时钟时才运行显示帧，所以测试调用 `update` 时，循环不会自行调用它。一个应用测试还会用 `gametest.ImageDraws` 读出画布画的图像，它能看到批次那一次 `DrawAtlas` 里的每个精灵：两个，篮子和星星。

## 下一章

一颗星星无休止地下落还算不上游戏。[第 5 章](tutorial-05-rules.md)加上规则。
