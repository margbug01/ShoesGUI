package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMain(tests *testing.M) {
	encryptionKey = func() ([]byte, error) { return bytes.Repeat([]byte{42}, 32), nil }
	networkCommand = func(args ...string) (string, error) { panic("真实系统代理命令被测试禁止") }
	os.Exit(tests.Run())
}
func TestDarwinCipher(t *testing.T) {
	plain := []byte("secret")
	encrypted, err := protect(plain, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := protect(plain, false)
	if err != nil || bytes.Equal(encrypted, second) {
		t.Fatal("nonce reused")
	}
	decoded, err := protect(encrypted, true)
	if err != nil || !bytes.Equal(plain, decoded) {
		t.Fatal("roundtrip", err)
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err := protect(encrypted, true); err == nil {
		t.Fatal("tampering accepted")
	}
	old := encryptionKey
	defer func() { encryptionKey = old }()
	encryptionKey = func() ([]byte, error) { return nil, errors.New("keychain locked") }
	if _, err := protect(plain, false); err == nil {
		t.Fatal("missing key accepted")
	}
}
func TestDarwinParsing(t *testing.T) {
	if got := parseServices("An asterisk (*) denotes that a network service is disabled.\nWi-Fi\n*Disabled\nUSB LAN\n"); !reflect.DeepEqual(got, []string{"Wi-Fi", "USB LAN"}) {
		t.Fatal(got)
	}
	endpoint, err := parseEndpoint("Enabled: Yes\nServer: localhost\nPort: 8080\nAuthenticated Proxy Enabled: 0\n")
	if err != nil || endpoint != (proxyEndpoint{true, "localhost", "8080", false}) {
		t.Fatal(endpoint, err)
	}
	if _, err := parseEndpoint("unexpected"); err == nil {
		t.Fatal("malformed output accepted")
	}
}
func TestDarwinSnapshot(t *testing.T) {
	old := networkCommand
	defer func() { networkCommand = old }()
	networkCommand = func(args ...string) (string, error) {
		switch args[0] {
		case "-listallnetworkservices":
			return "An asterisk (*) denotes disabled.\nWi-Fi\n*Disabled\n", nil
		case "-getwebproxy", "-getsecurewebproxy":
			return "Enabled: No\nServer: old\nPort: 80\nAuthenticated Proxy Enabled: 0\n", nil
		case "-getproxybypassdomains":
			return "localhost\n*.example.com\n", nil
		case "-getautoproxyurl":
			return "URL: https://example.com/proxy.pac\nEnabled: Yes\n", nil
		default:
			t.Fatalf("unexpected command: %v", args)
			return "", nil
		}
	}
	snapshot, err := readProxy()
	if err != nil || len(snapshot) != 1 || !snapshot["Wi-Fi"].PAC.Enabled || len(snapshot["Wi-Fi"].Bypass) != 2 {
		t.Fatal(snapshot, err)
	}
}
func TestDarwinProxyJournal(t *testing.T) {
	before := proxySnapshot{"Wi-Fi": {Web: proxyEndpoint{true, "old", "80", false}, PAC: pacProxy{true, "https://old/pac"}, Bypass: []string{"old"}}}
	current := before
	path := filepath.Join(t.TempDir(), "journal")
	writes := 0
	manager := proxyManager{path: path, read: func() (proxySnapshot, error) { return current, nil }, write: func(values proxySnapshot) error {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("write before journal", err)
		}
		writes++
		current = values
		return nil
	}}
	if err := manager.enable("10808"); err != nil {
		t.Fatal(err)
	}
	service := current["Wi-Fi"]
	service.Web.Server = "external"
	current["Wi-Fi"] = service
	restarted := proxyManager{path: path, read: manager.read, write: manager.write}
	if err := restarted.restore(); err != nil {
		t.Fatal(err)
	}
	restored := current["Wi-Fi"]
	if restored.Web.Server != "external" || restored.PAC != before["Wi-Fi"].PAC || !reflect.DeepEqual(restored.Bypass, before["Wi-Fi"].Bypass) {
		t.Fatal(restored)
	}
	if err := restarted.restore(); err != nil || writes != 2 {
		t.Fatal("missing journal wrote proxy")
	}
}
func TestDarwinProxyFailureRecovery(t *testing.T) {
	before := proxySnapshot{"Wi-Fi": {}}
	current := before
	fail := true
	manager := proxyManager{path: filepath.Join(t.TempDir(), "journal"), read: func() (proxySnapshot, error) { return current, nil }, write: func(values proxySnapshot) error {
		if fail {
			current = values
			return errors.New("write failed")
		}
		current = values
		return nil
	}}
	if manager.enable("10808") == nil || !manager.active {
		t.Fatal("failure lost recovery")
	}
	fail = false
	if err := manager.restore(); err != nil || manager.active || !reflect.DeepEqual(current, before) {
		t.Fatal("retry", current, err)
	}
	manager.path = filepath.Join(t.TempDir(), "missing", "journal")
	manager.write = func(proxySnapshot) error { t.Fatal("write without journal"); return nil }
	if manager.enable("10808") == nil {
		t.Fatal("missing directory accepted")
	}
}
func TestDarwinKernelValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shoes")
	if validKernel("shoes") == nil {
		t.Fatal("relative path accepted")
	}
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if validKernel(path) == nil {
		t.Fatal("nonexecutable accepted")
	}
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := validKernel(path); err != nil {
		t.Fatal(err)
	}
}

func TestDarwinBundledKernel(t *testing.T) {
	for path, want := range map[string]bool{
		"/Applications/ShoesGUI.app/Contents/Resources/shoes":     true,
		"/Volumes/ShoesGUI/ShoesGUI.app/Contents/Resources/shoes": true,
		"/Applications/ShoesGUI.app/Contents/MacOS/shoes":         false,
		"/usr/local/bin/shoes":                                    false,
		"/opt/Resources/shoes":                                    false,
	} {
		if got := bundledKernel(path); got != want {
			t.Errorf("%s: got %v", path, got)
		}
	}
}
