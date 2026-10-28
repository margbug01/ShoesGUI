package main

import "github.com/egoist/mygo/ui"

// palette 全 app 的颜色，深浅色各一套，照 MySpotify 的：内容区纯色，侧栏略灰，
// 字、次要字、发丝线、悬停、按下各一档，再加一个强调色。good / warn / bad 只用来说连没连上、延迟快慢、出没出错
type palette struct {
	dark     bool
	bg       ui.Color // 内容区的底
	sidebar  ui.Color // 侧栏底下没有毛玻璃时铺的颜色
	card     ui.Color // 分组、卡片的底，比内容区深一点点
	text     ui.Color
	muted    ui.Color
	faint    ui.Color
	hairline ui.Color
	hover    ui.Color
	press    ui.Color
	accent   ui.Color
	onAccent ui.Color
	up       ui.Color // 网速图里的上传；下载用 accent
	good     ui.Color
	warn     ui.Color
	bad      ui.Color
}

func paletteFor(dark bool) palette {
	if dark {
		return palette{
			dark:     true,
			bg:       ui.Hex("#141416"),
			sidebar:  ui.Hex("#1c1c1f"),
			card:     ui.RGBA(255, 255, 255, 0.045),
			text:     ui.Hex("#f5f5f7"),
			muted:    ui.RGBA(235, 235, 245, 0.56),
			faint:    ui.RGBA(235, 235, 245, 0.3),
			hairline: ui.RGBA(255, 255, 255, 0.08),
			hover:    ui.RGBA(255, 255, 255, 0.06),
			press:    ui.RGBA(255, 255, 255, 0.1),
			accent:   ui.Hex("#0a84ff"),
			onAccent: ui.Hex("#ffffff"),
			up:       ui.Hex("#bf5af2"),
			good:     ui.Hex("#30d158"),
			warn:     ui.Hex("#ff9f0a"),
			bad:      ui.Hex("#ff453a"),
		}
	}
	return palette{
		bg:       ui.Hex("#ffffff"),
		sidebar:  ui.Hex("#f4f4f6"),
		card:     ui.RGBA(0, 0, 0, 0.028),
		text:     ui.Hex("#1d1d1f"),
		muted:    ui.RGBA(60, 60, 67, 0.62),
		faint:    ui.RGBA(60, 60, 67, 0.32),
		hairline: ui.RGBA(0, 0, 0, 0.07),
		hover:    ui.RGBA(0, 0, 0, 0.04),
		press:    ui.RGBA(0, 0, 0, 0.08),
		accent:   ui.Hex("#007aff"),
		onAccent: ui.Hex("#ffffff"),
		up:       ui.Hex("#af52de"),
		good:     ui.Hex("#34c759"),
		warn:     ui.Hex("#ff9500"),
		bad:      ui.Hex("#ff3b30"),
	}
}

// pal 当前深浅色的色板
func pal(c *ui.Context) palette { return paletteFor(c.Theme().Dark) }

// applyPalette 把色板填进 MyGo 的主题。按钮、输入框、下拉框的底和边用 MyGo 自带的（MySpotify 也是），
// 换掉的是底色、字色、强调色和报错色，开关、主按钮、焦点圈就都跟着强调色走
func applyPalette(t *ui.Theme, p palette) {
	t.Background = p.bg
	t.Text, t.TextMuted = p.text, p.muted
	t.Accent, t.AccentText = p.accent, p.onAccent
	t.AccentHover = p.accent.Mix(ui.RGB(255, 255, 255), 0.12)
	t.AccentPressed = p.accent.Mix(ui.RGB(0, 0, 0), 0.12)
	t.Selection = p.accent.Alpha(0.3)
	t.Focus = p.accent.Alpha(0.5)
	t.Danger = p.bad
}

// paneColor / paneBorder 分组和卡片：比内容区深一点点的底，加一圈发丝线
func paneColor(t *ui.Theme) ui.Color { return paletteFor(t.Dark).card }

func paneBorder(t *ui.Theme) ui.Color { return paletteFor(t.Dark).hairline }
