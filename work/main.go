package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type application struct {
	importing      bool
	importText     string
	importParsed   string
	importObserved string
	importChanged  time.Time
	importResult   importResult
	importTarget   string
	importNew      bool
	importNewName  string
	fetching       bool   // 正在拉订阅（更新订阅）
	parsing        bool   // 粘贴导入正在识别（订阅地址要联网）
	lastImport     string // 这次打开 Shoes 后最后导入的内容，剪贴板还是它就不再预填
	share          *shareState
	notice         string
	noticeUntil    time.Time
	addingGroup    bool
	renaming       string // 正在改名的分组
	groupDelete    string // 正在确认删除的分组
	sortByDelay    bool
	sortKey        string // 上次排序时的节点集合，测速中集合没变就沿用 sorted
	sorted         []int
	focusSearch    bool
	reveal         bool // 编辑节点时把密码、UUID 显示出来
	logSearch      string
	groupDraft     string
	groupError     string
	nodeGroup      string
	nodeSearch     string
	revealSelected bool // 下一帧把正在用的节点卡片滚进视野（仪表盘「切换节点」过来时）
	advanced       bool
	settings       Settings
	engine         *engine
	window         *mygo.Window
	busy           atomic.Bool
	tab            string
	shownTab       string // 上一帧显示的页，换页时清掉报错
	editing        bool
	editIndex      int
	draft          Node
	errorText      string
	deleteConfirm  bool
	storageOK      bool
	settingsPath   string
	kernelDraft    string
	portDraft      string
	kernelError    string
	portError      string
	kernelFocus    bool // 上一帧输入框有没有焦点，失焦就保存
	portFocus      bool
	delays         map[string]int // delayKeys（节点配置）→ 毫秒；0 测速中，-1 超时
	testing        bool
	quitting       bool
	openAtLogin    bool
	tray           *mygo.Tray
	trayOn         bool
	trayTip        string
	dash           dashboard
	// 下面这些是控件的跨帧绑定，见 choice
	hy2SpeedPick            choice[float64]
	themePick, modePick     choice[int]
	loginPick, autoPick     choice[bool]
	closePick               choice[bool]
	securityPick, extraPick choice[string]
	logPick, sortPick       choice[int]
	logFilter               int         // 0 全部，1 只看重要（藏起连接异常）
	dashOn                  atomic.Bool // 仪表盘正显示着，才去拉连接数
}

func (a *application) async(fn func()) {
	if !a.busy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.busy.Store(false)
		fn()
		if a.window != nil {
			a.window.Invalidate()
		}
	}()
}

func (a *application) save(next Settings) bool {
	if !a.storageOK {
		a.errorText = "无法读取设置，请检查本地数据"
		return false
	}
	if err := saveEncrypted(a.settingsPath, next); err != nil {
		a.errorText = "保存失败"
		return false
	}
	a.settings = next
	a.errorText = ""
	return true
}

// commit 保存节点、端口或内核这类要重写配置的改动，内核在后台重启（约 1 秒，系统代理保持）
func (a *application) commit(next Settings) bool {
	if !a.save(next) {
		return false
	}
	if len(next.Nodes) == 0 {
		go a.engine.stop()
		return true
	}
	a.engine.reload(next)
	return true
}

// choose 运行中直接切节点；切不了（内核没起或在重启）就按新选择重启
func (a *application) choose(index int) {
	next := a.settings
	next.Selected = index
	if !a.save(next) {
		return
	}
	go func() {
		if a.engine.choose(groupSelect, kernelNames(next.Nodes)[index]) != nil {
			a.engine.reload(next)
			return
		}
		if !next.KeepConnections {
			a.engine.closeConnections()
		}
		a.window.Update(func() { a.dash.route++ })
	}()
}

func (a *application) setMode(mode string) {
	if mode == a.settings.Mode {
		return
	}
	next := a.settings
	next.Mode = mode
	if !a.save(next) || len(next.Nodes) == 0 {
		return
	}
	go func() {
		domestic, other := modeTargets(mode)
		if a.engine.choose(groupDomestic, domestic) != nil || a.engine.choose(groupOther, other) != nil {
			a.engine.reload(next)
			return
		}
		if !next.KeepConnections {
			a.engine.closeConnections()
		}
		a.window.Update(func() { a.dash.route++ })
	}()
	if a.window != nil {
		a.window.Invalidate()
	}
}

func (a *application) toggleConnect() {
	if _, _, _, proxy := a.engine.snapshot(); proxy {
		a.async(a.engine.disconnect)
		return
	}
	s := a.settings
	a.async(func() { a.engine.connect(s) })
}

func (a *application) selectedNode() (Node, bool) {
	if a.settings.Selected >= 0 && a.settings.Selected < len(a.settings.Nodes) {
		return a.settings.Nodes[a.settings.Selected], true
	}
	return Node{}, false
}

// testNodes 测 indices 这些节点：tcp 为 true 做 TCPing，否则做真连接测速。结果都记在 delays，后测的覆盖先测的，和 v2rayN 一样
func (a *application) testNodes(indices []int, tcp bool) {
	if tcp {
		filtered := make([]int, 0, len(indices))
		for _, index := range indices {
			if a.settings.Nodes[index].Kind != "Hysteria2" {
				filtered = append(filtered, index)
			}
		}
		if len(filtered) != len(indices) {
			a.showNotice("HY2 使用 UDP，已跳过 TCPing；请用真连接测速")
		}
		indices = filtered
	}
	if a.testing || len(indices) == 0 {
		return
	}
	a.testing = true
	all := delayKeys(a.settings.Nodes)
	nodes, keys := make([]Node, len(indices)), make([]string, len(indices))
	for k, i := range indices {
		nodes[k], keys[k] = a.settings.Nodes[i], all[i]
		a.delays[keys[k]] = 0
	}
	kernel := a.settings.Kernel
	go func() {
		report := func(k, ms int) { a.window.Update(func() { a.delays[keys[k]] = ms }) }
		var err error
		if tcp {
			eachNode(len(nodes), func(k int) int { return tcping(nodes[k]) }, report)
		} else {
			err = realPings(kernel, a.engine.dir, nodes, report)
		}
		a.window.Update(func() {
			a.testing = false
			for _, key := range keys {
				if a.delays[key] == 0 {
					a.delays[key] = -1
				}
			}
			if err != nil {
				a.errorText = "测速失败：" + err.Error()
			}
			a.saveDelays()
		})
	}()
}

// saveDelays 把测速结果存进设置，只留还在的节点
func (a *application) saveDelays() {
	if !a.storageOK {
		return
	}
	next := a.settings
	next.Delays = map[string]int{}
	for _, key := range delayKeys(next.Nodes) {
		if ms, ok := a.delays[key]; ok && ms != 0 {
			next.Delays[key] = ms
		}
	}
	a.save(next)
}

// forgetDelays 清掉 group 里节点的测速结果（订阅刚更新过，旧数字不作数）
func (a *application) forgetDelays(group string) {
	for _, n := range a.settings.Nodes {
		if n.Group == group {
			delete(a.delays, nodeIdentity(n))
		}
	}
	a.saveDelays()
}

func field(c *ui.Context, label string, value *string) {
	settingRow(c, label, "", func() { ui.TextInput(c.Key(label), value).Width(240).Label(label) })
}

// secretField 密码、UUID 这类真正的秘密默认遮住，旁边一个「显示」好核对粘进来的对不对
func (a *application) secretField(c *ui.Context, label string, value *string) {
	settingRow(c, label, "", func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			input := ui.TextInput(c.Key(label), value).Width(170).Label(label)
			text := "显示"
			if a.reveal {
				text = "隐藏"
			} else {
				input.Password()
			}
			if ui.Button(c.Key(label+"-reveal"), text).Shrink(0).Clicked() {
				a.reveal = !a.reveal
			}
		})
	})
}

func (a *application) beginEdit(index int) {
	a.share = nil
	a.editing, a.editIndex, a.deleteConfirm, a.reveal = true, index, false, false
	a.errorText = ""
	a.importing = false
	a.advanced = false
	a.draft = Node{Kind: "SOCKS5", Port: "1080", Cipher: "aes-256-gcm"}
	if index >= 0 {
		a.draft = a.settings.Nodes[index]
	} else if a.nodeGroup != "全部" && a.nodeGroup != "未分组" {
		a.draft.Group = a.nodeGroup
	}
}

func (a *application) nodesView(c *ui.Context) {
	if a.share != nil {
		a.shareView(c)
		return
	}
	if a.importing {
		a.importView(c)
		return
	}
	if a.editing {

		group(c, "", func() {
			field(c, "名称", &a.draft.Name)
			field(c, "分组", &a.draft.Group)
			settingRow(c, "协议", "", func() {
				ui.Select(c.Key("protocol"), &a.draft.Kind, []string{"SOCKS5", "HTTP", "Shadowsocks", "Trojan", "VLESS", "VMess", "AnyTLS", "Hysteria2"}).Width(240)
			})
			field(c, "服务器", &a.draft.Host)
			field(c, "端口", &a.draft.Port)
		})
		group(c.Key(a.draft.Kind), "认证", func() {
			switch a.draft.Kind {
			case "SOCKS5", "HTTP":
				field(c, "用户名", &a.draft.Username)
				a.secretField(c, "密码", &a.draft.Password)
			case "Shadowsocks":
				settingRow(c, "加密方式", "", func() {
					ui.Select(c.Key("cipher"), &a.draft.Cipher, []string{"aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-ietf-poly1305"}).Width(240)
				})
				a.secretField(c, "密码", &a.draft.Password)
			case "Trojan", "AnyTLS", "Hysteria2":
				a.secretField(c, "密码", &a.draft.Password)
			case "VLESS", "VMess":
				a.secretField(c, "UUID", &a.draft.UUID)
			}
		})

		a.transportFields(c)
		ui.Row(c).FillWidth().Padding(12, 0, 0, 0).Gap(8).Children(func() {
			ui.Spacer(c)
			if ui.Button(c.Key("cancel-edit"), "取消").Clicked() {
				a.editing = false
				a.errorText = ""
			}

			if ui.PrimaryButton(c.Key("save-node"), "保存").MinWidth(88).Clicked() {
				if err := a.draft.validate(); err != nil {
					a.errorText = err.Error()
				} else {
					next := a.settings
					next.Nodes = append([]Node(nil), next.Nodes...)
					if a.editIndex < 0 {
						next.Nodes = append(next.Nodes, a.draft)
						if next.Selected < 0 { // 只有第一个节点才自动用上，别把正在跑的流量切到没测过的新节点
							next.Selected = len(next.Nodes) - 1
						}
					} else {
						next.Nodes[a.editIndex] = a.draft
					}
					if a.commit(next) {
						a.editing = false
					}
				}
			}
		})
		return
	}
	a.nodeGrid(c)
}

func detectKernel() string {
	executable, _ := os.Executable()
	candidate := filepath.Join(filepath.Dir(executable), "shoes.exe")
	if validKernel(candidate) == nil {
		return candidate
	}
	if path, err := exec.LookPath("shoes.exe"); err == nil {
		full, _ := filepath.Abs(path)
		return full
	}
	return ""
}

func configureRenderer() {
	if _, configured := os.LookupEnv("MYGO_GPU"); !configured {
		os.Setenv("MYGO_GPU", "0")
	}
}

func main() {
	configureRenderer()
	if !mygo.App.RequestSingleInstanceLock() {
		return // 已经在跑的那个会把窗口叫出来
	}
	dir, err := dataDir()
	if err != nil {
		return
	}
	e := newEngine(dir)
	a := &application{settings: defaultSettings(), engine: e, tab: "仪表盘", storageOK: true, settingsPath: filepath.Join(dir, "settings.dpapi"), delays: map[string]int{}, nodeGroup: "全部", logFilter: 1}
	if err := loadEncrypted(a.settingsPath, &a.settings); err != nil && !errors.Is(err, os.ErrNotExist) {
		a.storageOK = false
		a.errorText = "无法读取本地设置"
		a.settings = defaultSettings()
	}
	if a.settings.Selected < 0 || a.settings.Selected >= len(a.settings.Nodes) {
		a.settings.Selected = min(0, len(a.settings.Nodes)-1)
	}
	if a.settings.Port == "" {
		a.settings.Port = "10808"
	}
	for name, ms := range a.settings.Delays { // 上次的测速结果，重开也看得到
		a.delays[name] = ms
	}
	if a.settings.Kernel == "" {
		a.settings.Kernel = detectKernel()
	}
	a.kernelDraft, a.portDraft = a.settings.Kernel, a.settings.Port
	applyTheme(a.settings.Theme)
	if err := e.restoreProxy(); err != nil {
		e.event("", err.Error())
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "runtime-*"))
	for _, path := range leftovers {
		os.Remove(path)
	}
	e.event("", "Shoes 已启动")
	if len(a.settings.Nodes) > 0 {
		if a.settings.AutoConnect {
			s := a.settings
			a.async(func() { e.connect(s) })
		} else {
			e.reload(a.settings)
		}
	}
	mygo.App.OnWindowAllClosed(func() {}) // 只剩托盘时接着跑
	mygo.App.OnSecondInstance(func([]string, string) { a.showWindow() })
	mygo.App.WhenReady(func() {
		a.openAtLogin = mygo.App.OpenAtLogin()
		a.window = mygo.NewWindow(mygo.WindowOptions{Title: "Shoes", Width: 820, Height: 620, MinWidth: 600, MinHeight: 480, Frameless: true, DisableMaximize: true, StateKey: "main", Hidden: mygo.App.WasOpenedAtLogin(), Content: ui.View(a.view)})
		a.createTray()
		a.window.OnHide(func() { a.dashOn.Store(false) })
		go a.watchTraffic()
		go a.watchConnections()
		stopTick := make(chan struct{})
		a.window.OnClose(func(event *mygo.CloseEvent) {
			if !a.quitting {
				event.PreventDefault()
				a.window.Hide()
				a.hintTray()
				return
			}
			a.quitting = false
			if a.busy.Load() {
				event.PreventDefault()
				a.showWindow()
				a.errorText = "正在处理，请稍后再退出"
				return
			}
			a.engine.stop()
			if _, _, _, active := a.engine.snapshot(); active {
				event.PreventDefault()
				a.showWindow()
				a.errorText = "系统代理恢复失败，请重试退出"
				return
			}
			close(stopTick)
		})
		go func() {
			ticker := time.NewTicker(400 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					a.window.Update(func() {
						a.expireNotice(time.Now())
						if a.importReady(time.Now()) {
							a.parseImportAsync()
						}
						a.syncTray()
					})
				case <-stopTick:
					return
				}
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		e.event("", "原生界面运行失败")
	}
	e.stop()
}
