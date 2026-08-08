<template>
  <div class="plaza-pricing-table overflow-x-auto" :style="accentStyle">
    <table class="w-full min-w-[860px] table-fixed border-collapse text-sm tabular-nums">
      <colgroup>
        <col class="w-[22%]" />
        <col class="w-[10%]" />
        <col class="w-[10%]" />
        <col class="w-[14%]" />
        <col class="w-[10%]" />
        <col class="w-[10%]" />
        <col class="w-[14%]" />
        <col class="w-[10%]" />
      </colgroup>
      <thead>
        <tr class="text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">
          <th
            rowspan="2"
            class="border-r border-gray-100 py-2.5 pl-5 pr-4 text-left align-middle dark:border-dark-700/60"
          >
            {{ t('modelPlaza.table.model') }}
          </th>
          <th colspan="3" class="pz-bg pt-2 text-center">
            <div class="pz-title border-b pb-2 font-semibold">
              {{ t('modelPlaza.table.paidPrice') }}
              <span class="pz-unit ml-1 normal-case font-normal">{{ t('modelPlaza.table.unitPerMillion') }}</span>
            </div>
          </th>
          <th colspan="3" class="border-l border-gray-100 pt-2 text-center dark:border-dark-700/60">
            <div class="border-b border-gray-200 pb-2 text-gray-400 dark:border-dark-600 dark:text-dark-500">
              {{ t('modelPlaza.table.officialPrice') }}
              <span class="ml-1 normal-case font-normal text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.unitPerMillion') }}</span>
            </div>
          </th>
          <th
            rowspan="2"
            class="border-l border-gray-100 py-2.5 pl-3 pr-5 text-center align-middle dark:border-dark-700/60"
          >
            {{ t('modelPlaza.table.rate') }}
          </th>
        </tr>
        <tr class="border-b border-gray-200 text-left text-[11px] font-medium uppercase leading-4 tracking-wide text-gray-400 dark:border-dark-700 dark:text-dark-500">
          <th class="pz-bg px-3 py-2 font-medium">{{ t('modelPlaza.table.input') }}</th>
          <th class="pz-bg px-3 py-2 font-medium">{{ t('modelPlaza.table.output') }}</th>
          <th class="pz-bg px-3 py-2 font-medium">{{ t('modelPlaza.table.cache') }}</th>
          <th class="border-l border-gray-100 px-3 py-2 font-medium dark:border-dark-700/60">
            {{ t('modelPlaza.table.input') }}
          </th>
          <th class="px-3 py-2 font-medium">{{ t('modelPlaza.table.output') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('modelPlaza.table.cache') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="m in sortedModels"
          :key="rowKey(m)"
          class="border-b border-gray-100 transition-colors last:border-b-0 hover:bg-gray-50/70 dark:border-dark-800 dark:hover:bg-dark-800/50"
        >
          <td class="border-r border-gray-100 py-2.5 pl-5 pr-4 align-middle dark:border-dark-700/60">
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="font-medium text-gray-900 dark:text-white">{{ m.name }}</span>
              <span
                v-if="platform && m.platform !== platform"
                :class="[
                  'inline-flex items-center rounded-md px-1.5 py-0.5 text-[10px] font-medium',
                  platformBadgeLightClass(m.platform)
                ]"
              >
                {{ platformLabel(m.platform) }}
              </span>
              <span
                v-if="billingMode(m) !== BILLING_MODE_TOKEN"
                class="rounded-md bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium text-gray-500 dark:bg-dark-700/70 dark:text-dark-300"
              >
                {{ billingModeLabel(m) }}
              </span>
            </div>
            <div
              v-if="m.source_group_name"
              class="mt-1 flex flex-wrap items-center gap-1 text-[10px] leading-4 text-gray-400 dark:text-dark-500"
            >
              <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-dark-700/70">{{ m.source_group_name }}</span>
              <span
                v-if="m.source_group_is_exclusive"
                class="rounded bg-purple-50 px-1.5 py-0.5 text-purple-600 dark:bg-purple-900/20 dark:text-purple-300"
              >
                {{ t('modelPlaza.badges.exclusive') }}
              </span>
              <span
                v-if="m.source_group_subscription_type === 'subscription'"
                class="rounded bg-violet-50 px-1.5 py-0.5 text-violet-600 dark:bg-violet-900/20 dark:text-violet-300"
              >
                {{ t('modelPlaza.badges.subscription') }}
              </span>
              <span
                v-if="m.source_group_peak_rate_enabled"
                class="rounded bg-amber-50 px-1.5 py-0.5 text-amber-600 dark:bg-amber-900/20 dark:text-amber-300"
              >
                {{ sourcePeakLabel(m) }}
              </span>
            </div>
          </td>

          <template v-if="billingMode(m) === BILLING_MODE_TOKEN">
            <td class="pz-cell px-3 py-2.5 align-middle font-mono font-semibold text-gray-900 dark:text-gray-50">
              <template v-if="tokenIntervals(m).length">
                <div
                  v-for="(iv, idx) in tokenIntervals(m)"
                  :key="idx"
                  class="whitespace-nowrap text-xs leading-5"
                >
                  <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ tierLabel(iv) }}</span>
                  {{ paidPerMillion(m, iv.input_price) }}
                </div>
              </template>
              <template v-else>{{ paidPerMillion(m, m.pricing?.input_price) }}</template>
            </td>
            <td class="pz-cell px-3 py-2.5 align-middle font-mono font-semibold text-gray-900 dark:text-gray-50">
              <template v-if="tokenIntervals(m).length">
                <div
                  v-for="(iv, idx) in tokenIntervals(m)"
                  :key="idx"
                  class="whitespace-nowrap text-xs leading-5"
                >
                  <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ tierLabel(iv) }}</span>
                  {{ paidPerMillion(m, iv.output_price) }}
                </div>
              </template>
              <template v-else>{{ paidPerMillion(m, m.pricing?.output_price) }}</template>
            </td>
            <td class="pz-cell px-3 py-2.5 align-middle">
              <div
                v-if="hasCachePricing(m)"
                class="space-y-0.5 font-mono text-xs text-gray-800 dark:text-gray-200"
              >
                <div>
                  <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.cacheWrite') }}</span>
                  {{ paidPerMillion(m, m.pricing?.cache_write_price) }}
                </div>
                <div>
                  <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.cacheRead') }}</span>
                  {{ paidPerMillion(m, m.pricing?.cache_read_price) }}
                </div>
              </div>
              <span v-else class="text-gray-400 dark:text-dark-500">-</span>
            </td>
          </template>

          <template v-else>
            <td colspan="3" class="pz-cell px-3 py-2.5 align-middle">
              <div v-if="requestIntervals(m).length" class="flex flex-wrap items-center gap-1.5">
                <span
                  v-for="(iv, idx) in requestIntervals(m)"
                  :key="idx"
                  class="inline-flex items-center gap-1 rounded-md bg-gray-100 px-2 py-0.5 font-mono text-xs text-gray-800 dark:bg-dark-700/60 dark:text-gray-200"
                >
                  <span class="font-sans text-gray-400 dark:text-dark-500">{{ tierLabel(iv) }}</span>
                  {{ paidRequestPrice(m, iv.per_request_price) }}
                  <span class="font-sans text-gray-400 dark:text-dark-500">{{ perUnitSuffix(m) }}</span>
                </span>
              </div>
              <template v-else-if="m.pricing?.per_request_price != null">
                <span class="font-mono font-semibold text-gray-900 dark:text-gray-50">
                  {{ paidRequestPrice(m, m.pricing.per_request_price) }}
                </span>
                <span class="ml-1 text-xs text-gray-400 dark:text-dark-500">{{ perUnitSuffix(m) }}</span>
              </template>
              <span v-else class="text-gray-400 dark:text-dark-500">-</span>
            </td>
          </template>

          <td
            class="border-l border-gray-100 px-3 py-2.5 align-middle font-mono text-xs text-gray-500 dark:border-dark-700/60 dark:text-dark-400"
          >
            <template v-if="officialTokenIntervals(m).length">
              <div
                v-for="(iv, idx) in officialTokenIntervals(m)"
                :key="idx"
                class="whitespace-nowrap leading-5"
              >
                <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ tierLabel(iv) }}</span>
                {{ official(iv.input_price) }}
              </div>
            </template>
            <template v-else>{{ official(m.official_pricing?.input_price) }}</template>
          </td>
          <td class="px-3 py-2.5 align-middle font-mono text-xs text-gray-500 dark:text-dark-400">
            <template v-if="officialTokenIntervals(m).length">
              <div
                v-for="(iv, idx) in officialTokenIntervals(m)"
                :key="idx"
                class="whitespace-nowrap leading-5"
              >
                <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ tierLabel(iv) }}</span>
                {{ official(iv.output_price) }}
              </div>
            </template>
            <template v-else>{{ official(m.official_pricing?.output_price) }}</template>
          </td>
          <td class="px-3 py-2.5 align-middle">
            <div
              v-if="officialTokenIntervals(m).length"
              class="space-y-0.5 font-mono text-xs text-gray-500 dark:text-dark-400"
            >
              <div
                v-for="(iv, idx) in officialTokenIntervals(m)"
                :key="idx"
              >
                <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ tierLabel(iv) }}</span>
                <template v-if="hasIntervalCachePricing(iv)">
                  <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.cacheWrite') }}</span>{{ official(iv.cache_write_price) }}
                  <span class="mx-1 font-sans text-gray-400 dark:text-dark-500">/</span>
                  <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.cacheRead') }}</span>{{ official(iv.cache_read_price) }}
                </template>
                <template v-else>-</template>
              </div>
            </div>
            <div
              v-else-if="m.official_pricing && hasOfficialCache(m.official_pricing)"
              class="space-y-0.5 font-mono text-xs text-gray-500 dark:text-dark-400"
            >
              <div>
                <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.cacheWrite') }}</span>
                {{ official(m.official_pricing.cache_write_price) }}<template v-if="m.official_pricing.cache_write_1h_price != null"><span class="font-sans text-gray-400 dark:text-dark-500"> (1h </span>{{ official(m.official_pricing.cache_write_1h_price) }}<span class="font-sans text-gray-400 dark:text-dark-500">)</span></template>
              </div>
              <div>
                <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.cacheRead') }}</span>
                {{ official(m.official_pricing.cache_read_price) }}
              </div>
            </div>
            <span v-else class="text-gray-400 dark:text-dark-500">-</span>
          </td>

          <td
            class="border-l border-gray-100 py-2.5 pl-3 pr-5 text-center align-middle font-mono text-xs dark:border-dark-700/60"
          >
            <span
              v-if="usesIndependentImageRate(m)"
              class="font-bold text-gray-700 dark:text-gray-300"
            >
              {{ formatRateMultiplier(requestRate(m)) }}x
            </span>
            <template v-else-if="hasCustomRate(m)">
              <span class="mr-1 text-gray-400 line-through dark:text-dark-500">{{ formatRateMultiplier(rowRateMultiplier(m)) }}x</span>
              <span class="font-bold text-primary-600 dark:text-primary-400">{{ formatRateMultiplier(effectiveRate(m)) }}x</span>
            </template>
            <span v-else class="font-bold text-gray-700 dark:text-gray-300">{{ formatRateMultiplier(effectiveRate(m)) }}x</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatScaled } from '@/utils/pricing'
import { formatRateMultiplier } from '@/utils/formatters'
import { platformAccentColor, platformBadgeLightClass, platformLabel } from '@/utils/platformColors'
import { BILLING_MODE_TOKEN, BILLING_MODE_IMAGE, type BillingMode } from '@/constants/channel'
import type { PlazaModel } from '@/api/modelPlaza'
import type { UserPricingInterval } from '@/api/channels'

const props = defineProps<{
  models: PlazaModel[]
  platform?: string
  rateMultiplier: number
  userRateMultiplier?: number | null
  imageRateIndependent?: boolean
  imageRateMultiplier?: number | null
}>()

const { t } = useI18n()
const accentStyle = computed(() => ({ '--plaza-accent': platformAccentColor(props.platform ?? '') }))

const PER_MILLION = 1_000_000
const MIN_DECIMALS = 2

const sortedModels = computed(() => {
  return [...props.models].sort((a, b) => {
    const ta = billingMode(a) === BILLING_MODE_TOKEN
    const tb = billingMode(b) === BILLING_MODE_TOKEN
    if (ta !== tb) return ta ? -1 : 1
    const pa = a.official_pricing?.output_price ?? null
    const pb = b.official_pricing?.output_price ?? null
    if (pa != null && pb != null && pa !== pb) return pb - pa
    if (pa != null && pb == null) return -1
    if (pa == null && pb != null) return 1
    const nameOrder = b.name.localeCompare(a.name)
    if (nameOrder !== 0) return nameOrder
    return b.platform.localeCompare(a.platform)
  })
})

function rowKey(m: PlazaModel): string {
  return `${m.platform}:${m.name}:${m.source_group_id ?? ''}`
}

function billingMode(m: PlazaModel): BillingMode {
  return (m.pricing?.billing_mode || BILLING_MODE_TOKEN) as BillingMode
}

function billingModeLabel(m: PlazaModel): string {
  return billingMode(m) === BILLING_MODE_IMAGE
    ? t('modelPlaza.table.perImage')
    : t('modelPlaza.table.perRequest')
}

function sourcePeakLabel(m: PlazaModel): string {
  const start = (m.source_group_peak_start || '').trim()
  const end = (m.source_group_peak_end || '').trim()
  const multiplier = formatRateMultiplier(m.source_group_peak_rate_multiplier ?? 1)
  const window = start && end ? `${start}-${end} x${multiplier}` : `x${multiplier}`
  return t('common.peakRateTooltip', { window })
}

function rowRateMultiplier(m: PlazaModel): number {
  return m.rate_multiplier ?? props.rateMultiplier
}

function rowUserRateMultiplier(m: PlazaModel): number | null {
  return m.user_rate_multiplier ?? props.userRateMultiplier ?? null
}

function effectiveRate(m: PlazaModel): number {
  return rowUserRateMultiplier(m) ?? rowRateMultiplier(m)
}

function hasCustomRate(m: PlazaModel): boolean {
  const userRate = rowUserRateMultiplier(m)
  return userRate != null && userRate !== rowRateMultiplier(m)
}

function usesIndependentImageRate(m: PlazaModel): boolean {
  return billingMode(m) === BILLING_MODE_IMAGE && (m.image_rate_independent ?? props.imageRateIndependent) === true
}

function requestRate(m: PlazaModel): number {
  return usesIndependentImageRate(m) ? (m.image_rate_multiplier ?? props.imageRateMultiplier ?? 1) : effectiveRate(m)
}

function paidPerMillion(m: PlazaModel, value: number | null | undefined): string {
  if (value == null) return '-'
  return formatScaled(value * effectiveRate(m), PER_MILLION, MIN_DECIMALS)
}

function paidRequestPrice(m: PlazaModel, value: number | null | undefined): string {
  if (value == null) return '-'
  return formatScaled(value * requestRate(m), 1, MIN_DECIMALS)
}

function official(value: number | null | undefined): string {
  if (value == null) return '-'
  return formatScaled(value, PER_MILLION, MIN_DECIMALS)
}

function perUnitSuffix(m: PlazaModel): string {
  return billingMode(m) === BILLING_MODE_IMAGE
    ? t('modelPlaza.table.perUnitImage')
    : t('modelPlaza.table.perUnitRequest')
}

function hasCachePricing(m: PlazaModel): boolean {
  return m.pricing?.cache_write_price != null || m.pricing?.cache_read_price != null
}

function hasOfficialCache(o: NonNullable<PlazaModel['official_pricing']>): boolean {
  return o.cache_write_price != null || o.cache_read_price != null || o.cache_write_1h_price != null
}

function tokenIntervals(m: PlazaModel): UserPricingInterval[] {
  return m.pricing?.intervals ?? []
}

function officialTokenIntervals(m: PlazaModel): UserPricingInterval[] {
  return m.official_pricing?.intervals ?? []
}

function hasIntervalCachePricing(iv: UserPricingInterval): boolean {
  return iv.cache_write_price != null || iv.cache_read_price != null
}

function requestIntervals(m: PlazaModel): UserPricingInterval[] {
  return (m.pricing?.intervals ?? []).filter((iv) => iv.per_request_price != null)
}

function tierLabel(iv: UserPricingInterval): string {
  if (iv.tier_label) return iv.tier_label
  const { min_tokens: min, max_tokens: max } = iv
  if (max == null) return `>${formatTokenCount(min)}`
  if (min === 0) return `≤${formatTokenCount(max)}`
  return `${formatTokenCount(min)}-${formatTokenCount(max)}`
}

function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${trimZero(n / 1_000_000)}M`
  if (n >= 1_000) return `${trimZero(n / 1_000)}K`
  return String(n)
}

function trimZero(n: number): string {
  return String(Math.round(n * 100) / 100)
}
</script>

<style scoped>
.plaza-pricing-table {
  --pz-title: color-mix(in srgb, var(--plaza-accent) 88%, black);
  --pz-bg: color-mix(in srgb, var(--plaza-accent) 7%, transparent);
  --pz-bg-hover: color-mix(in srgb, var(--plaza-accent) 13%, transparent);
}

.dark .plaza-pricing-table {
  --pz-title: color-mix(in srgb, var(--plaza-accent) 70%, white);
  --pz-bg: color-mix(in srgb, var(--plaza-accent) 6%, transparent);
  --pz-bg-hover: color-mix(in srgb, var(--plaza-accent) 10%, transparent);
}

.pz-bg,
.pz-cell {
  background-color: var(--pz-bg);
}

.pz-cell {
  transition: background-color 150ms cubic-bezier(0.4, 0, 0.2, 1);
}

tbody tr:hover .pz-cell {
  background-color: var(--pz-bg-hover);
}

.pz-title {
  color: var(--plaza-accent);
  color: var(--pz-title);
  border-color: color-mix(in srgb, var(--pz-title) 30%, transparent);
}

.pz-unit {
  color: color-mix(in srgb, var(--pz-title) 62%, transparent);
}
</style>
