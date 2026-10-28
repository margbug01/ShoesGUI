package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func shellTestApp(t *testing.T) *application {
	t.Helper()
	dir := t.TempDir()
	e := newEngine(dir)
	e.ready, e.proxy.active, e.since, e.port = true, true, time.Now().Add(-time.Hour), "10808"
	a := &application{settings: defaultSettings(), engine: e, tab: "仪表盘", storageOK: true, settingsPath: filepath.Join(dir, "s.dpapi"), delays: map[string]int{}, nodeGroup: "全部"}
	a.settings.Nodes, a.settings.Selected = []Node{sampleNode("Trojan"), sampleNode("VLESS")}, 0
	a.settings.Port, a.portDraft = "10808", "10808"
	a.dash.exitKey = "0/0" // 已经查过出口 IP，测试里别真去查
	return a
}

// 默认是软件渲染，每一帧都花 CPU：页面摆好、过渡走完以后什么都不动，就不该再要帧
func TestIdlePagesAskForNoFrames(t *testing.T) {
	applyTheme = func(string) {}
	for _, dark := range []bool{false, true} {
		for i, tab := range append(tabs, "日志") {
			a := shellTestApp(t)
			a.tab = tab
			a.flows.record([]flowConn{flow("1", "www.google.com", viaNode, ruleRest, 1, 1)}, time.Now())
			if i == len(tabs) {
				a.logPane = paneLogs // 最后再看一遍日志页的另一块
			}
			tester := ui.NewTester(a.view, 880, 640)
			tester.SetDark(dark)
			tester.SetTitleBar(ui.TitleBar{Height: 50, Left: 78})
			tester.Frame()
			tester.Advance(2 * time.Second)
			tester.Frame()
			if tester.FramePending() {
				t.Fatalf("%s (dark %v) keeps asking for frames", tab, dark)
			}
		}
	}
}

// 同一句提示说第二遍（比如连着复制两次日志）也要再弹一次 Toast，不能因为字一样就不弹
func TestNoticeShowsAsToastEachTime(t *testing.T) {
	a := shellTestApp(t)
	tester := ui.NewTester(a.view, 880, 640)
	tester.Frame()
	a.showNotice("已复制 3 条日志")
	tester.Frame()
	if !tester.HasText("已复制 3 条日志") {
		t.Fatal("no toast")
	}
	tester.Advance(6 * time.Second)
	tester.Frame()
	if tester.HasText("已复制 3 条日志") {
		t.Fatal("toast never went away")
	}
	a.showNotice("已复制 3 条日志")
	tester.Frame()
	if !tester.HasText("已复制 3 条日志") {
		t.Fatal("the same notice again showed no toast")
	}
}
