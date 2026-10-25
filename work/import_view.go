package main

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var pasteIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="8" y="3" width="8" height="4" rx="1"/><path d="M8 5H5v16h14V5h-3M8 13h8M8 17h5"/></svg>`))

func (a *application) transportFields(c *ui.Context) {
	group(c, "传输", func() {
		if a.draft.Network == "" {
			a.draft.Network = "tcp"
		}
		settingRow(c, "传输方式", "", func() { ui.Select(c.Key("network"), &a.draft.Network, []string{"tcp", "ws", "xhttp"}).Width(240) })
		security := "none"
		if a.draft.TLS || a.draft.Kind == "Trojan" || a.draft.Kind == "AnyTLS" {
			security = "tls"
		}
		if a.draft.Reality {
			security = "reality"
		}
		choices := []string{"none", "tls", "reality"}
		if a.draft.Kind == "Trojan" || a.draft.Kind == "AnyTLS" {
			choices = []string{"tls"}
		}
		p, picked, _ := a.securityPick.bind(security)
		security = picked
		settingRow(c, "安全层", "", func() { ui.Select(c.Key("security"), p, choices).Width(240) })
		a.draft.TLS, a.draft.Reality = security == "tls", security == "reality"
		if security != "none" {
			field(c, "SNI", &a.draft.SNI)
		}
		if a.draft.Reality {
			field(c, "公钥", &a.draft.PublicKey)
			field(c, "Short ID", &a.draft.ShortID)
		}
		if a.draft.Network == "ws" || a.draft.Network == "xhttp" {
			field(c, "Host", &a.draft.HTTPHost)
			field(c, "路径", &a.draft.Path)
		}
		if a.draft.Network == "xhttp" {
			if a.draft.Mode == "" {
				a.draft.Mode = "auto"
			}
			settingRow(c, "模式", "", func() {
				ui.Select(c.Key("xhttp-mode"), &a.draft.Mode, []string{"auto", "stream-one", "stream-up", "packet-up"}).Width(240)
			})
		}
		if a.draft.Kind == "VLESS" {
			settingRow(c, "Flow", "", func() { ui.Select(c.Key("flow"), &a.draft.Flow, []string{"", "xtls-rprx-vision"}).Width(240) })
		}
		if a.draft.Kind == "VMess" {
			if a.draft.Cipher != "auto" && a.draft.Cipher != "aes-128-gcm" && a.draft.Cipher != "chacha20-poly1305" && a.draft.Cipher != "none" {
				a.draft.Cipher = "auto"
			}
			settingRow(c, "加密", "", func() {
				ui.Select(c.Key("vmess-cipher"), &a.draft.Cipher, []string{"auto", "aes-128-gcm", "chacha20-poly1305", "none"}).Width(240)
			})
		}
	})
	ui.Row(c).FillWidth().Children(func() {
		ui.Spacer(c)
		if ui.Button(c.Key("advanced"), "高级参数").Clicked() {
			a.advanced = !a.advanced
		}
	})
	if a.advanced {
		group(c, "", func() {
			field(c, "ALPN", &a.draft.ALPN)
			if a.draft.Kind == "VLESS" {
				field(c, "VLESS 加密", &a.draft.Encryption)
			}
			if a.draft.Reality {
				field(c, "ML-DSA 验证", &a.draft.MLDSAVerify)
			}
			if a.draft.TLS {
				settingRow(c, "跳过证书验证", "", func() { ui.Switch(c.Key("skip-verify"), &a.draft.SkipVerify) })
			}
			if a.draft.Network == "xhttp" {
				p, extra, _ := a.extraPick.bind(string(a.draft.XHTTPExtra))
				ui.Text(c, "XHTTP 扩展参数").FontSize(12).Padding(8, 0)
				ui.TextArea(c.Key("xhttp-extra"), p).FillWidth().Height(100)
				a.draft.XHTTPExtra = []byte(extra)
			}
		})
	}
}

func (a *application) showNotice(text string) {
	a.notice = text
	a.noticeUntil = time.Now().Add(4 * time.Second)
}

func (a *application) expireNotice(now time.Time) {
	if a.notice != "" && !now.Before(a.noticeUntil) {
		a.notice = ""
	}
}

func (a *application) importReady(now time.Time) bool {
	if !a.importing {
		return false
	}
	if a.importObserved != a.importText {
		a.importObserved = a.importText
		a.importChanged = now
		a.importParsed = ""
		a.importResult = importResult{}
		return false
	}
	return strings.TrimSpace(a.importText) != "" && a.importParsed != a.importText && !a.parsing && now.Sub(a.importChanged) >= 600*time.Millisecond
}

// parseImportAsync 识别粘贴的内容（订阅地址会联网读）。只出预览，导不导由人点「导入」决定。
// 用自己的 parsing 标记，不占 busy，拉订阅时启动/停止照常能点
func (a *application) parseImportAsync() {
	if a.parsing || strings.TrimSpace(a.importText) == "" {
		return
	}
	text := a.importText
	a.importObserved = text
	a.importChanged = time.Now()
	a.importParsed = ""
	a.importResult = importResult{}
	a.errorText = ""
	a.parsing = true
	go func() {
		result := loadImport(text)
		a.window.Update(func() {
			a.parsing = false
			if a.importing && a.importText == text {
				a.importResult = result
				a.importParsed = text
				a.suggestTarget(result)
			}
		})
	}()
}

// suggestTarget 订阅给个合适的去处：已经绑过的就回到那个分组（再导一次就是更新）；
// 要导去的分组里已经有节点、绑不上订阅的，就建议新建一个以订阅域名命名的分组
func (a *application) suggestTarget(result importResult) {
	if result.Source == "" {
		return
	}
	for group, source := range a.settings.Subscriptions {
		if source == result.Source {
			a.importNew, a.importTarget = false, nodeGroupName(Node{Group: group})
			return
		}
	}
	counts := groupCounts(a.settings)
	if a.importNew || counts[a.importTarget] == 0 {
		return
	}
	name := "订阅"
	if u, err := url.Parse(result.Source); err == nil && u.Hostname() != "" {
		name = u.Hostname()
	}
	base := name
	for k := 2; counts[name] > 0 || name == "全部" || name == "未分组" || slices.Contains(a.settings.Groups, name); k++ {
		name = fmt.Sprintf("%s %d", base, k)
	}
	a.importNew, a.importNewName = true, name
}

func (a *application) finishImport() {
	if a.importParsed != a.importText || len(a.importResult.Nodes) == 0 {
		return
	}
	name := a.importTarget
	if a.importNew {
		name = strings.TrimSpace(a.importNewName)
		_, exists := groupCounts(a.settings)[name]
		if name == "" || name == "全部" || name == "未分组" || exists {
			a.errorText = "请输入新的分组名称，不能重名或使用保留名称"
			return
		}
	}
	if name == "" || name == "全部" {
		a.errorText = "请选择导入分组"
		return
	}
	target := name
	if target == "未分组" {
		target = ""
	}
	result := a.importResult
	source := result.Source
	if source != "" && a.settings.Subscriptions[target] == source {
		// 同一个订阅再导一次就是更新，不然换了密码的节点会再加一份、旧的留着
		if a.applySync(target, result) {
			a.closeImport()
		}
		return
	}
	// 订阅只绑到空分组，免得订阅节点和原来手动加的混在一组、重名分不清
	bind := source != "" && groupCounts(a.settings)[name] == 0
	next, added := importBatch(a.settings, result.Nodes, target)
	if bind {
		for i := len(a.settings.Nodes); i < len(next.Nodes); i++ {
			next.Nodes[i].FromSub = true
		}
		next.Subscriptions = maps.Clone(next.Subscriptions)
		if next.Subscriptions == nil {
			next.Subscriptions = map[string]string{}
		}
		next.Subscriptions[target] = source
		if target != "" && !slices.Contains(next.Groups, target) {
			next.Groups = append(append([]string(nil), next.Groups...), target)
		}
	}
	if (added > 0 || bind) && !a.commit(next) {
		return
	}
	skipped := len(result.Nodes) - added
	text := fmt.Sprintf("%d 个节点已存在，没有重复导入", skipped)
	if added > 0 {
		text = fmt.Sprintf("已导入 %d 个至「%s」，跳过 %d 个重复节点", added, name, skipped)
	}
	switch {
	case bind:
		text += "。以后在这个分组里点「更新订阅」就能同步"
	case source != "":
		text += "。分组里原来就有节点，订阅没绑到这里；想一键更新请导入到新分组"
	}
	a.showNotice(text)
	a.closeImport()
}

func (a *application) closeImport() {
	a.importing = false
	a.importText, a.importObserved, a.importParsed = "", "", ""
	a.importResult = importResult{}
}

// applySync 用订阅内容同步 group，返回是否成功（已是最新也算）
func (a *application) applySync(group string, result importResult) bool {
	label := nodeGroupName(Node{Group: group})
	if len(result.Nodes) == 0 {
		reason := "订阅里没有能用的节点"
		if len(result.Issues) > 0 {
			reason = result.Issues[0]
		}
		a.errorText = "更新「" + label + "」失败：" + reason
		return false
	}
	next, added, updated, removed := syncGroup(a.settings, group, result.Nodes)
	if added+updated+removed == 0 {
		a.showNotice("「" + label + "」已是最新")
		return true
	}
	if !a.commit(next) {
		return false
	}
	text := fmt.Sprintf("已更新「%s」：新增 %d、更新 %d、移除 %d", label, added, updated, removed)
	if len(result.Issues) > 0 {
		text += fmt.Sprintf("，%d 条没认出来", len(result.Issues))
	}
	a.showNotice(text)
	return true
}

// updateSubscription 重新拉 group 的订阅并同步。不占 busy，拉订阅时启动/停止照常能点
func (a *application) updateSubscription(group string) {
	source, ok := a.settings.Subscriptions[group]
	if !ok || a.fetching {
		return
	}
	a.fetching = true
	go func() {
		result := loadImport(source)
		a.window.Update(func() {
			a.fetching = false
			if a.settings.Subscriptions[group] == source {
				a.applySync(group, result)
			}
		})
	}()
}

func (a *application) openImport() {
	a.importTarget = a.nodeGroup
	if a.nodeGroup == "" || a.nodeGroup == "全部" {
		a.importTarget = "未分组"
	}
	a.importNew, a.importNewName = false, ""
	a.importing = true
	a.editing = false
	a.errorText = ""
	a.notice = ""
	a.importText = ""
	a.importParsed = ""
	a.importResult = importResult{}
	if text := readClipboard(); importable(text) {
		a.importText = text
	}
	a.importObserved = a.importText
	a.importChanged = time.Now()
}

// linkSchemes 认得的节点链接开头（不支持的也算，好在预览里说清楚为什么不行）
var linkSchemes = []string{"vless", "vmess", "ss", "trojan", "anytls", "socks", "socks5", "http", "https", "hysteria2", "hy2", "tuic"}

// importable 剪贴板里是节点链接、Base64 订阅内容或订阅地址才自动填进来，
// 免得把刚复制的密码之类原样摊在输入框里
func importable(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if subscriptionURL(text) {
		return true
	}
	if !strings.Contains(text, "://") {
		data, err := decodeBase64(text)
		if err != nil {
			return false
		}
		text = string(data)
	}
	for _, line := range strings.Fields(text) {
		if scheme, _, ok := strings.Cut(line, "://"); ok && slices.Contains(linkSchemes, strings.ToLower(scheme)) {
			return true
		}
	}
	return false
}

func (a *application) importView(c *ui.Context) {
	muted := c.Theme().TextMuted
	groups := orderedGroups(a.settings)
	if !slices.Contains(groups, "未分组") {
		groups = append(groups, "未分组")
	}
	group(c, "", func() {
		ui.Column(c).FillWidth().Padding(12, 0).Gap(10).Children(func() {
			ui.Text(c, "节点链接或订阅地址").FontSize(14)
			ui.TextArea(c.Key("import-text"), &a.importText).FillWidth().Height(140).Disabled(a.parsing).Placeholder("支持多条链接、Base64 订阅内容和订阅 URL")
			ui.Text(c, "VLESS · VMess · Shadowsocks · Trojan · AnyTLS · SOCKS5 · HTTP").FontSize(11).TextColor(muted)
		})
	})
	ui.Row(c).FillWidth().Padding(8, 0).Gap(8).AlignItems(ui.Center).Children(func() {
		switch {
		case a.parsing:
			ui.Text(c, "正在识别…订阅地址会联网读取").FontSize(12).TextColor(muted).Grow(1)
		case strings.TrimSpace(a.importText) == "":
			ui.Text(c, "粘贴后会自动识别，看过预览再点「导入」").FontSize(12).TextColor(muted).Grow(1)
		case a.importParsed != a.importText:
			ui.Text(c, "停下输入后自动识别…").FontSize(12).TextColor(muted).Grow(1)
		default:
			ui.Spacer(c)
		}
		if iconButton(c.Key("paste-again"), pasteIcon, "粘贴", false, a.parsing).Clicked() {
			a.importText = readClipboard()
			a.importParsed = ""
			a.importResult = importResult{}
		}
		if ui.Button(c.Key("parse"), "重新识别").Disabled(a.parsing || strings.TrimSpace(a.importText) == "").Clicked() {
			a.parseImportAsync()
		}
	})
	if a.importParsed != "" && a.importParsed == a.importText {
		ui.Text(c, fmt.Sprintf("识别到 %d 个节点", len(a.importResult.Nodes))).FontSize(14).FontWeight(600)
		for index, node := range a.importResult.Nodes {
			if index == 8 {
				ui.Text(c, fmt.Sprintf("另有 %d 个节点", len(a.importResult.Nodes)-8)).FontSize(12).TextColor(muted)
				break
			}
			ui.Text(c, node.Name+" · "+node.Kind).FontSize(12).TextColor(muted)
		}
		for index, issue := range a.importResult.Issues {
			if index == 8 {
				ui.Text(c, fmt.Sprintf("另有 %d 项未识别", len(a.importResult.Issues)-8)).FontSize(12)
				break
			}
			ui.Text(c, issue).FontSize(12).TextColor(c.Theme().Danger)
		}
	}
	ui.Row(c).FillWidth().Padding(12, 0, 4, 0).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, "导入到").FontSize(13)
		if a.importNew {
			ui.TextInput(c.Key("import-new-group"), &a.importNewName).Width(200).Placeholder("新分组名称")
			if ui.Button(c.Key("import-existing"), "选已有分组").Clicked() {
				a.importNew = false
			}
		} else {
			ui.Select(c.Key("import-target"), &a.importTarget, groups).Width(200)
			if ui.Button(c.Key("import-new"), "新建分组").Clicked() {
				a.importNew = true
			}
		}
		ui.Spacer(c)
		if ui.Button(c.Key("cancel-import"), "取消").Clicked() {
			a.closeImport()
		}
		if ui.PrimaryButton(c.Key("confirm-import"), "导入").MinWidth(88).Disabled(a.parsing || a.importParsed != a.importText || len(a.importResult.Nodes) == 0).Clicked() {
			a.finishImport()
		}
	})
	if source := a.importResult.Source; source != "" && a.importParsed == a.importText {
		hint := "订阅放进单独的分组，以后在分组里点「更新订阅」就能同步"
		if a.importNew {
			hint = "已建议新建分组：订阅单独一组，以后在分组里点「更新订阅」就能同步"
		}
		for group, bound := range a.settings.Subscriptions {
			if bound == source && !a.importNew && a.importTarget == nodeGroupName(Node{Group: group}) {
				hint = "这个订阅已经在「" + a.importTarget + "」里，导入会按订阅更新这个分组"
			}
		}
		ui.Text(c, hint).FontSize(12).TextColor(muted)
	}
}

// readClipboard 测试里换掉：离屏测试没有主循环，读剪贴板会一直等主线程
var readClipboard = func() string { return mygo.Clipboard.ReadText() }

// writeClipboard 和 readClipboard 一样，测试里换掉
var writeClipboard = func(text string) { mygo.Clipboard.WriteText(text) }
