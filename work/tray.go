package main

import (
	_ "embed"
	"fmt"

	"github.com/egoist/mygo"
)

//go:embed assets/tray-on.png
var trayOnPNG []byte

//go:embed assets/tray-off.png
var trayOffPNG []byte

// trayIcon 托盘图标：加粗的 S，已接管绿底、没接管灰底。和应用图标同一条 S，但去掉了虚线和点，16px 也认得出
func trayIcon(on bool) []byte {
	if on {
		return trayOnPNG
	}
	return trayOffPNG
}

func (a *application) createTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(false), ToolTip: "Shoes"})
	if err != nil {
		a.engine.event("", "托盘图标创建失败")
		return
	}
	a.tray = tray
	tray.OnClick(a.showWindow)
	tray.OnRightClick(func() {
		_, _, _, proxy := a.engine.snapshot()
		toggle := "连接"
		if proxy {
			toggle = "断开"
		}
		var modeItems []*mygo.MenuItem
		for _, m := range modes {
			key := m.key
			modeItems = append(modeItems, &mygo.MenuItem{Label: m.label, Type: mygo.MenuItemRadio, Checked: a.settings.Mode == key,
				Click: func(*mygo.MenuItem, *mygo.Window) { a.setMode(key) }})
		}
		menu := mygo.NewMenu([]*mygo.MenuItem{
			{Label: "打开 Shoes", Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow() }},
			{Label: toggle, Disabled: a.busy.Load() || len(a.settings.Nodes) == 0, Click: func(*mygo.MenuItem, *mygo.Window) { a.toggleConnect() }},
			{Label: "节点", Submenu: a.trayNodes(), Disabled: len(a.settings.Nodes) == 0},
			{Label: "出站模式", Submenu: modeItems},
			mygo.Separator(),
			{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { a.quit() }},
		})
		tray.SetMenu(menu)
		defer tray.SetMenu(nil)
		tray.PopUpMenu()
	})
}

// trayNodes 托盘里切节点：一个分组一个子菜单（只有一组就直接列出来），带上次测的延迟
func (a *application) trayNodes() []*mygo.MenuItem {
	keys := delayKeys(a.settings.Nodes)
	groups := orderedGroups(a.settings)
	var out []*mygo.MenuItem
	for _, group := range groups {
		var items []*mygo.MenuItem
		for i, n := range a.settings.Nodes {
			if nodeGroupName(n) != group {
				continue
			}
			index, label := i, n.Name
			if ms := a.delays[keys[i]]; ms > 0 {
				label += fmt.Sprintf("    %d ms", ms)
			}
			items = append(items, &mygo.MenuItem{Label: label, Type: mygo.MenuItemRadio, Checked: a.settings.Selected == i,
				Click: func(*mygo.MenuItem, *mygo.Window) {
					a.window.Update(func() {
						if index != a.settings.Selected && index < len(a.settings.Nodes) {
							a.choose(index)
						}
					})
				}})
		}
		if len(groups) == 1 {
			return items
		}
		out = append(out, &mygo.MenuItem{Label: group, Submenu: items})
	}
	return out
}

// syncTray 只在状态变了时换图标和提示
func (a *application) syncTray() {
	if a.tray == nil {
		return
	}
	_, _, _, proxy := a.engine.snapshot()
	tip := "Shoes · 未启动"
	if proxy {
		tip = "Shoes · 运行中"
		if node, ok := a.selectedNode(); ok {
			tip += " · " + node.Name
		}
	}
	if proxy != a.trayOn {
		a.trayOn = proxy
		a.tray.SetIcon(trayIcon(proxy), false)
	}
	if tip != a.trayTip {
		a.trayTip = tip
		a.tray.SetToolTip(tip)
	}
}

// hintTray 第一次关窗口时用系统通知说一声：窗口不见了不等于退出了
func (a *application) hintTray() {
	if a.settings.TrayHinted {
		return
	}
	next := a.settings
	next.TrayHinted = true
	a.save(next)
	go mygo.NewNotification(mygo.NotificationOptions{Title: "Shoes 还在托盘里运行", Body: "关窗口只是收到托盘，代理照常工作。要退出请右键托盘图标。"}).Show()
}

func (a *application) showWindow() {
	if a.window == nil {
		return
	}
	a.window.Show()
	a.window.Restore()
	a.window.Focus()
}

func (a *application) quit() {
	a.quitting = true
	mygo.App.Quit()
}
