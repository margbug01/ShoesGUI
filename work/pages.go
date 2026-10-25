package main

import (
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

//go:embed resources/logo.png
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

func iconButton(c *ui.Context, icon *ui.SVG, label string, primary, disabled bool) ui.Element {
	theme := c.Theme()
	background, foreground := theme.Surface, theme.Text
	if primary {
		background, foreground = theme.Accent, theme.AccentText
	}
	button := ui.ButtonBase(c).Size(36, 36).Center().Radius(9).Background(background).Border(1, theme.Border).Label(label).Tooltip(label).Disabled(disabled).Cursor(ui.CursorPointer)
	if button.Hovered() && !disabled {
		if primary {
			button.Background(theme.AccentHover)
		} else {
			button.Background(theme.SurfaceHover)
		}
	}
	if disabled {
		foreground = theme.TextMuted
		button.Background(theme.SurfacePressed)
	}
	return button.Children(func() { ui.Icon(c, icon).Size(18, 18).TextColor(foreground) })
}

func group(c *ui.Context, title string, body func()) {
	t := c.Theme()
	if title != "" {
		ui.Text(c, title).FontSize(12).TextColor(t.TextMuted).Padding(18, 4, 8, 4)
	}
	ui.Column(c).FillWidth().Padding(4, 16).Radius(12).Background(paneColor(t)).Border(1, paneBorder(t)).Shadow(0, 1, 2, 0, ui.RGBA(10, 20, 35, 0.03)).Children(body)
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
			ui.Text(c, title).FontSize(14)
			if description != "" {
				ui.Text(c, description).FontSize(12).TextColor(c.Theme().TextMuted)
			}
		})
		control()
	})
}

func (a *application) titleBar(c *ui.Context) {
	ui.Row(c).FillWidth().Height(36).Shrink(0).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Grow(1).Height(36).DragWindow()
		for _, b := range []struct {
			label string
			run   func()
		}{{"–", a.window.Minimize}, {"✕", a.window.Close}} {
			button := ui.ButtonBase(c.Key(b.label)).Size(46, 36).Center().Cursor(ui.CursorPointer)
			if button.Hovered() {
				button.Background(c.Theme().SurfaceHover)
			}
			if button.Children(func() { ui.Text(c, b.label).FontSize(13) }).Clicked() {
				b.run()
			}
		}
	})
}

func (a *application) view(c *ui.Context) {
	registerFonts()
	// 快捷键。输入框有焦点时按键先给输入框，所以 Ctrl+V 在输入框里照常是粘贴
	c.OnShortcut(ui.Ctrl, ui.KeyF, func() { a.tab, a.editing, a.importing, a.focusSearch = "代理", false, false, true })
	c.OnShortcut(ui.Ctrl, ui.KeyN, func() { a.tab = "代理"; a.beginEdit(-1) })
	if a.tab == "代理" && !a.editing && !a.importing {
		c.OnShortcut(ui.Ctrl, ui.KeyV, a.openImport)
	}
	theme := *c.Theme()
	theme.Font = "MiSans, system-ui"
	mamiColors(&theme)
	c.SetTheme(&theme)
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
	ui.Row(c).Fill().AlignItems(ui.Stretch).Draw(func(p *ui.Painter, r ui.Rect) { windowTexture(theme.Dark).draw(p, r, 0) }).Children(func() {
		c.SetTheme(sidebarTheme(&theme))
		ui.Column(c).Width(184).FillHeight().Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + r.W - 1, Y: r.Y, W: 1, H: r.H}, paneBorder(&theme), 0)
		}).Children(func() {
			ui.Row(c).Padding(22, 20, 20, 20).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Image(c, logo).Size(28, 28)
				ui.Text(c, "Shoes").FontSize(15).FontWeight(600)
			}).DragWindow()
			ui.Sidebar(c, &a.tab, func() {
				for i, label := range []string{"仪表盘", "代理", "设置", "日志"} {
					item := ui.SidebarItem(c, label, navIcons[i], label).Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
					if a.tab == label {
						item.Background(c.Theme().SurfacePressed).Shadow(0, 1, 3, 0, ui.RGBA(10, 20, 35, 0.07)).FontWeight(600)
					}
				}
			}).Grow(1)
			ui.Text(c, "Shoes · 0.1").FontSize(11).TextColor(theme.TextMuted).Padding(18, 20)
		})
		c.SetTheme(&theme)
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			a.titleBar(c)
			ui.Scroll(c).FillWidth().Grow(1).Children(func() {
				ui.Column(c.Key(a.tab)).FillWidth().Padding(0, 28, 20, 28).Gap(8).Children(func() {
					title := a.tab
					if a.tab == "代理" && a.importing {
						title = "粘贴导入"
					} else if a.tab == "代理" && a.editing {
						title = "编辑节点"
						if a.editIndex < 0 {
							title = "添加节点"
						}
					}
					ui.Row(c).FillWidth().AlignItems(ui.Center).Padding(0, 0, 8, 0).Children(func() {
						ui.Text(c, title).FontSize(21).FontWeight(600).Grow(1)
						if a.tab == "仪表盘" {
							a.dashboardHeader(c, status, ready, proxy, busy)
						}
						if a.tab == "日志" {
							p, v, changed := a.logPick.bind(a.logFilter)
							ui.Segmented(c.Key("log-filter"), p, "全部", "只看重要").Label("日志筛选")
							if changed {
								a.logFilter = v
							}
						}
						if a.tab == "代理" && !a.editing && !a.importing {
							visible := visibleNodes(a.settings.Nodes, a.nodeGroup, a.nodeSearch)
							label := fmt.Sprintf("测速：%s %d 个", a.nodeGroup, len(visible))
							if a.testing {
								label = "测速中…"
							}
							// 点一下就测最常用的真连接延迟，TCPing 放右键
							test := iconButton(c.Key("test-nodes"), boltIcon, label, false, a.testing || len(visible) == 0).Tooltip(label + "（右键可选 TCPing）").ContextMenu(func(m *ui.Menu) {
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
							if iconButton(c.Key("import-nodes"), pasteIcon, "粘贴导入", false, false).Clicked() {
								a.openImport()
							}
							if iconButton(c.Key("add-node"), addIcon, "添加节点", false, false).Clicked() {
								a.beginEdit(-1)
							}
						}
					})
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
		})
	})
}

// statusBar 窗口底部一行，在滚动区外面，列表滚到哪都看得见：报错红字、普通提示灰字，都能关
func (a *application) statusBar(c *ui.Context) {
	text, color, closable := a.errorText, c.Theme().Danger, true
	if text == "" && !a.storageOK {
		text, closable = "无法读取本地设置，改动不会保存", false
	}
	if text == "" {
		text, color = a.notice, c.Theme().TextMuted
	}
	if text == "" {
		return
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Padding(6, 22, 12, 28).Children(func() {
		ui.Text(c, text).FontSize(12).TextColor(color).Grow(1)
		if !closable {
			return
		}
		button := ui.ButtonBase(c.Key("dismiss-status")).Size(24, 24).Center().Radius(6).Label("关闭提示").Tooltip("关闭提示").Cursor(ui.CursorPointer)
		if button.Hovered() {
			button.Background(c.Theme().SurfaceHover)
		}
		if button.Children(func() { ui.Text(c, "✕").FontSize(11).TextColor(c.Theme().TextMuted) }).Clicked() {
			a.errorText, a.notice = "", ""
		}
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

func (a *application) settingsView(c *ui.Context) {
	group(c, "常规", func() {
		settingRow(c, "主题", "", func() {
			index := 0
			for i, t := range themes {
				if t.key == a.settings.Theme {
					index = i
				}
			}
			p, chosen, changed := a.themePick.bind(index)
			ui.Segmented(c.Key("theme"), p, themes[0].label, themes[1].label, themes[2].label).Label("主题")
			if changed {
				next := a.settings
				next.Theme = themes[chosen].key
				if a.save(next) {
					applyTheme(next.Theme)
				}
			}
		})
		settingRow(c, "开机时启动 Shoes", "登录 Windows 后待在托盘；接不接管代理看下一项", func() {
			p, on, changed := a.loginPick.bind(a.openAtLogin)
			ui.Switch(c.Key("open-at-login"), p)
			if changed {
				if err := mygo.App.SetOpenAtLogin(on); err != nil {
					a.errorText = "无法设置开机启动"
				} else {
					a.openAtLogin = on
				}
			}
		})
		settingRow(c, "打开时自动接管系统代理", "包括开机启动的时候，打开就能直接用", func() {
			p, on, changed := a.autoPick.bind(a.settings.AutoConnect)
			ui.Switch(c.Key("auto-connect"), p)
			if changed {
				next := a.settings
				next.AutoConnect = on
				a.save(next)
			}
		})
	})
	group(c, "连接", func() {
		settingRow(c, "本地端口", "HTTP / SOCKS5 共用，回车或点别处就保存，内核会重启", func() {
			input := ui.TextInput(c.Key("port"), &a.portDraft).Width(130).Label("本地端口")
			if leftOrSubmitted(input, &a.portFocus) {
				a.savePort()
			}
		})
		if a.portError != "" {
			ui.Text(c, a.portError).FontSize(12).TextColor(c.Theme().Danger).Padding(0, 0, 10, 0)
		}
		settingRow(c, "切换节点时断开现有连接", "断开后网页马上走新节点；关掉的话下载、通话不会被打断，新连接才走新节点", func() {
			p, on, changed := a.closePick.bind(!a.settings.KeepConnections)
			ui.Switch(c.Key("close-connections"), p)
			if changed {
				next := a.settings
				next.KeepConnections = !on
				a.save(next)
			}
		})
	})
	group(c, "高级", func() {
		ui.Column(c).FillWidth().Padding(12, 0).Gap(10).Children(func() {
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, "Shoes 内核路径").FontSize(14)
				ui.Text(c, "一般会自动找到，和 ShoesGUI.exe 放在一起就行").FontSize(12).TextColor(c.Theme().TextMuted)
			})
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				input := ui.TextInput(c.Key("kernel-path"), &a.kernelDraft).Grow(1).Placeholder("shoes.exe").Label("Shoes 内核路径")
				if leftOrSubmitted(input, &a.kernelFocus) {
					a.saveKernel()
				}
				if ui.Button(c.Key("browse"), "选择").Clicked() {
					paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.window, Title: "选择 shoes.exe", Filters: []mygo.FileFilter{{Name: "可执行文件", Extensions: []string{"exe"}}}})
					if err != nil {
						a.errorText = "无法打开文件"
					} else if len(paths) > 0 {
						a.kernelDraft = paths[0]
						a.saveKernel()
					}
				}
			})
			if a.kernelError != "" {
				ui.Text(c, a.kernelError).FontSize(12).TextColor(c.Theme().Danger)
			}
		})
	})
	ui.Text(c, "关闭窗口会缩到托盘，从托盘右键菜单退出。").FontSize(12).TextColor(c.Theme().TextMuted).Padding(16, 4)
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
