package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"
	"unsafe"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"golang.org/x/sys/windows"
)

func TestMain(tests *testing.M) {
	if os.Getenv("SHOES_NATIVE_MEMORY") != "1" {
		os.Exit(tests.Run())
	}
	configureRenderer()
	type counters struct {
		Size, Faults                                                                                          uint32
		PeakWorkingSet, WorkingSet, PeakPaged, Paged, PeakNonPaged, NonPaged, Pagefile, PeakPagefile, Private uintptr
	}
	getMemory := windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	report := func(stage string) {
		runtime.GC()
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		memory := counters{}
		memory.Size = uint32(unsafe.Sizeof(memory))
		ok, _, err := getMemory.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&memory)), uintptr(memory.Size))
		if ok == 0 {
			panic(err)
		}
		fmt.Printf("%s WS=%.1f MiB private=%.1f MiB GoLive=%.1f MiB GoSys=%.1f MiB\n", stage, float64(memory.WorkingSet)/(1<<20), float64(memory.Private)/(1<<20), float64(stats.HeapAlloc)/(1<<20), float64(stats.Sys)/(1<<20))
	}
	report("before-window")
	if err := os.MkdirAll("../tmp", 0700); err != nil {
		panic(err)
	}
	dir, err := os.MkdirTemp("../tmp", "native-memory-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	a := &application{settings: defaultSettings(), engine: newEngine(dir), tab: "代理", shownTab: "代理", nodeGroup: "全部", delays: map[string]int{}, storageOK: true, settingsPath: filepath.Join(dir, "settings.dpapi")}
	for index := 0; index < 80; index++ {
		node := sampleNode("VLESS")
		node.Name = fmt.Sprintf("示例节点 %d", index)
		a.settings.Nodes = append(a.settings.Nodes, node)
	}
	showApp := false
	mygo.App.WhenReady(func() {
		a.window = mygo.NewWindow(mygo.WindowOptions{Title: "Shoes 内存测量 · 假数据", Width: 820, Height: 620, Frameless: true, Content: ui.View(func(c *ui.Context) {
			if showApp {
				a.view(c)
			} else {
				ui.Text(c, "原生渲染基线")
			}
		})})
		go func() {
			time.Sleep(2 * time.Second)
			a.window.Update(func() { report("plain-window"); showApp = true })
			time.Sleep(2 * time.Second)
			a.window.Update(func() { report("nodes-window") })
			keys := delayKeys(a.settings.Nodes)
			for frame := 0; frame < 30; frame++ {
				a.window.Update(func() {
					for index, key := range keys {
						a.delays[key] = 10 + frame + index
					}
				})
				time.Sleep(50 * time.Millisecond)
			}
			a.window.Update(func() { report("nodes-updating"); a.tab = "仪表盘" })
			time.Sleep(time.Second)
			a.window.Update(func() { report("dashboard-window") })
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		panic(err)
	}
}

func TestRendererDefault(t *testing.T) {
	t.Setenv("MYGO_GPU", "")
	if err := os.Unsetenv("MYGO_GPU"); err != nil {
		t.Fatal(err)
	}
	configureRenderer()
	if os.Getenv("MYGO_GPU") != "0" {
		t.Fatal("default renderer must use software")
	}
	for _, override := range []string{"1", "0", ""} {
		t.Setenv("MYGO_GPU", override)
		configureRenderer()
		if os.Getenv("MYGO_GPU") != override {
			t.Fatal("explicit renderer override ignored")
		}
	}
}

func TestMemoryProbe(t *testing.T) {
	output := os.Getenv("SHOES_MEMORY_PROBE")
	if output == "" {
		t.Skip("set SHOES_MEMORY_PROBE to a profile directory")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	report := func(stage string) {
		runtime.GC()
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		t.Logf("%s live=%.2f MiB heapSys=%.2f MiB totalAlloc=%.2f MiB", stage, float64(stats.HeapAlloc)/(1<<20), float64(stats.HeapSys)/(1<<20), float64(stats.TotalAlloc)/(1<<20))
		file, err := os.Create(filepath.Join(output, stage+".pprof"))
		if err != nil {
			t.Fatal(err)
		}
		if err := pprof.WriteHeapProfile(file); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	report("start")
	registerFonts()
	report("fonts")
	dir := t.TempDir()
	a := &application{settings: defaultSettings(), engine: newEngine(dir), tab: "代理", shownTab: "代理", nodeGroup: "全部", delays: map[string]int{}, storageOK: true, settingsPath: filepath.Join(dir, "settings.dpapi")}
	for index := 0; index < 80; index++ {
		node := sampleNode("VLESS")
		node.Name = fmt.Sprintf("示例节点 %d", index)
		a.settings.Nodes = append(a.settings.Nodes, node)
	}
	tester := ui.NewTester(a.view, 820, 620)
	tester.Frame()
	tester.Frame()
	report("nodes")
	for frame := 0; frame < 100; frame++ {
		tester.Frame()
	}
	report("idle")
	a.tab = "仪表盘"
	tester.Frame()
	tester.Frame()
	report("dashboard")
	for index := 0; index < 30; index++ {
		a.tab = "代理"
		a.openShare([]Node{a.settings.Nodes[index]}, "", "示例分享")
		tester.Frame()
		tester.Frame()
		a.share = nil
		tester.Frame()
	}
	report("shares")
	runtime.KeepAlive(tester)
}
