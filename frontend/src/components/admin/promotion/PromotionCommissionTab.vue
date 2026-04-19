<template>
  <div class="space-y-4">
    <section class="promo-panel-soft">
      <div class="flex flex-col gap-3 xl:flex-row xl:flex-wrap xl:items-end xl:justify-between">
        <div class="grid flex-1 gap-3 sm:grid-cols-2 xl:grid-cols-5">
          <label class="space-y-2 xl:col-span-1">
            <span class="text-xs uppercase tracking-[0.24em] text-slate-500">用户</span>
            <input v-model="keyword" type="text" class="promo-input-dark" placeholder="用户邮箱 / 用户名 / 邀请码" @keyup.enter="reload" />
          </label>
          <label class="space-y-2">
            <span class="text-xs uppercase tracking-[0.24em] text-slate-500">类型</span>
            <select v-model="typeFilter" class="promo-input-dark" @change="reload">
              <option v-for="option in typeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </label>
          <label class="space-y-2">
            <span class="text-xs uppercase tracking-[0.24em] text-slate-500">状态</span>
            <select v-model="statusFilter" class="promo-input-dark" @change="reload">
              <option v-for="option in statusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </label>
          <label class="space-y-2 xl:col-span-2">
            <span class="text-xs uppercase tracking-[0.24em] text-slate-500">日期范围</span>
            <DateRangePicker v-model:start-date="dateFrom" v-model:end-date="dateTo" @change="reload" />
          </label>
        </div>
        <div class="flex flex-wrap gap-3">
          <button type="button" class="promo-btn promo-btn-secondary" @click="reload">
            <Icon name="search" size="sm" />
            查询
          </button>
          <button type="button" class="promo-btn promo-btn-success" @click="showGrantDialog = true">
            <Icon name="edit" size="sm" />
            差额调整
          </button>
          <button type="button" class="promo-btn promo-btn-primary" :disabled="!selectedIds.length || submitting" @click="batchSettle">
            <Icon name="checkCircle" size="sm" />
            批量结算
          </button>
        </div>
      </div>
    </section>

    <section class="rounded-2xl border border-amber-400/20 bg-amber-500/10 p-4 text-sm leading-7 text-amber-100">
      结算逻辑：系统先按业务日聚合真实消费，生成待结算佣金；次日结算前优先处理上一业务日待结算账单。管理员可对佣金记录执行查看、编辑、撤销；人工发放与扣减统一记为“差额调整”。
    </section>

    <section class="promo-table-shell xl:flex xl:h-full xl:min-h-[640px] xl:max-h-[640px] xl:flex-col">
      <div class="overflow-y-auto overflow-x-hidden xl:flex-1">
        <table class="w-full table-fixed text-sm text-slate-200">
          <thead class="promo-table-head">
            <tr>
              <th class="w-[56px] px-2 py-3 text-center"><input type="checkbox" :checked="allChecked" @change="toggleAll" /></th>
              <th class="w-[18%] whitespace-nowrap px-3 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">时间</th>
              <th class="w-[20%] whitespace-nowrap px-3 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">收益用户</th>
              <th class="w-[10%] whitespace-nowrap px-3 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">类型</th>
              <th class="w-[16%] whitespace-nowrap px-3 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">来源说明</th>
              <th class="w-[7%] whitespace-nowrap px-1.5 py-3 text-right text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">金额</th>
              <th class="w-[9%] whitespace-nowrap px-3 py-3 text-center text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">状态</th>
              <th class="w-[20%] whitespace-nowrap px-3 py-3 text-center text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading" class="promo-table-row">
              <td colspan="8" class="px-4 py-12 text-center text-slate-400">佣金记录加载中...</td>
            </tr>
            <tr v-else-if="!items.length" class="promo-table-row">
              <td colspan="8" class="px-4 py-12 text-center text-slate-400">暂无佣金记录</td>
            </tr>
            <tr v-for="item in items" :key="item.id" class="promo-table-row last:border-b-0">
              <td class="px-3 py-4 text-center align-middle">
                <input type="checkbox" class="relative z-[1]" :checked="selectedIds.includes(item.id)" @change="toggleRow(item.id)" />
              </td>
              <td class="px-4 py-4 text-xs leading-6 text-slate-400">
                <div class="truncate whitespace-nowrap" :title="`创建日：${formatDateTime(item.created_at)}`">创建日：{{ formatDateTime(item.created_at) }}</div>
                <div class="truncate whitespace-nowrap" :title="`结算日：${item.settled_at ? formatDateTime(item.settled_at) : '-'}`">结算日：{{ item.settled_at ? formatDateTime(item.settled_at) : '-' }}</div>
              </td>
              <td class="px-4 py-4">
                <div class="truncate whitespace-nowrap font-medium text-white" :title="item.beneficiary_email">{{ item.beneficiary_email }}</div>
                <div class="mt-1 truncate whitespace-nowrap text-xs text-slate-500" :title="item.level_name || '未配置等级'">{{ item.level_name || '未配置等级' }}</div>
              </td>
              <td class="px-4 py-4">
                <span class="inline-flex max-w-full items-center rounded-full px-3 py-1 text-xs font-medium whitespace-nowrap" :class="typeClass(item.commission_type)">
                  {{ typeLabel(item.commission_type, item.relation_depth) }}
                </span>
              </td>
              <td class="px-4 py-4 text-sm leading-7 text-slate-300">
                <div class="truncate whitespace-nowrap" :title="sourceDescription(item)">{{ sourceDescription(item) }}</div>
              </td>
              <td class="px-1.5 py-4 text-right text-base font-semibold whitespace-nowrap" :class="item.amount >= 0 ? 'text-emerald-300' : 'text-red-300'">
                ${{ money(item.amount) }}
              </td>
              <td class="px-4 py-4 text-center">
                <span class="inline-flex max-w-full items-center rounded-full px-3 py-1 text-xs font-medium whitespace-nowrap" :class="statusClass(item.status)">
                  {{ statusLabel(item.status) }}
                </span>
              </td>
              <td class="px-4 py-4">
                <div class="flex items-center justify-center gap-1">
                  <button type="button" class="promo-btn promo-btn-secondary px-3 py-2 text-xs whitespace-nowrap" @click="openRecordDialog('view', item)">
                    <Icon name="eye" size="sm" />
                    查看
                  </button>
                  <button type="button" class="promo-btn promo-btn-secondary px-3 py-2 text-xs whitespace-nowrap" :disabled="item.status === 'cancelled'" @click="openRecordDialog('edit', item)">
                    <Icon name="edit" size="sm" />
                    编辑
                  </button>
                  <button type="button" class="promo-btn promo-btn-danger px-2.5 py-2 text-xs whitespace-nowrap" @click="cancel(item.id)">
                    <Icon name="refresh" size="sm" />
                    撤回
                  </button>
                </div>
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

    <ManualCommissionDialog
      :show="showGrantDialog"
      :submitting="submitting"
      @close="showGrantDialog = false"
      @confirm="grantCommission"
    />

    <CommissionRecordDialog
      :show="showRecordDialog"
      :submitting="submitting"
      :mode="recordDialogMode"
      :record="activeRecord"
      @close="showRecordDialog = false"
      @confirm="updateCommission"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import ManualCommissionDialog from './ManualCommissionDialog.vue'
import CommissionRecordDialog from './CommissionRecordDialog.vue'
import { adminPromotionAPI } from '@/api/admin/promotion'
import { getAdminPromotionPageSize, setAdminPromotionPageSize } from '@/composables/useAdminPromotionPreferences'
import type { PromotionCommissionItem } from '@/api/promotion'
import { useAppStore } from '@/stores'

const emit = defineEmits<{
  (e: 'dashboard-refresh'): void
}>()

const appStore = useAppStore()
const loading = ref(false)
const submitting = ref(false)
const items = ref<PromotionCommissionItem[]>([])
const selectedIds = ref<number[]>([])
const keyword = ref('')
const typeFilter = ref('all')
const statusFilter = ref('all')
const dateFrom = ref('')
const dateTo = ref('')
const showGrantDialog = ref(false)
const showRecordDialog = ref(false)
const recordDialogMode = ref<'view' | 'edit'>('view')
const activeRecord = ref<PromotionCommissionItem | null>(null)
const pagination = reactive({
  page: 1,
  page_size: getAdminPromotionPageSize('commissions', 20),
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

const allChecked = computed(() => !!items.value.length && items.value.every(item => selectedIds.value.includes(item.id)))

onMounted(() => {
  void fetchItems()
})

async function fetchItems() {
  loading.value = true
  try {
    const response = await adminPromotionAPI.getCommissions({
      page: pagination.page,
      page_size: pagination.page_size,
      keyword: keyword.value || undefined,
      type: typeFilter.value === 'all' ? undefined : typeFilter.value,
      status: statusFilter.value === 'all' ? undefined : statusFilter.value,
      date_from: dateFrom.value || undefined,
      date_to: dateTo.value || undefined
    })
    items.value = response.data.items || []
    pagination.total = response.data.total || 0
    selectedIds.value = selectedIds.value.filter(id => items.value.some(item => item.id === id))
  } catch (error) {
    console.error('Failed to load promotion commissions:', error)
    appStore.showError('加载佣金记录失败')
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
  setAdminPromotionPageSize('commissions', size)
  pagination.page = 1
  void fetchItems()
}

function toggleRow(id: number) {
  selectedIds.value = selectedIds.value.includes(id)
    ? selectedIds.value.filter(item => item !== id)
    : [...selectedIds.value, id]
}

function toggleAll() {
  selectedIds.value = allChecked.value ? [] : items.value.map(item => item.id)
}

function openRecordDialog(mode: 'view' | 'edit', item: PromotionCommissionItem) {
  recordDialogMode.value = mode
  activeRecord.value = { ...item }
  showRecordDialog.value = true
}

async function batchSettle() {
  if (!selectedIds.value.length) return
  submitting.value = true
  try {
    await adminPromotionAPI.batchSettle(selectedIds.value)
    appStore.showSuccess('批量结算完成')
    selectedIds.value = []
    await fetchItems()
    emit('dashboard-refresh')
  } catch (error) {
    console.error('Failed to batch settle promotion commissions:', error)
    appStore.showError('批量结算失败')
  } finally {
    submitting.value = false
  }
}

async function cancel(id: number) {
  submitting.value = true
  try {
    await adminPromotionAPI.cancelCommission(id)
    appStore.showSuccess('佣金记录已撤销')
    await fetchItems()
    emit('dashboard-refresh')
  } catch (error) {
    console.error('Failed to cancel promotion commission:', error)
    appStore.showError('撤销佣金失败')
  } finally {
    submitting.value = false
  }
}

async function grantCommission(payload: { user_id: number; amount: number; note?: string }) {
  submitting.value = true
  try {
    await adminPromotionAPI.manualGrant(payload)
    appStore.showSuccess('差额调整已发放到用户真实余额')
    showGrantDialog.value = false
    await fetchItems()
    emit('dashboard-refresh')
  } catch (error) {
    console.error('Failed to grant promotion commission:', error)
    appStore.showError('差额调整失败')
  } finally {
    submitting.value = false
  }
}

async function updateCommission(payload: { id: number; amount: number; note?: string }) {
  submitting.value = true
  try {
    await adminPromotionAPI.updateCommission(payload.id, {
      amount: payload.amount,
      note: payload.note
    })
    appStore.showSuccess('佣金记录已更新')
    showRecordDialog.value = false
    await fetchItems()
    emit('dashboard-refresh')
  } catch (error) {
    console.error('Failed to update promotion commission:', error)
    appStore.showError('更新佣金记录失败')
  } finally {
    submitting.value = false
  }
}

function typeLabel(type: string, depth: number) {
  switch (type) {
    case 'activation':
      return '激活奖励'
    case 'adjustment':
      return '差额调整'
    case 'promotion':
      return '活动奖励'
    default:
      return depth === 2 ? '二级返利' : '佣金返利'
  }
}

function typeClass(type: string) {
  switch (type) {
    case 'activation':
      return 'border border-emerald-400/20 bg-emerald-500/10 text-emerald-300'
    case 'adjustment':
      return 'border border-purple-400/20 bg-purple-500/10 text-purple-300'
    case 'promotion':
      return 'border border-pink-400/20 bg-pink-500/10 text-pink-300'
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

function sourceDescription(item: PromotionCommissionItem) {
  if (item.commission_type === 'adjustment') {
    return item.note || '管理员执行差额调整'
  }
  if (item.commission_type === 'activation') {
    return item.note || `邀请用户 ${item.source_user_masked || '-'} 消费金额满足激活条件，成功激活`
  }
  if (item.commission_type === 'commission') {
    const rebate = item.rate_snapshot != null ? `${Number(item.rate_snapshot).toFixed(4).replace(/\.?0+$/, '')}%` : '--'
    return item.note || `邀请用户 ${item.source_user_masked || '-'} 消费返利（真实消费 $${money(item.base_amount)} × 返利比例 ${rebate}）`
  }
  return item.note || '系统生成的佣金记录'
}

function money(value?: number) {
  return Number(value || 0).toFixed(2)
}

function formatDateTime(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

</script>
