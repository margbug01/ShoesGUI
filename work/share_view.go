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
	p := pal(c)
	state := a.share
	ui.Row(c).FillWidth().Wrap().Gap(20).Padding(18).Radius(12).Background(p.card).Border(1, p.hairline).Children(func() {
		if state.bitmap != nil {
			ui.Box(c).Size(232, 232).Padding(10).Radius(10).Background(ui.RGB(255, 255, 255)).Shrink(0).Children(func() {
				ui.Image(c.Key("share-qr"), state.bitmap).Size(212, 212)
			})
		}
		ui.Column(c).Grow(1).Basis(240).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, state.title).FontSize(17).FontWeight(700).SingleLine()
			if state.subscription {
				ui.Text(c, "分享整个订阅").FontSize(13).TextColor(p.muted)
				ui.Text(c, "对方可能拿到整个账号的节点并用你的流量；订阅导入后可以更新。只分享给可信的人。").FontSize(12.5).TextColor(p.bad).LineHeight(1.45)
			} else {
				ui.Text(c, fmt.Sprintf("%d 个节点 · 当前配置的快照，不会自动更新", state.count)).FontSize(13).TextColor(p.muted)
				ui.Text(c, "链接和二维码里有连接凭据，只分享给可信的人。").FontSize(12.5).LineHeight(1.45)
			}
			switch {
			case state.bitmap != nil:
				ui.Text(c, "打开 Shadowrocket 首页的扫一扫；也可以复制链接，在目标客户端里导入。").FontSize(12).TextColor(p.muted).LineHeight(1.45)
			case state.warning != "":
				ui.Text(c, state.warning).FontSize(12.5).TextColor(p.bad).LineHeight(1.45)
			default:
				ui.Text(c, "批量节点不合并成二维码，请复制链接或导出 TXT，在目标客户端批量导入。").FontSize(12).TextColor(p.muted).LineHeight(1.45)
			}
			ui.Text(c, "目标客户端要支持对应的协议和传输；AnyTLS、Reality、XHTTP 等请用支持这些配置的版本。没在 Shadowrocket 真机上验证过。").FontSize(12).TextColor(p.faint).LineHeight(1.45)
			ui.Row(c).FillWidth().Wrap().Gap(8).Margin(6, 0, 0, 0).Children(func() {
				label := "复制链接"
				if state.subscription {
					label = "复制订阅地址"
				}
				if ui.PrimaryButton(c.Key("copy-share"), label).Clicked() {
					writeClipboard(state.text)
					a.showNotice("已复制，剪贴板里有连接凭据")
				}
				if ui.Button(c.Key("save-share-text"), "导出 TXT").Disabled(a.busy.Load()).Clicked() {
					a.saveShare([]byte(state.text+"\n"), "txt")
				}
				if len(state.png) > 0 && ui.Button(c.Key("save-share-qr"), "保存二维码").Disabled(a.busy.Load()).Clicked() {
					a.saveShare(state.png, "png")
				}
			})
		})
	})
}
