<template>
  <AppLayout>
    <div class="custom-page-layout">
      <div class="card flex-1 min-h-0 overflow-hidden">
        <div v-if="loading" class="flex h-full items-center justify-center py-12">
          <div
            class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"
          ></div>
        </div>

        <div
          v-else-if="!menuItem"
          class="flex h-full items-center justify-center p-10 text-center"
        >
          <div class="max-w-md">
            <div
              class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700"
            >
              <Icon name="link" size="lg" class="text-gray-400" />
            </div>
            <h3 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('customPage.notFoundTitle') }}
            </h3>
            <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
              {{ t('customPage.notFoundDesc') }}
            </p>
          </div>
        </div>

        <div v-else-if="isMarkdownPage" class="custom-markdown-shell">
          <div v-if="markdownLoading" class="flex h-full items-center justify-center py-12">
            <div
              class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"
            ></div>
          </div>
          <div
            v-else-if="markdownError"
            class="flex h-full items-center justify-center p-10 text-center"
          >
            <div class="max-w-md">
              <div
                class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700"
              >
                <Icon name="link" size="lg" class="text-gray-400" />
              </div>
              <h3 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('customPage.notConfiguredTitle') }}
              </h3>
              <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
                {{ markdownError }}
              </p>
            </div>
          </div>
          <article v-else class="custom-markdown-body" v-html="markdownHtml"></article>
        </div>

        <div v-else-if="!isValidUrl" class="flex h-full items-center justify-center p-10 text-center">
          <div class="max-w-md">
            <div
              class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700"
            >
              <Icon name="link" size="lg" class="text-gray-400" />
            </div>
            <h3 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('customPage.notConfiguredTitle') }}
            </h3>
            <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
              {{ t('customPage.notConfiguredDesc') }}
            </p>
          </div>
        </div>

        <div
          v-else-if="shouldAutoOpenInNewTab"
          class="flex h-full items-center justify-center p-10 text-center"
        >
          <div class="max-w-md">
            <div
              class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700"
            >
              <Icon name="externalLink" size="lg" class="text-gray-400" />
            </div>
            <h3 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('customPage.autoOpenTitle') }}
            </h3>
            <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
              {{ t('customPage.autoOpenDesc') }}
            </p>
            <a
              ref="openLinkRef"
              :href="embeddedUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="btn btn-secondary mt-5"
            >
              <Icon name="externalLink" size="sm" class="mr-1.5" :stroke-width="2" />
              {{ t('customPage.openInNewTab') }}
            </a>
          </div>
        </div>

        <div v-else class="custom-embed-shell">
          <a
            ref="openLinkRef"
            :href="embeddedUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="btn btn-secondary btn-sm custom-open-fab"
          >
            <Icon name="externalLink" size="sm" class="mr-1.5" :stroke-width="2" />
            {{ t('customPage.openInNewTab') }}
          </a>
          <iframe
            :src="embeddedUrl"
            class="custom-embed-frame"
            allowfullscreen
          ></iframe>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { buildEmbeddedUrl, detectTheme } from '@/utils/embedded-url'
import { getMarkdownPage } from '@/api/pages'
import { marked } from 'marked'
import DOMPurify from 'dompurify'

const { t, locale } = useI18n()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const adminSettingsStore = useAdminSettingsStore()

const loading = ref(false)
const pageTheme = ref<'light' | 'dark'>('light')
const openLinkRef = ref<HTMLAnchorElement | null>(null)
const lastAutoOpenedId = ref<string | null>(null)
const markdownLoading = ref(false)
const markdownHtml = ref('')
const markdownError = ref('')
let themeObserver: MutationObserver | null = null
let markdownLoadSeq = 0

const menuItemId = computed(() => route.params.id as string)

const menuItem = computed(() => {
  const id = menuItemId.value
  const publicItems = appStore.cachedPublicSettings?.custom_menu_items ?? []
  const found = publicItems.find((item) => item.id === id) ?? null
  if (found) return found
  if (authStore.isAdmin) {
    return adminSettingsStore.customMenuItems.find((item) => item.id === id) ?? null
  }
  return null
})

const markdownSlug = computed(() => {
  const rawUrl = String(menuItem.value?.url || '').trim()
  if (!rawUrl.toLowerCase().startsWith('md:')) return ''
  return rawUrl.slice(3).trim()
})

const markdownPagesEnabled = computed(
  () => appStore.cachedPublicSettings?.markdown_pages_enabled === true
)

const isMarkdownPage = computed(
  () => markdownPagesEnabled.value && markdownSlug.value.length > 0
)

const embeddedUrl = computed(() => {
  if (!menuItem.value || isMarkdownPage.value) return ''
  return buildEmbeddedUrl(
    menuItem.value.url,
    authStore.user?.id,
    pageTheme.value,
    locale.value,
  )
})

const isValidUrl = computed(() => {
  if (isMarkdownPage.value) return false
  const url = embeddedUrl.value
  return url.startsWith('http://') || url.startsWith('https://')
})

const shouldAutoOpenInNewTab = computed(() =>
  menuItem.value?.open_in_new_tab === true && isValidUrl.value
)

async function triggerAutoOpen(menuId: string) {
  if (!shouldAutoOpenInNewTab.value || lastAutoOpenedId.value === menuId) return
  lastAutoOpenedId.value = menuId
  await nextTick()
  openLinkRef.value?.click()
}

watch(
  () => ({
    id: menuItemId.value,
    enabled: shouldAutoOpenInNewTab.value,
    url: embeddedUrl.value,
  }),
  ({ id, enabled, url }) => {
    if (!enabled || !url) return
    void triggerAutoOpen(id)
  },
  { immediate: true }
)

watch(
  () => ({
    slug: markdownSlug.value,
    enabled: markdownPagesEnabled.value,
  }),
  () => {
    void loadMarkdownPage()
  },
  { immediate: true }
)

async function loadMarkdownPage() {
  const seq = ++markdownLoadSeq
  if (!isMarkdownPage.value) {
    markdownLoading.value = false
    markdownHtml.value = ''
    markdownError.value = ''
    return
  }

  markdownLoading.value = true
  markdownError.value = ''
  try {
    const slug = markdownSlug.value
    const content = await getMarkdownPage(slug)
    if (seq !== markdownLoadSeq || slug !== markdownSlug.value) return
    const rendered = await marked.parse(content)
    markdownHtml.value = DOMPurify.sanitize(rendered)
  } catch (error: unknown) {
    if (seq !== markdownLoadSeq) return
    markdownHtml.value = ''
    markdownError.value =
      error instanceof Error && error.message
        ? error.message
        : t('customPage.notConfiguredDesc')
  } finally {
    if (seq === markdownLoadSeq) {
      markdownLoading.value = false
    }
  }
}

onMounted(async () => {
  pageTheme.value = detectTheme()

  if (typeof document !== 'undefined') {
    themeObserver = new MutationObserver(() => {
      pageTheme.value = detectTheme()
    })
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    })
  }

  if (appStore.publicSettingsLoaded) return
  loading.value = true
  try {
    await appStore.fetchPublicSettings()
  } finally {
    loading.value = false
  }
})

onUnmounted(() => {
  if (themeObserver) {
    themeObserver.disconnect()
    themeObserver = null
  }
})
</script>

<style scoped>
.custom-page-layout {
  @apply flex flex-col;
  height: calc(100vh - 64px - 4rem);
}

.toc-sidebar {
  @apply flex flex-col h-full border-r border-gray-200 dark:border-dark-600 bg-gray-50 dark:bg-dark-800;
  width: min(240px, 30%);
  min-width: 160px;
  max-width: 280px;
  overflow: hidden;
}

@media (max-width: 640px) {
  .toc-sidebar {
    position: absolute;
    left: 0;
    top: 0;
    z-index: 20;
    width: 70%;
    max-width: 240px;
    height: 100%;
    box-shadow: 2px 0 8px rgba(0, 0, 0, 0.1);
  }
}

.toc-header {
  @apply flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-dark-600;
}

.toc-title {
  @apply text-sm font-semibold text-gray-700 dark:text-dark-200;
}

.toc-close-btn {
  @apply p-1 rounded text-gray-400 hover:text-gray-600 dark:hover:text-dark-200 hover:bg-gray-200 dark:hover:bg-dark-600 transition-colors;
}

.toc-nav {
  @apply flex-1 overflow-y-auto py-2 px-2;
}

.toc-item {
  @apply block px-2 py-1.5 text-sm rounded transition-colors truncate;
  @apply text-gray-600 dark:text-dark-300 hover:text-gray-900 dark:hover:text-white hover:bg-gray-200 dark:hover:bg-dark-600;
}

.toc-item.toc-active {
  @apply text-primary-600 dark:text-primary-400 bg-primary-50 dark:bg-primary-900/20 font-medium;
}

.toc-level-1 { padding-left: 8px; }
.toc-level-2 { padding-left: 20px; }
.toc-level-3 { padding-left: 32px; }
.toc-level-4 { padding-left: 44px; }

.toc-toggle-btn {
  @apply absolute left-2 top-2 z-10 flex items-center px-2 py-1.5 rounded-md text-sm;
  @apply bg-white dark:bg-dark-700 border border-gray-200 dark:border-dark-500;
  @apply text-gray-600 dark:text-dark-300 hover:bg-gray-100 dark:hover:bg-dark-600;
  @apply shadow-sm transition-colors cursor-pointer;
}

.custom-embed-shell {
  @apply relative;
  @apply h-full w-full overflow-hidden rounded-2xl;
  @apply bg-gradient-to-b from-gray-50 to-white dark:from-dark-900 dark:to-dark-950;
  @apply p-0;
}

.custom-open-fab {
  @apply absolute right-3 top-3 z-10;
  @apply shadow-sm backdrop-blur supports-[backdrop-filter]:bg-white/80 dark:supports-[backdrop-filter]:bg-dark-800/80;
}

.custom-markdown-shell {
  @apply h-full overflow-auto rounded-2xl bg-white p-6 dark:bg-dark-900;
}

.custom-markdown-body {
  @apply mx-auto max-w-4xl text-gray-800 dark:text-gray-100;
}

.custom-markdown-body :deep(h1) {
  @apply mb-6 text-3xl font-bold text-gray-950 dark:text-white;
}

.custom-markdown-body :deep(h2) {
  @apply mb-4 mt-8 text-2xl font-semibold text-gray-950 dark:text-white;
}

.custom-markdown-body :deep(h3) {
  @apply mb-3 mt-6 text-xl font-semibold text-gray-950 dark:text-white;
}

.custom-markdown-body :deep(p) {
  @apply my-4 leading-7;
}

.custom-markdown-body :deep(a) {
  @apply text-primary-600 underline underline-offset-2 dark:text-primary-400;
}

.custom-markdown-body :deep(ul) {
  @apply my-4 list-disc pl-6;
}

.custom-markdown-body :deep(ol) {
  @apply my-4 list-decimal pl-6;
}

.custom-markdown-body :deep(blockquote) {
  @apply my-4 border-l-4 border-gray-200 pl-4 text-gray-600 dark:border-dark-600 dark:text-gray-300;
}

.custom-markdown-body :deep(code) {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-sm dark:bg-dark-700;
}

.custom-markdown-body :deep(pre) {
  @apply my-4 overflow-auto rounded-xl bg-gray-950 p-4 text-gray-100;
}

.custom-markdown-body :deep(pre code) {
  @apply bg-transparent p-0 text-gray-100;
}

.custom-markdown-body :deep(img) {
  @apply my-5 max-w-full rounded-xl border border-gray-100 dark:border-dark-700;
}

.custom-markdown-body :deep(table) {
  @apply my-5 w-full border-collapse text-left text-sm;
}

.custom-markdown-body :deep(th),
.custom-markdown-body :deep(td) {
  @apply border border-gray-200 px-3 py-2 dark:border-dark-700;
}

.custom-embed-frame {
  display: block;
  margin: 0;
  width: 100%;
  height: 100%;
  border: 0;
  border-radius: 0;
  box-shadow: none;
  background: transparent;
}
</style>

<style>
.markdown-page-content {
  line-height: 1.7;
  color: inherit;
}
.markdown-page-content h1 { @apply text-3xl font-bold mt-8 mb-4 pb-2 border-b border-gray-200 dark:border-dark-600; }
.markdown-page-content h2 { @apply text-2xl font-bold mt-6 mb-3; }
.markdown-page-content h3 { @apply text-xl font-semibold mt-5 mb-2; }
.markdown-page-content h4 { @apply text-lg font-semibold mt-4 mb-2; }
.markdown-page-content p { @apply mb-4; }
.markdown-page-content ul { @apply list-disc pl-6 mb-4; }
.markdown-page-content ol { @apply list-decimal pl-6 mb-4; }
.markdown-page-content li { @apply mb-1; }
.markdown-page-content a { @apply text-primary-500 hover:text-primary-600 underline; }
.markdown-page-content blockquote { @apply border-l-4 border-gray-300 dark:border-dark-500 pl-4 italic text-gray-600 dark:text-dark-300 my-4; }
.markdown-page-content img { @apply max-w-full h-auto rounded-lg my-4; }
.markdown-page-content table { @apply w-full border-collapse my-4; }
.markdown-page-content th { @apply border border-gray-300 dark:border-dark-500 px-3 py-2 bg-gray-50 dark:bg-dark-700 font-semibold text-left; }
.markdown-page-content td { @apply border border-gray-300 dark:border-dark-500 px-3 py-2; }
.markdown-page-content code { @apply bg-gray-100 dark:bg-dark-700 px-1.5 py-0.5 rounded text-sm font-mono; }
.markdown-page-content pre { @apply bg-gray-900 dark:bg-dark-900 text-gray-100 p-4 rounded-lg overflow-x-auto my-4 relative; }
.markdown-page-content pre code { @apply bg-transparent p-0 text-inherit; }
.markdown-page-content hr { @apply my-6 border-gray-200 dark:border-dark-600; }

.copy-btn {
  position: absolute;
  top: 8px;
  right: 8px;
  padding: 4px 10px;
  font-size: 12px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.15);
  color: #e2e8f0;
  border: 1px solid rgba(255, 255, 255, 0.2);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s, background 0.2s;
  font-family: inherit;
}
.copy-btn:hover { background: rgba(255, 255, 255, 0.25); }
pre:hover .copy-btn { opacity: 1; }
</style>
