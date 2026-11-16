package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 菜单栏网速要定长：字符数不变、单位不变、数字里不混普通空格（普通空格比数字窄）
func TestTraySpeedFixedWidth(t *testing.T) {
	want := ""
	for _, r := range []rate{{}, {up: 512, down: 10 << 10}, {up: 3 << 20, down: 87 << 20}, {up: 120 << 20, down: 5 << 30}} {
		got := traySpeed(r)
		if want == "" {
			want = got
		}
		if utf8.RuneCountInString(got) != utf8.RuneCountInString(want) || strings.Count(got, " ") != 2 || !strings.HasSuffix(got, " MB/s") {
			t.Fatalf("traySpeed(%v) = %q, want same shape as %q", r, got, want)
		}
	}
	if got := traySpeed(rate{up: 1 << 20, down: 5 << 30}); got != "↑\u2007\u20071.00 ↓999.99 MB/s" {
		t.Fatalf("got %q", got)
	}
}
