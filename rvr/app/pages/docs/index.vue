<template>
  <DocsShell>
    <p class="eyebrow">rvr / documentation</p>
    <h1>One durable layer for every agent.</h1>
    <p class="lede">Launch, monitor, attach to, and manage autonomous coding sessions regardless of the model or harness doing the work.</p>
    <div class="actions">
      <NuxtLink to="/docs/getting-started">Get started →</NuxtLink>
      <a href="https://github.com/LeJamon/rvr" target="_blank" rel="noreferrer">View on GitHub ↗</a>
    </div>
    <section>
      <h2>What rvr manages</h2>
      <p>rvr is a terminal-first session manager for autonomous coding agents. It manages the session lifecycle; your harness continues to own the conversation, planning, prompting, memory, and tools.</p>
      <p>An agent is a model plus a harness. rvr gives every agent a durable, observable session.</p>
    </section>
    <section class="feature-grid">
      <article><h2>Harness-agnostic</h2><p>Use opencode, pi, codex, or any configured PTY CLI. rvr provides one operational surface across them all.</p></article>
      <article><h2>Durable sessions</h2><p>Detached supervisors keep agents running after the dashboard closes and can auto-resume interrupted work.</p></article>
      <article><h2>One tab, many agents</h2><p>Track live state, output, branches, and stored logs without losing context between sessions.</p></article>
    </section>
  </DocsShell>
</template>

<style scoped>
.eyebrow { color: var(--docs-red); font: 600 .72rem 'JetBrains Mono', monospace; letter-spacing: .12em; text-transform: uppercase; }.lede { max-width: 42rem; color: var(--docs-muted); font-size: clamp(1.08rem, 2vw, 1.3rem); line-height: 1.65; }.actions { display: flex; flex-wrap: wrap; gap: .75rem; margin: 2rem 0 5rem; }.actions a { padding: .8rem 1rem; border: 1px solid var(--docs-line); font: 600 .76rem 'JetBrains Mono', monospace; }.actions a:first-child { color: #170d18; border-color: var(--docs-red); background: var(--docs-red); }.actions a:hover { border-color: var(--docs-cyan); color: var(--docs-cyan); background: transparent; }.feature-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: .75rem; }.feature-grid article { padding: 1.4rem; border: 1px solid var(--docs-line); background: rgb(28 22 40 / 80%); transition: border-color 160ms ease, transform 160ms ease; }.feature-grid article:hover { border-color: rgb(126 231 255 / 55%); transform: translateY(-3px); }.feature-grid h2 { margin: 0 0 .8rem; font-size: 1.65rem; }.feature-grid p { margin: 0; color: var(--docs-muted); font-size: .92rem; } @media (max-width: 800px) { .feature-grid { grid-template-columns: 1fr; } }
</style>
