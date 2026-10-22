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
    formatTime,
    relativeTime,
    lag,
  } from '$lib/api.js';
  import { nextTheme, setTheme } from '$lib/theme.js';

  // Static icon markup from the Kioku Sakura & Obsidian design system.
  const ICONS = {
    sun: '<svg class="theme-icon-rotate" xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"></circle><line x1="12" y1="1" x2="12" y2="3"></line><line x1="12" y1="21" x2="12" y2="23"></line><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"></line><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"></line><line x1="1" y1="12" x2="3" y2="12"></line><line x1="21" y1="12" x2="23" y2="12"></line><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"></line><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"></line></svg>',
    moon: '<svg class="theme-icon-rotate" xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"></path></svg>',
    logout:
      '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"></path><polyline points="16 17 21 12 16 7"></polyline><line x1="21" y1="12" x2="9" y2="12"></line></svg>',
    refresh:
      '<svg xmlns="http://www.w3.org/2000/svg" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"></polyline><polyline points="1 20 1 14 7 14"></polyline><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path></svg>',
    copy: '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>',
    eye: '<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path><circle cx="12" cy="12" r="3"></circle></svg>',
    eyeOff:
      '<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path><line x1="1" y1="1" x2="23" y2="23"></line></svg>',
    manga:
      '<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"></path><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"></path></svg>',
    chapters:
      '<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="8" y1="6" x2="21" y2="6"></line><line x1="8" y1="12" x2="21" y2="12"></line><line x1="8" y1="18" x2="21" y2="18"></line><line x1="3" y1="6" x2="3.01" y2="6"></line><line x1="3" y1="12" x2="3.01" y2="12"></line><line x1="3" y1="18" x2="3.01" y2="18"></line></svg>',
    categories:
      '<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>',
    history:
      '<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><polyline points="12 6 12 12 16 14"></polyline></svg>',
    preferences:
      '<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>',
    devices:
      '<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="2" width="14" height="20" rx="2" ry="2"></rect><line x1="12" y1="18" x2="12.01" y2="18"></line></svg>',
    stores:
      '<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"></path><polyline points="3.27 6.96 12 12.01 20.73 6.96"></polyline><line x1="12" y1="22.08" x2="12" y2="12"></line></svg>',
    tracks:
      '<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"></polygon></svg>',
    sources:
      '<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="2" y1="12" x2="22" y2="12"></line><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"></path></svg>',
    github:
      '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 19c-5 1.5-5-2.5-7-3m14 6v-3.87a3.37 3.37 0 0 0-.94-2.61c3.14-.35 6.44-1.54 6.44-7A5.44 5.44 0 0 0 20 4.77 5.07 5.07 0 0 0 19.91 1S18.73.65 16 2.48a13.38 13.38 0 0 0-7 0C6.27.65 5.09 1 5.09 1A5.07 5.07 0 0 0 5 4.77a5.44 5.44 0 0 0-1.5 3.78c0 5.42 3.3 6.61 6.44 7A3.37 3.37 0 0 0 9 18.13V22"></path></svg>',
    check:
      '<svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg>',
    error:
      '<svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>',
    info: '<svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="16" x2="12" y2="12"></line><line x1="12" y1="8" x2="12.01" y2="8"></line></svg>',
  };

  // How each synced record type is presented. Only types the server actually
  // reports get a card, so the grid never shows an invented zero.
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

  function buildStats(library) {
    if (!library) return [];
    const cards = [];
    for (const [type, meta] of Object.entries(TYPE_META)) {
      const record = (library.records ?? []).find((r) => r.type === type);
      if (!record) continue;
      const live = Math.max(0, record.count - record.tombstone);
      const desc = record.tombstone > 0 ? `${meta.desc} · ${record.tombstone} deleted` : meta.desc;
      cards.push({ ...meta, value: live, desc });
    }
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
    const onFocus = () => {
      checkHealth();
      load();
    };
    window.addEventListener('focus', onFocus);
    return () => {
      clearInterval(interval);
      window.removeEventListener('focus', onFocus);
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
      <div class="server-status-pill {statusClass}" title="Server status">
        <span class="status-dot"></span>
        <span class="status-label">{statusLabel}</span>
      </div>
      <button class="theme-toggle-btn" onclick={toggleTheme} title="Toggle theme" aria-label="Toggle theme">
        <span class="theme-icon-sun">{@html ICONS.sun}</span>
        <span class="theme-icon-moon">{@html ICONS.moon}</span>
        <span class="theme-toggle-label">theme</span>
      </button>
      {#if key && library}
        <button class="btn btn-secondary btn-sm" onclick={signOut}>
          {@html ICONS.logout}
          <span>Sign out</span>
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

            <button type="submit" class="btn btn-primary btn-block" disabled={checking}>
              {#if checking}<span class="btn-spinner"></span>{/if}
              <span>{checking ? 'Checking…' : 'Log in to dashboard'}</span>
            </button>
          </form>
        </div>
      </section>
    {:else if !library}
      <section class="view-panel center-panel">
        <div class="spinner-large"></div>
        <p class="text-muted mt-4 font-mono text-sm">Connecting to sync server…</p>
        {#if error}
          <div class="alert-banner alert-error mt-4" style="display:flex; gap:0.75rem; align-items:center;">
            <span>{error}</span>
            <button class="btn btn-secondary btn-sm" onclick={() => load()}>Try again</button>
          </div>
        {/if}
      </section>
    {:else}
      <section class="view-panel dashboard-panel">
        <div class="dashboard-header-card motion-fade-up">
          <div class="header-main-info">
            <div class="account-badge">
              <span class="badge-dot"></span>
              <span>Library active</span>
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
            <div class="account-date-meta font-mono">Created: {formatTime(library.created_at)}</div>
          </div>
          <div class="header-side-actions">
            <div class="rev-badge">
              <span class="rev-value font-mono tabular-nums">{library.last_seq}</span>
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
          <div class="alert-banner alert-error" style="display:flex; gap:0.75rem; align-items:center;">
            <span>{error}</span>
            <button class="btn btn-secondary btn-sm" onclick={() => load()}>Retry</button>
          </div>
        {/if}

        <div class="stats-grid">
          {#each stats as card, i (card.title)}
            <div class="stat-card motion-card-in {i % 3 === 0 ? '' : i % 3 === 1 ? 'motion-delay-1' : 'motion-delay-2'}">
              <div class="stat-icon-wrapper {card.accent}">{@html ICONS[card.icon]}</div>
              <div class="stat-content">
                <span class="stat-value font-mono tabular-nums">{card.value}</span>
                <span class="stat-title">{card.title}</span>
                <span class="stat-desc">{card.desc}</span>
              </div>
            </div>
          {/each}
        </div>

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
                  <div class="spinner-large"></div>
                {/if}
              </div>
              {#if pairingError}
                <span class="text-xs" style="color: var(--destructive);">{pairingError}</span>
              {:else}
                <span class="text-muted text-xs text-center font-mono">Server URL &bull; API key</span>
              {/if}
            </div>
          </div>
        </div>

        <div class="dashboard-section motion-fade-up motion-delay-2">
          <div class="section-header">
            <h3>Devices</h3>
            <p class="text-secondary">
              {devices.length === 1 ? '1 device is paired with this library' : `${devices.length} devices are paired with this library`}
            </p>
          </div>

          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Device</th>
                  <th>App</th>
                  <th>Last seen</th>
                  <th class="numeric">Position</th>
                  <th class="numeric">Lag</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {#each devices as device (device.id)}
                  <tr>
                    <td>
                      <span class="cell-strong">{device.name}</span>
                      <span class="cell-detail font-mono">{device.id.length > 14 ? device.id.slice(0, 12) + '…' : device.id}</span>
                    </td>
                    <td class="cell-mono cell-muted">{device.app_version}</td>
                    <td class="cell-muted" title={formatTime(device.last_seen)}>{relativeTime(device.last_seen)}</td>
                    <td class="numeric cell-mono">{device.last_seq}</td>
                    <td class="numeric cell-mono">{lag(library, device)}</td>
                    <td>
                      <span class="status-cell {device.online ? 'online' : ''}">
                        <span class="status-dot"></span>
                        <span>{device.online ? 'online' : 'offline'}</span>
                      </span>
                    </td>
                  </tr>
                {:else}
                  <tr><td class="empty-cell" colspan="6">No device has synced with this library yet.</td></tr>
                {/each}
              </tbody>
            </table>
          </div>
        </div>

        <div class="dashboard-section motion-fade-up motion-delay-3">
          <div class="section-header">
            <h3>Recent changes</h3>
            <p class="text-secondary">Newest events in this library's log</p>
          </div>

          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th class="numeric">Seq</th>
                  <th>Type</th>
                  <th>Change</th>
                  <th>When</th>
                </tr>
              </thead>
              <tbody>
                {#each events as event (event.seq)}
                  <tr>
                    <td class="numeric cell-mono cell-muted">{event.seq}</td>
                    <td><span class="type-tag">{event.type.replace('_', ' ')}</span></td>
                    <td class="cell-strong">{event.summary}</td>
                    <td class="cell-muted" title={formatTime(event.at)}>{relativeTime(event.at)}</td>
                  </tr>
                {:else}
                  <tr><td class="empty-cell" colspan="4">Nothing has changed on this library yet.</td></tr>
                {/each}
              </tbody>
            </table>
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

  <div class="toast-container">
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