package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

const speedSamples = 60

type rate struct{ up, down int64 }

// dashboard 是仪表盘的数据，只在界面线程读写，后台协程经 window.Update 交过来
type dashboard struct {
	speeds           []rate // 最近 60 秒，每秒一个，旧的在前
	up, down         int64  // Shoes 打开以来经过内核的流量，内核重启也接着加
	lastUp, lastDown int64  // 内核上次报的累计值
	conns            int
	memory           int64
	exit             exitInfo
	exitKey          string // 上次查出口 IP 时的「内核代次/切换次数」，变了就重查
	checking         bool
	route            int // 切节点、切模式成功一次加一
}

type exitInfo struct{ IP, Place, Via, Err string }

func (d *dashboard) addTraffic(up, down, upTotal, downTotal int64) {
	d.push(rate{up, down})
	d.up += counted(&d.lastUp, upTotal)
	d.down += counted(&d.lastDown, downTotal)
}

func (d *dashboard) push(r rate) {
	d.speeds = append(d.speeds, r)
	if len(d.speeds) > speedSamples {
		d.speeds = d.speeds[len(d.speeds)-speedSamples:]
	}
}

// counted 返回累计值比上次多出来的部分；内核重启后从 0 重新数，就整个算新的
func counted(last *int64, total int64) int64 {
	delta := total - *last
	if delta < 0 {
		delta = total
	}
	*last = total
	return delta
}

// watchTraffic 跟着内核 /traffic 的每秒推送记网速和流量；内核没起来时补 0，曲线照常往前走
func (a *application) watchTraffic() {
	for {
		if a.engine.isReady() {
			a.engine.stream("/traffic", func(line []byte) {
				var t struct{ Up, Down, UpTotal, DownTotal int64 }
				if json.Unmarshal(line, &t) == nil {
					a.window.Update(func() { a.dash.addTraffic(t.Up, t.Down, t.UpTotal, t.DownTotal) })
				}
			})
		} else {
			a.window.Update(func() { a.dash.push(rate{}) })
		}
		time.Sleep(time.Second)
	}
}

// watchConnections 仪表盘开着时每 2 秒拉一次连接数和内核内存
func (a *application) watchConnections() {
	for range time.Tick(2 * time.Second) {
		if !a.dashOn.Load() {
			continue
		}
		var snap struct {
			Connections []json.RawMessage
			Memory      int64
		}
		if a.engine.call(http.MethodGet, "/connections", nil, &snap) != nil {
			continue
		}
		count, memory := len(snap.Connections), snap.Memory
		a.window.Update(func() { a.dash.conns, a.dash.memory = count, memory })
	}
}

// 查出口 IP 的站点都在境外，规则模式下也走节点。前一个不通再试下一个
var exitSources = []struct {
	url   string
	parse func(data []byte) (ip, place string, err error)
}{
	{"https://ipwho.is/?lang=zh-CN&fields=ip,success,message,country,region,city", func(data []byte) (string, string, error) {
		var r struct {
			Success                            bool
			IP, Message, Country, Region, City string
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return "", "", err
		}
		if !r.Success || r.IP == "" {
			return "", "", errors.New("ipwho.is：" + r.Message)
		}
		return r.IP, placeName(r.Country, r.Region, r.City), nil
	}},
	{"http://ip-api.com/json/?lang=zh-CN&fields=status,message,query,country,regionName,city", func(data []byte) (string, string, error) {
		var r struct{ Status, Message, Query, Country, RegionName, City string }
		if err := json.Unmarshal(data, &r); err != nil {
			return "", "", err
		}
		if r.Status != "success" || r.Query == "" {
			return "", "", errors.New("ip-api.com：" + r.Message)
		}
		return r.Query, placeName(r.Country, r.RegionName, r.City), nil
	}},
}

// lookupExit 经本地代理端口查出口 IP，走的是和浏览器一样的分流
func lookupExit(port string) (ip, place string, err error) {
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "127.0.0.1:" + port})}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 6 * time.Second}
	for _, source := range exitSources {
		var data []byte
		if data, err = fetch(client, source.url); err == nil {
			if ip, place, err = source.parse(data); err == nil {
				return ip, place, nil
			}
		}
	}
	return "", "", err
}

func fetch(client *http.Client, target string) ([]byte, error) {
	response, err := client.Get(target)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New(response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 64<<10))
}

// placeName 把国家、地区、城市拼起来，跳过重复的（「中国香港 香港 香港」只留「中国香港」）
func placeName(parts ...string) string {
	out := ""
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || strings.Contains(out, part) {
			continue
		}
		if out != "" {
			out += " "
		}
		out += part
	}
	return out
}

func (a *application) checkExit(key, port string) {
	a.dash.checking, a.dash.exitKey = true, key
	via := "直连模式，没经过节点"
	if node, ok := a.selectedNode(); ok && a.settings.Mode != "direct" {
		via = "经「" + node.Name + "」"
	}
	go func() {
		ip, place, err := lookupExit(port)
		a.window.Update(func() {
			a.dash.checking = false
			a.dash.exit = exitInfo{IP: ip, Place: place, Via: via}
			if err != nil {
				a.dash.exit.Err = "查不到，当前节点可能不通"
				a.engine.event("", "出口 IP 查询失败："+err.Error())
			}
		})
	}()
}

func formatBytes(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	case n < 10<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n < 1<<30:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
}

func clock(d time.Duration) string {
	s := int(d / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}

// runState 仪表盘右上角那句话；bad 为 true 用红字
func runState(status string, ready, proxy, hasNodes bool, since, now time.Time) (text string, bad bool) {
	switch {
	case proxy && !since.IsZero():
		return "已运行 " + clock(now.Sub(since)), false
	case proxy:
		return "运行中", false
	case !hasNodes:
		return "还没有节点", false
	case ready:
		return "未启动", false
	case status == "启动失败" || status == "内核已退出":
		return status + "，点这里看日志", true
	}
	return status, false
}

func modeIndex(key string) int {
	for i, m := range modes {
		if m.key == key {
			return i
		}
	}
	return 0
}

// nodeSummary 「VLESS · Reality · XHTTP · 快车」
func nodeSummary(n Node) string {
	parts := []string{n.Kind}
	switch {
	case n.Reality:
		parts = append(parts, "Reality")
	case n.TLS && n.Kind != "Trojan" && n.Kind != "AnyTLS":
		parts = append(parts, "TLS")
	}
	switch n.Network {
	case "ws":
		parts = append(parts, "WebSocket")
	case "xhttp":
		parts = append(parts, "XHTTP")
	}
	return strings.Join(append(parts, nodeGroupName(n)), " · ")
}

var refreshIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 12a8 8 0 1 1-2.34-5.66M20 4v5h-5"/></svg>`))

// card 仪表盘上的一块。span 为 -1 时占满一行；aside 放在标题右边
func card(c *ui.Context, title string, span int, aside, body func()) {
	t := c.Theme()
	column := ui.Column(c).MinWidth(0).Padding(14, 16).Gap(8).Radius(12).Background(paneColor(t)).Border(1, paneBorder(t)).Shadow(0, 1, 2, 0, ui.RGBA(10, 20, 35, 0.03))
	if span != 0 {
		column.ColumnSpan(span)
	}
	column.Children(func() {
		ui.Row(c).FillWidth().MinHeight(18).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, title).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1)
			if aside != nil {
				aside()
			}
		})
		body()
	})
}

// dashboardView 对齐 FlClash 的仪表盘：网速和流量、当前节点、出站模式、出口 IP（含系统代理接管状态）。
// 启动/停止只有右上角一个按钮
func (a *application) dashboardView(c *ui.Context, ready, proxy, busy bool) {
	gen, _, port := a.engine.runInfo()
	if key := fmt.Sprintf("%d/%d", gen, a.dash.route); ready && proxy && !a.dash.checking && key != a.dash.exitKey {
		a.checkExit(key, port)
	}
	grid := ui.Grid(c.Key("dashboard")).FillWidth().Gap(12)
	columns := 2
	if w := grid.Bounds().W; w > 0 && w < 460 {
		columns = 1
	}
	grid.Columns(columns).Children(func() {
		a.speedCard(c, ready)
		a.nodeCard(c)
		a.modeCard(c)
		a.exitCard(c, ready, proxy)
	})
}

func (a *application) speedCard(c *ui.Context, ready bool) {
	t := c.Theme()
	now := rate{}
	if n := len(a.dash.speeds); n > 0 {
		now = a.dash.speeds[n-1]
	}
	speeds := a.dash.speeds
	// 数字每秒都变，按最宽的值留固定宽度、等宽数字、靠右，整块宽度不变才不会跳
	valueW, _ := c.MeasureText(1000, ui.Span{Text: "0000 MB/s", Size: 12})
	card(c, "网速", -1, func() {
		for _, item := range []struct {
			label string
			value int64
		}{{"上传", now.up}, {"下载", now.down}} {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Text(c, item.label).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(0)
				ui.Text(c, formatBytes(item.value)+"/s").FontSize(12).SingleLine().FontFeatures("tnum").TextAlign(ui.End).Width(valueW + 4).Shrink(0)
			})
		}
	}, func() {
		ui.Box(c).FillWidth().Height(64).Draw(func(p *ui.Painter, r ui.Rect) { drawSpeeds(p, r, speeds, t) })
		conns, memory := "—", "—"
		if ready {
			conns = strconv.Itoa(a.dash.conns)
			if a.dash.memory > 0 {
				memory = formatBytes(a.dash.memory)
			}
		}
		// 本次 = 从打开 Shoes 算起，经过内核的都算，直连的也算
		ui.Row(c).FillWidth().Padding(6, 0, 0, 0).Children(func() {
			for _, stat := range [][2]string{{"本次上传", formatBytes(a.dash.up)}, {"本次下载", formatBytes(a.dash.down)}, {"活动连接", conns}, {"内核内存", memory}} {
				ui.Column(c).Grow(1).Gap(2).MinWidth(0).Children(func() {
					ui.Text(c, stat[0]).FontSize(11).TextColor(t.TextMuted).SingleLine()
					ui.Text(c, stat[1]).FontSize(14).FontWeight(600).SingleLine().FontFeatures("tnum")
				})
			}
		})
	})
}

// drawSpeeds 下载是实线带浅色填充，上传是细的淡线；最新的一秒在最右边
func drawSpeeds(p *ui.Painter, r ui.Rect, speeds []rate, t *ui.Theme) {
	p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, t.Border, 0)
	if len(speeds) < 2 {
		return
	}
	top := int64(16 << 10) // 闲着时别把几百字节的噪声放大成满格
	for _, s := range speeds {
		top = max(top, s.up, s.down)
	}
	step := r.W / float32(speedSamples-1)
	point := func(i int, v int64) (float32, float32) {
		return r.X + r.W - float32(len(speeds)-1-i)*step, r.Y + r.H - 1 - float32(v)/float32(top)*(r.H-4)
	}
	var down, area, up ui.Path
	for i, s := range speeds {
		x, y := point(i, s.down)
		ux, uy := point(i, s.up)
		if i == 0 {
			down.MoveTo(x, y)
			area.MoveTo(x, r.Y+r.H-1).LineTo(x, y)
			up.MoveTo(ux, uy)
			continue
		}
		down.LineTo(x, y)
		area.LineTo(x, y)
		up.LineTo(ux, uy)
	}
	area.LineTo(r.X+r.W, r.Y+r.H-1).Close()
	p.FillPath(&area, t.Accent.Alpha(0.08))
	p.StrokePath(&up, 1, t.TextMuted.Alpha(0.7))
	p.StrokePath(&down, 1.5, t.Accent)
}

func (a *application) nodeCard(c *ui.Context) {
	t := c.Theme()
	node, ok := a.selectedNode()
	card(c, "当前节点", 0, nil, func() {
		if !ok {
			ui.Text(c, "还没有节点").FontSize(15).FontWeight(600)
			ui.Text(c, "有订阅链接就直接粘贴导入").FontSize(12).TextColor(t.TextMuted)
			a.addButtons(c, "dash")
			return
		}
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, node.Name).FontSize(15).FontWeight(600).SingleLine().Grow(1)
			delayPill(c, a.delays, nodeIdentity(node))
		})
		ui.Text(c, nodeSummary(node)).FontSize(12).TextColor(t.TextMuted).SingleLine()
		ui.Row(c).Gap(8).Padding(4, 0, 0, 0).Children(func() {
			if ui.Button(c.Key("dash-pick"), "切换节点").Clicked() {
				a.tab, a.editing, a.importing = "代理", false, false
				a.nodeGroup, a.nodeSearch, a.revealSelected = nodeGroupName(node), "", true
			}
			if ui.Button(c.Key("dash-test"), "测速").Disabled(a.testing).Clicked() {
				a.testNodes([]int{a.settings.Selected}, false)
			}
		})
	})
}

// addButtons 没有节点时的两个入口。大多数人手里是订阅，所以粘贴导入是主按钮
func (a *application) addButtons(c *ui.Context, key string) {
	ui.Row(c).Gap(8).Padding(4, 0, 0, 0).Children(func() {
		if ui.PrimaryButton(c.Key(key+"-import"), "粘贴导入").Clicked() {
			a.tab = "代理"
			a.openImport()
		}
		if ui.Button(c.Key(key+"-add"), "手动添加").Clicked() {
			a.tab = "代理"
			a.beginEdit(-1)
		}
	})
}

func delayPill(c *ui.Context, delays map[string]int, key string) {
	ms, ok := delays[key]
	if !ok {
		return
	}
	text, color := delayLabel(c, ms)
	width, _ := c.MeasureText(1000, ui.Span{Text: text, Size: 11})
	ui.Box(c).MinWidth(width+18).Padding(2, 8).Radius(10).Background(color.Alpha(0.14)).Shrink(0).Center().Children(func() {
		ui.Text(c, text).FontSize(11).TextColor(color).SingleLine()
	})
}

func (a *application) modeCard(c *ui.Context) {
	card(c, "出站模式", 0, nil, func() {
		p, chosen, changed := a.modePick.bind(modeIndex(a.settings.Mode))
		ui.Row(c).Children(func() {
			segmented(c.Key("mode"), p, modes[0].label, modes[1].label, modes[2].label)
		})
		if changed {
			a.setMode(modes[chosen].key)
		}
		ui.Text(c, modes[modeIndex(a.settings.Mode)].hint).FontSize(12).TextColor(c.Theme().TextMuted)
	})
}

// exitCard 浏览器现在从哪出去。没接管系统代理时浏览器是直连的，就照实说，不拿经本地端口查到的节点出口充数
func (a *application) exitCard(c *ui.Context, ready, proxy bool) {
	t := c.Theme()
	d := a.dash
	card(c, "出口 IP", -1, func() {
		button := ui.ButtonBase(c.Key("exit-refresh")).Size(22, 22).Center().Radius(6).Label("重新检测").Tooltip("重新检测").Disabled(d.checking || !ready || !proxy).Cursor(ui.CursorPointer)
		if button.Hovered() {
			button.Background(t.SurfaceHover)
		}
		if button.Children(func() { ui.Icon(c, refreshIcon).Size(14, 14).TextColor(t.TextMuted) }).Clicked() {
			a.dash.exitKey = ""
		}
	}, func() {
		big, small, smallColor := d.exit.IP, d.exit.Place, t.TextMuted
		via := d.exit.Via
		switch {
		case !ready:
			big, small, via = "—", "内核没有就绪", ""
		case !proxy:
			big, small, via = "未接管系统代理", "浏览器现在是直连的，点右上角「启动」后才走节点", ""
		case d.checking:
			big, small = "检测中…", ""
		case d.exit.Err != "":
			big, small, smallColor = "—", d.exit.Err, t.Danger
		}
		ui.Text(c, big).FontSize(15).FontWeight(600).SingleLine()
		if small != "" {
			ui.Text(c, small).FontSize(12).TextColor(smallColor).SingleLine()
		}
		if via != "" {
			ui.Text(c, via).FontSize(12).TextColor(t.TextMuted).SingleLine()
		}
		if proxy {
			ui.Text(c, "已接管系统代理 127.0.0.1:"+a.settings.Port+"，局域网和本机不走代理").FontSize(11).TextColor(t.TextMuted).SingleLine()
		}
	})
}

// dashboardHeader 标题右边：运行时长和启动/停止
func (a *application) dashboardHeader(c *ui.Context, status string, ready, proxy, busy bool) {
	_, since, _ := a.engine.runInfo()
	hasNodes := len(a.settings.Nodes) > 0
	text, bad := runState(status, ready, proxy, hasNodes, since, time.Now())
	if bad {
		// 报错让人去看日志，那就直接能点过去
		link := ui.ButtonBase(c.Key("open-logs")).Padding(0, 12).Label(text).Tooltip("打开日志").Cursor(ui.CursorPointer)
		link.Children(func() { ui.Text(c, text).FontSize(13).TextColor(c.Theme().Danger).Underline().SingleLine() })
		if link.Clicked() {
			a.tab = "日志"
		}
	} else {
		ui.Text(c, text).FontSize(13).TextColor(c.Theme().TextMuted).SingleLine().FontFeatures("tnum").Padding(0, 12)
	}
	label := "启动"
	if busy {
		label = "处理中…"
	} else if proxy {
		label = "停止"
	}
	if ui.PrimaryButton(c.Key("connect"), label).MinWidth(88).Disabled(busy || !hasNodes).Clicked() {
		a.toggleConnect()
	}
}
