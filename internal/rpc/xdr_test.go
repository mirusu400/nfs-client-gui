package rpc

import (
	"bytes"
	"testing"
)

func TestXDRUint32Roundtrip(t *testing.T) {
	w := NewXDRWriter(16)
	w.WriteUint32(0)
	w.WriteUint32(42)
	w.WriteUint32(0xFFFFFFFF)

	r := NewXDRReader(w.Bytes())
	for _, want := range []uint32{0, 42, 0xFFFFFFFF} {
		got, err := r.ReadUint32()
		if err != nil {
			t.Fatalf("ReadUint32: %v", err)
		}
		if got != want {
			t.Errorf("got %d, want %d", got, want)
		}
	}
}

func TestXDRUint64Roundtrip(t *testing.T) {
	w := NewXDRWriter(16)
	w.WriteUint64(0)
	w.WriteUint64(1 << 40)

	r := NewXDRReader(w.Bytes())
	for _, want := range []uint64{0, 1 << 40} {
		got, err := r.ReadUint64()
		if err != nil {
			t.Fatalf("ReadUint64: %v", err)
		}
		if got != want {
			t.Errorf("got %d, want %d", got, want)
		}
	}
}

func TestXDRBoolRoundtrip(t *testing.T) {
	w := NewXDRWriter(8)
	w.WriteBool(true)
	w.WriteBool(false)

	r := NewXDRReader(w.Bytes())
	if v, err := r.ReadBool(); err != nil || !v {
		t.Errorf("expected true, got %v (err=%v)", v, err)
	}
	if v, err := r.ReadBool(); err != nil || v {
		t.Errorf("expected false, got %v (err=%v)", v, err)
	}
}

func TestXDRStringRoundtrip(t *testing.T) {
	tests := []string{"", "hello", "abc", "four"}
	w := NewXDRWriter(64)
	for _, s := range tests {
		w.WriteString(s)
	}

	r := NewXDRReader(w.Bytes())
	for _, want := range tests {
		got, err := r.ReadString()
		if err != nil {
			t.Fatalf("ReadString: %v", err)
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestXDROpaquePadding(t *testing.T) {
	// Opaque of length 1 should be padded to 4 bytes (+ 4 byte length prefix = 8 total).
	w := NewXDRWriter(16)
	w.WriteOpaque([]byte{0xAB})

	if w.Len() != 8 {
		t.Errorf("expected 8 bytes, got %d", w.Len())
	}

	r := NewXDRReader(w.Bytes())
	got, err := r.ReadOpaque()
	if err != nil {
		t.Fatalf("ReadOpaque: %v", err)
	}
	if !bytes.Equal(got, []byte{0xAB}) {
		t.Errorf("got %x, want ab", got)
	}
}

func TestXDRFixedOpaque(t *testing.T) {
	// 32-byte fixed opaque (NFSv2 file handle size).
	fh := make([]byte, 32)
	for i := range fh {
		fh[i] = byte(i)
	}

	w := NewXDRWriter(32)
	w.WriteFixedOpaque(fh)

	if w.Len() != 32 {
		t.Errorf("expected 32 bytes, got %d", w.Len())
	}

	r := NewXDRReader(w.Bytes())
	got, err := r.ReadFixedOpaque(32)
	if err != nil {
		t.Fatalf("ReadFixedOpaque: %v", err)
	}
	if !bytes.Equal(got, fh) {
		t.Errorf("round-trip mismatch")
	}
}

func TestRecordFragmentRoundtrip(t *testing.T) {
	payload := []byte("hello, RPC!")
	var buf bytes.Buffer

	if err := WriteRecordFragment(&buf, payload); err != nil {
		t.Fatalf("WriteRecordFragment: %v", err)
	}

	got, err := ReadRecord(&buf)
	if err != nil {
		t.Fatalf("ReadRecord: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q, want %q", got, payload)
	}
}

func TestRecordFragmentTooLarge(t *testing.T) {
	// Craft a header claiming a fragment larger than maxFragmentSize.
	var buf bytes.Buffer
	hdr := uint32(maxFragmentSize+1) | (1 << 31)
	b := make([]byte, 4)
	b[0] = byte(hdr >> 24)
	b[1] = byte(hdr >> 16)
	b[2] = byte(hdr >> 8)
	b[3] = byte(hdr)
	buf.Write(b)

	_, err := ReadRecord(&buf)
	if err == nil {
		t.Error("expected error for oversized fragment")
	}
}
