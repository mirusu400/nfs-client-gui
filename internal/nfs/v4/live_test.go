package v4_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mirusu400/nfs-client-gui/internal/nfs"
	v4 "github.com/mirusu400/nfs-client-gui/internal/nfs/v4"
	"github.com/mirusu400/nfs-client-gui/internal/rpc"
	"github.com/mirusu400/nfs-client-gui/internal/transport"
)

// TestLive_V4_Ganesha tests NFSv4 against a real nfs-ganesha server.
// Run with: NFS4_TEST_HOST=127.0.0.1 go test -v -run TestLive
// Skip if NFS4_TEST_HOST is not set.
func TestLive_V4_Ganesha(t *testing.T) {
	host := os.Getenv("NFS4_TEST_HOST")
	if host == "" {
		t.Skip("NFS4_TEST_HOST not set, skipping live test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := v4.New(transport.Direct(), host, rpc.DefaultAuthSys())
	defer client.Close()

	// 1. Mount root
	t.Run("Mount", func(t *testing.T) {
		rootFH, err := client.Mount(ctx, "/")
		if err != nil {
			t.Fatalf("Mount /: %v", err)
		}
		t.Logf("Root FH: %x (len=%d)", []byte(rootFH)[:8], len(rootFH))

		// 2. ListExports (reads pseudo-fs root)
		t.Run("ListExports", func(t *testing.T) {
			exports, err := client.ListExports(ctx)
			if err != nil {
				t.Logf("ListExports error (may be expected): %v", err)
			} else {
				for _, e := range exports {
					t.Logf("  export: %s", e.Dir)
				}
			}
		})

		// 3. Browse from root (Pseudo=/ maps to /data)
		t.Run("Browse", func(t *testing.T) {
			// 4. ReadDir from root
			entries, err := client.ReadDir(ctx, rootFH)
			if err != nil {
				t.Fatalf("ReadDir: %v", err)
			}
			t.Logf("Entries: %d", len(entries))

			var testFH nfs.FileHandle
			for _, e := range entries {
				t.Logf("  %s (type=%s, size=%d)", e.Name, e.Attr.Type, e.Attr.Size)
				if e.Name == "test.txt" {
					testFH = e.FH
				}
			}

			// 5. Lookup
			if testFH == nil {
				lFH, attr, err := client.Lookup(ctx, rootFH, "test.txt")
				if err != nil {
					t.Logf("Lookup test.txt: %v", err)
				} else {
					testFH = lFH
					t.Logf("Lookup test.txt: type=%s size=%d", attr.Type, attr.Size)
				}
			}

			if testFH == nil {
				t.Log("test.txt not found, skipping Read test")
				return
			}

			// 6. GetAttr
			attr, err := client.GetAttr(ctx, testFH)
			if err != nil {
				t.Fatalf("GetAttr: %v", err)
			}
			t.Logf("GetAttr: type=%s size=%d mode=%04o", attr.Type, attr.Size, attr.Mode)

			// 7. Read
			data, err := client.Read(ctx, testFH, 0, 1024)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			t.Logf("Read: %d bytes: %q", len(data), string(data))
		})
	})
}
