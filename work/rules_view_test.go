package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func rulesTestApp(t *testing.T, tab string) (*application, *ui.Tester) {
	t.Helper()
	dir := t.TempDir()
	a := &application{settings: defaultSettings(), engine: newEngine(dir), tab: tab, storageOK: true, settingsPath: filepath.Join(dir, "s.dpapi"), delays: map[string]int{}, nodeGroup: "全部"}
	a.settings.Nodes, a.settings.Selected = []Node{sampleNode("Trojan")}, 0
	tester := ui.NewTester(a.view, 880, 1400)
	tester.Frame()
	return a, tester
}

// issue #5：设置页里真点一遍：回车加、写错了说为什么、改走法、下移、删除，每一步都存进设置
func TestRulesSettingsFlow(t *testing.T) {
	a, tester := rulesTestApp(t, "设置")
	frames := func() {
		for range 3 {
			tester.Frame()
		}
	}
	if err := tester.Click("规则内容"); err != nil {
		t.Fatal(err)
	}
	tester.Type("*.Example.com")
	tester.Key(0, ui.KeyEnter)
	frames()
	if want := []Rule{{"suffix", "example.com", "direct"}}; !reflect.DeepEqual(a.settings.Rules, want) {
		t.Fatalf("after Enter: %v", a.settings.Rules)
	}
	if a.ruleDraft != "" {
		t.Fatal("draft should clear after adding")
	}
	tester.Type("https://x.com/a")
	tester.Key(0, ui.KeyEnter)
	frames()
	if len(a.settings.Rules) != 1 || a.ruleError == "" || a.ruleDraft == "" {
		t.Fatalf("bad input: rules %v, error %q, draft %q", a.settings.Rules, a.ruleError, a.ruleDraft)
	}
	if _, ok := tester.Find(a.ruleError); !ok {
		t.Fatal("error not shown under the input")
	}
	a.ruleDraft, a.ruleKind, a.ruleAction = "1.2.3.4", "IP 段", "拒绝"
	frames()
	if err := tester.Click("添加"); err != nil {
		t.Fatal(err)
	}
	frames()
	want := []Rule{{"suffix", "example.com", "direct"}, {"ip", "1.2.3.4/32", "reject"}}
	if !reflect.DeepEqual(a.settings.Rules, want) || a.ruleError != "" {
		t.Fatalf("after 添加: %v (error %q)", a.settings.Rules, a.ruleError)
	}
	if err := tester.Click("命中后：直连"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.ChooseMenuItem("代理"); err != nil {
		t.Fatal(err)
	}
	frames()
	if a.settings.Rules[0].Action != "proxy" {
		t.Fatalf("action change lost: %v", a.settings.Rules)
	}
	more, ok := tester.Find("规则操作") // 第一条的「⋯」
	if !ok {
		t.Fatal("⋯ missing")
	}
	tester.Press(more.X+more.W/2, more.Y+more.H/2)
	tester.Frame()
	if err := tester.ChooseMenuItem("下移"); err != nil {
		t.Fatal(err)
	}
	frames()
	if a.settings.Rules[0].Kind != "ip" || a.settings.Rules[1].Value != "example.com" {
		t.Fatalf("move down: %v", a.settings.Rules)
	}
	if err := tester.RightClick("example.com"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.ChooseMenuItem("删除"); err != nil {
		t.Fatal(err)
	}
	frames()
	if want := []Rule{{"ip", "1.2.3.4/32", "reject"}}; !reflect.DeepEqual(a.settings.Rules, want) {
		t.Fatalf("after delete: %v", a.settings.Rules)
	}
	var stored Settings // 真的存下了，重开还在
	if err := loadEncrypted(a.settingsPath, &stored); err != nil || !reflect.DeepEqual(stored.Rules, a.settings.Rules) {
		t.Fatalf("stored %v, err %v", stored.Rules, err)
	}
}

// 连接记录里右键一个域名就能按它所在的站点加规则
func TestFlowRowAddsRule(t *testing.T) {
	a, tester := rulesTestApp(t, "日志")
	a.logPane = paneFlows
	var conn flowConn
	conn.ID, conn.Metadata.Host, conn.Chains = "1", "www.google.com", []string{"a0", groupSelect, groupOther}
	a.flows.record([]flowConn{conn}, time.Now())
	tester.Frame()
	tester.Frame()
	if err := tester.RightClick("www.google.com"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.ChooseMenuItem("添加规则", "google.com 及子域名 → 直连"); err != nil {
		t.Fatalf("%v; menu %q", err, tester.Menu())
	}
	tester.Frame()
	tester.Frame()
	if want := []Rule{{"suffix", "google.com", "direct"}}; !reflect.DeepEqual(a.settings.Rules, want) {
		t.Fatalf("rules %v", a.settings.Rules)
	}
}

// 旧版 direct-domains.txt：打开时并进设置并存下，原文件改名留底，日志里说一声；再打开不会重复并
func TestImportLegacyDirect(t *testing.T) {
	a, _ := rulesTestApp(t, "设置")
	dir := filepath.Dir(a.settingsPath)
	legacy := filepath.Join(dir, "direct-domains.txt")
	os.WriteFile(legacy, []byte("# 注释\ncotaiticketing.com\nticketflap.queue-it.net\nbad domain\n"), 0600)
	a.importLegacyDirect(dir)
	want := []Rule{{"suffix", "cotaiticketing.com", "direct"}, {"suffix", "ticketflap.queue-it.net", "direct"}}
	var stored Settings
	if err := loadEncrypted(a.settingsPath, &stored); err != nil || !reflect.DeepEqual(stored.Rules, want) || !reflect.DeepEqual(a.settings.Rules, want) {
		t.Fatalf("stored %v (%v), in memory %v", stored.Rules, err, a.settings.Rules)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy file still in place")
	}
	if _, err := os.Stat(legacy + ".imported"); err != nil {
		t.Fatal("backup missing")
	}
	if !logged(a.engine, "已并进") || !logged(a.engine, "bad domain") {
		t.Fatal("import not reported in the log")
	}
	a.importLegacyDirect(dir) // 文件已改名，什么都不做
	if !reflect.DeepEqual(a.settings.Rules, want) {
		t.Fatalf("second run changed rules: %v", a.settings.Rules)
	}
}

func TestRuleTargets(t *testing.T) {
	for host, want := range map[string][]Rule{
		"www.google.com":    {{Kind: "suffix", Value: "google.com"}, {Kind: "domain", Value: "www.google.com"}},
		"google.com":        {{Kind: "suffix", Value: "google.com"}},
		"a.b.example.co.uk": {{Kind: "suffix", Value: "example.co.uk"}, {Kind: "domain", Value: "a.b.example.co.uk"}},
		"localhost":         {{Kind: "suffix", Value: "localhost"}},
		"1.2.3.4":           {{Kind: "ip", Value: "1.2.3.4/32"}},
		"2001:db8::1":       {{Kind: "ip", Value: "2001:db8::1/128"}},
	} {
		if got := ruleTargets(host); !reflect.DeepEqual(got, want) {
			t.Errorf("ruleTargets(%q) = %v, want %v", host, got, want)
		}
	}
}
