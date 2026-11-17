package main

import (
	"os"
	"testing"
)

func TestParseKernelVersion(t *testing.T) {
	for in, want := range map[string]string{"shoes 0.3.4\n": "0.3.4", "": "", "shoes": "", "other 1.0": ""} {
		if got := parseKernelVersion(in); got != want {
			t.Fatalf("parseKernelVersion(%q) = %q, want %q", in, got, want)
		}
	}
	if k := os.Getenv("SHOES_TEST_KERNEL"); k != "" && kernelVersion(k) == "" {
		t.Fatalf("kernelVersion(%s) is empty", k)
	}
	if kernelVersion("/nonexistent/shoes") != "" {
		t.Fatal("missing kernel should give empty version")
	}
}
