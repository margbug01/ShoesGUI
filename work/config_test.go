package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func sampleNode(kind string) Node {
	return Node{Name: "test", Kind: kind, Host: "127.0.0.1", Port: "18443", Password: "local-test-only", Username: "test", Cipher: "aes-256-gcm", UUID: "ca761232-ed42-41ce-bacd-00aa0057b223"}
}

var testRuntime = runtimeConfig{APIPort: 19090, Secret: "secret", RulesDir: "rules"}

func TestGenerateConfig(t *testing.T) {
	var nodes []Node
	for _, kind := range []string{"SOCKS5", "HTTP", "Shadowsocks", "Trojan", "VLESS"} {
		nodes = append(nodes, sampleNode(kind))
	}
	for _, mode := range []string{"", "global", "direct"} {
		data, err := generateConfig(Settings{Nodes: nodes, Selected: 2, Port: "10808", Mode: mode}, testRuntime)
		if err != nil {
			t.Fatal(err)
		}
		var entries []map[string]any
		if err := json.Unmarshal(data, &entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != 6 || entries[0]["clash_api_address"] != "127.0.0.1:19090" || entries[5]["address"] != "127.0.0.1:10808" {
			t.Fatal("unsafe listener")
		}
		if protocol := entries[5]["protocol"].(map[string]any); protocol["type"] != "mixed" || protocol["udp_enabled"] != false {
			t.Fatal("unexpected mixed configuration")
		}
		selector := entries[2]
		if selector["client_group"] != groupSelect || selector["default"] != "test 3" || len(selector["client_proxies"].([]any)) != len(nodes) {
			t.Fatal("selector does not hold every node with unique names")
		}
		domestic, other := modeTargets(mode)
		if entries[3]["default"] != domestic || entries[4]["default"] != other {
			t.Fatal("mode groups start at the wrong target")
		}
		rules := entries[5]["rules"].([]any)
		if first := rules[0].(map[string]any); first["client_chain"] != groupDirect || first["no_resolve"] != true {
			t.Fatal("private ranges must go direct without DNS")
		}
		if last := rules[len(rules)-1].(map[string]any); last["masks"] != "0.0.0.0/0" || last["client_chain"] != groupOther {
			t.Fatal("missing final rule")
		}
		if !bytes.Contains(data, []byte(`"verify": true`)) {
			t.Fatal("Trojan requires verified TLS")
		}
	}
	if _, err := generateConfig(Settings{Port: "10808"}, testRuntime); err == nil {
		t.Fatal("empty node list accepted")
	}
	bad := sampleNode("HTTP")
	bad.Host = ""
	if _, err := generateConfig(Settings{Nodes: []Node{nodes[0], bad}, Port: "10808"}, testRuntime); err == nil {
		t.Fatal("invalid node accepted")
	}
}

func TestKernelNames(t *testing.T) {
	nodes := []Node{{Name: "香港"}, {Name: "香港"}, {Name: ""}, {Name: "direct"}, {Name: groupSelect}, {Name: "a\tb\x00"}, {Name: strings.Repeat("长", 200)}}
	names := kernelNames(nodes)
	want := []string{"香港", "香港 2", "节点", "direct 2", groupSelect + " 2", "ab"}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("name %d: got %q, want %q", i, names[i], name)
		}
	}
	if long := names[len(names)-1]; len(long) > 256 || !utf8.ValidString(long) {
		t.Fatal("long name not truncated on a rune boundary")
	}
}

func TestTidyKernelLine(t *testing.T) {
	if got := tidyKernelLine("[2026-10-25T12:59:29.292 ERROR shoes::tcp::tcp_server] 127.0.0.1:52833 failed"); got != "ERROR  127.0.0.1:52833 failed" {
		t.Fatal(got)
	}
	if got := tidyKernelLine("Servers ready"); got != "Servers ready" {
		t.Fatal(got)
	}
}

func TestValidation(t *testing.T) {
	for _, bad := range []string{"0", "65536", "-1", "10808\n- address: 0.0.0.0:80", "", "1.0"} {
		if _, err := generateConfig(Settings{Nodes: []Node{sampleNode("SOCKS5")}, Port: bad}, testRuntime); err == nil {
			t.Fatalf("accepted invalid port")
		}
	}
	for _, host := range []string{"https://example.com", "host:443", "[::1]", "host\nother", "host/path", ""} {
		node := sampleNode("HTTP")
		node.Host = host
		if node.validate() == nil {
			t.Fatalf("accepted invalid host")
		}
	}
	for _, kind := range []string{"Hysteria2", "Reality", ""} {
		if sampleNode(kind).validate() == nil {
			t.Fatal("unsupported protocol accepted")
		}
	}
	node := sampleNode("VLESS")
	node.UUID = "not-uuid"
	if node.validate() == nil {
		t.Fatal("invalid UUID accepted")
	}
	node = sampleNode("Shadowsocks")
	node.Cipher = "rc4-md5"
	if node.validate() == nil {
		t.Fatal("legacy cipher accepted")
	}
	node = sampleNode("SOCKS5")
	node.Host = "::1"
	if node.validate() != nil {
		t.Fatal("IPv6 rejected")
	}
	node.Password = ""
	if node.validate() == nil {
		t.Fatal("partial credentials accepted")
	}
}

func TestConfigEscapesCredentials(t *testing.T) {
	node := sampleNode("HTTP")
	node.Password = "quote\"\n- address: 0.0.0.0:80"
	data, err := generateConfig(Settings{Nodes: []Node{node}, Port: "10808"}, testRuntime)
	if err != nil {
		t.Fatal(err)
	}
	var entries []any
	if json.Unmarshal(data, &entries) != nil || len(entries) != 6 {
		t.Fatal("configuration injection")
	}
}

func TestEncryptedPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.dpapi")
	want := Settings{Nodes: []Node{sampleNode("Trojan")}, Selected: 0, Port: "10808"}
	if err := saveEncrypted(path, want); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(want.Nodes[0].Password)) {
		t.Fatal("plaintext secret on disk")
	}
	var got Settings
	if err := loadEncrypted(path, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatal("DPAPI roundtrip mismatch")
	}
	want.Port = "10809"
	if err := saveEncrypted(path, want); err != nil {
		t.Fatal(err)
	}
	if err := loadEncrypted(path, &got); err != nil || got.Port != want.Port {
		t.Fatal("atomic replacement failed")
	}
	if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if loadEncrypted(path, &got) == nil {
		t.Fatal("damaged DPAPI accepted")
	}
}

func TestRestorePlanPreservesExternalChanges(t *testing.T) {
	before := desiredProxy("9000")
	before["AutoConfigURL"] = stringReg("http://old.invalid/proxy.pac")
	owned := desiredProxy("10808")
	current := desiredProxy("10808")
	current["ProxyServer"] = stringReg("external.invalid:8000")
	plan := restoreValues(current, proxyJournal{Before: before, Owned: owned})
	if !reflect.DeepEqual(plan["ProxyServer"], current["ProxyServer"]) {
		t.Fatal("external proxy overwritten")
	}
	for _, name := range []string{"ProxyEnable", "AutoConfigURL", "ProxyOverride"} {
		if !reflect.DeepEqual(plan[name], before[name]) {
			t.Fatal("previous value not restored")
		}
	}
}

func TestNoProxyWritesWithoutJournal(t *testing.T) {
	p := proxyManager{path: filepath.Join(t.TempDir(), "missing.dpapi")}
	if err := p.restore(); err != nil || p.active {
		t.Fatal("missing journal should do nothing")
	}
	if err := os.WriteFile(p.path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if p.restore() == nil || !p.active {
		t.Fatal("corrupt journal must require recovery")
	}
}

func TestSaveFailureKeepsNodes(t *testing.T) {
	a := &application{storageOK: true, settings: defaultSettings(), settingsPath: filepath.Join(t.TempDir(), "missing", "settings")}
	next := a.settings
	next.Nodes = []Node{sampleNode("HTTP")}
	if a.save(next) || len(a.settings.Nodes) != 0 {
		t.Fatal("failed save altered committed state")
	}
}
