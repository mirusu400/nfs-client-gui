package v3_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	gonfs "github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"

	"github.com/mirusu400/nfs-client-gui/internal/nfs"
	v3 "github.com/mirusu400/nfs-client-gui/internal/nfs/v3"
	"github.com/mirusu400/nfs-client-gui/internal/rpc"
	"github.com/mirusu400/nfs-client-gui/internal/transport"
)

// startTestNFSServer starts a pure-Go NFSv3 server with an in-memory filesystem.
// Returns the listener (caller must close) and its port.
func startTestNFSServer(t *testing.T) (net.Listener, int) {
	t.Helper()

	mem := memfs.New()

	// Create test files.
	f, err := mem.Create("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("hello from nfs-client-gui test!"))
	f.Close()

	if err := mem.MkdirAll("subdir", 0755); err != nil {
		t.Fatal(err)
	}
	f2, err := mem.Create("subdir/nested.txt")
	if err != nil {
		t.Fatal(err)
	}
	f2.Write([]byte("nested content"))
	f2.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	handler := nfshelper.NewNullAuthHandler(mem)
	cacheHelper := nfshelper.NewCachingHandler(handler, 256)

	go func() {
		gonfs.Serve(ln, cacheHelper)
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	t.Logf("NFS test server on port %d", port)
	return ln, port
}

// startMockPortmapper starts a minimal portmapper that responds to PMAPPROC_GETPORT
// by always returning the given target port, and PMAPPROC_DUMP with an empty list.
func startMockPortmapper(t *testing.T, targetPort int) (net.Listener, int) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handlePortmapConn(conn, uint32(targetPort))
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	t.Logf("Mock portmapper on port %d", port)
	return ln, port
}

func handlePortmapConn(conn net.Conn, targetPort uint32) {
	defer conn.Close()

	for {
		// Read record fragment header.
		var hdr uint32
		if err := binary.Read(conn, binary.BigEndian, &hdr); err != nil {
			return
		}
		length := hdr & 0x7FFFFFFF
		body := make([]byte, length)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}

		if len(body) < 24 {
			return
		}

		xid := binary.BigEndian.Uint32(body[0:4])
		// msgType := binary.BigEndian.Uint32(body[4:8])
		// rpcVers := binary.BigEndian.Uint32(body[8:12])
		// prog := binary.BigEndian.Uint32(body[12:16])
		// vers := binary.BigEndian.Uint32(body[16:20])
		proc := binary.BigEndian.Uint32(body[20:24])

		var replyBody []byte

		switch proc {
		case 3: // PMAPPROC_GETPORT
			// Reply with the target port.
			replyBody = makeRPCReply(xid, func(buf []byte) []byte {
				b := make([]byte, 4)
				binary.BigEndian.PutUint32(b, targetPort)
				return append(buf, b...)
			})
		case 4: // PMAPPROC_DUMP
			// Reply with empty list.
			replyBody = makeRPCReply(xid, func(buf []byte) []byte {
				b := make([]byte, 4)
				binary.BigEndian.PutUint32(b, 0) // no more entries
				return append(buf, b...)
			})
		default:
			// NULL or unknown: empty success reply.
			replyBody = makeRPCReply(xid, nil)
		}

		// Write record fragment.
		fragHdr := uint32(len(replyBody)) | (1 << 31)
		binary.Write(conn, binary.BigEndian, fragHdr)
		conn.Write(replyBody)
	}
}

func makeRPCReply(xid uint32, writeResult func([]byte) []byte) []byte {
	buf := make([]byte, 24)
	binary.BigEndian.PutUint32(buf[0:4], xid)   // xid
	binary.BigEndian.PutUint32(buf[4:8], 1)      // reply
	binary.BigEndian.PutUint32(buf[8:12], 0)     // accepted
	binary.BigEndian.PutUint32(buf[12:16], 0)    // verifier flavor = AUTH_NONE
	binary.BigEndian.PutUint32(buf[16:20], 0)    // verifier length = 0
	binary.BigEndian.PutUint32(buf[20:24], 0)    // accept status = success
	if writeResult != nil {
		buf = writeResult(buf)
	}
	return buf
}

func TestIntegration_V3_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Start servers.
	nfsLn, nfsPort := startTestNFSServer(t)
	defer nfsLn.Close()

	pmapLn, pmapPort := startMockPortmapper(t, nfsPort)
	defer pmapLn.Close()

	// Give servers a moment.
	time.Sleep(100 * time.Millisecond)

	// We need our portmapper to use port pmapPort, but our code hardcodes 111.
	// Workaround: connect to the NFS server directly by overriding the host format.
	// Actually, let's test at a lower level first — use MountClient + direct RPC.

	// Test 1: Portmapper works.
	t.Run("Portmapper_GetPort", func(t *testing.T) {
		// Create a portmapper pointed at our mock.
		pm := rpc.NewClient(transport.Direct(), fmt.Sprintf("127.0.0.1:%d", pmapPort), nil)
		defer pm.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		reply, err := pm.Call(ctx, 100000, 2, 3, // portmapper prog, vers=2, proc=GETPORT
			func(w *rpc.XDRWriter) {
				w.WriteUint32(100003) // nfs prog
				w.WriteUint32(3)      // nfs vers
				w.WriteUint32(6)      // TCP
				w.WriteUint32(0)      // port (ignored)
			})
		if err != nil {
			t.Fatalf("GETPORT call failed: %v", err)
		}

		r := rpc.NewXDRReader(reply)
		port, err := r.ReadUint32()
		if err != nil {
			t.Fatalf("parse port: %v", err)
		}
		if port != uint32(nfsPort) {
			t.Errorf("got port %d, want %d", port, nfsPort)
		}
	})

	// Test 2: Direct NFS MOUNT + READDIR via our v3 client.
	// Since our client uses portmapper (hardcoded to :111), we test the lower-level
	// RPC client directly talking to the go-nfs server (which handles MOUNT+NFS on same port).
	t.Run("NFS_Mount_And_ReadDir", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		nfsAddr := fmt.Sprintf("127.0.0.1:%d", nfsPort)

		// Mount via RPC (MOUNT program = 100005, version = 3, proc MNT = 1).
		mountClient := rpc.NewClient(transport.Direct(), nfsAddr, rpc.DefaultAuthSys())
		defer mountClient.Close()

		reply, err := mountClient.Call(ctx, 100005, 3, 1, func(w *rpc.XDRWriter) {
			w.WriteString("/")
		})
		if err != nil {
			t.Fatalf("MOUNT call failed: %v", err)
		}

		r := rpc.NewXDRReader(reply)
		status, _ := r.ReadUint32()
		if status != 0 {
			t.Fatalf("MOUNT status: %d", status)
		}
		rootFH, err := r.ReadOpaque()
		if err != nil {
			t.Fatalf("read root FH: %v", err)
		}
		t.Logf("Root FH: %x (len=%d)", rootFH, len(rootFH))

		// Now use our NFS v3 RPC client to do READDIRPLUS.
		nfsClient := rpc.NewClient(transport.Direct(), nfsAddr, rpc.DefaultAuthSys())
		defer nfsClient.Close()

		reply, err = nfsClient.Call(ctx, 100003, 3, 17, // READDIRPLUS
			func(w *rpc.XDRWriter) {
				w.WriteOpaque(rootFH)           // dir handle
				w.WriteUint64(0)                 // cookie
				w.WriteFixedOpaque(make([]byte, 8)) // cookieverf
				w.WriteUint32(4096)              // dircount
				w.WriteUint32(32768)             // maxcount
			})
		if err != nil {
			t.Fatalf("READDIRPLUS call failed: %v", err)
		}

		rdR := rpc.NewXDRReader(reply)
		rdStatus, _ := rdR.ReadUint32()
		if rdStatus != 0 {
			t.Fatalf("READDIRPLUS status: %d", rdStatus)
		}
		t.Log("READDIRPLUS succeeded")

		// Skip post_op_attr.
		hasAttr, _ := rdR.ReadBool()
		if hasAttr {
			// fattr3 is 84 bytes (21 uint32s).
			rdR.Skip(84)
		}

		// Read cookieverf.
		rdR.ReadFixedOpaque(8)

		// Read entries.
		var names []string
		for {
			hasEntry, err := rdR.ReadBool()
			if err != nil {
				t.Fatalf("read has_entry: %v", err)
			}
			if !hasEntry {
				break
			}
			// fileid
			rdR.ReadUint64()
			// name
			name, _ := rdR.ReadString()
			names = append(names, name)
			// cookie
			rdR.ReadUint64()
			// post_op_attr
			ha, _ := rdR.ReadBool()
			if ha {
				rdR.Skip(84)
			}
			// post_op_fh
			hf, _ := rdR.ReadBool()
			if hf {
				rdR.ReadOpaque()
			}
		}

		t.Logf("Directory entries: %v", names)

		foundHello := false
		foundSubdir := false
		for _, n := range names {
			if n == "hello.txt" {
				foundHello = true
			}
			if n == "subdir" {
				foundSubdir = true
			}
		}
		if !foundHello {
			t.Error("expected hello.txt in directory listing")
		}
		if !foundSubdir {
			t.Error("expected subdir in directory listing")
		}
	})

	// Test 3: Full v3.Client flow using our adapter (with overridden port resolution).
	t.Run("V3Client_Full_Flow", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Create a v3 client pointing at the NFS server directly.
		// Since go-nfs handles MOUNT+NFS on the same port, we can test
		// ListExports and Mount+ReadDir if we use the right addressing.
		client := v3.New(transport.Direct(), "127.0.0.1", rpc.DefaultAuthSys())
		defer client.Close()

		// Override mount and NFS ports by using the test helper.
		client.SetPortOverrides(uint32(nfsPort), uint32(nfsPort))

		// ListExports
		exports, err := client.ListExports(ctx)
		if err != nil {
			t.Logf("ListExports error (may be expected with go-nfs): %v", err)
		} else {
			t.Logf("Exports: %+v", exports)
		}

		// Mount
		rootFH, err := client.Mount(ctx, "/")
		if err != nil {
			t.Fatalf("Mount: %v", err)
		}
		t.Logf("Mounted, root handle: %x", []byte(rootFH))

		// ReadDir
		entries, err := client.ReadDir(ctx, rootFH)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		t.Logf("Root entries: %d", len(entries))

		var helloEntry *nfs.DirEntry
		for i, e := range entries {
			t.Logf("  [%d] %s (type=%s, size=%d, mode=%04o, uid=%d)",
				i, e.Name, e.Attr.Type, e.Attr.Size, e.Attr.Mode, e.Attr.UID)
			if e.Name == "hello.txt" {
				helloEntry = &entries[i]
			}
		}

		if helloEntry == nil {
			t.Fatal("hello.txt not found")
		}

		// GetAttr
		attr, err := client.GetAttr(ctx, helloEntry.FH)
		if err != nil {
			t.Fatalf("GetAttr: %v", err)
		}
		if attr.Type != nfs.FileTypeRegular {
			t.Errorf("type: got %v, want regular", attr.Type)
		}
		t.Logf("hello.txt attrs: size=%d mode=%04o", attr.Size, attr.Mode)

		// Read
		data, err := client.Read(ctx, helloEntry.FH, 0, 1024)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		t.Logf("Read %d bytes: %q", len(data), string(data))

		if string(data) != "hello from nfs-client-gui test!" {
			t.Errorf("got %q, want %q", string(data), "hello from nfs-client-gui test!")
		}

		// Lookup subdir
		subdirFH, subdirAttr, err := client.Lookup(ctx, rootFH, "subdir")
		if err != nil {
			t.Fatalf("Lookup subdir: %v", err)
		}
		if subdirAttr.Type != nfs.FileTypeDirectory {
			t.Errorf("subdir type: got %v, want dir", subdirAttr.Type)
		}

		// ReadDir subdir
		subEntries, err := client.ReadDir(ctx, subdirFH)
		if err != nil {
			t.Fatalf("ReadDir subdir: %v", err)
		}
		foundNested := false
		for _, e := range subEntries {
			t.Logf("  subdir/%s", e.Name)
			if e.Name == "nested.txt" {
				foundNested = true
			}
		}
		if !foundNested {
			t.Error("nested.txt not found in subdir")
		}
	})
}
