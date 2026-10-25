package main

import (
	"fmt"
	"net"
	"sort"
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

func (a *application) addGroup() {
	name := strings.TrimSpace(a.groupDraft)
	if name == "" || name == "全部" || name == "未分组" {
		a.groupError = "请输入分组名称，不能使用「全部」或「未分组」"
		return
	}
	if _, exists := groupCounts(a.settings)[name]; exists {
		a.groupError = "这个分组已经存在"
		return
	}
	next := a.settings
	next.Groups = append(append([]string(nil), next.Groups...), name)
	if a.save(next) {
		a.nodeGroup, a.nodeSearch = name, ""
		a.addingGroup, a.groupDraft, a.groupError = false, "", ""
	}
}

func (a *application) moveNode(index int, group string) {
	next := a.settings
	next.Nodes = append([]Node(nil), next.Nodes...)
	previous := next.Nodes[index].Group
	if previous != "" {
		found := false
		for _, name := range next.Groups {
			found = found || name == previous
		}
		if !found {
			next.Groups = append(append([]string(nil), next.Groups...), previous)
		}
	}
	next.Nodes[index].Group = group
	a.save(next)
}

func (a *application) nodeGrid(c *ui.Context) {
	counts := groupCounts(a.settings)
	groups := make([]string, 0, len(counts))
	for group := range counts {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	groups = append([]string{"全部"}, groups...)
	_, groupExists := counts[a.nodeGroup]
	if a.nodeGroup == "" || (a.nodeGroup != "全部" && !groupExists) {
		a.nodeGroup = "全部"
	}
	ui.TextInput(c.Key("search-nodes"), &a.nodeSearch).FillWidth().Placeholder("搜索节点")
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
			a.addingGroup = true
			a.groupError = ""
		}
	})
	if a.addingGroup {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("group-name"), &a.groupDraft).Grow(1).Placeholder("分组名称")
			if ui.Button(c.Key("cancel-group"), "取消").Clicked() {
				a.addingGroup, a.groupDraft, a.groupError = false, "", ""
			}
			if ui.PrimaryButton(c.Key("create-group"), "创建分组").Clicked() {
				a.addGroup()
			}
		})
		if a.groupError != "" {
			ui.Text(c, a.groupError).FontSize(12).TextColor(c.Theme().Danger)
		}
	}
	indices := visibleNodes(a.settings.Nodes, a.nodeGroup, a.nodeSearch)
	if len(indices) == 0 {
		text := "没有匹配的节点"
		if len(a.settings.Nodes) == 0 {
			text = "还没有节点，点右上角 ＋ 添加"
		}
		ui.Text(c, text).FontSize(13).TextColor(c.Theme().TextMuted).Padding(16, 0)
		return
	}
	names := kernelNames(a.settings.Nodes)
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
			}
			menu := func(m *ui.Menu) {
				if m.Item("真连接延迟").Disabled(a.testing).Chosen() {
					a.testNodes([]int{index}, false)
				}
				if m.Item("TCPing").Disabled(a.testing).Chosen() {
					a.testNodes([]int{index}, true)
				}
				for _, target := range groups[1:] {
					if target == nodeGroupName(node) {
						continue
					}
					if m.Item("移至分组：" + target).Chosen() {
						name := target
						if name == "未分组" {
							name = ""
						}
						a.moveNode(index, name)
					}
				}
				if m.Item("编辑").Chosen() {
					a.beginEdit(index)
				}
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
							delayBadge(c, a.delays, names[index])
						})
						ui.Text(c, node.Kind+" · "+nodeGroupName(node)).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
					})
				})
				if choose.Clicked() && !selected {
					a.choose(index)
				}
				more := ui.ButtonBase(c.Key("more")).Size(26, 30).Center().Radius(6).Label("更多操作").Tooltip("更多操作").Cursor(ui.CursorPointer)
				if !card.Hovered() && !card.FocusWithin() {
					more.Opacity(0)
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
func delayBadge(c *ui.Context, delays map[string]int, name string) {
	ms, ok := delays[name]
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
