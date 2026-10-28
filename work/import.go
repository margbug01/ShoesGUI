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
	Source string // 从订阅地址拉来的就是那个地址
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
	if format := configFormat(text); format != "" {
		return importResult{Issues: []string{"这是一整份" + format + "，暂不支持；请用 v2rayN 格式的分享链接或订阅"}}
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

// subscriptionUA 机场面板按 User-Agent 决定回什么格式，不认得的多半当 Clash 回 YAML。
// 我们解析的是 v2rayN 那种分享链接，所以照它的样子报；有的面板只看开头是不是 v2rayN，后缀放自己的名字
const subscriptionUA = "v2rayN/7.13.8 ShoesGUI/0.4"

// configFormat 认出整份配置文件（Clash YAML、JSON），这种不是链接列表，别把里面的网址当节点
func configFormat(text string) string {
	switch {
	case strings.HasPrefix(text, "proxies:") || strings.Contains(text, "\nproxies:"):
		return "Clash 配置（YAML）"
	case strings.HasPrefix(text, "{") || strings.HasPrefix(text, "["):
		return "JSON 配置"
	}
	return ""
}

// subscriptionURL 认出订阅地址：http(s)、不带账号、只有一行。只有主机加端口的当成 HTTP 代理链接
func subscriptionURL(text string) bool {
	text = strings.TrimSpace(text)
	target, err := url.Parse(text)
	return err == nil && (target.Scheme == "http" || target.Scheme == "https") && target.User == nil && (target.Port() == "" || (target.Path != "" && target.Path != "/") || target.RawQuery != "") && !strings.ContainsAny(text, "\r\n ")
}

func loadImport(text string) importResult {
	text = strings.TrimSpace(text)
	if subscriptionURL(text) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, text, nil)
		if err != nil {
			return importResult{Issues: []string{"订阅地址无效"}}
		}
		request.Header.Set("User-Agent", subscriptionUA)
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
		result := parseImport(string(data))
		result.Source = text
		return result
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
		// 代理链接是 http://账号@主机:端口；带路径、参数或没写端口的是普通网址（比如配置里的 DNS、规则地址）
		if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Port() == "" {
			return Node{}, errors.New("这是网址，不是代理链接")
		}
		node.Kind = "HTTP"
		node.Username = parsed.User.Username()
		node.Password, _ = parsed.User.Password()
		node.TLS = parsed.Scheme == "https"
	case "hysteria2", "hy2":
		return parseHY2(parsed)
	case "tuic":
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

// syncGroup 拿订阅的最新内容替换 group 里来自订阅的节点：同名的就地换成新配置（位置不变），
// 新名字加到最后，订阅里已经没有的删掉。手动加进这个分组的节点不动。
// 正在用的节点还在就接着用它，没了就换成这个分组的第一个
func syncGroup(settings Settings, group string, incoming []Node) (next Settings, added, updated, removed int) {
	queue := map[string][]int{} // 名字 → 组里同名订阅节点的下标，按先后
	for i, n := range settings.Nodes {
		if n.Group == group && n.FromSub {
			queue[n.Name] = append(queue[n.Name], i)
		}
	}
	replacement := map[int]Node{}
	var fresh []Node
	for _, n := range incoming {
		n.Group, n.FromSub = group, true
		if q := queue[n.Name]; len(q) > 0 {
			replacement[q[0]], queue[n.Name] = n, q[1:]
		} else {
			fresh = append(fresh, n)
		}
	}
	next = settings
	next.Nodes = nil
	moved := map[int]int{}
	for i, n := range settings.Nodes {
		if n.Group == group && n.FromSub {
			r, ok := replacement[i]
			if !ok {
				removed++
				continue
			}
			if nodeIdentity(r) != nodeIdentity(n) {
				updated++
			}
			n = r
		}
		moved[i] = len(next.Nodes)
		next.Nodes = append(next.Nodes, n)
	}
	next.Nodes = append(next.Nodes, fresh...)
	added = len(fresh)
	next.Selected = -1
	if j, ok := moved[settings.Selected]; ok {
		next.Selected = j
	} else if len(next.Nodes) > 0 {
		next.Selected = 0
		for j, n := range next.Nodes {
			if n.Group == group {
				next.Selected = j
				break
			}
		}
	}
	return next, added, updated, removed
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
	if node.Kind == "Hysteria2" {
		data, _ := json.Marshal(clientConfig(node))
		return string(data)
	}
	data, _ := json.Marshal(map[string]any{"host": node.Host, "port": node.Port, "protocol": protocol(node)})
	return string(data)
}
