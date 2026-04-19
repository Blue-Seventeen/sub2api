<template>
  <div class="space-y-6">
    <section class="grid gap-4 md:grid-cols-3">
      <article v-for="card in summaryCards" :key="card.label" class="promo-stat-card">
        <div class="flex items-start justify-between gap-3">
          <div>
            <div class="text-sm text-slate-400">{{ card.label }}</div>
            <div class="mt-3 text-3xl font-semibold" :class="card.valueClass">{{ card.value }}</div>
            <div class="mt-2 text-xs text-slate-500">{{ card.note }}</div>
          </div>
          <div class="flex h-12 w-12 items-center justify-center rounded-2xl border border-white/10 bg-white/5" :class="card.iconTone">
            <Icon :name="card.icon" size="lg" />
          </div>
        </div>
      </article>
    </section>

    <section class="promo-panel-soft">
      <div class="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
        <div class="flex flex-wrap gap-2">
          <button
            v-for="option in filterOptions"
            :key="option.value"
            type="button"
            class="promo-btn px-4 py-2"
            :class="status === option.value ? 'promo-btn-primary' : 'promo-btn-secondary'"
            @click="changeStatus(option.value)"
          >
            {{ option.label }}
          </button>
        </div>
        <div class="grid gap-3 sm:grid-cols-2 xl:w-[1080px] xl:grid-cols-5">
          <label class="space-y-2 sm:col-span-2 xl:col-span-2">
            <span class="text-xs uppercase tracking-[0.24em] text-slate-500">搜索用户</span>
            <div class="flex gap-2">
              <input
                v-model.trim="keywordInput"
                type="text"
                class="promo-input-dark flex-1"
                placeholder="输入用户名搜索你的一二级下级"
                @keyup.enter="applyKeywordSearch"
              />
              <button type="button" class="promo-btn promo-btn-secondary px-4 py-2" @click="applyKeywordSearch">搜索</button>
            </div>
          </label>
          <label class="space-y-2">
            <span class="text-xs uppercase tracking-[0.24em] text-slate-500">排序字段</span>
            <select v-model="sortBy" class="promo-input-dark" @change="handleSortChange">
              <option v-for="option in sortByOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </label>
          <label class="space-y-2">
            <span class="text-xs uppercase tracking-[0.24em] text-slate-500">排序方向</span>
            <select v-model="sortOrder" class="promo-input-dark" @change="handleSortChange">
              <option v-for="option in sortOrderOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </label>
          <label class="space-y-2">
            <span class="text-xs uppercase tracking-[0.24em] text-slate-500">每页条数</span>
            <select v-model.number="pageSizeValue" class="promo-input-dark" @change="handlePageSizeSelect">
              <option v-for="option in pageSizeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </label>
        </div>
      </div>
    </section>

    <section class="promo-table-shell">
      <div class="overflow-x-auto">
        <table class="min-w-[1180px] w-full table-fixed text-sm text-slate-200">
          <thead class="promo-table-head">
            <tr>
              <th class="w-[28%] whitespace-nowrap px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">用户</th>
              <th class="w-[12%] whitespace-nowrap px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">等级</th>
              <th class="w-[11%] whitespace-nowrap px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">层级</th>
              <th class="w-[11%] whitespace-nowrap px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">状态</th>
              <th class="w-[10%] whitespace-nowrap px-4 py-3 text-right text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">今日贡献</th>
              <th class="w-[10%] whitespace-nowrap px-4 py-3 text-right text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">累计贡献</th>
              <th class="w-[9%] whitespace-nowrap px-4 py-3 text-right text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">加入时间</th>
              <th class="w-[9%] whitespace-nowrap px-4 py-3 text-right text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">激活时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading" class="promo-table-row">
              <td colspan="8" class="px-4 py-12 text-center text-slate-400">团队数据加载中...</td>
            </tr>
            <tr v-else-if="!items.length" class="promo-table-row">
              <td colspan="8" class="px-4 py-12 text-center text-slate-400">暂无团队成员</td>
            </tr>
            <tr
              v-for="item in items"
              :key="`${item.masked_email}-${item.joined_at}-${item.relation_depth}`"
              class="promo-table-row last:border-b-0"
            >
              <td class="px-4 py-4">
                <div class="truncate whitespace-nowrap font-medium text-white" :title="item.masked_email">{{ item.masked_email }}</div>
                <div v-if="item.username" class="mt-1 truncate whitespace-nowrap text-xs text-slate-500" :title="`用户名：${item.username}`">用户名：{{ item.username }}</div>
              </td>
              <td class="px-4 py-4">
                <span class="inline-flex max-w-full items-center rounded-full px-3 py-1 text-xs font-medium whitespace-nowrap" :class="levelTagClass(item.level_name)">
                  {{ levelTagLabel(item.level_name) }}
                </span>
              </td>
              <td class="px-4 py-4">
                <span class="inline-flex max-w-full items-center rounded-full px-3 py-1 text-xs font-medium whitespace-nowrap" :class="relationDepthTagClass(item.relation_depth)">
                  {{ relationDepthLabel(item.relation_depth) }}
                </span>
              </td>
              <td class="px-4 py-4">
                <span class="inline-flex max-w-full items-center rounded-full px-3 py-1 text-xs font-medium whitespace-nowrap" :class="statusTagClass(item.activated)">
                  {{ item.activated ? '已激活' : '未激活' }}
                </span>
              </td>
              <td class="px-4 py-4 text-right text-base font-semibold whitespace-nowrap text-cyan-300">${{ money(item.today_contribution) }}</td>
              <td class="px-4 py-4 text-right text-base font-semibold whitespace-nowrap text-emerald-300">${{ money(item.total_contribution) }}</td>
              <td class="px-4 py-4 text-right text-xs leading-6 text-slate-400">
                <div class="truncate whitespace-nowrap" :title="formatDate(item.joined_at)">{{ formatDate(item.joined_at) }}</div>
              </td>
              <td class="px-4 py-4 text-right text-xs leading-6 text-slate-400">
                <div class="truncate whitespace-nowrap" :title="item.activated_at ? formatDate(item.activated_at) : '-'">{{ item.activated_at ? formatDate(item.activated_at) : '-' }}</div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="border-t border-white/10 px-4 py-4">
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </div>
    </section>

    <section class="rounded-2xl border border-cyan-400/20 bg-cyan-500/10 p-4 text-sm leading-7 text-cyan-100">
      团队列表默认按照“今日贡献降序”展示，默认每页 10 条。搜索仅会命中你自己的一二级下级成员，且分页与排序偏好会保存在当前浏览器。
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import { promotionAPI, type PromotionOverview, type PromotionTeamItem } from '@/api/promotion'
import { useAppStore } from '@/stores'
import {
  getPromotionTeamPageSize,
  getPromotionTeamSortBy,
  getPromotionTeamSortOrder,
  setPromotionTeamPageSize,
  setPromotionTeamSortBy,
  setPromotionTeamSortOrder,
  type PromotionTeamSortBy,
  type PromotionTeamSortOrder
} from '@/composables/usePromotionTeamPreferences'

const appStore = useAppStore()
const props = defineProps<{
  overview?: PromotionOverview | null
}>()
const loading = ref(false)
const items = ref<PromotionTeamItem[]>([])
const keyword = ref('')
const keywordInput = ref('')
const status = ref<'all' | 'active' | 'inactive'>('all')
const sortBy = ref<PromotionTeamSortBy>(getPromotionTeamSortBy())
const sortOrder = ref<PromotionTeamSortOrder>(getPromotionTeamSortOrder())
const pageSizeValue = ref<number>(getPromotionTeamPageSize())

const pagination = reactive({
  page: 1,
  page_size: pageSizeValue.value,
  total: 0
})

const filterOptions = [
  { value: 'all', label: '全部' },
  { value: 'active', label: '已激活' },
  { value: 'inactive', label: '未激活' }
] as const

const sortByOptions = [
  { value: 'today_contribution', label: '按今日贡献' },
  { value: 'total_contribution', label: '按累计贡献' },
  { value: 'joined_at', label: '按加入时间' },
  { value: 'activated_at', label: '按激活时间' }
]

const sortOrderOptions = [
  { value: 'desc', label: '降序' },
  { value: 'asc', label: '升序' }
]

const pageSizeOptions = [
  { value: 10, label: '10 / 页' },
  { value: 20, label: '20 / 页' },
  { value: 50, label: '50 / 页' }
]

const totals = computed(() => ({
  total: props.overview?.total_invites ?? pagination.total,
  activated: props.overview?.activated_invites ?? items.value.filter(item => item.activated).length,
  inactive: props.overview?.inactive_invites ?? items.value.filter(item => !item.activated).length
}))

const summaryCards = computed(() => [
  {
    label: '团队总人数',
    value: String(totals.value.total),
    note: '已建立推广绑定关系的全部成员',
    icon: 'users' as const,
    valueClass: 'text-white',
    iconTone: 'text-slate-200'
  },
  {
    label: '已激活',
    value: String(totals.value.activated),
    note: '累计真实消费严格大于门槛的成员',
    icon: 'bolt' as const,
    valueClass: 'text-emerald-300',
    iconTone: 'text-emerald-300'
  },
  {
    label: '未激活',
    value: String(totals.value.inactive),
    note: '仍在等待首轮激活达标的成员',
    icon: 'clock' as const,
    valueClass: 'text-amber-300',
    iconTone: 'text-amber-300'
  }
])

onMounted(() => {
  void fetchItems()
})

async function fetchItems() {
  loading.value = true
  try {
    const data = await promotionAPI.getTeam({
      page: pagination.page,
      page_size: pagination.page_size,
      keyword: keyword.value || undefined,
      status: status.value,
      sort_by: sortBy.value,
      sort_order: sortOrder.value
    })
    items.value = data.items || []
    pagination.total = data.total || 0
  } catch (error) {
    console.error('Failed to load promotion team:', error)
    appStore.showError('加载团队数据失败')
  } finally {
    loading.value = false
  }
}

function changeStatus(nextStatus: 'all' | 'active' | 'inactive') {
  status.value = nextStatus
  pagination.page = 1
  void fetchItems()
}

function applyKeywordSearch() {
  keyword.value = keywordInput.value.trim()
  pagination.page = 1
  void fetchItems()
}

function handleSortChange() {
  setPromotionTeamSortBy(sortBy.value)
  setPromotionTeamSortOrder(sortOrder.value)
  pagination.page = 1
  void fetchItems()
}

function handlePageSizeSelect() {
  pagination.page_size = Number(pageSizeValue.value)
  setPromotionTeamPageSize(pagination.page_size)
  pagination.page = 1
  void fetchItems()
}

function handlePageChange(page: number) {
  pagination.page = page
  void fetchItems()
}

function handlePageSizeChange(size: number) {
  pageSizeValue.value = size
  handlePageSizeSelect()
}

function money(value?: number) {
  return Number(value || 0).toFixed(2)
}

function resolveLevelNo(levelName?: string) {
  const normalized = (levelName || '').trim()
  if (!normalized) return 0
  const matched = props.overview?.level_rate_summaries?.find(item => item.level_name === normalized)
  return matched?.level_no || 0
}

function levelTagLabel(levelName?: string) {
  const normalized = (levelName || '').trim() || '未配置等级'
  const levelNo = resolveLevelNo(normalized)
  return levelNo > 0 ? `Lv${levelNo} ${normalized}` : normalized
}

function levelTagClass(levelName?: string) {
  const levelNo = resolveLevelNo(levelName)
  switch (levelNo) {
    case 1:
      return 'border border-white/10 bg-white/5 text-slate-300'
    case 2:
      return 'border border-cyan-400/20 bg-cyan-500/10 text-cyan-300'
    case 3:
      return 'border border-violet-400/20 bg-violet-500/10 text-violet-300'
    case 4:
      return 'border border-amber-400/20 bg-amber-500/10 text-amber-300'
    default:
      return levelNo > 4
        ? 'border border-emerald-400/20 bg-emerald-500/10 text-emerald-300'
        : 'border border-white/10 bg-white/5 text-slate-400'
  }
}

function relationDepthTagClass(depth?: number) {
  switch (depth) {
    case 1:
      return 'border border-cyan-400/20 bg-cyan-500/10 text-cyan-300'
    case 2:
      return 'border border-purple-400/20 bg-purple-500/10 text-purple-300'
    default:
      return 'border border-slate-400/20 bg-slate-500/10 text-slate-300'
  }
}

function relationDepthLabel(depth?: number) {
  switch (depth) {
    case 1:
      return '一级子代理'
    case 2:
      return '二级子代理'
    default:
      return `${depth || 0}级子代理`
  }
}

function statusTagClass(activated: boolean) {
  return activated
    ? 'border border-emerald-400/20 bg-emerald-500/10 text-emerald-300'
    : 'border border-white/10 bg-white/5 text-slate-400'
}

function formatDate(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}
</script>
