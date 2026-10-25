package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestProxyTransactionWithFakeRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.dpapi")
	before := desiredProxy("9000")
	before["AutoConfigURL"] = stringReg("http://previous.invalid/proxy.pac")
	before["ProxyOverride"] = regValue{}
	current := before
	writes := 0
	p := proxyManager{path: path, read: func() (proxySnapshot, error) { return current, nil }, write: func(values proxySnapshot) error {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("registry write before durable recovery journal")
		}
		current = values
		writes++
		return nil
	}}
	if err := p.enable("10808"); err != nil || !p.active {
		t.Fatal("enable transaction failed")
	}
	var journal proxyJournal
	if err := loadEncrypted(path, &journal); err != nil || !reflect.DeepEqual(journal.Before, before) {
		t.Fatal("snapshot not durable")
	}
	if err := p.restore(); err != nil || p.active || !reflect.DeepEqual(current, before) || writes != 2 {
		t.Fatal("exact restore failed")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("successful recovery journal not removed")
	}
	if err := p.restore(); err != nil || writes != 2 {
		t.Fatal("restore without journal touched registry")
	}
}

func TestProxyPartialWriteRollbackAndRecoveryRetry(t *testing.T) {
	before := desiredProxy("9000")
	current := before
	attempts := 0
	failWrites := true
	p := proxyManager{path: filepath.Join(t.TempDir(), "recovery.dpapi"), read: func() (proxySnapshot, error) { return current, nil }, write: func(values proxySnapshot) error {
		attempts++
		if failWrites {
			if attempts == 1 {
				current = desiredProxy("10808")
			}
			return errors.New("simulated registry failure")
		}
		current = values
		return nil
	}}
	if p.enable("10808") == nil || !p.active {
		t.Fatal("failed rollback must retain active recovery state")
	}
	if _, err := os.Stat(p.path); err != nil {
		t.Fatal("failed rollback lost recovery journal")
	}
	failWrites = false
	if err := p.restore(); err != nil || p.active || !reflect.DeepEqual(current, before) {
		t.Fatal("recovery retry failed")
	}
}

func TestProxyJournalFailureDoesNotTouchRegistry(t *testing.T) {
	writes := 0
	p := proxyManager{path: filepath.Join(t.TempDir(), "missing", "recovery.dpapi"), read: func() (proxySnapshot, error) { return desiredProxy("9000"), nil }, write: func(proxySnapshot) error { writes++; return nil }}
	if p.enable("10808") == nil || writes != 0 || p.active {
		t.Fatal("journal failure did not prevent mutation")
	}
}
