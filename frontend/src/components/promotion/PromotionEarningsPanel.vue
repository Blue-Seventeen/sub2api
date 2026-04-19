<template>
  <div class="space-y-6">
    <section class="rounded-2xl border border-cyan-400/20 bg-cyan-500/10 p-4 text-sm leading-7 text-cyan-100">
      收益不是用户一消费就即时到账：系统会在每天固定时刻聚合下级真实消费，生成待结算奖励，然后经过一天审核期后，再在结款时间统一发放到用户真实余额中。
    </section>

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
      <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">搜索记录</span>
          <div class="flex gap-2">
            <input
              v-model.trim="keywordInput"
              type="text"
              class="promo-input-dark flex-1"
              placeholder="输入用户名 / 邮箱 / ID"
              @keyup.enter="reload"
            />
            <button type="button" class="promo-btn promo-btn-secondary px-4 py-2" @click="reload">搜索</button>
          </div>
        </label>
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">奖励类型</span>
          <select v-model="typeFilter" class="promo-input-dark" @change="reload">
            <option v-for="option in typeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
        </label>
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">结算状态</span>
          <select v-model="statusFilter" class="promo-input-dark" @change="reload">
            <option v-for="option in statusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
        </label>
      </div>
    </section>

    <section class="promo-table-shell">
      <div class="overflow-x-auto">
        <table class="min-w-[1080px] w-full table-fixed text-sm text-slate-200">
          <thead class="promo-table-head">
            <tr>
              <th class="w-[20%] whitespace-nowrap px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">记录时间</th>
              <th class="w-[16%] whitespace-nowrap px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">奖励类型</th>
              <th class="w-[40%] whitespace-nowrap px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">来源说明</th>
              <th class="w-[12%] whitespace-nowrap px-4 py-3 text-right text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">奖励金额</th>
              <th class="w-[12%] whitespace-nowrap px-4 py-3 text-center text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading" class="promo-table-row">
              <td colspan="5" class="px-4 py-12 text-center text-slate-400">收益明细加载中...</td>
            </tr>
            <tr v-else-if="!items.length" class="promo-table-row">
              <td colspan="5" class="px-4 py-12 text-center text-slate-400">暂无收益记录</td>
            </tr>
            <tr v-for="item in items" :key="item.id" class="promo-table-row last:border-b-0">
              <td class="px-4 py-4 text-xs leading-6 text-slate-400">
                <div class="truncate whitespace-nowrap" :title="formatDateTime(item.created_at)">{{ formatDateTime(item.created_at) }}</div>
                <div class="mt-1 truncate whitespace-nowrap text-slate-500" :title="`业务日：${formatBusinessDate(item.business_date)}`">业务日：{{ formatBusinessDate(item.business_date) }}</div>
              </td>
              <td class="px-4 py-4">
                <span class="inline-flex max-w-full items-center rounded-full px-3 py-1 text-xs font-medium whitespace-nowrap" :class="typeClass(item.commission_type)">
                  {{ typeLabel(item.commission_type) }}
                </span>
              </td>
              <td class="px-4 py-4 text-sm leading-7 text-slate-300">
                <div class="truncate whitespace-nowrap" :title="detailTitle(item)">{{ detailTitle(item) }}</div>
                <div class="mt-1 truncate whitespace-nowrap text-xs text-slate-500" :title="item.note || '系统按业务日聚合生成奖励记录'">{{ item.note || '系统按业务日聚合生成奖励记录' }}</div>
              </td>
              <td class="px-4 py-4 text-right text-base font-semibold whitespace-nowrap text-emerald-300">${{ money(item.amount) }}</td>
              <td class="px-4 py-4 text-center">
                <span class="inline-flex max-w-full items-center rounded-full px-3 py-1 text-xs font-medium whitespace-nowrap" :class="statusClass(item.status)">
                  {{ statusLabel(item.status) }}
                </span>
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
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import { promotionAPI, type PromotionCommissionItem, type PromotionOverview } from '@/api/promotion'
import { useAppStore } from '@/stores'

const appStore = useAppStore()
const props = defineProps<{
  overview?: PromotionOverview | null
}>()
const loading = ref(false)
const items = ref<PromotionCommissionItem[]>([])
const keywordInput = ref('')
const typeFilter = ref('all')
const statusFilter = ref('all')
const pagination = reactive({
  page: 1,
  page_size: 20,
  total: 0
})

const typeOptions = [
  { value: 'all', label: '全部类型' },
  { value: 'commission', label: '佣金返利' },
  { value: 'activation', label: '激活奖励' },
  { value: 'adjustment', label: '差额调整' }
]

const statusOptions = [
  { value: 'all', label: '全部状态' },
  { value: 'pending', label: '待结算' },
  { value: 'settled', label: '已结算' },
  { value: 'cancelled', label: '已撤销' }
]

const summary = computed(() => ({
  total: props.overview?.total_reward_amount ?? items.value.reduce((sum, item) => sum + (item.amount || 0), 0),
  commission: props.overview?.commission_amount ?? items.value.filter(item => item.commission_type === 'commission').reduce((sum, item) => sum + (item.amount || 0), 0),
  activation: props.overview?.activation_amount ?? items.value.filter(item => item.commission_type === 'activation').reduce((sum, item) => sum + (item.amount || 0), 0)
}))

const summaryCards = computed(() => [
  {
    label: '全部奖励',
    value: `$${money(summary.value.total)}`,
    note: '包含佣金返利、激活奖励与差额调整',
    icon: 'gift' as const,
    valueClass: 'text-white',
    iconTone: 'text-slate-100'
  },
  {
    label: '佣金返利',
    value: `$${money(summary.value.commission)}`,
    note: '由一级 / 二级链路真实消费聚合计算',
    icon: 'chartBar' as const,
    valueClass: 'text-cyan-300',
    iconTone: 'text-cyan-300'
  },
  {
    label: '激活奖励',
    value: `$${money(summary.value.activation)}`,
    note: '邀请用户激活后生成待结算记录',
    icon: 'bolt' as const,
    valueClass: 'text-emerald-300',
    iconTone: 'text-emerald-300'
  }
])

onMounted(() => {
  void fetchItems()
})

async function fetchItems() {
  loading.value = true
  try {
    const data = await promotionAPI.getEarnings({
      page: pagination.page,
      page_size: pagination.page_size,
      keyword: keywordInput.value.trim() || undefined,
      type: typeFilter.value === 'all' ? undefined : typeFilter.value,
      status: statusFilter.value === 'all' ? undefined : statusFilter.value
    })
    items.value = data.items || []
    pagination.total = data.total || 0
  } catch (error) {
    console.error('Failed to load promotion earnings:', error)
    appStore.showError('加载收益明细失败')
  } finally {
    loading.value = false
  }
}

function reload() {
  pagination.page = 1
  void fetchItems()
}

function handlePageChange(page: number) {
  pagination.page = page
  void fetchItems()
}

function handlePageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  void fetchItems()
}

function typeLabel(type: string) {
  switch (type) {
    case 'activation':
      return '激活奖励'
    case 'adjustment':
      return '差额调整'
    case 'manual':
      return '手工奖励'
    case 'promotion':
      return '活动奖励'
    default:
      return '佣金返利'
  }
}

function typeClass(type: string) {
  switch (type) {
    case 'activation':
      return 'border border-emerald-400/20 bg-emerald-500/10 text-emerald-300'
    case 'adjustment':
      return 'border border-amber-400/20 bg-amber-500/10 text-amber-300'
    case 'manual':
      return 'border border-slate-400/20 bg-slate-500/10 text-slate-300'
    case 'promotion':
      return 'border border-purple-400/20 bg-purple-500/10 text-purple-300'
    default:
      return 'border border-cyan-400/20 bg-cyan-500/10 text-cyan-300'
  }
}

function statusLabel(status: string) {
  switch (status) {
    case 'settled':
      return '已结算'
    case 'cancelled':
      return '已撤销'
    default:
      return '待结算'
  }
}

function statusClass(status: string) {
  switch (status) {
    case 'settled':
      return 'border border-emerald-400/20 bg-emerald-500/10 text-emerald-300'
    case 'cancelled':
      return 'border border-red-400/20 bg-red-500/10 text-red-300'
    default:
      return 'border border-amber-400/20 bg-amber-500/10 text-amber-300'
  }
}

function detailTitle(item: PromotionCommissionItem) {
  if (item.commission_type === 'activation') {
    return `邀请用户 ${item.source_user_masked || '-'} 消费金额满足激活条件，成功激活`
  }
  if (item.commission_type === 'manual') {
    return '后台手工发放奖励'
  }
  if (item.commission_type === 'adjustment') {
    return '后台差额调整'
  }
  if (item.commission_type === 'promotion') {
    return '推广活动奖励'
  }
  return `${item.relation_depth === 2 ? '二级返利' : '一级返利'} · 来源 ${item.source_user_masked || '-'}`
}

function money(value?: number) {
  return Number(value || 0).toFixed(2)
}

function formatBusinessDate(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleDateString()
}

function formatDateTime(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}
</script>
