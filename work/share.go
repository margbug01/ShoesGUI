package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strings"

	"github.com/skip2/go-qrcode"
)

func shareLink(node Node) (string, error) {
	normalized, err := finishNode(node)
	if err != nil {
		return "", err
	}
	node = normalized
	target := url.URL{Host: net.JoinHostPort(node.Host, node.Port), Fragment: node.Name}
	query := url.Values{}
	switch node.Kind {
	case "Hysteria2":
		target.Scheme, target.User = "hysteria2", url.User(node.Password)
		if node.SNI != "" {
			query.Set("sni", node.SNI)
		}
		if node.CertPin != "" {
			query.Set("pinSHA256", node.CertPin)
		}
		if node.SkipVerify {
			query.Set("insecure", "1")
		}
		query.Set("downmbps", fmt.Sprint(node.HY2DownloadMbps))
		target.RawQuery = query.Encode()
		return checkedShare(node, target.String())
	case "VMess":
		fields := map[string]string{"v": "2", "ps": node.Name, "add": node.Host, "port": node.Port, "id": node.UUID, "aid": "0", "scy": node.Cipher, "net": node.Network, "type": "none", "host": node.HTTPHost, "path": node.Path, "sni": node.SNI, "alpn": node.ALPN, "fp": node.Fingerprint}
		if node.TLS {
			fields["tls"] = "tls"
		}
		if node.SkipVerify {
			fields["allowInsecure"] = "true"
		}
		data, marshalErr := json.Marshal(fields)
		if marshalErr != nil {
			return "", errors.New("无法生成 VMess 链接")
		}
		return checkedShare(node, "vmess://"+base64.StdEncoding.EncodeToString(data))
	case "Shadowsocks":
		target.Scheme = "ss"
		target.User = url.User(base64.RawURLEncoding.EncodeToString([]byte(node.Cipher + ":" + node.Password)))
		if strings.HasPrefix(node.Cipher, "2022-") {
			target.User = url.UserPassword(node.Cipher, node.Password)
		}
	case "VLESS":
		target.Scheme, target.User = "vless", url.User(node.UUID)
	case "Trojan", "AnyTLS":
		target.Scheme, target.User = strings.ToLower(node.Kind), url.User(node.Password)
	case "SOCKS5", "HTTP":
		target.Scheme = "socks5"
		if node.Kind == "HTTP" {
			target.Scheme = "http"
			if node.TLS {
				target.Scheme = "https"
			}
		}
		if node.Username != "" || node.Password != "" {
			target.User = url.UserPassword(node.Username, node.Password)
		}
	default:
		return "", errors.New("此协议暂不支持分享")
	}
	if node.Kind != "Shadowsocks" && node.Kind != "HTTP" {
		for key, value := range map[string]string{"type": node.Network, "sni": node.SNI, "alpn": node.ALPN, "fp": node.Fingerprint, "path": node.Path, "host": node.HTTPHost, "mode": node.Mode, "flow": node.Flow, "encryption": node.Encryption, "pbk": node.PublicKey, "sid": node.ShortID, "pqv": node.MLDSAVerify, "extra": string(node.XHTTPExtra)} {
			if value != "" {
				query.Set(key, value)
			}
		}
		if node.Reality {
			query.Set("security", "reality")
		} else if node.TLS {
			query.Set("security", "tls")
		}
		if node.SkipVerify {
			query.Set("allowInsecure", "1")
		}
	}
	target.RawQuery = query.Encode()
	return checkedShare(node, target.String())
}

func shareComparable(node Node) Node {
	node.Group, node.FromSub = "", false
	switch node.Kind {
	case "SOCKS5", "HTTP":
		node.UUID, node.Cipher = "", ""
	case "Shadowsocks":
		node.UUID, node.Username = "", ""
	case "Trojan", "AnyTLS":
		node.UUID, node.Username, node.Cipher = "", "", ""
	case "VLESS":
		node.Username, node.Password, node.Cipher = "", "", ""
	case "VMess":
		node.Username, node.Password = "", ""
	}
	if node.Network == "" {
		node.Network = "tcp"
	}
	return node
}

func checkedShare(node Node, link string) (string, error) {
	parsed, err := parseLink(link)
	if err != nil || !reflect.DeepEqual(shareComparable(node), shareComparable(parsed)) {
		return "", errors.New("此节点含无法完整写入标准分享链接的参数，未导出；请使用原始订阅")
	}
	return link, nil
}

func shareNodes(nodes []Node) (string, error) {
	if len(nodes) == 0 {
		return "", errors.New("没有可导出的节点")
	}
	links := make([]string, 0, len(nodes))
	for index, node := range nodes {
		link, err := shareLink(node)
		if err != nil {
			return "", fmt.Errorf("第 %d 个节点：%w", index+1, err)
		}
		links = append(links, link)
	}
	return strings.Join(links, "\n"), nil
}

func shareQR(text string) ([]byte, error) {
	code, err := qrcode.New(text, qrcode.Medium)
	if err != nil || code.VersionNumber > 20 {
		return nil, errors.New("内容太长，无法生成易扫描的二维码，请复制链接")
	}
	return code.PNG(-6)
}
