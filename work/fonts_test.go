package main

import (
	"runtime"
	"testing"
)

func TestPlatformFont(t *testing.T) {
	want := "MiSans, system-ui"
	if runtime.GOOS == "darwin" {
		want = "system-ui"
	}
	if appFont != want {
		t.Fatalf("font = %q, want %q", appFont, want)
	}
	registerFonts()
}
