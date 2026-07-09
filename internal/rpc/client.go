package rpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/mirusu400/nfsprobe/internal/transport"
)

// RPC message types (RFC 5531 §8).
const (
	MsgTypeCall  = 0
	MsgTypeReply = 1
)

// RPC reply status.
const (
	ReplyAccepted = 0
	ReplyDenied   = 1
)

// Accept status.
const (
	AcceptSuccess      = 0
	AcceptProgUnavail  = 1
	AcceptProgMismatch = 2
	AcceptProcUnavail  = 3
	AcceptGarbageArgs  = 4
	AcceptSystemErr    = 5
)

// RPCError represents an RPC-level error.
type RPCError struct {
	ReplyStatus  uint32
	AcceptStatus uint32
	Msg          string
}

func (e *RPCError) Error() string { return e.Msg }

// Client is a SUN RPC client over TCP with record-marking framing.
// It maintains a single persistent connection to a specific address.
// Safe for concurrent use; calls are serialized on the connection.
type Client struct {
	dialer transport.Dialer
	addr   string

	mu   sync.Mutex
	auth *AuthSysParams
	conn net.Conn
	xid  atomic.Uint32
}

// NewClient creates an RPC client bound to the given address.
// The connection is lazy — established on the first Call.
func NewClient(dialer transport.Dialer, addr string, auth *AuthSysParams) *Client {
	c := &Client{
		dialer: dialer,
		addr:   addr,
		auth:   auth,
	}
	c.xid.Store(1)
	return c
}

// SetAuth updates the AUTH_SYS credentials for subsequent calls.
// This allows runtime uid/gid switching without reconnecting.
func (c *Client) SetAuth(auth *AuthSysParams) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.auth = auth
}

// Call performs an RPC call to the given program/version/procedure.
// It reuses the persistent connection, reconnecting if needed.
func (c *Client) Call(ctx context.Context, prog, vers, proc uint32, encodeArgs func(*XDRWriter)) ([]byte, error) {
	xid := c.xid.Add(1)

	// Build the RPC call message.
	w := NewXDRWriter(256)
	w.WriteUint32(xid)         // xid
	w.WriteUint32(MsgTypeCall) // message type
	w.WriteUint32(2)           // RPC version
	w.WriteUint32(prog)        // program
	w.WriteUint32(vers)        // version
	w.WriteUint32(proc)        // procedure

	// Credentials.
	c.mu.Lock()
	auth := c.auth
	c.mu.Unlock()

	if auth != nil {
		w.WriteUint32(AuthSys) // cred flavor
		auth.Encode(w)
	} else {
		WriteAuthNone(w)
	}
	WriteAuthNone(w) // verifier (always AUTH_NONE for AUTH_SYS)

	// Procedure arguments.
	if encodeArgs != nil {
		encodeArgs(w)
	}

	// Dial + send + receive under lock (serialized per connection).
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, err := c.dialLocked(ctx)
	if err != nil {
		return nil, err
	}

	if err := WriteRecordFragment(conn, w.Bytes()); err != nil {
		c.closeLocked()
		return nil, fmt.Errorf("rpc: write call: %w", err)
	}

	reply, err := ReadRecord(conn)
	if err != nil {
		c.closeLocked()
		return nil, fmt.Errorf("rpc: read reply: %w", err)
	}

	return c.parseReply(xid, reply)
}

func (c *Client) dialLocked(ctx context.Context) (net.Conn, error) {
	if c.conn != nil {
		return c.conn, nil
	}
	conn, err := c.dialer.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return nil, fmt.Errorf("rpc: dial %s: %w", c.addr, err)
	}
	c.conn = conn
	return conn, nil
}

func (c *Client) closeLocked() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

func (c *Client) parseReply(expectedXID uint32, data []byte) ([]byte, error) {
	r := NewXDRReader(data)

	xid, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("rpc: reply: read xid: %w", err)
	}
	if xid != expectedXID {
		return nil, fmt.Errorf("rpc: xid mismatch: got %d, want %d", xid, expectedXID)
	}

	msgType, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("rpc: reply: read msg type: %w", err)
	}
	if msgType != MsgTypeReply {
		return nil, fmt.Errorf("rpc: expected reply, got msg type %d", msgType)
	}

	replyStatus, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("rpc: reply: read reply status: %w", err)
	}
	if replyStatus == ReplyDenied {
		return nil, &RPCError{ReplyStatus: replyStatus, Msg: "rpc: call rejected"}
	}

	// Accepted: read verifier (skip it).
	if _, err := r.ReadUint32(); err != nil {
		return nil, fmt.Errorf("rpc: reply: read verifier flavor: %w", err)
	}
	if _, err := r.ReadOpaque(); err != nil {
		return nil, fmt.Errorf("rpc: reply: read verifier body: %w", err)
	}

	acceptStatus, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("rpc: reply: read accept status: %w", err)
	}
	if acceptStatus != AcceptSuccess {
		return nil, &RPCError{
			ReplyStatus:  replyStatus,
			AcceptStatus: acceptStatus,
			Msg:          fmt.Sprintf("rpc: call failed with accept status %d", acceptStatus),
		}
	}

	// Return the remaining data as the procedure result.
	remaining := r.Remaining()
	if remaining <= 0 {
		return nil, nil
	}
	result := make([]byte, remaining)
	copy(result, data[len(data)-remaining:])
	return result, nil
}
