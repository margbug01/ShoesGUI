package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func flow(id, host string, chains []string, payload string, up, down int64) flowConn {
	f := flowConn{ID: id, Chains: chains, RulePayload: payload, Upload: up, Download: down}
	f.Metadata.Host, f.Metadata.DestinationPort = host, "443"
	return f
}

var (
	viaNode     = []string{"gc-jp", groupSelect, groupOther}
	viaDirect   = []string{directMember, groupDirect, groupDomestic}
	ruleRest    = `mixed-in (root): "0.0.0.0/0"`
	ruleSiteCN  = `mixed-in (root): {"path":"/x/rules/geosite-cn.mrs","behavior":"domain"}`
	ruleIPCN    = `mixed-in (root): {"path":"/x/rules/geoip-cn.mrs","behavior":"ipcidr"}`
	rulePrivate = `mixed-in (root): ["` + strings.Join(privateRanges, `","`) + `"]`
)

// 走法看 chains：有「直连」组就是直连，不然第一项是节点；全局模式下国内的也经节点。规则翻成人话
func TestFlowRouteAndRule(t *testing.T) {
	for _, test := range []struct {
		chains []string
		direct bool
		via    string
	}{
		{viaNode, false, "gc-jp"},
		{viaDirect, true, ""},
		{[]string{directMember, groupDirect}, true, ""},
		{[]string{"gc-jp", groupSelect, groupDomestic}, false, "gc-jp"},
	} {
		if direct, via := flowRoute(test.chains); direct != test.direct || via != test.via {
			t.Fatalf("%v: direct %v via %q", test.chains, direct, via)
		}
	}
	for payload, want := range map[string]string{ruleRest: "其余", ruleSiteCN: "国内域名", ruleIPCN: "国内 IP", rulePrivate: "局域网", "": ""} {
		if got := ruleLabel(payload); got != want {
			t.Fatalf("%s: got %q, want %q", payload, got, want)
		}
	}
}

// 内核每秒报一次开着的连接：同一条只算一次，流量按比上次多的加；断了次数还在；内核重启后同一个 id 是新连接
func TestFlowBookCountsEachConnectionOnce(t *testing.T) {
	var b flowBook
	now := time.Now()
	b.record([]flowConn{flow("1", "google.com", viaNode, ruleRest, 100, 1000)}, now)
	b.record([]flowConn{flow("1", "google.com", viaNode, ruleRest, 250, 4000), flow("2", "google.com", viaNode, ruleRest, 0, 10)}, now.Add(time.Second))
	rows, _ := b.rows()
	if len(rows) != 1 || rows[0].Total != 2 || rows[0].Active != 2 || rows[0].Up != 250 || rows[0].Down != 4010 || rows[0].Rule != "其余" {
		t.Fatalf("%+v", rows)
	}
	if b.record([]flowConn{flow("1", "google.com", viaNode, ruleRest, 250, 4000), flow("2", "google.com", viaNode, ruleRest, 0, 10)}, now.Add(2*time.Second)) {
		t.Fatal("nothing moved, but changed")
	}
	if !b.record(nil, now.Add(3*time.Second)) {
		t.Fatal("connections closed, but unchanged")
	}
	rows, _ = b.rows()
	if rows[0].Total != 2 || rows[0].Active != 0 || !rows[0].Last.Equal(now.Add(2*time.Second)) {
		t.Fatalf("after close: %+v", rows[0])
	}
	restarted := flow("1", "google.com", viaNode, ruleRest, 5, 5)
	restarted.ShoesGeneration = 2
	b.record([]flowConn{restarted}, now.Add(4*time.Second))
	if rows, _ = b.rows(); rows[0].Total != 3 {
		t.Fatalf("after restart: %+v", rows[0])
	}
}

// 同一个域名换了走法（比如切到全局）另起一行；最近有连接的排在上面
func TestFlowBookSplitsByRoute(t *testing.T) {
	var b flowBook
	now := time.Now()
	b.record([]flowConn{flow("1", "baidu.com", viaDirect, ruleSiteCN, 1, 1)}, now)
	b.record([]flowConn{flow("2", "baidu.com", []string{"gc-jp", groupSelect, groupDomestic}, ruleSiteCN, 1, 1)}, now.Add(time.Second))
	rows, _ := b.rows()
	if len(rows) != 2 || rows[0].Direct || rows[0].Via != "gc-jp" || !rows[1].Direct {
		t.Fatalf("%+v", rows)
	}
}

// 清空以后还开着的连接再记一条，流量从清空时算起，不把之前的再加一遍
func TestFlowBookClear(t *testing.T) {
	var b flowBook
	now := time.Now()
	b.record([]flowConn{flow("1", "google.com", viaNode, ruleRest, 100, 100)}, now)
	b.clear()
	if rows, _ := b.rows(); len(rows) != 0 {
		t.Fatal("not cleared")
	}
	b.record([]flowConn{flow("1", "google.com", viaNode, ruleRest, 130, 100)}, now.Add(time.Second))
	rows, _ := b.rows()
	if len(rows) != 1 || rows[0].Total != 1 || rows[0].Up != 30 || rows[0].Down != 0 {
		t.Fatalf("%+v", rows)
	}
}

// 记录有上限，超了丢最久没动静的，开着的不丢
func TestFlowBookTrims(t *testing.T) {
	var b flowBook
	now := time.Now()
	for i := 0; i < maxHostFlows+5; i++ {
		b.record([]flowConn{flow("keep", "open.example", viaNode, ruleRest, 0, 0), flow(fmt.Sprint(i), fmt.Sprintf("h%d.example", i), viaNode, ruleRest, 0, 0)}, now.Add(time.Duration(i)*time.Second))
	}
	rows, _ := b.rows()
	if len(rows) != maxHostFlows {
		t.Fatalf("%d rows", len(rows))
	}
	hosts := map[string]bool{}
	for _, r := range rows {
		hosts[r.Host] = true
	}
	if !hosts["open.example"] || hosts["h0.example"] || hosts["h5.example"] || !hosts[fmt.Sprintf("h%d.example", maxHostFlows+4)] {
		t.Fatal("trimmed the wrong rows")
	}
}

// 日志页先看到连接记录：每个域名标着代理还是直连，能只看直连；节点写的是用户起的名字，不是内核里加了「 2」的
func TestFlowsShowWhichWayEachDomainWent(t *testing.T) {
	a := shellTestApp(t)
	a.tab = "日志"
	a.settings.Nodes[0].Name, a.settings.Nodes[1].Name = "香港", "香港"
	names := kernelNames(a.settings.Nodes)
	a.flows.record([]flowConn{
		flow("1", "www.google.com", []string{names[1], groupSelect, groupOther}, ruleRest, 1, 1),
		flow("2", "www.baidu.com", viaDirect, ruleSiteCN, 1, 1),
	}, time.Now())
	tester := ui.NewTester(a.view, 1100, 700)
	tester.Frame()
	for _, text := range []string{"www.google.com", "www.baidu.com", "国内域名 · 端口 443", "香港"} {
		if !tester.HasText(text) {
			t.Fatalf("no %q in %v", text, tester.Texts())
		}
	}
	if names[1] == "香港" || tester.HasText(names[1]) {
		t.Fatalf("kernel name %q shown", names[1])
	}
	if err := tester.Click("直连"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Frame()
	if tester.HasText("www.google.com") || !tester.HasText("www.baidu.com") {
		t.Fatal("direct filter")
	}
	if err := tester.RightClick("www.baidu.com"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.ChooseMenuItem("只看这个域名"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if a.flowSearch != "www.baidu.com" || a.flowFilter != 0 {
		t.Fatalf("search %q filter %d", a.flowSearch, a.flowFilter)
	}
	tester.Key(ui.Cmd, ui.KeyF)
	tester.Frame()
	tester.Frame()
	if a.tab != "日志" || !tester.Focused("搜索域名") {
		t.Fatal("⌘F on the log page should search it, not jump to the proxies")
	}
}
