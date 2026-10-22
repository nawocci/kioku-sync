<script>
  import { onMount } from 'svelte';
  import QRCode from 'qrcode';

  import {
    fetchLibrary,
    fetchDevices,
    fetchEvents,
    storedKey,
    storeKey,
    forgetKey,
    formatTime,
    relativeTime,
    lag,
  } from '$lib/api.js';

  let key = $state(storedKey());
  let loginKey = $state('');
  let error = $state('');
  let authFailed = $state(false);
  let checking = $state(false);
  let library = $state(null);
  let devices = $state([]);
  let events = $state([]);
  let refreshing = $state(false);
  let lastRefresh = $state(null);
  let pairing = $state(false);
  let pairingQr = $state('');
  let pairingUrl = $state('');
  let pairingError = $state('');

  // The pairing QR carries the key and this dashboard's own origin — the
  // address the browser is using is by definition reachable from the phone.
  async function showPairingQr() {
    pairingError = '';
    try {
      const payload = JSON.stringify({
        v: 1,
        url: window.location.origin,
        key,
      });
      pairingUrl = window.location.origin;
      pairingQr = await QRCode.toDataURL(payload, {
        errorCorrectionLevel: 'M',
        margin: 2,
        width: 220,
      });
      pairing = true;
    } catch (e) {
      pairingError = 'Could not render the pairing code.';
    }
  }

  // This is deliberately plain (non-reactive) state: it guarantees that
  // effects, focus events, and interval ticks can never overlap requests.
  let loadInFlight = null;

  function load() {
    if (!key) return Promise.resolve();
    if (loadInFlight) return loadInFlight;

    refreshing = true;
    loadInFlight = (async () => {
      try {
        [library, devices, events] = await Promise.all([
          fetchLibrary(key),
          fetchDevices(key),
          fetchEvents(key),
        ]);
        error = '';
        authFailed = false;
        lastRefresh = Date.now();
      } catch (e) {
        error = e.message;
        if (e.message === 'The key was rejected.') {
          authFailed = true;
          signOut();
        }
      } finally {
        refreshing = false;
        loadInFlight = null;
      }
    })();
    return loadInFlight;
  }

  async function signIn() {
    if (!loginKey.trim()) return;
    checking = true;
    error = '';
    try {
      await fetchLibrary(loginKey.trim());
      key = loginKey.trim();
      storeKey(key);
      await load();
    } catch (e) {
      error = e.message;
    } finally {
      checking = false;
    }
  }

  function signOut() {
    key = '';
    forgetKey();
    library = null;
    devices = [];
    events = [];
    lastRefresh = null;
  }

  // Lifecycle-driven rather than reactive: one initial load, a conservative
  // refresh while visible, and an immediate refresh when this tab regains
  // focus. Cleanup guarantees hot reloads/remounts cannot leave old timers.
  onMount(() => {
    load();
    const interval = setInterval(() => {
      if (document.visibilityState === 'visible') load();
    }, 30_000);
    const onFocus = () => load();
    window.addEventListener('focus', onFocus);
    return () => {
      clearInterval(interval);
      window.removeEventListener('focus', onFocus);
    };
  });

  const liveRecords = $derived(
    library?.records?.reduce((sum, r) => sum + r.count - r.tombstone, 0) ?? 0,
  );

  const deletedRecords = $derived(
    library?.records?.reduce((sum, r) => sum + r.tombstone, 0) ?? 0,
  );
</script>

{#if !key}
  <div class="login-box">
    <div class="card">
      <h1>Kioku Sync</h1>
      <p style="color: var(--text-dim); margin: 0;">
        Sign in with your library's API key, minted on the server with
        kioku-sync key. This dashboard is read-only.
      </p>
      <form class="stack" onsubmit={(e) => { e.preventDefault(); signIn(); }}>
        <input
          type="password"
          placeholder="kioku_…"
          autocomplete="off"
          bind:value={loginKey}
        />
        {#if error}<div class="error">{error}</div>{/if}
        <button type="submit" disabled={checking}>
          {checking ? 'Checking…' : 'Sign in'}
        </button>
      </form>
    </div>
  </div>
{:else if !library}
  <div class="login-box">
    <div class="card">
      {#if refreshing}
        <div class="empty">Loading…</div>
      {:else if error}
        <div class="error">{error}</div>
        <button onclick={() => load()}>Try again</button>
      {:else}
        <div class="empty">Loading…</div>
      {/if}
    </div>
  </div>
{:else}
  <header>
    <div class="brand">
      <h1>{library.label}</h1>
      <small>kioku sync</small>
    </div>
    <div class="header-right">
      {#if refreshing}<small class="text-dim">refreshing…</small>
      {:else if lastRefresh}<small class="text-dim">updated {relativeTime(Math.floor(lastRefresh / 1000))}</small>{/if}
      <button onclick={() => (pairing ? (pairing = false) : showPairingQr())}>
        {pairing ? 'Hide pairing code' : 'Pair a device'}
      </button>
      <button onclick={signOut}>Sign out</button>
    </div>
  </header>

  {#if pairing}
    <section class="panel pair-panel">
      <div class="pair-body">
        <div class="pair-qr">
          {#if pairingQr}<img src={pairingQr} alt="Pairing QR code" width="220" height="220" />
          {:else}<div class="empty">Rendering…</div>{/if}
        </div>
        <div class="pair-text">
          <h2 style="margin-bottom:8px;">Pair a device</h2>
          <p style="margin:0; color: var(--text-dim);">
            Open Kioku on your phone, choose <strong>Settings → Sync → Pair with a server</strong>,
            and scan this code. It carries this server's address
            ({pairingUrl}) and the library key — treat it like a password.
          </p>
          {#if pairingError}<div class="error">{pairingError}</div>{/if}
          <p style="margin:8px 0 0; color: var(--text-dim); font-size:13px;">
            Sign out or hide the code when done.
          </p>
        </div>
      </div>
    </section>
  {/if}

  {#if error}<div class="error banner">{error} <button onclick={() => load()}>Retry</button></div>{/if}

  <div class="cards">
    <div class="card">
      <div class="label">Devices online</div>
      <div class="value">{library.online}<span style="font-size:15px; color: var(--text-dim);"> / {library.devices}</span></div>
      <div class="sub">{library.online === library.devices ? 'all caught up in real time' : 'others sync when they connect'}</div>
    </div>
    <div class="card">
      <div class="label">Records</div>
      <div class="value">{liveRecords}</div>
      {#if deletedRecords}
        <div class="sub">{deletedRecords} deleted records still tracked</div>
      {:else}
        <div class="sub">{library.last_seq} changes recorded so far</div>
      {/if}
    </div>
    <div class="card">
      <div class="label">Created</div>
      <div class="value" style="font-size:18px; margin-top:8px;">{formatTime(library.created_at)}</div>
    </div>
  </div>

  <section class="panel">
    <h2>Records by type</h2>
    {#if library.records?.length === 0}
      <div class="empty">Nothing has synced yet. Pair a device to get started.</div>
    {:else}
      <table>
        <thead>
          <tr><th>Type</th><th>Live</th><th>Deleted</th></tr>
        </thead>
        <tbody>
          {#each library.records ?? [] as r}
            <tr>
              <td>{r.type}</td>
              <td>{r.count - r.tombstone}</td>
              <td class="text-dim">{r.tombstone}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>

  <section class="panel">
    <h2>Devices</h2>
    {#if devices.length === 0}
      <div class="empty">No devices have connected yet.</div>
    {:else}
      <table>
        <thead>
          <tr><th>Device</th><th>App</th><th>Sync progress</th><th>Last seen</th><th></th></tr>
        </thead>
        <tbody>
          {#each devices as d}
            {@const behind = lag(library, d)}
            <tr>
              <td>{d.name || d.id}</td>
              <td>{d.app_version || '—'}</td>
              <td>
                {#if behind === 0}
                  <span title="synced through change #{d.last_seq}">up to date</span>
                {:else}
                  <span title="synced through change #{d.last_seq} of {library.last_seq}">{behind} {behind === 1 ? 'change' : 'changes'} behind</span>
                {/if}
              </td>
              <td title={formatTime(d.last_seen)}>{relativeTime(d.last_seen)}</td>
              <td>
                {#if d.online}<span class="pill online">online</span>
                {:else}<span class="pill offline">offline</span>{/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>

  <section class="panel">
    <h2>Recent changes</h2>
    {#if events.length === 0}
      <div class="empty">Nothing has synced yet.</div>
    {:else}
      <ul class="feed">
        {#each events as e}
          <li>
            <span class="type">{e.type}</span>
            <span>{e.summary}</span>
            <span class="when" title={formatTime(e.at)}>{relativeTime(e.at)}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{/if}
