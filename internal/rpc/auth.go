package rpc

import (
	"os"
	"time"
)

// Auth flavors (RFC 5531 §7.2).
const (
	AuthNone = 0
	AuthSys  = 1
)

// AuthSysParams holds AUTH_SYS (AUTH_UNIX) credentials per RFC 5531 §8.2.
// These are caller-controlled for pentest use: arbitrary uid/gid spoofing.
type AuthSysParams struct {
	Stamp   uint32
	Machine string
	UID     uint32
	GID     uint32
	GIDs    []uint32
}

// DefaultAuthSys returns AUTH_SYS credentials with the current process's
// uid/gid. Useful as a starting point; callers should override for pentesting.
func DefaultAuthSys() *AuthSysParams {
	return &AuthSysParams{
		Stamp:   uint32(time.Now().Unix()),
		Machine: hostname(),
		UID:     uint32(os.Getuid()),
		GID:     uint32(os.Getgid()),
		GIDs:    []uint32{uint32(os.Getgid())},
	}
}

func hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		h = "localhost"
	}
	return h
}

// Encode writes AUTH_SYS credentials in XDR format.
func (a *AuthSysParams) Encode(w *XDRWriter) {
	// AUTH_SYS body is itself an opaque: length-prefixed XDR struct.
	body := NewXDRWriter(64)
	body.WriteUint32(a.Stamp)
	body.WriteString(a.Machine)
	body.WriteUint32(a.UID)
	body.WriteUint32(a.GID)
	body.WriteUint32(uint32(len(a.GIDs)))
	for _, g := range a.GIDs {
		body.WriteUint32(g)
	}
	w.WriteOpaque(body.Bytes())
}

// WriteAuthNone writes a AUTH_NONE credential/verifier.
func WriteAuthNone(w *XDRWriter) {
	w.WriteUint32(AuthNone) // flavor
	w.WriteUint32(0)        // length (empty body)
}
