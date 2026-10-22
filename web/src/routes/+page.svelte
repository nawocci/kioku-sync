<script>
  import { onMount } from 'svelte';
  import QRCode from 'qrcode';

  import {
    fetchHealth,
    fetchLibrary,
    fetchDevices,
    fetchEvents,
    storedKey,
    storeKey,
    forgetKey,
    formatDate,
    formatTime,
    relativeTime,
    isoTime,
    lag,
  } from '$lib/api.js';
  import { ICONS } from '$lib/icons.js';
  import { nextTheme, setTheme } from '$lib/theme.js';

  // How each synced record type is presented. A type the server reports that
  // this table does not know yet still gets a card, under a generated label.
  const TYPE_META = {
    manga: { title: 'Manga', desc: 'Entries in the synced library', accent: 'icon-manga', icon: 'manga' },
    chapter: { title: 'Chapters', desc: 'Read, bookmark & page progress', accent: 'icon-chapters', icon: 'chapters' },
    history: { title: 'History', desc: 'Reading logs and durations', accent: 'icon-history', icon: 'history' },
    category: { title: 'Categories', desc: 'Custom organization folders', accent: 'icon-categories', icon: 'categories' },
    track: { title: 'Tracking', desc: 'Entries linked to a tracker', accent: 'icon-tracks', icon: 'tracks' },
    preference: { title: 'Preferences', desc: 'Theme, reader & app settings', accent: 'icon-preferences', icon: 'preferences' },
    source_preference: { title: 'Source preferences', desc: 'Per-source settings', accent: 'icon-preferences', icon: 'preferences' },
    extension_store: { title: 'Extension stores', desc: 'Registered extension repositories', accent: 'icon-extension-stores', icon: 'stores' },
    source_name: { title: 'Sources', desc: 'Source names & languages', accent: 'icon-source-names', icon: 'sources' },
  };

  const FALLBACK_ACCENTS = ['icon-preferences', 'icon-tracks', 'icon-source-names', 'icon-extension-stores'];
  const FALLBACK_ICONS = ['preferences', 'tracks', 'sources', 'stores'];

  function typeMeta(type, index) {
    const known = TYPE_META[type];
    if (known) return known;
    const words = type.replace(/_/g, ' ');
    return {
      title: words.replace(/^./, (c) => c.toUpperCase()),
      desc: 'Records of this type',
      accent: FALLBACK_ACCENTS[index % FALLBACK_ACCENTS.length],
      icon: FALLBACK_ICONS[index % FALLBACK_ICONS.length],
    };
  }

  function buildStats(library) {
    if (!library) return [];
    const cards = (library.records ?? []).map((record, index) => {
      const meta = typeMeta(record.type, index);
      const live = Math.max(0, record.count - record.tombstone);
      return {
        ...meta,
        value: live,
        desc: record.tombstone > 0 ? `${meta.desc} · ${record.tombstone} deleted` : meta.desc,
      };
    });
    const online = library.online ?? 0;
    cards.push({
      title: 'Devices',
      desc: online > 0 ? `${online} online right now` : 'Distinct client instances synced',
      accent: 'icon-devices',
      icon: 'devices',
      value: library.devices ?? 0,
    });
    return cards;
  }

  let key = $state(storedKey());
  let loginKey = $state('');
  let showKey = $state(false);
  let error = $state('');
  let checking = $state(false);

  let library = $state(null);
  let devices = $state([]);
  let events = $state([]);
  let refreshing = $state(false);

  let health = $state(null);
  let healthChecked = $state(false);

  let pairingQr = $state('');
  let pairingError = $state('');

  let toasts = $state([]);

  const pairingUrl = $derived(typeof window === 'undefined' ? '' : window.location.origin);
  const maskedKey = $derived(key ? `${key.slice(0, 7)}••••••••` : '');
  const stats = $derived(buildStats(library));

  const statusClass = $derived(health ? 'online' : healthChecked ? 'error' : '');
  const statusLabel = $derived(health ? `protocol ${health.protocol}` : healthChecked ? 'offline' : 'checking…');
  const statusTitle = $derived(health ? `Server reachable, sync protocol ${health.protocol}` : healthChecked ? 'Server unreachable' : 'Checking the server…');

  // This is deliberately plain (non-reactive) state: it guarantees that
  // effects, focus events, and interval ticks can never overlap requests.
  let loadInFlight = null;

  function load() {
    if (!key) return Promise.resolve();
    // A hidden tab has nobody watching; the next visibility change reloads.
    if (typeof document !== 'undefined' && document.hidden) return Promise.resolve();
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
      } catch (e) {
        error = e.message;
        if (e.message === 'The key was rejected.') {
          signOut();
        }
      } finally {
        refreshing = false;
        loadInFlight = null;
      }
    })();
    return loadInFlight;
  }

  async function checkHealth() {
    try {
      health = await fetchHealth();
    } catch {
      health = null;
    } finally {
      healthChecked = true;
    }
  }

  // The pairing QR carries the key and this dashboard's own origin — the
  // address the browser is using is by definition reachable from the phone.
  async function renderQr() {
    if (!key) return;
    pairingError = '';
    try {
      pairingQr = await QRCode.toDataURL(
        JSON.stringify({ v: 1, url: window.location.origin, key }),
        { errorCorrectionLevel: 'M', margin: 1, width: 180 },
      );
    } catch {
      pairingQr = '';
      pairingError = 'Could not render the pairing code.';
    }
  }

  $effect(() => {
    if (key && library) renderQr();
  });

  let toastSeq = 0;

  function toast(text, type = 'success') {
    const id = ++toastSeq;
    toasts = [...toasts, { id, text, type }];
    setTimeout(() => {
      toasts = toasts.filter((t) => t.id !== id);
    }, 2600);
  }

  async function copy(text, label) {
    try {
      if (navigator.clipboard) {
        await navigator.clipboard.writeText(text);
      } else {
        // Plain HTTP on a LAN is not a secure context, so fall back to a
        // selection copy.
        const area = document.createElement('textarea');
        area.value = text;
        document.body.appendChild(area);
        area.select();
        document.execCommand('copy');
        area.remove();
      }
      toast(`${label} copied`);
    } catch {
      toast('Could not copy', 'error');
    }
  }

  function signIn() {
    const candidate = loginKey.trim();
    if (!candidate) return;
    error = '';
    checking = true;
    key = candidate;
    storeKey(candidate);
    load().finally(() => {
      checking = false;
    });
  }

  function signOut() {
    forgetKey();
    key = '';
    loginKey = '';
    showKey = false;
    library = null;
    devices = [];
    events = [];
    pairingQr = '';
  }

  function toggleTheme() {
    setTheme(nextTheme());
  }

  onMount(() => {
    checkHealth();
    load();
    const interval = setInterval(() => {
      checkHealth();
      load();
    }, 30000);
    // Catch up immediately when the tab comes back, since polling paused.
    const onVisible = () => {
      if (!document.hidden) {
        checkHealth();
        load();
      }
    };
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('focus', onVisible);
    return () => {
      clearInterval(interval);
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('focus', onVisible);
    };
  });
</script>

{#snippet brandLogo(cls)}
  <svg
    class={cls}
    xmlns="http://www.w3.org/2000/svg"
    width="24"
    height="24"
    viewBox="0 -960 960 960"
    fill="currentColor"
    aria-hidden="true"
  >
    <path
      d="M160-160v-80h109q-51-44-80-106t-29-134q0-112 68-197.5T400-790v84q-70 25-115 86.5T240-480q0 54 21.5 99.5T320-302v-98h80v240H160Zm440 0q-50 0-85-35t-35-85q0-48 33-82.5t81-36.5q17-36 50.5-58.5T720-480q53 0 91.5 34.5T858-360q42 0 72 29t30 70q0 42-29 71.5T860-160H600Zm116-360q-7-41-27-76t-49-62v98h-80v-240h240v80H691q43 38 70.5 89T797-520h-81ZM600-240h260q8 0 14-6t6-14q0-8-6-14t-14-6h-70v-50q0-29-20.5-49.5T720-400q-29 0-49.5 20.5T650-330v10h-50q-17 0-28.5 11.5T560-280q0 17 11.5 28.5T600-240Zm120-80Z"
    />
  </svg>
{/snippet}

<div class="app-container">
  <header class="navbar">
    <div class="nav-brand">
      {@render brandLogo('brand-logo')}
      <span class="brand-title">kioku-sync</span>
    </div>
    <div class="nav-actions">
      <div class="server-status-pill {statusClass}" title={statusTitle}>
        <span class="status-dot"></span>
        <span class="status-label">{statusLabel}</span>
      </div>
      <button class="theme-toggle-btn" onclick={toggleTheme} title="Toggle theme" aria-label="Toggle theme">
        <span class="theme-icon-sun">{@html ICONS.sun}</span>
        <span class="theme-icon-moon">{@html ICONS.moon}</span>
        <span class="theme-toggle-label">theme</span>
      </button>
      {#if key}
        <button class="btn btn-secondary btn-sm" onclick={signOut} title="Sign out" aria-label="Sign out">
          {@html ICONS.logout}
          <span class="btn-label">Sign out</span>
        </button>
      {/if}
    </div>
  </header>

  <main class="main-content">
    {#if !key}
      <section class="view-panel center-panel">
        <div class="auth-card motion-scale-in">
          <div class="auth-header">
            {@render brandLogo('auth-logo')}
            <h2>Welcome to kioku-sync</h2>
            <p class="text-secondary">Self-hosted sync dashboard for Kioku</p>
          </div>

          <form
            class="auth-form"
            onsubmit={(e) => {
              e.preventDefault();
              signIn();
            }}
          >
            <div class="form-group">
              <label for="loginApiKey">API key</label>
              <div class="input-wrapper">
                <input
                  type={showKey ? 'text' : 'password'}
                  id="loginApiKey"
                  placeholder="kioku_…"
                  autocomplete="off"
                  spellcheck="false"
                  class="font-mono"
                  bind:value={loginKey}
                />
                <button
                  type="button"
                  class="action-btn"
                  title="Show/hide key"
                  aria-label="Show or hide the key"
                  onclick={() => (showKey = !showKey)}
                >
                  {#if showKey}{@html ICONS.eyeOff}{:else}{@html ICONS.eye}{/if}
                </button>
              </div>
              <span class="input-hint">
                Minted on the server with <code>kioku-sync key</code>. The dashboard is read-only.
              </span>
            </div>

            {#if error}
              <div class="alert-banner alert-error">{error}</div>
            {/if}

            <button type="submit" class="btn btn-primary btn-block" disabled={checking} aria-busy={checking}>
              {#if checking}<span class="btn-spinner"></span>{/if}
              <span>{checking ? 'Checking…' : 'Log in to dashboard'}</span>
            </button>
          </form>
        </div>
      </section>
    {:else if !library}
      <section class="view-panel center-panel">
        <div class="spinner-large" role="status" aria-label="Loading the library"></div>
        <p class="text-muted mt-4 font-mono text-sm">Connecting to sync server…</p>
        {#if error}
          <div class="alert-banner alert-error mt-4 alert-row">
            <span>{error}</span>
            <span class="alert-actions">
              <button class="btn btn-secondary btn-sm" onclick={() => load()}>Try again</button>
              <button class="btn btn-secondary btn-sm" onclick={signOut}>Sign out</button>
            </span>
          </div>
        {/if}
      </section>
    {:else}
      <section class="view-panel dashboard-panel">
        <div class="dashboard-header-card motion-fade-up">
          <div class="header-main-info">
            <div class="header-title">
              <h2 class="library-title">{library.label}</h2>
              <div class="account-badge">
                <span class="badge-dot"></span>
                <span>Library active</span>
              </div>
            </div>
            <div class="account-key-meta">
              <span class="meta-label font-mono">KEY:</span>
              <code class="meta-code font-mono">{maskedKey}</code>
              <button
                class="icon-btn-inline"
                title="Copy API key"
                aria-label="Copy API key"
                onclick={() => copy(key, 'API key')}
              >
                {@html ICONS.copy}
              </button>
            </div>
            <div class="account-date-meta font-mono" title={formatTime(library.created_at)}>
              Created: <time datetime={isoTime(library.created_at)}>{formatDate(library.created_at)}</time>
            </div>
          </div>
          <div class="header-side-actions">
            <div class="rev-badge" title="Position in this library's event log">
              <span class="rev-value font-mono tabular-nums">{library.last_seq.toLocaleString()}</span>
              <span class="rev-label font-mono">SEQUENCE</span>
            </div>
            <button
              class="btn btn-secondary btn-sm"
              title="Refresh statistics"
              disabled={refreshing}
              onclick={() => load()}
            >
              <span class={refreshing ? 'spinning' : ''}>{@html ICONS.refresh}</span>
              <span>{refreshing ? 'Refreshing…' : 'Refresh'}</span>
            </button>
          </div>
        </div>

        {#if error}
          <div class="alert-banner alert-error alert-row">
            <span>{error}</span>
            <span class="alert-actions">
              <button class="btn btn-secondary btn-sm" onclick={() => load()}>Retry</button>
            </span>
          </div>
        {/if}

        <div class="stats-grid">
          {#each stats as card, i (card.title)}
            <div class="stat-card motion-card-in {i % 3 === 0 ? '' : i % 3 === 1 ? 'motion-delay-1' : 'motion-delay-2'}">
              <div class="stat-icon-wrapper {card.accent}">{@html ICONS[card.icon]}</div>
              <div class="stat-content">
                <span class="stat-line">
                  <span class="stat-value font-mono tabular-nums">{card.value.toLocaleString()}</span>
                  <span class="stat-title">{card.title}</span>
                </span>
                <span class="stat-desc">{card.desc}</span>
              </div>
            </div>
          {/each}
        </div>

        {#if library.records.length === 0 && library.devices === 0}
          <p class="text-secondary text-sm">
            Nothing has synced yet — pair a device with the key below and it will fill in.
          </p>
        {/if}

        <div class="dashboard-section motion-fade-up motion-delay-1">
          <div class="section-header">
            <h3>Quick setup (Kioku)</h3>
            <p class="text-secondary">Configure the Android app with these connection settings</p>
          </div>

          <div class="setup-grid">
            <div class="setup-form-card">
              <div class="form-group">
                <label for="setupServerUrl">Server URL</label>
                <div class="input-action-row">
                  <div class="input-wrapper">
                    <input type="text" id="setupServerUrl" readonly spellcheck="false" class="font-mono" value={pairingUrl} />
                  </div>
                  <button class="btn btn-secondary btn-sm copy-btn" onclick={() => copy(pairingUrl, 'Server URL')}>
                    {@html ICONS.copy}
                    <span>Copy</span>
                  </button>
                </div>
              </div>

              <div class="form-group">
                <label for="setupApiKey">API key</label>
                <div class="input-action-row">
                  <div class="input-wrapper">
                    <input
                      type={showKey ? 'text' : 'password'}
                      id="setupApiKey"
                      readonly
                      spellcheck="false"
                      class="font-mono"
                      value={key}
                    />
                    <button
                      type="button"
                      class="action-btn"
                      title="Show/hide key"
                      aria-label="Show or hide the key"
                      onclick={() => (showKey = !showKey)}
                    >
                      {#if showKey}{@html ICONS.eyeOff}{:else}{@html ICONS.eye}{/if}
                    </button>
                  </div>
                  <button class="btn btn-secondary btn-sm copy-btn" onclick={() => copy(key, 'API key')}>
                    {@html ICONS.copy}
                    <span>Copy</span>
                  </button>
                </div>
              </div>

              <div class="setup-steps-list">
                <div class="step-item">
                  <span class="step-num">1</span>
                  <span>Open <strong>Kioku</strong> on your Android device.</span>
                </div>
                <div class="step-item">
                  <span class="step-num">2</span>
                  <span>Navigate to <strong>More &gt; Settings &gt; Sync</strong>.</span>
                </div>
                <div class="step-item">
                  <span class="step-num">3</span>
                  <span>Scan the code, or paste the <strong>Server URL</strong> and <strong>API key</strong>.</span>
                </div>
                <div class="step-item">
                  <span class="step-num">4</span>
                  <span>Tap <strong>Pair with a server</strong>; the library syncs on its own.</span>
                </div>
              </div>
            </div>

            <div class="setup-qr-card">
              <h4>Quick connect QR</h4>
              <p class="text-secondary text-xs">Scan with Kioku to configure sync</p>
              <div class="qr-canvas-container">
                {#if pairingQr}
                  <img src={pairingQr} alt="Pairing QR code" width="180" height="180" />
                {:else}
                  <div class="spinner-large" role="status" aria-label="Rendering the pairing code"></div>
                {/if}
              </div>
              {#if pairingError}
                <span class="text-xs text-danger">{pairingError}</span>
              {:else}
                <span class="text-muted text-xs text-center font-mono">Server URL &bull; API key</span>
              {/if}
            </div>
          </div>
        </div>

        <div class="panels-grid">
          <div class="dashboard-section motion-fade-up motion-delay-2">
            <div class="section-header">
              <h3>Devices</h3>
              <p class="text-secondary">
                {devices.length === 1 ? '1 device is paired with this library' : `${devices.length} devices are paired with this library`}
              </p>
            </div>

            <div class="table-wrap">
              <table aria-label="Devices paired with this library">
                <thead>
                  <tr>
                    <th scope="col">Device</th>
                    <th scope="col">Last seen</th>
                    <th scope="col" class="numeric">Lag</th>
                    <th scope="col">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {#each devices as device (device.id)}
                    <tr>
                      <td>
                        <span class="cell-strong">{device.name}</span>
                        <span class="cell-detail font-mono">
                          {device.app_version} &bull; pos {device.last_seq.toLocaleString()}
                        </span>
                      </td>
                      <td class="cell-muted">
                        <time datetime={isoTime(device.last_seen)} title={formatTime(device.last_seen)}>
                          {relativeTime(device.last_seen)}
                        </time>
                      </td>
                      <td class="numeric cell-mono" title="Events this device has not seen yet">{lag(library, device)}</td>
                      <td>
                        <span class="status-cell {device.online ? 'online' : ''}">
                          <span class="status-dot"></span>
                          <span>{device.online ? 'online' : 'offline'}</span>
                        </span>
                      </td>
                    </tr>
                  {:else}
                    <tr><td class="empty-cell" colspan="4">No device has synced with this library yet.</td></tr>
                  {/each}
                </tbody>
              </table>
            </div>
          </div>

          <div class="dashboard-section motion-fade-up motion-delay-3">
            <div class="section-header">
              <h3>Recent changes</h3>
              <p class="text-secondary">Newest {events.length} events in this library's log</p>
            </div>

            <div class="table-wrap table-scroll">
              <table aria-label="Newest events in this library's log">
                <thead>
                  <tr>
                    <th scope="col" class="numeric">Seq</th>
                    <th scope="col">Type</th>
                    <th scope="col">Change</th>
                    <th scope="col">When</th>
                  </tr>
                </thead>
                <tbody>
                  {#each events as event (event.seq)}
                    <tr>
                      <td class="numeric cell-mono cell-muted">{event.seq.toLocaleString()}</td>
                      <td><span class="type-tag">{event.type.replace(/_/g, ' ')}</span></td>
                      <td class="cell-strong cell-clamp" title={event.summary}>{event.summary}</td>
                      <td class="cell-muted">
                        <time datetime={isoTime(event.at)} title={formatTime(event.at)}>{relativeTime(event.at)}</time>
                      </td>
                    </tr>
                  {:else}
                    <tr><td class="empty-cell" colspan="4">Nothing has changed on this library yet.</td></tr>
                  {/each}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </section>
    {/if}
  </main>

  <footer class="footer">
    <div class="footer-content">
      <div class="footer-left">
        {@render brandLogo('footer-logo')}
        <span>
          <strong>kioku-sync</strong> &bull; Self-hosted sync backend for
          <a href="https://github.com/nawocci/kioku" target="_blank" rel="noopener noreferrer" class="footer-brand-link">Kioku</a>
        </span>
      </div>
      <div class="footer-right">
        <a href="https://github.com/nawocci/kioku-sync" target="_blank" rel="noopener noreferrer" class="footer-link">
          {@html ICONS.github}
          <span>GitHub</span>
        </a>
      </div>
    </div>
  </footer>

  <div class="toast-container" role="status" aria-live="polite">
    {#each toasts as t (t.id)}
      <div class="toast toast-{t.type}">
        <span class="toast-icon toast-icon-{t.type}">
          {#if t.type === 'error'}{@html ICONS.error}{:else if t.type === 'info'}{@html ICONS.info}{:else}{@html ICONS.check}{/if}
        </span>
        <span class="toast-text">{t.text}</span>
      </div>
    {/each}
  </div>
</div>