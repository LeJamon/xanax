<template>
  <main class="docs-shell">
    <header class="docs-header">
      <NuxtLink
        to="/"
        class="docs-brand"
      >rvr<span>✳</span></NuxtLink>
      <nav aria-label="Documentation navigation">
        <a href="#install">Install</a>
        <a href="#commands">Commands</a>
        <a href="#harnesses">Harnesses</a>
        <a href="#configuration">Configuration</a>
      </nav>
      <a
        href="https://github.com/LeJamon/rvr"
        target="_blank"
        rel="noreferrer"
      >GitHub ↗</a>
    </header>

    <section class="docs-hero">
      <p>rvr documentation</p>
      <h1>One durable layer<br>for every agent.</h1>
      <span>Terminal-first session management for autonomous coding work.</span>
    </section>

    <div class="docs-layout">
      <aside class="docs-aside">
        <a href="#install">01 / Install</a>
        <a href="#commands">02 / Commands</a>
        <a href="#dashboard">03 / Dashboard</a>
        <a href="#harnesses">04 / Harnesses</a>
        <a href="#configuration">05 / Configuration</a>
      </aside>

      <article class="docs-content">
        <section id="install">
          <p class="eyebrow">
            01 / Install
          </p>
          <h2>Start with one command.</h2>
          <p>Install rvr with Go 1.26.5 or newer. Tagged releases are also available for macOS and Linux from GitHub Releases.</p>
          <pre><code>go install github.com/LeJamon/rvr/cmd/rvr@latest</code></pre>
          <p>To build the current checkout instead:</p>
          <pre><code>go build -o rvr ./cmd/rvr</code></pre>
        </section>

        <section id="commands">
          <p class="eyebrow">
            02 / Commands
          </p>
          <h2>Launch, observe, return.</h2>
          <p>rvr manages the session lifecycle. Your selected harness still owns planning, prompting, memory, tools, and the conversation itself.</p>
          <pre><code>rvr                                    # dashboard: all sessions
rvr ~/code/api                         # dashboard scoped to one path
rvr new --harness opencode fix the failing tests
rvr new --harness pi --repo ~/code/api "add pagination"
rvr list [--json]                      # aliases: ls, ps
rvr attach &lt;id&gt;                        # reattach to a live session
rvr resume &lt;id&gt;                        # reattach or relaunch natively
rvr kill   &lt;id&gt;                        # stop, keep the record
rvr rm     &lt;id&gt;... [--force]           # remove sessions
rvr logs   &lt;id&gt; [-f]                   # print or follow raw output
rvr config                             # print resolved configuration</code></pre>
          <p>Session IDs accept unique prefixes, like Git commit IDs.</p>
        </section>

        <section id="dashboard">
          <p class="eyebrow">
            03 / Dashboard
          </p>
          <h2>Operate many agents from one tab.</h2>
          <div class="key-grid">
            <div><kbd>Enter</kbd><span>Open the selected session</span></div>
            <div><kbd>Tab</kbd><span>Choose the next session’s harness</span></div>
            <div><kbd>Space</kbd><span>Toggle a live output peek</span></div>
            <div><kbd>r</kbd><span>Resume a finished or interrupted session</span></div>
            <div><kbd>Ctrl+X</kbd><span>Remove a session; confirm again if it is live</span></div>
            <div><kbd>Ctrl+Q</kbd><span>Detach from a live harness session</span></div>
          </div>
          <p>Closing the dashboard does not close your sessions. rvr supervises them independently and can auto-resume interrupted work on the next launch.</p>
        </section>

        <section id="harnesses">
          <p class="eyebrow">
            04 / Harnesses
          </p>
          <h2>Harness-agnostic by design.</h2>
          <p>An agent is a model plus a harness. rvr gives each session the same operational surface regardless of the harness doing the work.</p>
          <div class="compatibility-table">
            <div class="table-row table-head">
              <span>Harness</span><span>Integration</span><span>Resume</span>
            </div>
            <div class="table-row">
              <strong>opencode</strong><span>Native adapter, local SSE API</span><span>Captured session ID</span>
            </div>
            <div class="table-row">
              <strong>pi</strong><span>Native adapter, embedded hook</span><span>Captured session file</span>
            </div>
            <div class="table-row">
              <strong>codex</strong><span>Generic full-screen adapter</span><span><code>codex resume --last</code></span>
            </div>
            <div class="table-row">
              <strong>Other PTY CLIs</strong><span>Configured generic adapter</span><span>Configured <code>resume_args</code></span>
            </div>
          </div>
          <p>When a native side channel is unavailable, rvr preserves the running harness and degrades to process-level running/exited state.</p>
        </section>

        <section id="configuration">
          <p class="eyebrow">
            05 / Configuration
          </p>
          <h2>Configure only what you need.</h2>
          <p>opencode, pi, and codex work without a configuration file. For custom defaults or generic adapters, add <code>~/.config/rvr/config.toml</code>.</p>
          <pre><code>default_harness   = "opencode"
auto_resume       = true
notifications     = true
interact_exit_key = "ctrl+q"

[harness.goose]
adapter     = "generic"
command     = "goose"
args        = ["session"]
resume_args = ["session", "--resume"]</code></pre>
          <p>Use <code>rvr config</code> to inspect the full resolved configuration, paths, and defaults.</p>
        </section>
      </article>
    </div>

    <footer><span>rvr / durable session infrastructure</span><NuxtLink to="/">Back to home ←</NuxtLink></footer>
  </main>
</template>

<style>
@import url('https://fonts.googleapis.com/css2?family=Bodoni+Moda:ital,opsz,wght@0,6..96,400;0,6..96,500;1,6..96,400&family=DM+Mono:wght@400;500&family=Space+Grotesk:wght@400;500;600&display=swap');

:root { --ink: #070707; --paper: #f5f1e9; --red: #ff493f; --line: #ffffff2b; --muted: #aaa59c; }
* { box-sizing: border-box; }
html { scroll-behavior: smooth; }
body { margin: 0; background: var(--ink); color: var(--paper); font-family: 'Space Grotesk', sans-serif; }
a { color: inherit; text-decoration: none; }
.docs-shell { min-height: 100vh; overflow: hidden; background: var(--ink); }.docs-header { display: grid; grid-template-columns: 1fr auto 1fr; align-items: center; min-height: 72px; padding: 0 clamp(20px, 4vw, 72px); border-bottom: 1px solid var(--line); font: 10px 'DM Mono', monospace; letter-spacing: .08em; text-transform: uppercase; }.docs-brand { justify-self: start; font: 600 26px 'Space Grotesk', sans-serif; letter-spacing: -.1em; text-transform: lowercase; }.docs-brand span { color: var(--red); font-size: 14px; vertical-align: top; }.docs-header nav { display: flex; gap: 24px; color: var(--muted); }.docs-header > a:last-child { justify-self: end; }.docs-header a:hover, footer a:hover { color: var(--red); }
.docs-hero { padding: clamp(84px, 12vw, 180px) clamp(20px, 9vw, 150px) clamp(76px, 10vw, 150px); border-bottom: 1px solid var(--line); background: radial-gradient(ellipse at 82% 30%, #1b20ff3d, transparent 44%); }.docs-hero p, .eyebrow { margin: 0; color: var(--muted); font: 10px 'DM Mono', monospace; letter-spacing: .14em; text-transform: uppercase; }.docs-hero h1 { max-width: 940px; margin: 22px 0; font: 500 clamp(58px, 8vw, 132px)/.82 'Bodoni Moda', Didot, serif; letter-spacing: -.075em; }.docs-hero h1::first-line { color: var(--paper); }.docs-hero span { color: #c7c2ba; font: 14px 'DM Mono', monospace; }
.docs-layout { display: grid; grid-template-columns: 220px minmax(0, 760px); gap: clamp(40px, 9vw, 170px); max-width: 1300px; padding: clamp(64px, 9vw, 130px) clamp(20px, 9vw, 150px); }.docs-aside { position: sticky; top: 32px; display: grid; align-content: start; gap: 14px; height: max-content; color: var(--muted); font: 10px 'DM Mono', monospace; letter-spacing: .08em; text-transform: uppercase; }.docs-aside a:hover { color: var(--red); }.docs-content { display: grid; gap: 88px; }.docs-content section { scroll-margin-top: 36px; }.docs-content h2 { margin: 14px 0 18px; font: 500 clamp(38px, 4.2vw, 62px)/.9 'Bodoni Moda', Didot, serif; letter-spacing: -.06em; }.docs-content p { max-width: 650px; color: #c1bdb5; font-size: 16px; line-height: 1.6; }.docs-content code { color: #e6e1d8; font: .9em 'DM Mono', monospace; }.docs-content pre { max-width: 100%; overflow-x: auto; margin: 25px 0; padding: 20px; color: #e6e1d8; background: #101010; border: 1px solid var(--line); font: 12px/1.65 'DM Mono', monospace; }.key-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 1px; margin: 28px 0; background: var(--line); border: 1px solid var(--line); }.key-grid div { display: grid; grid-template-columns: auto 1fr; gap: 12px; align-items: center; min-height: 76px; padding: 14px; background: var(--ink); color: #c1bdb5; font-size: 13px; }.key-grid kbd { padding: 5px 7px; color: var(--red); border: 1px solid var(--line); font: 10px 'DM Mono', monospace; }.compatibility-table { margin: 28px 0; border: 1px solid var(--line); }.table-row { display: grid; grid-template-columns: 1fr 1.55fr 1fr; gap: 18px; padding: 15px; border-top: 1px solid var(--line); color: #c1bdb5; font-size: 13px; line-height: 1.4; }.table-row:first-child { border-top: 0; }.table-row strong { color: var(--paper); font-weight: 500; }.table-head { color: var(--red); font: 10px 'DM Mono', monospace; letter-spacing: .08em; text-transform: uppercase; }footer { display: flex; justify-content: space-between; gap: 20px; padding: 24px clamp(20px, 4vw, 72px); color: #88837a; border-top: 1px solid var(--line); font: 10px 'DM Mono', monospace; letter-spacing: .08em; text-transform: uppercase; }
@media (max-width: 800px) { .docs-header { grid-template-columns: 1fr auto; }.docs-header nav { display: none; }.docs-layout { display: block; }.docs-aside { display: none; }.docs-content { gap: 64px; }.key-grid { grid-template-columns: 1fr; }.table-row { grid-template-columns: 1fr; gap: 7px; }.table-head { display: none; } }
</style>
