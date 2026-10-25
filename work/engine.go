package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// engine 管一个常驻内核：应用打开就跑，配置变了就重启；「启动/停止」只是接管和恢复系统代理
type engine struct {
	op        sync.Mutex // 内核进程和系统代理一次只改一样
	mu        sync.Mutex // 保护下面的状态
	status    string
	logs      []logEntry
	tail      string
	ready     bool
	port      string
	api       runtimeConfig
	proxy     proxyManager
	proxyPort string
	since     time.Time // 这次接管系统代理是什么时候开始的
	gen       int       // 内核每次就绪加一，换了内核的地方据此刷新
	want      *Settings
	kick      chan struct{}

	cmd    *exec.Cmd
	done   chan struct{}
	job    windows.Handle
	config string
	dir    string
}

func newEngine(dir string) *engine {
	e := &engine{dir: dir, status: "未连接", proxy: proxyManager{path: filepath.Join(dir, "proxy-recovery.dpapi")}, kick: make(chan struct{}, 1)}
	go e.worker()
	return e
}

func (e *engine) event(status, message string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if status != "" {
		e.status = status
	}
	e.addLog(logEntry{at: time.Now(), text: message})
}

// logEntry 一条日志。conn 非空表示某条连接没正常收尾，同样的原因一分钟内合并成一条计数
type logEntry struct {
	at    time.Time
	text  string
	conn  string
	count int
}

func (l logEntry) String() string {
	text := l.text
	if l.conn != "" {
		text = "连接异常：" + l.conn
		if l.count > 1 {
			text = fmt.Sprintf("连接异常 ×%d：%s", l.count, l.conn)
		}
	}
	return l.at.Format("15:04:05") + "  " + text
}

// connEvent 记一条连接异常：一分钟内有同样原因的就加一，并挪到最新的位置
func (e *engine) connEvent(reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	entry := logEntry{at: now, conn: reason, count: 1}
	for i := len(e.logs) - 1; i >= 0 && now.Sub(e.logs[i].at) < time.Minute; i-- {
		if e.logs[i].conn == reason {
			entry.count += e.logs[i].count
			e.logs = append(e.logs[:i], e.logs[i+1:]...)
			break
		}
	}
	e.addLog(entry)
}

func (e *engine) addLog(entry logEntry) {
	e.logs = append(e.logs, entry)
	if len(e.logs) > 300 {
		e.logs = e.logs[len(e.logs)-300:]
	}
}

func (e *engine) snapshot() (string, []logEntry, bool, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status, append([]logEntry(nil), e.logs...), e.ready, e.proxy.active
}

// runInfo 给仪表盘用：内核代次、接管开始时间（没接管为零值）、本地端口
func (e *engine) runInfo() (gen int, since time.Time, port string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.proxy.active {
		since = e.since
	}
	return e.gen, since, e.port
}

func (e *engine) isReady() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ready
}

// reload 要求内核按 s 重启。连续几次只做最后一次
func (e *engine) reload(s Settings) {
	e.mu.Lock()
	e.want = &s
	e.mu.Unlock()
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

func (e *engine) worker() {
	for range e.kick {
		e.mu.Lock()
		s := e.want
		e.want = nil
		e.mu.Unlock()
		if s != nil {
			e.op.Lock()
			e.restart(*s)
			e.op.Unlock()
		}
	}
}

func validKernel(path string) error {
	if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".exe") {
		return errors.New("请选择 shoes Windows .exe 的绝对路径")
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return errors.New("内核文件不存在")
	}
	return nil
}

// restart 换内核时系统代理保持接管，新内核起不来才恢复
func (e *engine) restart(s Settings) {
	e.killKernel()
	if err := e.launch(s); err != nil {
		if rerr := e.restoreProxy(); rerr != nil {
			e.event("", rerr.Error())
		}
		e.event("启动失败", err.Error())
		return
	}
	e.mu.Lock()
	active, old, since := e.proxy.active, e.proxyPort, e.since
	e.mu.Unlock()
	if active && old != s.Port {
		e.setProxy(false, "")
		e.setProxy(true, s.Port)
		e.mu.Lock()
		e.since = since // 只是换了端口，运行时长接着算
		e.mu.Unlock()
	}
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// spawnKernel 写临时配置并启动内核。进程放进 job，GUI 不管怎么退出它都跟着结束。
// 启动播报和配置错误走 stdout/stderr；日志写文件再跟读，因为内核写管道时会把中文系统错误变成问号。
// 进程结束后调 flush，读完剩下的日志并删掉日志文件
func spawnKernel(kernel, dir string, data []byte, out *kernelLog) (cmd *exec.Cmd, job windows.Handle, path string, flush func(), err error) {
	file, err := os.CreateTemp(dir, "runtime-*.yaml")
	if err != nil {
		return nil, 0, "", nil, errors.New("无法创建临时配置")
	}
	path = file.Name()
	logPath := strings.TrimSuffix(path, ".yaml") + ".log"
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(path)
		return nil, 0, "", nil, errors.New("临时配置写入失败")
	}
	job, err = newJob()
	if err != nil {
		os.Remove(path)
		return nil, 0, "", nil, errors.New("无法创建内核进程")
	}
	cmd = exec.Command(kernel, "--no-reload", "-l", logPath, path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	cmd.Env = append(os.Environ(), "RUST_LOG=warn")
	cmd.Stdout, cmd.Stderr = out, out
	cmd.Dir = filepath.Dir(kernel)
	if err := cmd.Start(); err != nil {
		windows.CloseHandle(job)
		os.Remove(path)
		return nil, 0, "", nil, errors.New("内核启动失败")
	}
	if err := assignJob(job, cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		windows.CloseHandle(job)
		os.Remove(path)
		os.Remove(logPath)
		return nil, 0, "", nil, errors.New("无法管理内核进程")
	}
	return cmd, job, path, tailLog(logPath, &kernelLog{onLine: out.onLine}), nil
}

// tailLog 每 300 毫秒把日志文件新写的内容交给 out；返回的 flush 读完剩下的、删掉文件
func tailLog(path string, out io.Writer) (flush func()) {
	stop, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		defer os.Remove(path)
		var file *os.File
		defer func() {
			if file != nil {
				file.Close()
			}
		}()
		buf := make([]byte, 32<<10)
		for {
			last := false
			select {
			case <-stop:
				last = true
			case <-time.After(300 * time.Millisecond):
			}
			if file == nil {
				file, _ = os.Open(path)
			}
			for file != nil {
				n, err := file.Read(buf)
				out.Write(buf[:n])
				if err != nil || n == 0 {
					break
				}
			}
			if last {
				return
			}
		}
	}()
	return func() { close(stop); <-finished }
}

// connReasons 内核常见的单条连接收尾报错，换成人话。只是那一条连接的事，不影响别的连接
var connReasons = []struct{ match, reason string }{
	{"flow cancelled", "换节点或模式时断开的旧连接"},
	{"close_notify", "对方断开 TLS 时没打招呼，一般无害"},
	{"kind: ConnectionAborted", "本机程序中止了连接，多半是浏览器关了页面或取消了请求"},
	{"kind: ConnectionReset", "对方重置了连接"},
}

// connLine 认出「127.0.0.1:端口 finished with error: …」这种单条连接的报错，返回给人看的原因
func connLine(line string) (string, bool) {
	_, detail, ok := strings.Cut(line, " finished with error: ")
	if !ok {
		return "", false
	}
	for _, r := range connReasons {
		if strings.Contains(detail, r.match) {
			return r.reason, true
		}
	}
	return detail, true
}

func (e *engine) launch(s Settings) error {
	e.event("正在启动", "启动内核")
	if err := validKernel(s.Kernel); err != nil {
		return err
	}
	rules, err := ensureRules(e.dir)
	if err != nil {
		return errors.New("无法准备规则文件")
	}
	apiPort, err := freePort()
	if err != nil {
		return errors.New("找不到空闲端口")
	}
	secret := make([]byte, 24)
	rand.Read(secret)
	rt := runtimeConfig{APIPort: apiPort, Secret: hex.EncodeToString(secret), RulesDir: rules}
	data, err := generateConfig(s, rt)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.tail = ""
	e.mu.Unlock()
	ready := make(chan struct{})
	out := &kernelLog{ready: ready, onLine: func(line string) {
		if reason, ok := connLine(line); ok {
			e.connEvent(reason)
			return
		}
		if !startupLine(line) {
			e.mu.Lock()
			e.tail = line // 退出时拿来当原因
			e.mu.Unlock()
		}
		e.event("", "内核  "+line)
	}}
	cmd, job, path, flush, err := spawnKernel(s.Kernel, e.dir, data, out)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	e.cmd, e.done, e.job, e.config = cmd, done, job, path
	e.mu.Lock()
	e.api, e.port = rt, s.Port
	e.mu.Unlock()
	go func() {
		cmd.Wait()
		flush()
		close(done)
		e.op.Lock()
		defer e.op.Unlock()
		if e.cmd != cmd {
			return
		}
		e.killKernel()
		if err := e.restoreProxy(); err != nil {
			e.event("", err.Error())
		}
		e.event("内核已退出", e.exitReason())
	}()
	select {
	case <-ready:
		e.mu.Lock()
		e.ready = true
		e.gen++
		e.mu.Unlock()
		e.event("就绪", fmt.Sprintf("内核就绪：%d 个节点，本地端口 %s", len(s.Nodes), s.Port))
		return nil
	case <-done:
		reason := e.exitReason()
		e.killKernel() // 先清掉 e.cmd，上面的协程就不会再报一次
		return errors.New(reason)
	case <-time.After(10 * time.Second):
		e.killKernel()
		return errors.New("内核 10 秒内没有就绪，已停止")
	}
}

func (e *engine) exitReason() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.tail == "" {
		return "内核意外退出"
	}
	return "内核退出：" + e.tail
}

// killKernel 只停进程，不碰系统代理
func (e *engine) killKernel() {
	e.mu.Lock()
	e.ready = false
	e.mu.Unlock()
	if e.job != 0 {
		windows.TerminateJobObject(e.job, 1)
		windows.CloseHandle(e.job)
		e.job = 0
	}
	if e.cmd != nil {
		e.cmd.Process.Kill()
		if e.done != nil {
			<-e.done
		}
	}
	e.cmd = nil
	if e.config != "" {
		os.Remove(e.config)
		e.config = ""
	}
}

// stop 退出时用：停内核并恢复系统代理
func (e *engine) stop() {
	e.op.Lock()
	defer e.op.Unlock()
	e.killKernel()
	if err := e.restoreProxy(); err != nil {
		e.event("", err.Error())
	}
	e.event("未连接", "已停止")
}

func (e *engine) restoreProxy() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.proxy.restore()
}

// connect 内核没起来就先起，再接管系统代理
func (e *engine) connect(s Settings) {
	e.op.Lock()
	defer e.op.Unlock()
	if !e.isReady() {
		e.restart(s)
	}
	e.mu.Lock()
	ready, port := e.ready, e.port
	e.mu.Unlock()
	if ready {
		e.setProxy(true, port)
	}
}

func (e *engine) disconnect() {
	e.op.Lock()
	defer e.op.Unlock()
	e.setProxy(false, "")
}

func (e *engine) setProxy(on bool, port string) {
	e.mu.Lock()
	if !on && !e.proxy.active {
		e.mu.Unlock()
		return
	}
	var err error
	if on {
		if err = e.proxy.enable(port); err == nil {
			e.proxyPort, e.since = port, time.Now()
		}
	} else {
		err = e.proxy.restore()
	}
	e.mu.Unlock()
	switch {
	case err != nil:
		e.event("", err.Error())
	case on:
		e.event("", "已接管系统代理：127.0.0.1:"+port)
	default:
		e.event("", "已恢复系统代理")
	}
}

// kernelLog 把内核输出按行交给 onLine；看到 Servers ready 说明所有监听都绑好了
type kernelLog struct {
	ready  chan struct{}
	once   sync.Once
	buf    []byte
	onLine func(line string)
}

func (k *kernelLog) Write(p []byte) (int, error) {
	k.buf = append(k.buf, p...)
	for {
		i := bytes.IndexByte(k.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(string(k.buf[:i]))
		k.buf = k.buf[i+1:]
		if line == "" {
			continue
		}
		if line == "Servers ready" && k.ready != nil {
			k.once.Do(func() { close(k.ready) })
		}
		k.onLine(tidyKernelLine(line))
	}
	if len(k.buf) > 64<<10 {
		k.buf = k.buf[:0]
	}
	return len(p), nil
}

// startupLine 是启动播报，不能拿来当退出原因
func startupLine(line string) bool {
	return line == "Servers ready" || strings.HasPrefix(line, "Starting ")
}

// tidyKernelLine 把 "[2026-10-25T12:59:29.292 ERROR shoes::tcp] msg" 收成 "ERROR msg"
func tidyKernelLine(line string) string {
	if !strings.HasPrefix(line, "[") {
		return line
	}
	head, rest, ok := strings.Cut(line[1:], "] ")
	fields := strings.Fields(head)
	if !ok || len(fields) < 2 {
		return line
	}
	return fields[1] + "  " + rest
}
