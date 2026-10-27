package main

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

func nodeColumns(width float32) int {
	if width <= 0 {
		return 2
	}
	return max(1, int((width+8)/228))
}

func nodeGroupName(node Node) string {
	if node.Group == "" {
		return "未分组"
	}
	return node.Group
}

// groupKey 分组标签对应的 Node.Group：「未分组」是 ""，「全部」不是一个分组
func groupKey(label string) (string, bool) {
	switch label {
	case "全部", "":
		return "", false
	case "未分组":
		return "", true
	}
	return label, true
}

func visibleNodes(nodes []Node, group, query string) []int {
	query = strings.ToLower(strings.TrimSpace(query))
	indices := make([]int, 0, len(nodes))
	for index, node := range nodes {
		if group != "全部" && nodeGroupName(node) != group {
			continue
		}
		if !strings.Contains(strings.ToLower(node.Name+" "+node.Group), query) {
			continue
		}
		indices = append(indices, index)
	}
	return indices
}

func groupCounts(settings Settings) map[string]int {
	counts := map[string]int{}
	for _, name := range settings.Groups {
		counts[name] = 0
	}
	for _, node := range settings.Nodes {
		counts[nodeGroupName(node)]++
	}
	return counts
}

// groupNameError 新分组名不合法的原因；except 是正在改名的那个旧名字，和它一样不算重名
func (a *application) groupNameError(name, except string) string {
	if name == "" || name == "全部" || name == "未分组" {
		return "请输入分组名称，不能使用「全部」或「未分组」"
	}
	if _, exists := groupCounts(a.settings)[name]; exists && name != except {
		return "这个分组已经存在"
	}
	return ""
}

func (a *application) addGroup() {
	name := strings.TrimSpace(a.groupDraft)
	if a.groupError = a.groupNameError(name, ""); a.groupError != "" {
		return
	}
	next := a.settings
	next.Groups = append(append([]string(nil), next.Groups...), name)
	if a.save(next) {
		a.nodeGroup, a.nodeSearch = name, ""
		a.addingGroup, a.groupDraft, a.groupError = false, "", ""
	}
}

// renameGroup 把分组 old 改成输入框里的名字，节点和订阅跟着走
func (a *application) renameGroup(old string) {
	name := strings.TrimSpace(a.groupDraft)
	if a.groupError = a.groupNameError(name, old); a.groupError != "" {
		return
	}
	next := a.settings
	next.Groups = slices.Clone(next.Groups)
	if i := slices.Index(next.Groups, old); i >= 0 {
		next.Groups[i] = name
	} else {
		next.Groups = append(next.Groups, name)
	}
	next.Nodes = slices.Clone(next.Nodes)
	for i := range next.Nodes {
		if next.Nodes[i].Group == old {
			next.Nodes[i].Group = name
		}
	}
	if source, ok := next.Subscriptions[old]; ok {
		next.Subscriptions = maps.Clone(next.Subscriptions)
		delete(next.Subscriptions, old)
		next.Subscriptions[name] = source
	}
	if a.save(next) {
		a.nodeGroup = name
		a.renaming, a.groupDraft, a.groupError = "", "", ""
	}
}

// deleteGroup 删掉分组 name。withNodes 为 false 时里面的节点挪到未分组，否则一起删；正在用的节点还在就接着用
func (a *application) deleteGroup(name string, withNodes bool) {
	next := a.settings
	next.Groups = slices.DeleteFunc(slices.Clone(next.Groups), func(g string) bool { return g == name })
	if _, ok := next.Subscriptions[name]; ok {
		next.Subscriptions = maps.Clone(next.Subscriptions)
		delete(next.Subscriptions, name)
	}
	next.Nodes, next.Selected = nil, -1
	removed := 0
	for i, n := range a.settings.Nodes {
		if n.Group == name {
			if withNodes {
				removed++
				continue
			}
			n.Group, n.FromSub = "", false
		}
		if i == a.settings.Selected {
			next.Selected = len(next.Nodes)
		}
		next.Nodes = append(next.Nodes, n)
	}
	if next.Selected < 0 && len(next.Nodes) > 0 {
		next.Selected = 0
	}
	saved := false
	if removed > 0 {
		saved = a.commit(next)
	} else {
		saved = a.save(next)
	}
	if !saved {
		return
	}
	a.groupDelete, a.nodeGroup = "", "全部"
	if removed > 0 {
		a.showNotice(fmt.Sprintf("已删除分组「%s」和里面的 %d 个节点", name, removed))
	} else {
		a.showNotice("已删除分组「" + name + "」")
	}
}

func (a *application) moveNode(index int, group string) {
	next := a.settings
	next.Nodes = append([]Node(nil), next.Nodes...)
	previous := next.Nodes[index].Group
	if previous != "" && !slices.Contains(next.Groups, previous) {
		next.Groups = append(append([]string(nil), next.Groups...), previous)
	}
	// 挪走就不归原来的订阅管了，也不能被目标分组的订阅更新删掉
	next.Nodes[index].Group, next.Nodes[index].FromSub = group, false
	a.save(next)
}

// orderedGroups 分组标签的顺序：按建的先后，再是只在节点上出现过的，「未分组」固定放最后
func orderedGroups(settings Settings) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if !seen[name] && name != "未分组" {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, name := range settings.Groups {
		add(name)
	}
	ungrouped := false
	for _, n := range settings.Nodes {
		add(nodeGroupName(n))
		ungrouped = ungrouped || n.Group == ""
	}
	if ungrouped {
		out = append(out, "未分组")
	}
	return out
}

// delayKeys 测速结果按节点配置（地址、协议、凭证、传输）记，不按名字：内核名字重名时会加「 2」，
// 删掉前一个后面的就改名，结果会张冠李戴；配置改了结果也自然作废
func delayKeys(nodes []Node) []string {
	keys := make([]string, len(nodes))
	for i, n := range nodes {
		keys[i] = nodeIdentity(n)
	}
	return keys
}

// byDelay 按延迟从快到慢排，没测过的在后面，超时的最后；同样的保持原来的先后
func byDelay(indices []int, keys []string, delays map[string]int) []int {
	rank := func(i int) int {
		ms, ok := delays[keys[i]]
		switch {
		case !ok || ms == 0:
			return math.MaxInt - 1
		case ms < 0:
			return math.MaxInt
		}
		return ms
	}
	out := slices.Clone(indices)
	slices.SortStableFunc(out, func(x, y int) int { return cmp.Compare(rank(x), rank(y)) })
	return out
}

// fastest indices 里测出来最快的那个
func fastest(indices []int, keys []string, delays map[string]int) (int, bool) {
	best, bestMS := -1, 0
	for _, i := range indices {
		if ms := delays[keys[i]]; ms > 0 && (best < 0 || ms < bestMS) {
			best, bestMS = i, ms
		}
	}
	return best, best >= 0
}

func (a *application) groupTabs(c *ui.Context, groups []string, counts map[string]int) {
	ui.Row(c).FillWidth().Wrap().Gap(6).Padding(4, 0, 8, 0).Children(func() {
		for _, group := range groups {
			count := counts[group]
			if group == "全部" {
				count = len(a.settings.Nodes)
			}
			chosen := a.nodeGroup == group
			label := fmt.Sprintf("%s  %d", group, count)
			labelWidth, _ := c.MeasureText(1000, ui.Span{Text: label, Size: 12})
			tab := ui.ButtonBase(c.Key("group-"+group)).MinWidth(labelWidth+28).Padding(7, 12).Radius(7).Label(group).Cursor(ui.CursorPointer)
			if chosen {
				tab.Background(c.Theme().Accent)
			} else if tab.Hovered() {
				tab.Background(c.Theme().SurfaceHover)
			}
			if group != "全部" && group != "未分组" {
				tab.Tooltip("右键可以重命名或删除").ContextMenu(func(m *ui.Menu) {
					if m.Item("重命名").Chosen() {
						a.renaming, a.groupDraft, a.groupError = group, group, ""
						a.addingGroup, a.groupDelete = false, ""
					}
					if m.Item("删除分组").Chosen() {
						a.groupDelete, a.renaming, a.addingGroup = group, "", false
					}
				})
			}
			tab.Children(func() {
				color := c.Theme().TextMuted
				if chosen {
					color = c.Theme().AccentText
				}
				ui.Text(c, label).FontSize(12).TextColor(color).SingleLine()
			})
			if tab.Clicked() {
				a.nodeGroup = group
				a.deleteConfirm = false
			}
		}
		if ui.Button(c.Key("add-group"), "+ 新建分组").Clicked() {
			a.addingGroup, a.renaming, a.groupDelete = true, "", ""
			a.groupDraft, a.groupError = "", ""
		}
	})
	if a.addingGroup || a.renaming != "" {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			input := ui.TextInput(c.Key("group-name"), &a.groupDraft).Grow(1).Placeholder("分组名称").AutoFocus()
			if ui.Button(c.Key("cancel-group"), "取消").Clicked() {
				a.addingGroup, a.renaming, a.groupDraft, a.groupError = false, "", "", ""
			}
			action := "创建分组"
			if a.renaming != "" {
				action = "重命名"
			}
			if ui.PrimaryButton(c.Key("create-group"), action).Clicked() || input.Submitted() {
				if a.renaming != "" {
					a.renameGroup(a.renaming)
				} else {
					a.addGroup()
				}
			}
		})
		if a.groupError != "" {
			ui.Text(c, a.groupError).FontSize(12).TextColor(c.Theme().Danger)
		}
	}
	if name := a.groupDelete; name != "" {
		n := counts[name]
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Padding(8, 12).Radius(9).Background(c.Theme().Danger.Alpha(0.08)).Children(func() {
			text := "删除分组「" + name + "」？"
			if n > 0 {
				text = fmt.Sprintf("删除分组「%s」？里面有 %d 个节点", name, n)
			}
			ui.Text(c, text).FontSize(12).Grow(1).MinWidth(0)
			if ui.Button(c.Key("cancel-group-delete"), "取消").Clicked() {
				a.groupDelete = ""
			}
			if n > 0 {
				if ui.Button(c.Key("group-delete-keep"), "节点挪到未分组").Clicked() {
					a.deleteGroup(name, false)
				}
				if ui.Button(c.Key("group-delete-all"), "连节点一起删").TextColor(c.Theme().Danger).Clicked() {
					a.deleteGroup(name, true)
				}
			} else if ui.Button(c.Key("group-delete-empty"), "删除").TextColor(c.Theme().Danger).Clicked() {
				a.deleteGroup(name, false)
			}
		})
	}
}

func (a *application) nodeGrid(c *ui.Context) {
	counts := groupCounts(a.settings)
	groups := append([]string{"全部"}, orderedGroups(a.settings)...)
	_, groupExists := counts[a.nodeGroup]
	if a.nodeGroup == "" || (a.nodeGroup != "全部" && !groupExists) {
		a.nodeGroup = "全部"
	}
	keys := delayKeys(a.settings.Nodes)
	indices := visibleNodes(a.settings.Nodes, a.nodeGroup, a.nodeSearch)
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		search := ui.TextInput(c.Key("search-nodes"), &a.nodeSearch).Grow(1).Placeholder("搜索节点（Ctrl+F）").Label("搜索节点")
		if a.focusSearch {
			search.Focus()
			a.focusSearch = false
		}
		sortIndex := 0
		if a.sortByDelay {
			sortIndex = 1
		}
		p, picked, changed := a.sortPick.bind(sortIndex)
		ui.Segmented(c.Key("sort"), p, "默认顺序", "按延迟").Label("排序")
		if changed {
			a.sortByDelay = picked == 1
		}
		best, ok := fastest(indices, keys, a.delays)
		if ui.Button(c.Key("use-fastest"), "用最快的").Disabled(!ok || a.testing).Tooltip("切到这一组里测出来最快的节点").Clicked() && best != a.settings.Selected {
			a.choose(best)
			a.showNotice(fmt.Sprintf("已切到最快的「%s」（%d ms）", a.settings.Nodes[best].Name, a.delays[keys[best]]))
		}
	})
	a.groupTabs(c, groups, counts)
	if ui.Button(c.Key("export-group"), "导出当前分组").Disabled(len(a.settings.Nodes) == 0).Tooltip("导出整个分组，不受搜索筛选影响；选全部则导出所有节点").Clicked() {
		a.shareCurrentGroup()
	}
	// 分组标签上能连节点一起删分组，节点列表可能这一帧刚变短；下面画卡片要按现在的重新算，拿上面的下标会越界崩掉
	keys, indices = delayKeys(a.settings.Nodes), visibleNodes(a.settings.Nodes, a.nodeGroup, a.nodeSearch)
	if group, ok := groupKey(a.nodeGroup); ok {
		if source, ok := a.settings.Subscriptions[group]; ok {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Padding(0, 0, 8, 0).Children(func() {
				host := source
				if u, err := url.Parse(source); err == nil {
					host = u.Host
				}
				ui.Text(c, "订阅 · "+host).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Grow(1).MinWidth(0)
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
	}
	if len(indices) == 0 {
		if len(a.settings.Nodes) == 0 {
			ui.Column(c).Padding(16, 0).Gap(6).Children(func() {
				ui.Text(c, "还没有节点").FontSize(13).TextColor(c.Theme().TextMuted)
				a.addButtons(c, "grid")
			})
			return
		}
		ui.Text(c, "没有匹配的节点").FontSize(13).TextColor(c.Theme().TextMuted).Padding(16, 0)
		return
	}
	if a.sortByDelay {
		// 测速时结果一个个回来，顺序先别动，测完再排，免得卡片在手底下跳
		if key := fmt.Sprint(indices); !a.testing || key != a.sortKey {
			a.sortKey, a.sorted = key, byDelay(indices, keys, a.delays)
		}
		indices = a.sorted
	}
	grid := ui.Grid(c.Key("node-grid")).FillWidth().Gap(8)
	remove := -1 // 确认删除要等网格建完再删，不然这一帧后面的下标就错了
	defer func() {
		if remove >= 0 {
			a.deleteNode(remove)
		}
	}()
	grid.Columns(nodeColumns(grid.Bounds().W)).Children(func() {
		for _, index := range indices {
			node := a.settings.Nodes[index]
			selected := a.settings.Selected == index
			if a.deleteConfirm && a.editIndex == index {
				if a.deleteCard(c, index) {
					remove = index
				}
				continue
			}
			card := ui.Row(c.Key(fmt.Sprintf("node-%d", index))).MinWidth(0).Height(64).Clip().AlignItems(ui.Center).Radius(9)
			background := paneColor(c.Theme())
			if card.Hovered() {
				background = c.Theme().SurfaceHover
			}
			if selected {
				background = c.Theme().SurfacePressed
			}
			card.Background(background).Border(1, c.Theme().Border)
			if selected {
				card.Border(2, c.Theme().Accent)
				if a.revealSelected {
					card.ScrollIntoView()
					a.revealSelected = false
				}
			}
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
			card.ContextMenu(menu).Children(func() {
				choose := ui.ButtonBase(c.Key("select")).MinWidth(0).Grow(1).FillHeight().Padding(10, 12).Label(node.Name).Tooltip(net.JoinHostPort(node.Host, node.Port)).Cursor(ui.CursorPointer)
				choose.Children(func() {
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
							if selected {
								ui.Text(c, "✓").FontSize(14).FontWeight(700).TextColor(c.Theme().Accent).Shrink(0)
							}
							ui.Text(c, node.Name).FontSize(13).FontWeight(600).SingleLine().Grow(1)
							delayBadge(c, a.delays, keys[index])
						})
						ui.Text(c, node.Kind+" · "+nodeGroupName(node)).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
					})
				})
				if choose.Clicked() && !selected {
					a.choose(index)
				}
				more := ui.ButtonBase(c.Key("more")).Size(26, 30).Center().Radius(6).Label("更多操作").Tooltip("更多操作").Cursor(ui.CursorPointer)
				if !card.Hovered() && !card.FocusWithin() {
					more.Opacity(0.35) // 平时淡淡地在，让人知道卡片还能编辑、删除
				}
				if more.Hovered() {
					more.Background(c.Theme().SurfaceHover)
				}
				more.Children(func() { ui.Text(c, "⋯").FontSize(17).TextColor(c.Theme().TextMuted).AlignSelf(ui.Center) }).Menu(menu)
				ui.Box(c).Width(6).Shrink(0)
			})
		}
	})
}

// deleteCard 删除确认就放在那张卡片原来的位置上，点的是哪张就在哪张上确认
func (a *application) deleteCard(c *ui.Context, index int) (confirmed bool) {
	t := c.Theme()
	ui.Row(c.Key(fmt.Sprintf("node-%d", index))).MinWidth(0).Height(64).Clip().AlignItems(ui.Center).Gap(6).Padding(0, 10, 0, 12).Radius(9).Background(t.Danger.Alpha(0.08)).Border(1, t.Danger.Alpha(0.45)).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, "删除这个节点？").FontSize(12).FontWeight(600).SingleLine()
			ui.Text(c, a.settings.Nodes[index].Name).FontSize(11).TextColor(t.TextMuted).SingleLine()
		})
		if ui.Button(c.Key("cancel-delete"), "取消").Clicked() {
			a.deleteConfirm = false
		}
		confirmed = ui.Button(c.Key("confirm-delete"), "删除").TextColor(t.Danger).Clicked()
	})
	return confirmed
}

// deleteNode 删掉第 index 个节点。删的是正在用的那个就顺延到下一个，并说一声换成了谁
func (a *application) deleteNode(index int) {
	next := a.settings
	next.Nodes = append([]Node(nil), next.Nodes...)
	next.Nodes = append(next.Nodes[:index], next.Nodes[index+1:]...)
	wasSelected := next.Selected == index
	if next.Selected > index || (wasSelected && index == len(next.Nodes)) {
		next.Selected--
	}
	if !a.commit(next) {
		return
	}
	a.deleteConfirm = false
	if wasSelected && next.Selected >= 0 {
		a.showNotice("删掉的是正在用的节点，已改用「" + next.Nodes[next.Selected].Name + "」")
	}
}

// delayBadge 显示延迟：0 表示测速中，-1 超时，没测过不显示
func delayBadge(c *ui.Context, delays map[string]int, key string) {
	ms, ok := delays[key]
	if !ok {
		return
	}
	text, color := delayLabel(c, ms)
	ui.Text(c, text).FontSize(11).TextColor(color).SingleLine().Shrink(0)
}

// delayLabel 延迟的文字和颜色：0 测速中，-1 超时
func delayLabel(c *ui.Context, ms int) (string, ui.Color) {
	switch {
	case ms < 0:
		return "超时", c.Theme().Danger
	case ms > 0:
		return fmt.Sprintf("%d ms", ms), delayColor(ms)
	}
	return "…", c.Theme().TextMuted
}

func delayColor(ms int) ui.Color {
	switch {
	case ms <= 500: // 和 v2rayN 一样，500 ms 以内算好
		return ui.Hex("#65967d")
	case ms <= 1000:
		return ui.Hex("#b08a4a")
	}
	return ui.Hex("#c0645a")
}
