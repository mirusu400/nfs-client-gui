package rpc

import (
	"context"
	"fmt"

	"github.com/mirusu400/nfs-client-gui/internal/transport"
)

// Portmapper / rpcbind constants (RFC 1833).
const (
	PortmapperPort    = 111
	PortmapperProg    = 100000
	PortmapperVers    = 2
	PmapProcNull      = 0
	PmapProcGetPort   = 3
	PmapProcDump      = 4
)

// Transport protocols for portmapper.
const (
	IPProtoTCP = 6
	IPProtoUDP = 17
)

// PortMapping represents a single portmapper mapping entry.
type PortMapping struct {
	Prog  uint32
	Vers  uint32
	Prot  uint32
	Port  uint32
}

// Portmapper queries the portmapper/rpcbind service on a remote host.
type Portmapper struct {
	client *Client
}

// NewPortmapper creates a portmapper client for the given host.
// All connections go through the provided dialer.
func NewPortmapper(dialer transport.Dialer, host string, auth *AuthSysParams) *Portmapper {
	addr := fmt.Sprintf("%s:%d", host, PortmapperPort)
	return &Portmapper{
		client: NewClient(dialer, addr, auth),
	}
}

// GetPort queries the portmapper for the port of a given program/version over TCP.
func (p *Portmapper) GetPort(ctx context.Context, prog, vers uint32) (uint32, error) {
	reply, err := p.client.Call(ctx, PortmapperProg, PortmapperVers, PmapProcGetPort,
		func(w *XDRWriter) {
			w.WriteUint32(prog)      // program
			w.WriteUint32(vers)      // version
			w.WriteUint32(IPProtoTCP) // protocol (we always use TCP)
			w.WriteUint32(0)          // port (ignored in request)
		})
	if err != nil {
		return 0, fmt.Errorf("portmapper: getport prog=%d vers=%d: %w", prog, vers, err)
	}

	r := NewXDRReader(reply)
	port, err := r.ReadUint32()
	if err != nil {
		return 0, fmt.Errorf("portmapper: parse getport reply: %w", err)
	}
	if port == 0 {
		return 0, fmt.Errorf("portmapper: prog=%d vers=%d not registered", prog, vers)
	}
	return port, nil
}

// Dump returns all registered portmapper mappings (PMAPPROC_DUMP).
// Useful for recon: see which RPC services are available.
func (p *Portmapper) Dump(ctx context.Context) ([]PortMapping, error) {
	reply, err := p.client.Call(ctx, PortmapperProg, PortmapperVers, PmapProcDump, nil)
	if err != nil {
		return nil, fmt.Errorf("portmapper: dump: %w", err)
	}

	r := NewXDRReader(reply)
	var mappings []PortMapping

	for {
		hasMore, err := r.ReadBool()
		if err != nil {
			return nil, fmt.Errorf("portmapper: parse dump: %w", err)
		}
		if !hasMore {
			break
		}

		var m PortMapping
		if m.Prog, err = r.ReadUint32(); err != nil {
			return nil, err
		}
		if m.Vers, err = r.ReadUint32(); err != nil {
			return nil, err
		}
		if m.Prot, err = r.ReadUint32(); err != nil {
			return nil, err
		}
		if m.Port, err = r.ReadUint32(); err != nil {
			return nil, err
		}
		mappings = append(mappings, m)
	}

	return mappings, nil
}

// Close closes the portmapper connection.
func (p *Portmapper) Close() error {
	return p.client.Close()
}
