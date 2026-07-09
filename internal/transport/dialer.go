// Package transport provides a Dialer abstraction that funnels every outbound
// TCP connection through a single injection point. When a SOCKS5 proxy is
// configured the dialer routes traffic through it; otherwise it dials directly.
//
// This is THE proxy invariant: no code outside this package may call net.Dial.
package transport

import (
	"context"
	"fmt"
	"net"

	"golang.org/x/net/proxy"
)

// Dialer is the single abstraction every protocol package uses to open TCP
// connections. Implementations must be safe for concurrent use.
type Dialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// Direct returns a Dialer that connects without a proxy.
func Direct() Dialer {
	return &net.Dialer{}
}

// SOCKS5 returns a Dialer that routes every connection through the given
// SOCKS5 proxy. proxyAddr is "host:port". auth may be nil for unauthenticated
// proxies.
//
// Important: callers must pass hostnames (not resolved IPs) into DialContext so
// that DNS resolution happens at the proxy side — critical when pivoting into
// a network whose names are not resolvable locally.
func SOCKS5(proxyAddr string, auth *proxy.Auth) (Dialer, error) {
	// proxy.SOCKS5 returns a proxy.Dialer, but we need DialContext support.
	// Use proxy.SOCKS5 with a direct forwarder so x/net handles the SOCKS5
	// handshake, and wrap it to satisfy our Dialer interface.
	d, err := proxy.SOCKS5("tcp", proxyAddr, auth, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("transport: socks5 proxy %q: %w", proxyAddr, err)
	}

	// proxy.SOCKS5 returns a proxy.ContextDialer since Go 1.17+ x/net versions.
	if cd, ok := d.(proxy.ContextDialer); ok {
		return &contextDialerWrapper{cd: cd}, nil
	}

	// Fallback: wrap the non-context dialer (loses cancelation but still proxies).
	return &legacyDialerWrapper{d: d}, nil
}

type contextDialerWrapper struct {
	cd proxy.ContextDialer
}

func (w *contextDialerWrapper) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return w.cd.DialContext(ctx, network, addr)
}

type legacyDialerWrapper struct {
	d proxy.Dialer
}

func (w *legacyDialerWrapper) DialContext(_ context.Context, network, addr string) (net.Conn, error) {
	return w.d.Dial(network, addr)
}
