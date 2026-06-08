<script lang="ts">
  import '../app.css';
  let { children } = $props();
</script>

<a class="skip-link" href="#main">Skip to main</a>

<div class="app">
  <aside class="sidebar" aria-label="Primary navigation">
    <div class="brand mono">regatta</div>
    <nav>
      <a href="/" data-key="g d">Dashboard</a>
      <a href="/runs" data-key="g r">Runs</a>
      <a href="/agents" data-key="g a">Agents</a>
      <a href="/reviews" data-key="g v">Reviews</a>
      <a href="/settings" data-key="g s">Settings</a>
    </nav>
    <div class="hint mono">cmd+k</div>
  </aside>

  <main id="main">
    {@render children?.()}
  </main>
</div>

<style>
  .app {
    display: grid;
    grid-template-columns: var(--sidebar-width) 1fr;
    min-height: 100dvh;
  }
  .sidebar {
    background: var(--color-card);
    border-right: 1px solid var(--color-border);
    padding: 16px 8px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .brand {
    font-size: 16px;
    font-weight: 500;
    padding: 0 8px;
    color: var(--color-accent);
  }
  nav {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  nav a {
    color: var(--color-fg);
    text-decoration: none;
    padding: 8px 12px;
    border-radius: 4px;
    font-size: 14px;
    transition: background 160ms ease-out;
  }
  nav a:hover {
    background: var(--color-muted);
  }
  nav a[data-key]::after {
    content: attr(data-key);
    float: right;
    color: var(--color-muted-fg);
    font-family: var(--font-mono);
    font-size: 11px;
  }
  .hint {
    margin-top: auto;
    color: var(--color-muted-fg);
    font-size: 11px;
    padding: 0 12px;
  }
  main {
    padding: 24px;
    overflow: auto;
  }
  .skip-link {
    position: absolute;
    left: -9999px;
  }
  .skip-link:focus {
    left: 8px;
    top: 8px;
    background: var(--color-accent);
    color: var(--color-bg);
    padding: 4px 8px;
    z-index: 100;
  }
</style>
