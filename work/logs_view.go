package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
)

// 日志页的两块：每个域名怎么走的，和 Shoes、内核自己的运行日志
const (
	paneFlows = iota
	paneLogs
)

var trashIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12M9 7V4h6v3"/></svg>`))

func (a *application) logsPage(c *ui.Context, top float32, logs []logEntry, ready, proxy bool) {
	if a.logPane == paneLogs {
		a.logLines(c, top, logs)
		return
	}
	a.flowsPage(c, top, ready, proxy)
}

// logsHeader 日志页第 0 行开头：大标题、「连接记录 / 运行日志」，然后是这一块的内容
func (a *application) logsHeader(c *ui.Context, top float32, body func()) {
	ui.Column(c).Padding(0, 10, 6, 10).Gap(10).Children(func() {
		a.startMark(c, top)
		a.pageHeader(c, "", false, false, false)
		body()
	})
}

// flowsPage 连接记录：像 v2rayN 的连接页，看每个域名是直连还是经哪个节点出去的、命中哪条规则。
// 一个域名一行，同一个域名换了走法另起一行；最近有连接的在上面
func (a *application) flowsPage(c *ui.Context, top float32, ready, proxy bool) {
	p := pal(c)
	rows, _ := a.flows.rows()
	names := map[string]string{} // 内核里的节点名 → 用户起的名字（重名时内核会加「 2」）
	for i, name := range kernelNames(a.settings.Nodes) {
		names[name] = a.settings.Nodes[i].Name
	}
	query := strings.ToLower(strings.TrimSpace(a.flowSearch))
	var matched, shown []hostFlow
	proxied, direct, open := 0, 0, 0
	for _, row := range rows {
		open += row.Active
		if query != "" && !strings.Contains(strings.ToLower(row.Host), query) && !strings.Contains(strings.ToLower(names[row.Via]), query) {
			continue
		}
		matched = append(matched, row)
		if row.Direct {
			direct++
		} else {
			proxied++
		}
		if a.flowFilter == 0 || (a.flowFilter == 2) == row.Direct {
			shown = append(shown, row)
		}
	}
	w, _ := c.Size()
	wide := w-sidebarWidth-2*pagePad > 600
	a.underTop = true
	ui.List(c.Key("flows"), &a.flowList, len(shown)+1, func(i int) {
		if i > 0 {
			a.flowRow(c, shown[i-1], names, wide)
			return
		}
		a.logsHeader(c, top, func() {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				input := searchBox(c, "search-flows", "搜索域名", "搜索域名或节点（"+shortcutHint("F")+"）", &a.flowSearch, &a.flowSearchFocused)
				if a.focusLogSearch {
					input.Focus()
					a.focusLogSearch = false
				}
				if iconButton(c.Key("clear-flows"), trashIcon, "清空记录", len(rows) == 0).Clicked() {
					a.flows.clear()
				}
				iconButton(c.Key("flows-more"), moreIcon, "连接操作", false).Menu(func(m *ui.Menu) {
					if m.Item(fmt.Sprintf("断开所有连接（%d 条）", open)).Disabled(open == 0).Chosen() {
						go a.engine.closeConnections()
						a.showNotice(fmt.Sprintf("已断开 %d 条连接，网页会自己重新连", open))
					}
				})
			})
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				for i, label := range []string{"全部", "代理", "直连"} {
					count := []int{len(matched), proxied, direct}[i]
					if filterChip(c.Key("flow-filter-"+label), label, count, a.flowFilter == i).Clicked() {
						a.flowFilter = i
					}
				}
				ui.Spacer(c)
				text := "没有开着的连接"
				if open > 0 {
					text = fmt.Sprintf("%d 条连接开着", open)
				}
				ui.Text(c, text).FontSize(12).TextColor(p.muted).FontFeatures("tnum")
			})
			switch {
			case !ready:
				emptyNote(c, "内核没在运行", "连上以后，这里会列出每个域名走了直连还是代理")
				return
			case !proxy:
				ui.Text(c, "没接管系统代理：只有自己设了代理（127.0.0.1:"+a.settings.Port+"）的程序会出现在这里").FontSize(12).TextColor(p.muted).LineHeight(1.4)
			}
			switch {
			case len(rows) == 0:
				emptyNote(c, "还没有连接记录", "打开个网页试试。记录只放在内存里，退出 Shoes 就没了")
			case len(shown) == 0:
				emptyNote(c, "没有匹配的域名", "")
			default:
				flowColumns(c, wide)
			}
		})
	}).Grow(1).Padding(top+4, pagePad-10, 32, pagePad-10).ScrollbarInsets(top, 0, 0, 0)
}

func emptyNote(c *ui.Context, title, hint string) {
	p := pal(c)
	ui.Column(c).FillWidth().Padding(36, 0).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(15).FontWeight(700)
		if hint != "" {
			ui.Text(c, hint).FontSize(12.5).TextColor(p.muted)
		}
	})
}

// 连接记录的列宽，表头和每一行共用
const (
	routeWidth = 46
	countWidth = 48
	bytesWidth = 76
	clockWidth = 62
)

func flowColumns(c *ui.Context, wide bool) {
	p := pal(c)
	ui.Row(c).FillWidth().Height(30).Gap(12).AlignItems(ui.Center).Margin(4, 0, 0, 0).BorderWidth(0, 0, 1, 0).BorderColor(p.hairline).Children(func() {
		header := func(text string) ui.Element {
			return ui.Text(c, text).FontSize(12).FontWeight(500).TextColor(p.faint).SingleLine()
		}
		header("走法").Width(routeWidth)
		header("域名").Grow(3).Basis(0).MinWidth(0)
		if wide {
			header("节点").Grow(1.3).Basis(0).MinWidth(0)
		}
		header("次数").Width(countWidth).TextAlign(ui.End)
		if wide {
			header("流量").Width(bytesWidth).TextAlign(ui.End)
		}
		header("最近").Width(clockWidth).TextAlign(ui.End)
	})
}

// flowRow 一个域名一行：走法、域名和命中的规则、经过的节点、连了几次（还开着的打绿点）、流量、最近一次
func (a *application) flowRow(c *ui.Context, h hostFlow, names map[string]string, wide bool) {
	p := pal(c)
	via := names[h.Via]
	if via == "" {
		via = h.Via
	}
	menu := func(m *ui.Menu) {
		if m.Item("复制域名").Chosen() {
			writeClipboard(h.Host)
			a.showNotice("已复制 " + h.Host)
		}
		if m.Item("只看这个域名").Chosen() {
			a.flowSearch, a.flowFilter = h.Host, 0
		}
		if h.Active > 0 {
			m.Separator()
			if m.Item(fmt.Sprintf("断开这 %d 条连接", h.Active)).Chosen() {
				ids := h.ids
				go func() {
					for _, id := range ids {
						a.engine.call(http.MethodDelete, "/connections/"+url.PathEscape(id), nil, nil)
					}
				}()
				a.showNotice(fmt.Sprintf("已断开 %s 的 %d 条连接", h.Host, h.Active))
			}
		}
	}
	key := fmt.Sprintf("flow-%v-%s-%s", h.Direct, h.Via, h.Host)
	row := ui.ButtonBase(c.Key(key)).Height(46).PaddingX(10).Gap(12).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(h.Host).ContextMenu(menu)
	if row.Hovered() {
		row.Background(p.hover)
	}
	row.Children(func() {
		ui.Row(c).Width(routeWidth).Shrink(0).Children(func() { routeTag(c, h.Direct) })
		ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, h.Host).FontSize(13.5).FontWeight(500).SingleLine()
			parts := []string{}
			if h.Rule != "" {
				parts = append(parts, h.Rule)
			}
			if h.Port != "" {
				parts = append(parts, "端口 "+h.Port)
			}
			if !wide && !h.Direct && via != "" {
				parts = append(parts, via)
			}
			ui.Text(c, strings.Join(parts, " · ")).FontSize(12).TextColor(p.muted).SingleLine()
		})
		if wide {
			text := via
			if h.Direct {
				text = "—"
			}
			ui.Text(c, text).FontSize(12.5).TextColor(p.muted).SingleLine().Grow(1.3).Basis(0).MinWidth(0)
		}
		count := ui.Row(c).Width(countWidth).Shrink(0).Gap(5).Justify(ui.End).AlignItems(ui.Center)
		if h.Active > 0 {
			count.Tooltip(fmt.Sprintf("一共 %d 条，%d 条还开着", h.Total, h.Active))
		}
		count.Children(func() {
			if h.Active > 0 {
				ui.Box(c).Size(6, 6).Radius(3).Background(p.good)
			}
			ui.Text(c, fmt.Sprint(h.Total)).FontSize(12.5).FontFeatures("tnum").SingleLine()
		})
		if wide {
			ui.Column(c).Width(bytesWidth).Shrink(0).Gap(1).AlignItems(ui.End).Children(func() {
				ui.Text(c, "↓ "+formatBytes(h.Down)).FontSize(11.5).TextColor(p.muted).FontFeatures("tnum").SingleLine()
				ui.Text(c, "↑ "+formatBytes(h.Up)).FontSize(11.5).TextColor(p.faint).FontFeatures("tnum").SingleLine()
			})
		}
		ui.Text(c, h.Last.Format("15:04:05")).FontSize(12).TextColor(p.faint).FontFeatures("tnum").SingleLine().Width(clockWidth).TextAlign(ui.End)
	})
}

// routeTag 「代理」是强调色的小标签，「直连」是灰的
func routeTag(c *ui.Context, direct bool) {
	p := pal(c)
	text, fg, bg := "代理", p.accent, p.accent.Alpha(0.12)
	if direct {
		text, fg, bg = "直连", p.muted, p.press
	}
	ui.Text(c, text).FontSize(11.5).FontWeight(600).TextColor(fg).Padding(2, 7).Radius(5).Background(bg).SingleLine()
}

// logLines 运行日志：一行一条，左边一列时间，出错的打红点、字也是红的；连接异常打橙点，可以藏起来
func (a *application) logLines(c *ui.Context, top float32, logs []logEntry) {
	p := pal(c)
	query := strings.ToLower(strings.TrimSpace(a.logSearch))
	hidden := 0
	var shown []logEntry
	for i := len(logs) - 1; i >= 0; i-- {
		if a.logFilter == 1 && logs[i].conn != "" {
			hidden += logs[i].count
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(logs[i].String()), query) {
			continue
		}
		shown = append(shown, logs[i])
	}
	a.underTop = true
	ui.List(c.Key("logs"), &a.logList, len(shown)+1, func(i int) {
		if i > 0 {
			logLine(c, shown[i-1])
			return
		}
		a.logsHeader(c, top, func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				input := searchBox(c, "search-logs", "搜索日志", "搜索日志（"+shortcutHint("F")+"）", &a.logSearch, &a.logSearchFocused)
				if a.focusLogSearch {
					input.Focus()
					a.focusLogSearch = false
				}
				pick, v, changed := a.logPick.bind(a.logFilter)
				segmented(c.Key("log-filter"), pick, "全部", "只看重要")
				if changed {
					a.logFilter = v
				}
				if ui.Button(c.Key("log-copy"), "复制").Disabled(len(shown) == 0).Tooltip("复制下面显示的日志，发给别人帮忙看").Clicked() {
					lines := make([]string, len(shown))
					for i, entry := range shown {
						lines[i] = entry.String()
					}
					writeClipboard(strings.Join(lines, "\n"))
					a.showNotice(fmt.Sprintf("已复制 %d 条日志", len(shown)))
				}
				if ui.Button(c.Key("log-clear"), "清空").Disabled(len(logs) == 0).Clicked() {
					a.engine.clearLogs()
				}
			})
			if hidden > 0 {
				ui.Text(c, fmt.Sprintf("已藏起 %d 条连接异常：单条连接没正常收尾，不影响别的连接", hidden)).FontSize(12).TextColor(p.muted)
			}
			if len(shown) == 0 {
				title := "暂无日志"
				if query != "" {
					title = "没有匹配的日志"
				}
				emptyNote(c, title, "")
				return
			}
			ui.Box(c).FillWidth().Height(1).Margin(4, 0, 0, 0).Background(p.hairline)
		})
	}).Grow(1).Padding(top+4, pagePad-10, 32, pagePad-10).ScrollbarInsets(top, 0, 0, 0)
}

func logLine(c *ui.Context, entry logEntry) {
	p := pal(c)
	dot, color := ui.Transparent, p.text
	switch {
	case entry.bad():
		dot, color = p.bad, p.bad
	case entry.conn != "":
		dot, color = p.warn, p.muted
	}
	ui.Row(c).FillWidth().Padding(5, 10).Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Text(c, entry.at.Format("15:04:05")).FontSize(12).TextColor(p.faint).FontFeatures("tnum").Width(58).Shrink(0).LineHeight(1.5)
		ui.Box(c).Size(6, 6).Radius(3).Background(dot).Margin(6, 0, 0, 0).Shrink(0)
		ui.Text(c, entry.message()).FontSize(12.5).TextColor(color).LineHeight(1.5).Grow(1).MinWidth(0).Selectable()
	})
}
