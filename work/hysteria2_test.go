package main

import (
	"encoding/json"
	"github.com/egoist/mygo/ui"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHY2Live(t *testing.T) {
	link, kernel := os.Getenv("SHOES_HY2_TEST_URI"), os.Getenv("SHOES_HY2_TEST_KERNEL")
	if link == "" || kernel == "" {
		t.Skip("optional native HY2 integration")
	}
	node, err := parseLink(link)
	if err != nil {
		t.Fatal("live HY2 link rejected")
	}
	delay := -1
	if err := realPings(kernel, t.TempDir(), []Node{node}, func(_ int, ms int) { delay = ms }); err != nil {
		t.Fatal(err)
	}
	if delay < 0 {
		t.Fatal("GUI native true-connection test failed")
	}
	t.Logf("Native GUI true-connection latency: %d ms", delay)
	engine := testEngine(t)
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	settings := defaultSettings()
	settings.Kernel, settings.Port, settings.Mode = kernel, strconv.Itoa(port), "global"
	settings.Nodes, settings.Selected = []Node{node}, 0
	if err := engine.launch(settings); err != nil {
		t.Fatal(err)
	}
	if realPing(port) < 0 {
		t.Fatal("Full GUI routing configuration did not connect")
	}
	engine.stop()
	files, _ := filepath.Glob(filepath.Join(engine.dir, "runtime-*"))
	if len(files) > 0 {
		t.Fatal("Runtime files were not removed after stop")
	}
}

func TestHY2EditorAndTCPingSkip(t *testing.T) {
	node, err := parseLink("hy2://test-only@example.com#HY2")
	if err != nil {
		t.Fatal(err)
	}
	a := &application{draft: node, settings: Settings{Nodes: []Node{node}}}
	tester := ui.NewTester(a.transportFields, 820, 600)
	tester.Frame()
	for _, label := range []string{"SNI", "证书 SHA256 指纹", "下载带宽 Mbps"} {
		if _, ok := tester.Find(label); !ok {
			t.Fatalf("missing HY2 editor field: %s", label)
		}
	}
	a.hy2SpeedPick.v = 50
	tester.Frame()
	if a.draft.HY2DownloadMbps != 50 {
		t.Fatal("bandwidth edit lost across frames")
	}
	a.testNodes([]int{0}, true)
	if a.testing || !strings.Contains(a.notice, "跳过 TCPing") {
		t.Fatal("HY2 TCPing was not skipped")
	}
}

func TestHY2ImportConfigAndShare(t *testing.T) {
	pin := strings.Repeat("ab", 32)
	for _, scheme := range []string{"hy2", "hysteria2"} {
		node, err := parseLink(scheme + "://test%3Apassword@127.0.0.1:443/?sni=test.example&insecure=1&pinSHA256=" + pin + "#HY2-test")
		if err != nil {
			t.Fatal(err)
		}
		if node.Kind != "Hysteria2" || node.Password != "test:password" || node.HY2DownloadMbps != 100 {
			t.Fatal("HY2 import lost fields")
		}
		cfg := clientConfig(node)
		if cfg["transport"] != "quic" {
			t.Fatal("HY2 must use QUIC")
		}
		tls := cfg["quic_settings"].(map[string]any)
		pins := tls["server_fingerprints"].([]string)
		if len(pins) != 1 || pins[0] != pin {
			t.Fatal("certificate pin lost")
		}
		protocol := cfg["protocol"].(map[string]any)
		if protocol["type"] != "hysteria2" || protocol["download_mbps"] != uint32(100) {
			t.Fatal("wrong protocol")
		}
		link, err := shareLink(node)
		if err != nil || !strings.Contains(link, "pinSHA256=") {
			t.Fatalf("share round trip failed: %v", err)
		}
		var ping []map[string]any
		if err := json.Unmarshal(pingConfig([]Node{node}, []int{18973}), &ping); err != nil {
			t.Fatal(err)
		}
		if _, ok := ping[0]["global_limits"]; !ok {
			t.Fatal("HY2 QUIC windows missing")
		}
		rules := ping[len(ping)-1]["rules"].([]any)
		hop := rules[0].(map[string]any)["client_chain"].(map[string]any)
		if hop["transport"] != "quic" {
			t.Fatal("speed test omitted QUIC")
		}
	}
}

func TestHY2IdentityTracksAuthenticationAndPin(t *testing.T) {
	node, err := parseLink("hy2://test-only@example.com")
	if err != nil {
		t.Fatal(err)
	}
	key := nodeIdentity(node)
	changed := node
	changed.Password = "changed"
	if nodeIdentity(changed) == key {
		t.Fatal("HY2 credentials missing from identity")
	}
	changed = node
	changed.CertPin = strings.Repeat("ab", 32)
	if nodeIdentity(changed) == key {
		t.Fatal("HY2 pin missing from identity")
	}
	changed = node
	changed.Name = "renamed"
	if nodeIdentity(changed) != key {
		t.Fatal("Node rename changed HY2 identity")
	}
	if len(withHY2Limits(nil, []Node{sampleNode("Trojan")})) != 0 {
		t.Fatal("HY2 limits applied to existing protocols")
	}
}

func TestHY2RejectsUnsupportedOrUnsafeOptions(t *testing.T) {
	for _, link := range []string{
		"hy2://secret@example.com?insecure=1",
		"hy2://secret@example.com?pinSHA256=bad",
		"hy2://secret@example.com?obfs=salamander",
		"hy2://secret@example.com?mport=443,8443",
		"hy2://secret@example.com?alpn=h2",
		"hy2://secret@example.com?sni=a&sni=b",
		"hy2://secret@example.com?downmbps=10001",
		"hy2://secret@example.com?insecure=maybe",
		"hy2://secret%0Ainjected@example.com",
	} {
		if _, err := parseLink(link); err == nil {
			t.Errorf("accepted unsupported or unsafe HY2 URL: %s", link)
		}
	}
	node, err := parseLink("hy2://secret@example.com")
	if err != nil {
		t.Fatal(err)
	}
	tls := clientConfig(node)["quic_settings"].(map[string]any)
	if tls["verify"] != true {
		t.Fatal("default certificate verification disabled")
	}
}
