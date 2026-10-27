package main

import (
	"errors"
	"testing"
)

func TestChooseKernel(t *testing.T) {
	exists := map[string]bool{
		"/Applications/ShoesGUI.app/Contents/Resources/shoes":                          true,
		"/Users/me/A/shoes/outputs/darwin-arm64/ShoesGUI.app/Contents/Resources/shoes": true,
		"/opt/shoes/shoes": true,
	}
	valid := func(p string) error {
		if exists[p] {
			return nil
		}
		return errors.New("missing")
	}
	bundled := func(p string) bool { return p != "/opt/shoes/shoes" && p != "/usr/local/bin/shoes" }
	own := []string{"/Applications/ShoesGUI.app/Contents/MacOS/shoes", "/Applications/ShoesGUI.app/Contents/Resources/shoes"}
	mine := "/Applications/ShoesGUI.app/Contents/Resources/shoes"
	none := func() string { return "" }
	for _, c := range []struct {
		name, saved string
		own         []string
		lookPath    func() string
		want        string
	}{
		{"first launch", "", own, none, mine},
		{"saved is this copy's", mine, own, none, mine},
		{"saved is another copy's (dev build)", "/Users/me/A/shoes/outputs/darwin-arm64/ShoesGUI.app/Contents/Resources/shoes", own, none, mine},
		{"saved from the ejected dmg", "/Volumes/ShoesGUI/ShoesGUI.app/Contents/Resources/shoes", own, none, mine},
		{"user's own kernel kept", "/opt/shoes/shoes", own, none, "/opt/shoes/shoes"},
		{"another copy's kernel, none bundled here", "/Users/me/A/shoes/outputs/darwin-arm64/ShoesGUI.app/Contents/Resources/shoes", nil, none, "/Users/me/A/shoes/outputs/darwin-arm64/ShoesGUI.app/Contents/Resources/shoes"},
		{"gone, found in PATH", "/Volumes/ShoesGUI/ShoesGUI.app/Contents/Resources/shoes", nil, func() string { return "/usr/local/bin/shoes" }, "/usr/local/bin/shoes"},
		{"gone, nowhere else", "/Volumes/ShoesGUI/ShoesGUI.app/Contents/Resources/shoes", nil, none, "/Volumes/ShoesGUI/ShoesGUI.app/Contents/Resources/shoes"},
	} {
		if got := chooseKernel(c.saved, c.own, valid, bundled, c.lookPath); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
