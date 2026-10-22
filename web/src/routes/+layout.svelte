<script>
  import '../app.css';

  import { onMount } from 'svelte';

  import { applyTheme, storedTheme } from '$lib/theme.js';

  let { children } = $props();

  // The inline script in app.html already set the initial attribute; this
  // keeps a "system" choice following the OS while the app is open.
  onMount(() => {
    applyTheme(storedTheme());
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    const onChange = () => {
      if (storedTheme() === 'system') applyTheme('system');
    };
    media.addEventListener('change', onChange);
    return () => media.removeEventListener('change', onChange);
  });
</script>

{@render children()}