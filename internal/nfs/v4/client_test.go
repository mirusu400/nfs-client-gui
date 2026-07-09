package v4

import (
	"testing"

	"github.com/mirusu400/nfs-client-gui/internal/nfs"
	"github.com/mirusu400/nfs-client-gui/internal/rpc"
)

func TestSplitPath(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"/", nil},
		{"/exports/share", []string{"exports", "share"}},
		{"exports/share/", []string{"exports", "share"}},
		{"/a", []string{"a"}},
		{"///a///b///", []string{"a", "b"}},
	}
	for _, tt := range tests {
		got := splitPath(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("splitPath(%q): got %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitPath(%q)[%d]: got %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestMapFileType4(t *testing.T) {
	tests := []struct {
		in   uint32
		want nfs.FileType
	}{
		{NF4REG, nfs.FileTypeRegular},
		{NF4DIR, nfs.FileTypeDirectory},
		{NF4LNK, nfs.FileTypeSymlink},
	}
	for _, tt := range tests {
		if got := mapFileType4(tt.in); got != tt.want {
			t.Errorf("mapFileType4(%d): got %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestDecodeAttrs(t *testing.T) {
	// Build attrs with type=REG, size=1024
	w := rpc.NewXDRWriter(32)
	w.WriteUint32(NF4REG) // type
	w.WriteUint64(1024)   // size

	bitmap := []uint32{
		(1 << FATTR4_TYPE) | (1 << FATTR4_SIZE),
		0,
	}

	attr := decodeAttrs(bitmap, w.Bytes())

	if attr.Type != nfs.FileTypeRegular {
		t.Errorf("type: got %v, want regular", attr.Type)
	}
	if attr.Size != 1024 {
		t.Errorf("size: got %d, want 1024", attr.Size)
	}
}

func TestBasicAttrRequest(t *testing.T) {
	bm := basicAttrRequest()
	if len(bm) != 2 {
		t.Fatalf("expected 2-word bitmap, got %d", len(bm))
	}
	if bm[0]&(1<<FATTR4_TYPE) == 0 {
		t.Error("expected TYPE bit set")
	}
}

func TestFullAttrRequest(t *testing.T) {
	bm := fullAttrRequest()
	if len(bm) != 2 {
		t.Fatalf("expected 2-word bitmap, got %d", len(bm))
	}
	if bm[0]&(1<<FATTR4_TYPE) == 0 {
		t.Error("expected TYPE bit set")
	}
	if bm[0]&(1<<FATTR4_SIZE) == 0 {
		t.Error("expected SIZE bit set")
	}
	if bm[1]&(1<<(FATTR4_MODE-32)) == 0 {
		t.Error("expected MODE bit set")
	}
}

func TestClientVersion(t *testing.T) {
	c := New(nil, "test", nil)
	if c.Version() != nfs.NFSv4 {
		t.Errorf("got %v, want NFSv4", c.Version())
	}
}
