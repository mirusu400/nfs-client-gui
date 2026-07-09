// Package v2 implements the NFSv2 adapter behind the nfs.Client interface.
// It wraps github.com/mirusu400/go-nfs2 (separate module).
package v2

import (
	"context"
	"fmt"

	gomount1 "github.com/mirusu400/go-nfs2/mount1"
	gonfs2 "github.com/mirusu400/go-nfs2/nfs2"
	gorpc "github.com/mirusu400/go-nfs2/rpc"

	"github.com/mirusu400/nfsprobe/internal/nfs"
	"github.com/mirusu400/nfsprobe/internal/rpc"
	"github.com/mirusu400/nfsprobe/internal/transport"
)

// Client implements nfs.Client for NFSv2.
type Client struct {
	dialer transport.Dialer
	host   string
	auth   *rpc.AuthSysParams

	nfs2Client *gonfs2.Client

	// Port overrides for testing (0 = use portmapper).
	overrideMountPort uint32
	overrideNFSPort   uint32
}

// New creates an NFSv2 client.
func New(dialer transport.Dialer, host string, auth *rpc.AuthSysParams) *Client {
	return &Client{
		dialer: dialer,
		host:   host,
		auth:   auth,
	}
}

func (c *Client) Version() nfs.Version { return nfs.NFSv2 }

// SetPortOverrides sets explicit ports, bypassing portmapper.
func (c *Client) SetPortOverrides(mountPort, nfsPort uint32) {
	c.overrideMountPort = mountPort
	c.overrideNFSPort = nfsPort
}

// SetAuth updates credentials at runtime.
func (c *Client) SetAuth(auth *rpc.AuthSysParams) {
	c.auth = auth
	if c.nfs2Client != nil {
		c.nfs2Client.SetAuth(toGoNFS2Auth(auth))
	}
}

// ListExports uses MOUNT v1 to enumerate exports.
func (c *Client) ListExports(ctx context.Context) ([]nfs.Export, error) {
	mountPort, err := c.resolveMountPort(ctx)
	if err != nil {
		return nil, err
	}

	mc := gomount1.New(wrapDialer(c.dialer), c.host, mountPort, toGoNFS2Auth(c.auth))
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

// Mount mounts an export and establishes the nfsd connection.
func (c *Client) Mount(ctx context.Context, exportPath string) (nfs.FileHandle, error) {
	mountPort, err := c.resolveMountPort(ctx)
	if err != nil {
		return nil, err
	}

	mc := gomount1.New(wrapDialer(c.dialer), c.host, mountPort, toGoNFS2Auth(c.auth))
	fh, err := mc.Mount(ctx, exportPath)
	if err != nil {
		return nil, err
	}

	// Resolve nfsd port.
	nfsPort, err := c.resolveNFSPort(ctx)
	if err != nil {
		return nil, err
	}

	nfsdAddr := fmt.Sprintf("%s:%d", c.host, nfsPort)
	c.nfs2Client = gonfs2.New(wrapDialer(c.dialer), nfsdAddr, toGoNFS2Auth(c.auth))

	return nfs.FileHandle(fh[:]), nil
}

// ReadDir lists directory contents. NFSv2 READDIR doesn't return file handles,
// so we do LOOKUP for each entry to get handles and attrs.
func (c *Client) ReadDir(ctx context.Context, dir nfs.FileHandle) ([]nfs.DirEntry, error) {
	if c.nfs2Client == nil {
		return nil, fmt.Errorf("nfsv2: not mounted")
	}

	var fh gonfs2.FileHandle
	copy(fh[:], dir)

	rawEntries, err := c.nfs2Client.ReadDir(ctx, fh)
	if err != nil {
		return nil, err
	}

	entries := make([]nfs.DirEntry, 0, len(rawEntries))
	for _, e := range rawEntries {
		// LOOKUP each entry to get file handle and attrs.
		lfh, attr, err := c.nfs2Client.Lookup(ctx, fh, e.Name)
		if err != nil {
			// Skip entries we can't lookup (permission denied, etc).
			entries = append(entries, nfs.DirEntry{Name: e.Name})
			continue
		}
		entries = append(entries, nfs.DirEntry{
			Name: e.Name,
			FH:   nfs.FileHandle(lfh[:]),
			Attr: convertAttr(attr),
		})
	}
	return entries, nil
}

// Lookup finds a named entry in a directory.
func (c *Client) Lookup(ctx context.Context, dir nfs.FileHandle, name string) (nfs.FileHandle, nfs.Attr, error) {
	if c.nfs2Client == nil {
		return nil, nfs.Attr{}, fmt.Errorf("nfsv2: not mounted")
	}

	var fh gonfs2.FileHandle
	copy(fh[:], dir)

	lfh, attr, err := c.nfs2Client.Lookup(ctx, fh, name)
	if err != nil {
		return nil, nfs.Attr{}, err
	}
	return nfs.FileHandle(lfh[:]), convertAttr(attr), nil
}

// GetAttr returns file attributes.
func (c *Client) GetAttr(ctx context.Context, fh nfs.FileHandle) (nfs.Attr, error) {
	if c.nfs2Client == nil {
		return nfs.Attr{}, fmt.Errorf("nfsv2: not mounted")
	}

	var handle gonfs2.FileHandle
	copy(handle[:], fh)

	attr, err := c.nfs2Client.GetAttr(ctx, handle)
	if err != nil {
		return nfs.Attr{}, err
	}
	return convertAttr(attr), nil
}

// Read reads data from a file. NFSv2 is 32-bit, so offset is capped.
func (c *Client) Read(ctx context.Context, fh nfs.FileHandle, offset uint64, count uint32) ([]byte, error) {
	if c.nfs2Client == nil {
		return nil, fmt.Errorf("nfsv2: not mounted")
	}

	var handle gonfs2.FileHandle
	copy(handle[:], fh)

	if offset > 0xFFFFFFFF {
		return nil, fmt.Errorf("nfsv2: offset %d exceeds 32-bit limit", offset)
	}

	data, _, err := c.nfs2Client.Read(ctx, handle, uint32(offset), count)
	return data, err
}

// Write is not supported for NFSv2 in this implementation.
func (c *Client) Write(ctx context.Context, fh nfs.FileHandle, offset uint64, data []byte) (uint32, error) {
	return 0, fmt.Errorf("nfsv2: write not implemented")
}

// Close releases resources.
func (c *Client) Close() error {
	if c.nfs2Client != nil {
		return c.nfs2Client.Close()
	}
	return nil
}

// --- Port resolution ---

func (c *Client) resolveMountPort(ctx context.Context) (uint32, error) {
	if c.overrideMountPort > 0 {
		return c.overrideMountPort, nil
	}
	pm := rpc.NewPortmapper(c.dialer, c.host, c.auth)
	defer pm.Close()
	return pm.GetPort(ctx, 100005, 1) // MOUNT prog, version 1
}

func (c *Client) resolveNFSPort(ctx context.Context) (uint32, error) {
	if c.overrideNFSPort > 0 {
		return c.overrideNFSPort, nil
	}
	pm := rpc.NewPortmapper(c.dialer, c.host, c.auth)
	defer pm.Close()
	return pm.GetPort(ctx, 100003, 2) // NFS prog, version 2
}

// --- Adapters between nfsprobe and go-nfs2 types ---

func toGoNFS2Auth(a *rpc.AuthSysParams) *gorpc.AuthSys {
	if a == nil {
		return nil
	}
	return &gorpc.AuthSys{
		Stamp:   a.Stamp,
		Machine: a.Machine,
		UID:     a.UID,
		GID:     a.GID,
		GIDs:    a.GIDs,
	}
}

func convertAttr(a gonfs2.Attr) nfs.Attr {
	return nfs.Attr{
		Type:  convertFileType(a.Type),
		Mode:  a.Mode,
		NLink: a.NLink,
		UID:   a.UID,
		GID:   a.GID,
		Size:  uint64(a.Size), // widen 32→64
		MTime: a.MTime,
		ATime: a.ATime,
	}
}

func convertFileType(t gonfs2.FileType) nfs.FileType {
	switch uint32(t) {
	case gonfs2.NFREG:
		return nfs.FileTypeRegular
	case gonfs2.NFDIR:
		return nfs.FileTypeDirectory
	case gonfs2.NFBLK:
		return nfs.FileTypeBlock
	case gonfs2.NFCHR:
		return nfs.FileTypeChar
	case gonfs2.NFLNK:
		return nfs.FileTypeSymlink
	default:
		return nfs.FileType(t)
	}
}

// wrapDialer adapts nfsprobe's transport.Dialer to go-nfs2's rpc.Dialer.
// Both interfaces have identical signatures (DialContext returning net.Conn).
func wrapDialer(d transport.Dialer) gorpc.Dialer {
	return d
}
