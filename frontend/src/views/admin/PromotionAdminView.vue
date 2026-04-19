<template>
  <AppLayout>
    <section class="promo-shell">
      <div class="promo-content space-y-8 px-5 py-6 sm:px-6 lg:px-10 lg:py-8">
        <header class="flex flex-col gap-5 xl:flex-row xl:items-end xl:justify-between">
          <div>
            <p class="promo-chip mb-3 w-fit">
              <Icon name="shield" size="sm" class="text-cyan-300" />
              管理推广关系、佣金结算与规则模板
            </p>
            <h1 class="promo-page-title">推广中心后台管理</h1>
            <p class="promo-page-desc max-w-3xl">
              覆盖关系管理、佣金管理、等级与返利配置、推广话术管理；结款时间也在这里统一维护，默认次日 00:00 发放昨日推广收益。
            </p>
          </div>
          <div class="grid gap-3 sm:grid-cols-2 xl:min-w-[420px]">
            <div class="promo-panel-soft">
              <div class="text-xs uppercase tracking-[0.24em] text-slate-500">系统累计已结算</div>
              <div class="mt-2 text-2xl font-semibold text-emerald-300">${{ money(dashboard?.total_settled_amount) }}</div>
              <div class="mt-2 text-xs text-slate-400">已发放到真实余额的推广收益总额。</div>
            </div>
            <div class="promo-panel-soft">
              <div class="text-xs uppercase tracking-[0.24em] text-slate-500">今日待结算</div>
              <div class="mt-2 text-2xl font-semibold text-cyan-300">${{ money(dashboard?.today_pending_amount) }}</div>
              <div class="mt-2 text-xs text-slate-400">将随日结任务进入用户真实余额。</div>
            </div>
          </div>
        </header>

        <section class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <article v-for="card in dashboardCards" :key="card.label" class="promo-stat-card">
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

        <nav class="promo-tabbar flex flex-wrap gap-2">
          <button
            v-for="tab in tabs"
            :key="tab.key"
            type="button"
            class="promo-tab"
            :class="currentTab === tab.key ? 'promo-tab-active' : ''"
            @click="currentTab = tab.key"
          >
            <Icon :name="tab.icon" size="sm" />
            <span>{{ tab.label }}</span>
          </button>
        </nav>

        <PromotionRelationsTab v-if="currentTab === 'relations'" />
        <PromotionCommissionTab v-else-if="currentTab === 'commissions'" @dashboard-refresh="fetchDashboard" />
        <PromotionConfigTab v-else-if="currentTab === 'config'" @dashboard-refresh="fetchDashboard" />
        <PromotionScriptsTab v-else />
      </div>
    </section>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PromotionRelationsTab from '@/components/admin/promotion/PromotionRelationsTab.vue'
import PromotionCommissionTab from '@/components/admin/promotion/PromotionCommissionTab.vue'
import PromotionConfigTab from '@/components/admin/promotion/PromotionConfigTab.vue'
import PromotionScriptsTab from '@/components/admin/promotion/PromotionScriptsTab.vue'
import { adminPromotionAPI, type PromotionAdminDashboard } from '@/api/admin/promotion'
import { useAppStore } from '@/stores'

type TabKey = 'relations' | 'commissions' | 'config' | 'scripts'

const appStore = useAppStore()
const currentTab = ref<TabKey>('relations')
const dashboard = ref<PromotionAdminDashboard | null>(null)

const tabs: Array<{ key: TabKey; label: string; icon: 'users' | 'dollar' | 'cog' | 'chatBubble' }> = [
  { key: 'relations', label: '推广关系管理', icon: 'users' },
  { key: 'commissions', label: '佣金管理', icon: 'dollar' },
  { key: 'config', label: '等级与返利配置', icon: 'cog' },
  { key: 'scripts', label: '推广话术管理', icon: 'chatBubble' }
]

const dashboardCards = computed(() => [
  {
    label: '待结算金额',
    value: `$${money(dashboard.value?.pending_amount)}`,
    note: '尚未执行日结的推广收益总额',
    icon: 'clock' as const,
    valueClass: 'text-amber-300',
    iconTone: 'text-amber-300'
  },
  {
    label: '已绑定用户',
    value: String(dashboard.value?.bound_users || 0),
    note: '已建立推广绑定关系的用户数',
    icon: 'users' as const,
    valueClass: 'text-white',
    iconTone: 'text-slate-100'
  },
  {
    label: '已激活用户',
    value: String(dashboard.value?.activated_users || 0),
    note: '累计真实消费严格大于激活门槛',
    icon: 'bolt' as const,
    valueClass: 'text-cyan-300',
    iconTone: 'text-cyan-300'
  },
  {
    label: '今日新增激活',
    value: String(dashboard.value?.today_new_activates || 0),
    note: '用于衡量等级升级与激活转化',
    icon: 'chartBar' as const,
    valueClass: 'text-purple-300',
    iconTone: 'text-purple-300'
  }
])

onMounted(() => {
  void fetchDashboard()
})

async function fetchDashboard() {
  try {
    const response = await adminPromotionAPI.getDashboard()
    dashboard.value = response.data
  } catch (error) {
    console.error('Failed to load promotion admin dashboard:', error)
    appStore.showError('加载推广后台统计失败')
  }
}

function money(value?: number) {
  return Number(value || 0).toFixed(2)
}
</script>
