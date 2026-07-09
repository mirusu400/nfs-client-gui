<script>
  import { Connect, Disconnect, ListExports, MountExport, ListDir, DownloadFile, SetCredentials, DumpPortmapper } from '../wailsjs/go/main/App.js'

  let connected = $state(false)
  let connVersion = $state('')
  let statusMsg = $state('')
  let loading = $state(false)

  // Connection form
  let host = $state('')
  let proxyAddr = $state('')
  let proxyUser = $state('')
  let proxyPass = $state('')
  let uid = $state(0)
  let gid = $state(0)
  let forceVersion = $state(0)
  let showAdvanced = $state(false)

  // Exports
  let exports = $state([])
  let showExports = $state(false)

  // Browser
  let mounted = $state(false)
  let currentExport = $state('')
  let rootHandle = $state('')
  let dirStack = $state([])
  let entries = $state([])

  // Portmapper
  let portmapEntries = $state([])
  let showPortmap = $state(false)

  // Credentials modal
  let showCreds = $state(false)
  let newUid = $state(0)
  let newGid = $state(0)

  // Active tab
  let activeTab = $state('exports')

  async function doConnect() {
    loading = true
    statusMsg = ''
    try {
      const result = await Connect({
        host, proxyAddr, proxyUser, proxyPass,
        uid, gid, forceVersion
      })
      if (result.success) {
        connected = true
        connVersion = result.version
        statusMsg = `Connected via ${result.version}`
      } else {
        statusMsg = result.error
      }
    } catch (e) {
      statusMsg = `${e}`
    }
    loading = false
  }

  async function doDisconnect() {
    await Disconnect()
    connected = false
    mounted = false
    exports = []
    entries = []
    dirStack = []
    portmapEntries = []
    showExports = false
    showPortmap = false
    statusMsg = ''
    activeTab = 'exports'
  }

  async function doListExports() {
    loading = true
    try {
      exports = await ListExports()
      activeTab = 'exports'
    } catch (e) {
      statusMsg = `${e}`
    }
    loading = false
  }

  async function doMount(dir) {
    loading = true
    try {
      const result = await MountExport(dir)
      if (result.success) {
        mounted = true
        currentExport = dir
        rootHandle = result.rootHandle
        dirStack = [{ name: dir, handle: result.rootHandle }]
        await loadDir(result.rootHandle)
        activeTab = 'browser'
        statusMsg = `Mounted ${dir}`
      } else {
        statusMsg = result.error
      }
    } catch (e) {
      statusMsg = `${e}`
    }
    loading = false
  }

  async function loadDir(handle) {
    loading = true
    try {
      entries = await ListDir(handle)
    } catch (e) {
      statusMsg = `${e}`
    }
    loading = false
  }

  async function navigateTo(entry) {
    if (entry.type === 'dir') {
      dirStack = [...dirStack, { name: entry.name, handle: entry.handle }]
      await loadDir(entry.handle)
    }
  }

  async function navigateBreadcrumb(index) {
    dirStack = dirStack.slice(0, index + 1)
    await loadDir(dirStack[dirStack.length - 1].handle)
  }

  async function doDownload(entry) {
    try {
      const path = await DownloadFile(entry.handle, entry.name)
      if (path) statusMsg = `Saved to ${path}`
    } catch (e) {
      statusMsg = `${e}`
    }
  }

  async function doSetCredentials() {
    SetCredentials(newUid, newGid)
    uid = newUid
    gid = newGid
    showCreds = false
    statusMsg = `Credentials: uid=${newUid}, gid=${newGid}`
  }

  async function doDumpPortmap() {
    loading = true
    try {
      portmapEntries = await DumpPortmapper()
      activeTab = 'portmap'
    } catch (e) {
      statusMsg = `${e}`
    }
    loading = false
  }

  function formatSize(bytes) {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} K`
    if (bytes < 1073741824) return `${(bytes / 1048576).toFixed(1)} M`
    return `${(bytes / 1073741824).toFixed(2)} G`
  }

  function formatMode(mode) {
    const perms = ['---', '--x', '-w-', '-wx', 'r--', 'r-x', 'rw-', 'rwx']
    const m = parseInt(mode, 8)
    return perms[(m >> 6) & 7] + perms[(m >> 3) & 7] + perms[m & 7]
  }

  const programNames = {
    100000: 'portmapper', 100003: 'nfs', 100005: 'mountd',
    100021: 'nlockmgr', 100024: 'status', 100227: 'nfs_acl',
  }
</script>

<div class="app">
  <!-- Sidebar -->
  <aside class="sidebar">
    <div class="logo">
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
      </svg>
      <span>NFS Client</span>
    </div>

    {#if connected}
      <div class="sidebar-section">
        <div class="conn-badge">
          <span class="dot"></span>
          {connVersion}
        </div>
        <div class="conn-host" title={host}>{host}</div>
      </div>

      <nav class="sidebar-nav">
        <button class:active={activeTab === 'exports'}
                onclick={() => { activeTab = 'exports'; doListExports() }}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7l-2-2H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2z"/></svg>
          Exports
        </button>
        {#if mounted}
          <button class:active={activeTab === 'browser'}
                  onclick={() => activeTab = 'browser'}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/></svg>
            Browse
          </button>
        {/if}
        <button class:active={activeTab === 'portmap'}
                onclick={() => { activeTab = 'portmap'; doDumpPortmap() }}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          Portmapper
        </button>
      </nav>

      <div class="sidebar-bottom">
        <button class="sidebar-action" onclick={() => { showCreds = true; newUid = uid; newGid = gid }}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
          uid:{uid} gid:{gid}
        </button>
        <button class="sidebar-action disconnect" onclick={doDisconnect}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>
          Disconnect
        </button>
      </div>
    {/if}
  </aside>

  <!-- Main content -->
  <div class="content">
    {#if !connected}
      <!-- Connection screen -->
      <div class="connect-wrapper">
        <div class="connect-card">
          <div class="connect-header">
            <h2>Connect to NFS Server</h2>
            <p>Browse remote NFS exports via SOCKS5 proxy</p>
          </div>

          <div class="field">
            <label for="host">Target Host</label>
            <input id="host" type="text" bind:value={host}
                   placeholder="10.10.10.1 or nfs.internal"
                   onkeydown={(e) => e.key === 'Enter' && host && doConnect()} />
          </div>

          <div class="field-row">
            <div class="field">
              <label for="proxy">SOCKS5 Proxy</label>
              <input id="proxy" type="text" bind:value={proxyAddr} placeholder="127.0.0.1:1080" />
            </div>
            <div class="field">
              <label for="version">Protocol</label>
              <select id="version" bind:value={forceVersion}>
                <option value={0}>Auto-negotiate</option>
                <option value={2}>NFSv2</option>
                <option value={3}>NFSv3</option>
                <option value={4}>NFSv4</option>
              </select>
            </div>
          </div>

          <button class="toggle-advanced" onclick={() => showAdvanced = !showAdvanced}>
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                 style="transform: rotate({showAdvanced ? 90 : 0}deg); transition: transform 0.15s">
              <polyline points="9 18 15 12 9 6"/>
            </svg>
            Advanced options
          </button>

          {#if showAdvanced}
            <div class="advanced-fields">
              <div class="field-row">
                <div class="field">
                  <label for="puser">Proxy Username</label>
                  <input id="puser" type="text" bind:value={proxyUser} placeholder="optional" />
                </div>
                <div class="field">
                  <label for="ppass">Proxy Password</label>
                  <input id="ppass" type="password" bind:value={proxyPass} placeholder="optional" />
                </div>
              </div>
              <div class="field-row">
                <div class="field">
                  <label for="uid">UID</label>
                  <input id="uid" type="number" bind:value={uid} />
                </div>
                <div class="field">
                  <label for="gid">GID</label>
                  <input id="gid" type="number" bind:value={gid} />
                </div>
              </div>
            </div>
          {/if}

          <button class="connect-btn" onclick={doConnect} disabled={loading || !host}>
            {#if loading}
              <span class="spinner-inline"></span> Connecting...
            {:else}
              Connect
            {/if}
          </button>

          {#if statusMsg}
            <div class="connect-error">{statusMsg}</div>
          {/if}

          <div class="connect-note">
            <strong>Note:</strong> Servers with <code>secure</code> exports may reject connections
            through SOCKS proxy (source port &gt; 1024). This is not a vulnerability verdict.
          </div>
        </div>
      </div>

    {:else}
      <!-- Connected content area -->
      <div class="content-inner">
        {#if activeTab === 'exports'}
          <div class="page-header">
            <h2>Exports</h2>
            <span class="page-hint">Available NFS shares on {host}</span>
          </div>
          {#if exports.length > 0}
            <div class="card-grid">
              {#each exports as exp}
                <button class="export-card" onclick={() => doMount(exp.dir)}>
                  <div class="export-icon">
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                  </div>
                  <div class="export-info">
                    <div class="export-path">{exp.dir}</div>
                    <div class="export-hosts">{exp.groups?.length > 0 ? exp.groups.join(', ') : 'everyone'}</div>
                  </div>
                  <svg class="export-arrow" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg>
                </button>
              {/each}
            </div>
          {:else if !loading}
            <div class="empty-state">
              <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
              <p>Click to fetch exports from {host}</p>
              <button class="btn-secondary" onclick={doListExports}>Fetch Exports</button>
            </div>
          {/if}

        {:else if activeTab === 'browser'}
          <div class="page-header">
            <h2>
              {currentExport}
            </h2>
            <nav class="breadcrumb">
              {#each dirStack as crumb, i}
                {#if i > 0}<span class="sep">/</span>{/if}
                <button class="crumb" class:active={i === dirStack.length - 1}
                        onclick={() => navigateBreadcrumb(i)}>
                  {i === 0 ? '~' : crumb.name}
                </button>
              {/each}
            </nav>
          </div>

          <div class="file-list">
            <div class="file-header">
              <span class="col-name">Name</span>
              <span class="col-perm">Permissions</span>
              <span class="col-owner">Owner</span>
              <span class="col-size">Size</span>
              <span class="col-time">Modified</span>
              <span class="col-action"></span>
            </div>
            {#each entries as entry}
              <div class="file-row" class:is-dir={entry.type === 'dir'}>
                <span class="col-name">
                  {#if entry.type === 'dir'}
                    <button class="file-link" onclick={() => navigateTo(entry)}>
                      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--clr-folder)" stroke-width="2"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                      {entry.name}
                    </button>
                  {:else if entry.type === 'symlink'}
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--clr-symlink)" stroke-width="2"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
                    <span class="symlink-name">{entry.name}</span>
                  {:else}
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--clr-file)" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
                    {entry.name}
                  {/if}
                </span>
                <span class="col-perm"><code>{formatMode(entry.mode)}</code></span>
                <span class="col-owner">{entry.uid}:{entry.gid}</span>
                <span class="col-size">{entry.type === 'dir' ? '-' : formatSize(entry.size)}</span>
                <span class="col-time">{entry.mtime}</span>
                <span class="col-action">
                  {#if entry.type === 'file'}
                    <button class="dl-btn" onclick={() => doDownload(entry)} title="Download">
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                    </button>
                  {/if}
                </span>
              </div>
            {/each}
            {#if entries.length === 0 && !loading}
              <div class="empty-dir">Empty directory</div>
            {/if}
          </div>

        {:else if activeTab === 'portmap'}
          <div class="page-header">
            <h2>Portmapper</h2>
            <span class="page-hint">Registered RPC programs on {host}:111</span>
          </div>
          {#if portmapEntries.length > 0}
            <div class="data-table">
              <div class="data-header">
                <span>Program</span>
                <span>Service</span>
                <span>Version</span>
                <span>Proto</span>
                <span>Port</span>
              </div>
              {#each portmapEntries as entry}
                <div class="data-row">
                  <span class="mono">{entry.program}</span>
                  <span class="svc-name">{programNames[entry.program] || '-'}</span>
                  <span>v{entry.version}</span>
                  <span class="proto-badge">{entry.protocol}</span>
                  <span class="mono">{entry.port}</span>
                </div>
              {/each}
            </div>
          {:else if !loading}
            <div class="empty-state">
              <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
              <p>Query registered RPC services</p>
              <button class="btn-secondary" onclick={doDumpPortmap}>Scan Portmapper</button>
            </div>
          {/if}
        {/if}
      </div>

      <!-- Status bar -->
      <footer>
        {#if loading}<span class="spinner-sm"></span>{/if}
        <span>{statusMsg}</span>
      </footer>
    {/if}
  </div>

  <!-- Credentials modal -->
  {#if showCreds}
    <div class="overlay" onclick={() => showCreds = false} role="button" tabindex="-1" onkeydown={() => {}}>
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="dialog" onclick={(e) => e.stopPropagation()} role="dialog" tabindex="0" onkeydown={() => {}}>
        <h3>AUTH_SYS Credentials</h3>
        <p class="dialog-desc">Spoof uid/gid for NFS requests. Changes apply immediately.</p>
        <div class="field-row">
          <div class="field">
            <label for="nuid">UID</label>
            <input id="nuid" type="number" bind:value={newUid} />
          </div>
          <div class="field">
            <label for="ngid">GID</label>
            <input id="ngid" type="number" bind:value={newGid} />
          </div>
        </div>
        <div class="dialog-actions">
          <button class="btn-ghost" onclick={() => showCreds = false}>Cancel</button>
          <button class="btn-primary" onclick={doSetCredentials}>Apply</button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  :root {
    --bg: #0c0c0c;
    --bg-sidebar: #141414;
    --bg-card: #1a1a1a;
    --bg-hover: #222;
    --bg-input: #111;
    --border: #2a2a2a;
    --border-light: #333;
    --text: #e8e8e8;
    --text-secondary: #888;
    --text-muted: #555;
    --accent: #3b82f6;
    --accent-hover: #2563eb;
    --green: #22c55e;
    --red: #ef4444;
    --clr-folder: #f59e0b;
    --clr-file: #6b7280;
    --clr-symlink: #a78bfa;
    --radius: 6px;
  }

  :global(*) { box-sizing: border-box; }

  :global(body) {
    margin: 0;
    font-family: -apple-system, BlinkMacSystemFont, 'Inter', 'Segoe UI', sans-serif;
    background: var(--bg);
    color: var(--text);
    font-size: 13px;
    line-height: 1.5;
    -webkit-font-smoothing: antialiased;
  }

  .app {
    display: flex;
    height: 100vh;
    overflow: hidden;
  }

  /* ─── Sidebar ─── */
  .sidebar {
    width: 220px;
    min-width: 220px;
    background: var(--bg-sidebar);
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    padding: 0;
  }

  .logo {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 20px 18px 16px;
    font-weight: 700;
    font-size: 15px;
    letter-spacing: -0.3px;
    color: var(--text);
    --wails-draggable: drag;
  }
  .logo svg { opacity: 0.7; }

  .sidebar-section {
    padding: 0 18px 16px;
    border-bottom: 1px solid var(--border);
  }

  .conn-badge {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-weight: 600;
    color: var(--green);
  }

  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--green);
    box-shadow: 0 0 6px var(--green);
  }

  .conn-host {
    margin-top: 4px;
    font-size: 11px;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sidebar-nav {
    flex: 1;
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .sidebar-nav button {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 8px 10px;
    border: none;
    border-radius: var(--radius);
    background: transparent;
    color: var(--text-secondary);
    font-size: 13px;
    cursor: pointer;
    text-align: left;
    transition: all 0.1s;
  }
  .sidebar-nav button:hover { background: var(--bg-hover); color: var(--text); }
  .sidebar-nav button.active { background: var(--bg-hover); color: var(--text); font-weight: 500; }

  .sidebar-bottom {
    padding: 8px;
    border-top: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .sidebar-action {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 10px;
    border: none;
    border-radius: var(--radius);
    background: transparent;
    color: var(--text-muted);
    font-size: 12px;
    cursor: pointer;
    text-align: left;
  }
  .sidebar-action:hover { background: var(--bg-hover); color: var(--text-secondary); }
  .sidebar-action.disconnect:hover { color: var(--red); }

  /* ─── Content ─── */
  .content {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-width: 0;
  }

  .content-inner {
    flex: 1;
    overflow-y: auto;
    padding: 24px 32px;
  }

  /* ─── Connect screen ─── */
  .connect-wrapper {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 40px;
  }

  .connect-card {
    width: 100%;
    max-width: 460px;
  }

  .connect-header {
    margin-bottom: 28px;
  }
  .connect-header h2 {
    margin: 0 0 6px;
    font-size: 22px;
    font-weight: 600;
    letter-spacing: -0.5px;
  }
  .connect-header p {
    margin: 0;
    color: var(--text-secondary);
    font-size: 14px;
  }

  .field {
    margin-bottom: 16px;
  }
  .field label {
    display: block;
    margin-bottom: 6px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .field-row {
    display: flex;
    gap: 12px;
  }
  .field-row .field { flex: 1; }

  input, select {
    width: 100%;
    padding: 10px 12px;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    color: var(--text);
    font-size: 13px;
    font-family: inherit;
    transition: border-color 0.15s;
  }
  input:focus, select:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.15);
  }
  input::placeholder { color: var(--text-muted); }

  .toggle-advanced {
    display: flex;
    align-items: center;
    gap: 6px;
    border: none;
    background: none;
    color: var(--text-muted);
    font-size: 12px;
    cursor: pointer;
    padding: 4px 0;
    margin-bottom: 12px;
  }
  .toggle-advanced:hover { color: var(--text-secondary); }

  .advanced-fields {
    padding: 16px;
    margin-bottom: 16px;
    background: var(--bg-card);
    border-radius: var(--radius);
    border: 1px solid var(--border);
  }
  .advanced-fields .field:last-child,
  .advanced-fields .field-row:last-child .field { margin-bottom: 0; }

  .connect-btn {
    width: 100%;
    padding: 11px;
    border: none;
    border-radius: var(--radius);
    background: var(--accent);
    color: white;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition: background 0.15s;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }
  .connect-btn:hover { background: var(--accent-hover); }
  .connect-btn:disabled { opacity: 0.5; cursor: default; }

  .connect-error {
    margin-top: 12px;
    padding: 10px 12px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.2);
    border-radius: var(--radius);
    color: var(--red);
    font-size: 12px;
  }

  .connect-note {
    margin-top: 20px;
    padding: 12px;
    background: var(--bg-card);
    border-radius: var(--radius);
    font-size: 11px;
    color: var(--text-muted);
    line-height: 1.6;
  }
  .connect-note strong { color: var(--text-secondary); }
  .connect-note code {
    background: var(--bg);
    padding: 1px 5px;
    border-radius: 3px;
    font-size: 11px;
  }

  /* ─── Page header ─── */
  .page-header {
    margin-bottom: 20px;
  }
  .page-header h2 {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
    letter-spacing: -0.3px;
  }
  .page-hint {
    font-size: 12px;
    color: var(--text-muted);
  }

  /* ─── Export cards ─── */
  .card-grid {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .export-card {
    display: flex;
    align-items: center;
    gap: 14px;
    width: 100%;
    padding: 14px 16px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    color: var(--text);
    cursor: pointer;
    text-align: left;
    transition: all 0.1s;
  }
  .export-card:hover {
    border-color: var(--border-light);
    background: var(--bg-hover);
  }

  .export-icon {
    flex-shrink: 0;
    width: 36px;
    height: 36px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(245, 158, 11, 0.1);
    border-radius: var(--radius);
    color: var(--clr-folder);
  }

  .export-info { flex: 1; min-width: 0; }
  .export-path { font-weight: 500; font-size: 14px; font-family: 'SF Mono', 'Fira Code', monospace; }
  .export-hosts { font-size: 11px; color: var(--text-muted); margin-top: 2px; }
  .export-arrow { color: var(--text-muted); flex-shrink: 0; }

  /* ─── File browser ─── */
  .breadcrumb {
    display: flex;
    align-items: center;
    gap: 2px;
    margin-top: 6px;
  }
  .sep { color: var(--text-muted); font-size: 12px; }
  .crumb {
    border: none;
    background: none;
    color: var(--accent);
    font-size: 12px;
    cursor: pointer;
    padding: 2px 4px;
    border-radius: 3px;
    font-family: 'SF Mono', 'Fira Code', monospace;
  }
  .crumb:hover { background: var(--bg-hover); }
  .crumb.active { color: var(--text-secondary); cursor: default; }

  .file-list {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .file-header, .file-row {
    display: grid;
    grid-template-columns: 1fr 100px 80px 80px 140px 40px;
    align-items: center;
    padding: 0 16px;
  }

  .file-header {
    height: 36px;
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    border-bottom: 1px solid var(--border);
    background: rgba(255,255,255,0.02);
  }

  .file-row {
    height: 38px;
    border-bottom: 1px solid var(--border);
    font-size: 13px;
    transition: background 0.05s;
  }
  .file-row:last-child { border-bottom: none; }
  .file-row:hover { background: var(--bg-hover); }

  .col-name {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .col-perm code {
    font-family: 'SF Mono', 'Fira Code', monospace;
    font-size: 12px;
    color: var(--text-secondary);
  }

  .col-owner { color: var(--text-secondary); font-size: 12px; }
  .col-size { text-align: right; font-variant-numeric: tabular-nums; color: var(--text-secondary); }
  .col-time { font-size: 12px; color: var(--text-muted); }
  .col-action { display: flex; justify-content: center; }

  .file-link {
    display: flex;
    align-items: center;
    gap: 8px;
    border: none;
    background: none;
    color: var(--text);
    cursor: pointer;
    font-size: 13px;
    padding: 0;
    font-weight: 500;
  }
  .file-link:hover { color: var(--accent); }

  .symlink-name { color: var(--clr-symlink); font-style: italic; }

  .dl-btn {
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
  }
  .dl-btn:hover { background: var(--bg-hover); color: var(--accent); }

  .empty-dir {
    padding: 48px;
    text-align: center;
    color: var(--text-muted);
    font-size: 13px;
  }

  /* ─── Data table (portmapper) ─── */
  .data-table {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .data-header, .data-row {
    display: grid;
    grid-template-columns: 100px 1fr 80px 60px 80px;
    align-items: center;
    padding: 0 16px;
  }

  .data-header {
    height: 36px;
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    border-bottom: 1px solid var(--border);
    background: rgba(255,255,255,0.02);
  }

  .data-row {
    height: 36px;
    border-bottom: 1px solid var(--border);
    font-size: 13px;
  }
  .data-row:last-child { border-bottom: none; }
  .data-row:hover { background: var(--bg-hover); }

  .mono { font-family: 'SF Mono', 'Fira Code', monospace; font-size: 12px; }
  .svc-name { font-weight: 500; }
  .proto-badge {
    font-size: 11px;
    text-transform: uppercase;
    color: var(--text-secondary);
  }

  /* ─── Empty state ─── */
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    padding: 64px 20px;
    color: var(--text-muted);
    text-align: center;
  }
  .empty-state p { margin: 0; font-size: 14px; }

  .btn-secondary {
    padding: 8px 20px;
    border: 1px solid var(--border-light);
    border-radius: var(--radius);
    background: var(--bg-card);
    color: var(--text);
    font-size: 13px;
    cursor: pointer;
  }
  .btn-secondary:hover { background: var(--bg-hover); border-color: var(--text-muted); }

  /* ─── Dialog ─── */
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.6);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }

  .dialog {
    background: var(--bg-sidebar);
    border: 1px solid var(--border-light);
    border-radius: 10px;
    padding: 28px;
    width: 380px;
    box-shadow: 0 20px 60px rgba(0,0,0,0.5);
  }
  .dialog h3 {
    margin: 0 0 4px;
    font-size: 16px;
    font-weight: 600;
  }
  .dialog-desc {
    margin: 0 0 20px;
    font-size: 12px;
    color: var(--text-muted);
  }

  .dialog-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 20px;
  }

  .btn-ghost {
    padding: 8px 16px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: transparent;
    color: var(--text-secondary);
    font-size: 13px;
    cursor: pointer;
  }
  .btn-ghost:hover { background: var(--bg-hover); }

  .btn-primary {
    padding: 8px 20px;
    border: none;
    border-radius: var(--radius);
    background: var(--accent);
    color: white;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
  }
  .btn-primary:hover { background: var(--accent-hover); }

  /* ─── Footer ─── */
  footer {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 16px;
    border-top: 1px solid var(--border);
    font-size: 11px;
    color: var(--text-muted);
    background: var(--bg-sidebar);
    min-height: 28px;
  }

  /* ─── Spinners ─── */
  .spinner-sm, .spinner-inline {
    width: 12px;
    height: 12px;
    border: 2px solid var(--border-light);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }
  .spinner-inline { width: 14px; height: 14px; }

  @keyframes spin { to { transform: rotate(360deg); } }
</style>
