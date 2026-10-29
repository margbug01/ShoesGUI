package main

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"runtime"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
)

var (
	searchIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="6.5"/><path d="m16 16 4.5 4.5"/></svg>`))
	checkIcon  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12.5 4.5 4.5L19 7.5"/></svg>`))
	moreIcon   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="currentColor"><circle cx="5.5" cy="12" r="1.7"/><circle cx="12" cy="12" r="1.7"/><circle cx="18.5" cy="12" r="1.7"/></svg>`))
	backIcon   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5 8 12l7 7"/></svg>`))
	linkIcon   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M10 14a4.5 4.5 0 0 0 6.4 0l3-3a4.5 4.5 0 0 0-6.4-6.4l-1 1"/><path d="M14 10a4.5 4.5 0 0 0-6.4 0l-3 3a4.5 4.5 0 0 0 6.4 6.4l1-1"/></svg>`))
	closeIcon  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="m7 7 10 10M17 7 7 17"/></svg>`))
)

// shortcutHint 快捷键在这个系统上怎么写
func shortcutHint(key string) string {
	if runtime.GOOS == "darwin" {
		return "⌘" + key
	}
	return "Ctrl+" + key
}

// nodeListShown 代理页现在显示的是节点列表（不是编辑、导入、分享）
func (a *application) nodeListShown() bool {
	return a.tab == "代理" && !a.editing && !a.importing && a.share == nil
}

// nodesPage 代理页：整页是一个虚拟列表（照 MySpotify 的歌曲列表），第 0 行是标题、搜索和分组，后面一行一个节点。
// 只建看得见的那些行，几百个节点滚起来也不卡
func (a *application) nodesPage(c *ui.Context, top float32) {
	counts := groupCounts(a.settings)
	groups := append([]string{"全部"}, orderedGroups(a.settings)...)
	_, groupExists := counts[a.nodeGroup]
	if a.nodeGroup == "" || (a.nodeGroup != "全部" && !groupExists) {
		a.nodeGroup = "全部"
	}
	// 列表的行要等排好版、知道哪些看得见才建，那时弹窗里可能已经删了节点。行读的是这里存的这一份，
	// 下标和节点对得上，不会越界
	nodes, selected := a.settings.Nodes, a.settings.Selected
	keys := delayKeys(nodes)
	indices := visibleNodes(nodes, a.nodeGroup, a.nodeSearch)
	if a.sortByDelay {
		// 测速时结果一个个回来，顺序先别动，测完再排，免得行在手底下跳
		if key := fmt.Sprint(indices); !a.testing || key != a.sortKey {
			a.sortKey, a.sorted = key, byDelay(indices, keys, a.delays)
		}
		indices = a.sorted
	}
	showGroup := a.nodeGroup == "全部" && len(groups) > 2
	if a.revealSelected {
		a.revealSelected = false
		if at := slices.Index(indices, selected); at >= 0 {
			a.nodeList.ScrollIntoView(at + 1)
		}
	}
	// 第 0 行滚出去就不建了，它里面的 startMark 也就不报；那时内容一定在顶条底下
	a.underTop = true
	ui.List(c.Key("nodes"), &a.nodeList, len(indices)+1, func(i int) {
		if i == 0 {
			ui.Column(c).Padding(0, 10, 6, 10).Gap(10).Children(func() {
				a.startMark(c, top)
				a.pageHeader(c, "", false, false, false)
				a.nodeToolbar(c, indices, keys)
				a.groupChips(c, groups, counts)
				a.subscriptionLine(c)
				switch {
				case len(nodes) == 0:
					emptyNodes(c, a)
				case len(indices) == 0:
					ui.Text(c, "没有匹配的节点").FontSize(13).TextColor(pal(c).muted).Padding(24, 0).AlignSelf(ui.Center)
				default:
					nodeColumnsHeader(c, showGroup)
				}
			})
			return
		}
		index := indices[i-1]
		a.nodeRow(c, nodes[index], index, i, index == selected, keys[index], groups, showGroup)
	}).Grow(1).Padding(top+4, pagePad-10, 32, pagePad-10).ScrollbarInsets(top, 0, 0, 0)
	a.nodeDialogs(c, counts)
}

func emptyNodes(c *ui.Context, a *application) {
	p := pal(c)
	ui.Column(c).FillWidth().Padding(40, 0).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "还没有节点").FontSize(16).FontWeight(700)
		ui.Text(c, "有订阅链接就直接粘贴导入，也可以手动添加").FontSize(13).TextColor(p.muted)
		ui.Box(c).Height(6)
		a.addButtons(c, "grid")
	})
}

// 列宽：序号、名称、分组、延迟、更多。表头和每一行都用这一份，才对得齐
const (
	numberWidth = 24
	delayWidth  = 80
	moreWidth   = 28
)

func nodeColumnsHeader(c *ui.Context, showGroup bool) {
	p := pal(c)
	ui.Row(c).FillWidth().Height(30).Gap(12).AlignItems(ui.Center).Margin(4, 0, 0, 0).BorderWidth(0, 0, 1, 0).BorderColor(p.hairline).Children(func() {
		header := func(text string) ui.Element {
			return ui.Text(c, text).FontSize(12).FontWeight(500).TextColor(p.faint).SingleLine()
		}
		header("#").Width(numberWidth).TextAlign(ui.Center)
		header("名称").Grow(3).Basis(0).MinWidth(0)
		if showGroup {
			header("分组").Grow(1).Basis(0).MinWidth(0)
		}
		header("延迟").Width(delayWidth).TextAlign(ui.End)
		ui.Box(c).Width(moreWidth)
	})
}

// nodeToolbar 搜索、排序、用最快的
func (a *application) nodeToolbar(c *ui.Context, indices []int, keys []string) {
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		a.searchField(c)
		sortIndex := 0
		if a.sortByDelay {
			sortIndex = 1
		}
		p, picked, changed := a.sortPick.bind(sortIndex)
		segmented(c.Key("sort"), p, "默认顺序", "按延迟")
		if changed {
			a.sortByDelay = picked == 1
		}
		best, ok := fastest(indices, keys, a.delays)
		if ui.Button(c.Key("use-fastest"), "用最快的").Disabled(!ok || a.testing).Tooltip("切到这一组里测出来最快的节点").Clicked() && best != a.settings.Selected {
			a.choose(best)
			a.showNotice(fmt.Sprintf("已切到最快的「%s」（%d ms）", a.settings.Nodes[best].Name, a.delays[keys[best]]))
		}
	})
}

// searchField 节点搜索框，⌘F / Ctrl+F 会把焦点放进来
func (a *application) searchField(c *ui.Context) {
	input := searchBox(c, "search-nodes", "搜索节点", "搜索节点（"+shortcutHint("F")+"）", &a.nodeSearch, &a.searchFocused)
	if a.focusSearch {
		input.Focus()
		a.focusSearch = false
	}
}

// groupChips MySpotify 首页「全部 / 音乐 / 播客」那样的胶囊，选中的反色。右键自建的分组能改名、删除；
// 最后的「⋯」是当前分组的操作：导出、订阅、改名、删除
func (a *application) groupChips(c *ui.Context, groups []string, counts map[string]int) {
	ui.Row(c).FillWidth().Wrap().Gap(6).AlignItems(ui.Center).Children(func() {
		for _, group := range groups {
			count := counts[group]
			if group == "全部" {
				count = len(a.settings.Nodes)
			}
			chip := filterChip(c.Key("group-"+group), group, count, a.nodeGroup == group)
			if group != "全部" && group != "未分组" {
				chip.ContextMenu(func(m *ui.Menu) { a.groupMenu(m, group, false) })
			}
			if chip.Clicked() {
				a.nodeGroup = group
				a.deleteConfirm = false
			}
		}
		if iconButton(c.Key("add-group"), addIcon, "新建分组", false).Size(30, 30).Clicked() {
			a.addingGroup, a.renaming, a.groupDelete = true, "", ""
			a.groupDraft, a.groupError = "", ""
		}
		iconButton(c.Key("group-more"), moreIcon, "分组操作", false).Size(30, 30).Menu(func(m *ui.Menu) { a.groupMenu(m, a.nodeGroup, true) })
	})
}

// groupMenu 分组的菜单。export 为 true 时是「⋯」里那份，多一项导出整个分组
func (a *application) groupMenu(m *ui.Menu, group string, export bool) {
	items := 0
	if export {
		items++
		if m.Item("导出当前分组").Disabled(len(a.settings.Nodes) == 0).Chosen() {
			a.shareCurrentGroup()
		}
	}
	if key, ok := groupKey(group); ok {
		if source, bound := a.settings.Subscriptions[key]; bound {
			items += 2
			if m.Item("更新订阅").Disabled(a.fetching).Chosen() {
				a.updateSubscription(key)
			}
			if m.Item("分享订阅").Chosen() {
				a.openShare(nil, source, group+" · 分享订阅")
			}
		}
	}
	if group == "全部" || group == "未分组" {
		return
	}
	if items > 0 {
		m.Separator()
	}
	if m.Item("重命名…").Chosen() {
		a.renaming, a.groupDraft, a.groupError = group, group, ""
		a.addingGroup, a.groupDelete = false, ""
	}
	if m.Item("删除分组…").Chosen() {
		a.groupDelete, a.renaming, a.addingGroup = group, "", false
	}
}

// subscriptionLine 当前分组绑了订阅时，分组下面一行：订阅的主机，更新和分享
func (a *application) subscriptionLine(c *ui.Context) {
	group, ok := groupKey(a.nodeGroup)
	if !ok {
		return
	}
	source, bound := a.settings.Subscriptions[group]
	if !bound {
		return
	}
	p := pal(c)
	host := source
	if u, err := url.Parse(source); err == nil {
		host = u.Host
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Padding(8, 8, 8, 12).Radius(10).Background(p.card).Border(1, p.hairline).Children(func() {
		ui.Icon(c, linkIcon).Size(14, 14).TextColor(p.muted)
		ui.Text(c, "订阅 · "+host).FontSize(12.5).TextColor(p.muted).SingleLine().Grow(1).MinWidth(0)
		if ui.Button(c.Key("share-sub"), "分享订阅").Clicked() {
			a.openShare(nil, source, a.nodeGroup+" · 分享订阅")
		}
		label := "更新订阅"
		if a.fetching {
			label = "更新中…"
		}
		if ui.Button(c.Key("update-sub"), label).Disabled(a.fetching).Clicked() {
			a.updateSubscription(group)
		}
	})
}

// nodeRow 一个节点一行，像 MySpotify 的歌：序号、名字和协议、分组、延迟；正在用的那行打勾、字是强调色。
// 点一下就切过去，右键或「⋯」是更多操作
func (a *application) nodeRow(c *ui.Context, node Node, index, number int, chosen bool, key string, groups []string, showGroup bool) {
	p := pal(c)
	menu := func(m *ui.Menu) {
		if m.Item("分享节点").Chosen() {
			a.openShare([]Node{node}, "", node.Name)
		}
		if m.Item("编辑").Chosen() {
			a.beginEdit(index)
		}
		m.Submenu("测速", func(m *ui.Menu) {
			if m.Item("真连接延迟").Disabled(a.testing).Chosen() {
				a.testNodes([]int{index}, false)
			}
			if m.Item("TCPing").Disabled(a.testing).Chosen() {
				a.testNodes([]int{index}, true)
			}
		})
		var targets []string
		for _, target := range groups[1:] {
			if target != nodeGroupName(node) {
				targets = append(targets, target)
			}
		}
		if len(targets) > 0 {
			m.Submenu("移至分组", func(m *ui.Menu) {
				for _, target := range targets {
					if m.Item(target).Chosen() {
						name, _ := groupKey(target)
						a.moveNode(index, name)
					}
				}
			})
		}
		m.Separator()
		if m.Item("删除").Chosen() {
			a.editIndex = index
			a.deleteConfirm = true
		}
	}
	row := ui.ButtonBase(c.Key(fmt.Sprintf("node-%d", index))).Height(50).PaddingX(10).Gap(12).Radius(10).AlignItems(ui.Center).FocusRing(false).
		Label(node.Name).Tooltip(net.JoinHostPort(node.Host, node.Port)).ContextMenu(menu)
	hovered := row.Hovered()
	bg := ui.Transparent
	switch {
	case chosen:
		bg = p.accent.Alpha(0.09)
	case hovered:
		bg = p.hover
	}
	row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 100 * time.Millisecond}))
	row.Children(func() {
		ui.Box(c).Width(numberWidth).Center().Children(func() {
			if chosen {
				ui.Icon(c, checkIcon).Size(15, 15).TextColor(p.accent)
			} else {
				ui.Text(c, fmt.Sprint(number)).FontSize(12.5).FontFeatures("tnum").TextColor(p.faint)
			}
		})
		ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(2).Children(func() {
			name := ui.Text(c, node.Name).FontSize(13.5).FontWeight(500).SingleLine()
			if chosen {
				name.TextColor(p.accent).FontWeight(600)
			}
			ui.Text(c, nodeProtocol(node)).FontSize(12).TextColor(p.muted).SingleLine()
		})
		if showGroup {
			ui.Text(c, nodeGroupName(node)).FontSize(12.5).TextColor(p.muted).SingleLine().Grow(1).Basis(0).MinWidth(0)
		}
		ui.Row(c).Width(delayWidth).Justify(ui.End).Children(func() { delayTag(c, a.delays, key) })
		ui.Box(c).Size(moreWidth, moreWidth).Center().Children(func() {
			// 每行都建着，只是不悬停时藏起来。MyGo 在选中后的下一帧才把选的项交给菜单，
			// 那时鼠标已经在菜单上、离开了这一行；按钮要是没建，选的项就没人接，点了等于没点。
			// 菜单开着时按钮有焦点，照样显示，看得出是哪一行的菜单
			more := iconButton(c.Key("more"), moreIcon, "更多操作", false).Size(moreWidth, moreWidth).Menu(menu)
			if !hovered && !more.Focused() {
				more.Invisible()
			}
		})
	})
	if row.Clicked() && !chosen {
		a.choose(index)
	}
}

// delayTag 延迟：绿、橙、红的字，前面一个同色的点；超时写「超时」，没测过不显示。
// 测速中是一小段一明一暗的灰条，八九帧一秒就够，软件渲染下别为它把 CPU 吃满
func delayTag(c *ui.Context, delays map[string]int, key string) {
	ms, ok := delays[key]
	if !ok {
		return
	}
	p := pal(c)
	if ms == 0 {
		phase := float64(time.Now().UnixMilli()%1400) / 1400
		ui.Box(c).Size(38, 8).Radius(4).Background(p.press).Opacity(float32(0.6 + 0.4*math.Cos(phase*2*math.Pi)))
		c.After(120 * time.Millisecond)
		return
	}
	text, color := "超时", p.bad
	if ms > 0 {
		text, color = fmt.Sprintf("%d ms", ms), delayColor(p, ms)
	}
	ui.Row(c).Gap(5).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(color)
		ui.Text(c, text).FontSize(12).FontWeight(500).TextColor(color).FontFeatures("tnum").SingleLine()
	})
}

func delayColor(p palette, ms int) ui.Color {
	switch {
	case ms <= 500: // 和 v2rayN 一样，500 ms 以内算好
		return p.good
	case ms <= 1000:
		return p.warn
	}
	return p.bad
}

// dialog 照 MySpotify 的确认框：压暗的底，中间一块圆角面板，标题、说明，右下角一排按钮。
// 点外面或按 Esc 关掉时返回 dismissed，调用的人自己把状态清掉
func dialog(c *ui.Context, key string, open bool, width float32, title, message string, body, buttons func()) (dismissed bool) {
	p := pal(c)
	shown := open
	panel := ui.DialogBase(c.Key(key), &shown, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.32))
		panel.Width(width).Padding(22, 22, 18, 22).Radius(16).Gap(8).Background(p.bg).Border(1, p.hairline).
			Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.35)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		ui.Text(c, title).FontSize(16).FontWeight(700)
		if message != "" {
			ui.Text(c, message).FontSize(13).TextColor(p.muted).LineHeight(1.45)
		}
		if body != nil {
			body()
		}
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Margin(10, 0, 0, 0).Children(buttons)
	})
	return panel.Dismissed()
}

// nodeDialogs 删除节点、新建或改名分组、删除分组，三个弹窗
func (a *application) nodeDialogs(c *ui.Context, counts map[string]int) {
	p := pal(c)
	if a.deleteConfirm && (a.editIndex < 0 || a.editIndex >= len(a.settings.Nodes)) {
		a.deleteConfirm = false
	}
	name := ""
	if a.deleteConfirm {
		name = a.settings.Nodes[a.editIndex].Name
	}
	if dialog(c, "delete-node", a.deleteConfirm, 360, "删除这个节点？", "「"+name+"」会从列表里删掉，删了要重新导入才能找回来。", nil, func() {
		if pillButton(c.Key("cancel"), nil, "取消", neutralFill(p), p.text).Clicked() {
			a.deleteConfirm = false
		}
		if pillButton(c.Key("confirm-delete"), nil, "删除", p.bad, ui.RGB(255, 255, 255)).Clicked() {
			a.deleteNode(a.editIndex)
		}
	}) {
		a.deleteConfirm = false
	}

	naming := a.addingGroup || a.renaming != ""
	title, action := "新建分组", "创建分组"
	if a.renaming != "" {
		title, action = "重命名分组「"+a.renaming+"」", "重命名"
	}
	submit := func() {
		if a.renaming != "" {
			a.renameGroup(a.renaming)
		} else {
			a.addGroup()
		}
	}
	closeNaming := func() { a.addingGroup, a.renaming, a.groupDraft, a.groupError = false, "", "", "" }
	if dialog(c, "group-name", naming, 360, title, "", func() {
		input := ui.TextInput(c.Key("group-name"), &a.groupDraft).FillWidth().Placeholder("分组名称").AutoFocus().Label("分组名称").Margin(6, 0, 0, 0)
		if input.Submitted() {
			submit()
		}
		if a.groupError != "" {
			ui.Text(c, a.groupError).FontSize(12).TextColor(p.bad)
		}
	}, func() {
		if pillButton(c.Key("cancel"), nil, "取消", neutralFill(p), p.text).Clicked() {
			closeNaming()
		}
		if pillButton(c.Key("create-group"), nil, action, p.accent, p.onAccent).Clicked() {
			submit()
		}
	}) {
		closeNaming()
	}

	group := a.groupDelete
	n := counts[group]
	message := "这是个空分组。"
	if n > 0 {
		message = fmt.Sprintf("里面有 %d 个节点。可以把它们挪到「未分组」，也可以连节点一起删掉。", n)
	}
	if _, bound := a.settings.Subscriptions[group]; bound {
		message += "绑定的订阅也会一起去掉。"
	}
	if dialog(c, "group-delete", group != "", 420, "删除分组「"+group+"」？", message, nil, func() {
		if pillButton(c.Key("cancel"), nil, "取消", neutralFill(p), p.text).Clicked() {
			a.groupDelete = ""
		}
		if n > 0 {
			if pillButton(c.Key("group-delete-keep"), nil, "节点挪到未分组", neutralFill(p), p.text).Clicked() {
				a.deleteGroup(group, false)
			}
			if pillButton(c.Key("group-delete-all"), nil, "连节点一起删", p.bad, ui.RGB(255, 255, 255)).Clicked() {
				a.deleteGroup(group, true)
			}
		} else if pillButton(c.Key("group-delete-empty"), nil, "删除", p.bad, ui.RGB(255, 255, 255)).Clicked() {
			a.deleteGroup(group, false)
		}
	}) {
		a.groupDelete = ""
	}
}
