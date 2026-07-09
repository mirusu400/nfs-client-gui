package v3_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/mirusu400/nfsprobe/internal/nfs"
	v3 "github.com/mirusu400/nfsprobe/internal/nfs/v3"
	"github.com/mirusu400/nfsprobe/internal/rpc"
	"github.com/mirusu400/nfsprobe/internal/transport"
)

// minimalSOCKS5Server is a basic SOCKS5 proxy (no auth, CONNECT only).
// It records all connections made through it for verification.
type minimalSOCKS5Server struct {
	ln          net.Listener
	connections []string // "host:port" of each CONNECT request
}

func startSOCKS5Proxy(t *testing.T) *minimalSOCKS5Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	s := &minimalSOCKS5Server{ln: ln}
	go s.serve()
	t.Logf("SOCKS5 proxy on %s", ln.Addr())
	return s
}

func (s *minimalSOCKS5Server) addr() string { return s.ln.Addr().String() }
func (s *minimalSOCKS5Server) close()       { s.ln.Close() }

func (s *minimalSOCKS5Server) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleClient(conn)
	}
}

func (s *minimalSOCKS5Server) handleClient(client net.Conn) {
	defer client.Close()

	// SOCKS5 greeting: client sends version + method count + methods
	buf := make([]byte, 258)
	n, err := client.Read(buf)
	if err != nil || n < 3 || buf[0] != 0x05 {
		return
	}

	// Reply: no auth required
	client.Write([]byte{0x05, 0x00})

	// CONNECT request
	n, err = client.Read(buf)
	if err != nil || n < 7 || buf[0] != 0x05 || buf[1] != 0x01 {
		return
	}

	var targetAddr string
	switch buf[3] {
	case 0x01: // IPv4
		if n < 10 {
			return
		}
		ip := net.IP(buf[4:8])
		port := int(buf[8])<<8 | int(buf[9])
		targetAddr = fmt.Sprintf("%s:%d", ip, port)
	case 0x03: // Domain name
		domainLen := int(buf[4])
		if n < 5+domainLen+2 {
			return
		}
		domain := string(buf[5 : 5+domainLen])
		port := int(buf[5+domainLen])<<8 | int(buf[5+domainLen+1])
		targetAddr = fmt.Sprintf("%s:%d", domain, port)
	default:
		return
	}

	s.connections = append(s.connections, targetAddr)

	// Connect to the target
	target, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if err != nil {
		// Reply: connection refused
		client.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer target.Close()

	// Reply: success (bound addr = 0.0.0.0:0)
	client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// Bidirectional relay
	done := make(chan struct{}, 2)
	go func() { io.Copy(target, client); done <- struct{}{} }()
	go func() { io.Copy(client, target); done <- struct{}{} }()
	<-done
}

func TestProxy_V3_ThroughSOCKS5(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping proxy test in short mode")
	}

	// 1. Start NFS server (reuse from integration test)
	nfsLn, nfsPort := startTestNFSServer(t)
	defer nfsLn.Close()

	// 2. Start SOCKS5 proxy
	proxy := startSOCKS5Proxy(t)
	defer proxy.close()

	time.Sleep(100 * time.Millisecond)

	// 3. Create a SOCKS5 dialer pointing at our proxy
	dialer, err := transport.SOCKS5(proxy.addr(), nil)
	if err != nil {
		t.Fatalf("SOCKS5 dialer: %v", err)
	}

	// 4. Create v3 client that goes through the proxy
	client := v3.New(dialer, "127.0.0.1", rpc.DefaultAuthSys())
	defer client.Close()

	// Override ports (no portmapper in test)
	client.SetPortOverrides(uint32(nfsPort), uint32(nfsPort))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 5. Mount through proxy
	rootFH, err := client.Mount(ctx, "/")
	if err != nil {
		t.Fatalf("Mount via SOCKS5: %v", err)
	}
	t.Logf("Mounted via SOCKS5, root handle: %x", []byte(rootFH)[:8])

	// 6. ReadDir through proxy
	entries, err := client.ReadDir(ctx, rootFH)
	if err != nil {
		t.Fatalf("ReadDir via SOCKS5: %v", err)
	}
	t.Logf("ReadDir via SOCKS5: %d entries", len(entries))

	var helloFH nfs.FileHandle
	for _, e := range entries {
		t.Logf("  %s (type=%s)", e.Name, e.Attr.Type)
		if e.Name == "hello.txt" {
			helloFH = e.FH
		}
	}

	if helloFH == nil {
		t.Fatal("hello.txt not found")
	}

	// 7. Read file through proxy
	data, err := client.Read(ctx, helloFH, 0, 1024)
	if err != nil {
		t.Fatalf("Read via SOCKS5: %v", err)
	}

	expected := "hello from nfsprobe test!"
	if string(data) != expected {
		t.Errorf("got %q, want %q", string(data), expected)
	}
	t.Logf("Read via SOCKS5: %q", string(data))

	// 8. Verify traffic went through the proxy
	if len(proxy.connections) == 0 {
		t.Error("no connections went through the SOCKS5 proxy!")
	} else {
		t.Logf("Proxy saw %d connections:", len(proxy.connections))
		for _, c := range proxy.connections {
			t.Logf("  CONNECT %s", c)
		}
	}
}
