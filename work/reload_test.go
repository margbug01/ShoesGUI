package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testKernel 真内核：SHOES_TEST_KERNEL，没设就用打包用的 work/resources 里那份，都没有就跳过
func testKernel(t *testing.T) string {
	t.Helper()
	kernel := os.Getenv("SHOES_TEST_KERNEL")
	if kernel == "" {
		kernel = filepath.Join("resources", runtime.GOOS+"-"+runtime.GOARCH, kernelName)
	}
	if _, err := os.Stat(kernel); err != nil {
		t.Skip("没有内核：放进 work/resources/，或者设 SHOES_TEST_KERNEL")
	}
	abs, err := filepath.Abs(kernel)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// echoAddr 本地回声服务。127.0.0.1 按私网规则直连，不碰节点
func echoAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()
	return l.Addr().String()
}

// tunnel 经本地代理端口 CONNECT 到 target，确认通了才返回
func tunnel(port, target string) (net.Conn, error) {
	c, err := net.DialTimeout("tcp4", "127.0.0.1:"+port, 2*time.Second)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	r := bufio.NewReader(c)
	response, err := http.ReadResponse(r, nil)
	if err == nil && response.StatusCode != 200 {
		err = errors.New(response.Status)
	}
	if err == nil && r.Buffered() > 0 {
		err = errors.New("代理多回了数据")
	}
	if err == nil {
		err = echoes(c)
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func echoes(c net.Conn) error {
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Write([]byte("ping\n")); err != nil {
		return err
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(c, got); err != nil {
		return err
	}
	if string(got) != "ping\n" {
		return errors.New("回声不对")
	}
	return nil
}

func reloadNodes(names ...string) []Node {
	nodes := make([]Node, len(names))
	for i, name := range names {
		nodes[i] = sampleNode("Shadowsocks")
		nodes[i].Name = name
	}
	return nodes
}

// runningKernel 起一个真内核：s 的节点是 names，选的是 selected
func runningKernel(t *testing.T, selected int, names ...string) (*engine, Settings) {
	t.Helper()
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	e := testEngine(t)
	s := defaultSettings()
	s.Kernel, s.Port = testKernel(t), strconv.Itoa(port)
	s.Nodes, s.Selected = reloadNodes(names...), selected
	if err := e.launch(s); err != nil {
		t.Fatal(err)
	}
	return e, s
}

// apply 和 worker 一样拿着 op 调 restart，返回内核进程换没换
func apply(e *engine, s Settings) (sameProcess bool) {
	pid := e.cmd.Process.Pid
	e.op.Lock()
	defer e.op.Unlock()
	e.restart(s)
	return e.cmd != nil && e.cmd.Process.Pid == pid
}

func members(t *testing.T, e *engine) []string {
	t.Helper()
	var proxy struct{ All []string }
	if err := e.call(http.MethodGet, "/proxies/"+groupSelect, nil, &proxy); err != nil {
		t.Fatal(err)
	}
	return proxy.All
}

func logged(e *engine, text string) bool {
	_, logs, _, _ := e.snapshot()
	return slices.ContainsFunc(logs, func(l logEntry) bool { return strings.Contains(l.text, text) })
}

// 换订阅、改节点：内核就地换配置，进程不换，开着的连接不断，新连接照常，新节点真的进了内核
func TestReloadKeepsConnections(t *testing.T) {
	e, s := runningKernel(t, 1, "a0", "a1", "a2")
	echo := echoAddr(t)
	old, err := tunnel(s.Port, echo)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	gen, _, _ := e.runInfo()
	next := s
	next.Nodes, next.Selected = reloadNodes("b0", "a1", "b2", "b3"), 1
	if !apply(e, next) {
		t.Fatal("内核被重启了，应该就地换配置")
	}
	if err := echoes(old); err != nil {
		t.Fatal("换配置把开着的连接断了：", err)
	}
	fresh, err := tunnel(s.Port, echo)
	if err != nil {
		t.Fatal("换完配置新连接不通：", err)
	}
	fresh.Close()
	if all := members(t, e); !slices.Contains(all, "b3") || slices.Contains(all, "a0") {
		t.Fatalf("内核里的节点还是旧的：%v", all)
	}
	if now, _ := e.current(groupSelect); now != "a1" {
		t.Fatalf("选的节点变成了 %q", now)
	}
	if after, _, _ := e.runInfo(); after != gen+1 {
		t.Fatal("换完配置代次没加一，出口 IP 不会重查")
	}
	if !logged(e, "内核已换成新配置，现有连接不断") {
		t.Fatal("日志里没说换了配置")
	}
}

// 内核记得从 API 选过的节点；换配置后要以设置为准
func TestReloadFollowsTheSettingsSelection(t *testing.T) {
	e, s := runningKernel(t, 0, "a0", "a1", "a2")
	if err := e.choose(groupSelect, "a2"); err != nil {
		t.Fatal(err)
	}
	next := s
	next.Nodes, next.Selected = reloadNodes("a0", "a1", "a2", "a3"), 1
	next.KeepConnections = true
	if !apply(e, next) {
		t.Fatal("内核被重启了")
	}
	if now, _ := e.current(groupSelect); now != "a1" {
		t.Fatalf("换完配置选的是 %q，设置里是 a1", now)
	}
	next.Mode = "global"
	apply(e, next)
	if now, _ := e.current(groupDomestic); now != groupSelect {
		t.Fatalf("全局模式下国内组选的是 %q", now)
	}
}

// 用的节点没了（订阅里删了）：和切节点一样，没开「保持连接」就断开现有连接
func TestReloadDropsConnectionsWhenTheNodeChanges(t *testing.T) {
	for _, keep := range []bool{false, true} {
		e, s := runningKernel(t, 1, "a0", "a1")
		old, err := tunnel(s.Port, echoAddr(t))
		if err != nil {
			t.Fatal(err)
		}
		next := s
		next.Nodes, next.Selected, next.KeepConnections = reloadNodes("a0", "b1"), 0, keep
		if !apply(e, next) {
			t.Fatal("内核被重启了")
		}
		if now, _ := e.current(groupSelect); now != "a0" {
			t.Fatalf("选的是 %q", now)
		}
		alive := echoes(old) == nil
		old.Close()
		if alive != keep {
			t.Fatalf("保持连接=%v，换完配置连接还在=%v", keep, alive)
		}
	}
}

// 改本地端口还是整个重启
func TestReloadPortChangeRestarts(t *testing.T) {
	e, s := runningKernel(t, 0, "a0")
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	next := s
	next.Port = strconv.Itoa(port)
	if apply(e, next) {
		t.Fatal("改了端口内核没重启")
	}
	if !e.isReady() {
		t.Fatal("重启后没就绪")
	}
	c, err := tunnel(next.Port, echoAddr(t))
	if err != nil {
		t.Fatal("新端口不通：", err)
	}
	c.Close()
}

// 内核不接受新配置：退回整个重启，在那里报出原因，和以前一样
func TestReloadRejectedFallsBackToRestart(t *testing.T) {
	e, s := runningKernel(t, 0, "a0")
	bad := sampleNode("Shadowsocks")
	bad.Name, bad.Cipher = "bad", "2022-blake3-aes-256-gcm" // 2022 要 base64 的 32 字节密钥，内核才认
	next := s
	next.Nodes = append(reloadNodes("a0"), bad)
	if _, err := generateConfig(next, testRuntime); err != nil {
		t.Skip("GUI 自己就拦下了这个节点，换一个只有内核才拒绝的：", err)
	}
	if apply(e, next) {
		t.Fatal("内核拒绝了新配置却还当成换好了")
	}
	if !logged(e, "内核没接受新配置") || !logged(e, "改为重启内核") {
		t.Fatal("日志里没说为什么改成重启")
	}
	if status, _, ready, _ := e.snapshot(); ready || status != "启动失败" {
		t.Fatalf("状态 %q ready=%v", status, ready)
	}
}

func TestReloadLine(t *testing.T) {
	e := newEngine(t.TempDir())
	wait := make(chan error, 1)
	e.reloading = wait
	e.reloadLine("Starting Mixed (HTTP+SOCKS5) TCP server at 127.0.0.1:1")
	if len(wait) != 0 {
		t.Fatal("启动播报不是回话")
	}
	e.reloadLine("Reload rejected; current servers retained: invalid key")
	if err := <-wait; err == nil || err.Error() != "invalid key" {
		t.Fatalf("拒绝原因 %v", err)
	}
	if e.reloading != nil {
		t.Fatal("回过话还在等")
	}
	e.reloadLine("Servers ready") // 没人等的时候不能卡住
}
