package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/net/proxy"

	"github.com/mirusu400/nfs-client-gui/internal/nfs"
	v2 "github.com/mirusu400/nfs-client-gui/internal/nfs/v2"
	v3 "github.com/mirusu400/nfs-client-gui/internal/nfs/v3"
	v4 "github.com/mirusu400/nfs-client-gui/internal/nfs/v4"
	"github.com/mirusu400/nfs-client-gui/internal/rpc"
	"github.com/mirusu400/nfs-client-gui/internal/transport"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails-bound application struct.
type App struct {
	ctx context.Context

	mu     sync.Mutex
	client nfs.Client
	dialer transport.Dialer
	auth   *rpc.AuthSysParams
	host   string
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{
		auth: rpc.DefaultAuthSys(),
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// --- Connection ---

// ConnectRequest is the frontend connection form data.
type ConnectRequest struct {
	Host         string `json:"host"`
	ProxyAddr    string `json:"proxyAddr"`
	ProxyUser    string `json:"proxyUser"`
	ProxyPass    string `json:"proxyPass"`
	UID          uint32 `json:"uid"`
	GID          uint32 `json:"gid"`
	ForceVersion int    `json:"forceVersion"` // 0=auto, 2/3/4=force
}

// ConnectResult is returned after a connection attempt.
type ConnectResult struct {
	Success bool   `json:"success"`
	Version string `json:"version"`
	Error   string `json:"error"`
}

// Connect establishes a connection to an NFS server.
func (a *App) Connect(req ConnectRequest) ConnectResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Close existing connection.
	if a.client != nil {
		a.client.Close()
		a.client = nil
	}

	// Set up dialer.
	var d transport.Dialer
	if req.ProxyAddr != "" {
		var pauth *proxy.Auth
		if req.ProxyUser != "" {
			pauth = &proxy.Auth{User: req.ProxyUser, Password: req.ProxyPass}
		}
		var err error
		d, err = transport.SOCKS5(req.ProxyAddr, pauth)
		if err != nil {
			return ConnectResult{Error: err.Error()}
		}
	} else {
		d = transport.Direct()
	}
	a.dialer = d
	a.host = req.Host

	// Set up auth.
	a.auth = &rpc.AuthSysParams{
		Stamp:   a.auth.Stamp,
		Machine: a.auth.Machine,
		UID:     req.UID,
		GID:     req.GID,
		GIDs:    []uint32{req.GID},
	}

	// Create client based on version selection.
	var client nfs.Client
	switch req.ForceVersion {
	case 4:
		client = v4.New(d, req.Host, a.auth)
	case 3:
		client = v3.New(d, req.Host, a.auth)
	case 2:
		client = v2.New(d, req.Host, a.auth)
	default: // auto: try v4, then v3
		client = v4.New(d, req.Host, a.auth)
	}

	a.client = client
	return ConnectResult{
		Success: true,
		Version: client.Version().String(),
	}
}

// Disconnect closes the current NFS connection.
func (a *App) Disconnect() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil {
		a.client.Close()
		a.client = nil
	}
}

// --- Export enumeration ---

// ExportInfo represents an export for the frontend.
type ExportInfo struct {
	Dir    string   `json:"dir"`
	Groups []string `json:"groups"`
}

// ListExports returns the server's exports (showmount -e).
func (a *App) ListExports() ([]ExportInfo, error) {
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()

	if client == nil {
		return nil, fmt.Errorf("not connected")
	}

	exports, err := client.ListExports(a.ctx)
	if err != nil {
		return nil, err
	}

	result := make([]ExportInfo, len(exports))
	for i, e := range exports {
		result[i] = ExportInfo{Dir: e.Dir, Groups: e.Groups}
	}
	return result, nil
}

// --- Mount + Browse ---

// MountResult is returned after mounting an export.
type MountResult struct {
	Success    bool   `json:"success"`
	RootHandle string `json:"rootHandle"` // hex-encoded
	Error      string `json:"error"`
}

// MountExport mounts the given export path.
func (a *App) MountExport(exportPath string) MountResult {
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()

	if client == nil {
		return MountResult{Error: "not connected"}
	}

	fh, err := client.Mount(a.ctx, exportPath)
	if err != nil {
		return MountResult{Error: err.Error()}
	}

	return MountResult{
		Success:    true,
		RootHandle: hex.EncodeToString(fh),
	}
}

// FileInfo represents a file/directory entry for the frontend.
type FileInfo struct {
	Name   string `json:"name"`
	Handle string `json:"handle"` // hex-encoded
	Type   string `json:"type"`
	Mode   string `json:"mode"`
	UID    uint32 `json:"uid"`
	GID    uint32 `json:"gid"`
	Size   uint64 `json:"size"`
	MTime  string `json:"mtime"`
}

// ListDir lists the contents of a directory by its hex-encoded file handle.
func (a *App) ListDir(handleHex string) ([]FileInfo, error) {
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()

	if client == nil {
		return nil, fmt.Errorf("not connected")
	}

	fh, err := hex.DecodeString(handleHex)
	if err != nil {
		return nil, fmt.Errorf("invalid handle: %w", err)
	}

	entries, err := client.ReadDir(a.ctx, nfs.FileHandle(fh))
	if err != nil {
		return nil, err
	}

	result := make([]FileInfo, len(entries))
	for i, e := range entries {
		result[i] = FileInfo{
			Name:   e.Name,
			Handle: hex.EncodeToString(e.FH),
			Type:   e.Attr.Type.String(),
			Mode:   fmt.Sprintf("%04o", e.Attr.Mode),
			UID:    e.Attr.UID,
			GID:    e.Attr.GID,
			Size:   e.Attr.Size,
			MTime:  e.Attr.MTime.Format("2006-01-02 15:04:05"),
		}
	}
	return result, nil
}

// --- File download ---

// DownloadFile downloads a file to a user-chosen location.
func (a *App) DownloadFile(handleHex string, fileName string) (string, error) {
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()

	if client == nil {
		return "", fmt.Errorf("not connected")
	}

	fh, err := hex.DecodeString(handleHex)
	if err != nil {
		return "", fmt.Errorf("invalid handle: %w", err)
	}

	// Ask user where to save.
	savePath, err := wailsRuntime.SaveFileDialog(a.ctx, wailsRuntime.SaveDialogOptions{
		DefaultFilename: fileName,
		Title:           "Save file as...",
	})
	if err != nil {
		return "", err
	}
	if savePath == "" {
		return "", nil // user cancelled
	}

	// Get file size.
	attr, err := client.GetAttr(a.ctx, nfs.FileHandle(fh))
	if err != nil {
		return "", fmt.Errorf("getattr: %w", err)
	}

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
		return "", err
	}

	f, err := os.Create(savePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Read in chunks.
	const chunkSize = 32768
	var offset uint64
	for offset < attr.Size {
		count := uint32(chunkSize)
		remaining := attr.Size - offset
		if uint64(count) > remaining {
			count = uint32(remaining)
		}

		data, err := client.Read(a.ctx, nfs.FileHandle(fh), offset, count)
		if err != nil {
			return "", fmt.Errorf("read at offset %d: %w", offset, err)
		}
		if len(data) == 0 {
			break
		}

		if _, err := f.Write(data); err != nil {
			return "", err
		}
		offset += uint64(len(data))
	}

	return savePath, nil
}

// --- Credentials ---

// SetCredentials updates AUTH_SYS credentials at runtime.
func (a *App) SetCredentials(uid, gid uint32) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.auth.UID = uid
	a.auth.GID = gid
	a.auth.GIDs = []uint32{gid}

	if a.client != nil {
		switch c := a.client.(type) {
		case *v2.Client:
			c.SetAuth(a.auth)
		case *v3.Client:
			c.SetAuth(a.auth)
		case *v4.Client:
			c.SetAuth(a.auth)
		}
	}
}

// --- Portmapper recon ---

// PortmapEntry represents a portmapper mapping for the frontend.
type PortmapEntry struct {
	Program  uint32 `json:"program"`
	Version  uint32 `json:"version"`
	Protocol string `json:"protocol"`
	Port     uint32 `json:"port"`
}

// DumpPortmapper returns all registered RPC services on port 111.
func (a *App) DumpPortmapper() ([]PortmapEntry, error) {
	a.mu.Lock()
	dialer := a.dialer
	host := a.host
	auth := a.auth
	a.mu.Unlock()

	if dialer == nil || host == "" {
		return nil, fmt.Errorf("not connected")
	}

	pm := rpc.NewPortmapper(dialer, host, auth)
	defer pm.Close()

	mappings, err := pm.Dump(a.ctx)
	if err != nil {
		return nil, err
	}

	result := make([]PortmapEntry, len(mappings))
	for i, m := range mappings {
		proto := "tcp"
		if m.Prot == rpc.IPProtoUDP {
			proto = "udp"
		}
		result[i] = PortmapEntry{
			Program:  m.Prog,
			Version:  m.Vers,
			Protocol: proto,
			Port:     m.Port,
		}
	}
	return result, nil
}

