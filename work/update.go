package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// 自动更新用 MyGo 的签名更新：发版时 mygo build 用私钥签好每个平台的压缩包和清单，传到 GitHub Release；
// 这里读最新 Release 里本平台的清单，有新版本就下载、验签、替换，再重启。仓库和公钥写在 mygo.json 的 updates。
// 直接 go build 的开发版，或者放在没有写权限的地方（比如还在 dmg 里），不能自己更新，只给下载页

const (
	repoPage     = "https://github.com/margbug01/ShoesGUI"
	releasesPage = repoPage + "/releases"
)

type updateStage int

const (
	updateIdle updateStage = iota // 没查过，或者已是最新
	updateChecking
	updateAvailable
	updateDownloading
	updateReady // 新版本已经换好，等重启
)

type updater struct {
	mu       sync.Mutex
	enabled  bool // 启动时问过一次 mygo.Updater.Enabled
	stage    updateStage
	found    *mygo.Update
	progress float64 // 下载进度 0–1
	err      string
	checked  time.Time // 上次查完的时间，不管结果
	latest   bool      // 上次查到的是已是最新
}

// updateView 是界面画一帧用的快照
type updateView struct {
	enabled        bool
	stage          updateStage
	version, notes string
	progress       float64
	err            string
	checked        time.Time
	latest         bool
}

func (u *updater) view() updateView {
	u.mu.Lock()
	defer u.mu.Unlock()
	v := updateView{enabled: u.enabled, stage: u.stage, progress: u.progress, err: u.err, checked: u.checked, latest: u.latest}
	if u.found != nil {
		v.version, v.notes = u.found.Version, u.found.Notes
	}
	return v
}

// directKey 放进请求的 context，表示这次更新请求不经内核
type directKey struct{}

// routeUpdates 让更新请求经自己的内核：国内直连 GitHub 常常很慢或连不上。
// MyGo 的更新器用 http.DefaultClient，这里只认它的请求（User-Agent 以 MyGo/ 开头，跳转后也带着），订阅等别的请求照旧
func routeUpdates(e *engine) {
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		t.Proxy = updateProxy(e.localProxy)
	}
}

func updateProxy(local func() (string, bool)) func(*http.Request) (*url.URL, error) {
	return func(r *http.Request) (*url.URL, error) {
		if strings.HasPrefix(r.Header.Get("User-Agent"), "MyGo/") && r.Context().Value(directKey{}) == nil {
			if port, ok := local(); ok {
				return &url.URL{Scheme: "http", Host: "127.0.0.1:" + port}, nil
			}
		}
		return http.ProxyFromEnvironment(r)
	}
}

// viaKernelThenDirect 内核就绪时先经内核请求，失败了（节点不通之类）再直连试一次
func viaKernelThenDirect(e *engine, timeout time.Duration, fn func(context.Context) error) error {
	_, routed := e.localProxy()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	err := fn(ctx)
	cancel()
	if err == nil || !routed || errors.Is(err, mygo.ErrUpdatesDisabled) {
		return err
	}
	ctx, cancel = context.WithTimeout(context.WithValue(context.Background(), directKey{}, true), timeout)
	defer cancel()
	return fn(ctx)
}

// updateError 给人看的原因，原始错误写进日志
func updateError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "连接 GitHub 超时"
	case strings.Contains(err.Error(), "404"):
		return "GitHub 上的最新版本还没有更新包"
	}
	return "网络不通或 GitHub 暂时连不上，详情见日志"
}

func (a *application) refresh() {
	if a.window != nil {
		a.window.Invalidate()
	}
}

// checkUpdate 查一次有没有新版本。manual 是用户点的：查不成要说原因；自动检查失败不打扰
func (a *application) checkUpdate(manual bool) {
	u := &a.update
	u.mu.Lock()
	if !u.enabled || u.stage == updateChecking || u.stage == updateDownloading || u.stage == updateReady {
		u.mu.Unlock()
		return
	}
	before := u.stage
	u.stage, u.err = updateChecking, ""
	u.mu.Unlock()
	a.refresh()
	go func() {
		var found *mygo.Update
		err := viaKernelThenDirect(a.engine, 30*time.Second, func(ctx context.Context) (err error) {
			found, err = mygo.Updater.Check(ctx)
			return err
		})
		u.mu.Lock()
		u.checked = time.Now()
		switch {
		case err != nil:
			u.stage = before // 之前查到的新版本还在
			if manual {
				u.err = "检查更新失败：" + updateError(err)
			}
		case found == nil:
			u.stage, u.found, u.latest = updateIdle, nil, true
		default:
			u.stage, u.found, u.latest = updateAvailable, found, false
		}
		u.mu.Unlock()
		if err != nil && manual {
			a.engine.event("", "检查更新失败："+err.Error())
		}
		a.refresh()
	}()
}

// installUpdate 下载、验签、替换，成功了就重启。正在跑的这一份不受影响，换的是磁盘上的文件
func (a *application) installUpdate() {
	u := &a.update
	u.mu.Lock()
	found := u.found
	if u.stage != updateAvailable || found == nil {
		u.mu.Unlock()
		return
	}
	u.stage, u.progress, u.err = updateDownloading, 0, ""
	u.mu.Unlock()
	a.refresh()
	go func() {
		err := viaKernelThenDirect(a.engine, 15*time.Minute, func(ctx context.Context) error {
			return found.Install(ctx, func(done, total int64) {
				if total > 0 {
					u.mu.Lock()
					u.progress = float64(done) / float64(total)
					u.mu.Unlock()
					a.refresh()
				}
			})
		})
		u.mu.Lock()
		if err != nil {
			u.stage, u.err = updateAvailable, "更新没装上："+updateError(err)
		} else {
			u.stage = updateReady
		}
		u.mu.Unlock()
		if err != nil {
			a.engine.event("", "更新失败："+err.Error())
			a.refresh()
			return
		}
		a.engine.event("", "已更新到 "+found.Version+"，正在重启")
		a.restartForUpdate()
	}()
}

// restartForUpdate 和托盘「退出」走同一条路：停内核、还原系统代理，再由 MyGo 启动新版本。
// 更新前接管着系统代理的话留个记号，新版本打开后接着接管。退出被拦下时（比如正在处理）留在「立即重启」
func (a *application) restartForUpdate() {
	if _, _, _, proxy := a.engine.snapshot(); proxy {
		markResume(filepath.Dir(a.settingsPath), time.Now())
	}
	mygo.RunOnMain(func() {
		a.quitting = true
		mygo.App.Relaunch()
	})
}

// watchUpdates 打开后等内核起来再查（好经它访问 GitHub），之后每 12 小时查一次；设置里可以关
func (a *application) watchUpdates() {
	time.Sleep(10 * time.Second)
	for {
		skip := true // 应用退出后 RunOnMain 不再执行，就别查了
		mygo.RunOnMain(func() { skip = a.settings.SkipUpdateCheck })
		if !skip {
			a.checkUpdate(false)
		}
		time.Sleep(12 * time.Hour)
	}
}

const resumeFile = "resume-after-update"

func markResume(dir string, now time.Time) {
	atomicFile(filepath.Join(dir, resumeFile), []byte(now.UTC().Format(time.RFC3339)))
}

// takeResume 读掉更新前留的记号。两分钟内留的才算，免得一次没重启成的更新，过几天打开时自己接管代理
func takeResume(dir string, now time.Time) bool {
	path := filepath.Join(dir, resumeFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	os.Remove(path)
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	age := now.Sub(at)
	return err == nil && age >= 0 && age < 2*time.Minute
}

// noteLines 把 Markdown 的更新说明压成几行纯文本：去掉标题和强调符号，列表项换成圆点，太长的截掉
func noteLines(md string, max int) []string {
	var out []string
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, bullet := range []string{"- ", "* "} {
			if rest, ok := strings.CutPrefix(line, bullet); ok {
				line = "· " + rest
			}
		}
		out = append(out, strings.NewReplacer("**", "", "`", "").Replace(line))
	}
	if max > 0 && len(out) > max {
		out = append(out[:max-1], "…")
	}
	return out
}

// updateRows 设置页「关于」里的版本和更新：现在是哪一版、有没有新的、下载到哪了，再加一个自动检查的开关
func (a *application) updateRows(c *ui.Context) {
	u := a.update.view()
	current := mygo.App.Version()
	if current == "" {
		current = "开发版"
	}
	switch {
	case !u.enabled:
		rowWith(c, "Shoes "+current, "这一份不能自己更新（开发版，或所在文件夹没有写入权限），新版本请到 GitHub 下载", u.err, nil, func() {
			ui.Link(c, "打开下载页", releasesPage)
		})
	case u.stage == updateAvailable:
		notes := func() {
			lines := noteLines(u.notes, 8)
			if len(lines) == 0 {
				return
			}
			ui.Column(c).FillWidth().Gap(3).Padding(6, 0, 0, 0).Children(func() {
				for _, line := range lines {
					ui.Text(c, line).FontSize(12).TextColor(pal(c).muted).LineHeight(1.4)
				}
			})
		}
		rowWith(c, "有新版本 "+u.version, "现在是 "+current+"。更新时 Shoes 会重启一下，接管着的系统代理重启后接着接管", u.err, notes, func() {
			if ui.PrimaryButton(c.Key("install-update"), "更新并重启").MinWidth(88).Clicked() {
				a.installUpdate()
			}
		})
	case u.stage == updateDownloading:
		rowWith(c, "正在下载 "+u.version, fmt.Sprintf("%d%%，装好会自动重启", int(u.progress*100)), u.err, nil, func() {
			ui.Progress(c, u.progress).Width(140)
		})
	case u.stage == updateReady:
		rowWith(c, u.version+" 已经装好", "重启 Shoes 后生效", u.err, nil, func() {
			if ui.PrimaryButton(c.Key("restart-update"), "立即重启").MinWidth(88).Clicked() {
				a.restartForUpdate()
			}
		})
	default: // 没查过、已是最新、检查中
		desc := "还没检查过更新"
		if u.latest {
			at := u.checked.Format("15:04")
			if y, m, d := u.checked.Date(); y != time.Now().Year() || m != time.Now().Month() || d != time.Now().Day() {
				at = u.checked.Format("01-02 15:04")
			}
			desc = "已是最新 · " + at + " 检查过"
		}
		rowWith(c, "Shoes "+current, desc, u.err, nil, func() {
			label := "检查更新"
			if u.stage == updateChecking {
				label = "检查中…"
			}
			if ui.Button(c.Key("check-update"), label).Disabled(u.stage == updateChecking).Clicked() {
				a.checkUpdate(true)
			}
		})
	}
	if u.enabled {
		row(c, "自动检查更新", "打开时和之后每 12 小时问一次 GitHub，有新版本只提示，不会自己装", func() {
			p, on, changed := a.updatePick.bind(!a.settings.SkipUpdateCheck)
			ui.Switch(c.Key("auto-update-check"), p).Label("自动检查更新")
			if changed {
				next := a.settings
				next.SkipUpdateCheck = !on
				if a.save(next) && on {
					a.checkUpdate(false)
				}
			}
		})
	}
}

// updateBadge 侧栏底部的新版本提示，点了去设置页更新
func (a *application) updateBadge(c *ui.Context) {
	u := a.update.view()
	if u.stage != updateAvailable && u.stage != updateDownloading && u.stage != updateReady {
		return
	}
	p := pal(c)
	badge := ui.ButtonBase(c.Key("update-badge")).FillWidth().Height(32).PaddingX(12).Gap(8).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Label("新版本 " + u.version).FocusRing(false).Cursor(ui.CursorPointer)
	bg := ui.Transparent
	if badge.Hovered() {
		bg = p.hover
	}
	badge.Background(badge.AnimateColor("bg", bg, hoverFade))
	badge.Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(p.accent).Shrink(0)
		ui.Text(c, "新版本 "+u.version).FontSize(12.5).FontWeight(500).TextColor(p.accent).SingleLine()
	})
	if badge.Clicked() {
		a.tab = "设置"
	}
}
