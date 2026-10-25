package main

import "github.com/egoist/mygo/ui"

func mamiColors(t *ui.Theme) {
	if t.Dark {
		t.Background = ui.Hex("#1d2025")
		t.Surface = ui.Hex("#24282e")
		t.SurfaceHover = ui.Hex("#2b3037")
		t.SurfacePressed = ui.Hex("#323841")
		t.Border = ui.RGBA(255, 255, 255, 0.09)
		t.Text = ui.Hex("#e7eaee")
		t.TextMuted = ui.Hex("#8d949f")
		t.Accent = ui.Hex("#a7b0bc")
		t.AccentHover = ui.Hex("#b6bec8")
		t.AccentPressed = ui.Hex("#98a1ad")
		t.Selection = ui.RGBA(150, 165, 190, 0.32)
		t.Focus = ui.RGBA(200, 210, 225, 0.45)
		t.AccentText = ui.Hex("#15181c")
	} else {
		t.Background = ui.Hex("#ffffff")
		t.Surface = ui.Hex("#ffffff")
		t.SurfaceHover = ui.Hex("#f3f5f8")
		t.SurfacePressed = ui.Hex("#e9edf2")
		t.Border = ui.RGBA(20, 30, 45, 0.11)
		t.Text = ui.Hex("#1c2027")
		t.TextMuted = ui.Hex("#6a717c")
		t.Accent = ui.Hex("#23272e")
		t.AccentHover = ui.Hex("#363b44")
		t.AccentPressed = ui.Hex("#15181d")
		t.Selection = ui.RGBA(70, 100, 140, 0.2)
		t.Focus = ui.RGBA(35, 39, 46, 0.35)
		t.AccentText = ui.Hex("#ffffff")
	}
}

// paneColor / paneBorder 设置分组、引导里那句话的卡片：半透明白加一圈很淡的边
func paneColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(255, 255, 255, 0.045)
	}
	return ui.RGBA(255, 255, 255, 0.66)
}

func paneBorder(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(255, 255, 255, 0.07)
	}
	return ui.RGBA(20, 30, 45, 0.06)
}

// sideSelected 侧边栏选中项的底：一块半透明的白
func sideSelected(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(255, 255, 255, 0.08)
	}
	return ui.RGBA(255, 255, 255, 0.8)
}

// sidebarTheme 侧边栏换一份主题：自己不铺底（坐在整窗底纹上），图标用淡灰。
// mygo 的选中项拿焦点时用 Accent 铺底、AccentText 写字，所以 Accent 就是没选中的图标色，
// AccentText 是选中后图标和字的颜色，底色由 sideItem 盖成半透明白
func sidebarTheme(t *ui.Theme) *ui.Theme {
	s := *t
	s.Surface = ui.Transparent
	s.Accent = t.TextMuted
	s.AccentText = t.Text
	s.SurfacePressed = sideSelected(t)
	if t.Dark {
		s.SurfaceHover = ui.RGBA(255, 255, 255, 0.045)
	} else {
		s.SurfaceHover = ui.RGBA(20, 30, 45, 0.045)
	}
	return &s
}
