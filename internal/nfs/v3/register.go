package v3

import (
	"github.com/mirusu400/nfsprobe/internal/nfs"
	"github.com/mirusu400/nfsprobe/internal/rpc"
	"github.com/mirusu400/nfsprobe/internal/transport"
)

func init() {
	nfs.Register(nfs.NFSv3, func(dialer transport.Dialer, host string, auth *rpc.AuthSysParams) nfs.Client {
		return New(dialer, host, auth)
	})
}
