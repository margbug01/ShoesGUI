package main

import "testing"

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
