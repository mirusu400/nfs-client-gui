# CLAUDE.md

> Working name: **nfsprobe** (rename freely). A cross-platform, GUI NFS browser
> for **authorized security assessments**. Think "FileZilla for NFS", but it
> speaks NFSv2/v3/v4 and tunnels every byte through a SOCKS5 proxy so it can be
> used from a pivot host during a pentest.

This file is the source of truth for design decisions. When a decision below has
a **Rationale**, treat it as a hard constraint — do not silently undo it.

---

## 1. Non-negotiable requirements

1. **Protocol coverage: NFSv2, NFSv3, NFSv4** — all three must work.
2. **Language: Go** for everything (backend + all protocol logic).
3. **v2 lives in its own repository** (see §4). The main app depends on it as a
   Go module.
4. **Proxy is mandatory, not optional.** Every outbound TCP connection the tool
   makes MUST be able to go through a SOCKS5 proxy. This is the whole point —
   the tool is expected to run against internal NFS servers reached only via a
   pivot. See §6; this is the central invariant.
5. **GUI, cross-platform.** macOS and Linux are required; Windows is a
   nice-to-have (should work but is lower priority).
6. **Browsing only** — list exports, walk directories, read/download files
   (upload optional). **No FUSE, no OS mounting.** We never touch a kernel NFS
   client.

---

## 2. Why not just use an existing library / proxychains

Context the agent must not re-litigate:

- **No off-the-shelf library covers v2.** `libnfs` is v3/v4 only
  (`version=<3|4>`). The pure-Go options are single-version:
  `go-nfs/nfsv3` (v3 only) and `Cyberax/go-nfs-client` (v4 only). Pure-Rust is
  v3-focused. Therefore **v2 must be implemented by us** on top of the shared
  RPC/XDR/MOUNT plumbing.
- **proxychains is not viable here.** It relies on `LD_PRELOAD`, which is
  Linux/glibc only (macOS SIP blocks injection, Windows has no equivalent), and
  it cannot intercept a *kernel* NFS client anyway. Because we own the socket
  layer in Go, we inject SOCKS5 natively instead. Do not add proxychains as a
  dependency or assume it.

---

## 3. Architecture & key decisions

- **We own the RPC/XDR/MOUNT/portmapper layer.**
  Rationale: it is the only way to (a) support v2, (b) route *every* connection
  through our own dialer, and (c) forge AUTH_SYS credentials — all required.
- **One transport abstraction, injected once (§6).** Every dial — portmapper
  (111), mountd, nfsd, and the v4 port (2049) — goes through the same `Dialer`.
  Rationale: a single choke point is the proxy guarantee. If any code path calls
  `net.Dial` directly, that's a bug.
- **Version-agnostic `Client` interface with per-version adapters (§5).** The
  GUI and transfer logic never branch on protocol version; only the adapter
  behind the interface does.
  Rationale: keeps v2's quirks (32-bit sizes, fixed file handles, MOUNTv1)
  contained, and lets us bolt v4 on without touching the UI.
- **AUTH_SYS with caller-supplied uid/gid (§7).** First-class feature, not a
  hack.
- **GUI = Wails v2** (Go backend + system webview + web frontend).
  Rationale: the two-pane / table / tree UI FileZilla needs is far easier and
  nicer in HTML/CSS than in an immediate-mode Go toolkit, while all NFS + SOCKS
  logic stays in Go. Wails uses the OS-native webview → small bundle.
  If the agent prefers a pure-Go single binary with no webview, **Fyne** is the
  only acceptable alternative — flag the tradeoff before switching.

---

## 4. Repository layout

**Repo A — `nfsprobe` (this repo): the Wails application.**

```
nfsprobe/
  CLAUDE.md
  wails.json
  go.mod
  main.go                 # Wails bootstrap
  app.go                  # Wails-bound methods (Connect, ListExports, List, Stat,
                          #   Read/Download, Upload, SetCreds, SetProxy)
  internal/
    transport/            # Dialer abstraction + SOCKS5 (THE injection point)
    rpc/                  # SUN RPC + XDR + portmapper + generic MOUNT client
    nfs/
      client.go           # version-agnostic Client interface (§5)
      negotiate.go        # version negotiation / fallback (§9)
      v3/                 # adapter over go-nfs/nfsv3
      v4/                 # adapter over Cyberax/go-nfs-client
      v2/                 # thin adapter that wraps Repo B
  frontend/               # Svelte or React two-pane UI
```

**Repo B — `go-nfs2` (separate module): the NFSv2 client.**

```
go-nfs2/
  go.mod                  # module github.com/<you>/go-nfs2
  nfs2/                   # NFSv2 procedures (RFC 1094)
  mount1/                 # MOUNT protocol v1 (RFC 1094 appendix)
```

- `go-nfs2` should reuse the same RPC/XDR primitives. Easiest path: extract the
  `rpc/` package into its own small module (e.g. `go-sunrpc`) that both repos
  import, OR have `go-nfs2` depend on `nfsprobe/internal/rpc` promoted to a
  public package. Decide early and note it here once chosen.
- `go-nfs2` must accept an injected `Dialer` and `AuthSys` — it must NOT open
  sockets on its own. Same proxy invariant applies inside it.

---

## 5. Core interface (target shape — refine as you build)

```go
// internal/nfs/client.go
type FileHandle []byte // opaque; v2 = fixed 32 bytes, v3 = variable <=64

type Attr struct {
    Type  FileType
    Mode  uint32
    UID   uint32
    GID   uint32
    Size  uint64 // v2 is 32-bit on the wire; widen to uint64 in our model
    MTime time.Time
}

type DirEntry struct {
    Name string
    FH   FileHandle
    Attr Attr
}

type Export struct {
    Dir    string
    Groups []string // host/netgroup allow-list as reported by MOUNT
}

// Every method takes context.Context and is cancelable.
type Client interface {
    Version() Version
    ListExports(ctx context.Context) ([]Export, error) // == showmount -e
    Mount(ctx context.Context, exportPath string) (root FileHandle, err error)
    ReadDir(ctx context.Context, dir FileHandle) ([]DirEntry, error)
    Lookup(ctx context.Context, dir FileHandle, name string) (FileHandle, Attr, error)
    GetAttr(ctx context.Context, fh FileHandle) (Attr, error)
    Read(ctx context.Context, fh FileHandle, offset uint64, count uint32) ([]byte, error)
    Write(ctx context.Context, fh FileHandle, offset uint64, data []byte) (uint32, error) // optional
    Close() error
}
```

v4 has no MOUNT protocol; its adapter implements `ListExports`/`Mount` via
PUTROOTFH + READDIR of the pseudo-fs. Keep the interface identical so the UI
doesn't care.

---

## 6. Transport / SOCKS5 — the proxy invariant

```go
// internal/transport/dialer.go
type Dialer interface {
    DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// Direct (no proxy).
func Direct() Dialer { return &net.Dialer{} }

// SOCKS5 via golang.org/x/net/proxy.
func SOCKS5(proxyAddr string, auth *proxy.Auth) (Dialer, error)
```

Rules:
- **All** protocol packages receive a `Dialer` and use ONLY it. No bare
  `net.Dial` anywhere in `internal/rpc`, `internal/nfs/*`, or `go-nfs2`.
- **Remote DNS:** dial with the target *hostname*, not a pre-resolved IP, so the
  SOCKS5 proxy resolves it (x/net/proxy sends FQDN address type). During a pivot
  you usually cannot resolve internal names locally — let the proxy do it.
- Portmapper, mountd, nfsd, and the v4 2049 dial all funnel through this one
  `Dialer`. If a new dial site appears, it takes a `Dialer` param — no
  exceptions.
- UDP is out of scope: force TCP everywhere (NFSv4 requires it; v3/v2 support it
  too). SOCKS5 UDP-associate is unreliable and we don't need it.

---

## 7. AUTH_SYS credentials + pentest features

```go
// internal/rpc/authsys.go
type AuthSys struct {
    Stamp   uint32
    Machine string
    UID     uint32
    GID     uint32
    GIDs    []uint32
}
```

- The credential is caller-controlled and settable from the GUI. The classic
  assessment move against `no_root_squash` / world-readable exports is to
  present `UID=0` (or the file owner's uid) and re-read. Make uid/gid/aux-gids
  editable at runtime, and allow re-issuing a request with new creds without
  reconnecting.
- Ship **export enumeration** (`ListExports`, i.e. `showmount -e`) and
  **portmapper enumeration** (dump registered RPC programs on 111) as explicit
  recon actions surfaced in the UI.
- **Privileged-port caveat (document in UI):** servers exported with `secure`
  expect a client source port <1024. Through a SOCKS proxy we do not control the
  source port, so `secure` exports may reject us while `insecure` ones work.
  Surface this as a likely explanation for auth failures rather than a false
  "not vulnerable" conclusion.

---

## 8. NFS version cheat-sheet (keep handy while implementing)

| | NFSv2 | NFSv3 | NFSv4 |
|---|---|---|---|
| Spec | RFC 1094 | RFC 1813 | RFC 7530 / 5661 |
| Portmapper (111) | yes | yes | **no** |
| MOUNT protocol | v1 (RFC 1094) | v3 (RFC 1813) | **none** (PUTROOTFH) |
| Well-known port | via portmapper | via portmapper | **2049 fixed** |
| File handle | **fixed 32 bytes** | variable <=64 bytes | variable |
| File size | **32-bit (2GB cap)** | 64-bit | 64-bit |
| Transport | UDP/TCP (we force TCP) | UDP/TCP (we force TCP) | TCP only |
| Proxy story | multi-dial (pmap→mnt→nfs) | multi-dial | **single dial, cleanest** |

RPC itself: RFC 5531 (older RFC 1057); portmapper/rpcbind: RFC 1833.

Implication for v2/v3: dial 111 → `PMAPPROC_GETPORT` for mountd → MOUNT the
export → `GETPORT` for nfsd → NFS ops. All dials via the injected `Dialer`.

---

## 9. Version negotiation

- Default behavior: try **v4** (direct 2049), then **v3**, then **v2**. First
  that answers wins.
- Must also allow **forcing** a specific version from the CLI/GUI (pentest often
  needs to probe exactly one version and observe the server's behavior).
- Record and display which version/transport actually connected.

---

## 10. Dependencies

- `golang.org/x/net/proxy` — SOCKS5 dialer. (Stable, trust it.)
- NFSv3 adapter: the `go-nfs/nfsv3` lineage (VMware/davecheney fork family).
- NFSv4 adapter: `Cyberax/go-nfs-client` (v4 only).
- GUI: Wails v2 (`wails.io`), frontend framework Svelte or React (pick one, note
  it here).

> **Verify exact module import paths from each repo's `go.mod` before importing**
> — the `go-nfs-client` name is used by several unrelated projects and paths
> differ between forks. Don't guess; check the repo.

These adapters are minimal (limited/no locking, AUTH_SYS only, no Kerberos).
That is acceptable — we are a browser, not a general-purpose FS. Do not pull in
heavier deps to "fix" this.

---

## 11. Build / run

```bash
# dev (hot reload UI)
wails dev

# production build for the host OS
wails build

# Go core is pure Go and OS-independent — test it anywhere:
go test ./...
```

Cross-platform reality:
- `wails build` targets the **host** OS. Ship all three via a CI matrix
  (GitHub Actions: macos-latest, ubuntu-latest, windows-latest). Wails cannot
  build the Linux target from macOS/Windows.
- macOS distribution needs codesign + notarization (Apple Developer, paid) — out
  of scope until there's something to ship.
- Linux uses WebKitGTK (the weakest system webview); keep the UI simple to avoid
  renderer-specific bugs.

---

## 12. Conventions

- Pass `context.Context` to every network call; make everything cancelable
  (connect, list, read must abort cleanly — important on dead/hanging servers).
- No package-level mutable state. Wire a `Dialer` and `AuthSys` through
  explicitly.
- Wrap protocol errors with the RPC/NFS status code preserved (e.g.
  `NFS3ERR_ACCES`), and map to a small typed error set the UI can branch on.
- Keep the protocol packages GUI-agnostic (no Wails imports below `app.go`) so
  the core stays reusable as a library/CLI.
- Timeouts and a hard per-op deadline everywhere; NFS servers behind a pivot go
  silent often.

---

## 13. Scope & roadmap

In scope: connect, enumerate exports, browse dirs, read/download files, custom
AUTH_SYS creds, SOCKS5 proxy, v2/v3/v4, portmapper recon.

Out of scope (for now): mounting/FUSE, file locking (NLM), Kerberos (`sec=krb5`),
UDP, ACL editing, write-heavy workflows.

Suggested build order:
1. `internal/transport` (Dialer + SOCKS5) + `internal/rpc` (RPC/XDR/portmapper/
   MOUNT) — the shared spine, proxy-aware from line one.
2. v3 adapter behind `Client` (most servers, best library) → prove end-to-end:
   connect via SOCKS5 → `showmount -e` → browse → download.
3. Minimal Wails GUI: two-pane browser + connection dialog (host, export, proxy,
   uid/gid) + transfer.
4. `go-nfs2` (separate repo): MOUNTv1 + NFSv2 read path → v2 adapter.
5. v4 adapter (`Cyberax`) + version negotiation.
6. Recon niceties: portmapper dump, uid/gid quick-switch, secure/insecure hint.

---

## 14. Known gotchas

- Any direct `net.Dial` = proxy leak. Grep for it in CI.
- v2 file handles are exactly 32 bytes and sizes are 32-bit — don't reuse v3
  XDR structs for v2.
- Pass hostnames (not IPs) into the dialer so DNS resolves at the proxy.
- `secure` exports + SOCKS = likely rejection (source port); not a vuln verdict.
- Wails can't cross-compile Linux from mac/win; use CI.

---

## 15. Local test servers

Stand these up in Docker to develop against each version:
- v3/v4: `nfs-ganesha` (userspace, supports v3 + v4), or Linux `knfsd`.
- v3: `willscott/go-nfs` (pure-Go NFSv3 server) is handy for quick tests.
- v2: hardest — modern `knfsd` disables v2. Use an old/userspace daemon
  (`unfsd`/UNFS3-style) or a pinned old kernel image with `vers=2` enabled.
  Cross-check your v2 client against `nmap` nfs NSE and Metasploit `nfsmount`.
