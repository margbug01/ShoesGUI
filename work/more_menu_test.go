package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// 行尾「⋯」打开的菜单：选项目时鼠标已经在菜单上、离开了这一行，选的项照样要生效
func TestMoreMenuWorksAfterThePointerLeaves(t *testing.T) {
	for _, tc := range []struct {
		item []string
		done func(a *application) bool
	}{
		{[]string{"编辑"}, func(a *application) bool { return a.editing && a.editIndex == 0 }},
		{[]string{"分享节点"}, func(a *application) bool { return a.share != nil && a.share.count == 1 }},
		{[]string{"删除"}, func(a *application) bool { return a.deleteConfirm && a.editIndex == 0 }},
		{[]string{"移至分组", "机场"}, func(a *application) bool { return a.settings.Nodes[0].Group == "机场" }},
	} {
		t.Run(strings.Join(tc.item, "›"), func(t *testing.T) {
			dir := t.TempDir()
			a := &application{storageOK: true, settingsPath: filepath.Join(dir, "s.dpapi"), settings: defaultSettings(), engine: newEngine(dir), nodeGroup: "全部", tab: "代理", delays: map[string]int{}}
			a.settings.Nodes = []Node{sampleNode("Trojan"), sampleNode("VLESS")}
			a.settings.Nodes[0].Name, a.settings.Nodes[1].Name = "东京 01", "大阪 02"
			a.settings.Nodes[0].TLS = true // 不带 TLS 的 Trojan 分享不出去
			a.settings.Nodes[1].Group = "机场"
			a.settings.Selected = 1
			tester := ui.NewTester(a.view, 880, 680)
			tester.Frame()
			row, ok := tester.Find(a.settings.Nodes[0].Name)
			if !ok {
				t.Fatal("row missing")
			}
			tester.Move(row.X+row.W/2, row.Y+row.H/2)
			for range 4 {
				tester.Frame()
			}
			more, ok := tester.Find("更多操作")
			if !ok {
				t.Fatal("⋯ missing on the hovered row")
			}
			tester.Press(more.X+more.W/2, more.Y+more.H/2)
			tester.Frame()
			if len(tester.Menu()) == 0 {
				t.Fatal("menu did not open")
			}
			// 鼠标挪到菜单上（行的下面）去点
			tester.Move(more.X-60, row.Y+row.H+60)
			for range 4 {
				tester.Frame()
			}
			if err := tester.ChooseMenuItem(tc.item...); err != nil {
				t.Fatal(err)
			}
			tester.Frame()
			tester.Frame()
			if !tc.done(a) {
				t.Fatalf("%s did nothing (error %q)", tc.item, a.errorText)
			}
		})
	}
}
