<template>
  <div class="space-y-4">
    <section class="promo-panel-soft">
      <div class="flex flex-col gap-3 xl:flex-row xl:items-end xl:justify-between">
        <div class="grid flex-1 gap-3 xl:grid-cols-[minmax(0,1fr)_auto] xl:items-end">
          <label class="space-y-2 xl:min-w-[620px]">
            <span class="text-xs uppercase tracking-[0.24em] text-slate-500">关键词</span>
            <input v-model="keyword" type="text" class="promo-input-dark" placeholder="搜索话术名称 / 标签 / 内容..." @keyup.enter="reload" />
          </label>
          <div class="flex items-end xl:justify-end">
            <button type="button" class="promo-btn promo-btn-secondary px-4 py-2.5" @click="reload">
              <Icon name="search" size="sm" />
              查询
            </button>
          </div>
        </div>
        <button type="button" class="promo-btn promo-btn-primary" @click="openCreate">
          <Icon name="plus" size="sm" />
          新增话术
        </button>
      </div>
    </section>

    <section class="grid gap-4 xl:grid-cols-2">
      <article v-if="loading" class="promo-empty-state xl:col-span-2">推广话术加载中...</article>
      <article v-else-if="!items.length" class="promo-empty-state xl:col-span-2">暂无推广话术</article>
      <template v-else>
        <article
          v-for="item in items"
          :key="item.id"
          class="group flex h-full flex-col overflow-hidden rounded-2xl border border-white/10 bg-slate-950/40 p-5 shadow-[0_20px_50px_-24px_rgba(0,0,0,0.75)] transition hover:border-cyan-400/20 hover:bg-slate-900/70"
        >
          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <span class="rounded-full border px-3 py-1 text-xs font-medium" :class="tagColorClass(item.category)">
                  {{ displayTag(item.category) }}
                </span>
                <span class="rounded-full border px-3 py-1 text-xs font-medium" :class="item.enabled ? 'border-emerald-400/20 bg-emerald-500/10 text-emerald-300' : 'border-white/10 bg-white/5 text-slate-400'">
                  {{ item.enabled ? '启用中' : '已停用' }}
                </span>
              </div>
              <div class="mt-3 truncate text-lg font-semibold text-white">{{ item.name }}</div>
            </div>
            <div class="flex shrink-0 flex-wrap justify-end gap-2">
              <button type="button" class="promo-btn promo-btn-secondary px-3 py-2 text-xs whitespace-nowrap" @click="editItem(item)">
                <Icon name="edit" size="sm" />
                编辑
              </button>
              <button type="button" class="promo-btn promo-btn-danger px-3 py-2 text-xs whitespace-nowrap" @click="removeItem(item.id)">
                <Icon name="trash" size="sm" />
                删除
              </button>
            </div>
          </div>

          <div class="mt-4 grid gap-3 sm:grid-cols-3">
            <div class="rounded-xl border border-white/10 bg-white/5 px-4 py-3">
              <div class="text-[11px] uppercase tracking-[0.2em] text-slate-500">创建时间</div>
              <div class="mt-2 text-sm text-slate-300">{{ formatDate(item.created_at) }}</div>
            </div>
            <div class="rounded-xl border border-white/10 bg-white/5 px-4 py-3">
              <div class="text-[11px] uppercase tracking-[0.2em] text-slate-500">使用次数</div>
              <div class="mt-2 text-sm font-medium text-cyan-300">{{ item.use_count }} 次</div>
            </div>
            <div class="rounded-xl border border-white/10 bg-white/5 px-4 py-3">
              <div class="text-[11px] uppercase tracking-[0.2em] text-slate-500">标签预览</div>
              <div class="mt-2 truncate text-sm text-slate-300">{{ displayTag(item.category) }}</div>
            </div>
          </div>

          <div class="mt-4 flex-1 rounded-2xl border border-white/10 bg-slate-900/70 p-4">
            <div class="mb-2 text-xs uppercase tracking-[0.2em] text-slate-500">话术预览</div>
            <div class="max-h-[168px] overflow-auto text-sm leading-7 whitespace-pre-wrap text-slate-300">
              {{ renderPreview(item.content) }}
            </div>
          </div>
        </article>
      </template>
    </section>

    <section class="promo-panel-soft">
      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />
    </section>

    <PromotionScriptDialog
      :show="showDialog"
      :submitting="submitting"
      :model-value="editingItem"
      @close="showDialog = false"
      @confirm="saveItem"
    />
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import PromotionScriptDialog from './PromotionScriptDialog.vue'
import { adminPromotionAPI } from '@/api/admin/promotion'
import { getAdminPromotionPageSize, setAdminPromotionPageSize } from '@/composables/useAdminPromotionPreferences'
import type { PromotionScript } from '@/api/promotion'
import { useAppStore } from '@/stores'

const appStore = useAppStore()
const loading = ref(false)
const submitting = ref(false)
const items = ref<PromotionScript[]>([])
const keyword = ref('')
const showDialog = ref(false)
const editingItem = ref<PromotionScript | null>(null)
const pagination = reactive({
  page: 1,
  page_size: getAdminPromotionPageSize('scripts', 12),
  total: 0
})

const legacyTagMap: Record<string, string> = {
  default: '默认',
  wechat: '朋友圈',
  tech: '技术群',
  social: '社交平台',
  email: '邮件'
}

const tagColorPalette = [
  'border-purple-400/20 bg-purple-500/10 text-purple-300',
  'border-cyan-400/20 bg-cyan-500/10 text-cyan-300',
  'border-emerald-400/20 bg-emerald-500/10 text-emerald-300',
  'border-amber-400/20 bg-amber-500/10 text-amber-300',
  'border-pink-400/20 bg-pink-500/10 text-pink-300',
  'border-indigo-400/20 bg-indigo-500/10 text-indigo-300',
  'border-sky-400/20 bg-sky-500/10 text-sky-300',
  'border-rose-400/20 bg-rose-500/10 text-rose-300'
]

onMounted(() => {
  void fetchItems()
})

async function fetchItems() {
  loading.value = true
  try {
    const response = await adminPromotionAPI.getScripts({
      page: pagination.page,
      page_size: pagination.page_size,
      keyword: keyword.value || undefined
    })
    items.value = response.data.items || []
    pagination.total = response.data.total || 0
  } catch (error) {
    console.error('Failed to load promotion scripts:', error)
    appStore.showError('加载推广话术失败')
  } finally {
    loading.value = false
  }
}

function reload() {
  pagination.page = 1
  void fetchItems()
}

function openCreate() {
  editingItem.value = null
  showDialog.value = true
}

function editItem(item: PromotionScript) {
  editingItem.value = item
  showDialog.value = true
}

async function saveItem(payload: { id?: number; name: string; category: string; content: string; enabled: boolean }) {
  submitting.value = true
  try {
    if (payload.id) {
      await adminPromotionAPI.updateScript(payload.id, payload)
      appStore.showSuccess('推广话术已更新')
    } else {
      await adminPromotionAPI.createScript(payload)
      appStore.showSuccess('推广话术已创建')
    }
    showDialog.value = false
    await fetchItems()
  } catch (error) {
    console.error('Failed to save promotion script:', error)
    appStore.showError('保存推广话术失败')
  } finally {
    submitting.value = false
  }
}

async function removeItem(id: number) {
  try {
    await adminPromotionAPI.deleteScript(id)
    appStore.showSuccess('推广话术已删除')
    await fetchItems()
  } catch (error) {
    console.error('Failed to delete promotion script:', error)
    appStore.showError('删除推广话术失败')
  }
}

function handlePageChange(page: number) {
  pagination.page = page
  void fetchItems()
}

function handlePageSizeChange(size: number) {
  pagination.page_size = size
  setAdminPromotionPageSize('scripts', size)
  pagination.page = 1
  void fetchItems()
}

function displayTag(value?: string) {
  const normalized = String(value || '').trim()
  if (!normalized) return '默认'
  return legacyTagMap[normalized] || normalized
}

function tagColorClass(value?: string) {
  const label = displayTag(value)
  let hash = 0
  for (const char of label) {
    hash = (hash * 31 + char.charCodeAt(0)) >>> 0
  }
  return tagColorPalette[hash % tagColorPalette.length]
}

function formatDate(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function renderPreview(content?: string) {
  return String(content || '')
    .replace(/\{\{INVITE_CODE\}\}/g, 'DA044C326EC7BF13')
    .replace(/\{\{REF_LINK\}\}/g, 'https://api.example.test/?ref=DA044C326EC7BF13')
    .replace(/\{\{USER_NAME\}\}/g, '推广达人')
    .replace(/\{\{SITE_NAME\}\}/g, 'Sub2API')
    .replace(/\{\{LEVEL\}\}/g, 'Lv3 推广大使')
    .replace(/\{\{TOTAL_EARNINGS\}\}/g, '128.50')
}
</script>
