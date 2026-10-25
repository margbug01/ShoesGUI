package main

import (
	"testing"
	"time"
)

func TestTrafficSurvivesKernelRestart(t *testing.T) {
	var d dashboard
	d.addTraffic(10, 100, 10, 100)
	d.addTraffic(5, 50, 15, 150)
	d.addTraffic(1, 7, 1, 7) // 内核重启，累计值从头数
	d.addTraffic(2, 3, 3, 10)
	if d.up != 18 || d.down != 160 {
		t.Fatalf("got up=%d down=%d", d.up, d.down)
	}
	for i := 0; i < speedSamples+20; i++ {
		d.push(rate{int64(i), 0})
	}
	if len(d.speeds) != speedSamples || d.speeds[speedSamples-1].up != speedSamples+19 {
		t.Fatal("speed history not trimmed to the newest samples")
	}
}

func TestPlaceName(t *testing.T) {
	for _, test := range []struct {
		parts []string
		want  string
	}{
		{[]string{"中国", "广东省", "佛山"}, "中国 广东省 佛山"},
		{[]string{"中国香港", "香港", "香港"}, "中国香港"},
		{[]string{"日本", "", " 东京 "}, "日本 东京"},
		{[]string{"", "", ""}, ""},
	} {
		if got := placeName(test.parts...); got != test.want {
			t.Fatalf("%q: got %q", test.parts, got)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 12 << 10: "12 KB", 3355443: "3.2 MB", 134 << 20: "134 MB", 2480343613: "2.31 GB"} {
		if got := formatBytes(n); got != want {
			t.Fatalf("%d: got %q, want %q", n, got, want)
		}
	}
}

func TestRunState(t *testing.T) {
	now := time.Now()
	if text, bad := runState("就绪", true, true, true, now.Add(-(time.Hour + 24*time.Minute + 45*time.Second)), now); text != "已运行 01:24:45" || bad {
		t.Fatal(text)
	}
	if text, _ := runState("就绪", true, false, true, time.Time{}, now); text != "未启动" {
		t.Fatal(text)
	}
	if text, bad := runState("启动失败", false, false, true, time.Time{}, now); !bad || text != "启动失败，详情看日志" {
		t.Fatal(text)
	}
	if text, _ := runState("未连接", false, false, false, time.Time{}, now); text != "还没有节点" {
		t.Fatal(text)
	}
}
