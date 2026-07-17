<script setup>
useHead({
  meta: [
    { name: 'viewport', content: 'width=device-width, initial-scale=1' }
  ],
  link: [
    { rel: 'icon', href: '/favicon.ico' }
  ],
  htmlAttrs: {
    lang: 'en'
  }
})

const title = 'rvr'
const description = 'tmux + htop for AI agents'

useSeoMeta({
  title,
  description,
  ogTitle: title,
  ogDescription: description,
  ogImage: '/og.svg',
  twitterCard: 'summary_large_image'
})
</script>

<template>
  <UApp class="bg-[#0a0a0a] text-white">
    <NuxtPage />
  </UApp>
</template>
