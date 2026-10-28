package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
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
	if text, bad := runState("启动失败", false, false, true, time.Time{}, now); !bad || text != "启动失败，点这里看日志" {
		t.Fatal(text)
	}
	if text, _ := runState("未连接", false, false, false, time.Time{}, now); text != "还没有节点" {
		t.Fatal(text)
	}
}

func TestDashboardTellsTheTruth(t *testing.T) {
	saved := readClipboard
	readClipboard = func() string { return "" }
	defer func() { readClipboard = saved }()
	dir := t.TempDir()
	a := &application{settings: defaultSettings(), engine: newEngine(dir), tab: "仪表盘", storageOK: true, settingsPath: filepath.Join(dir, "s.dpapi"), delays: map[string]int{}, nodeGroup: "全部"}
	tester := ui.NewTester(a.view, 820, 1000)
	tester.Frame()
	if err := tester.Click("粘贴导入"); err != nil {
		t.Fatal("empty dashboard should offer paste import:", err)
	}
	tester.Frame()
	if a.tab != "代理" || !a.importing {
		t.Fatal("paste import did not open")
	}
	a.importing, a.tab = false, "仪表盘"
	a.settings.Nodes, a.settings.Selected = []Node{sampleNode("Trojan")}, 0
	a.engine.mu.Lock()
	a.engine.ready = true
	a.engine.mu.Unlock()
	tester.Frame()
	tester.Frame()
	if _, ok := tester.Find("未接管系统代理"); !ok {
		t.Fatal("exit card must say the browser is direct while not taken over")
	}
	if _, ok := tester.Find("TUN"); ok {
		t.Fatal("the always-off TUN switch is back")
	}
	if a.dash.checking {
		t.Fatal("exit lookup started without system proxy")
	}
}

// 网速图的曲线在两个采样点之间不冲出这两点的范围：一秒 3 MB、下一秒 0，中间不能画到底线下面去
func TestTrafficCurveStaysBetweenSamples(t *testing.T) {
	xs := []float32{0, 10, 20, 30, 40, 50, 60}
	ys := []float32{100, 100, 2, 100, 100, 40, 100} // 屏幕坐标，越小越高；100 是底线
	m := monotoneSlopes(xs, ys)
	for i := 0; i+1 < len(xs); i++ {
		h := (xs[i+1] - xs[i]) / 3
		p0, p1, p2, p3 := ys[i], ys[i]+m[i]*h, ys[i+1]-m[i+1]*h, ys[i+1]
		lo, hi := min(p0, p3), max(p0, p3)
		for k := 0; k <= 20; k++ {
			u := float32(k) / 20
			y := (1-u)*(1-u)*(1-u)*p0 + 3*(1-u)*(1-u)*u*p1 + 3*(1-u)*u*u*p2 + u*u*u*p3
			if y < lo-0.01 || y > hi+0.01 {
				t.Fatalf("segment %d leaves [%.1f, %.1f] at %.2f: %.2f", i, lo, hi, u, y)
			}
		}
	}
}

// 刻度的顶取 1、2、5 档，比最大值大一成以上，最少 20 KB/s；刻度上的字不带多余的小数
func TestRateScale(t *testing.T) {
	for peak, want := range map[int64]int64{0: 20 << 10, 300: 20 << 10, 19 << 10: 50 << 10, 600 << 10: 1 << 20, 3400 << 10: 5 << 20, 1 << 30: 2 << 30} {
		if got := rateScale(peak); got != want {
			t.Fatalf("peak %d: got %d, want %d", peak, got, want)
		}
	}
	for n, want := range map[int64]string{20 << 10: "20 KB/s", 512 << 10: "512 KB/s", 5 << 20: "5 MB/s", 5 << 19: "2.5 MB/s", 2 << 30: "2 GB/s"} {
		if got := rateText(n); got != want {
			t.Fatalf("%d: got %q, want %q", n, got, want)
		}
	}
}

// 指针放到网速图上，看得到那一秒的上下行；最右边是「现在」
func TestTrafficChartShowsTheSecondUnderThePointer(t *testing.T) {
	a := shellTestApp(t)
	for i := 0; i < speedSamples; i++ {
		a.dash.push(rate{up: 1 << 10, down: int64(i) << 20})
	}
	tester := ui.NewTester(a.view, 880, 900)
	tester.Frame()
	r, ok := tester.Find("过去一分钟的网速")
	if !ok {
		t.Fatal("no chart")
	}
	tester.Move(r.X+r.W-1, r.Y+r.H/2)
	tester.Frame()
	if !tester.HasText("现在") || !tester.HasText("下载 59 MB/s") {
		t.Fatal("no tooltip for the last second:", tester.Texts())
	}
	tester.Move(r.X+r.W-1-10*r.W/(speedSamples-1), r.Y+r.H/2)
	tester.Frame()
	if !tester.HasText("10 秒前") || !tester.HasText("下载 49 MB/s") {
		t.Fatal("no tooltip ten seconds back:", tester.Texts())
	}
}
