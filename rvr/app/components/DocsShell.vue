<script setup lang="ts">
const navigation = [
  { label: 'Overview', to: '/docs' },
  { label: 'Getting started', to: '/docs/getting-started' },
  { label: 'Dashboard', to: '/docs/dashboard' },
  { label: 'Commands', to: '/docs/commands' },
  { label: 'Harnesses', to: '/docs/harnesses' },
  { label: 'Configuration', to: '/docs/configuration' }
]
</script>

<template>
  <div class="docs-shell">
    <header class="docs-header">
      <NuxtLink to="/" class="docs-brand" aria-label="rvr home">rvr<span>✳</span></NuxtLink>
      <nav aria-label="Main navigation">
        <NuxtLink to="/">Home</NuxtLink>
        <NuxtLink to="/docs">Docs</NuxtLink>
        <a href="https://github.com/LeJamon/rvr" target="_blank" rel="noreferrer">GitHub ↗</a>
      </nav>
    </header>

    <div class="docs-layout">
      <aside class="docs-sidebar">
        <p>rvr / docs</p>
        <nav aria-label="Documentation navigation">
          <NuxtLink v-for="item in navigation" :key="item.to" :to="item.to">{{ item.label }}</NuxtLink>
        </nav>
      </aside>
      <main class="docs-content">
        <slot />
      </main>
    </div>
  </div>
</template>

<style scoped>
:global(:root) { --docs-ink: #15101f; --docs-panel: #1c1628; --docs-paper: #fbf8ff; --docs-muted: #b7aec2; --docs-line: rgb(255 255 255 / 10%); --docs-red: #ff4b72; --docs-cyan: #7ee7ff; }
:global(*) { box-sizing: border-box; }
:global(body) { margin: 0; color: var(--docs-paper); background-color: var(--docs-ink); background-image: linear-gradient(rgb(255 255 255 / 2.4%) 1px, transparent 1px), linear-gradient(90deg, rgb(255 255 255 / 2.4%) 1px, transparent 1px); background-position: center top; background-size: 32px 32px; font-family: Geist, Inter, ui-sans-serif, system-ui, sans-serif; }
:global(a) { color: inherit; text-decoration: none; }
.docs-header { display: flex; min-height: 72px; align-items: center; justify-content: space-between; padding: 0 clamp(20px, 4vw, 72px); border-bottom: 1px solid var(--docs-line); background: rgb(21 16 31 / 88%); backdrop-filter: blur(12px); }
.docs-brand { font-size: 1.45rem; font-weight: 700; letter-spacing: -.09em; }.docs-brand span { color: var(--docs-red); font-size: .62em; vertical-align: top; }
.docs-header nav { display: flex; gap: 1.6rem; color: var(--docs-muted); font: 600 .75rem 'JetBrains Mono', ui-monospace, monospace; letter-spacing: .03em; text-transform: uppercase; }.docs-header nav a:hover, .docs-header nav a.router-link-active { color: var(--docs-cyan); }
.docs-layout { display: grid; grid-template-columns: minmax(190px, 240px) minmax(0, 1fr); min-height: calc(100vh - 72px); }
.docs-sidebar { position: sticky; top: 0; height: calc(100vh - 72px); padding: 2.5rem clamp(20px, 3vw, 48px); border-right: 1px solid var(--docs-line); background: rgb(21 16 31 / 72%); }.docs-sidebar > p { margin: 0 0 1.3rem; color: var(--docs-red); font: 600 .7rem 'JetBrains Mono', ui-monospace, monospace; letter-spacing: .1em; text-transform: uppercase; }
.docs-sidebar nav { display: grid; gap: .25rem; }.docs-sidebar a { padding: .55rem .65rem; color: var(--docs-muted); border-left: 1px solid transparent; font-size: .86rem; }.docs-sidebar a:hover { color: var(--docs-paper); background: rgb(255 255 255 / 4%); }.docs-sidebar a.router-link-active { color: var(--docs-cyan); border-left-color: var(--docs-cyan); background: rgb(126 231 255 / 7%); }
.docs-content { width: min(100%, 960px); padding: clamp(3rem, 7vw, 7rem) clamp(20px, 8vw, 9rem); }
@media (max-width: 760px) { .docs-header { min-height: 60px; }.docs-header nav { gap: 1rem; font-size: .66rem; }.docs-layout { display: block; }.docs-sidebar { position: static; height: auto; padding: 1.25rem 20px; border-right: 0; border-bottom: 1px solid var(--docs-line); }.docs-sidebar nav { display: flex; overflow-x: auto; }.docs-sidebar a { white-space: nowrap; }.docs-content { padding-top: 3rem; } }
</style>
