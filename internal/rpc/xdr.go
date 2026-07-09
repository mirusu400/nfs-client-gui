// Package rpc implements SUN RPC (RFC 5531) over TCP with XDR encoding
// (RFC 4506), portmapper (RFC 1833), and the MOUNT protocol.
package rpc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// XDR encoder — writes XDR-encoded values into a byte slice.
type XDRWriter struct {
	buf []byte
}

func NewXDRWriter(capacity int) *XDRWriter {
	return &XDRWriter{buf: make([]byte, 0, capacity)}
}

func (w *XDRWriter) Bytes() []byte { return w.buf }
func (w *XDRWriter) Len() int      { return len(w.buf) }

func (w *XDRWriter) WriteUint32(v uint32) {
	b := [4]byte{}
	binary.BigEndian.PutUint32(b[:], v)
	w.buf = append(w.buf, b[:]...)
}

func (w *XDRWriter) WriteUint64(v uint64) {
	b := [8]byte{}
	binary.BigEndian.PutUint64(b[:], v)
	w.buf = append(w.buf, b[:]...)
}

func (w *XDRWriter) WriteInt32(v int32) {
	w.WriteUint32(uint32(v))
}

func (w *XDRWriter) WriteBool(v bool) {
	if v {
		w.WriteUint32(1)
	} else {
		w.WriteUint32(0)
	}
}

// WriteOpaque writes a variable-length opaque (length-prefixed, padded to 4).
func (w *XDRWriter) WriteOpaque(data []byte) {
	w.WriteUint32(uint32(len(data)))
	w.WriteFixedOpaque(data)
}

// WriteFixedOpaque writes a fixed-length opaque (no length prefix, padded to 4).
func (w *XDRWriter) WriteFixedOpaque(data []byte) {
	w.buf = append(w.buf, data...)
	if pad := (4 - len(data)%4) % 4; pad > 0 {
		w.buf = append(w.buf, make([]byte, pad)...)
	}
}

// WriteString writes an XDR string (same encoding as variable-length opaque).
func (w *XDRWriter) WriteString(s string) {
	w.WriteOpaque([]byte(s))
}

// XDR decoder — reads XDR-encoded values from a byte slice.
type XDRReader struct {
	data []byte
	pos  int
}

func NewXDRReader(data []byte) *XDRReader {
	return &XDRReader{data: data}
}

func (r *XDRReader) Remaining() int { return len(r.data) - r.pos }

func (r *XDRReader) ReadUint32() (uint32, error) {
	if r.pos+4 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *XDRReader) ReadUint64() (uint64, error) {
	if r.pos+8 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint64(r.data[r.pos:])
	r.pos += 8
	return v, nil
}

func (r *XDRReader) ReadInt32() (int32, error) {
	v, err := r.ReadUint32()
	return int32(v), err
}

func (r *XDRReader) ReadBool() (bool, error) {
	v, err := r.ReadUint32()
	return v != 0, err
}

// ReadOpaque reads a variable-length opaque.
func (r *XDRReader) ReadOpaque() ([]byte, error) {
	length, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	return r.ReadFixedOpaque(int(length))
}

// ReadFixedOpaque reads a fixed-length opaque (with padding).
func (r *XDRReader) ReadFixedOpaque(n int) ([]byte, error) {
	padded := n + (4-n%4)%4
	if r.pos+padded > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}
	out := make([]byte, n)
	copy(out, r.data[r.pos:r.pos+n])
	r.pos += padded
	return out, nil
}

// ReadString reads an XDR string.
func (r *XDRReader) ReadString() (string, error) {
	b, err := r.ReadOpaque()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Skip advances the reader by n bytes (padded to 4-byte boundary).
func (r *XDRReader) Skip(n int) error {
	padded := n + (4-n%4)%4
	if r.pos+padded > len(r.data) {
		return io.ErrUnexpectedEOF
	}
	r.pos += padded
	return nil
}

// Record fragment framing for SUN RPC over TCP (RFC 5531 §11).
// Each fragment: 4-byte header (bit 31 = last fragment flag, bits 0-30 = length).

const maxFragmentSize = 1 << 20 // 1 MiB per fragment, safety limit

// WriteRecordFragment writes a complete record (single fragment, last-flag set).
func WriteRecordFragment(w io.Writer, payload []byte) error {
	hdr := uint32(len(payload)) | (1 << 31) // last fragment
	if err := binary.Write(w, binary.BigEndian, hdr); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// ReadRecord reads a complete record, reassembling fragments.
func ReadRecord(r io.Reader) ([]byte, error) {
	var record []byte
	for {
		var hdr uint32
		if err := binary.Read(r, binary.BigEndian, &hdr); err != nil {
			return nil, fmt.Errorf("rpc: read fragment header: %w", err)
		}
		last := hdr&(1<<31) != 0
		length := hdr & 0x7FFFFFFF

		if length > uint32(maxFragmentSize) {
			return nil, fmt.Errorf("rpc: fragment too large: %d bytes", length)
		}
		if len(record)+int(length) > math.MaxInt32 {
			return nil, errors.New("rpc: record too large")
		}

		frag := make([]byte, length)
		if _, err := io.ReadFull(r, frag); err != nil {
			return nil, fmt.Errorf("rpc: read fragment body: %w", err)
		}
		record = append(record, frag...)

		if last {
			return record, nil
		}
	}
}
