package main

import (
	_ "embed"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

//go:embed assets/logo.png
var logoPNG []byte

// logo 侧栏左上角的应用图标，96px 的图显示成 28，高分屏也清楚
var logo = func() *ui.Bitmap {
	b, err := ui.DecodeBitmap(logoPNG)
	if err != nil {
		panic(err)
	}
	return b
}()

var navIcons = []*ui.SVG{
	ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 16a7.5 7.5 0 1 1 15 0M12 16l3.5-4"/><circle cx="12" cy="16" r="1"/></svg>`)),
	ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.01M7 17.5h.01"/></svg>`)),
	ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M4 7h16M4 17h16"/><circle cx="9" cy="7" r="3"/><circle cx="15" cy="17" r="3"/></svg>`)),
	ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3h10l3 3v15H3V3h2M8 8h8M8 12h8M8 16h5"/></svg>`)),
}

var boltIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M13 3 5 13.5h6L10 21l8-10.5h-6z"/></svg>`))

var addIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>`))

// group / settingRow 编辑节点、粘贴导入还在用的分组，代理页换成新样式时再一起改掉
func group(c *ui.Context, title string, body func()) {
	t := c.Theme()
	if title != "" {
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(t.TextMuted).Padding(16, 4, 6, 4)
	}
	ui.Column(c).FillWidth().Padding(4, 16).Radius(12).Background(paneColor(t)).Border(1, paneBorder(t)).Children(body)
}

// choice 给分段按钮、开关、下拉框、输入框绑一个跨帧的变量。MyGo 在一帧建完之后才把点击和输入写回
// 绑定的变量，绑每帧新建的局部变量的话，这次操作就丢了。v 绑给控件，shown 是上一帧摆出来的值，两者不同就是用户刚改过
type choice[T comparable] struct{ v, shown T }

// bind 每帧调用一次，current 是现在该显示的值。返回绑给控件的指针，以及用户刚选的值（changed 为 true 时）
func (ch *choice[T]) bind(current T) (p *T, picked T, changed bool) {
	if ch.v != ch.shown {
		ch.shown = ch.v
		return &ch.v, ch.v, true
	}
	ch.v, ch.shown = current, current
	return &ch.v, current, false
}

func settingRow(c *ui.Context, title, description string, control func()) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Padding(12, 0).Gap(16).Children(func() {
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Text(c, title).FontSize(13.5).FontWeight(500)
			if description != "" {
				ui.Text(c, description).FontSize(12).TextColor(c.Theme().TextMuted)
			}
		})
		control()
	})
}

const (
	sidebarWidth = 212
	pagePad      = 32
	// winBarHeight Windows / Linux 上那条标题栏的高度：隐藏了系统标题栏，系统的最小化、关闭按钮占满它
	winBarHeight = 40
)

var tabs = []string{"仪表盘", "代理", "设置", "日志"}

// sidebarVibrancy 侧栏底下的毛玻璃，只有 macOS 用。Windows 的毛玻璃深色主题下还是浅的，侧栏的字会看不清，就自己铺色
func sidebarVibrancy() mygo.Vibrancy {
	if runtime.GOOS == "darwin" {
		return mygo.VibrancySidebar
	}
	return mygo.VibrancyNone
}

// topBar 窗口顶上那一条的高度，内容从它下面开始：macOS 是红绿灯那一条，别的系统是窗口按钮那一条
func topBar(c *ui.Context) float32 {
	if runtime.GOOS != "darwin" {
		return winBarHeight
	}
	return max(c.TitleBar().Height, 40)
}

func (a *application) view(c *ui.Context) {
	registerFonts()
	// 快捷键。输入框有焦点时按键先给输入框，所以 Ctrl+V 在输入框里照常是粘贴
	c.OnShortcut(ui.Cmd, ui.KeyF, func() { a.share = nil; a.tab, a.editing, a.importing, a.focusSearch = "代理", false, false, true })
	c.OnShortcut(ui.Cmd, ui.KeyN, func() { a.tab = "代理"; a.beginEdit(-1) })
	if a.tab == "代理" && !a.editing && !a.importing && a.share == nil {
		c.OnShortcut(ui.Cmd, ui.KeyV, a.openImport)
	}
	theme := *c.Theme()
	theme.Font = "MiSans, system-ui"
	p := paletteFor(theme.Dark)
	applyPalette(&theme, p)
	c.SetTheme(&theme)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent) // 侧栏底下透出毛玻璃，内容区自己铺底
	}
	status, logs, ready, proxy := a.engine.snapshot()
	busy := a.busy.Load()
	a.dashOn.Store(a.tab == "仪表盘")
	if a.tab != a.shownTab {
		if a.shownTab == "设置" { // 输入框里改了还没回车就换页，也照样存下
			a.savePort()
			a.saveKernel()
		}
		a.shownTab, a.errorText = a.tab, "" // 报错只跟着出错的那一页
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c).Width(sidebarWidth).Shrink(0).FillHeight()
		if !c.Vibrancy() {
			side.Background(p.sidebar)
		}
		side.Children(func() { a.sidebar(c, status, ready, proxy, busy) })
		ui.Column(c.Key("main")).Grow(1).MinWidth(0).FillHeight().Background(p.bg).BorderWidth(0, 0, 0, 1).BorderColor(p.hairline).Children(func() {
			top := topBar(c)
			ui.Scroll(c.Key("page-"+a.tab)).FillWidth().Grow(1).Padding(top+4, pagePad, 32, pagePad).ScrollbarInsets(top, 0, 0, 0).Children(func() {
				a.startMark(c, top)
				ui.Column(c.Key(a.tab)).FillWidth().Gap(8).Children(func() {
					a.pageHeader(c, status, ready, proxy, busy)
					switch a.tab {
					case "仪表盘":
						a.dashboardView(c, ready, proxy, busy)
					case "代理":
						a.nodesView(c)
					case "设置":
						a.settingsView(c)
					case "日志":
						a.logView(c, logs)
					}
				})
			})
			a.statusBar(c)
			// 顶上那一条压在页面上面：拖得动窗口，内容滚到它底下时出一道磨砂边
			a.pageTop(c, top)
		})
	})
	// 成功一类的提示用 Toast，几秒后自己消失；报错留在页面底下，见 statusBar
	if a.noticeSeq != a.toasted {
		a.toasted = a.noticeSeq
		if a.notice != "" {
			c.AddToast(ui.Toast{ID: "notice", Title: a.notice})
		}
	}
}

// pageHeader 页面的大标题，右边放这一页的操作
func (a *application) pageHeader(c *ui.Context, status string, ready, proxy, busy bool) {
	title := a.tab
	if a.tab == "代理" && a.share != nil {
		title = "分享与导出"
	} else if a.tab == "代理" && a.importing {
		title = "粘贴导入"
	} else if a.tab == "代理" && a.editing {
		title = "编辑节点"
		if a.editIndex < 0 {
			title = "添加节点"
		}
	}
	ui.Row(c).FillWidth().MinHeight(36).AlignItems(ui.Center).Gap(6).Padding(0, 0, 10, 0).Children(func() {
		heading(c, title).Grow(1).MinWidth(0)
		switch {
		case a.tab == "仪表盘":
			a.dashboardHeader(c, status, ready, proxy, busy)
		case a.tab == "日志":
			p, v, changed := a.logPick.bind(a.logFilter)
			segmented(c.Key("log-filter"), p, "全部", "只看重要")
			if changed {
				a.logFilter = v
			}
		case a.tab == "代理" && !a.editing && !a.importing && a.share == nil:
			visible := visibleNodes(a.settings.Nodes, a.nodeGroup, a.nodeSearch)
			label := fmt.Sprintf("测速：%s %d 个", a.nodeGroup, len(visible))
			if a.testing {
				label = "测速中…"
			}
			// 点一下就测最常用的真连接延迟，TCPing 放右键
			test := iconButton(c.Key("test-nodes"), boltIcon, label, a.testing || len(visible) == 0).Tooltip(label + "（右键可选 TCPing）").ContextMenu(func(m *ui.Menu) {
				if m.Item(fmt.Sprintf("真连接延迟（%d 个）", len(visible))).Disabled(a.testing).Chosen() {
					a.testNodes(visible, false)
				}
				if m.Item(fmt.Sprintf("TCPing（%d 个）", len(visible))).Disabled(a.testing).Chosen() {
					a.testNodes(visible, true)
				}
			})
			if test.Clicked() {
				a.testNodes(visible, false)
			}
			if iconButton(c.Key("import-nodes"), pasteIcon, "粘贴导入", false).Clicked() {
				a.openImport()
			}
			if iconButton(c.Key("add-node"), addIcon, "添加节点", false).Clicked() {
				a.beginEdit(-1)
			}
		}
	})
}

// sidebar 左边一栏：页面导航，底下是新版本提示和连接状态。没被按钮接住的地方拖得动窗口，和它顶到的标题栏一样
func (a *application) sidebar(c *ui.Context, status string, ready, proxy, busy bool) {
	ui.Column(c).Fill().Padding(0, 12, 12, 12).Gap(2).DragWindow().Children(func() {
		if c.TitleBar().Left > 0 {
			ui.Box(c).Height(topBar(c) + 4).Shrink(0) // 让开红绿灯
		} else {
			// 没有红绿灯的地方（Windows）左上角空着，放应用的名字
			ui.Row(c).Padding(14, 8, 14, 8).Gap(9).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Image(c, logo).Size(22, 22)
				ui.Text(c, "Shoes").FontSize(14).FontWeight(600)
			})
		}
		for i, label := range tabs {
			a.navItem(c, navIcons[i], label)
		}
		ui.Spacer(c)
		a.updateBadge(c)
		a.statusCard(c, status, ready, proxy, busy)
	})
}

func (a *application) navItem(c *ui.Context, icon *ui.SVG, label string) {
	p := pal(c)
	selected := a.tab == label
	b := ui.ButtonBase(c.Key("nav-" + label)).FillWidth().Height(32).PaddingX(10).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Label(label).FocusRing(false)
	bg := ui.Transparent
	switch {
	case selected:
		bg = p.press
	case b.Hovered():
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, hoverFade))
	color := p.text
	if selected {
		color = p.accent
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(17, 17).TextColor(color)
		ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.text)
	})
	if b.Clicked() {
		a.tab = label
	}
}

// statusCard 侧栏底部的连接状态，哪一页都看得到；点了去仪表盘
func (a *application) statusCard(c *ui.Context, status string, ready, proxy, busy bool) {
	p := pal(c)
	node, hasNode := a.selectedNode()
	title, sub, dot := status, "", p.faint
	if hasNode {
		sub = node.Name
	}
	switch {
	case busy:
		title = "处理中…"
	case proxy:
		title, dot = "已连接", p.good
		if hasNode {
			sub = node.Name + " · " + modes[modeIndex(a.settings.Mode)].label
		}
	case !hasNode:
		title, sub = "还没有节点", "点这里导入"
	case status == "启动失败" || status == "内核已退出":
		dot = p.bad
	case ready:
		title = "未连接"
	}
	card := ui.ButtonBase(c.Key("status-card")).FillWidth().Gap(10).Padding(9, 12).Radius(12).AlignItems(ui.Center).Label("连接状态：" + title).FocusRing(false).Cursor(ui.CursorPointer)
	bg := p.hover
	if card.Hovered() {
		bg = p.press
	}
	card.Background(card.AnimateColor("bg", bg, hoverFade))
	card.Children(func() {
		light := ui.Box(c).Size(8, 8).Radius(4).Background(dot).Shrink(0)
		if proxy {
			light.Shadow(0, 0, 0, 3, dot.Alpha(0.22))
		}
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(12.5).FontWeight(600).SingleLine()
			if sub != "" {
				ui.Text(c, sub).FontSize(11.5).TextColor(p.muted).SingleLine()
			}
		})
	})
	if card.Clicked() {
		a.tab = "仪表盘"
	}
}

// startMark 放在页面内容的最前面：它往上走过顶上那一条，内容就在那一条底下了
func (a *application) startMark(c *ui.Context, top float32) {
	b := ui.Box(c.Key("start-mark")).Height(0).Bounds()
	if b.W > 0 {
		a.underTop = b.Y < top-0.5
	}
}

// pageTop 页面顶上那一条：拖得动窗口；内容滚到底下时像 macOS 26 的硬滚动边那样磨砂，平时看不见
func (a *application) pageTop(c *ui.Context, top float32) {
	edge := ui.Box(c.Key("top-edge")).Absolute().Top(0).Left(0).Right(0).Height(top).DragWindow().Material(glass.ScrollEdge{Hard: true, Background: pal(c).bg})
	target := float32(0)
	if a.underTop {
		target = 1
	}
	edge.Opacity(edge.AnimateTo("shown", target, ui.Animation{Duration: 180 * time.Millisecond}))
}

// statusBar 窗口底部一行，在滚动区外面，列表滚到哪都看得见。只放报错，成功一类的提示走 Toast
func (a *application) statusBar(c *ui.Context) {
	text, closable := a.errorText, true
	if text == "" && !a.storageOK {
		text, closable = "无法读取本地设置，改动不会保存", false
	}
	if text == "" {
		return
	}
	p := pal(c)
	ui.Row(c).FillWidth().Padding(0, pagePad, 14, pagePad).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Padding(8, 8, 8, 12).Radius(9).Background(p.bad.Alpha(0.1)).Children(func() {
			ui.Text(c, text).FontSize(12.5).TextColor(p.bad).Grow(1)
			if !closable {
				return
			}
			button := ui.ButtonBase(c.Key("dismiss-status")).Size(22, 22).Center().Radius(6).Label("关闭提示").Tooltip("关闭提示").Cursor(ui.CursorPointer)
			if button.Hovered() {
				button.Background(p.bad.Alpha(0.12))
			}
			if button.Children(func() { ui.Text(c, "✕").FontSize(11).TextColor(p.bad) }).Clicked() {
				a.errorText = ""
			}
		})
	})
}

var themes = []struct{ key, label string }{{"", "跟随系统"}, {"light", "浅色"}, {"dark", "深色"}}

// applyTheme 测试里换掉：离屏测试没有主循环，SetSource 会一直等主线程
var applyTheme = func(key string) { mygo.Theme.SetSource(themeSource(key)) }

func themeSource(key string) mygo.ThemeSource {
	switch key {
	case "light":
		return mygo.ThemeLight
	case "dark":
		return mygo.ThemeDark
	}
	return mygo.ThemeSystem
}

// closeHint 关窗口不等于退出，在设置里说一声怎么退出
func closeHint() string {
	if runtime.GOOS == "darwin" {
		return "关掉窗口后 Shoes 还在菜单栏里运行，代理照常工作。要退出按 ⌘Q，或点菜单栏图标里的「退出」。"
	}
	return "关掉窗口后 Shoes 还在托盘里运行，代理照常工作。要退出，从托盘图标的右键菜单里点「退出」。"
}

func (a *application) settingsView(c *ui.Context) {
	section(c, "通用", func() {
		row(c, "外观", "", func() {
			index := 0
			for i, t := range themes {
				if t.key == a.settings.Theme {
					index = i
				}
			}
			p, chosen, changed := a.themePick.bind(index)
			segmented(c.Key("theme"), p, themes[0].label, themes[1].label, themes[2].label)
			if changed {
				next := a.settings
				next.Theme = themes[chosen].key
				if a.save(next) {
					applyTheme(next.Theme)
				}
			}
		})
		row(c, "开机时启动 Shoes", "登录系统后在后台待命；接不接管代理看下一项", func() {
			p, on, changed := a.loginPick.bind(a.openAtLogin)
			ui.Switch(c.Key("open-at-login"), p).Label("开机时启动 Shoes")
			if changed {
				if err := mygo.App.SetOpenAtLogin(on); err != nil {
					a.errorText = "无法设置开机启动"
				} else {
					a.openAtLogin = on
				}
			}
		})
		row(c, "打开时自动接管系统代理", "包括开机启动的时候，打开就能直接用", func() {
			p, on, changed := a.autoPick.bind(a.settings.AutoConnect)
			ui.Switch(c.Key("auto-connect"), p).Label("打开时自动接管系统代理")
			if changed {
				next := a.settings
				next.AutoConnect = on
				a.save(next)
			}
		})
	})
	sectionNote(c, closeHint())
	section(c, "连接", func() {
		rowWith(c, "本地端口", "HTTP / SOCKS5 共用，回车或点别处就保存，内核会重启", a.portError, nil, func() {
			input := ui.TextInput(c.Key("port"), &a.portDraft).Width(110).Label("本地端口")
			if leftOrSubmitted(input, &a.portFocus) {
				a.savePort()
			}
		})
		row(c, "切换节点时断开现有连接", "断开后网页马上走新节点；关掉的话下载、通话不会被打断，新连接才走新节点", func() {
			p, on, changed := a.closePick.bind(!a.settings.KeepConnections)
			ui.Switch(c.Key("close-connections"), p).Label("切换节点时断开现有连接")
			if changed {
				next := a.settings
				next.KeepConnections = !on
				a.save(next)
			}
		})
	})
	section(c, "高级", func() {
		path := a.kernelDraft
		if path == "" {
			path = "还没找到 " + kernelName
		}
		rowWith(c, "Shoes 内核", path, a.kernelError, nil, func() {
			if ui.Button(c.Key("browse"), "更改…").Tooltip(kernelHint).Clicked() {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.window, Title: "选择 " + kernelName, Filters: []mygo.FileFilter{{Name: "可执行文件", Extensions: kernelExtensions}}})
				if err != nil {
					a.errorText = "无法打开文件"
				} else if len(paths) > 0 {
					a.kernelDraft = paths[0]
					a.saveKernel()
				}
			}
		})
	})
	section(c, "关于", func() {
		a.updateRows(c)
		row(c, "GitHub", "源代码、更新说明和各平台的下载", func() {
			ui.Link(c, "打开", repoPage)
		})
	})
}

// leftOrSubmitted 回车或焦点离开输入框时为 true。was 是上一帧有没有焦点，要跨帧存着
func leftOrSubmitted(input ui.Element, was *bool) bool {
	submitted := input.Submitted()
	now := input.Focused()
	left := *was && !now
	*was = now
	return submitted || left
}

// savePort 端口改了就保存并重启内核；不合法就留着草稿、在输入框下面说为什么
func (a *application) savePort() {
	if a.portDraft == a.settings.Port {
		a.portError = ""
		return
	}
	if _, err := portNumber(a.portDraft); err != nil {
		a.portError = err.Error()
		return
	}
	next := a.settings
	next.Port = a.portDraft
	if a.commit(next) {
		a.portError = ""
		a.showNotice("本地端口已改成 " + next.Port + "，内核正在重启")
	}
}

func (a *application) saveKernel() {
	if a.kernelDraft == a.settings.Kernel {
		a.kernelError = ""
		return
	}
	if err := validKernel(a.kernelDraft); err != nil {
		a.kernelError = err.Error()
		return
	}
	next := a.settings
	next.Kernel = a.kernelDraft
	if a.commit(next) {
		a.kernelError = ""
		a.showNotice("内核路径已保存")
	}
}

// logView 最新的在上面。「只看重要」藏起连接异常，顶上说一句藏了多少。能搜、能选中复制、能一键复制和清空，报错标红
func (a *application) logView(c *ui.Context, logs []logEntry) {
	t := c.Theme()
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
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Padding(0, 0, 6, 0).Children(func() {
		ui.TextInput(c.Key("log-search"), &a.logSearch).Grow(1).Placeholder("搜索日志").Label("搜索日志")
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
		ui.Text(c, fmt.Sprintf("已藏起 %d 条连接异常：单条连接没正常收尾，不影响别的连接", hidden)).FontSize(12).TextColor(t.TextMuted).Padding(0, 0, 4, 0)
	}
	if len(shown) == 0 {
		text := "暂无日志"
		if query != "" {
			text = "没有匹配的日志"
		}
		ui.Text(c, text).FontSize(13).TextColor(t.TextMuted)
		return
	}
	ui.Column(c).FillWidth().Gap(4).Selectable().Children(func() {
		for _, entry := range shown {
			color := t.TextMuted
			if entry.bad() {
				color = t.Danger
			}
			ui.Text(c, entry.String()).FontSize(12).TextColor(color)
		}
	})
}
