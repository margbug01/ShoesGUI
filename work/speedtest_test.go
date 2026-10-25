package main

import (
	"encoding/json"
	"net"
	"strconv"
	"sync"
	"testing"
)

func TestTcping(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	node := Node{Host: "127.0.0.1", Port: port}
	if ms := tcping(node); ms < 1 {
		t.Fatalf("open port: got %d", ms)
	}
	listener.Close()
	if ms := tcping(node); ms != -1 {
		t.Fatalf("closed port: got %d", ms)
	}
}

func TestEachNodeReportsEveryIndexOnce(t *testing.T) {
	var mu sync.Mutex
	seen := map[int]int{}
	eachNode(150, func(i int) int { return i + 1 }, func(i, ms int) {
		mu.Lock()
		defer mu.Unlock()
		if ms != i+1 {
			t.Errorf("index %d reported %d", i, ms)
		}
		seen[i]++
	})
	if len(seen) != 150 {
		t.Fatalf("got %d results", len(seen))
	}
	for i, n := range seen {
		if n != 1 {
			t.Fatalf("index %d reported %d times", i, n)
		}
	}
}

func TestPingConfigSkipsUnassignedPorts(t *testing.T) {
	nodes := []Node{sampleNode("Trojan"), sampleNode("SOCKS5"), sampleNode("VLESS")}
	var entries []map[string]any
	if err := json.Unmarshal(pingConfig(nodes, []int{20001, 0, 20003}), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0]["address"] != "127.0.0.1:20001" || entries[1]["address"] != "127.0.0.1:20003" {
		t.Fatal("listeners do not match assigned ports")
	}
	rule := entries[0]["rules"].([]any)[0].(map[string]any)
	if rule["client_chain"].(map[string]any)["address"] != "127.0.0.1:18443" {
		t.Fatal("listener not wired straight to its node")
	}
}
