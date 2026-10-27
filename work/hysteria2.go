package main

import (
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func parseHY2(parsed *url.URL) (Node, error) {
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return Node{}, errors.New("HY2 参数格式无效")
	}
	for key, values := range query {
		switch key {
		case "sni", "insecure", "allowInsecure", "pinSHA256", "alpn", "downmbps":
			if len(values) != 1 {
				return Node{}, errors.New("HY2 参数重复")
			}
		default:
			return Node{}, errors.New("HY2 暂不支持此扩展参数（含混淆、端口跳跃）")
		}
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return Node{}, errors.New("HY2 链接不支持路径")
	}
	node := Node{Kind: "Hysteria2", Host: parsed.Hostname(), Port: parsed.Port(), Name: parsed.Fragment, Network: "quic", TLS: true, SNI: query.Get("sni"), CertPin: strings.ToLower(strings.ReplaceAll(query.Get("pinSHA256"), ":", "")), HY2DownloadMbps: 100}
	if node.Port == "" {
		node.Port = "443"
	}
	if parsed.User != nil {
		node.Password = parsed.User.Username()
		if password, ok := parsed.User.Password(); ok {
			node.Password += ":" + password
		}
	}
	for _, key := range []string{"insecure", "allowInsecure"} {
		if value := query.Get(key); value != "" {
			switch value {
			case "1", "true":
				node.SkipVerify = true
			case "0", "false":
			default:
				return Node{}, errors.New("HY2 insecure 参数无效")
			}
		}
	}
	if value := query.Get("alpn"); value != "" && value != "h3" {
		return Node{}, errors.New("HY2 仅支持 ALPN h3")
	}
	if value := query.Get("downmbps"); value != "" {
		speed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return Node{}, errors.New("HY2 下载带宽无效")
		}
		node.HY2DownloadMbps = uint32(speed)
	}
	return finishNode(node)
}

func validateHY2(node Node) error {
	if node.Password == "" {
		return errors.New("请填写 HY2 密码")
	}
	for _, char := range node.Password {
		if char < 32 || char > 126 {
			return errors.New("HY2 密码必须为可打印 ASCII 字符")
		}
	}
	if strings.ContainsAny(node.SNI, " /\\\r\n\t:") {
		return errors.New("SNI 仅填写域名")
	}
	if node.CertPin != "" {
		raw, err := hex.DecodeString(node.CertPin)
		if err != nil || len(raw) != 32 {
			return errors.New("证书 SHA256 指纹必须为 64 位十六进制")
		}
	}
	if node.SkipVerify && node.CertPin == "" {
		return errors.New("HY2 不允许无指纹跳过证书验证，请填写 pinSHA256 或使用可信证书")
	}
	if node.Network != "quic" || node.Reality || node.Flow != "" || node.Path != "" {
		return errors.New("HY2 使用原生 QUIC，不支持叠加其他传输")
	}
	if node.HY2DownloadMbps > 10000 {
		return errors.New("HY2 下载带宽范围为 0–10000 Mbps，0 为自动")
	}
	return nil
}

func withHY2Limits(config []any, nodes []Node) []any {
	for _, node := range nodes {
		if node.Kind == "Hysteria2" {
			return append([]any{map[string]any{"global_limits": map[string]any{"quic_receive_window": 16 << 20, "quic_stream_window": 8 << 20, "quic_send_window": 4 << 20}}}, config...)
		}
	}
	return config
}

func clientConfig(node Node) map[string]any {
	result := map[string]any{"address": net.JoinHostPort(node.Host, node.Port)}
	if node.Kind != "Hysteria2" {
		result["protocol"] = protocol(node)
		return result
	}
	sni := node.SNI
	if sni == "" {
		sni = node.Host
	}
	tls := map[string]any{"verify": true, "sni_hostname": sni, "alpn_protocols": []string{"h3"}}
	if node.CertPin != "" {
		tls["verify"] = false
		tls["server_fingerprints"] = []string{node.CertPin}
	}
	result["transport"] = "quic"
	result["quic_settings"] = tls
	result["protocol"] = map[string]any{"type": "hysteria2", "password": node.Password, "download_mbps": node.HY2DownloadMbps}
	return result
}
