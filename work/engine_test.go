package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func testEngine(t *testing.T) *engine {
	t.Helper()
	dir := t.TempDir()
	e := newEngine(dir)
	t.Cleanup(e.stop)
	return e
}

func TestInvalidStart(t *testing.T) {
	e := testEngine(t)
	e.restart(defaultSettings())
	status, _, ready, _ := e.snapshot()
	if status != "启动失败" || ready {
		t.Fatal("invalid configuration started")
	}
}

func TestPortFreeNamesTheOwner(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	exe, _ := os.Executable()
	err = portFree(port)
	if err == nil || !strings.Contains(err.Error(), filepath.Base(exe)) {
		t.Fatalf("want owner %s in error, got %v", filepath.Base(exe), err)
	}
	l.Close()
	if err := portFree(port); err != nil {
		t.Fatalf("freed port still reported busy: %v", err)
	}
}
