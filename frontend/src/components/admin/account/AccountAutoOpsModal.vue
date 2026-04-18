<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.autoOpsDialog.title')"
    width="extra-wide"
    @close="emit('close')"
  >
    <div class="space-y-5" data-testid="auto-ops-modal">
      <div class="flex items-start justify-between gap-4">
        <div>
          <div class="text-sm font-medium text-gray-900 dark:text-white">
            {{ t('admin.accounts.autoOpsDialog.summaryTitle') }}
          </div>
          <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.autoOpsDialog.summaryDescription') }}
          </div>
        </div>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="loading" @click="loadAll">{{ t('common.refresh') }}</button>
          <button class="btn btn-primary" :disabled="saving || loading" @click="saveConfig">{{ saving ? t('common.saving') : t('common.save') }}</button>
        </div>
      </div>

      <div v-if="loading" class="flex items-center justify-center py-10 text-sm text-gray-500 dark:text-gray-400">
        <Icon name="refresh" size="md" class="mr-2 animate-spin" />
        {{ t('common.loading') }}
      </div>

      <template v-else>
        <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
          <div class="rounded-xl border border-gray-200 bg-white/80 p-4 shadow-sm dark:border-dark-600 dark:bg-dark-800/60">
            <div class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.autoOpsDialog.runtimeTitle') }}</div>
            <div class="mt-3 flex items-center justify-between gap-4">
              <div>
                <div class="text-sm text-gray-700 dark:text-gray-200">{{ t('admin.accounts.autoOpsDialog.enabledLabel') }}</div>
                <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoOpsDialog.enabledHint') }}</div>
              </div>
              <button
                type="button"
                :class="['relative inline-flex h-6 w-11 items-center rounded-full transition-colors', form.enabled ? 'bg-primary-600' : 'bg-gray-300 dark:bg-dark-500']"
                @click="form.enabled = !form.enabled"
              >
                <span :class="['inline-block h-5 w-5 transform rounded-full bg-white transition-transform', form.enabled ? 'translate-x-5' : 'translate-x-1']" />
              </button>
            </div>
          </div>

          <div class="rounded-xl border border-gray-200 bg-white/80 p-4 shadow-sm dark:border-dark-600 dark:bg-dark-800/60">
            <label class="mb-1 block text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.autoOpsDialog.intervalLabel') }}</label>
            <input v-model.number="form.interval_minutes" type="number" min="1" class="input" :placeholder="t('admin.accounts.autoOpsDialog.intervalPlaceholder')" />
            <div class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoOpsDialog.intervalHint') }}</div>
          </div>

          <div class="rounded-xl border border-gray-200 bg-white/80 p-4 shadow-sm dark:border-dark-600 dark:bg-dark-800/60">
            <div class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.autoOpsDialog.statusTitle') }}</div>
            <div class="mt-3 grid grid-cols-2 gap-2 text-xs">
              <div class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-700/70">
                <div class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoOpsDialog.configStatus') }}</div>
                <div class="mt-1 font-medium text-gray-800 dark:text-gray-100">
                  {{ form.configured ? t('admin.accounts.autoOpsDialog.configured') : t('admin.accounts.autoOpsDialog.notConfigured') }}
                </div>
              </div>
              <div class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-700/70">
                <div class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoOpsDialog.ruleCount') }}</div>
                <div class="mt-1 font-medium text-gray-800 dark:text-gray-100">{{ form.rules.length }}</div>
              </div>
              <div class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-700/70">
                <div class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoOpsDialog.logCount') }}</div>
                <div class="mt-1 font-medium text-gray-800 dark:text-gray-100">{{ logs.length }}</div>
              </div>
              <div class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-700/70">
                <div class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoOpsDialog.sampleCount') }}</div>
                <div class="mt-1 font-medium text-gray-800 dark:text-gray-100">{{ samples.length }}</div>
              </div>
            </div>
          </div>
        </div>

        <div class="rounded-xl border border-gray-200 bg-white/80 p-4 shadow-sm dark:border-dark-600 dark:bg-dark-800/60">
          <div class="mb-3">
            <div class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.autoOpsDialog.modelsTitle') }}</div>
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoOpsDialog.modelsDescription') }}</div>
          </div>

          <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
            <div v-for="platform in platforms" :key="platform.value" class="rounded-lg border border-gray-200 bg-gray-50/70 p-3 dark:border-dark-700 dark:bg-dark-900/30">
              <div class="mb-2 flex items-center justify-between">
                <div class="text-sm font-medium text-gray-800 dark:text-gray-100">{{ platform.label }}</div>
                <button class="text-xs text-gray-500 hover:text-red-500" @click="clearPlatformModels(platform.value)">{{ t('admin.accounts.autoOpsDialog.clear') }}</button>
              </div>

              <div class="flex flex-wrap gap-2">
                <span
                  v-for="model in platformModels(platform.value)"
                  :key="`${platform.value}-${model}`"
                  class="inline-flex items-center gap-1 rounded-full bg-primary-50 px-2 py-1 text-xs text-primary-700 dark:bg-primary-900/20 dark:text-primary-300"
                >
                  {{ model }}
                  <button type="button" @click="removePlatformModel(platform.value, model)"><Icon name="x" size="xs" /></button>
                </span>
                <span v-if="platformModels(platform.value).length === 0" class="text-xs text-gray-400 dark:text-gray-500">{{ t('admin.accounts.autoOpsDialog.modelsEmpty') }}</span>
              </div>

              <div class="mt-3 grid grid-cols-1 gap-2 md:grid-cols-[1fr_auto]">
                <Select v-model="selectedModelToAdd[platform.value]" :options="modelOptionSelects[platform.value] || []" :placeholder="t('admin.accounts.autoOpsDialog.selectSystemModel')" />
                <button class="btn btn-secondary" @click="appendSelectedModel(platform.value)">{{ t('admin.accounts.autoOpsDialog.addSystemModel') }}</button>
              </div>

              <div class="mt-2 grid grid-cols-1 gap-2 md:grid-cols-[1fr_auto]">
                <input v-model.trim="customModelToAdd[platform.value]" class="input" :placeholder="t('admin.accounts.autoOpsDialog.customModelPlaceholder')" @keyup.enter="appendCustomModel(platform.value)" />
                <button class="btn btn-secondary" @click="appendCustomModel(platform.value)">{{ t('admin.accounts.autoOpsDialog.addCustomModel') }}</button>
              </div>
            </div>
          </div>
        </div>

        <div class="rounded-xl border border-gray-200 bg-white/80 p-4 shadow-sm dark:border-dark-600 dark:bg-dark-800/60">
          <div class="mb-3 flex items-center justify-between">
            <div>
              <div class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.autoOpsDialog.rulesTitle') }}</div>
              <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoOpsDialog.rulesDescription') }}</div>
            </div>
            <button class="btn btn-primary" @click="addRule">{{ t('admin.accounts.autoOpsDialog.addRule') }}</button>
          </div>

          <div v-if="form.rules.length === 0" class="rounded-lg border border-dashed border-gray-300 py-8 text-center text-sm text-gray-400 dark:border-dark-600 dark:text-gray-500">
            {{ t('admin.accounts.autoOpsDialog.rulesEmpty') }}
          </div>

          <div v-else class="space-y-2">
            <div class="hidden items-center gap-3 rounded-lg bg-gray-50 px-3 py-2 text-xs font-medium text-gray-500 dark:bg-dark-700 dark:text-gray-300 lg:grid lg:grid-cols-[28px_72px_1.2fr_140px_100px_1.5fr_150px_112px]" data-testid="auto-ops-rule-table">
              <div></div>
              <div>{{ t('admin.accounts.autoOpsDialog.columns.priority') }}</div>
              <div>{{ t('admin.accounts.autoOpsDialog.columns.name') }}</div>
              <div>{{ t('admin.accounts.autoOpsDialog.columns.subject') }}</div>
              <div>{{ t('admin.accounts.autoOpsDialog.columns.matchType') }}</div>
              <div>{{ t('admin.accounts.autoOpsDialog.columns.pattern') }}</div>
              <div>{{ t('admin.accounts.autoOpsDialog.columns.action') }}</div>
              <div>{{ t('admin.accounts.autoOpsDialog.columns.operation') }}</div>
            </div>

            <VueDraggable v-model="form.rules" item-key="id" handle=".drag-handle" :animation="150" @end="handleRuleDragEnd" class="space-y-2" data-testid="auto-ops-rules-list">
              <div v-for="rule in form.rules" :key="rule.id" class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900/30" :data-testid="`auto-ops-rule-row-${rule.id}`">
                <div class="grid grid-cols-1 gap-3 px-3 py-3 text-sm lg:grid-cols-[28px_72px_1.2fr_140px_100px_1.5fr_150px_112px] lg:items-center">
                  <div class="flex items-center"><button type="button" class="drag-handle cursor-move text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"><Icon name="menu" size="sm" /></button></div>
                  <div class="text-xs font-medium text-gray-700 dark:text-gray-200">
                    <span class="inline-flex min-w-[40px] justify-center rounded-full bg-gray-100 px-2 py-1 dark:bg-dark-700">{{ rule.priority }}</span>
                  </div>
                  <div class="truncate text-gray-900 dark:text-white" :title="rule.name">
                    {{ rule.name || t('admin.accounts.autoOpsDialog.unnamedRule') }}
                  </div>
                  <div class="text-xs text-gray-600 dark:text-gray-300">
                    <span class="inline-flex rounded-full bg-slate-100 px-2 py-1 dark:bg-dark-700">{{ subjectLabel(rule.subject) }}</span>
                  </div>
                  <div class="text-xs text-gray-600 dark:text-gray-300">
                    <span class="inline-flex rounded-full bg-slate-100 px-2 py-1 dark:bg-dark-700">{{ matchTypeLabel(rule.match_type) }}</span>
                  </div>
                  <div class="truncate text-xs text-gray-500 dark:text-gray-400" :title="rule.pattern">{{ summarize(rule.pattern, 72) }}</div>
                  <div class="text-xs text-gray-600 dark:text-gray-300">
                    <span class="inline-flex rounded-full bg-primary-50 px-2 py-1 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">{{ actionLabel(rule.action) }}</span>
                  </div>
                  <div class="flex items-center gap-2">
                    <button class="text-xs text-primary-600 hover:text-primary-700" :data-testid="`auto-ops-edit-${rule.id}`" @click="toggleEdit(rule.id)">{{ editingRuleId === rule.id ? t('admin.accounts.autoOpsDialog.collapse') : t('common.edit') }}</button>
                    <button class="text-xs text-red-500 hover:text-red-600" @click="removeRule(rule.id)">{{ t('common.delete') }}</button>
                  </div>
                </div>

                <div v-if="editingRuleId === rule.id" class="border-t border-gray-200 bg-gray-50 px-3 py-4 dark:border-dark-700 dark:bg-dark-800/70" :data-testid="`auto-ops-edit-panel-${rule.id}`">
                  <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
                    <div>
                      <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.autoOpsDialog.edit.priority') }}</label>
                      <input v-model.number="rule.priority" type="number" min="1" class="input" :data-testid="`auto-ops-priority-${rule.id}`" @blur="handlePriorityBlur(rule)" />
                    </div>
                    <div>
                      <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.autoOpsDialog.edit.name') }}</label>
                      <input v-model.trim="rule.name" class="input" :placeholder="t('admin.accounts.autoOpsDialog.edit.namePlaceholder')" />
                    </div>
                    <div class="xl:col-span-2">
                      <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.autoOpsDialog.edit.subject') }}</label>
                      <div class="grid grid-cols-1 gap-2 md:grid-cols-3">
                        <label
                          v-for="option in subjectOptions"
                          :key="option.value"
                          :class="[
                            'flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors',
                            rule.subject === option.value
                              ? 'border-primary-300 bg-primary-50 text-primary-700 dark:border-primary-500/60 dark:bg-primary-900/20 dark:text-primary-300'
                              : 'border-gray-200 text-gray-700 hover:border-primary-300 dark:border-dark-700 dark:text-gray-300'
                          ]"
                        >
                          <input v-model="rule.subject" type="radio" :name="`auto-ops-subject-${rule.id}`" class="h-4 w-4 border-gray-300 text-primary-600 focus:ring-primary-500" :value="option.value" />
                          <span>{{ option.label }}</span>
                        </label>
                      </div>
                    </div>
                    <div>
                      <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.autoOpsDialog.edit.matchType') }}</label>
                      <Select v-model="rule.match_type" :options="matchTypeOptions" />
                    </div>
                    <div>
                      <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.autoOpsDialog.edit.action') }}</label>
                      <Select v-model="rule.action" :options="actionOptions" />
                    </div>
                    <div class="xl:col-span-2">
                      <label class="mb-1 block text-xs font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.autoOpsDialog.edit.pattern') }}</label>
                      <textarea v-model.trim="rule.pattern" rows="4" class="input min-h-[110px] resize-y" :placeholder="t('admin.accounts.autoOpsDialog.edit.patternPlaceholder')" />
                    </div>
                  </div>
                </div>
              </div>
            </VueDraggable>
          </div>
        </div>
        <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <div class="rounded-xl border border-gray-200 bg-white/80 p-4 shadow-sm dark:border-dark-600 dark:bg-dark-800/60">
            <div class="mb-3">
              <div class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.autoOpsDialog.samplesTitle') }}</div>
              <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoOpsDialog.samplesDescription') }}</div>
            </div>
            <div class="max-h-[420px] space-y-2 overflow-auto pr-1">
              <div v-for="sample in samples" :key="`${sample.subject}-${sample.response_hash}`" class="rounded-lg border border-gray-200 bg-gray-50/70 p-3 dark:border-dark-700 dark:bg-dark-900/30">
                <div class="flex items-center justify-between gap-3 text-xs">
                  <div class="flex items-center gap-2">
                    <span class="rounded bg-gray-100 px-2 py-0.5 text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ subjectLabel(sample.subject) }}</span>
                    <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoOpsDialog.sampleOccurrences', { count: sample.occurrences }) }}</span>
                  </div>
                  <span class="text-gray-400 dark:text-gray-500">{{ formatDateTime(sample.last_seen_at) }}</span>
                </div>
                <pre class="mt-2 whitespace-pre-wrap break-words rounded bg-white p-2 text-[11px] text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ summarize(sample.response_text, 360) }}</pre>
              </div>
              <div v-if="samples.length === 0" class="py-8 text-center text-sm text-gray-400 dark:text-gray-500">{{ t('admin.accounts.autoOpsDialog.samplesEmpty') }}</div>
            </div>
          </div>

          <div class="rounded-xl border border-gray-200 bg-white/80 p-4 shadow-sm dark:border-dark-600 dark:bg-dark-800/60">
            <div class="mb-3">
              <div class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.autoOpsDialog.logsTitle') }}</div>
              <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoOpsDialog.logsDescription') }}</div>
            </div>
            <div class="max-h-[420px] space-y-3 overflow-auto pr-1">
              <div v-for="run in logs" :key="run.id" :class="['rounded-lg border p-3 shadow-sm', highlightRunId === run.id ? 'border-primary-400 bg-primary-50 dark:border-primary-500 dark:bg-primary-900/10' : 'border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900/30']" :data-testid="`auto-ops-run-${run.id}`">
                <div class="flex items-center justify-between gap-3">
                  <div class="flex items-center gap-2">
                    <span class="rounded bg-gray-100 px-2 py-0.5 text-[11px] text-gray-600 dark:bg-dark-700 dark:text-gray-300">#{{ run.id }}</span>
                    <span class="rounded px-2 py-0.5 text-[11px]" :class="triggerModeClass(run.trigger_mode)">{{ run.trigger_mode === 'automatic' ? t('admin.accounts.autoOpsDialog.automaticRun') : t('admin.accounts.autoOpsDialog.manualRun') }}</span>
                    <span class="rounded px-2 py-0.5 text-[11px]" :class="runStatusClass(run.status)">{{ runStatusLabel(run.status) }}</span>
                  </div>
                  <span class="text-[11px] text-gray-400 dark:text-gray-500">{{ formatDateTime(run.started_at) }}</span>
                </div>
                <div class="mt-2 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                  <span>{{ t('admin.accounts.autoOpsDialog.runSummary', { total: run.total_accounts, eligible: run.eligible_accounts, completed: run.completed_accounts }) }}</span>
                  <span class="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] text-slate-600 dark:bg-dark-700 dark:text-gray-300">
                    {{ t('admin.accounts.autoOpsDialog.matchedStepsCount', { count: matchedSteps(run).length }) }}
                  </span>
                </div>
                <div v-if="run.error_message" class="mt-1 text-xs text-red-500">{{ run.error_message }}</div>
                <div class="mt-3 space-y-2">
                  <template v-if="matchedSteps(run).length > 0">
                    <div v-for="step in matchedSteps(run)" :key="step.id" class="rounded border border-gray-200 bg-gray-50/70 p-3 text-xs dark:border-dark-700 dark:bg-dark-800/60" :data-testid="`auto-ops-step-${step.id}`">
                      <div class="flex flex-wrap items-center justify-between gap-2">
                        <div class="font-medium text-gray-700 dark:text-gray-200">{{ step.account_name }}</div>
                        <div class="text-[11px] text-gray-400 dark:text-gray-500">{{ formatDateTime(step.created_at) }}</div>
                      </div>
                      <div class="mt-2 flex flex-wrap gap-2">
                        <span class="rounded-full bg-slate-100 px-2 py-1 text-[11px] text-slate-600 dark:bg-dark-700 dark:text-gray-300">{{ subjectLabel(step.subject) }}</span>
                        <span class="rounded-full bg-primary-50 px-2 py-1 text-[11px] text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">{{ actionLabel(step.action) }}</span>
                        <span class="rounded-full px-2 py-1 text-[11px]" :class="stepStatusClass(step.status)">{{ stepStatusLabel(step.status) }}</span>
                      </div>
                      <div class="mt-2 text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoOpsDialog.matchedRuleLabel') }}：{{ step.matched_rule_name }}</div>
                      <div v-if="matchedRuleMeta(step)?.match_type === 'not_contains'" class="mt-2 rounded bg-amber-50 px-2 py-1 text-[11px] text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('admin.accounts.autoOpsDialog.notContainsLabel', { pattern: matchedRuleMeta(step)?.pattern || '' }) }}</div>
                      <div v-if="step.response_text" class="mt-2">
                        <div class="mb-1 text-[11px] font-medium uppercase tracking-wide text-gray-400 dark:text-gray-500">{{ t('admin.accounts.autoOpsDialog.responseLabel') }}</div>
                        <div class="whitespace-pre-wrap break-words rounded bg-white p-2 text-[11px] text-gray-700 dark:bg-dark-900 dark:text-gray-300" v-html="highlightedResponseHtml(step)" />
                      </div>
                      <div v-if="step.action_result_text" class="mt-2">
                        <div class="mb-1 text-[11px] font-medium uppercase tracking-wide text-gray-400 dark:text-gray-500">{{ t('admin.accounts.autoOpsDialog.actionResultLabel') }}</div>
                        <pre class="whitespace-pre-wrap break-words rounded bg-white p-2 text-[11px] text-gray-700 dark:bg-dark-900 dark:text-gray-300">{{ summarize(step.action_result_text, 240) }}</pre>
                      </div>
                    </div>
                  </template>
                  <div v-else class="rounded border border-dashed border-gray-300 px-3 py-4 text-center text-xs text-gray-400 dark:border-dark-600 dark:text-gray-500">{{ t('admin.accounts.autoOpsDialog.noMatchedRules') }}</div>
                </div>
              </div>
              <div v-if="logs.length === 0" class="py-8 text-center text-sm text-gray-400 dark:text-gray-500">{{ t('admin.accounts.autoOpsDialog.logsEmpty') }}</div>
            </div>
          </div>
        </div>
      </template>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button>
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
import type { AccountAutoOpsAction, AccountAutoOpsConfig, AccountAutoOpsMatchType, AccountAutoOpsRule, AccountAutoOpsRun, AccountAutoOpsSample, AccountAutoOpsStep, AccountAutoOpsSubject } from '@/types'

const props = defineProps<{ show: boolean; highlightRunId?: number | null }>()
const emit = defineEmits<{ close: []; saved: [config: AccountAutoOpsConfig] }>()
const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const saving = ref(false)
const editingRuleId = ref<string | null>(null)
const logs = ref<AccountAutoOpsRun[]>([])
const samples = ref<AccountAutoOpsSample[]>([])
const modelOptions = ref<Record<string, Array<{ id: string; display_name: string }>>>({})
const form = reactive<AccountAutoOpsConfig>({ enabled: false, interval_minutes: 10, rules: [], test_models_by_platform: {}, configured: false })
const selectedModelToAdd = reactive<Record<string, string | null>>({})
const customModelToAdd = reactive<Record<string, string>>({})
const platforms = [{ value: 'anthropic', label: 'Anthropic' }, { value: 'openai', label: 'OpenAI' }, { value: 'gemini', label: 'Gemini' }, { value: 'antigravity', label: 'Antigravity' }]
const subjectOptions = computed<Array<SelectOption & { value: AccountAutoOpsSubject }>>(() => [
  { value: 'account_name', label: t('admin.accounts.autoOpsDialog.subject.account_name') },
  { value: 'test_response', label: t('admin.accounts.autoOpsDialog.subject.test_response') },
  { value: 'refresh_response', label: t('admin.accounts.autoOpsDialog.subject.refresh_response') }
])
const matchTypeOptions = computed<Array<SelectOption & { value: AccountAutoOpsMatchType }>>(() => [
  { value: 'contains', label: t('admin.accounts.autoOpsDialog.matchType.contains') },
  { value: 'not_contains', label: t('admin.accounts.autoOpsDialog.matchType.not_contains') }
])
const actionOptions = computed<Array<SelectOption & { value: AccountAutoOpsAction }>>(() => [
  { value: 'retest', label: t('admin.accounts.autoOpsDialog.action.retest') },
  { value: 'refresh_token', label: t('admin.accounts.autoOpsDialog.action.refresh_token') },
  { value: 'recover_state', label: t('admin.accounts.autoOpsDialog.action.recover_state') },
  { value: 'enable_schedulable', label: t('admin.accounts.autoOpsDialog.action.enable_schedulable') },
  { value: 'disable_schedulable', label: t('admin.accounts.autoOpsDialog.action.disable_schedulable') },
  { value: 'delete_account', label: t('admin.accounts.autoOpsDialog.action.delete_account') }
])
const ruleMetaById = computed<Record<string, AccountAutoOpsRule>>(() => Object.fromEntries(form.rules.map((rule) => [rule.id, rule])))
const modelOptionSelects = computed<Record<string, SelectOption[]>>(() => Object.fromEntries(platforms.map((platform) => [platform.value, (modelOptions.value[platform.value] || []).map((item) => ({ value: item.id, label: item.display_name || item.id }))])))
const matchedSteps = (run: AccountAutoOpsRun): AccountAutoOpsStep[] => (run.steps || []).filter((step) => step.matched_rule_id && step.matched_rule_id !== 'default_retest')
const platformModels = (platform: string) => form.test_models_by_platform[platform] || []
const buildRuleId = () => `rule_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`
const toRule = (rule?: Partial<AccountAutoOpsRule>): AccountAutoOpsRule => ({ id: rule?.id || buildRuleId(), name: rule?.name || '', subject: rule?.subject || 'test_response', priority: rule?.priority && rule.priority > 0 ? rule.priority : (form.rules.length + 1) * 10, match_type: rule?.match_type || 'contains', pattern: rule?.pattern || '', action: rule?.action || 'recover_state' })
const compareRules = (a: AccountAutoOpsRule, b: AccountAutoOpsRule) => (a.priority === b.priority ? a.id.localeCompare(b.id) : a.priority - b.priority)
const sortRulesByPriority = () => form.rules.sort(compareRules)
const normalizeRulePrioritiesByOrder = () => form.rules.forEach((rule, index) => { rule.priority = (index + 1) * 10 })
const summarize = (text: string | null | undefined, max: number) => { const raw = String(text || '').trim(); return raw.length > max ? `${raw.slice(0, max)}…` : raw }
const escapeHtml = (text: string) =>
  text
    .split('&').join('&amp;')
    .split('<').join('&lt;')
    .split('>').join('&gt;')
    .split('"').join('&quot;')
    .split("'").join('&#39;')
const isAsciiWordPattern = (pattern: string) => [...pattern].every((char) => char.charCodeAt(0) <= 0x7f) && /[A-Za-z0-9_]/.test(pattern)
const isAsciiWordChar = (char?: string) => !!char && /[A-Za-z0-9_]/.test(char)
const findStrictAsciiMatches = (source: string, pattern: string) => { const ranges: Array<{ start: number; end: number }> = []; const lowerSource = source.toLowerCase(); const lowerPattern = pattern.toLowerCase(); let cursor = 0; while (cursor < lowerSource.length) { const idx = lowerSource.indexOf(lowerPattern, cursor); if (idx === -1) break; const before = idx > 0 ? lowerSource[idx - 1] : undefined; const after = idx + lowerPattern.length < lowerSource.length ? lowerSource[idx + lowerPattern.length] : undefined; if (!isAsciiWordChar(before) && !isAsciiWordChar(after)) ranges.push({ start: idx, end: idx + lowerPattern.length }); cursor = idx + 1 } return ranges }
const findSubstringMatches = (source: string, pattern: string) => { const ranges: Array<{ start: number; end: number }> = []; const lowerSource = source.toLowerCase(); const lowerPattern = pattern.toLowerCase(); let cursor = 0; while (cursor < lowerSource.length) { const idx = lowerSource.indexOf(lowerPattern, cursor); if (idx === -1) break; ranges.push({ start: idx, end: idx + lowerPattern.length }); cursor = idx + lowerPattern.length } return ranges }
const buildHighlightedHtml = (text: string, pattern: string) => { const source = summarize(text, 320); const query = pattern.trim(); if (!source || !query) return escapeHtml(source); const ranges = isAsciiWordPattern(query) ? findStrictAsciiMatches(source, query) : findSubstringMatches(source, query); if (ranges.length === 0) return escapeHtml(source); let html = ''; let cursor = 0; for (const range of ranges) { html += escapeHtml(source.slice(cursor, range.start)); html += `<mark class="rounded bg-yellow-200 px-0.5 text-gray-900 dark:bg-yellow-400/70">${escapeHtml(source.slice(range.start, range.end))}</mark>`; cursor = range.end } html += escapeHtml(source.slice(cursor)); return html }
const matchedRuleMeta = (step: AccountAutoOpsStep) => ruleMetaById.value[step.matched_rule_id]
const highlightedResponseHtml = (step: AccountAutoOpsStep) => { const rule = matchedRuleMeta(step); if (!step.response_text) return ''; if (!rule || rule.match_type === 'not_contains') return escapeHtml(summarize(step.response_text, 320)); return buildHighlightedHtml(step.response_text, rule.pattern) }
const subjectLabel = (subject: string) => subjectOptions.value.find((item) => item.value === subject)?.label || subject
const matchTypeLabel = (matchType: string) => matchTypeOptions.value.find((item) => item.value === matchType)?.label || matchType
const actionLabel = (action: string) => actionOptions.value.find((item) => item.value === action)?.label || action
const triggerModeClass = (mode: string) => mode === 'automatic' ? 'bg-blue-50 text-blue-700 dark:bg-blue-900/20 dark:text-blue-300' : 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
const runStatusClass = (status: string) => status === 'completed' ? 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300' : status === 'failed' ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300' : 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300'
const stepStatusClass = (status: string) => status === 'action_executed' ? 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300' : status === 'loop_guard_stopped' || status === 'action_failed' ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300' : 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300'
const runStatusLabel = (status: string) => { const key = `admin.accounts.autoOpsDialog.runStatus.${status}`; const translated = t(key); return translated === key ? status : translated }
const stepStatusLabel = (status: string) => { const key = `admin.accounts.autoOpsDialog.stepStatus.${status}`; const translated = t(key); return translated === key ? status : translated }
const resetForm = (config?: AccountAutoOpsConfig) => { const next = config || { enabled: false, interval_minutes: 10, rules: [], test_models_by_platform: {}, configured: false }; form.enabled = !!next.enabled; form.interval_minutes = next.interval_minutes || 10; form.rules = (next.rules || []).map((rule) => toRule(rule)); sortRulesByPriority(); form.test_models_by_platform = {}; for (const platform of platforms) { form.test_models_by_platform[platform.value] = [...(next.test_models_by_platform?.[platform.value] || [])]; selectedModelToAdd[platform.value] = null; customModelToAdd[platform.value] = '' } form.configured = !!next.configured; if (editingRuleId.value && !form.rules.some((rule) => rule.id === editingRuleId.value)) editingRuleId.value = null }
const loadAll = async () => { loading.value = true; try { const [configRes, logsRes, samplesRes, optionsRes] = await Promise.all([adminAPI.accounts.getAutoOpsConfig(), adminAPI.accounts.getAutoOpsLogs(), adminAPI.accounts.getAutoOpsSamples(), adminAPI.accounts.getAutoOpsModelOptions()]); resetForm(configRes); logs.value = logsRes.runs || []; samples.value = samplesRes.samples || []; modelOptions.value = optionsRes.model_options || {} } catch (error: any) { console.error('Failed to load account auto ops data:', error); appStore.showError(error?.message || t('admin.accounts.autoOpsDialog.toast.loadFailed')) } finally { loading.value = false } }
const validateRules = () => form.rules.every((rule) => !!rule.name.trim() && !!rule.subject && !!rule.match_type && !!rule.pattern.trim() && !!rule.action && rule.priority > 0)
const saveConfig = async () => { if (!validateRules()) { appStore.showError(t('admin.accounts.autoOpsDialog.toast.validationFailed')); return } saving.value = true; try { sortRulesByPriority(); normalizeRulePrioritiesByOrder(); const payload: AccountAutoOpsConfig = { enabled: form.enabled, interval_minutes: Math.max(1, Number(form.interval_minutes) || 1), rules: form.rules.map((rule) => ({ id: rule.id || buildRuleId(), name: rule.name.trim(), subject: rule.subject, priority: rule.priority, match_type: rule.match_type, pattern: rule.pattern.trim(), action: rule.action })), test_models_by_platform: JSON.parse(JSON.stringify(form.test_models_by_platform)) }; const saved = await adminAPI.accounts.updateAutoOpsConfig(payload); resetForm(saved); emit('saved', saved); await loadAll(); appStore.showSuccess(t('admin.accounts.autoOpsDialog.toast.saveSuccess')) } catch (error: any) { console.error('Failed to save account auto ops config:', error); appStore.showError(error?.message || t('admin.accounts.autoOpsDialog.toast.saveFailed')) } finally { saving.value = false } }
const addRule = () => { form.rules.push(toRule()); sortRulesByPriority(); editingRuleId.value = form.rules[form.rules.length - 1]?.id || null }
const removeRule = (ruleId: string) => { form.rules = form.rules.filter((rule) => rule.id !== ruleId); if (editingRuleId.value === ruleId) editingRuleId.value = null; normalizeRulePrioritiesByOrder() }
const toggleEdit = (ruleId: string) => { editingRuleId.value = editingRuleId.value === ruleId ? null : ruleId }
const handlePriorityBlur = (rule: AccountAutoOpsRule) => { if (!(rule.priority > 0)) rule.priority = 10; sortRulesByPriority() }
const handleRuleDragEnd = () => normalizeRulePrioritiesByOrder()
const appendSelectedModel = (platform: string) => { const selected = selectedModelToAdd[platform]; if (!selected) return; if (!form.test_models_by_platform[platform]) form.test_models_by_platform[platform] = []; if (!form.test_models_by_platform[platform].includes(String(selected))) form.test_models_by_platform[platform].push(String(selected)); selectedModelToAdd[platform] = null }
const appendCustomModel = (platform: string) => { const value = (customModelToAdd[platform] || '').trim(); if (!value) return; if (!form.test_models_by_platform[platform]) form.test_models_by_platform[platform] = []; if (!form.test_models_by_platform[platform].includes(value)) form.test_models_by_platform[platform].push(value); customModelToAdd[platform] = '' }
const removePlatformModel = (platform: string, model: string) => { form.test_models_by_platform[platform] = platformModels(platform).filter((item) => item !== model) }
const clearPlatformModels = (platform: string) => { form.test_models_by_platform[platform] = [] }
watch(() => props.show, (visible) => { if (visible) loadAll() }, { immediate: true })
</script>
