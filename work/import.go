package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type importResult struct {
	Nodes  []Node
	Issues []string
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.Join(strings.Fields(value), "")
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if data, err := encoding.DecodeString(value); err == nil {
			return data, nil
		}
	}
	return nil, errors.New("Base64 内容无效")
}

func parseImport(text string) importResult {
	text = strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if !strings.Contains(text, "://") {
		if data, err := decodeBase64(text); err == nil {
			text = strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
		}
	}
	result := importResult{}
	for index, line := range strings.Fields(text) {
		node, err := parseLink(line)
		if err != nil {
			result.Issues = append(result.Issues, fmt.Sprintf("第 %d 项：%s", index+1, err))
			continue
		}
		result.Nodes = append(result.Nodes, node)
	}
	if len(result.Nodes) == 0 && len(result.Issues) == 0 {
		result.Issues = []string{"请粘贴节点链接或订阅内容"}
	}
	return result
}

func loadImport(text string) importResult {
	text = strings.TrimSpace(text)
	target, err := url.Parse(text)
	if err == nil && (target.Scheme == "http" || target.Scheme == "https") && target.User == nil && (target.Port() == "" || (target.Path != "" && target.Path != "/") || target.RawQuery != "") && !strings.ContainsAny(text, "\r\n ") {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, text, nil)
		if err != nil {
			return importResult{Issues: []string{"订阅地址无效"}}
		}
		request.Header.Set("User-Agent", "ShoesGUI/0.1")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return importResult{Issues: []string{"无法读取订阅，请检查地址和网络"}}
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return importResult{Issues: []string{fmt.Sprintf("订阅返回 HTTP %d", response.StatusCode)}}
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024+1))
		if err != nil || len(data) > 8*1024*1024 {
			return importResult{Issues: []string{"订阅内容无法读取或过大"}}
		}
		return parseImport(string(data))
	}
	return parseImport(text)
}

func parseLink(link string) (Node, error) {
	if strings.HasPrefix(strings.ToLower(link), "vmess://") {
		return parseVMess(link[len("vmess://"):])
	}
	if strings.HasPrefix(strings.ToLower(link), "ss://") {
		return parseSS(link[len("ss://"):])
	}
	for _, scheme := range []string{"socks://", "socks5://"} {
		if strings.HasPrefix(strings.ToLower(link), scheme) {
			body, fragment, hasFragment := strings.Cut(link[len(scheme):], "#")
			if !strings.ContainsAny(body, "@:") {
				if decoded, err := decodeBase64(body); err == nil && strings.Contains(string(decoded), "@") {
					link = scheme + string(decoded)
					if hasFragment {
						link += "#" + fragment
					}
				}
			}
		}
	}
	parsed, err := url.Parse(link)
	if err != nil || parsed.Hostname() == "" {
		return Node{}, errors.New("节点链接格式无效")
	}
	node := Node{Host: parsed.Hostname(), Port: parsed.Port(), Name: parsed.Fragment, Network: "tcp"}
	query := parsed.Query()
	switch strings.ToLower(parsed.Scheme) {
	case "vless":
		node.Kind = "VLESS"
		node.UUID = parsed.User.Username()
	case "trojan":
		node.Kind = "Trojan"
		node.Password = parsed.User.Username()
		node.TLS = true
	case "anytls":
		node.Kind = "AnyTLS"
		node.Password = parsed.User.Username()
		node.TLS = true
	case "socks", "socks5":
		node.Kind = "SOCKS5"
		node.Username = parsed.User.Username()
		node.Password, _ = parsed.User.Password()
	case "http", "https":
		node.Kind = "HTTP"
		node.Username = parsed.User.Username()
		node.Password, _ = parsed.User.Password()
		node.TLS = parsed.Scheme == "https"
	case "hysteria2", "hy2", "tuic":
		return Node{}, errors.New("当前 Shoes 内核不支持此协议的出站连接")
	default:
		return Node{}, errors.New("不支持此链接类型")
	}
	if (node.Kind == "SOCKS5" || node.Kind == "HTTP") && node.Username != "" && node.Password == "" {
		if decoded, err := decodeBase64(node.Username); err == nil {
			username, password, ok := strings.Cut(string(decoded), ":")
			if ok {
				node.Username, node.Password = username, password
			}
		}
	}
	if node.Port == "" {
		if node.TLS {
			node.Port = "443"
		} else {
			return Node{}, errors.New("链接缺少端口")
		}
	}
	node.SNI = firstValue(query, "sni", "peer", "serverName")
	node.ALPN = query.Get("alpn")
	node.Fingerprint = query.Get("fp")
	node.SkipVerify = firstValue(query, "allowInsecure", "insecure", "skip-cert-verify") == "1" || firstValue(query, "allowInsecure", "insecure", "skip-cert-verify") == "true"
	switch query.Get("security") {
	case "", "none":
	case "tls":
		node.TLS = true
	case "reality":
		node.Reality = true
		node.TLS = false
	default:
		return Node{}, errors.New("不支持此安全层")
	}
	node.Network = firstValue(query, "type", "net")
	if node.Network == "" || node.Network == "raw" {
		node.Network = "tcp"
	}
	if node.Network == "websocket" {
		node.Network = "ws"
	}
	if node.Network == "splithttp" {
		node.Network = "xhttp"
	}
	node.Path = query.Get("path")
	node.HTTPHost = query.Get("host")
	node.Mode = query.Get("mode")
	node.Flow = query.Get("flow")
	node.Encryption = query.Get("encryption")
	node.PublicKey = query.Get("pbk")
	node.ShortID = query.Get("sid")
	node.MLDSAVerify = query.Get("pqv")
	if node.Network == "ws" && node.Path == "" {
		node.Path = "/"
	}
	if query.Get("extra") != "" {
		node.XHTTPExtra = json.RawMessage(query.Get("extra"))
	}
	if header := query.Get("headerType"); header != "" && header != "none" {
		return Node{}, errors.New("暂不支持 TCP 伪装")
	}
	for _, key := range []string{"ech", "echConfigList", "finalmask", "packetEncoding"} {
		if query.Get(key) != "" {
			return Node{}, errors.New("链接包含尚未支持的扩展参数")
		}
	}
	if strings.Contains(node.Path, "ed=") || query.Get("ed") != "" {
		return Node{}, errors.New("暂不支持 WebSocket Early Data")
	}
	return finishNode(node)
}

func firstValue(query url.Values, keys ...string) string {
	for _, key := range keys {
		if value := query.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func finishNode(node Node) (Node, error) {
	if node.Network == "ws" && strings.Contains(node.Path, "ed=") {
		return Node{}, errors.New("暂不支持 WebSocket Early Data")
	}
	if node.Name == "" {
		node.Name = net.JoinHostPort(node.Host, node.Port)
	}
	if node.Kind == "VMess" && node.Cipher == "" {
		node.Cipher = "auto"
	}
	if len(node.XHTTPExtra) > 0 {
		var extra map[string]any
		if json.Unmarshal(node.XHTTPExtra, &extra) != nil {
			return Node{}, errors.New("XHTTP 扩展配置无效")
		}
		if len(extra) == 0 {
			node.XHTTPExtra = nil
		} else {
			node.XHTTPExtra, _ = json.Marshal(extra)
		}
	}
	if err := node.validate(); err != nil {
		return Node{}, err
	}
	return node, nil
}

func parseSS(value string) (Node, error) {
	body, fragment, _ := strings.Cut(value, "#")
	name, err := url.PathUnescape(fragment)
	if err != nil {
		return Node{}, errors.New("节点名称编码无效")
	}
	if !strings.Contains(body, "@") {
		data, err := decodeBase64(body)
		if err != nil {
			return Node{}, err
		}
		body = string(data)
	}
	auth, server, ok := strings.Cut(body, "@")
	if !ok {
		return Node{}, errors.New("Shadowsocks 链接缺少服务器")
	}
	if !strings.Contains(auth, ":") {
		data, err := decodeBase64(auth)
		if err != nil {
			return Node{}, err
		}
		auth = string(data)
	} else {
		auth, err = url.PathUnescape(auth)
		if err != nil {
			return Node{}, errors.New("Shadowsocks 认证编码无效")
		}
	}
	cipher, password, ok := strings.Cut(auth, ":")
	if !ok {
		return Node{}, errors.New("Shadowsocks 认证格式无效")
	}
	target, err := url.Parse("ss://" + server)
	if err != nil {
		return Node{}, errors.New("Shadowsocks 地址无效")
	}
	if target.Query().Get("plugin") != "" {
		return Node{}, errors.New("暂不支持 Shadowsocks 插件")
	}
	return finishNode(Node{Name: name, Kind: "Shadowsocks", Host: target.Hostname(), Port: target.Port(), Cipher: cipher, Password: password})
}

func parseVMess(value string) (Node, error) {
	data, err := decodeBase64(value)
	if err != nil {
		return Node{}, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return Node{}, errors.New("VMess 内容无效")
	}
	get := func(key string) string {
		var value string
		if json.Unmarshal(fields[key], &value) == nil {
			return value
		}
		var number json.Number
		if json.Unmarshal(fields[key], &number) == nil {
			return number.String()
		}
		return ""
	}
	if aid := get("aid"); aid != "" && aid != "0" {
		return Node{}, errors.New("不支持旧版 VMess alterId")
	}
	if header := get("type"); header != "" && header != "none" {
		return Node{}, errors.New("不支持 VMess TCP 伪装")
	}
	node := Node{Name: get("ps"), Kind: "VMess", Host: get("add"), Port: get("port"), UUID: get("id"), Cipher: get("scy"), Network: get("net"), TLS: get("tls") == "tls", SNI: get("sni"), Path: get("path"), HTTPHost: get("host"), ALPN: get("alpn"), Fingerprint: get("fp")}
	if node.Network == "" {
		node.Network = "tcp"
	}
	if node.Network == "ws" && node.Path == "" {
		node.Path = "/"
	}
	if insecure, err := strconv.ParseBool(get("allowInsecure")); err == nil {
		node.SkipVerify = insecure
	}
	return finishNode(node)
}

func importBatch(settings Settings, incoming []Node, target string) (Settings, int) {
	next, added := mergeNodes(settings, incoming)
	for index := len(settings.Nodes); index < len(next.Nodes); index++ {
		next.Nodes[index].Group = target
	}
	if _, exists := groupCounts(settings)[target]; added > 0 && target != "" && !exists {
		next.Groups = append(append([]string(nil), settings.Groups...), target)
	}
	return next, added
}

func mergeNodes(settings Settings, incoming []Node) (Settings, int) {
	next := settings
	next.Nodes = append([]Node(nil), settings.Nodes...)
	seen := map[string]bool{}
	for _, node := range next.Nodes {
		seen[nodeIdentity(node)] = true
	}
	added := 0
	for _, node := range incoming {
		identity := nodeIdentity(node)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		next.Nodes = append(next.Nodes, node)
		added++
	}
	if next.Selected < 0 && len(next.Nodes) > 0 {
		next.Selected = 0
	}
	return next, added
}

func nodeIdentity(node Node) string {
	data, _ := json.Marshal(map[string]any{"host": node.Host, "port": node.Port, "protocol": protocol(node)})
	return string(data)
}
