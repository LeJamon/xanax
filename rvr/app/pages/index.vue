<script setup lang="ts">
import { ref } from 'vue'

const installCommand = 'go install github.com/LeJamon/rvr/cmd/rvr@latest'
const hasCopied = ref(false)

async function copyInstallCommand() {
  try {
    await navigator.clipboard.writeText(installCommand)
  } catch {
    const input = document.createElement('textarea')
    input.value = installCommand
    input.style.position = 'fixed'
    input.style.opacity = '0'
    document.body.append(input)
    input.select()
    document.execCommand('copy')
    input.remove()
  }

  hasCopied.value = true
  window.setTimeout(() => {
    hasCopied.value = false
  }, 1800)
}
</script>

<template>
  <main class="site-shell">
    <header class="site-header">
      <NuxtLink
        to="/"
        class="brand"
        aria-label="rvr home"
      >rvr<span>✳</span></NuxtLink>
      <nav aria-label="Main navigation">
        <a href="#system">System</a>
        <a href="/docs/">Docs</a>
        <a
          href="https://github.com/LeJamon/rvr"
          target="_blank"
          rel="noreferrer"
        >GitHub ↗</a>
      </nav>
      <span aria-hidden="true" />
    </header>

    <section class="hero">
      <div class="hero-copy">
        <h1>One tab.<br><em>Every agent.</em></h1>
        <p class="hero-description">
          rvr is the durable session layer for autonomous coding work. It lets you run and manage multiple agents in one tab—across any model and any harness.
        </p>
        <div class="hero-actions">
          <div class="hero-install">
            <code><span>$</span> {{ installCommand }}</code>
            <button
              type="button"
              @click="copyInstallCommand"
            >
              {{ hasCopied ? 'Copied' : 'Copy' }}
            </button>
          </div>
        </div>
      </div>
      <div
        class="squid-stage"
        aria-hidden="true"
      >
        <img
          src="/images/rvr-squid.png"
          alt=""
          class="squid-art"
        >
      </div>
      <p class="hero-index">
        01 / RVR — SESSION INFRASTRUCTURE
      </p>
    </section>

    <section
      id="system"
      class="system-section"
    >
      <div class="feature-grid">
        <article>
          <p>01 / HARNESS-AGNOSTIC</p>
          <h3>Choose the agent you need.</h3>
          <img
            src="/images/harness-agnostic.png"
            alt="Three keys orbiting a central control dial"
            class="feature-image"
          >
          <small>Change models. Change harnesses. Keep a single session control layer.</small>
        </article>
        <article>
          <p>02 / MULTI-AGENT</p>
          <h3>Work in parallel without losing the plot.</h3>
          <img
            src="/images/multi-agent.png"
            alt="Three autonomous survey drones converging on one shared field map"
            class="feature-image"
          >
          <small>Run multiple agents from one tab, with their state and output always within reach.</small>
        </article>
        <article>
          <p>03 / DURABLE</p>
          <h3>Step away. Come back exactly where you left off.</h3>
          <img
            src="/images/durable-session.png"
            alt="A mechanical lantern protected inside a glass bell"
            class="feature-image"
          >
          <small>Detached supervision keeps work alive beyond a terminal, a dashboard, or a reboot.</small>
        </article>
      </div>
    </section>

    <footer>
      <span>rvr / durable session infrastructure</span>
      <a
        href="https://github.com/LeJamon/rvr"
        target="_blank"
        rel="noreferrer"
      >GitHub ↗</a>
    </footer>
  </main>
</template>

<style>
@import url('https://fonts.googleapis.com/css2?family=Bodoni+Moda:ital,opsz,wght@0,6..96,400;0,6..96,500;1,6..96,400&family=DM+Mono:wght@400;500&family=Space+Grotesk:wght@400;500;600&display=swap');

:root { --ink: #060606; --paper: #f5f1e9; --blue: #1b20ff; --electric: #514fff; --red: #ff493f; --line: #ffffff2b; --muted: #afaaa1; }
* { box-sizing: border-box; }
html { scroll-behavior: smooth; }
body { margin: 0; background: var(--ink); color: var(--paper); font-family: 'Space Grotesk', sans-serif; }
a { color: inherit; text-decoration: none; }
button { font: inherit; }
.site-shell { overflow: hidden; background: var(--ink); }
.site-header { display: grid; grid-template-columns: 1fr auto 1fr; align-items: center; min-height: 72px; padding: 0 clamp(20px, 4vw, 72px); border-bottom: 1px solid var(--line); font: 10px 'DM Mono', monospace; letter-spacing: .08em; text-transform: uppercase; }
.brand { justify-self: start; font: 600 26px 'Space Grotesk', sans-serif; letter-spacing: -.1em; text-transform: lowercase; }.brand span { color: var(--red); font-size: 14px; vertical-align: top; }
.site-header nav { display: flex; gap: 28px; color: #b9b5ad; }.site-header nav a:hover, footer a:hover { color: var(--red); }
.hero { position: relative; display: grid; grid-template-columns: minmax(0, .9fr) minmax(460px, 1.1fr); min-height: min(830px, calc(100vh - 72px)); padding: clamp(56px, 8vw, 128px) clamp(20px, 9vw, 150px) 64px; background: var(--ink); border-bottom: 1px solid var(--line); }
.hero-copy { position: relative; z-index: 2; align-self: start; }.kicker { margin: 0; color: #aaa59c; font: 10px 'DM Mono', monospace; letter-spacing: .14em; line-height: 1.5; text-transform: uppercase; }.hero h1 { margin: 0 0 22px; font: 500 clamp(60px, 7.3vw, 124px)/.82 'Bodoni Moda', Didot, serif; letter-spacing: -.075em; }.hero h1 em { color: var(--red); font-style: italic; }.hero-description { max-width: 440px; margin: 0; color: #d0ccc4; font-size: 17px; line-height: 1.55; }.hero-actions { margin-top: 34px; }.hero-install { display: inline-flex; max-width: 100%; align-items: stretch; overflow-x: auto; color: var(--paper); background: #101010; border: 1px solid var(--line); font: 11px 'DM Mono', monospace; white-space: nowrap; }.hero-install code { padding: 15px 18px; }.hero-install code span { margin-right: 10px; color: var(--red); }.hero-install button { padding: 0 15px; color: var(--paper); background: transparent; border: 0; border-left: 1px solid var(--line); cursor: pointer; font: inherit; text-transform: uppercase; }.hero-install button:hover { color: var(--ink); background: var(--red); }
.squid-stage { position: relative; align-self: center; height: min(680px, 68vw); }.squid-art { position: absolute; right: -8%; bottom: -7%; z-index: 1; width: min(690px, 58vw); }.hero-index { position: absolute; bottom: 26px; left: clamp(20px, 9vw, 150px); margin: 0; color: #77736c; font: 10px 'DM Mono', monospace; letter-spacing: .1em; }
.system-section { padding: clamp(80px, 11vw, 170px) clamp(20px, 9vw, 150px); }.feature-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }.feature-grid article { display: flex; min-height: 530px; flex-direction: column; padding: 24px; border: 1px solid var(--line); transition: border-color 180ms ease, box-shadow 180ms ease, transform 180ms ease; }.feature-grid article:hover { border-color: var(--red); box-shadow: 9px 9px 0 #ff493f33; transform: translate(-4px, -4px); }.feature-grid article > p, .feature-grid small { margin: 0; color: #938f87; font: 10px/1.6 'DM Mono', monospace; letter-spacing: .08em; text-transform: uppercase; }.feature-grid h3 { max-width: 270px; margin: 74px 0 25px; font: 500 38px/.91 'Bodoni Moda', Didot, serif; letter-spacing: -.055em; transition: color 180ms ease; }.feature-grid article:hover h3 { color: var(--red); }.feature-grid small { margin-top: auto; }.feature-image { width: 100%; min-height: 170px; margin-top: auto; margin-bottom: 24px; border-top: 1px solid var(--line); border-bottom: 1px solid var(--line); object-fit: contain; transition: transform 240ms ease; }.feature-grid article:hover .feature-image { transform: scale(1.035); }
footer { display: flex; justify-content: space-between; gap: 20px; padding: 24px clamp(20px, 4vw, 72px); color: #88837a; border-top: 1px solid var(--line); font: 10px 'DM Mono', monospace; letter-spacing: .08em; text-transform: uppercase; }
@media (max-width: 900px) { .hero { grid-template-columns: 1fr; min-height: auto; }.hero-copy { max-width: 620px; }.squid-stage { height: 480px; margin-top: 35px; }.squid-art { width: min(590px, 90vw); }.feature-grid { grid-template-columns: 1fr; }.feature-grid article { min-height: 400px; }.feature-grid h3 { margin-top: 40px; } }
@media (max-width: 600px) { .site-header { grid-template-columns: 1fr auto; }.site-header nav { display: none; }.hero { padding-top: 76px; }.hero h1 { font-size: 70px; }.hero-description { font-size: 15px; }.squid-stage { height: 330px; }.squid-art { bottom: -15%; }footer { flex-direction: column; } }
@media (prefers-reduced-motion: reduce) { .feature-grid article, .feature-grid h3, .feature-image { transition: none; } }
</style>
