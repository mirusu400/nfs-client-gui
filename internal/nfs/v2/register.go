package v2

import (
	"github.com/mirusu400/nfsprobe/internal/nfs"
	"github.com/mirusu400/nfsprobe/internal/rpc"
	"github.com/mirusu400/nfsprobe/internal/transport"
)

func init() {
	nfs.Register(nfs.NFSv2, func(dialer transport.Dialer, host string, auth *rpc.AuthSysParams) nfs.Client {
		return New(dialer, host, auth)
	})
}
