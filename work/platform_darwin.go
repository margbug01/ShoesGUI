package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const kernelName = "shoes"
const kernelHint = "一般会自动找到，将 shoes 放在应用旁边或应用资源目录中"

var kernelExtensions = []string{"*"}

func dataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Library", "Application Support", "ShoesGUI")
	return dir, os.MkdirAll(dir, 0700)
}
func validKernel(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("请选择 shoes 可执行文件的绝对路径")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("内核文件不存在")
	}
	if info.Mode().Perm()&0111 == 0 {
		return errors.New("内核文件没有执行权限")
	}
	return nil
}

// bundledKernel 是不是某个 .app 包里自带的内核
func bundledKernel(path string) bool {
	resources := filepath.Dir(path)
	contents := filepath.Dir(resources)
	return filepath.Base(path) == kernelName && filepath.Base(resources) == "Resources" &&
		filepath.Base(contents) == "Contents" && strings.HasSuffix(filepath.Dir(contents), ".app")
}

func kernelCandidates(executable string) []string {
	dir := filepath.Dir(executable)
	return []string{filepath.Join(dir, kernelName), filepath.Join(dir, "..", "Resources", kernelName)}
}

var encryptionKey = keychainKey
var keyMu sync.Mutex

func keychainKey() ([]byte, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	chain := filepath.Join(home, "Library", "Keychains", "login.keychain-db")
	args := []string{"find-generic-password", "-s", "dev.local.shoesgui", "-a", "encryption", "-w", chain}
	out, err := exec.Command("/usr/bin/security", args...).Output()
	if err == nil {
		key, decodeErr := hex.DecodeString(strings.TrimSpace(string(out)))
		if decodeErr != nil || len(key) != 32 {
			return nil, errors.New("钥匙串中的加密密钥损坏")
		}
		return key, nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 44 {
		return nil, fmt.Errorf("无法访问登录钥匙串：%w", err)
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	cmd := exec.Command("/usr/bin/security", "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -s dev.local.shoesgui -a encryption -w %s %s\n", hex.EncodeToString(key), strconv.Quote(chain)))
	if _, err = cmd.Output(); err != nil {
		return nil, fmt.Errorf("无法保存登录钥匙串密钥：%w", err)
	}
	// 交互工具可能仅报告子命令错误，必须回读确认。
	out, err = exec.Command("/usr/bin/security", args...).Output()
	if err != nil || strings.TrimSpace(string(out)) != hex.EncodeToString(key) {
		return nil, errors.New("无法确认登录钥匙串密钥")
	}
	return key, nil
}
func protect(data []byte, decrypt bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("加密数据为空")
	}
	key, err := encryptionKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if decrypt {
		if len(data) < gcm.NonceSize() {
			return nil, errors.New("加密数据损坏")
		}
		return gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, data, nil), nil
}
func atomicFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".shoes-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func saveEncrypted(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data, err = protect(data, false)
	if err != nil {
		return err
	}
	return atomicFile(path, data)
}
func loadEncrypted(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data, err = protect(data, true)
	if err != nil {
		return err
	}
	if json.Unmarshal(data, value) != nil {
		return errors.New("本地设置损坏")
	}
	return nil
}

type proxyEndpoint struct {
	Enabled      bool
	Server, Port string
	Auth         bool
}
type pacProxy struct {
	Enabled bool
	URL     string
}
type serviceProxy struct {
	Web, Secure proxyEndpoint
	Bypass      []string
	PAC         pacProxy
}
type proxySnapshot map[string]serviceProxy
type proxyJournal struct{ Before, Owned proxySnapshot }
type proxyManager struct {
	path   string
	active bool
	read   func() (proxySnapshot, error)
	write  func(proxySnapshot) error
}

var networkCommand = func(args ...string) (string, error) {
	out, err := exec.Command("/usr/sbin/networksetup", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("networksetup: %w: %s", err, out)
	}
	text := string(out)
	if strings.Contains(text, "Error") || strings.Contains(text, "error") {
		return "", errors.New(text)
	}
	return text, nil
}

func parseServices(text string) []string {
	var services []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "*") && !strings.HasPrefix(line, "An asterisk") {
			services = append(services, line)
		}
	}
	return services
}
func parseFields(text string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return fields
}
func parseEndpoint(text string) (proxyEndpoint, error) {
	fields := parseFields(text)
	if fields["Enabled"] != "Yes" && fields["Enabled"] != "No" {
		return proxyEndpoint{}, errors.New("无法解析系统代理")
	}
	return proxyEndpoint{fields["Enabled"] == "Yes", fields["Server"], fields["Port"], fields["Authenticated Proxy Enabled"] == "1"}, nil
}
func readProxy() (proxySnapshot, error) {
	text, err := networkCommand("-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	result := proxySnapshot{}
	for _, name := range parseServices(text) {
		var s serviceProxy
		for _, part := range []struct {
			flag string
			dst  *proxyEndpoint
		}{{"-getwebproxy", &s.Web}, {"-getsecurewebproxy", &s.Secure}} {
			text, err = networkCommand(part.flag, name)
			if err != nil {
				return nil, err
			}
			*part.dst, err = parseEndpoint(text)
			if err != nil {
				return nil, err
			}
		}
		text, err = networkCommand("-getproxybypassdomains", name)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(strings.TrimSpace(text), "There aren't any") {
			for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
				if line != "" {
					s.Bypass = append(s.Bypass, line)
				}
			}
		}
		text, err = networkCommand("-getautoproxyurl", name)
		if err != nil {
			return nil, err
		}
		fields := parseFields(text)
		if fields["Enabled"] != "Yes" && fields["Enabled"] != "No" {
			return nil, errors.New("无法解析自动代理")
		}
		s.PAC = pacProxy{fields["Enabled"] == "Yes", fields["URL"]}
		result[name] = s
	}
	return result, nil
}
func proxyState(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
func writeProxy(values proxySnapshot) error {
	current, err := readProxy()
	if err != nil {
		return err
	}
	for name, s := range values {
		old, exists := current[name]
		if !exists {
			continue
		}
		for _, part := range []struct {
			flag       string
			value, old proxyEndpoint
		}{{"web", s.Web, old.Web}, {"secureweb", s.Secure, old.Secure}} {
			if reflect.DeepEqual(part.value, part.old) {
				continue
			}
			if part.value.Auth || part.old.Auth {
				return errors.New("无法安全修改需要认证的系统代理")
			}
			if part.value.Server != "" {
				if _, err := networkCommand("-set"+part.flag+"proxy", name, part.value.Server, part.value.Port); err != nil {
					return err
				}
			}
			if _, err := networkCommand("-set"+part.flag+"proxystate", name, proxyState(part.value.Enabled)); err != nil {
				return err
			}
		}
		if !reflect.DeepEqual(s.Bypass, old.Bypass) {
			domains := s.Bypass
			if len(domains) == 0 {
				domains = []string{"Empty"}
			}
			if _, err := networkCommand(append([]string{"-setproxybypassdomains", name}, domains...)...); err != nil {
				return err
			}
		}
		if s.PAC != old.PAC {
			if s.PAC.URL != "" && s.PAC.URL != "(null)" {
				if _, err := networkCommand("-setautoproxyurl", name, s.PAC.URL); err != nil {
					return err
				}
			}
			if _, err := networkCommand("-setautoproxystate", name, proxyState(s.PAC.Enabled)); err != nil {
				return err
			}
		}
	}
	return nil
}
func desiredProxy(before proxySnapshot, port string) proxySnapshot {
	// macOS 的例外列表认 CIDR 和 *.local，不认 Windows 的 <local>
	bypass := []string{"localhost", "127.0.0.0/8", "*.local", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16"}
	values := proxySnapshot{}
	for name, s := range before {
		s.Web = proxyEndpoint{true, "127.0.0.1", port, false}
		s.Secure = s.Web
		s.Bypass = bypass
		s.PAC.Enabled = false
		values[name] = s
	}
	return values
}
func restoreValues(current proxySnapshot, journal proxyJournal) proxySnapshot {
	values := proxySnapshot{}
	for name, s := range current {
		owned, ok := journal.Owned[name]
		before, exists := journal.Before[name]
		if ok && exists {
			if s.Web == owned.Web {
				s.Web = before.Web
			}
			if s.Secure == owned.Secure {
				s.Secure = before.Secure
			}
			if reflect.DeepEqual(s.Bypass, owned.Bypass) {
				s.Bypass = before.Bypass
			}
			if s.PAC == owned.PAC {
				s.PAC = before.PAC
			}
		}
		values[name] = s
	}
	return values
}
func (p *proxyManager) current() (proxySnapshot, error) {
	if p.read != nil {
		return p.read()
	}
	return readProxy()
}
func (p *proxyManager) apply(values proxySnapshot) error {
	if p.write != nil {
		return p.write(values)
	}
	return writeProxy(values)
}
func (p *proxyManager) enable(port string) error {
	if p.active {
		return nil
	}
	if _, err := os.Stat(p.path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("请先停止并恢复代理")
	}
	before, err := p.current()
	if err != nil {
		return fmt.Errorf("无法读取系统代理：%w", err)
	}
	for _, s := range before {
		if s.Web.Auth || s.Secure.Auth {
			return errors.New("无法安全修改需要认证的系统代理")
		}
	}
	journal := proxyJournal{before, desiredProxy(before, port)}
	if err := saveEncrypted(p.path, journal); err != nil {
		return fmt.Errorf("无法保存代理设置：%w", err)
	}
	p.active = true
	if err := p.apply(journal.Owned); err != nil {
		if p.restore() != nil {
			p.active = true
		}
		return fmt.Errorf("系统代理设置失败：%w", err)
	}
	return nil
}
func (p *proxyManager) restore() error {
	var journal proxyJournal
	err := loadEncrypted(p.path, &journal)
	if errors.Is(err, os.ErrNotExist) {
		p.active = false
		return nil
	}
	p.active = true
	if err != nil {
		return fmt.Errorf("无法读取代理恢复记录：%w", err)
	}
	if journal.Before == nil || journal.Owned == nil || len(journal.Before) != len(journal.Owned) {
		return errors.New("代理恢复记录损坏")
	}
	for name := range journal.Before {
		if _, ok := journal.Owned[name]; !ok {
			return errors.New("代理恢复记录损坏")
		}
	}
	current, err := p.current()
	if err != nil {
		return err
	}
	if err := p.apply(restoreValues(current, journal)); err != nil {
		return fmt.Errorf("代理恢复失败，请重试：%w", err)
	}
	if err := os.Remove(p.path); err != nil {
		return err
	}
	p.active = false
	return nil
}

type kernelGroup uintptr

var groups sync.Map

type groupInfo struct {
	dir, path string
	pid       int
}

var groupSequence uintptr
var groupMu sync.Mutex

func newKernelGroup(dir string) (kernelGroup, error) {
	groupMu.Lock()
	defer groupMu.Unlock()
	groupSequence++
	group := kernelGroup(groupSequence)
	groups.Store(group, &groupInfo{dir: dir})
	return group, nil
}
func configureKernel(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// macOS 上内核不盯配置文件，改完配置发 SIGHUP 叫它马上换（--no-reload 时 SIGHUP 照样管用）
const watchConfig = false

// reloadWait 等内核换好配置的上限：准备新配置约 0.1 秒，换监听时端口被占最多重试 3 秒
const reloadWait = 10 * time.Second

func askReload(p *os.Process) error { return p.Signal(syscall.SIGHUP) }
func (g kernelGroup) assign(pid int) error {
	value, ok := groups.Load(g)
	if !ok {
		return errors.New("内核进程组不存在")
	}
	info := value.(*groupInfo)
	info.pid = pid
	info.path = filepath.Join(info.dir, fmt.Sprintf("kernel-%d.pid", pid))
	return atomicFile(info.path, []byte(strconv.Itoa(pid)))
}
func terminateGroup(pid int) {
	if pid <= 1 {
		return
	}
	syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if syscall.Kill(-pid, 0) != nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	syscall.Kill(-pid, syscall.SIGKILL)
}
func (g kernelGroup) kill() {
	if value, ok := groups.Load(g); ok {
		terminateGroup(value.(*groupInfo).pid)
	}
}
func (g kernelGroup) close() {
	if value, ok := groups.LoadAndDelete(g); ok {
		info := value.(*groupInfo)
		if info.path != "" {
			os.Remove(info.path)
		}
	}
}
func recoverKernels(dir string) {
	paths, _ := filepath.Glob(filepath.Join(dir, "kernel-*.pid"))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(string(data))
		if err == nil && pid > 1 && processName(uint32(pid)) == kernelName {
			if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid {
				terminateGroup(pid)
			}
		}
		os.Remove(path)
	}
}
func processName(pid uint32) string {
	out, err := exec.Command("/bin/ps", "-o", "comm=", "-p", strconv.FormatUint(uint64(pid), 10)).Output()
	if err != nil {
		return ""
	}
	return filepath.Base(strings.TrimSpace(string(out)))
}
func portOwner(port string) string {
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return ""
	}
	out, _ := exec.Command("/usr/sbin/lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-Fpc").Output()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "c") {
			return strings.TrimPrefix(line, "c")
		}
	}
	return ""
}
