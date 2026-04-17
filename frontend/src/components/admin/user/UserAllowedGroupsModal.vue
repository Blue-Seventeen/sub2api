<template>
  <BaseDialog :show="show" :title="t('admin.users.groupConfig')" width="wide" @close="$emit('close')">
    <div v-if="user" class="space-y-6">
      <div class="flex items-center gap-4 rounded-2xl bg-gradient-to-r from-primary-50 to-primary-100 p-5 dark:from-primary-900/30 dark:to-primary-800/20">
        <div class="flex h-14 w-14 items-center justify-center rounded-full bg-white shadow-sm dark:bg-dark-700">
          <span class="text-2xl font-semibold text-primary-600 dark:text-primary-400">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div class="flex-1">
          <p class="text-lg font-semibold text-gray-900 dark:text-white">{{ user.email }}</p>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ t('admin.users.groupConfigHint', { email: user.email }) }}</p>
        </div>
      </div>

      <div v-if="loading" class="flex justify-center py-12">
        <svg class="h-10 w-10 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
      </div>

      <div v-else class="space-y-6">
        <div class="rounded-2xl border border-primary-200/70 bg-primary-50/60 p-5 dark:border-primary-800/50 dark:bg-primary-900/10">
          <div class="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
            <div>
              <h4 class="text-sm font-semibold text-gray-900 dark:text-white">专属统一倍率</h4>
              <p class="mt-1 text-xs text-gray-600 dark:text-gray-400">
                开启后：最终倍率 = 基础倍率 × 统一倍率；显示余额 = 真实余额 × 统一倍率。
              </p>
            </div>
            <label class="inline-flex cursor-pointer items-center gap-3">
              <span class="text-sm text-gray-700 dark:text-gray-300">启用</span>
              <button
                type="button"
                class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors"
                :class="unifiedRateEnabled ? 'bg-primary-600' : 'bg-gray-300 dark:bg-dark-500'"
                @click="unifiedRateEnabled = !unifiedRateEnabled"
              >
                <span
                  class="inline-block h-5 w-5 transform rounded-full bg-white transition-transform"
                  :class="unifiedRateEnabled ? 'translate-x-5' : 'translate-x-1'"
                />
              </button>
            </label>
          </div>

          <div class="mt-4 grid gap-4 md:grid-cols-3">
            <div>
              <label class="mb-1 block text-xs font-medium text-gray-500 dark:text-gray-400">统一倍率</label>
              <input
                v-model.number="unifiedRateMultiplier"
                type="number"
                step="0.001"
                min="0"
                class="hide-spinner w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium transition-colors focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/20 dark:border-dark-500 dark:bg-dark-700 dark:text-white"
              />
            </div>
            <div class="rounded-xl border border-white/60 bg-white/70 px-4 py-3 dark:border-dark-600 dark:bg-dark-800/60">
              <div class="text-xs text-gray-500 dark:text-gray-400">生效倍率</div>
              <div class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ effectiveUnifiedMultiplier.toFixed(3) }}x</div>
            </div>
            <div class="rounded-xl border border-white/60 bg-white/70 px-4 py-3 dark:border-dark-600 dark:bg-dark-800/60">
              <div class="text-xs text-gray-500 dark:text-gray-400">说明</div>
              <div class="mt-1 text-sm text-gray-700 dark:text-gray-300">
                {{ unifiedRateEnabled ? '当前用户将按统一倍率放大显示余额与最终倍率' : '关闭后统一按 1x 处理' }}
              </div>
            </div>
          </div>
        </div>

        <div v-if="exclusiveGroups.length > 0">
          <div class="mb-3 flex items-center gap-2">
            <div class="h-1.5 w-1.5 rounded-full bg-purple-500"></div>
            <h4 class="text-sm font-semibold text-gray-700 dark:text-gray-300">{{ t('admin.users.exclusiveGroups') }}</h4>
            <span class="text-xs text-gray-400">({{ exclusiveGroupConfigs.filter(c => c.isSelected).length }}/{{ exclusiveGroupConfigs.length }})</span>
          </div>
          <div class="grid gap-3">
            <div
              v-for="config in exclusiveGroupConfigs"
              :key="config.groupId"
              class="group relative overflow-hidden rounded-xl border-2 p-4 transition-all duration-200"
              :class="config.isSelected
                ? 'border-primary-400 bg-primary-50/50 shadow-sm dark:border-primary-500 dark:bg-primary-900/20'
                : 'border-gray-200 bg-white hover:border-gray-300 dark:border-dark-600 dark:bg-dark-800 dark:hover:border-dark-500'"
            >
              <div class="flex items-center gap-4">
                <div class="flex-shrink-0">
                  <label class="relative flex h-6 w-6 cursor-pointer items-center justify-center">
                    <input
                      type="checkbox"
                      :checked="config.isSelected"
                      @change="toggleExclusiveGroup(config.groupId)"
                      class="peer sr-only"
                    />
                    <div class="h-5 w-5 rounded-md border-2 border-gray-300 transition-all peer-checked:border-primary-500 peer-checked:bg-primary-500 dark:border-dark-500 peer-checked:dark:border-primary-500">
                      <svg v-if="config.isSelected" class="h-full w-full text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
                      </svg>
                    </div>
                  </label>
                </div>

                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-2">
                    <span class="text-base font-semibold text-gray-900 dark:text-white">{{ config.groupName }}</span>
                    <span class="inline-flex items-center rounded-full bg-purple-100 px-2 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/40 dark:text-purple-300">
                      {{ t('admin.groups.exclusive') }}
                    </span>
                  </div>
                  <div class="mt-1.5 flex flex-wrap items-center gap-3 text-sm">
                    <span class="inline-flex items-center gap-1 text-gray-500 dark:text-gray-400">
                      <PlatformIcon :platform="config.platform" size="xs" />
                      <span>{{ config.platform }}</span>
                    </span>
                    <span class="text-gray-300 dark:text-dark-500">•</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      {{ t('admin.users.defaultRate') }}: <span class="font-medium text-gray-700 dark:text-gray-300">{{ config.defaultRate }}x</span>
                    </span>
                    <span class="text-gray-300 dark:text-dark-500">•</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      基础倍率: <span class="font-medium text-gray-700 dark:text-gray-300">{{ getBaseRate(config).toFixed(3) }}x</span>
                    </span>
                    <span class="text-gray-300 dark:text-dark-500">•</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      最终倍率: <span class="font-medium text-primary-700 dark:text-primary-300">{{ getFinalRate(config).toFixed(3) }}x</span>
                    </span>
                  </div>
                </div>

                <div class="flex flex-shrink-0 items-center gap-3">
                  <label class="text-sm font-medium text-gray-600 dark:text-gray-400">{{ t('admin.users.customRate') }}</label>
                  <input
                    type="number"
                    step="0.001"
                    min="0"
                    :value="config.customRate ?? ''"
                    @input="updateCustomRate(config.groupId, ($event.target as HTMLInputElement).value)"
                    :placeholder="String(config.defaultRate)"
                    class="hide-spinner w-24 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium transition-colors focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/20 dark:border-dark-500 dark:bg-dark-700 dark:focus:border-primary-500"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>

        <div v-if="publicGroups.length > 0">
          <div class="mb-3 flex items-center gap-2">
            <div class="h-1.5 w-1.5 rounded-full bg-green-500"></div>
            <h4 class="text-sm font-semibold text-gray-700 dark:text-gray-300">{{ t('admin.users.publicGroups') }}</h4>
            <span class="text-xs text-gray-400">({{ publicGroupConfigs.length }})</span>
          </div>
          <div class="grid gap-3">
            <div
              v-for="config in publicGroupConfigs"
              :key="config.groupId"
              class="relative overflow-hidden rounded-xl border-2 border-green-200 bg-green-50/50 p-4 dark:border-green-800/50 dark:bg-green-900/10"
            >
              <div class="flex items-center gap-4">
                <div class="flex-shrink-0">
                  <div class="flex h-5 w-5 items-center justify-center rounded-md border-2 border-green-400 bg-green-500 dark:border-green-600 dark:bg-green-600">
                    <svg class="h-full w-full text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
                    </svg>
                  </div>
                </div>

                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-2">
                    <span class="text-base font-semibold text-gray-900 dark:text-white">{{ config.groupName }}</span>
                  </div>
                  <div class="mt-1.5 flex flex-wrap items-center gap-3 text-sm">
                    <span class="inline-flex items-center gap-1 text-gray-500 dark:text-gray-400">
                      <PlatformIcon :platform="config.platform" size="xs" />
                      <span>{{ config.platform }}</span>
                    </span>
                    <span class="text-gray-300 dark:text-dark-500">•</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      {{ t('admin.users.defaultRate') }}: <span class="font-medium text-gray-700 dark:text-gray-300">{{ config.defaultRate }}x</span>
                    </span>
                    <span class="text-gray-300 dark:text-dark-500">•</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      基础倍率: <span class="font-medium text-gray-700 dark:text-gray-300">{{ getBaseRate(config).toFixed(3) }}x</span>
                    </span>
                    <span class="text-gray-300 dark:text-dark-500">•</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      最终倍率: <span class="font-medium text-primary-700 dark:text-primary-300">{{ getFinalRate(config).toFixed(3) }}x</span>
                    </span>
                  </div>
                </div>

                <div class="flex flex-shrink-0 items-center gap-3">
                  <label class="text-sm font-medium text-gray-600 dark:text-gray-400">{{ t('admin.users.customRate') }}</label>
                  <input
                    type="number"
                    step="0.001"
                    min="0"
                    :value="config.customRate ?? ''"
                    @input="updateCustomRate(config.groupId, ($event.target as HTMLInputElement).value)"
                    :placeholder="String(config.defaultRate)"
                    class="hide-spinner w-24 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium transition-colors focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/20 dark:border-dark-500 dark:bg-dark-700 dark:focus:border-primary-500"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>

        <div v-if="groups.length === 0" class="flex flex-col items-center justify-center py-12 text-center">
          <div class="mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700">
            <svg class="h-8 w-8 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
            </svg>
          </div>
          <p class="text-gray-500 dark:text-gray-400">{{ t('common.noGroupsAvailable') }}</p>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" class="btn btn-secondary px-5">{{ t('common.cancel') }}</button>
        <button @click="handleSave" :disabled="submitting" class="btn btn-primary px-6">
          <svg v-if="submitting" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          {{ submitting ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { AdminUser, Group, GroupPlatform } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'

interface GroupRateConfig {
  groupId: number
  groupName: string
  platform: GroupPlatform
  isExclusive: boolean
  defaultRate: number
  customRate: number | null
  isSelected: boolean
}

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits(['close', 'success'])
const { t } = useI18n()
const appStore = useAppStore()

const groups = ref<Group[]>([])
const groupConfigs = ref<GroupRateConfig[]>([])
const originalGroupRates = ref<Record<number, number>>({})
const unifiedRateEnabled = ref(false)
const unifiedRateMultiplier = ref(1)
const loading = ref(false)
const submitting = ref(false)

const exclusiveGroups = computed(() => groups.value.filter((g) => g.is_exclusive))
const publicGroups = computed(() => groups.value.filter((g) => !g.is_exclusive))
const exclusiveGroupConfigs = computed(() => groupConfigs.value.filter((c) => c.isExclusive))
const publicGroupConfigs = computed(() => groupConfigs.value.filter((c) => !c.isExclusive))
const effectiveUnifiedMultiplier = computed(() => {
  if (!unifiedRateEnabled.value) return 1
  const normalized = Number(unifiedRateMultiplier.value)
  if (!Number.isFinite(normalized) || normalized < 0) return 1
  return normalized
})

watch(
  () => props.show,
  (v) => {
    if (v && props.user) {
      void load()
    }
  }
)

const load = async () => {
  loading.value = true
  try {
    const res = await adminAPI.groups.list(1, 1000)
    groups.value = res.items.filter((g) => g.subscription_type === 'standard' && g.status === 'active')

    const userAllowedGroups = props.user?.allowed_groups || []
    const userGroupRates = props.user?.group_rates || {}
    unifiedRateEnabled.value = props.user?.unified_rate_enabled ?? false
    unifiedRateMultiplier.value = props.user?.unified_rate_multiplier ?? 1
    originalGroupRates.value = { ...userGroupRates }

    groupConfigs.value = groups.value.map((g) => ({
      groupId: g.id,
      groupName: g.name,
      platform: g.platform,
      isExclusive: g.is_exclusive,
      defaultRate: g.rate_multiplier,
      customRate: userGroupRates[g.id] ?? null,
      isSelected: g.is_exclusive ? userAllowedGroups.includes(g.id) : true
    }))
  } catch (error) {
    console.error('Failed to load groups:', error)
    appStore.showError(t('common.error'))
  } finally {
    loading.value = false
  }
}

const toggleExclusiveGroup = (groupId: number) => {
  const config = groupConfigs.value.find((c) => c.groupId === groupId)
  if (config && config.isExclusive) {
    config.isSelected = !config.isSelected
  }
}

const updateCustomRate = (groupId: number, value: string) => {
  const config = groupConfigs.value.find((c) => c.groupId === groupId)
  if (!config) return
  if (value === '' || value == null) {
    config.customRate = null
    return
  }
  const numValue = parseFloat(value)
  config.customRate = Number.isNaN(numValue) ? null : numValue
}

const getBaseRate = (config: GroupRateConfig) => config.customRate ?? config.defaultRate
const getFinalRate = (config: GroupRateConfig) => getBaseRate(config) * effectiveUnifiedMultiplier.value

const handleSave = async () => {
  if (!props.user) return
  submitting.value = true
  try {
    const allowedGroups = groupConfigs.value
      .filter((c) => c.isExclusive && c.isSelected)
      .map((c) => c.groupId)

    const groupRates: Record<number, number | null> = {}
    for (const c of groupConfigs.value) {
      const hadOriginalRate = originalGroupRates.value[c.groupId] !== undefined
      if (c.customRate !== null) {
        groupRates[c.groupId] = c.customRate
      } else if (hadOriginalRate) {
        groupRates[c.groupId] = null
      }
    }

    await adminAPI.users.update(props.user.id, {
      allowed_groups: allowedGroups,
      unified_rate_enabled: unifiedRateEnabled.value,
      unified_rate_multiplier: effectiveUnifiedMultiplier.value,
      group_rates: Object.keys(groupRates).length > 0 ? groupRates : undefined
    })

    appStore.showSuccess(t('admin.users.groupConfigUpdated'))
    emit('success')
    emit('close')
  } catch (error: any) {
    console.error('Failed to update user group config:', error)
    appStore.showError(error?.response?.data?.detail || t('common.error'))
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.hide-spinner::-webkit-outer-spin-button,
.hide-spinner::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.hide-spinner {
  -moz-appearance: textfield;
}
</style>
