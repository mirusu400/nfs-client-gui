package v3

import (
	"testing"
	"time"

	"github.com/mirusu400/nfsprobe/internal/nfs"
	"github.com/mirusu400/nfsprobe/internal/rpc"
)

func TestParseFattr3(t *testing.T) {
	// Build a synthetic fattr3 structure.
	w := rpc.NewXDRWriter(128)
	w.WriteUint32(NF3DIR)  // type = directory
	w.WriteUint32(0755)    // mode
	w.WriteUint32(3)       // nlink
	w.WriteUint32(1000)    // uid
	w.WriteUint32(1000)    // gid
	w.WriteUint64(4096)    // size
	w.WriteUint64(8192)    // used
	w.WriteUint32(0)       // rdev.specdata1
	w.WriteUint32(0)       // rdev.specdata2
	w.WriteUint64(42)      // fsid
	w.WriteUint64(100)     // fileid
	w.WriteUint32(1700000000) // atime.seconds
	w.WriteUint32(0)          // atime.nseconds
	w.WriteUint32(1700000100) // mtime.seconds
	w.WriteUint32(500)        // mtime.nseconds
	w.WriteUint32(1700000200) // ctime.seconds
	w.WriteUint32(0)          // ctime.nseconds

	r := rpc.NewXDRReader(w.Bytes())
	attr, err := parseFattr3(r)
	if err != nil {
		t.Fatalf("parseFattr3: %v", err)
	}

	if attr.Type != nfs.FileTypeDirectory {
		t.Errorf("type: got %v, want directory", attr.Type)
	}
	if attr.Mode != 0755 {
		t.Errorf("mode: got %o, want 755", attr.Mode)
	}
	if attr.NLink != 3 {
		t.Errorf("nlink: got %d, want 3", attr.NLink)
	}
	if attr.UID != 1000 {
		t.Errorf("uid: got %d, want 1000", attr.UID)
	}
	if attr.GID != 1000 {
		t.Errorf("gid: got %d, want 1000", attr.GID)
	}
	if attr.Size != 4096 {
		t.Errorf("size: got %d, want 4096", attr.Size)
	}
	expectedMTime := time.Unix(1700000100, 500)
	if !attr.MTime.Equal(expectedMTime) {
		t.Errorf("mtime: got %v, want %v", attr.MTime, expectedMTime)
	}
}

func TestReadPostOpAttr_None(t *testing.T) {
	w := rpc.NewXDRWriter(4)
	w.WriteBool(false) // no attributes follow

	r := rpc.NewXDRReader(w.Bytes())
	attr, err := readPostOpAttr(r)
	if err != nil {
		t.Fatalf("readPostOpAttr: %v", err)
	}
	if attr.Type != 0 {
		t.Errorf("expected zero attr, got type=%v", attr.Type)
	}
}

func TestReadPostOpAttr_Present(t *testing.T) {
	w := rpc.NewXDRWriter(128)
	w.WriteBool(true)      // attributes follow
	w.WriteUint32(NF3REG)  // type
	w.WriteUint32(0644)    // mode
	w.WriteUint32(1)       // nlink
	w.WriteUint32(0)       // uid
	w.WriteUint32(0)       // gid
	w.WriteUint64(1024)    // size
	w.WriteUint64(1024)    // used
	w.WriteUint32(0)       // rdev.specdata1
	w.WriteUint32(0)       // rdev.specdata2
	w.WriteUint64(1)       // fsid
	w.WriteUint64(2)       // fileid
	w.WriteUint32(0)       // atime.seconds
	w.WriteUint32(0)       // atime.nseconds
	w.WriteUint32(0)       // mtime.seconds
	w.WriteUint32(0)       // mtime.nseconds
	w.WriteUint32(0)       // ctime.seconds
	w.WriteUint32(0)       // ctime.nseconds

	r := rpc.NewXDRReader(w.Bytes())
	attr, err := readPostOpAttr(r)
	if err != nil {
		t.Fatalf("readPostOpAttr: %v", err)
	}
	if attr.Type != nfs.FileTypeRegular {
		t.Errorf("type: got %v, want regular", attr.Type)
	}
	if attr.Size != 1024 {
		t.Errorf("size: got %d, want 1024", attr.Size)
	}
}

func TestMapFileType3(t *testing.T) {
	tests := []struct {
		in   uint32
		want nfs.FileType
	}{
		{NF3REG, nfs.FileTypeRegular},
		{NF3DIR, nfs.FileTypeDirectory},
		{NF3LNK, nfs.FileTypeSymlink},
		{NF3BLK, nfs.FileTypeBlock},
		{NF3CHR, nfs.FileTypeChar},
		{NF3SOCK, nfs.FileTypeSocket},
		{NF3FIFO, nfs.FileTypeFIFO},
	}
	for _, tt := range tests {
		got := mapFileType3(tt.in)
		if got != tt.want {
			t.Errorf("mapFileType3(%d): got %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestClientVersion(t *testing.T) {
	c := New(nil, "test", nil)
	if c.Version() != nfs.NFSv3 {
		t.Errorf("got %v, want NFSv3", c.Version())
	}
}
