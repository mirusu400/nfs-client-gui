// Package v3 implements the NFSv3 adapter behind the nfs.Client interface.
// It uses our own RPC/XDR layer and speaks RFC 1813 over TCP.
// All connections go through the injected transport.Dialer (proxy invariant).
package v3

import (
	"context"
	"fmt"
	"time"

	"github.com/mirusu400/nfs-client-gui/internal/nfs"
	"github.com/mirusu400/nfs-client-gui/internal/rpc"
	"github.com/mirusu400/nfs-client-gui/internal/transport"
)

// NFSv3 program number and version.
const (
	NFS3Prog = 100003
	NFS3Vers = 3
)

// NFSv3 procedure numbers (RFC 1813 §2).
const (
	NFSPROC3_NULL        = 0
	NFSPROC3_GETATTR     = 1
	NFSPROC3_SETATTR     = 2
	NFSPROC3_LOOKUP      = 3
	NFSPROC3_ACCESS      = 4
	NFSPROC3_READLINK    = 5
	NFSPROC3_READ        = 6
	NFSPROC3_WRITE       = 7
	NFSPROC3_CREATE      = 8
	NFSPROC3_MKDIR       = 9
	NFSPROC3_SYMLINK     = 10
	NFSPROC3_MKNOD       = 11
	NFSPROC3_REMOVE      = 12
	NFSPROC3_RMDIR       = 13
	NFSPROC3_RENAME      = 14
	NFSPROC3_LINK        = 15
	NFSPROC3_READDIR     = 16
	NFSPROC3_READDIRPLUS = 17
	NFSPROC3_FSSTAT      = 18
	NFSPROC3_FSINFO      = 19
	NFSPROC3_PATHCONF    = 20
	NFSPROC3_COMMIT      = 21
)

// NFSv3 file types (ftype3).
const (
	NF3REG  = 1
	NF3DIR  = 2
	NF3BLK  = 3
	NF3CHR  = 4
	NF3LNK  = 5
	NF3SOCK = 6
	NF3FIFO = 7
)

// Client implements nfs.Client for NFSv3.
type Client struct {
	dialer transport.Dialer
	host   string
	auth   *rpc.AuthSysParams

	// nfsClient is the persistent RPC connection to nfsd.
	nfsClient *rpc.Client
	nfsAddr   string

	// Port overrides for testing (0 = use portmapper).
	overrideMountPort uint32
	overrideNFSPort   uint32
}

// New creates an NFSv3 client for the given host.
func New(dialer transport.Dialer, host string, auth *rpc.AuthSysParams) *Client {
	return &Client{
		dialer: dialer,
		host:   host,
		auth:   auth,
	}
}

func (c *Client) Version() nfs.Version { return nfs.NFSv3 }

// ListExports uses the MOUNT protocol to enumerate exported filesystems.
func (c *Client) ListExports(ctx context.Context) ([]nfs.Export, error) {
	mc := rpc.NewMountClient(c.dialer, c.host, c.auth, rpc.MountVers3)
	if c.overrideMountPort > 0 {
		mc.SetPortOverride(c.overrideMountPort)
	}
	exports, err := mc.Export(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]nfs.Export, len(exports))
	for i, e := range exports {
		result[i] = nfs.Export{Dir: e.Dir, Groups: e.Groups}
	}
	return result, nil
}

// Mount mounts the export and establishes a persistent nfsd connection.
func (c *Client) Mount(ctx context.Context, exportPath string) (nfs.FileHandle, error) {
	// Step 1: MOUNT the export to get the root file handle.
	mc := rpc.NewMountClient(c.dialer, c.host, c.auth, rpc.MountVers3)
	if c.overrideMountPort > 0 {
		mc.SetPortOverride(c.overrideMountPort)
	}
	fh, err := mc.Mount(ctx, exportPath)
	if err != nil {
		return nil, err
	}

	// Step 2: Discover nfsd port.
	var nfsPort uint32
	if c.overrideNFSPort > 0 {
		nfsPort = c.overrideNFSPort
	} else {
		pm := rpc.NewPortmapper(c.dialer, c.host, c.auth)
		defer pm.Close()
		nfsPort, err = pm.GetPort(ctx, NFS3Prog, NFS3Vers)
		if err != nil {
			return nil, fmt.Errorf("nfsv3: resolve nfsd port: %w", err)
		}
	}

	// Step 3: Create persistent RPC client for nfsd.
	c.nfsAddr = fmt.Sprintf("%s:%d", c.host, nfsPort)
	c.nfsClient = rpc.NewClient(c.dialer, c.nfsAddr, c.auth)

	return nfs.FileHandle(fh), nil
}

// SetPortOverrides sets explicit ports for mountd and nfsd, bypassing portmapper.
// Used for testing with servers that don't run a portmapper.
func (c *Client) SetPortOverrides(mountPort, nfsPort uint32) {
	c.overrideMountPort = mountPort
	c.overrideNFSPort = nfsPort
}

// SetAuth updates credentials for subsequent NFS calls.
func (c *Client) SetAuth(auth *rpc.AuthSysParams) {
	c.auth = auth
	if c.nfsClient != nil {
		c.nfsClient.SetAuth(auth)
	}
}

func (c *Client) call(ctx context.Context, proc uint32, encodeArgs func(*rpc.XDRWriter)) ([]byte, error) {
	if c.nfsClient == nil {
		return nil, fmt.Errorf("nfsv3: not mounted")
	}
	return c.nfsClient.Call(ctx, NFS3Prog, NFS3Vers, proc, encodeArgs)
}

// GetAttr returns file attributes for the given file handle.
func (c *Client) GetAttr(ctx context.Context, fh nfs.FileHandle) (nfs.Attr, error) {
	reply, err := c.call(ctx, NFSPROC3_GETATTR, func(w *rpc.XDRWriter) {
		w.WriteOpaque([]byte(fh))
	})
	if err != nil {
		return nfs.Attr{}, err
	}

	r := rpc.NewXDRReader(reply)

	status, err := r.ReadUint32()
	if err != nil {
		return nfs.Attr{}, err
	}
	if err := rpc.CheckNFSStatus(status); err != nil {
		return nfs.Attr{}, err
	}

	return parseFattr3(r)
}

// Lookup finds a named entry in a directory.
func (c *Client) Lookup(ctx context.Context, dir nfs.FileHandle, name string) (nfs.FileHandle, nfs.Attr, error) {
	reply, err := c.call(ctx, NFSPROC3_LOOKUP, func(w *rpc.XDRWriter) {
		// diropargs3: dir file handle + name
		w.WriteOpaque([]byte(dir))
		w.WriteString(name)
	})
	if err != nil {
		return nil, nfs.Attr{}, err
	}

	r := rpc.NewXDRReader(reply)

	status, err := r.ReadUint32()
	if err != nil {
		return nil, nfs.Attr{}, err
	}
	if err := rpc.CheckNFSStatus(status); err != nil {
		return nil, nfs.Attr{}, err
	}

	// object file handle
	fh, err := r.ReadOpaque()
	if err != nil {
		return nil, nfs.Attr{}, fmt.Errorf("nfsv3: lookup: read fh: %w", err)
	}

	// obj_attributes (post_op_attr): optional
	attr, err := readPostOpAttr(r)
	if err != nil {
		return nil, nfs.Attr{}, err
	}

	return nfs.FileHandle(fh), attr, nil
}

// Read reads data from a file.
func (c *Client) Read(ctx context.Context, fh nfs.FileHandle, offset uint64, count uint32) ([]byte, error) {
	reply, err := c.call(ctx, NFSPROC3_READ, func(w *rpc.XDRWriter) {
		w.WriteOpaque([]byte(fh))
		w.WriteUint64(offset)
		w.WriteUint32(count)
	})
	if err != nil {
		return nil, err
	}

	r := rpc.NewXDRReader(reply)

	status, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	if err := rpc.CheckNFSStatus(status); err != nil {
		return nil, err
	}

	// post_op_attr (skip)
	if _, err := readPostOpAttr(r); err != nil {
		return nil, err
	}

	// count (actual bytes read)
	_, err = r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("nfsv3: read: count: %w", err)
	}

	// eof
	_, err = r.ReadBool()
	if err != nil {
		return nil, fmt.Errorf("nfsv3: read: eof: %w", err)
	}

	// data (opaque)
	data, err := r.ReadOpaque()
	if err != nil {
		return nil, fmt.Errorf("nfsv3: read: data: %w", err)
	}

	return data, nil
}

// Write writes data to a file.
func (c *Client) Write(ctx context.Context, fh nfs.FileHandle, offset uint64, data []byte) (uint32, error) {
	reply, err := c.call(ctx, NFSPROC3_WRITE, func(w *rpc.XDRWriter) {
		w.WriteOpaque([]byte(fh))
		w.WriteUint64(offset)
		w.WriteUint32(uint32(len(data)))
		w.WriteUint32(2) // UNSTABLE write (FILE_SYNC=2 for durability, UNSTABLE=0)
		w.WriteOpaque(data)
	})
	if err != nil {
		return 0, err
	}

	r := rpc.NewXDRReader(reply)

	status, err := r.ReadUint32()
	if err != nil {
		return 0, err
	}
	if err := rpc.CheckNFSStatus(status); err != nil {
		return 0, err
	}

	// wcc_data (skip)
	if err := skipWccData(r); err != nil {
		return 0, err
	}

	// count
	count, err := r.ReadUint32()
	if err != nil {
		return 0, fmt.Errorf("nfsv3: write: count: %w", err)
	}

	return count, nil
}

// ReadDir lists the contents of a directory using READDIRPLUS for efficiency
// (returns file handles + attributes alongside names).
func (c *Client) ReadDir(ctx context.Context, dir nfs.FileHandle) ([]nfs.DirEntry, error) {
	var entries []nfs.DirEntry
	var cookie uint64
	cookieVerf := make([]byte, 8) // initially zero

	for {
		reply, err := c.call(ctx, NFSPROC3_READDIRPLUS, func(w *rpc.XDRWriter) {
			w.WriteOpaque([]byte(dir))
			w.WriteUint64(cookie)
			w.WriteFixedOpaque(cookieVerf)
			w.WriteUint32(4096)  // dircount (advisory)
			w.WriteUint32(32768) // maxcount
		})
		if err != nil {
			return nil, err
		}

		r := rpc.NewXDRReader(reply)

		status, err := r.ReadUint32()
		if err != nil {
			return nil, err
		}
		if err := rpc.CheckNFSStatus(status); err != nil {
			return nil, err
		}

		// dir_attributes (post_op_attr, skip)
		if _, err := readPostOpAttr(r); err != nil {
			return nil, err
		}

		// cookieverf3
		cv, err := r.ReadFixedOpaque(8)
		if err != nil {
			return nil, fmt.Errorf("nfsv3: readdirplus: cookieverf: %w", err)
		}
		copy(cookieVerf, cv)

		// Read entries.
		for {
			hasEntry, err := r.ReadBool()
			if err != nil {
				return nil, fmt.Errorf("nfsv3: readdirplus: has_entry: %w", err)
			}
			if !hasEntry {
				break
			}

			entry, err := parseEntryPlus3(r)
			if err != nil {
				return nil, err
			}

			cookie = entry.cookie

			// Skip . and ..
			if entry.Name != "." && entry.Name != ".." {
				entries = append(entries, nfs.DirEntry{
					Name: entry.Name,
					FH:   entry.FH,
					Attr: entry.Attr,
				})
			}
		}

		// eof
		eof, err := r.ReadBool()
		if err != nil {
			return nil, fmt.Errorf("nfsv3: readdirplus: eof: %w", err)
		}
		if eof {
			break
		}
	}

	return entries, nil
}

// Close releases all resources.
func (c *Client) Close() error {
	if c.nfsClient != nil {
		return c.nfsClient.Close()
	}
	return nil
}

// --- XDR parsing helpers for NFSv3 types ---

type entryPlus3 struct {
	nfs.DirEntry
	cookie uint64
}

func parseEntryPlus3(r *rpc.XDRReader) (entryPlus3, error) {
	var e entryPlus3

	// fileid
	_, err := r.ReadUint64()
	if err != nil {
		return e, fmt.Errorf("nfsv3: entryplus: fileid: %w", err)
	}

	// name
	e.Name, err = r.ReadString()
	if err != nil {
		return e, fmt.Errorf("nfsv3: entryplus: name: %w", err)
	}

	// cookie
	e.cookie, err = r.ReadUint64()
	if err != nil {
		return e, fmt.Errorf("nfsv3: entryplus: cookie: %w", err)
	}

	// name_attributes (post_op_attr)
	e.Attr, err = readPostOpAttr(r)
	if err != nil {
		return e, fmt.Errorf("nfsv3: entryplus: attrs: %w", err)
	}

	// name_handle (post_op_fh3)
	hasHandle, err := r.ReadBool()
	if err != nil {
		return e, fmt.Errorf("nfsv3: entryplus: has_handle: %w", err)
	}
	if hasHandle {
		fh, err := r.ReadOpaque()
		if err != nil {
			return e, fmt.Errorf("nfsv3: entryplus: handle: %w", err)
		}
		e.FH = nfs.FileHandle(fh)
	}

	return e, nil
}

// parseFattr3 reads an fattr3 structure (RFC 1813 §2.5).
func parseFattr3(r *rpc.XDRReader) (nfs.Attr, error) {
	var a nfs.Attr

	ftype, err := r.ReadUint32()
	if err != nil {
		return a, err
	}
	a.Type = mapFileType3(ftype)

	mode, err := r.ReadUint32()
	if err != nil {
		return a, err
	}
	a.Mode = mode

	nlink, err := r.ReadUint32()
	if err != nil {
		return a, err
	}
	a.NLink = nlink

	uid, err := r.ReadUint32()
	if err != nil {
		return a, err
	}
	a.UID = uid

	gid, err := r.ReadUint32()
	if err != nil {
		return a, err
	}
	a.GID = gid

	size, err := r.ReadUint64()
	if err != nil {
		return a, err
	}
	a.Size = size

	// used (uint64) - skip
	if _, err := r.ReadUint64(); err != nil {
		return a, err
	}

	// rdev: specdata3 (2 x uint32) - skip
	if _, err := r.ReadUint32(); err != nil {
		return a, err
	}
	if _, err := r.ReadUint32(); err != nil {
		return a, err
	}

	// fsid (uint64) - skip
	if _, err := r.ReadUint64(); err != nil {
		return a, err
	}

	// fileid (uint64) - skip
	if _, err := r.ReadUint64(); err != nil {
		return a, err
	}

	// atime: nfstime3
	atime, err := readNfsTime3(r)
	if err != nil {
		return a, err
	}
	a.ATime = atime

	// mtime: nfstime3
	mtime, err := readNfsTime3(r)
	if err != nil {
		return a, err
	}
	a.MTime = mtime

	// ctime: nfstime3 - skip
	if _, err := readNfsTime3(r); err != nil {
		return a, err
	}

	return a, nil
}

func readNfsTime3(r *rpc.XDRReader) (time.Time, error) {
	secs, err := r.ReadUint32()
	if err != nil {
		return time.Time{}, err
	}
	nsecs, err := r.ReadUint32()
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(int64(secs), int64(nsecs)), nil
}

// readPostOpAttr reads a post_op_attr (optional fattr3).
func readPostOpAttr(r *rpc.XDRReader) (nfs.Attr, error) {
	follows, err := r.ReadBool()
	if err != nil {
		return nfs.Attr{}, err
	}
	if !follows {
		return nfs.Attr{}, nil
	}
	return parseFattr3(r)
}

// skipWccData skips a wcc_data structure (pre_op_attr + post_op_attr).
func skipWccData(r *rpc.XDRReader) error {
	// pre_op_attr
	hasPre, err := r.ReadBool()
	if err != nil {
		return err
	}
	if hasPre {
		// wcc_attr: size(8) + mtime(8) + ctime(8) = 24 bytes
		if err := r.Skip(24); err != nil {
			return err
		}
	}
	// post_op_attr
	_, err = readPostOpAttr(r)
	return err
}

func mapFileType3(t uint32) nfs.FileType {
	switch t {
	case NF3REG:
		return nfs.FileTypeRegular
	case NF3DIR:
		return nfs.FileTypeDirectory
	case NF3BLK:
		return nfs.FileTypeBlock
	case NF3CHR:
		return nfs.FileTypeChar
	case NF3LNK:
		return nfs.FileTypeSymlink
	case NF3SOCK:
		return nfs.FileTypeSocket
	case NF3FIFO:
		return nfs.FileTypeFIFO
	default:
		return nfs.FileType(t)
	}
}
