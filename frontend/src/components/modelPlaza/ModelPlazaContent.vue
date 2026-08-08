<template>
  <div class="space-y-5">
    <div v-if="!embedded">
      <h1 class="text-2xl font-bold tracking-tight text-gray-900 dark:text-white sm:text-3xl">{{ t('modelPlaza.title') }}</h1>
      <p class="mt-1.5 text-sm text-gray-500 dark:text-dark-400">{{ t('modelPlaza.description') }}</p>
    </div>

    <div
      v-if="descriptionHtml"
      class="plaza-description rounded-2xl border border-gray-100 bg-white px-5 py-4 text-sm shadow-card dark:border-dark-700/50 dark:bg-dark-800/50"
      v-html="descriptionHtml"
    ></div>

    <p v-if="!isAuthenticated" class="flex items-center gap-1.5 text-xs text-gray-400 dark:text-dark-500">
      <Icon name="infoCircle" size="xs" class="h-3.5 w-3.5" />
      {{ t('modelPlaza.anonymousHint') }}
    </p>

    <div v-if="loading" class="flex min-h-[240px] items-center justify-center">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-600/25 border-t-primary-600 dark:border-primary-400/25 dark:border-t-primary-400"></div>
    </div>
    <div
      v-else-if="error"
      class="rounded-2xl border border-red-200 bg-red-50 px-5 py-8 text-center text-sm text-red-600 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300"
    >
      {{ t('modelPlaza.loadFailed') }}
    </div>
    <template v-else>
      <PlazaFilterBar
        :platforms="platforms"
        :groups="groupOptions"
        :rates="rates"
        :platform="selectedPlatform"
        :group-id="selectedGroupId"
        :rate="selectedRate"
        :search="searchQuery"
        @update:platform="selectedPlatform = $event"
        @update:group-id="selectedGroupId = $event"
        @update:rate="selectedRate = $event"
        @update:search="searchQuery = $event"
      />

      <div v-if="displayedGroups.length > 0" class="space-y-5">
        <PlazaGroupSection v-for="g in displayedGroups" :key="g.id" :group="g" />
      </div>
      <div
        v-else
        class="rounded-2xl border border-dashed border-gray-300 px-5 py-12 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400"
      >
        {{ searchActive ? t('modelPlaza.noSearchResult') : t('modelPlaza.empty') }}
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import Icon from '@/components/icons/Icon.vue'
import PlazaFilterBar from './PlazaFilterBar.vue'
import PlazaGroupSection from './PlazaGroupSection.vue'
import type { ModelPlazaGroup, ModelPlazaResponse, PlazaModel } from '@/api/modelPlaza'
import { useAuthStore } from '@/stores/auth'

const props = defineProps<{
  response: ModelPlazaResponse | null
  loading: boolean
  error?: boolean
  embedded?: boolean
}>()

const { t } = useI18n()
const authStore = useAuthStore()
const isAuthenticated = computed(() => authStore.isAuthenticated)

const selectedPlatform = ref<string>('all')
const selectedGroupId = ref<number | 'all'>('all')
const selectedRate = ref<number | 'all'>('all')
const searchQuery = ref('')

const searchActive = computed(() => searchQuery.value.trim() !== '')

const descriptionHtml = computed(() => {
  const md = props.response?.description?.trim()
  if (!md) return ''
  return DOMPurify.sanitize(marked.parse(md) as string)
})

function groupEffectiveRate(g: ModelPlazaGroup): number {
  return g.user_rate_multiplier ?? g.rate_multiplier
}

function modelBillingMode(m: PlazaModel): string {
  return m.pricing?.billing_mode || 'token'
}

function modelEffectiveRate(g: ModelPlazaGroup, m: PlazaModel): number {
  if (modelBillingMode(m) === 'image' && g.image_rate_independent) {
    return g.image_rate_multiplier ?? 1
  }
  return groupEffectiveRate(g)
}

const platforms = computed(() =>
  [...new Set((props.response?.groups ?? []).map((g) => g.platform).filter(Boolean))].sort()
)

const groupOptions = computed(() =>
  (props.response?.groups ?? []).map((g) => ({
    id: g.id,
    name: g.name,
    platform: g.platform,
    rate: groupEffectiveRate(g)
  }))
)

const rates = computed(() =>
  [...new Set((props.response?.groups ?? []).map(groupEffectiveRate))].sort((a, b) => a - b)
)

watch(rates, (list) => {
  if (selectedRate.value !== 'all' && !list.includes(selectedRate.value)) {
    selectedRate.value = 'all'
  }
})

const visibleGroups = computed(() => {
  let groups = props.response?.groups ?? []
  if (selectedPlatform.value !== 'all') {
    groups = groups.filter((g) => g.platform === selectedPlatform.value)
  }
  if (selectedGroupId.value !== 'all') {
    groups = groups.filter((g) => g.id === selectedGroupId.value)
  }
  if (selectedRate.value !== 'all') {
    groups = groups.filter((g) => groupEffectiveRate(g) === selectedRate.value)
  }

  const q = searchQuery.value.trim().toLowerCase()
  if (q) {
    groups = groups
      .map((g) => ({ ...g, models: g.models.filter((m) => m.name.toLowerCase().includes(q)) }))
      .filter((g) => g.models.length > 0)
  }

  return [...groups].sort((a, b) => groupEffectiveRate(a) - groupEffectiveRate(b) || a.name.localeCompare(b.name))
})

const shouldAggregateByPlatform = computed(
  () => selectedPlatform.value === 'all' && selectedGroupId.value === 'all'
)

function cloneAggregateModel(source: PlazaModel, group: ModelPlazaGroup): PlazaModel {
  return {
    ...source,
    rate_multiplier: group.rate_multiplier,
    user_rate_multiplier: group.user_rate_multiplier ?? null,
    image_rate_independent: group.image_rate_independent,
    image_rate_multiplier: group.image_rate_multiplier,
    source_group_id: group.id,
    source_group_name: group.name,
    source_group_subscription_type: group.subscription_type,
    source_group_is_exclusive: group.is_exclusive,
    source_group_peak_rate_enabled: group.peak_rate_enabled,
    source_group_peak_start: group.peak_start,
    source_group_peak_end: group.peak_end,
    source_group_peak_rate_multiplier: group.peak_rate_multiplier
  }
}

function aggregateModelKey(model: PlazaModel): string {
  return `${model.platform}:${model.name}`
}

function aggregateByPlatform(groups: ModelPlazaGroup[]): ModelPlazaGroup[] {
  const platformMap = new Map<
    string,
    {
      group: ModelPlazaGroup
      models: Map<
        string,
        {
          model: PlazaModel
          sourceGroup: ModelPlazaGroup
          rate: number
        }
      >
    }
  >()

  for (const group of groups) {
    let entry = platformMap.get(group.platform)
    if (!entry) {
      entry = {
        group: {
          id: -1,
          name: group.platform,
          description: '',
          platform: group.platform,
          subscription_type: 'standard',
          rate_multiplier: groupEffectiveRate(group),
          peak_rate_enabled: false,
          peak_start: '',
          peak_end: '',
          peak_rate_multiplier: 1,
          is_exclusive: false,
          image_rate_independent: false,
          image_rate_multiplier: 1,
          models: []
        },
        models: new Map()
      }
      platformMap.set(group.platform, entry)
    }

    for (const model of group.models) {
      const rate = modelEffectiveRate(group, model)
      const key = aggregateModelKey(model)
      const existing = entry.models.get(key)
      const better =
        !existing ||
        rate < existing.rate ||
        (rate === existing.rate &&
          (group.name.localeCompare(existing.sourceGroup.name) < 0 ||
            (group.name === existing.sourceGroup.name && group.id < existing.sourceGroup.id)))

      if (better) {
        entry.models.set(key, {
          model: cloneAggregateModel(model, group),
          sourceGroup: group,
          rate
        })
      }
    }
  }

  return [...platformMap.values()]
    .filter((entry) => entry.models.size > 0)
    .map((entry, index) => {
      const models = [...entry.models.values()]
        .sort((a, b) => a.model.name.localeCompare(b.model.name) || a.model.platform.localeCompare(b.model.platform))
        .map((item) => item.model)
      const minRate = Math.min(...[...entry.models.values()].map((item) => item.rate))
      return {
        ...entry.group,
        id: -(index + 1),
        rate_multiplier: minRate,
        models
      }
    })
    .sort((a, b) => a.platform.localeCompare(b.platform))
}

const displayedGroups = computed(() => {
  if (shouldAggregateByPlatform.value) {
    return aggregateByPlatform(visibleGroups.value)
  }
  return visibleGroups.value
})
</script>

<style scoped>
.plaza-description {
  line-height: 1.7;
  overflow-wrap: anywhere;
}

.plaza-description :deep(h1),
.plaza-description :deep(h2),
.plaza-description :deep(h3) {
  @apply mb-2 mt-3 font-semibold text-gray-900 first:mt-0 dark:text-white;
}

.plaza-description :deep(p) {
  @apply mb-2 text-gray-700 last:mb-0 dark:text-dark-200;
}

.plaza-description :deep(a) {
  @apply text-primary-600 underline underline-offset-4 hover:text-primary-700 dark:text-primary-300;
}

.plaza-description :deep(ul) {
  @apply mb-2 list-disc pl-5;
}

.plaza-description :deep(ol) {
  @apply mb-2 list-decimal pl-5;
}

.plaza-description :deep(li) {
  @apply mb-0.5 text-gray-700 dark:text-dark-200;
}

.plaza-description :deep(code) {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs dark:bg-dark-800;
}

.plaza-description :deep(blockquote) {
  @apply my-2 border-l-4 border-gray-300 pl-3 text-gray-600 dark:border-dark-600 dark:text-dark-300;
}
</style>
