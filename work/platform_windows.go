package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func dataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "ShoesGUI")
	return dir, os.MkdirAll(dir, 0700)
}

func validKernel(path string) error {
	if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".exe") {
		return errors.New("请选择 shoes Windows .exe 的绝对路径")
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return errors.New("内核文件不存在")
	}
	return nil
}

func recoverKernels(dir string) {}

const kernelName = "shoes.exe"
const kernelHint = "一般会自动找到，和 ShoesGUI.exe 放在一起就行"

var kernelExtensions = []string{"exe"}

type kernelGroup windows.Handle

func newKernelGroup(dir string) (kernelGroup, error) { h, err := newJob(); return kernelGroup(h), err }
func configureKernel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
func (g kernelGroup) assign(pid int) error { return assignJob(windows.Handle(g), pid) }
func (g kernelGroup) kill()                { windows.TerminateJobObject(windows.Handle(g), 1) }
func (g kernelGroup) close()               { windows.CloseHandle(windows.Handle(g)) }

// bundledKernel 旁边就是 ShoesGUI.exe，说明是某一份 Shoes 自带的内核
func bundledKernel(path string) bool {
	if !strings.EqualFold(filepath.Base(path), kernelName) {
		return false
	}
	_, err := os.Stat(filepath.Join(filepath.Dir(path), "ShoesGUI.exe"))
	return err == nil
}

func kernelCandidates(executable string) []string {
	return []string{filepath.Join(filepath.Dir(executable), kernelName)}
}

func protect(data []byte, decrypt bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("加密数据为空")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, errors.New("无法处理账户加密数据")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

func atomicFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".shoes-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	from, _ := windows.UTF16PtrFromString(name)
	to, _ := windows.UTF16PtrFromString(path)
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
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

const proxyKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

var proxyNames = []string{"ProxyEnable", "ProxyServer", "ProxyOverride", "AutoConfigURL"}

type regValue struct {
	Present bool
	Kind    uint32
	Data    []byte
}
type proxySnapshot map[string]regValue
type proxyJournal struct {
	Before proxySnapshot
	Owned  proxySnapshot
}
type proxyManager struct {
	path   string
	active bool
	read   func() (proxySnapshot, error)
	write  func(proxySnapshot) error
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

func readProxy() (proxySnapshot, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, proxyKey, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer key.Close()
	result := proxySnapshot{}
	for _, name := range proxyNames {
		size, kind, err := key.GetValue(name, nil)
		if errors.Is(err, registry.ErrNotExist) {
			result[name] = regValue{}
			continue
		}
		if err != nil && !errors.Is(err, syscall.ERROR_MORE_DATA) {
			return nil, err
		}
		data := make([]byte, size)
		_, kind, err = key.GetValue(name, data)
		if err != nil {
			return nil, err
		}
		result[name] = regValue{true, kind, data}
	}
	return result, nil
}

func stringReg(s string) regValue {
	chars, _ := windows.UTF16FromString(s)
	data := append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(&chars[0])), len(chars)*2)...)
	return regValue{true, registry.SZ, data}
}

func desiredProxy(port string) proxySnapshot {
	return proxySnapshot{
		"ProxyEnable":   {true, registry.DWORD, []byte{1, 0, 0, 0}},
		"ProxyServer":   stringReg("http=127.0.0.1:" + port + ";https=127.0.0.1:" + port),
		"ProxyOverride": stringReg(proxyBypass), "AutoConfigURL": {},
	}
}

// 局域网和本机不交给代理，和 privateRanges 对应
var proxyBypass = func() string {
	list := []string{"localhost", "127.*", "10.*", "192.168.*", "169.254.*"}
	for i := 16; i <= 31; i++ {
		list = append(list, fmt.Sprintf("172.%d.*", i))
	}
	return strings.Join(append(list, "<local>"), ";")
}()

var regSet = windows.NewLazySystemDLL("advapi32.dll").NewProc("RegSetValueExW")
var internetOption = windows.NewLazySystemDLL("wininet.dll").NewProc("InternetSetOptionW")

func writeProxy(values proxySnapshot) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, proxyKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	for _, name := range proxyNames {
		v := values[name]
		if !v.Present {
			if err := key.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
				return err
			}
		} else {
			keyName, _ := windows.UTF16PtrFromString(name)
			var ptr uintptr
			if len(v.Data) > 0 {
				ptr = uintptr(unsafe.Pointer(&v.Data[0]))
			}
			code, _, _ := regSet.Call(uintptr(key), uintptr(unsafe.Pointer(keyName)), 0, uintptr(v.Kind), ptr, uintptr(len(v.Data)))
			if code != 0 {
				return syscall.Errno(code)
			}
		}
	}
	internetOption.Call(0, 39, 0, 0)
	internetOption.Call(0, 37, 0, 0)
	return nil
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
		return errors.New("无法读取系统代理")
	}
	journal := proxyJournal{Before: before, Owned: desiredProxy(port)}
	if err := saveEncrypted(p.path, journal); err != nil {
		return errors.New("无法保存代理设置")
	}
	p.active = true
	if err := p.apply(journal.Owned); err != nil {
		if p.apply(before) == nil && os.Remove(p.path) == nil {
			p.active = false
		}
		return errors.New("系统代理设置失败")
	}
	p.active = true
	return nil
}

func restoreValues(current proxySnapshot, j proxyJournal) proxySnapshot {
	values := proxySnapshot{}
	for _, name := range proxyNames {
		if reflect.DeepEqual(current[name], j.Owned[name]) {
			values[name] = j.Before[name]
		} else {
			values[name] = current[name]
		}
	}
	return values
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
		return errors.New("无法读取代理恢复记录")
	}
	if len(journal.Before) != len(proxyNames) || len(journal.Owned) != len(proxyNames) {
		return errors.New("代理恢复记录损坏")
	}
	for _, name := range proxyNames {
		if _, ok := journal.Before[name]; !ok {
			return errors.New("代理恢复记录损坏")
		}
		if _, ok := journal.Owned[name]; !ok {
			return errors.New("代理恢复记录损坏")
		}
	}
	current, err := p.current()
	if err != nil {
		return errors.New("无法读取系统代理")
	}
	if err := p.apply(restoreValues(current, journal)); err != nil {
		return errors.New("代理恢复失败，请重试")
	}
	if err := os.Remove(p.path); err != nil {
		return errors.New("无法清理代理恢复记录")
	}
	p.active = false
	return nil
}

func newJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

func assignJob(job windows.Handle, pid int) error {
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	return windows.AssignProcessToJobObject(job, process)
}

var getExtendedTcpTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

// portOwner 找在 port 上监听的进程名（IPv4），找不到返回空
func portOwner(port string) string {
	want, err := strconv.Atoi(port)
	if err != nil {
		return ""
	}
	var size uint32
	getExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, windows.AF_INET, 3, 0) // 3 = TCP_TABLE_OWNER_PID_LISTENER
	if size == 0 {
		return ""
	}
	buf := make([]byte, size)
	if r, _, _ := getExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, windows.AF_INET, 3, 0); r != 0 {
		return ""
	}
	n := binary.LittleEndian.Uint32(buf)
	for i := uint32(0); i < n; i++ {
		row := buf[4+i*24:]
		if len(row) < 24 {
			break
		}
		// MIB_TCPROW_OWNER_PID：state, localAddr, localPort（网络字节序）, remoteAddr, remotePort, pid
		if int(binary.BigEndian.Uint16(row[8:10])) == want {
			return processName(binary.LittleEndian.Uint32(row[20:24]))
		}
	}
	return ""
}

func processName(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return fmt.Sprintf("进程 %d", pid)
	}
	defer windows.CloseHandle(h)
	name := make([]uint16, windows.MAX_PATH)
	size := uint32(len(name))
	if windows.QueryFullProcessImageName(h, 0, &name[0], &size) != nil {
		return fmt.Sprintf("进程 %d", pid)
	}
	return filepath.Base(windows.UTF16ToString(name[:size]))
}
