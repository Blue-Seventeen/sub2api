<template>
  <div class="grid gap-4 xl:grid-cols-[minmax(0,1.44fr)_minmax(340px,0.56fr)] xl:grid-rows-[auto_minmax(0,1fr)] 2xl:grid-cols-[minmax(0,1.5fr)_minmax(360px,0.5fr)]">
    <section class="promo-panel-soft xl:col-start-1 xl:row-start-1">
      <div class="flex flex-col gap-3">
        <div class="flex w-full gap-3">
          <label class="w-full flex-1">
            <span class="sr-only">搜索用户</span>
            <div class="relative">
              <Icon name="search" size="sm" class="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-slate-500" />
              <input
                v-model="keyword"
                type="text"
                class="promo-input-dark pl-11"
                placeholder="搜索用户邮箱 / 用户名..."
                @keyup.enter="reload"
              />
            </div>
          </label>
          <button type="button" class="promo-btn promo-btn-secondary" @click="reload">
            <Icon name="search" size="sm" />
            查询
          </button>
        </div>
      </div>
    </section>

    <section class="promo-table-shell xl:col-start-1 xl:row-start-2 xl:flex xl:h-full xl:min-h-[720px] xl:max-h-[720px] xl:flex-col">
      <div class="overflow-y-auto overflow-x-hidden xl:flex-1">
        <table class="w-full table-fixed text-sm text-slate-200">
          <thead class="promo-table-head">
            <tr>
              <th class="w-[39%] whitespace-nowrap px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">用户信息</th>
              <th class="w-[29%] whitespace-nowrap px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">一级上级</th>
              <th class="w-[14%] whitespace-nowrap px-4 py-3 text-center text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">下级数量</th>
              <th class="w-[18%] whitespace-nowrap px-2.5 py-3 text-center text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading" class="promo-table-row">
              <td colspan="4" class="px-4 py-12 text-center text-slate-400">推广关系加载中...</td>
            </tr>
            <tr v-else-if="!items.length" class="promo-table-row">
              <td colspan="4" class="px-4 py-12 text-center text-slate-400">暂无推广关系数据</td>
            </tr>
            <tr
              v-for="item in items"
              :key="item.user_id"
              class="promo-table-row cursor-pointer last:border-b-0"
              :class="selectedUserId === item.user_id ? 'bg-cyan-500/10' : ''"
              @click="selectUser(item.user_id)"
            >
              <td class="px-4 py-4">
                <div class="flex min-w-0 items-center gap-3">
                  <div class="promo-avatar bg-gradient-to-br from-cyan-400 to-indigo-500">
                    {{ userInitial(item.email) }}
                  </div>
                  <div class="min-w-0 flex-1">
                    <div class="truncate font-medium text-white" :title="item.email">{{ item.email }}</div>
                    <div class="mt-1 truncate text-xs text-slate-500" :title="`ID: ${item.user_id} · ${item.level_name || '未配置等级'} · 码 ${item.invite_code || '--'}`">
                      ID: {{ item.user_id }} · {{ item.level_name || '未配置等级' }} · 码 {{ item.invite_code || '--' }}
                    </div>
                  </div>
                </div>
              </td>
              <td class="px-4 py-4 text-sm text-slate-300">
                <div v-if="item.parent_email" class="flex min-w-0 items-center gap-2">
                  <span class="shrink-0 rounded-full border border-cyan-400/20 bg-cyan-500/10 px-2 py-1 text-xs text-cyan-300">上级</span>
                  <span class="min-w-0 flex-1 truncate whitespace-nowrap" :title="item.parent_email">{{ item.parent_email }}</span>
                </div>
                <span v-else class="inline-block max-w-full truncate whitespace-nowrap text-slate-500">- 无上级 -</span>
              </td>
              <td class="px-4 py-4 text-center whitespace-nowrap">
                <span class="inline-flex max-w-full items-center justify-center whitespace-nowrap rounded-full border border-cyan-400/20 bg-cyan-500/10 px-3 py-1 text-xs font-medium text-cyan-300">
                  {{ item.total_children_count }} 人
                </span>
              </td>
              <td class="px-2.5 py-4 text-center whitespace-nowrap">
                <div class="mx-auto flex w-fit items-center justify-center gap-1 whitespace-nowrap" @click.stop>
                  <button
                    type="button"
                    class="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl border border-white/10 bg-white/5 text-cyan-300 transition hover:border-cyan-400/25 hover:bg-cyan-500/10"
                    title="下级管理"
                    @click="openDownlineManager(item.user_id)"
                  >
                    <Icon name="eye" size="sm" />
                  </button>
                  <button
                    type="button"
                    class="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl border border-white/10 bg-white/5 text-slate-200 transition hover:border-white/20 hover:bg-white/10"
                    title="设置上级"
                    @click="openSetParent(item.user_id)"
                  >
                    <Icon name="link" size="sm" />
                  </button>
                  <button
                    type="button"
                    class="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl border border-red-400/20 bg-red-500/10 text-red-300 transition hover:border-red-400/30 hover:bg-red-500/15"
                    title="移除上级"
                    :disabled="!item.parent_user_id"
                    @click="removeParent(item.user_id)"
                  >
                    <Icon name="x" size="sm" />
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

    <div class="space-y-4 xl:col-start-2 xl:row-start-2 xl:flex xl:h-full xl:min-h-[720px] xl:max-h-[720px] xl:flex-col xl:justify-self-stretch">
      <section class="promo-panel xl:flex xl:flex-1 xl:w-full xl:min-h-0 xl:flex-col xl:overflow-hidden">
        <div class="mb-4 flex items-center gap-3">
          <div>
            <h3 class="promo-section-title">
              <Icon name="link" size="sm" class="text-cyan-300" />
              关系详情
            </h3>
            <p class="promo-section-note">展示顺序：当前查看 → 一级上级 → 二级上级，并展示该链路实际返利比例。</p>
          </div>
        </div>

        <div v-if="chainLoading" class="promo-empty-state">关系详情加载中...</div>
        <div v-else-if="!chain?.current" class="promo-empty-state">点击左侧小眼睛查看推广链路</div>
        <div v-else class="max-h-[600px] space-y-2.5 overflow-y-auto pr-1 xl:max-h-none xl:flex-1 xl:min-h-0">
          <template v-for="(node, index) in chainNodes" :key="node.key">
            <article class="rounded-2xl border p-3.5 min-h-[164px]" :class="node.cardClass">
              <div class="text-xs uppercase tracking-[0.24em]" :class="node.labelClass">{{ node.title }}</div>
              <template v-if="node.data">
                <div class="mt-3 min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <div class="truncate text-base font-medium text-white">{{ node.data.email }}</div>
                    <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs text-slate-200">{{ node.data.level_name || '未配置等级' }}</span>
                  </div>
                </div>
                <div class="mt-3 grid gap-2.5 sm:grid-cols-3">
                  <div class="rounded-xl border border-white/10 bg-white/5 px-3 py-2.5 text-center">
                    <div class="text-[11px] uppercase tracking-[0.2em] text-slate-500">邀请码</div>
                    <div class="mt-2 text-sm font-semibold text-slate-100 break-all">{{ node.data.invite_code || '--' }}</div>
                  </div>
                  <div class="rounded-xl border border-white/10 bg-white/5 px-3 py-2.5 text-center">
                    <div class="text-[11px] uppercase tracking-[0.2em] text-slate-500">已邀人数</div>
                    <div class="mt-2 text-lg font-semibold text-slate-100">{{ node.data.invite_count || 0 }}</div>
                  </div>
                  <div class="rounded-xl border border-white/10 bg-white/5 px-3 py-2.5 text-center">
                    <div class="text-[11px] uppercase tracking-[0.2em] text-slate-500">总提成</div>
                    <div class="mt-2 text-lg font-semibold text-slate-100">{{ rate(node.data.total_rate) }}%</div>
                    <div
                      v-if="node.showActual"
                      class="mt-2 text-xs text-cyan-300"
                      :title="`针对当前查看用户，这一层实际上拿到的返利比例是 ${rate(node.data.actual_rebate_rate)}%`"
                    >
                      对当前用户返利 {{ rate(node.data.actual_rebate_rate) }}%
                    </div>
                  </div>
                </div>
              </template>
              <div v-else class="mt-3 flex min-h-[116px] items-center justify-center rounded-xl border border-dashed border-white/10 bg-white/5 text-sm text-slate-500">暂无节点</div>
            </article>
            <div v-if="index < chainNodes.length - 1" class="flex justify-center">
              <div class="promo-divider-arrow">
                <Icon name="arrowDown" size="sm" />
              </div>
            </div>
          </template>
        </div>
      </section>

      <div class="rounded-xl border border-slate-700 bg-slate-800/30 p-3.5 xl:mt-auto">
        <h3 class="mb-3 flex items-center gap-2 font-semibold">
          <Icon name="infoCircle" size="sm" class="text-blue-400" />
          操作说明
        </h3>
        <ul class="space-y-2 text-sm text-slate-400">
          <li class="flex items-start gap-2">
            <Icon name="eye" size="sm" class="mt-1 text-cyan-400" />
            <span>查看下级：弹出窗口查看该用户的全部下级，并管理直接下级</span>
          </li>
          <li class="flex items-start gap-2">
            <Icon name="x" size="sm" class="mt-1 text-red-400" />
            <span>移除上级：取消当前用户与上级的绑定关系</span>
          </li>
          <li class="flex items-start gap-2">
            <Icon name="userPlus" size="sm" class="mt-1 text-emerald-400" />
            <span>设置上级：为无上级用户指定新的上级代理</span>
          </li>
        </ul>
      </div>
    </div>

    <SetParentDialog
      :show="showSetParentDialog"
      :submitting="bindingSubmitting"
      :initial-user-id="selectedUserId"
      :current-user="selectedRelation"
      @close="showSetParentDialog = false"
      @confirm="handleBindParent"
    />

    <BaseDialog :show="showDownlineDialog" title="下级管理" width="extra-wide" @close="closeDownlineDialog">
      <div class="space-y-5">
        <section class="rounded-2xl border border-white/10 bg-slate-950/40 p-4">
          <div class="text-xs uppercase tracking-[0.24em] text-slate-500">当前用户</div>
          <div class="mt-3 text-lg font-semibold text-white">{{ selectedRelation?.email || chain?.current?.email || '未选择用户' }}</div>
          <div class="mt-2 flex flex-wrap items-center gap-2 text-xs text-slate-400">
            <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1">{{ selectedRelation?.level_name || chain?.current?.level_name || '未配置等级' }}</span>
            <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1">邀请码 {{ selectedRelation?.invite_code || chain?.current?.invite_code || '--' }}</span>
          </div>
        </section>

        <section class="rounded-2xl border border-cyan-400/15 bg-cyan-500/5 p-4">
          <div class="mb-4 flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
            <label class="space-y-2 lg:min-w-[420px] lg:flex-1">
              <span class="text-xs uppercase tracking-[0.24em] text-slate-500">新增下级</span>
              <div class="flex gap-2">
                <input
                  v-model.trim="downlineKeyword"
                  type="text"
                  class="promo-input-dark flex-1"
                  placeholder="搜索用户邮箱或用户名"
                  @keyup.enter="searchDownlineCandidates"
                />
                <button type="button" class="promo-btn promo-btn-secondary" :disabled="downlineSearchLoading" @click="searchDownlineCandidates">
                  <Icon name="search" size="sm" />
                  {{ downlineSearchLoading ? '搜索中...' : '搜索' }}
                </button>
              </div>
              <p class="text-xs text-slate-500">通过邮箱或用户名搜索，将目标用户直接绑定为当前用户的一级下级。</p>
            </label>
          </div>

          <div v-if="downlineSearchLoading" class="promo-empty-state">候选用户搜索中...</div>
          <div v-else-if="downlineKeyword && !candidateRelations.length" class="promo-empty-state">未找到可绑定为下级的用户</div>
          <div v-else-if="candidateRelations.length" class="space-y-3">
            <article
              v-for="candidate in candidateRelations"
              :key="candidate.user_id"
              class="flex flex-col gap-3 rounded-2xl border border-white/10 bg-slate-950/50 p-4 lg:flex-row lg:items-center lg:justify-between"
            >
              <div class="min-w-0">
                <div class="truncate font-medium text-white">{{ candidate.email }}</div>
                <div class="mt-1 text-xs text-slate-500">用户名：{{ candidate.username || '--' }}</div>
                <div class="mt-2 flex flex-wrap items-center gap-2 text-xs text-slate-400">
                  <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1">{{ candidate.level_name || '未配置等级' }}</span>
                  <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1">
                    {{ candidate.parent_email ? `当前上级：${candidate.parent_email}` : '当前无上级' }}
                  </span>
                </div>
              </div>
              <button
                type="button"
                class="promo-btn px-4 py-2"
                :class="candidate.parent_user_id === selectedUserId ? 'promo-btn-secondary' : 'promo-btn-primary'"
                :disabled="downlineSubmitting || candidate.parent_user_id === selectedUserId"
                @click="addDownline(candidate)"
              >
                <Icon name="userPlus" size="sm" />
                {{ candidate.parent_user_id === selectedUserId ? '已是直接下级' : (candidate.parent_user_id ? '改绑为下级' : '新增下级') }}
              </button>
            </article>
          </div>
        </section>

        <section class="rounded-2xl border border-white/10 bg-slate-950/40 p-4">
          <div class="mb-4 flex items-center justify-between gap-3">
            <div>
              <h3 class="promo-section-title">
                <Icon name="users" size="sm" class="text-cyan-300" />
                当前用户下级
              </h3>
              <p class="promo-section-note">这里展示当前用户的全部下级；只有一级下级允许直接移除，避免跨层级误删。</p>
            </div>
            <span class="promo-chip">全部下级</span>
          </div>

          <div v-if="downlineLoading" class="promo-empty-state">下级列表加载中...</div>
          <div v-else-if="!downlines.length" class="promo-empty-state">当前没有下级</div>
          <div v-else class="space-y-3">
            <article
              v-for="item in downlines"
              :key="item.user_id"
              class="rounded-2xl border border-white/10 bg-slate-950/50 p-4"
            >
              <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                <div class="min-w-0">
                  <div class="truncate font-medium text-white">{{ item.email || item.masked_email }}</div>
                  <div class="mt-1 text-xs text-slate-500">用户名：{{ item.username || '--' }}</div>
                  <div class="mt-2 flex flex-wrap items-center gap-2 text-xs text-slate-400">
                    <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1">{{ item.level_name || '未配置等级' }}</span>
                    <span class="rounded-full border border-cyan-400/20 bg-cyan-500/10 px-3 py-1 text-cyan-300">第 {{ item.relation_depth }} 层</span>
                    <span class="rounded-full px-3 py-1" :class="item.activated ? 'border border-emerald-400/20 bg-emerald-500/10 text-emerald-300' : 'border border-white/10 bg-white/5 text-slate-400'">
                      {{ item.activated ? '已激活' : '未激活' }}
                    </span>
                  </div>
                  <div class="mt-3 grid gap-1 text-xs leading-6 text-slate-500 sm:grid-cols-2">
                    <div>今日贡献：${{ money(item.today_contribution) }}</div>
                    <div>累计贡献：${{ money(item.total_contribution) }}</div>
                  </div>
                </div>
                <button
                  type="button"
                  class="promo-btn px-3 py-2 text-xs"
                  :class="item.relation_depth === 1 ? 'promo-btn-danger' : 'promo-btn-secondary'"
                  :disabled="downlineSubmitting || item.relation_depth !== 1"
                  @click="removeDirectDownline(item.user_id)"
                >
                  <Icon name="x" size="sm" />
                  {{ item.relation_depth === 1 ? '移除下级' : '仅一级可移除' }}
                </button>
              </div>
            </article>
          </div>
        </section>
      </div>
      <template #footer>
        <div class="flex justify-end">
          <button type="button" class="promo-btn promo-btn-secondary" @click="closeDownlineDialog">关闭</button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import SetParentDialog from './SetParentDialog.vue'
import { adminPromotionAPI, type PromotionAdminDownlineItem, type PromotionRelationChain, type PromotionRelationItem } from '@/api/admin/promotion'
import { getAdminPromotionPageSize, setAdminPromotionPageSize } from '@/composables/useAdminPromotionPreferences'
import { useAppStore } from '@/stores'

const appStore = useAppStore()
const keyword = ref('')
const loading = ref(false)
const chainLoading = ref(false)
const downlineLoading = ref(false)
const items = ref<PromotionRelationItem[]>([])
const chain = ref<PromotionRelationChain | null>(null)
const downlines = ref<PromotionAdminDownlineItem[]>([])
const selectedUserId = ref<number | null>(null)
const showDownlineDialog = ref(false)
const downlineKeyword = ref('')
const candidateRelations = ref<PromotionRelationItem[]>([])
const downlineSearchLoading = ref(false)
const downlineSubmitting = ref(false)
const showSetParentDialog = ref(false)
const bindingSubmitting = ref(false)

const pagination = reactive({
  page: 1,
  page_size: getAdminPromotionPageSize('relations', 10),
  total: 0
})

const chainNodes = computed(() => [
  {
    key: 'current',
    title: '当前查看',
    data: chain.value?.current,
    showActual: false,
    labelClass: 'text-slate-400',
    cardClass: 'border-white/10 bg-white/5'
  },
  {
    key: 'parent',
    title: '一级上级',
    data: chain.value?.parent,
    showActual: true,
    labelClass: 'text-cyan-300',
    cardClass: 'border-cyan-400/20 bg-cyan-500/10'
  },
  {
    key: 'grandparent',
    title: '二级上级',
    data: chain.value?.grandparent,
    showActual: true,
    labelClass: 'text-purple-300',
    cardClass: 'border-purple-400/20 bg-purple-500/10'
  }
])

const selectedRelation = computed(() => items.value.find(item => item.user_id === selectedUserId.value) || null)

onMounted(() => {
  void fetchRelations()
})

async function fetchRelations() {
  loading.value = true
  try {
    const response = await adminPromotionAPI.getRelations({
      page: pagination.page,
      page_size: pagination.page_size,
      keyword: keyword.value || undefined
    })
    items.value = response.data.items || []
    pagination.total = response.data.total || 0
    if (!items.value.length) {
      selectedUserId.value = null
      chain.value = null
      downlines.value = []
      return
    }
    const selectedStillExists = selectedUserId.value != null && items.value.some(item => item.user_id === selectedUserId.value)
    if (!selectedStillExists && items.value[0]) {
      await selectUser(items.value[0].user_id)
    }
  } catch (error) {
    console.error('Failed to load promotion relations:', error)
    appStore.showError('加载推广关系失败')
  } finally {
    loading.value = false
  }
}

async function selectUser(userID: number) {
  selectedUserId.value = userID
  chainLoading.value = true
  downlineLoading.value = true
  try {
    const [chainResp, downlineResp] = await Promise.all([
      adminPromotionAPI.getRelationChain(userID),
      adminPromotionAPI.getDownlines(userID, {
        page: 1,
        page_size: 100,
        sort_by: 'today_contribution',
        sort_order: 'desc'
      })
    ])
    chain.value = chainResp.data
    downlines.value = downlineResp.data.items || []
  } catch (error) {
    console.error('Failed to load promotion relation detail:', error)
    appStore.showError('加载关系详情失败')
  } finally {
    chainLoading.value = false
    downlineLoading.value = false
  }
}

async function openDownlineManager(userID: number) {
  showDownlineDialog.value = true
  downlineKeyword.value = ''
  candidateRelations.value = []
  await selectUser(userID)
}

function closeDownlineDialog() {
  showDownlineDialog.value = false
  downlineKeyword.value = ''
  candidateRelations.value = []
}

function openSetParent(userID?: number) {
  selectedUserId.value = userID || selectedUserId.value || null
  showSetParentDialog.value = true
}

async function handleBindParent(payload: { user_id: number; parent_user_id: number; note?: string }) {
  bindingSubmitting.value = true
  try {
    await adminPromotionAPI.bindParent(payload)
    appStore.showSuccess('上级设置成功')
    showSetParentDialog.value = false
    await fetchRelations()
    await selectUser(payload.user_id)
  } catch (error) {
    console.error('Failed to bind promotion parent:', error)
    appStore.showError('设置上级失败')
  } finally {
    bindingSubmitting.value = false
  }
}

async function removeParent(userID: number) {
  try {
    await adminPromotionAPI.removeParent(userID)
    appStore.showSuccess('已移除上级关系')
    await fetchRelations()
    await selectUser(userID)
  } catch (error) {
    console.error('Failed to remove promotion parent:', error)
    appStore.showError('移除上级失败')
  }
}

async function removeDirectDownline(downlineUserID: number) {
  if (!selectedUserId.value) return
  downlineSubmitting.value = true
  try {
    await adminPromotionAPI.removeDirectDownline(selectedUserId.value, downlineUserID)
    appStore.showSuccess('已移除直接下级关系')
    await selectUser(selectedUserId.value)
    await fetchRelations()
  } catch (error) {
    console.error('Failed to remove direct promotion downline:', error)
    appStore.showError('移除下级失败')
  } finally {
    downlineSubmitting.value = false
  }
}

async function searchDownlineCandidates() {
  const normalized = downlineKeyword.value.trim()
  if (!normalized) {
    candidateRelations.value = []
    return
  }
  downlineSearchLoading.value = true
  try {
    const response = await adminPromotionAPI.getRelations({
      page: 1,
      page_size: 8,
      keyword: normalized
    })
    candidateRelations.value = (response.data.items || []).filter(item => item.user_id !== selectedUserId.value)
  } catch (error) {
    console.error('Failed to search promotion candidates:', error)
    appStore.showError('搜索候选下级失败')
  } finally {
    downlineSearchLoading.value = false
  }
}

async function addDownline(candidate: PromotionRelationItem) {
  if (!selectedUserId.value) return
  downlineSubmitting.value = true
  try {
    await adminPromotionAPI.bindParent({
      user_id: candidate.user_id,
      parent_user_id: selectedUserId.value
    })
    appStore.showSuccess('已设置为直接下级')
    await fetchRelations()
    await selectUser(selectedUserId.value)
    await searchDownlineCandidates()
  } catch (error) {
    console.error('Failed to add promotion downline:', error)
    appStore.showError('新增下级失败')
  } finally {
    downlineSubmitting.value = false
  }
}

function reload() {
  pagination.page = 1
  void fetchRelations()
}

function handlePageChange(page: number) {
  pagination.page = page
  void fetchRelations()
}

function handlePageSizeChange(size: number) {
  pagination.page_size = size
  setAdminPromotionPageSize('relations', size)
  pagination.page = 1
  void fetchRelations()
}

function money(value?: number) {
  return Number(value || 0).toFixed(2)
}

function rate(value?: number) {
  const formatted = Number(value || 0).toFixed(4)
  return formatted.replace(/\.?0+$/, '')
}

function userInitial(email?: string) {
  return (email || '?').charAt(0).toUpperCase()
}
</script>
