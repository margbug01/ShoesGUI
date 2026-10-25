package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Node struct {
	Group       string
	FromSub     bool `json:",omitempty"` // 来自分组绑定的订阅，「更新订阅」只动这些
	Network     string
	Reality     bool
	PublicKey   string
	ShortID     string
	Flow        string
	Encryption  string
	Path        string
	HTTPHost    string
	Mode        string
	ALPN        string
	SkipVerify  bool
	Fingerprint string
	MLDSAVerify string
	XHTTPExtra  json.RawMessage `json:",omitempty"`

	Name     string
	Kind     string
	Host     string
	Port     string
	Username string
	Password string
	Cipher   string
	UUID     string
	TLS      bool
	SNI      string
}

type Settings struct {
	Groups []string `json:",omitempty"`
	// Subscriptions 分组 → 订阅地址，「更新订阅」照它重新拉。未分组的键是 ""
	Subscriptions map[string]string `json:",omitempty"`
	Nodes         []Node
	Selected      int
	Kernel        string
	Port          string
	Mode          string // "" 规则，"global" 全局，"direct" 直连
	AutoConnect   bool
	Theme         string // "" 跟随系统，"light"，"dark"
	TrayHinted    bool   `json:",omitempty"` // 第一次关窗口时提示过「还在托盘里」
	// KeepConnections 切节点、切模式时不断开现有连接（默认断开，浏览器马上走新节点）
	KeepConnections bool `json:",omitempty"`
}

// 内核里的组名。节点名不能和它们重复
const (
	groupSelect   = "节点选择"
	groupDomestic = "国内"
	groupOther    = "其他"
	groupDirect   = "直连"
	directMember  = "直连出口"
)

var modes = []struct{ key, label, hint string }{
	{"", "规则", "局域网和国内直连，其余走节点"},
	{"global", "全局", "除局域网外都走节点"},
	{"direct", "直连", "全部直连，不经过节点"},
}

// modeTargets 返回「国内」「其他」两个组在该模式下指向哪里
func modeTargets(mode string) (domestic, other string) {
	switch mode {
	case "global":
		return groupSelect, groupSelect
	case "direct":
		return groupDirect, groupDirect
	}
	return groupDirect, groupSelect
}

// kernelNames 给每个节点一个内核里唯一、合法的名字（非空、≤256 字节、无控制字符、不占保留名）
func kernelNames(nodes []Node) []string {
	used := map[string]bool{}
	for _, name := range []string{groupSelect, groupDomestic, groupOther, groupDirect, directMember} {
		used[name] = true
	}
	taken := func(name string) bool {
		for _, reserved := range []string{"DIRECT", "REJECT", "GLOBAL"} {
			if strings.EqualFold(name, reserved) {
				return true
			}
		}
		return used[name]
	}
	names := make([]string, len(nodes))
	for i, node := range nodes {
		base := strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, node.Name))
		for len(base) > 240 {
			_, size := utf8.DecodeLastRuneInString(base)
			base = base[:len(base)-size]
		}
		if base == "" {
			base = "节点"
		}
		name := base
		for k := 2; taken(name); k++ {
			name = fmt.Sprintf("%s %d", base, k)
		}
		used[name] = true
		names[i] = name
	}
	return names
}

type runtimeConfig struct {
	APIPort  int
	Secret   string
	RulesDir string
}

func defaultSettings() Settings { return Settings{Selected: -1, Port: "10808"} }

func portNumber(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, errors.New("端口必须为 1–65535")
	}
	return n, nil
}

func (n Node) validate() error {
	if strings.TrimSpace(n.Name) == "" {
		return errors.New("请填写节点名称")
	}
	if n.Host == "" || strings.ContainsAny(n.Host, " /\\\r\n\t@") {
		return errors.New("服务器仅填写主机名或 IP，不含协议、路径或端口")
	}
	if strings.Contains(n.Host, ":") && net.ParseIP(n.Host) == nil {
		return errors.New("IPv6 请直接填写地址，不加方括号")
	}
	if _, err := portNumber(n.Port); err != nil {
		return err
	}
	switch n.Kind {
	case "SOCKS5", "HTTP":
		if (n.Username == "") != (n.Password == "") {
			return errors.New("用户名和密码必须同时填写或同时留空")
		}
	case "Shadowsocks":
		if n.Password == "" {
			return errors.New("请填写密码")
		}
		switch n.Cipher {
		case "aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-ietf-poly1305":
		default:
			return errors.New("不支持的 Shadowsocks 加密方式")
		}
	case "Trojan", "AnyTLS":
		if n.Password == "" {
			return errors.New("请填写密码")
		}
	case "VLESS", "VMess":
		if n.Kind == "VMess" && n.Cipher != "" && n.Cipher != "auto" && n.Cipher != "none" && n.Cipher != "aes-128-gcm" && n.Cipher != "chacha20-poly1305" {
			return errors.New("不支持的 VMess 加密方式")
		}
		compact := strings.ReplaceAll(n.UUID, "-", "")
		if len(n.UUID) != 36 || len(compact) != 32 || n.UUID[8] != '-' || n.UUID[13] != '-' || n.UUID[18] != '-' || n.UUID[23] != '-' {
			return errors.New("VLESS 需要标准 UUID")
		}
		if n.UUID[14] != '4' || !strings.ContainsRune("89abAB", rune(n.UUID[19])) {
			return errors.New("shoes 的 VLESS 需要 RFC 4122 UUID v4")
		}
		for _, ch := range compact {
			if !strings.ContainsRune("0123456789abcdefABCDEF", ch) {
				return errors.New("UUID 包含无效字符")
			}
		}
	default:
		return errors.New("不支持的协议")
	}
	if strings.ContainsAny(n.SNI, " /\\\r\n\t:") {
		return errors.New("SNI 仅填写域名")
	}
	switch n.Network {
	case "", "tcp", "ws", "xhttp":
	default:
		return errors.New("暂不支持此传输方式")
	}
	if n.Reality && (n.PublicKey == "" || n.SNI == "") {
		return errors.New("Reality 需要公钥和 SNI")
	}
	if n.Reality && n.SkipVerify {
		return errors.New("Reality 不使用跳过证书验证")
	}
	if n.Flow != "" && n.Flow != "xtls-rprx-vision" {
		return errors.New("不支持此 Flow")
	}
	if n.Flow != "" && (n.Kind != "VLESS" || (n.Network != "" && n.Network != "tcp")) {
		return errors.New("Vision 需要 VLESS TCP")
	}
	if n.Flow != "" && !n.Reality && !n.TLS && (n.Encryption == "" || n.Encryption == "none") {
		return errors.New("Vision 需要 TLS、Reality 或 VLESS 加密")
	}
	if n.Mode != "" && n.Mode != "auto" && n.Mode != "stream-one" && n.Mode != "stream-up" && n.Mode != "packet-up" {
		return errors.New("XHTTP 模式无效")
	}
	if len(n.XHTTPExtra) > 0 {
		var extra map[string]any
		if json.Unmarshal(n.XHTTPExtra, &extra) != nil {
			return errors.New("XHTTP 扩展配置无效")
		}
		if value, exists := extra["noSSEHeader"]; exists && value != false {
			return errors.New("不支持 noSSEHeader 扩展")
		}
		for key := range extra {
			switch key {
			case "xPaddingBytes", "noGRPCHeader", "noSSEHeader", "scMaxEachPostBytes", "scMinPostsIntervalMs", "headers":
			default:
				return errors.New("XHTTP 扩展选项暂不支持")
			}
		}
	}

	return nil
}

func protocol(n Node) map[string]any {
	p := map[string]any{}
	switch n.Kind {
	case "SOCKS5", "HTTP":
		p["type"] = "socks"
		if n.Kind == "HTTP" {
			p["type"] = "http"
		}
		if n.Username != "" {
			p["username"], p["password"] = n.Username, n.Password
		}
	case "Shadowsocks":
		p["type"], p["cipher"], p["password"] = "shadowsocks", n.Cipher, n.Password
	case "Trojan":
		p["type"], p["password"] = "trojan", n.Password
	case "AnyTLS":
		p["type"], p["password"] = "anytls", n.Password
	case "VMess":
		cipher := n.Cipher
		if cipher == "" || cipher == "auto" {
			cipher = "aes-128-gcm"
		}
		p["type"], p["user_id"], p["cipher"] = "vmess", n.UUID, cipher
	case "VLESS":
		p["type"], p["user_id"] = "vless", n.UUID
		if n.Encryption != "" && n.Encryption != "none" {
			p["encryption"] = n.Encryption
			p["vision"] = n.Flow == "xtls-rprx-vision"
		}
	}
	if n.Network == "ws" {
		p = map[string]any{"type": "websocket", "matching_path": n.Path, "protocol": p}
		if n.HTTPHost != "" {
			p["matching_headers"] = map[string]string{"Host": n.HTTPHost}
		}
	}
	if n.Network == "xhttp" {
		mode := n.Mode
		if mode == "" {
			mode = "auto"
		}
		p = map[string]any{"type": "xhttp", "path": n.Path, "mode": mode, "protocol": p}
		if n.HTTPHost != "" {
			p["host"] = n.HTTPHost
		}
		var extra map[string]any
		json.Unmarshal(n.XHTTPExtra, &extra)
		for source, target := range map[string]string{"xPaddingBytes": "x_padding_bytes", "noGRPCHeader": "no_grpc_header", "scMaxEachPostBytes": "sc_max_each_post_bytes", "scMinPostsIntervalMs": "sc_min_posts_interval_ms", "headers": "headers"} {
			if value, ok := extra[source]; ok {
				p[target] = value
			}
		}
	}
	sni := n.SNI
	if sni == "" {
		sni = n.Host
	}
	vision := n.Flow == "xtls-rprx-vision" && (n.Encryption == "" || n.Encryption == "none")
	if n.Reality {
		p = map[string]any{"type": "reality", "public_key": n.PublicKey, "short_id": n.ShortID, "sni_hostname": sni, "vision": vision, "protocol": p}
		if n.MLDSAVerify != "" {
			p["mldsa65_verify"] = n.MLDSAVerify
		}
	} else if n.TLS || n.Kind == "Trojan" || n.Kind == "AnyTLS" {
		p = map[string]any{"type": "tls", "verify": !n.SkipVerify, "sni_hostname": sni, "vision": vision, "protocol": p}
		if n.ALPN != "" {
			p["alpn_protocols"] = strings.Split(n.ALPN, ",")
		}
	}
	return p
}

var privateRanges = []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10", "::1/128", "fc00::/7", "fe80::/10"}

// generateConfig 把全部节点放进「节点选择」，再用「国内」「其他」两个选择组实现出站模式，切模式只需 PUT
func generateConfig(s Settings, rt runtimeConfig) ([]byte, error) {
	if len(s.Nodes) == 0 {
		return nil, errors.New("请先添加节点")
	}
	if _, err := portNumber(s.Port); err != nil {
		return nil, err
	}
	names := kernelNames(s.Nodes)
	members := make([]any, len(s.Nodes))
	for i, n := range s.Nodes {
		if err := n.validate(); err != nil {
			return nil, fmt.Errorf("节点「%s」：%v", n.Name, err)
		}
		members[i] = map[string]any{"name": names[i], "address": net.JoinHostPort(n.Host, n.Port), "protocol": protocol(n)}
	}
	selected := names[0]
	if s.Selected >= 0 && s.Selected < len(names) {
		selected = names[s.Selected]
	}
	domestic, other := modeTargets(s.Mode)
	route := func(masks any, chain string, noResolve bool) map[string]any {
		rule := map[string]any{"masks": masks, "action": "allow", "client_chain": chain}
		if noResolve {
			rule["no_resolve"] = true
		}
		return rule
	}
	config := []any{
		map[string]any{"clash_api_address": fmt.Sprintf("127.0.0.1:%d", rt.APIPort), "secret": rt.Secret},
		map[string]any{"client_group": groupDirect, "client_proxies": []any{map[string]any{"name": directMember, "protocol": map[string]any{"type": "direct"}}}},
		map[string]any{"client_group": groupSelect, "strategy": "selector", "default": selected, "client_proxies": members},
		map[string]any{"client_group": groupDomestic, "strategy": "selector", "default": domestic, "client_proxies": []string{groupDirect, groupSelect}},
		map[string]any{"client_group": groupOther, "strategy": "selector", "default": other, "client_proxies": []string{groupSelect, groupDirect}},
		map[string]any{
			"name":     "mixed-in",
			"address":  net.JoinHostPort("127.0.0.1", s.Port),
			"protocol": map[string]any{"type": "mixed", "udp_enabled": false},
			"rules": []any{
				route(privateRanges, groupDirect, true),
				route(map[string]any{"path": filepath.Join(rt.RulesDir, "geosite-cn.mrs"), "behavior": "domain"}, groupDomestic, false),
				route(map[string]any{"path": filepath.Join(rt.RulesDir, "geoip-cn.mrs"), "behavior": "ipcidr"}, groupDomestic, true),
				route("0.0.0.0/0", groupOther, false),
			},
		},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("无法生成配置")
	}
	return data, nil
}
