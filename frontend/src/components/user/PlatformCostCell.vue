<template>
  <div v-if="usage" class="text-sm">
    <div class="flex items-center gap-1.5">
      <span class="text-gray-500 dark:text-gray-400">{{ t('admin.users.today') }}:</span>
      <span class="font-medium text-gray-900 dark:text-white">{{ formatCostAmount(displayTodayCost) }}</span>
    </div>
    <div class="mt-0.5 flex items-center gap-1.5">
      <span class="text-gray-500 dark:text-gray-400">{{ t('admin.users.total') }}:</span>
      <span class="font-medium text-gray-900 dark:text-white">{{ formatCostAmount(displayTotalCost) }}</span>
    </div>
  </div>
  <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlatformUsage } from '@/api/admin/dashboard'
import { formatCostAmount } from '@/utils/format'

const props = defineProps<{
  usage?: PlatformUsage
}>()

const { t } = useI18n()

const displayTodayCost = computed(() =>
  props.usage?.real_today_actual_cost ?? props.usage?.today_actual_cost ?? 0
)
const displayTotalCost = computed(() =>
  props.usage?.real_total_actual_cost ?? props.usage?.total_actual_cost ?? 0
)
</script>
