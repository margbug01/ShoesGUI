package main

import (
	"image"
	"math"
	"math/rand/v2"

	"github.com/egoist/mygo/ui"
)

// 颗粒渐变底纹（cover-wall.html 那套的 Go 版）：上浅偏冷、下深偏暖的竖向渐变，叠四团边缘渐隐的色团，再盖一层颗粒。
// mygo 没有径向渐变和混合模式，所以分两层：
//   - 渐变+色团画成一张 1/4 尺寸的小图，交给 GPU 拉伸。它本身极平滑，放大不会糊，窗口拖大小时也只重算这张小图（< 1ms）。
//   - 颗粒是一块 128×128 的灰度噪点，按一个噪点 = 一个物理像素平铺。叠加（overlay）没法在 GPU 上做，
//     用「黑点/白点 + 按噪点深浅给透明度」近似。黑白透明度要压低：overlay 在很亮、很暗的地方颗粒本来就弱，
//     近似做不到这点，给高了亮处会像电视雪花。

type blob struct {
	c       [3]float32 // 0..1
	x, y, s float32    // 圆心（占宽高的比例）、直径（占宽高几何平均的比例）
}

type palette struct {
	top, bottom [3]float32
	blobs       [4]blob
}

func hexRGB(s string) [3]float32 {
	c := ui.Hex(s)
	return [3]float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255}
}

func mkBlob(hex string, x, y, s float32) blob { return blob{hexRGB(hex), x, y, s} }

// mistLight 雾灰：上面近白偏冷，下面淡灰蓝，四团只差一两档的灰蓝。设置窗口和引导整窗都铺它，要淡到看着像纸
var mistLight = palette{
	top: hexRGB("#f7f8fa"), bottom: hexRGB("#e4e9ef"),
	blobs: [4]blob{
		mkBlob("#dde6f0", 0.12, 0.02, 0.95),
		mkBlob("#eef1f5", 0.98, 0.30, 0.80),
		mkBlob("#d6dee8", 0.82, 1.00, 1.10),
		mkBlob("#e7ecf2", 0.00, 0.72, 0.90),
	},
}

// mistDark 深色模式的雾灰：石墨灰，上面略亮，色团只比底亮暗一点点
var mistDark = palette{
	top: hexRGB("#23272d"), bottom: hexRGB("#121418"),
	blobs: [4]blob{
		mkBlob("#2b3038", 0.12, 0.02, 0.95),
		mkBlob("#1e2228", 0.98, 0.30, 0.80),
		mkBlob("#15181c", 0.82, 1.00, 1.10),
		mkBlob("#262a31", 0.00, 0.72, 0.90),
	},
}

var (
	mistTexture     = &texture{pal: mistLight, grain: 0.3}
	mistDarkTexture = &texture{pal: mistDark, grain: 0.45}
)

// windowTexture 设置窗口整窗的底纹，跟着深浅色走
func windowTexture(dark bool) *texture {
	if dark {
		return mistDarkTexture
	}
	return mistTexture
}

// blobAlpha 和 CSS 的 closest-side 色团一样：中心不透明，45% 处 0.55，75% 处 0.18，边上 0；段与段之间再用 smoothstep 抹平
func blobAlpha(d float32) float32 {
	stops := [...][2]float32{{0, 1}, {0.45, 0.55}, {0.75, 0.18}, {1, 0}}
	if d >= 1 {
		return 0
	}
	for i := 1; i < len(stops); i++ {
		if d <= stops[i][0] {
			a, b := stops[i-1], stops[i]
			t := (d - a[0]) / (b[0] - a[0])
			t = t * t * (3 - 2*t)
			return a[1] + (b[1]-a[1])*t
		}
	}
	return 0
}

// paintTexture 渐变+色团，w×h 像素，不带颗粒
func paintTexture(p palette, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fw, fh := float32(w), float32(h)
	unit := float32(math.Sqrt(float64(fw * fh))) // 直径按几何平均算：宽窗口里色团不会只顾横向
	for y := range h {
		v := (float32(y) + 0.5) / fh
		for x := range w {
			var c [3]float32
			for k := range 3 {
				c[k] = p.top[k] + (p.bottom[k]-p.top[k])*v
			}
			for _, b := range p.blobs {
				r := b.s * unit / 2
				dx, dy := float32(x)+0.5-b.x*fw, float32(y)+0.5-b.y*fh
				d := float32(math.Sqrt(float64(dx*dx+dy*dy))) / r
				if a := blobAlpha(d); a > 0 {
					for k := range 3 {
						c[k] += (b.c[k] - c[k]) * a
					}
				}
			}
			i := img.PixOffset(x, y)
			for k := range 3 {
				img.Pix[i+k] = uint8(min(max(c[k], 0), 1)*255 + 0.5)
			}
			img.Pix[i+3] = 255
		}
	}
	return img
}

const grainSize = 128

// grainTile 128×128 颗粒：噪点 n 偏暗画黑、偏亮画白，透明度跟着偏离 0.5 的程度走。种子固定，每次一样
func grainTile(dark, light float32) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, grainSize, grainSize))
	rng := rand.New(rand.NewPCG(7, 11))
	for i := 0; i < len(img.Pix); i += 4 {
		d := rng.Float32()*2 - 1
		v, a := uint8(255), d*light
		if d < 0 {
			v, a = 0, -d*dark
		}
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = v, v, v, uint8(a*255+0.5)
	}
	return img
}

// texture 一块颗粒渐变底：缓存那张小图，尺寸变了才重画
type texture struct {
	pal   palette
	grain float32 // 颗粒强度，1 ≈ CSS 里 overlay 0.4
	bmp   *ui.Bitmap
	w, h  int
}

var grainBmps = map[float32]*ui.Bitmap{}

func grainBitmap(k float32) *ui.Bitmap {
	if b, ok := grainBmps[k]; ok {
		return b
	}
	b := ui.NewBitmap(grainTile(0.2*k, 0.18*k)).Pixelated()
	grainBmps[k] = b
	return b
}

// textureDownscale 色团小图比 DIP 尺寸小几倍
const textureDownscale = 4

// draw 铺满 r（DIP），圆角 radius。在 Element.Draw 里调
func (tx *texture) draw(p *ui.Painter, r ui.Rect, radius float32) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	w, h := max(int(math.Ceil(float64(r.W/textureDownscale))), 2), max(int(math.Ceil(float64(r.H/textureDownscale))), 2)
	if tx.bmp == nil || w != tx.w || h != tx.h {
		tx.bmp, tx.w, tx.h = ui.NewBitmap(paintTexture(tx.pal, w, h)), w, h
	}
	p.Clip(r, radius, func() {
		p.ImageRegion(tx.bmp, ui.Rect{}, r, ui.ImageOptions{})
		drawGrain(p, r, tx.grain)
	})
}

// drawGrain 在 r 里平铺颗粒，一个噪点压一个屏幕像素。不裁剪，圆角由外面的 Clip 管
func drawGrain(p *ui.Painter, r ui.Rect, k float32) {
	if k <= 0 {
		return
	}
	s := p.Scale()
	if s <= 0 {
		s = 1
	}
	g := grainBitmap(k)
	tile := grainSize / s
	// 从物理像素的整数格开始铺
	x0 := float32(math.Floor(float64(r.X*s))) / s
	y0 := float32(math.Floor(float64(r.Y*s))) / s
	for y := y0; y < r.Y+r.H; y += tile {
		for x := x0; x < r.X+r.W; x += tile {
			p.ImageRegion(g, ui.Rect{}, ui.Rect{X: x, Y: y, W: tile, H: tile}, ui.ImageOptions{})
		}
	}
}
