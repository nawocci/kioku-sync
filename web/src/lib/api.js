const KEY_STORAGE = 'kioku-sync-key';

export function storedKey() {
  if (typeof localStorage === 'undefined') return '';
  return localStorage.getItem(KEY_STORAGE) ?? '';
}

export function storeKey(key) {
  localStorage.setItem(KEY_STORAGE, key);
}

export function forgetKey() {
  localStorage.removeItem(KEY_STORAGE);
}

async function call(path, key) {
  const response = await fetch(path, {
    headers: { Authorization: `Bearer ${key}` },
  });
  if (response.status === 401) throw new Error('The key was rejected.');
  if (!response.ok) throw new Error(`Request failed (${response.status}).`);
  return response.json();
}

export async function fetchLibrary(key) {
  return call('/api/v1/library', key);
}

export async function fetchDevices(key) {
  return call('/api/v1/devices', key);
}

export async function fetchEvents(key) {
  return call('/api/v1/events?limit=100', key);
}

// Liveness and protocol version; no key required.
export async function fetchHealth() {
  const response = await fetch('/healthz');
  if (!response.ok) throw new Error(`Request failed (${response.status}).`);
  return response.json();
}

export function formatTime(unixSeconds) {
  if (!unixSeconds) return '—';
  return new Date(unixSeconds * 1000).toLocaleString();
}

// Date without the time of day, for the header card.
export function formatDate(unixSeconds) {
  if (!unixSeconds) return '—';
  return new Date(unixSeconds * 1000).toLocaleDateString();
}

// Machine-readable timestamp for <time datetime>.
export function isoTime(unixSeconds) {
  if (!unixSeconds) return '';
  return new Date(unixSeconds * 1000).toISOString();
}

export function relativeTime(unixSeconds) {
  if (!unixSeconds) return 'never';
  const seconds = Math.max(0, Math.floor(Date.now() / 1000 - unixSeconds));
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

// How far a device trails the library's latest recorded change. Zero means
// the device has seen everything the server holds.
export function lag(library, device) {
  if (!library || !device) return 0;
  return Math.max(0, library.last_seq - device.last_seq);
}