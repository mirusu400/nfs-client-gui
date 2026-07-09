package rpc

import (
	"context"
	"fmt"

	"github.com/mirusu400/nfs-client-gui/internal/transport"
)

// MOUNT protocol constants.
const (
	MountProg = 100005

	// MOUNT protocol versions.
	MountVers1 = 1 // for NFSv2 (RFC 1094)
	MountVers3 = 3 // for NFSv3 (RFC 1813)

	MountProcNull   = 0
	MountProcMnt    = 1
	MountProcExport = 5
	MountProcUmnt   = 3
)

// MOUNT status codes.
const (
	MNT_OK             = 0
	MNTERR_PERM        = 1
	MNTERR_NOENT       = 2
	MNTERR_IO          = 5
	MNTERR_ACCES       = 13
	MNTERR_NOTDIR      = 20
	MNTERR_INVAL       = 22
	MNTERR_NAMETOOLONG = 63
	MNTERR_NOTSUPP     = 10004
	MNTERR_SERVERFAULT = 10006
)

// MountExport represents an exported filesystem as returned by MOUNTPROC_EXPORT.
type MountExport struct {
	Dir    string
	Groups []string
}

// MountClient handles the MOUNT protocol (v1 and v3).
// It uses portmapper to discover the mountd port, then performs MOUNT operations.
type MountClient struct {
	dialer       transport.Dialer
	auth         *AuthSysParams
	host         string
	mountVer     uint32
	overridePort uint32 // 0 = use portmapper
}

// NewMountClient creates a MOUNT protocol client.
// mountVer should be MountVers1 (for NFSv2) or MountVers3 (for NFSv3).
func NewMountClient(dialer transport.Dialer, host string, auth *AuthSysParams, mountVer uint32) *MountClient {
	return &MountClient{
		dialer:   dialer,
		auth:     auth,
		host:     host,
		mountVer: mountVer,
	}
}

// SetAuth updates credentials for subsequent calls.
func (m *MountClient) SetAuth(auth *AuthSysParams) {
	m.auth = auth
}

// SetPortOverride sets an explicit mountd port, bypassing portmapper.
func (m *MountClient) SetPortOverride(port uint32) {
	m.overridePort = port
}

// resolveMountPort uses portmapper to find the mountd port.
func (m *MountClient) resolveMountPort(ctx context.Context) (uint32, error) {
	if m.overridePort > 0 {
		return m.overridePort, nil
	}
	pm := NewPortmapper(m.dialer, m.host, m.auth)
	defer pm.Close()
	return pm.GetPort(ctx, MountProg, m.mountVer)
}

// Export returns the list of exported filesystems (showmount -e).
func (m *MountClient) Export(ctx context.Context) ([]MountExport, error) {
	port, err := m.resolveMountPort(ctx)
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("%s:%d", m.host, port)
	client := NewClient(m.dialer, addr, m.auth)
	defer client.Close()

	reply, err := client.Call(ctx, MountProg, m.mountVer, MountProcExport, nil)
	if err != nil {
		return nil, fmt.Errorf("mount: export: %w", err)
	}

	return parseExportList(reply)
}

func parseExportList(data []byte) ([]MountExport, error) {
	r := NewXDRReader(data)
	var exports []MountExport

	for {
		hasMore, err := r.ReadBool()
		if err != nil {
			return nil, fmt.Errorf("mount: parse exports: %w", err)
		}
		if !hasMore {
			break
		}

		dir, err := r.ReadString()
		if err != nil {
			return nil, fmt.Errorf("mount: parse export dir: %w", err)
		}

		var groups []string
		for {
			hasGroup, err := r.ReadBool()
			if err != nil {
				return nil, fmt.Errorf("mount: parse export groups: %w", err)
			}
			if !hasGroup {
				break
			}
			group, err := r.ReadString()
			if err != nil {
				return nil, fmt.Errorf("mount: parse group name: %w", err)
			}
			groups = append(groups, group)
		}

		exports = append(exports, MountExport{Dir: dir, Groups: groups})
	}

	return exports, nil
}

// Mount mounts an export path and returns the root file handle.
// For MOUNT v1 (NFSv2): file handle is fixed 32 bytes.
// For MOUNT v3 (NFSv3): file handle is variable length.
func (m *MountClient) Mount(ctx context.Context, exportPath string) ([]byte, error) {
	port, err := m.resolveMountPort(ctx)
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("%s:%d", m.host, port)
	client := NewClient(m.dialer, addr, m.auth)
	defer client.Close()

	reply, err := client.Call(ctx, MountProg, m.mountVer, MountProcMnt,
		func(w *XDRWriter) {
			w.WriteString(exportPath)
		})
	if err != nil {
		return nil, fmt.Errorf("mount: mnt %q: %w", exportPath, err)
	}

	r := NewXDRReader(reply)

	if m.mountVer == MountVers3 {
		// MOUNT v3: status + file handle (variable) + auth flavors
		status, err := r.ReadUint32()
		if err != nil {
			return nil, fmt.Errorf("mount: parse mnt status: %w", err)
		}
		if status != MNT_OK {
			return nil, fmt.Errorf("mount: mnt %q failed: status %d", exportPath, status)
		}
		fh, err := r.ReadOpaque()
		if err != nil {
			return nil, fmt.Errorf("mount: parse file handle: %w", err)
		}
		return fh, nil
	}

	// MOUNT v1: status + fixed 32-byte file handle
	status, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("mount: parse mnt status: %w", err)
	}
	if status != MNT_OK {
		return nil, fmt.Errorf("mount: mnt %q failed: status %d", exportPath, status)
	}
	fh, err := r.ReadFixedOpaque(32)
	if err != nil {
		return nil, fmt.Errorf("mount: parse v1 file handle: %w", err)
	}
	return fh, nil
}
