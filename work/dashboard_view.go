package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

var (
	refreshIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 12a8 8 0 1 1-2.34-5.66M20 4v5h-5"/></svg>`))
	powerIcon   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 3.5v8"/><path d="M6.6 6.8a7.6 7.6 0 1 0 10.8 0"/></svg>`))
	copyIcon    = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="8" y="8" width="12" height="12" rx="2.5"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/></svg>`))
)

// card 仪表盘上的一块：左上一行小标题，aside 放在标题右边。span 为 -1 时占满一行
func card(c *ui.Context, title string, span int, aside, body func()) {
	p := pal(c)
	column := ui.Column(c).MinWidth(0).Padding(14, 16, 16, 16).Gap(6).Radius(12).Background(p.card).Border(1, p.hairline)
	if span != 0 {
		column.ColumnSpan(span)
	}
	column.Children(func() {
		ui.Row(c).FillWidth().MinHeight(22).AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(p.muted).SingleLine().Grow(1)
			if aside != nil {
				aside()
			}
		})
		body()
	})
}

// dashboardView 一张连接卡（状态、连接/断开、出站模式），网速图，下面是当前节点和出口 IP
func (a *application) dashboardView(c *ui.Context, status string, ready, proxy, busy bool) {
	gen, _, port := a.engine.runInfo()
	if key := fmt.Sprintf("%d/%d", gen, a.dash.route); ready && proxy && !a.dash.checking && key != a.dash.exitKey {
		a.checkExit(key, port)
	}
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		a.connectionCard(c, status, ready, proxy, busy)
		grid := ui.Grid(c.Key("dashboard")).FillWidth().Gap(12)
		columns := 2
		if w := grid.Bounds().W; w > 0 && w < 520 {
			columns = 1
		}
		grid.Columns(columns).Children(func() {
			a.trafficCard(c, ready)
			a.nodeCard(c)
			a.exitCard(c, ready, proxy)
		})
	})
}

// connectionCard 最上面那张：大的状态灯、现在怎样、连接/断开，下面一行出站模式
func (a *application) connectionCard(c *ui.Context, status string, ready, proxy, busy bool) {
	p := pal(c)
	_, since, port := a.engine.runInfo()
	if port == "" {
		port = a.settings.Port
	}
	hasNodes := len(a.settings.Nodes) > 0
	state, bad := runState(status, ready, proxy, hasNodes, since, time.Now())
	title, sub, tone := "未连接", "内核已就绪，点「连接」后浏览器才走节点", p.muted
	switch {
	case busy:
		title, sub = "处理中…", "正在切换系统代理"
	case proxy:
		title, sub, tone = "已连接", state+" · 系统代理 127.0.0.1:"+port, p.good
	case !hasNodes:
		title, sub = "还没有节点", "有订阅链接就直接粘贴导入，也可以手动添加"
	case bad:
		title, sub, tone = status, "", p.bad
	case !ready:
		title, sub = status, "内核正在启动"
	}
	ui.Column(c).FillWidth().Radius(12).Background(p.card).Border(1, p.hairline).Dividers(1, p.hairline).Children(func() {
		ui.Row(c).FillWidth().Padding(16, 16, 16, 18).Gap(14).AlignItems(ui.Center).Children(func() {
			light := ui.Box(c).Size(44, 44).Radius(22).Center().Shrink(0).Background(tone.Alpha(0.14))
			if proxy {
				light.Shadow(0, 0, 0, 4, tone.Alpha(0.08))
			}
			light.Children(func() { ui.Icon(c, powerIcon).Size(21, 21).TextColor(tone) })
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, title).FontSize(19).FontWeight(700).SingleLine()
				if bad {
					// 报错让人去看日志，那就直接能点过去
					link := ui.ButtonBase(c.Key("open-logs")).Label("打开日志").Cursor(ui.CursorPointer)
					link.Children(func() { ui.Text(c, "点这里看日志").FontSize(12.5).TextColor(p.bad).Underline() })
					if link.Clicked() {
						a.tab, a.logPane = "日志", paneLogs
					}
				} else if sub != "" {
					ui.Text(c, sub).FontSize(12.5).TextColor(p.muted).FontFeatures("tnum").LineHeight(1.4)
				}
			})
			if !hasNodes {
				a.addButtons(c, "dash")
				return
			}
			label, bg, fg := "连接", p.accent, p.onAccent
			if proxy {
				label, bg, fg = "断开", neutralFill(p), p.text
			}
			if busy {
				label = "处理中…"
			}
			button := pillButton(c.Key("connect"), powerIcon, label, bg, fg).MinWidth(96).Disabled(busy)
			if busy {
				button.Opacity(0.6)
			}
			if button.Clicked() {
				a.toggleConnect()
			}
		})
		rowWith(c, "出站模式", modes[modeIndex(a.settings.Mode)].hint, "", nil, func() {
			p, chosen, changed := a.modePick.bind(modeIndex(a.settings.Mode))
			segmented(c.Key("mode"), p, modes[0].label, modes[1].label, modes[2].label)
			if changed {
				a.setMode(modes[chosen].key)
			}
		})
	})
}

// neutralFill 不是主操作的胶囊按钮的底
func neutralFill(p palette) ui.Color {
	if p.dark {
		return ui.Hex("#2c2c30")
	}
	return ui.Hex("#e9e9ee")
}

// addButtons 没有节点时的两个入口。大多数人手里是订阅，所以粘贴导入是主按钮
func (a *application) addButtons(c *ui.Context, key string) {
	ui.Row(c).Gap(8).Shrink(0).Children(func() {
		if ui.Button(c.Key(key+"-add"), "手动添加").Clicked() {
			a.tab = "代理"
			a.beginEdit(-1)
		}
		if ui.PrimaryButton(c.Key(key+"-import"), "粘贴导入").Clicked() {
			a.tab = "代理"
			a.openImport()
		}
	})
}

// trafficCard 网速：上下行的大数字，过去一分钟的曲线，下面一排本次流量、连接数和内核内存
func (a *application) trafficCard(c *ui.Context, ready bool) {
	p := pal(c)
	now := rate{}
	if n := len(a.dash.speeds); n > 0 {
		now = a.dash.speeds[n-1]
	}
	card(c, "网速", -1, func() {
		ui.Text(c, "过去 1 分钟").FontSize(12).TextColor(p.faint)
	}, func() {
		// 左边上下行的大数字，右边过去一分钟的曲线
		ui.Row(c).FillWidth().Gap(18).AlignItems(ui.Center).Padding(2, 0, 0, 0).Children(func() {
			ui.Column(c).Width(132).Shrink(0).Gap(14).Children(func() {
				for _, item := range []struct {
					label string
					value int64
					color ui.Color
				}{{"下载", now.down, p.accent}, {"上传", now.up, p.up}} {
					ui.Column(c).Gap(2).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Box(c).Size(8, 8).Radius(4).Background(item.color)
							ui.Text(c, item.label).FontSize(12).TextColor(p.muted)
						})
						ui.Text(c, formatBytes(item.value)+"/s").FontSize(20).FontWeight(650).LetterSpacing(-0.3).FontFeatures("tnum").SingleLine()
					})
				}
			})
			a.trafficChart(c)
		})
		conns, memory := "—", "—"
		if ready {
			conns = strconv.Itoa(a.dash.conns)
			if a.dash.memory > 0 {
				memory = formatBytes(a.dash.memory)
			}
		}
		// 本次 = 从打开 Shoes 算起，经过内核的都算，直连的也算
		ui.Row(c).FillWidth().Margin(8, 0, 0, 0).Padding(12, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.hairline).Children(func() {
			for _, stat := range [][2]string{{"本次下载", formatBytes(a.dash.down)}, {"本次上传", formatBytes(a.dash.up)}, {"活动连接", conns}, {"内核内存", memory}} {
				ui.Column(c).Grow(1).Basis(0).Gap(3).MinWidth(0).Children(func() {
					ui.Text(c, stat[0]).FontSize(11.5).TextColor(p.muted).SingleLine()
					ui.Text(c, stat[1]).FontSize(15).FontWeight(600).SingleLine().FontFeatures("tnum")
				})
			}
		})
	})
}

// trafficChart 过去 60 秒的上下行。刻度跟着最大值走，指针放上去看那一秒的数
func (a *application) trafficChart(c *ui.Context) {
	p := pal(c)
	speeds := a.dash.speeds
	peak := int64(0)
	for _, s := range speeds {
		peak = max(peak, s.up, s.down)
	}
	top := rateScale(peak)
	box := ui.Box(c.Key("traffic-chart")).Grow(1).MinWidth(0).Height(chartHeight).Label("过去一分钟的网速")
	hover := -1
	if x, _, over := box.PointerPosition(); over && len(speeds) > 0 {
		if w := box.Bounds().W; w > 0 {
			step := w / float32(speedSamples-1)
			if i := len(speeds) - 1 - int(math.Round(float64((w-x)/step))); i >= 0 && i < len(speeds) {
				hover = i
			}
		}
	}
	box.Draw(func(pt *ui.Painter, r ui.Rect) { drawTraffic(pt, r, speeds, top, hover, p) })
	box.Children(func() {
		// 刻度的字在线的右上方
		for _, tick := range []struct {
			v  int64
			at float32
		}{{top, 0}, {top / 2, 0.5}} {
			y := chartTop + (chartHeight-1-chartTop)*tick.at
			ui.Text(c, rateText(tick.v)).FontSize(10.5).TextColor(p.faint).FontFeatures("tnum").Absolute().Right(0).Top(y - 15)
		}
		if hover < 0 {
			return
		}
		// 悬停的那一秒：几秒前、下载、上传
		s := speeds[hover]
		ago := "现在"
		if n := len(speeds) - 1 - hover; n > 0 {
			ago = fmt.Sprintf("%d 秒前", n)
		}
		w := box.Bounds().W
		x := w - float32(len(speeds)-1-hover)*(w/float32(speedSamples-1))
		tip := ui.Column(c).Absolute().Top(4).Gap(2).Padding(6, 9).Radius(8).Background(p.bg).Border(1, p.hairline).Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.12)).PassThrough()
		if x > w-150 {
			tip.Right(w - x + 10)
		} else {
			tip.Left(x + 10)
		}
		tip.Children(func() {
			ui.Text(c, ago).FontSize(11).TextColor(p.muted)
			for _, line := range []struct {
				name  string
				v     int64
				color ui.Color
			}{{"下载", s.down, p.accent}, {"上传", s.up, p.up}} {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).Background(line.color)
					ui.Text(c, line.name+" "+formatBytes(line.v)+"/s").FontSize(12).FontFeatures("tnum").SingleLine()
				})
			}
		})
	})
}

// chartHeight 网速图的高；chartTop 是最上面那根刻度线离顶的距离，留给刻度的字
const (
	chartHeight = 118
	chartTop    = 16
)

// rateScale 刻度的顶：比最大值大一成以上的 1、2、5 档，最少 20 KB/s，闲着时别把几百字节的噪声放大成满格
func rateScale(peak int64) int64 {
	for _, unit := range []int64{1 << 10, 1 << 20, 1 << 30} {
		for _, m := range []int64{1, 2, 5, 10, 20, 50, 100, 200, 500} {
			if v := m * unit; v >= 20<<10 && v >= peak+peak/10 {
				return v
			}
		}
	}
	return peak + peak/10
}

// rateText 刻度上的网速：整数不带小数，「512 KB/s」「2.5 MB/s」
func rateText(n int64) string {
	unit, size := "KB", float64(1<<10)
	switch {
	case n >= 1<<30:
		unit, size = "GB", 1<<30
	case n >= 1<<20:
		unit, size = "MB", 1<<20
	}
	text := strconv.FormatFloat(float64(n)/size, 'f', 1, 64)
	return strings.TrimSuffix(text, ".0") + " " + unit + "/s"
}

// drawTraffic 三根刻度线，下载是带渐变的蓝色曲线，上传是淡一点的紫色；最新的一秒在最右边，打一个点
func drawTraffic(p *ui.Painter, r ui.Rect, speeds []rate, top int64, hover int, pal palette) {
	base := r.Y + r.H - 1
	plotTop := r.Y + chartTop
	h := base - plotTop
	for _, at := range []float32{0, 0.5, 1} {
		color := pal.hairline
		if at == 1 {
			color = pal.faint.Alpha(0.35) // 底线比刻度线实一点
		}
		p.Fill(ui.Rect{X: r.X, Y: plotTop + h*at, W: r.W, H: 1}, color, 0)
	}
	n := len(speeds)
	if n < 2 {
		return
	}
	step := r.W / float32(speedSamples-1)
	xs, down, up := make([]float32, n), make([]float32, n), make([]float32, n)
	y := func(v int64) float32 { return base - min(1, float32(v)/float32(top))*h }
	for i, s := range speeds {
		xs[i] = r.X + r.W - float32(n-1-i)*step
		down[i], up[i] = y(s.down), y(s.up)
	}
	for _, series := range []struct {
		ys    []float32
		color ui.Color
		fill  float32
		width float32
	}{{up, pal.up, 0.14, 1.5}, {down, pal.accent, 0.26, 2}} {
		var area, line ui.Path
		area.MoveTo(xs[0], base).LineTo(xs[0], series.ys[0])
		smoothTo(&area, xs, series.ys)
		area.LineTo(xs[n-1], base).Close()
		line.MoveTo(xs[0], series.ys[0])
		smoothTo(&line, xs, series.ys)
		p.FillPathGradient(&area, ui.LinearGradient{From: series.color.Alpha(series.fill), To: series.color.Alpha(0), Angle: 180})
		p.StrokePath(&line, series.width, series.color)
	}
	dot := func(x, y float32, color ui.Color) {
		var ring, core ui.Path
		ring.Circle(x, y, 5)
		core.Circle(x, y, 3.2)
		p.FillPath(&ring, pal.bg)
		p.FillPath(&core, color)
	}
	if hover >= 0 {
		p.Fill(ui.Rect{X: xs[hover] - 0.5, Y: plotTop, W: 1, H: h}, pal.faint, 0)
		dot(xs[hover], up[hover], pal.up)
		dot(xs[hover], down[hover], pal.accent)
		return
	}
	dot(xs[n-1], down[n-1], pal.accent)
}

// smoothTo 从 (xs[0], ys[0]) 起画一条过所有点的平滑曲线，每两点之间一段三次贝塞尔
func smoothTo(path *ui.Path, xs, ys []float32) {
	m := monotoneSlopes(xs, ys)
	for i := 0; i+1 < len(xs); i++ {
		h := (xs[i+1] - xs[i]) / 3
		path.CubeTo(xs[i]+h, ys[i]+m[i]*h, xs[i+1]-h, ys[i+1]-m[i+1]*h, xs[i+1], ys[i+1])
	}
}

// monotoneSlopes 单调三次插值（Fritsch–Carlson）在每个点的斜率：两点之间的曲线不会冲出这两点的范围，
// 网速不会画成负的，平的地方也不会鼓起来
func monotoneSlopes(xs, ys []float32) []float32 {
	n := len(xs)
	d := make([]float32, n-1)
	for i := range d {
		d[i] = (ys[i+1] - ys[i]) / (xs[i+1] - xs[i])
	}
	m := make([]float32, n)
	m[0], m[n-1] = d[0], d[n-2]
	for i := 1; i < n-1; i++ {
		if d[i-1]*d[i] > 0 {
			m[i] = (d[i-1] + d[i]) / 2
		}
	}
	for i := range d {
		if d[i] == 0 {
			m[i], m[i+1] = 0, 0
			continue
		}
		a, b := m[i]/d[i], m[i+1]/d[i]
		if s := a*a + b*b; s > 9 {
			t := 3 / float32(math.Sqrt(float64(s)))
			m[i], m[i+1] = t*a*d[i], t*b*d[i]
		}
	}
	return m
}

// nodeCard 正在用的节点。「切换节点」弹出这一组里按延迟排好的节点，点一个就换
func (a *application) nodeCard(c *ui.Context) {
	p := pal(c)
	node, ok := a.selectedNode()
	card(c, "当前节点", 0, func() {
		if ok {
			delayTag(c, a.delays, nodeIdentity(node))
		}
	}, func() {
		if !ok {
			ui.Text(c, "还没有节点").FontSize(17).FontWeight(700)
			ui.Text(c, "导入以后，正在用的节点显示在这里").FontSize(12.5).TextColor(p.muted)
			return
		}
		ui.Text(c, node.Name).FontSize(17).FontWeight(700).SingleLine()
		ui.Row(c).Wrap().Gap(5).Children(func() {
			for _, part := range strings.Split(nodeSummary(node), " · ") {
				chip(c, part)
			}
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(func() {
			pick := ui.Button(c.Key("dash-pick"), "切换节点")
			if pick.Clicked() {
				a.picking = true
			}
			ui.Popover(c.Key("pick-node"), pick, &a.picking, func() { a.quickPick(c, node) })
			if iconButton(c.Key("dash-test"), boltIcon, "测这个节点的延迟", a.testing).Clicked() {
				a.testNodes([]int{a.settings.Selected}, false)
			}
		})
	})
}

// chip 节点的协议、传输、分组，一个一个的小标签
func chip(c *ui.Context, text string) {
	p := pal(c)
	ui.Text(c, text).FontSize(11).FontWeight(500).TextColor(p.muted).Padding(2, 7).Radius(5).Background(p.press).SingleLine()
}

// quickPick 仪表盘上「切换节点」弹出来的那一列：同组按延迟从快到慢，最多十个，底下一行去代理页看全部
func (a *application) quickPick(c *ui.Context, current Node) {
	p := pal(c)
	group := nodeGroupName(current)
	keys := delayKeys(a.settings.Nodes)
	indices := byDelay(visibleNodes(a.settings.Nodes, group, ""), keys, a.delays)
	ui.Column(c).Width(300).Padding(6).Gap(1).Children(func() {
		ui.Text(c, "「"+group+"」里的节点 · 按延迟").FontSize(11.5).FontWeight(600).TextColor(p.muted).Padding(6, 8, 4, 8)
		for n, index := range indices {
			if n == 10 {
				break
			}
			node := a.settings.Nodes[index]
			chosen := index == a.settings.Selected
			item := ui.ButtonBase(c.Key(fmt.Sprintf("pick-%d", index))).Height(34).PaddingX(8).Gap(8).Radius(7).AlignItems(ui.Center).Label(node.Name).FocusRing(false)
			if item.Hovered() {
				item.Background(p.hover)
			}
			item.Children(func() {
				ui.Box(c).Width(16).Center().Children(func() {
					if chosen {
						ui.Icon(c, checkIcon).Size(14, 14).TextColor(p.accent)
					}
				})
				color := p.text
				if chosen {
					color = p.accent
				}
				ui.Text(c, node.Name).FontSize(13).FontWeight(500).TextColor(color).SingleLine().Grow(1).MinWidth(0)
				delayTag(c, a.delays, keys[index])
			})
			if item.Clicked() {
				a.picking = false
				if !chosen {
					a.choose(index)
				}
			}
		}
		ui.Box(c).FillWidth().Height(1).Margin(4, 0).Background(p.hairline)
		all := ui.ButtonBase(c.Key("pick-all")).Height(32).PaddingX(8).Radius(7).AlignItems(ui.Center).Justify(ui.Start).Label("在代理页查看全部").FocusRing(false)
		if all.Hovered() {
			all.Background(p.hover)
		}
		all.Children(func() {
			ui.Text(c, fmt.Sprintf("在代理页查看全部 %d 个", len(indices))).FontSize(12.5).TextColor(p.accent)
		})
		if all.Clicked() {
			a.picking = false
			a.tab, a.editing, a.importing, a.share = "代理", false, false, nil
			a.nodeGroup, a.nodeSearch, a.revealSelected = group, "", true
		}
	})
}

// exitCard 浏览器现在从哪出去。没接管系统代理时浏览器是直连的，就照实说，不拿经本地端口查到的节点出口充数
func (a *application) exitCard(c *ui.Context, ready, proxy bool) {
	p := pal(c)
	d := a.dash
	card(c, "出口 IP", 0, func() {
		if d.exit.IP != "" && proxy && !d.checking && d.exit.Err == "" {
			if iconButton(c.Key("exit-copy"), copyIcon, "复制 IP", false).Size(26, 26).Clicked() {
				writeClipboard(d.exit.IP)
				a.showNotice("已复制 " + d.exit.IP)
			}
		}
		if iconButton(c.Key("exit-refresh"), refreshIcon, "重新检测", d.checking || !ready || !proxy).Size(26, 26).Clicked() {
			a.dash.exitKey = ""
		}
	}, func() {
		big, small, smallColor := d.exit.IP, d.exit.Place, p.muted
		via := d.exit.Via
		switch {
		case !ready:
			big, small, via = "—", "内核没有就绪", ""
		case !proxy:
			big, small, via = "未接管系统代理", "浏览器现在是直连的，点上面的「连接」后才走节点", ""
		case d.checking:
			big, small = "检测中…", ""
		case d.exit.Err != "":
			big, small, smallColor = "—", d.exit.Err, p.bad
		}
		ui.Text(c, big).FontSize(17).FontWeight(700).FontFeatures("tnum").SingleLine().Selectable()
		if small != "" {
			ui.Text(c, small).FontSize(12.5).TextColor(smallColor).LineHeight(1.4)
		}
		if via != "" {
			ui.Text(c, via).FontSize(12.5).TextColor(p.muted).SingleLine()
		}
	})
}
