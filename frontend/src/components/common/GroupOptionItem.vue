<template>
  <div :class="optionRootClass">
    <!-- Left: name + description -->
    <div
      class="flex min-w-0 flex-1 flex-col items-start"
      :title="description || undefined"
    >
      <!-- Row 1: platform badge (name bold) -->
      <GroupBadge
        :name="name"
        :platform="platform"
        :subscription-type="subscriptionType"
        :show-rate="false"
        class="groupOptionItemBadge"
      />
      <!-- Row 2: description with top spacing -->
      <span
        v-if="description"
        class="mt-1.5 w-full whitespace-pre-line [overflow-wrap:anywhere] text-left text-xs leading-relaxed text-gray-500 dark:text-gray-400 line-clamp-3"
      >
        {{ description }}
      </span>
    </div>

    <!-- Right: rate pill + checkmark (vertically centered to first row) -->
    <div class="flex shrink-0 items-center gap-2 pt-0.5">
      <div :class="ratePillGroupClass">
        <!-- Rate pill (platform color) -->
        <span v-if="rateMultiplier !== undefined" :class="['inline-flex items-center whitespace-nowrap rounded-full px-3 py-1 text-xs font-semibold', ratePillClass]">
          <template v-if="hasEffectiveRate">
            <span class="mr-1 line-through opacity-50">{{ formattedRateMultiplier }}x</span>
            <span class="font-bold">{{ formattedDisplayRateMultiplier }}x</span>
          </template>
          <template v-else>
            {{ formattedRateMultiplier }}x {{ t('admin.groups.rateLabel') }}
          </template>
        </span>
        <PeakRatePill
          v-if="hasPeakRate"
          :peak-rate-enabled="props.peakRateEnabled"
          :peak-start="props.peakStart"
          :peak-end="props.peakEnd"
          :peak-rate-multiplier="props.peakRateMultiplier"
          :peak-rate-windows="props.peakRateWindows"
          :display-mode="props.peakDisplayMode"
          :base-multiplier="peakBaseMultiplier"
          :pill-class="peakRatePillClass"
          include-billing-note
          data-test="group-option-peak-rate"
        />
      </div>
      <!-- Checkmark -->
      <svg
        v-if="showCheckmark && selected"
        class="h-4 w-4 shrink-0 text-primary-600 dark:text-primary-400"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
        stroke-width="2"
      >
        <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
      </svg>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from './GroupBadge.vue'
import PeakRatePill from './PeakRatePill.vue'
import type { SubscriptionType, GroupPlatform } from '@/types'
import { platformBadgeLightClass } from '@/utils/platformColors'
import {
  hasPeakRate as hasPeakRateFields,
  type PeakRateDisplayMode,
  type PeakRateWindow,
} from '@/utils/peak-rate'
import { formatRateMultiplier } from '@/utils/formatters'

const { t } = useI18n()

interface Props {
  name: string
  platform: GroupPlatform
  subscriptionType?: SubscriptionType
  rateMultiplier?: number
  effectiveRateMultiplier?: number | null
  userRateMultiplier?: number | null
  peakRateEnabled?: boolean
  peakStart?: string
  peakEnd?: string
  peakRateMultiplier?: number
  peakRateWindows?: PeakRateWindow[]
  peakDisplayMode?: PeakRateDisplayMode
  description?: string | null
  selected?: boolean
  showCheckmark?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  subscriptionType: 'standard',
  selected: false,
  showCheckmark: true,
  effectiveRateMultiplier: null,
  userRateMultiplier: null,
  peakRateEnabled: false,
  peakDisplayMode: 'compact'
})

// Whether effective/final rate differs from the default group rate.
const displayRateMultiplier = computed(() => props.effectiveRateMultiplier ?? props.userRateMultiplier)
const formattedRateMultiplier = computed(() =>
  props.rateMultiplier !== undefined ? formatRateMultiplier(props.rateMultiplier) : ''
)
const formattedDisplayRateMultiplier = computed(() =>
  displayRateMultiplier.value !== null && displayRateMultiplier.value !== undefined
    ? formatRateMultiplier(displayRateMultiplier.value)
    : ''
)

const hasEffectiveRate = computed(() => {
  return (
    displayRateMultiplier.value !== null &&
    displayRateMultiplier.value !== undefined &&
    props.rateMultiplier !== undefined &&
    displayRateMultiplier.value !== props.rateMultiplier
  )
})

const peakRateFields = computed(() => ({
  peak_rate_enabled: props.peakRateEnabled,
  peak_start: props.peakStart,
  peak_end: props.peakEnd,
  peak_rate_multiplier: props.peakRateMultiplier,
  peak_rate_windows: props.peakRateWindows
}))

const hasPeakRate = computed(() => {
  return hasPeakRateFields(peakRateFields.value)
})

const peakBaseMultiplier = computed(() => displayRateMultiplier.value ?? props.rateMultiplier ?? 1)

const peakRatePillClass = computed(() => {
  const base = 'inline-flex items-center rounded-full bg-amber-50 px-3 py-1 text-xs font-semibold text-amber-700 dark:bg-amber-900/20 dark:text-amber-300'
  if (props.peakDisplayMode === 'full') {
    return `${base} max-w-64 whitespace-normal break-words text-right leading-4`
  }
  return `${base} whitespace-nowrap`
})

const ratePillGroupClass = computed(() => {
  const base = 'flex shrink-0'
  if (props.peakDisplayMode === 'full') {
    return `${base} flex-col items-end gap-1`
  }
  return `${base} flex-row flex-wrap items-center justify-end gap-2`
})

const optionRootClass = computed(() => {
  const base = 'flex min-w-0 flex-1 gap-3'
  if (props.peakDisplayMode === 'full') {
    return `${base} flex-col items-stretch sm:flex-row sm:items-start sm:justify-between`
  }
  return `${base} items-start justify-between`
})

// Rate pill color matches platform badge color
const ratePillClass = computed(() => platformBadgeLightClass(props.platform))
</script>

<style scoped>
/* Bold the group name inside GroupBadge when used in dropdown option */
.groupOptionItemBadge :deep(span.truncate) {
  font-weight: 600;
}
</style>
