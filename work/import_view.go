package main

import (
	"fmt"
	"sort"
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
			field(c, "SNI", &a.draft.SNI, false)
		}
		if a.draft.Reality {
			field(c, "公钥", &a.draft.PublicKey, true)
			field(c, "Short ID", &a.draft.ShortID, true)
		}
		if a.draft.Network == "ws" || a.draft.Network == "xhttp" {
			field(c, "Host", &a.draft.HTTPHost, false)
			field(c, "路径", &a.draft.Path, false)
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
			field(c, "ALPN", &a.draft.ALPN, false)
			if a.draft.Kind == "VLESS" {
				field(c, "VLESS 加密", &a.draft.Encryption, true)
			}
			if a.draft.Reality {
				field(c, "ML-DSA 验证", &a.draft.MLDSAVerify, true)
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
	return strings.TrimSpace(a.importText) != "" && a.importParsed != a.importText && !a.busy.Load() && now.Sub(a.importChanged) >= 600*time.Millisecond
}

func (a *application) parseImportAsync() {
	if a.busy.Load() || strings.TrimSpace(a.importText) == "" {
		return
	}
	text := a.importText
	a.importObserved = text
	a.importChanged = time.Now()
	a.importParsed = ""
	a.importResult = importResult{}
	a.errorText = ""
	a.async(func() {
		result := loadImport(text)
		a.window.Update(func() {
			if a.importing && a.importText == text {
				a.importResult = result
				a.importParsed = text
				if len(result.Nodes) > 0 && len(result.Issues) == 0 && !a.importNeedsTarget {
					a.finishImport()
				}
			}
		})
	})
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
	next, added := importBatch(a.settings, a.importResult.Nodes, target)
	if added > 0 && !a.commit(next) {
		return
	}
	skipped := len(a.importResult.Nodes) - added
	if added > 0 {
		a.showNotice(fmt.Sprintf("已导入 %d 个至「%s」，跳过 %d 个重复节点", added, name, skipped))
	} else {
		a.showNotice(fmt.Sprintf("%d 个节点已存在，未重复导入或新建分组", skipped))
	}
	a.importing = false
	a.importText, a.importObserved, a.importParsed = "", "", ""
	a.importResult = importResult{}
}

func (a *application) openImport() {
	a.importTarget = a.nodeGroup
	a.importNeedsTarget = a.nodeGroup == "" || a.nodeGroup == "全部"
	if a.importNeedsTarget {
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
	a.importText = mygo.Clipboard.ReadText()
	a.importObserved = a.importText
	a.importChanged = time.Now()
}

func (a *application) importView(c *ui.Context) {
	if a.importNeedsTarget {
		groups := []string{"未分组"}
		for name := range groupCounts(a.settings) {
			if name != "未分组" && name != "全部" {
				groups = append(groups, name)
			}
		}
		sort.Strings(groups[1:])
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "导入到").FontSize(13)
			if a.importNew {
				ui.TextInput(c.Key("import-new-group"), &a.importNewName).Width(240).Placeholder("新分组名称")
				if ui.Button(c.Key("import-existing"), "选择已有分组").Clicked() {
					a.importNew = false
				}
			} else {
				ui.Select(c.Key("import-target"), &a.importTarget, groups).Width(240)
				if ui.Button(c.Key("import-new"), "新建分组").Clicked() {
					a.importNew = true
				}
			}
		})
	} else {
		ui.Text(c, "将自动导入当前分组："+a.importTarget).FontSize(13)
	}
	group(c, "", func() {
		ui.Column(c).FillWidth().Padding(12, 0).Gap(10).Children(func() {
			ui.Text(c, "节点链接或订阅地址").FontSize(14)
			ui.TextArea(c.Key("import-text"), &a.importText).FillWidth().Height(140).Disabled(a.busy.Load()).Placeholder("支持多条链接、Base64 订阅内容和订阅 URL")
			ui.Text(c, "VLESS · VMess · Shadowsocks · Trojan · AnyTLS · SOCKS5 · HTTP").FontSize(11).TextColor(c.Theme().TextMuted)
		})
	})
	ui.Row(c).FillWidth().Padding(8, 0).Gap(8).Children(func() {
		ui.Spacer(c)
		if iconButton(c.Key("paste-again"), pasteIcon, "粘贴", false, a.busy.Load()).Clicked() {
			a.importText = mygo.Clipboard.ReadText()
			a.importParsed = ""
			a.importResult = importResult{}
		}
		if ui.Button(c.Key("parse"), "重新识别").Disabled(a.busy.Load() || strings.TrimSpace(a.importText) == "").Clicked() {
			a.parseImportAsync()
		}
	})
	if a.busy.Load() {
		ui.Text(c, "正在识别…订阅地址会自动联网读取").FontSize(12).TextColor(c.Theme().TextMuted)
	} else if strings.TrimSpace(a.importText) == "" {
		ui.Text(c, "具体分组内自动导入当前组；从「全部」进入时请选择归属后点导入").FontSize(12).TextColor(c.Theme().TextMuted)
	} else if a.importParsed != a.importText {
		ui.Text(c, "等待输入完成，随后自动识别…").FontSize(12).TextColor(c.Theme().TextMuted)
	}
	if a.importParsed != "" && a.importParsed == a.importText {
		ui.Text(c, fmt.Sprintf("识别到 %d 个节点", len(a.importResult.Nodes))).FontSize(14).FontWeight(600)
		for index, node := range a.importResult.Nodes {
			if index == 8 {
				ui.Text(c, fmt.Sprintf("另有 %d 个节点", len(a.importResult.Nodes)-8)).FontSize(12).TextColor(c.Theme().TextMuted)
				break
			}
			ui.Text(c, node.Name+" · "+node.Kind).FontSize(12).TextColor(c.Theme().TextMuted)
		}
		for index, issue := range a.importResult.Issues {
			if index == 8 {
				ui.Text(c, fmt.Sprintf("另有 %d 项未识别", len(a.importResult.Issues)-8)).FontSize(12)
				break
			}
			ui.Text(c, issue).FontSize(12).TextColor(c.Theme().Danger)
		}
	}
	ui.Row(c).FillWidth().Padding(12, 0).Gap(8).Children(func() {
		ui.Spacer(c)
		if ui.Button(c.Key("cancel-import"), "取消").Disabled(a.busy.Load()).Clicked() {
			a.importing = false
			a.importText = ""
			a.importParsed = ""
			a.importResult = importResult{}
		}
		if ui.PrimaryButton(c.Key("confirm-import"), "导入").MinWidth(88).Disabled(a.importParsed != a.importText || len(a.importResult.Nodes) == 0).Clicked() {
			a.finishImport()
		}
	})
}
