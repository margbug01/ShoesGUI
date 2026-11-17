package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// 菜单栏图标旁的文字只能放一行（MyGo 的 Tray 只有 SetTitle），所以上下两行的网速直接画进图标图片里：
// 左边是原来的 S 图标，右边上传、下载各一行。等宽字体 + 固定位数，宽度不随网速变。
// shortcut: 字体读系统自带的 SF Mono，读不到就回退成一行文字（traySpeed），不自带字体文件

var trayFace struct {
	once sync.Once
	face font.Face
}

func speedFace() font.Face {
	trayFace.once.Do(func() {
		data, err := os.ReadFile("/System/Library/Fonts/SFNSMono.ttf")
		if err != nil {
			return
		}
		f, err := opentype.Parse(data)
		if err != nil {
			return
		}
		trayFace.face, _ = opentype.NewFace(f, &opentype.FaceOptions{Size: 13, DPI: 72, Hinting: font.HintingFull})
	})
	return trayFace.face
}

// traySpeedLines 上传、下载各一行，MB/s、两位小数，数字补到 6 位宽
func traySpeedLines(r rate) [2]string {
	mb := func(n int64) string { return fmt.Sprintf("%6.2f MB/s", min(float64(n)/(1<<20), 999.99)) }
	return [2]string{"↑" + mb(r.up), "↓" + mb(r.down)}
}

// traySpeedIcon 把 base（32×32 的图标）和两行网速拼成一张模板图。alpha 是文字的不透明度，没接管时和图标一样半透明
func traySpeedIcon(base []byte, lines [2]string, alpha uint8) ([]byte, error) {
	face := speedFace()
	if face == nil {
		return nil, errors.New("没有找到系统等宽字体")
	}
	icon, err := png.Decode(bytes.NewReader(base))
	if err != nil {
		return nil, err
	}
	const gap = 4
	ib := icon.Bounds()
	textW := font.MeasureString(face, lines[0]).Ceil()
	img := image.NewNRGBA(image.Rect(0, 0, ib.Dx()+gap+textW, ib.Dy()))
	draw.Draw(img, ib.Sub(ib.Min), icon, ib.Min, draw.Src)
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.NRGBA{A: alpha}), Face: face}
	for i, line := range lines {
		d.Dot = fixed.P(ib.Dx()+gap, 14+i*15)
		d.DrawString(line)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
