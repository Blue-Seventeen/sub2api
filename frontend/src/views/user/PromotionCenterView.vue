<template>
  <AppLayout>
    <section class="promo-shell">
      <div class="promo-content space-y-8 px-5 py-6 sm:px-6 lg:px-10 lg:py-8">
        <header class="flex flex-col gap-5 xl:flex-row xl:items-end xl:justify-between">
          <div>
            <p class="promo-chip mb-3 w-fit">
              <Icon name="userGroup" size="sm" class="text-cyan-300" />
              推广收益结算到真实余额
            </p>
            <h1 class="promo-page-title">推广中心</h1>
            <p class="promo-page-desc max-w-2xl">
              邀请好友，按真实消费返佣；激活奖励、一级返利、二级返利都会按业务日聚合，在次日结款时间统一发放。
            </p>
          </div>
          <div class="grid gap-3 sm:grid-cols-2 xl:min-w-[360px]">
            <div class="promo-panel-soft">
              <div class="text-xs uppercase tracking-[0.24em] text-slate-500">今日收益</div>
              <div class="mt-2 text-2xl font-semibold text-emerald-300">
                ${{ money(overview?.today_earnings) }}
              </div>
              <div class="mt-2 text-xs text-slate-400">系统按业务日汇总，不是即时到账。</div>
            </div>
            <div class="promo-panel-soft">
              <div class="text-xs uppercase tracking-[0.24em] text-slate-500">累计收益</div>
              <div class="mt-2 text-2xl font-semibold text-cyan-300">
                ${{ money(overview?.total_reward_amount) }}
              </div>
              <div class="mt-2 text-xs text-slate-400">包含待发放与已发放的累计推广收益。</div>
            </div>
          </div>
        </header>

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

        <PromotionOverviewPanel v-if="currentTab === 'overview'" :overview="overview" :loading="loadingOverview" />
        <PromotionInvitePanel v-else-if="currentTab === 'invite'" :overview="overview" />
        <PromotionTeamPanel v-else-if="currentTab === 'team'" :overview="overview" />
        <PromotionEarningsPanel v-else :overview="overview" />
      </div>
    </section>
  </AppLayout>
</template>

<script setup lang="ts">
import { defineAsyncComponent, onMounted, ref } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { promotionAPI, type PromotionOverview } from '@/api/promotion'
import { useAppStore } from '@/stores'

type TabKey = 'overview' | 'invite' | 'team' | 'earnings'

const appStore = useAppStore()
const currentTab = ref<TabKey>('overview')
const overview = ref<PromotionOverview | null>(null)
const loadingOverview = ref(false)

const PromotionOverviewPanel = defineAsyncComponent(() => import('@/components/promotion/PromotionOverviewPanel.vue'))
const PromotionInvitePanel = defineAsyncComponent(() => import('@/components/promotion/PromotionInvitePanel.vue'))
const PromotionTeamPanel = defineAsyncComponent(() => import('@/components/promotion/PromotionTeamPanel.vue'))
const PromotionEarningsPanel = defineAsyncComponent(() => import('@/components/promotion/PromotionEarningsPanel.vue'))

const tabs: Array<{ key: TabKey; label: string; icon: 'chartBar' | 'userPlus' | 'users' | 'dollar' }> = [
  { key: 'overview', label: '总览', icon: 'chartBar' },
  { key: 'invite', label: '邀请推广', icon: 'userPlus' },
  { key: 'team', label: '我的团队', icon: 'users' },
  { key: 'earnings', label: '收益明细', icon: 'dollar' }
]

onMounted(() => {
  void fetchOverview()
})

async function fetchOverview() {
  loadingOverview.value = true
  try {
    overview.value = await promotionAPI.getOverview()
  } catch (error) {
    console.error('Failed to load promotion overview:', error)
    appStore.showError('加载推广中心失败')
  } finally {
    loadingOverview.value = false
  }
}

function money(value?: number) {
  return Number(value || 0).toFixed(2)
}
</script>
