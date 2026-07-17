<script setup lang="ts">
const route = useRoute()
const section = computed(() => String(route.params.section))

const pages = {
  'getting-started': { title: 'Getting started', intro: 'Install rvr, open the dashboard, and launch your first durable agent session.' },
  dashboard: { title: 'Dashboard', intro: 'The session list and prompt box form one navigable column.' },
  commands: { title: 'Command reference', intro: 'The complete command-line surface for managing rvr sessions.' },
  harnesses: { title: 'Harnesses', intro: 'rvr is harness-agnostic. Adapters translate harness-specific state and resume behavior into the shared session layer.' },
  configuration: { title: 'Configuration', intro: 'Configuration is optional. Override defaults or add a generic harness in ~/.config/rvr/config.toml.' }
} as const

const page = computed(() => pages[section.value as keyof typeof pages])
if (!page.value) throw createError({ statusCode: 404, statusMessage: 'Documentation page not found' })
</script>

<template>
  <DocsShell>
    <p class="eyebrow">rvr / {{ section }}</p>
    <h1>{{ page.title }}</h1>
    <p class="lede">{{ page.intro }}</p>

    <template v-if="section === 'getting-started'">
      <h2>Install</h2><p>With Go 1.26.5 or newer:</p><pre><code>go install github.com/LeJamon/rvr/cmd/rvr@latest</code></pre><p>Tagged releases publish checksummed macOS and Linux archives for amd64 and arm64 on <a href="https://github.com/LeJamon/rvr/releases">GitHub Releases</a>. To build a checkout instead:</p><pre><code>go build -o rvr ./cmd/rvr</code></pre>
      <h2>Start the dashboard</h2><pre><code>rvr
rvr ~/code/api</code></pre><p>Passing a repository path scopes the dashboard and new sessions to that location.</p>
      <h2>Launch an agent</h2><pre><code>rvr new --harness opencode fix the failing tests
rvr new --harness pi --repo ~/code/api "add pagination"
printf '%s\n' "long prompt" | rvr new -</code></pre><p>The dashboard prompt box can also launch several background sessions. Press <code>Enter</code> to launch, or <code>Ctrl+O</code> to launch and attach immediately.</p>
    </template>

    <template v-else-if="section === 'dashboard'">
      <p>A path-scoped dashboard shows only the sessions beneath that repository and launches new work there.</p><h2>Prompt box</h2><ul><li><code>Enter</code> launches a session in the background.</li><li><code>Ctrl+O</code> launches and attaches to the session.</li><li><code>Tab</code> opens the harness picker for the next session.</li><li><code>Esc</code> clears a draft prompt.</li></ul><h2>Session list</h2><ul><li><code>Enter</code> or <code>→</code> opens a live session or stored logs.</li><li><code>Space</code> toggles a live output peek.</li><li><code>l</code> shows stored logs; <code>e</code> renames the rvr-only label.</li><li><code>r</code> resumes a finished or interrupted session.</li><li><code>Ctrl+X</code> removes a session; live sessions require a second confirmation.</li><li><code>/</code> filters sessions.</li></ul><h2>Attach and detach</h2><p>Opening a session enters the harness’s native TUI. Every harness key is forwarded while attached. Press <code>Ctrl+Q</code> by default to detach; the session keeps running in the background.</p><p>Sessions survive dashboard exits and reboots. On the next launch, rvr can auto-resume interrupted sessions with the harness’s native resume behavior.</p>
    </template>

    <template v-else-if="section === 'commands'">
      <pre><code>rvr                                    # dashboard: all sessions
rvr ~/code/api                         # dashboard scoped to one path
rvr new [flags] [prompt ...]           # create a session
rvr list [--json]                      # aliases: ls, ps
rvr attach &lt;id&gt;                        # attach to a live session
rvr resume &lt;id&gt;                        # reattach or relaunch natively
rvr kill &lt;id&gt;                          # terminate, retain the record
rvr rm &lt;id&gt;... [--force]               # remove sessions
rvr prune                              # remove terminal sessions
rvr logs &lt;id&gt; [-f]                     # print or follow raw output
rvr config                             # print resolved configuration</code></pre><p>Session IDs accept unique prefixes, like Git commit IDs.</p><h2>New session flags</h2><p>Use <code>--harness</code> to select a configured harness and <code>--repo</code> to choose the working repository. A prompt may be passed as arguments or through standard input.</p><pre><code>rvr new --harness opencode fix the failing tests
rvr new --harness pi --repo ~/code/api "add pagination"</code></pre>
    </template>

    <template v-else-if="section === 'harnesses'">
      <table><thead><tr><th>Harness</th><th>Integration</th><th>State detection</th><th>Resume behavior</th></tr></thead><tbody><tr><td>opencode</td><td>Native adapter, local SSE API</td><td>Busy, idle, permission/input, error</td><td>Captured session ID</td></tr><tr><td>pi</td><td>Native adapter, embedded hook</td><td>Agent busy/idle lifecycle</td><td>Captured session file</td></tr><tr><td>codex</td><td>Generic full-screen adapter</td><td>Output pattern plus idle timeout</td><td><code>codex resume --last</code></td></tr><tr><td>Other PTY CLIs</td><td>Configured generic adapter</td><td>Optional waiting pattern and idle timeout</td><td>Configured <code>resume_args</code></td></tr></tbody></table><p>If a native side channel is unavailable or changes upstream, the harness keeps running and rvr degrades to process-level running/exited state.</p><h2>Generic adapters</h2><p>Any compatible PTY CLI can use the generic adapter. Provide the command, start arguments, and resume arguments; set <code>prompt_arg</code> or <code>prompt_positional</code> if the CLI accepts the prompt directly. For diff-rendered TUIs, set <code>full_screen = true</code>.</p>
    </template>

    <template v-else-if="section === 'configuration'">
      <h2>Default configuration</h2><pre><code>default_harness   = "opencode"
auto_resume       = true
notifications     = true
interact_exit_key = "ctrl+q"

[harness.opencode]
adapter = "opencode"
command = "opencode"

[harness.pi]
adapter = "pi"
command = "pi"</code></pre><h2>Codex</h2><p>Codex uses the generic full-screen adapter by default. Override it only when needed:</p><pre><code>[harness.codex]
adapter           = "generic"
command           = "codex"
full_screen       = true
prompt_positional = true
resume_args       = ["resume", "--last"]
idle_timeout      = 120</code></pre><h2>Generic harness example</h2><pre><code>[harness.goose]
adapter     = "generic"
command     = "goose"
args        = ["session"]
resume_args = ["session", "--resume"]</code></pre><p>Run <code>rvr config</code> to inspect the full resolved configuration, paths, and defaults.</p>
    </template>
  </DocsShell>
</template>

<style scoped>
h1, h2 { font-family: Fraunces, 'Iowan Old Style', Georgia, serif; font-weight: 500; letter-spacing: -.045em; } h1 { max-width: 42rem; margin: 0; font-size: clamp(3.2rem, 6vw, 5.5rem); line-height: .9; } h2 { margin: 3.5rem 0 1.25rem; padding-top: 1.25rem; border-top: 1px solid var(--docs-line); font-size: clamp(2rem, 4vw, 3rem); line-height: 1; }.eyebrow { color: var(--docs-red); font: 600 .72rem 'JetBrains Mono', monospace; letter-spacing: .12em; text-transform: uppercase; }.lede { max-width: 42rem; color: var(--docs-muted); font-size: 1.12rem; line-height: 1.65; }p, li { line-height: 1.7; }a { color: var(--docs-cyan); text-decoration: underline; text-underline-offset: .2em; }code, pre { font-family: 'JetBrains Mono', ui-monospace, monospace; }p code, li code, td code { padding: .1rem .3rem; border: 1px solid var(--docs-line); border-radius: 4px; color: var(--docs-cyan); background: rgb(126 231 255 / 8%); font-size: .85em; }pre { overflow-x: auto; padding: 1.15rem; border: 1px solid var(--docs-line); border-radius: 12px; background: var(--docs-panel); box-shadow: 0 24px 60px -30px rgb(255 75 114 / 35%); line-height: 1.65; }table { width: 100%; margin: 2rem 0; border-collapse: collapse; font-size: .9rem; }th, td { padding: .8rem; border: 1px solid var(--docs-line); text-align: left; vertical-align: top; }th { color: var(--docs-cyan); font: 600 .72rem 'JetBrains Mono', monospace; text-transform: uppercase; }ul { padding-left: 1.2rem; }
</style>
