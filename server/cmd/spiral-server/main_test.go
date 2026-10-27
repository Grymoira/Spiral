package main

import (
	"net"
	"strings"
	"testing"
)

func TestListenLoopbackRejectsNonLoopback(t *testing.T) {
	for _, address := range []string{
		"0.0.0.0:0", "[::]:0", "192.0.2.10:0", "example.com:0", ":0", "not-a-host-port",
	} {
		t.Run(address, func(t *testing.T) {
			listener, err := listenLoopback(address)
			if err == nil {
				listener.Close()
				t.Fatal("non-loopback or malformed address accepted")
			}
		})
	}
}

func TestListenLoopbackBindsLoopback(t *testing.T) {
	for _, address := range []string{"127.0.0.1:0", "localhost:0"} {
		t.Run(address, func(t *testing.T) {
			listener, err := listenLoopback(address)
			if err != nil {
				if strings.HasPrefix(address, "localhost:") {
					t.Skipf("localhost hostname not configured: %v", err)
				}
				t.Fatal(err)
			}
			defer listener.Close()
			addr, ok := listener.Addr().(*net.TCPAddr)
			if !ok || !addr.IP.IsLoopback() {
				t.Fatalf("unexpected bound address: %v", listener.Addr())
			}
		})
	}
}

func TestLogLevelContract(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "WARN"} {
		if _, err := logLevel(level); err != nil {
			t.Fatalf("rejected supported level %q: %v", level, err)
		}
	}
	for _, level := range []string{"trace", "quiet", "", "off"} {
		if _, err := logLevel(level); err == nil {
			t.Fatalf("accepted undocumented level %q", level)
		}
	}
}
