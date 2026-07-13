import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'rvr',
  description: 'Durable session infrastructure for autonomous coding agents',
  lang: 'en-US',
  base: '/docs/',
  head: [
    ['link', { rel: 'preconnect', href: 'https://fonts.googleapis.com' }],
    ['link', { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' }],
    ['link', { rel: 'stylesheet', href: 'https://fonts.googleapis.com/css2?family=Bodoni+Moda:ital,opsz,wght@0,6..96,400;0,6..96,500;1,6..96,400&family=DM+Mono:wght@400;500&family=Space+Grotesk:wght@400;500;600&display=swap' }]
  ],
  themeConfig: {
    nav: [
      { text: 'Guides', items: [{ text: 'Introduction', link: '/' }, { text: 'Getting started', link: '/getting-started' }, { text: 'Dashboard', link: '/dashboard' }] },
      { text: 'Reference', items: [{ text: 'Commands', link: '/commands' }, { text: 'Harnesses', link: '/harnesses' }, { text: 'Configuration', link: '/configuration' }] },
      { text: 'GitHub', link: 'https://github.com/LeJamon/rvr' }
    ],
    sidebar: [
      { text: 'Guides', items: [{ text: 'Introduction', link: '/' }, { text: 'Getting started', link: '/getting-started' }, { text: 'Dashboard', link: '/dashboard' }] },
      { text: 'Reference', items: [{ text: 'Commands', link: '/commands' }, { text: 'Harnesses', link: '/harnesses' }, { text: 'Configuration', link: '/configuration' }] }
    ],
    socialLinks: [{ icon: 'github', link: 'https://github.com/LeJamon/rvr' }],
    search: { provider: 'local' },
    footer: { message: 'Open source under the MIT License.', copyright: 'rvr' }
  },
  markdown: { lineNumbers: true }
})
