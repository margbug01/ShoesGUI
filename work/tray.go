package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"

	"github.com/egoist/mygo"
)

// trayIcon 画一个 32px 的圆角方块加白点：运行中是绿色，没接管是灰色，深浅任务栏上都看得见
func trayIcon(on bool) []byte {
	fill := color.NRGBA{0x8d, 0x94, 0x9f, 0xff}
	if on {
		fill = color.NRGBA{0x4f, 0x8a, 0x6c, 0xff}
	}
	const size, radius = 32, 8.0
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			// 到圆角矩形的距离，取 1px 抗锯齿
			dx := math.Max(math.Abs(px-16)-(16-radius-1), 0)
			dy := math.Max(math.Abs(py-16)-(16-radius-1), 0)
			edge := math.Hypot(dx, dy) - radius
			alpha := math.Min(math.Max(0.5-edge, 0), 1)
			if alpha == 0 {
				continue
			}
			c := fill
			if dot := math.Hypot(px-16, py-16) - 5.5; dot < 0.5 {
				t := math.Min(math.Max(0.5-dot, 0), 1)
				c.R = uint8(float64(c.R)*(1-t) + 255*t)
				c.G = uint8(float64(c.G)*(1-t) + 255*t)
				c.B = uint8(float64(c.B)*(1-t) + 255*t)
			}
			c.A = uint8(alpha * 255)
			img.SetNRGBA(x, y, c)
		}
	}
	var out bytes.Buffer
	png.Encode(&out, img)
	return out.Bytes()
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
		toggle := "启动"
		if proxy {
			toggle = "停止"
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
