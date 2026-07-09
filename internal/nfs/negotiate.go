package nfs

import (
	"context"
	"fmt"

	"github.com/mirusu400/nfs-client-gui/internal/rpc"
	"github.com/mirusu400/nfs-client-gui/internal/transport"
)

// ClientFactory creates a Client for a specific NFS version.
type ClientFactory func(dialer transport.Dialer, host string, auth *rpc.AuthSysParams) Client

// registry holds version-specific client factories.
var registry = map[Version]ClientFactory{}

// Register registers a client factory for an NFS version.
// Called by version-specific packages in their init() functions.
func Register(version Version, factory ClientFactory) {
	registry[version] = factory
}

// ConnectOptions configures how to connect to an NFS server.
type ConnectOptions struct {
	Host     string
	Dialer   transport.Dialer
	Auth     *rpc.AuthSysParams
	// ForceVersion, if set, skips negotiation and uses this version directly.
	ForceVersion *Version
}

// Connect establishes an NFS connection with version negotiation.
// Default order: v4 → v3 → v2 (first that works wins).
// If ForceVersion is set, only that version is tried.
func Connect(ctx context.Context, opts ConnectOptions) (Client, error) {
	if opts.Dialer == nil {
		opts.Dialer = transport.Direct()
	}
	if opts.Auth == nil {
		opts.Auth = rpc.DefaultAuthSys()
	}

	if opts.ForceVersion != nil {
		return connectVersion(ctx, opts, *opts.ForceVersion)
	}

	// Try versions in preference order: v4, v3, v2.
	for _, v := range []Version{NFSv4, NFSv3, NFSv2} {
		client, err := connectVersion(ctx, opts, v)
		if err != nil {
			continue
		}
		return client, nil
	}

	return nil, fmt.Errorf("nfs: no supported version available on %s", opts.Host)
}

func connectVersion(_ context.Context, opts ConnectOptions, v Version) (Client, error) {
	factory, ok := registry[v]
	if !ok {
		return nil, fmt.Errorf("nfs: %s adapter not registered", v)
	}
	return factory(opts.Dialer, opts.Host, opts.Auth), nil
}
