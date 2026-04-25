<template>
  <div class="space-y-6">
    <div v-if="loading && !overview" class="promo-panel">
      <div class="promo-empty-state">推广数据加载中...</div>
    </div>

    <template v-else-if="overview">
      <section class="promo-panel promo-hero-card">
        <div class="mb-4 flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
          <div class="min-w-0">
            <div class="mb-2 flex flex-wrap items-center gap-3">
              <span class="text-2xl font-bold bg-gradient-to-r from-cyan-300 to-indigo-300 bg-clip-text text-transparent">
                {{ currentLevelDisplay }}
              </span>
              <span class="rounded-full bg-slate-700 px-3 py-1 text-xs text-slate-300">
                当前等级
              </span>
            </div>
            <p class="text-sm text-slate-400">
              <template v-if="overview.next_level_required_activate != null && overview.next_level_no != null">
                再邀请 <span class="font-semibold text-cyan-400">{{ neededToNextLevel }}</span> 人激活可升级至 {{ nextLevelDisplay }}
              </template>
              <template v-else>
                当前已达到最高等级
              </template>
            </p>
          </div>

          <div class="text-left md:text-right">
            <div class="mb-1 text-sm text-slate-400">总收益</div>
            <div class="text-2xl font-bold text-emerald-400">${{ money(overview.total_reward_amount) }}</div>
            <div class="mt-1 text-xs text-slate-500">
              待发放 <span class="text-slate-300">${{ money(overview.pending_amount) }}</span> |
              已发放 <span class="text-emerald-400">${{ money(overview.settled_amount) }}</span>
            </div>
          </div>
        </div>

        <div class="space-y-2">
          <div class="flex justify-between text-xs text-slate-400">
            <span>{{ currentLevelShortLabel }}</span>
            <span>{{ overview.current_direct_activated }}/{{ progressTarget }} 激活</span>
            <span>{{ nextLevelShortLabel }}</span>
          </div>
          <div class="h-2 overflow-hidden rounded-full bg-slate-700">
            <div
              class="h-full rounded-full bg-gradient-to-r from-cyan-400 to-indigo-500"
              :style="{ width: `${progressPercent}%` }"
            ></div>
          </div>
        </div>
      </section>

      <section class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <article
          v-for="card in statCards"
          :key="card.label"
          class="promo-stat-card"
        >
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

      <section class="promo-panel">
        <div class="mb-5 flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <h3 class="promo-section-title">
              <Icon name="badge" size="sm" class="text-cyan-300" />
              等级提成比例
            </h3>
            <p class="promo-section-note">每一级展示一级返利 + 二级返利 = 总提成比例，新增等级后会随后台配置即时变化。</p>
          </div>
          <span class="promo-chip">当前等级总提成 {{ rate(overview.current_total_rate) }}%</span>
        </div>

        <div v-if="!overview.level_rate_summaries?.length" class="promo-empty-state">
          暂未配置等级规则
        </div>
        <div v-else class="relative">
          <div class="promo-scroll-fade-left"></div>
          <div class="promo-scroll-fade-right"></div>
          <div
            ref="levelScrollerRef"
            class="flex gap-4 overflow-x-auto scroll-smooth px-1 pb-2 pt-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
            :style="levelScrollerStyle"
            @wheel.prevent="handleLevelWheel"
          >
            <article
              v-for="item in overview.level_rate_summaries"
              :key="item.level_no"
              :data-level-no="item.level_no"
              class="w-[280px] min-w-[280px] rounded-2xl border p-5 transition-all duration-200 md:w-[320px] md:min-w-[320px]"
              :class="item.level_no === overview.current_level_no
                ? 'border-cyan-400/30 bg-cyan-500/10 shadow-[0_0_24px_-18px_rgba(34,211,238,0.65)]'
                : 'border-white/10 bg-slate-950/40 hover:border-white/20'"
            >
              <div class="flex items-center justify-between gap-3">
                <div>
                  <div class="text-lg font-semibold text-white">Lv{{ item.level_no }} {{ item.level_name }}</div>
                  <div class="mt-1 text-xs text-slate-500">激活 {{ item.required_activated_invites }} 人升级</div>
                </div>
                <span v-if="item.level_no === overview.current_level_no" class="rounded-full border border-cyan-400/25 bg-cyan-500/10 px-3 py-1 text-xs font-medium text-cyan-300">
                  当前
                </span>
              </div>
              <div class="mt-5 space-y-2 text-sm text-slate-300">
                <div class="flex items-center justify-between">
                  <span>一级返利</span>
                  <span class="font-medium text-cyan-300">{{ rate(item.direct_rate) }}%</span>
                </div>
                <div class="flex items-center justify-between">
                  <span>二级返利</span>
                  <span class="font-medium text-indigo-300">{{ rate(item.indirect_rate) }}%</span>
                </div>
                <div class="mt-3 flex items-center justify-between rounded-xl border border-white/10 bg-white/5 px-3 py-2">
                  <span class="text-slate-400">总提成</span>
                  <span class="text-base font-semibold text-emerald-300">{{ rate(item.total_rate) }}%</span>
                </div>
              </div>
            </article>
          </div>
        </div>
      </section>

      <section class="promo-panel">
        <div class="mb-5 flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <h3 class="promo-section-title">
              <Icon name="gift" size="sm" class="text-yellow-400" />
              推广排行 Top 10
            </h3>
            <p class="promo-section-note">仅统计正向自动收益（佣金返利 + 激活奖励），手工奖励不参与排行。</p>
          </div>
          <span class="promo-chip">按累计自动收益排序</span>
        </div>

        <div v-if="!overview.leaderboard?.length" class="promo-empty-state">暂无排行数据</div>
        <div v-else class="space-y-3">
          <article
            v-for="(item, index) in overview.leaderboard"
            :key="item.user_id"
            class="leaderboard-item flex items-center gap-4 rounded-2xl border px-4 py-4 transition-all duration-200"
            :class="leaderboardItemClass(index)"
          >
            <div class="flex h-11 w-11 items-center justify-center rounded-full text-base font-semibold text-white shadow-lg" :class="leaderboardBadgeClass(index)">
              {{ index + 1 }}
            </div>
            <div class="min-w-0 flex-1">
              <div class="truncate text-sm font-medium text-white">{{ item.masked_email }}</div>
              <div class="mt-1 text-xs text-slate-400">
                {{ item.level_name || '未配置等级' }} · 邀请 {{ item.invite_count }} 人
              </div>
            </div>
            <div class="text-right">
              <div class="text-lg font-semibold text-emerald-300">${{ money(item.total_earnings) }}</div>
              <div class="mt-1 text-xs text-slate-500">累计佣金</div>
            </div>
          </article>
        </div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import type { PromotionOverview } from '@/api/promotion'

const props = defineProps<{
  overview: PromotionOverview | null
  loading?: boolean
}>()

const levelScrollerRef = ref<HTMLElement | null>(null)
const levelScrollerPadding = ref(0)

const neededToNextLevel = computed(() => {
  if (!props.overview?.next_level_required_activate) return 0
  return Math.max(props.overview.next_level_required_activate - (props.overview.current_direct_activated || 0), 0)
})

const progressPercent = computed(() => {
  if (!props.overview) return 0
  if (!props.overview.next_level_required_activate) return 100
  const current = props.overview.current_direct_activated || 0
  const target = props.overview.next_level_required_activate || 1
  return Math.max(0, Math.min((current / target) * 100, 100))
})

const progressTarget = computed(() => {
  if (!props.overview) return 0
  return props.overview.next_level_required_activate || props.overview.current_direct_activated || 0
})

const currentLevelDisplay = computed(() => formatLevelDisplay(props.overview?.current_level_no, props.overview?.current_level_name))

const nextLevelDisplay = computed(() => formatLevelDisplay(props.overview?.next_level_no, props.overview?.next_level_name))

const currentLevelShortLabel = computed(() => `Lv${props.overview?.current_level_no || 0}`)

const nextLevelShortLabel = computed(() => {
  if (props.overview?.next_level_no != null) {
    return `Lv${props.overview.next_level_no}`
  }
  return '当前'
})

const activationRate = computed(() => {
  if (!props.overview?.total_invites) return 0
  return (props.overview.activated_invites / props.overview.total_invites) * 100
})

const statCards = computed(() => [
  {
    label: '今日收益',
    value: `$${money(props.overview?.today_earnings)}`,
    note: '今日新增的待发放收益总和',
    icon: 'dollar' as const,
    valueClass: 'text-emerald-300',
    iconTone: 'text-emerald-300'
  },
  {
    label: '邀请总人数',
    value: String(props.overview?.total_invites || 0),
    note: '全部已绑定推广关系的下级用户',
    icon: 'users' as const,
    valueClass: 'text-white',
    iconTone: 'text-slate-200'
  },
  {
    label: '已激活人数',
    value: String(props.overview?.activated_invites || 0),
    note: `激活率 ${activationRate.value.toFixed(1)}%`,
    icon: 'bolt' as const,
    valueClass: 'text-cyan-300',
    iconTone: 'text-cyan-300'
  },
  {
    label: '总返利收益',
    value: `$${money(props.overview?.commission_amount)}`,
    note: '不含激活奖励，只统计佣金返利',
    icon: 'chartBar' as const,
    valueClass: 'text-purple-300',
    iconTone: 'text-purple-300'
  }
])

const levelScrollerStyle = computed(() => ({
  paddingLeft: `${levelScrollerPadding.value}px`,
  paddingRight: `${levelScrollerPadding.value}px`
}))

watch(
  () => [props.overview?.current_level_no, props.overview?.level_rate_summaries?.length],
  async () => {
    await nextTick()
    updateLevelScrollerPadding()
    centerCurrentLevelCard()
  },
  { immediate: true }
)

onMounted(() => {
  updateLevelScrollerPadding()
  centerCurrentLevelCard()
  window.addEventListener('resize', handleScrollerResize)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', handleScrollerResize)
})

function money(value?: number) {
  return Number(value || 0).toFixed(2)
}

function rate(value?: number) {
  const formatted = Number(value || 0).toFixed(4)
  return formatted.replace(/\.?0+$/, '')
}

function leaderboardItemClass(index: number) {
  if (index === 0) {
    return 'border-yellow-400/30 bg-gradient-to-r from-yellow-500/10 to-transparent'
  }
  if (index === 1) {
    return 'border-slate-300/20 bg-gradient-to-r from-slate-400/10 to-transparent'
  }
  if (index === 2) {
    return 'border-orange-500/25 bg-gradient-to-r from-orange-500/10 to-transparent'
  }
  return 'border-white/10 bg-white/5 hover:border-cyan-400/20 hover:bg-cyan-500/5'
}

function leaderboardBadgeClass(index: number) {
  if (index === 0) return 'bg-gradient-to-br from-yellow-400 to-amber-500'
  if (index === 1) return 'bg-gradient-to-br from-slate-300 to-slate-500'
  if (index === 2) return 'bg-gradient-to-br from-orange-500 to-orange-700'
  return 'bg-slate-700/90 text-slate-200'
}

function formatLevelDisplay(levelNo?: number, levelName?: string) {
  const label = String(levelName || '').trim()
  const prefix = levelNo != null ? `Lv${levelNo}` : ''

  if (!label) {
    return prefix || '未配置等级'
  }

  if (!prefix) {
    return label
  }

  if (label === prefix || label.startsWith(`${prefix} `)) {
    return label
  }

  return `${prefix} ${label}`
}

function handleLevelWheel(event: WheelEvent) {
  const scroller = levelScrollerRef.value
  if (!scroller) return

  const delta = Math.abs(event.deltaY) > Math.abs(event.deltaX) ? event.deltaY : event.deltaX
  scroller.scrollBy({
    left: delta,
    behavior: 'smooth'
  })
}

function handleScrollerResize() {
  updateLevelScrollerPadding()
  centerCurrentLevelCard()
}

function updateLevelScrollerPadding() {
  const scroller = levelScrollerRef.value
  if (!scroller) return

  const target = scroller.querySelector<HTMLElement>('[data-level-no]')
  if (!target) return

  levelScrollerPadding.value = Math.max((scroller.clientWidth - target.clientWidth) / 2, 0)
}

function centerCurrentLevelCard() {
  const scroller = levelScrollerRef.value
  const currentLevelNo = props.overview?.current_level_no
  if (!scroller || currentLevelNo == null) return

  const target = scroller.querySelector<HTMLElement>(`[data-level-no="${currentLevelNo}"]`)
  if (!target) return

  const left = target.offsetLeft - (scroller.clientWidth - target.clientWidth) / 2
  scroller.scrollTo({
    left: Math.max(left, 0),
    behavior: 'smooth'
  })
}
</script>
