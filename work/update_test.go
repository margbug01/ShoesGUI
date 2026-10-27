package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestUpdateProxyOnlyRoutesUpdater(t *testing.T) {
	ready := true
	proxy := updateProxy(func() (string, bool) { return "10808", ready })
	request := func(ua string, ctx context.Context) *http.Request {
		r, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://github.com/margbug01/ShoesGUI/releases/latest/download/update-darwin-arm64.json", nil)
		r.Header.Set("User-Agent", ua)
		return r
	}
	got, err := proxy(request("MyGo/0.6.4 ShoesGUI/0.3.0", context.Background()))
	if err != nil || got == nil || got.String() != "http://127.0.0.1:10808" {
		t.Fatalf("updater request not routed through the kernel: %v %v", got, err)
	}
	same := func(r *http.Request) {
		t.Helper()
		got, err := proxy(r)
		want, wantErr := http.ProxyFromEnvironment(r)
		if !reflect.DeepEqual(got, want) || (err == nil) != (wantErr == nil) {
			t.Fatalf("got %v %v, want the environment's %v %v", got, err, want, wantErr)
		}
	}
	same(request("v2rayN/7.13.8 ShoesGUI/0.2", context.Background())) // 订阅照旧
	same(request("MyGo/0.6.4 ShoesGUI/0.3.0", context.WithValue(context.Background(), directKey{}, true)))
	ready = false
	same(request("MyGo/0.6.4 ShoesGUI/0.3.0", context.Background()))
}

func TestUpdateFallsBackToDirect(t *testing.T) {
	e := &engine{ready: true, port: "10808"}
	var direct []bool
	err := viaKernelThenDirect(e, time.Second, func(ctx context.Context) error {
		direct = append(direct, ctx.Value(directKey{}) != nil)
		if len(direct) == 1 {
			return errors.New("node down")
		}
		return nil
	})
	if err != nil || !reflect.DeepEqual(direct, []bool{false, true}) {
		t.Fatalf("err=%v attempts=%v", err, direct)
	}
	// 内核没就绪时本来就是直连，失败了不再试一遍
	e.ready, direct = false, nil
	err = viaKernelThenDirect(e, time.Second, func(ctx context.Context) error {
		direct = append(direct, ctx.Value(directKey{}) != nil)
		return errors.New("offline")
	})
	if err == nil || len(direct) != 1 {
		t.Fatalf("err=%v attempts=%v", err, direct)
	}
}

func TestResumeAfterUpdate(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 27, 18, 0, 0, 0, time.UTC)
	if takeResume(dir, now) {
		t.Fatal("resumed without a mark")
	}
	markResume(dir, now)
	if !takeResume(dir, now.Add(20*time.Second)) {
		t.Fatal("fresh mark not honoured")
	}
	if takeResume(dir, now.Add(30*time.Second)) {
		t.Fatal("mark used twice")
	}
	markResume(dir, now)
	if takeResume(dir, now.Add(3*time.Minute)) {
		t.Fatal("stale mark honoured")
	}
	if _, err := os.Stat(filepath.Join(dir, resumeFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale mark left behind")
	}
}

func TestNoteLines(t *testing.T) {
	md := "## 0.3.0\n\n- **自动更新**：设置里点一下就装\n* 修了 `XHTTP`\n\n普通一行\n"
	want := []string{"· 自动更新：设置里点一下就装", "· 修了 XHTTP", "普通一行"}
	if got := noteLines(md, 8); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
	if got := noteLines(md, 2); !reflect.DeepEqual(got, []string{"· 自动更新：设置里点一下就装", "…"}) {
		t.Fatalf("truncated: got %q", got)
	}
}

func TestSidebarVersionInTests(t *testing.T) {
	if got := sidebarVersion(); got != "Shoes" { // go test 不是 mygo build，没有版本号
		t.Fatalf("got %q", got)
	}
}
