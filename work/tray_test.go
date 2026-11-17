package main

import (
	"bytes"
	"image/png"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// 菜单栏网速要定长：字符数不变、单位不变、数字里不混普通空格（普通空格比数字窄）
func TestTraySpeedFixedWidth(t *testing.T) {
	want := ""
	for _, r := range []rate{{}, {up: 512, down: 10 << 10}, {up: 3 << 20, down: 87 << 20}, {up: 120 << 20, down: 5 << 30}} {
		got := traySpeed(r)
		if want == "" {
			want = got
		}
		if utf8.RuneCountInString(got) != utf8.RuneCountInString(want) || strings.Count(got, " ") != 2 || !strings.HasSuffix(got, " MB/s") {
			t.Fatalf("traySpeed(%v) = %q, want same shape as %q", r, got, want)
		}
	}
	if got := traySpeed(rate{up: 1 << 20, down: 5 << 30}); got != "↑\u2007\u20071.00 ↓999.99 MB/s" {
		t.Fatalf("got %q", got)
	}
}

// 上下两行的网速画进图标：不管网速多大，图片宽度不变；两行都真的画了东西
func TestTraySpeedIconFixedWidth(t *testing.T) {
	base := trayTemplateOnPNG
	var w int
	for _, r := range []rate{{}, {up: 512, down: 10 << 10}, {up: 3 << 20, down: 87 << 20}, {up: 120 << 20, down: 5 << 30}} {
		data, err := traySpeedIcon(base, traySpeedLines(r), 255)
		if err != nil {
			t.Skip("系统没有 SF Mono：", err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if w == 0 {
			w = img.Bounds().Dx()
			if dump := os.Getenv("TRAY_DUMP"); dump != "" {
				os.WriteFile(dump, data, 0644)
			}
		}
		if img.Bounds().Dx() != w {
			t.Fatalf("width %d, want %d for %v", img.Bounds().Dx(), w, r)
		}
		for _, rows := range [][2]int{{0, 16}, {16, 32}} {
			ink := false
			for y := rows[0]; y < rows[1]; y++ {
				for x := 36; x < w; x++ {
					if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
						ink = true
					}
				}
			}
			if !ink {
				t.Fatalf("rows %v empty for %v", rows, r)
			}
		}
	}
}
