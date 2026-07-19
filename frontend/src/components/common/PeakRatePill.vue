<template>
  <span v-if="hasPeakRate" class="inline-flex">
    <span
      :class="pillClass"
      role="button"
      tabindex="0"
      :aria-label="accessibleLabel"
      :data-test="dataTest"
      @mouseenter="showTooltip"
      @focus="showTooltip"
      @mouseleave="hideTooltip"
      @blur="hideTooltip"
      @keydown.escape="hideTooltip"
    >
      {{ displayText }}
    </span>
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-150 ease-out"
        enter-from-class="opacity-0 scale-95"
        enter-to-class="opacity-100 scale-100"
        leave-active-class="transition duration-100 ease-in"
        leave-from-class="opacity-100 scale-100"
        leave-to-class="opacity-0 scale-95"
      >
        <div
          v-if="tooltipState"
          class="pointer-events-none fixed z-[100000050] rounded-xl border border-amber-200 bg-white px-3 py-2.5 text-left shadow-[0_24px_70px_-28px_rgba(120,53,15,0.55)] ring-1 ring-amber-100 dark:border-amber-800/70 dark:bg-dark-800 dark:ring-amber-900/40"
          :class="tooltipState.placement === 'top' ? '-translate-y-full' : ''"
          :style="tooltipState.style"
          data-test="peak-rate-tooltip"
        >
          <div class="text-[11px] font-semibold text-amber-700 dark:text-amber-300">
            {{ tooltipHeader }}
          </div>
          <div class="mt-1.5 space-y-1">
            <div
              v-for="line in windowLines"
              :key="line"
              class="rounded-md bg-amber-50 px-2 py-1 font-mono text-[11px] leading-4 text-amber-900 break-words dark:bg-amber-950/40 dark:text-amber-100"
              data-test="peak-rate-window"
            >
              {{ line }}
            </div>
          </div>
          <div
            v-if="includeBillingNote"
            class="mt-1.5 text-[11px] leading-4 text-gray-500 break-words dark:text-gray-400"
          >
            {{ billingNote }}
          </div>
          <div
            class="absolute h-3 w-3 rotate-45 border-amber-200 bg-white dark:border-amber-800/70 dark:bg-dark-800"
            :class="
              tooltipState.placement === 'top'
                ? 'top-full -translate-x-1/2 -translate-y-1/2 border-b border-r'
                : 'bottom-full -translate-x-1/2 translate-y-1/2 border-l border-t'
            "
            :style="tooltipState.arrowStyle"
          ></div>
        </div>
      </Transition>
    </Teleport>
  </span>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import {
  formatPeakRateWindow,
  hasPeakRate as hasPeakRateFields,
  peakRateWindowsForDisplay,
  serverTimezoneLabel,
  type PeakRateDisplayMode,
  type PeakRateWindow,
} from '@/utils/peak-rate'
import { formatRateMultiplier } from '@/utils/formatters'

interface Props {
  peakRateEnabled?: boolean
  peakStart?: string
  peakEnd?: string
  peakRateMultiplier?: number
  peakRateWindows?: PeakRateWindow[]
  displayMode?: PeakRateDisplayMode
  baseMultiplier?: number
  includeBillingNote?: boolean
  pillClass?: string
  dataTest?: string
}

interface TooltipState {
  placement: 'top' | 'bottom'
  style: Record<string, string>
  arrowStyle: Record<string, string>
}

const props = withDefaults(defineProps<Props>(), {
  peakRateEnabled: false,
  displayMode: 'compact',
  baseMultiplier: 1,
  includeBillingNote: false,
  pillClass: '',
  dataTest: 'peak-rate-pill',
})

const { t } = useI18n()
const appStore = useAppStore()

const TOOLTIP_MARGIN = 12
const TOOLTIP_MAX_WIDTH = 560
const TOOLTIP_MIN_WIDTH = 320

const tooltipState = ref<TooltipState | null>(null)

const fields = computed(() => ({
  peak_rate_enabled: props.peakRateEnabled,
  peak_start: props.peakStart,
  peak_end: props.peakEnd,
  peak_rate_multiplier: props.peakRateMultiplier,
  peak_rate_windows: props.peakRateWindows,
}))

const hasPeakRate = computed(() => hasPeakRateFields(fields.value))
const windows = computed(() => peakRateWindowsForDisplay(fields.value))
const timezoneLabel = computed(() => serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))

const fullText = computed(() => formatPeakRateWindow(fields.value, timezoneLabel.value))

const displayText = computed(() => {
  if (props.displayMode === 'full') return fullText.value
  if (windows.value.length === 1) {
    return t('common.peakRateCompactSingle', { multiplier: formatRateMultiplier(windows.value[0].multiplier ?? 1) })
  }
  return t('common.peakRateCompactMultiple', { count: windows.value.length })
})

const formatPeakRateLine = (window: PeakRateWindow) => {
  const base = props.baseMultiplier ?? 1
  const peak = window.multiplier ?? 1
  return t('common.peakRateFormula', {
    window: `${window.start}-${window.end}`,
    base: formatRateMultiplier(base),
    peak: formatRateMultiplier(peak),
    final: formatRateMultiplier(base * peak),
  })
}

const windowLines = computed(() =>
  windows.value.map(formatPeakRateLine),
)

const tooltipHeader = computed(() => {
  const base = t('common.peakRateTooltip', { window: '' }).trim()
  return timezoneLabel.value ? `${base} (${timezoneLabel.value})` : base
})

const billingNote = computed(() => t('common.peakRateImageNote').replace(/^[；;]\s*/, ''))

const accessibleLabel = computed(() => {
  const note = props.includeBillingNote ? ` ${billingNote.value}` : ''
  return `${tooltipHeader.value} ${windowLines.value.join('; ')}${note}`
})

const tooltipWidth = () => {
  if (typeof window === 'undefined') return TOOLTIP_MAX_WIDTH
  const viewportSafeWidth = window.innerWidth - TOOLTIP_MARGIN * 2
  return Math.max(
    Math.min(TOOLTIP_MIN_WIDTH, viewportSafeWidth),
    Math.min(TOOLTIP_MAX_WIDTH, viewportSafeWidth),
  )
}

const showTooltip = (event: MouseEvent | FocusEvent) => {
  if (typeof window === 'undefined') return

  const target = (event.currentTarget || event.target) as HTMLElement | null
  if (!target) return

  const rect = target.getBoundingClientRect()
  const width = tooltipWidth()
  const anchorCenter = rect.left + rect.width / 2
  const left = Math.min(
    Math.max(TOOLTIP_MARGIN, anchorCenter - width / 2),
    Math.max(TOOLTIP_MARGIN, window.innerWidth - width - TOOLTIP_MARGIN),
  )
  const estimatedHeight = Math.min(
    Math.max(128, 94 + windowLines.value.length * 30 + (props.includeBillingNote ? 24 : 0)),
    Math.max(128, window.innerHeight - TOOLTIP_MARGIN * 2),
  )
  const placement: TooltipState['placement'] =
    rect.top > estimatedHeight + TOOLTIP_MARGIN ? 'top' : 'bottom'
  const top =
    placement === 'top'
      ? Math.max(TOOLTIP_MARGIN, rect.top - 10)
      : Math.min(window.innerHeight - TOOLTIP_MARGIN, rect.bottom + 10)
  const arrowLeft = Math.min(Math.max(16, anchorCenter - left), width - 16)

  tooltipState.value = {
    placement,
    style: {
      left: `${left}px`,
      top: `${top}px`,
      width: `${width}px`,
      maxHeight: `${Math.max(128, window.innerHeight - TOOLTIP_MARGIN * 2)}px`,
      overflowY: 'auto',
    },
    arrowStyle: {
      left: `${arrowLeft}px`,
    },
  }
}

const hideTooltip = () => {
  tooltipState.value = null
}

if (typeof window !== 'undefined') {
  window.addEventListener('resize', hideTooltip)
  window.addEventListener('scroll', hideTooltip, true)
}

onUnmounted(() => {
  if (typeof window === 'undefined') return
  window.removeEventListener('resize', hideTooltip)
  window.removeEventListener('scroll', hideTooltip, true)
})
</script>
