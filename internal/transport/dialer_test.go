package transport

import (
	"context"
	"net"
	"testing"
)

func TestDirectDialer(t *testing.T) {
	// Start a local TCP listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Write([]byte("OK"))
		conn.Close()
	}()

	d := Direct()
	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 2)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "OK" {
		t.Errorf("got %q, want %q", string(buf[:n]), "OK")
	}
}

func TestSOCKS5InvalidProxy(t *testing.T) {
	// SOCKS5 with an unreachable proxy should fail on dial, not on construction.
	d, err := SOCKS5("127.0.0.1:1", nil)
	if err != nil {
		t.Fatalf("SOCKS5 constructor should not fail: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*1e6) // 100ms
	defer cancel()

	_, err = d.DialContext(ctx, "tcp", "example.com:80")
	if err == nil {
		t.Error("expected dial to fail with unreachable proxy")
	}
}
