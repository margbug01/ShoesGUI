package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestGroupsAndNotice(t *testing.T) {
	dir := t.TempDir()
	a := &application{storageOK: true, settingsPath: filepath.Join(dir, "settings.dpapi"), settings: defaultSettings()}
	a.groupDraft = "  收藏  "
	a.addGroup()
	if a.nodeGroup != "收藏" || len(a.settings.Groups) != 1 {
		t.Fatal("group not created")
	}
	var stored Settings
	if err := loadEncrypted(a.settingsPath, &stored); err != nil {
		t.Fatal(err)
	}
	if _, ok := groupCounts(stored)["收藏"]; !ok {
		t.Fatal("empty group not persisted")
	}
	for _, name := range []string{"收藏", "全部", "未分组", " "} {
		a.groupDraft = name
		a.addGroup()
		if a.groupError == "" || len(a.settings.Groups) != 1 {
			t.Fatal("invalid group accepted", name)
		}
	}
	a.settings.Nodes = []Node{{Name: "one", Group: "旧组"}}
	a.moveNode(0, "收藏")
	if a.settings.Nodes[0].Group != "收藏" {
		t.Fatal("move failed")
	}
	if _, ok := groupCounts(a.settings)["旧组"]; !ok {
		t.Fatal("source group disappeared")
	}
	a.beginEdit(-1)
	if a.draft.Group != "收藏" {
		t.Fatal("new node does not inherit active group")
	}
	a.showNotice("first")
	oldDeadline := a.noticeUntil
	a.showNotice("second")
	a.noticeUntil = oldDeadline.Add(time.Second)
	a.expireNotice(oldDeadline)
	if a.notice != "second" {
		t.Fatal("new notice expired early")
	}
	a.expireNotice(a.noticeUntil)
	if a.notice != "" {
		t.Fatal("notice stayed visible")
	}
}

func TestGroupControls(t *testing.T) {
	for _, dark := range []bool{false, true} {
		dir := t.TempDir()
		a := &application{storageOK: true, settingsPath: filepath.Join(dir, "settings.dpapi"), settings: defaultSettings()}
		a.settings.Nodes = []Node{{Name: "sample", Group: "已有"}}
		tester := ui.NewTester(func(c *ui.Context) {
			theme := *c.Theme()
			theme.Dark = dark
			mamiColors(&theme)
			c.SetTheme(&theme)
			ui.Column(c).Fill().Children(func() { a.nodeGrid(c); a.statusBar(c) })
		}, 820, 620)
		click := func(label string) {
			t.Helper()
			if err := tester.Click(label); err != nil {
				t.Fatal(err)
			}
			tester.Frame()
			tester.Frame()
		}
		click("+ 新建分组")
		a.groupDraft = "空组"
		click("创建分组")
		if a.nodeGroup != "空组" || a.addingGroup {
			t.Fatal("create group click failed")
		}
		tester.Frame()
		if a.nodeGroup != "空组" {
			t.Fatal("empty group reset on render")
		}
		click("全部")
		if a.nodeGroup != "全部" {
			t.Fatal("group switch failed")
		}
		a.showNotice("已导入 1 个")
		tester.Frame()
		click("关闭提示")
		if a.notice != "" {
			t.Fatal("dismiss failed")
		}
	}
}

func TestNodeGridLayout(t *testing.T) {
	for _, test := range []struct {
		width   float32
		columns int
	}{{0, 2}, {360, 1}, {447, 1}, {448, 2}, {540, 2}, {676, 3}, {904, 4}} {
		if got := nodeColumns(test.width); got != test.columns {
			t.Fatalf("width %.0f: got %d columns, want %d", test.width, got, test.columns)
		}
	}
	nodes := []Node{{Name: "Hong Kong", Group: "uzi"}, {Name: "香港", Group: "快车"}, {Name: "Local"}}
	if got := visibleNodes(nodes, "uzi", "KONG"); len(got) != 1 || got[0] != 0 {
		t.Fatal("group/search intersection incorrect")
	}
	if got := visibleNodes(nodes, "快车", ""); len(got) != 1 || got[0] != 1 {
		t.Fatal("wrong original index after filtering")
	}
	if got := visibleNodes(nodes, "未分组", ""); len(got) != 1 || got[0] != 2 {
		t.Fatal("ungrouped node missing")
	}
	if got := visibleNodes(nodes, "全部", "missing"); len(got) != 0 {
		t.Fatal("empty search result incorrect")
	}
}

func TestDeleteConfirmsOnTheCard(t *testing.T) {
	dir := t.TempDir()
	a := &application{storageOK: true, settingsPath: filepath.Join(dir, "settings.dpapi"), settings: defaultSettings(), engine: newEngine(dir), nodeGroup: "全部", delays: map[string]int{}}
	a.settings.Nodes = []Node{sampleNode("Trojan"), sampleNode("Trojan"), sampleNode("Trojan")}
	for i, name := range []string{"一号", "二号", "三号"} {
		a.settings.Nodes[i].Name = name
	}
	a.settings.Selected = 1
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { a.nodeGrid(c); a.statusBar(c) })
	}, 820, 620)
	a.editIndex, a.deleteConfirm = 1, true
	tester.Frame()
	if _, ok := tester.Find("删除这个节点？"); !ok {
		t.Fatal("confirmation not shown on the card")
	}
	if err := tester.Click("删除"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Frame()
	if len(a.settings.Nodes) != 2 || a.settings.Selected != 1 || a.settings.Nodes[1].Name != "三号" {
		t.Fatalf("delete: %+v selected %d", len(a.settings.Nodes), a.settings.Selected)
	}
	if !strings.Contains(a.notice, "三号") {
		t.Fatalf("no notice about the new selection: %q", a.notice)
	}
}
