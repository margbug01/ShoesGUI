package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// 这一份是各页共用的控件，样子照 MySpotify。Shoes 默认软件渲染，所以动效都是一下就停的短过渡，
// 什么都不动时不要帧（perf_test.go 守着）

// hoverFade 悬停、选中时底色渐变的时长
var hoverFade = ui.Animation{Duration: 120 * time.Millisecond}

// iconButton 无边框的圆形图标按钮：指针移上来出一圈淡底，按下去缩一下再弹回来
func iconButton(c *ui.Context, icon *ui.SVG, label string, disabled bool) ui.Element {
	p := pal(c)
	b := ui.ButtonBase(c).Size(32, 32).Center().Radius(999).Label(label).Tooltip(label).Disabled(disabled).FocusRing(false).Cursor(ui.CursorPointer)
	bg := ui.Transparent
	if b.Hovered() && !disabled {
		bg = ui.RGBA(128, 128, 128, 0.14)
	}
	b.Background(b.AnimateColor("bg", bg, hoverFade))
	pose := ui.Pose{}
	if b.Pressed() && !disabled {
		pose.Scale = 0.88
	}
	b.AnimatePose(pose, ui.Snappy)
	color := p.text
	if disabled {
		color = p.faint
	}
	return b.Children(func() { ui.Icon(c, icon).Size(18, 18).TextColor(color) })
}

// pillButton 胶囊按钮，可以带图标。悬停时底色提亮一点，按下去缩一下
func pillButton(c *ui.Context, icon *ui.SVG, label string, bg, fg ui.Color) ui.Element {
	b := ui.ButtonBase(c).Height(34).PaddingX(18).Gap(8).Radius(999).AlignItems(ui.Center).Label(label).TextColor(fg).Cursor(ui.CursorPointer)
	fill := bg
	if b.Hovered() {
		fill = bg.Mix(ui.RGB(255, 255, 255), 0.12)
	}
	b.Background(b.AnimateColor("bg", fill, hoverFade))
	pose := ui.Pose{}
	if b.Pressed() {
		pose.Scale = 0.96
	}
	b.AnimatePose(pose, ui.Snappy)
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14)
		}
		ui.Text(c, label).FontSize(13.5).FontWeight(600).SingleLine()
	})
	return b
}

// segmented AppKit 那样的分段控件：浅灰的槽里几段一样宽，段与段之间一根细线，选中的那段托在一块白色胶囊上，
// 换选时胶囊滑过去。selected 要绑跨帧的变量（见 choice）
func segmented(c *ui.Context, selected *int, labels ...string) ui.Element {
	t := c.Theme()
	const height, inset, radius = 26, 2, 7
	width := float32(0)
	for _, l := range labels {
		w, _ := c.MeasureText(0, ui.Span{Text: l, Size: 13})
		width = max(width, w+26)
	}
	track, capsule, divider := ui.RGBA(0, 0, 0, 0.06), ui.RGB(255, 255, 255), ui.RGBA(0, 0, 0, 0.13)
	if t.Dark {
		track, capsule, divider = ui.RGBA(255, 255, 255, 0.08), ui.RGBA(255, 255, 255, 0.22), ui.RGBA(255, 255, 255, 0.13)
	}
	parts := ui.SegmentedBase(c, selected, len(labels))
	at := parts.Track.AnimateTo("at", float32(*selected), ui.Animation{Spring: ui.SpringOptions{Duration: 280 * time.Millisecond, Bounce: 0.08}})
	n, dark := len(labels), t.Dark
	parts.Track.Height(height).Padding(inset).Radius(radius).Background(track).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		// 段与段之间的细线，挨着胶囊的不画
		for i := 1; i < n; i++ {
			if d := float32(i) - at; d > -1 && d < 2 {
				continue
			}
			x := r.X + inset + float32(i)*width
			p.Fill(ui.Rect{X: x - 0.5, Y: r.Y + 7, W: 1, H: r.H - 14}, divider, 0)
		}
		pill := ui.Rect{X: r.X + inset + at*width, Y: r.Y + inset, W: width, H: r.H - 2*inset}
		if !dark {
			p.Shadow(pill, radius-1, 0, 0.5, 1, 0, ui.RGBA(0, 0, 0, 0.2))
			p.Shadow(pill, radius-1, 0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.06))
		}
		p.Fill(pill, capsule, radius-1)
	}).Children(func() {
		for i, l := range labels {
			seg := parts.Segment(i).Width(width).Height(height - 2*inset).Center().Radius(radius - 1).Label(l).Cursor(ui.CursorPointer)
			if seg.Pressed() && i != *selected {
				seg.Background(divider)
			}
			seg.Children(func() { ui.Text(c, l).FontSize(13).TextColor(t.Text).SingleLine() })
		}
	})
	return parts.Track
}

// heading 页面顶上的大标题
func heading(c *ui.Context, title string) ui.Element {
	return ui.Text(c, title).FontSize(28).FontWeight(700).LetterSpacing(-0.5).SingleLine()
}

// section 设置页式的分组（照 MySpotify 的设置窗口）：小号灰字标题，下面一块圆角底，行与行之间一根发丝线
func section(c *ui.Context, title string, rows func()) {
	p := pal(c)
	if title != "" {
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(p.muted).Margin(16, 4, 6, 4)
	}
	ui.Column(c).FillWidth().Radius(12).Background(p.card).Border(1, p.hairline).Dividers(1, p.hairline).Children(rows)
}

// sectionNote 分组下面的一句小字
func sectionNote(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(12).TextColor(pal(c).muted).LineHeight(1.4).Margin(8, 4, 0, 4)
}

// row 分组里的一行：左边标题和说明，右边控件
func row(c *ui.Context, label, desc string, control func()) {
	rowWith(c, label, desc, "", nil, control)
}

// rowWith 和 row 一样，另外在说明下面放一行红字 problem，或者再放点别的（below）
func rowWith(c *ui.Context, label, desc, problem string, below, control func()) {
	p := pal(c)
	ui.Row(c).FillWidth().Padding(12, 16).Gap(16).AlignItems(ui.Center).MinHeight(48).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, label).FontSize(13.5).FontWeight(500)
			if desc != "" {
				ui.Text(c, desc).FontSize(12).TextColor(p.muted).LineHeight(1.4)
			}
			if problem != "" {
				ui.Text(c, problem).FontSize(12).TextColor(p.bad).LineHeight(1.4)
			}
			if below != nil {
				below()
			}
		})
		if control != nil {
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(control)
		}
	})
}

// searchBox MySpotify 侧栏那样的搜索框：放大镜、圆角浅底，有焦点时一圈强调色。focused 存上一帧有没有焦点，
// 要跨帧。返回里面的输入框，要聚焦就对它调 Focus
func searchBox(c *ui.Context, key, label, placeholder string, value *string, focused *bool) ui.Element {
	p := pal(c)
	var input ui.Element
	field := ui.Row(c.Key(key)).Grow(1).MinWidth(140).Height(32).PaddingX(10).Gap(7).Radius(9).AlignItems(ui.Center)
	if *focused {
		field.Background(p.bg).Border(1, p.accent.Alpha(0.6)).Shadow(0, 0, 0, 3, p.accent.Alpha(0.2))
	} else {
		field.Background(p.hover).Border(1, p.hairline)
	}
	field.Children(func() {
		ui.Icon(c, searchIcon).Size(14, 14).TextColor(p.muted)
		input = ui.TextInputBase(c.Key("input"), value).Grow(1).Placeholder(placeholder).FontSize(13).Label(label)
		*focused = input.Focused()
		if *value != "" {
			clear := ui.ButtonBase(c.Key("clear-search")).Size(18, 18).Center().Radius(9).Background(p.faint).Label("清空搜索").Cursor(ui.CursorPointer)
			clear.Children(func() { ui.Icon(c, closeIcon).Size(10, 10).TextColor(p.bg) })
			if clear.Clicked() {
				*value = ""
			}
		}
	})
	return input
}

// filterChip MySpotify 首页「全部 / 音乐 / 播客」那样的胶囊，后面跟一个数，选中的反色
func filterChip(c *ui.Context, label string, count int, chosen bool) ui.Element {
	p := pal(c)
	chip := ui.ButtonBase(c).Height(30).PaddingX(13).Gap(6).Radius(999).AlignItems(ui.Center).Label(label).FocusRing(false).Cursor(ui.CursorPointer)
	bg, fg, sub := p.hover, p.text, p.muted
	switch {
	case chosen:
		bg, fg, sub = p.text, p.bg, p.bg.Alpha(0.6)
	case chip.Hovered():
		bg = p.press
	}
	chip.Background(chip.AnimateColor("bg", bg, hoverFade))
	chip.Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(fg).SingleLine()
		ui.Text(c, fmt.Sprint(count)).FontSize(12).TextColor(sub).FontFeatures("tnum")
	})
	return chip
}
