package main

import (
	_ "embed"
	"fmt"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
)

//go:embed assets/tray-on.png
var trayOnPNG []byte

//go:embed assets/tray-off.png
var trayOffPNG []byte

//go:embed assets/tray-template-on.png
var trayTemplateOnPNG []byte

//go:embed assets/tray-template-off.png
var trayTemplateOffPNG []byte

// trayIcon 托盘图标：加粗的 S，已接管绿底、没接管灰底。和应用图标同一条 S，但去掉了虚线和点，16px 也认得出。
// macOS 菜单栏用同形状的黑白模板图（系统按浅色/深色栏着色，和别的应用一致），没接管时半透明
func trayIcon(on bool) (png []byte, template bool) {
	if runtime.GOOS == "darwin" {
		if on {
			return trayTemplateOnPNG, true
		}
		return trayTemplateOffPNG, true
	}
	if on {
		return trayOnPNG, false
	}
	return trayOffPNG, false
}

// traySpeed 菜单栏上的网速。只有数字等宽，换单位（K→M）宽度会变，所以固定用 MB/s、两位小数，
// 不足的位用和数字一样宽的 figure space 补齐，菜单栏项宽度就不再跟着网速伸缩
func traySpeed(r rate) string {
	mb := func(n int64) string {
		return strings.ReplaceAll(fmt.Sprintf("%6.2f", min(float64(n)/(1<<20), 999.99)), " ", "\u2007")
	}
	return "↑" + mb(r.up) + " ↓" + mb(r.down) + " MB/s"
}

func (a *application) createTray() {
	icon, template := trayIcon(false)
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: icon, IconIsTemplate: template, ToolTip: "MyShoes"})
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
		items := []*mygo.MenuItem{
			{Label: "打开 MyShoes", Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow() }},
			{Label: toggle, Disabled: a.busy.Load() || len(a.settings.Nodes) == 0, Click: func(*mygo.MenuItem, *mygo.Window) { a.toggleConnect() }},
			{Label: "节点", Submenu: a.trayNodes(), Disabled: len(a.settings.Nodes) == 0},
			{Label: "出站模式", Submenu: modeItems},
		}
		if runtime.GOOS == "darwin" { // 托盘旁的文字只有 macOS 菜单栏能显示
			items = append(items, &mygo.MenuItem{Label: "在菜单栏显示网速", Type: mygo.MenuItemCheckbox, Checked: a.settings.TraySpeed,
				Click: func(*mygo.MenuItem, *mygo.Window) {
					a.window.Update(func() {
						next := a.settings
						next.TraySpeed = !next.TraySpeed
						if a.save(next) {
							a.syncTray()
						}
					})
				}})
		}
		items = append(items, mygo.Separator(), &mygo.MenuItem{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { a.quit() }})
		menu := mygo.NewMenu(items)
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

// syncTray 只在状态变了时换图标、提示和菜单栏网速
func (a *application) syncTray() {
	if a.tray == nil {
		return
	}
	_, _, _, proxy := a.engine.snapshot()
	tip := "MyShoes · 未启动"
	if proxy {
		tip = "MyShoes · 运行中"
		if node, ok := a.selectedNode(); ok {
			tip += " · " + node.Name
		}
	}
	if tip != a.trayTip {
		a.trayTip = tip
		a.tray.SetToolTip(tip)
	}
	speed := a.settings.TraySpeed && runtime.GOOS == "darwin"
	var last rate
	if n := len(a.dash.speeds); speed && n > 0 {
		last = a.dash.speeds[n-1]
	}
	lines := traySpeedLines(last)
	key := fmt.Sprint(proxy, speed, lines)
	if !speed {
		key = fmt.Sprint(proxy, speed)
	}
	if key == a.trayKey {
		return
	}
	a.trayKey = key
	icon, template := trayIcon(proxy)
	title := ""
	if speed {
		alpha := uint8(255)
		if !proxy {
			alpha = 128 // 没接管时和图标一样半透明
		}
		if img, err := traySpeedIcon(icon, lines, alpha); err == nil {
			icon = img
		} else {
			title = traySpeed(last) // 画不了两行就退回图标旁边一行文字
		}
	}
	a.tray.SetIcon(icon, template)
	a.tray.SetTitle(title)
}

// hintTray 第一次关窗口时用系统通知说一声：窗口不见了不等于退出了
func (a *application) hintTray() {
	if a.settings.TrayHinted {
		return
	}
	next := a.settings
	next.TrayHinted = true
	a.save(next)
	go mygo.NewNotification(mygo.NotificationOptions{Title: "MyShoes 还在托盘里运行", Body: "关窗口只是收到托盘，代理照常工作。要退出请右键托盘图标。"}).Show()
}

func (a *application) showWindow() {
	if a.window == nil {
		return
	}
	mygo.App.Dock.Show()
	a.window.Show()
	a.window.Restore()
	a.window.Focus()
}

func (a *application) quit() {
	a.quitting = true
	mygo.App.Quit()
}
