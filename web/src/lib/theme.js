/**
 * Theme handling for the dashboard: "system" follows the OS, while an
 * explicit choice pins the theme and is remembered. The initial attribute
 * is set by an inline script in app.html so the first paint already has the
 * right colors.
 */

const THEME_KEY = 'kioku-sync-theme';

export function storedTheme() {
  if (typeof localStorage === 'undefined') return 'system';
  return localStorage.getItem(THEME_KEY) ?? 'system';
}

/** Applies a theme name to <html>; "system" clears the override. */
export function applyTheme(theme) {
  if (typeof document === 'undefined') return;
  if (theme === 'light' || theme === 'dark') {
    document.documentElement.setAttribute('data-theme', theme);
  } else {
    document.documentElement.removeAttribute('data-theme');
  }
}

export function setTheme(theme) {
  if (typeof localStorage !== 'undefined') {
    localStorage.setItem(THEME_KEY, theme);
  }
  applyTheme(theme);
}

/** Whether the theme currently in effect is dark. */
export function isDark() {
  if (typeof document === 'undefined') return false;
  const explicit = document.documentElement.getAttribute('data-theme');
  if (explicit === 'dark') return true;
  if (explicit === 'light') return false;
  return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

/** The theme the toggle switches to: the opposite of the one in effect. */
export function nextTheme() {
  return isDark() ? 'light' : 'dark';
}