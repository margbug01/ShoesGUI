package main

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

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
