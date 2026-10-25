package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const importUUID = "550e8400-e29b-41d4-a716-446655440000"

func TestImportBatchAndCompletion(t *testing.T) {
	first := parseImport("socks5://one.example:1080").Nodes
	second := parseImport("socks5://two.example:1080").Nodes
	initial := defaultSettings()
	next, added := importBatch(initial, first, "收藏")
	if added != 1 || next.Nodes[0].Group != "收藏" {
		t.Fatal("wrong target")
	}
	next, added = importBatch(next, second, "收藏")
	if added != 1 || len(next.Groups) != 1 || next.Nodes[1].Group != "收藏" {
		t.Fatal("import must reuse current group")
	}
	next, added = importBatch(next, first, "不应创建")
	if added != 0 || len(next.Groups) != 1 {
		t.Fatal("duplicate created empty group")
	}
	ungrouped, _ := importBatch(initial, first, "")
	if ungrouped.Nodes[0].Group != "" || len(ungrouped.Groups) != 0 {
		t.Fatal("ungrouped import created group")
	}
	dir := t.TempDir()
	a := &application{settings: initial, storageOK: true, settingsPath: filepath.Join(dir, "s.dpapi"), engine: newEngine(dir), importing: true, importText: "source", importParsed: "source", importResult: importResult{Nodes: first}, nodeSearch: "old search", nodeGroup: "全部", importTarget: "收藏"}
	a.finishImport()
	if a.importing || a.importText != "" || len(a.importResult.Nodes) != 0 || a.nodeSearch != "old search" || a.nodeGroup != "全部" || a.settings.Nodes[0].Group != "收藏" {
		t.Fatal("successful import did not close and select its group")
	}
	var saved Settings
	if err := loadEncrypted(a.settingsPath, &saved); err != nil || len(saved.Groups) != 1 || len(saved.Nodes) != 1 {
		t.Fatal("import not persisted", err)
	}
	a.importing, a.importText, a.importParsed = true, "source2", "source2"
	a.importResult = importResult{Nodes: second}
	a.importNew = true
	for _, name := range []string{"", "全部", "未分组", "收藏"} {
		a.importNewName = name
		a.finishImport()
		if !a.importing || len(a.settings.Nodes) != 1 {
			t.Fatal("invalid new group accepted", name)
		}
	}
	a.importNew = false
	a.storageOK = false
	a.finishImport()
	if !a.importing || len(a.settings.Nodes) != 1 || a.errorText == "" {
		t.Fatal("failed save closed import or lost state")
	}
}

func TestImportAutoRecognition(t *testing.T) {
	now := time.Now()
	a := &application{importing: true, importText: "socks5://example.com:1080"}
	if a.importReady(now) || a.importReady(now.Add(599*time.Millisecond)) {
		t.Fatal("parsed before debounce")
	}
	if !a.importReady(now.Add(time.Second)) {
		t.Fatal("paste did not trigger recognition")
	}
	a.parsing = true
	if a.importReady(now.Add(time.Second)) {
		t.Fatal("duplicate request while busy")
	}
	a.parsing = false
	a.importParsed = a.importText
	a.importResult = parseImport(a.importText)
	if a.importReady(now.Add(2 * time.Second)) {
		t.Fatal("parsed content requested again")
	}
	a.importText = "socks5://other.example:1080"
	if a.importReady(now.Add(3*time.Second)) || a.importParsed != "" || len(a.importResult.Nodes) != 0 {
		t.Fatal("editing must invalidate preview and restart debounce")
	}
	if !a.importReady(now.Add(4 * time.Second)) {
		t.Fatal("edited content not recognized")
	}
	a.importText = " \n "
	a.importReady(now.Add(5 * time.Second))
	if a.importReady(now.Add(6 * time.Second)) {
		t.Fatal("blank content parsed")
	}
	a.importText = "socks5://example.com:1080"
	a.importing = false
	if a.importReady(now.Add(7 * time.Second)) {
		t.Fatal("closed import page requested data")
	}
}

func TestShareLinks(t *testing.T) {
	encode := func(value string) string { return base64.RawURLEncoding.EncodeToString([]byte(value)) }
	vmess, _ := json.Marshal(map[string]any{"v": "2", "ps": "VMess 测试", "add": "example.com", "port": 443, "id": importUUID, "aid": 0, "net": "ws", "type": "none", "tls": "tls", "path": "/ws", "host": "cdn.example.com", "scy": "auto"})
	tests := []struct{ name, link, kind string }{
		{"reality", "vless://" + importUUID + "@example.com:443?security=reality&type=tcp&pbk=" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)) + "&sid=abcd&sni=example.org&flow=xtls-rprx-vision#Reality", "VLESS"},
		{"xhttp", "vless://" + importUUID + "@example.com:443?security=tls&type=xhttp&path=%2Ftunnel&mode=stream-up&extra=" + url.QueryEscape(`{"noGRPCHeader":false,"xPaddingBytes":"100-1000"}`), "VLESS"},
		{"vmess", "vmess://" + base64.StdEncoding.EncodeToString(vmess), "VMess"},
		{"ss-sip002", "ss://" + encode("aes-256-gcm:test-only") + "@[::1]:443#%E6%B5%8B%E8%AF%95", "Shadowsocks"},
		{"ss-legacy", "ss://" + encode("aes-128-gcm:test-only@example.com:443") + "#legacy", "Shadowsocks"},
		{"ss-plain", "ss://aes-256-gcm:test%3Aonly@example.com:443", "Shadowsocks"},
		{"trojan", "trojan://test%3Aonly@example.com:443?sni=server.example.com", "Trojan"},
		{"anytls", "anytls://test-only@example.com:443?insecure=0", "AnyTLS"},
		{"socks", "socks5://user:test-only@[::1]:1080", "SOCKS5"},
		{"socks-base64", "socks://" + encode("user:test-only") + "@example.com:1080", "SOCKS5"},
		{"socks-legacy", "socks://" + encode("user:test-only@example.com:1080"), "SOCKS5"},
		{"http", "http://user:test-only@example.com:8080", "HTTP"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node, err := parseLink(test.link)
			if err != nil {
				t.Fatal(err)
			}
			if node.Kind != test.kind {
				t.Fatal("wrong protocol")
			}
			if _, err := generateConfig(Settings{Nodes: []Node{node}, Port: "10808"}, testRuntime); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestImportBatchAndDedupe(t *testing.T) {
	link := "trojan://test-only@example.com:443#Test"
	result := parseImport("\ufeff" + link + "\r\ninvalid\n" + link)
	if len(result.Nodes) != 2 || len(result.Issues) != 1 {
		t.Fatal("partial batch not preserved")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(link + "\n" + link))
	decoded := parseImport(encoded)
	if len(decoded.Nodes) != 2 || len(decoded.Issues) != 0 {
		t.Fatal("base64 list failed")
	}
	next, added := mergeNodes(defaultSettings(), decoded.Nodes)
	if added != 1 || len(next.Nodes) != 1 || next.Selected != 0 {
		t.Fatal("batch dedupe failed")
	}
	next.Nodes[0].Group = "existing"
	next.Nodes[0].Name = "renamed"
	again, added := mergeNodes(next, decoded.Nodes)
	if added != 0 || len(again.Nodes) != 1 || again.Nodes[0].Group != "existing" {
		t.Fatal("cross-group duplicate was added")
	}
}

func TestUnsupportedImportIsReported(t *testing.T) {
	for _, link := range []string{
		"vless://" + importUUID + "@example.com:443?type=grpc",
		"vless://" + importUUID + "@example.com:443?type=ws&path=%2F%3Fed%3D2048",
		"vless://" + importUUID + "@example.com:443?security=reality",
		"vless://" + importUUID + "@example.com:443?type=xhttp&extra=" + url.QueryEscape(`{"downloadSettings":{}}`),
		"hysteria2://test-only@example.com:443",
		"tuic://" + importUUID + ":test-only@example.com:443",
		"ss://YWVzLTEyOC1nY206dGVzdA@example.com:443?plugin=obfs-local",
		"trojan://example.com:443",
	} {
		if _, err := parseLink(link); err == nil {
			t.Fatal("unsupported input accepted")
		}
	}
	result := parseImport("unknown://private-value@example.com:443")
	if len(result.Issues) != 1 || strings.Contains(result.Issues[0], "private-value") {
		t.Fatal("error exposed link content")
	}
}

func TestSubscriptionImport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, base64.StdEncoding.EncodeToString([]byte("anytls://test-only@example.com:443#FromSubscription")))
	}))
	defer server.Close()
	result := loadImport(server.URL + "/subscription?token=test-only")
	if len(result.Nodes) != 1 || len(result.Issues) != 0 || result.Nodes[0].Kind != "AnyTLS" {
		t.Fatal("subscription import failed")
	}
}

func TestTransportConfig(t *testing.T) {
	node := sampleNode("VLESS")
	node.TLS = true
	node.Network = "xhttp"
	node.Path = "/tunnel"
	node.Mode = "stream-up"
	node.XHTTPExtra = json.RawMessage(`{"noGRPCHeader":true,"xPaddingBytes":"100-1000"}`)
	config := protocol(node)
	inner := config["protocol"].(map[string]any)
	if config["type"] != "tls" || inner["type"] != "xhttp" || inner["mode"] != "stream-up" || inner["no_grpc_header"] != true {
		t.Fatal("transport nesting incorrect")
	}
	node.Network = "tcp"
	node.Flow = "xtls-rprx-vision"
	node.Reality = true
	node.PublicKey = "test"
	node.SNI = "example.com"
	config = protocol(node)
	if config["type"] != "reality" || config["vision"] != true {
		t.Fatal("Reality Vision not enabled")
	}
	node.Encryption = "test-native-encryption"
	config = protocol(node)
	if config["vision"] != false || config["protocol"].(map[string]any)["vision"] != true {
		t.Fatal("native Vision at wrong layer")
	}
}

func TestSyncGroup(t *testing.T) {
	node := func(name, group, password string) Node {
		n := sampleNode("Trojan")
		n.Name, n.Group, n.Password, n.FromSub = name, group, password, group == "机场"
		return n
	}
	s := defaultSettings()
	s.Nodes = []Node{node("手动", "", "x"), node("香港", "机场", "old"), node("日本", "机场", "old"), node("美国", "机场", "old"), node("美国", "", "manual")}
	s.Nodes[4].Group = "机场" // 手动挪进来的，同名也不能被订阅改掉或删掉
	s.Selected = 2          // 日本
	next, added, updated, removed := syncGroup(s, "机场", []Node{node("日本", "", "new"), node("香港", "", "old"), node("新加坡", "", "new")})
	if added != 1 || updated != 1 || removed != 1 {
		t.Fatalf("added %d updated %d removed %d", added, updated, removed)
	}
	names := []string{}
	for _, n := range next.Nodes {
		names = append(names, n.Name+"/"+n.Group+"/"+n.Password)
	}
	if strings.Join(names, ",") != "手动//x,香港/机场/old,日本/机场/new,美国/机场/manual,新加坡/机场/new" {
		t.Fatal(names)
	}
	if next.Nodes[next.Selected].Name != "日本" {
		t.Fatal("selection did not follow the node", next.Selected)
	}
	s.Selected = 3 // 美国，同步后没了
	next, _, _, _ = syncGroup(s, "机场", []Node{node("香港", "", "old")})
	if next.Nodes[next.Selected].Name != "香港" {
		t.Fatal("removed selection should fall back to the group", next.Selected)
	}
	if len(s.Nodes) != 5 || s.Nodes[2].Password != "old" {
		t.Fatal("input settings were modified")
	}
}

func TestSubscriptionBindsAndReimportUpdates(t *testing.T) {
	dir := t.TempDir()
	a := &application{settings: defaultSettings(), storageOK: true, settingsPath: filepath.Join(dir, "s.dpapi"), engine: newEngine(dir), nodeGroup: "全部"}
	first := parseImport("trojan://old@example.com:443#香港").Nodes
	start := func(nodes []Node) {
		a.importing, a.importText, a.importParsed = true, "https://sub.example/x", "https://sub.example/x"
		a.importResult = importResult{Nodes: nodes, Source: "https://sub.example/x"}
	}
	start(first)
	a.importNew, a.importNewName = true, "机场"
	a.finishImport()
	if a.settings.Subscriptions["机场"] != "https://sub.example/x" || len(a.settings.Nodes) != 1 {
		t.Fatalf("subscription not bound: %v", a.settings.Subscriptions)
	}
	start(parseImport("trojan://new@example.com:443#香港").Nodes)
	a.importNew, a.importTarget = false, "机场"
	a.finishImport()
	if len(a.settings.Nodes) != 1 || a.settings.Nodes[0].Password != "new" || a.importing {
		t.Fatalf("re-import should update in place: %+v", a.settings.Nodes)
	}
	a.settings.Nodes = append(a.settings.Nodes, Node{Name: "手动", Kind: "SOCKS5", Host: "h", Port: "1"})
	start(first)
	a.importTarget = "未分组"
	a.finishImport()
	if _, bound := a.settings.Subscriptions[""]; bound {
		t.Fatal("subscription bound to a group with manual nodes")
	}
}

func TestClipboardOnlyPrefillsImportableText(t *testing.T) {
	for text, want := range map[string]bool{
		"trojan://secret@example.com:443#a":                     true,
		"https://sub.example.com/api/v1/client?token=abc":       true,
		base64.StdEncoding.EncodeToString([]byte("ss://x@h:1")): true,
		"hunter2-my-password":                                   false,
		"会议纪要：明天下午三点":                                           false,
		"":                                                      false,
		base64.StdEncoding.EncodeToString([]byte("just text")):  false,
	} {
		if got := importable(text); got != want {
			t.Errorf("%q: got %v", text, got)
		}
	}
}

func TestSubscriptionSuggestsItsOwnGroup(t *testing.T) {
	a := &application{settings: defaultSettings(), importTarget: "未分组"}
	a.settings.Nodes = []Node{{Name: "手动"}}
	a.suggestTarget(importResult{Source: "https://sub.example.com/x"})
	if !a.importNew || a.importNewName != "sub.example.com" {
		t.Fatalf("new group not suggested: %v %q", a.importNew, a.importNewName)
	}
	a.importNew, a.importTarget = false, "未分组"
	a.settings.Subscriptions = map[string]string{"机场": "https://sub.example.com/x"}
	a.suggestTarget(importResult{Source: "https://sub.example.com/x"})
	if a.importNew || a.importTarget != "机场" {
		t.Fatal("known subscription should go back to its group")
	}
	a.importTarget = "空组"
	a.settings.Subscriptions = nil
	a.settings.Groups = []string{"空组"}
	a.suggestTarget(importResult{Source: "https://other.example/x"})
	if a.importNew {
		t.Fatal("an empty group can hold the subscription")
	}
}
