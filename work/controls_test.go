package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestChoiceBind(t *testing.T) {
	var ch choice[int]
	if p, v, changed := ch.bind(2); changed || v != 2 || *p != 2 {
		t.Fatal("first frame must show the current value")
	}
	*ch.bind0() = 1 // 控件在这一帧之后写回
	if _, v, changed := ch.bind(2); !changed || v != 1 {
		t.Fatal("user pick lost")
	}
	if _, v, changed := ch.bind(0); changed || v != 0 {
		t.Fatal("outside change (tray, failed save) must win when the user did nothing")
	}
}

func (ch *choice[T]) bind0() *T { return &ch.v }

// MyGo 在一帧建完之后才把点击写回绑定的变量；这里真点一遍，确认每个控件的改动都能落到设置里
func TestControlsRespondToClicks(t *testing.T) {
	applied := ""
	saved := applyTheme
	applyTheme = func(key string) { applied = key }
	defer func() { applyTheme = saved }()
	dir := t.TempDir()
	a := &application{settings: defaultSettings(), engine: newEngine(dir), tab: "设置", storageOK: true, settingsPath: filepath.Join(dir, "s.dpapi"), delays: map[string]int{}, nodeGroup: "全部"}
	a.settings.Nodes, a.settings.Selected = []Node{sampleNode("Trojan")}, 0
	tester := ui.NewTester(a.view, 820, 2000)
	click := func(label string) {
		t.Helper()
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
		tester.Frame()
		tester.Frame()
	}
	click("浅色")
	if a.settings.Theme != "light" || applied != "light" {
		t.Fatalf("theme: saved %q, applied %q", a.settings.Theme, applied)
	}
	r, _ := tester.Find("打开时自动接管系统代理")
	tester.ClickAt(770, r.Y+r.H/2+9)
	tester.Frame()
	if !a.settings.AutoConnect {
		t.Fatal("auto-connect switch ignored")
	}
	a.tab = "仪表盘"
	tester.Frame()
	click("全局")
	if a.settings.Mode != "global" {
		t.Fatalf("mode: %q", a.settings.Mode)
	}
	a.tab = "代理"
	a.beginEdit(-1)
	a.draft.Kind, a.draft.Network, a.advanced = "VLESS", "xhttp", true
	tester.Frame()
	click("none")
	click("tls")
	if !a.draft.TLS {
		t.Fatal("security select ignored")
	}
	r, _ = tester.Find("XHTTP 扩展参数")
	tester.ClickAt(r.X+40, r.Y+r.H+40)
	tester.Type(`{"noGRPCHeader":true}`)
	tester.Frame()
	if string(a.draft.XHTTPExtra) != `{"noGRPCHeader":true}` {
		t.Fatalf("textarea: %q", a.draft.XHTTPExtra)
	}
}

// 设置页没有保存按钮：端口回车就存，失焦也存，不合法就在下面说为什么、不存
func TestPortSavesOnEnterAndBlur(t *testing.T) {
	dir := t.TempDir()
	a := &application{settings: defaultSettings(), engine: newEngine(dir), tab: "设置", storageOK: true, settingsPath: filepath.Join(dir, "s.dpapi"), delays: map[string]int{}, nodeGroup: "全部"}
	a.portDraft = a.settings.Port
	tester := ui.NewTester(a.view, 820, 2000)
	tester.Frame()
	r, ok := tester.Find("本地端口")
	if !ok {
		t.Fatal("no port row")
	}
	tester.ClickAt(720, r.Y+r.H/2+9)
	tester.Frame()
	tester.Key(ui.Ctrl, ui.KeyA)
	tester.Type("18080")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	tester.Frame()
	if a.settings.Port != "18080" {
		t.Fatalf("enter did not save: %q (draft %q)", a.settings.Port, a.portDraft)
	}
	tester.ClickAt(720, r.Y+r.H/2+9)
	tester.Frame()
	tester.Key(ui.Ctrl, ui.KeyA)
	tester.Type("99999")
	tester.Frame()
	tester.ClickAt(300, 30) // 点别处
	tester.Frame()
	tester.Frame()
	if a.settings.Port != "18080" || a.portError == "" {
		t.Fatalf("invalid port: saved %q, error %q", a.settings.Port, a.portError)
	}
}
