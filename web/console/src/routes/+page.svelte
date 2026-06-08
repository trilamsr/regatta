<script lang="ts">
  const kpis = [
    { label: 'RUNNING', value: 12, tone: 'running' },
    { label: 'QUEUED', value: 31, tone: 'pending' },
    { label: 'BLOCKED', value: 2, tone: 'blocked' },
    { label: 'MERGED', value: 84, tone: 'merged' },
    { label: 'FAILED', value: 13, tone: 'blocked' }
  ];
</script>

<header>
  <h1 class="mono">Dashboard</h1>
  <span class="muted mono">scaffold preview — backend not wired</span>
</header>

<section class="kpis" aria-label="Run status totals">
  {#each kpis as kpi}
    <article class="kpi" data-tone={kpi.tone}>
      <div class="label mono">{kpi.label}</div>
      <div class="value mono">{kpi.value}</div>
    </article>
  {/each}
</section>

<section class="placeholder">
  <p>Runs table, alerts strip, and live event stream land in subsequent slices.</p>
</section>

<style>
  header {
    display: flex;
    align-items: baseline;
    gap: 12px;
    margin-bottom: 24px;
  }
  h1 {
    font-size: 18px;
    margin: 0;
  }
  .muted {
    color: var(--color-muted-fg);
    font-size: 12px;
  }
  .kpis {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: var(--grid-gap);
  }
  .kpi {
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 4px;
    padding: var(--card-padding);
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .kpi .label {
    font-size: 11px;
    color: var(--color-muted-fg);
    letter-spacing: 0.05em;
  }
  .kpi .value {
    font-size: 28px;
    font-weight: 500;
  }
  .kpi[data-tone='running'] .value {
    color: var(--status-running);
  }
  .kpi[data-tone='pending'] .value {
    color: var(--status-pending);
  }
  .kpi[data-tone='blocked'] .value {
    color: var(--status-blocked);
  }
  .kpi[data-tone='merged'] .value {
    color: var(--status-merged);
  }
  .placeholder {
    margin-top: 24px;
    color: var(--color-muted-fg);
    font-size: 13px;
  }
</style>
