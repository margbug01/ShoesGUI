package main

import (
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
