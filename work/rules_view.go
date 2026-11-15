package main

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"golang.org/x/net/publicsuffix"
)

// rulesSection 设置页的「自定义规则」：第一行加规则，下面按顺序列出已有的。改完内核就地换配置，连接不断
func (a *application) rulesSection(c *ui.Context) {
	p := pal(c)
	kinds := make([]string, len(ruleKinds))
	for i, k := range ruleKinds {
		kinds[i] = k.label
	}
	actions := make([]string, len(ruleActions))
	for i, act := range ruleActions {
		actions[i] = act.label
	}
	if !slices.Contains(kinds, a.ruleKind) {
		a.ruleKind = kinds[0]
	}
	if !slices.Contains(actions, a.ruleAction) {
		a.ruleAction = actions[0]
	}
	section(c, "自定义规则", func() {
		// 下拉框有最小宽度，窗口最窄时三样挤一行输入框就没了，所以分两行
		ui.Column(c).FillWidth().Padding(12, 16).Gap(8).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Select(c.Key("rule-kind"), &a.ruleKind, kinds).Label("规则类型")
				ui.Select(c.Key("rule-action"), &a.ruleAction, actions).Label("命中后")
			})
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				hint := ruleKinds[slices.Index(kinds, a.ruleKind)].hint
				input := ui.TextInput(c.Key("rule-value"), &a.ruleDraft).Grow(1).MinWidth(0).Placeholder(hint).Label("规则内容")
				add := ui.Button(c.Key("add-rule"), "添加").Disabled(strings.TrimSpace(a.ruleDraft) == "")
				if add.Clicked() || input.Submitted() {
					a.addRuleDraft()
				}
			})
			if a.ruleError != "" {
				ui.Text(c, a.ruleError).FontSize(12).TextColor(p.bad).LineHeight(1.4)
			}
		})
		for i, r := range a.settings.Rules {
			a.ruleRow(c, i, r)
		}
	})
	sectionNote(c, "按顺序排在所有规则前面，先匹配上的那条算数。「直连」「拒绝」在全局模式下也算；「代理」走选中的节点，直连模式下照样直连。也可以在「日志 › 连接记录」里右键一个域名来加。")
}

// addRuleDraft 把输入的那条加进去；写法不对就在输入框下面说为什么，草稿留着改
func (a *application) addRuleDraft() {
	kind := ruleKinds[slices.IndexFunc(ruleKinds, func(k struct{ key, label, mask, hint string }) bool { return k.label == a.ruleKind })]
	action := ruleActions[slices.IndexFunc(ruleActions, func(k struct{ key, label string }) bool { return k.label == a.ruleAction })]
	value, err := normalizeRule(kind.key, a.ruleDraft)
	if err != nil {
		a.ruleError = err.Error()
		return
	}
	if a.setRules(addRule(a.settings.Rules, Rule{kind.key, value, action.key})) {
		a.ruleDraft, a.ruleError = "", ""
		a.showNotice(fmt.Sprintf("规则已保存：%s %s", value, action.label))
	}
}

// setRules 存下新的规则表，内核在后台换配置
func (a *application) setRules(rules []Rule) bool {
	next := a.settings
	next.Rules = rules
	return a.commit(next)
}

// ruleRow 一条规则：写法和类型，右边改走法，「⋯」里上移、下移、删除（右键也一样）
func (a *application) ruleRow(c *ui.Context, i int, r Rule) {
	p := pal(c)
	rules := a.settings.Rules
	menu := func(m *ui.Menu) {
		if m.Item("上移").Disabled(i == 0).Chosen() {
			next := slices.Clone(rules)
			next[i-1], next[i] = next[i], next[i-1]
			a.setRules(next)
		}
		if m.Item("下移").Disabled(i == len(rules)-1).Chosen() {
			next := slices.Clone(rules)
			next[i], next[i+1] = next[i+1], next[i]
			a.setRules(next)
		}
		m.Separator()
		if m.Item("删除").Danger().Chosen() {
			a.setRules(slices.Delete(slices.Clone(rules), i, i+1))
		}
	}
	ui.Row(c.Key("rule-"+r.Kind+"-"+r.Value)).FillWidth().Padding(8, 8, 8, 16).Gap(8).AlignItems(ui.Center).MinHeight(44).ContextMenu(menu).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, r.Value).FontSize(13.5).FontWeight(500).SingleLine()
			ui.Text(c, ruleKinds[ruleKindIndex(r.Kind)].label).FontSize(12).TextColor(p.muted).SingleLine()
		})
		current := ruleActions[ruleActionIndex(r.Action)]
		ui.Button(c.Key("action"), current.label+" ▾").Label("命中后：" + current.label).Menu(func(m *ui.Menu) {
			for _, act := range ruleActions {
				if m.Item(act.label).Radio(act.key == current.key).Chosen() && act.key != current.key {
					a.setRules(addRule(rules, Rule{r.Kind, r.Value, act.key}))
				}
			}
		})
		iconButton(c.Key("more"), moreIcon, "规则操作", false).Menu(menu)
	})
}

// ruleTargets 从连接记录里的一行能加哪些规则：IP 就是这个 IP；域名是它所在的站点（含子域名），
// 站点之外还有更细的子域名时再给一个只管这个域名的
func ruleTargets(host string) []Rule {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []Rule{{Kind: "ip", Value: netip.PrefixFrom(addr, addr.BitLen()).String()}}
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	site, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		site = host
	}
	var targets []Rule
	for _, t := range []Rule{{Kind: "suffix", Value: site}, {Kind: "domain", Value: host}} {
		if value, err := normalizeRule(t.Kind, t.Value); err == nil && (t.Kind == "suffix" || host != site) {
			targets = append(targets, Rule{Kind: t.Kind, Value: value})
		}
	}
	return targets
}

// ruleMenu 连接记录右键的「添加规则」：每个可加的写法下面列出三种走法
func (a *application) ruleMenu(m *ui.Menu, host string) {
	targets := ruleTargets(host)
	m.Submenu("添加规则", func(m *ui.Menu) {
		for i, t := range targets {
			if i > 0 {
				m.Separator()
			}
			desc := map[string]string{"suffix": t.Value + " 及子域名", "domain": "只有 " + t.Value, "ip": t.Value}[t.Kind]
			for _, act := range ruleActions {
				if m.Item(desc + " → " + act.label).Chosen() {
					t.Action = act.key
					if a.setRules(addRule(a.settings.Rules, t)) {
						a.showNotice(fmt.Sprintf("规则已保存：%s %s，可在「设置 › 自定义规则」里改", t.Value, act.label))
					}
				}
			}
		}
	}).Disabled(len(targets) == 0)
}
