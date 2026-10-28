package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestShareRoundTrip(t *testing.T) {
	for _, kind := range []string{"SOCKS5", "HTTP", "Shadowsocks", "Trojan", "AnyTLS", "VLESS", "VMess"} {
		for _, host := range []string{"example.com", "2001:db8::1"} {
			t.Run(kind+host, func(t *testing.T) {
				node := sampleNode(kind)
				if kind == "VMess" {
					node.Cipher = "auto"
				}
				node.Name, node.Host, node.Password, node.Username = "香港 #1 / 测试", host, "p@ss:/?#空 格+%", "user@空 格"
				node.Group, node.FromSub = "组", true
				node.TLS = kind == "Trojan" || kind == "AnyTLS" || kind == "HTTP"
				link, err := shareLink(node)
				if err != nil {
					t.Fatal(err)
				}
				parsed, err := parseLink(link)
				expected, _ := finishNode(node)
				if err != nil || !reflect.DeepEqual(shareComparable(expected), shareComparable(parsed)) {
					t.Fatal("connection parameters changed")
				}
				if strings.Contains(link, "\n") {
					t.Fatal("multiline URI")
				}
			})
		}
	}
}

func TestShareAdvancedParameters(t *testing.T) {
	for _, network := range []string{"tcp", "ws", "xhttp"} {
		node := sampleNode("VLESS")
		node.Network, node.Path, node.HTTPHost = network, "/路径?q=1&next=%20", "cdn.example.com"
		node.Reality, node.PublicKey, node.ShortID, node.Fingerprint, node.SNI = true, "test-public-key", "abcd", "chrome", "example.com"
		if network == "xhttp" {
			node.Mode = "auto"
			node.XHTTPExtra = json.RawMessage(`{"scMaxEachPostBytes":1000000}`)
		}
		link, err := shareLink(node)
		if err != nil {
			t.Fatal(network, err)
		}
		parsed, err := parseLink(link)
		expected, _ := finishNode(node)
		if err != nil || !reflect.DeepEqual(shareComparable(expected), shareComparable(parsed)) {
			t.Fatal("advanced parameters changed")
		}
	}
	node := sampleNode("VMess")
	node.Cipher = "auto"
	node.Network, node.Path, node.HTTPHost, node.TLS, node.SNI, node.ALPN, node.Fingerprint, node.SkipVerify = "ws", "/ws", "example.com", true, "example.com", "h2,http/1.1", "chrome", true
	link, err := shareLink(node)
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(link, "vmess://"))
	var fields map[string]string
	if err != nil || json.Unmarshal(data, &fields) != nil || fields["v"] != "2" || fields["aid"] != "0" {
		t.Fatal("nonstandard VMess payload")
	}
	node = sampleNode("HTTP")
	node.TLS, node.SNI = true, "different.example.com"
	if _, err := shareLink(node); err == nil {
		t.Fatal("silently lost HTTP SNI")
	}
	if text, err := shareNodes([]Node{sampleNode("SOCKS5"), node}); err == nil || text != "" {
		t.Fatal("partial batch must not be exported")
	}
}

func TestShareBatchAndQR(t *testing.T) {
	if _, err := shareNodes(nil); err == nil {
		t.Fatal("empty export accepted")
	}
	nodes := []Node{sampleNode("SOCKS5"), sampleNode("VLESS")}
	text, err := shareNodes(nodes)
	if err != nil {
		t.Fatal(err)
	}
	result := parseImport(text)
	if len(result.Nodes) != 2 || len(result.Issues) != 0 {
		t.Fatal("invalid batch export")
	}
	data, err := shareQR(strings.Split(text, "\n")[0])
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil || image.Bounds().Dx() < 200 {
		t.Fatal("invalid QR PNG")
	}
	if _, err := shareQR(strings.Repeat("x", 10000)); err == nil {
		t.Fatal("oversize QR accepted")
	}
}

func TestShareUI(t *testing.T) {
	oldClipboard := writeClipboard
	defer func() { writeClipboard = oldClipboard }()
	var copied string
	writeClipboard = func(text string) { copied = text }
	node := sampleNode("Trojan")
	node.TLS, node.Group = true, "旅行"
	dir := t.TempDir()
	a := &application{settings: defaultSettings(), engine: newEngine(dir), tab: "代理", storageOK: true, settingsPath: filepath.Join(dir, "s.dpapi"), nodeGroup: "旅行", delays: map[string]int{}}
	a.settings.Nodes = []Node{node, sampleNode("VLESS")}
	a.settings.Subscriptions = map[string]string{"旅行": "https://example.com/sub?token=fixture"}
	tester := ui.NewTester(a.view, 880, 900) // 返回按钮在标题栏里，要整个窗口
	tester.Frame()
	if err := tester.RightClick(node.Name); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.ChooseMenuItem("分享节点"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Frame()
	if a.share == nil || a.share.bitmap == nil || a.share.subscription {
		t.Fatal("node share missing")
	}
	if err := tester.Click("复制链接"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if copied == "" || copied != a.share.text {
		t.Fatal("copy failed")
	}
	if err := tester.Click("返回节点"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Frame()
	if err := tester.Click("分享订阅"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Frame()
	if a.share == nil || !a.share.subscription {
		t.Fatal("subscription share missing")
	}
	if err := tester.Click("复制订阅地址"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if copied != a.settings.Subscriptions["旅行"] {
		t.Fatal("subscription copy failed")
	}
	a.share = nil
	a.nodeSearch = "not-found"
	tester.Frame()
	tester.Frame()
	if err := tester.Click("分组操作"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.ChooseMenuItem("导出当前分组"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Frame()
	if a.share == nil || a.share.count != 1 {
		t.Fatal("group export should ignore search")
	}
	a.nodeGroup = "全部"
	a.shareCurrentGroup()
	if a.share.count != 2 || a.share.bitmap != nil {
		t.Fatal("all groups export")
	}
	a.nodeGroup = "未分组"
	a.shareCurrentGroup()
	if a.share.count != 1 {
		t.Fatal("ungrouped export")
	}
	a.openShare(nil, "https://example.com/"+strings.Repeat("x", 10000), "long subscription")
	if a.share.bitmap != nil || a.share.warning == "" || a.share.text == "" {
		t.Fatal("QR fallback lost text")
	}
}

func TestShareSIP002(t *testing.T) {
	node := Node{Name: "Example", Kind: "Shadowsocks", Host: "example.com", Port: "8388", Cipher: "aes-256-gcm", Password: "password"}
	link, err := shareLink(node)
	if err != nil || link != "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@example.com:8388#Example" {
		t.Fatal("SIP002 encoding mismatch")
	}
	node.Cipher, node.Password = "2022-blake3-aes-128-gcm", "AAAAAAAAAAAAAAAAAAAAAA=="
	link, err = shareLink(node)
	if err != nil || !strings.HasPrefix(link, "ss://2022-blake3-aes-128-gcm:") {
		t.Fatal("SS2022 requires plain URI userinfo")
	}
}

func TestSharePage(t *testing.T) {
	dir := t.TempDir()
	a := &application{settings: defaultSettings(), engine: newEngine(dir), tab: "代理", shownTab: "代理", nodeGroup: "全部", delays: map[string]int{}, storageOK: true, settingsPath: filepath.Join(dir, "settings.dpapi")}
	node := sampleNode("VLESS")
	node.Name, node.Host, node.TLS, node.SNI = "东京 · 示例节点", "example.com", true, "example.com"
	a.settings.Nodes = []Node{node}
	a.openShare([]Node{node}, "", node.Name)
	if a.share == nil {
		t.Fatal(a.errorText)
	}
	tester := ui.NewTester(a.view, 820, 620)
	tester.Frame()
	tester.Frame()
	if _, found := tester.Find("分享与导出"); !found {
		t.Fatal("share title missing")
	}
	if output := os.Getenv("SHOES_SHARE_SHOTS"); output != "" {
		if err := os.MkdirAll(output, 0700); err != nil {
			t.Fatal(err)
		}
		var image bytes.Buffer
		if err := png.Encode(&image, tester.Image()); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{"share.png": image.Bytes(), "qr.png": a.share.png, "expected.txt": []byte(a.share.text)} {
			if err := os.WriteFile(filepath.Join(output, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	tester.Key(ui.Cmd, ui.KeyF)
	tester.Frame()
	tester.Frame()
	if a.share != nil {
		t.Fatal("search shortcut did not leave share")
	}
	a.openShare([]Node{node}, "", node.Name)
	tester.Frame()
	tester.Frame()
	tester.Key(ui.Cmd, ui.KeyN)
	tester.Frame()
	tester.Frame()
	if a.share != nil || !a.editing {
		t.Fatal("add shortcut did not leave share")
	}
}

func TestShareSave(t *testing.T) {
	original := saveShareDialog
	defer func() { saveShareDialog = original }()
	dir := t.TempDir()
	for _, extension := range []string{"txt", "png"} {
		path := filepath.Join(dir, "share."+extension)
		saveShareDialog = func(_ *mygo.Window, ext string) (string, error) {
			if ext != extension {
				return "", os.ErrInvalid
			}
			return path, nil
		}
		a := &application{}
		data := []byte("fixture\n")
		if extension == "png" {
			var err error
			data, err = shareQR("https://example.com")
			if err != nil {
				t.Fatal(err)
			}
		}
		a.saveShare(data, extension)
		deadline := time.Now().Add(5 * time.Second)
		for a.busy.Load() && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if a.busy.Load() {
			t.Fatal("save timeout")
		}
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, data) {
			t.Fatal("saved content differs")
		}
	}
	saveShareDialog = func(_ *mygo.Window, _ string) (string, error) { return "", nil }
	a := &application{}
	a.saveShare([]byte("cancel"), "txt")
	for a.busy.Load() {
		time.Sleep(time.Millisecond)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatal("cancel wrote a file")
	}
}
