package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const speedSamples = 60

type rate struct{ up, down int64 }

// dashboard 是仪表盘的数据，只在界面线程读写，后台协程经 window.Update 交过来
type dashboard struct {
	speeds           []rate // 最近 60 秒，每秒一个，旧的在前
	up, down         int64  // Shoes 打开以来经过内核的流量，内核重启也接着加
	lastUp, lastDown int64  // 内核上次报的累计值
	conns            int
	memory           int64
	exit             exitInfo
	exitKey          string // 上次查出口 IP 时的「内核代次/切换次数」，变了就重查
	checking         bool
	route            int // 切节点、切模式成功一次加一
}

type exitInfo struct{ IP, Place, Via, Err string }

func (d *dashboard) addTraffic(up, down, upTotal, downTotal int64) {
	d.push(rate{up, down})
	d.up += counted(&d.lastUp, upTotal)
	d.down += counted(&d.lastDown, downTotal)
}

func (d *dashboard) push(r rate) {
	d.speeds = append(d.speeds, r)
	if len(d.speeds) > speedSamples {
		d.speeds = d.speeds[len(d.speeds)-speedSamples:]
	}
}

// counted 返回累计值比上次多出来的部分；内核重启后从 0 重新数，就整个算新的
func counted(last *int64, total int64) int64 {
	delta := total - *last
	if delta < 0 {
		delta = total
	}
	*last = total
	return delta
}

// watchTraffic 跟着内核 /traffic 的每秒推送记网速和流量；内核没起来时补 0，曲线照常往前走
func (a *application) watchTraffic() {
	for {
		if a.engine.isReady() {
			a.engine.stream("/traffic", func(line []byte) {
				var t struct{ Up, Down, UpTotal, DownTotal int64 }
				if json.Unmarshal(line, &t) == nil {
					a.window.Update(func() { a.dash.addTraffic(t.Up, t.Down, t.UpTotal, t.DownTotal) })
				}
			})
		} else {
			a.window.Update(func() { a.dash.push(rate{}) })
		}
		time.Sleep(time.Second)
	}
}

// watchConnections 每秒拉一次连接：记进 a.flows（连接页看每个域名怎么走的），顺带拿连接数和内核内存给仪表盘
func (a *application) watchConnections() {
	shown := [2]int64{-1, -1}
	for range time.Tick(time.Second) {
		var snap struct {
			Connections []flowConn
			Memory      int64
		}
		if err := a.engine.call(http.MethodGet, "/connections", nil, &snap); err != nil {
			if _, _, ready := a.engine.endpoint(); ready {
				continue // 内核还在，只是这一次没问到，别把连接都当成断了
			}
			snap.Connections = nil
		}
		changed := a.flows.record(snap.Connections, time.Now())
		count, memory := int64(len(snap.Connections)), snap.Memory
		dash := a.dashOn.Load() && shown != [2]int64{count, memory}
		if dash || a.connsOn.Load() && changed {
			if dash {
				shown = [2]int64{count, memory}
			}
			a.window.Update(func() { a.dash.conns, a.dash.memory = int(count), memory })
		}
	}
}

// 查出口 IP 的站点都在境外，规则模式下也走节点。前一个不通再试下一个
var exitSources = []struct {
	url   string
	parse func(data []byte) (ip, place string, err error)
}{
	{"https://ipwho.is/?lang=zh-CN&fields=ip,success,message,country,region,city", func(data []byte) (string, string, error) {
		var r struct {
			Success                            bool
			IP, Message, Country, Region, City string
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return "", "", err
		}
		if !r.Success || r.IP == "" {
			return "", "", errors.New("ipwho.is：" + r.Message)
		}
		return r.IP, placeName(r.Country, r.Region, r.City), nil
	}},
	{"http://ip-api.com/json/?lang=zh-CN&fields=status,message,query,country,regionName,city", func(data []byte) (string, string, error) {
		var r struct{ Status, Message, Query, Country, RegionName, City string }
		if err := json.Unmarshal(data, &r); err != nil {
			return "", "", err
		}
		if r.Status != "success" || r.Query == "" {
			return "", "", errors.New("ip-api.com：" + r.Message)
		}
		return r.Query, placeName(r.Country, r.RegionName, r.City), nil
	}},
}

// lookupExit 经本地代理端口查出口 IP，走的是和浏览器一样的分流
func lookupExit(port string) (ip, place string, err error) {
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "127.0.0.1:" + port})}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 6 * time.Second}
	for _, source := range exitSources {
		var data []byte
		if data, err = fetch(client, source.url); err == nil {
			if ip, place, err = source.parse(data); err == nil {
				return ip, place, nil
			}
		}
	}
	return "", "", err
}

func fetch(client *http.Client, target string) ([]byte, error) {
	response, err := client.Get(target)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New(response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 64<<10))
}

// placeName 把国家、地区、城市拼起来，跳过重复的（「中国香港 香港 香港」只留「中国香港」）
func placeName(parts ...string) string {
	out := ""
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || strings.Contains(out, part) {
			continue
		}
		if out != "" {
			out += " "
		}
		out += part
	}
	return out
}

func (a *application) checkExit(key, port string) {
	a.dash.checking, a.dash.exitKey = true, key
	via := "直连模式，没经过节点"
	if node, ok := a.selectedNode(); ok && a.settings.Mode != "direct" {
		via = "经「" + node.Name + "」"
	}
	go func() {
		ip, place, err := lookupExit(port)
		a.window.Update(func() {
			a.dash.checking = false
			a.dash.exit = exitInfo{IP: ip, Place: place, Via: via}
			if err != nil {
				a.dash.exit.Err = "查不到，当前节点可能不通"
				a.engine.event("", "出口 IP 查询失败："+err.Error())
			}
		})
	}()
}

func formatBytes(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	case n < 10<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n < 1<<30:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
}

func clock(d time.Duration) string {
	s := int(d / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}

// runState 仪表盘右上角那句话；bad 为 true 用红字
func runState(status string, ready, proxy, hasNodes bool, since, now time.Time) (text string, bad bool) {
	switch {
	case proxy && !since.IsZero():
		return "已运行 " + clock(now.Sub(since)), false
	case proxy:
		return "运行中", false
	case !hasNodes:
		return "还没有节点", false
	case ready:
		return "未启动", false
	case status == "启动失败" || status == "内核已退出":
		return status + "，点这里看日志", true
	}
	return status, false
}

func modeIndex(key string) int {
	for i, m := range modes {
		if m.key == key {
			return i
		}
	}
	return 0
}

// nodeSummary 「VLESS · Reality · XHTTP · 快车」
func nodeSummary(n Node) string { return nodeProtocol(n) + " · " + nodeGroupName(n) }

// nodeProtocol 「VLESS · Reality · XHTTP」
func nodeProtocol(n Node) string {
	parts := []string{n.Kind}
	switch {
	case n.Reality:
		parts = append(parts, "Reality")
	case n.TLS && n.Kind != "Trojan" && n.Kind != "AnyTLS":
		parts = append(parts, "TLS")
	}
	switch n.Network {
	case "ws":
		parts = append(parts, "WebSocket")
	case "xhttp":
		parts = append(parts, "XHTTP")
	}
	return strings.Join(parts, " · ")
}
