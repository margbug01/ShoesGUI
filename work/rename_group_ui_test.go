package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func renameFixture(t *testing.T, nodeGroup string) *application {
	dir := t.TempDir()
	a := &application{storageOK: true, settingsPath: filepath.Join(dir, "s.dpapi"), settings: defaultSettings(), engine: newEngine(dir), nodeGroup: nodeGroup, tab: "代理", delays: map[string]int{}}
	a.settings.Nodes = []Node{sampleNode("Trojan"), sampleNode("VLESS"), sampleNode("SOCKS5")}
	a.settings.Nodes[0].Name, a.settings.Nodes[1].Name, a.settings.Nodes[2].Name = "东京 01", "大阪 02", "香港 03"
	a.settings.Nodes[0].Group, a.settings.Nodes[1].Group = "机场", "机场"
	a.settings.Nodes[0].FromSub, a.settings.Nodes[1].FromSub = true, true
	a.settings.Groups = []string{"机场"}
	a.settings.Subscriptions = map[string]string{"机场": "https://example.com/sub?token=fixture"}
	return a
}

// 从界面上改分组名：右键分组胶囊，或选中分组后点「⋯ 分组操作」→「重命名…」→ 弹窗里输入 → 点按钮。
// 「未分组」也能改：就是给没分组的节点起个组名
func TestRenameGroupFromTheUI(t *testing.T) {
	for _, tc := range []struct {
		name, group, via, button, want string
	}{
		{"机场/右键", "机场", "右键", "重命名", "新机场"},
		{"机场/分组操作", "机场", "分组操作", "重命名", "新机场"},
		{"未分组/右键", "未分组", "右键", "确定", "自建"},
		{"未分组/分组操作", "未分组", "分组操作", "确定", "自建"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := renameFixture(t, "全部")
			tester := ui.NewTester(a.view, 880, 680)
			for range 3 {
				tester.Frame()
			}
			if tc.via == "右键" {
				if err := tester.RightClick(tc.group); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := tester.Click(tc.group); err != nil {
					t.Fatal(err)
				}
				tester.Frame()
				if a.nodeGroup != tc.group {
					t.Fatal("chip did not select the group")
				}
				r, ok := tester.Find("分组操作")
				if !ok {
					t.Fatal("⋯ missing")
				}
				tester.Press(r.X+r.W/2, r.Y+r.H/2)
			}
			tester.Frame()
			if !slices.Contains(tester.Menu(), "重命名…") {
				t.Fatal("no rename in", tester.Menu())
			}
			if tc.group == "未分组" && slices.Contains(tester.Menu(), "删除分组…") {
				t.Fatal("未分组 cannot be deleted")
			}
			if err := tester.ChooseMenuItem("重命名…"); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				tester.Frame()
			}
			if !tester.Focused("分组名称") {
				t.Fatal("name input not focused")
			}
			if tc.group == "未分组" {
				if a.groupDraft != "" || !tester.HasText("这 1 个节点会一起放进这个新分组") {
					t.Fatalf("dialog for 未分组: draft %q", a.groupDraft)
				}
			} else if a.groupDraft != tc.group {
				t.Fatalf("draft %q", a.groupDraft)
			}
			tester.Command("selectAll")
			tester.Type(tc.want)
			tester.Frame()
			if err := tester.Click(tc.button); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				tester.Frame()
			}
			if a.renaming != "" || a.groupError != "" {
				t.Fatalf("dialog still open: renaming %q error %q", a.renaming, a.groupError)
			}
			if a.nodeGroup != tc.want || !slices.Contains(a.settings.Groups, tc.want) {
				t.Fatalf("groups %q, showing %q", a.settings.Groups, a.nodeGroup)
			}
			var got []string
			for _, n := range a.settings.Nodes {
				got = append(got, n.Group)
			}
			want := []string{"机场", "机场", tc.want}
			if tc.group == "机场" {
				want = []string{tc.want, tc.want, ""}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("node groups %q, want %q", got, want)
			}
			if tc.group == "未分组" && slices.Contains(orderedGroups(a.settings), "未分组") {
				t.Fatal("未分组 still listed")
			}
		})
	}
}

func TestRenameUngrouped(t *testing.T) {
	a := renameFixture(t, "未分组")
	// 订阅绑在「未分组」上的，跟着节点走
	a.settings.Subscriptions = map[string]string{"": "https://example.com/plain"}
	a.settings.Nodes[2].FromSub = true

	a.groupDraft = "机场"
	a.renameGroup("未分组")
	if a.groupError != "这个分组已经存在" || a.settings.Nodes[2].Group != "" {
		t.Fatalf("taken name: error %q", a.groupError)
	}
	a.groupDraft = "未分组"
	a.renameGroup("未分组")
	if a.groupError == "" {
		t.Fatal("「未分组」accepted as a name")
	}

	a.groupDraft = " 自建 "
	a.renameGroup("未分组")
	if a.groupError != "" {
		t.Fatal(a.groupError)
	}
	n := a.settings.Nodes[2]
	if n.Group != "自建" || !n.FromSub || a.settings.Nodes[0].Group != "机场" {
		t.Fatalf("nodes: %+v", a.settings.Nodes)
	}
	if !slices.Equal(a.settings.Groups, []string{"机场", "自建"}) {
		t.Fatalf("groups %q", a.settings.Groups)
	}
	if _, left := a.settings.Subscriptions[""]; left || a.settings.Subscriptions["自建"] != "https://example.com/plain" {
		t.Fatalf("subscriptions %v", a.settings.Subscriptions)
	}
	if a.nodeGroup != "自建" {
		t.Fatalf("showing %q", a.nodeGroup)
	}
}
