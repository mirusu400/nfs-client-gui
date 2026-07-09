// Package v4 implements the NFSv4 adapter behind the nfs.Client interface.
// NFSv4 (RFC 7530) uses compound operations over a single TCP connection to
// port 2049. There is no portmapper or MOUNT protocol — the root filehandle
// is obtained via PUTROOTFH.
package v4

import (
	"context"
	"fmt"
	"time"

	"github.com/mirusu400/nfs-client-gui/internal/nfs"
	"github.com/mirusu400/nfs-client-gui/internal/rpc"
	"github.com/mirusu400/nfs-client-gui/internal/transport"
)

// NFSv4 program number, version, and well-known port.
const (
	NFS4Prog = 100003
	NFS4Vers = 4
	NFS4Port = 2049
)

// NFSv4 operation numbers (RFC 7530 §15).
const (
	OP_ACCESS       = 3
	OP_CLOSE        = 4
	OP_GETATTR      = 9
	OP_GETFH        = 10
	OP_LOOKUP       = 15
	OP_OPEN         = 18
	OP_PUTFH        = 22
	OP_PUTROOTFH    = 24
	OP_READ         = 25
	OP_READDIR      = 26
	OP_SETCLIENTID  = 35
	OP_SETCLIENTID_CONFIRM = 36
)

// NFSv4 file types.
const (
	NF4REG  = 1
	NF4DIR  = 2
	NF4BLK  = 3
	NF4CHR  = 4
	NF4LNK  = 5
	NF4SOCK = 6
	NF4FIFO = 7
)

// Fattr4 attribute bit positions.
const (
	FATTR4_TYPE       = 1
	FATTR4_SIZE       = 4
	FATTR4_FILEID     = 20
	FATTR4_MODE       = 33
	FATTR4_NUMLINKS   = 35
	FATTR4_OWNER      = 36
	FATTR4_OWNER_GROUP = 37
	FATTR4_TIME_ACCESS = 47
	FATTR4_TIME_MODIFY = 53
	FATTR4_RDATTR_ERROR = 12
)

// NFS4_OK and common errors.
const (
	NFS4_OK            = 0
	NFS4ERR_NOENT      = 2
	NFS4ERR_IO         = 5
	NFS4ERR_ACCES      = 13
	NFS4ERR_EXIST      = 17
	NFS4ERR_NOTDIR     = 20
	NFS4ERR_ISDIR      = 21
	NFS4ERR_INVAL      = 22
	NFS4ERR_NOSPC      = 28
	NFS4ERR_ROFS       = 30
	NFS4ERR_NAMETOOLONG = 63
	NFS4ERR_NOTEMPTY   = 66
	NFS4ERR_STALE      = 70
)

// Client implements nfs.Client for NFSv4.
type Client struct {
	dialer transport.Dialer
	host   string
	auth   *rpc.AuthSysParams
	port   uint32

	rpcClient *rpc.Client
}

// New creates an NFSv4 client.
func New(dialer transport.Dialer, host string, auth *rpc.AuthSysParams) *Client {
	return &Client{
		dialer: dialer,
		host:   host,
		auth:   auth,
		port:   NFS4Port,
	}
}

func (c *Client) Version() nfs.Version { return nfs.NFSv4 }

func (c *Client) ensureConn() {
	if c.rpcClient == nil {
		addr := fmt.Sprintf("%s:%d", c.host, c.port)
		c.rpcClient = rpc.NewClient(c.dialer, addr, c.auth)
	}
}

// SetAuth updates credentials.
func (c *Client) SetAuth(auth *rpc.AuthSysParams) {
	c.auth = auth
	if c.rpcClient != nil {
		c.rpcClient.SetAuth(auth)
	}
}

// compound sends a COMPOUND request (proc 1) with the given operations.
func (c *Client) compound(ctx context.Context, tag string, ops []op) ([]opResult, error) {
	c.ensureConn()

	reply, err := c.rpcClient.Call(ctx, NFS4Prog, NFS4Vers, 1, // NFSPROC4_COMPOUND
		func(w *rpc.XDRWriter) {
			w.WriteString(tag)      // tag
			w.WriteUint32(0)        // minorversion
			w.WriteUint32(uint32(len(ops))) // argarray count
			for _, o := range ops {
				o.encode(w)
			}
		})
	if err != nil {
		return nil, err
	}

	return parseCompoundReply(reply)
}

// --- Client interface implementation ---

// ListExports for v4 reads the pseudo-filesystem root directory.
// NFSv4 has no MOUNT protocol; we PUTROOTFH + READDIR the root.
func (c *Client) ListExports(ctx context.Context) ([]nfs.Export, error) {
	results, err := c.compound(ctx, "exports", []op{
		opPutRootFH{},
		opReadDir{cookie: 0, count: 8192, attrRequest: readdirAttrRequest()},
	})
	if err != nil {
		return nil, err
	}

	// Skip PUTROOTFH result, get READDIR result.
	if len(results) < 2 {
		return nil, fmt.Errorf("nfsv4: unexpected compound result count: %d", len(results))
	}

	rdResult := results[1]
	if rdResult.status != NFS4_OK {
		return nil, fmt.Errorf("nfsv4: readdir: status %d", rdResult.status)
	}

	entries, err := parseReadDirResult(rdResult.data)
	if err != nil {
		return nil, err
	}

	var exports []nfs.Export
	for _, e := range entries {
		if e.Attr.Type == nfs.FileTypeDirectory {
			exports = append(exports, nfs.Export{Dir: "/" + e.Name})
		}
	}
	return exports, nil
}

// Mount for v4 does PUTROOTFH + GETFH to get the root handle,
// then optionally LOOKUP the export path components.
func (c *Client) Mount(ctx context.Context, exportPath string) (nfs.FileHandle, error) {
	ops := []op{opPutRootFH{}, opGetFH{}}

	// If export path has components beyond "/", LOOKUP each one.
	components := splitPath(exportPath)
	for _, comp := range components {
		ops = append(ops, opLookup{name: comp}, opGetFH{})
	}

	results, err := c.compound(ctx, "mount", ops)
	if err != nil {
		return nil, err
	}

	// The last GETFH result has our file handle.
	for i := len(results) - 1; i >= 0; i-- {
		if results[i].opCode == OP_GETFH {
			if results[i].status != NFS4_OK {
				return nil, fmt.Errorf("nfsv4: mount: getfh status %d", results[i].status)
			}
			r := rpc.NewXDRReader(results[i].data)
			fh, err := r.ReadOpaque()
			if err != nil {
				return nil, err
			}
			return nfs.FileHandle(fh), nil
		}
	}

	return nil, fmt.Errorf("nfsv4: mount: no GETFH result")
}

// ReadDir lists the contents of a directory.
func (c *Client) ReadDir(ctx context.Context, dir nfs.FileHandle) ([]nfs.DirEntry, error) {
	var allEntries []nfs.DirEntry
	var cookie uint64
	cookieVerf := make([]byte, 8)

	for {
		results, err := c.compound(ctx, "readdir", []op{
			opPutFH{fh: []byte(dir)},
			opReadDir{cookie: cookie, cookieVerf: cookieVerf, count: 32768, attrRequest: readdirAttrRequest()},
		})
		if err != nil {
			return nil, err
		}

		if len(results) < 2 || results[1].status != NFS4_OK {
			status := uint32(0)
			if len(results) >= 2 {
				status = results[1].status
			}
			return nil, fmt.Errorf("nfsv4: readdir: status %d", status)
		}

		rdEntries, eof, newVerf, err := parseReadDirEntriesFull(results[1].data)
		if err != nil {
			return nil, err
		}

		for _, e := range rdEntries {
			if e.Name != "." && e.Name != ".." {
				allEntries = append(allEntries, e.DirEntry)
			}
			cookie = e.cookie
		}
		copy(cookieVerf, newVerf)

		if eof {
			break
		}
	}

	return allEntries, nil
}

// Lookup finds a named entry in a directory.
func (c *Client) Lookup(ctx context.Context, dir nfs.FileHandle, name string) (nfs.FileHandle, nfs.Attr, error) {
	results, err := c.compound(ctx, "lookup", []op{
		opPutFH{fh: []byte(dir)},
		opLookup{name: name},
		opGetFH{},
		opGetAttr{request: fullAttrRequest()},
	})
	if err != nil {
		return nil, nfs.Attr{}, err
	}

	if len(results) < 4 {
		return nil, nfs.Attr{}, fmt.Errorf("nfsv4: lookup: unexpected result count")
	}

	// Check LOOKUP status.
	if results[1].status != NFS4_OK {
		return nil, nfs.Attr{}, &rpc.NFSError{Status: rpc.NFSStatus(results[1].status)}
	}

	// GETFH
	r := rpc.NewXDRReader(results[2].data)
	fh, err := r.ReadOpaque()
	if err != nil {
		return nil, nfs.Attr{}, err
	}

	// GETATTR
	attr, err := parseGetAttrResult(results[3].data)
	if err != nil {
		return nil, nfs.Attr{}, err
	}

	return nfs.FileHandle(fh), attr, nil
}

// GetAttr returns file attributes.
func (c *Client) GetAttr(ctx context.Context, fh nfs.FileHandle) (nfs.Attr, error) {
	results, err := c.compound(ctx, "getattr", []op{
		opPutFH{fh: []byte(fh)},
		opGetAttr{request: fullAttrRequest()},
	})
	if err != nil {
		return nfs.Attr{}, err
	}

	if len(results) < 2 || results[1].status != NFS4_OK {
		return nfs.Attr{}, fmt.Errorf("nfsv4: getattr failed")
	}

	return parseGetAttrResult(results[1].data)
}

// Read reads data from a file. NFSv4 requires a stateid; we use the
// anonymous stateid (all zeros) for READ without OPEN.
func (c *Client) Read(ctx context.Context, fh nfs.FileHandle, offset uint64, count uint32) ([]byte, error) {
	results, err := c.compound(ctx, "read", []op{
		opPutFH{fh: []byte(fh)},
		opRead{offset: offset, count: count},
	})
	if err != nil {
		return nil, err
	}

	if len(results) < 2 || results[1].status != NFS4_OK {
		return nil, fmt.Errorf("nfsv4: read failed")
	}

	r := rpc.NewXDRReader(results[1].data)
	// eof
	if _, err := r.ReadBool(); err != nil {
		return nil, err
	}
	// data
	data, err := r.ReadOpaque()
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Write writes data to a file (optional, may not be supported).
func (c *Client) Write(ctx context.Context, fh nfs.FileHandle, offset uint64, data []byte) (uint32, error) {
	return 0, fmt.Errorf("nfsv4: write not implemented")
}

// Close releases resources.
func (c *Client) Close() error {
	if c.rpcClient != nil {
		return c.rpcClient.Close()
	}
	return nil
}

// --- Op types ---

type op interface {
	encode(w *rpc.XDRWriter)
}

type opResult struct {
	opCode uint32
	status uint32
	data   []byte
}

type opPutRootFH struct{}

func (o opPutRootFH) encode(w *rpc.XDRWriter) {
	w.WriteUint32(OP_PUTROOTFH)
}

type opPutFH struct{ fh []byte }

func (o opPutFH) encode(w *rpc.XDRWriter) {
	w.WriteUint32(OP_PUTFH)
	w.WriteOpaque(o.fh)
}

type opGetFH struct{}

func (o opGetFH) encode(w *rpc.XDRWriter) {
	w.WriteUint32(OP_GETFH)
}

type opLookup struct{ name string }

func (o opLookup) encode(w *rpc.XDRWriter) {
	w.WriteUint32(OP_LOOKUP)
	w.WriteString(o.name)
}

type opGetAttr struct{ request []uint32 }

func (o opGetAttr) encode(w *rpc.XDRWriter) {
	w.WriteUint32(OP_GETATTR)
	// bitmap4: array of uint32
	w.WriteUint32(uint32(len(o.request)))
	for _, v := range o.request {
		w.WriteUint32(v)
	}
}

type opReadDir struct {
	cookie     uint64
	cookieVerf []byte
	count      uint32
	attrRequest []uint32
}

func (o opReadDir) encode(w *rpc.XDRWriter) {
	w.WriteUint32(OP_READDIR)
	w.WriteUint64(o.cookie)
	if len(o.cookieVerf) == 8 {
		w.WriteFixedOpaque(o.cookieVerf)
	} else {
		w.WriteFixedOpaque(make([]byte, 8))
	}
	w.WriteUint32(o.count) // dircount
	w.WriteUint32(o.count) // maxcount
	// attr_request bitmap
	w.WriteUint32(uint32(len(o.attrRequest)))
	for _, v := range o.attrRequest {
		w.WriteUint32(v)
	}
}

type opRead struct {
	offset uint64
	count  uint32
}

func (o opRead) encode(w *rpc.XDRWriter) {
	w.WriteUint32(OP_READ)
	// stateid4: anonymous (seqid=0, other=0)
	w.WriteUint32(0) // seqid
	w.WriteFixedOpaque(make([]byte, 12)) // other
	w.WriteUint64(o.offset)
	w.WriteUint32(o.count)
}

// --- Attribute bitmaps ---

// basicAttrRequest asks for type only.
func basicAttrRequest() []uint32 {
	return []uint32{
		(1 << FATTR4_TYPE),
		0,
	}
}

// fullAttrRequest asks for type, size, mode, numlinks, mtime.
func fullAttrRequest() []uint32 {
	word0 := uint32(0)
	word0 |= (1 << FATTR4_TYPE) // bit 1
	word0 |= (1 << FATTR4_SIZE) // bit 4

	word1 := uint32(0)
	word1 |= (1 << (FATTR4_MODE - 32))        // bit 33 -> word1 bit 1
	word1 |= (1 << (FATTR4_NUMLINKS - 32))    // bit 35 -> word1 bit 3
	word1 |= (1 << (FATTR4_TIME_MODIFY - 32)) // bit 53 -> word1 bit 21

	return []uint32{word0, word1}
}

// readdirAttrRequest: minimal attrs for READDIR (type only).
func readdirAttrRequest() []uint32 {
	return []uint32{1 << FATTR4_TYPE, 0}
}

// --- Reply parsing ---

func parseCompoundReply(data []byte) ([]opResult, error) {
	r := rpc.NewXDRReader(data)

	// status
	status, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}

	// tag
	if _, err := r.ReadString(); err != nil {
		return nil, err
	}

	// resarray count
	count, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}

	// If the compound itself failed, return the top-level error.
	// Individual ops may still have partial results.
	results := make([]opResult, count)
	for i := uint32(0); i < count; i++ {
		opCode, err := r.ReadUint32()
		if err != nil {
			return results[:i], err
		}
		opStatus, err := r.ReadUint32()
		if err != nil {
			return results[:i], err
		}

		results[i] = opResult{opCode: opCode, status: opStatus}

		if opStatus != NFS4_OK {
			continue
		}

		// Parse op-specific result data.
		startPos := r.Remaining()
		if err := skipOpResult(r, opCode); err != nil {
			return results[:i+1], err
		}
		endPos := r.Remaining()
		consumed := startPos - endPos

		// Re-read the consumed bytes as raw data.
		// We need to go back. Instead, let's capture at parse time.
		// This is a bit awkward; let's use a different approach.
		results[i].data = data[len(data)-startPos : len(data)-endPos]
		_ = consumed
	}

	if status != NFS4_OK && count == 0 {
		return nil, fmt.Errorf("nfsv4: compound failed: status %d", status)
	}

	return results, nil
}

func skipOpResult(r *rpc.XDRReader, opCode uint32) error {
	switch opCode {
	case OP_PUTROOTFH, OP_PUTFH, OP_LOOKUP:
		// No result data beyond status.
		return nil
	case OP_GETFH:
		// filehandle (opaque)
		_, err := r.ReadOpaque()
		return err
	case OP_GETATTR:
		// fattr4: bitmap + attr_vals (opaque)
		return skipFattr4(r)
	case OP_READDIR:
		return skipReadDirReply(r)
	case OP_READ:
		// eof (bool) + data (opaque)
		if _, err := r.ReadBool(); err != nil {
			return err
		}
		_, err := r.ReadOpaque()
		return err
	default:
		return nil
	}
}

func skipFattr4(r *rpc.XDRReader) error {
	// bitmap4
	bmLen, err := r.ReadUint32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < bmLen; i++ {
		if _, err := r.ReadUint32(); err != nil {
			return err
		}
	}
	// attr_vals (opaque)
	_, err = r.ReadOpaque()
	return err
}

func skipReadDirReply(r *rpc.XDRReader) error {
	// cookieverf
	if _, err := r.ReadFixedOpaque(8); err != nil {
		return err
	}
	// entries
	for {
		hasEntry, err := r.ReadBool()
		if err != nil {
			return err
		}
		if !hasEntry {
			break
		}
		// cookie
		if _, err := r.ReadUint64(); err != nil {
			return err
		}
		// name
		if _, err := r.ReadString(); err != nil {
			return err
		}
		// attrs (fattr4)
		if err := skipFattr4(r); err != nil {
			return err
		}
	}
	// eof
	_, err := r.ReadBool()
	return err
}

// --- ReadDir result parsing ---

type readDirEntry struct {
	nfs.DirEntry
	cookie uint64
}

func parseReadDirResult(data []byte) ([]nfs.DirEntry, error) {
	rdEntries, _, _, err := parseReadDirEntriesFull(data)
	if err != nil {
		return nil, err
	}
	entries := make([]nfs.DirEntry, len(rdEntries))
	for i, e := range rdEntries {
		entries[i] = e.DirEntry
	}
	return entries, nil
}

func parseReadDirEntriesFull(data []byte) ([]readDirEntry, bool, []byte, error) {
	r := rpc.NewXDRReader(data)

	// cookieverf
	verf, err := r.ReadFixedOpaque(8)
	if err != nil {
		return nil, false, nil, err
	}

	var entries []readDirEntry
	for {
		hasEntry, err := r.ReadBool()
		if err != nil {
			return nil, false, nil, err
		}
		if !hasEntry {
			break
		}

		ck, err := r.ReadUint64()
		if err != nil {
			return nil, false, nil, err
		}

		name, err := r.ReadString()
		if err != nil {
			return nil, false, nil, err
		}

		attr, fh, err := parseFattr4(r)
		if err != nil {
			return nil, false, nil, err
		}

		entries = append(entries, readDirEntry{
			DirEntry: nfs.DirEntry{Name: name, FH: fh, Attr: attr},
			cookie:   ck,
		})
	}

	eof, err := r.ReadBool()
	if err != nil {
		return nil, false, nil, err
	}

	return entries, eof, verf, nil
}

// parseFattr4 parses an fattr4 from a READDIR or GETATTR response.
func parseFattr4(r *rpc.XDRReader) (nfs.Attr, nfs.FileHandle, error) {
	var attr nfs.Attr

	// bitmap4
	bmLen, err := r.ReadUint32()
	if err != nil {
		return attr, nil, err
	}
	bitmap := make([]uint32, bmLen)
	for i := uint32(0); i < bmLen; i++ {
		bitmap[i], err = r.ReadUint32()
		if err != nil {
			return attr, nil, err
		}
	}

	// attr_vals (opaque)
	attrData, err := r.ReadOpaque()
	if err != nil {
		return attr, nil, err
	}

	attr = decodeAttrs(bitmap, attrData)
	return attr, nil, nil
}

func parseGetAttrResult(data []byte) (nfs.Attr, error) {
	r := rpc.NewXDRReader(data)
	attr, _, err := parseFattr4(r)
	return attr, err
}

func decodeAttrs(bitmap []uint32, data []byte) nfs.Attr {
	var attr nfs.Attr
	r := rpc.NewXDRReader(data)

	hasBit := func(bit int) bool {
		word := bit / 32
		pos := bit % 32
		if word >= len(bitmap) {
			return false
		}
		return bitmap[word]&(1<<pos) != 0
	}

	// Attributes must be decoded in order of bit position.
	// Word 0 attributes:
	if hasBit(FATTR4_TYPE) {
		t, _ := r.ReadUint32()
		attr.Type = mapFileType4(t)
	}
	if hasBit(FATTR4_SIZE) {
		attr.Size, _ = r.ReadUint64()
	}
	if hasBit(FATTR4_RDATTR_ERROR) {
		r.ReadUint32() // skip error
	}

	// Word 1 attributes:
	if hasBit(FATTR4_MODE) {
		attr.Mode, _ = r.ReadUint32()
	}
	if hasBit(FATTR4_NUMLINKS) {
		attr.NLink, _ = r.ReadUint32()
	}
	// OWNER/OWNER_GROUP are utf8str_mixed (string) — skip for now, we don't request them.
	if hasBit(FATTR4_TIME_MODIFY) {
		secs, _ := r.ReadUint64()
		nsecs, _ := r.ReadUint32()
		attr.MTime = time.Unix(int64(secs), int64(nsecs))
	}

	return attr
}

func mapFileType4(t uint32) nfs.FileType {
	switch t {
	case NF4REG:
		return nfs.FileTypeRegular
	case NF4DIR:
		return nfs.FileTypeDirectory
	case NF4BLK:
		return nfs.FileTypeBlock
	case NF4CHR:
		return nfs.FileTypeChar
	case NF4LNK:
		return nfs.FileTypeSymlink
	case NF4SOCK:
		return nfs.FileTypeSocket
	case NF4FIFO:
		return nfs.FileTypeFIFO
	default:
		return nfs.FileType(t)
	}
}

// splitPath splits an export path into components, ignoring empty strings.
func splitPath(path string) []string {
	var parts []string
	current := ""
	for _, ch := range path {
		if ch == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
