// Package nfs defines the version-agnostic NFS client interface.
// Per-version adapters (v2, v3, v4) implement this interface so the GUI
// and transfer logic never branch on protocol version.
package nfs

import (
	"context"
	"fmt"
	"time"
)

// Version represents an NFS protocol version.
type Version int

const (
	NFSv2 Version = 2
	NFSv3 Version = 3
	NFSv4 Version = 4
)

func (v Version) String() string {
	return fmt.Sprintf("NFSv%d", int(v))
}

// FileType represents the type of a file (regular, directory, etc).
type FileType uint32

const (
	FileTypeRegular   FileType = 1
	FileTypeDirectory FileType = 2
	FileTypeBlock     FileType = 3
	FileTypeChar      FileType = 4
	FileTypeSymlink   FileType = 5
	FileTypeSocket    FileType = 6
	FileTypeFIFO      FileType = 7
)

func (ft FileType) String() string {
	switch ft {
	case FileTypeRegular:
		return "file"
	case FileTypeDirectory:
		return "dir"
	case FileTypeBlock:
		return "block"
	case FileTypeChar:
		return "char"
	case FileTypeSymlink:
		return "symlink"
	case FileTypeSocket:
		return "socket"
	case FileTypeFIFO:
		return "fifo"
	default:
		return fmt.Sprintf("unknown(%d)", ft)
	}
}

// FileHandle is an opaque file handle.
// NFSv2: fixed 32 bytes. NFSv3: variable, up to 64 bytes. NFSv4: variable.
type FileHandle []byte

// Attr holds file attributes, normalized across NFS versions.
type Attr struct {
	Type  FileType
	Mode  uint32
	NLink uint32
	UID   uint32
	GID   uint32
	Size  uint64 // v2 is 32-bit on wire; widened to uint64 in our model
	MTime time.Time
	ATime time.Time
}

// DirEntry is a single directory listing entry.
type DirEntry struct {
	Name string
	FH   FileHandle
	Attr Attr
}

// Export describes a single NFS export as reported by the MOUNT protocol.
type Export struct {
	Dir    string
	Groups []string // host/netgroup allow-list
}

// Client is the version-agnostic NFS interface. Every method takes
// context.Context for cancelation and deadline support.
type Client interface {
	// Version returns the NFS protocol version this client speaks.
	Version() Version

	// ListExports returns the server's exported filesystems (showmount -e).
	ListExports(ctx context.Context) ([]Export, error)

	// Mount mounts the given export path and returns the root file handle.
	Mount(ctx context.Context, exportPath string) (FileHandle, error)

	// ReadDir lists the contents of a directory.
	ReadDir(ctx context.Context, dir FileHandle) ([]DirEntry, error)

	// Lookup finds a named entry in a directory and returns its handle + attrs.
	Lookup(ctx context.Context, dir FileHandle, name string) (FileHandle, Attr, error)

	// GetAttr returns attributes for a file handle.
	GetAttr(ctx context.Context, fh FileHandle) (Attr, error)

	// Read reads data from a file at the given offset.
	Read(ctx context.Context, fh FileHandle, offset uint64, count uint32) ([]byte, error)

	// Write writes data to a file at the given offset (optional — may return ErrNotSupported).
	Write(ctx context.Context, fh FileHandle, offset uint64, data []byte) (uint32, error)

	// Close releases resources (connections, etc).
	Close() error
}
