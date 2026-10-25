package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConnLine(t *testing.T) {
	for _, test := range []struct{ line, want string }{
		{`ERROR  127.0.0.1:50715 finished with error: Custom { kind: ConnectionAborted, error: "flow cancelled" }`, "换节点或模式时断开的旧连接"},
		{`ERROR  127.0.0.1:65471 finished with error: Os { code: 10053, kind: ConnectionAborted, message: "你的主机中的软件中止了一个已建立的连接。" }`, "本机程序中止了连接，多半是浏览器关了页面或取消了请求"},
		{`ERROR  127.0.0.1:61754 finished with error: Os { code: 10054, kind: ConnectionReset, message: "远程主机强迫关闭了一个现有的连接。" }`, "对方重置了连接"},
		{`ERROR  127.0.0.1:60169 finished with error: Custom { kind: UnexpectedEof, error: "peer closed connection without sending TLS close_notify: https://docs.rs/rustls" }`, "对方断开 TLS 时没打招呼，一般无害"},
		{`ERROR  127.0.0.1:52833 finished with error: Custom { kind: ConnectionRefused, error: "x" }`, `Custom { kind: ConnectionRefused, error: "x" }`},
	} {
		if got, ok := connLine(test.line); !ok || got != test.want {
			t.Fatalf("%s\n got %q", test.line, got)
		}
	}
	for _, line := range []string{"Servers ready", "ERROR  failed to bind 127.0.0.1:10808", "WARN  something"} {
		if _, ok := connLine(line); ok {
			t.Fatalf("not a connection line: %q", line)
		}
	}
}

func TestConnEventsMerge(t *testing.T) {
	e := newEngine(t.TempDir())
	e.connEvent("A")
	e.event("", "其他事件")
	e.connEvent("B")
	e.connEvent("A")
	e.connEvent("A")
	_, logs, _, _ := e.snapshot()
	if len(logs) != 3 || logs[2].conn != "A" || logs[2].count != 3 || logs[1].conn != "B" || logs[0].text != "其他事件" {
		t.Fatalf("%+v", logs)
	}
	if s := logs[2].String(); !strings.HasSuffix(s, "连接异常 ×3：A") {
		t.Fatal(s)
	}
	// 一分钟前的不再合并
	e.mu.Lock()
	e.logs[2].at = time.Now().Add(-2 * time.Minute)
	e.mu.Unlock()
	e.connEvent("A")
	_, logs, _, _ = e.snapshot()
	if len(logs) != 4 || logs[3].count != 1 {
		t.Fatalf("%+v", logs)
	}
}

func TestTailLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k.log")
	var mu sync.Mutex
	var lines []string
	flush := tailLog(path, &kernelLog{onLine: func(line string) {
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
	}})
	time.Sleep(100 * time.Millisecond) // 文件还没建也不能出错
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	file.WriteString("第一行\n")
	time.Sleep(700 * time.Millisecond)
	file.WriteString("由于目标计算机积极拒绝，无法连接。\n")
	file.Close()
	flush()
	if len(lines) != 2 || lines[1] != "由于目标计算机积极拒绝，无法连接。" {
		t.Fatalf("%q", lines)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("log file not removed")
	}
}
