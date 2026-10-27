package main

import (
	"fmt"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type shareState struct {
	title        string
	text         string
	subscription bool
	count        int
	png          []byte
	bitmap       *ui.Bitmap
	warning      string
}

var saveShareDialog = func(window *mygo.Window, extension string) (string, error) {
	return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: window, Title: "保存分享内容", DefaultPath: "Shoes-share." + extension, Filters: []mygo.FileFilter{{Name: extension, Extensions: []string{extension}}}})
}

func (a *application) openShare(nodes []Node, source string, title string) {
	state := &shareState{title: title, count: len(nodes), subscription: source != ""}
	var err error
	if source != "" {
		state.text = source
	} else {
		state.text, err = shareNodes(nodes)
	}
	if err != nil {
		a.errorText = err.Error()
		return
	}
	if state.subscription || len(nodes) == 1 {
		state.png, err = shareQR(state.text)
		if err == nil {
			state.bitmap, err = ui.DecodeBitmap(state.png)
		}
		if err != nil {
			state.warning = "二维码不可用或内容过长，请复制链接。"
			state.png = nil
		}
	}
	a.share = state
	a.errorText = ""
}

func (a *application) shareCurrentGroup() {
	var nodes []Node
	for _, node := range a.settings.Nodes {
		if a.nodeGroup == "全部" || nodeGroupName(node) == a.nodeGroup {
			nodes = append(nodes, node)
		}
	}
	a.openShare(nodes, "", a.nodeGroup+" · 导出节点")
}

func (a *application) saveShare(data []byte, extension string) {
	a.async(func() {
		path, err := saveShareDialog(a.window, extension)
		if err == nil && path != "" {
			err = os.WriteFile(path, data, 0600)
		}
		update := func() {
			if err != nil {
				a.errorText = "保存失败，请检查文件权限与可用空间"
			} else if path != "" {
				a.showNotice("已保存分享文件，请妥善保管")
			}
		}
		if a.window != nil {
			a.window.Update(update)
		}
	})
}

func (a *application) shareView(c *ui.Context) {
	state := a.share
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		if ui.Button(c.Key("close-share"), "返回节点").Clicked() {
			a.share = nil
		}
		ui.Text(c, state.title).FontSize(18).SingleLine().Grow(1).MinWidth(0)
	})
	ui.Column(c).FillWidth().Gap(12).Padding(16, 0).Children(func() {
		if state.subscription {
			ui.Text(c, "分享整个订阅").FontSize(16)
			ui.Text(c, "注意：对方可能获得整个账号的节点并使用你的流量；订阅导入后可以更新。仅分享给可信的人。").TextColor(c.Theme().Danger)
		} else {
			ui.Text(c, fmt.Sprintf("%d 个节点 · 当前配置快照，不会自动更新", state.count)).TextColor(c.Theme().TextMuted)
			ui.Text(c, "链接和二维码包含连接凭据，仅分享给可信的人。")
		}
		if state.bitmap != nil {
			ui.Image(c.Key("share-qr"), state.bitmap).Width(280).Height(280)
			ui.Text(c, "打开 Shadowrocket 首页的扫一扫；也可以复制链接后在目标客户端导入。").FontSize(12).TextColor(c.Theme().TextMuted)
		} else if state.warning != "" {
			ui.Text(c, state.warning).TextColor(c.Theme().Danger)
		} else {
			ui.Text(c, "批量节点不合并成二维码，请复制链接或导出 TXT，在目标客户端批量导入。").TextColor(c.Theme().TextMuted)
		}
		ui.Text(c, "目标客户端须支持对应协议和传输；AnyTLS、Reality、XHTTP 等请使用支持该配置的版本。未经过 Shadowrocket 真机验证。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
			label := "复制链接"
			if state.subscription {
				label = "复制订阅地址"
			}
			if ui.PrimaryButton(c.Key("copy-share"), label).Clicked() {
				writeClipboard(state.text)
				a.showNotice("已复制，剪贴板中包含连接凭据")
			}
			if ui.Button(c.Key("save-share-text"), "导出 TXT").Disabled(a.busy.Load()).Clicked() {
				a.saveShare([]byte(state.text+"\n"), "txt")
			}
			if len(state.png) > 0 && ui.Button(c.Key("save-share-qr"), "保存二维码").Disabled(a.busy.Load()).Clicked() {
				a.saveShare(state.png, "png")
			}
		})
	})
}
