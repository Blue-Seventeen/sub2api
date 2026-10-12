<template>
  <button
    ref="trigger"
    type="button"
    data-testid="usage-ip-filter-trigger"
    class="inline-flex items-center gap-0.5 rounded p-1 transition-colors focus:outline-none focus:ring-2 focus:ring-primary-500"
    :class="modelValue.length ? 'bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400' : 'text-gray-400 hover:bg-gray-100 hover:text-primary-600 dark:text-dark-400 dark:hover:bg-dark-700 dark:hover:text-primary-400'"
    :aria-label="modelValue.length ? t('usage.ipFilter.active', { count: modelValue.length }) : t('usage.ipFilter.title')"
    :title="t('usage.ipFilter.title')"
    aria-haspopup="dialog"
    :aria-expanded="open"
    @click.stop="open ? close() : show()"
    @keydown.down.prevent="show"
  >
    <Icon name="filter" size="sm" />
    <span v-if="modelValue.length" class="text-[10px] font-semibold">{{ modelValue.length }}</span>
  </button>
  <Teleport to="body">
    <div
      v-if="open"
      ref="panel"
      data-testid="usage-ip-filter-panel"
      role="dialog"
      :aria-label="t('usage.ipFilter.title')"
      class="fixed z-[100000030] flex flex-col rounded-lg border border-gray-200 bg-white text-left text-sm font-normal normal-case tracking-normal text-gray-700 shadow-xl dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200"
      :style="position"
      @click.stop
      @keydown.esc.stop.prevent="close(true)"
    >
      <div class="border-b border-gray-100 p-3 dark:border-dark-700">
        <label class="flex items-center gap-2 rounded-md border border-gray-200 px-2 dark:border-dark-600">
          <Icon name="search" size="sm" class="shrink-0 text-gray-400" />
          <input
            ref="searchInput"
            v-model="search"
            data-testid="usage-ip-filter-search"
            type="search"
            :placeholder="t('usage.ipFilter.search')"
            :aria-label="t('usage.ipFilter.search')"
            class="min-w-0 flex-1 bg-transparent py-2 text-sm outline-none"
          />
        </label>
        <p class="mt-2 text-xs text-gray-400 dark:text-dark-400">{{ t('usage.ipFilter.scope') }}</p>
      </div>
      <div class="min-h-0 flex-1 overflow-y-auto p-2">
        <label v-for="ip in displayedOptions" :key="ip" class="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 hover:bg-gray-50 dark:hover:bg-dark-700">
          <input
            :data-ip="ip"
            type="checkbox"
            :checked="draft.includes(ip)"
            :disabled="draft.length >= 100 && !draft.includes(ip)"
            class="h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500 disabled:opacity-50 dark:border-dark-500 dark:bg-dark-900"
            @change="toggle(ip, ($event.target as HTMLInputElement).checked)"
          />
          <span class="break-all font-mono text-xs">{{ ip }}</span>
        </label>
        <p v-if="loading" role="status" class="px-2 py-3 text-xs text-gray-500">{{ t('common.loading') }}</p>
        <div v-else-if="failed" data-testid="usage-ip-filter-error" class="p-2 text-xs text-red-600 dark:text-red-400">
          {{ t('usage.ipFilter.failed') }}
          <button type="button" data-testid="usage-ip-filter-retry" class="ml-2 underline" @click="load(nextOffset > 0)">{{ t('usage.ipFilter.retry') }}</button>
        </div>
        <p v-else-if="!displayedOptions.length" class="px-2 py-3 text-xs text-gray-500">{{ t('usage.ipFilter.empty') }}</p>
        <button v-if="hasMore && !failed" type="button" data-testid="usage-ip-filter-more" :disabled="loading" class="w-full rounded px-2 py-2 text-xs text-primary-600 hover:bg-primary-50 disabled:opacity-50 dark:text-primary-400 dark:hover:bg-primary-900/20" @click="load(true)">{{ t('usage.ipFilter.more') }}</button>
      </div>
      <p v-if="draft.length >= 100" class="px-3 pb-2 text-xs text-gray-500">{{ t('usage.ipFilter.limit') }}</p>
      <div class="flex items-center justify-between gap-2 border-t border-gray-100 p-3 dark:border-dark-700">
        <button type="button" data-testid="usage-ip-filter-clear" class="btn btn-secondary btn-sm" @click="clear">{{ t('usage.ipFilter.clear') }}</button>
        <button type="button" data-testid="usage-ip-filter-apply" class="btn btn-primary btn-sm" @click="apply">{{ t('usage.ipFilter.apply') }}</button>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { UsageIPOptions } from '@/types'

const props = withDefaults(defineProps<{
  modelValue: string[]
  loadOptions: (search: string, offset: number, signal: AbortSignal) => Promise<UsageIPOptions>
  refreshVersion?: number
  resetVersion?: number
}>(), { refreshVersion: 0, resetVersion: 0 })
const emit = defineEmits<{ apply: [ips: string[]] }>()
const { t } = useI18n()
const trigger = ref<HTMLButtonElement>()
const panel = ref<HTMLDivElement>()
const searchInput = ref<HTMLInputElement>()
const open = ref(false)
const search = ref('')
const draft = ref<string[]>([])
const options = ref<string[]>([])
const loading = ref(false)
const failed = ref(false)
const hasMore = ref(false)
const position = ref<Record<string, string>>({})
const displayedOptions = computed(() => [...new Set([...draft.value, ...options.value])])
let nextOffset = 0
let sequence = 0
let controller: AbortController | undefined
let debounce: ReturnType<typeof setTimeout> | undefined

function cancelPending() {
  sequence++
  controller?.abort()
  if (debounce) clearTimeout(debounce)
  debounce = undefined
  loading.value = false
}

async function load(append = false) {
  cancelPending()
  const seq = sequence
  const request = new AbortController()
  controller = request
  if (!append) {
    options.value = []
    nextOffset = 0
    hasMore.value = false
  }
  failed.value = false
  loading.value = true
  try {
    const result = await props.loadOptions(search.value.trim(), nextOffset, request.signal)
    if (request.signal.aborted || seq !== sequence || !open.value) return
    options.value = append ? [...new Set([...options.value, ...result.ips])] : result.ips
    nextOffset += result.ips.length
    hasMore.value = result.has_more
  } catch {
    if (!request.signal.aborted && seq === sequence && open.value) failed.value = true
  } finally {
    if (seq === sequence) loading.value = false
  }
}

function updatePosition() {
  if (!open.value || !trigger.value) return
  const rect = trigger.value.getBoundingClientRect()
  const width = Math.max(180, Math.min(288, window.innerWidth - 16))
  const height = Math.min(400, window.innerHeight - 16)
  position.value = {
    width: `${width}px`,
    maxHeight: `${height}px`,
    left: `${Math.max(8, Math.min(rect.right - width, window.innerWidth - width - 8))}px`,
    top: `${Math.max(8, Math.min(rect.bottom + 6, window.innerHeight - height - 8))}px`,
  }
}

async function show() {
  if (open.value) return
  draft.value = [...props.modelValue]
  search.value = ''
  open.value = true
  updatePosition()
  void load()
  await nextTick()
  searchInput.value?.focus()
}

function close(restoreFocus = false) {
  open.value = false
  cancelPending()
  if (restoreFocus) trigger.value?.focus()
}

function toggle(ip: string, selected: boolean) {
  if (selected && !draft.value.includes(ip) && draft.value.length < 100) draft.value.push(ip)
  else if (!selected) draft.value = draft.value.filter(value => value !== ip)
}

function apply() {
  emit('apply', [...new Set(draft.value)].sort())
  close(true)
}

function clear() {
  draft.value = []
  search.value = ''
  emit('apply', [])
  close(true)
}

function onOutside(event: Event) {
  const target = event.target as Node | null
  if (open.value && target && !trigger.value?.contains(target) && !panel.value?.contains(target)) close()
}

watch(search, () => {
  if (!open.value) return
  cancelPending()
  options.value = []
  nextOffset = 0
  hasMore.value = false
  failed.value = false
  debounce = setTimeout(() => { void load() }, 250)
})
watch(() => props.modelValue.join(','), () => { draft.value = [...props.modelValue] })
watch(() => props.refreshVersion, () => { if (open.value) void load() })
watch(() => props.resetVersion, () => { close(); search.value = ''; draft.value = [] })
onMounted(() => {
  document.addEventListener('pointerdown', onOutside)
  window.addEventListener('resize', updatePosition)
  window.addEventListener('scroll', updatePosition, true)
})
onUnmounted(() => {
  cancelPending()
  document.removeEventListener('pointerdown', onOutside)
  window.removeEventListener('resize', updatePosition)
  window.removeEventListener('scroll', updatePosition, true)
})
</script>
