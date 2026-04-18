<template>
  <BaseDialog
    :show="show"
    title="自动运维"
    width="extra-wide"
    @close="emit('close')"
  >
    <div class="space-y-5">
      <div class="flex items-start justify-between gap-4">
        <div>
          <div class="text-sm font-medium text-gray-900 dark:text-white">全局自动运维配置</div>
          <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            仅处理状态为错误且仍处于调度状态的账号；保存并启用后会立即触发一次自动运维。
          </div>
        </div>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="loading" @click="loadAll">
            刷新
          </button>
          <button class="btn btn-primary" :disabled="saving || loading" @click="saveConfig">
            {{ saving ? '保存中...' : '保存配置' }}
          </button>
        </div>
      </div>

      <div v-if="loading" class="flex items-center justify-center py-10 text-sm text-gray-500 dark:text-gray-400">
        <Icon name="refresh" size="md" class="mr-2 animate-spin" />
        加载中...
      </div>

      <template v-else>
        <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
          <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
            <div class="text-sm font-medium text-gray-900 dark:text-white">运行开关</div>
            <div class="mt-3 flex items-center justify-between">
              <div>
                <div class="text-sm text-gray-700 dark:text-gray-200">启用自动运维</div>
                <div class="text-xs text-gray-500 dark:text-gray-400">保存后立即执行一次，之后按间隔轮询。</div>
              </div>
              <button
                type="button"
                :class="[
                  'relative inline-flex h-6 w-11 items-center rounded-full transition-colors',
                  form.enabled ? 'bg-primary-600' : 'bg-gray-300 dark:bg-dark-500'
                ]"
                @click="form.enabled = !form.enabled"
              >
                <span
                  :class="[
                    'inline-block h-5 w-5 transform rounded-full bg-white transition-transform',
                    form.enabled ? 'translate-x-5' : 'translate-x-1'
                  ]"
                />
              </button>
            </div>
          </div>

          <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
            <label class="mb-1 block text-sm font-medium text-gray-900 dark:text-white">自动触发时间间隔（分钟）</label>
            <input
              v-model.number="form.interval_minutes"
              type="number"
              min="1"
              class="input"
              placeholder="例如 10"
            />
            <div class="mt-2 text-xs text-gray-500 dark:text-gray-400">
              使用正整数分钟；自动触发只对全局符合条件账号生效。
            </div>
          </div>

          <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
            <div class="text-sm font-medium text-gray-900 dark:text-white">当前状态</div>
            <div class="mt-3 space-y-2 text-xs text-gray-600 dark:text-gray-300">
              <div>配置状态：{{ form.configured ? '已保存' : '未保存' }}</div>
              <div>规则数量：{{ form.rules.length }}</div>
              <div>最近日志：{{ logs.length }} 组</div>
              <div>响应样本：{{ samples.length }} 条</div>
            </div>
          </div>
        </div>

        <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
          <div class="mb-3 flex items-center justify-between">
            <div>
              <div class="text-sm font-medium text-gray-900 dark:text-white">测试模型配置</div>
              <div class="text-xs text-gray-500 dark:text-gray-400">自动运维执行“重新测试”时，会按平台配置的模型列表依次尝试。</div>
            </div>
          </div>

          <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
            <div
              v-for="platform in platforms"
              :key="platform.value"
              class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"
            >
              <div class="mb-2 flex items-center justify-between">
                <div class="text-sm font-medium text-gray-800 dark:text-gray-100">{{ platform.label }}</div>
                <button class="text-xs text-gray-500 hover:text-red-500" @click="clearPlatformModels(platform.value)">
                  清空
                </button>
              </div>

              <div class="flex flex-wrap gap-2">
                <span
                  v-for="model in platformModels(platform.value)"
                  :key="`${platform.value}-${model}`"
                  class="inline-flex items-center gap-1 rounded-full bg-primary-50 px-2 py-1 text-xs text-primary-700 dark:bg-primary-900/20 dark:text-primary-300"
                >
                  {{ model }}
                  <button type="button" @click="removePlatformModel(platform.value, model)">
                    <Icon name="x" size="xs" />
                  </button>
                </span>
                <span
                  v-if="platformModels(platform.value).length === 0"
                  class="text-xs text-gray-400 dark:text-gray-500"
                >
                  未配置，届时会使用系统默认测试模型
                </span>
              </div>

              <div class="mt-3 grid grid-cols-1 gap-2 md:grid-cols-[1fr_auto]">
                <Select
                  v-model="selectedModelToAdd[platform.value]"
                  :options="modelOptionSelects[platform.value] || []"
                  placeholder="从系统模型中选择"
                />
                <button class="btn btn-secondary" @click="appendSelectedModel(platform.value)">
                  添加系统模型
                </button>
              </div>

              <div class="mt-2 grid grid-cols-1 gap-2 md:grid-cols-[1fr_auto]">
                <input
                  v-model.trim="customModelToAdd[platform.value]"
                  class="input"
                  placeholder="输入自定义模型 ID"
                  @keyup.enter="appendCustomModel(platform.value)"
                />
                <button class="btn btn-secondary" @click="appendCustomModel(platform.value)">
                  添加自定义模型
                </button>
              </div>
            </div>
          </div>
        </div>

        <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
          <div class="mb-3 flex items-center justify-between">
            <div>
              <div class="text-sm font-medium text-gray-900 dark:text-white">规则编排</div>
              <div class="text-xs text-gray-500 dark:text-gray-400">从上到下依次匹配；拖拽可以调整优先级。</div>
            </div>
            <button class="btn btn-primary" @click="addRule">新增规则</button>
          </div>

          <VueDraggable
            v-model="form.rules"
            :animation="150"
            handle=".drag-handle"
            class="space-y-3"
          >
            <div
              v-for="(rule, index) in form.rules"
              :key="rule.id"
              class="rounded-xl border border-gray-200 p-4 dark:border-dark-700"
            >
              <div class="mb-3 flex items-center justify-between gap-3">
                <div class="flex items-center gap-3">
                  <button type="button" class="drag-handle cursor-move text-gray-400 hover:text-gray-600 dark:hover:text-gray-200">
                    <Icon name="menu" size="sm" />
                  </button>
                  <span class="rounded bg-gray-100 px-2 py-1 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                    #{{ index + 1 }}
                  </span>
                </div>
                <button class="text-xs text-red-500 hover:text-red-600" @click="removeRule(index)">
                  删除规则
                </button>
              </div>

              <div class="grid grid-cols-1 gap-3 lg:grid-cols-2">
                <div>
                  <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">规则名称</label>
                  <input v-model.trim="rule.name" class="input" placeholder="例如：测试命中 deactivated 则暂停调度" />
                </div>

                <div>
                  <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">处置手段</label>
                  <Select v-model="rule.action" :options="actionOptions" />
                </div>
              </div>

              <div class="mt-3">
                <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">规则对象</label>
                <div class="flex flex-wrap gap-3">
                  <label v-for="subject in subjectOptions" :key="subject.value" class="inline-flex items-center gap-1.5 text-sm text-gray-700 dark:text-gray-300">
                    <input
                      type="checkbox"
                      class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                      :checked="rule.subjects.includes(subject.value)"
                      @change="toggleSubjectFromEvent(rule, subject.value, $event)"
                    />
                    {{ subject.label }}
                  </label>
                </div>
              </div>

              <div class="mt-3 grid grid-cols-1 gap-3 lg:grid-cols-[220px_1fr]">
                <div>
                  <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">匹配规则</label>
                  <Select v-model="rule.match_type" :options="matchTypeOptions" />
                </div>
                <div>
                  <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">匹配内容</label>
                  <textarea
                    v-model.trim="rule.pattern"
                    rows="2"
                    class="input"
                    placeholder="输入关键词或子串"
                  />
                </div>
              </div>
            </div>
          </VueDraggable>

          <div v-if="form.rules.length === 0" class="rounded-lg border border-dashed border-gray-300 py-8 text-center text-sm text-gray-400 dark:border-dark-600 dark:text-gray-500">
            暂无规则，未命中账号名称规则时将默认执行重新测试。
          </div>
        </div>

        <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
            <div class="mb-3 flex items-center justify-between">
              <div>
                <div class="text-sm font-medium text-gray-900 dark:text-white">响应样本</div>
                <div class="text-xs text-gray-500 dark:text-gray-400">近 24 小时自动运维捕获到的测试/刷新响应内容去重结果。</div>
              </div>
            </div>

            <div class="max-h-[420px] space-y-3 overflow-auto pr-1">
              <div
                v-for="sample in samples"
                :key="`${sample.subject}-${sample.response_hash}`"
                class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"
              >
                <div class="flex items-center justify-between gap-3">
                  <div class="flex items-center gap-2">
                    <span class="rounded bg-gray-100 px-2 py-0.5 text-[11px] text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                      {{ subjectLabel(sample.subject) }}
                    </span>
                    <span class="text-xs text-gray-500 dark:text-gray-400">出现 {{ sample.occurrences }} 次</span>
                  </div>
                  <span class="text-[11px] text-gray-400 dark:text-gray-500">{{ formatDateTime(sample.last_seen_at) }}</span>
                </div>
                <pre class="mt-2 whitespace-pre-wrap break-words rounded bg-gray-50 p-3 text-xs text-gray-700 dark:bg-dark-700 dark:text-gray-300">{{ sample.response_text }}</pre>
              </div>
              <div v-if="samples.length === 0" class="py-8 text-center text-sm text-gray-400 dark:text-gray-500">
                暂无响应样本
              </div>
            </div>
          </div>

          <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
            <div class="mb-3 flex items-center justify-between">
              <div>
                <div class="text-sm font-medium text-gray-900 dark:text-white">运维记录</div>
                <div class="text-xs text-gray-500 dark:text-gray-400">仅保留最近 24 小时日志。</div>
              </div>
            </div>

            <div class="max-h-[420px] space-y-3 overflow-auto pr-1">
              <div
                v-for="run in logs"
                :key="run.id"
                :class="[
                  'rounded-lg border p-3',
                  highlightRunId === run.id
                    ? 'border-primary-400 bg-primary-50 dark:border-primary-500 dark:bg-primary-900/10'
                    : 'border-gray-200 dark:border-dark-700'
                ]"
              >
                <div class="flex items-center justify-between gap-3">
                  <div class="flex items-center gap-2">
                    <span class="rounded bg-gray-100 px-2 py-0.5 text-[11px] text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                      #{{ run.id }}
                    </span>
                    <span class="rounded px-2 py-0.5 text-[11px]" :class="run.trigger_mode === 'automatic' ? 'bg-blue-50 text-blue-700 dark:bg-blue-900/20 dark:text-blue-300' : 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'">
                      {{ run.trigger_mode === 'automatic' ? '自动' : '手动' }}
                    </span>
                    <span class="rounded px-2 py-0.5 text-[11px]" :class="run.status === 'completed' ? 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300' : run.status === 'failed' ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300' : 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300'">
                      {{ run.status }}
                    </span>
                  </div>
                  <span class="text-[11px] text-gray-400 dark:text-gray-500">{{ formatDateTime(run.started_at) }}</span>
                </div>

                <div class="mt-2 text-xs text-gray-500 dark:text-gray-400">
                  请求 {{ run.total_accounts }} 个账号，符合条件 {{ run.eligible_accounts }} 个，已完成 {{ run.completed_accounts }} 个
                </div>
                <div v-if="run.error_message" class="mt-1 text-xs text-red-500">
                  {{ run.error_message }}
                </div>

                <details class="mt-3">
                  <summary class="cursor-pointer text-xs font-medium text-gray-700 dark:text-gray-200">
                    展开步骤（{{ run.steps?.length || 0 }}）
                  </summary>
                  <div class="mt-2 space-y-2">
                    <div
                      v-for="step in run.steps || []"
                      :key="step.id"
                      class="rounded border border-gray-200 p-2 text-xs dark:border-dark-700"
                    >
                      <div class="flex flex-wrap items-center gap-2">
                        <span class="font-medium text-gray-700 dark:text-gray-200">{{ step.account_name }}</span>
                        <span class="rounded bg-gray-100 px-2 py-0.5 text-[11px] text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                          {{ subjectLabel(step.subject) }}
                        </span>
                        <span class="rounded bg-gray-100 px-2 py-0.5 text-[11px] text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                          {{ actionLabel(step.action) }}
                        </span>
                        <span class="text-gray-400 dark:text-gray-500">{{ step.status }}</span>
                      </div>
                      <div v-if="step.matched_rule_name" class="mt-1 text-gray-500 dark:text-gray-400">
                        规则：{{ step.matched_rule_name }}
                      </div>
                      <pre v-if="step.response_text" class="mt-2 whitespace-pre-wrap break-words rounded bg-gray-50 p-2 text-[11px] text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ step.response_text }}</pre>
                      <pre v-if="step.action_result_text" class="mt-2 whitespace-pre-wrap break-words rounded bg-gray-50 p-2 text-[11px] text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ step.action_result_text }}</pre>
                    </div>
                  </div>
                </details>
              </div>
              <div v-if="logs.length === 0" class="py-8 text-center text-sm text-gray-400 dark:text-gray-500">
                暂无运维记录
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="btn btn-secondary" @click="emit('close')">
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import type {
  AccountAutoOpsAction,
  AccountAutoOpsConfig,
  AccountAutoOpsMatchType,
  AccountAutoOpsModelOption,
  AccountAutoOpsRun,
  AccountAutoOpsRule,
  AccountAutoOpsSample,
  AccountAutoOpsSubject
} from '@/types'

const props = defineProps<{
  show: boolean
  highlightRunId?: number | null
}>()

const emit = defineEmits<{
  close: []
  saved: [config: AccountAutoOpsConfig]
}>()

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const saving = ref(false)
const logs = ref<AccountAutoOpsRun[]>([])
const samples = ref<AccountAutoOpsSample[]>([])
const modelOptions = ref<Record<string, AccountAutoOpsModelOption[]>>({})

const form = reactive<AccountAutoOpsConfig>({
  enabled: false,
  interval_minutes: 10,
  rules: [],
  test_models_by_platform: {},
  configured: false
})

const selectedModelToAdd = reactive<Record<string, string | null>>({})
const customModelToAdd = reactive<Record<string, string>>({})

const platforms = [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' }
]

const subjectOptions: Array<{ value: AccountAutoOpsSubject; label: string }> = [
  { value: 'account_name', label: '账号名称' },
  { value: 'test_response', label: '测试连接' },
  { value: 'refresh_response', label: '刷新令牌' }
]

const matchTypeOptions: Array<{ value: AccountAutoOpsMatchType; label: string }> = [
  { value: 'contains', label: '包含' },
  { value: 'not_contains', label: '不包含' }
]

const actionOptions: Array<{ value: AccountAutoOpsAction; label: string }> = [
  { value: 'retest', label: '重新测试' },
  { value: 'refresh_token', label: '刷新令牌' },
  { value: 'recover_state', label: '恢复状态' },
  { value: 'enable_schedulable', label: '启用调度' },
  { value: 'disable_schedulable', label: '暂停调度' },
  { value: 'delete_account', label: '删除账号' }
]

const modelOptionSelects = computed<Record<string, SelectOption[]>>(() => {
  const result: Record<string, SelectOption[]> = {}
  for (const platform of platforms) {
    result[platform.value] = (modelOptions.value[platform.value] || []).map((item) => ({
      value: item.id,
      label: item.display_name || item.id
    }))
  }
  return result
})

const resetForm = (config?: AccountAutoOpsConfig) => {
  const next = config || {
    enabled: false,
    interval_minutes: 10,
    rules: [],
    test_models_by_platform: {},
    configured: false
  }
  form.enabled = !!next.enabled
  form.interval_minutes = next.interval_minutes || 10
  form.rules = (next.rules || []).map((rule) => ({
    id: rule.id,
    name: rule.name,
    subjects: [...rule.subjects],
    match_type: rule.match_type,
    pattern: rule.pattern,
    action: rule.action
  }))
  form.test_models_by_platform = {}
  for (const platform of platforms) {
    form.test_models_by_platform[platform.value] = [...(next.test_models_by_platform?.[platform.value] || [])]
    selectedModelToAdd[platform.value] = null
    customModelToAdd[platform.value] = ''
  }
  form.configured = !!next.configured
}

const loadAll = async () => {
  loading.value = true
  try {
    const [configRes, logsRes, samplesRes, optionsRes] = await Promise.all([
      adminAPI.accounts.getAutoOpsConfig(),
      adminAPI.accounts.getAutoOpsLogs(),
      adminAPI.accounts.getAutoOpsSamples(),
      adminAPI.accounts.getAutoOpsModelOptions()
    ])
    resetForm(configRes)
    logs.value = logsRes.runs || []
    samples.value = samplesRes.samples || []
    modelOptions.value = optionsRes.model_options || {}
  } catch (error: any) {
    console.error('Failed to load account auto ops data:', error)
    appStore.showError(error?.message || '加载自动运维数据失败')
  } finally {
    loading.value = false
  }
}

const saveConfig = async () => {
  saving.value = true
  try {
    const payload: AccountAutoOpsConfig = {
      enabled: form.enabled,
      interval_minutes: Math.max(1, Number(form.interval_minutes) || 1),
      rules: form.rules.map((rule) => ({
        id: rule.id || buildRuleId(),
        name: rule.name.trim(),
        subjects: [...rule.subjects],
        match_type: rule.match_type,
        pattern: rule.pattern.trim(),
        action: rule.action
      })),
      test_models_by_platform: JSON.parse(JSON.stringify(form.test_models_by_platform))
    }
    const saved = await adminAPI.accounts.updateAutoOpsConfig(payload)
    resetForm(saved)
    emit('saved', saved)
    await loadAll()
    appStore.showSuccess('自动运维配置已保存')
  } catch (error: any) {
    console.error('Failed to save account auto ops config:', error)
    appStore.showError(error?.message || '保存自动运维配置失败')
  } finally {
    saving.value = false
  }
}

const buildRuleId = () => `rule_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`

const addRule = () => {
  form.rules.push({
    id: buildRuleId(),
    name: '',
    subjects: ['test_response'],
    match_type: 'contains',
    pattern: '',
    action: 'recover_state'
  })
}

const removeRule = (index: number) => {
  form.rules.splice(index, 1)
}

const toggleSubject = (rule: AccountAutoOpsRule, subject: AccountAutoOpsSubject, checked: boolean) => {
  if (checked) {
    if (!rule.subjects.includes(subject)) {
      rule.subjects.push(subject)
    }
    return
  }
  rule.subjects = rule.subjects.filter((item) => item !== subject)
}

const toggleSubjectFromEvent = (rule: AccountAutoOpsRule, subject: AccountAutoOpsSubject, event: Event) => {
  const target = event.target as HTMLInputElement | null
  toggleSubject(rule, subject, !!target?.checked)
}

const platformModels = (platform: string) => form.test_models_by_platform[platform] || []

const appendSelectedModel = (platform: string) => {
  const selected = selectedModelToAdd[platform]
  if (!selected) return
  if (!form.test_models_by_platform[platform]) {
    form.test_models_by_platform[platform] = []
  }
  if (!form.test_models_by_platform[platform].includes(String(selected))) {
    form.test_models_by_platform[platform].push(String(selected))
  }
  selectedModelToAdd[platform] = null
}

const appendCustomModel = (platform: string) => {
  const value = (customModelToAdd[platform] || '').trim()
  if (!value) return
  if (!form.test_models_by_platform[platform]) {
    form.test_models_by_platform[platform] = []
  }
  if (!form.test_models_by_platform[platform].includes(value)) {
    form.test_models_by_platform[platform].push(value)
  }
  customModelToAdd[platform] = ''
}

const removePlatformModel = (platform: string, model: string) => {
  form.test_models_by_platform[platform] = platformModels(platform).filter((item) => item !== model)
}

const clearPlatformModels = (platform: string) => {
  form.test_models_by_platform[platform] = []
}

const subjectLabel = (subject: string) => {
  return subjectOptions.find((item) => item.value === subject)?.label || subject
}

const actionLabel = (action: string) => {
  return actionOptions.find((item) => item.value === action)?.label || action
}

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      loadAll()
    }
  }
)
</script>
