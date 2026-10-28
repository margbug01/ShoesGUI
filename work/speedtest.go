package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// 测速照 v2rayN 来：同一个测试地址，每个节点最多 5 秒（它的 Global.LocalFetch）
const (
	pingURL     = "https://www.google.com/generate_204"
	pingTimeout = 5 * time.Second
	pingWorkers = 64
)

// eachNode 同时最多测 pingWorkers 个，每测完一个就回报；ms 为 -1 表示失败或超时。
// 超时从轮到这个节点时才开始算，排队不占它的 5 秒
func eachNode(count int, test func(i int) int, report func(i, ms int)) {
	slots := make(chan struct{}, pingWorkers)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			ms := test(i)
			<-slots
			report(i, ms)
		}()
	}
	wg.Wait()
}

func elapsedMS(started time.Time) int {
	return max(1, int(time.Since(started).Milliseconds()))
}

// tcping 和 v2rayN 一样：先解析域名（不计时），再计 TCP 连上服务器端口的时间。
// 只说明服务器端口开着，不经过代理协议，不代表节点能用
func tcping(n Node) int {
	if n.Kind == "Hysteria2" {
		return -1
	}
	lookup, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(lookup, n.Host)
	if err != nil || len(addrs) == 0 {
		return -1
	}
	started := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(addrs[0].IP.String(), n.Port), pingTimeout)
	if err != nil {
		return -1
	}
	ms := elapsedMS(started)
	conn.Close()
	return ms
}

// realPing 是 v2rayN 的「真连接延迟」：经这个节点专属的本地入口，用同一个客户端连发两次 GET，取小的那次。
// 第一次要握手，第二次复用连接，所以结果基本是经节点到 Google 的一个来回。两次都成功才算数，和 v2rayN 一致
func realPing(port int) int {
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "socks5", Host: fmt.Sprintf("127.0.0.1:%d", port)})}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	best := -1
	for k := 0; k < 2; k++ {
		if k > 0 {
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
				return -1
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, pingURL, nil)
		if err != nil {
			return -1
		}
		started := time.Now()
		response, err := client.Do(request)
		if err != nil {
			return -1
		}
		io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		if ms := elapsedMS(started); best < 0 || ms < best {
			best = ms
		}
	}
	return best
}

// pingConfig 给每个节点一个本地入口，入口直接接到这个节点，不经过分流规则
func pingConfig(nodes []Node, ports []int) []byte {
	var config []any
	for i, n := range nodes {
		if ports[i] == 0 {
			continue
		}
		config = append(config, map[string]any{
			"address":  fmt.Sprintf("127.0.0.1:%d", ports[i]),
			"protocol": map[string]any{"type": "mixed", "udp_enabled": false},
			"rules": []any{map[string]any{
				"masks": "0.0.0.0/0", "action": "allow",
				"client_chain": clientConfig(n),
			}},
		})
	}
	data, _ := json.Marshal(withHY2Limits(config, nodes))
	return data
}

// realPings 和 v2rayN 一样另起一个只用来测速的内核，测完就关，不影响正在用的那个。
// 校验不过的节点直接算失败
func realPings(kernel, dir string, nodes []Node, report func(i, ms int)) error {
	if err := validKernel(kernel); err != nil {
		return err
	}
	ports := make([]int, len(nodes))
	var listeners []net.Listener
	for i, n := range nodes {
		if n.validate() != nil {
			continue
		}
		// 先都占住再一起放开，免得拿到重复端口
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			break
		}
		listeners = append(listeners, listener)
		ports[i] = listener.Addr().(*net.TCPAddr).Port
	}
	for _, listener := range listeners {
		listener.Close()
	}
	if len(listeners) == 0 {
		return errors.New("没有可测的节点")
	}
	ready := make(chan struct{})
	var mu sync.Mutex
	last := ""
	out := &kernelLog{ready: ready, onLine: func(line string) {
		if _, conn := connLine(line); !conn && !startupLine(line) {
			mu.Lock()
			last = line
			mu.Unlock()
		}
	}}
	cmd, job, path, flush, err := spawnKernel(kernel, dir, pingConfig(nodes, ports), out, false)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		flush()
		close(done)
	}()
	defer func() {
		job.kill()
		job.close()
		<-done
		os.Remove(path)
	}()
	select {
	case <-ready:
	case <-done:
		mu.Lock()
		defer mu.Unlock()
		return fmt.Errorf("测速内核没起来：%s", last)
	case <-time.After(10 * time.Second):
		return errors.New("测速内核 10 秒内没有就绪")
	}
	eachNode(len(nodes), func(i int) int {
		if ports[i] == 0 {
			return -1
		}
		return realPing(ports[i])
	}, report)
	return nil
}
