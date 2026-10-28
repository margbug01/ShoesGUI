package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsBundledKernel(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	for _, f := range []string{filepath.Join(dir, "ShoesGUI.exe"), filepath.Join(dir, "shoes.exe"), filepath.Join(other, "shoes.exe")} {
		os.WriteFile(f, []byte("x"), 0o644)
	}
	if !bundledKernel(filepath.Join(dir, "shoes.exe")) || !bundledKernel(filepath.Join(dir, "SHOES.EXE")) {
		t.Fatal("kernel next to ShoesGUI.exe not recognised")
	}
	if err := os.Rename(filepath.Join(dir, "ShoesGUI.exe"), filepath.Join(dir, "MyShoes.exe")); err != nil {
		t.Fatal(err)
	}
	if !bundledKernel(filepath.Join(dir, "shoes.exe")) {
		t.Fatal("kernel next to MyShoes.exe not recognised")
	}
	if bundledKernel(filepath.Join(other, "shoes.exe")) {
		t.Fatal("a kernel the user put elsewhere is not a bundled one")
	}
}
