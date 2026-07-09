package rpc

import (
	"testing"
)

func TestAuthSysEncode(t *testing.T) {
	auth := &AuthSysParams{
		Stamp:   12345,
		Machine: "test",
		UID:     1000,
		GID:     1000,
		GIDs:    []uint32{1000, 1001},
	}

	w := NewXDRWriter(128)
	auth.Encode(w)

	// The encoded result should be a length-prefixed opaque containing the struct.
	r := NewXDRReader(w.Bytes())

	// Read the opaque body.
	body, err := r.ReadOpaque()
	if err != nil {
		t.Fatalf("ReadOpaque: %v", err)
	}

	// Parse the body.
	br := NewXDRReader(body)

	stamp, _ := br.ReadUint32()
	if stamp != 12345 {
		t.Errorf("stamp: got %d, want 12345", stamp)
	}

	machine, _ := br.ReadString()
	if machine != "test" {
		t.Errorf("machine: got %q, want %q", machine, "test")
	}

	uid, _ := br.ReadUint32()
	if uid != 1000 {
		t.Errorf("uid: got %d, want 1000", uid)
	}

	gid, _ := br.ReadUint32()
	if gid != 1000 {
		t.Errorf("gid: got %d, want 1000", gid)
	}

	nGids, _ := br.ReadUint32()
	if nGids != 2 {
		t.Errorf("nGids: got %d, want 2", nGids)
	}

	for _, want := range []uint32{1000, 1001} {
		g, _ := br.ReadUint32()
		if g != want {
			t.Errorf("gid: got %d, want %d", g, want)
		}
	}
}
